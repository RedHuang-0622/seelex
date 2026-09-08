package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
)

// TestPersistKeepsToolNarrationAndReasoningInRecord：真实轨迹里工具轮之间
// 有 LLM 输出（正文/说明）与草稿（reasoning）。持久化从 transcript 重建
// 可见会话时不得丢弃：
//   - assistant 普通消息的 ReasoningContent；
//   - 同一 assistant 事件“正文 + ToolCalls”里的正文（应先出 assistant
//     消息，再出工具调用消息，与实时视图一致）。
func TestPersistKeepsToolNarrationAndReasoningInRecord(t *testing.T) {
	sessions := &archiveSessions{record: SessionRecord{
		Version: session_runtime.SessionRecordVersion,
		ID:      "session-reasoning",
	}}
	service := newTestService(t, &fakeEngine{sessionID: "session-reasoning"}, withTestSessions(sessions))
	defer service.Shutdown()
	service.ViewMu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-reasoning"}
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{Role: "user", Content: "q1"})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		Role: "assistant", Content: "answer1", ReasoningContent: "draft1",
	})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{Role: "user", Content: "q2"})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		Role: "assistant", Content: "narration before tool", ReasoningContent: "draft-n",
		ToolCalls: []TranscriptToolCall{{ID: "call-1", Name: "get_time", Arguments: `{}`}},
	})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		Role: "tool", ToolCallID: "call-1", Name: "get_time", Content: "ok",
	})
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		Role: "assistant", Content: "answer2", ReasoningContent: "draft2",
	})
	service.ViewMu.Unlock()

	if err := service.components.sessions.PersistCurrentSession(
		session_runtime.Location{Meta: SessionInfo{ID: "session-reasoning"}}, "session-reasoning",
	); err != nil {
		t.Fatal(err)
	}
	messages := sessions.record.Conversation.Messages
	if len(messages) != 7 {
		t.Fatalf("durable conversation = %d messages, want 7 (narration + tool call must both survive)", len(messages))
	}
	expect := []struct {
		role      string
		content   string
		reasoning string
	}{
		{"user", "q1", ""},
		{"assistant", "answer1", "draft1"},
		{"user", "q2", ""},
		{"assistant", "narration before tool", "draft-n"},
		{"tool", "", ""},
		{"tool_result", "ok", ""},
		{"assistant", "answer2", "draft2"},
	}
	for index, want := range expect {
		message := messages[index]
		if message.Role != want.role || message.Content != want.content || message.ReasoningContent != want.reasoning {
			t.Fatalf("message[%d] = role=%q content=%q reasoning=%q, want role=%q content=%q reasoning=%q",
				index, message.Role, message.Content, message.ReasoningContent,
				want.role, want.content, want.reasoning)
		}
	}
	if messages[4].Tool == nil || messages[4].Tool.ID != "call-1" || messages[5].Tool == nil || messages[5].Tool.ID != "call-1" {
		t.Fatalf("tool call/result pairing missing: %#v", messages[4:6])
	}
}
