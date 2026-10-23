package registry

// transport_decision.go —— "这次调用走哪个执行面"的**决策前移**（链序：选择拆两段）。
//
// 为什么要把一次"选择"拆成两段：
//
//	旧链序：事件 → 路径门 → 权限门 → 选择（探针 + **建连**）→ 诊断 → handler
//	新链序：事件 → **执行面决策（只读）** → 路径门 → 权限门 → **建连（迟绑定）** → 诊断 → handler
//
// 旧链序的病：路径门按**本地**项目根判包含关系，而"这次到底走不走远端"要等权限门
// 之后才知道。于是"项目配了连接 + 探针通 + 调用方未指名"这种**自动选中 ssh** 的调用，
// 远端绝对路径（例如 /etc/nginx/nginx.conf）会被本地根判成越界直接拒掉——旧口径只对
// **显式指名 ssh** 豁免，自动选中那一格至今是误拒。
//
// 拆开的两条硬约束（各自都不可省）：
//
//  1. **决策只读**：决策段跑在权限门之前，被拒的调用也会经过它。所以这段只读项目
//     连接配置与**探针缓存**，绝不建连、不发网络包（未探 ≠ 通，照旧降级 local 并
//     留痕）。显式指名者的"探测费"（ProbeNow）推到建连段付，权限拒绝因此仍是
//     **零探针零建连**。
//  2. **建连迟绑定**：真正的拨号/握手仍在权限门之后——被拒的调用不白连一次远端；
//     显式指名而不可用的报错也留在建连段，被拒的调用照旧先看到权限拒绝，不被一条
//     "连不上"顶掉。
//
// 决策是 **ctx 上的一个事实**（WithTransportDecision）：路径门按它取根
// （path_gate_face.go），建连段按它落地（transport_selector.go 的 bindTransportSelection）。
// 两段都不读对方的内部状态，只共享这一个值。

import (
	"context"
	"fmt"
	"strings"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
)

// TransportDecision 是"本次调用走哪个执行面"的结论：决策段的产出，路径门与建连段的输入。
//
// 两个维度必须分开（合成一个格子就会再次出现"说走远端其实跑本地"）：
//   - **命名空间在谁那边**（Kind == ssh ⇒ 路径是对端路径）；
//   - **这次能不能真的连上**（Feasible）。
//
// 显式指名 ssh 而端点不可用时，Kind 仍是 ssh（路径属对端命名空间，不是本地路径），
// 只是 Feasible=false 且 FailureCode 有值——真正的报错留给建连段。
type TransportDecision struct {
	// Decided 报告这一段结论是否存在（false = 没装决策段，调用方按旧口径处理）。
	Decided bool
	// Kind 是这次的执行面：ssh = 远端命名空间；local = 本地面（含所有降级）。
	Kind TransportKind
	// ProjectID 是这次调用归属的项目（工作区 ID）；空 = 没有项目归属。
	ProjectID string
	// Env 是调用方指名的**环境名**（工具参数 env）；空 = 没指名（走项目默认/单条连接）。
	//
	// 它只记录"指名了什么"，不代表解析成功：解析不到时 Kind 仍是 ssh、Feasible=false，
	// 失败码 SSH_ENV_UNKNOWN（报错留给建连段，与显式指名 ssh 的口径一致）。
	Env string
	// Endpoint 是项目连接配置里的端点（含 RemoteRoot：远端项目根，未知时为空）。
	Endpoint SSHEndpoint
	// Configured 报告项目是否真的配了 SSH 端点（Endpoint 有值 ≠ 配了）。
	Configured bool
	// Explicit 报告调用方是否显式指名了传输。
	Explicit bool
	// Reason 是这次决策的理由（取值见 transport.go 的 transportReasons）。
	Reason string
	// Degraded 表示"本可以/本应走远端，但决策就落在本地"（未探 / 探针挂）——留痕用。
	Degraded bool
	// ProbeState / ProbeClass / ProbeDetail 是**缓存**里的探测结论（未探时为空）。
	ProbeState  TransportProbeState
	ProbeClass  string
	ProbeDetail string
	// Feasible 报告"能不能真的连上"：探针通 = true；显式指名但未探 = false（探测费
	// 在建连段付，付完再定）。
	Feasible bool
	// NeedsProbe 表示"意图是远端、但还没有探测结论"：只有显式指名会走到这里，建连段
	// 因此替指名者付一次探测费。
	NeedsProbe bool
	// FailureCode / FailureMsg 是显式指名不可用的固定码与文案（决策段不报错）。
	FailureCode string
	FailureMsg  string
}

// Remote 报告这次的路径命名空间在远端（= 走过 ssh 那一支，或调用方指名了 ssh）。
func (d TransportDecision) Remote() bool { return d.Kind == TransportSSH }

type transportDecisionKey struct{}

// WithTransportDecision 把一次调用的执行面决策写进 ctx。
func WithTransportDecision(ctx context.Context, decision TransportDecision) context.Context {
	return context.WithValue(ctx, transportDecisionKey{}, decision)
}

