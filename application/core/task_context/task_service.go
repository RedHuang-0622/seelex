package task_context

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/model"
)

// PlanProjectionReader 是 TaskService 的 Plan 投影读取面。PlanState 由事件
// 投影（event.Sink → HandlePlanNodeComplete）累积而来；TaskService 只消费
// 它，不访问 workplan 执行器内部状态。
type PlanProjectionReader interface {
	AllNodes() []string
	NodeStatus(nodeID string) model.NodeStatus
	PlanStatus() model.PlanStatus
	Converged() bool
	Flush(ctx context.Context) error
	// Plan 返回当前 plan 投影的只读深拷贝（TaskService 终态判定用；协调器
	// 投影缺失时回退视图镜像，保证测试直设 Snapshot 的旧路径仍可用）。
	Plan() *model.PlanState
}

// planProjectionReader 是 PlanProjectionReader 的默认实现：读取由事件投影
// 累积的 PlanState（snapshot.Runtime.Plan）。当前接线中 workplan 事件在
// Append 内同步入库并投影，生产 Flush 为空操作；flush 钩子保留给测试注入。
type planProjectionReader struct {
	c         *Coordinator
	core      *state.Core
	sessionID string
	flush     func(context.Context) error
}

func (r *planProjectionReader) plan() *model.PlanState {
	if r == nil {
		return nil
	}
	if r.c != nil {
		if plan := r.c.PlanProjectionCopy(r.sessionID); plan != nil {
			return plan
		}
	}
	if r.core == nil {
		return nil
	}
	// 回退：测试/迁移期直设 Snapshot.Runtime.Plan 的路径（调用方持有
	// Core.ViewMu 时读取镜像）。
	return model.CloneRuntimeState(model.RuntimeState{Plan: r.core.Snapshot.Runtime.Plan}).Plan
}

func (r *planProjectionReader) AllNodes() []string {
	plan := r.plan()
	if plan == nil {
		return nil
	}
	nodes := make([]string, 0, len(plan.Nodes))
	for _, node := range plan.Nodes {
		nodes = append(nodes, node.ID)
	}
	return nodes
}

func (r *planProjectionReader) NodeStatus(nodeID string) model.NodeStatus {
	plan := r.plan()
	if plan == nil {
		return model.NodePending
	}
	for _, node := range plan.Nodes {
		if node.ID == nodeID {
			return node.Status
		}
	}
	return model.NodePending
}

func (r *planProjectionReader) PlanStatus() model.PlanStatus {
	if plan := r.plan(); plan != nil {
		return plan.Status
	}
	return ""
}

func (r *planProjectionReader) Converged() bool {
	plan := r.plan()
	if plan == nil {
		return true
	}
	return plan.Status != model.PlanRunning
}

func (r *planProjectionReader) Plan() *model.PlanState {
	return r.plan()
}

func (r *planProjectionReader) Flush(ctx context.Context) error {
	if r == nil || r.flush == nil {
		return nil
	}
	return r.flush(ctx)
}

// ToolObservation 是一次工具执行的观测（LoopHooks.OnToolComplete 路径）。
type ToolObservation struct {
	RequestID string
	Name      string
	Result    string
	Err       error
}

// PlanEvent 是一次 plan 事件投影的观测（节点 queued/running/终态）。
type PlanEvent struct {
	NodeID    string
	Status    string
	Output    string
	Failure   string
	Objective string
}

// ModelOutput 是一次模型回复的观测（自然终态判定输入）。
type ModelOutput struct {
	RequestID string
	Reply     string
	Err       error
}

// TaskResumeRecord 是任务终态后保留的最小恢复记录（objective + 排队输入
// 引用），供 TaskFrame 与事件投影在 slice 7 落库。
type TaskResumeRecord struct {
	TaskID     string
	Objective  string
	QueuedRefs []string
}

// ChatEndSummary 是 OnChatEnd 的输入：会话结果 + 排队输入引用。
type ChatEndSummary struct {
	RequestID  string
	Reply      string
	QueuedRefs []string
}

