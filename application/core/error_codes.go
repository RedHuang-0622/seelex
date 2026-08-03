package core

import (
	seeleerrors "github.com/RedHuang-0622/Seele/errors"
)

// 结构化错误码（slice 8：seeleerrors.From 结构性读取，替代字符串匹配）。
// 错误分类语义与旧字符串匹配一致，但错误链上携带稳定 code，前端/日志
// 可结构化解读（Struct/Function/Step/Path 提供模块定位信息）。
const (
	errorCodePersistenceFailed = "session.persistence_failed"
	errorCodePlanPreflight     = "plan.preflight_failed"
	errorCodeReActBudget       = "react.budget_exhausted"
	errorCodeContextExceeded   = "context.budget_exceeded"
)

// wrapError 给错误附加结构化上下文（seeleerrors.Wrap 语义）：
// 已有结构化信封则原地补字段；否则包装为 seeleerrors.Error。
// 包装后 err.Error() 保留原始消息，字符串匹配仍可兼容（过渡期）。
func wrapError(err error, code string) error {
	return seeleerrors.Wrap(err, seeleerrors.Context{Code: code})
}

// structuredErrorCode 返回错误链上的结构化错误码（seeleerrors.From）；
// 无结构化错误时返回空串。
func structuredErrorCode(err error) string {
	structured := seeleerrors.From(err)
	if structured == nil {
		return ""
	}
	return structured.Code
}

// classifyStructuredError 按结构化定位字段（Function/Step/Path）推断分类
// （无稳定 code 时的回退路径，slice 8）。返回 false 表示无结构化信息可读。
func classifyStructuredError(err error) (presentedError, bool) {
	structured := seeleerrors.From(err)
	if structured == nil {
		return presentedError{}, false
	}
	switch structured.Function {
	case "persistCurrentSession":
		return persistenceFailurePresentation(), true
	case "PreparePlan":
		return planPreflightPresentation(), true
	}
	if structured.Step != "" || structured.Path != "" {
		return unclassifiedPresentation(), true
	}
	return presentedError{}, false
}

func persistenceFailurePresentation() presentedError {
	return presentedError{
		module: "会话持久化", method: "persistCurrentSession",
		summary: "任务状态未能可靠写入持久化存储。",
		next:    "不要假定进度已经保存；请先检查存储连接或磁盘状态，再决定是否重试。",
	}
}

func planPreflightPresentation() presentedError {
	return presentedError{
		module: "计划预检", method: "PreparePlan",
		summary: "当前计划无法生成或通过校验。",
		next:    "请简化目标后重新规划，或改为直接执行该任务。",
	}
}

func reactBudgetPresentation() presentedError {
	return presentedError{
		module: "执行预算", method: "finalizeReActBudget",
		summary: "本轮已达到执行预算，但未能形成可交付的收尾结果。",
		next:    "请基于已收集的证据要求收尾，或明确下一步要继续调查的方向。",
	}
}

func contextExhaustedPresentation() presentedError {
	return presentedError{
		module: "上下文恢复", method: "recoverProviderFailure",
		summary: "当前执行上下文超过了模型服务可接收的范围。",
		next:    "当前进度已保存；请发送“继续”恢复任务，必要时可新建会话。",
	}
}

func unclassifiedPresentation() presentedError {
	return presentedError{
		module: "代理运行时", method: "runChat",
		summary:      "当前任务未能完成。",
		next:         "已保留可用进度；请重试，或补充更明确的下一步。",
		unclassified: true,
	}
}
