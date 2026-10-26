package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

// UnifiedDeps supplies the narrow host ports used by the unified conversation
// capability. Main-session chat remains a session aggregate command and is
// therefore injected as a callback rather than copied into this package.
type UnifiedDeps struct {
	SnapshotOf          func(string) (protocol.SessionSnapshot, error)
	Records             SessionRecordsPort
	Agents              contract.AgentConversationPort
	SubmitToSession     func(context.Context, string, string) error
	SnapshotUnavailable error
}

// UnifiedApplication owns routing between the main session and execution-side
// agent windows. It does not own session state or chat execution.
type UnifiedApplication struct {
	deps UnifiedDeps
}

func NewUnified(deps UnifiedDeps) *UnifiedApplication {
	return &UnifiedApplication{deps: deps}
}

func (application *UnifiedApplication) View(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error) {
	adapter, err := application.session(ref)
	if err != nil {
		return dto.SessionRecordPage{}, err
	}
	return adapter.View(ctx, ref, request)
}

func (application *UnifiedApplication) Chat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error) {
	adapter, err := application.session(ref)
	if err != nil {
		return dto.ChatReceipt{}, err
	}
	return adapter.Chat(ctx, ref, request)
}

func (application *UnifiedApplication) Recover(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error) {
	adapter, err := application.session(ref)
	if err != nil {
		return dto.SessionRecovery{}, err
	}
	return adapter.Recover(ctx, ref)
}

func (application *UnifiedApplication) session(ref dto.ConversationSessionRef) (ConversationSession, error) {
	if application == nil {
		return nil, errors.New("conversation: application is nil")
	}
	switch ref.Kind {
	case dto.ConversationMain:
		if strings.TrimSpace(ref.SessionID) == "" {
			return nil, errors.New("conversation: main session ID is required")
		}
		return mainConversation{application: application}, nil
	case dto.ConversationSubagent, dto.ConversationTeammate:
		if application.deps.Agents == nil {
			return nil, fmt.Errorf("conversation: %s adapter is not configured", ref.Kind)
		}
		if ref.Kind == dto.ConversationTeammate && strings.TrimSpace(ref.WorkItemID) == "" {
			return nil, errors.New("conversation: teammate window requires a WorkItem")
		}
		return agentConversation{port: application.deps.Agents}, nil
	default:
		return nil, fmt.Errorf("conversation: unknown kind %q", ref.Kind)
	}
}

type mainConversation struct {
	application *UnifiedApplication
}

func (adapter mainConversation) View(_ context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error) {
	snapshot, err := adapter.application.deps.SnapshotOf(ref.SessionID)
	if err != nil {
		if errors.Is(err, adapter.application.deps.SnapshotUnavailable) {
			return dto.SessionRecordPage{Recovery: dto.RecoveryMissing, SessionID: ref.SessionID, Records: []dto.ConversationRecord{}}, nil
		}
		return dto.SessionRecordPage{}, err
	}
	page := adapter.application.deps.Records.PaginateRecords(
		ref.SessionID,
		MainRecords(snapshot.Conversation),
		request,
	)
	page.Status = string(snapshot.Session.Status)
	page.Recovery = MainRecovery(snapshot)
	return page, nil
}

func (adapter mainConversation) Chat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error) {
	if strings.TrimSpace(request.Message) == "" {
		return dto.ChatReceipt{}, errors.New("conversation: message is required")
	}
	if err := adapter.application.deps.SubmitToSession(ctx, ref.SessionID, request.Message); err != nil {
		return dto.ChatReceipt{}, err
	}
	return dto.ChatReceipt{Accepted: true, SessionID: ref.SessionID, State: dto.RecoveryLive}, nil
}

func (adapter mainConversation) Recover(_ context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error) {
	snapshot, err := adapter.application.deps.SnapshotOf(ref.SessionID)
	if err != nil {
		if errors.Is(err, adapter.application.deps.SnapshotUnavailable) {
			return dto.SessionRecovery{State: dto.RecoveryMissing, SessionID: ref.SessionID}, nil
		}
		return dto.SessionRecovery{}, err
	}
	return dto.SessionRecovery{
		State:       MainRecovery(snapshot),
		SessionID:   ref.SessionID,
		Durable:     true,
		RecoveredAt: time.Now(),
	}, nil
}

// MainRecovery classifies the durable/live state without taking an
// application lock; the session query port returns an immutable snapshot.
func MainRecovery(snapshot protocol.SessionSnapshot) dto.SessionRecoveryState {
	if snapshot.Resident {
		return dto.RecoveryLive
	}
	if snapshot.Session.Status == protocol.SessionStatusRunning {
		return dto.RecoveryInterrupted
	}
	return dto.RecoveryCold
}

// MainRecords projects transcript messages into the shared conversation record
// vocabulary. Pagination and clipping remain owned by SessionRecordsPort.
func MainRecords(messages []protocol.Message) []dto.ConversationRecord {
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
			records = append(records, dto.ConversationRecord{
				ID: toolID + ":result", Kind: dto.ConversationRecordToolResult, Role: "tool",
				Content: message.Tool.Result, Error: message.Tool.Error, ToolName: message.Tool.Name,
				ToolCallID: message.Tool.ID, Status: string(message.Tool.Status), At: message.CreatedAt,
			})
		}
	}
	return records
}

type agentConversation struct {
	port contract.AgentConversationPort
}

func (adapter agentConversation) View(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error) {
	return adapter.port.ViewAgentConversation(ctx, ref, request)
}

func (adapter agentConversation) Chat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error) {
	return adapter.port.ChatAgentConversation(ctx, ref, request)
}

func (adapter agentConversation) Recover(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error) {
	return adapter.port.RecoverAgentConversation(ctx, ref)
}
