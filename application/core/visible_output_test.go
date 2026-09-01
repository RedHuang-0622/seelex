package core

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/chat"
)

func TestAppendDeltaDoesNotExposeThoughtContent(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.Mu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "request-1"}
	service.chatRuntimeLocked(service.Core.Snapshot.Session.ID).SetStream(chat.NewVisibleOutputStream("request-1"))
	service.appendMessageLocked("assistant", "", nil)
	service.Mu.Unlock()

	service.appendDelta("request-1", "answer<think>private reasoning</think> done")
	snapshot := service.Snapshot()
	content := snapshot.Conversation[len(snapshot.Conversation)-1].Content
	if content != "answer done" || strings.Contains(content, "think") || strings.Contains(content, "private") {
		t.Fatalf("visible content = %q", content)
	}
}
