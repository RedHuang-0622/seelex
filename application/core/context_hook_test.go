package core

import (
	"context"
	"strings"
	"testing"
)

func TestIterationHookDoesNotTriggerContextControl(t *testing.T) {
	// OnIterationComplete 不再触发 compactTaskContext（压缩决策移交
	// seelectx.ContextController，plan.md §3.5）：上下文控制失败不会在
	// 迭代钩子中产生，迭代保持可用。
	engine := &fakeEngine{history: []EngineMessage{{
		Role: "tool", Content: strings.Repeat("output ", 4000), ContentSet: true,
	}}}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: &fakeRuntime{}, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: fakeSessions{},
	})
	defer service.Shutdown()

	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "inspect", "high", nil, TaskCheckpoint{})
	service.components.tasks.SetTaskStateLocked("task-1", TaskProgressing, "Task is in progress.")
	service.ViewMu.Unlock()

	bridge := NewToolHookBridge()
	bridge.Bind(service)
	if !bridge.Hooks().OnIterationComplete(context.Background(), 0) {
		t.Fatal("iteration must remain available (context control moved to Controller)")
	}
	if got := service.components.context.TakeContextControlFailure("task-1"); got != nil {
		t.Fatalf("no context-control failure expected in iteration hook, got %v", got)
	}
}
