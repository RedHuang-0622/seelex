package registry

// transport_selector.go —— 传输选择中间件（**决策 + 建连两段**，见 transport_decision.go）。
//
// 链上位置有两处，同一份中间件装在两处（靠 ctx 段标记区分）：
//
//	事件 → **决策段** → 路径门 → 权限门 → **建连段** → 诊断 → handler
//
//   - 决策段（位置：事件之后、路径门**之前**）：只读项目连接配置与探针缓存，算出
//     "这次走哪个面"写进 ctx。**绝不建连、不发网络包**——它跑在权限门之前，被拒的
//     调用也会经过它。路径门据此按面取根（path_gate_face.go），这是"自动选 ssh 时
//     远端绝对路径被本地根误拒"的修法。
//   - 建连段（位置：权限门**之后**、diagnostic 之前）：决策已定，这里才探针/拨号。
//     放权限门之前：被拒的调用会先去连远端——一次越权尝试就能让本进程对外发起一次
//     SSH 握手，白连 + 白暴露（还可能把口令失败次数记到远端的审计里）；放 diagnostic
//     之后：诊断看到的是"已经选完"的调用，选择失败/降级的现场就只剩一行结果，看不出
//     "为什么没走远端"。
//
// 选择规则（四格加上显式指名，判据只写在这里，不散到各个 handler）：
//
//	① 项目已配连接 + 探针通                  → ssh
//	② 未配 / 未探 / 探针挂 / 建连失败 + 未指名 → 降级 local，**留痕**（Degraded）
//	③ 显式指名而不可用                        → 报错（SSH_* 固定码），**绝不降级**
//	④ 显式指名 local                          → local（尊重调用方，不留降级痕）
//
// "显式指名"有两个来源，优先级由高到低：调用 ctx（宿主/界面按会话指定，见
// WithExplicitTransport）> 工具入参的 "transport" 字段（模型自己挑）。

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	seelessh "github.com/RedHuang-0622/seelex/backend/tool/ssh"
)

// TransportArgKey 是工具入参里"显式指名传输"的字段名（"" / "auto" = 未指名）。
const TransportArgKey = "transport"

// SSHEndpoint 是选择器需要的 SSH 端点读数：**无口令**（凭据面不在这里）。
//
// 单独一个窄形状而不是直接吃 model.WorkspaceConnection：registry 不认识项目配置的
// 全貌（git 那半截、口令三态都不该出现在工具层的判定里），装配层负责把它折出来。
type SSHEndpoint struct {
	Host string
	Port int
	User string
	// Name 是这次连接在**人眼里的名字**（项目连接配置里的显示名；空 = 没有名字）。
	// 它进回执的 env 一格：模型看到的不该只是一个 IP——"哪台机器"是执行事实的一部分。
	Name string
	// RemoteRoot 是**远端项目根**（路径门的远端取根口径；空 = 未知）。
	//
	// 来源是项目连接配置的 SSH.RemoteRoot（远端工作目录，对端 POSIX 绝对路径），
	// 由装配层折进来（assemble.sshEndpoint）。它让"根内 / 越界"在远端面上有真正的
	// 判据：根内路径不需要 ssh.userspace，走出它才需要（见 path_gate_face.go）。
	// 空（未配置）时路径门退回保守口径：把本地根也容不下的绝对路径全算成越界候选。
	RemoteRoot string
}

// Addr 返回 host:port 形态（端口缺省 22）。
func (e SSHEndpoint) Addr() string {
	port := e.Port
	if port <= 0 {
		port = 22
	}
	return net.JoinHostPort(strings.TrimSpace(e.Host), strconv.Itoa(port))
}

// Label 返回环境名：有名字用名字，否则 user@host:port（回执 env 一格的判据只此一处）。
func (e SSHEndpoint) Label() string {
	if trimmed := strings.TrimSpace(e.Name); trimmed != "" {
		return trimmed
	}
	if user := strings.TrimSpace(e.User); user != "" {
		return user + "@" + e.Addr()
	}
	return e.Addr()
}

// Configured 报告这个端点是否真的被配置过（没配主机 = 未配）。
func (e SSHEndpoint) Configured() bool { return strings.TrimSpace(e.Host) != "" }

// TransportProbeState 是探测结论的三态。
type TransportProbeState string

