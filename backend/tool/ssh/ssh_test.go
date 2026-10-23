package ssh

// ssh_test.go —— 全部判据跑在**进程内 sshd** 上（vendor 里 x/crypto/ssh/server.go
// 的 NewServerConn 已可用），不依赖真机、不进外网。
//
// 这组用例同时是"文档里的纪律是否真的落地"的机械证据：非 ASCII 拒收、channel
// 超时不是失败、退出码非零不是失败、输出有界、字节透明、哈希 known_hosts、
// 信任需指纹相符。

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// ── 进程内 sshd ─────────────────────────────────────────────────────────

// testPassword 是假 sshd 的默认口令：没有显式配置认证方式的服务器用它，
// 这样客户端必须真的走一遍 password 认证（"无认证连接"不是本包的场景）。
const testPassword = "test-pw"

// serverConfig 控制假 sshd 的行为；零值 = 默认口令认证 + 回显命令。
type serverConfig struct {
	// password 非空则启用密码认证（口令必须相等）。
	password string
	// pubKey 非 nil 则启用公钥认证（必须等于该公钥）。
	pubKey xssh.PublicKey
	// delayBanner 在握手前故意延迟（触发 banner 段超时）。
	delayBanner time.Duration
	// exec 处理一次 exec 请求并返回退出码；nil = 回显 "ok" 并退出 0。
	exec func(cmd string, ch xssh.Channel) uint32
}

// testServer 是一个监听本机随机端口的假 sshd。
type testServer struct {
	addr     string
	hostPub  xssh.PublicKey
	listener net.Listener
}

// startServer 起一个进程内 sshd，t.Cleanup 自动关停。
func startServer(t *testing.T, cfg serverConfig) *testServer {
	t.Helper()

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	hostSigner, err := xssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}

	srvCfg := &xssh.ServerConfig{}
	// 没指定认证方式 ⇒ 用默认口令；"无认证连接"不是本包的场景，测试必须真的
	// 走一遍认证，否则测不到 auth 段的分类逻辑。
	if cfg.password == "" && cfg.pubKey == nil {
		cfg.password = testPassword
	}
	if cfg.password != "" {
		want := cfg.password
		srvCfg.PasswordCallback = func(_ xssh.ConnMetadata, got []byte) (*xssh.Permissions, error) {
			if string(got) != want {
				return nil, errors.New("wrong password")
			}
			return &xssh.Permissions{}, nil
		}
	}
	if cfg.pubKey != nil {
		want := cfg.pubKey.Marshal()
		srvCfg.PublicKeyCallback = func(_ xssh.ConnMetadata, got xssh.PublicKey) (*xssh.Permissions, error) {
			if !strings.EqualFold(base64.StdEncoding.EncodeToString(got.Marshal()),
				base64.StdEncoding.EncodeToString(want)) {
				return nil, errors.New("unknown public key")
			}
			return &xssh.Permissions{}, nil
		}
	}
	// 到这里 password 或 pubKey 必有一个已配置，NoClientAuth 分支不会生效；
	// 保留它是为了支持显式 `serverConfig{noClientAuth:true}` 之类的扩展。
	srvCfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &testServer{addr: ln.Addr().String(), hostPub: hostSigner.PublicKey(), listener: ln}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go serveConn(conn, srvCfg, cfg)
		}
	}()
	return srv
}

// serveConn 跑一条连接的握手与会话循环。
func serveConn(conn net.Conn, srvCfg *xssh.ServerConfig, cfg serverConfig) {
	defer conn.Close()
	if cfg.delayBanner > 0 {
		time.Sleep(cfg.delayBanner)
	}
	_, chans, reqs, err := xssh.NewServerConn(conn, srvCfg)
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
		go serveSession(ch, chReqs, cfg)
	}
}

// serveSession 处理一次 exec：调用测试处理器，然后回 exit-status。
func serveSession(ch xssh.Channel, chReqs <-chan *xssh.Request, cfg serverConfig) {
	defer ch.Close()
	for req := range chReqs {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		_ = xssh.Unmarshal(req.Payload, &payload)
		_ = req.Reply(true, nil)

		status := uint32(0)
		if cfg.exec != nil {
			status = cfg.exec(payload.Command, ch)
		} else {
			_, _ = ch.Write([]byte("ok"))
		}
		_, _ = ch.SendRequest("exit-status", false, xssh.Marshal(struct{ Status uint32 }{status}))
		return
	}
}