// terminalToolHandlers 是三个终态工具的应用入口。
type terminalToolHandlers map[string]func(context.Context, taskTerminal) (string, error)

// TaskService 拥有单个任务的功能打点快照与终态判定。每个任务（含排队输入
// 续接任务）构造一个实例；任务终态后不再演化，由下一任务替换。它只持有
// 共享内核（锁 + 只读 Snapshot）与任务状态，不接触其它域。
type TaskService struct {
	*state.Core
	sessionID    string
	tasks        *Coordinator
	state        *TaskExecutionState
	projection   PlanProjectionReader
	terminals    terminalToolHandlers
	modelOutput  ModelOutput
	resumeRecord TaskResumeRecord
	// lastTaskState 是本任务最近一次可见状态值（F 镜像收口：task 域不再直写
	// Core.Snapshot.Task；由根调用方在 Core.ViewMu 段把权威 TaskStateFor
	// 镜像进 Snapshot，见 chat.go runChat 收尾）。
	lastTaskState *model.TaskState
	queueRefs     func() []string
}

// newTaskService 构造当前任务的 TaskService。state 为 nil 时表示无活跃任务。
// sessionID 是该任务归属会话（多会话并行时用于快照写入守卫与状态路由）。
func newTaskService(sessionID string, core *state.Core, tasks *Coordinator, taskState *TaskExecutionState, queueRefs func() []string) *TaskService {
	service := &TaskService{
		Core:       core,
		sessionID:  sessionID,
		tasks:      tasks,
		state:      taskState,
		projection: &planProjectionReader{c: tasks, core: core, sessionID: sessionID},
		queueRefs:  queueRefs,
	}
	service.terminals = terminalToolHandlers{
		ToolComplete:          service.applyCompleteLocked,
		ToolCheckNode:         service.applyCheckNodeLocked,
		ToolFailed:            service.applyFailedLocked,
		ToolNeedsUserDecision: service.applyDecisionLocked,
	}
	return service
}

// SemanticProgress 返回任务的语义进展计数（epoch）。
func (s *TaskService) SemanticProgress(requestID string) (uint64, bool) {
	if s == nil || s.state == nil || s.state.RequestID != requestID {
		return 0, false
	}
	return s.state.ProgressEpoch, true
}

// ResumeRecord 返回任务终态时保留的最小恢复记录。
func (s *TaskService) ResumeRecord() TaskResumeRecord {
	if s == nil {
		return TaskResumeRecord{}
	}
	return s.resumeRecord
}

// ObserveTool 记录一次工具执行观测（tool 签名去重 + progressEpoch 推进）。
func (s *TaskService) ObserveTool(observation ToolObservation) {
	if s == nil || s.state == nil || s.state.RequestID != observation.RequestID {
		return
	}
	s.state.RecordTool(observation.Name, observation.Result, observation.Err)
}

// ObservePlanEvent 记录一次 plan 事件投影观测（节点终态 → checkpoint 打点）。
func (s *TaskService) ObservePlanEvent(event PlanEvent) {
	if s == nil || s.state == nil {
		return
	}
	s.state.Checkpoint(event.NodeID, event.Objective, event.Status, event.Output, event.Failure)
}

// ObserveModelOutput 记录一次模型回复观测（自然终态判定输入）。
func (s *TaskService) ObserveModelOutput(ctx context.Context, output ModelOutput) error {
	if s == nil || s.state == nil {
		return nil
	}
	s.ViewMu.Lock()
	defer s.ViewMu.Unlock()
	if s.state.RequestID != output.RequestID {
		return nil
	}
	s.modelOutput = output
	return nil
}

