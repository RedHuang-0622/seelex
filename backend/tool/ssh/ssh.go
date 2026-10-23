// Package ssh 是 Seelex 内置 SSH 工具的**连接基座**：库内直连（不 shell out 到
// ssh.exe）、四段独立超时、known_hosts 严格校验、可分类的失败探针、三种返回形态
// 的命令执行。
//
// 生态位：本包只负责"一次连接 + 一次命令"这件事，不做工具注册、不做权限判定、
// 不做 host 注册表持久化。上层（backend/tool/registry 的 ssh_* 工具族）把项目内
// host 注册表的一行填成 Spec 交进来，再决定怎么把 Result 交回模型。
// 设计依据见 docs/arch/ssh-tool.md（§5 四段超时、§6 指纹、§9 命令纪律）。
//
// 三条硬约束（违反即事故，动手前先读）：
//
//  1. 内容通道只认文件 / stdin。一次性命令串（Spec/Request 里的 Inline）只允许
//     单行 ASCII；含非 ASCII 直接拒（CodeNonASCIIInline）。理由：一次性参数经
//     多层代码页与引号转换会**静默**损坏，而损坏后的脚本仍能被解析。
//  2. 本端的等待超时不能翻译成对端的状态。channel 段超时返回 StatusChannelTimeout
//     且 StillRunning=true，绝不返回"失败"。
//  3. 存在性判断靠结构，不靠文本。known_hosts 条目可能是 |1|salt|hash 形态，
//     一切判定都经过 knownhosts 包，禁止 grep 文本。
package ssh

import (
	"errors"
	"fmt"
	"net"
	"time"

	xssh "golang.org/x/crypto/ssh"
)

// 会话级错误码。工具层据此给模型可操作的提示，而不是让它读裸错误串。
const (
	// CodeNonASCIIInline 表示一次性命令里出现了非 ASCII 字符，必须改走
	// CommandFile / Stdin。
	CodeNonASCIIInline = "SSH_NON_ASCII_INLINE_COMMAND"
	// CodeNoCommand 表示三种命令来源都没给。
	CodeNoCommand = "SSH_NO_COMMAND"
	// CodeCommandConflict 表示同时给了互斥的命令来源。
	CodeCommandConflict = "SSH_COMMAND_SOURCE_CONFLICT"
	// CodeInlineMultiline 表示一次性命令里出现了换行。
	CodeInlineMultiline = "SSH_INLINE_COMMAND_MULTILINE"
	// CodeInvalidScriptUTF8 表示脚本文件不是合法 UTF-8。
	CodeInvalidScriptUTF8 = "SSH_SCRIPT_NOT_UTF8"
	// CodeKnownHostsMissing 表示 known_hosts 文件不存在（首次连接前必须先信任）。
	CodeKnownHostsMissing = "SSH_KNOWN_HOSTS_MISSING"
	// CodeFingerprintMismatch 表示待信任的指纹与本次握手结果不一致。
	CodeFingerprintMismatch = "SSH_FINGERPRINT_MISMATCH"
	// CodeNoCredential 表示没有可用的认证材料。
	CodeNoCredential = "SSH_NO_CREDENTIAL"
)

// CodeError 是带错误码的失败：Code 给机器判定，Msg 给人读。
type CodeError struct {
	Code string
	Msg  string
	Err  error
}

func (e *CodeError) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Msg + ": " + e.Err.Error()
	}
	return e.Code + ": " + e.Msg
}

func (e *CodeError) Unwrap() error { return e.Err }

