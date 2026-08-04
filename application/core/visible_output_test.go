package core

import (
	"strings"
	"testing"
)

func TestVisibleOutputStreamSuppressesSplitThoughtBlock(t *testing.T) {
	stream := newVisibleOutputStream("request-1")
	if got := stream.Consume("visible<th"); got != "visible" {
		t.Fatalf("first chunk = %q", got)
	}
	if got := stream.Consume("ink>private</think> answer"); got != " answer" {
		t.Fatalf("second chunk = %q", got)
	}
}

func TestAppendDeltaDoesNotExposeThoughtContent(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "request-1"}
	service.streamOutput = newVisibleOutputStream("request-1")
	service.appendMessageLocked("assistant", "", nil)
	service.mu.Unlock()

	service.appendDelta("request-1", "answer<think>private reasoning</think> done")
	snapshot := service.Snapshot()
	content := snapshot.Conversation[len(snapshot.Conversation)-1].Content
	if content != "answer done" || strings.Contains(content, "think") || strings.Contains(content, "private") {
		t.Fatalf("visible content = %q", content)
	}
}