const (
	// TransportProbeNotRun 表示还没有结论（未探）——连接是花钱的动作，选择器不替人探。
	TransportProbeNotRun TransportProbeState = ""
	// TransportProbeOK 表示上一次探测通过。
	TransportProbeOK TransportProbeState = "ok"
	// TransportProbeFailed 表示上一次探测失败。
	TransportProbeFailed TransportProbeState = "failed"
)

// TransportProbeVerdict 是一次探测的结论（Class 取值见 backend/tool/ssh 的 Class*）。
type TransportProbeVerdict struct {
	State  TransportProbeState
	Class  string
	Detail string
}

// TransportSelectorDeps 是选择器的全部外部依赖（函数值端口：registry 不反向依赖
// 装配层，也不认识项目存储与凭据面）。
type TransportSelectorDeps struct {
	// Enabled 是装配级开关：false = 这台宿主上没有 SSH 能力（SSH_ENV_DISABLED）。
	Enabled bool

	// Consumers 是**真的会读执行面**的工具名集合（见 TransportConsumers）。
	//
	// 空集 = 直通：中间件不返回（nil MetaMiddleware），链上逐字等于今天的行为。
	// 这条闸门不是可选装饰：选择器把 ssh 写进 ctx、而 handler 不读它，就是"说走远端
	// 其实跑了本地"——比不选更危险。因此消费者名单与实际消费点必须同一个提交里改。
	Consumers map[string]struct{}

	// SessionKey 从调用 ctx 解析会话（留痕与项目归属）。
	SessionKey func(ctx context.Context) string
	// Project 解析这次调用归属的项目（工作区 ID）；按会话，不按进程。
	Project func(ctx context.Context, sessionID string) string
	// Endpoint 读项目连接配置里的 SSH 端点（读面**永不返回口令**）。
	// 返回 (endpoint, true, nil) = 配了；(_, false, nil) = 没配；error = 读失败。
	Endpoint func(projectID string) (SSHEndpoint, bool, error)
	// EndpointFor 按**环境名**读项目环境表里的端点（nil = 未装配环境表）。
	//
	// 与 Endpoint 的分工：Endpoint 是单条连接兜底（env 为空时用），EndpointFor 是
	// 项目侧的多环境表（dev/test/prod）。一次调用只会走其中一条——不会"两个都查、
	// 谁有值用谁"，那会让"环境表为空"和"环境名打错"变成同一个结果。
	EndpointFor func(projectID, env string) (SSHEndpoint, bool, error)
	// EnvFor 读**会话当前限定的环境名**（缺省环境）；nil 或空串 = 没有缺省。
	//
	// 调用方显式写了 env 参数时**不读它**：显式值永远优先，否则一次性的环境切换
	// 会被会话状态悄悄改回去。
	EnvFor func(ctx context.Context, sessionID string) string
	// EnvGranted 报告某环境是否在**该会话**被放权（nil = 放权面未装配 → 一律拒绝）。
	//
	// 它挡的是"显式 env 参数绕过会话放权"这条直路：会话状态里没有放权的环境，
	// 工具调用也不许指名。
	EnvGranted func(ctx context.Context, sessionID, env string) bool
	// Probe 读**缓存**的探测结论：ok=false 表示还没有结论（未探）。
	//
	// 它必须是一次纯读（内存缓存）：决策段在权限门之前跑，一次调用就会读一次。
	Probe func(ctx context.Context, projectID string, endpoint SSHEndpoint) (TransportProbeVerdict, bool)
	// ProbeNow 当场探一次。只在"显式指名 ssh 且未探"时调用，且只在**建连段**——
	// 这一次的代价由指名者付。
	ProbeNow func(ctx context.Context, projectID string, endpoint SSHEndpoint) TransportProbeVerdict
	// Dial 建立 ssh 传输。只在建连段、探测通过之后调用（探针通 = 可以建连）。
	Dial func(ctx context.Context, projectID string, endpoint SSHEndpoint) (Transport, error)
	// Trace 是留痕口（nil = 不记）。
	Trace TransportTraceSink
}

