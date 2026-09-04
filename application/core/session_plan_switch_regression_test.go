package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// TestHotAttachKeepsBackgroundPlanProgress 回归：后台会话运行期间 plan 节点
// 事件推进的是协调器投影（planMu）；切回该会话的热挂载必须保留这段进度，
// 不得用 plan 栈旧基线覆盖投影/镜像——否则“切回后视图不新/工具后状态错位”。
func TestHotAttachKeepsBackgroundPlanProgress(t *testing.T) {
	engine := newMultiSessionEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	defer service.Shutdown()
	sessions.setInfos([]SessionInfo{{ID: "sess-a"}, {ID: "sess-b"}})
	engine.register("sess-a")
	engine.register("sess-b")

	if err := service.ResumeSession("sess-a"); err != nil {
		t.Fatalf("resume a: %v", err)
	}
	service.components.tasks.SeedPlanProjection("sess-a", &PlanState{
		Status: PlanPending,
		Nodes:  []PlanNode{{ID: "n1", Status: NodePending}},
	})

	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("resume b: %v", err)
	}
	// a 在后台收到节点完成事件（视图在 b）。
	service.HandlePlanNodeComplete(dto.PlanNodeEvent{
		SessionID: "sess-a", NodeID: "n1", Status: "completed", Output: "done",
	})

	if err := service.ResumeSession("sess-a"); err != nil {
		t.Fatalf("switch back to a: %v", err)
	}
	snapshot := service.Snapshot()
	if snapshot.Runtime.Plan == nil || len(snapshot.Runtime.Plan.Nodes) != 1 {
		t.Fatalf("mirror plan missing after hot attach: %+v", snapshot.Runtime.Plan)
	}
	node := snapshot.Runtime.Plan.Nodes[0]
	if node.Status != NodeCompleted || node.Output != "done" {
		t.Fatalf("mirror plan lost background progress: %+v", node)
	}
	projection := service.components.tasks.PlanProjectionCopy("sess-a")
	if projection == nil || len(projection.Nodes) != 1 || projection.Nodes[0].Status != NodeCompleted {
		t.Fatalf("coordinator projection lost background progress: %+v", projection)
	}
}
