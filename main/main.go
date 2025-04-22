package main

import (
	"context"
	"log"
	"mini-rpc/clients"
	"mini-rpc/logger"
	"mini-rpc/registry"
	"mini-rpc/server"
	"mini-rpc/types"
	"net"
	"net/http"
	"sync"
	"time"
)

// RPC框架需要解决的问题：

// 客户端请求 → 网络二进制流 → 解码器 → argv(reflect.Value)
//                      ↓
//业务处理 → replyv(reflect.Value) → 编码器 → 网络二进制流 → 客户端响应

//func startServer(addr chan string) {
//	// pick a free port
//	l, err := net.Listen("tcp", ":0")
//	if err != nil {
//		log.Fatal("network error:", err)
//	}
//	log.Println("start rpc server on", l.Addr())
//	addr <- l.Addr().String()
//	server.Accept(l)
//}

//func startServer(addr chan string) {
//	var foo types.Foo
//	logger.ServerLog("开始执行服务注册: server.Register(&foo)")
//	if err := server.Register(&foo); err != nil {
//		log.Fatal("register error:", err)
//	}
//	// pick a free port
//	l, err := net.Listen("tcp", ":0")
//	logger.ServerLog("开始监听端口: " + l.Addr().String())
//	if err != nil {
//		log.Fatal("network error:", err)
//	}
//	//log.Println("start rpc server on", l.Addr())
//	// 通道可以实现异步通信，使得服务器启动和调用者获取地址信息的过程可以并发进行。
//	// 而直接返回地址参数会使调用者阻塞，直到服务器启动完成并返回地址
//	server.HandleHTTP()
//	addr <- l.Addr().String()
//
//	// 如果直接返回string的端口地址，需要等待server.Accept(l)执行完毕，这个时候调用方才能获取string
//	server.Accept(l)
//}

//func main() {
//	// 初始化开发环境日志
//	logger.InitLogger("development")
//	defer logger.Logger.Sync() // 确保日志缓冲区刷新
//
//	// 传入的参数 0 表示不设置任何额外的输出标志。这意味着日志记录器在输出日志时，只会输出你传递给它的日志信息，而不会包含任何额外的时间、日期、文件名等信息
//	log.SetFlags(0)
//	addr := make(chan string)
//	go startServer(addr)
//
//	// in fact, following code is like a simple minirpc clients
//	// 客户端尝试建立连接
//	//conn, _ := net.Dial("tcp", <-addr)
//	logger.ClientLog("客户端尝试建立连接: clients.Dial(\"tcp\", <-addr)")
//	client, _ := clients.Dial("tcp", <-addr)
//	defer func() { _ = client.Close() }()
//
//	// 等待连接建立完成
//	time.Sleep(time.Second)
//
//	//// 发送选项信息
//	//_ = json.NewEncoder(conn).Encode(server.DefaultOption)
//	//
//	//// 创建编解码器，用于后续的编码和解码
//	//cc := codec.NewGobCodec(conn)
//	//
//	//// send request & receive response
//	//for i := 0; i < 5; i++ {
//	//	h := &codec.Header{
//	//		ServiceMethod: "Foo.Sum",
//	//		Seq:           uint64(i),
//	//	}
//	//	//使用编码器cc将请求头和请求体编码发送给服务端
//	//	_ = cc.Write(h, fmt.Sprintf("minirpc req %d", h.Seq))
//	//
//	//	// 检查发送的请求
//	//	_ = cc.ReadHeader(h)
//	//	var reply string
//	//	_ = cc.ReadBody(&reply)
//	//	log.Println("reply:", reply)
//	//}
//	// send request & receive response
//	var wg sync.WaitGroup
//	for i := 0; i < 2; i++ {
//		wg.Add(1)
//		go func(i int) {
//			defer wg.Done()
//			args := &types.Args{Num1: i, Num2: i * i}
//
//			// 创建一个带有 1 秒超时设置的 context.Context，用于控制 RPC 调用的生命周期，在超时发生时能够及时取消操作并处理相关逻辑
//			ctx, _ := context.WithTimeout(context.Background(), time.Second)
//
//			var reply int
//			logger.ClientLog(fmt.Sprintf("在客户端通过rpc请求远程调用 Foo.Sum 计算%d + %d", args.Num1, args.Num2))
//			if err := client.Call(ctx, "Foo.Sum", args, &reply); err != nil {
//				log.Fatal("call Foo.Sum error in main:", err)
//			}
//			//log.Println("reply:", reply)
//			logger.ClientLog(fmt.Sprintf("%d + %d = %d", args.Num1, args.Num2, reply))
//		}(i)
//	}
//	wg.Wait()
//}

