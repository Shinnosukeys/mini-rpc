package clients

import (
	"context"
	"io"
	"mini-rpc/server"
	"reflect"
	"sync"
)

type SelectMode int

const (
	RandomSelect     SelectMode = iota // select randomly
	RoundRobinSelect                   // select using Robbin algorithm
)

type LoadBalancedClient struct {
	d       Discovery
	mode    SelectMode
	opt     *server.Option
	mu      sync.Mutex // protect following
	clients map[string]*Client
}

var _ io.Closer = (*LoadBalancedClient)(nil)

// NewLoadBalancedClient 构造函数需要传入三个参数，服务发现实例 Discovery、负载均衡模式 SelectMode 以及协议选项 Option。
// 为了尽量地复用已经创建好的 Socket 连接，使用 clients 保存创建成功的 Client 实例，并提供 Close 方法在结束后，关闭已经建立的连接
func NewLoadBalancedClient(d Discovery, mode SelectMode, opt *server.Option) *LoadBalancedClient {
	return &LoadBalancedClient{d: d, mode: mode, opt: opt, clients: make(map[string]*Client)}
}

func (l *LoadBalancedClient) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, client := range l.clients {
		// I have no idea how to deal with error, just ignore it.
		_ = client.Close()
		delete(l.clients, key)
	}
	return nil
}

func (l *LoadBalancedClient) dial(rpcAddr string) (*Client, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	client, ok := l.clients[rpcAddr]
	if ok && !client.IsAvailable() {
		_ = client.Close()
		delete(l.clients, rpcAddr)
		client = nil
	}
	if client == nil {
		var err error
		client, err = XDial(rpcAddr, l.opt)
		if err != nil {
			return nil, err
		}
		l.clients[rpcAddr] = client
	}
	return client, nil
}

func (l *LoadBalancedClient) call(rpcAddr string, ctx context.Context, serviceMethod string, args, reply interface{}) error {
	client, err := l.dial(rpcAddr)
	if err != nil {
		return err
	}
	return client.Call(ctx, serviceMethod, args, reply)
}

// Call invokes the named function, waits for it to complete,
// and returns its error status.
// xc will choose a proper server.
func (l *LoadBalancedClient) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	rpcAddr, err := l.d.Get(l.mode)
	if err != nil {
		return err
	}
	return l.call(rpcAddr, ctx, serviceMethod, args, reply)
}

// Broadcast invokes the named function for every server registered in discovery
func (l *LoadBalancedClient) Broadcast(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	// 获取所有可用的服务器地址
	servers, err := l.d.GetAll()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex // protect e and replyDone
	var e error
	replyDone := reply == nil // if reply is nil, don't need to set value: replyDone 被初始化为 true，因为 reply 为 nil，不需要进行赋值操作
	// 借助 context.WithCancel 确保有错误发生时，快速失败
	ctx, cancel := context.WithCancel(ctx)
	for _, rpcAddr := range servers {

		// 为每个服务器地址启动一个新的 goroutine，并增加 WaitGroup 的计数
		wg.Add(1)
		go func(rpcAddr string) {
			defer wg.Done()
			var clonedReply interface{}

			// 如果 reply 不为 nil，则使用反射创建一个与 reply 相同类型的新对象，用于接收每个请求的响应
			if reply != nil {
				clonedReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}
			// 调用 call 方法向指定的服务器地址发送 RPC 请求，并将响应存储在 clonedReply 中
			err := l.call(rpcAddr, ctx, serviceMethod, args, clonedReply)
			mu.Lock()

			// 如果某个请求失败且之前没有记录过错误，则将该错误赋值给 e，并调用 cancel() 取消所有未完成的请求
			if err != nil && e == nil {
				e = err
				cancel() // if any call failed, cancel unfinished calls
			}

			// 如果某个请求成功且还没有将响应结果赋值给 reply，则使用反射将 clonedReply 的值赋值给 reply，并将 replyDone 标记为 true
			if err == nil && !replyDone {
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(clonedReply).Elem())
				replyDone = true
			}
			mu.Unlock()
		}(rpcAddr)
	}
	wg.Wait()
	return e
}
