package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestChatStateIsPublishedNotInferred 验证运行态由后端下发：提交后收到
// chat.changed(running=true)，回合收尾再收到 chat.changed(running=false)，
// 客户端不需要从"收到增量事件"反推自己是否在跑。
func TestChatStateIsPublishedNotInferred(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	subscription, err := service.SubscribeSession("", 256)
	if err != nil {
		t.Fatalf("SubscribeSession(view): %v", err)
	}
	defer subscription.Close()

	if err := service.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}

	states := []ChatState{}
	deadline := time.After(500 * time.Millisecond)
collect:
	for {
		select {
		case event := <-subscription.Events:
			if event.Kind != EventChatChanged {
				continue
			}
			if event.SessionID != service.Snapshot().Session.ID {
				t.Fatalf("chat.changed routed to %q, want the view session", event.SessionID)
			}
			var state ChatState
			if err := json.Unmarshal(event.Payload, &state); err != nil {
				t.Fatalf("decode chat.changed payload: %v", err)
			}
			states = append(states, state)
		case <-deadline:
			break collect
		}
	}
	if len(states) < 2 {
		t.Fatalf("chat.changed emissions = %d, want at least a start and a stop: %+v", len(states), states)
	}
	if !states[0].Running || states[0].RequestID == "" {
		t.Fatalf("first chat.changed = %+v, want running with a request id", states[0])
	}
	if last := states[len(states)-1]; last.Running {
		t.Fatalf("last chat.changed = %+v, want running=false after the turn", last)
	}
}
