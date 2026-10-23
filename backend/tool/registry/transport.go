package registry

// transport.go —— 传输抽象：**一个接口**（执行面）+ **一个读面**（这次调用用哪个执行面）。
//
// 为什么要把"传输"单独抽出来：命令类工具（bash 及后续的 ssh_* 族）真正需要的不是
// "一条命令"，而是"这条命令在哪台机器上跑"。把它写进每个 handler 的入参里，就变成
// 每个工具各自解释一遍"远端/本地"；写成 ctx 里的一个读面，则**选择**（谁选的、为什么
// 这么选、留没留痕）与**执行**（怎么跑）各归一处：
//
//	选择：transport_selector.go（permission 之后、diagnostic 之前的中间件）
//	执行：Kind == ssh 时由 Transport 承担；Kind == local 时 Transport 为 nil，
//	      执行面即今天那条本地路径（**不新增第二条本地实现**）
//
// 三条硬约束（违反即事故）：
//
//  1. 静默降级比失败更危险：调用方**显式指名** ssh 而它不可用时必须报错，不许悄悄
//     跑本地（本地跑一条 rm 与远端跑一条 rm 是两件事，模型/人都会读错结果）；
//  2. 选择结果必须**可留痕**：降级与失败都记一条 TransportTrace（不含命令、不含凭据）；
//  3. 错误码固定（SSH_*），且与权限拒绝**可区分**：权限拒绝 unwrap 到框架的
//     ErrPermissionDenied，且不携带任何传输码（见 IsTransportErrorCode 的用例）。

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// TransportKind 是一次调用选中的执行面。
type TransportKind string

const (
	// TransportLocal 表示在宿主本机执行（= 今天的行为；Transport 为 nil）。
	TransportLocal TransportKind = "local"
	// TransportSSH 表示在项目配置的远端主机上执行。
	TransportSSH TransportKind = "ssh"
)

// TransportRequest 是一次经传输执行的请求。命令本体是**脚本文本**（可以多行、
// 可以是 UTF-8）：具体走内联还是 stdin 由实现方按 ssh 包的纪律决定（非 ASCII /
// 多行只能走 stdin，见 backend/tool/ssh/exec.go）。
type TransportRequest struct {
	// Command 是要执行的命令或脚本正文。
	Command string
	// Workdir 是执行目录；空 = 实现的默认目录（远端为登录目录）。
	Workdir string
}

// TransportOutput 是一次传输执行的事实回报：不判业务成功（exit code 非零仍是
// "跑完了"），与 backend/tool/ssh 的 Result 同一口径。
type TransportOutput struct {
	Stdout       string
	Stderr       string
	ExitCode     int
	Status       string
	Truncated    bool
	StillRunning bool
	AuthUsed     string
	DurationMS   int64
}

// Transport 是"执行一条命令"的能力面：ssh 一支由 registry 里的适配器实现
// （ssh_transport.go，底座是 backend/tool/ssh），local 一支不实现（= 既有路径）。
//
// 方法名与设计稿 §4.1 的 Exec 对齐：后续的 Read/Write/Edit/Manage 四个能力面
// （同一份 handler、两份传输）按同一形状往这个接口上加，不再另开接口。
type Transport interface {
	// Kind 返回这一支的标识（只有 ssh 实现；local 由"没有 Transport"表达）。
	Kind() TransportKind
	// Exec 执行一次请求。返回的 error 一律是可分类的传输错误（见 TransportError）。
	Exec(ctx context.Context, req TransportRequest) (TransportOutput, error)
}

// TransportSelection 是"这次调用的执行面"——中间件写进 ctx、执行面读出来。
//
// 它是**一次调用的局部量**：同一进程里的两个会话各自的选择互不可见（并发矩阵
// 用例就是为了钉住这一点）。
type TransportSelection struct {
	// Kind 是选中的执行面（local / ssh）。
	Kind TransportKind
	// Transport 只在 Kind == ssh 时非 nil。
	Transport Transport
	// ProjectID 是这次调用归属的项目（工作区 ID）；空 = 没有项目归属。
	ProjectID string
	// Endpoint 是选中的端点（host:port）；local 时为空。**不含凭据**。
	Endpoint string
	// Reason 是这次选择的理由（留痕口径，取值见 transportReasons）。
	Reason string
	// Explicit 报告调用方是否显式指名了传输。
	Explicit bool
	// ProbeClass 是探针的失败分类（降级/报错时才有值，取值见 backend/tool/ssh）。
	ProbeClass string
}

// Local 报告这次调用是否走本地执行面。
func (s TransportSelection) Local() bool { return s.Kind != TransportSSH }

type transportSelectionKey struct{}

// WithTransportSelection 把一次调用的执行面写进 ctx。
func WithTransportSelection(ctx context.Context, selection TransportSelection) context.Context {
	return context.WithValue(ctx, transportSelectionKey{}, selection)
}

// TransportSelectionFromContext 读回这次调用的执行面（含理由，供诊断/审计）。
func TransportSelectionFromContext(ctx context.Context) (TransportSelection, bool) {
	if ctx == nil {
		return TransportSelection{}, false
	}
	selection, ok := ctx.Value(transportSelectionKey{}).(TransportSelection)
	return selection, ok
}

// TransportFromContext 只读执行面这一格：ok=false 表示"没有远端传输可用，按本地
// 执行"。执行面的消费方一律走这个函数，不要去猜 ctx 里还有没有别的开关。
func TransportFromContext(ctx context.Context) (Transport, bool) {
	selection, ok := TransportSelectionFromContext(ctx)
	if !ok || selection.Kind != TransportSSH || selection.Transport == nil {
		return nil, false
	}
	return selection.Transport, true
}

