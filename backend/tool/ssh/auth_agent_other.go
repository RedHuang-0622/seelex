//go:build !windows

package ssh

// auth_agent_other.go —— unix 上的 ssh-agent 接入（unix domain socket）。

import (
	"fmt"
	"io"
	"net"
	"os"
)

// defaultAgentSocket 返回 unix 上 agent 的默认套接字路径（来自 $SSH_AUTH_SOCK）。
func defaultAgentSocket() string { return os.Getenv("SSH_AUTH_SOCK") }

// dialDefaultAgent 打开平台默认的 agent 通道。
func dialDefaultAgent() (io.ReadWriteCloser, error) {
	sock := defaultAgentSocket()
	if sock == "" {
		return nil, fmt.Errorf("ssh: SSH_AUTH_SOCK is not set (is ssh-agent running?)")
	}
	return dialAgent(sock)
}

// dialAgent 打开指定路径的 agent 套接字。
func dialAgent(name string) (io.ReadWriteCloser, error) {
	conn, err := net.Dial("unix", name)
	if err != nil {
		return nil, fmt.Errorf("ssh: dial agent socket %q: %w", name, err)
	}
	return conn, nil
}
