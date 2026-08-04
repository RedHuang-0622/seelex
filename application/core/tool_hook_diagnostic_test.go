package core

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/session"
)

func TestToolHookDiagnosticObserverMarksCompletionProjection(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	bridge := NewToolHookBridge()
	bridge.Bind(service)
	var stages []string
	bridge.SetDiagnosticObserver(func(event ToolHookDiagnosticEvent) {
		stages = append(stages, event.Stage)
	})
	hooks := bridge.Hooks()
	info := session.ToolCallInfo{Turn: 0, Name: "bash", Arguments: `{"command":"echo ok"}`}
	hooks.OnToolStart(context.Background(), info)
	hooks.OnToolComplete(context.Background(), info)
	want := []string{
		"toolhook.start.enter", "toolhook.start.matched", "toolhook.start.project.start", "toolhook.start.project.done",
		"toolhook.complete.enter", "toolhook.complete.matched", "toolhook.complete.project.start",
		"toolhook.complete.flush.start", "toolhook.complete.flush.done", "toolhook.complete.lock.start", "toolhook.complete.lock.done",
		"toolhook.complete.transcript.start", "toolhook.complete.transcript.done", "toolhook.complete.task.start", "toolhook.complete.task.done",
		"toolhook.complete.runtime.start", "toolhook.complete.runtime.done", "toolhook.complete.unlock.done", "toolhook.complete.event.start", "toolhook.complete.event.done",
		"toolhook.complete.project.done",
	}
	if strings.Join(stages, ",") != strings.Join(want, ",") {
		t.Fatalf("tool hook diagnostic stages = %v, want %v", stages, want)
	}
}
