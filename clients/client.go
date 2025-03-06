package clients

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mini-rpc/codec"
	"mini-rpc/logger"
	"mini-rpc/server"
	"mini-rpc/types"
	"net"
	"sync"
)

// Call represents an active RPC.
type Call struct {
	Seq           uint64
	ServiceMethod string      // format "<server>.<method>"
	Args          interface{} // arguments to the function
	Reply         interface{} // reply from the function
	Error         error       // if error occurs, it will be set
	Done          chan *Call  // Strobes when call is complete.
}

// 当调用结束时，会调用 call.done() 通知调用方
func (call *Call) done() {
	logger.ClientLog(fmt.Sprintf("请求处理完成，服务方法: %s，准备将结果发送到 Done 通道。callSeq: %d", call.ServiceMethod, call.Seq))
	call.Done <- call
	logger.ClientLog(fmt.Sprintf("结果已发送到 Done 通道，服务方法: %s", call.ServiceMethod))
}

type Client struct {
	cc      codec.Codec
	header  codec.Header //header 只有在请求发送时才需要，而请求发送是互斥的，因此每个客户端只需要一个，声明在 Client 结构体中可以复用
	opt     *server.Option
	pending map[uint64]*Call //存储未处理完的请求，键是编号，值是 Call 实例
	mu      sync.Mutex
	sending sync.Mutex
	seq     uint64
	closed  bool
}

func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closed {
		return errors.New("clients already closed")
	}
	client.closed = true
	return nil
}

var _ io.Closer = (*Client)(nil)

// 将参数 call 添加到 clients.pending 中，并更新 clients.seq
func (client *Client) registerCall(call *Call) (uint64, error) {
	// 它首先对 client.mu 互斥锁进行加锁，确保在注册过程中不会有其他协程同时修改客户端的状态
	client.mu.Lock()
	defer func() {
		client.mu.Unlock()
		logger.ClientLog(fmt.Sprintf("registerCall方法 lient.mu.Lock()互斥锁进行解锁，此时请求编号: %d", client.seq-1))
	}()
	logger.ClientLog(fmt.Sprintf("registerCall方法 lient.mu.Lock()互斥锁进行加锁，此时请求编号: %d", client.seq))

	if client.closed {
		return 0, errors.New("clients already closed")
	}

	// client.seq = call.Seq
	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

// 从 clients.pending 中移除对应的 call，并返回
func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer func() {
		client.mu.Unlock()
		logger.ClientLog(fmt.Sprintf("removeCall方法 lient.mu.Lock()互斥锁进行解锁，此时请求编号: %d", seq))
	}()
	logger.ClientLog(fmt.Sprintf("removeCall方法 lient.mu.Lock()互斥锁进行加锁，此时请求编号: %d", seq))

	if client.closed {
		return nil
	}
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

// 服务端或客户端发生错误时调用，将 shutdown 设置为 true，且将错误信息通知所有 pending 状态的 call
func (client *Client) terminateCalls(err error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.sending.Lock()
	defer client.sending.Unlock()
	client.closed = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

func (client *Client) send(call *Call) {
	client.sending.Lock()
	defer func() {
		client.sending.Unlock()
		logger.ClientLog(fmt.Sprintf("send方法准备完毕 client.sending.Unlock() 互斥锁进行解锁，此时请求编号: %d", client.seq-1))
	}()
	logger.ClientLog(fmt.Sprintf("send方法准备执行 client.sending.Lock() 互斥锁进行加锁，此时请求编号: %d", client.seq))

	// 注册一个请求
	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	// prepare request header
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	err = client.cc.Write(&client.header, call.Args)
	if err != nil {
		call = client.removeCall(seq)
		if call != nil {
			call.Error = err
			call.done()

		}
	}
}

func (client *Client) receive() {
	var err error
	logger.ClientLog("receive()方法 进入一个循环，只要没有错误就继续接收服务端的响应")
	for err == nil {
		var h codec.Header
		err = client.cc.ReadHeader(&h)
		logger.ClientLog(fmt.Sprintf("cc.ReadHeader(&h)方法读取到对应的响应头: client.cc.ReadHeader(&h), CallSeq:%d", h.Seq))
		if err != nil {
			client.terminateCalls(err)
		}
		callSeq := h.Seq
		logger.ClientLog(fmt.Sprintf("从客户端的 pending 映射中移除对应的 call 并返回: client.removeCall(callSeq), CallSeq:%d", h.Seq))
		call := client.removeCall(callSeq)
		switch {
		// 可能是请求没有发送完整，或者因为其他原因被取消，但是服务端仍旧处理了
		case call == nil:
			err = client.cc.ReadBody(nil)
		// 可能是请求没有发送完整，或者因为其他原因被取消，但是服务端仍旧处理了
		case call.Error != nil:
			err = call.Error
			call.done()
		// 请求正常
		default:
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			logger.ClientLog(fmt.Sprintf("cc.ReadHeader(&h)方法读取到对应的响应体: client.cc.ReadBody(call.Reply), CallSeq:%d", h.Seq))
			call.done()
		}
	}
	client.terminateCalls(err)
}

func NewClient(conn net.Conn, opt *server.Option) (*Client, error) {
	newCodecFuncMap := codec.NewCodecFuncMap[opt.CodecType]
	if newCodecFuncMap == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("rpc clients: codec error:", err)
		return nil, err
	}
	// send options with server
	// 发送选项信息
	logger.ClientLog("将选项信息发送到服务端: json.NewEncoder(conn).Encode(opt)")
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc clients: options error: ", err)
		_ = conn.Close()
		return nil, err
	}
	cc := newCodecFuncMap(conn)
	logger.ClientLog("调用 newClientCodec 函数，传入编解码器实例和选项信息，创建一个新的客户端实例: newClientCodec(cc, opt)")
	return newClientCodec(cc, opt), nil
}

