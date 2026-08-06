package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

// ── 子代理树投影（fork 内存态；GUI 树视图数据源）────────────────────

// TestHandlePlanNodeCompleteProjectsSubAgentTree 验证 plan 节点事件把
// fork 子代理树投影刷新进权威 Snapshot（HandlePlanNodeComplete 路径）：
// 前端经 subagent.changed 增量收到完整 Snapshot 后即可渲染树视图。
func TestHandlePlanNodeCompleteProjectsSubAgentTree(t *testing.T) {
	engine := &fakeEngine{subAgentTree: []seelebridge.SubAgentTreeNode{{
		ID: "main", Status: seelebridge.SubAgentRunning,
		Children: []seelebridge.SubAgentTreeNode{{
			ID: "s1", ParentID: "main", Status: seelebridge.SubAgentDone, Goal: "audit", Summary: "done",
		}},
	}}}
	svc := newTestService(t, engine)
	svc.mu.Lock()
	svc.snapshot.Runtime.Plan = &PlanState{Status: PlanRunning, Nodes: []PlanNode{{ID: "s1", Status: NodePending}}}
	svc.mu.Unlock()

	svc.HandlePlanNodeComplete(seelebridge.PlanNodeEvent{NodeID: "s1", Status: "completed"})

	svc.mu.RLock()
	tree := svc.snapshot.Runtime.SubAgentTree
	svc.mu.RUnlock()
	if len(tree) != 1 || tree[0].ID != "main" {
		t.Fatalf("subagent tree not projected: %+v", tree)
	}
	if len(tree[0].Children) != 1 || tree[0].Children[0].ID != "s1" || tree[0].Children[0].Status != seelebridge.SubAgentDone {
		t.Fatalf("tree child mismatch: %+v", tree[0].Children)
	}
}

// TestHandlePlanBranchEventProjectsSubAgentTree 验证分支生命周期事件同样
// 刷新子代理树投影（queued/started/failed 路径）。
func TestHandlePlanBranchEventProjectsSubAgentTree(t *testing.T) {
	engine := &fakeEngine{subAgentTree: []seelebridge.SubAgentTreeNode{{
		ID: "main", Status: seelebridge.SubAgentFailed,
		Children: []seelebridge.SubAgentTreeNode{{ID: "s1", ParentID: "main", Status: seelebridge.SubAgentFailed}},
	}}}
	svc := newTestService(t, engine)
	svc.mu.Lock()
	svc.snapshot.Runtime.Plan = &PlanState{Status: PlanPending, Nodes: []PlanNode{{ID: "s1", Status: NodePending}}}
	svc.mu.Unlock()

	svc.HandlePlanBranchEvent(seelebridge.PlanBranchEvent{NodeID: "s1", Type: "failed"})

	svc.mu.RLock()
	tree := svc.snapshot.Runtime.SubAgentTree
	svc.mu.RUnlock()
	if len(tree) != 1 || tree[0].Status != seelebridge.SubAgentFailed || len(tree[0].Children) != 1 {
		t.Fatalf("tree not projected on branch event: %+v", tree)
	}
}

// TestCollectRuntimeProjectionCarriesSubAgentTree 验证权威 Snapshot 投影
// 携带子代理树（初始快照与 runtime.changed 增量数据源）。
func TestCollectRuntimeProjectionCarriesSubAgentTree(t *testing.T) {
	engine := &fakeEngine{subAgentTree: []seelebridge.SubAgentTreeNode{{
		ID: "main", Status: seelebridge.SubAgentRunning,
		Children: []seelebridge.SubAgentTreeNode{{ID: "s1", ParentID: "main", Status: seelebridge.SubAgentRunning, Goal: "g"}},
	}}}
	svc := newTestService(t, engine)

	projection := svc.collectRuntimeProjection(t.Context())

	if len(projection.runtime.SubAgentTree) != 1 || projection.runtime.SubAgentTree[0].Children[0].Goal != "g" {
		t.Fatalf("runtime projection missing subagent tree: %+v", projection.runtime.SubAgentTree)
	}
	// 克隆契约：投影树的深拷贝独立于引擎数据（改克隆不改引擎）。
	cloned := cloneRuntimeState(projection.runtime)
	cloned.SubAgentTree[0].Children[0].Goal = "mutated"
	if projection.runtime.SubAgentTree[0].Children[0].Goal != "g" {
		t.Fatal("clone must not mutate the source tree")
	}
}
