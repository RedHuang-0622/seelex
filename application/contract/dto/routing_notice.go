package dto

import (
	"fmt"
	"time"
)

// RoutingNotice 是一次**账号路由事实**的只读投影：本次模型请求实际由哪个账号
// 承接、与请求时的路由意图（固定账号 / provider 过滤）是否一致。
//
// 生态位：账号路由发生在 seelebridge（stream 适配器 + accountpool），而"让用户
// 看见"是 application 的职责（Service.AddNotice → system 消息 + message.added
// 事件）。两者靠这条 DTO 经 CSP channel 解耦（与 PlanNodeEvent / TaskRecord
// 同一模式）：runtime 侧非阻塞投递，application 消费者决定呈现方式。
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
	// ReasoningEffort 是承接账号配置的思考强度（仅 request 钩子填充）；
	// `session` 表示跟随会话档位，由 application 消费端解析成当前 wire 值。
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// FailureKind 是触发换号的账号资格失败类别（quota_exhausted / auth_rejected；
	// 仅 failover 有值）。
	FailureKind string `json:"failure_kind,omitempty"`
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

// RequestMeta 是 request.meta 会话事件的载荷：一次模型请求的**瞬态**路由元
// 信息（角色 / 账号 / provider / model / 思考强度 / 时间）。
//
// 生态位与口径（2026-10-09 用户决定）：它由请求发送前的钩子产生，只进会话
// 事件流（GUI 实时渲染），**不写入 messages 真相源**——这类信息为时效性服务，
// 历史消息回放不需要它；将来要做模型对比/评测再考虑持久化。
type RequestMeta struct {
	// Role 是发起这次请求的角色（agent / subagent / goalplan）。
	Role string `json:"role"`
	// Account / Provider / Model 是实际承接请求的账号路由属性。
	Account  string `json:"account,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// ReasoningEffort 是本次请求的思考强度；`session` 已由消费端解析为当前
	// wire 值，这里不会再出现 "session"。
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// At 是请求发出时间（请求发送前钩子的触发时刻）。
	At time.Time `json:"at"`
}

// Message 把这条路由事实翻成一句给用户看的提示（会话 system 消息与 B 类
// 摘要 Detail 共用同一份文案——两处呈现永远同源）。只带账号名 / provider /
// model 这类非敏感路由属性。
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

// failureKindText 把账号资格失败类别翻成一句中文（与 seelebridge/account 的
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
