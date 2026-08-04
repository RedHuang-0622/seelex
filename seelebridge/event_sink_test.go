package seelebridge

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// newTestRouter 创建 JSON 后端测试 Router（事件库落盘目标）。
func newTestRouter(t testing.TB) *sessionstore.Router {
	t.Helper()
	baseDir := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(baseDir, "session-storage.json"), baseDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

// TestPlanRunExecutionFactsPersistToEventStore 验证双轨事件（slice 8）：
// 事实轨 —— plan_run 执行事实经 event.Sink 落 sessionstore 事件库
// （按 agent.runtime Location 的 session_id 路由）；
// 快照轨 —— 投影订阅（前端 EventHub 快照路径）仍正常投递。
func TestPlanRunExecutionFactsPersistToEventStore(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	eventStore := sessionstore.NewEventStore(newTestRouter(t))
	runtime.SetEventPersister(eventStore.Append)

	projected := make(chan PlanNodeEvent, 16)
	runtime.SetPlanNodeCallback(func(ev PlanNodeEvent) { projected <- ev })
	runtime.SetPlanBranchBinding(PlanBranchBinding{
		SessionID: "session-1", WorkspaceID: "workspace-1", PlanID: "plan-1", EntryNodeID: "start",
	})

	plan := `{"entry":"start","nodes":{"start":{"input":"start"},"finish":{"input":"finish"}},"edges":{"start":["finish"]}}`
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", plan); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`)
	if err != nil {
		t.Fatalf("plan_run failed: %v", err)
	}
	if !strings.Contains(result, `"status":"completed"`) {
		t.Fatalf("plan_run result = %q, want completed", result)
	}

	// 事实轨：执行事实落 sessionstore 事件库（按 session_id 路由）。
	events, err := eventStore.Load(context.Background(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("event store is empty after plan_run")
	}
	statuses := map[frameworkevent.Status]bool{}
	for _, event := range events {
		statuses[event.Status] = true
	}
	if !statuses[frameworkevent.StatusCompleted] {
		t.Fatalf("event store missing completed status: %#v", events)
	}

	// 快照轨：投影订阅仍工作（前端 EventHub 快照路径）。
	select {
	case event := <-projected:
		if event.Status != "completed" && event.Status != "running" {
			t.Fatalf("unexpected projection: %#v", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no projection delivered")
	}
}

// TestEventSinkFailureDoesNotBreakWorkPlanControlFlow 验证 event/README.md
// 语义：Sink 投递失败交给 ErrorHandler，不改写 WorkPlan 控制流结果。
func TestEventSinkFailureDoesNotBreakWorkPlanControlFlow(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	failingPersister := func(context.Context, frameworkevent.Event) error {
		return errors.New("storage unavailable")
	}
	runtime.SetEventPersister(failingPersister)
	var sinkErrors []error
	runtime.SetEventErrorHandler(func(_ context.Context, err error) {
		sinkErrors = append(sinkErrors, err)
	})

	runtime.SetPlanBranchBinding(PlanBranchBinding{
		SessionID: "session-2", WorkspaceID: "workspace-1", PlanID: "plan-2", EntryNodeID: "start",
	})

	plan := `{"entry":"start","nodes":{"start":{"input":"start"},"finish":{"input":"finish"}},"edges":{"start":["finish"]}}`
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", plan); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`)
	if err != nil {
		t.Fatalf("plan_run failed: %v", err)
	}
	if !strings.Contains(result, `"status":"completed"`) {
		t.Fatalf("sink failure changed control flow: result = %q, want completed", result)
	}
	if len(sinkErrors) == 0 {
		t.Fatal("sink failure did not reach ErrorHandler")
	}
}

// TestEventStoreRoutesByAgentRuntimeLocation 验证 EventStore 按
// agent.runtime Location（session_id）路由；缺失时跳过持久化（best-effort）。
func TestEventStoreRoutesByAgentRuntimeLocation(t *testing.T) {
	eventStore := sessionstore.NewEventStore(newTestRouter(t))

	withLocation := frameworkevent.Event{
		ID: "evt-1", Sequence: 1, Source: "workplan.runner",
		Type: frameworkevent.TypeLifecycle, Status: frameworkevent.StatusCompleted,
		Locations: []frameworkevent.Location{{
			Kind: "agent.runtime",
			IDs:  map[string]string{"agent_id": "seelex-main", "session_id": "session-3"},
		}},
	}
	if err := eventStore.Append(context.Background(), withLocation); err != nil {
		t.Fatal(err)
	}
	// 缺失 session_id：跳过持久化，不报错。
	if err := eventStore.Append(context.Background(), frameworkevent.Event{ID: "evt-2", Sequence: 2}); err != nil {
		t.Fatal(err)
	}

	events, err := eventStore.Load(context.Background(), "session-3")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "evt-1" {
		t.Fatalf("loaded events = %#v", events)
	}
	other, err := eventStore.Load(context.Background(), "session-missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("expected empty log, got %#v", other)
	}
}

func TestWorktreePhasesProjectAndPersistWithSessionLocation(t *testing.T) {
	sink := newPlanEventSink()
	var persisted []frameworkevent.Event
	var projected []PlanNodeEvent
	sink.SetPersister(func(_ context.Context, event frameworkevent.Event) error {
		persisted = append(persisted, event)
		return nil
	})
	sink.Subscribe(func(event PlanNodeEvent) { projected = append(projected, event) })
	binding := PlanBranchBinding{SessionID: "session-worktree", PlanID: "plan-1"}
	for _, status := range []string{"worktree_creating", "rebasing", "merging"} {
		sink.AppendPhase(context.Background(), binding, "run-1", "worker", status)
	}

	if len(persisted) != 3 || len(projected) != 3 {
		t.Fatalf("persisted=%d projected=%d, want three worktree phases", len(persisted), len(projected))
	}
	for index, status := range []string{"worktree_creating", "rebasing", "merging"} {
		event := persisted[index]
		if string(event.Status) != status || event.Scope.NodeID != "worker" || event.Scope.RunID != "run-1" {
			t.Fatalf("phase %d event = %#v", index, event)
		}
		if len(event.Locations) != 1 || event.Locations[0].Kind != "agent.runtime" || event.Locations[0].IDs["session_id"] != "session-worktree" {
			t.Fatalf("phase %d location = %#v", index, event.Locations)
		}
		if projected[index].Status != status || projected[index].NodeID != "worker" {
			t.Fatalf("phase %d projection = %#v", index, projected[index])
		}
	}
}
