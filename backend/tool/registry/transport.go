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
	"strings"
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
	// Stdin 是**命令的 stdin 数据**（与 Command 正交：命令走命令行，数据走内容通道）。
	//
	// 为什么要把它做成一个字段而不是让调用方 heredoc 拼接：数据一旦拼进命令正文，
	// 就要经过 shell 词法（引号、`$`、反引号、代码页），任何一处漏转义都是**静默**
	// 改数据；而数据通道是字节透明的。远端实现把它原样喂给进程 stdin（见
	// backend/tool/ssh 的 Request.StdinData）。
	Stdin string
	// Workdir 是执行目录；空 = 实现的默认目录（远端为登录目录）。
	//
	// **远端面上它是远端路径**，不做本地解析：命令正文与路径的改写属权限模式表，
	// 传输层不猜（见 transport_surface_test.go 的"远端路径不被本地改写"用例）。
	Workdir string
	// Timeout 是这次执行在本端的等待预算（0 = 实现方默认）。预算用尽**不是失败**：
	// 远端作业仍在跑，回执里给 handle 与 log_path（见 TransportOutput）。
	Timeout time.Duration
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
	// Handle / LogPath 只在"仍在跑"（或在远端脱钩）时有值：它们是**取回与终止的
	// 唯一凭据**，没有它们就不许在回执里说"稍后取回"。
	Handle  string
	LogPath string
	// Note 是事实提醒（不含秘密）：为什么拿不到输出、为什么调用方不该把它读成失败。
	Note string
}

// TransportReadRequest 是一次远端读请求。Path 是**远端路径**（原样，不解析）。
type TransportReadRequest struct {
	Path      string
	StartLine int
	EndLine   int
}

// TransportReadOutput 是一次远端读的事实回报（Content 已按窗口切好）。
type TransportReadOutput struct {
	Content    string
	TotalLines int
	Bytes      int
	// Truncated 表示**内容在传输上限处被截断**：回执必须说出去，否则残缺内容会被
	// 当成完整文件读（这是"静默损坏"里最贵的一类）。
	Truncated bool
	Note      string
}

// TransportWriteRequest 是一次远端写请求（覆盖写；Content 走数据通道）。
type TransportWriteRequest struct {
	Path    string
	Content string
}

// TransportWriteOutput 是一次远端写的事实回报。
type TransportWriteOutput struct {
	Bytes int
}

// TransportEditRequest 是一次远端替换请求（语义与本地 actor 的 Edit 同口径：
// 全部出现处替换，返回替换次数）。
type TransportEditRequest struct {
	Path      string
	OldString string
	NewString string
}

// TransportEditOutput 是一次远端替换的事实回报。
type TransportEditOutput struct {
	Replacements int
}

// TransportManageRequest 是一次远端作业管理请求（Op 词表与 bg_manage 同源：
// observe / fetch / kill / done）。
type TransportManageRequest struct {
	Op       string
	Handle   string
	WaitMS   int
	MaxBytes int
}

// TransportManageOutput 是一次远端作业管理的事实回报。
type TransportManageOutput struct {
	// Handle 是新建远端作业时返回的自描述句柄（`ssh:` 族）。
	Handle string
	// State 是远端作业的状态（"running" / "exited" / "gone"）。
	State        string
	ExitCode     int
	Stdout       string
	Stderr       string
	Truncated    bool
	StillRunning bool
	LogPath      string
	Note         string
}

// 远端作业状态词表（与本地作业面的 running/done 语义对齐，词不同因为它们说的
// 不是同一张表：这里是远端进程目录的事实）。
const (
	// RemoteJobRunning 表示远端进程还在（没有退出码文件）。
	RemoteJobRunning = "running"
	// RemoteJobExited 表示远端进程已结束（有退出码文件）。
	RemoteJobExited = "exited"
	// RemoteJobGone 表示远端作业目录不存在（被清理 / 换了机器）。
	RemoteJobGone = "gone"
)

