package model

import "testing"

func TestConversationWindowKeepsSystemAndDurableRowsSeparately(t *testing.T) {
	rows := []Message{
		{Role: "system", Content: "system-1"},
		{Role: "user", Content: "user-1"},
		{Role: "assistant", Content: "assistant-1"},
		{Role: "system", Content: "system-2"},
		{Role: "user", Content: "user-2"},
	}

	tail := BoundConversationTail(rows, 1)
	if len(tail) != 2 || tail[0].Role != "system" || tail[1].Content != "user-2" {
		t.Fatalf("tail = %+v, want last system + last durable row", tail)
	}
	if got := DurableConversationCount(tail); got != 1 {
		t.Fatalf("durable count = %d, want 1", got)
	}

	head := BoundConversationHead(rows, 1)
	if len(head) != 2 || head[0].Content != "system-1" || head[1].Content != "user-1" {
		t.Fatalf("head = %+v, want first system + first durable row", head)
	}
}
