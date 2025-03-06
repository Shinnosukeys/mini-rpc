package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type GobCodec struct {
	conn io.ReadWriteCloser
	buf  *bufio.Writer
	dec  *gob.Decoder
	enc  *gob.Encoder
}

// 作用是进行编译时的接口实现检查。具体来说，这行代码将nil指针转换为*GobCodec类型
// 并赋值给一个类型为Codec的变量。如果GobCodec没有完全实现Codec接口的所有方法，编译器会在此处报错
// 从而确保类型正确实现了接口。这种方法在Go语言中是一种常见的模式，用于静态检查接口的实现，避免在运行时才发现问题
var _ Codec = (*GobCodec)(nil)

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn), // 它用于从 conn（一个 io.ReadWriteCloser 类型的连接）中读取数据并进行反序列化
		enc:  gob.NewEncoder(buf),  //enc序列化后的数据会存储在buf中， 它用于将数据序列化并写入到 buf（一个 bufio.Writer）中
	}
}

// gob.Decoder 的 Decode 方法会阻塞，等待网络连接中数据的到来。
func (c *GobCodec) ReadHeader(h *Header) error {
	// conn 通常代表一个网络连接，当没有足够的数据到达时，Decode 方法会阻塞当前的执行流程，等待数据的到来。
	return c.dec.Decode(h)
}

func (c *GobCodec) ReadBody(body interface{}) error {
	// conn 通常代表一个网络连接，当没有足够的数据到达时，Decode 方法会阻塞当前的执行流程，等待数据的到来。
	return c.dec.Decode(body)
}

// Write 方法中的 buf.Flush() 会阻塞，等待网络连接的发送缓冲区有足够的空间来写入数据。
func (c *GobCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		_ = c.buf.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()
	// c.enc.Encode 会将数据序列化到 buf 中，但最终数据需要通过 buf.Flush() 方法将缓冲区中的数据刷新到底层的 conn 连接中
	if err = c.enc.Encode(h); err != nil {
		log.Println("rpc: gob error encoding header:", err)
		return
	}
	if err = c.enc.Encode(body); err != nil {
		log.Println("rpc: gob error encoding body:", err)
		return
	}
	return
}

func (c *GobCodec) Close() error {
	return c.conn.Close()
}