// OnChatEnd 把自然停止转换为可审计的完成/交接。只消费事件投影与终态标记；
// 在途的 authoritative Plan 不会被静默完成。
func (s *TaskService) OnChatEnd(ctx context.Context, summary ChatEndSummary) (model.TaskState, error) {
	if s == nil {
		return model.TaskState{}, nil
	}
	s.ViewMu.Lock()
	defer s.ViewMu.Unlock()
	state := s.state
	if state == nil || state.RequestID != summary.RequestID || state.Terminal != nil {
		return model.TaskState{}, nil
	}
	if plan := s.projection.Plan(); plan != nil {
		switch plan.Status {
		case model.PlanPending, model.PlanRunning:
			state.Status = StatusNeedsUserDecision
			state.Terminal = &taskTerminal{
				Kind:             ToolNeedsUserDecision,
				Summary:          "The authoritative plan is ready but has not been executed.",
				DecisionQuestion: "Should Seelex execute the loaded plan, revise it, or stop here?",
				DecisionOptions:  []string{"execute", "revise", "stop"},
			}
			s.setTaskStateLocked(summary.RequestID, model.TaskNeedsUserDecision, "Plan is ready but not executed. Choose whether to execute it, revise it, or stop here.")
			state.ProgressEpoch++
			s.rememberResumeLocked(summary)
			return s.taskStateResultLocked(summary.RequestID, model.TaskNeedsUserDecision, "Plan is ready but not executed. Choose whether to execute it, revise it, or stop here."), nil
		case model.PlanFailed, model.PlanAborted:
			state.Status = StatusFailed
			state.Checkpoint("plan", "authoritative plan", string(plan.Status), "", "plan did not complete")
			s.setTaskStateLocked(summary.RequestID, model.TaskFailed, "The authoritative plan did not reach completion.")
			s.rememberResumeLocked(summary)
			return s.taskStateResultLocked(summary.RequestID, model.TaskFailed, "The authoritative plan did not reach completion."), nil
		}
	}
	state.Status = StatusCompleted
	state.Terminal = &taskTerminal{
		Kind: ToolComplete, Summary: "Model returned a final response without an explicit terminal tool call.",
	}
	s.setTaskStateLocked(summary.RequestID, model.TaskCompleted, state.Terminal.Summary)
	state.ProgressEpoch++
	s.rememberResumeLocked(summary)
	return s.taskStateResultLocked(summary.RequestID, model.TaskCompleted, state.Terminal.Summary), nil
}

// taskStateResultLocked 构造任务的可见状态结果。优先取共享快照中的 Task
// （活跃会话已由 setTaskStateLocked 写入）；非活跃会话（后台并行）快照未
// 写入时由本方法构造，避免 nil 解引用。
func (s *TaskService) taskStateResultLocked(requestID string, status model.TaskStatus, summary string) model.TaskState {
	if s.lastTaskState != nil && s.lastTaskState.RequestID == requestID {
		return *s.lastTaskState
	}
	var compactions []model.ContextCompaction
	if s.state != nil && s.state.RequestID == requestID {
		compactions = append([]model.ContextCompaction(nil), s.state.ContextCompactions...)
	}
	return model.TaskState{
		RequestID:          requestID,
		Status:             status,
		Summary:            strings.TrimSpace(summary),
		ContextCompactions: compactions,
		UpdatedAt:          time.Now(),
	}
}

// VerifyAndApply 是终态/打点工具的 Registry handler 入口：解析/校验入参 →
// 同步 flush 投影 → 校验完成度与收敛性 → 应用状态。
// 任务归属由本 TaskService 绑定的会话状态决定（多会话并行时，调用方从
// ctx 会话路由出正确的 TaskService），不再依赖共享快照的"当前 Chat"。
func (s *TaskService) VerifyAndApply(ctx context.Context, kind, argsJSON string) (string, error) {
	var input taskTerminal
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("%s: invalid JSON: %w", kind, err)
	}
	input.Kind = kind
	if kind == ToolCheckNode {
		if strings.TrimSpace(input.NodeID) == "" {
			return "", fmt.Errorf("%s: node_id is required", kind)
		}
		if err := s.projection.Flush(ctx); err != nil {
			return "", fmt.Errorf("%s: plan projection flush failed: %w", kind, err)
		}
		s.ViewMu.Lock()
		defer s.ViewMu.Unlock()
		state := s.state
		if state == nil || state.Terminal != nil {
			return "", fmt.Errorf("%s: no active task execution", kind)
		}
		return s.applyCheckNodeLocked(ctx, input)
	}
	input.Summary = strings.TrimSpace(input.Summary)
	if input.Summary == "" {
		return "", fmt.Errorf("%s: summary is required", kind)
	}
	if kind == ToolFailed && strings.TrimSpace(input.FailureType) == "" {
		return "", fmt.Errorf("%s: failure_type is required", kind)
	}
	if kind == ToolNeedsUserDecision && strings.TrimSpace(input.DecisionQuestion) == "" {
		return "", fmt.Errorf("%s: decision_question is required", kind)
	}
	if err := s.projection.Flush(ctx); err != nil {
		return "", fmt.Errorf("%s: plan projection flush failed: %w", kind, err)
	}
	s.ViewMu.Lock()
	defer s.ViewMu.Unlock()
	state := s.state
	if state == nil || state.Terminal != nil {
		return "", fmt.Errorf("%s: no active task execution", kind)
	}
	handler, ok := s.terminals[kind]
	if !ok {
		return "", fmt.Errorf("unsupported task terminal %q", kind)
	}
	return handler(ctx, input)
}

