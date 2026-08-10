package core

import (
	"context"
	"errors"
	"strings"
)

// presentedError is the stable, user-facing form of an internal failure. It
// intentionally omits provider status codes, request IDs, and raw transport
// payloads. The original error is retained by the caller for recovery and
// diagnostics, but never copied into a Snapshot, conversation message, or UI
// event.
type presentedError struct {
	module       string
	method       string
	summary      string
	next         string
	unclassified bool
}

func (presentation presentedError) String() string {
	return "【模块：" + presentation.module + "｜方法：" + presentation.method + "】\n" +
		presentation.summary + "\n" + presentation.next
}

func presentUserError(err error) string {
	return classifyPresentedError(err).String()
}

func isUnclassifiedRunChatError(err error) bool {
	return classifyPresentedError(err).unclassified
}

func classifyPresentedError(err error) presentedError {
	message := ""
	if err != nil {
		message = strings.ToLower(err.Error())
	}
	// 会话持久化（slice 8：结构化 code 优先，字符串匹配回退；优先级与旧一致）
	if structuredErrorCode(err) == errorCodePersistenceFailed {
		return persistenceFailurePresentation()
	}
	if strings.Contains(message, "persistence failed and recovery is not guaranteed") || strings.Contains(message, "save atomic session snapshot") {
		return persistenceFailurePresentation()
	}
	if errors.Is(err, context.Canceled) {
		return presentedError{
			module: "任务执行", method: "runChat",
			summary: "任务已停止，尚未形成可交付的最终结果。",
			next:    "已保留当前可恢复的进度；如需继续，请重新发送“继续”。",
		}
	}

	switch {
	case isProviderContextExhaustion(err) || structuredErrorCode(err) == errorCodeContextExceeded:
		return contextExhaustedPresentation()
	case strings.Contains(message, "chat content is empty"):
		return presentedError{
			module: "会话安全", method: "prepareProviderHistory",
			summary: "会话记录中存在缺少可发送文本的条目，本次请求未能安全提交。",
			next:    "当前进度已保存；请发送“继续”重试。",
		}
	case structuredErrorCode(err) == errorCodePlanPreflight ||
		strings.Contains(message, "plan preflight") || strings.Contains(message, "plan_load:") ||
		strings.Contains(message, "plan policy "):
		return planPreflightPresentation()
	case errors.Is(err, ErrReActBudgetExceeded) || structuredErrorCode(err) == errorCodeReActBudget ||
		strings.Contains(message, "react execution budget"):
		return reactBudgetPresentation()
	case strings.Contains(message, "result_ref is not available") ||
		strings.Contains(message, "result_ref is required"):
		// read_tool_result 引用问题：不落 unclassified 兜底，给模型/用户
		// 可行动的指引（省略占位中的 result_ref 原样使用）。
		return resultRefUnavailablePresentation()
	case classifyProviderFailure(err) == providerFailureTimeout:
		return presentedError{
			module: "模型传输", method: "ChatStream",
			summary: "模型服务响应超时，本次工具操作的最终状态可能尚未确定。",
			next:    "当前进度已保存；请确认外部副作用后发送“继续”，系统不会自动重放操作。",
		}
	case classifyProviderFailure(err) == providerFailureServer:
		return presentedError{
			module: "模型传输", method: "ChatStream",
			summary: "模型服务暂时不可用，未能完成本次请求。",
			next:    "当前进度已保存；请稍后发送“继续”恢复任务。",
		}
	default:
		// 无字符串匹配时按结构化定位字段（Struct/Function/Step/Path）回退推断。
		if presentation, ok := classifyStructuredError(err); ok {
			return presentation
		}
		return unclassifiedPresentation()
	}
}

func presentToolError(toolName string, err error) string {
	presentation := classifyPresentedError(err)
	if presentation.module != "代理运行时" {
		return presentation.String()
	}
	return (presentedError{
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