// TransportDecisionFromContext 读回这次调用的执行面决策（诊断/路径门/建连段共用）。
func TransportDecisionFromContext(ctx context.Context) (TransportDecision, bool) {
	if ctx == nil {
		return TransportDecision{}, false
	}
	decision, ok := ctx.Value(transportDecisionKey{}).(TransportDecision)
	if !ok || !decision.Decided {
		return TransportDecision{}, false
	}
	return decision, true
}

// ── 两段的自定位标记 ──────────────────────────────────────────────
//
// 同一份传输中间件被装在两处（决策段在路径门之前，建连段在权限门之后），靠 ctx 上
// 这个标记区分"本次是哪一段"。默认（没有标记）= 建连段 = 旧行为：直接拿到中间件
// 调用（用例、第三方装配）时它自己算决策再建连，一次调用一个结论。

type transportStageKey struct{}

type transportStage int

const (
	// transportStageDial 是建连段（缺省）：算（或复用）决策 → 落地 → 写 selection。
	transportStageDial transportStage = iota
	// transportStageDecision 是决策段：只算决策、写进 ctx、转发，绝不建连。
	transportStageDecision
)

func withTransportStage(ctx context.Context, stage transportStage) context.Context {
	return context.WithValue(ctx, transportStageKey{}, stage)
}

func transportStageFromContext(ctx context.Context) transportStage {
	if ctx == nil {
		return transportStageDial
	}
	if stage, ok := ctx.Value(transportStageKey{}).(transportStage); ok {
		return stage
	}
	return transportStageDial
}

// TransportDecisionStage 把传输中间件包装成**决策段**（装在路径门之前）。
//
// nil → nil：没有传输中间件时链上逐字等于旧顺序（不多一层空转）。
// 只打标记不做事：决策内容仍由中间件自己按 deps 算（一份判据）。
func TransportDecisionStage(middleware frameworktools.MetaMiddleware) frameworktools.MetaMiddleware {
	if middleware == nil {
		return nil
	}
	return func(name string, meta frameworktools.ToolMeta, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		inner := middleware(name, meta, next)
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			return inner.Execute(withTransportStage(ctx, transportStageDecision), argsJSON)
		})
	}
}

// TransportDialStage 把传输中间件包装成**建连段**（权限门之后、诊断之前）。
//
// 必须显式打标记：决策段的标记随 ctx 往下游走，不清掉的话建连段会把自己当成决策段，
// 于是"选中了 ssh"永远落不了地（选而不连 = 静默跑本地）。
func TransportDialStage(middleware frameworktools.MetaMiddleware) frameworktools.MetaMiddleware {
	if middleware == nil {
		return nil
	}
	return func(name string, meta frameworktools.ToolMeta, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		inner := middleware(name, meta, next)
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			return inner.Execute(withTransportStage(ctx, transportStageDial), argsJSON)
		})
	}
}

