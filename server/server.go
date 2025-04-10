package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mini-rpc/codec"
	"mini-rpc/logger"
	"mini-rpc/service"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

const (
	MagicNumber      = 0x3bef5c
	Connected        = "200 Connected to Mini RPC"
	DefaultRPCPath   = "/_miniprc_"
	DefaultDebugPath = "/debug/minirpc"
)

// | Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
// | <------      固定 JSON 编码      ------>  | <-------   编码方式由 CodeType 决定    -------> |
type Option struct {
	MagicNumber int        // MagicNumber marks this is a mini-rpc request
	CodecType   codec.Type // clients may choose different Codec to encode body

	ConnectTimeout time.Duration // 0 means no limit
	HandleTimeout  time.Duration
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,

	ConnectTimeout: time.Second * 10,
}

// request stores all information of a call
type request struct {
	h            *codec.Header // header of request
	argv, replyv reflect.Value // argv and replyv of request

	svc   *service.Service
	mtype *service.MethodType
}

// Server represents an RPC Server.
type Server struct {
	// sync.Map是Go语言标准库中提供的并发安全的映射，用于在多个 goroutine 同时访问时保证数据的一致性
	ServiceMap sync.Map
}

// NewServer returns a new Server.
func NewServer() *Server {
	return &Server{}
}

// DefaultServer is the default instance of *Server.
var DefaultServer = NewServer()

// Register publishes in the server the set of methods of the
func (server *Server) Register(rcvr interface{}) error {
	s := service.NewService(rcvr)
	if _, dup := server.ServiceMap.LoadOrStore(s.Name, s); dup {
		return errors.New("rpc: service already defined: " + s.Name)
	}
	return nil
}

// Register publishes the receiver's methods in the DefaultServer.
func Register(rcvr interface{}) error { return DefaultServer.Register(rcvr) }

func (server *Server) findService(serviceMethod string) (svc *service.Service, mtype *service.MethodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svci, ok := server.ServiceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
		return
	}
	svc = svci.(*service.Service)
	mtype = svc.Methods[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}
	return
}

// Accept accepts connections on the listener and serves requests
// for each incoming connection.
func (server *Server) Accept(lis net.Listener) {
	// for 循环的目的是让服务器持续监听新的客户端连接
	// 开启一个新的 goroutine 来处理该连接（通过 go server.ServeConn(conn)），然后循环继续等待下一个客户端连接
	// 如果 lis.Accept() 返回错误（例如网络异常、服务器关闭等情况），则会打印错误信息并返回，从而结束 Accept 方法的执行，服务器不再监听新的连接。
	logger.ServerLog("Accept方法 开始不断循环监听新的客户端连接")
	for {
		// 该方法会一直阻塞，程序执行流会停留在这一行代码处，不会继续执行后面的语句。
		// 直到有新的客户端尝试连接到服务器，Accept 方法才会返回一个表示客户端连接的 net.Conn 对象和一个可能的错误 err
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		// 获取客户端地址信息
		clientAddr := conn.RemoteAddr().String()
		logger.ServerLog("监听到一个新的客户端连接，客户端地址: " + clientAddr)
		go server.ServeConn(conn) // 开启子协程处理服务连接
	}
}

// ServeConn runs the server on a single connection.
// ServeConn blocks, serving the connection until the clients hangs up.
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() { _ = conn.Close() }()
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}
	codecFuncMap := codec.NewCodecFuncMap[opt.CodecType]
	if codecFuncMap == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}
	// get codec.Codec
	cc := codecFuncMap(conn)
	server.serveCodec(cc, &opt)
}

// invalidRequest is a placeholder for response argv when error occurs
var invalidRequest = struct{}{}

