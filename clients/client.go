package clients

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mini-rpc/codec"
	"mini-rpc/service"
	"net"
	"sync"
)

// Call represents an active RPC.
type Call struct {
	Seq           uint64
	ServiceMethod string      // format "<service>.<method>"
	Args          interface{} // arguments to the function
	Reply         interface{} // reply from the function
	Error         error       // if error occurs, it will be set
	Done          chan *Call  // Strobes when call is complete.
}

// 当调用结束时，会调用 call.done() 通知调用方
func (call *Call) done() {
	call.Done <- call
}

type Client struct {
	cc      codec.Codec
	header  codec.Header //header 只有在请求发送时才需要，而请求发送是互斥的，因此每个客户端只需要一个，声明在 Client 结构体中可以复用
	opt     *service.Option
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
	client.mu.Lock()
	defer client.mu.Unlock()

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
	defer client.mu.Unlock()

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
	defer client.sending.Unlock()

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
		call := client.removeCall(seq)
		if call != nil {
			call.Error = err
			call.done()

		}
	}

}

func (client *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		err = client.cc.ReadHeader(&h)
		if err != nil {
			client.terminateCalls(err)
		}
		callSeq := h.Seq
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
			call.done()
		}
	}
	client.terminateCalls(err)
}

func NewClient(conn net.Conn, opt *service.Option) (*Client, error) {
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("rpc clients: codec error:", err)
		return nil, err
	}
	// send options with server
	// 发送选项信息
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc clients: options error: ", err)
		_ = conn.Close()
		return nil, err
	}
	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cc codec.Codec, opt *service.Option) *Client {
	client := &Client{
		seq:     100, // seq starts with 1, 0 means invalid call
		cc:      cc,
		opt:     opt,
		pending: make(map[uint64]*Call),
	}
	go client.receive()
	return client
}

func checkOptions(opts ...*service.Option) (*service.Option, error) {
	// if opts is nil or pass nil as parameter
	if len(opts) == 0 || opts[0] == nil {
		return service.DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]
	opt.MagicNumber = service.DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = service.DefaultOption.CodecType
	}
	return opt, nil
}

func Dial(network, address string, opts ...*service.Option) (client *Client, err error) {
	opt, err := checkOptions(opts...)
	if err != nil {
		return nil, err
	}

	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}

	defer func() {
		if client == nil {
			_ = conn.Close()
		}
	}()
	return NewClient(conn, opt)
}

// Go invokes the function asynchronously.
// It returns the Call structure representing the invocation.
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc clients: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	client.send(call)
	return call
}

// Call invokes the named function, waits for it to complete,
// and returns its error status.
func (client *Client) Call(serviceMethod string, args, reply interface{}) error {
	call := <-client.Go(serviceMethod, args, reply, make(chan *Call, 1)).Done
	return call.Error
}
