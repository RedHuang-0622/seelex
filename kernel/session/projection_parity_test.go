package session_runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestProjectConversationEventsPreservesVisibleFields(t *testing.T) {
	createdAt := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	events := []protocol.TranscriptEvent{
		{Seq: 2, Role: "user", Content: "question", CreatedAt: createdAt},
		{Seq: 3, Role: "system", Content: "system"},
		{Seq: 4, Role: "user", Content: "<!-- seelex:internal -->internal"},
		{Seq: 5, Role: "assistant", RoleName: protocol.RoleNameTL, Content: "advisor"},
		{
			Seq: 6, Role: "assistant", Kind: protocol.TranscriptEventKindLLM,
			Content: "answer", ReasoningContent: "reasoning", CreatedAt: createdAt,
			RoleName: protocol.RoleNameMain, RoleSessionID: "main", RoundID: 7, UnitSeq: 8,
			ConfigRole: "agent", ConfigModel: "model-a", ConfigProvider: "provider-a",
			ToolCalls: []protocol.TranscriptToolCall{
				{ID: "call-a", Name: "read", Arguments: `{"path":"a"}`},
				{ID: "call-b", Name: "write", Arguments: `{"path":"b"}`},
			},
		},
		{
			Seq: 9, Role: "tool", Kind: protocol.TranscriptEventKindToolOutput,
			Content: "result", ToolCallID: "call-a", Name: "read", CreatedAt: createdAt,
			RoleName: protocol.RoleNameMain, RoleSessionID: "main", RoundID: 7, UnitSeq: 8,
		},
	}

	messages := ProjectConversationEvents(events, ConversationProjectionPolicy{
		IsInternal: func(content string) bool {
			return strings.Contains(content, "<!-- seelex:internal -->")
		},
	})
	if len(messages) != 5 {
		t.Fatalf("messages = %#v, want 5 visible messages", messages)
	}
	if got := messages[0]; got.ID != "message-2" || got.Role != "user" || got.EventSeq != 2 {
		t.Fatalf("user message = %#v", got)
	}
	if got := messages[1]; got.ID != "message-6" || got.Content != "answer" ||
		got.ReasoningContent != "reasoning" || got.RoleName != protocol.RoleNameMain ||
		got.RoleSessionID != "main" || got.RoundID != 7 || got.UnitSeq != 8 ||
		got.ConfigRole != "agent" || got.ConfigModel != "model-a" || got.ConfigProvider != "provider-a" {
		t.Fatalf("assistant message = %#v", got)
	}
	if got := messages[2]; got.ID != "message-6-0" || got.Role != "tool" ||
		got.Tool == nil || got.Tool.ID != "call-a" || got.Tool.Status != dto.ToolEventSuccess {
		t.Fatalf("first tool call = %#v", got)
	}
	if got := messages[3]; got.ID != "message-6-1" || got.Tool == nil || got.Tool.ID != "call-b" {
		t.Fatalf("second tool call = %#v", got)
	}
	if got := messages[4]; got.ID != "message-9" || got.Role != "tool_result" ||
		got.Tool == nil || got.Tool.ID != "call-a" || got.Tool.Result != "result" {
		t.Fatalf("tool result = %#v", got)
	}
}
