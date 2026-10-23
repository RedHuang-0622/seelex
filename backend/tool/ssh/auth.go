package ssh

// auth.go —— 认证材料的组装与**实际生效方式**的回报。
//
// 认证策略只有一条优先级（docs/arch/ssh-tool.md §7.3）：agent → key(+口令) → password。
// 优先级写在这里，不在调用方：两处各写一份必然漂移，而漂移的表现是"有时用 key、
// 有时回落密码"，最难归因。

import (
	"fmt"
	"io"
	"os"
	"sync"

	xssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// 协议层方法名（RFC 4252），用于与 AuthContext 的 AllowedMethods/TriedMethods 对齐。
const (
	protoPublicKey = "publickey"
	protoPassword  = "password"
)

// 报给调用方的认证方式名（比协议名更细：口令私钥单列一格）。
const (
	AuthNameAgent     = "agent"
	AuthNamePublicKey = "publickey"
	AuthNameKeyPass   = "publickey+passphrase"
	AuthNamePassword  = "password"
)

// authCandidate 是一个候选认证方式。
type authCandidate struct {
	// name 是回报给调用方的名字（AuthName*）。
	name string
	// proto 是协议层方法名，用于与服务端允许列表/已试列表比对。
	proto  string
	method xssh.AuthMethod
}

// authPlan 是本次连接的认证计划，并承载握手期观测到的服务端信息。
//
// 观测为什么要留：区分"公钥不被接受"与"公钥被接受但认证被拒"靠的不是比对错误
// 字符串，而是**服务端允许的方法列表**（ClientAuthContext.AllowedMethods）——
// 前者说明我们的 key 根本没进认证流程，后者说明 key 进去了但被否（典型是有口令
// 但无人输入、或 agent 没解锁）。修法完全不同，混为一谈会把人引到错方向。
type authPlan struct {
	mu         sync.Mutex
	candidates []authCandidate
	allowed    []string
	tried      []string
	started    bool

	authOnce sync.Once
	authCh   chan struct{}
}

// newAuthPlan 构造计划并初始化认证段信号通道。
func newAuthPlan() *authPlan {
	return &authPlan{authCh: make(chan struct{})}
}

// authStarted 返回"已进入认证段"的信号通道（关闭即代表握手完成）。
func (p *authPlan) authStarted() <-chan struct{} { return p.authCh }

// markAuthStart 只关一次通道：认证段可能被多次调用（每种方法一次）。
func (p *authPlan) markAuthStart() { p.authOnce.Do(func() { close(p.authCh) }) }

// buildAuthPlan 按 spec 组装候选认证方式。
func buildAuthPlan(spec AuthSpec) (*authPlan, error) {
	mode := spec.Mode
	if mode == "" {
		mode = AuthAuto
	}
	plan := newAuthPlan()

	addAgent := func() error {
		rwc, err := openAgent(spec.AgentSocket)
		if err != nil {
			return err
		}
		ag := agent.NewClient(rwc)
		plan.candidates = append(plan.candidates, authCandidate{
			name:  AuthNameAgent,
			proto: protoPublicKey,
			// Signers 是 agent 的签名面：公钥列表与签名都由 agent 代持，
			// 私钥永远不进本进程内存。
			method: xssh.PublicKeysCallback(ag.Signers),
		})
		return nil
	}
	addKey := func() error {
		if spec.KeyPath == "" {
			return codeErr(CodeNoCredential, "auth mode %q requires a key path", mode)
		}
		pem, err := os.ReadFile(spec.KeyPath)
		if err != nil {
			return fmt.Errorf("ssh: read private key %q: %w", spec.KeyPath, err)
		}
		name := AuthNamePublicKey
		signer, parseErr := xssh.ParsePrivateKey(pem)
		if parseErr != nil {
			// 有口令的私钥：ParsePrivateKey 会返回 PassphraseMissingError。
			if _, ok := parseErr.(*xssh.PassphraseMissingError); ok || spec.Passphrase != "" {
				if spec.Passphrase == "" {
					return codeErr(CodeNoCredential, "private key %q is passphrase-protected but no passphrase was supplied",
						spec.KeyPath)
				}
				signer, parseErr = xssh.ParsePrivateKeyWithPassphrase(pem, []byte(spec.Passphrase))
				name = AuthNameKeyPass
			}
			if parseErr != nil {
				return fmt.Errorf("ssh: parse private key %q: %w", spec.KeyPath, parseErr)
			}
		}
		plan.candidates = append(plan.candidates, authCandidate{
			name: name, proto: protoPublicKey, method: xssh.PublicKeys(signer),
		})
		return nil
	}
	addPassword := func() error {
		if spec.Password == "" {
			return codeErr(CodeNoCredential, "auth mode %q requires a password", mode)
		}
		plan.candidates = append(plan.candidates, authCandidate{
			name: AuthNamePassword, proto: protoPassword, method: xssh.Password(spec.Password),
		})
		return nil
	}

	switch mode {
	case AuthAuto:
		// auto 的每一步都是"尽力而为"：agent 没跑、key 不存在都不该阻断后面的候选。
		// 只有一条都不剩才算没有凭据（否则会报"没有凭据"，而实际是 agent 没启动）。
		_ = addAgent()
		if spec.KeyPath != "" {
			_ = addKey()
		}
		if spec.Password != "" {
			_ = addPassword()
		}
	case AuthAgent:
		if err := addAgent(); err != nil {
			return nil, err
		}
	case AuthKey:
		if err := addKey(); err != nil {
			return nil, err
		}
	case AuthPassword:
		if err := addPassword(); err != nil {
			return nil, err
		}
	default:
		return nil, codeErr(CodeNoCredential, "unknown auth mode %q", mode)
	}

	if len(plan.candidates) == 0 {
		return nil, codeErr(CodeNoCredential,
			"no usable credential: agent not reachable, no key path, no password")
	}
	return plan, nil
}

// methods 返回交给 ClientConfig.Auth 的方法列表（顺序即优先级）。
func (p *authPlan) methods() []xssh.AuthMethod {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]xssh.AuthMethod, 0, len(p.candidates))
	for _, c := range p.candidates {
		out = append(out, c.method)
	}
	return out
}

