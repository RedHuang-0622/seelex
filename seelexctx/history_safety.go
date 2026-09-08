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

// interruptedToolResultPrefix 是合成 tool 占位结果正文的前缀（见
// repairInterruptedToolChains）：标识非真实工具输出、provider-only。
const interruptedToolResultPrefix = "[Seelex recovery note: interrupted tool call"

func interruptedToolResultContent(name string) string {
	return interruptedToolResultPrefix + " \"" + name + "\" may not have executed; its result was lost before recording. Verify side effects or re-issue the call before continuing.]"
}

// PrepareReplaceHistory 在 ContextDecision.ReplaceHistory 生效前执行：
// 清理上下文控制块（checkpoint/旧压缩帧），再修复中断工具链缺失结果与
// 空正文（配对规则）。返回修复后的历史（不修改入参）。
func PrepareReplaceHistory(history []types.Message) []types.Message {
	filtered := removeContextMarkers(history)
	return repairEmptyHistoryContent(repairInterruptedToolChains(filtered))
}

// repairInterruptedToolChains 补齐中断（残缺）工具链缺失的 tool 结果：
// 对每条 assistant+tool_calls，仅当其后文再无该调用 ID 的结果时，在链
// 断裂点插入合成 tool 占位消息，保证替换后历史 assistant/tool 配对合法
// （幂等；协议占位 provider-only，不渲染为真实输出）。
func repairInterruptedToolChains(history []types.Message) []types.Message {
	prepared := make([]types.Message, 0, len(history)+2)
	for index := 0; index < len(history); {
		message := history[index]
		prepared = append(prepared, message)
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			index++
			continue
		}
		wanted := make([]string, 0, len(message.ToolCalls))
		names := make(map[string]string, len(message.ToolCalls))
		wantedSet := make(map[string]struct{}, len(message.ToolCalls))
		valid := true
		for _, call := range message.ToolCalls {
			if call.ID == "" {
				valid = false // 空 ID 无法配对；链保持原样
				break
			}
			if _, duplicate := wantedSet[call.ID]; duplicate {
				valid = false // 重复 ID 无法配对；链保持原样
				break
			}
			wantedSet[call.ID] = struct{}{}
			wanted = append(wanted, call.ID)
			names[call.ID] = call.Function.Name
		}
		if !valid {
			index++
			continue
		}
		matched := make(map[string]struct{}, len(wanted))
		next := index + 1
		for next < len(history) && len(matched) < len(wanted) {
			result := history[next]
			if result.Role != "tool" {
				break
			}
			if _, ok := wantedSet[result.ToolCallID]; !ok {
				break
			}
			if _, duplicate := matched[result.ToolCallID]; duplicate {
				break
			}
			matched[result.ToolCallID] = struct{}{}
			prepared = append(prepared, result)
			next++
		}
		index = next
		if len(matched) == len(wanted) {
			continue
		}
		for _, id := range wanted {
			if _, ok := matched[id]; ok {
				continue
			}
			if toolResultExistsLater(history, index, id) {
				continue
			}
			content := interruptedToolResultContent(names[id])
			prepared = append(prepared, types.Message{
				Role: "tool", ToolCallID: id, Name: names[id], Content: &content,
			})
		}
	}
	return prepared
}

// toolResultExistsLater 报告指定 tool 调用 ID 的结果是否出现在历史后文。
func toolResultExistsLater(history []types.Message, start int, id string) bool {
	for index := start; index < len(history); index++ {
		if history[index].Role == "tool" && history[index].ToolCallID == id {
			return true
		}
	}
	return false
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
	return content == missingHistoryContent || content == toolCallHistoryContent ||
		strings.HasPrefix(content, interruptedToolResultPrefix)
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
