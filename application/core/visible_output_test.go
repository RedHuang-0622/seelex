package core

import (
	"github.com/RedHuang-0622/seelex/backend/engine/react"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"strings"
	"testing"
)

func TestAppendDeltaDoesNotExposeThoughtContent(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer frontendOfTestService(service).Shutdown()
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = model.ChatState{Running: true, RequestID: "request-1"}
	service.sessionUnitLocked(service.Core.Snapshot.Session.ID).SetStream(chat.NewVisibleOutputStream("request-1"))
	service.components.view.AppendMessageLocked("assistant", "", nil)
	service.ViewMu.Unlock()

	service.appendDelta("request-1", "answer<think>private reasoning</think> done")
	snapshot := service.Snapshot()
	content := snapshot.Conversation[len(snapshot.Conversation)-1].Content
	if content != "answer done" || strings.Contains(content, "think") || strings.Contains(content, "private") {
		t.Fatalf("visible content = %q", content)
	}
}