// observe 是 ClientConfig.AuthCallback 的实现：只观测，不改变选路。
//
// 刻意不在这里"挑"方法：方法选择交给库按 Auth 顺序走（库保证同一 RFC 方法只用
// 第一次出现的那一个），我们只把它观测到的事实记下来（允许哪些、试过哪些）。
func (p *authPlan) observe(ctx *xssh.ClientAuthContext) (xssh.AuthMethod, error) {
	p.mu.Lock()
	p.started = true
	p.allowed = append(p.allowed[:0], ctx.AllowedMethods...)
	p.tried = append(p.tried[:0], ctx.TriedMethods...)
	p.mu.Unlock()
	// banner 段到此结束（认证段开始）：dial.go 用这个信号切换计时器。
	p.markAuthStart()
	return nil, nil
}

// startedAuth 报告握手是否已经进入认证段（banner 段计时据此停止）。
func (p *authPlan) startedAuth() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.started
}

// usedName 推断**实际生效**的认证方式。
//
// 判据：库按 Auth 顺序尝试、成功即停止，且失败的尝试会出现在 TriedMethods 里，
// 因此"第一个没出现在 TriedMethods 的候选"就是成功的那一个。
// 局限（已知不处理）：多步认证（partial success）下 TriedMethods 会含中间步骤，
// 那时返回值只是一个近似；单步认证（几乎全部现实场景）准确。
func (p *authPlan) usedName() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.candidates {
		if !containsString(p.tried, c.proto) {
			return c.name
		}
	}
	return ""
}

// offeredProtos 返回我们提交过的协议层方法名（分类用）。
func (p *authPlan) offeredProtos() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := make(map[string]struct{}, len(p.candidates))
	out := make([]string, 0, len(p.candidates))
	for _, c := range p.candidates {
		if _, ok := seen[c.proto]; ok {
			continue
		}
		seen[c.proto] = struct{}{}
		out = append(out, c.proto)
	}
	return out
}

// serverAllowed 返回服务端允许的方法列表快照（空 = 没走到认证段或观测不到）。
func (p *authPlan) serverAllowed() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.allowed...)
}

// triedMethods 返回已尝试且失败的方法名快照。
func (p *authPlan) triedMethods() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.tried...)
}

// serverAllows 报告服务端是否接受某协议层方法。
func (p *authPlan) serverAllows(proto string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return containsString(p.allowed, proto)
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// openAgent 打开 agent 通道：优先显式套接字，其次平台默认位置。
func openAgent(explicit string) (io.ReadWriteCloser, error) {
	if explicit != "" {
		return dialAgent(explicit)
	}
	return dialDefaultAgent()
}
