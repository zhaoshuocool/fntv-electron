package utils

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"time"
)

// socksDialer 通过 SOCKS5（无认证）建立连接，避免为代理支持引入新依赖。
func socksDialer(proxy *url.URL) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: 15 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", proxy.Host)
		if err != nil {
			return nil, err
		}
		if err := socksConnect(conn, addr); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return conn, nil
	}
}

// socksConnect 完成 SOCKS5 握手并 CONNECT 到目标地址。
func socksConnect(conn net.Conn, addr string) error {
	host, portString, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		return err
	}

	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}

	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != 0x05 || header[1] != 0x00 {
		return errors.New("socks5 方法协商失败")
	}

	message := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil && len(ip) == 16 {
		message = append(message, 0x04)
		message = append(message, ip...)
	} else {
		message = append(message, 0x01, byte(len(host)))
		message = append(message, []byte(host)...)
	}
	message = append(message, byte(port>>8), byte(port&0xff))

	if _, err := conn.Write(message); err != nil {
		return err
	}

	response := make([]byte, 5)
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	if response[0] != 0x05 || response[1] != 0x00 {
		return errors.New("socks5 CONNECT 失败")
	}

	switch response[3] {
	case 0x01:
		_, err = io.ReadFull(conn, make([]byte, 6))
	case 0x04:
		_, err = io.ReadFull(conn, make([]byte, 18))
	case 0x03:
		_, err = io.ReadFull(conn, make([]byte, int(response[4])+2))
	default:
		return errors.New("socks5 返回了无法解析的地址")
	}
	return err
}
