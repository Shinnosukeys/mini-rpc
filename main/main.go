package main

import (
	"fmt"
	"log"
	"mini-rpc/clients"
	"mini-rpc/service"
	"net"
	"sync"
	"time"
)

// RPC框架需要解决的问题：

// 客户端请求 → 网络二进制流 → 解码器 → argv(reflect.Value)
//                      ↓
//业务处理 → replyv(reflect.Value) → 编码器 → 网络二进制流 → 客户端响应

func startServer(addr chan string) {
	// pick a free port
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	service.Accept(l)
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	go startServer(addr)

	// in fact, following code is like a simple geerpc clients
	// 客户端尝试建立连接
	//conn, _ := net.Dial("tcp", <-addr)
	client, _ := clients.Dial("tcp", <-addr)
	defer func() { _ = client.Close() }()

	// 等待连接建立完成
	time.Sleep(time.Second)

	//// 发送选项信息
	//_ = json.NewEncoder(conn).Encode(service.DefaultOption)
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
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := fmt.Sprintf("geerpc req %d", i)
			var reply string
			if err := client.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Println("reply:", reply)
		}(i)
	}
	wg.Wait()
}
