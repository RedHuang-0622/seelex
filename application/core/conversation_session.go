package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// ConversationView is the Application entry for the unified conversation
// capability. Main stays on this service; subagent/teammate are delegated to
// the optional execution-side port.
func (service *Service) ConversationView(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error) {
	adapter, err := service.conversationSession(ref)
	if err != nil {
		return dto.SessionRecordPage{}, err
	}
	return adapter.View(ctx, ref, request)
}

// ConversationChat starts one bounded chat turn through the unified capability.
func (service *Service) ConversationChat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error) {
	adapter, err := service.conversationSession(ref)
	if err != nil {
		return dto.ChatReceipt{}, err
	}
	return adapter.Chat(ctx, ref, request)
}

// RecoverConversation resolves the unified live/cold/interrupted/missing state.
func (service *Service) RecoverConversation(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error) {
	adapter, err := service.conversationSession(ref)
	if err != nil {
		return dto.SessionRecovery{}, err
	}
	return adapter.Recover(ctx, ref)
}

func (service *Service) conversationSession(ref dto.ConversationSessionRef) (applicationconversation.ConversationSession, error) {
	if service == nil {
		return nil, errors.New("conversation: application service is nil")
	}
	switch ref.Kind {
	case dto.ConversationMain:
		if strings.TrimSpace(ref.SessionID) == "" {
			return nil, errors.New("conversation: main session ID is required")
		}
		return mainConversationAdapter{service: service}, nil
	case dto.ConversationSubagent, dto.ConversationTeammate:
		port, ok := service.Deps.Runtime.(contract.AgentConversationPort)
		if !ok {
			return nil, fmt.Errorf("conversation: %s adapter is not configured", ref.Kind)
		}
		if ref.Kind == dto.ConversationTeammate && strings.TrimSpace(ref.WorkItemID) == "" {
			return nil, errors.New("conversation: teammate window requires a WorkItem")
		}
		return agentConversationAdapter{port: port}, nil
	default:
		return nil, fmt.Errorf("conversation: unknown kind %q", ref.Kind)
	}
}

type mainConversationAdapter struct {
	service *Service
}

func (adapter mainConversationAdapter) View(_ context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error) {
	snapshot, err := adapter.service.components.sessionQuery.SnapshotOf(ref.SessionID)
	if err != nil {
		if errors.Is(err, ErrSessionSnapshotUnavailable) {
			return dto.SessionRecordPage{Recovery: dto.RecoveryMissing, SessionID: ref.SessionID, Records: []dto.ConversationRecord{}}, nil
		}
		return dto.SessionRecordPage{}, err
	}
	page := adapter.service.components.sessionRecords.PaginateRecords(
		ref.SessionID,
		mainConversationRecords(snapshot.Conversation),
		request,
	)
	page.Status = string(snapshot.Session.Status)
	page.Recovery = mainConversationRecovery(snapshot)
	return page, nil
}

func (adapter mainConversationAdapter) Chat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error) {
	if strings.TrimSpace(request.Message) == "" {
		return dto.ChatReceipt{}, errors.New("conversation: message is required")
	}
	if err := adapter.service.SubmitToSession(ctx, ref.SessionID, request.Message); err != nil {
		return dto.ChatReceipt{}, err
	}
	return dto.ChatReceipt{Accepted: true, SessionID: ref.SessionID, State: dto.RecoveryLive}, nil
}

func (adapter mainConversationAdapter) Recover(_ context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error) {
	snapshot, err := adapter.service.components.sessionQuery.SnapshotOf(ref.SessionID)
	if err != nil {
		if errors.Is(err, ErrSessionSnapshotUnavailable) {
			return dto.SessionRecovery{State: dto.RecoveryMissing, SessionID: ref.SessionID}, nil
		}
		return dto.SessionRecovery{}, err
	}
	return dto.SessionRecovery{
		State:       mainConversationRecovery(snapshot),
		SessionID:   ref.SessionID,
		Durable:     true,
		RecoveredAt: time.Now(),
	}, nil
}

func mainConversationRecovery(snapshot model.SessionSnapshot) dto.SessionRecoveryState {
	if snapshot.Resident {
		return dto.RecoveryLive
	}
	if snapshot.Session.Status == model.SessionStatusRunning {
		return dto.RecoveryInterrupted
	}
	return dto.RecoveryCold
}

func mainConversationRecords(messages []model.Message) []dto.ConversationRecord {
	records := make([]dto.ConversationRecord, 0, len(messages)+1)
	for index, message := range messages {
		id := strings.TrimSpace(message.ID)
		if id == "" {
			id = fmt.Sprintf("message-%d", index+1)
		}
		role := strings.TrimSpace(message.Role)
		switch role {
		case "system":
			records = append(records, dto.ConversationRecord{
				ID: id, Kind: dto.ConversationRecordSystem, Role: role,
				Content: message.Content, At: message.CreatedAt,
			})
		case "user":
			records = append(records, dto.ConversationRecord{
				ID: id, Kind: dto.ConversationRecordUser, Role: role,
				Content: message.Content, At: message.CreatedAt,
			})
		case "assistant":
			if strings.TrimSpace(message.ReasoningContent) != "" {
				records = append(records, dto.ConversationRecord{
					ID: id + ":reasoning", Kind: dto.ConversationRecordReasoning, Role: role,
					Reasoning: message.ReasoningContent, At: message.CreatedAt,
				})
			}
			records = append(records, dto.ConversationRecord{
				ID: id, Kind: dto.ConversationRecordAssistant, Role: role,
				Content: message.Content, At: message.CreatedAt,
			})
		}
		if message.Tool == nil {
			continue
		}
		toolID := strings.TrimSpace(message.Tool.ID)
		if toolID == "" {
			toolID = id + ":tool"
		}
		records = append(records, dto.ConversationRecord{
			ID: toolID + ":call", Kind: dto.ConversationRecordToolCall, Role: "assistant",
			Content: message.Tool.Arguments, ToolName: message.Tool.Name, ToolCallID: message.Tool.ID,
			Status: string(message.Tool.Status), At: message.CreatedAt,
		})
		if strings.TrimSpace(message.Tool.Result) != "" || strings.TrimSpace(message.Tool.Error) != "" {
			status := string(message.Tool.Status)
			records = append(records, dto.ConversationRecord{
				ID: toolID + ":result", Kind: dto.ConversationRecordToolResult, Role: "tool",
				Content: message.Tool.Result, Error: message.Tool.Error, ToolName: message.Tool.Name,
				ToolCallID: message.Tool.ID, Status: status, At: message.CreatedAt,
			})
		}
	}
	return records
}

type agentConversationAdapter struct {
	port contract.AgentConversationPort
}

func (adapter agentConversationAdapter) View(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error) {
	return adapter.port.ViewAgentConversation(ctx, ref, request)
}

func (adapter agentConversationAdapter) Chat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error) {
	return adapter.port.ChatAgentConversation(ctx, ref, request)
}

func (adapter agentConversationAdapter) Recover(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error) {
	return adapter.port.RecoverAgentConversation(ctx, ref)
}
