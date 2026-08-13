package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seelplan "github.com/RedHuang-0622/seelex/seelebridge/plan"
)

// ── 工作表格投影（白盒功能测试；注册表为唯一 task 源）────────

func TestBuildWorkTableMapsPlanNodes(t *testing.T) {
	queuedAt := time.Now().Add(-2 * time.Minute)
	completedAt := time.Now().Add(-time.Minute)
	toolAt := time.Now().Add(-30 * time.Second)
	plan := &PlanState{
		Status: PlanRunning,
		Nodes: []PlanNode{
			{
				ID: "n1", Label: "调研", Kind: "auto", Status: NodeCompleted, Output: "完成调研",
				Events: []PlanNodeEventInfo{
					{Status: NodeQueued, At: queuedAt, Output: "queued"},
					{Status: NodeCompleted, At: completedAt, Output: "ok"},
				},
				ToolEvents: []SubagentToolEvent{
					{ID: "t1", NodeID: "n1", Name: "read_file", Status: "success", StartedAt: toolAt, Result: "源码", Duration: 1500 * time.Millisecond},
				},
				Elapsed: "2m",
			},
			{
				ID: "n2", Label: "实现", Status: NodeRunning,
				Children: []PlanNode{{ID: "n2a", Label: "子任务", Status: NodePending}},
			},
		},
		Edges: []seelplan.PlanEdge{{From: "n1", To: "n2"}, {From: "n2", To: "n2a"}},
	}
	tasks := []dto.TaskRecord{
		{ID: "plan:n1", Key: "plan:n1", Phase: "plan", Task: "调研", Status: dto.TaskCompleted, Kind: "plan", SourceID: "n1", Elapsed: "2m"},
		{ID: "plan:n2", Key: "plan:n2", Phase: "plan", Task: "实现", Status: dto.TaskRunning, Kind: "plan", SourceID: "n2", Dependencies: []string{"plan:n1"}},
		{ID: "plan:n2a", Key: "plan:n2a", Phase: "plan", Task: "子任务", Status: dto.TaskPending, Kind: "plan", SourceID: "n2a", Dependencies: []string{"plan:n2"}},
	}

	rows := buildWorkTable(plan, tasks, nil)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	n1, n2, n2a := rows[0], rows[1], rows[2]
	if n1.ID != "plan:n1" || n1.Phase != "plan" || n1.Status != "completed" || n1.Task != "调研" {
		t.Fatalf("n1 row = %+v", n1)
	}
	if n1.Elapsed != "2m" || n1.SourceID != "n1" {
		t.Fatalf("n1 elapsed/source = %+v", n1)
	}
	if len(n2.Dependencies) != 1 || n2.Dependencies[0] != "plan:n1" {
		t.Fatalf("dependency mapping wrong: n2=%v", n2.Dependencies)
	}
	if len(n2a.Dependencies) != 1 || n2a.Dependencies[0] != "plan:n2" {
		t.Fatalf("nested dependency mapping wrong: %v", n2a.Dependencies)
	}
	// trace：注册表打点 + plan 节点事件/工具活动合并（按时间倒序、有界）。
	traceOps := make([]string, 0, len(n1.Trace))
	for _, point := range n1.Trace {
		traceOps = append(traceOps, point.Operation)
	}
	if !containsString(traceOps, "read_file") || !containsString(traceOps, "node.lifecycle") {
		t.Fatalf("n1 trace ops = %v", traceOps)
	}
}

func TestBuildWorkTableTasklistModeMarksCheckNode(t *testing.T) {
	at := time.Now()
	plan := &PlanState{
		Status: PlanCompleted, // 非 running → tasklist 门禁模式
		Nodes: []PlanNode{{
			ID: "n1", Label: "步骤", Status: NodeCompleted,
			Events: []PlanNodeEventInfo{{Status: NodeCompleted, At: at, Output: "ok"}},
		}},
	}
	tasks := []dto.TaskRecord{{ID: "plan:n1", Phase: "plan", Task: "步骤", Status: dto.TaskCompleted, Kind: "plan", SourceID: "n1"}}
	rows := buildWorkTable(plan, tasks, nil)
	ops := make([]string, 0, len(rows[0].Trace))
	for _, point := range rows[0].Trace {
		ops = append(ops, point.Operation)
	}
	if len(rows) != 1 || !containsString(ops, "task_check_node") {
		t.Fatalf("tasklist mode trace = %+v", rows)
	}
}