// 固定错误码。这五个是契约面：工具层/界面/用例都按它们判定，不许新增同义码。
const (
	// CodeSSHUnreachable 表示主机不可达（DNS/端口/防火墙/对端不是 SSH）。
	CodeSSHUnreachable = "SSH_UNREACHABLE"
	// CodeSSHAuthFailed 表示认证材料被拒或本端没有可用凭据。
	CodeSSHAuthFailed = "SSH_AUTH_FAILED"
	// CodeSSHHostUntrusted 表示 known_hosts 无条目或指纹不符（首次连接必须先显式信任）。
	CodeSSHHostUntrusted = "SSH_HOST_UNTRUSTED"
	// CodeSSHTimeout 表示某一段超出了本端的等待上限。
	CodeSSHTimeout = "SSH_TIMEOUT"
	// CodeSSHEnvDisabled 表示这台宿主上没有 SSH 能力（装配级开关关闭）。
	CodeSSHEnvDisabled = "SSH_ENV_DISABLED"
)

// TransportError 是传输选择的失败。Code 给机器判定，Msg 给人读的下一步。
//
// 与权限拒绝的区别是结构性的：这里从不 unwrap 到框架的 ErrPermissionDenied，
// 权限拒绝也从不携带 SSH_* 码。所以"被拒"与"连不上"在任何一层都能分开处理。
type TransportError struct {
	Code string
	Msg  string
	Err  error
}

func (e *TransportError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Code + ": " + e.Msg + ": " + e.Err.Error()
	}
	return e.Code + ": " + e.Msg
}

func (e *TransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// transportErr 造一个带固定码的传输错误。
func transportErr(code, format string, args ...any) error {
	return &TransportError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// TransportErrorCode 返回错误的传输码；不是传输错误时返回空串。
func TransportErrorCode(err error) string {
	var transport *TransportError
	if errors.As(err, &transport) && transport != nil {
		return transport.Code
	}
	return ""
}

// IsTransportErrorCode 报告 err 是否携带给定码之一（空列表 = 只要是一个固定码即可）。
//
// 这是"权限拒绝"与"传输失败"的唯一判定口：权限拒绝不带码，所以
// IsTransportErrorCode(permErr, ...) 恒为 false。
func IsTransportErrorCode(err error, codes ...string) bool {
	code := TransportErrorCode(err)
	if code == "" {
		return false
	}
	if len(codes) == 0 {
		return true
	}
	for _, want := range codes {
		if code == want {
			return true
		}
	}
	return false
}

// 一次选择的理由词表（留痕与用例共用一份）。
const (
	// ReasonExplicitLocal 表示调用方显式指名本地。
	ReasonExplicitLocal = "explicit_local"
	// ReasonEnvDisabled 表示宿主上 SSH 能力关闭。
	ReasonEnvDisabled = "env_disabled"
	// ReasonUnconfigured 表示项目没有配 SSH 连接。
	ReasonUnconfigured = "unconfigured"
	// ReasonProbeNotRun 表示还没有探测结论（未探）。
	ReasonProbeNotRun = "probe_not_run"
	// ReasonProbeFailed 表示探测失败。
	ReasonProbeFailed = "probe_failed"
	// ReasonProbeOK 表示探测通过、选中 ssh。
	ReasonProbeOK = "probe_ok"
	// ReasonExplicitSSH 表示调用方显式指名 ssh 且探测通过。
	ReasonExplicitSSH = "explicit_ssh"
	// ReasonDialFailed 表示探测通过但建连失败（降级到本地并留痕）。
	ReasonDialFailed = "dial_failed"
)

// TransportTrace 是一次传输选择的事实记录。不含命令正文、不含任何凭据：
// 它进日志/诊断读面的成本必须与内容无关。
type TransportTrace struct {
	At        time.Time
	SessionID string
	ProjectID string
	Tool      string
	Kind      TransportKind
	Explicit  bool
	// Degraded 表示"本可以/本应走远端，但回退到了本地"——这是需要被人看见的那一类。
	Degraded   bool
	Reason     string
	Endpoint   string
	ErrorCode  string
	ProbeClass string
}

// TransportTraceSink 是留痕口（装配层可换成结构化日志；nil = 不记）。
type TransportTraceSink interface {
	RecordTransportTrace(TransportTrace)
}

// TransportTraceLog 是可注入的内存留痕（环形、并发安全）：诊断读面与用例都读它。
type TransportTraceLog struct {
	mu      sync.Mutex
	max     int
	entries []TransportTrace
}

// NewTransportTraceLog 造一个容量为 max 的留痕（max<=0 取默认 64）。
func NewTransportTraceLog(max int) *TransportTraceLog {
	if max <= 0 {
		max = 64
	}
	return &TransportTraceLog{max: max}
}

// RecordTransportTrace 追加一条（满了丢最旧的一条：留痕是诊断辅助，不是账本）。
func (log *TransportTraceLog) RecordTransportTrace(entry TransportTrace) {
	if log == nil {
		return
	}
	if entry.At.IsZero() {
		entry.At = time.Now()
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if len(log.entries) >= log.max {
		log.entries = append(log.entries[:0], log.entries[1:]...)
	}
	log.entries = append(log.entries, entry)
}

// Entries 返回留痕快照（只读拷贝）。
func (log *TransportTraceLog) Entries() []TransportTrace {
	if log == nil {
		return nil
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]TransportTrace(nil), log.entries...)
}

// Degradations 只返回降级/失败那几条（"该走远端却走了本地"是最需要被看见的一类）。
func (log *TransportTraceLog) Degradations() []TransportTrace {
	if log == nil {
		return nil
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	out := make([]TransportTrace, 0, len(log.entries))
	for _, entry := range log.entries {
		if entry.Degraded || entry.ErrorCode != "" {
			out = append(out, entry)
		}
	}
	return out
}
