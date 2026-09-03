package task_context

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// Coordinator 是任务执行域协调器：task 子状态、plan 子状态与 token 审计
// 在此自持；跨域协作（会话持久化、上下文装配、prompt 组装）经消费方端口
// 表达，装配根注入。
//
// 多会话并行（M2）：任务/plan/预算状态按会话分片（sessionStates +
// requestToSession）。无会话参数的方法按"活跃会话"（Snapshot.Session.ID）
// 路由，保持单会话兼容；并行执行路径显式携带 sessionID 或 requestID。
type Coordinator struct {
	*state.Core
	prompt PromptPort

	limits            func() seelexctx.Limits
	isInternalContent func(string) bool
	isOversized       func(string, int) bool
	oversizedWarning  func(string, string) string
	presentToolError  func(string, error) string
	queuedInputRefs   func() []string
	currentSessionID  func() string

	goalSkillActive atomic.Bool
	tokenCounter    RequestTokenCounter

	// sessionStates 是会话级任务/plan 运行时状态分片（M2）。所有访问在
	// Core.Mu 下进行（Locked 方法）或由方法自行加锁。
	sessionStates map[string]*sessionTaskRuntime
	// requestToSession 维护 requestID → sessionID 绑定（BeginTask 登记，
	// 任务结束后清理），供带 requestID 的方法反查会话状态。会话域重构后
	// 后台流式路径经 requestMu 并发读，不取全局锁。
	requestToSession map[string]string
	requestMu        sync.RWMutex
}

// sessionTaskRuntime 是单个会话的任务/plan 运行时状态（M2 分片单元）。
type sessionTaskRuntime struct {
	taskExecution           *TaskExecutionState
	taskService             *TaskService // 当前任务的 TaskService（与 taskExecution 同生命周期）
	transcript              []model.TranscriptEvent
	transcriptSeq           uint64
	pendingProviderCalls    []model.TranscriptToolCall
	pendingToolResults      []model.StoredToolResult
	toolResultRefs          []model.ToolResultRef
	resultRefsByToolCallID  map[string]string
	taskCheckpoints         []model.TaskCheckpoint
	planStack               []model.SessionPlanFrame
	activePlanID            string
	planSequence            uint64
	replanInFlight          map[string]struct{}
	reactBudget             *activeReActBudget
	contextControlFailure   error
	contextControlRequestID string
}

func newSessionTaskRuntime() *sessionTaskRuntime {
	return &sessionTaskRuntime{
		resultRefsByToolCallID: make(map[string]string),
		replanInFlight:         make(map[string]struct{}),
	}
}

// NewCoordinator 构造任务域协调器。
func NewCoordinator(deps Deps) *Coordinator {
	return &Coordinator{
		Core:              deps.Core,
		prompt:            deps.Prompt,
		limits:            deps.Limits,
		isInternalContent: deps.IsInternalContent,
		isOversized:       deps.IsOversizedToolResult,
		oversizedWarning:  deps.OversizedToolResultWarning,
		presentToolError:  deps.PresentToolError,
		queuedInputRefs:   deps.QueuedInputRefs,
		currentSessionID:  deps.CurrentSessionID,
		tokenCounter:      NewCalibratedTokenCounter(),
	}
}

// activeSessionIDLocked 返回当前活跃会话（快照归属会话）。调用方持有
// Core.Mu。
func (c *Coordinator) activeSessionIDLocked() string {
	return c.Snapshot.Session.ID
}

// sessionStateLocked 返回指定会话的运行时状态（按需创建）。调用方持有
// Core.Mu。
func (c *Coordinator) sessionStateLocked(sessionID string) *sessionTaskRuntime {
	if c.sessionStates == nil {
		c.sessionStates = make(map[string]*sessionTaskRuntime)
	}
	st := c.sessionStates[sessionID]
	if st == nil {
		st = newSessionTaskRuntime()
		c.sessionStates[sessionID] = st
	}
	return st
}

// activeSessionLocked 返回活跃会话的运行时状态。调用方持有 Core.Mu。
func (c *Coordinator) activeSessionLocked() *sessionTaskRuntime {
	return c.sessionStateLocked(c.activeSessionIDLocked())
}

// sessionForRequestLocked 按 requestID 反查会话状态（未绑定 → nil）。
// 调用方持有 Core.Mu。
func (c *Coordinator) sessionForRequestLocked(requestID string) *sessionTaskRuntime {
	if requestID == "" {
		return nil
	}
	if sessionID, ok := c.requestToSession[requestID]; ok {
		return c.sessionStateLocked(sessionID)
	}
	return nil
}

