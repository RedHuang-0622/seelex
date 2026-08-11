package seelebridge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
)

const (
	planExecutorLifecyclePlan = `{"entry":"start","nodes":{"start":{"input":"start"},"finish":{"input":"finish"}},"edges":{"start":["finish"]}}`
	planExecutorApprovePlan   = `{"entry":"gate","nodes":{"gate":{"input":"approve this step","kind":"approve"}},"edges":{}}`
)

func TestPlanExecutorPolicyAndBinding(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	executor := runtime.planExecutor
	if executor == nil {
		t.Fatal("plan executor is not constructed")
	}

	executor.SetPolicy(PlanPolicy{Effort: "test", MaxNodes: 3, MaxForkConcurrency: 2})
	policy := executor.Policy()
	if policy.Effort != "test" || policy.MaxNodes != 3 || policy.MaxForkConcurrency != 2 {
		t.Fatalf("policy = %+v, want test/max-nodes-3/fork-2", policy)
	}

	binding := PlanBranchBinding{
		SessionID: "s1", WorkspaceID: "w1", PlanID: "p1", EntryNodeID: "gate",
		AccountID: "a1", PrimaryRole: RoleAgent, TraceID: "t1",
	}
	executor.SetBinding(binding)
	if got := executor.Binding(); got != binding {
		t.Fatalf("binding = %+v, want %+v", got, binding)
	}
}

func TestPlanExecutorRunLifecycle(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	executor := runtime.planExecutor

	projected := make(chan PlanNodeEvent, 16)
	executor.SetPlanNodeCallback(func(ev PlanNodeEvent) { projected <- ev })

	runtime.SetPlanBranchBinding(PlanBranchBinding{SessionID: "session-1", PlanID: "p-life", WorkspaceID: "w1"})
	runtime.appendNodePhase(context.Background(), "start", "running")
	select {
	case ev := <-projected:
		if ev.NodeID != "start" || ev.Status != "running" || ev.PlanID != "p-life" {
			t.Fatalf("phase event = %+v, want node=start/status=running/plan=p-life", ev)
		}
		if ev.RunID != "" {
			t.Fatalf("phase run ID = %q, want empty before plan_run", ev.RunID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("phase projection was not delivered")
	}

	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planExecutorLifecyclePlan); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`); err != nil {
		t.Fatalf("plan_run failed: %v", err)
	}
	executor.runMu.RLock()
	runID := executor.currentRunID
	executor.runMu.RUnlock()
	if runID != "" {
		t.Fatalf("run ID after plan_run = %q, want cleared", runID)
	}
}

// blockingApprovalGate 阻塞在用户决策上，用于在 plan_run 进行中观察 run ID。
type blockingApprovalGate struct {
	asked    chan struct{}
	decision chan string
	once     sync.Once
}

func (g *blockingApprovalGate) Ask(ctx context.Context, _ approve.Question) (any, error) {
	g.once.Do(func() { close(g.asked) })
	select {
	case choice := <-g.decision:
		return choice, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestPlanExecutorRunIDVisibleDuringRun(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	executor := runtime.planExecutor

	gate := &blockingApprovalGate{asked: make(chan struct{}), decision: make(chan string, 1)}
	runtime.SetPlanApprovalGate(gate)

	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planExecutorApprovePlan); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`)
	}()

	select {
	case <-gate.asked:
	case <-time.After(10 * time.Second):
		t.Fatal("approval gate was not asked")
	}
	executor.runMu.RLock()
	runID := executor.currentRunID
	executor.runMu.RUnlock()
	if runID == "" {
		t.Fatal("run ID is empty while plan_run is in progress")
	}

	gate.decision <- "execute"
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("plan_run did not finish after approval")
	}
	executor.runMu.RLock()
	runID = executor.currentRunID
	executor.runMu.RUnlock()
	if runID != "" {
		t.Fatalf("run ID after plan_run = %q, want cleared", runID)
	}
}

func TestPlanExecutorConcurrentEventProjection(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	executor := runtime.planExecutor

	const goroutines = 16
	const eventsPer = 8
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for j := 0; j < eventsPer; j++ {
				nodeID := fmt.Sprintf("n%d", seed*eventsPer+j)
				nr := &workplanTypes.NodeResult{NodeBase: workplanTypes.NodeBase{
					NodeID: nodeID, Kind: "auto", Status: "completed", Output: nodeID,
					StartedAt: time.Now().Add(-time.Second), EndedAt: time.Now(),
				}}
				executor.events.AppendNodeResult(context.Background(), "p", "r", nr)
				executor.events.AppendPhase(context.Background(), executor.Binding(), "r", nodeID, "running")
			}
		}(i)
	}
	wg.Wait()

	got := executor.events.Events()
	want := goroutines * eventsPer * 2
	if len(got) != want {
		t.Fatalf("event store length = %d, want %d", len(got), want)
	}
}

func TestPlanExecutorPersisterAndErrorHandler(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	executor := runtime.planExecutor

	var persistedMu sync.Mutex
	persisted := make([]frameworkevent.Event, 0)
	executor.SetEventPersister(func(_ context.Context, ev frameworkevent.Event) error {
		persistedMu.Lock()
		persisted = append(persisted, ev)
		persistedMu.Unlock()
		return nil
	})
	if err := executor.events.Append(context.Background(), frameworkevent.Event{
		Type: frameworkevent.TypeLifecycle, Status: frameworkevent.StatusRunning,
	}); err != nil {
		t.Fatal(err)
	}
	persistedMu.Lock()
	count := len(persisted)
	persistedMu.Unlock()
	if count != 1 {
		t.Fatalf("persisted events = %d, want 1", count)
	}

	executor.SetEventPersister(func(_ context.Context, _ frameworkevent.Event) error {
		return errors.New("persist: boom")
	})
	if err := executor.events.Append(context.Background(), frameworkevent.Event{
		Type: frameworkevent.TypeLifecycle, Status: frameworkevent.StatusCompleted,
	}); err == nil {
		t.Fatal("persister failure was swallowed")
	}

	delivered := make(chan error, 1)
	executor.SetEventErrorHandler(func(_ context.Context, err error) {
		select {
		case delivered <- err:
		default:
		}
	})
	handler := executor.currentEventError()
	if handler == nil {
		t.Fatal("event error handler was not stored")
	}
	handler(context.Background(), errors.New("sink: boom"))
	select {
	case err := <-delivered:
		if err == nil || err.Error() != "sink: boom" {
			t.Fatalf("delivered error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event error handler was not invoked")
	}
}
