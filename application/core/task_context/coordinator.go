package task_context

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// Coordinator 是任务执行域协调器：task 子状态、plan 子状态与 token 审计
// 在此自持；跨域协作（会话持久化、上下文装配、prompt 组装）经消费方端口
// 表达，装配根注入。
type Coordinator struct {
	*state.Core
	prompt PromptPort

	limits            func() seelexctx.Limits
	isInternalContent func(string) bool
	isOversized       func(string, int) bool
	oversizedWarning  func(string, string) string
	presentToolError  func(string, error) string
	queuedInputRefs   func() []string

	taskRuntimeState
	planRuntimeState
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
		taskRuntimeState: taskRuntimeState{
			goalSkillActive:        atomic.Bool{},
			tokenCounter:           NewCalibratedTokenCounter(),
			resultRefsByToolCallID: make(map[string]string),
		},
		planRuntimeState: planRuntimeState{
			replanInFlight: make(map[string]struct{}),
		},
	}
}

// taskRuntimeState 是任务执行域自持状态（含 plan 子状态：planStack/
// activePlanID/planSequence/replanInFlight/reactBudget）。
type taskRuntimeState struct {
	taskExecution           *TaskExecutionState
	taskService             *TaskService // 当前任务的 TaskService（与 taskExecution 同生命周期）
	goalSkillActive         atomic.Bool
	contextControlFailure   error
	contextControlRequestID string
	tokenCounter            RequestTokenCounter
	transcript              []model.TranscriptEvent
	transcriptSeq           uint64
	pendingProviderCalls    []model.TranscriptToolCall
	pendingToolResults      []model.StoredToolResult
	toolResultRefs          []model.ToolResultRef
	resultRefsByToolCallID  map[string]string
	taskCheckpoints         []model.TaskCheckpoint
}

// planRuntimeState 是 plan 帧/重规划/预算子状态（随任务域自持）。
type planRuntimeState struct {
	planStack      []model.SessionPlanFrame
	activePlanID   string
	planSequence   uint64
	replanInFlight map[string]struct{}
	reactBudget    *activeReActBudget
}

type activeReActBudget struct {
	requestID         string
	budget            prompt.ReActBudget
	toolCalls         int
	lastProgressEpoch uint64
	noProgressRounds  int
	reason            string
}

// semanticProgressLocked 返回当前任务的语义进展计数（TaskService epoch）；
// 未装配 TaskService 时回退到直接状态读取。调用方持有 Core.Mu。
func (c *Coordinator) semanticProgressLocked(requestID string) (uint64, bool) {
	ts := c.taskService
	state := c.taskExecution
	if ts != nil && ts.state == state {
		return ts.SemanticProgress(requestID)
	}
	if state == nil || state.RequestID != requestID {
		return 0, false
	}
	return state.ProgressEpoch, true
}

// ActiveSkillIDs 返回当前任务的激活 skill ID 列表（锁内快照）。
func (c *Coordinator) ActiveSkillIDs() []string {
	c.Mu.RLock()
	defer c.Mu.RUnlock()
	state := c.taskExecution
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

// CurrentTaskExecution 返回当前任务执行状态（调用方持有 Core.Mu 时读取）。
func (c *Coordinator) CurrentTaskExecution() *TaskExecutionState {
	return c.taskExecution
}

// ActivePlanID 返回当前激活 plan 帧 ID。
func (c *Coordinator) ActivePlanID() string {
	return c.activePlanID
}

// PlanSequence 返回 plan 帧序列号。
func (c *Coordinator) PlanSequence() uint64 {
	return c.planSequence
}

// PlanStack 返回会话 plan 帧栈。
func (c *Coordinator) PlanStack() []model.SessionPlanFrame {
	return c.planStack
}

// ClearActivePlanLocked 清空激活 plan 帧 ID（plan_clear 路径）。
func (c *Coordinator) ClearActivePlanLocked() {
	c.activePlanID = ""
}

// ResetPlanStateLocked 清空 plan 帧状态（新会话/恢复路径；调用方持有
// Core.Mu）。
func (c *Coordinator) ResetPlanStateLocked() {
	c.planStack = nil
	c.activePlanID = ""
	c.planSequence = 0
}

// SetPlanStateLocked 装载 plan 帧栈与激活帧（测试/恢复路径；调用方持有
// Core.Mu）。
func (c *Coordinator) SetPlanStateLocked(stack []model.SessionPlanFrame, activeID string) {
	c.planStack = stack
	c.activePlanID = activeID
	c.planSequence = uint64(len(stack))
}

// SetResultRefByCallIDLocked 登记 callID → resultRef（测试/恢复路径；调用方
// 持有 Core.Mu）。
func (c *Coordinator) SetResultRefByCallIDLocked(callID, ref string) {
	c.resultRefsByToolCallID[callID] = ref
}

// ReplanInFlight 判定重规划交互是否在途。
func (c *Coordinator) ReplanInFlight(interactionID string) bool {
	_, exists := c.replanInFlight[interactionID]
	return exists
}

// MarkReplanInFlight 登记重规划交互。
func (c *Coordinator) MarkReplanInFlight(interactionID string) {
	c.replanInFlight[interactionID] = struct{}{}
}

// DeleteReplanInFlight 移除重规划交互标记。
func (c *Coordinator) DeleteReplanInFlight(interactionID string) {
	delete(c.replanInFlight, interactionID)
}

// ErrReActBudgetExceeded 标记请求被其捕获的执行预算终止。
var ErrReActBudgetExceeded = errors.New("ReAct execution budget exhausted")

// StartReActBudgetLocked 启动请求级执行预算（调用方持有 Core.Mu）。
func (c *Coordinator) StartReActBudgetLocked(requestID string, budget prompt.ReActBudget) {
	c.reactBudget = &activeReActBudget{requestID: requestID, budget: budget}
}

// SetReActBudgetExhaustedLocked 直接置位预算终止原因（测试模拟预算耗尽；
// 调用方持有 Core.Mu）。
func (c *Coordinator) SetReActBudgetExhaustedLocked(requestID, reason string) {
	if c.reactBudget != nil && c.reactBudget.requestID == requestID {
		c.reactBudget.reason = reason
	}
}

// ClearReActBudget 清除请求级预算（自行加锁）。
func (c *Coordinator) ClearReActBudget(requestID string) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if c.reactBudget != nil && c.reactBudget.requestID == requestID {
		c.reactBudget = nil
	}
}

