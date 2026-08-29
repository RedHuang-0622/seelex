package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/subagent_view"
	seelplan "github.com/RedHuang-0622/seelex/seelebridge/plan"
	seelsession "github.com/RedHuang-0622/seelex/seelebridge/session"
)

func TestPlanRunJSONFailureOpensRecoveryInteraction(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.handleToolStart(context.Background(), "plan_load", "load-1", `{"entry":"build","nodes":{"build":{"input":"build it"}},"edges":{}}`)
	service.handleToolComplete("plan_load", "load-1", `{"status":"loaded"}`, nil, 0)

	service.handleToolStart(context.Background(), "plan_run", "run-1", `{}`)
	service.handleToolComplete("plan_run", "run-1", `{"status":"failed","error":"node \"build\": failed"}`, nil, 0)

	snapshot := service.Snapshot()
	if snapshot.Runtime.Plan == nil || snapshot.Runtime.Plan.Status != PlanFailed {
		t.Fatalf("plan = %+v, want failed", snapshot.Runtime.Plan)
	}
	if snapshot.Runtime.Plan.Nodes[0].Status != NodeFailed {
		t.Fatalf("node status = %q, want %q", snapshot.Runtime.Plan.Nodes[0].Status, NodeFailed)
	}
	if snapshot.Interaction == nil || snapshot.Interaction.Kind != "plan_retry" {
		t.Fatalf("interaction = %+v, want plan_retry", snapshot.Interaction)
	}
}

