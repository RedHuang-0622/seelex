package seelebridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	seetelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
)

// TestPlanLoadPerSessionPolicyIsolation（G1-C 运行路径）：plan_load /
// plan_validate 按执行 ctx 的会话读取自己的策略槽——会话 A 的 MaxNodes=2
// 拒绝 3 节点 DAG，会话 B 的 high 策略放行并把 fork 并发写进加载文档；
// 显式会话没有槽时绝不回退全局默认槽。
func TestPlanLoadPerSessionPolicyIsolation(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	executor := runtime.planExecutor

	plan3 := `{"entry":"a","nodes":{"a":{"input":"1"},"b":{"input":"2"},"c":{"input":"3"}},"edges":{"a":["b"],"b":["c"]}}`
	ctxA := seetelemetry.WithSessionID(context.Background(), "sess-a")
	ctxB := seetelemetry.WithSessionID(context.Background(), "sess-b")

	executor.SetPolicyFor("sess-a", dto.PlanPolicy{Effort: "lite", MaxNodes: 2, RequireSerial: true, MaxForkConcurrency: 1})
	executor.SetPolicyFor("sess-b", dto.PlanPolicy{Effort: "high", MaxForkConcurrency: 3})

	if _, err := runtime.Agent().DirectDispatch(ctxA, "plan_load", plan3); err == nil {
		t.Fatal("sess-a plan_load of 3-node DAG must be rejected by its own MaxNodes=2")
	}
	if _, err := runtime.Agent().DirectDispatch(ctxA, "plan_validate", `{"plan":"`+strings.ReplaceAll(plan3, `"`, `\"`)+`"}`); err == nil {
		t.Fatal("sess-a plan_validate of 3-node DAG must be rejected by its own policy")
	}

	if _, err := runtime.Agent().DirectDispatch(ctxB, "plan_load", plan3); err != nil {
		t.Fatalf("sess-b plan_load rejected: %v", err)
	}
	if got := executor.MaxForkConcurrency(); got != 3 {
		t.Fatalf("loaded plan MaxForkConc = %d, want sess-b high policy fork=3", got)
	}
}

// TestPlanRunPerSessionRunIDAndEventSession（G1-C 运行路径）：plan_run 的
// run ID 登记在 ctx 会话槽；节点事件携带执行绑定会话（sink 写入，不再由
// 订阅者读全局单例补号）。会话 A 运行期间不占用/污染会话 B 的槽。
func TestPlanRunPerSessionRunIDAndEventSession(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	executor := runtime.planExecutor

	projected := make(chan dto.PlanNodeEvent, 16)
	executor.SetPlanNodeCallback(func(ev dto.PlanNodeEvent) { projected <- ev })

	bindingA := dto.PlanBranchBinding{
		SessionID: "sess-a", PlanID: "p-a", EntryNodeID: "gate",
		AccountID: "a1", PrimaryRole: model.RoleAgent, TraceID: "t-a",
	}
	executor.SetBindingFor("sess-a", bindingA)

	gate := &blockingApprovalGate{asked: make(chan struct{}), decision: make(chan string, 1)}
	runtime.SetPlanApprovalGate(gate)

	ctxA := seetelemetry.WithSessionID(context.Background(), "sess-a")
	if _, err := runtime.Agent().DirectDispatch(ctxA, "plan_load", planExecutorApprovePlan); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = runtime.Agent().DirectDispatch(ctxA, "plan_run", `{}`)
	}()

	select {
	case <-gate.asked:
	case <-time.After(10 * time.Second):
		t.Fatal("approval gate was not asked")
	}
	if executor.CurrentRunIDFor("sess-a") == "" {
		t.Fatal("run ID for sess-a must be visible while its plan_run is in progress")
	}
	if executor.CurrentRunIDFor("sess-b") != "" {
		t.Fatal("sess-a plan_run must not register a run in sess-b slot")
	}
	if executor.CurrentRunIDFor("") != "" {
		t.Fatal("sess-a plan_run must not register a run in the legacy default slot")
	}

	gate.decision <- "execute"
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("plan_run did not finish after approval")
	}
	if executor.CurrentRunIDFor("sess-a") != "" {
		t.Fatal("run ID for sess-a must be cleared after plan_run")
	}

	var nodeEvents []dto.PlanNodeEvent
drain:
	for {
		select {
		case ev := <-projected:
			nodeEvents = append(nodeEvents, ev)
		default:
			break drain
		}
	}
	found := false
	for _, ev := range nodeEvents {
		if ev.NodeID == "gate" && ev.SessionID == "sess-a" && ev.PlanID == "p-a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no gate event attributed to sess-a/p-a: %+v", nodeEvents)
	}

	// runner 生命周期/节点事件（经 WithEventSink 入库的 kernel 事件）同样
	// 使用 per-run binding 的定位：EventSink 内任何带 agent.runtime 定位
	// 的事件必须属于执行会话 sess-a，绝不回读默认槽。
	var located int
	for _, ev := range executor.EventSink().Events() {
		sessionID := ""
		for _, location := range ev.Locations {
			if location.Kind == "agent.runtime" {
				sessionID = location.IDs["session_id"]
			}
		}
		if sessionID == "" {
			continue
		}
		located++
		if sessionID != "sess-a" {
			t.Fatalf("sink event located to %q, want sess-a: %#v", sessionID, ev)
		}
	}
	if located == 0 {
		t.Fatal("no sink event carries the executing session location (newPlanRunner binding lost?)")
	}
}