// TransportConsumers 声明哪些工具会读执行面（空参数 = 直通）。
func TransportConsumers(names ...string) map[string]struct{} {
	if len(names) == 0 {
		return nil
	}
	consumers := make(map[string]struct{}, len(names))
	for _, name := range names {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			consumers[trimmed] = struct{}{}
		}
	}
	return consumers
}

// TransportSelector 返回传输选择中间件（**两段共用一份**，见文件头与 transport_decision.go）。
//
// 同一份中间件装在两处：决策段（事件之后、路径门之前）与建连段（权限门之后、诊断
// 之前）。它靠 ctx 上的段标记区分本次是哪一段：
//
//	决策段 → 只算"这次走哪个面"写进 ctx（WithTransportDecision），转发，绝不建连；
//	建连段 → 按决策落地（探针费 / 建连 / 降级 / 报错），把 selection 写进 ctx。
//
// 没有标记时按**建连段**处理（直接拿中间件调用 = 旧口径：一次调用一个结论）。
// 没有消费者时返回 nil（框架跳过 nil 中间件）：链上逐字等于旧顺序。
func TransportSelector(deps TransportSelectorDeps) frameworktools.MetaMiddleware {
	if len(deps.Consumers) == 0 {
		return nil
	}
	return func(name string, _ frameworktools.ToolMeta, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		if _, ok := deps.Consumers[name]; !ok {
			// 不读执行面的工具：一个判断都不做，也不留痕（否则留痕会被噪音淹掉）。
			return next
		}
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			explicitKind, explicit := explicitTransport(ctx, argsJSON)
			// 已有决策（决策段跑过）就**不重算**：一次调用只有一份选择事实。
			decision, decided := TransportDecisionFromContext(ctx)
			if !decided {
				decision = resolveTransportDecision(ctx, deps, name, explicit, explicitKind, explicitEnv(argsJSON))
			}
			if transportStageFromContext(ctx) == transportStageDecision {
				// 决策段：只写事实，绝不建连（跑在权限门之前，被拒的调用也会经过）。
				return next.Execute(WithTransportDecision(ctx, decision), argsJSON)
			}
			selection, err := bindTransportSelection(ctx, deps, name, decision)
			if err != nil {
				return "", err
			}
			return next.Execute(WithTransportSelection(ctx, selection), argsJSON)
		})
	}
}