// bindRequestLocked 登记 requestID → sessionID（BeginTask 时；调用方持有
// Core.Mu）。
func (c *Coordinator) bindRequestLocked(requestID, sessionID string) {
	c.requestMu.Lock()
	defer c.requestMu.Unlock()
	if c.requestToSession == nil {
		c.requestToSession = make(map[string]string)
	}
	c.requestToSession[requestID] = sessionID
}

// unbindRequestLocked 移除 requestID → sessionID（任务结束时；调用方持有
// Core.Mu）。
func (c *Coordinator) unbindRequestLocked(requestID string) {
	c.requestMu.Lock()
	defer c.requestMu.Unlock()
	delete(c.requestToSession, requestID)
}

// semanticProgressLocked 返回指定请求的语义进展计数（TaskService epoch）；
// 未装配 TaskService 时回退到直接状态读取。调用方持有 Core.Mu。
func (c *Coordinator) semanticProgressLocked(requestID string) (uint64, bool) {
	st := c.sessionForRequestLocked(requestID)
	if st == nil {
		st = c.activeSessionLocked()
	}
	if st == nil {
		return 0, false
	}
	ts := st.taskService
	state := st.taskExecution
	if ts != nil && ts.state == state {
		return ts.SemanticProgress(requestID)
	}
	if state == nil || state.RequestID != requestID {
		return 0, false
	}
	return state.ProgressEpoch, true
}

// ActiveSkillIDs 返回活跃会话当前任务的激活 skill ID 列表（锁内快照）。
func (c *Coordinator) ActiveSkillIDs() []string {
	c.Mu.RLock()
	defer c.Mu.RUnlock()
	return c.activeSkillIDsLocked(c.activeSessionIDLocked())
}

// ActiveSkillIDsFor 返回指定会话当前任务的激活 skill ID 列表（G1：后台
// 会话投影同样需要；与活跃视图解耦）。
func (c *Coordinator) ActiveSkillIDsFor(sessionID string) []string {
	c.Mu.RLock()
	defer c.Mu.RUnlock()
	return c.activeSkillIDsLocked(sessionID)
}

// activeSkillIDsLocked 是 ActiveSkillIDs/ActiveSkillIDsFor 的锁内实现。
func (c *Coordinator) activeSkillIDsLocked(sessionID string) []string {
	state := c.sessionStateLocked(sessionID).taskExecution
	if state == nil {
		return nil
	}
	ids := make([]string, 0, len(state.ActiveSkills))
	for _, active := range state.ActiveSkills {
		ids = append(ids, active.SkillID)
	}
	return ids
}

// GoalSkillActive 返回 goal skill 可见性投影（lock-free 原子值）。
func (c *Coordinator) GoalSkillActive() bool {
	return c.goalSkillActive.Load()
}

// GoalSkillActiveFor 返回指定会话当前任务的 goal skill 激活判定（G1：
// lock-free 原子值只镜像活跃会话；后台会话按自身任务状态实时计算）。
func (c *Coordinator) GoalSkillActiveFor(sessionID string) bool {
	c.Mu.RLock()
	defer c.Mu.RUnlock()
	return c.goalSkillActiveForLocked(sessionID)
}

// goalSkillActiveForLocked 是 GoalSkillActiveFor/syncGoalSkillActiveLocked
// 的锁内实现（调用方持有 Core.Mu）。
func (c *Coordinator) goalSkillActiveForLocked(sessionID string) bool {
	state := c.sessionStateLocked(sessionID).taskExecution
	if state == nil {
		return false
	}
	for _, skill := range state.ActiveSkills {
		if skill.SkillID == "goal" {
			return true
		}
	}
	return false
}

// CurrentTaskExecution 返回活跃会话当前任务执行状态（调用方持有 Core.Mu
// 时读取）。
func (c *Coordinator) CurrentTaskExecution() *TaskExecutionState {
	return c.activeSessionLocked().taskExecution
}

// CurrentTaskExecutionFor 返回指定会话当前任务执行状态（调用方持有
// Core.Mu）。
func (c *Coordinator) CurrentTaskExecutionFor(sessionID string) *TaskExecutionState {
	return c.sessionStateLocked(sessionID).taskExecution
}

// ActivePlanID 返回活跃会话当前激活 plan 帧 ID。
func (c *Coordinator) ActivePlanID() string {
	return c.activeSessionLocked().activePlanID
}

