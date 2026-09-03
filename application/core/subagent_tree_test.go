package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seelplan "github.com/RedHuang-0622/seelex/seelebridge/plan"
)

// ── 子代理树投影（fork 内存态；GUI 树视图数据源）────────────────────

// TestHandlePlanNodeCompleteProjectsSubAgentTree 验证 plan 节点事件把
// fork 子代理树投影刷新进权威 Snapshot（HandlePlanNodeComplete 路径）：
// 前端经 subagent.changed 增量收到完整 Snapshot 后即可渲染树视图。
func TestHandlePlanNodeCompleteProjectsSubAgentTree(t *testing.T) {
	engine := &fakeEngine{subAgentTree: []dto.SubAgentTreeNode{{
		ID: "main", Status: dto.SubAgentRunning,
		Children: []dto.SubAgentTreeNode{{
			ID: "s1", ParentID: "main", Status: dto.SubAgentDone, Goal: "audit", Summary: "done",
		}},
	}}}
	svc := newTestService(t, engine)
	svc.ViewMu.Lock()
	svc.Core.Snapshot.Runtime.Plan = &PlanState{Status: PlanRunning, Nodes: []PlanNode{{ID: "s1", Status: NodePending}}}
	svc.ViewMu.Unlock()

	svc.HandlePlanNodeComplete(dto.PlanNodeEvent{NodeID: "s1", Status: "completed"})

	svc.ViewMu.RLock()
	tree := svc.Core.Snapshot.Runtime.SubAgentTree
	svc.ViewMu.RUnlock()
	if len(tree) != 1 || tree[0].ID != "main" {
		t.Fatalf("subagent tree not projected: %+v", tree)
	}
	if len(tree[0].Children) != 1 || tree[0].Children[0].ID != "s1" || tree[0].Children[0].Status != dto.SubAgentDone {
		t.Fatalf("tree child mismatch: %+v", tree[0].Children)
	}
}

// TestHandlePlanBranchEventProjectsSubAgentTree 验证分支生命周期事件同样
// 刷新子代理树投影（queued/started/failed 路径）。
func TestHandlePlanBranchEventProjectsSubAgentTree(t *testing.T) {
	engine := &fakeEngine{subAgentTree: []dto.SubAgentTreeNode{{
		ID: "main", Status: dto.SubAgentFailed,
		Children: []dto.SubAgentTreeNode{{ID: "s1", ParentID: "main", Status: dto.SubAgentFailed}},
	}}}
	svc := newTestService(t, engine)
	svc.ViewMu.Lock()
	svc.Core.Snapshot.Runtime.Plan = &PlanState{Status: PlanPending, Nodes: []PlanNode{{ID: "s1", Status: NodePending}}}
	svc.ViewMu.Unlock()

	svc.HandlePlanBranchEvent(seelplan.PlanBranchEvent{NodeID: "s1", Type: "failed"})

	svc.ViewMu.RLock()
	tree := svc.Core.Snapshot.Runtime.SubAgentTree
	svc.ViewMu.RUnlock()
	if len(tree) != 1 || tree[0].Status != dto.SubAgentFailed || len(tree[0].Children) != 1 {
		t.Fatalf("tree not projected on branch event: %+v", tree)
	}
}

// TestCollectRuntimeProjectionCarriesSubAgentTree 验证权威 Snapshot 投影
// 携带子代理树（初始快照与 runtime.changed 增量数据源）。
func TestCollectRuntimeProjectionCarriesSubAgentTree(t *testing.T) {
	engine := &fakeEngine{subAgentTree: []dto.SubAgentTreeNode{{
		ID: "main", Status: dto.SubAgentRunning,
		Children: []dto.SubAgentTreeNode{{ID: "s1", ParentID: "main", Status: dto.SubAgentRunning, Goal: "g"}},
	}}}
	svc := newTestService(t, engine)

	projection := svc.collectRuntimeProjection(t.Context())

	if len(projection.Runtime.SubAgentTree) != 1 || projection.Runtime.SubAgentTree[0].Children[0].Goal != "g" {
		t.Fatalf("runtime projection missing subagent tree: %+v", projection.Runtime.SubAgentTree)
	}
	// 克隆契约：投影树的深拷贝独立于引擎数据（改克隆不改引擎）。
	cloned := cloneRuntimeState(projection.Runtime)
	cloned.SubAgentTree[0].Children[0].Goal = "mutated"
	if projection.Runtime.SubAgentTree[0].Children[0].Goal != "g" {
		t.Fatal("clone must not mutate the source tree")
	}
}
