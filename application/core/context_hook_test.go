package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type failingContextEngine struct {
	*fakeEngine
	err error
}

func (engine *failingContextEngine) ReplaceHistory(string, []EngineMessage) error {
	return engine.err
}

func TestIterationHookReportsContextControlFailure(t *testing.T) {
	want := errors.New("history replacement failed")
	engine := &failingContextEngine{
		fakeEngine: &fakeEngine{history: []EngineMessage{{
			Role: "tool", Content: strings.Repeat("output ", 4000), ContentSet: true,
		}}},
		err: want,
	}
	service := New(Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: fakeSessions{},
	})
	defer service.Shutdown()

	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "inspect", "high")
	service.setTaskStateLocked("task-1", TaskProgressing, "Task is in progress.")
	service.mu.Unlock()

	bridge := NewToolHookBridge()
	bridge.Bind(service)
	if bridge.Hooks().OnIterationComplete(context.Background(), 0) {
		t.Fatal("iteration should stop after context-control failure")
	}
	if got := service.takeContextControlFailure("task-1"); !errors.Is(got, want) {
		t.Fatalf("failure = %v, want wrapped %v", got, want)
	}
}