// bindTransportSelection 是**建连段**：决策已定，这里只落地 + 留痕 + 报错。
//
// 为什么建连必须迟绑定到这里：这一刻已经在权限门之后——被拒的调用不会白连一次远端
// （越权尝试不许让本进程对外握手），显式指名不可用的报错也不会顶掉权限拒绝。
func bindTransportSelection(
	ctx context.Context,
	deps TransportSelectorDeps,
	toolName string,
	decision TransportDecision,
) (TransportSelection, error) {
	sessionID := ""
	if deps.SessionKey != nil {
		sessionID = deps.SessionKey(ctx)
	}
	local := TransportSelection{
		Kind: TransportLocal, ProjectID: decision.ProjectID, Explicit: decision.Explicit,
		// 本地回执的 env 一格：就是 "local"（不是空串——空格在回执里等于没说）。
		Env: string(TransportLocal),
	}
	record := func(selection TransportSelection, degraded bool, code, probeClass string) {
		if deps.Trace == nil {
			return
		}
		deps.Trace.RecordTransportTrace(TransportTrace{
			SessionID: sessionID, ProjectID: decision.ProjectID, Tool: toolName,
			Kind: selection.Kind, Explicit: decision.Explicit, Degraded: degraded,
			Reason: selection.Reason, Endpoint: selection.Endpoint,
			ErrorCode: code, ProbeClass: probeClass,
		})
	}
	// 显式指名 ssh 但不可用：一律报错（不降级的唯一出口）。
	fail := func(code, probeClass, format string, args ...any) (TransportSelection, error) {
		record(local, true, code, probeClass)
		return TransportSelection{}, transportErr(code, format, args...)
	}
	// 非显式指名的远端失败：降级到本地并留痕（本地与远端不是同一件事，必须留理由）。
	degrade := func(reason, code, probeClass string) (TransportSelection, error) {
		local.Reason, local.ProbeClass = reason, probeClass
		record(local, true, code, probeClass)
		return local, nil
	}

	// 本地面：决策已经落定（未配 / 未探 / 探针挂 / 显式指名 local / 开关关闭）。
	if !decision.Remote() {
		local.Reason, local.ProbeClass = decision.Reason, decision.ProbeClass
		record(local, decision.Degraded, "", decision.ProbeClass)
		return local, nil
	}

	// 远端命名空间但已判不可用：报错或降级（判据：调用方是否显式指名）。
	if decision.FailureCode != "" {
		if !decision.Explicit {
			return degrade(ReasonUnconfigured, decision.FailureCode, decision.ProbeClass)
		}
		record(local, true, decision.FailureCode, decision.ProbeClass)
		return TransportSelection{}, &TransportError{Code: decision.FailureCode, Msg: decision.FailureMsg}
	}

	// 显式指名 + 未探：探测费由指名者付（这一段的唯一一次网络动作）。
	if decision.NeedsProbe {
		if deps.ProbeNow == nil {
			return fail(CodeSSHUnreachable, "",
				"项目 %s 的 SSH 端点 %s 还没有探测结论，无法确认可用性；"+
					"显式指名的 ssh 不降级（先跑一次探针）", decision.ProjectID, decision.Endpoint.Addr())
		}
		verdict := deps.ProbeNow(ctx, decision.ProjectID, decision.Endpoint)
		decision.ProbeState, decision.ProbeClass, decision.ProbeDetail = verdict.State, verdict.Class, verdict.Detail
		decision.Feasible = verdict.State == TransportProbeOK
		if !decision.Feasible {
			code := transportCodeForProbeClass(verdict.Class)
			return fail(code, verdict.Class,
				"项目 %s 的 SSH 端点 %s 探测失败（分类 %s），显式指名的 ssh 不降级到本地%s",
				decision.ProjectID, decision.Endpoint.Addr(), trimmedOr(verdict.Class, "unknown"), probeDetailSuffix(verdict.Detail))
		}
	}
	if !decision.Feasible {
		// 结构上不该到这里（不可用已在决策段记成 FailureCode）：宁可显式报错，也不
		// 假装"可以走远端"然后跑本地。
		code := transportCodeForProbeClass(decision.ProbeClass)
		if !decision.Explicit {
			return degrade(ReasonProbeFailed, code, decision.ProbeClass)
		}
		return fail(code, decision.ProbeClass,
			"项目 %s 的 SSH 端点 %s 探测结论不可用（分类 %s），显式指名的 ssh 不降级到本地",
			decision.ProjectID, decision.Endpoint.Addr(), trimmedOr(decision.ProbeClass, "unknown"))
	}

	// ① 探针通 → 建 ssh 一支。
	transport, err := dialTransport(ctx, deps, decision.ProjectID, decision.Endpoint)
	if err != nil {
		code := transportCodeForProbeClass(errProbeClass(err))
		if !decision.Explicit {
			return degrade(ReasonDialFailed, code, errProbeClass(err))
		}
		return fail(code, errProbeClass(err),
			"项目 %s 的 SSH 端点 %s 建连失败，显式指名的 ssh 不降级到本地: %v",
			decision.ProjectID, decision.Endpoint.Addr(), err)
	}

	selection := TransportSelection{
		Kind: TransportSSH, Transport: transport, ProjectID: decision.ProjectID,
		Endpoint: decision.Endpoint.Addr(), Env: decision.Endpoint.Label(),
		Explicit: decision.Explicit, Reason: decision.Reason,
	}
	record(selection, false, "", "")
	return selection, nil
}

// dialTransport 建连（deps 没给建连面时按"不可用"处理：宁可降级并留痕，也不假装）。
func dialTransport(ctx context.Context, deps TransportSelectorDeps, projectID string, endpoint SSHEndpoint) (Transport, error) {
	if deps.Dial == nil {
		return nil, &TransportError{Code: CodeSSHUnreachable, Msg: "没有装配 SSH 建连面"}
	}
	transport, err := deps.Dial(ctx, projectID, endpoint)
	if err != nil {
		return nil, err
	}
	if transport == nil {
		return nil, &TransportError{Code: CodeSSHUnreachable, Msg: "SSH 建连面返回了空传输"}
	}
	return transport, nil
}