// sendExitStatus 供测试处理器主动给出非零退出码。
func sendExitStatus(ch xssh.Channel, code uint32) {
	_, _ = ch.SendRequest("exit-status", false, xssh.Marshal(struct{ Status uint32 }{code}))
}

// ── 夹具 ────────────────────────────────────────────────────────────────

// knownHostsLine 造一条 known_hosts 条目；hash=true 时用哈希主机名形态。
//
// 注意哈希的是**规范化后**的地址：knownhosts 校验时会先 Normalize(address)，
// 所以 `hash(Normalize(addr))` 才是那个能命中的值。
func knownHostsLine(addr string, key xssh.PublicKey, hash bool) string {
	host := addr
	if hash {
		host = knownhosts.HashHostname(knownhosts.Normalize(addr))
	}
	return host + " " + key.Type() + " " + base64.StdEncoding.EncodeToString(key.Marshal())
}

// writeKnownHosts 写一个临时 known_hosts，返回路径。
func writeKnownHosts(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return path
}

// specFor 是连接假 sshd 的 Spec 工厂。
func specFor(t *testing.T, srv *testServer, khPath string, auth AuthSpec) Spec {
	t.Helper()
	host, portStr, err := net.SplitHostPort(srv.addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return Spec{
		ID:       "test-host",
		Hostname: host,
		Port:     port,
		User:     "tester",
		Auth:     auth,
		KnownHosts: KnownHostsSpec{
			Files: []string{khPath},
			Mode:  KnownHostsStrict,
		},
		Timeouts: Timeouts{
			Connect: 2 * time.Second,
			Banner:  2 * time.Second,
			Auth:    2 * time.Second,
			Channel: 300 * time.Millisecond,
		},
	}
}

// writeKeyFile 生成一把 ed25519 私钥并落盘（可带口令），返回路径与公钥。
func writeKeyFile(t *testing.T, passphrase string) (string, xssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	var block *pem.Block
	if passphrase != "" {
		b, mErr := xssh.MarshalPrivateKeyWithPassphrase(priv, "seelex-test", []byte(passphrase))
		if mErr != nil {
			t.Fatalf("marshal key with passphrase: %v", mErr)
		}
		block = b
	} else {
		b, mErr := xssh.MarshalPrivateKey(priv, "seelex-test")
		if mErr != nil {
			t.Fatalf("marshal key: %v", mErr)
		}
		block = b
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	sshPub, err := xssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("public key: %v", err)
	}
	return path, sshPub
}

// ── 1) 入参纪律：一次性参数只收单行 ASCII ───────────────────────────────

func TestValidateInline(t *testing.T) {
	cases := []struct {
		name    string
		cmd     string
		wantErr bool
		code    string
	}{
		{name: "简单命令", cmd: "git rev-parse HEAD"},
		{name: "含换行", cmd: "echo a\necho b", wantErr: true, code: CodeInlineMultiline},
		{name: "含中文", cmd: "echo 你好", wantErr: true, code: CodeNonASCIIInline},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateInline(tc.cmd)
			if tc.wantErr {
				var codeErr *CodeError
				if !errors.As(err, &codeErr) {
					t.Fatalf("want CodeError, got %v", err)
				}
				if codeErr.Code != tc.code {
					t.Fatalf("code = %q, want %q", codeErr.Code, tc.code)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestResolveRejectsNonASCIIInline 证明拒收发生在**拨号之前**（Client 零值即可判定）。
func TestResolveRejectsNonASCIIInline(t *testing.T) {
	client := &Client{}
	_, err := client.resolve(Request{Inline: "echo 未在后台注册"})
	var codeErr *CodeError
	if !errors.As(err, &codeErr) || codeErr.Code != CodeNonASCIIInline {
		t.Fatalf("want %s, got %v", CodeNonASCIIInline, err)
	}
}

// TestResolveCommandSourcesMutuallyExclusive 证明三种来源互斥且必填其一。
func TestResolveCommandSourcesMutuallyExclusive(t *testing.T) {
	client := &Client{}
	if _, err := client.resolve(Request{}); err == nil {
		t.Fatal("expected error for empty request")
	}
	if _, err := client.resolve(Request{Inline: "ls", Stdin: "ls"}); err == nil {
		t.Fatal("expected error for conflicting sources")
	}
}

// ── 2) 三态语义：退出码非零不是失败，channel 超时不是失败 ────────────────

func TestRunCompletedAndExitCode(t *testing.T) {
	srv := startServer(t, serverConfig{
		exec: func(_ string, ch xssh.Channel) uint32 {
			_, _ = ch.Write([]byte("hello\r\n"))
			return 3
		},
	})
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, false))
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthPassword, Password: testPassword})

	client, err := Dial(context.Background(), spec)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	res, err := client.Run(context.Background(), Request{Inline: "echo hello"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// 关键断言：远端跑完但退出码非零 ⇒ completed，而**不是** failed。
	if res.Status != StatusCompleted {
		t.Fatalf("status = %q, want %q", res.Status, StatusCompleted)
	}
	if res.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3", res.ExitCode)
	}
	// 行尾不做归一：远端给的 CRLF 必须原样回来。
	if res.Stdout != "hello\r\n" {
		t.Fatalf("stdout = %q, want CRLF preserved", res.Stdout)
	}
}

func TestRunChannelTimeoutStillRunning(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	srv := startServer(t, serverConfig{
		exec: func(_ string, ch xssh.Channel) uint32 {
			<-release // 一直不结束：模拟"远端仍在跑"
			return 0
		},
	})
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, false))
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthPassword, Password: testPassword})
	// 通道超时压到很短，确保必然触发
	spec.Timeouts.Channel = 120 * time.Millisecond

	client, err := Dial(context.Background(), spec)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	res, err := client.Run(context.Background(), Request{Inline: "sleep 600"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusChannelTimeout {
		t.Fatalf("status = %q, want %q", res.Status, StatusChannelTimeout)
	}
	if !res.StillRunning {
		t.Fatal("still_running must be true on channel timeout")
	}
	if res.Segment != SegChannel {
		t.Fatalf("segment = %q, want %q", res.Segment, SegChannel)
	}
	if res.Failed() {
		t.Fatal("channel timeout must NOT be reported as failure")
	}
}

// ── 3) 保真：脚本经 stdin 字节透明（含 CRLF 与中文）─────────────────────

func TestScriptChannelIsByteTransparent(t *testing.T) {
	const script = "#!/bin/bash\r\n# 未在后台注册\r\necho '中文断言'\r\n"
	srv := startServer(t, serverConfig{
		exec: func(_ string, ch xssh.Channel) uint32 {
			// 把 stdin 原样回显，由客户端比对字节。
			_, _ = io.Copy(ch, ch)
			return 0
		},
	})
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, false))
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthPassword, Password: testPassword})

	client, err := Dial(context.Background(), spec)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	res, err := client.Run(context.Background(), Request{Stdin: script})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Stdout != script {
		t.Fatalf("script bytes changed in transit:\n got %q\nwant %q", res.Stdout, script)
	}
	if res.LossyUTF8 {
		t.Fatal("utf-8 must be clean")
	}
}

