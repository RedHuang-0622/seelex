package sessionaccess

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

type stubAdapter struct {
	page dto.SessionRecordPage
}

func (s stubAdapter) View(context.Context, dto.ConversationSessionRef, dto.ConversationViewRequest) (dto.SessionRecordPage, error) {
	return s.page, nil
}

func (s stubAdapter) Chat(context.Context, dto.ConversationSessionRef, dto.ConversationChatRequest) (dto.ChatReceipt, error) {
	return dto.ChatReceipt{Accepted: true}, nil
}

func (s stubAdapter) Recover(context.Context, dto.ConversationSessionRef) (dto.SessionRecovery, error) {
	return dto.SessionRecovery{State: dto.RecoveryLive}, nil
}

func TestRegistryRequiresWorkItemForTeammateWindow(t *testing.T) {
	registry := NewRegistry()
	registry.Register(dto.ConversationTeammate, stubAdapter{})
	if _, err := registry.Session(dto.ConversationSessionRef{
		Kind: dto.ConversationTeammate, SessionID: "role-1", Role: "exec",
	}); err == nil || !strings.Contains(err.Error(), "WorkItem") {
		t.Fatalf("teammate without WorkItem must fail, got %v", err)
	}
	if _, err := registry.Session(dto.ConversationSessionRef{
		Kind: dto.ConversationTeammate, SessionID: "role-1", Role: "exec", WorkItemID: "wi-1",
	}); err != nil {
		t.Fatalf("teammate WorkItem window must resolve: %v", err)
	}
}

func TestSessionRecordsUseOnePaginationAndUTF8ClipRule(t *testing.T) {
	records := []dto.ConversationRecord{
		{ID: "1", Kind: dto.ConversationRecordSystem, Content: "system"},
		{ID: "2", Kind: dto.ConversationRecordUser, Content: "你好世界"},
		{ID: "3", Kind: dto.ConversationRecordAssistant, Content: "answer"},
		{ID: "4", Kind: dto.ConversationRecordToolResult, Content: "result"},
	}
	page := PaginateRecords("sess-1", records, dto.ConversationViewRequest{
		Offset: 0, Limit: 2, MaxRunes: 3,
		Kinds: []dto.ConversationRecordKind{
			dto.ConversationRecordUser, dto.ConversationRecordAssistant, dto.ConversationRecordToolResult,
		},
	})
	if page.Total != 3 || len(page.Records) != 2 || !page.HasMore || page.Offset != 0 || page.Limit != 2 {
		t.Fatalf("unexpected page: %+v", page)
	}
	if page.Records[0].Content != "你好世…" || !page.Records[0].Truncated || !page.Truncated {
		t.Fatalf("rune-safe clipping/truncated flags missing: %+v", page.Records[0])
	}
	if records[1].Content != "你好世界" {
		t.Fatal("pagination must not mutate source records")
	}
}

func TestRecoveryStateVocabularyIsStable(t *testing.T) {
	for _, state := range []dto.SessionRecoveryState{
		dto.RecoveryLive, dto.RecoveryCold, dto.RecoveryInterrupted, dto.RecoveryMissing,
	} {
		if string(state) == "" {
			t.Fatal("empty recovery state")
		}
	}
}
