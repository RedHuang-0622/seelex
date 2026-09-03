package core

import (
	"encoding/json"
	"testing"
)

// TestAttachLatestReasoningExposesThinkingToVisibleMessage 验证回合结束后，
// 引擎历史中的推理内容会挂到可见 assistant 消息并发布 message.delta 事件
// （reasoning_content 与 content 分离：聊天区一行带过，轨迹区完整查看）。
func TestAttachLatestReasoningExposesThinkingToVisibleMessage(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()

	sessionID := service.Core.Snapshot.Session.ID
	engine.ReplaceHistory(sessionID, []EngineMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "answer", ReasoningContent: "thinking steps"},
	})

	sub := service.Events.Subscribe(16)
	defer sub.Close()

	service.ViewMu.Lock()
	assistant := service.appendMessageLocked("assistant", "", nil)
	service.ViewMu.Unlock()

	service.attachLatestReasoning(sessionID, "request-1")

	service.ViewMu.Lock()
	got := assistant.ReasoningContent
	service.ViewMu.Unlock()
	if got != "thinking steps" {
		t.Fatalf("visible assistant reasoning = %q, want thinking steps", got)
	}

	sawReasoningEvent := false
	for event := range sub.Events {
		if event.Kind != EventMessageDelta {
			continue
		}
		var delta MessageDelta
		if err := json.Unmarshal(event.Payload, &delta); err != nil {
			t.Fatalf("unmarshal message delta: %v", err)
		}
		if delta.MessageID == assistant.ID && delta.ReasoningContent == "thinking steps" {
			sawReasoningEvent = true
			break
		}
	}
	if !sawReasoningEvent {
		t.Fatal("expected EventMessageDelta carrying reasoning_content")
	}
}
