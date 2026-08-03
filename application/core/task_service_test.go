package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// 本文件覆盖 plan.md §3.4 TaskService 语义：
// - 终态校验基于事件投影（PlanProjectionReader），不访问执行器内部；
// - 终态判定前同步 flush 投影（Sink 追加返回后再判定）；
// - 投影未收敛（计划运行中）时 task_complete 拒绝；
// - 任务终态保留最小恢复记录（objective + 排队输入引用），快照不持久化。

func TestTaskCompleteRejectedWhenProjectionNotConverged(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "audit", "high")
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	// 投影未收敛：计划运行中（执行器仍持有 DAG，事件可能滞后）
	service.snapshot.Runtime.Plan = &PlanState{
		Status: PlanRunning,
		Nodes:  []PlanNode{{ID: "inspect", Status: NodeRunning}, {ID: "verify", Status: NodeCompleted}},
	}
	service.mu.Unlock()

	_, err := service.TaskTerminalHandler(taskCompleteTool)(context.Background(), `{"summary":"done","completed_nodes":["inspect","verify"]}`)
	if err == nil || !strings.Contains(err.Error(), "not converged") {
		t.Fatalf("task_complete error = %v, want projection-not-converged rejection", err)
	}
	service.mu.RLock()
	state := service.taskExecution
	service.mu.RUnlock()
	if state.status != taskStatusRunning || state.terminal != nil {
		t.Fatalf("task state must stay running after rejected terminal: %+v", state)
	}
}

func TestTaskCompleteFlushConvergesProjectionBeforeVerdict(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "audit", "high")
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	service.snapshot.Runtime.Plan = &PlanState{
		Status: PlanRunning,
		Nodes:  []PlanNode{{ID: "inspect", Status: NodeRunning}},
	}
	// flush 钩子模拟 Sink 同步写入：追加返回后投影收敛（计划完成、节点全部终态）
	service.taskService.projection = &planProjectionReader{
		serviceState: service.serviceState,
		flush: func(ctx context.Context) error {
			service.mu.Lock()
			plan := service.snapshot.Runtime.Plan
			plan.Status = PlanCompleted
			for index := range plan.Nodes {
				plan.Nodes[index].Status = NodeCompleted
			}
			service.mu.Unlock()
			return nil
		},
	}
	service.mu.Unlock()

	// 判定在 flush 之后执行：若未先 flush，PlanRunning 会被拒绝
	_, err := service.TaskTerminalHandler(taskCompleteTool)(context.Background(), `{"summary":"done","completed_nodes":["inspect"]}`)
	if err != nil {
		t.Fatalf("task_complete after flush should be accepted, got: %v", err)
	}
	service.mu.RLock()
	state := service.taskExecution
	plan := service.snapshot.Runtime.Plan
	service.mu.RUnlock()
	if state.status != taskStatusCompleted || state.terminal == nil || state.terminal.Kind != taskCompleteTool {
		t.Fatalf("terminal state = %+v", state)
	}
	if plan.Status != PlanCompleted || plan.Progress != 1 || plan.Nodes[0].Status != NodeCompleted {
		t.Fatalf("completed plan = %#v", plan)
	}
}

func TestTaskCompleteRejectedWhenProjectionFlushFails(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "audit", "high")
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	service.taskService.projection = &planProjectionReader{
		serviceState: service.serviceState,
		flush: func(ctx context.Context) error {
			return fmt.Errorf("sink unavailable")
		},
	}
	service.mu.Unlock()

	_, err := service.TaskTerminalHandler(taskCompleteTool)(context.Background(), `{"summary":"done"}`)
	if err == nil || !strings.Contains(err.Error(), "plan projection flush failed") {
		t.Fatalf("task_complete error = %v, want flush failure rejection", err)
	}
}

func TestTerminalResumeRecordKeepsObjectiveAndQueuedInputs(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "write report", "high")
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	service.inputQueue = []chatRequest{
		{displayInput: "first follow-up"},
		{displayInput: "second follow-up"},
	}
	service.mu.Unlock()

	// 无 Plan 投影 → 无需校验节点覆盖，直接接受
	result, err := service.TaskTerminalHandler(taskCompleteTool)(context.Background(), `{"summary":"report is ready"}`)
	if err != nil || !strings.Contains(result, `"accepted"`) {
		t.Fatalf("terminal result = %q err=%v", result, err)
	}
	service.mu.RLock()
	resume := service.taskService.ResumeRecord()
	service.mu.RUnlock()
	if resume.TaskID != "task-1" || resume.Objective != "write report" {
		t.Fatalf("resume record = %+v", resume)
	}
	if len(resume.QueuedRefs) != 2 || resume.QueuedRefs[0] != "first follow-up" || resume.QueuedRefs[1] != "second follow-up" {
		t.Fatalf("queued refs = %v, want both queued inputs", resume.QueuedRefs)
	}
}

