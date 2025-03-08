package clients

import (
	"context"
	"mini-rpc/logger"
	"mini-rpc/server"
	"mini-rpc/types"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func _assert(condition bool, msg string) {
	if !condition {
		panic(msg)
	}
}

func TestClient_dialTimeout(t *testing.T) {
	logger.InitLogger("development")
	defer logger.Logger.Sync() // 确保日志缓冲区刷新
	t.Parallel()
	l, _ := net.Listen("tcp", ":0")

	newClientFunc := func(conn net.Conn, opt *server.Option) (client *Client, err error) {
		_ = conn.Close()
		time.Sleep(time.Second * 2)
		return nil, nil
	}
	t.Run("timeout", func(t *testing.T) {
		_, err := dialTimeout(newClientFunc, "tcp", l.Addr().String(), &server.Option{ConnectTimeout: time.Second})
		_assert(err != nil && strings.Contains(err.Error(), "connect timeout"), "expect a timeout error")
	})
	t.Run("0", func(t *testing.T) {
		_, err := dialTimeout(newClientFunc, "tcp", l.Addr().String(), &server.Option{ConnectTimeout: 0})
		_assert(err == nil, "0 means no limit")
	})
}

func startServer(addr chan string) {
	var b Bar
	var foo types.Foo
	_ = server.Register(&b)
	_ = server.Register(&foo)
	// pick a free port
	l, _ := net.Listen("tcp", ":0")
	addr <- l.Addr().String()
	server.Accept(l)
}

type Bar int

func (b Bar) Timeout(argv int, reply *int) error {
	time.Sleep(time.Second * 2)
	return nil
}

func TestClient_Call(t *testing.T) {
	logger.InitLogger("development")
	defer logger.Logger.Sync() // 确保日志缓冲区刷新
	t.Parallel()
	addrCh := make(chan string)
	go startServer(addrCh)
	addr := <-addrCh
	time.Sleep(time.Second)

	t.Run("client timeout", func(t *testing.T) {
		client, _ := Dial("tcp", addr)
		ctx, _ := context.WithTimeout(context.Background(), time.Second)
		var reply int
		err := client.Call(ctx, "Foo.Timeout", 1, &reply)
		if err == nil || !strings.Contains(err.Error(), ctx.Err().Error()) {
			t.Errorf("expect a timeout error, but got %v", err)
		}
		//_assert(err != nil && strings.Contains(err.Error(), ctx.Err().Error()), "expect a timeout error")
	})
	t.Run("server handle timeout", func(t *testing.T) {
		client, _ := Dial("tcp", addr, &server.Option{
			HandleTimeout: time.Second,
		})
		var reply int
		err := client.Call(context.Background(), "Foo.Timeout", 1, &reply)
		//_assert(err != nil && strings.Contains(err.Error(), "handle timeout"), "expect a timeout error")
		if err == nil || !strings.Contains(err.Error(), "handle timeout") {
			t.Errorf("expect a timeout error, but got %v", err)
		}
	})
}

func TestXDial(t *testing.T) {
	logger.InitLogger("development")
	defer logger.Logger.Sync() // 确保日志缓冲区刷新
	if runtime.GOOS == "linux" {
		ch := make(chan struct{})
		addr := "/tmp/minirpc.sock"
		go func() {
			_ = os.Remove(addr)
			l, err := net.Listen("unix", addr)
			if err != nil {
				t.Fatal("failed to listen unix socket")
			}
			ch <- struct{}{}
			server.Accept(l)
		}()
		<-ch
		_, err := XDial("unix@" + addr)
		_assert(err == nil, "failed to connect unix socket")
	}
}
