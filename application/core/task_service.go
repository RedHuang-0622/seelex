package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// 本文件是 plan.md §3.4 的 TaskService：Task 与 Plan 分离。
//
// - TaskService 拥有 taskExecutionState（功能打点快照，见
//   docs/feature-instrumentation.md 打点表）；任务开始创建，终态
//   （task_complete / task_failed / task_needs_user_decision 被接受）即结束，
//   不持久化、不承担会话恢复。
// - 输入面全部为观察/事件（ObserveTool / ObservePlanEvent / ObserveModelOutput
//   / OnChatEnd），不访问 workplan 执行器内部；Plan 终态校验只消费事件投影
//   （PlanProjectionReader，由 event.Sink → HandlePlanNodeComplete 累积）。
// - 终态工具注册在 tools.Registry（seelebridge.taskTerminalProvider），handler
//   内调用 TaskService.VerifyAndApply：先同步 flush 投影（Sink 追加返回后再
//   判定，保证投影不滞后），再校验完成度与收敛性。

// PlanProjectionReader 是 TaskService 的 Plan 投影读取面。PlanState 由事件投影
// （event.Sink → HandlePlanNodeComplete）累积而来；TaskService 只消费它，
// 不访问 workplan 执行器内部状态。
type PlanProjectionReader interface {
	// AllNodes 返回投影中全部 Plan 节点 ID（按加载顺序，含未完成节点）。
	AllNodes() []string
	// NodeStatus 返回指定节点的投影状态（未找到 → NodePending）。
	NodeStatus(nodeID string) NodeStatus
	// PlanStatus 返回计划级投影状态（无 Plan 投影时返回空串）。
	PlanStatus() PlanStatus
	// Converged 判定投影是否收敛：无 Plan 投影时为 true（无校验对象）；
	// 计划运行中（PlanRunning）视为未收敛——执行器仍持有 DAG，事件可能滞后。
	// 注意：PlanPending 不算未收敛（preflight 权威清单可由模型手动执行，
	// 完成声明由 completed_nodes 覆盖）。
	Converged() bool
	// Flush 在终态判定前同步刷新投影（Sink 追加返回后再判定；失败返回 error，
	// 调用方按终态协议拒绝该工具）。
	Flush(ctx context.Context) error
}

// planProjectionReader 是 PlanProjectionReader 的默认实现：读取由事件投影累积
// 的 PlanState（snapshot.Runtime.Plan）。当前接线中 workplan 事件在 Append 内
// 同步入库并投影，Dispatch 返回前投影已收敛，故生产 Flush 为空操作；flush
// 钩子保留给测试注入（模拟 Sink 延迟投影）与未来持久化 Sink 的接线点。
type planProjectionReader struct {
	*serviceState
	flush func(context.Context) error
}

func (r *planProjectionReader) AllNodes() []string {
	plan := r.snapshot.Runtime.Plan
	if plan == nil {
		return nil
	}
	nodes := make([]string, 0, len(plan.Nodes))
	for _, node := range plan.Nodes {
		nodes = append(nodes, node.ID)
	}
	return nodes
}

func (r *planProjectionReader) NodeStatus(nodeID string) NodeStatus {
	plan := r.snapshot.Runtime.Plan
	if plan == nil {
		return NodePending
	}
	for _, node := range plan.Nodes {
		if node.ID == nodeID {
			return node.Status
		}
	}
	return NodePending
}

func (r *planProjectionReader) PlanStatus() PlanStatus {
	if plan := r.snapshot.Runtime.Plan; plan != nil {
		return plan.Status
	}
	return ""
}