//func startServer(addrCh chan string) {
//	var foo types.Foo
//	l, _ := net.Listen("tcp", ":9999")
//	_ = server.Register(&foo)
//	server.HandleHTTP()
//	addrCh <- l.Addr().String()
//	_ = http.Serve(l, nil)
//}
//
//func call(addrCh chan string) {
//	client, _ := clients.DialHTTP("tcp", <-addrCh)
//	defer func() { _ = client.Close() }()
//
//	time.Sleep(time.Second)
//	// send request & receive response
//	var wg sync.WaitGroup
//	for i := 0; i < 5; i++ {
//		wg.Add(1)
//		go func(i int) {
//			defer wg.Done()
//			args := &types.Args{Num1: i, Num2: i * i}
//			var reply int
//			if err := client.Call(context.Background(), "Foo.Sum", args, &reply); err != nil {
//				log.Fatal("call Foo.Sum error:", err)
//			}
//			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
//		}(i)
//	}
//	wg.Wait()
//}
//
//func main() {
//	logger.InitLogger("development")
//	defer logger.Logger.Sync() // 确保日志缓冲区刷新
//	log.SetFlags(0)
//	ch := make(chan string)
//	go call(ch)
//	startServer(ch)
//}

// ----------------------------------------------------day 3----------------------------------------------------
//type Foo int
//
//type Args struct{ Num1, Num2 int }
//
//func (f Foo) Sum(args Args, reply *int) error {
//	*reply = args.Num1 + args.Num2
//	return nil
//}
//
//func startServer(addr chan string) {
//	var foo Foo
//	if err := server.Register(&foo); err != nil {
//		log.Fatal("register error:", err)
//	}
//	// pick a free port
//	l, err := net.Listen("tcp", ":0")
//	if err != nil {
//		log.Fatal("network error:", err)
//	}
//	log.Println("start rpc server on", l.Addr())
//	addr <- l.Addr().String()
//	server.Accept(l)
//}
//
//func main() {
//	logger.InitLogger("development")
//	defer logger.Logger.Sync() // 确保日志缓冲区刷新
//	log.SetFlags(0)
//	addr := make(chan string)
//	go startServer(addr)
//	client, _ := clients.Dial("tcp", <-addr)
//	defer func() { _ = client.Close() }()
//
//	time.Sleep(time.Second)
//	// send request & receive response
//	var wg sync.WaitGroup
//	for i := 0; i < 5; i++ {
//		wg.Add(1)
//		go func(i int) {
//			defer wg.Done()
//			args := &Args{Num1: i, Num2: i * i}
//			var reply int
//			if err := client.Call(context.Background(), "Foo.Sum", args, &reply); err != nil {
//				log.Fatal("call Foo.Sum error:", err)
//			}
//			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
//		}(i)
//	}
//	wg.Wait()
//}

// ----------------------------------------------------day 6----------------------------------------------------
//func startServer(addrCh chan string) {
//	var foo types.Foo
//	l, _ := net.Listen("tcp", ":0")
//	server := server.NewServer()
//	_ = server.Register(&foo)
//	log.Println("打印服务器的地址" + l.Addr().String())
//	addrCh <- l.Addr().String()
//	server.Accept(l)
//}
//
//func foo(lbClient *clients.LoadBalancedClient, ctx context.Context, typ, serviceMethod string, args *types.Args) {
//	var reply int
//	var err error
//	switch typ {
//	case "call":
//		err = lbClient.Call(ctx, serviceMethod, args, &reply)
//	case "broadcast":
//		err = lbClient.Broadcast(ctx, serviceMethod, args, &reply)
//	}
//	if err != nil {
//		log.Printf("%s %s error: %v", typ, serviceMethod, err)
//	} else {
//		log.Printf("%s %s success: %d + %d = %d", typ, serviceMethod, args.Num1, args.Num2, reply)
//	}
//}
//
//func main() {
//	logger.InitLogger("development")
//	defer logger.Logger.Sync() // 确保日志缓冲区刷新
//	log.SetFlags(0)
//	ch1 := make(chan string)
//	ch2 := make(chan string)
//	// start two servers
//	go startServer(ch1)
//	go startServer(ch2)
//
//	addr1 := <-ch1
//	addr2 := <-ch2
//
//	time.Sleep(time.Second)
//	call(addr1, addr2)
//	broadcast(addr1, addr2)
//}
//
//func broadcast(addr1, addr2 string) {
//	d := clients.NewMultiServerDiscovery([]string{"tcp@" + addr1, "tcp@" + addr2})
//	xc := clients.NewLoadBalancedClient(d, clients.RandomSelect, nil)
//	defer func() { _ = xc.Close() }()
//	var wg sync.WaitGroup
//	for i := 0; i < 5; i++ {
//		wg.Add(1)
//		go func(i int) {
//			defer wg.Done()
//			foo(xc, context.Background(), "broadcast", "Foo.Sum", &types.Args{Num1: i, Num2: i * i})
//			// expect 2 - 5 timeout
//			ctx, _ := context.WithTimeout(context.Background(), time.Second*2)
//			foo(xc, ctx, "broadcast", "Foo.Sleep", &types.Args{Num1: i, Num2: i * i})
//		}(i)
//	}
//	wg.Wait()
//}
//
//func call(addr1, addr2 string) {
//	multiServersDiscovery := clients.NewMultiServerDiscovery([]string{"tcp@" + addr1, "tcp@" + addr2})
//	xc := clients.NewLoadBalancedClient(multiServersDiscovery, clients.RandomSelect, nil)
//	defer func() { _ = xc.Close() }()
//	// send request & receive response
//	var wg sync.WaitGroup
//	for i := 0; i < 5; i++ {
//		wg.Add(1)
//		go func(i int) {
//			defer wg.Done()
//			foo(xc, context.Background(), "call", "Foo.Sum", &types.Args{Num1: i, Num2: i * i})
//		}(i)
//	}
//	wg.Wait()
//}