// TestPlanRunExplicitSessionWithoutSlotSkipsDefaultBinding（G1-C）：显式
// 会话没有自己的绑定槽时按零值绑定执行（PlanID 回退 loaded.Entry），不得
// 读取全局默认槽（防后台会话假借视图绑定的会话归属）。
func TestPlanRunExplicitSessionWithoutSlotSkipsDefaultBinding(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	executor := runtime.planExecutor

	executor.SetBindingFor("", dto.PlanBranchBinding{SessionID: "view", PlanID: "p-view", TraceID: "t-view"})
	gate := &blockingApprovalGate{asked: make(chan struct{}), decision: make(chan string, 1)}
	runtime.SetPlanApprovalGate(gate)

	ctx := seetelemetry.WithSessionID(context.Background(), "sess-x")
	if _, err := runtime.Agent().DirectDispatch(ctx, "plan_load", planExecutorApprovePlan); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = runtime.Agent().DirectDispatch(ctx, "plan_run", `{}`)
	}()
	select {
	case <-gate.asked:
	case <-time.After(10 * time.Second):
		t.Fatal("approval gate was not asked")
	}
	gate.decision <- "execute"
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("plan_run did not finish after approval")
	}
	// 默认槽绑定原样保留：sess-x 没有自己的槽时执行不读取/改写它。
	if binding := executor.Binding(); binding.PlanID != "p-view" {
		t.Fatalf("default binding was consumed by session without slot: %+v", binding)
	}
	if executor.CurrentRunIDFor("sess-x") != "" {
		t.Fatalf("run ID for sess-x must be cleared: %q", executor.CurrentRunIDFor("sess-x"))
	}
}

// TestPlanRunNodeProjectionsCarryExecutingSession（G1-C kernel 路径）：带
// 确定性 DAG 的 plan_run 里，queued/running/completed 节点投影（kernel
// 经 sink 入库 + 投影，NodeHook 之外）必须携带执行会话 sid；plan 级事件
// 外的节点事件不允许缺失归属或落到别的会话。
func TestPlanRunNodeProjectionsCarryExecutingSession(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	executor := runtime.planExecutor

	projected := make(chan dto.PlanNodeEvent, 128)
	executor.SetPlanNodeCallback(func(ev dto.PlanNodeEvent) { projected <- ev })

	binding := dto.PlanBranchBinding{
		SessionID: "sess-kernel", PlanID: "p-k", EntryNodeID: "start",
		AccountID: "a1", PrimaryRole: model.RoleAgent, TraceID: "t-k",
	}
	executor.SetBindingFor("sess-kernel", binding)

	ctx := seetelemetry.WithSessionID(context.Background(), "sess-kernel")
	canonical := `{"entry":"start","nodes":{"start":{"input":"start"},"left":{"input":"left"},"right":{"input":"right"},"finish":{"input":"finish"}},"edges":{"start":["left","right"],"left":["finish"],"right":["finish"]}}`
	if _, err := runtime.Agent().DirectDispatch(ctx, "plan_load", canonical); err != nil {
		t.Fatalf("plan_load: %v", err)
	}
	result, err := runtime.Agent().DirectDispatch(ctx, "plan_run", `{}`)
	if err != nil {
		t.Fatalf("plan_run: %v", err)
	}
	var out struct {
		Status    string `json:"status"`
		NodeCount int    `json:"node_count"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil || out.Status != "completed" || out.NodeCount != 4 {
		t.Fatalf("plan_run result = %q err=%v, want completed/4 nodes", result, err)
	}

	var nodeEvents []dto.PlanNodeEvent
drain:
	for {
		select {
		case ev := <-projected:
			if ev.NodeID == "" {
				continue // 计划级投影不要求 sid 字段（归属由状态载体决定）
			}
			nodeEvents = append(nodeEvents, ev)
		default:
			break drain
		}
	}
	seenFinal := map[string]string{}
	for _, ev := range nodeEvents {
		if ev.SessionID != "sess-kernel" {
			t.Fatalf("node %q projection SessionID = %q, want sess-kernel: %+v", ev.NodeID, ev.SessionID, ev)
		}
		if ev.Status == "completed" {
			seenFinal[ev.NodeID] = ev.Status
		}
	}
	for _, id := range []string{"start", "left", "right", "finish"} {
		if seenFinal[id] != "completed" {
			t.Fatalf("node %q never completed in projections: %+v", id, nodeEvents)
		}
	}
}
