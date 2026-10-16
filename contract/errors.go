package contract

import (
	"errors"
	"strings"
)

// 本文件收纳**跨层共享的错误判据与词汇**：引擎产出这些标识，中间层（呈现/投影）
// 与后端（恢复策略）都要按同一份判据分类。放在契约层是因为 plan §1.1 规定
// "前端 → 门面 → 中间层 → 后端 → 底座" 的单向依赖：中间层不得 import 后端，
// 后端不得 import 中间层，唯一都能依赖的是契约层。
//
// 依赖约束：只用 stdlib（不引入 Seele 类型），所以不改变 `contract` 的依赖闭包。

// 结构化错误码（slice 8：seeleerrors.From 结构性读取，替代字符串匹配）。
// 错误分类语义与旧字符串匹配一致，但错误链上携带稳定 code，前端/日志
// 可结构化解读（Struct/Function/Step/Path 提供模块定位信息）。
const (
	ErrorCodePersistenceFailed = "session.persistence_failed"
	ErrorCodePlanPreflight     = "plan.preflight_failed"
	ErrorCodeReActBudget       = "react.budget_exhausted"
	ErrorCodeContextExceeded   = "context.budget_exceeded"
)

// ErrReActBudgetExceeded 标记请求被其捕获的执行预算终止。
//
// 身份唯一：引擎（backend/engine/task）产生它，呈现层（middle/view/errors）
// 与用例断言都按同一个 sentinel 判定——判据只有这一处（AGENTS §8.3）。
var ErrReActBudgetExceeded = errors.New("ReAct execution budget exhausted")

// ProviderFailureKind 是 provider 传输失败的分类（按 provider 措辞判定）。
//
// 消费方：中间层呈现层（选用户可见文案）与后端恢复策略（决定是否写有界
// 检查点、能否安全重放一次）。两者不得各写一份分类。
type ProviderFailureKind string

const (
	ProviderFailureNone    ProviderFailureKind = ""
	ProviderFailureContext ProviderFailureKind = "context_exhausted"
	ProviderFailureHistory ProviderFailureKind = "invalid_history"
	ProviderFailureTimeout ProviderFailureKind = "timeout"
	ProviderFailureServer  ProviderFailureKind = "server_unavailable"
)

// IsProviderContextExhaustion 判定 provider 是否以"上下文窗口超出"为由拒绝请求。
func IsProviderContextExhaustion(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "context window exceeds") ||
		strings.Contains(message, "context length") ||
		strings.Contains(message, "maximum context") ||
		strings.Contains(message, "上下文窗口")
}

// ClassifyProviderFailure 按 provider 措辞把传输失败归到具体类别；无法归类返回
// ProviderFailureNone。contextExhausted 由 IsProviderContextExhaustion 判定，
// 两条判据只此一份。
func ClassifyProviderFailure(err error) ProviderFailureKind {
	if IsProviderContextExhaustion(err) {
		return ProviderFailureContext
	}
	if err == nil {
		return ProviderFailureNone
	}
	message := strings.ToLower(err.Error())
	// tool 配对协议的两种措辞都算"记录不合法"：孤儿/乱序 tool 结果（2026-09-20
	// 现场）与 assistant 宣告的调用没拿到足够回执（"insufficient tool messages
	// following tool_calls message"，2026-09-24 探针 P4 实测、现场复现）。
	if strings.Contains(message, "chat content is empty") ||
		strings.Contains(message, "must be a response to a preceding message with") ||
		strings.Contains(message, "must be followed by tool messages") {
		// 后两者 = provider 拒绝 tool 配对协议（孤儿/乱序 tool 结果，2026-09-20
		// 现场）：同属"请求里的记录不合法"，走有界检查点的历史恢复，而不是把
		// 会话循环判死。wire 出口已按协议剔除这类形状（seelexctx 装配器），
		// 这里保留兜底——provider 的措辞与严格度不由我们决定。
		return ProviderFailureHistory
	}
	if strings.Contains(message, "http 504") || strings.Contains(message, "timeout_error") ||
		strings.Contains(message, "upstream timeout") || strings.Contains(message, "request timeout") {
		return ProviderFailureTimeout
	}
	if strings.Contains(message, "http 500") || strings.Contains(message, "http 502") ||
		strings.Contains(message, "http 503") || strings.Contains(message, "http 529") ||
		strings.Contains(message, "server_error") || strings.Contains(message, "internal server error") {
		return ProviderFailureServer
	}
	return ProviderFailureNone
}