// RecordReActToolCall 累计一次工具调用（自行加锁）。
func (c *Coordinator) RecordReActToolCall() {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if c.reactBudget != nil {
		c.reactBudget.toolCalls++
	}
}

// AllowNextReActIteration 判定是否允许下一轮模型迭代（自行加锁）。
func (c *Coordinator) AllowNextReActIteration(turn int) bool {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	budget := c.reactBudget
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
	if c.reactBudget == nil || c.reactBudget.requestID != requestID || c.reactBudget.reason == "" {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrReActBudgetExceeded, c.reactBudget.reason)
}

// RecordContextControlFailure 把 hook 失败转移给 runChat（自行加锁）。
func (c *Coordinator) RecordContextControlFailure(requestID string, err error) {
	if err == nil {
		return
	}
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if c.Snapshot.Chat.RequestID != requestID {
		return
	}
	c.contextControlFailure = fmt.Errorf("context control: %w", err)
	c.contextControlRequestID = requestID
}

// TakeContextControlFailure 取走当前请求的 context 控制失败（自行加锁）。
func (c *Coordinator) TakeContextControlFailure(requestID string) error {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if c.contextControlRequestID != requestID {
		return nil
	}
	err := c.contextControlFailure
	c.contextControlFailure = nil
	c.contextControlRequestID = ""
	return err
}

// currentTaskService 返回当前任务的 TaskService（自行加 RLock，供无锁调用点）。
func (c *Coordinator) currentTaskService() *TaskService {
	c.Mu.RLock()
	ts := c.currentTaskServiceLocked()
	c.Mu.RUnlock()
	return ts
}

// currentTaskServiceLocked 返回当前任务的 TaskService；要求调用方持有
// Core.Mu。测试/恢复路径未装配 taskService 时，以当前状态构造瞬态实例。
func (c *Coordinator) currentTaskServiceLocked() *TaskService {
	ts := c.taskService
	state := c.taskExecution
	if ts != nil && ts.state == state {
		return ts
	}
	return newTaskService(c.Core, state, c.queuedInputRefs)
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

// RestoreSessionTaskLocked 装载会话恢复的任务/plan 状态（调用方持有
// Core.Mu；对应 resumeSession 的 hasRecord 分支）。
func (c *Coordinator) RestoreSessionTaskLocked(restored RestoredTaskState) {
	c.planStack = restored.PlanStack
	c.activePlanID = restored.ActivePlanID
	c.planSequence = uint64(len(c.planStack))
	c.transcript = append([]model.TranscriptEvent(nil), restored.Transcript...)
	c.transcriptSeq = restored.TranscriptSeq
	c.taskCheckpoints = append([]model.TaskCheckpoint(nil), restored.Checkpoints...)
	c.toolResultRefs = append([]model.ToolResultRef(nil), restored.ToolResults...)
	c.pendingProviderCalls = nil
	c.pendingToolResults = nil
	c.resultRefsByToolCallID = make(map[string]string)
	c.restoreTaskProjectionLocked(restored.Projection, restored.FallbackObjective)
}

// ResetForNewSessionLocked 清空任务/plan 状态（BeginNewSession / 无 record
// 的 resumeSession 分支；调用方持有 Core.Mu）。
func (c *Coordinator) ResetForNewSessionLocked() {
	c.planStack = nil
	c.activePlanID = ""
	c.planSequence = 0
	c.transcript = nil
	c.transcriptSeq = 0
	c.taskExecution = nil
	c.taskService = nil
	c.syncGoalSkillActiveLocked()
	c.taskCheckpoints = nil
	c.toolResultRefs = nil
	c.pendingToolResults = nil
	c.pendingProviderCalls = nil
	c.resultRefsByToolCallID = make(map[string]string)
}