// ── 4) 输出有界 ─────────────────────────────────────────────────────────

func TestRunOutputBounded(t *testing.T) {
	srv := startServer(t, serverConfig{
		exec: func(_ string, ch xssh.Channel) uint32 {
			blob := strings.Repeat("x", 8192)
			for i := 0; i < 32; i++ { // 256 KiB
				if _, err := ch.Write([]byte(blob)); err != nil {
					return 0
				}
			}
			return 0
		},
	})
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, false))
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthPassword, Password: testPassword})

	client, err := Dial(context.Background(), spec)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	res, err := client.Run(context.Background(), Request{Inline: "flood", MaxOutputBytes: 1000})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Stdout) > 1000 {
		t.Fatalf("stdout len = %d, want <= 1000", len(res.Stdout))
	}
	if !res.Truncated {
		t.Fatal("truncated must be true when output exceeds the cap")
	}
}

// ── 5) 指纹：未知 → 显式信任 → 哈希条目可识别 ───────────────────────────

func TestProbeHostUnknownThenHashedTrust(t *testing.T) {
	srv := startServer(t, serverConfig{})
	kh := writeKnownHosts(t) // 空指纹库
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthPassword, Password: testPassword})

	first := Probe(context.Background(), spec)
	if first.OK {
		t.Fatal("probe against empty known_hosts must fail")
	}
	if first.Class != ClassHostUnknown {
		t.Fatalf("class = %q, want %q (hint: %s)", first.Class, ClassHostUnknown, first.Hint)
	}
	if first.Fingerprint == "" {
		t.Fatal("probe must report the observed fingerprint for human confirmation")
	}

	// 指纹不符时不得写盘（A8）
	before, err := os.ReadFile(kh)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if _, _, _, trustErr := TrustFromProbe(context.Background(), spec, "SHA256:bogus"); trustErr == nil {
		t.Fatal("trust with mismatched fingerprint must fail")
	}
	after, err := os.ReadFile(kh)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("known_hosts must not change when the fingerprint does not match")
	}

	// 指纹相符才写盘
	if _, _, _, trustErr := TrustFromProbe(context.Background(), spec, first.Fingerprint); trustErr != nil {
		t.Fatalf("trust: %v", trustErr)
	}
	second := Probe(context.Background(), spec)
	if !second.OK {
		t.Fatalf("probe after trust must succeed, class=%q hint=%s", second.Class, second.Hint)
	}

	// 哈希形态的条目也必须被识别（A6：存在性靠解析，不靠文本匹配）
	hashedKH := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, true))
	hashedSpec := specFor(t, srv, hashedKH, AuthSpec{Mode: AuthPassword, Password: testPassword})
	if got := Probe(context.Background(), hashedSpec); !got.OK {
		t.Fatalf("hashed known_hosts entry must be recognised, class=%q", got.Class)
	}
}