func codeErr(code, format string, args ...any) error {
	return &CodeError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// ErrKnownHostsMissing 是本包对"首个连接尚未信任"的稳定错误值（可用 errors.Is 判）。
var ErrKnownHostsMissing = errors.New("ssh: no known_hosts entry for host")

// AuthMode 选择认证材料的来源；Auto 按 agent → key → password 的优先级尝试。
type AuthMode string

const (
	AuthAuto     AuthMode = "auto"
	AuthAgent    AuthMode = "agent"
	AuthKey      AuthMode = "key"
	AuthPassword AuthMode = "password"
)

// AuthSpec 是一次认证所需的**引用**而非秘密本体：上层从 OS 凭据库读出后注入。
// 本结构永远不进日志、不进工具返回值（Result 只回 AuthUsed 的分类名）。
type AuthSpec struct {
	Mode AuthMode
	// KeyPath 是私钥文件路径（AuthKey 时必填）。
	KeyPath string
	// Passphrase 是有口令私钥的口令；空 = 私钥无口令。
	Passphrase string
	// Password 是密码认证的密码。
	Password string
	// AgentSocket 是 agent 套接字；空 = 平台默认（unix: $SSH_AUTH_SOCK，
	// windows: \\.\pipe\openssh-ssh-agent）。
	AgentSocket string
}

// KnownHostsMode 是主机指纹策略。
//
// 只有 Strict 一种**连接期**策略：首次信任必须由显式动作（Trust）写入条目后
// 再连。TOFU（连不上就自动追加）不提供——那正是 AutoAddPolicy 的形态。
type KnownHostsMode string

const (
	// KnownHostsStrict 是唯一允许的策略：缺条目即失败，由 Probe 报 host_unknown。
	KnownHostsStrict KnownHostsMode = "strict"
)

// KnownHostsSpec 指定指纹库位置。
type KnownHostsSpec struct {
	// Files 为空时用 ~/.ssh/known_hosts。
	Files []string
	Mode  KnownHostsMode
}

// Timeouts 是四段独立超时。四段必须分开配：一个 timeout 混用会把"主机不在"
// 与"口令错"与"命令慢"混成同一种表现（见 docs/arch/ssh-tool.md §5）。
type Timeouts struct {
	// Connect 只覆盖 TCP 建连。
	Connect time.Duration
	// Banner 覆盖版本交换 + 算法协商 + banner。
	Banner time.Duration
	// Auth 覆盖认证轮次（含 agent 交互与私钥口令）。
	Auth time.Duration
	// Channel 覆盖单条命令的通道存活；到点返回 channel_timeout，不是失败。
	Channel time.Duration
}

// DefaultTimeouts 是出厂默认值（可按 host 覆盖）。
func DefaultTimeouts() Timeouts {
	return Timeouts{
		Connect: 10 * time.Second,
		Banner:  10 * time.Second,
		Auth:    20 * time.Second,
		Channel: 120 * time.Second,
	}
}

// withDefaults 补齐零值，避免"零 = 无限等"。
func (t Timeouts) withDefaults() Timeouts {
	d := DefaultTimeouts()
	if t.Connect <= 0 {
		t.Connect = d.Connect
	}
	if t.Banner <= 0 {
		t.Banner = d.Banner
	}
	if t.Auth <= 0 {
		t.Auth = d.Auth
	}
	if t.Channel <= 0 {
		t.Channel = d.Channel
	}
	return t
}

// Spec 是一次连接的全部输入：对应项目内 host 注册表的一行（docs/arch/ssh-tool.md §8.2）。
type Spec struct {
	// ID 是稳定标识（不可变），用于日志与引用，不参与连接。
	ID string
	// Hostname 是纯主机名或 IP；user 单列，不塞进 hostname。
	Hostname string
	// Port 为 0 时用 22。
	Port int
	User string

	Auth       AuthSpec
	KnownHosts KnownHostsSpec
	Timeouts   Timeouts

	// Workdir 是要 cd 进去的远端目录（空 = 登录目录）。
	Workdir string
	// MaxOutputBytes 是单次命令 stdout/stderr 各自的字节上限（0 = 默认 256 KiB）。
	MaxOutputBytes int
}

// DefaultMaxOutputBytes 是输出上限默认值：常量成本落在上下文里，超了显式标 truncated。
const DefaultMaxOutputBytes = 256 << 10

// Addr 返回 host:port 形态（连接与指纹核对都用它）。
func (s Spec) Addr() string {
	port := s.Port
	if port <= 0 {
		port = 22
	}
	return fmt.Sprintf("%s:%d", s.Hostname, port)
}

// maxOutput 返回生效的输出上限。
func (s Spec) maxOutput() int {
	if s.MaxOutputBytes > 0 {
		return s.MaxOutputBytes
	}
	return DefaultMaxOutputBytes
}

// Client 是一条已建立的连接。并发使用不安全：一条连接与一个远端 sshd 会话通道
// 绑定，调用方按需自建（上层工具族每次调用一次 Dial）。
type Client struct {
	spec     Spec
	conn     net.Conn
	client   *xssh.Client
	authUsed string
	hostKey  recordedKey
}

// Close 关闭连接。可重复调用。
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// Spec 返回本连接使用的规格（探针与审计面用）。
func (c *Client) Spec() Spec { return c.spec }

// AuthUsed 返回**实际生效**的认证方式（agent / publickey / publickey+passphrase /
// password / keyboard-interactive）。
//
// 这条信息是排障里最贵的一格：以为在用 key、其实回落到了密码，是"能连上但认证
// 行为不符合预期"的典型现场。
func (c *Client) AuthUsed() string { return c.authUsed }

// HostKeyFingerprint 返回本次握手观测到的主机公钥指纹（SHA256，OpenSSH 形态）。
func (c *Client) HostKeyFingerprint() string { return c.hostKey.fingerprint }

// HostKey 返回本次握手观测到的公钥（Trust 用；nil = 没连上）。
func (c *Client) HostKey() xssh.PublicKey { return c.hostKey.key }
