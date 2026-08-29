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

// HistoryCoordinator 拥有 provider 缓存归一化（空内容修复）。
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
// 安全（工具调用保留，仅恢复缺失的说明文本）。sessionID 指明目标会话。
func (h *HistoryCoordinator) PrepareProviderHistoryFor(sessionID string) error {
	history := h.engineHistory(sessionID)
	prepared, repaired := RepairEmptyHistoryContent(history)
	if !repaired {
		return nil
	}
	if err := h.replaceEngineHistory(sessionID, prepared); err != nil {
		return fmt.Errorf("repair empty provider history content: %w", err)
	}
	return nil
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
	return content == MissingHistoryContent || content == ToolCallHistoryContent
}