// Transport 是"一次远端调用"的能力面：ssh 一支由 registry 里的适配器实现
// （ssh_transport.go，底座是 backend/tool/ssh），local 一支不实现（= 既有路径）。
//
// 为什么一个接口装五个方法而不是五个接口：这五个能力面是**同一份 handler 的两份
// 传输**（服务端差异只在"怎么连、怎么带字节"，不在"工具长什么样"）。拆成五个接口
// 只会让每个 handler 各写一遍类型断言，且"全都要"这件事从此不可见（漏实现一个方法
// 就到运行时才发现）。宽接口 + 唯一的 ssh 实现，编译器替我们钉住完整性。
type Transport interface {
	// Kind 返回这一支的标识（只有 ssh 实现；local 由"没有 Transport"表达）。
	Kind() TransportKind
	// Exec 执行一次命令（命令正文 + stdin 数据）。
	Exec(ctx context.Context, req TransportRequest) (TransportOutput, error)
	// Start 派发一条脱钩后台作业，只返回受理回执，不等待命令结束。
	Start(ctx context.Context, req TransportRequest) (TransportManageOutput, error)
	// Read 读一个远端文件（按行窗口切好再回来）。
	Read(ctx context.Context, req TransportReadRequest) (TransportReadOutput, error)
	// Write 覆盖写一个远端文件（内容走数据通道，不经 shell 词法）。
	Write(ctx context.Context, req TransportWriteRequest) (TransportWriteOutput, error)
	// Edit 在远端文件里做一次"全部出现处替换"。
	Edit(ctx context.Context, req TransportEditRequest) (TransportEditOutput, error)
	// Manage 管理远端作业（observe / fetch / kill / done）。
	Manage(ctx context.Context, req TransportManageRequest) (TransportManageOutput, error)
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
	// Env 是这次调用的**环境名**（回执里要写的那一格）：ssh = 端点的显示名
	// （配置里有名字就用名字，否则 user@host:port），local = "local"。
	Env string
	// Reason 是这次选择的理由（留痕口径，取值见 transportReasons）。
	Reason string
	// Explicit 报告调用方是否显式指名了传输。
	Explicit bool
	// ProbeClass 是探针的失败分类（降级/报错时才有值，取值见 backend/tool/ssh）。
	ProbeClass string
}

// Local 报告这次调用是否走本地执行面。
func (s TransportSelection) Local() bool { return s.Kind != TransportSSH }

// ExecutionSurface 返回回执里的执行面标识（"local" / "ssh"）。
//
// 判据只有一处：本地就是"没有选中 ssh"，不去看 Transport 是否为 nil——那会把
// "选了 ssh 但装配漏了实例"报成 local，而这正是最危险的一种谎（跑本地却说远端）。
func (s TransportSelection) ExecutionSurface() string {
	if s.Local() {
		return string(TransportLocal)
	}
	return string(TransportSSH)
}

// EnvLabel 返回回执里的环境名；没有选择（未装配选择器）时按本地报。
func (s TransportSelection) EnvLabel() string {
	if s.Local() {
		if trimmed := strings.TrimSpace(s.Env); trimmed != "" {
			return trimmed
		}
		return string(TransportLocal)
	}
	if trimmed := strings.TrimSpace(s.Env); trimmed != "" {
		return trimmed
	}
	return trimmedOrString(s.Endpoint, string(TransportSSH))
}

// trimmedOrString 是"空则取 fallback"（本文件与选择器共用一处，别各写一份）。
func trimmedOrString(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

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

// ExecutionFace 返回回执里**必须**自报的两格：执行面（local|ssh）与环境名。
//
// 五份 handler 都从这里取（一份判据 = 一处函数）：远端回执少写这两格，模型就会把
// 远端文件当本地文件、把远端输出当本机现场，而它下一次调用可能就落在另一侧。
func ExecutionFace(ctx context.Context) (surface, env string) {
	selection, ok := TransportSelectionFromContext(ctx)
	if !ok {
		return string(TransportLocal), string(TransportLocal)
	}
	return selection.ExecutionSurface(), selection.EnvLabel()
}

// RemoteTransport 返回"这次调用的远端执行面"（ok=false = 走本地既有路径）。
func RemoteTransport(ctx context.Context) (Transport, bool) {
	return TransportFromContext(ctx)
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
	// CodeSSHEnvUnknown 表示调用方指名的**环境名**不在项目环境表里。
	//
	// 与 SSH_ENV_DISABLED 分开：那个是"这台宿主没有 SSH 能力"，这个是"项目里没有
	// 这个环境"。混成一个码会让用户去查宿主的开关，而真正要改的是项目配置。
	CodeSSHEnvUnknown = "SSH_ENV_UNKNOWN"
	// CodeSSHEnvConflict 表示工具同时指名了环境与相反的传输类型。
	CodeSSHEnvConflict = "SSH_ENV_CONFLICT"
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
	// ReasonEnvironmentLocal 表示命名环境本身要求本地执行。
	ReasonEnvironmentLocal = "environment_local"
	// ReasonEnvironmentSSH 表示命名环境本身要求 SSH 执行。
	ReasonEnvironmentSSH = "environment_ssh"
	// ReasonEnvironmentConflict 表示命名环境与显式 transport 互相矛盾。
	ReasonEnvironmentConflict = "environment_conflict"
	// ReasonSessionEnvironmentUnbound 表示会话还没有固定命名环境，按本地执行。
	ReasonSessionEnvironmentUnbound = "session_environment_unbound"
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