func TestProbeKeyMismatch(t *testing.T) {
	srv := startServer(t, serverConfig{})
	// 用一把**别的**主机公钥写条目 ⇒ 指纹不符
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}
	otherSigner, err := xssh.NewSignerFromKey(otherPriv)
	if err != nil {
		t.Fatalf("other signer: %v", err)
	}
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, otherSigner.PublicKey(), false))
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthPassword, Password: testPassword})

	res := Probe(context.Background(), spec)
	if res.Class != ClassKeyMismatch {
		t.Fatalf("class = %q, want %q", res.Class, ClassKeyMismatch)
	}
}

// ── 6) 认证分类：两种病因必须分开 ───────────────────────────────────────

func TestProbeAuthRejectedPassword(t *testing.T) {
	srv := startServer(t, serverConfig{password: "right"})
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, false))
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthPassword, Password: "wrong"})

	res := Probe(context.Background(), spec)
	if res.Class != ClassAuthRejected {
		t.Fatalf("class = %q, want %q (hint: %s)", res.Class, ClassAuthRejected, res.Hint)
	}
	if res.Segment != SegAuth {
		t.Fatalf("segment = %q, want %q", res.Segment, SegAuth)
	}
}

func TestProbeNoAuthMethod(t *testing.T) {
	// 服务端只接受密码，客户端只提交公钥 ⇒ 公钥根本没被接受
	srv := startServer(t, serverConfig{password: "right"})
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, false))
	keyPath, _ := writeKeyFile(t, "")
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthKey, KeyPath: keyPath})

	res := Probe(context.Background(), spec)
	if res.Class != ClassNoAuthMethod {
		t.Fatalf("class = %q, want %q (allowed=%v offered=%v)",
			res.Class, ClassNoAuthMethod, res.AllowedMethods, res.OfferedMethods)
	}
}

func TestAuthUsedReportsKey(t *testing.T) {
	keyPath, pub := writeKeyFile(t, "")
	srv := startServer(t, serverConfig{pubKey: pub})
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, false))
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthKey, KeyPath: keyPath})

	client, err := Dial(context.Background(), spec)
	if err != nil {
		t.Fatalf("dial with key: %v", err)
	}
	defer func() { _ = client.Close() }()
	if got := client.AuthUsed(); got != AuthNamePublicKey {
		t.Fatalf("auth_used = %q, want %q", got, AuthNamePublicKey)
	}
}

