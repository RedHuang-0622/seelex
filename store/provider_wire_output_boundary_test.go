package store

import (
	"context"
	"testing"
	"time"
)

// TestDurableHistoryPreservesToolCallAssistantOutputContent pins the provider
// output boundary used by DeepSeek context caching: narration generated with a
// tool call belongs to the model-output prefix and must survive reload.
func TestDurableHistoryPreservesToolCallAssistantOutputContent(t *testing.T) {
	const narration = "I will inspect the file before editing it."
	router := newTestRouter(t)
	history := NewDurableHistory(router, "session-output-boundary")
	history.SetTailBudget(100_000, 10)
	rows := []Event{
		{Seq: 1, MessageID: "m1", Role: "user", Kind: EventKindUserInput, Content: "fix it", TokenCount: 2, CreatedAt: time.Now().UTC()},
		{Seq: 2, MessageID: "m2", Role: "assistant", Kind: EventKindToolCall, Content: narration,
			ToolCalls:  []EventToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"x"}`}},
			TokenCount: 9, CreatedAt: time.Now().UTC()},
		{Seq: 3, MessageID: "m3", Role: "tool", Kind: EventKindToolOutput, ToolCallID: "call-1", Name: "read_file",
			Content: "OK", TokenCount: 1, CreatedAt: time.Now().UTC()},
	}
	if err := router.SaveCommit("session-output-boundary", Commit{Events: rows}); err != nil {
		t.Fatal(err)
	}

	loaded, err := history.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range loaded {
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			continue
		}
		if message.Content == nil || *message.Content != narration {
			t.Fatalf("tool-call assistant content = %v, want provider output %q", message.Content, narration)
		}
		return
	}
	t.Fatalf("tool-call assistant missing from loaded history: %+v", loaded)
}