func (server *Server) serveCodec(cc codec.Codec, opt *Option) {
	sending := new(sync.Mutex) // make sure to send a complete response
	wg := new(sync.WaitGroup)  // wait until all request are handled
	logger.ServerLog("serverCode方法 开始进入for循环，并通过指定编解码器（codec.Codec）处理接收到的客户端请求！！！")
	// 1. 支持长连接
	// 2. 保证了请求处理的顺序性
	// 3. 在循环中，如果读取或处理某个请求时出现错误，服务器可以根据错误类型进行相应的处理（如发送错误响应），然后继续循环读取下一个请求，而不会因为一个请求的错误而关闭整个连接，从而实现一定程度的错误恢复和持续服务
	for {
		req, err := server.readRequest(cc)
		//logger.ServerLog("server.readRequest(cc) 读取到一个请求: " + req.String())
		if err != nil {
			if req == nil {
				break // it's not possible to recover, so close the connection
			}
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go server.handleRequest(cc, req, sending, wg, opt.HandleTimeout)
	}
	wg.Wait()
	_ = cc.Close()
}

func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	//h, err := server.readRequestHeader(cc)
	//if err != nil {
	//	return nil, err
	//}

	var h codec.Header
	var err error
	// server中的cc.ReadHeader(&h)方法会阻塞当前的执行流程，等待数据的到来
	if err = cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	req := &request{h: &h}
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}

	req.argv = req.mtype.NewArgv()
	req.replyv = req.mtype.NewReplyv()

	// day 1, just suppose it's string
	//req.argv = reflect.New(reflect.TypeOf(""))

	// make sure that argvi is a pointer, ReadBody need a pointer as parameter
	// 若 req.argv 是值类型（非指针），直接通过反射修改其值会失败，因为值类型不可寻址
	// 通过 Addr() 获取指针后，可确保后续操作（如反序列化、字段赋值等）能够修改原始数据
	// Interface() 方法将 req.argv（假设是 reflect.Value 类型）转换为接口值 argvi。此时 argvi 的类型可能与原始值的类型一致
	argvi := req.argv.Interface()
	// 检查 req.argv 的类型是否是指针类型​
	if req.argv.Type().Kind() != reflect.Ptr {
		// 通过 Addr() 获取 req.argv 的指针​（即 reflect.Value 表示的指针），再通过 Interface() 将其转换为接口值。此时 argvi 将是一个指针类型的接口
		argvi = req.argv.Addr().Interface()
	}

	if err := cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read argv err:", err)
		return req, err
	}
	return req, nil
}

//func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
//	var h codec.Header
//	if err := cc.ReadHeader(&h); err != nil {
//		if err != io.EOF && err != io.ErrUnexpectedEOF {
//			log.Println("rpc server: read header error:", err)
//		}
//		return nil, err
//	}
//	return &h, nil
//}

func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

type serverResult struct {
	req *request
	err error
}

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	// TODO, should call registered rpc methods to get the right reply
	// day 1, just print argv and send a hello message
	defer wg.Done()
	//log.Println(req.h, req.argv.Elem())
	//req.replyv = reflect.ValueOf(fmt.Sprintf("minirpc resp %d", req.h.Seq))

	// err := req.svc.Call(req.mtype, req.argv, req.replyv)
	// 超时处理
	called := make(chan struct{})
	sent := make(chan struct{})
	go func() {
		err := req.svc.Call(req.mtype, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		//logger.ServerLog("sendResponse 准备发送处理后的请求: " + req.String())
		server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()

	if timeout == 0 {
		<-called
		<-sent
		return
	}

	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		//logger.ServerLog("sendResponse 准备发送处理后的请求: " + req.String())
		server.sendResponse(cc, req.h, invalidRequest, sending)
	case <-called:
		<-sent
	}
}

// Accept accepts connections on the listener and serves requests
// for each incoming connection.
func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

// Server 类型实现了 ServeHTTP 方法，所以 Server 类型的实例能够当作 HTTP 处理器来处理 HTTP 请求
// ServeHTTP implements a http.Handler that answers RPC requests.
func (server *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "CONNECT" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, "405 must CONNECT\n")
		return
	}
	// Hijack 方法会将底层的 TCP 连接从 HTTP 服务器中分离出来，交给调用者管理
	// 此时 HTTP 服务器会将连接的控制权完全交给 RPC 服务，但仍依赖 http.Serve 的底层循环来持续监听新连接
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Print("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
		return
	}

	// 在成功接管连接后，向客户端发送一个简单的 HTTP 响应头（HTTP/1.0）
	_, _ = io.WriteString(conn, "HTTP/1.0 "+Connected+"\n\n")
	// 调用 server.ServeConn(conn) 方法处理 RPC 请求
	server.ServeConn(conn)
}

// HandleHTTP registers an HTTP handler for RPC messages on rpcPath.
// It is still necessary to invoke http.Serve(), typically in a go statement.
func (server *Server) HandleHTTP() {
	// 通过 http.Handle 方法将 DefaultRPCPath（默认的 RPC 路径）与 server 绑定
	// 当客户端访问 DefaultRPCPath 时，请求会被 ServeHTTP 方法处理。
	http.Handle(DefaultRPCPath, server)
	http.Handle(DefaultDebugPath, DebugHTTP{Server: server})
	log.Println("rpc server debug path:", DefaultDebugPath)
}

// HandleHTTP is a convenient approach for default server to register HTTP handlers
func HandleHTTP() {
	DefaultServer.HandleHTTP()
}
