package core

// S0 靶场：跨会话污染回归（会话域重构后必须转绿）。
//
// 场景还原（对应 dev 区会话记录中的真实症状）：
//   1. 会话 A 前台运行，plan 节点同步进 A 自身的 task scope；
//   2. 用户切到会话 B（注册表当前槽换为 B，A 的 scope 分区保留）；
//   3. A 在后台继续跑，plan 生命周期再次同步到 A 自己的 scope。
//
// 目标约束（会话域重构）：
//   - A 的写必须路由到 A 自己的 scope，B 的注册表/工作台不得出现 A 的任务；
//   - A 的 scope 实时一致（TaskSnapshotFor(A) 能看到 A 后台新增的任务）。

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/model"
)

func TestS0BackgroundSessionTaskWriteMustNotPolluteActiveRegistry(t *testing.T) {
	runtime := &fakeRuntime{}
	service := newTestService(t, &fakeEngine{sessionID: "session-a"}, withTestRuntime(runtime))
	defer service.Shutdown()

	planWith := func(nodes ...PlanNode) []model.SessionPlanFrame {
		return []model.SessionPlanFrame{{
			ID:   "p1",
			Plan: &PlanState{Status: PlanRunning, Nodes: nodes},
		}}
	}

	// 1) A 前台：A 的 plan（n1）同步进 A 自身 scope
	service.Deps.Runtime.SwitchSessionTasks("session-a", nil)
	service.Mu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-a"}
	service.components.tasks.RestoreSessionTaskLockedFor("session-a", task_context.RestoredTaskState{
		PlanStack:    planWith(PlanNode{ID: "n1", Label: "A 调研", Status: NodeRunning}),
		ActivePlanID: "p1",
	})
	service.Core.Snapshot.Runtime.Plan = task_context.ActivePlanFromStack(
		planWith(PlanNode{ID: "n1", Label: "A 调研", Status: NodeRunning}), "p1",
	)
	service.Mu.Unlock()
	service.syncTasksFromSourcesFor("session-a")
	if got := runtime.TaskSnapshotFor("session-a"); len(got) != 1 || got[0].Key != "plan:n1" {
		t.Fatalf("seed: A scope records = %+v, want plan:n1", got)
	}

	// 2) 切到 B：注册表当前槽换为 B（A 的 scope 分区保留）
	service.Deps.Runtime.SwitchSessionTasks("session-b", nil)
	service.Mu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-b"}
	service.Mu.Unlock()

	// 3) A 后台继续跑：A 的 plan 新增 n2 并同步到 A 自身 scope
	service.Mu.Lock()
	service.components.tasks.RestoreSessionTaskLockedFor("session-a", task_context.RestoredTaskState{
		PlanStack: planWith(
			PlanNode{ID: "n1", Label: "A 调研", Status: NodeRunning},
			PlanNode{ID: "n2", Label: "A 第二个节点", Status: NodeRunning},
		),
		ActivePlanID: "p1",
	})
	service.Mu.Unlock()
	service.syncTasksFromSourcesFor("session-a")

	// 目标态断言 1：B 的注册表不得出现 A 的 plan 任务
	for _, record := range runtime.TaskSnapshotFor("session-b") {
		if strings.HasPrefix(record.Key, "plan:n") {
			t.Fatalf("cross-session pollution: A plan task %q landed in B registry", record.Key)
		}
	}
	// 目标态断言 2：A 的 scope 应实时一致（能看到后台新增的 plan:n2）
	aRecords := runtime.TaskSnapshotFor("session-a")
	foundN2 := false
	for _, record := range aRecords {
		if record.Key == "plan:n2" {
			foundN2 = true
			break
		}
	}
	if !foundN2 {
		t.Fatalf("A scope lost background task plan:n2; A records = %+v", aRecords)
	}
}
