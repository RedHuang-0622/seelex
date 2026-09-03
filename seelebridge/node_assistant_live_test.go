package seelebridge

import (
	"testing"
	"time"
)

// TestSubscribeSubagentLiveReceivesAssistantDelta G7：assistant 正文增量经
// node 实时面即时投递 + 历史回放。正文内容源证据见 node/agent_node.go
// （ChatStream onChunk 转写；ChatStream 与 Chat 等价，仅多出流式分片）。
func TestSubscribeSubagentLiveReceivesAssistantDelta(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	history, live, cancel, err := runtime.SubscribeSubagentLive("node-assistant")
	if err != nil {
		t.Fatalf("SubscribeSubagentLive: %v", err)
	}
	defer cancel()
	if len(history) != 0 {
		t.Fatalf("history before deltas = %d, want 0", len(history))
	}

	runtime.recordNodeAssistant("node-assistant", "hel")
	runtime.recordNodeAssistant("node-assistant", "lo")

	var got string
	deadline := time.After(3 * time.Second)
	for got != "hello" {
		select {
		case event := <-live:
			if event.NodeID != "node-assistant" || event.Kind != "assistant" {
				t.Fatalf("live event = %+v, want assistant for node-assistant", event)
			}
			if event.Assistant == nil {
				t.Fatal("assistant event without payload")
			}
			got += event.Assistant.Text
		case <-deadline:
			t.Fatalf("assistant deltas did not arrive; got %q", got)
		}
	}

	// 历史回放：重新订阅仍能看到这两条增量（liveHistory 随广播累积）。
	replay, _, cancelReplay, err := runtime.SubscribeSubagentLive("node-assistant")
	if err != nil {
		t.Fatalf("re-subscribe: %v", err)
	}
	defer cancelReplay()
	joined := ""
	for _, event := range replay {
		if event.Kind == "assistant" && event.Assistant != nil {
			joined += event.Assistant.Text
		}
	}
	if joined != "hello" {
		t.Fatalf("replay assistant text = %q, want hello", joined)
	}
}