// resolveTransportDecision 是"这次走哪个面"的**唯一判据**（顺序即优先级，勿重排）。
//
// 四格加上显式指名，与旧 resolveTransportSelection 的判定逐格等价，区别只有一处：
// 这里**不建连、不探、不报错**（后果都在 bindTransportSelection 里落地）。
func resolveTransportDecision(
	ctx context.Context,
	deps TransportSelectorDeps,
	toolName string,
	explicit bool,
	explicitKind TransportKind,
	env string,
) TransportDecision {
	sessionID := ""
	if deps.SessionKey != nil {
		sessionID = deps.SessionKey(ctx)
	}
	projectID := ""
	if deps.Project != nil {
		projectID = deps.Project(ctx, sessionID)
	}
	// 环境名的优先级：**显式参数 env > 会话当前环境 > 都没有**。
	//
	// 会话那一档只提供"缺省"，不覆盖显式值：调用方写了 env 就以它为准（否则
	// 一次性的环境切换会被会话状态悄悄改回去，用户看到的是"我明明指名了却连到
	// 另一个环境"）。会话当前环境本身已在写入侧校验过 ⊆ 放权集合 ⊆ 项目环境表，
	// 这里不再重复判定，只做取值。
	if env == "" && deps.EnvFor != nil {
		env = strings.TrimSpace(deps.EnvFor(ctx, sessionID))
	}
	decision := TransportDecision{
		Decided: true, Kind: TransportLocal, ProjectID: projectID,
		Explicit: explicit, Reason: ReasonUnconfigured,
	}
	// 显式指名 ssh 却不可用：只记事实（码 + 文案），报错留给建连段。
	unavailable := func(code, class, format string, args ...any) TransportDecision {
		decision.Kind = TransportSSH
		decision.Reason = ReasonExplicitSSH
		decision.Feasible = false
		decision.FailureCode = code
		decision.ProbeClass = class
		decision.FailureMsg = fmt.Sprintf(format, args...)
		return decision
	}
	sshIntent := explicit && explicitKind == TransportSSH

	// ④ 显式指名 local：尊重调用方，不去连远端，也不记成降级。
	if explicit && explicitKind == TransportLocal {
		decision.Reason = ReasonExplicitLocal
		return decision
	}

	// 装配级开关：宿主没有 SSH 能力。
	if !deps.Enabled {
		if sshIntent {
			return unavailable(CodeSSHEnvDisabled, "",
				"这台宿主上 SSH 传输未启用，调用方显式指名的 %s 无法执行（不降级到本地）", toolName)
		}
		decision.Reason = ReasonEnvDisabled
		return decision
	}

	// 环境放权：指名了环境（或会话有缺省环境）就必须在本会话**被放权过**。
	//
	// 为什么这里还要判一次：写入侧已保证 CurrentEnv ⊆ AllowedEnvs ⊆ 项目环境表，
	// 但**显式 env 参数**是绕过会话状态的一条直路——不在这一步卡住，任何能写工具
	// 参数的调用方都能把调用指到未经放权的环境上，"缺省不放权"就成了摆设。
	// 放权面未装配时同样拒绝（fail-closed）：环境是可选能力，装不上就当没有。
	decision.Env = env
	if env != "" {
		if deps.EnvGranted == nil {
			return unavailable(CodeSSHEnvNotGranted, "",
				"环境 %q 需要会话放权，但放权面未装配：拒绝（缺省不放权）", env)
		}
		if !deps.EnvGranted(ctx, sessionID, env) {
			return unavailable(CodeSSHEnvNotGranted, "",
				"环境 %q 没有在本会话放权（先放权再使用；缺省不放权）", env)
		}
	}

	// 项目连接配置：未配 = 没有远端可去（读面永不返回口令）。
	var endpoint SSHEndpoint
	configured := false
	// 显式给的环境名（工具参数 env）：查项目环境表。查不到 = 显式失败，**不回落
	// 默认环境**——回落会静默连到另一台机器上，那是这类设计最贵的一种错。
	readEndpoint := deps.Endpoint
	if env != "" {
		if deps.EndpointFor == nil {
			if sshIntent {
				return unavailable(CodeSSHEnvUnknown, "", "项目环境表未装配，指名环境 %q 的调用无法解析端点", env)
			}
		} else {
			readEndpoint = func(pid string) (SSHEndpoint, bool, error) { return deps.EndpointFor(pid, env) }
		}
	}
	if readEndpoint != nil && projectID != "" {
		read, ok, err := readEndpoint(projectID)
		if err != nil {
			if sshIntent {
				return unavailable(CodeSSHUnreachable, "",
					"读取项目 %s 的连接配置失败，显式指名的 ssh 无法确认可用性: %v", projectID, err)
			}
			decision.Reason = ReasonUnconfigured
			return decision
		}
		endpoint, configured = read, ok && read.Configured()
	}
	if !configured && env != "" {
		return unavailable(CodeSSHEnvUnknown, "",
			"环境 %q 不在项目 %s 的环境表里（或在表里但没填 host）：不回落默认环境，避免连错机器",
			env, trimmedOr(projectID, "(未绑定项目)"))
	}
	if !configured {
		if sshIntent {
			return unavailable(CodeSSHUnreachable, "",
				"项目 %s 没有配置 SSH 连接，显式指名的 ssh 无法执行（不降级到本地；"+
					"先在项目设置里填 SSH 端点，或显式指名 transport=local）", trimmedOr(projectID, "(未绑定项目)"))
		}
		decision.Reason = ReasonUnconfigured
		return decision
	}
	decision.Endpoint, decision.Configured = endpoint, true

	// 探测结论：只读**缓存**。未探是"不知道"，不是"通"——决策段不替人探。
	verdict, probed := TransportProbeVerdict{}, false
	if deps.Probe != nil {
		verdict, probed = deps.Probe(ctx, projectID, endpoint)
	}
	if probed {
		decision.ProbeState, decision.ProbeClass, decision.ProbeDetail = verdict.State, verdict.Class, verdict.Detail
	}
	if !probed {
		if sshIntent {
			// 指名者付探测费：这一次例外推迟到建连段付（决策段不发网络包）。
			decision.Kind = TransportSSH
			decision.Reason = ReasonExplicitSSH
			decision.NeedsProbe = true
			return decision
		}
		decision.Reason = ReasonProbeNotRun
		decision.Degraded = true
		return decision
	}

	if verdict.State != TransportProbeOK {
		if sshIntent {
			return unavailable(transportCodeForProbeClass(verdict.Class), verdict.Class,
				"项目 %s 的 SSH 端点 %s 探测失败（分类 %s），显式指名的 ssh 不降级到本地%s",
				projectID, endpoint.Addr(), trimmedOr(verdict.Class, "unknown"), probeDetailSuffix(verdict.Detail))
		}
		decision.Reason = ReasonProbeFailed
		decision.Degraded = true
		decision.ProbeClass = verdict.Class
		return decision
	}

	// ① 探针通 → 意图 ssh（真正的建连在建连段，权限门之后）。
	decision.Kind = TransportSSH
	decision.Feasible = true
	decision.Reason = ReasonProbeOK
	if sshIntent {
		decision.Reason = ReasonExplicitSSH
	}
	return decision
}
