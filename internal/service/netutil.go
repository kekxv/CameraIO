package service

import (
	"fmt"
	"net"
	"time"
)

// probeTCPPort 检测 TCP 端口是否可达。
func probeTCPPort(ip string, port int, timeout time.Duration) error {
	addr := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}