// ----------------------------------------------------day 7----------------------------------------------------
func startRegistry(wg *sync.WaitGroup) {
	l, _ := net.Listen("tcp", ":9999")
	registry.HandleHTTP()
	wg.Done()
	_ = http.Serve(l, nil)
}

func startServer(registryAddr string, wg *sync.WaitGroup) {
	var foo types.Foo
	l, _ := net.Listen("tcp", ":0")
	server := server.NewServer()
	_ = server.Register(&foo)
	registry.Heartbeat(registryAddr, "ComputeService", "tcp@"+l.Addr().String(), 0)
	wg.Done()
	server.Accept(l)
}

func call(registry string) {
	d := clients.NewRegistryDiscovery(registry, 0)
	lbc := clients.NewLoadBalancedClient(d, clients.RandomSelect, nil)
	defer func() { _ = lbc.Close() }()
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(lbc, context.Background(), "call", "ComputeService", "Foo.Sum", &types.Args{Num1: i, Num2: i * i})
		}(i)
	}
	wg.Wait()
}

func broadcast(registry string) {
	d := clients.NewRegistryDiscovery(registry, 0)
	lbc := clients.NewLoadBalancedClient(d, clients.RandomSelect, nil)
	defer func() { _ = lbc.Close() }()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(lbc, context.Background(), "broadcast", "ComputeService", "Foo.Sum", &types.Args{Num1: i, Num2: i * i})
			// expect 2 - 5 timeout
			ctx, _ := context.WithTimeout(context.Background(), time.Second*2)
			foo(lbc, ctx, "broadcast", "ComputeService", "Foo.Sleep", &types.Args{Num1: i, Num2: i * i})
		}(i)
	}
	wg.Wait()
}

func foo(lbClient *clients.LoadBalancedClient, ctx context.Context, typ, serviceName, serviceMethod string, args *types.Args) {
	var reply int
	var err error
	switch typ {
	case "call":
		err = lbClient.Call(ctx, serviceName, serviceMethod, args, &reply)
	case "broadcast":
		err = lbClient.Broadcast(ctx, serviceName, serviceMethod, args, &reply)
	}
	if err != nil {
		log.Printf("%s %s error: %v", typ, serviceMethod, err)
	} else {
		log.Printf("%s %s success: %d + %d = %d", typ, serviceMethod, args.Num1, args.Num2, reply)
	}
}

func main() {
	logger.InitLogger("development")
	defer logger.Logger.Sync() // 确保日志缓冲区刷新
	log.SetFlags(0)
	registryAddr := "http://localhost:9999/_minirpc_/registry"
	var wg sync.WaitGroup
	wg.Add(1)
	go startRegistry(&wg)
	wg.Wait()

	time.Sleep(time.Second)
	wg.Add(2)
	go startServer(registryAddr, &wg)
	go startServer(registryAddr, &wg)
	wg.Wait()

	time.Sleep(time.Second)
	call(registryAddr)
	broadcast(registryAddr)
}
