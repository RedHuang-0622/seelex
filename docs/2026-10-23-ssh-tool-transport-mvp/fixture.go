package main

// fixture.go —— 进程内 sshd 夹具 + 一个极小的 POSIX 命令解释器。
//
// 为什么要夹具：可行性验证不能依赖"某台真机"或外网。夹具承载 exec 面，
// 解释器只认本设计真正会发出的那几条命令形态（cat / printf|base64 -d / echo /
// exit / sleep），因此它顺带证明了**命令形态**是合理的。
//
// 明确边界：夹具不是 shell，遇到不认识的命令返回 exit 127 —— 这正是生产环境里
// `SSH_REMOTE_UNSUPPORTED` 该被触发的位置。

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// fixtureServer 是假 sshd：一个端口、一个口令、一套内存文件系统。
type fixtureServer struct {
	Addr    string
	Pass    string
	HostPub xssh.PublicKey

	mu sync.Mutex
	fs map[string]string

	listener net.Listener
}

// StartFixture 起一个进程内 sshd。files 是远端文件系统初值（相对路径 → 内容）。
func StartFixture(password string, files map[string]string) (*fixtureServer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	signer, err := xssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}

	cfg := &xssh.ServerConfig{
		PasswordCallback: func(_ xssh.ConnMetadata, got []byte) (*xssh.Permissions, error) {
			if string(got) != password {
				return nil, fmt.Errorf("wrong password")
			}
			return &xssh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	fs := make(map[string]string, len(files))
	for k, v := range files {
		fs[k] = v
	}
	srv := &fixtureServer{
		Addr: ln.Addr().String(), Pass: password, HostPub: signer.PublicKey(),
		fs: fs, listener: ln,
	}
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go srv.serve(conn, cfg)
		}
	}()
	return srv, nil
}

// KnownHostsLine 造一条 known_hosts 条目（首次信任的模拟）。
func (s *fixtureServer) KnownHostsLine() string {
	return knownhosts.Line([]string{s.Addr}, s.HostPub)
}

// Close 关停夹具。
func (s *fixtureServer) Close() {
	if s.listener != nil {
		_ = s.listener.Close()
	}
}

// File 读远端文件（断言用）。
func (s *fixtureServer) File(path string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.fs[path]
	return value, ok
}

func (s *fixtureServer) serve(conn net.Conn, cfg *xssh.ServerConfig) {
	defer conn.Close()
	_, chans, reqs, err := xssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go xssh.DiscardRequests(reqs)
	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			_ = newChan.Reject(xssh.UnknownChannelType, "only session")
			continue
		}
		ch, chReqs, acceptErr := newChan.Accept()
		if acceptErr != nil {
			continue
		}
		go s.serveSession(ch, chReqs)
	}
}

func (s *fixtureServer) serveSession(ch xssh.Channel, chReqs <-chan *xssh.Request) {
	defer ch.Close()
	for req := range chReqs {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		_ = xssh.Unmarshal(req.Payload, &payload)
		_ = req.Reply(true, nil)

		exitCode, stdout, stderr := s.runCommand(payload.Command)
		if stderr != "" {
			_, _ = ch.Stderr().Write([]byte(stderr))
		}
		if stdout != "" {
			_, _ = ch.Write([]byte(stdout))
		}
		_, _ = ch.SendRequest("exit-status", false, xssh.Marshal(struct{ Status uint32 }{uint32(exitCode)}))
		return
	}
}

// runCommand 是夹具的极小解释器：只认本设计真正会发出的命令形态。
func (s *fixtureServer) runCommand(cmd string) (exitCode int, stdout, stderr string) {
	cmd = strings.TrimSpace(cmd)

	// 写：printf %s '<base64>' | base64 -d > '<path>'
	if rest, ok := strings.CutPrefix(cmd, "printf %s '"); ok {
		const marker = "' | base64 -d > "
		idx := strings.Index(rest, marker)
		if idx < 0 {
			return 127, "", "fixture: unsupported write form"
		}
		payload := rest[:idx]
		path, pathErr := resolvePathExpr(rest[idx+len(marker):])
		if pathErr != nil {
			return 127, "", "fixture: " + pathErr.Error()
		}
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return 1, "", "base64: invalid input"
		}
		s.mu.Lock()
		s.fs[path] = string(data)
		s.mu.Unlock()
		return 0, "", ""
	}

	// 读：cat -- '<path>' 或 cat -- "$(printf %s '<b64>' | base64 -d)"
	if rest, ok := strings.CutPrefix(cmd, "cat -- "); ok {
		path, pathErr := resolvePathExpr(rest)
		if pathErr != nil {
			return 127, "", "fixture: " + pathErr.Error()
		}
		s.mu.Lock()
		value, found := s.fs[path]
		s.mu.Unlock()
		if !found {
			return 1, "", "cat: " + path + ": No such file or directory"
		}
		return 0, value, ""
	}

	// 普通输出：echo X
	if rest, ok := strings.CutPrefix(cmd, "echo "); ok {
		return 0, unquote(rest) + "\n", ""
	}

	// 退出码：exit N
	if rest, ok := strings.CutPrefix(cmd, "exit "); ok {
		code := atoiOr(rest, 1)
		return code, "", ""
	}

	// 长任务：sleep N（用于演示 channel 超时不被报成失败）
	if rest, ok := strings.CutPrefix(cmd, "sleep "); ok {
		seconds := atoiOr(rest, 0)
		time.Sleep(time.Duration(seconds) * time.Second)
		return 0, "", ""
	}

	return 127, "", "fixture: command not found: " + cmd
}

// unquote 去掉夹具里用到的单引号字面量。
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], `'\''`, `'`)
	}
	return s
}

// resolvePathExpr 解析传输侧发出的两种路径形态：
//
//	'<plain>'                              → 原样
//	"$(printf %s '<base64>' | base64 -d)"  → base64 解出（含非 ASCII 的路径走这里）
func resolvePathExpr(expr string) (string, error) {
	expr = strings.TrimSpace(expr)
	const open = `"$(printf %s '`
	if rest, ok := strings.CutPrefix(expr, open); ok {
		const closeMarker = `' | base64 -d)"`
		idx := strings.Index(rest, closeMarker)
		if idx < 0 {
			return "", fmt.Errorf("unsupported path substitution form")
		}
		data, err := base64.StdEncoding.DecodeString(rest[:idx])
		if err != nil {
			return "", fmt.Errorf("bad base64 path: %w", err)
		}
		return string(data), nil
	}
	return unquote(expr), nil
}
