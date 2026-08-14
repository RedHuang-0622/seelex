package session

import "testing"

// TestToolEventStateMultiObserver 验证工具事件多观察者：
// SetCallback（main 路径）+ Subscribe（实时流路径）并行收到，取消后不再收到。
func TestToolEventStateMultiObserver(t *testing.T) {
	state := NewToolEventState()
	var callbackSeen []string
	var observerSeen []string
	state.SetCallback(func(event SubagentToolEvent) {
		callbackSeen = append(callbackSeen, event.ID)
	})
	cancel := state.Subscribe(func(event SubagentToolEvent) {
		observerSeen = append(observerSeen, event.ID)
	})

	state.Publish(SubagentToolEvent{ID: "t1", Status: "running"})
	cancel()
	state.Publish(SubagentToolEvent{ID: "t2", Status: "success"})

	if len(callbackSeen) != 2 {
		t.Fatalf("callback seen = %v, want 2 events", callbackSeen)
	}
	if len(observerSeen) != 1 || observerSeen[0] != "t1" {
		t.Fatalf("observer seen = %v, want [t1] (canceled after t1)", observerSeen)
	}
}
