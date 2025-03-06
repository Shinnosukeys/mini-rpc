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
	"reflect"
	"strings"
	"sync"
)

const MagicNumber = 0x3bef5c

// | Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
// | <------      固定 JSON 编码      ------>  | <-------   编码方式由 CodeType 决定    -------> |
type Option struct {
	MagicNumber int        // MagicNumber marks this is a mini-rpc request
	CodecType   codec.Type // clients may choose different Codec to encode body
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
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
	serviceMap sync.Map
}

// NewServer returns a new Server.
func NewServer() *Server {
	return &Server{}
}

// DefaultServer is the default instance of *Server.
var DefaultServer = NewServer()

// Register publishes in the server the set of methods of the
func (server *Server) Register(rcvr interface{}) error {
	logger.ServerLog("初始化service: service.NewService(rcvr)")
	s := service.NewService(rcvr)
	logger.ServerLog("将初始化后的service放入sync.Map: server.serviceMap.LoadOrStore(s.Name, s)")
	if _, dup := server.serviceMap.LoadOrStore(s.Name, s); dup {
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
	svci, ok := server.serviceMap.Load(serviceName)
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
	logger.ServerLog("开始不断循环监听新的客户端连接")
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
	logger.ServerLog("从conn中读取option并确认是否符合要求: json.NewDecoder(conn).Decode(&opt)")
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
	server.serveCodec(cc)
}

// invalidRequest is a placeholder for response argv when error occurs
var invalidRequest = struct{}{}

func (server *Server) serveCodec(cc codec.Codec) {
	sending := new(sync.Mutex) // make sure to send a complete response
	wg := new(sync.WaitGroup)  // wait until all request are handled
	logger.ServerLog("开始进入for循环，并通过指定编解码器（codec.Codec）处理接收到的客户端请求！！！")
	// 1. 支持长连接
	// 2. 保证了请求处理的顺序性
	// 3. 在循环中，如果读取或处理某个请求时出现错误，服务器可以根据错误类型进行相应的处理（如发送错误响应），然后继续循环读取下一个请求，而不会因为一个请求的错误而关闭整个连接，从而实现一定程度的错误恢复和持续服务
	for {
		req, err := server.readRequest(cc)
		logger.ServerLog("server.readRequest(cc) 读取到一个请求: " + req.String())
		if err != nil {
			if req == nil {
				break // it's not possible to recover, so close the connection
			}
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go server.handleRequest(cc, req, sending, wg)
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
	logger.ServerLog(fmt.Sprintf("cc.ReadHeader(&h)方法读取到对应的响应头: cc.ReadHeader(&h), 请求序列: %d", h.Seq))
	req := &request{h: &h}
	// TODO: now we don't know the type of request argv
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
	}
	logger.ServerLog(fmt.Sprintf("cc.ReadBody(argvi) 方法读取到对应的响应体: cc.ReadBody(argvi), 请求序列: %d", req.h.Seq))
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

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	// TODO, should call registered rpc methods to get the right reply
	// day 1, just print argv and send a hello message
	defer wg.Done()
	//log.Println(req.h, req.argv.Elem())
	//req.replyv = reflect.ValueOf(fmt.Sprintf("geerpc resp %d", req.h.Seq))
	err := req.svc.Call(req.mtype, req.argv, req.replyv)
	if err != nil {
		req.h.Error = err.Error()
		server.sendResponse(cc, req.h, invalidRequest, sending)
		return
	}
	logger.ServerLog("sendResponse 准备发送处理后的请求: " + req.String())
	server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
	//log.Printf("sendResponse 发送处理后的请求完毕！！！: %s\n", req)
}

// Accept accepts connections on the listener and serves requests
// for each incoming connection.
func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

func (r *request) String() string {
	var argStr, replyStr string
	// 处理 argv 字段
	if r.argv.IsValid() && r.argv.CanInterface() {
		argStr = fmt.Sprintf("%v", r.argv.Interface())
	}
	// 处理 replyv 字段
	if r.replyv.IsValid() {
		// 检查 replyv 是否为指针类型
		if r.replyv.Kind() == reflect.Ptr {
			// 解引用指针
			replyvValue := r.replyv.Elem()
			if replyvValue.IsValid() && replyvValue.CanInterface() {
				replyStr = fmt.Sprintf("%v", replyvValue.Interface())
			}
		} else if r.replyv.CanInterface() {
			replyStr = fmt.Sprintf("%v", r.replyv.Interface())
		}
	}
	// 提取 h 字段的信息
	var serviceMethod string
	if r.h != nil {
		serviceMethod = r.h.ServiceMethod
	}
	// 提取 svc 字段的信息
	var serviceName string
	if r.svc != nil {
		serviceName = r.svc.Name
	}
	// 提取 mtype 字段的信息
	var methodName string
	var argTypeName string
	var replyTypeName string
	if r.mtype != nil {
		methodName = r.mtype.Method.Name
		if r.mtype.ArgType != nil {
			argTypeName = r.mtype.ArgType.Name()
		}
		if r.mtype.ReplyType != nil {
			// 处理指针类型的 ReplyType
			if r.mtype.ReplyType.Kind() == reflect.Ptr {
				replyTypeName = r.mtype.ReplyType.Elem().Name()
			} else {
				replyTypeName = r.mtype.ReplyType.Name()
			}
		}
	}
	// 格式化输出，添加 serviceMethod 信息
	return fmt.Sprintf("服务方法: %s, 服务名: %s, 方法名: %s, 请求参数类型: %s, 请求参数值: %s, 响应参数类型: %s, 响应参数值: %s",
		serviceMethod, serviceName, methodName, argTypeName, argStr, replyTypeName, replyStr)
}
