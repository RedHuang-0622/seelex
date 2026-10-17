// Package viewerrors 是中间层的**错误呈现**落点：把引擎/工具产生的错误分类成
// 稳定的用户可见文本（模块 + 方法 + 摘要 + 下一步），供工具错误出口与视图使用。
//
// 生态位与依赖方向见 README.md；跨层共享的判据（错误码、provider 失败分类、
// ReAct 预算哨兵）在 `contract`，本包只消费它们。
package viewerrors

import (
	seeleerrors "github.com/RedHuang-0622/Seele/errors"
)

// Wrap 给错误附加结构化上下文（seeleerrors.Wrap 语义）：
// 已有结构化信封则原地补字段；否则包装为 seeleerrors.Error。
// 包装后 err.Error() 保留原始消息，字符串匹配仍可兼容（过渡期）。
func Wrap(err error, code string) error {
	return seeleerrors.Wrap(err, seeleerrors.Context{Code: code})
}

// StructuredCode 返回错误链上的结构化错误码（seeleerrors.From）；
// 无结构化错误时返回空串。
func StructuredCode(err error) string {
	structured := seeleerrors.From(err)
	if structured == nil {
		return ""
	}
	return structured.Code
}

// ClassifyStructured 按结构化定位字段（Function/Step/Path）推断分类
// （无稳定 code 时的回退路径，slice 8）。返回 false 表示无结构化信息可读。
func ClassifyStructured(err error) (Presented, bool) {
	structured := seeleerrors.From(err)
	if structured == nil {
		return Presented{}, false
	}
	switch structured.Function {
	case "persistCurrentSession":
		return persistenceFailurePresentation(), true
	}
	if structured.Step != "" || structured.Path != "" {
		return unclassifiedPresentation(), true
	}
	return Presented{}, false
}

func persistenceFailurePresentation() Presented {
	return Presented{
		module: "会话持久化", method: "persistCurrentSession",
		summary: "任务状态未能可靠写入持久化存储。",
		next:    "不要假定进度已经保存；请先检查存储连接或磁盘状态，再决定是否重试。",
	}
}

func reactBudgetPresentation() Presented {
	return Presented{
		module: "执行预算", method: "finalizeReActBudget",
		summary: "本轮已达到执行预算，但未能形成可交付的收尾结果。",
		next:    "请基于已收集的证据要求收尾，或明确下一步要继续调查的方向。",
	}
}

// resultRefUnavailablePresentation 是 read_tool_result 结果引用不可读的
// 用户友好分类（否则落入 unclassified 兜底，模型/用户看不到真实原因）。
func resultRefUnavailablePresentation() Presented {
	return Presented{
		module:  "工具执行",
		method:  "read_tool_result",
		summary: "结果引用不可读：指定的 result_ref 在当前会话或节点现场不存在。",
		next:    "请使用省略占位中给出的 result_ref（或 node:<nodeID>: 前缀）原样读取，不要自行拼接；必要时重新执行产生该结果的工具。",
	}
}

func contextExhaustedPresentation() Presented {
	return Presented{
		module: "上下文恢复", method: "recoverProviderFailure",
		summary: "当前执行上下文超过了模型服务可接收的范围。",
		next:    "当前进度已保存；请发送“继续”恢复任务，必要时可新建会话。",
	}
}

func unclassifiedPresentation() Presented {
	return Presented{
		module: "代理运行时", method: "runChat",
		summary:      "当前任务未能完成。",
		next:         "已保留可用进度；请重试，或补充更明确的下一步。",
		unclassified: true,
	}
}
