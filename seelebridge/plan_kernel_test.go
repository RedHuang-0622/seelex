package seelebridge

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
)

// ── DSL → codec 文档 ─────────────────────────────────────────────────

func TestCanonicalPlanDocumentConvertsDSL(t *testing.T) {
	canonical := `{"entry":"inspect","nodes":{"inspect":{"input":"read source"},"verify":{"input":"verify claims","kind":"function"},"report":{"input":"report findings","kind":"deliver"}},"edges":{"inspect":["verify"],"verify":["report"]}}`
	document, err := canonicalPlanDocument(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if document.Version != codec.Version || document.Entry != "inspect" {
		t.Fatalf("document header = %+v", document)
	}
	if len(document.Nodes) != 3 || len(document.Edges) != 2 {
		t.Fatalf("document nodes=%d edges=%d, want 3/2", len(document.Nodes), len(document.Edges))
	}
	byID := map[string]codec.NodeSpec[SeelexNodeInput]{}
	for _, spec := range document.Nodes {
		byID[spec.ID] = spec
	}
	if got := byID["verify"].Input; got.Kind != "function" || got.Input != "verify claims" || got.ID != "verify" {
		t.Fatalf("verify node spec = %+v", got)
	}
	if byID["inspect"].Input.Kind != "" {
		t.Fatalf("kind-less node must stay empty in the document, got %q", byID["inspect"].Input.Kind)
	}
	edges := document.Edges
	if edges[0] != (codec.EdgeSpec{From: "inspect", To: "verify"}) || edges[1] != (codec.EdgeSpec{From: "verify", To: "report"}) {
		t.Fatalf("edges = %+v", edges)
	}
}

func TestCanonicalPlanDocumentRejectsMalformedNode(t *testing.T) {
	canonical := `{"entry":"a","nodes":{"a":123},"edges":{}}`
	if _, err := canonicalPlanDocument(canonical); err == nil {
		t.Fatal("non-object node payload must be rejected")
	}
}

// ── buildNode kind 映射 ──────────────────────────────────────────────

func TestBuildNodeKindMapping(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	gate := &scriptedApprovalGate{decision: "execute"}
	runtime.SetPlanApprovalGate(gate)

	cases := map[string]node.NodeKind{
		"auto":     node.KindAuto,
		"function": node.KindMethod,
		"verify":   node.KindMethod,
		"deliver":  node.KindMethod,
	}
	for kind, wantKind := range cases {
		built, err := runtime.buildNode(codec.NodeSpec[SeelexNodeInput]{
			ID:    "n-" + kind,
			Kind:  kind,
			Input: SeelexNodeInput{ID: "n-" + kind, Input: "work", Kind: kind},
		})
		if err != nil {
			t.Fatalf("kind %q: %v", kind, err)
		}
		described, ok := built.(node.Kinded)
		if !ok || described.Kind() != wantKind {
			t.Fatalf("kind %q: node kind = %v, want %v", kind, described.Kind(), wantKind)
		}
		output, err := built.Run(context.Background(), nil)
		if err != nil || output != "work" {
			t.Fatalf("kind %q: deterministic output = %q, err = %v", kind, output, err)
		}
	}

	// approve：经门控执行
	approveNode, err := runtime.buildNode(codec.NodeSpec[SeelexNodeInput]{
		ID:    "n-approve",
		Kind:  "approve",
		Input: SeelexNodeInput{ID: "n-approve", Input: "approve me", Kind: "approve"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if described, ok := approveNode.(node.Kinded); !ok || described.Kind() != node.KindApprove {
		t.Fatalf("approve node kind = %v", described.Kind())
	}
	if output, err := approveNode.Run(context.Background(), nil); err != nil || output != "approve me" {
		t.Fatalf("approve node output = %q, err = %v", output, err)
	}
	if atomic.LoadInt32(&gate.asked) != 1 {
		t.Fatal("approval gate was not asked")
	}

	// agent：SeelexAgentNode（bridge.NewAgentFactory 子代理包装）
	agentNode, err := runtime.buildNode(codec.NodeSpec[SeelexNodeInput]{
		ID:    "n-agent",
		Kind:  "agent",
		Input: SeelexNodeInput{ID: "n-agent", Input: "think", Kind: "agent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if described, ok := agentNode.(node.Kinded); !ok || described.Kind() != node.KindAgent {
		t.Fatalf("agent node kind = %v, want %v", described.Kind(), node.KindAgent)
	}
	if _, ok := agentNode.(*seenode.AgentNode); !ok {
		t.Fatalf("agent kind must map to SeelexAgentNode, got %T", agentNode)
	}

	// 未知 kind 拒绝
	if _, err := runtime.buildNode(codec.NodeSpec[SeelexNodeInput]{
		ID:    "n-bad",
		Kind:  "magic",
		Input: SeelexNodeInput{ID: "n-bad", Input: "x", Kind: "magic"},
	}); err == nil {
		t.Fatal("unsupported kind must be rejected")
	}
}

// ── plan_load → codec 导入 ───────────────────────────────────────────

func TestPlanLoadImportsCodecPlan(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", `{"entry":"inspect","nodes":{"inspect":{"input":"read source"},"verify":{"input":"verify claims","kind":"verify"},"report":{"input":"report findings","kind":"deliver"}},"edges":{"inspect":["verify"],"verify":["report"]}}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"status":"loaded"`, `"node_count":3`, `"edge_count":2`} {
		if !strings.Contains(result, required) {
			t.Errorf("plan_load result %q is missing %s", result, required)
		}
	}
	loaded, _ := runtime.planExecutor.LoadedPlan()
	if loaded == nil || loaded.Plan == nil {
		t.Fatal("plan_load did not materialize a codec plan")
	}
	nodes := loaded.Plan.AllNodes()
	if len(nodes) != 3 {
		t.Fatalf("materialized nodes = %v", nodes)
	}
	if loaded.Plan.Entry() != "inspect" {
		t.Fatalf("entry = %q", loaded.Plan.Entry())
	}
	if len(loaded.Plan.AllEdges()) != 2 {
		t.Fatalf("materialized edges = %d, want 2", len(loaded.Plan.AllEdges()))
	}
}

func TestPlanLoadRejectsCycleThroughCodec(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	cyclic := `{"entry":"a","nodes":{"a":{"input":"a"},"b":{"input":"b"}},"edges":{"a":["b"],"b":["a"]}}`
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", cyclic); err == nil {
		t.Fatal("cyclic plan must be rejected")
	}
}

func TestPlanLoadRejectsUnknownKind(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	payload := `{"entry":"a","nodes":{"a":{"input":"a","kind":"magic"}},"edges":{}}`
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", payload); err == nil {
		t.Fatal("unsupported kind must be rejected by plan_load")
	}
}

// ── plan_run 执行（确定性 DAG + 事件投影）──────────────────────────

func TestPlanRunExecutesDeterministicDAG(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	plan := `{"entry":"inspect","nodes":{"inspect":{"input":"read source"},"implement":{"input":"make change"},"verify":{"input":"run tests"}},"edges":{"inspect":["implement"],"implement":["verify"]}}`
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", plan); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`)
	if err != nil {
		t.Fatalf("plan_run failed: %v", err)
	}
	var out struct {
		Status    string `json:"status"`
		NodeCount int    `json:"node_count"`
		Nodes     []struct {
			NodeID string `json:"node_id"`
			Status string `json:"status"`
			Output string `json:"output"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("plan_run result %q: %v", result, err)
	}
	if out.Status != "completed" || out.NodeCount != 3 {
		t.Fatalf("plan_run result = %q, want completed with 3 nodes", result)
	}
	byID := map[string]string{}
	for _, n := range out.Nodes {
		byID[n.NodeID] = n.Status
	}
	for _, id := range []string{"inspect", "implement", "verify"} {
		if byID[id] != "completed" {
			t.Fatalf("node %q status = %q, want completed", id, byID[id])
		}
	}
}

func TestPlanRunProjectsNodeAndPlanStatus(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	projected := make(chan PlanNodeEvent, 32)
	runtime.SetPlanNodeCallback(func(ev PlanNodeEvent) {
		projected <- ev
	})

	plan := `{"entry":"start","nodes":{"start":{"input":"start"},"left":{"input":"left"},"right":{"input":"right"},"finish":{"input":"finish"}},"edges":{"start":["left","right"],"left":["finish"],"right":["finish"]}}`
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", plan); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`); err != nil {
		t.Fatalf("plan_run failed: %v", err)
	}

	statuses := map[string][]string{}
	elapsedSeen := map[string]bool{}
	nodeOutput := map[string]string{}
	for {
		select {
		case ev := <-projected:
			statuses[ev.NodeID] = append(statuses[ev.NodeID], ev.Status)
			if ev.NodeID != "" {
				if ev.Elapsed != "" {
					elapsedSeen[ev.NodeID] = true
				}
				if ev.Output != "" {
					nodeOutput[ev.NodeID] = ev.Output
				}
			}
		default:
			goto drained
		}
	}
drained:
	for _, id := range []string{"start", "left", "right", "finish"} {
		// 节点生命周期：queued → running → completed（sink 投影）+ 含 elapsed 的完成事件
		seen := statuses[id]
		if !containsStatus(seen, "queued") || !containsStatus(seen, "running") {
			t.Errorf("node %q projection = %v, want queued and running", id, seen)
		}
		if len(seen) == 0 || seen[len(seen)-1] != "completed" {
			t.Errorf("node %q projection = %v, want final completed", id, seen)
		}
		if !elapsedSeen[id] {
			t.Errorf("node %q never received a completion event with elapsed", id)
		}
		// executor 对节点输出做 ToJSON 归一化（引号包裹），语义与旧 NodeResult 一致
		var decoded string
		if err := json.Unmarshal([]byte(nodeOutput[id]), &decoded); err != nil || decoded != id {
			t.Errorf("node %q projected output = %q, want JSON %q", id, nodeOutput[id], id)
		}
	}
	if statuses[""] == nil || statuses[""][0] != "running" || statuses[""][len(statuses[""])-1] != "completed" {
		t.Errorf("plan projection = %v, want running -> completed", statuses[""])
	}
}

func containsStatus(statuses []string, want string) bool {
	for _, status := range statuses {
		if status == want {
			return true
		}
	}
	return false
}

func TestPlanRunApprovalGateNode(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	gate := &scriptedApprovalGate{decision: "execute"}
	runtime.SetPlanApprovalGate(gate)

	plan := `{"entry":"gate","nodes":{"gate":{"input":"approve this step","kind":"approve"}},"edges":{}}`
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", plan); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`)
	if err != nil {
		t.Fatalf("plan_run failed: %v", err)
	}
	if !strings.Contains(result, `"status":"completed"`) {
		t.Fatalf("approve plan_run result = %q, want completed", result)
	}
	if atomic.LoadInt32(&gate.asked) != 1 {
		t.Fatal("approval gate was not asked during plan_run")
	}
}

func TestPlanRunRejectsWhenNoPlanLoaded(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`); err == nil || !strings.Contains(err.Error(), "no plan is loaded") {
		t.Fatalf("plan_run error = %v, want no-plan notice", err)
	}
}

// ── planEventSink 单元 ───────────────────────────────────────────────

func TestPlanEventSinkAppendAndProjection(t *testing.T) {
	sink := newPlanEventSink()

	var projected []PlanNodeEvent
	sink.Subscribe(func(ev PlanNodeEvent) { projected = append(projected, ev) })

	// 计划级事件 → NodeID 为空
	if err := sink.Append(context.Background(), frameworkevent.Event{
		Type: frameworkevent.TypeLifecycle, Status: frameworkevent.StatusRunning,
		Scope: frameworkevent.Scope{PlanID: "p1", RunID: "r1"},
	}); err != nil {
		t.Fatal(err)
	}
	// 非生命周期事件不投影但入库
	if err := sink.Append(context.Background(), frameworkevent.Event{
		Type: frameworkevent.TypeProgress, Status: frameworkevent.StatusCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	if got := sink.Events(); len(got) != 2 {
		t.Fatalf("event store length = %d, want 2", len(got))
	}
	if len(projected) != 1 || projected[0].NodeID != "" || projected[0].Status != "running" || projected[0].PlanID != "p1" {
		t.Fatalf("plan projection = %+v", projected)
	}

	// 节点完成（NodeHook 路径）→ 含 kind/elapsed
	started := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)
	ended := started.Add(3 * time.Second)
	nr := &workplanTypes.NodeResult{NodeBase: workplanTypes.NodeBase{
		NodeID: "n1", Kind: "auto", Status: "completed", Output: "output-1",
		StartedAt: started, EndedAt: ended,
	}}
	sink.AppendNodeResult(context.Background(), "p1", "r1", nr)
	if len(projected) != 2 {
		t.Fatalf("projection count = %d, want 2", len(projected))
	}
	nodeEvent := projected[1]
	if nodeEvent.NodeID != "n1" || nodeEvent.Status != "completed" || nodeEvent.Output != "output-1" || nodeEvent.Kind != "auto" {
		t.Fatalf("node projection = %+v", nodeEvent)
	}
	if nodeEvent.Elapsed != "3s" {
		t.Fatalf("node elapsed = %q, want 3s", nodeEvent.Elapsed)
	}
	if got := sink.Events(); len(got) != 3 {
		t.Fatalf("event store length = %d, want 3", len(got))
	}
}

func TestPlanEventSinkPersisterReceivesEveryEvent(t *testing.T) {
	sink := newPlanEventSink()
	var persisted []frameworkevent.Event
	sink.SetPersister(func(_ context.Context, ev frameworkevent.Event) error {
		persisted = append(persisted, ev)
		return nil
	})
	_ = sink.Append(context.Background(), frameworkevent.Event{Type: frameworkevent.TypeLifecycle, Status: frameworkevent.StatusRunning})
	started := time.Now().Add(-time.Second)
	nr := &workplanTypes.NodeResult{NodeBase: workplanTypes.NodeBase{
		NodeID: "n1", Kind: "auto", Status: "completed", Output: "out",
		StartedAt: started, EndedAt: time.Now(),
	}}
	sink.AppendNodeResult(context.Background(), "p", "r", nr)
	if len(persisted) != 2 {
		t.Fatalf("persisted = %d, want 2", len(persisted))
	}
}

// ── 测试辅助 ─────────────────────────────────────────────────────────

type scriptedApprovalGate struct {
	decision string
	asked    int32
}

func (g *scriptedApprovalGate) Ask(_ context.Context, _ approve.Question) (any, error) {
	atomic.AddInt32(&g.asked, 1)
	return g.decision, nil
}