func TestBuildWorkTableMapsTodoItems(t *testing.T) {
	tasks := []dto.TaskRecord{
		{ID: "todo:0", Phase: "tasklist", Task: "a", Status: dto.TaskPending, Kind: "todo"},
		{ID: "todo:1", Phase: "tasklist", Task: "b", Status: dto.TaskDoing, Kind: "todo"},
		{ID: "todo:2", Phase: "tasklist", Task: "c", Status: dto.TaskCompleted, Kind: "todo"},
		{ID: "todo:3", Phase: "tasklist", Task: "d", Status: dto.TaskCompleted, Kind: "todo"},
	}
	rows := buildWorkTable(nil, tasks, nil)
	if len(rows) != 4 {
		t.Fatalf("rows = %d", len(rows))
	}
	for index, want := range []string{"pending", "doing", "done", "done"} {
		row := rows[index]
		if row.ID != "todo:"+string(rune('0'+index)) {
			t.Fatalf("row[%d].ID = %s", index, row.ID)
		}
		if row.Phase != "tasklist" || row.Status != want || row.Kind != "todo" {
			t.Fatalf("row[%d] = %+v, want status %s", index, row, want)
		}
	}
}

func TestBuildWorkTableMapsSubagentTasks(t *testing.T) {
	startedAt := time.Now().Add(-10 * time.Minute)
	endedAt := time.Now().Add(-5 * time.Minute)
	tasks := []dto.TaskRecord{
		{
			ID: "subagent:s1", Phase: "subagent", Task: "g1", Status: dto.TaskRunning,
			Assignee: "s1", Kind: "subagent", SourceID: "s1", StartedAt: startedAt,
			Trace: []dto.TaskTracePoint{{At: startedAt, Status: "running", Operation: "subagent.lifecycle"}},
		},
		{
			ID: "subagent:s1a", Phase: "subagent", Task: "g1a", Description: "完成", Status: dto.TaskCompleted,
			Assignee: "s1a", Kind: "subagent", SourceID: "s1a", Dependencies: []string{"subagent:s1"},
			StartedAt: startedAt, EndedAt: endedAt,
			Trace: []dto.TaskTracePoint{
				{At: startedAt, Status: "running", Operation: "subagent.lifecycle"},
				{At: endedAt, Status: "done", Operation: "subagent.lifecycle"},
			},
		},
	}
	rows := buildWorkTable(nil, tasks, nil)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	s1, s1a := rows[0], rows[1]
	if s1.ID != "subagent:s1" || s1.Phase != "subagent" || s1.Status != "running" || s1.Assignee != "s1" {
		t.Fatalf("s1 = %+v", s1)
	}
	if len(s1.Dependencies) != 0 {
		t.Fatalf("s1 deps = %v", s1.Dependencies)
	}
	if s1a.Task != "g1a" || s1a.Description != "完成" || s1a.Status != "done" || len(s1a.Dependencies) != 1 || s1a.Dependencies[0] != "subagent:s1" {
		t.Fatalf("s1a = %+v", s1a)
	}
	if len(s1a.Trace) != 2 || s1a.Trace[0].Status != "done" || s1a.Trace[1].Status != "running" {
		t.Fatalf("s1a trace = %+v", s1a.Trace)
	}
}

func TestBuildWorkTableBoundsRowsAndTruncatesEvidence(t *testing.T) {
	tasks := make([]dto.TaskRecord, 0, Limits().WorkTableRows+10)
	for index := 0; index < Limits().WorkTableRows+10; index++ {
		tasks = append(tasks, dto.TaskRecord{
			ID: "todo:" + string(rune('a'+index%26)) + string(rune('0'+index/26)), Phase: "tasklist",
			Task: "t", Status: dto.TaskPending, Kind: "todo",
		})
	}
	rows := buildWorkTable(nil, tasks, nil)
	if len(rows) != Limits().WorkTableRows {
		t.Fatalf("rows = %d, want %d", len(rows), Limits().WorkTableRows)
	}

	long := strings.Repeat("x", Limits().EvidenceChars*2)
	row := buildWorkTable(nil, []dto.TaskRecord{{
		ID: "task:1", Phase: "task", Task: long, Description: long, Kind: "task",
		Trace: []dto.TaskTracePoint{{Status: "pending", Operation: "task.add", Evidence: long}},
	}}, nil)[0]
	if !strings.HasSuffix(row.Description, "…") || len([]rune(row.Description)) != Limits().EvidenceChars+1 {
		t.Fatalf("description truncation = %d runes", len([]rune(row.Description)))
	}
	if !strings.HasSuffix(row.Trace[0].Evidence, "…") {
		t.Fatalf("trace evidence not truncated: %d", len(row.Trace[0].Evidence))
	}
}

