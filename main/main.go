package main

import (
	"fmt"
	"log"
	"mini-rpc/clients"
	"mini-rpc/logger"
	"mini-rpc/server"
	"mini-rpc/types"
	"net"
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

func startServer(addr chan string) {
	var foo types.Foo
	logger.ServerLog("开始执行服务注册: server.Register(&foo)")
	if err := server.Register(&foo); err != nil {
		log.Fatal("register error:", err)
	}
	// pick a free port
	l, err := net.Listen("tcp", ":0")
	logger.ServerLog("开始监听端口: " + l.Addr().String())
	if err != nil {
		log.Fatal("network error:", err)
	}
	//log.Println("start rpc server on", l.Addr())
	// 通道可以实现异步通信，使得服务器启动和调用者获取地址信息的过程可以并发进行。
	// 而直接返回地址参数会使调用者阻塞，直到服务器启动完成并返回地址
	addr <- l.Addr().String()

	// 如果直接返回string的端口地址，需要等待server.Accept(l)执行完毕，这个时候调用方才能获取string
	server.Accept(l)
}

func main() {
	// 初始化开发环境日志
	logger.InitLogger("development")
	defer logger.Logger.Sync() // 确保日志缓冲区刷新

	// 传入的参数 0 表示不设置任何额外的输出标志。这意味着日志记录器在输出日志时，只会输出你传递给它的日志信息，而不会包含任何额外的时间、日期、文件名等信息
	log.SetFlags(0)
	addr := make(chan string)
	go startServer(addr)

	// in fact, following code is like a simple geerpc clients
	// 客户端尝试建立连接
	//conn, _ := net.Dial("tcp", <-addr)
	logger.ClientLog("客户端尝试建立连接: clients.Dial(\"tcp\", <-addr)")
	client, _ := clients.Dial("tcp", <-addr)
	defer func() { _ = client.Close() }()

	// 等待连接建立完成
	time.Sleep(time.Second)

	//// 发送选项信息
	//_ = json.NewEncoder(conn).Encode(server.DefaultOption)
	//
	//// 创建编解码器，用于后续的编码和解码
	//cc := codec.NewGobCodec(conn)
	//
	//// send request & receive response
	//for i := 0; i < 5; i++ {
	//	h := &codec.Header{
	//		ServiceMethod: "Foo.Sum",
	//		Seq:           uint64(i),
	//	}
	//	//使用编码器cc将请求头和请求体编码发送给服务端
	//	_ = cc.Write(h, fmt.Sprintf("geerpc req %d", h.Seq))
	//
	//	// 检查发送的请求
	//	_ = cc.ReadHeader(h)
	//	var reply string
	//	_ = cc.ReadBody(&reply)
	//	log.Println("reply:", reply)
	//}
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &types.Args{Num1: i, Num2: i * i}
			var reply int
			logger.ClientLog(fmt.Sprintf("在客户端通过rpc请求远程调用 Foo.Sum 计算%d + %d", args.Num1, args.Num2))
			if err := client.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			//log.Println("reply:", reply)
			logger.ClientLog(fmt.Sprintf("%d + %d = %d", args.Num1, args.Num2, reply))
		}(i)
	}
	wg.Wait()
}
