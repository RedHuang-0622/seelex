package conversation

import (
	"context"
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

type unifiedQueryStub struct {
	snapshot protocol.SessionSnapshot
	err      error
}

func (stub unifiedQueryStub) SnapshotOf(string) (protocol.SessionSnapshot, error) {
	return stub.snapshot, stub.err
}

type unifiedRecordsStub struct{}

func (unifiedRecordsStub) PaginateRecords(sessionID string, records []dto.ConversationRecord, _ dto.ConversationViewRequest) dto.SessionRecordPage {
	return dto.SessionRecordPage{SessionID: sessionID, Records: records}
}

type unifiedAgentStub struct {
	viewRef dto.ConversationSessionRef
}

func (stub *unifiedAgentStub) ViewAgentConversation(_ context.Context, ref dto.ConversationSessionRef, _ dto.ConversationViewRequest) (dto.SessionRecordPage, error) {
	stub.viewRef = ref
	return dto.SessionRecordPage{SessionID: ref.SessionID}, nil
}

func (*unifiedAgentStub) ChatAgentConversation(context.Context, dto.ConversationSessionRef, dto.ConversationChatRequest) (dto.ChatReceipt, error) {
	return dto.ChatReceipt{Accepted: true}, nil
}

func (*unifiedAgentStub) RecoverAgentConversation(context.Context, dto.ConversationSessionRef) (dto.SessionRecovery, error) {
	return dto.SessionRecovery{State: dto.RecoveryLive}, nil
}

func TestUnifiedApplicationRoutesMainAndAgentConversations(t *testing.T) {
	missing := errors.New("snapshot unavailable")
	var submittedSessionID, submittedText string
	agent := &unifiedAgentStub{}
	application := NewUnified(UnifiedDeps{
		SnapshotOf: func(sessionID string) (protocol.SessionSnapshot, error) {
			if sessionID == "missing" {
				return protocol.SessionSnapshot{}, missing
			}
			return protocol.SessionSnapshot{
				Resident: true,
				Session:  protocol.SessionState{ID: sessionID},
				Conversation: []protocol.Message{{
					ID: "m1", Role: "user", Content: "hello",
				}},
			}, nil
		},
		Records: unifiedRecordsStub{},
		Agents:  agent,
		SubmitToSession: func(_ context.Context, sessionID, text string) error {
			submittedSessionID, submittedText = sessionID, text
			return nil
		},
		SnapshotUnavailable: missing,
	})

	page, err := application.View(context.Background(), dto.ConversationSessionRef{
		Kind: dto.ConversationMain, SessionID: "main",
	}, dto.ConversationViewRequest{})
	if err != nil || page.Recovery != dto.RecoveryLive || len(page.Records) != 1 {
		t.Fatalf("main View = %#v, %v", page, err)
	}
	if _, err := application.Chat(context.Background(), dto.ConversationSessionRef{
		Kind: dto.ConversationMain, SessionID: "main",
	}, dto.ConversationChatRequest{Message: "next"}); err != nil {
		t.Fatal(err)
	}
	if submittedSessionID != "main" || submittedText != "next" {
		t.Fatalf("submit = %q:%q", submittedSessionID, submittedText)
	}
	missingPage, err := application.View(context.Background(), dto.ConversationSessionRef{
		Kind: dto.ConversationMain, SessionID: "missing",
	}, dto.ConversationViewRequest{})
	if err != nil || missingPage.Recovery != dto.RecoveryMissing {
		t.Fatalf("missing View = %#v, %v", missingPage, err)
	}

	agentRef := dto.ConversationSessionRef{Kind: dto.ConversationTeammate, SessionID: "role-1", WorkItemID: "item-1"}
	if _, err := application.View(context.Background(), agentRef, dto.ConversationViewRequest{}); err != nil {
		t.Fatal(err)
	}
	if agent.viewRef.WorkItemID != "item-1" {
		t.Fatalf("agent ref = %#v", agent.viewRef)
	}
	subagentRef := dto.ConversationSessionRef{Kind: dto.ConversationSubagent, SessionID: "node-1", MainSessionID: "main"}
	if _, err := application.View(context.Background(), subagentRef, dto.ConversationViewRequest{}); err != nil {
		t.Fatal(err)
	}
	if agent.viewRef.Kind != dto.ConversationSubagent || agent.viewRef.SessionID != "node-1" {
		t.Fatalf("subagent should use the same AgentConversationPort: %#v", agent.viewRef)
	}
}