// ActivePlanIDFor 返回指定会话当前激活 plan 帧 ID。
func (c *Coordinator) ActivePlanIDFor(sessionID string) string {
	return c.sessionStateLocked(sessionID).activePlanID
}

// PlanSequence 返回活跃会话 plan 帧序列号。
func (c *Coordinator) PlanSequence() uint64 {
	return c.activeSessionLocked().planSequence
}

// PlanSequenceFor 返回指定会话 plan 帧序列号。
func (c *Coordinator) PlanSequenceFor(sessionID string) uint64 {
	return c.sessionStateLocked(sessionID).planSequence
}

// PlanStack 返回活跃会话 plan 帧栈。
func (c *Coordinator) PlanStack() []model.SessionPlanFrame {
	return c.activeSessionLocked().planStack
}

// ClearActivePlanLocked 清空活跃会话激活 plan 帧 ID（plan_clear 路径）。
func (c *Coordinator) ClearActivePlanLocked() {
	c.activeSessionLocked().activePlanID = ""
}

// ResetPlanStateLocked 清空活跃会话 plan 帧状态（新会话/恢复路径；调用方
// 持有 Core.Mu）。
func (c *Coordinator) ResetPlanStateLocked() {
	st := c.activeSessionLocked()
	st.planStack = nil
	st.activePlanID = ""
	st.planSequence = 0
}

// SetPlanStateLocked 装载活跃会话 plan 帧栈与激活帧（测试/恢复路径；调用
// 方持有 Core.Mu）。
func (c *Coordinator) SetPlanStateLocked(stack []model.SessionPlanFrame, activeID string) {
	st := c.activeSessionLocked()
	st.planStack = stack
	st.activePlanID = activeID
	st.planSequence = uint64(len(stack))
}

// SetResultRefByCallIDLocked 登记活跃会话 callID → resultRef（测试/恢复
// 路径；调用方持有 Core.Mu）。
func (c *Coordinator) SetResultRefByCallIDLocked(callID, ref string) {
	c.activeSessionLocked().resultRefsByToolCallID[callID] = ref
}

// ReplanInFlight 判定活跃会话重规划交互是否在途。
func (c *Coordinator) ReplanInFlight(interactionID string) bool {
	_, exists := c.activeSessionLocked().replanInFlight[interactionID]
	return exists
}

// MarkReplanInFlight 登记活跃会话重规划交互。
func (c *Coordinator) MarkReplanInFlight(interactionID string) {
	c.activeSessionLocked().replanInFlight[interactionID] = struct{}{}
}

// DeleteReplanInFlight 移除活跃会话重规划交互标记。
func (c *Coordinator) DeleteReplanInFlight(interactionID string) {
	delete(c.activeSessionLocked().replanInFlight, interactionID)
}

// ErrReActBudgetExceeded 标记请求被其捕获的执行预算终止。
var ErrReActBudgetExceeded = errors.New("ReAct execution budget exhausted")

// StartReActBudgetLocked 启动活跃会话请求级执行预算（调用方持有 Core.Mu）。
func (c *Coordinator) StartReActBudgetLocked(requestID string, budget prompt.ReActBudget) {
	st := c.activeSessionLocked()
	st.reactBudget = &activeReActBudget{requestID: requestID, budget: budget}
	c.bindRequestLocked(requestID, c.activeSessionIDLocked())
}

// StartReActBudgetForLocked 启动指定会话请求级执行预算（调用方持有
// Core.Mu）。
func (c *Coordinator) StartReActBudgetForLocked(sessionID, requestID string, budget prompt.ReActBudget) {
	st := c.sessionStateLocked(sessionID)
	st.reactBudget = &activeReActBudget{requestID: requestID, budget: budget}
	c.bindRequestLocked(requestID, sessionID)
}

// SetReActBudgetExhaustedLocked 直接置位预算终止原因（测试模拟预算耗尽；
// 调用方持有 Core.Mu）。
func (c *Coordinator) SetReActBudgetExhaustedLocked(requestID, reason string) {
	if st := c.sessionForRequestLocked(requestID); st != nil && st.reactBudget != nil && st.reactBudget.requestID == requestID {
		st.reactBudget.reason = reason
	}
}

// ClearReActBudget 清除请求级预算（自行加锁）。
func (c *Coordinator) ClearReActBudget(requestID string) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if st := c.sessionForRequestLocked(requestID); st != nil && st.reactBudget != nil && st.reactBudget.requestID == requestID {
		st.reactBudget = nil
	}
	c.unbindRequestLocked(requestID)
}

