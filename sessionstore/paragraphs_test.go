package sessionstore

import (
	"reflect"
	"testing"
)

func TestEventParagraphsDeriveCompleteUnitBoundaries(t *testing.T) {
	events := []Event{
		{Seq: 1, TaskID: "chat-1", Role: "user", Content: "inspect", MessageID: "m1"},
		{Seq: 2, TaskID: "chat-1", Role: "assistant", ToolCalls: []EventToolCall{{ID: "a"}, {ID: "b"}}, MessageID: "m2"},
		{Seq: 3, TaskID: "chat-1", Role: "tool", ToolCallID: "b", MessageID: "m3"},
		{Seq: 4, TaskID: "chat-1", Role: "tool", ToolCallID: "a", MessageID: "m4"},
		{Seq: 5, TaskID: "chat-1", Role: "assistant", Content: "done", MessageID: "m5"},
		{Seq: 6, TaskID: "chat-2", Role: "user", Content: "next", MessageID: "m6"},
		{Seq: 7, TaskID: "chat-2", Role: "assistant", Content: "answer", MessageID: "m7"},
		{Seq: 8, Role: "tool", ToolCallID: "orphan"}, // 孤儿不构成段落
	}
	paragraphs := EventParagraphs(events)
	want := []EventParagraph{
		{EventFrom: 1, EventTo: 5, RequestID: "chat-1", MessageFrom: "m1", MessageTo: "m5"},
		{EventFrom: 6, EventTo: 7, RequestID: "chat-2", MessageFrom: "m6", MessageTo: "m7"},
	}
	if !reflect.DeepEqual(paragraphs, want) {
		t.Fatalf("paragraphs = %#v, want %#v", paragraphs, want)
	}
}

func TestParagraphBoundaryChecks(t *testing.T) {
	events := []Event{
		{Seq: 1, Role: "user", Content: "a"},
		{Seq: 2, Role: "assistant", Content: "b"},
		{Seq: 3, Role: "user", Content: "c"},
		{Seq: 4, Role: "assistant", Content: "d"},
	}
	if !IsParagraphBoundary(events, 0) {
		t.Fatal("seq 0 (fork 起点) must be a boundary")
	}
	for _, boundary := range []uint64{2, 4} {
		if !IsParagraphBoundary(events, boundary) {
			t.Fatalf("seq %d must be a boundary", boundary)
		}
	}
	for _, interior := range []uint64{1, 3} {
		if IsParagraphBoundary(events, interior) {
			t.Fatalf("seq %d must not be a boundary", interior)
		}
	}
	if end, ok := ParagraphEnd(events, 1); !ok || end != 2 {
		t.Fatalf("ParagraphEnd(1) = %d,%v, want 2,true", end, ok)
	}
}
