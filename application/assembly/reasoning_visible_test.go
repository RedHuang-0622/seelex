package assembly

import (
	"encoding/json"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/event"
	"testing"
)

// TestAttachLatestReasoningExposesThinkingToVisibleMessage 验证回合结束后，
// 引擎历史中的推理内容会挂到可见 assistant 消息并发布 message.delta 事件
// （reasoning_content 与 content 分离：聊天区一行带过，轨迹区完整查看）。
func TestAttachLatestReasoningExposesThinkingToVisibleMessage(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer frontendOfTestService(service).Shutdown()

	sessionID := service.Core.Snapshot.Session.ID
	engine.ReplaceHistory(sessionID, []contract.EngineMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "answer", ReasoningContent: "thinking steps"},
	})

	sub := service.Events.Subscribe(16)
	defer sub.Close()

	service.ViewMu.Lock()
	assistant := service.components.view.AppendMessageLocked("assistant", "", nil)
	service.ViewMu.Unlock()

	service.attachLatestReasoning(sessionID, "request-1")

	service.ViewMu.Lock()
	got := assistant.ReasoningContent
	service.ViewMu.Unlock()
	if got != "thinking steps" {
		t.Fatalf("visible assistant reasoning = %q, want thinking steps", got)
	}

	sawReasoningEvent := false
	// 循环变量不能叫 `event`：它会把 `middle/protocol/event` 包名遮蔽掉，
	// 而这里要用 `event.MessageDelta`（R10b 之后别名改成限定名）。
	for evt := range sub.Events {
		if evt.Kind != EventMessageDelta {
			continue
		}
		var delta event.MessageDelta
		if err := json.Unmarshal(evt.Payload, &delta); err != nil {
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