func TestOnChatEndKeepsResumeRecord(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "prepare a plan", "high")
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	service.inputQueue = []chatRequest{{displayInput: "queued after natural stop"}}
	service.mu.Unlock()

	visible, err := service.taskService.OnChatEnd(context.Background(), ChatEndSummary{RequestID: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	if visible.Status != TaskCompleted || visible.RequestID != "task-1" {
		t.Fatalf("natural terminal task state = %#v", visible)
	}
	service.mu.RLock()
	resume := service.taskService.ResumeRecord()
	service.mu.RUnlock()
	if len(resume.QueuedRefs) != 1 || resume.QueuedRefs[0] != "queued after natural stop" {
		t.Fatalf("resume record = %+v", resume)
	}
}

// ── task_check_node 在途打点（tasklist 门禁）──────────────────────

func TestCheckNodeMarksNodeCompletedInTasklist(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "audit", "high")
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	service.snapshot.Runtime.Plan = &PlanState{
		Status: PlanPending,
		Nodes:  []PlanNode{{ID: "inspect", Label: "inspect the call path", Status: NodePending}, {ID: "verify", Label: "verify the claim", Status: NodePending}},
	}
	service.mu.Unlock()

	result, err := service.TaskTerminalHandler(taskCheckNodeTool)(context.Background(), `{"node_id":"inspect","output":"read controller.go"}`)
	if err != nil {
		t.Fatalf("task_check_node error = %v", err)
	}
	if !strings.Contains(result, `"accepted"`) || !strings.Contains(result, `"node_status":"completed"`) {
		t.Fatalf("check result = %q, want accepted completed", result)
	}
	service.mu.RLock()
	plan := service.snapshot.Runtime.Plan
	state := service.taskExecution
	service.mu.RUnlock()
	if plan.Nodes[0].Status != NodeCompleted || plan.Nodes[1].Status != NodePending {
		t.Fatalf("node statuses = %+v, want inspect completed / verify pending", plan.Nodes)
	}
	if plan.Progress != 0.5 {
		t.Fatalf("progress = %v, want 0.5", plan.Progress)
	}
	// 非终态：任务仍 running、无 terminal，可在途继续
	if state.status != taskStatusRunning || state.terminal != nil {
		t.Fatalf("task must stay running after in-progress check: %+v", state)
	}
	// checkpoint 打点写入（供恢复/上下文摘要消费）
	if checkpoint := state.checkpoints["inspect"]; checkpoint == nil || checkpoint.Status != "completed" || checkpoint.Facts[0] != "read controller.go" {
		t.Fatalf("checkpoint = %+v, want completed with output fact", checkpoint)
	}
	// 打点事件写入节点时间线（详情页入口：`…` 按钮的 events 数据源）
	if len(plan.Nodes[0].Events) != 1 || plan.Nodes[0].Events[0].Status != NodeCompleted || plan.Nodes[0].Events[0].Output != "read controller.go" {
		t.Fatalf("node events = %+v, want one completed event with output", plan.Nodes[0].Events)
	}
}

func TestCheckNodeRejectsUnknownNode(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "audit", "high")
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	service.snapshot.Runtime.Plan = &PlanState{Status: PlanPending, Nodes: []PlanNode{{ID: "inspect", Status: NodePending}}}
	service.mu.Unlock()

	_, err := service.TaskTerminalHandler(taskCheckNodeTool)(context.Background(), `{"node_id":"missing"}`)
	if err == nil || !strings.Contains(err.Error(), `unknown node "missing"`) {
		t.Fatalf("error = %v, want unknown-node rejection", err)
	}
}

func TestCheckNodeRequiresLoadedPlanAndNodeID(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "audit", "high")
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	service.mu.Unlock()

	if _, err := service.TaskTerminalHandler(taskCheckNodeTool)(context.Background(), `{"node_id":"inspect"}`); err == nil || !strings.Contains(err.Error(), "no task structure is loaded") {
		t.Fatalf("without plan error = %v, want no-task-structure rejection", err)
	}
	if _, err := service.TaskTerminalHandler(taskCheckNodeTool)(context.Background(), `{}`); err == nil || !strings.Contains(err.Error(), "node_id is required") {
		t.Fatalf("without node_id error = %v, want required rejection", err)
	}
}

