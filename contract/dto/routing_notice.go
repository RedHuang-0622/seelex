package dto

import (
	"fmt"
	"time"
)

// RoutingNotice 是一次**账号路由事实**的只读投影：本次模型请求实际由哪个账号
// 承接、与请求时的路由意图（固定账号 / provider 过滤）是否一致。
//
// 生态位：账号路由发生在 seelebridge（stream 适配器 + accountpool），消费方
// 也是 seelebridge Runtime：request/failover 事实覆盖 per-session ServedRoute
// 注册表（application 在回合收尾经 RuntimePort.TakeServedRoute 取走并回填
// 真相源）；failover / unpinned_selection 另落 B 类摘要（durable）供审计。
// 不再投会话 system 消息（2026-10-10 用户决定：会话内通知退役）。
//
// 脱敏口径：只带账号名 / provider / model —— 这三者本就是账号池 Metadata 的
// 非敏感路由属性（GUI 账号面板同源）；凭据、错误原文、消息内容一律不进本结构。
type RoutingNotice struct {
	// SessionID 是发起这次模型请求的会话（telemetry session 上下文投影；
	// 空 = 解析不到会话的进程级请求）。
	SessionID string `json:"session_id,omitempty"`
	// NodeID 非空 = 子代理/plan 节点请求；空 = 主会话链路。
	NodeID string `json:"node_id,omitempty"`
	// At 是路由事实发生时间。
	At time.Time `json:"at"`
	// Reason 说明这次投影为什么产生：
	//   - "request"：每次模型请求发出前的派发钩子（前端 message 块元信息条的数据源）；
	//   - "failover"：第一志愿账号发生**账号资格失败**（额度/凭据），请求换号重试；
	//   - "unpinned_selection"：请求未固定账号，由账号池在多账号中选定。
	Reason string `json:"reason"`
	// Role 是发起这次请求的角色（agent / subagent / goalplan）：主链路 = agent，
	// 节点请求取 NodeScope.Role。仅 request 钩子填充。
	Role string `json:"role,omitempty"`
	// From 是换号前的账号（仅 failover 有值）；To 是实际承接的账号。
	From string `json:"from,omitempty"`
	To   string `json:"to"`
	// Model / Provider 是实际承接账号的路由属性（来自账号池 Metadata）。
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`
	// FailureKind 是触发换号的账号资格失败类别（quota_exhausted / auth_rejected；
	// 仅 failover 有值）。
	FailureKind string `json:"failure_kind,omitempty"`
}

// ServedRoute 是一次模型请求的**实际承接路由**：由请求解析中间件（账号池
// completer 的租约路径）在请求发出时得出——本轮 assistant 回应由哪个 config
// role 的哪个 model（含厂商类型）承接。它随 response 回填进 messages 真相源
// （assistant 事件行与可见消息），用于消息归属审计与不同模型的 agent 能力
// 追溯/测试（2026-10-10 用户决定：取代 request.meta 瞬态事件——通知不进会话，
// 元信息直接长在消息上）。
type ServedRoute struct {
	// Role 是承接账号所属的 config 角色（agent / subagent / goalplan）。
	Role string `json:"role"`
	// Account / Provider / Model 是实际承接账号的路由属性（账号池 Metadata）。
	Account  string `json:"account,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// At 是路由事实（请求派发）发生时间。
	At time.Time `json:"at"`
}

// RoutingNotice 的 Reason 取值（唯一一处定义，边界处只引常量不写字面量）。
const (
	// RoutingReasonRequest = 请求派发钩子（每次模型请求发出前一次）。
	RoutingReasonRequest = "request"
	// RoutingReasonFailover = 账号资格失败后的换号重试。
	RoutingReasonFailover = "failover"
	// RoutingReasonUnpinnedSelection = 未固定账号时的池内选定。
	RoutingReasonUnpinnedSelection = "unpinned_selection"
)

// RequestMeta 已随 request.meta 瞬态事件退役（2026-10-10）：路由元信息改为
// 随 response 回填真相源（dto.ServedRoute + 消息行 config_role/config_model/
// config_provider 字段），前端从消息本体渲染，不再消费 request.meta 事件。

// Message 把这条路由事实翻成一句给用户看的提示（B 类摘要 Detail 与文案同源）。
// 只带账号名 / provider / model 这类非敏感路由属性。
func (n RoutingNotice) Message() string {
	label := func(name, provider, model string) string {
		if provider == "" && model == "" {
			return name
		}
		return fmt.Sprintf("%s（%s/%s）", name, provider, model)
	}
	switch n.Reason {
	case RoutingReasonFailover:
		return fmt.Sprintf("⚠ 账号 %s 发生账号资格失败（%s），本次请求已改用 %s 继续",
			n.From, n.failureKindText(), label(n.To, n.Provider, n.Model))
	case RoutingReasonUnpinnedSelection:
		return fmt.Sprintf("ℹ 会话未固定账号，本次请求由账号池选中 %s；如需固定请到账号面板选择",
			label(n.To, n.Provider, n.Model))
	default:
		return fmt.Sprintf("ℹ 本次请求由账号 %s 承接", label(n.To, n.Provider, n.Model))
	}
}

// failureKindText 把账号资格失败类别翻成一句中文（与 backend/engine/account 的
// FailureAdvice 同口径；dto 不引依赖，只认字符串词表）。
func (n RoutingNotice) failureKindText() string {
	switch n.FailureKind {
	case "quota_exhausted":
		return "已无可用额度"
	case "auth_rejected":
		return "凭据被拒"
	default:
		return "不可用"
	}
}
