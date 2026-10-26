package errorpresentation

import (
	"context"
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
)

// Presented is the stable, user-facing form of a tool failure. It
// intentionally omits provider status codes, request IDs, and raw transport
// payloads. The original error is retained by the caller for recovery and
// diagnostics.
//
// 适用范围：只有工具错误出口（PresentToolError → ToolCall.Error 呈现文本、
// wire 上的 {"error": ...} 合成）走这套改写；runChat 的 chat 报错不再经过
// 本层——chat.Error / 会话 error 行 / EventError 直接是原始 err 正文，便于
// 现场诊断（原始错误只落在视图/事件侧，不会进 provider 上下文：
// task 域 transcriptProtocolUnits 只收 user/assistant/tool）。
type Presented struct {
	module       string
	method       string
	summary      string
	next         string
	unclassified bool
}

func (presentation Presented) String() string {
	return "【模块：" + presentation.module + "｜方法：" + presentation.method + "】\n" +
		presentation.summary + "\n" + presentation.next
}

// IsUnclassified 判定错误是否落到兜底分类（调用方据此决定是否用原始正文）。
func IsUnclassified(err error) bool {
	return Classify(err).unclassified
}

// Classify 把错误分类成用户可见的呈现文本。判据顺序即优先级：
// 结构化 code 优先、字符串匹配回退（与迁移前逐条一致）。
func Classify(err error) Presented {
	message := ""
	if err != nil {
		message = strings.ToLower(err.Error())
	}
	// 会话持久化（slice 8：结构化 code 优先，字符串匹配回退；优先级与旧一致）
	if StructuredCode(err) == contract.ErrorCodePersistenceFailed {
		return persistenceFailurePresentation()
	}
	if strings.Contains(message, "persistence failed and recovery is not guaranteed") || strings.Contains(message, "save atomic session snapshot") {
		return persistenceFailurePresentation()
	}
	if errors.Is(err, context.Canceled) {
		return Presented{
			module: "任务执行", method: "runChat",
			summary: "任务已停止，尚未形成可交付的最终结果。",
			next:    "已保留当前可恢复的进度；如需继续，请重新发送“继续”。",
		}
	}

	switch {
	case contract.IsProviderContextExhaustion(err) || StructuredCode(err) == contract.ErrorCodeContextExceeded:
		return contextExhaustedPresentation()
	case strings.Contains(message, "chat content is empty"):
		return Presented{
			module: "会话安全", method: "prepareProviderHistory",
			summary: "会话记录中存在缺少可发送文本的条目，本次请求未能安全提交。",
			next:    "当前进度已保存；请发送“继续”重试。",
		}
	case errors.Is(err, contract.ErrReActBudgetExceeded) || StructuredCode(err) == contract.ErrorCodeReActBudget ||
		strings.Contains(message, "react execution budget"):
		return reactBudgetPresentation()
	case strings.Contains(message, "result_ref is not available") ||
		strings.Contains(message, "result_ref is required"):
		// read_tool_result 引用问题：不落 unclassified 兜底，给模型/用户
		// 可行动的指引（省略占位中的 result_ref 原样使用）。
		return resultRefUnavailablePresentation()
	case contract.ClassifyProviderFailure(err) == contract.ProviderFailureTimeout:
		return Presented{
			module: "模型传输", method: "ChatStream",
			summary: "模型服务响应超时，本次工具操作的最终状态可能尚未确定。",
			next:    "当前进度已保存；请确认外部副作用后发送“继续”，系统不会自动重放操作。",
		}
	case contract.ClassifyProviderFailure(err) == contract.ProviderFailureServer:
		return Presented{
			module: "模型传输", method: "ChatStream",
			summary: "模型服务暂时不可用，未能完成本次请求。",
			next:    "当前进度已保存；请稍后发送“继续”恢复任务。",
		}
	default:
		// 无字符串匹配时按结构化定位字段（Struct/Function/Step/Path）回退推断。
		if presentation, ok := ClassifyStructured(err); ok {
			return presentation
		}
		return unclassifiedPresentation()
	}
}

// PresentToolError 把工具错误呈现为 provider 可见文本（工具错误唯一出口）。
func PresentToolError(toolName string, err error) string {
	presentation := Classify(err)
	if presentation.module != "代理运行时" {
		return presentation.String()
	}
	return (Presented{
		module:  "工具执行",
		method:  "handleToolComplete(" + safeToolName(toolName) + ")",
		summary: "该工具未能完成本次操作。",
		next:    "当前进度已保留；请检查任务条件后重试或调整下一步。",
	}).String()
}

func safeToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
}
