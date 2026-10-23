package ssh

// dial.go —— 四段独立超时的一次连接。
//
// 为什么不用 ssh.Dial：库的 ClientConfig.Timeout 只覆盖 TCP 建连（见
// vendor/golang.org/x/crypto/ssh/client.go 的字段注释），握手与认证没有独立上限，
// 而 ssh.Session 的方法也收不到 ctx。把四段压成一段的后果是"主机端口不通"与
// "口令错"与"命令慢"表现完全一样——排障时无从下手（docs/arch/ssh-tool.md §5）。
//
// 缝就在 NewClientConn(c net.Conn, addr, cfg)：我们自己拨号、自己拿取消权，
// 库只负责握手与认证。

import (
	"context"
	"fmt"
	"net"
	"time"

	xssh "golang.org/x/crypto/ssh"
)

// DialError 是连接期失败，带**分段归因**。上层据此说"哪一段坏了"，而不是把裸
// 错误串丢给模型。
type DialError struct {
	Segment Segment
	// TimedOut 表示该段是被本端计时器掐断的（而非对端返回错误）。
	TimedOut bool
	Err      error
}

func (e *DialError) Error() string {
	return fmt.Sprintf("ssh %s: %v", e.Segment, e.Err)
}

func (e *DialError) Unwrap() error { return e.Err }

// dialTrace 是连接期的全部观测（失败时也保留：探针要靠它分类）。
type dialTrace struct {
	Segment  Segment
	Plan     *authPlan
	Verifier *hostKeyVerifier
}

// Dial 建立连接。四段超时各自独立生效。
func Dial(ctx context.Context, spec Spec) (*Client, error) {
	client, _, err := dial(ctx, spec)
	return client, err
}

// dial 是 Dial 的实现，额外返回观测轨迹供 Probe 分类。
func dial(ctx context.Context, spec Spec) (*Client, *dialTrace, error) {
	if spec.Hostname == "" {
		return nil, &dialTrace{Segment: SegConnect},
			codeErr("SSH_NO_HOST", "hostname is required")
	}
	if spec.User == "" {
		return nil, &dialTrace{Segment: SegAuth},
			codeErr("SSH_NO_USER", "user is required")
	}

	timeouts := spec.Timeouts.withDefaults()
	trace := &dialTrace{}

	// ── 第 1 段：connect（只覆盖 TCP 建连）──────────────────────────────
	//
	// 建连放在认证材料组装**之前**：否则"没有凭据"会先于"主机连不上"报出来，
	// 探针就会把一次网络不通误报成认证失败（分段归因失效）。
	dialer := net.Dialer{Timeout: timeouts.Connect, KeepAlive: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", spec.Addr())
	if err != nil {
		trace.Segment = SegConnect
		return nil, trace, &DialError{Segment: SegConnect, Err: err}
	}
	closeConn := func() { _ = conn.Close() }

	plan, err := buildAuthPlan(spec.Auth)
	if err != nil {
		closeConn()
		trace.Segment = SegAuth
		return nil, trace, err
	}
	trace.Plan = plan

	verifier, err := newHostKeyVerifier(spec)
	if err != nil {
		closeConn()
		trace.Segment = SegBanner
		return nil, trace, err
	}
	trace.Verifier = verifier

	cfg := &xssh.ClientConfig{
		User: spec.User,
		Auth: plan.methods(),
		// 指纹判定一律经由 knownhosts；这里没有 InsecureIgnoreHostKey 的位置。
		HostKeyCallback: verifier.callback,
		// 只观测（允许哪些方法 / 试过哪些），不改变库的选路。
		AuthCallback: plan.observe,
		// 保留库语义上的 TCP 超时；本连接已建好，实际以 dialer 为准。
		Timeout:       timeouts.Connect,
		ClientVersion: "SSH-2.0-SeelexSSH_0.1",
	}

	// ── 第 2/3 段：banner 与 auth ───────────────────────────────────────
	// NewClientConn 一次调用里同时做握手与认证，两段的分界是"第一次认证回调"。
	type handshake struct {
		conn  xssh.Conn
		chans <-chan xssh.NewChannel
		reqs  <-chan *xssh.Request
		err   error
	}
	done := make(chan handshake, 1)
	go func() {
		c, chans, reqs, hsErr := xssh.NewClientConn(conn, spec.Addr(), cfg)
		done <- handshake{conn: c, chans: chans, reqs: reqs, err: hsErr}
	}()

	finish := func(h handshake) (*Client, *dialTrace, error) {
		if h.err != nil {
			// 进了认证段之后的失败归 auth，之前的归 banner：这是"端口上不是 SSH"
			// 与"key/口令不对"的分水岭。
			seg := SegBanner
			if plan.startedAuth() {
				seg = SegAuth
			}
			trace.Segment = seg
			return nil, trace, &DialError{Segment: seg, Err: h.err}
		}
		return &Client{
			spec:     spec,
			conn:     conn,
			client:   xssh.NewClient(h.conn, h.chans, h.reqs),
			authUsed: plan.usedName(),
			hostKey:  verifier.observed(),
		}, trace, nil
	}

	bannerTimer := time.NewTimer(timeouts.Banner)
	defer bannerTimer.Stop()

	var authTimer *time.Timer
	var authC <-chan time.Time
	defer func() {
		if authTimer != nil {
			authTimer.Stop()
		}
	}()

	select {
	case h := <-done:
		return finish(h)
	case <-plan.authStarted():
		// banner 段结束：停掉 banner 计时，换成 auth 计时。两段各记一笔账，
		// 这样"握手卡住"与"认证卡住"能分开归因。
		if !bannerTimer.Stop() {
			select {
			case <-bannerTimer.C:
			default:
			}
		}
		authTimer = time.NewTimer(timeouts.Auth)
		authC = authTimer.C
	case <-bannerTimer.C:
		closeConn()
		trace.Segment = SegBanner
		return nil, trace, &DialError{
			Segment:  SegBanner,
			TimedOut: true,
			Err:      fmt.Errorf("handshake did not complete within %s", timeouts.Banner),
		}
	case <-ctx.Done():
		closeConn()
		trace.Segment = SegBanner
		return nil, trace, ctx.Err()
	}

	select {
	case h := <-done:
		return finish(h)
	case <-authC:
		closeConn()
		trace.Segment = SegAuth
		return nil, trace, &DialError{
			Segment:  SegAuth,
			TimedOut: true,
			Err:      fmt.Errorf("authentication did not complete within %s", timeouts.Auth),
		}
	case <-ctx.Done():
		closeConn()
		trace.Segment = SegAuth
		return nil, trace, ctx.Err()
	}
}
