package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// TestAdaptSubagentConversation 验证会话记录适配：截断（evidence_chars）、
// 条数上限、工具消息携带 Tool 摘要。
func TestAdaptSubagentConversation(t *testing.T) {
	long := strings.Repeat("x", Limits().EvidenceChars+100)
	messages := []types.Message{
		{Role: "user", Content: strPtr("goal")},
		{Role: "assistant", Content: strPtr(long)},
		{Role: "assistant", Content: strPtr("calling"), ToolCallID: "t1", Name: "read_file"},
		{Role: "tool", Content: strPtr("file content"), ToolCallID: "t1", Name: "read_file"},
	}
	adapted := adaptSubagentConversation(messages)
	if len(adapted) != 4 {
		t.Fatalf("adapted = %d messages, want 4", len(adapted))
	}
	// 超长单条截断到 evidence_chars（字节）+ 省略号（… 为 3 字节 UTF-8）。
	if len(adapted[1].Content) > Limits().EvidenceChars+3 {
		t.Fatalf("oversized message not truncated: %d", len(adapted[1].Content))
	}
	// 工具消息携带 Tool 摘要（详情弹窗可渲染工具名）。
	if adapted[2].Tool == nil || adapted[2].Tool.Name != "read_file" {
		t.Fatalf("tool message must carry tool summary: %+v", adapted[2])
	}
}

// TestSubagentSessionDetailMissingNode 验证无节点/无会话时的错误路径。
func TestSubagentSessionDetailMissingNode(t *testing.T) {
	svc := newTestService(t, &fakeEngine{})
	if _, err := svc.SubagentSessionDetail(""); err == nil {
		t.Fatal("empty node id must be rejected")
	}
	if _, err := svc.SubagentSessionDetail("missing"); err == nil {
		t.Fatal("unknown node must return an error")
	}
}

// TestAdaptSubagentContext 验证上下文快照适配：截断（evidence_chars）、
// 条目上限、空快照 → nil。
func TestAdaptSubagentContext(t *testing.T) {
	if adapted := adaptSubagentContext(nil); adapted != nil {
		t.Fatalf("nil snapshot must adapt to nil, got %+v", adapted)
	}
	long := strings.Repeat("x", Limits().EvidenceChars+100)
	findings := make([]string, maxSubagentContextItems+5)
	for index := range findings {
		findings[index] = long
	}
	adapted := adaptSubagentContext(&snapshot.ContextSnapshot{
		Goal:          long,
		Progress:      "in progress",
		MessageCount:  42,
		TokenEstimate: 1234,
		Findings:      findings,
		Decisions:     []snapshot.Decision{{What: long, Why: long}},
		Constraints:   []string{long},
		PendingWork:   []string{"work"},
	})
	if adapted == nil {
		t.Fatal("adapt must not return nil for non-nil snapshot")
	}
	if len(adapted.Goal) > Limits().EvidenceChars+3 || len(adapted.Findings) != maxSubagentContextItems {
		t.Fatalf("truncation/limit failed: goal=%d findings=%d", len(adapted.Goal), len(adapted.Findings))
	}
	if adapted.MessageCount != 42 || adapted.TokenEstimate != 1234 || adapted.Progress != "in progress" {
		t.Fatalf("scalar fields = %+v", adapted)
	}
	if len(adapted.Decisions) != 1 || len(adapted.Decisions[0].What) > Limits().EvidenceChars+3 {
		t.Fatalf("decisions adaptation failed: %+v", adapted.Decisions)
	}
	if len(adapted.Constraints) != 1 || len(adapted.PendingWork) != 1 {
		t.Fatalf("list fields = constraints %d pending %d", len(adapted.Constraints), len(adapted.PendingWork))
	}
}

// TestSubagentSessionDetailCarriesContext 验证详情弹窗完整载荷：
// 节点投影 + 会话记录 + 结构化上下文快照（fakeEngine 注入）。
func TestSubagentSessionDetailCarriesContext(t *testing.T) {
	engine := &fakeEngine{nodeContext: &snapshot.ContextSnapshot{
		Goal: "audit module", Progress: "60%", MessageCount: 12, TokenEstimate: 900,
		Findings: []string{"found race"}, Decisions: []snapshot.Decision{{What: "use mutex", Why: "race"}},
	}}
	svc := newTestService(t, engine)
	defer svc.Shutdown()
	// 经 plan_load 播种节点（权威 Snapshot 投影）。
	svc.handleToolStart("plan_load", "load-1", `{"entry":"worker","nodes":{"worker":{"input":"audit module"}},"edges":{}}`)
	svc.handleToolComplete("plan_load", "load-1", `{"status":"loaded"}`, nil, 0)

	detail, err := svc.SubagentSessionDetail("worker")
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
		nodeWorktreeInfoFn: func(nodeID string) (seelebridge.NodeWorktreeInfo, bool) {
			if nodeID != "worker" {
				return seelebridge.NodeWorktreeInfo{}, false
			}
			return seelebridge.NodeWorktreeInfo{
				Path: "G:/tmp/seelex-seelex-worker", Branch: "seelex/worker", MainBranch: "main",
			}, true
		},
	}
	svc := newTestService(t, engine)
	defer svc.Shutdown()
	svc.handleToolStart("plan_load", "load-1", `{"entry":"worker","nodes":{"worker":{"input":"audit module"}},"edges":{}}`)
	svc.handleToolComplete("plan_load", "load-1", `{"status":"loaded"}`, nil, 0)

	detail, err := svc.SubagentSessionDetail("worker")
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
		scheduledTasks: []seelebridge.ScheduledTaskStatus{{
			ID: "sched_1", Name: "抓职位", Kind: "command",
			Enabled: true, NextRunAt: time.Now().Add(time.Minute),
		}},
	}
	svc := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	defer svc.Shutdown()
	if snapshot := svc.Snapshot(); len(snapshot.Runtime.ScheduledTasks) != 1 {
		t.Fatalf("assembly must project scheduled tasks: %+v", snapshot.Runtime.ScheduledTasks)
	}
	commands := svc.Snapshot().Runtime.ScheduledCommands
	if len(commands) != 1 || commands[0].Key != "auto_get_jobs" {
		t.Fatalf("allowlist must project: %+v", commands)
	}

	created, err := svc.ScheduleTask(context.Background(), seelebridge.ScheduledTaskSpec{
		Name: "周期提醒", Kind: seelebridge.ScheduledTaskPrompt,
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

	if err := svc.CancelScheduledTask("sched_test"); err != nil {
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
	defer svc.Shutdown()
	// 装配时已应用运行时投影；工具完成路径再次投影并发布 runtime.changed。
	if snapshot := svc.Snapshot(); len(snapshot.Runtime.TodoItems) != 2 {
		t.Fatalf("assembly must project todo items: %+v", snapshot.Runtime.TodoItems)
	}

	svc.handleToolStart("todolist_status", "t-1", `{}`)
	svc.handleToolComplete("todolist_status", "t-1", `{}`, nil, 0)
	snapshot := svc.Snapshot()
	if len(snapshot.Runtime.TodoItems) != 2 || !snapshot.Runtime.TodoItems[0].Done || snapshot.Runtime.TodoItems[1].Done {
		t.Fatalf("todo items = %+v", snapshot.Runtime.TodoItems)
	}
}

func strPtr(value string) *string { return &value }
