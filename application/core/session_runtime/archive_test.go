package session_runtime

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// TestEnrichTranscriptMessageIDsPairsEventsToMessages 验证 event-to-message
// 关联：tool call/result 按 CallID 配对，user/assistant 按角色+内容配对，
// 不按数组位置推导；无法稳定配对的保持空。
func TestEnrichTranscriptMessageIDsPairsEventsToMessages(t *testing.T) {
	record := model.SessionRecord{Conversation: model.ConversationRecord{Messages: []model.Message{
		{ID: "message-1", Role: "user", Content: "inspect the repo"},
		{ID: "message-2", Role: "assistant", Content: "let me read files"},
		{ID: "message-3", Role: "tool", Tool: &model.ToolCall{ID: "call-1", Name: "read", Status: "success"}},
		{ID: "message-4", Role: "tool_result", Tool: &model.ToolCall{ID: "call-1", Name: "read", Status: "success"}},
	}}}
	events := []model.TranscriptEvent{
		{Seq: 1, Role: "user", Content: "inspect the repo"},
		{Seq: 2, Role: "assistant", Content: "let me read files"},
		{Seq: 3, Role: "assistant", ToolCalls: []model.TranscriptToolCall{{ID: "call-1", Name: "read"}}},
		{Seq: 4, Role: "tool", ToolCallID: "call-1", Name: "read", Content: "found"},
		{Seq: 5, Role: "assistant", Content: "unrelated"},
		{Seq: 6, Role: "tool", ToolCallID: "call-orphan", Name: "bash"},
	}
	enrichTranscriptMessageIDs(events, record)

	want := []string{"message-1", "message-2", "message-3", "message-4", "", ""}
	for index, event := range events {
		if event.MessageID != want[index] {
			t.Fatalf("event %d (%s) MessageID = %q, want %q", index, event.Role, event.MessageID, want[index])
		}
	}
	// 已配对的 MessageID 不被重复覆盖。
	events[0].MessageID = "already-paired"
	enrichTranscriptMessageIDs(events, record)
	if events[0].MessageID != "already-paired" {
		t.Fatalf("existing MessageID overwritten: %q", events[0].MessageID)
	}
}

// TestEnrichTranscriptMessageIDsSkipsAmbiguousMultiCallEvents 验证多工具调用
// 事件不按数组位置猜测配对（保持空）。
func TestEnrichTranscriptMessageIDsSkipsAmbiguousMultiCallEvents(t *testing.T) {
	record := model.SessionRecord{Conversation: model.ConversationRecord{Messages: []model.Message{
		{ID: "message-1", Role: "tool", Tool: &model.ToolCall{ID: "call-a", Name: "read"}},
		{ID: "message-2", Role: "tool", Tool: &model.ToolCall{ID: "call-b", Name: "bash"}},
	}}}
	events := []model.TranscriptEvent{
		{Seq: 1, Role: "assistant", ToolCalls: []model.TranscriptToolCall{{ID: "call-a"}, {ID: "call-b"}}},
	}
	enrichTranscriptMessageIDs(events, record)
	if events[0].MessageID != "" {
		t.Fatalf("ambiguous multi-call event got MessageID %q, want empty", events[0].MessageID)
	}
}
