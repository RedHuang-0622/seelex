package conversation

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestMainRecordsPreserveOrderAndToolRows(t *testing.T) {
	records := MainRecords([]protocol.Message{
		{ID: "u1", Role: "user", Content: "question"},
		{
			ID: "a1", Role: "assistant", Content: "answer", ReasoningContent: "reason",
			Tool: &protocol.ToolCall{ID: "call-1", Name: "bash", Arguments: "{}", Result: "ok", Status: dto.ToolEventSuccess},
		},
	})
	kinds := []dto.ConversationRecordKind{
		dto.ConversationRecordUser,
		dto.ConversationRecordReasoning,
		dto.ConversationRecordAssistant,
		dto.ConversationRecordToolCall,
		dto.ConversationRecordToolResult,
	}
	if len(records) != len(kinds) {
		t.Fatalf("records = %#v", records)
	}
	for index, kind := range kinds {
		if records[index].Kind != kind {
			t.Fatalf("record %d kind = %q, want %q", index, records[index].Kind, kind)
		}
	}
}

func TestMainRecoveryClassifiesLiveColdAndInterrupted(t *testing.T) {
	if got := MainRecovery(protocol.SessionSnapshot{Resident: true}); got != dto.RecoveryLive {
		t.Fatalf("live = %q", got)
	}
	if got := MainRecovery(protocol.SessionSnapshot{}); got != dto.RecoveryCold {
		t.Fatalf("cold = %q", got)
	}
	if got := MainRecovery(protocol.SessionSnapshot{
		Session: protocol.SessionState{Status: protocol.SessionStatusRunning},
	}); got != dto.RecoveryInterrupted {
		t.Fatalf("interrupted = %q", got)
	}
}
