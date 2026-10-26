package core

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"testing"
)

// ── 子代理树投影（fork 内存态；GUI 树视图数据源）────────────────────

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
	cloned := model.CloneRuntimeState(projection.Runtime)
	cloned.SubAgentTree[0].Children[0].Goal = "mutated"
	if projection.Runtime.SubAgentTree[0].Children[0].Goal != "g" {
		t.Fatal("clone must not mutate the source tree")
	}
}
