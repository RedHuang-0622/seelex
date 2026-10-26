package core

import (
	"context"
	"testing"
	"time"

	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/snapshot"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// TestAdaptSubagentConversation 验证会话记录适配：截断（evidence_chars）、
// 条数上限、工具消息携带 Tool 摘要。
func TestSubagentSessionDetailMissingNode(t *testing.T) {
	svc := newTestService(t, &fakeEngine{})
	if _, err := frontendOfTestService(svc).SubagentSessionDetail(""); err == nil {
		t.Fatal("empty node id must be rejected")
	}
	if _, err := frontendOfTestService(svc).SubagentSessionDetail("missing"); err == nil {
		t.Fatal("unknown node must return an error")
	}
}

// TestAdaptSubagentContext 验证上下文快照适配：截断（evidence_chars）、
// 条目上限、空快照 → nil。
func TestSubagentSessionDetailCarriesContext(t *testing.T) {
	engine := &fakeEngine{nodeContext: &snapshot.ContextSnapshot{
		Goal: "audit module", Progress: "60%", MessageCount: 12, TokenEstimate: 900,
		Findings: []string{"found race"}, Decisions: []snapshot.Decision{{What: "use mutex", Why: "race"}},
	}}
	svc := newTestService(t, engine)
	defer frontendOfTestService(svc).Shutdown()
	// 历史 Plan 投影：直接种一个含 worker 节点的只读状态。
	svc.ViewMu.Lock()
	svc.Core.Snapshot.Runtime.Plan = &model.PlanState{
		Status: PlanRunning,
		Nodes:  []model.PlanNode{{ID: "worker", Status: NodePending}},
	}
	svc.ViewMu.Unlock()

	detail, err := frontendOfTestService(svc).SubagentSessionDetail("worker")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Context == nil || detail.Context.Goal != "audit module" || detail.Context.Progress != "60%" {
		t.Fatalf("context = %+v", detail.Context)
	}
	if len(detail.Context.Findings) != 1 || detail.Context.Findings[0] != "found race" {
		t.Fatalf("findings = %+v", detail.Context.Findings)
	}
	if detail.Context.MessageCount != 12 || detail.Context.TokenEstimate != 900 {
		t.Fatalf("context scalars = %+v", detail.Context)
	}
	if detail.Status != NodePending {
		t.Fatalf("status = %q, want pending", detail.Status)
	}
}

// TestSubagentSessionDetailCarriesWorktree 验证失败现场恢复入口：节点
// worktree 现场（路径/分支）随详情载荷返回（P2a 修复——合并失败后用户
// 能知道产出在哪、如何手动恢复）。
func TestSubagentSessionDetailCarriesWorktree(t *testing.T) {
	engine := &fakeEngine{
		nodeContext: &snapshot.ContextSnapshot{Goal: "audit module", MessageCount: 3},
		nodeWorktreeInfoFn: func(nodeID string) (assemble.NodeWorktreeInfo, bool) {
			if nodeID != "worker" {
				return assemble.NodeWorktreeInfo{}, false
			}
			return assemble.NodeWorktreeInfo{
				Path: "G:/tmp/seelex-seelex-worker", Branch: "seelex/worker", MainBranch: "main",
			}, true
		},
	}
	svc := newTestService(t, engine)
	defer frontendOfTestService(svc).Shutdown()
	svc.ViewMu.Lock()
	svc.Core.Snapshot.Runtime.Plan = &model.PlanState{
		Status: PlanRunning,
		Nodes:  []model.PlanNode{{ID: "worker", Status: NodePending}},
	}
	svc.ViewMu.Unlock()

	detail, err := frontendOfTestService(svc).SubagentSessionDetail("worker")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Worktree == nil || detail.Worktree.Path != "G:/tmp/seelex-seelex-worker" ||
		detail.Worktree.Branch != "seelex/worker" || detail.Worktree.MainBranch != "main" {
		t.Fatalf("worktree info = %+v", detail.Worktree)
	}
}

// TestScheduledTasksProjectIntoRuntimeSnapshot 验证周期任务与白名单命令经
// 运行时投影进入 GUI 快照（runtime.changed 增量数据源），且 ScheduleTask /
// CancelScheduledTask 变更入口转发到 Runtime 并刷新投影。
func TestScheduledTasksProjectIntoRuntimeSnapshot(t *testing.T) {
	runtime := &fakeRuntime{
		scheduledTasks: []assemble.ScheduledTaskStatus{{
			ID: "sched_1", Name: "抓职位", Kind: "command",
			Enabled: true, NextRunAt: time.Now().Add(time.Minute),
		}},
	}
	svc := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	defer frontendOfTestService(svc).Shutdown()
	if snapshot := svc.Snapshot(); len(snapshot.Runtime.ScheduledTasks) != 1 {
		t.Fatalf("assembly must project scheduled tasks: %+v", snapshot.Runtime.ScheduledTasks)
	}
	commands := svc.Snapshot().Runtime.ScheduledCommands
	if len(commands) != 1 || commands[0].Key != "auto_get_jobs" {
		t.Fatalf("allowlist must project: %+v", commands)
	}

	created, err := frontendOfTestService(svc).ScheduleTask(context.Background(), assemble.ScheduledTaskSpec{
		Name: "周期提醒", Kind: assemble.ScheduledTaskPrompt,
		Prompt: "检查发布状态", Interval: time.Minute, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "sched_test" {
		t.Fatalf("created = %+v", created)
	}
	if len(runtime.scheduledSpecs) != 1 || runtime.scheduledSpecs[0].Name != "周期提醒" {
		t.Fatalf("schedule not forwarded: %+v", runtime.scheduledSpecs)
	}
	if tasks := svc.Snapshot().Runtime.ScheduledTasks; len(tasks) != 2 {
		t.Fatalf("schedule must refresh runtime projection: %+v", tasks)
	}

	if err := frontendOfTestService(svc).CancelScheduledTask("sched_test"); err != nil {
		t.Fatal(err)
	}
	if tasks := svc.Snapshot().Runtime.ScheduledTasks; len(tasks) != 1 {
		t.Fatalf("cancel must refresh runtime projection: %+v", tasks)
	}
	if len(runtime.cancelledTasks) != 1 || runtime.cancelledTasks[0] != "sched_test" {
		t.Fatalf("cancel not forwarded: %+v", runtime.cancelledTasks)
	}
}

// TestTodoItemsProjectIntoRuntimeSnapshot 验证 todolist 清单经运行时投影
// 进入 GUI 快照（runtime.changed 增量数据源）。
func TestTodoItemsProjectIntoRuntimeSnapshot(t *testing.T) {
	runtime := &fakeRuntime{todoItems: []dto.TodoItem{{Text: "inspect module", Done: true}, {Text: "fix"}}}
	svc := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	defer frontendOfTestService(svc).Shutdown()
	// 装配时已应用运行时投影；工具完成路径再次投影并发布 runtime.changed。
	if snapshot := svc.Snapshot(); len(snapshot.Runtime.TodoItems) != 2 {
		t.Fatalf("assembly must project todo items: %+v", snapshot.Runtime.TodoItems)
	}

	svc.handleToolStart(context.Background(), "todolist_status", "t-1", `{}`)
	svc.handleToolComplete("todolist_status", "t-1", `{}`, nil, 0)
	snapshot := svc.Snapshot()
	if len(snapshot.Runtime.TodoItems) != 2 || !snapshot.Runtime.TodoItems[0].Done || snapshot.Runtime.TodoItems[1].Done {
		t.Fatalf("todo items = %+v", snapshot.Runtime.TodoItems)
	}
}

func strPtr(value string) *string { return &value }