func TestCheckNodeIdempotentAndDoesNotReplayEpoch(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "audit", "high")
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	service.snapshot.Runtime.Plan = &PlanState{Status: PlanPending, Nodes: []PlanNode{{ID: "inspect", Status: NodePending}}}
	service.mu.Unlock()

	handler := service.TaskTerminalHandler(taskCheckNodeTool)
	if _, err := handler(context.Background(), `{"node_id":"inspect"}`); err != nil {
		t.Fatal(err)
	}
	firstEpoch := func() uint64 {
		service.mu.RLock()
		defer service.mu.RUnlock()
		return service.taskExecution.progressEpoch
	}()
	if _, err := handler(context.Background(), `{"node_id":"inspect"}`); err != nil {
		t.Fatalf("re-check must be idempotent, got: %v", err)
	}
	service.mu.RLock()
	epoch := service.taskExecution.progressEpoch
	service.mu.RUnlock()
	if epoch != firstEpoch {
		t.Fatalf("re-check must not advance progress epoch: %d → %d", firstEpoch, epoch)
	}
}

// TestTaskCompleteCoversAlreadyCheckedNodes 验证歧义消除：在途打点已完成的节点
// 计入 task_complete 的覆盖，终态不必重复枚举 completed_nodes。
func TestTaskCompleteCoversAlreadyCheckedNodes(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "audit", "high")
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	service.snapshot.Runtime.Plan = &PlanState{
		Status: PlanPending,
		Nodes:  []PlanNode{{ID: "inspect", Status: NodePending}, {ID: "verify", Status: NodePending}},
	}
	service.mu.Unlock()

	// 节点 1 在途中已打点，节点 2 完成
	if _, err := service.TaskTerminalHandler(taskCheckNodeTool)(context.Background(), `{"node_id":"inspect"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TaskTerminalHandler(taskCheckNodeTool)(context.Background(), `{"node_id":"verify"}`); err != nil {
		t.Fatal(err)
	}
	// 终态只收尾：completed_nodes 可以不重复已打点节点
	_, err := service.TaskTerminalHandler(taskCompleteTool)(context.Background(), `{"summary":"audit done"}`)
	if err != nil {
		t.Fatalf("task_complete should accept already-checked nodes, got: %v", err)
	}
	service.mu.RLock()
	plan := service.snapshot.Runtime.Plan
	state := service.taskExecution
	service.mu.RUnlock()
	if state.status != taskStatusCompleted || state.terminal == nil || state.terminal.Kind != taskCompleteTool {
		t.Fatalf("terminal state = %+v", state)
	}
	if plan.Status != PlanCompleted || plan.Progress != 1 || plan.Nodes[0].Status != NodeCompleted || plan.Nodes[1].Status != NodeCompleted {
		t.Fatalf("completed plan = %#v", plan)
	}
}

// TestTaskCompleteStillRejectsUncheckedNodes 打点流下缺节点仍拒绝：
// 未打点、未枚举的节点必须出现在 completed_nodes。
func TestTaskCompleteStillRejectsUncheckedNodes(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "audit", "high")
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	service.snapshot.Runtime.Plan = &PlanState{
		Status: PlanPending,
		Nodes:  []PlanNode{{ID: "inspect", Status: NodePending}, {ID: "verify", Status: NodePending}},
	}
	service.mu.Unlock()

	if _, err := service.TaskTerminalHandler(taskCheckNodeTool)(context.Background(), `{"node_id":"inspect"}`); err != nil {
		t.Fatal(err)
	}
	_, err := service.TaskTerminalHandler(taskCompleteTool)(context.Background(), `{"summary":"half done"}`)
	if err == nil || !strings.Contains(err.Error(), `authoritative plan node "verify"`) {
		t.Fatalf("task_complete error = %v, want uncovered-node rejection", err)
	}
}

func TestNoProgressBudgetReadsTaskServiceSemanticProgress(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.mu.Lock()
	service.snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.taskExecution = newTaskExecutionState("task-1", "inspect", "high")
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	service.startReActBudgetLocked("task-1", ReActBudget{MaxNoProgressRounds: 2})
	service.mu.Unlock()

	bridge := NewToolHookBridge()
	bridge.Bind(service)
	hooks := bridge.Hooks()
	if !hooks.OnIterationComplete(context.Background(), 0) {
		t.Fatal("first no-progress round should remain available")
	}
	// 一次有进展的工具观测（经 TaskService epoch）重置无进展计数
	service.mu.Lock()
	service.taskService.ObserveTool(ToolObservation{RequestID: "task-1", Name: "read_file", Result: "found call path"})
	service.mu.Unlock()
	if !hooks.OnIterationComplete(context.Background(), 1) {
		t.Fatal("progress should reset the no-progress budget")
	}
	if !hooks.OnIterationComplete(context.Background(), 2) {
		t.Fatal("second no-progress round should remain available")
	}
	if hooks.OnIterationComplete(context.Background(), 3) {
		t.Fatal("third no-progress round should stop the loop")
	}
	if err := service.reactBudgetError("task-1"); err == nil || !strings.Contains(err.Error(), "no observable progress") {
		t.Fatalf("budget error = %v", err)
	}
}
