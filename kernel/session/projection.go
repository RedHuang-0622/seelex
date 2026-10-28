package session_runtime

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

// ConversationProjectionPolicy 是 transcript 行投影为可见消息时允许变化的
// 策略；持久事实、字段映射与工具展开只由 ProjectConversationEvents 决定。
type ConversationProjectionPolicy struct {
	// IncludeSystem 保留 role=system 行。默认 false；系统行只用于审计与装配。
	IncludeSystem bool
	// IncludeRoleTL 保留 role_name=tl 的行。默认 false；TL 是评审角色，
	// 不属于主对话席位。
	IncludeRoleTL bool
	// IsInternal 标记 provider-only/internal 正文。为 nil 时不过滤。
	IsInternal func(content string) bool
}

// ProjectConversationEvents 是 TranscriptEvent 到可见 Message 的唯一投影。
//
// 输入必须是同一条 message 分片事实流；输出逐行保序。一个 assistant 事件中的
// 正文/推理先投影为一条消息，再为每个 tool call 投影一条 tool 消息。tool
// 输出行投影为 tool_result。ID、EventSeq、kind、群聊归属和路由字段全部随行
// 保留，避免冷恢复、fork 与实时视图出现三套规则。
func ProjectConversationEvents(events []protocol.TranscriptEvent, policy ConversationProjectionPolicy) []protocol.Message {
	messages := make([]protocol.Message, 0, len(events))
	for _, event := range events {
		if event.Role == "system" && !policy.IncludeSystem {
			continue
		}
		if policy.IsInternal != nil && policy.IsInternal(event.Content) {
			continue
		}
		if event.RoleName == protocol.RoleNameTL && !policy.IncludeRoleTL {
			continue
		}
		message := protocol.Message{
			ID: fmt.Sprintf("message-%d", event.Seq), Role: event.Role,
			Content: event.Content, ReasoningContent: event.ReasoningContent,
			EventSeq: event.Seq, Kind: messageKindForEvent(event), CreatedAt: event.CreatedAt,
			RoleName: event.RoleName, RoleSessionID: event.RoleSessionID,
			RoundID: event.RoundID, UnitSeq: event.UnitSeq,
			ConfigRole: event.ConfigRole, ConfigModel: event.ConfigModel, ConfigProvider: event.ConfigProvider,
		}
		switch event.Role {
		case "assistant":
			if len(event.ToolCalls) > 0 {
				if strings.TrimSpace(event.Content) != "" || strings.TrimSpace(event.ReasoningContent) != "" {
					messages = append(messages, protocol.Message{
						ID: fmt.Sprintf("message-%d", event.Seq), Role: "assistant",
						Content: event.Content, ReasoningContent: event.ReasoningContent,
						EventSeq: event.Seq, Kind: protocol.TranscriptEventKindLLM, CreatedAt: event.CreatedAt,
						RoleName: event.RoleName, RoleSessionID: event.RoleSessionID,
						RoundID: event.RoundID, UnitSeq: event.UnitSeq,
						ConfigRole: event.ConfigRole, ConfigModel: event.ConfigModel, ConfigProvider: event.ConfigProvider,
					})
				}
				for callIndex, call := range event.ToolCalls {
					messages = append(messages, protocol.Message{
						ID: fmt.Sprintf("message-%d-%d", event.Seq, callIndex), Role: "tool",
						EventSeq: event.Seq, Kind: protocol.TranscriptEventKindToolCall, CreatedAt: event.CreatedAt,
						Tool: &protocol.ToolCall{
							ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: dto.ToolEventSuccess,
						},
						RoleName: event.RoleName, RoleSessionID: event.RoleSessionID,
						RoundID: event.RoundID, UnitSeq: event.UnitSeq,
					})
				}
				continue
			}
		case "tool":
			message.Role = "tool_result"
			message.Kind = protocol.TranscriptEventKindToolOutput
			message.Tool = &protocol.ToolCall{
				ID: event.ToolCallID, Name: event.Name, Result: event.Content, Status: dto.ToolEventSuccess,
			}
		}
		messages = append(messages, message)
	}
	return messages
}

// messageKindForEvent 把事件类别映射为可见消息的多线谱类别；旧数据 Kind
// 为空时按 Role/ToolCalls 回退，保证迁移前数据上的轨迹分类稳定。
func messageKindForEvent(event protocol.TranscriptEvent) string {
	if event.Kind != "" {
		return event.Kind
	}
	if event.Role == "assistant" && len(event.ToolCalls) > 0 {
		return protocol.TranscriptEventKindToolCall
	}
	switch event.Role {
	case "user":
		return protocol.TranscriptEventKindUserInput
	case "assistant":
		return protocol.TranscriptEventKindLLM
	case "tool", "tool_result":
		return protocol.TranscriptEventKindToolOutput
	case "system":
		return protocol.TranscriptEventKindSystem
	case "error":
		return protocol.TranscriptEventKindError
	default:
		return protocol.TranscriptEventKindNotice
	}
}