func TestPlanRunToolErrorDoesNotDeadlock(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.handleToolStart(context.Background(), "plan_load", "load-1", `{"entry":"build","nodes":{"build":{"input":"build it"}},"edges":{}}`)
	service.handleToolComplete("plan_load", "load-1", `{"status":"loaded"}`, nil, 0)

	done := make(chan struct{})
	go func() {
		service.handleToolStart(context.Background(), "plan_run", "run-1", `{}`)
		service.handleToolComplete("plan_run", "run-1", "", errors.New(`node "build": interrupted`), 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("plan_run tool error deadlocked")
	}
	if interaction := service.Snapshot().Interaction; interaction == nil || interaction.Kind != "plan_retry" {
		t.Fatalf("interaction = %+v, want plan_retry", interaction)
	}
}

func TestResolvePlanFailureReplansWithoutRunningReplacement(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{replanResult: dto.PlanPreflight{
		Arguments: `{"entry":"recover","nodes":{"recover":{"input":"diagnose the failed build"}},"edges":{}}`,
		Result:    `{"status":"loaded","node_count":1}`,
	}}
	service := newTestService(t, engine, withTestRuntime(runtime))

	service.Mu.Lock()
	service.appendMessageLocked("user", "build and verify the release", nil)
	service.Mu.Unlock()
	service.handleToolStart(context.Background(), "plan_load", "load-1", `{"entry":"build","nodes":{"build":{"input":"build release"}},"edges":{}}`)
	service.handleToolComplete("plan_load", "load-1", `{"status":"loaded"}`, nil, 0)
	service.handleToolStart(context.Background(), "plan_run", "run-1", `{}`)
	service.handleToolComplete("plan_run", "run-1", `{"status":"failed","error":"node \"build\": compiler failed"}`, nil, 0)

	interaction := service.Snapshot().Interaction
	if interaction == nil {
		t.Fatal("expected failed plan interaction")
	}
	if err := service.ResolveInteraction(context.Background(), interaction.ID, "replan"); err != nil {
		t.Fatal(err)
	}
	if len(runtime.replans) != 1 {
		t.Fatalf("replan calls = %d, want 1", len(runtime.replans))
	}
	request := runtime.replans[0]
	if request.Objective != "build and verify the release" || !strings.Contains(request.PreviousPlan, `"build"`) {
		t.Fatalf("replan request lost task or plan: %+v", request)
	}
	if !strings.Contains(request.Failure, "compiler failed") || !strings.Contains(request.Evidence, "node=build status=failed") {
		t.Fatalf("replan request lost failure evidence: %+v", request)
	}
	snapshot := service.Snapshot()
	if snapshot.Interaction != nil {
		t.Fatalf("interaction was not closed: %+v", snapshot.Interaction)
	}
	if snapshot.Runtime.Plan == nil || snapshot.Runtime.Plan.EntryNodeID != "recover" || snapshot.Runtime.Plan.Status != PlanPending {
		t.Fatalf("replacement plan = %+v", snapshot.Runtime.Plan)
	}
	if engine.lastInput != "" {
		t.Fatalf("replan unexpectedly entered ChatStream with %q", engine.lastInput)
	}
}

func TestResolvePlanFailureKeepsInteractionWhenReplanFails(t *testing.T) {
	runtime := &fakeRuntime{replanErr: errors.New("planner unavailable")}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	service.handleToolStart(context.Background(), "plan_load", "load-1", `{"entry":"build","nodes":{"build":{"input":"build release"}},"edges":{}}`)
	service.handleToolComplete("plan_load", "load-1", `{"status":"loaded"}`, nil, 0)
	service.handleToolStart(context.Background(), "plan_run", "run-1", `{}`)
	service.handleToolComplete("plan_run", "run-1", `{"status":"failed","error":"node \"build\": compiler failed"}`, nil, 0)

	interaction := service.Snapshot().Interaction
	if interaction == nil {
		t.Fatal("expected failed plan interaction")
	}
	if err := service.ResolveInteraction(context.Background(), interaction.ID, "replan"); err == nil || !strings.Contains(err.Error(), "planner unavailable") {
		t.Fatalf("replan error = %v, want planner unavailable", err)
	}
	if current := service.Snapshot().Interaction; current == nil || current.ID != interaction.ID {
		t.Fatalf("failed replan closed recovery interaction: %+v", current)
	}
}

func TestResolvePlanFailureStopsAfterPlanChainReplanLimit(t *testing.T) {
	runtime := &fakeRuntime{replanResult: dto.PlanPreflight{
		Arguments: `{"entry":"recover","nodes":{"recover":{"input":"diagnose"}},"edges":{}}`,
		Result:    `{"status":"loaded","node_count":1}`,
	}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	service.handleToolStart(context.Background(), "plan_load", "load-1", `{"entry":"build","nodes":{"build":{"input":"build"}},"edges":{}}`)
	service.handleToolComplete("plan_load", "load-1", `{"status":"loaded"}`, nil, 0)
	for attempt := 0; attempt < Limits().MaxReplansPerPlanChain; attempt++ {
		service.handleToolStart(context.Background(), "plan_run", fmt.Sprintf("run-%d", attempt), `{}`)
		service.handleToolComplete("plan_run", fmt.Sprintf("run-%d", attempt), `{"status":"failed","error":"node \"recover\": failed"}`, nil, 0)
		interaction := service.Snapshot().Interaction
		if interaction == nil {
			t.Fatalf("attempt %d did not open recovery interaction", attempt)
		}
		if err := service.ResolveInteraction(context.Background(), interaction.ID, "replan"); err != nil {
			t.Fatalf("attempt %d replan: %v", attempt, err)
		}
	}
	service.handleToolStart(context.Background(), "plan_run", "run-limit", `{}`)
	service.handleToolComplete("plan_run", "run-limit", `{"status":"failed","error":"node \"recover\": failed"}`, nil, 0)
	interaction := service.Snapshot().Interaction
	if interaction == nil {
		t.Fatal("expected recovery interaction after limit")
	}
	if err := service.ResolveInteraction(context.Background(), interaction.ID, "replan"); err == nil || !strings.Contains(err.Error(), "recovery limit") {
		t.Fatalf("limit error = %v", err)
	}
	if len(runtime.replans) != Limits().MaxReplansPerPlanChain {
		t.Fatalf("replan calls = %d, want %d", len(runtime.replans), Limits().MaxReplansPerPlanChain)
	}
	if plan := service.Snapshot().Runtime.Plan; plan == nil || plan.ReplanCount != Limits().MaxReplansPerPlanChain {
		t.Fatalf("plan replan count = %+v", plan)
	}
}

func TestRuntimeSnapshotIncludesReplanMonitor(t *testing.T) {
	runtime := &fakeRuntime{replanMetrics: dto.ReplanMetrics{
		InFlight: 1, ConcurrentLimit: 2, WindowAttempts: 3, WindowLimit: 6,
		Accepted: 3, Succeeded: 2, Failed: 1, Rejected: 4, DuplicateRejected: 1, ProviderRequests: 5,
	}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	projection := service.collectRuntimeProjection(context.Background())
	service.Mu.Lock()
	service.applyRuntimeProjectionLocked(projection)
	service.Mu.Unlock()
	monitor := service.Snapshot().Runtime.Replan
	if monitor.InFlight != 1 || monitor.WindowAttempts != 3 || monitor.Rejected != 4 || monitor.ProviderRequests != 5 {
		t.Fatalf("replan monitor = %+v", monitor)
	}
}

func TestNormalizePlanToolCallInfoUsesCanonicalAdapterJSON(t *testing.T) {
	info := session.ToolCallInfo{Name: "plan_load", Arguments: `{"entry":"inspect","nodes":[{"id":"inspect","input":"inspect"},{"id":"report","input":"report"}],"edges":[{"from":"inspect","to":"report"}]}`}
	normalized := normalizePlanToolCallInfo(info)
	if normalized.Arguments == info.Arguments || !strings.Contains(normalized.Arguments, `"nodes":{"inspect"`) || !strings.Contains(normalized.Arguments, `"edges":{"inspect":["report"]}`) {
		t.Fatalf("normalized plan args = %q", normalized.Arguments)
	}
	invalid := session.ToolCallInfo{Name: "plan_load", Arguments: `{"entry":"inspect","nodes":[],"edges":[]}`}
	if got := normalizePlanToolCallInfo(invalid); got.Arguments != invalid.Arguments {
		t.Fatalf("invalid plan arguments must remain visible: %q", got.Arguments)
	}
}

func TestHandlePlanBranchEventUpdatesLifecycleAndRuntime(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.handleToolStart(context.Background(), "plan_load", "load-1", `{"entry":"start","nodes":{"start":{"input":"start"},"left":{"input":"left"}},"edges":{"start":["left"]}}`)

	subscription := service.Subscribe(8)
	defer subscription.Close()
	service.HandlePlanBranchEvent(seelplan.PlanBranchEvent{Type: "queued", BranchID: "left", NodeID: "left"})
	service.HandlePlanBranchEvent(seelplan.PlanBranchEvent{Type: "started", BranchID: "left", NodeID: "left"})
	service.HandlePlanBranchEvent(seelplan.PlanBranchEvent{Type: "completed", BranchID: "left", NodeID: "left"})

	snapshot := service.Snapshot()
	if snapshot.Runtime.Plan == nil || snapshot.Runtime.Plan.Status != PlanRunning {
		t.Fatalf("plan status = %+v, want running", snapshot.Runtime.Plan)
	}
	if snapshot.Runtime.Plan.Nodes[0].Status != NodePending || snapshot.Runtime.Plan.Nodes[1].Status != NodeCompleted {
		t.Fatalf("node statuses = %+v", snapshot.Runtime.Plan.Nodes)
	}
	if snapshot.Runtime.Plan.Progress != 0.5 {
		t.Fatalf("progress = %v, want 0.5", snapshot.Runtime.Plan.Progress)
	}
	seen := 0
	deadline := time.After(time.Second)
	for seen < 3 {
		var event Event
		select {
		case event = <-subscription.Events:
		case <-deadline:
			t.Fatalf("received %d subagent.changed events, want 3", seen)
		}
		if event.Kind != EventSubagentChanged {
			continue
		}
		var payload SubagentEvent
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.NodeID != "left" || payload.Node.ID != "left" {
			t.Fatalf("event %d payload = %#v", seen, payload)
		}
		seen++
	}

	service.HandlePlanBranchEvent(seelplan.PlanBranchEvent{Type: "panicked", BranchID: "start", NodeID: "start"})
	snapshot = service.Snapshot()
	if snapshot.Runtime.Plan.Status != PlanFailed || snapshot.Runtime.Plan.Nodes[0].Status != NodePanicked {
		t.Fatalf("panic state = %+v", snapshot.Runtime.Plan)
	}
}

func TestHandleSubagentToolEventProjectsBoundedIncrementals(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.handleToolStart(context.Background(), "plan_load", "load-1", `{"entry":"start","nodes":{"start":{"input":"start"},"worker":{"input":"worker"}},"edges":{"start":["worker"]}}`)

	// 事件流现包含 worktable.changed（CSP 汇聚，异步到达）：订阅缓冲调大，
	// 避免杂散生命周期事件触发 deliver 排空丢弃本测试关注的工具事件。
	subscription := service.Subscribe(16)
	defer subscription.Close()
	long := strings.Repeat("x", Limits().EvidenceChars+20)
	started := seelsession.SubagentToolEvent{
		ID: "subtool-1", NodeID: "worker", Name: "read_file", Arguments: long,
		Status: "running", StartedAt: time.Now(),
	}
	service.HandleSubagentToolEvent(started)
	completed := started
	completed.Status = "success"
	completed.Result = long
	completed.Duration = time.Second
	service.HandleSubagentToolEvent(completed)

	snapshot := service.Snapshot()
	node := subagent_view.FindPlanNodeByID(snapshot.Runtime.Plan.Nodes, "worker")
	if node == nil || len(node.ToolEvents) != 1 {
		t.Fatalf("worker tool events = %#v", node)
	}
	if node.ToolEvents[0].Status != "success" || len(node.ToolEvents[0].Arguments) > Limits().EvidenceChars+3 || len(node.ToolEvents[0].Result) > Limits().EvidenceChars+3 {
		t.Fatalf("projected tool event = %#v", node.ToolEvents[0])
	}

	var first, second Event
	deadline := time.After(time.Second)
	for first.Kind == "" || second.Kind == "" {
		select {
		case received := <-subscription.Events:
			switch received.Kind {
			case EventSubagentToolStarted:
				first = received
			case EventSubagentToolCompleted:
				second = received
			}
		case <-deadline:
			t.Fatalf("did not receive subagent tool events; started=%q completed=%q", first.Kind, second.Kind)
		}
	}
	var payload SubagentToolEvent
	if err := json.Unmarshal(second.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID != "subtool-1" || payload.NodeID != "worker" || payload.Status != "success" {
		t.Fatalf("completed payload = %#v", payload)
	}
}

func TestToolHookBridgeAssignsUniqueStableIDs(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	bridge := NewToolHookBridge()
	bridge.Bind(service)
	hooks := bridge.Hooks()
	info := session.ToolCallInfo{Turn: 1, Name: "read", Arguments: `{"path":"a"}`}

	hooks.OnToolStart(context.Background(), info)
	hooks.OnToolComplete(context.Background(), info)
	hooks.OnToolStart(context.Background(), info)
	hooks.OnToolComplete(context.Background(), info)

	ids := make([]string, 0, 2)
	for _, message := range service.Snapshot().Conversation {
		if message.Role == "tool" && message.Tool != nil {
			ids = append(ids, message.Tool.ID)
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("tool IDs = %v, want two unique IDs", ids)
	}
}
