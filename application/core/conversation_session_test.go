package core

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestMainConversationRecordsKeepExecutionKinds(t *testing.T) {
	at := time.Unix(1, 0)
	records := mainConversationRecords([]model.Message{
		{ID: "s", Role: "system", Content: "prompt", CreatedAt: at},
		{ID: "u", Role: "user", Content: "ask", CreatedAt: at},
		{ID: "a", Role: "assistant", Content: "answer", ReasoningContent: "think", CreatedAt: at, Tool: &model.ToolCall{
			ID: "call-1", Name: "read_file", Arguments: `{"path":"a"}`, Result: "ok", Status: dto.ToolEventSuccess,
		}},
	})
	want := []dto.ConversationRecordKind{
		dto.ConversationRecordSystem,
		dto.ConversationRecordUser,
		dto.ConversationRecordReasoning,
		dto.ConversationRecordAssistant,
		dto.ConversationRecordToolCall,
		dto.ConversationRecordToolResult,
	}
	if len(records) != len(want) {
		t.Fatalf("records = %+v", records)
	}
	for index, kind := range want {
		if records[index].Kind != kind {
			t.Fatalf("record %d kind = %s, want %s", index, records[index].Kind, kind)
		}
	}
}

func TestMainConversationRecoverySeparatesLiveColdAndInterrupted(t *testing.T) {
	if got := mainConversationRecovery(model.SessionSnapshot{Resident: true}); got != dto.RecoveryLive {
		t.Fatalf("resident recovery = %s", got)
	}
	if got := mainConversationRecovery(model.SessionSnapshot{}); got != dto.RecoveryCold {
		t.Fatalf("non-resident recovery = %s", got)
	}
	if got := mainConversationRecovery(model.SessionSnapshot{Session: model.SessionState{Status: model.SessionStatusRunning}}); got != dto.RecoveryInterrupted {
		t.Fatalf("running without resident face = %s", got)
	}
}