// TestUpdateWorkItemStatusTodoThreeStates 走完整 Service 路径：
// 状态更新 → runtime.changed + worktable.changed 增量 → 快照反映三态。
func TestUpdateWorkItemStatusTodoThreeStates(t *testing.T) {
	runtime := &fakeRuntime{todoItems: []dto.TodoItem{
		{Text: "a", Status: dto.TodoItemPending},
		{Text: "b", Status: dto.TodoItemPending},
	}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	subscription := service.Subscribe(16)
	defer subscription.Close()

	if err := service.UpdateWorkItemStatus("todo:0", "doing"); err != nil {
		t.Fatal(err)
	}
	workEvent := waitForWorkTableEvent(t, subscription)
	if len(workEvent.Items) != 2 || workEvent.Items[0].Status != "doing" {
		t.Fatalf("worktable.changed items = %+v", workEvent.Items)
	}
	snapshot := service.Snapshot()
	if snapshot.Runtime.WorkTable[0].Status != "doing" || snapshot.Runtime.TodoItems[0].Status != dto.TodoItemDoing {
		t.Fatalf("snapshot work table = %+v todo = %+v", snapshot.Runtime.WorkTable, snapshot.Runtime.TodoItems)
	}

	if err := service.UpdateWorkItemStatus("todo:1", "done"); err != nil {
		t.Fatal(err)
	}
	workEvent = waitForWorkTableEvent(t, subscription)
	if workEvent.Items[1].Status != "done" {
		t.Fatalf("second worktable.changed = %+v", workEvent.Items)
	}
}

func TestUpdateWorkItemStatusRejectsInvalid(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	subscription := service.Subscribe(8)
	defer subscription.Close()

	if err := service.UpdateWorkItemStatus("todo:99", "doing"); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("out-of-range todo must fail, got %v", err)
	}
	if err := service.UpdateWorkItemStatus("plan:n1", "completed"); err == nil || !strings.Contains(err.Error(), "执行器管理") {
		t.Fatalf("plan status update must be rejected, got %v", err)
	}
	if err := service.UpdateWorkItemStatus("bogus", "doing"); err == nil {
		t.Fatal("malformed id must be rejected")
	}
	// 拒绝路径不得发布 worktable.changed（允许其他生命周期事件，
	// 例如 session catalog 的 snapshot.changed）。
	deadline := time.After(200 * time.Millisecond)
	for {
		select {
		case event := <-subscription.Events:
			if event.Kind == EventWorkTableChanged {
				t.Fatalf("no worktable.changed expected after rejected update, got %s", event.Kind)
			}
		case <-deadline:
			return
		}
	}
}

// TestRefreshWorkTableSnapshotPublishesSubagentRows 验证被动触发：
// 子代理树生命周期（fork 注册/完成）经 observer → RefreshWorkTableSnapshot
// 自动同步 task 注册表并发布 worktable.changed，无需模型调用任何工具。
func TestRefreshWorkTableSnapshotPublishesSubagentRows(t *testing.T) {
	engine := &fakeEngine{}
	engine.mu.Lock()
	engine.subAgentTree = []dto.SubAgentTreeNode{{
		ID: "main",
		Children: []dto.SubAgentTreeNode{{
			ID: "s1", Goal: "分析作者", Status: dto.SubAgentRunning, StartedAt: time.Now(),
		}},
	}}
	engine.mu.Unlock()
	service := newTestService(t, engine)
	subscription := service.Subscribe(8)
	defer subscription.Close()

	service.RefreshWorkTableSnapshot()
	event := waitForWorkTableEvent(t, subscription)
	if len(event.Items) != 1 || event.Items[0].ID != "subagent:s1" || event.Items[0].Status != "running" {
		t.Fatalf("worktable.changed = %+v", event.Items)
	}
	snapshot := service.Snapshot()
	if len(snapshot.Runtime.WorkTable) != 1 || snapshot.Runtime.WorkTable[0].Phase != "subagent" || snapshot.Runtime.WorkTable[0].Assignee != "s1" {
		t.Fatalf("snapshot work table = %+v", snapshot.Runtime.WorkTable)
	}
}

func waitForWorkTableEvent(t testing.TB, subscription Subscription) WorkTableEvent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-subscription.Events:
			if event.Kind != EventWorkTableChanged {
				continue
			}
			var payload WorkTableEvent
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatalf("decode worktable.changed: %v", err)
			}
			return payload
		case <-deadline:
			t.Fatal("timeout waiting for worktable.changed")
		}
	}
}
