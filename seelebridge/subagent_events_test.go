package seelebridge

import (
	"context"
	"errors"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	session "github.com/RedHuang-0622/seelex/seelebridge/session"
	"testing"
)

func TestSubagentToolMiddlewareProjectsStartedAndCompleted(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.SetFullAccess(true)
	runtime.RegisterTool("event_probe", "event probe", map[string]interface{}{
		"type": "object",
	}, func(_ context.Context, arguments string) (string, error) {
		return "result:" + arguments, nil
	})

	var events []session.SubagentToolEvent
	runtime.SetSubagentToolCallback(func(event session.SubagentToolEvent) {
		events = append(events, event)
	})

	if _, err := runtime.Agent().DirectDispatch(context.Background(), "event_probe", `{}`); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("main-agent dispatch emitted subagent events: %#v", events)
	}

	ctx := WithNodeScope(context.Background(), NodeScope{NodeID: "node-a", Role: model.RoleSubAgent})
	result, err := runtime.Agent().DirectDispatch(ctx, "event_probe", `{"value":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if result != `result:{"value":1}` {
		t.Fatalf("result = %q", result)
	}
	if len(events) != 2 {
		t.Fatalf("events = %#v, want started and completed", events)
	}
	started, completed := events[0], events[1]
	if started.ID == "" || started.ID != completed.ID || started.NodeID != "node-a" || started.Status != "running" {
		t.Fatalf("started = %#v", started)
	}
	if completed.Status != "success" || completed.Result != result || completed.Duration < 0 {
		t.Fatalf("completed = %#v", completed)
	}
}

func TestSubagentToolMiddlewareProjectsPermissionOrHandlerFailure(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.SetFullAccess(true)
	runtime.RegisterTool("event_failure", "event failure", map[string]interface{}{
		"type": "object",
	}, func(context.Context, string) (string, error) {
		return "", errors.New("probe failed")
	})

	var events []session.SubagentToolEvent
	runtime.SetSubagentToolCallback(func(event session.SubagentToolEvent) {
		events = append(events, event)
	})
	ctx := WithNodeScope(context.Background(), NodeScope{NodeID: "node-f", Role: model.RoleSubAgent})
	if _, err := runtime.Agent().DirectDispatch(ctx, "event_failure", `{}`); err == nil {
		t.Fatal("handler failure was not returned")
	}
	if len(events) != 2 || events[1].Status != "error" || events[1].Error != "probe failed" {
		t.Fatalf("events = %#v", events)
	}
}