func TestAuthWithPassphrase(t *testing.T) {
	keyPath, pub := writeKeyFile(t, "s3cret")
	srv := startServer(t, serverConfig{pubKey: pub})
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, false))
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthKey, KeyPath: keyPath, Passphrase: "s3cret"})

	client, err := Dial(context.Background(), spec)
	if err != nil {
		t.Fatalf("dial with passphrase: %v", err)
	}
	defer func() { _ = client.Close() }()
	if got := client.AuthUsed(); got != AuthNameKeyPass {
		t.Fatalf("auth_used = %q, want %q", got, AuthNameKeyPass)
	}

	// 有口令但没给口令 ⇒ 明确的入参错误，而不是"认证失败"
	specNoPass := spec
	specNoPass.Auth.Passphrase = ""
	_, err = Dial(context.Background(), specNoPass)
	var codeErr *CodeError
	if !errors.As(err, &codeErr) || codeErr.Code != CodeNoCredential {
		t.Fatalf("want %s, got %v", CodeNoCredential, err)
	}
}

// TestProbeUnreachableWinsOverMissingCredential 是分段归因顺序的回归：
// 先建连、后组装认证材料。否则"没有凭据"会抢在"主机连不上"前面报出来，
// 一次网络不通会被误读成认证失败。
func TestProbeUnreachableWinsOverMissingCredential(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // 端口立刻变成"没人听"

	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	spec := Spec{
		Hostname:   host,
		Port:       port,
		User:       "tester",
		Auth:       AuthSpec{Mode: AuthAuto}, // 没有 agent/key/password
		KnownHosts: KnownHostsSpec{Mode: KnownHostsStrict, Files: []string{writeKnownHosts(t)}},
		Timeouts:   Timeouts{Connect: 2 * time.Second, Banner: 2 * time.Second, Auth: 2 * time.Second, Channel: time.Second},
	}
	res := Probe(context.Background(), spec)
	if res.Class != ClassUnreachable {
		t.Fatalf("class = %q, want %q (segment=%q hint=%s)", res.Class, ClassUnreachable, res.Segment, res.Hint)
	}
	if res.Segment != SegConnect {
		t.Fatalf("segment = %q, want %q", res.Segment, SegConnect)
	}
}

// TestProbeNoCredential 证明"我们没提交凭据"与"凭据被拒"是两回事。
func TestProbeNoCredential(t *testing.T) {
	srv := startServer(t, serverConfig{})
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, false))
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthAuto})

	res := Probe(context.Background(), spec)
	if res.Class != ClassNoCredential {
		t.Fatalf("class = %q, want %q (hint: %s)", res.Class, ClassNoCredential, res.Hint)
	}
}

// ── 7) 分段归因：banner 段超时 ──────────────────────────────────────────

func TestProbeBannerTimeout(t *testing.T) {
	srv := startServer(t, serverConfig{delayBanner: 3 * time.Second})
	kh := writeKnownHosts(t, knownHostsLine(srv.addr, srv.hostPub, false))
	spec := specFor(t, srv, kh, AuthSpec{Mode: AuthPassword, Password: testPassword})
	spec.Timeouts.Banner = 150 * time.Millisecond

	res := Probe(context.Background(), spec)
	if res.Segment != SegBanner {
		t.Fatalf("segment = %q, want %q", res.Segment, SegBanner)
	}
	if res.Class != ClassTimeout {
		t.Fatalf("class = %q, want %q", res.Class, ClassTimeout)
	}
}

// ── 8) 规范对齐：Result 信封不带 success 字段 ───────────────────────────

func TestResultEnvelopeHasNoSuccessField(t *testing.T) {
	raw, err := json.Marshal(Result{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"success"`) {
		t.Fatal("Result must not carry a business-success judgement")
	}
	for _, want := range []string{`"status"`, `"exit_code"`, `"still_running"`, `"encoding_lossy"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing field %s in %s", want, raw)
		}
	}
}