func newClientCodec(cc codec.Codec, opt *server.Option) *Client {
	client := &Client{
		seq:     100, // seq starts with 1, 0 means invalid call
		cc:      cc,
		opt:     opt,
		pending: make(map[uint64]*Call),
	}
	logger.ClientLog("启动一个新的协程，调用 client.receive 方法，用于接收服务端的响应: go client.receive()")
	go client.receive()
	return client
}

func checkOptions(opts ...*server.Option) (*server.Option, error) {
	// if opts is nil or pass nil as parameter
	if len(opts) == 0 || opts[0] == nil {
		return server.DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]
	opt.MagicNumber = server.DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = server.DefaultOption.CodecType
	}
	return opt, nil
}

func Dial(network, address string, opts ...*server.Option) (client *Client, err error) {
	logger.ClientLog("检查Option是否符合要求: checkOptions(opts...)")
	opt, err := checkOptions(opts...)
	if err != nil {
		return nil, err
	}

	logger.ClientLog("向服务端发送连接请求: net.Dial(network, address)")
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}

	defer func() {
		if client == nil {
			_ = conn.Close()
		}
	}()
	logger.ClientLog("调用 NewClient 函数创建一个新的客户端实例: NewClient(conn, opt)")
	return NewClient(conn, opt)
}

// Go invokes the function asynchronously.
// It returns the Call structure representing the invocation.
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 1)
	} else if cap(done) == 0 {
		log.Panic("rpc clients: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	argObj, ok := args.(*types.Args)
	if !ok {
		logger.ClientLog("传入的 args 不是 *Args 类型，无法获取具体值")
	}

	logger.ClientLog(fmt.Sprintf("Go 方法开始，发起对 %s 的异步调用, 计算 %d+%d", serviceMethod, argObj.Num1, argObj.Num2))
	go client.send(call)
	logger.ClientLog(fmt.Sprintf("Go 方法返回 Call 对象，等待异步调用完成，服务方法: %s 计算 %d+%d", serviceMethod, argObj.Num1, argObj.Num2))
	return call
}

// Call invokes the named function, waits for it to complete,
// and returns its error status.
func (client *Client) Call(serviceMethod string, args, reply interface{}) error {
	// 调用 Go 方法发起异步调用，并通过 <- 操作符从 done 通道中接收调用结果
	// 进行类型断言，将 args 转换为 *Args 类型
	argObj, ok := args.(*types.Args)
	if !ok {
		logger.ClientLog("传入的 args 不是 *Args 类型，无法获取具体值")
	}
	// <-client.Go(...).Done 是一个阻塞操作，它会等待 Done 通道中有数据发送过来，也就是等待远程调用完成。
	// 一旦远程调用完成，结果会通过 Done 通道发送回来，此时 <-client.Go(...).Done 会解除阻塞并返回调用结果
	logger.ClientLog(fmt.Sprintf("Call 方法调用 Go 方法，计算 %d+%d", argObj.Num1, argObj.Num2))
	call := <-client.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done
	logger.ClientLog(fmt.Sprintf("Call 方法接收到异步调用结果，计算 %d+%d", argObj.Num1, argObj.Num2))
	return call.Error
}
