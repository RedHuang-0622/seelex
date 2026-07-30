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
	module  string
	method  string
	summary string
	next    string
}

func (presentation presentedError) String() string {
	return "【模块：" + presentation.module + "｜方法：" + presentation.method + "】\n" +
		presentation.summary + "\n" + presentation.next
}

func presentUserError(err error) string {
	return classifyPresentedError(err).String()
}

func classifyPresentedError(err error) presentedError {
	if errors.Is(err, context.Canceled) {
		return presentedError{
			module: "任务执行", method: "runChat",
			summary: "任务已停止，尚未形成可交付的最终结果。",
			next:    "已保留当前可恢复的进度；如需继续，请重新发送“继续”。",
		}
	}

	message := ""
	if err != nil {
		message = strings.ToLower(err.Error())
	}
	switch {
	case isProviderContextExhaustion(err):
		return presentedError{
			module: "上下文恢复", method: "recoverProviderFailure",
			summary: "当前执行上下文超过了模型服务可接收的范围。",
			next:    "当前进度已保存；请发送“继续”恢复任务，必要时可新建会话。",
		}
	case strings.Contains(message, "chat content is empty"):
		return presentedError{
			module: "会话安全", method: "prepareProviderHistory",
			summary: "会话记录中存在缺少可发送文本的条目，本次请求未能安全提交。",
			next:    "当前进度已保存；请发送“继续”重试。",
		}
	case strings.Contains(message, "plan preflight") || strings.Contains(message, "plan_load:") ||
		strings.Contains(message, "plan policy "):
		return presentedError{
			module: "计划预检", method: "PreparePlan",
			summary: "当前计划无法生成或通过校验。",
			next:    "请简化目标后重新规划，或改为直接执行该任务。",
		}
	case errors.Is(err, ErrReActBudgetExceeded) || strings.Contains(message, "react execution budget"):
		return presentedError{
			module: "执行预算", method: "finalizeReActBudget",
			summary: "本轮已达到执行预算，但未能形成可交付的收尾结果。",
			next:    "请基于已收集的证据要求收尾，或明确下一步要继续调查的方向。",
		}
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
		return presentedError{
			module: "代理运行时", method: "runChat",
			summary: "当前任务未能完成。",
			next:    "已保留可用进度；请重试，或补充更明确的下一步。",
		}
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