// applyCheckNodeLocked 接受 task_check_node：把已加载任务结构中的单个节点
// 打点为 completed（在途进度，不结束任务）。
func (s *TaskService) applyCheckNodeLocked(ctx context.Context, input taskTerminal) (string, error) {
	plan := s.projection.Plan()
	if plan == nil {
		return "", fmt.Errorf("%s: no task structure is loaded; load one with plan_load first", ToolCheckNode)
	}
	var label string
	for index := range plan.Nodes {
		if plan.Nodes[index].ID == input.NodeID {
			label = plan.Nodes[index].Label
			break
		}
	}
	found := false
	if s.tasks != nil {
		var changed bool
		found, changed = s.tasks.CheckPlanNodeProjection(s.sessionID, input.NodeID, input.Output)
		if changed {
			s.state.Checkpoint(input.NodeID, label, "completed", input.Output, "")
			s.state.ProgressEpoch++
		}
	}
	if !found {
		return "", fmt.Errorf("%s: unknown node %q", ToolCheckNode, input.NodeID)
	}
	encoded, _ := json.Marshal(map[string]string{
		"status": "accepted", "node_id": input.NodeID, "node_status": "completed",
	})
	return string(encoded), nil
}

func (s *TaskService) applyCompleteLocked(ctx context.Context, input taskTerminal) (string, error) {
	if err := s.verifyCompletionLocked(input); err != nil {
		return "", err
	}
	if err := s.completeAuthoritativePlanLocked(); err != nil {
		return "", err
	}
	s.state.Terminal = &input
	s.state.Status = StatusCompleted
	s.setTaskStateLocked(s.state.RequestID, model.TaskCompleted, input.Summary)
	s.state.ProgressEpoch++
	s.rememberResumeLocked(ChatEndSummary{RequestID: s.state.RequestID})
	encoded, _ := json.Marshal(map[string]string{"status": "accepted", "terminal": ToolComplete})
	return string(encoded), nil
}

func (s *TaskService) applyFailedLocked(ctx context.Context, input taskTerminal) (string, error) {
	s.state.Terminal = &input
	if input.FailureType == "blocked" || input.FailureType == "external_dependency" {
		s.state.Status = StatusBlocked
		s.setTaskStateLocked(s.state.RequestID, model.TaskBlocked, input.Summary)
	} else {
		s.state.Status = StatusFailed
		s.setTaskStateLocked(s.state.RequestID, model.TaskFailed, input.Summary)
	}
	s.state.ProgressEpoch++
	s.rememberResumeLocked(ChatEndSummary{RequestID: s.state.RequestID})
	encoded, _ := json.Marshal(map[string]string{"status": "accepted", "terminal": ToolFailed})
	return string(encoded), nil
}

func (s *TaskService) applyDecisionLocked(ctx context.Context, input taskTerminal) (string, error) {
	s.state.Terminal = &input
	s.state.Status = StatusNeedsUserDecision
	s.setTaskStateLocked(s.state.RequestID, model.TaskNeedsUserDecision, input.Summary)
	s.state.ProgressEpoch++
	s.rememberResumeLocked(ChatEndSummary{RequestID: s.state.RequestID})
	encoded, _ := json.Marshal(map[string]string{"status": "accepted", "terminal": ToolNeedsUserDecision})
	return string(encoded), nil
}