func (r *planProjectionReader) Converged() bool {
	plan := r.snapshot.Runtime.Plan
	if plan == nil {
		return true
	}
	return plan.Status != PlanRunning
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

// TaskResumeRecord 是任务终态后保留的最小恢复记录（objective + 排队输入引用），
// 供 TaskFrame 与事件投影在 slice 7 落库；不携带功能打点快照（终态即亡）。
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

// terminalToolHandlers 是三个终态工具的应用入口（task_complete / task_failed /
// task_needs_user_decision）。Registry 注册在 composition root（main.go →
// seelebridge.taskTerminalProvider），handler 内调用 TaskService.VerifyAndApply。
type terminalToolHandlers map[string]func(context.Context, taskTerminal) (string, error)

// TaskService 拥有单个任务的功能打点快照与终态判定。每个任务（含排队输入
// 续接任务）构造一个实例；任务终态后不再演化，由下一任务替换。
type TaskService struct {
	*serviceState
	state        *taskExecutionState
	projection   PlanProjectionReader
	terminals    terminalToolHandlers
	modelOutput  ModelOutput      // 最近一次模型回复观测（自然终态判定输入）
	resumeRecord TaskResumeRecord // 终态时保留的最小恢复记录（slice 7 落库）
}

// newTaskService 构造当前任务的 TaskService。state 为 nil 时表示无活跃任务
// （终态判定返回 no active task execution）。
func newTaskService(state *serviceState, taskState *taskExecutionState) *TaskService {
	service := &TaskService{
		serviceState: state,
		state:        taskState,
		projection:   &planProjectionReader{serviceState: state},
	}
	service.terminals = terminalToolHandlers{
		taskCompleteTool:          service.applyCompleteLocked,
		taskFailedTool:            service.applyFailedLocked,
		taskNeedsUserDecisionTool: service.applyDecisionLocked,
	}
	return service
}

// SemanticProgress 返回任务的语义进展计数（epoch，由工具/checkpoint 观测累积；
// 不再从工具输出 hash 推断）。react_budget.go 的无进展预算消费它。
func (s *TaskService) SemanticProgress(requestID string) (uint64, bool) {
	if s == nil || s.state == nil || s.state.requestID != requestID {
		return 0, false
	}
	return s.state.progressEpoch, true
}

// ResumeRecord 返回任务终态时保留的最小恢复记录（objective + 排队输入引用）。
func (s *TaskService) ResumeRecord() TaskResumeRecord {
	if s == nil {
		return TaskResumeRecord{}
	}
	return s.resumeRecord
}

// ObserveTool 记录一次工具执行观测（tool 签名去重 + progressEpoch 推进）。
// 调用方持有 service.mu（与 handleToolComplete 的锁范围一致）。
func (s *TaskService) ObserveTool(observation ToolObservation) {
	if s == nil || s.state == nil || s.state.requestID != observation.RequestID {
		return
	}
	s.state.recordTool(observation.Name, observation.Result, observation.Err)
}

// ObservePlanEvent 记录一次 plan 事件投影观测（节点终态 → checkpoint 打点）。
// 调用方持有 service.mu（与 HandlePlanNodeComplete / handlePlanRunFailureLocked
// 的锁范围一致）。
func (s *TaskService) ObservePlanEvent(event PlanEvent) {
	if s == nil || s.state == nil {
		return
	}
	s.state.checkpoint(event.NodeID, event.Objective, event.Status, event.Output, event.Failure)
}

// ObserveModelOutput 记录一次模型回复观测（自然终态判定输入）。
func (s *TaskService) ObserveModelOutput(ctx context.Context, output ModelOutput) error {
	if s == nil || s.state == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.requestID != output.RequestID {
		return nil
	}
	s.modelOutput = output
	return nil
}

// OnChatEnd 把自然停止转换为可审计的完成/交接（finalizeTaskExecution 演进）。
// 只消费事件投影（PlanStatus）与终态标记，不访问执行器内部状态。在途的
// authoritative Plan 不会被静默完成：模型在执行前停止时，可见结果是
// user-decision 交接而非不透明的运行时错误。
func (s *TaskService) OnChatEnd(ctx context.Context, summary ChatEndSummary) (TaskState, error) {
	if s == nil {
		return TaskState{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state
	if state == nil || state.requestID != summary.RequestID || state.terminal != nil {
		return TaskState{}, nil
	}
	if plan := s.snapshot.Runtime.Plan; plan != nil {
		switch plan.Status {
		case PlanPending, PlanRunning:
			state.status = taskStatusNeedsUserDecision
			state.terminal = &taskTerminal{
				Kind:             taskNeedsUserDecisionTool,
				Summary:          "The authoritative plan is ready but has not been executed.",
				DecisionQuestion: "Should Seelex execute the loaded plan, revise it, or stop here?",
				DecisionOptions:  []string{"execute", "revise", "stop"},
			}
			s.setTaskStateLocked(summary.RequestID, TaskNeedsUserDecision, "Plan is ready but not executed. Choose whether to execute it, revise it, or stop here.")
			state.progressEpoch++
			s.rememberResumeLocked(summary)
			return *s.snapshot.Task, nil
		case PlanFailed, PlanAborted:
			state.status = taskStatusFailed
			state.checkpoint("plan", "authoritative plan", string(plan.Status), "", "plan did not complete")
			s.setTaskStateLocked(summary.RequestID, TaskFailed, "The authoritative plan did not reach completion.")
			s.rememberResumeLocked(summary)
			return *s.snapshot.Task, nil
		}
	}
	state.status = taskStatusCompleted
	state.terminal = &taskTerminal{
		Kind: taskCompleteTool, Summary: "Model returned a final response without an explicit terminal tool call.",
	}
	s.setTaskStateLocked(summary.RequestID, TaskCompleted, state.terminal.Summary)
	state.progressEpoch++
	s.rememberResumeLocked(summary)
	return *s.snapshot.Task, nil
}

// VerifyAndApply 是终态工具的 Registry handler 入口：解析/校验入参 →
// 同步 flush 投影 → 校验完成度与收敛性 → 应用终态。
func (s *TaskService) VerifyAndApply(ctx context.Context, kind, argsJSON string) (string, error) {
	var input taskTerminal
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("%s: invalid JSON: %w", kind, err)
	}
	input.Kind = kind
	input.Summary = strings.TrimSpace(input.Summary)
	if input.Summary == "" {
		return "", fmt.Errorf("%s: summary is required", kind)
	}
	if kind == taskFailedTool && strings.TrimSpace(input.FailureType) == "" {
		return "", fmt.Errorf("%s: failure_type is required", kind)
	}
	if kind == taskNeedsUserDecisionTool && strings.TrimSpace(input.DecisionQuestion) == "" {
		return "", fmt.Errorf("%s: decision_question is required", kind)
	}
	// 终态判定前同步 flush 事件投影（Sink 追加返回后再判定；失败拒绝终态工具，
	// 按现有协议以工具错误呈现，避免基于滞后投影的误判）。
	if err := s.projection.Flush(ctx); err != nil {
		return "", fmt.Errorf("%s: plan projection flush failed: %w", kind, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state
	if state == nil || state.requestID != s.snapshot.Chat.RequestID || !s.snapshot.Chat.Running {
		return "", fmt.Errorf("%s: no active task execution", kind)
	}
	if state.terminal != nil {
		return "", fmt.Errorf("%s: task already reached %s", kind, state.terminal.Kind)
	}
	handler, ok := s.terminals[kind]
	if !ok {
		return "", fmt.Errorf("unsupported task terminal %q", kind)
	}
	return handler(ctx, input)
}

// applyCompleteLocked 接受 task_complete：先经投影校验（verifyCompletionLocked），
// 再收敛 PlanState 投影（completeAuthoritativePlanLocked）。
func (s *TaskService) applyCompleteLocked(ctx context.Context, input taskTerminal) (string, error) {
	if err := s.verifyCompletionLocked(input); err != nil {
		return "", err
	}
	if err := s.completeAuthoritativePlanLocked(); err != nil {
		return "", err
	}
	s.state.terminal = &input
	s.state.status = taskStatusCompleted
	s.setTaskStateLocked(s.state.requestID, TaskCompleted, input.Summary)
	s.state.progressEpoch++
	s.rememberResumeLocked(ChatEndSummary{RequestID: s.state.requestID})
	encoded, _ := json.Marshal(map[string]string{"status": "accepted", "terminal": taskCompleteTool})
	return string(encoded), nil
}

func (s *TaskService) applyFailedLocked(ctx context.Context, input taskTerminal) (string, error) {
	s.state.terminal = &input
	if input.FailureType == "blocked" || input.FailureType == "external_dependency" {
		s.state.status = taskStatusBlocked
		s.setTaskStateLocked(s.state.requestID, TaskBlocked, input.Summary)
	} else {
		s.state.status = taskStatusFailed
		s.setTaskStateLocked(s.state.requestID, TaskFailed, input.Summary)
	}
	s.state.progressEpoch++
	s.rememberResumeLocked(ChatEndSummary{RequestID: s.state.requestID})
	encoded, _ := json.Marshal(map[string]string{"status": "accepted", "terminal": taskFailedTool})
	return string(encoded), nil
}

func (s *TaskService) applyDecisionLocked(ctx context.Context, input taskTerminal) (string, error) {
	s.state.terminal = &input
	s.state.status = taskStatusNeedsUserDecision
	s.setTaskStateLocked(s.state.requestID, TaskNeedsUserDecision, input.Summary)
	s.state.progressEpoch++
	s.rememberResumeLocked(ChatEndSummary{RequestID: s.state.requestID})
	encoded, _ := json.Marshal(map[string]string{"status": "accepted", "terminal": taskNeedsUserDecisionTool})
	return string(encoded), nil
}

// verifyCompletionLocked 校验 task_complete 的完成声明：completed_nodes 必须覆盖
// 投影中的全部 Plan 节点（事件投影累积，非执行器内部状态），且投影已收敛
// （计划不在运行中——执行仍持有 DAG 时拒绝，防事件滞后造成的误判）。
func (s *TaskService) verifyCompletionLocked(terminal taskTerminal) error {
	nodes := s.projection.AllNodes()
	missing := nodesNotIn(nodes, terminal.CompletedNodes)
	if len(missing) > 0 {
		return fmt.Errorf("%s: completed_nodes must include authoritative plan node %q", taskCompleteTool, missing[0])
	}
	if !s.projection.Converged() {
		return fmt.Errorf("%s: plan projection has not converged; plan execution is still in flight", taskCompleteTool)
	}
	return nil
}

// completeAuthoritativePlanLocked 把投影中全部节点置为终态并收敛计划状态
// （投影校验通过后的收尾，不再读执行器）。
func (s *TaskService) completeAuthoritativePlanLocked() error {
	plan := s.snapshot.Runtime.Plan
	if plan == nil {
		return nil
	}
	for index := range plan.Nodes {
		plan.Nodes[index].Status = NodeCompleted
	}
	plan.Status = PlanCompleted
	plan.Progress = 1
	return nil
}

// rememberResumeLocked 在任务终态时保留最小恢复记录（objective + 排队输入引用），
// 供 TaskFrame 与事件投影落库（slice 7）；功能打点快照本身终态即亡，不持久化。
func (s *TaskService) rememberResumeLocked(summary ChatEndSummary) {
	s.resumeRecord = TaskResumeRecord{
		TaskID: summary.RequestID, Objective: s.state.objective,
		QueuedRefs: queuedInputRefs(s.inputQueue),
	}
}

// nodesNotIn 返回 projected 中不在 completed 里的节点 ID（保持 projected 顺序）。
func nodesNotIn(projected, completed []string) []string {
	covered := make(map[string]struct{}, len(completed))
	for _, nodeID := range completed {
		covered[nodeID] = struct{}{}
	}
	missing := make([]string, 0)
	for _, nodeID := range projected {
		if _, ok := covered[nodeID]; !ok {
			missing = append(missing, nodeID)
		}
	}
	return missing
}

// queuedInputRefs 取排队输入的最小引用（displayInput），供 TaskResumeRecord 使用。
func queuedInputRefs(queue []chatRequest) []string {
	refs := make([]string, 0, len(queue))
	for _, request := range queue {
		if input := strings.TrimSpace(request.displayInput); input != "" {
			refs = append(refs, input)
		}
	}
	return refs
}

// currentTaskService 返回当前任务的 TaskService（自行加 RLock，供无锁调用点）。
func (service *Service) currentTaskService() *TaskService {
	service.mu.RLock()
	ts := service.currentTaskServiceLocked()
	service.mu.RUnlock()
	return ts
}

// currentTaskServiceLocked 返回当前任务的 TaskService；要求调用方持有 service.mu。
// 测试/恢复路径未装配 taskService 时，以当前状态构造瞬态实例（终态判定语义不变）。
func (service *Service) currentTaskServiceLocked() *TaskService {
	ts := service.taskService
	state := service.taskExecution
	if ts != nil && ts.state == state {
		return ts
	}
	return newTaskService(service.serviceState, state)
}

// TaskTerminalHandler returns a Runtime-facing handler while keeping request
// state owned by Application. The handler has no external side effects and
// routes terminal tools through TaskService.VerifyAndApply（投影 flush + 校验）。
func (service *Service) TaskTerminalHandler(kind string) func(context.Context, string) (string, error) {
	return func(ctx context.Context, argsJSON string) (string, error) {
		return service.currentTaskService().VerifyAndApply(ctx, kind, argsJSON)
	}
}

// finalizeTaskExecution converts a natural model stop into an auditable
// completion or handoff. 终态判定委托 TaskService.OnChatEnd（只消费事件投影，
// 不访问执行器内部状态）。
func (service *Service) finalizeTaskExecution(requestID string) error {
	_, err := service.currentTaskService().OnChatEnd(context.Background(), ChatEndSummary{RequestID: requestID})
	return err
}
