package registry

// transport_selector.go —— 传输选择中间件。
//
// 链上位置：**permission 之后、diagnostic 之前**（见 registry_state.go 的
// NewRegistryState 装配序）。为什么必须在这个位置：
//
//   - 放 permission 之前：被拒的调用会先去连远端——一次越权尝试就能让本进程对外
//     发起一次 SSH 握手，白连 + 白暴露（还可能把口令失败次数记到远端的审计里）；
//   - 放 diagnostic 之后：诊断看到的是"已经选完"的调用，选择失败/降级的现场就只剩
//     一行结果，看不出"为什么没走远端"。
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
	// Probe 读**缓存**的探测结论：ok=false 表示还没有结论（未探）。
	Probe func(ctx context.Context, projectID string, endpoint SSHEndpoint) (TransportProbeVerdict, bool)
	// ProbeNow 当场探一次。只在"显式指名 ssh 且未探"时调用——这一次的代价由指名者付。
	ProbeNow func(ctx context.Context, projectID string, endpoint SSHEndpoint) TransportProbeVerdict
	// Dial 建立 ssh 传输。只在探测通过之后调用（探针通 = 可以建连）。
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

// TransportSelector 返回传输选择中间件。
//
// 没有消费者时返回 nil（框架跳过 nil 中间件）：链上顺序仍是
// 事件 → 路径门 → 权限 → **（这里）** → 诊断 → handler。
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
			selection, err := resolveTransportSelection(ctx, deps, name, explicit, explicitKind)
			if err != nil {
				return "", err
			}
			return next.Execute(WithTransportSelection(ctx, selection), argsJSON)
		})
	}
}

// resolveTransportSelection 是选择规则的唯一实现（顺序即优先级，勿重排）。
func resolveTransportSelection(
	ctx context.Context,
	deps TransportSelectorDeps,
	toolName string,
	explicit bool,
	explicitKind TransportKind,
) (TransportSelection, error) {
	sessionID := ""
	if deps.SessionKey != nil {
		sessionID = deps.SessionKey(ctx)
	}
	projectID := ""
	if deps.Project != nil {
		projectID = deps.Project(ctx, sessionID)
	}

	local := TransportSelection{
		Kind: TransportLocal, ProjectID: projectID, Explicit: explicit,
		// 本地回执的 env 一格：就是 "local"（不是空串——空格在回执里等于没说）。
		Env: string(TransportLocal),
	}
	record := func(selection TransportSelection, degraded bool, code, probeClass string) {
		if deps.Trace == nil {
			return
		}
		deps.Trace.RecordTransportTrace(TransportTrace{
			SessionID: sessionID, ProjectID: projectID, Tool: toolName,
			Kind: selection.Kind, Explicit: explicit, Degraded: degraded,
			Reason: selection.Reason, Endpoint: selection.Endpoint,
			ErrorCode: code, ProbeClass: probeClass,
		})
	}
	// 显式指名 ssh 但不可用：一律报错（不降级的唯一出口）。
	fail := func(code, probeClass, format string, args ...any) (TransportSelection, error) {
		record(local, true, code, probeClass)
		return TransportSelection{}, transportErr(code, format, args...)
	}

	// ④ 显式指名 local：尊重调用方，不去连远端，也不记成降级。
	if explicit && explicitKind == TransportLocal {
		local.Reason = ReasonExplicitLocal
		record(local, false, "", "")
		return local, nil
	}

	// 装配级开关：宿主没有 SSH 能力。
	if !deps.Enabled {
		if explicit && explicitKind == TransportSSH {
			return fail(CodeSSHEnvDisabled, "",
				"这台宿主上 SSH 传输未启用，调用方显式指名的 %s 无法执行（不降级到本地）", toolName)
		}
		local.Reason = ReasonEnvDisabled
		record(local, false, "", "")
		return local, nil
	}

	// 项目连接配置：未配 = 没有远端可去。
	var endpoint SSHEndpoint
	configured := false
	if deps.Endpoint != nil && projectID != "" {
		read, ok, err := deps.Endpoint(projectID)
		if err != nil {
			if explicit && explicitKind == TransportSSH {
				return fail(CodeSSHUnreachable, "",
					"读取项目 %s 的连接配置失败，显式指名的 ssh 无法确认可用性: %v", projectID, err)
			}
			local.Reason = ReasonUnconfigured
			record(local, false, "", "")
			return local, nil
		}
		endpoint, configured = read, ok && read.Configured()
	}
	if !configured {
		if explicit && explicitKind == TransportSSH {
			return fail(CodeSSHUnreachable, "",
				"项目 %s 没有配置 SSH 连接，显式指名的 ssh 无法执行（不降级到本地；"+
					"先在项目设置里填 SSH 端点，或显式指名 transport=local）", trimmedOr(projectID, "(未绑定项目)"))
		}
		local.Reason = ReasonUnconfigured
		record(local, false, "", "")
		return local, nil
	}
	local.Endpoint = endpoint.Addr()

	// 探测结论：未探是"不知道"，不是"通"。
	verdict, probed := TransportProbeVerdict{}, false
	if deps.Probe != nil {
		verdict, probed = deps.Probe(ctx, projectID, endpoint)
	}
	if !probed {
		if explicit && explicitKind == TransportSSH && deps.ProbeNow != nil {
			// 指名者付探测费：只有这一次例外，且只在显式指名时。
			verdict, probed = deps.ProbeNow(ctx, projectID, endpoint), true
		} else {
			if explicit && explicitKind == TransportSSH {
				return fail(CodeSSHUnreachable, "",
					"项目 %s 的 SSH 端点 %s 还没有探测结论，无法确认可用性；"+
						"显式指名的 ssh 不降级（先跑一次探针）", projectID, endpoint.Addr())
			}
			local.Reason = ReasonProbeNotRun
			record(local, true, "", "")
			return local, nil
		}
	}

	if verdict.State != TransportProbeOK {
		if explicit && explicitKind == TransportSSH {
			code := transportCodeForProbeClass(verdict.Class)
			return fail(code, verdict.Class,
				"项目 %s 的 SSH 端点 %s 探测失败（分类 %s），显式指名的 ssh 不降级到本地%s",
				projectID, endpoint.Addr(), trimmedOr(verdict.Class, "unknown"), probeDetailSuffix(verdict.Detail))
		}
		local.Reason = ReasonProbeFailed
		local.ProbeClass = verdict.Class
		record(local, true, "", verdict.Class)
		return local, nil
	}

	// ① 探针通 → 建 ssh 一支。
	transport, err := dialTransport(ctx, deps, projectID, endpoint)
	if err != nil {
		code := transportCodeForProbeClass(errProbeClass(err))
		if explicit && explicitKind == TransportSSH {
			return fail(code, errProbeClass(err),
				"项目 %s 的 SSH 端点 %s 建连失败，显式指名的 ssh 不降级到本地: %v",
				projectID, endpoint.Addr(), err)
		}
		local.Reason = ReasonDialFailed
		local.ProbeClass = errProbeClass(err)
		record(local, true, code, local.ProbeClass)
		return local, nil
	}

	selection := TransportSelection{
		Kind: TransportSSH, Transport: transport, ProjectID: projectID,
		Endpoint: endpoint.Addr(), Env: endpoint.Label(), Explicit: explicit, Reason: ReasonProbeOK,
	}
	if explicit {
		selection.Reason = ReasonExplicitSSH
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