// RecordReActToolCall 累计一次工具调用（自行加锁）。ctx 携带会话 ID 时
// 路由到对应会话，否则按活跃会话。
func (c *Coordinator) RecordReActToolCall(ctx context.Context) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	st := c.runtimeForContextLocked(ctx)
	if st != nil && st.reactBudget != nil {
		st.reactBudget.toolCalls++
	}
}

// AllowNextReActIteration 判定是否允许下一轮模型迭代（自行加锁）。ctx
// 携带会话 ID 时路由到对应会话，否则按活跃会话。
func (c *Coordinator) AllowNextReActIteration(ctx context.Context, turn int) bool {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	st := c.runtimeForContextLocked(ctx)
	if st == nil {
		return true
	}
	budget := st.reactBudget
	if budget == nil || budget.reason != "" {
		return budget == nil
	}
	if budget.budget.MaxToolCalls > 0 && budget.toolCalls >= budget.budget.MaxToolCalls {
		budget.reason = fmt.Sprintf("tool-call limit reached (%d)", budget.budget.MaxToolCalls)
		return false
	}
	if budget.budget.MaxToolRounds > 0 && turn+1 >= budget.budget.MaxToolRounds {
		budget.reason = fmt.Sprintf("tool-round limit reached (%d)", budget.budget.MaxToolRounds)
		return false
	}
	if budget.budget.MaxNoProgressRounds > 0 {
		// 无进展预算的决策输入来自 TaskService 的语义进展计数（epoch）。
		if epoch, active := c.semanticProgressLocked(budget.requestID); active {
			if epoch == budget.lastProgressEpoch {
				budget.noProgressRounds++
			} else {
				budget.lastProgressEpoch = epoch
				budget.noProgressRounds = 0
			}
			if budget.noProgressRounds >= budget.budget.MaxNoProgressRounds {
				budget.reason = fmt.Sprintf("no observable progress for %d tool rounds", budget.noProgressRounds)
				return false
			}
		}
	}
	return true
}

// ReActBudgetError 返回预算终止错误（自行加锁；无终止原因 → nil）。
func (c *Coordinator) ReActBudgetError(requestID string) error {
	c.Mu.RLock()
	defer c.Mu.RUnlock()
	if st := c.sessionForRequestLocked(requestID); st != nil && st.reactBudget != nil && st.reactBudget.requestID == requestID && st.reactBudget.reason != "" {
		return fmt.Errorf("%w: %s", ErrReActBudgetExceeded, st.reactBudget.reason)
	}
	return nil
}

// activeReActBudget 是一次 ReAct 请求的执行预算（tool 轮数/调用数/无进展
// 轮数）。
type activeReActBudget struct {
	requestID         string
	budget            prompt.ReActBudget
	toolCalls         int
	lastProgressEpoch uint64
	noProgressRounds  int
	reason            string
}

// runtimeForContextLocked 按 ctx 中的会话 ID 返回会话运行时（未注入 → 活跃
// 会话；无匹配 → nil）。调用方持有 Core.Mu。
func (c *Coordinator) runtimeForContextLocked(ctx context.Context) *sessionTaskRuntime {
	if ctx == nil {
		return c.activeSessionLocked()
	}
	if sessionID := SessionIDFromContext(ctx); sessionID != "" {
		return c.sessionStateLocked(sessionID)
	}
	return c.activeSessionLocked()
}

// RecordContextControlFailure 把 hook 失败转移给 runChat（自行加锁）。
func (c *Coordinator) RecordContextControlFailure(requestID string, err error) {
	if err == nil {
		return
	}
	c.Mu.Lock()
	defer c.Mu.Unlock()
	st := c.sessionForRequestLocked(requestID)
	if st == nil {
		return
	}
	if c.Snapshot.Chat.RequestID != requestID && st.taskExecution != nil && st.taskExecution.RequestID != requestID {
		return
	}
	st.contextControlFailure = fmt.Errorf("context control: %w", err)
	st.contextControlRequestID = requestID
}

// TakeContextControlFailure 取走当前请求的 context 控制失败（自行加锁）。
func (c *Coordinator) TakeContextControlFailure(requestID string) error {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	st := c.sessionForRequestLocked(requestID)
	if st == nil || st.contextControlRequestID != requestID {
		return nil
	}
	err := st.contextControlFailure
	st.contextControlFailure = nil
	st.contextControlRequestID = ""
	return err
}

