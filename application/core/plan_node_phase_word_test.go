package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// plan_node_phase_word_test.go — 「阶段词不是节点状态」的回归。
//
// 现场：子代理节点跑完、产出也拿回来了，收尾时发现自己在 worktree 里留了未提交改动
// （或主工作区被在途改动挡住）。按口径这**不判节点失败**：现场保留、产出末尾加一条显式
// 警告，节点按 Chat 结果算成功。这条警告经 `AppendNodePhase(nodeID, "worktree_unmerged")`
// 进事件流——它和节点状态走的是**同一个 Status 字段**。
//
// 读方先前用一句 `node.Status = PlanNodeStatus(event.Status)` 接住所有节点事件，而那个
// 映射的 default 是 NodePending。于是：一个跑完的节点，在收尾警告到达之后被显成"待开始"，
// 连带被从计划进度里扣掉（Progress 从 1 掉回 0）。
//
// 判据：只有**节点状态词**才推进节点状态；阶段词的原始词留在时间线的流水里（不许丢）。
// 两条路径都断言：后台会话的协调器投影（planMu）与热挂载后的视图镜像。
func TestPlanNodePhaseWordDoesNotOverwriteNodeStatus(t *testing.T) {
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
		Status: PlanRunning,
		Nodes:  []PlanNode{{ID: "n1", Status: NodePending}},
	})
	// 视图切到 b：a 的 plan 事件走**后台投影**路径（协调器投影）。
	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("resume b: %v", err)
	}

	// ① 节点跑完：真状态词，推进节点状态。
	service.HandlePlanNodeComplete(dto.PlanNodeEvent{SessionID: "sess-a", NodeID: "n1", Status: "completed", Output: "产出"})
	// ② 收尾警告：阶段词，不是节点状态，不许覆盖 ①。
	service.HandlePlanNodeComplete(dto.PlanNodeEvent{SessionID: "sess-a", NodeID: "n1", Status: "worktree_unmerged", Output: "现场保留"})

	assertPhaseWordKeptNodeStatus(t, "协调器投影（后台路径）", service.components.tasks.PlanProjectionCopy("sess-a"))

	// 切回 a：镜像取投影副本（这条链是热挂载的既有口径）。
	if err := service.ResumeSession("sess-a"); err != nil {
		t.Fatalf("resume a again: %v", err)
	}
	assertPhaseWordKeptNodeStatus(t, "视图镜像（热挂载）", service.Snapshot().Runtime.Plan)

	// ④ 反面对照：真状态词照样推进（这条判据不能把节点状态钉死在 completed）。
	service.HandlePlanNodeComplete(dto.PlanNodeEvent{SessionID: "sess-a", NodeID: "n1", Status: "failed", Output: "炸了"})
	if got := service.Snapshot().Runtime.Plan.Nodes[0].Status; got != NodeFailed {
		t.Fatalf("节点状态词没能推进节点状态：%v", got)
	}
}

func assertPhaseWordKeptNodeStatus(t *testing.T, name string, plan *PlanState) {
	t.Helper()
	if plan == nil || len(plan.Nodes) != 1 {
		t.Fatalf("%s：plan 缺失", name)
	}
	if got := plan.Nodes[0].Status; got != NodeCompleted {
		t.Fatalf("%s：阶段词覆盖了节点状态 %v（跑完的节点被显成 %v）", name, got, got)
	}
	if plan.Progress != 1 {
		t.Fatalf("%s：进度被阶段词扣掉：%v", name, plan.Progress)
	}
	// ③ 时间线的流水保留事件原始词：阶段这个词本身不许丢（详情页要看"当时发生了什么"）。
	events := plan.Nodes[0].Events
	if len(events) != 2 {
		t.Fatalf("%s：时间线 = %+v，期望两条流水（completed + worktree_unmerged）", name, events)
	}
	if events[1].Status != "worktree_unmerged" || events[1].Output != "现场保留" {
		t.Fatalf("%s：时间线第二条 = %+v，阶段词的原始词必须留在流水里", name, events[1])
	}
}