// transportCodeForProbeClass 把探针分类翻成**固定**错误码（唯一的映射表）。
//
// 未知分类一律落 SSH_UNREACHABLE：把它报成"认证失败"会让人去改凭据，而真实病因往往
// 是网络——错误码的作用是缩小排查范围，不是给一个看起来更专业的答案。
func transportCodeForProbeClass(class string) string {
	switch class {
	case seelessh.ClassAuthRejected, seelessh.ClassNoAuthMethod, seelessh.ClassNoCredential:
		return CodeSSHAuthFailed
	case seelessh.ClassHostUnknown, seelessh.ClassKeyMismatch, seelessh.ClassHostRevoked:
		return CodeSSHHostUntrusted
	case seelessh.ClassTimeout:
		return CodeSSHTimeout
	case seelessh.ClassUnreachable, seelessh.ClassNotSSH, seelessh.ClassAlgorithmMismatch:
		return CodeSSHUnreachable
	}
	return CodeSSHUnreachable
}

// errProbeClass 从错误里取探针分类（ssh 适配器用 ProbeClassError 带过来）。
func errProbeClass(err error) string {
	var probe *ProbeClassError
	if errors.As(err, &probe) && probe != nil {
		return probe.Class
	}
	return ""
}

// ProbeClassError 让建连失败也带上探针分类（错误码与分类同源，界面才能说清"哪一段坏了"）。
type ProbeClassError struct {
	Class string
	Err   error
}

func (e *ProbeClassError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Class + ": " + e.Err.Error()
	}
	return e.Class
}

func (e *ProbeClassError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// probeDetailSuffix 把探针的 Detail 收成一句可读后缀（空则不追加）。
func probeDetailSuffix(detail string) string {
	if trimmed := strings.TrimSpace(detail); trimmed != "" {
		return "（" + trimmed + "）"
	}
	return ""
}

// trimmedOr 返回去空白后的值，空则用 fallback。
func trimmedOr(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

// WithExplicitTransport 让宿主/界面按会话显式指名传输（优先级高于工具入参）。
func WithExplicitTransport(ctx context.Context, kind TransportKind) context.Context {
	return context.WithValue(ctx, explicitTransportKey{}, kind)
}

type explicitTransportKey struct{}

// explicitTransport 解析"调用方有没有显式指名"：ctx 优先，其次工具入参。
func explicitTransport(ctx context.Context, argsJSON string) (TransportKind, bool) {
	if ctx != nil {
		if kind, ok := ctx.Value(explicitTransportKey{}).(TransportKind); ok {
			if normalized, valid := normalizeTransportKind(string(kind)); valid {
				return normalized, true
			}
		}
	}
	return explicitTransportFromArgs(argsJSON)
}

// explicitEnv 读调用方指名的**环境名**（工具参数 env）。
//
// 与 explicitTransport 分开而不是塞进一个结构：两者是正交的——"走哪个面"（local /
// ssh）与"用哪个环境"（dev / test / prod）可以各自独立指名，混在一起就会出现
// "指名了 env 但传输是 local"这种自相矛盾的输入，还得再编一套优先级去解释它。
//
// 空串 = 没指名。不做任何猜测（不把 "default" 之类当成默认名）：环境名只在项目
// 环境表里有意义，猜一个不存在的名字只会得到一条更远的报错。
func explicitEnv(argsJSON string) string {
	if strings.TrimSpace(argsJSON) == "" {
		return ""
	}
	var args struct {
		Env string `json:"env"`
	}
	if json.Unmarshal([]byte(argsJSON), &args) != nil {
		return ""
	}
	return strings.TrimSpace(args.Env)
}

// explicitTransportFromArgs 读工具入参里的 transport 字段（"auto" / 空 = 未指名）。
func explicitTransportFromArgs(argsJSON string) (TransportKind, bool) {
	if strings.TrimSpace(argsJSON) == "" {
		return "", false
	}
	var args struct {
		Transport string `json:"transport"`
	}
	if json.Unmarshal([]byte(argsJSON), &args) != nil {
		return "", false
	}
	return normalizeTransportKind(args.Transport)
}

// normalizeTransportKind 归一化指名取值；未指名/无法识别一律视为"没指名"。
func normalizeTransportKind(raw string) (TransportKind, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(TransportSSH):
		return TransportSSH, true
	case string(TransportLocal):
		return TransportLocal, true
	}
	return "", false
}