// currentTaskService 返回活跃会话当前任务的 TaskService（自行加 RLock，
// 供无锁调用点）。
func (c *Coordinator) currentTaskService() *TaskService {
	c.Mu.RLock()
	ts := c.currentTaskServiceForLocked(c.activeSessionIDLocked())
	c.Mu.RUnlock()
	return ts
}

// currentTaskServiceLocked 返回活跃会话当前任务的 TaskService；要求调用方
// 持有 Core.Mu。测试/恢复路径未装配 taskService 时，以当前状态构造瞬态
// 实例。
func (c *Coordinator) currentTaskServiceLocked() *TaskService {
	return c.currentTaskServiceForLocked(c.activeSessionIDLocked())
}

// currentTaskServiceForLocked 返回指定会话当前任务的 TaskService；要求
// 调用方持有 Core.Mu。
func (c *Coordinator) currentTaskServiceForLocked(sessionID string) *TaskService {
	st := c.sessionStateLocked(sessionID)
	ts := st.taskService
	state := st.taskExecution
	if ts != nil && ts.state == state {
		return ts
	}
	return newTaskService(sessionID, c.Core, state, c.queuedInputRefs)
}

// taskServiceForRequestLocked 按 requestID 返回任务的 TaskService（未绑定
// → 活跃会话；调用方持有 Core.Mu）。
func (c *Coordinator) taskServiceForRequestLocked(requestID string) *TaskService {
	if st := c.sessionForRequestLocked(requestID); st != nil {
		ts := st.taskService
		state := st.taskExecution
		if ts != nil && ts.state == state {
			return ts
		}
		return newTaskService(c.SessionIDForRequest(requestID), c.Core, state, c.queuedInputRefs)
	}
	return c.currentTaskServiceForLocked(c.activeSessionIDLocked())
}

// RestoredTaskState 是一次会话恢复的任务/plan 状态装载输入。
type RestoredTaskState struct {
	PlanStack         []model.SessionPlanFrame
	ActivePlanID      string
	Transcript        []model.TranscriptEvent
	TranscriptSeq     uint64
	Checkpoints       []model.TaskCheckpoint
	ToolResults       []model.ToolResultRef
	Projection        *model.TaskContextProjection
	FallbackObjective string
}

// RestoreSessionTaskLocked 装载活跃会话恢复的任务/plan 状态（调用方持有
// Core.Mu；对应 resumeSession 的 hasRecord 分支）。
func (c *Coordinator) RestoreSessionTaskLocked(restored RestoredTaskState) {
	c.RestoreSessionTaskLockedFor(c.activeSessionIDLocked(), restored)
}

// RestoreSessionTaskLockedFor 装载指定会话恢复的任务/plan 状态（调用方持有
// Core.Mu；会话域重构：plan/task 状态写按会话路由，后台会话不再写活跃槽）。
func (c *Coordinator) RestoreSessionTaskLockedFor(sessionID string, restored RestoredTaskState) {
	st := c.sessionStateLocked(sessionID)
	st.planStack = append([]model.SessionPlanFrame(nil), restored.PlanStack...)
	st.activePlanID = restored.ActivePlanID
	st.planSequence = uint64(len(st.planStack))
	st.transcript = append([]model.TranscriptEvent(nil), restored.Transcript...)
	st.transcriptSeq = restored.TranscriptSeq
	st.taskCheckpoints = append([]model.TaskCheckpoint(nil), restored.Checkpoints...)
	st.toolResultRefs = append([]model.ToolResultRef(nil), restored.ToolResults...)
	st.pendingProviderCalls = nil
	st.pendingToolResults = nil
	st.resultRefsByToolCallID = make(map[string]string)
	c.restoreTaskProjectionLocked(st, restored.Projection, restored.FallbackObjective)
}

// ResetForNewSessionLocked 清空活跃会话任务/plan 状态（BeginNewSession /
// 无 record 的 resumeSession 分支；调用方持有 Core.Mu）。
func (c *Coordinator) ResetForNewSessionLocked() {
	st := c.activeSessionLocked()
	st.planStack = nil
	st.activePlanID = ""
	st.planSequence = 0
	st.transcript = nil
	st.transcriptSeq = 0
	st.taskExecution = nil
	st.taskService = nil
	c.syncGoalSkillActiveLocked()
	st.taskCheckpoints = nil
	st.toolResultRefs = nil
	st.pendingToolResults = nil
	st.pendingProviderCalls = nil
	st.resultRefsByToolCallID = make(map[string]string)
}
