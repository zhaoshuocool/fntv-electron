package utils

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// 假代理：完成方法协商、回 CONNECT 成功，然后立刻写一个数据字节。
// 客户端如果多读了一个字节，这个字节就会被吞掉，后面的读取会超时。
// 地址类型按 RFC 1928：0x01 IPv4，0x03 域名，0x04 IPv6。
func serveSocks(conn net.Conn, observed chan<- byte) {
	hello := make([]byte, 3)
	if _, err := io.ReadFull(conn, hello); err != nil {
		observed <- 0
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		observed <- 0
		return
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		observed <- 0
		return
	}
	atyp := head[3]
	observed <- atyp

	switch atyp {
	case 0x01:
		if _, err := io.ReadFull(conn, make([]byte, 4)); err != nil {
			return
		}
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return
		}
		if _, err := io.ReadFull(conn, make([]byte, int(length[0]))); err != nil {
			return
		}
	default:
		return
	}
	if _, err := io.ReadFull(conn, make([]byte, 2)); err != nil {
		return
	}

	reply := []byte{0x05, 0x00, 0x00, atyp}
	if atyp == 0x01 {
		reply = append(reply, 127, 0, 0, 1)
	} else {
		// 域名地址：长度 + 名字
		reply = append(reply, 0x01, 'h')
	}
	reply = append(reply, 0x00, 0x00)
	if _, err := conn.Write(reply); err != nil {
		return
	}
	_, _ = conn.Write([]byte{'A'})
}

func dialSocksHarness(t *testing.T) (net.Conn, chan byte, func()) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		listener.Close()
		t.Fatalf("dial: %v", err)
	}
	serverConn, err := listener.Accept()
	if err != nil {
		clientConn.Close()
		listener.Close()
		t.Fatalf("accept: %v", err)
	}

	observed := make(chan byte, 1)
	go serveSocks(serverConn, observed)

	// 实现如果读错字节数，这条测试要快速失败，不能挂住整个测试进程
	if err := clientConn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		clientConn.Close()
		serverConn.Close()
		listener.Close()
		t.Fatalf("read deadline: %v", err)
	}

	return clientConn, observed, func() {
		clientConn.Close()
		serverConn.Close()
		listener.Close()
	}
}

func TestSocksConnectSendsIPv4AsFourBytes(t *testing.T) {
	conn, observed, cleanup := dialSocksHarness(t)
	defer cleanup()

	if err := socksConnect(conn, "127.0.0.1:80"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if atyp := <-observed; atyp != 0x01 {
		t.Fatalf("IPv4 target must use address type 0x01, got 0x%02x", atyp)
	}

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	first := make([]byte, 1)
	if _, err := io.ReadFull(conn, first); err != nil {
		t.Fatalf("the CONNECT reply was read past its end and swallowed the first data byte: %v", err)
	}
	if first[0] != 'A' {
		t.Fatalf("first data byte corrupted: %q", first[0])
	}
}

func TestSocksConnectUsesHostnameForNonIPTargets(t *testing.T) {
	conn, observed, cleanup := dialSocksHarness(t)
	defer cleanup()

	if err := socksConnect(conn, "iptv.example:443"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if atyp := <-observed; atyp != 0x03 {
		t.Fatalf("hostname target must use address type 0x03, got 0x%02x", atyp)
	}

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	first := make([]byte, 1)
	if _, err := io.ReadFull(conn, first); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("connection closed: %v", err)
		}
		t.Fatalf("the CONNECT reply was read past its end and swallowed the first data byte: %v", err)
	}
	if first[0] != 'A' {
		t.Fatalf("first data byte corrupted: %q", first[0])
	}
}