func (s *TaskService) verifyCompletionLocked(terminal taskTerminal) error {
	nodes := s.projection.AllNodes()
	missing := nodesNotCovered(nodes, terminal.CompletedNodes, s.projection)
	if len(missing) > 0 {
		return fmt.Errorf("%s: completed_nodes must include authoritative plan node %q", ToolComplete, missing[0])
	}
	if !s.projection.Converged() {
		return fmt.Errorf("%s: plan projection has not converged; plan execution is still in flight", ToolComplete)
	}
	return nil
}

func (s *TaskService) completeAuthoritativePlanLocked() error {
	if s.tasks != nil {
		s.tasks.CompletePlanProjection(s.sessionID)
	}
	return nil
}

// rememberResumeLocked 在任务终态时保留最小恢复记录。
func (s *TaskService) rememberResumeLocked(summary ChatEndSummary) {
	refs := []string(nil)
	if s.queueRefs != nil {
		refs = s.queueRefs()
	}
	s.resumeRecord = TaskResumeRecord{
		TaskID: summary.RequestID, Objective: s.state.Objective,
		QueuedRefs: refs,
	}
}

// setTaskStateLocked 把任务可见状态写入快照。非活跃会话（后台并行执行）
// 跳过共享快照写入，避免污染活跃会话投影。
func (s *TaskService) setTaskStateLocked(requestID string, status model.TaskStatus, summary string) {
	var compactions []model.ContextCompaction
	if s.state != nil && s.state.RequestID == requestID {
		compactions = append([]model.ContextCompaction(nil), s.state.ContextCompactions...)
	}
	visible := &model.TaskState{
		RequestID:          requestID,
		Status:             status,
		Summary:            strings.TrimSpace(summary),
		ContextCompactions: compactions,
		UpdatedAt:          time.Now(),
	}
	s.lastTaskState = visible
}

func nodesNotCovered(projected, completed []string, projection PlanProjectionReader) []string {
	covered := make(map[string]struct{}, len(completed))
	for _, nodeID := range completed {
		covered[nodeID] = struct{}{}
	}
	missing := make([]string, 0)
	for _, nodeID := range projected {
		if _, ok := covered[nodeID]; ok {
			continue
		}
		switch projection.NodeStatus(nodeID) {
		case model.NodeCompleted, model.NodeSkipped:
			continue
		}
		missing = append(missing, nodeID)
	}
	return missing
}

// AppendPlanNodeEvent 把一次节点事件追加到节点时间线（详情页数据源；上限
// limits.plan_node_events，同状态合并心跳）。
func AppendPlanNodeEvent(node *model.PlanNode, event dto.PlanNodeEvent) {
	if node.Events == nil {
		node.Events = make([]model.PlanNodeEventInfo, 0, 8)
	}
	status := PlanNodeStatus(event.Status)
	output := event.Output
	if len(output) > 200 {
		output = output[:200] + "…"
	}
	if last := len(node.Events) - 1; last >= 0 {
		previous := &node.Events[last]
		if previous.Status == status {
			previous.At = event.At
			if output != "" {
				previous.Output = output
			}
			return
		}
	}
	node.Events = append(node.Events, model.PlanNodeEventInfo{Status: status, At: event.At, Output: output})
	if limit := limits.Get().PlanNodeEvents; limit > 0 && len(node.Events) > limit {
		node.Events = node.Events[len(node.Events)-limit:]
	}
}

// RecalculatePlanProgress 重算计划整体进度（completed/skipped / total）。
func RecalculatePlanProgress(plan *model.PlanState) {
	done := 0
	for _, node := range plan.Nodes {
		if node.Status == model.NodeCompleted || node.Status == model.NodeSkipped {
			done++
		}
	}
	if len(plan.Nodes) > 0 {
		plan.Progress = float64(done) / float64(len(plan.Nodes))
	}
}
