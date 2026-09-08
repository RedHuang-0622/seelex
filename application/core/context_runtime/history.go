package context_runtime

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
)

// MissingHistoryContent 是空 content 修复文本（provider 非空要求）。
const MissingHistoryContent = "[Seelex recovery note: the previous message had no text after an interrupted request; its original content is unavailable.]"

// ToolCallHistoryContent 是 assistant 工具调用缺正文的修复文本。
const ToolCallHistoryContent = "[Seelex recovery note: the assistant issued the recorded tool call(s); the original accompanying text is unavailable.]"

// InterruptedToolResultPrefix 是合成 tool 结果正文的前缀（见
// RepairInterruptedToolChains）：标识该结果不是真实工具输出，而是对
// 中断工具调用的协议占位（UI 不渲染、不得被当作成功证据）。
const InterruptedToolResultPrefix = "[Seelex recovery note: interrupted tool call"

// interruptedToolResultContent 生成缺失 tool 结果的协议占位正文：明示该
// 调用在记录到结果前被中断、是否已执行未知 —— 不谎称执行成功，模型据此
// 校验副作用或重新发起。
func interruptedToolResultContent(name string) string {
	return InterruptedToolResultPrefix + " \"" + name + "\" may not have executed; its result was lost before recording. Verify side effects or re-issue the call before continuing.]"
}

// HistoryCoordinator 拥有 provider 缓存归一化（空内容修复 + 中断工具链
// 配对修复）。
type HistoryCoordinator struct {
	*state.Core
}

// NewHistoryCoordinator 构造 history 域协调器。
func NewHistoryCoordinator(core *state.Core) *HistoryCoordinator {
	return &HistoryCoordinator{Core: core}
}

// PrepareProviderHistory 使每条持久化消息对拒绝空 content 的 provider 安全
// （工具调用保留，仅恢复缺失的说明文本；活跃会话兼容包装）。
func (h *HistoryCoordinator) PrepareProviderHistory() error {
	return h.PrepareProviderHistoryFor(h.Deps.Engine.SessionID())
}

// PrepareProviderHistoryFor 使每条持久化消息对拒绝空 content 的 provider
// 安全：先补齐中断（残缺）工具链缺失的 tool 结果（协议占位），再恢复空
// 正文。sessionID 指明目标会话。
func (h *HistoryCoordinator) PrepareProviderHistoryFor(sessionID string) error {
	history := h.engineHistory(sessionID)
	prepared, repaired := RepairInterruptedToolChains(history)
	if repaired {
		if err := h.replaceEngineHistory(sessionID, prepared); err != nil {
			return fmt.Errorf("repair interrupted tool chains: %w", err)
		}
	}
	final, repairedContent := RepairEmptyHistoryContent(prepared)
	if repairedContent {
		if err := h.replaceEngineHistory(sessionID, final); err != nil {
			return fmt.Errorf("repair empty provider history content: %w", err)
		}
	}
	return nil
}

// PrepareNewHistoryContentFor 仅修复引擎历史中新 append 的空正文消息
// （OnIterationComplete 中间态专用）：新 assistant/tool 记录的工具结果可能
// 尚未写入（活跃 ReAct 中），残缺链的配对修复留到装配/请求前由
// PrepareProviderHistoryFor 执行 —— 避免把"即将执行"的工具调用误判为
// 中断丢失而注入占位。
func (h *HistoryCoordinator) PrepareNewHistoryContentFor(sessionID string) error {
	history := h.engineHistory(sessionID)
	prepared, repaired := RepairEmptyHistoryContent(history)
	if !repaired {
		return nil
	}
	return h.replaceEngineHistory(sessionID, prepared)
}

// replaceEngineHistory 会话内替换指定会话引擎历史（会话路由引擎用
// ReplaceHistoryFor，不切活跃；否则回退契约 ReplaceHistory）。
func (h *HistoryCoordinator) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error {
	if routed, ok := h.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.ReplaceHistoryFor(sessionID, history)
	}
	return h.Deps.Engine.ReplaceHistory(sessionID, history)
}

// engineHistory 返回指定会话引擎历史（会话路由引擎用 HistoryFor，否则活跃
// 引擎）。
func (h *HistoryCoordinator) engineHistory(sessionID string) []contract.EngineMessage {
	if routed, ok := h.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.HistoryFor(sessionID)
	}
	return h.Deps.Engine.History()
}

// RepairInterruptedToolChains 修复中断（残缺）工具链：assistant 消息携带
// tool_calls，但其后只记录了部分（或没有）tool 结果 —— 会话中断/重启导致
// 结果丢失。对每个缺失调用 ID，若其后文再没有该 ID 的 tool 结果，就在链
// 断裂点（紧邻结果的最后一个之后、第一个非 tool 消息之前）插入合成 tool
// 占位消息，使发送给 provider 的序列保持 assistant/tool 配对合法。
//
// 幂等：已补齐的链再次执行不会重复插入；后文存在同 ID 结果时不插入（保守，
// 避免破坏既有配对）。合成正文带 InterruptedToolResultPrefix，属于
// provider-only，不渲染为真实工具输出。不修改入参。
func RepairInterruptedToolChains(history []contract.EngineMessage) ([]contract.EngineMessage, bool) {
	prepared := make([]contract.EngineMessage, 0, len(history)+2)
	repaired := false
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
			names[call.ID] = call.Name
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
			prepared = append(prepared, contract.EngineMessage{
				Role: "tool", ToolCallID: id, Name: names[id],
				Content: content, ContentSet: true,
			})
			repaired = true
		}
	}
	return prepared, repaired
}

// toolResultExistsLater 报告指定 tool 调用 ID 的结果是否出现在历史后文
// （用于防重复注入：该 ID 已有记录则不需要合成占位）。
func toolResultExistsLater(history []contract.EngineMessage, start int, id string) bool {
	for index := start; index < len(history); index++ {
		if history[index].Role == "tool" && history[index].ToolCallID == id {
			return true
		}
	}
	return false
}

// RepairEmptyHistoryContent 修复空 content 消息（assistant 工具调用 /
// 纯 reasoning / 其余角色）。
func RepairEmptyHistoryContent(history []contract.EngineMessage) ([]contract.EngineMessage, bool) {
	prepared := make([]contract.EngineMessage, len(history))
	copy(prepared, history)
	repaired := false
	for index := range prepared {
		message := &prepared[index]
		if strings.TrimSpace(message.Content) != "" {
			continue
		}
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			message.Content = ToolCallHistoryContent
			message.ContentSet = true
			repaired = true
			continue
		}
		if !message.ContentSet && message.Role == "assistant" && message.ReasoningContent != "" {
			message.Content = MissingHistoryContent
			message.ContentSet = true
			repaired = true
			continue
		}
		if message.Role == "system" || message.Role == "user" || message.Role == "assistant" || message.Role == "tool" {
			message.Content = MissingHistoryContent
			message.ContentSet = true
			repaired = true
		}
	}
	return prepared, repaired
}

// IsProviderOnlyHistoryContent 识别仅用于满足 provider 非空 content 要求的
// 修复文本（非用户创作，恢复后不得渲染为 assistant 回复）。
func IsProviderOnlyHistoryContent(content string) bool {
	return content == MissingHistoryContent || content == ToolCallHistoryContent ||
		strings.HasPrefix(content, InterruptedToolResultPrefix)
}
