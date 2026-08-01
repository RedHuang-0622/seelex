// history_safety.go — ReplaceHistory 前的历史安全规则（迁移自
// application/core/history_safety.go 的合法配对规则，语义保持一致）：
//
//  1. checkpoint/压缩帧标记清理：应用侧控制块（任务检查点、旧压缩帧）
//     不进入替换后的历史；
//  2. 空正文配对修复：assistant 带 tool_calls 保留协议、assistant 仅
//     推理正文保留协议，其余空正文消息补缺失标记（provider 拒绝空
//     content 字段）。
package seelexctx

import (
	"strings"

	"github.com/RedHuang-0622/Seele/types"
)

// missingHistoryContent 是空正文中断恢复的配对修复文本。
const missingHistoryContent = "[Seelex recovery note: the previous message had no text after an interrupted request; its original content is unavailable.]"

// toolCallHistoryContent 是 assistant+tool_calls 缺正文的配对修复文本。
const toolCallHistoryContent = "[Seelex recovery note: the assistant issued the recorded tool call(s); the original accompanying text is unavailable.]"

// PrepareReplaceHistory 在 ContextDecision.ReplaceHistory 生效前执行：
// 清理上下文控制块（checkpoint/旧压缩帧），再按配对规则修复空正文。
// 返回修复后的历史（不修改入参）。
func PrepareReplaceHistory(history []types.Message) []types.Message {
	filtered := removeContextMarkers(history)
	return repairEmptyHistoryContent(filtered)
}

// removeContextMarkers 移除 user 角色且以 checkpoint 或压缩帧标记开头的
// 控制消息（应用侧专用，不得作为对话内容渲染/持久化）。
func removeContextMarkers(history []types.Message) []types.Message {
	filtered := make([]types.Message, 0, len(history))
	for _, message := range history {
		if message.Role == "user" && message.Content != nil &&
			(strings.HasPrefix(*message.Content, checkpointMarker) ||
				strings.HasPrefix(*message.Content, compactContextMarker)) {
			continue
		}
		filtered = append(filtered, message)
	}
	return filtered
}

// repairEmptyHistoryContent 按配对规则修复空正文消息。
func repairEmptyHistoryContent(history []types.Message) []types.Message {
	prepared := cloneMessageSlice(history)
	for index := range prepared {
		message := &prepared[index]
		if message.Content != nil && strings.TrimSpace(*message.Content) != "" {
			continue
		}
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			content := toolCallHistoryContent
			message.Content = &content
			continue
		}
		if message.Role == "assistant" && message.ReasoningContent != "" {
			content := missingHistoryContent
			message.Content = &content
			continue
		}
		if message.Role == "system" || message.Role == "user" || message.Role == "assistant" || message.Role == "tool" {
			content := missingHistoryContent
			message.Content = &content
		}
	}
	return prepared
}

// IsProviderOnlyHistoryContent 识别仅用于满足 provider 非空正文的修复文本，
// 不得渲染为助手回复（与 application 侧语义一致）。
func IsProviderOnlyHistoryContent(content string) bool {
	return content == missingHistoryContent || content == toolCallHistoryContent
}

func cloneMessageSlice(messages []types.Message) []types.Message {
	out := make([]types.Message, len(messages))
	copy(out, messages)
	for index := range out {
		if messages[index].Content != nil {
			value := *messages[index].Content
			out[index].Content = &value
		}
		if messages[index].ToolCalls != nil {
			out[index].ToolCalls = append([]types.ToolCall(nil), messages[index].ToolCalls...)
		}
	}
	return out
}
