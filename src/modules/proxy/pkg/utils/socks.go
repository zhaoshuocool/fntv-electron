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
		// 握手要有超时，否则一个不回包的代理会把这条连接挂死
		if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if err := socksConnect(conn, addr); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if err := conn.SetReadDeadline(time.Time{}); err != nil {
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
	// 地址类型按 RFC 1928：0x01 IPv4（4 字节）、0x03 域名、0x04 IPv6。
	// net.ParseIP 对 IPv4 也返回 16 字节的 v4-mapped，必须用 To4() 判断，否则严格的服务端会拒绝。
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			message = append(message, 0x01)
			message = append(message, v4...)
		} else {
			message = append(message, 0x04)
			message = append(message, ip...)
		}
	} else {
		message = append(message, 0x03, byte(len(host)))
		message = append(message, []byte(host)...)
	}
	message = append(message, byte(port>>8), byte(port&0xff))

	if _, err := conn.Write(message); err != nil {
		return err
	}

	reply := make([]byte, 4)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		return errors.New("socks5 CONNECT 失败")
	}

	var addressLength int
	switch reply[3] {
	case 0x01:
		addressLength = 4
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return err
		}
		addressLength = int(length[0])
	case 0x04:
		addressLength = 16
	default:
		return errors.New("socks5 返回了无法解析的地址")
	}
	if _, err := io.ReadFull(conn, make([]byte, addressLength)); err != nil {
		return err
	}
	if _, err := io.ReadFull(conn, make([]byte, 2)); err != nil {
		return err
	}
	return nil
}
