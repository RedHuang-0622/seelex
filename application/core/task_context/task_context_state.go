package task_context

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/application/prompt"
)

const activeSkillVersion = "installed-v1"

// ActivateTaskSkillsLocked 把请求级 skill 层投影进任务状态（调用方持有
// Core.Mu）。
func (c *Coordinator) ActivateTaskSkillsLocked(state *TaskExecutionState, layers []prompt.PromptLayer) {
	if state == nil {
		return
	}
	st := c.sessionStateForTaskLocked(state)
	state.TrustedSkillLayers = append([]prompt.PromptLayer(nil), layers...)
	state.ActiveSkills = make([]model.ActiveSkill, 0, len(layers))
	for _, layer := range layers {
		digest := sha256.Sum256([]byte(layer.Text))
		state.ActiveSkills = append(state.ActiveSkills, model.ActiveSkill{
			SkillID: layer.Name, Version: activeSkillVersion,
			ContentHash: hex.EncodeToString(digest[:]), Scope: "task",
			ActivatedAt: time.Now(), SourceEvent: st.transcriptSeq + 1,
		})
	}
	c.syncGoalSkillActiveLocked()
}

// SyncGoalSkillActiveLocked 把任务级 skill 集投影到 lock-free 可见性值
// （Runtime.VisibleTools 消费；调用方持有 Core.Mu）。
func (c *Coordinator) SyncGoalSkillActiveLocked() {
	c.syncGoalSkillActiveLocked()
}

func (c *Coordinator) syncGoalSkillActiveLocked() {
	active := false
	if state := c.activeSessionLocked().taskExecution; state != nil {
		for _, skill := range state.ActiveSkills {
			if skill.SkillID == "goal" {
				active = true
				break
			}
		}
	}
	c.goalSkillActive.Store(active)
}

// AppendTranscriptEventLocked 追加一条 append-only transcript 事件（seq 自增；
// 调用方持有 Core.Mu）。事件归属会话由 event.TaskID 反查，缺省活跃会话。
func (c *Coordinator) AppendTranscriptEventLocked(event model.TranscriptEvent) model.TranscriptEvent {
	st := c.activeSessionLocked()
	if event.TaskID != "" {
		if byRequest := c.sessionForRequestLocked(event.TaskID); byRequest != nil {
			st = byRequest
		}
	}
	st.transcriptSeq++
	event.Seq = st.transcriptSeq
	if event.TaskID == "" && st.taskExecution != nil {
		event.TaskID = st.taskExecution.RequestID
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	event.TokenCount = c.CountTranscriptEvent(event)
	st.transcript = append(st.transcript, event)
	return event
}

// AppendTranscriptEventForLocked 追加一条指定会话的 transcript 事件（调用
// 方持有 Core.Mu；hook 等显式会话路径用）。
func (c *Coordinator) AppendTranscriptEventForLocked(sessionID string, event model.TranscriptEvent) model.TranscriptEvent {
	st := c.sessionStateLocked(sessionID)
	st.transcriptSeq++
	event.Seq = st.transcriptSeq
	if event.TaskID == "" && st.taskExecution != nil {
		event.TaskID = st.taskExecution.RequestID
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	event.TokenCount = c.CountTranscriptEvent(event)
	st.transcript = append(st.transcript, event)
	return event
}

// ImportEngineHistoryAsTranscriptLocked 把引擎既有历史导入活跃会话
// transcript（装配期/恢复路径；调用方持有 Core.Mu）。
func (c *Coordinator) ImportEngineHistoryAsTranscriptLocked(history []contract.EngineMessage) {
	st := c.activeSessionLocked()
	c.importEngineHistoryLocked(st, history)
}

func (c *Coordinator) importEngineHistoryLocked(st *sessionTaskRuntime, history []contract.EngineMessage) {
	for _, message := range history {
		if message.Role == "system" || c.isInternalContent(message.Content) {
			continue
		}
		event := model.TranscriptEvent{
			Role: message.Role, ReasoningContent: message.ReasoningContent, Content: message.Content,
			ToolCallID: message.ToolCallID, Name: message.Name,
		}
		if event.Role == "tool" && c.isOversized(event.Content, defaultToolResultLimit()) {
			stored := c.storeToolResultLocked(st, event.Name, event.Content)
			event.Content = c.oversizedWarning(event.Name, stored.Ref)
			event.ResultRef = stored.Ref
			st.resultRefsByToolCallID[event.ToolCallID] = stored.Ref
		}
		for _, call := range message.ToolCalls {
			event.ToolCalls = append(event.ToolCalls, model.TranscriptToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
		}
		c.appendTranscriptEventLocked(st, event)
	}
}

func (c *Coordinator) appendTranscriptEventLocked(st *sessionTaskRuntime, event model.TranscriptEvent) model.TranscriptEvent {
	st.transcriptSeq++
	event.Seq = st.transcriptSeq
	if event.TaskID == "" && st.taskExecution != nil {
		event.TaskID = st.taskExecution.RequestID
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	event.TokenCount = c.CountTranscriptEvent(event)
	st.transcript = append(st.transcript, event)
	return event
}

// CountTranscriptEvent 估算一条 transcript 事件的 token 数。
func (c *Coordinator) CountTranscriptEvent(event model.TranscriptEvent) int {
	message := contract.EngineMessage{
		Role: event.Role, ReasoningContent: event.ReasoningContent, Content: event.Content,
		ContentSet: true, ToolCallID: event.ToolCallID, Name: event.Name,
	}
	for _, call := range event.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, contract.EngineToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	return c.tokenCounter.CountMessage(message)
}

// RecordLLMComplete 记录一次 LLM 完成（真实 usage 校准 + assistant 事件；
// 自行加锁）。ctx 携带会话 ID 时路由到对应会话。
func (c *Coordinator) RecordLLMComplete(ctx context.Context, info session.LLMInfo) {
	if info.Response == "" && len(info.ToolCalls) == 0 && info.Usage == nil {
		return
	}
	c.Mu.Lock()
	defer c.Mu.Unlock()
	st := c.runtimeForContextLocked(ctx)
	state := st.taskExecution
	if state == nil || state.RequestID != c.Snapshot.Chat.RequestID {
		return
	}
	if info.Usage != nil {
		state.TokenAudit.ActualPromptTokens = info.Usage.PromptTokens
		state.TokenAudit.UpdatedAt = time.Now()
		// 用真实 usage 反馈校准估算因子（同一请求的估算与实际配对）。
		if counter, ok := c.tokenCounter.(*CalibratedTokenCounter); ok &&
			state.TokenAudit.EstimatedPromptTokens > 0 && info.Usage.PromptTokens > 0 {
			counter.Observe(state.TokenAudit.EstimatedPromptTokens, info.Usage.PromptTokens)
		}
	}
	if info.Response == "" && len(info.ToolCalls) == 0 {
		return
	}
	event := model.TranscriptEvent{Role: "assistant", Content: info.Response}
	for _, call := range info.ToolCalls {
		item := model.TranscriptToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments}
		event.ToolCalls = append(event.ToolCalls, item)
		st.pendingProviderCalls = append(st.pendingProviderCalls, item)
	}
	c.appendTranscriptEventLocked(st, event)
}

// EnsureToolCallTranscriptLocked 保证工具调用宣告已入指定会话 transcript
// （缺失时补一条 assistant 事件；调用方持有 Core.Mu）。
func (c *Coordinator) EnsureToolCallTranscriptLocked(sessionID, name, fallbackID, arguments string) {
	st := c.sessionStateLocked(sessionID)
	for _, call := range st.pendingProviderCalls {
		if call.Name == name && call.Arguments == arguments {
			return
		}
	}
	call := model.TranscriptToolCall{ID: fallbackID, Name: name, Arguments: arguments}
	st.pendingProviderCalls = append(st.pendingProviderCalls, call)
	c.appendTranscriptEventLocked(st, model.TranscriptEvent{Role: "assistant", ToolCalls: []model.TranscriptToolCall{call}})
}

// RecordToolTranscriptLocked 记录指定会话工具结果事件（错误呈现/超限引用；
// 返回可见内容与结果引用；调用方持有 Core.Mu）。
func (c *Coordinator) RecordToolTranscriptLocked(sessionID, name, fallbackID, arguments, result string, toolErr error) (string, string) {
	st := c.sessionStateLocked(sessionID)
	callID := fallbackID
	for index, call := range st.pendingProviderCalls {
		if call.Name != name || (arguments != "" && call.Arguments != arguments) {
			continue
		}
		callID = call.ID
		st.pendingProviderCalls = append(st.pendingProviderCalls[:index], st.pendingProviderCalls[index+1:]...)
		break
	}
	content := result
	resultRef := ""
	if toolErr != nil {
		content = c.presentToolError(name, toolErr)
	} else if c.isOversized(result, defaultToolResultLimit()) {
		stored := c.storeToolResultLocked(st, name, result)
		resultRef = stored.Ref
		content = c.oversizedWarning(name, resultRef)
		st.resultRefsByToolCallID[callID] = resultRef
	}
	c.appendTranscriptEventLocked(st, model.TranscriptEvent{
		Role: "tool", Content: content, ToolCallID: callID, Name: name, ResultRef: resultRef,
	})
	return content, resultRef
}

// defaultToolResultLimit 返回工具结果字符预算（seelex.yaml limits 段
// max_tool_result_chars，默认 20000）。
func defaultToolResultLimit() int {
	return limits.Get().MaxToolResultChars
}

// DefaultToolResultLimit 返回工具结果字符预算（导出面；根包兼容包装用）。
func DefaultToolResultLimit() int {
	return defaultToolResultLimit()
}

// StoreToolResultLocked 把超限工具结果以引用形式存储（活跃会话；内容 +
// 摘要元数据）。
func (c *Coordinator) StoreToolResultLocked(name, content string) model.StoredToolResult {
	st := c.activeSessionLocked()
	return c.storeToolResultLocked(st, name, content)
}

// StoreToolResultForLocked 把超限工具结果以引用形式存储到指定会话。
func (c *Coordinator) StoreToolResultForLocked(sessionID, name, content string) model.StoredToolResult {
	return c.storeToolResultLocked(c.sessionStateLocked(sessionID), name, content)
}

func (c *Coordinator) storeToolResultLocked(st *sessionTaskRuntime, name, content string) model.StoredToolResult {
	digest := sha256.Sum256([]byte(name + "\x00" + content))
	digestText := hex.EncodeToString(digest[:])
	result := model.StoredToolResult{
		ToolResultRef: model.ToolResultRef{
			Ref: "tr-" + digestText[:24], Tool: name, Digest: "sha256:" + digestText,
			Size: len([]byte(content)), TokenCount: c.tokenCounter.CountText(content), CreatedAt: time.Now(),
		},
		Content: content,
	}
	for _, existing := range st.toolResultRefs {
		if existing.Ref == result.Ref {
			return result
		}
	}
	st.toolResultRefs = append(st.toolResultRefs, result.ToolResultRef)
	st.pendingToolResults = append(st.pendingToolResults, result)
	return result
}

// EnsureFinalAssistantTranscript 在请求结束时补一条可见 assistant 终态事件
// （去重；自行加锁；requestID 反查会话）。
func (c *Coordinator) EnsureFinalAssistantTranscript(requestID, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	c.Mu.Lock()
	defer c.Mu.Unlock()
	st := c.sessionForRequestLocked(requestID)
	if st == nil {
		st = c.activeSessionLocked()
	}
	if state := st.taskExecution; state == nil || state.RequestID != requestID {
		return
	}
	if len(st.transcript) > 0 {
		last := st.transcript[len(st.transcript)-1]
		if last.TaskID == requestID && last.Role == "assistant" && last.Content == content && len(last.ToolCalls) == 0 {
			return
		}
	}
	c.appendTranscriptEventLocked(st, model.TranscriptEvent{TaskID: requestID, Role: "assistant", Content: content})
}

// sessionStateForTaskLocked 返回任务状态所属会话的运行时（调用方持有
// Core.Mu）。
func (c *Coordinator) sessionStateForTaskLocked(state *TaskExecutionState) *sessionTaskRuntime {
	if state != nil {
		if st := c.sessionForRequestLocked(state.RequestID); st != nil {
			return st
		}
	}
	return c.activeSessionLocked()
}

// TaskProjectionLocked 构建指定会话任务的权威投影（会话归档用；调用方持有
// Core.Mu）。
func (c *Coordinator) TaskProjectionLocked(sessionID string) *model.TaskContextProjection {
	st := c.sessionStateLocked(sessionID)
	state := st.taskExecution
	if state == nil {
		return nil
	}
	projectID := ""
	if c.Snapshot.CurrentWorkspace != nil {
		projectID = c.Snapshot.CurrentWorkspace.ID
	}
	checkpoint := c.buildTaskCheckpointLocked(st, state)
	objectiveRef := ""
	if checkpoint.CoversEventRange.Start != 0 {
		objectiveRef = fmt.Sprintf("event:%d", checkpoint.CoversEventRange.Start)
	}
	return &model.TaskContextProjection{
		SchemaVersion: 1, ProjectID: projectID, SessionID: sessionID, TaskID: state.RequestID,
		Status: state.Status, ObjectiveRef: objectiveRef,
		ActiveSkills: append([]model.ActiveSkill(nil), state.ActiveSkills...), ActivePlan: c.ActivePlanProjectionLocked(),
		Checkpoint: checkpoint, TokenAudit: state.TokenAudit, UpdatedAt: time.Now(),
	}
}

// BuildTaskCheckpointLocked 构建任务 checkpoint（调用方持有 Core.Mu）。
func (c *Coordinator) BuildTaskCheckpointLocked(state *TaskExecutionState) model.TaskCheckpoint {
	st := c.sessionStateForTaskLocked(state)
	return c.buildTaskCheckpointLocked(st, state)
}

func (c *Coordinator) buildTaskCheckpointLocked(st *sessionTaskRuntime, state *TaskExecutionState) model.TaskCheckpoint {
	checkpoint := model.TaskCheckpoint{}
	if state.InheritedCheckpoint != nil {
		checkpoint = CloneTaskCheckpoint(*state.InheritedCheckpoint)
	}
	checkpoint.Version = state.ContextVersion
	checkpoint.CoversEventRange = extendEventRange(checkpoint.CoversEventRange, eventRangeForTask(st.transcript, state.RequestID))
	checkpoint.UpdatedAt = time.Now()
	keys := make([]string, 0, len(state.checkpoints))
	for key := range state.checkpoints {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		node := state.checkpoints[key]
		record := fmt.Sprintf("node=%s status=%s", node.NodeKey, node.Status)
		switch node.Status {
		case string(model.NodeCompleted):
			checkpoint.CompletedWork = AppendUniqueStrings(checkpoint.CompletedWork, record)
		case string(model.NodeFailed), string(model.NodeAborted), string(model.NodeCanceled), string(model.NodePanicked):
			checkpoint.Failures = AppendUniqueStrings(checkpoint.Failures, record+boundedFailure(node.Failure))
		default:
			checkpoint.PendingWork = AppendUniqueStrings(checkpoint.PendingWork, record)
		}
		checkpoint.ChangedFiles = AppendUniqueStrings(checkpoint.ChangedFiles, node.ChangedFiles...)
		checkpoint.Artifacts = AppendUniqueStrings(checkpoint.Artifacts, node.Artifacts...)
	}
	if state.Terminal != nil {
		checkpoint.Artifacts = AppendUniqueStrings(checkpoint.Artifacts, state.Terminal.Artifacts...)
		checkpoint.Decisions = AppendUniqueStrings(checkpoint.Decisions, state.Terminal.DecisionQuestion)
	}
	for _, event := range st.transcript {
		if event.TaskID == state.RequestID && event.ResultRef != "" {
			checkpoint.ToolResultRefs = AppendUniqueStrings(checkpoint.ToolResultRefs, event.ResultRef)
		}
	}
	return checkpoint
}

func extendEventRange(existing, current model.EventRange) model.EventRange {
	if existing.Start == 0 {
		return current
	}
	if current.Start == 0 {
		return existing
	}
	if current.Start < existing.Start {
		existing.Start = current.Start
	}
	if current.End > existing.End {
		existing.End = current.End
	}
	return existing
}

func boundedFailure(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return " failure=" + BoundedEvidence(value)
}

func eventRangeForTask(events []model.TranscriptEvent, taskID string) model.EventRange {
	rangeForTask := model.EventRange{}
	for _, event := range events {
		if event.TaskID != taskID {
			continue
		}
		if rangeForTask.Start == 0 {
			rangeForTask.Start = event.Seq
		}
		rangeForTask.End = event.Seq
	}
	return rangeForTask
}

// AppendUniqueStrings 追加去重后的非空字符串。
func AppendUniqueStrings(values []string, incoming ...string) []string {
	for _, value := range incoming {
		value = strings.TrimSpace(value)
		if value != "" && !ContainsString(values, value) {
			values = append(values, value)
		}
	}
	return values
}

// ActivePlanProjectionLocked 返回活跃会话当前激活 Plan 的只读投影（调用方
// 持有 Core.Mu）。
func (c *Coordinator) ActivePlanProjectionLocked() *model.ActivePlanProjection {
	st := c.activeSessionLocked()
	return ActivePlanProjection(c.Snapshot.Runtime.Plan, st.activePlanID, st.planSequence)
}

func (c *Coordinator) restoreTaskProjectionLocked(st *sessionTaskRuntime, projection *model.TaskContextProjection, fallbackObjective string) {
	c.prompt.ClearSkillLayers()
	if projection == nil {
		st.taskExecution = nil
		st.taskService = nil
		c.syncGoalSkillActiveLocked()
		return
	}
	objective := c.resolveObjectiveRefLocked(st, projection.ObjectiveRef)
	if objective == "" {
		objective = strings.TrimSpace(fallbackObjective)
	}
	state := NewTaskExecutionState(projection.TaskID, objective, c.prompt.CurrentEffort())
	state.Status = projection.Status
	state.ContextVersion = projection.Checkpoint.Version
	if state.ContextVersion == 0 {
		state.ContextVersion = 1
	}
	checkpoint := CloneTaskCheckpoint(projection.Checkpoint)
	if HasSubstantiveCheckpoint(checkpoint) {
		state.InheritedCheckpoint = &checkpoint
	}
	state.ActiveSkills = append([]model.ActiveSkill(nil), projection.ActiveSkills...)
	state.TokenAudit = projection.TokenAudit
	if frame := ActivePlanFrame(st.planStack, st.activePlanID); frame != nil {
		state.PlanArguments = frame.Arguments
	}
	for _, active := range projection.ActiveSkills {
		skill, ok := c.Deps.Skills.Get(active.SkillID)
		if !ok {
			continue
		}
		digest := sha256.Sum256([]byte(skill.Prompt))
		if hex.EncodeToString(digest[:]) != active.ContentHash {
			continue
		}
		layer := prompt.PromptLayer{Kind: "skill", Name: skill.Name, Text: skill.Prompt}
		state.TrustedSkillLayers = append(state.TrustedSkillLayers, layer)
		c.prompt.PushSkillLayer(layer.Kind, layer.Name, layer.Text)
	}
	st.taskExecution = state
	st.taskService = newTaskService(c.activeSessionIDLocked(), c.Core, state, c.queuedInputRefs)
	c.syncGoalSkillActiveLocked()
}

func (c *Coordinator) resolveObjectiveRefLocked(st *sessionTaskRuntime, objectiveRef string) string {
	const eventPrefix = "event:"
	if !strings.HasPrefix(objectiveRef, eventPrefix) {
		return strings.TrimSpace(objectiveRef)
	}
	sequence, err := strconv.ParseUint(strings.TrimPrefix(objectiveRef, eventPrefix), 10, 64)
	if err != nil {
		return ""
	}
	for _, event := range st.transcript {
		if event.Seq == sequence && event.Role == "user" {
			return event.Content
		}
	}
	return ""
}

// RecordContextCompactionLocked 记录一次上下文压缩（仅运行中任务；调用方
// 持有 Core.Mu；requestID 反查会话）。
func (c *Coordinator) RecordContextCompactionLocked(requestID string, compaction model.ContextCompaction) bool {
	st := c.sessionForRequestLocked(requestID)
	if st == nil {
		st = c.activeSessionLocked()
	}
	state := st.taskExecution
	if state == nil || state.RequestID != requestID || state.Status != StatusRunning {
		return false
	}
	state.ContextCompactions = append(state.ContextCompactions, compaction)
	if task := c.Snapshot.Task; task != nil && task.RequestID == requestID {
		task.ContextCompactions = append([]model.ContextCompaction(nil), state.ContextCompactions...)
		task.UpdatedAt = compaction.CompactedAt
	}
	return true
}

// SetTaskStateLocked 把任务可见状态写入快照（调用方持有 Core.Mu；requestID
// 反查会话）。非活跃会话（后台并行执行）跳过共享快照写入，避免污染活跃
// 会话投影；任务内部状态由调用方独立维护。
func (c *Coordinator) SetTaskStateLocked(requestID string, status model.TaskStatus, summary string) {
	sessionID := c.SessionIDForRequest(requestID)
	if !c.isActiveSessionLocked(sessionID) {
		return
	}
	st := c.sessionStateLocked(sessionID)
	var compactions []model.ContextCompaction
	if taskState := st.taskExecution; taskState != nil && taskState.RequestID == requestID {
		compactions = append([]model.ContextCompaction(nil), taskState.ContextCompactions...)
	}
	c.Snapshot.Task = &model.TaskState{
		RequestID:          requestID,
		Status:             status,
		Summary:            strings.TrimSpace(summary),
		ContextCompactions: compactions,
		UpdatedAt:          time.Now(),
	}
}

// isActiveSessionLocked 判定会话是否为共享快照归属会话（调用方持有
// Core.Mu）。
func (c *Coordinator) isActiveSessionLocked(sessionID string) bool {
	return sessionID == c.Snapshot.Session.ID
}

// InterruptTaskLocked 把任务置为中断（快照 + 内部状态；调用方持有 Core.Mu）。
func (c *Coordinator) InterruptTaskLocked(requestID, summary string) {
	c.SetTaskStateLocked(requestID, model.TaskInterrupted, summary)
	st := c.sessionForRequestLocked(requestID)
	if st == nil {
		st = c.activeSessionLocked()
	}
	if state := st.taskExecution; state != nil && state.RequestID == requestID {
		state.Status = StatusInterrupted
	}
}

// FailTaskLocked 把任务置为失败（快照 + 内部状态；调用方持有 Core.Mu）。
func (c *Coordinator) FailTaskLocked(requestID, summary string) {
	c.SetTaskStateLocked(requestID, model.TaskFailed, summary)
	st := c.sessionForRequestLocked(requestID)
	if st == nil {
		st = c.activeSessionLocked()
	}
	if state := st.taskExecution; state != nil && state.RequestID == requestID {
		state.Status = StatusFailed
	}
}

// ResumeTaskLocked 恢复被压缩/中断的任务（快照 + 内部状态 + epoch 推进；
// 调用方持有 Core.Mu）。
func (c *Coordinator) ResumeTaskLocked(requestID, summary string) {
	st := c.sessionForRequestLocked(requestID)
	if st == nil {
		st = c.activeSessionLocked()
	}
	state := st.taskExecution
	if state == nil || state.RequestID != requestID {
		return
	}
	state.Status = StatusRunning
	state.ProgressEpoch++
	c.SetTaskStateLocked(requestID, model.TaskProgressing, summary)
}

// RememberCheckpointLocked 按 version 替换或追加活跃会话 checkpoint（调用
// 方持有 Core.Mu）。
func (c *Coordinator) RememberCheckpointLocked(checkpoint model.TaskCheckpoint) {
	st := c.activeSessionLocked()
	for index := range st.taskCheckpoints {
		if st.taskCheckpoints[index].Version == checkpoint.Version && checkpoint.Version != 0 {
			st.taskCheckpoints[index] = checkpoint
			return
		}
	}
	st.taskCheckpoints = append(st.taskCheckpoints, checkpoint)
}

// BeginTask 为活跃会话当前请求创建任务执行状态与 TaskService（调用方持有
// Core.Mu）。
func (c *Coordinator) BeginTask(requestID, objective, effort string, previous *TaskExecutionState, checkpoint model.TaskCheckpoint) *TaskExecutionState {
	return c.BeginTaskFor(c.activeSessionIDLocked(), requestID, objective, effort, previous, checkpoint)
}

// BeginTaskFor 为指定会话当前请求创建任务执行状态与 TaskService（调用方
// 持有 Core.Mu）。
func (c *Coordinator) BeginTaskFor(sessionID, requestID, objective, effort string, previous *TaskExecutionState, checkpoint model.TaskCheckpoint) *TaskExecutionState {
	st := c.sessionStateLocked(sessionID)
	state := continuationTaskExecutionState(requestID, objective, effort, previous, checkpoint)
	st.taskExecution = state
	st.taskService = newTaskService(sessionID, c.Core, state, c.queuedInputRefs)
	c.bindRequestLocked(requestID, sessionID)
	return state
}

// ContinuationSummary 返回活跃会话当前任务的恢复摘要（requestID 不匹配 →
// ""）。
func (c *Coordinator) ContinuationSummary(requestID string) string {
	st := c.activeSessionLocked()
	state := st.taskExecution
	if state == nil || state.RequestID != requestID {
		return ""
	}
	return state.ContextSummary()
}

// Transcript 返回活跃会话 append-only 事件。
func (c *Coordinator) Transcript() []model.TranscriptEvent {
	return c.activeSessionLocked().transcript
}

// TranscriptFor 返回指定会话 append-only 事件。
func (c *Coordinator) TranscriptFor(sessionID string) []model.TranscriptEvent {
	return c.sessionStateLocked(sessionID).transcript
}

// PendingToolResults 返回活跃会话尚未随会话原子提交的工具结果。
func (c *Coordinator) PendingToolResults() []model.StoredToolResult {
	return c.activeSessionLocked().pendingToolResults
}

// TaskCheckpoints 返回活跃会话任务 checkpoint 序列。
func (c *Coordinator) TaskCheckpoints() []model.TaskCheckpoint {
	return c.activeSessionLocked().taskCheckpoints
}

// ToolResultRefs 返回活跃会话工具结果引用表。
func (c *Coordinator) ToolResultRefs() []model.ToolResultRef {
	return c.activeSessionLocked().toolResultRefs
}

// ToolResultRefByCallID 按工具调用 ID 查活跃会话结果引用（未找到 → ""）。
func (c *Coordinator) ToolResultRefByCallID(callID string) string {
	return c.activeSessionLocked().resultRefsByToolCallID[callID]
}

// ResultRefsByCallID 返回活跃会话 callID → resultRef 全量拷贝（上下文拒绝
// 路径）。
func (c *Coordinator) ResultRefsByCallID() map[string]string {
	refs := make(map[string]string, len(c.activeSessionLocked().resultRefsByToolCallID))
	for callID, resultRef := range c.activeSessionLocked().resultRefsByToolCallID {
		refs[callID] = resultRef
	}
	return refs
}

// ResultRefsByCallIDFor 返回指定会话 callID → resultRef 全量拷贝。
func (c *Coordinator) ResultRefsByCallIDFor(sessionID string) map[string]string {
	refs := make(map[string]string, len(c.sessionStateLocked(sessionID).resultRefsByToolCallID))
	for callID, resultRef := range c.sessionStateLocked(sessionID).resultRefsByToolCallID {
		refs[callID] = resultRef
	}
	return refs
}

// PlanStackFor 返回指定会话 plan 帧栈。
func (c *Coordinator) PlanStackFor(sessionID string) []model.SessionPlanFrame {
	return c.sessionStateLocked(sessionID).planStack
}

// SessionIDForRequest 按 requestID 反查会话 ID（未绑定 → 活跃会话）。
func (c *Coordinator) SessionIDForRequest(requestID string) string {
	if requestID == "" {
		return c.activeSessionIDLocked()
	}
	if sessionID, ok := c.requestToSession[requestID]; ok {
		return sessionID
	}
	return c.activeSessionIDLocked()
}

// ActivePlanProjectionLockedFor 返回指定会话激活 Plan 的只读投影（调用方
// 持有 Core.Mu）。
func (c *Coordinator) ActivePlanProjectionLockedFor(sessionID string) *model.ActivePlanProjection {
	st := c.sessionStateLocked(sessionID)
	return ActivePlanProjection(c.Snapshot.Runtime.Plan, st.activePlanID, st.planSequence)
}

// SyncActivePlanFrameLocked 把当前快照 Plan 收敛进活跃会话激活帧（调用方
// 持有 Core.Mu）。
func (c *Coordinator) SyncActivePlanFrameLocked(now time.Time) {
	st := c.activeSessionLocked()
	if st.activePlanID == "" || len(st.planStack) == 0 {
		return
	}
	for index := range st.planStack {
		frame := &st.planStack[index]
		if frame.ID != st.activePlanID {
			continue
		}
		frame.Plan = model.CloneRuntimeState(model.RuntimeState{Plan: c.Snapshot.Runtime.Plan}).Plan
		frame.UpdatedAt = now
		return
	}
}

// PushLoadedPlanLocked 把 plan_load 参数追加为活跃会话新的激活帧（调用方
// 持有 Core.Mu）。
func (c *Coordinator) PushLoadedPlanLocked(arguments string, now time.Time) {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" || c.Snapshot.Runtime.Plan == nil {
		return
	}
	st := c.activeSessionLocked()
	st.planSequence++
	planID := fmt.Sprintf("plan-%d", st.planSequence)
	st.planStack = append(st.planStack, model.SessionPlanFrame{
		ID:        planID,
		Plan:      model.CloneRuntimeState(model.RuntimeState{Plan: c.Snapshot.Runtime.Plan}).Plan,
		Arguments: arguments,
		LoadedAt:  now,
		UpdatedAt: now,
	})
	st.activePlanID = planID
}

// RemoveCommittedToolResultsLocked 清理活跃会话已随会话快照提交的待定工具
// 结果。
func (c *Coordinator) RemoveCommittedToolResultsLocked(committed []model.StoredToolResult) {
	st := c.activeSessionLocked()
	if len(committed) == 0 || len(st.pendingToolResults) == 0 {
		return
	}
	refs := make(map[string]struct{}, len(committed))
	for _, result := range committed {
		refs[result.Ref] = struct{}{}
	}
	pending := st.pendingToolResults[:0]
	for _, result := range st.pendingToolResults {
		if _, ok := refs[result.Ref]; !ok {
			pending = append(pending, result)
		}
	}
	st.pendingToolResults = pending
}

// TokenCounterName 返回当前 token 计数器标识。
func (c *Coordinator) TokenCounterName() string {
	return c.tokenCounter.Name()
}

// CountRequestTokens 估算一次完整请求 token 数。
func (c *Coordinator) CountRequestTokens(systemPrompt string, history []contract.EngineMessage, currentInput string, tools []model.Tool) int {
	return c.tokenCounter.CountRequest(systemPrompt, history, currentInput, tools)
}

// CountTextTokens 估算文本 token 数。
func (c *Coordinator) CountTextTokens(value string) int {
	return c.tokenCounter.CountText(value)
}

// VerifyAndApply 是终态/打点工具的入口（Registry handler 面）。ctx 携带
// 会话 ID 时路由到对应会话，否则按活跃会话。
func (c *Coordinator) VerifyAndApply(ctx context.Context, kind, argsJSON string) (string, error) {
	c.Mu.RLock()
	ts := c.taskServiceForContextLocked(ctx)
	c.Mu.RUnlock()
	return ts.VerifyAndApply(ctx, kind, argsJSON)
}

// FinalizeTask 把自然停止转换为可审计完成/交接（OnChatEnd 入口；summary
// requestID 反查会话）。
func (c *Coordinator) FinalizeTask(ctx context.Context, summary ChatEndSummary) error {
	c.Mu.RLock()
	ts := c.taskServiceForRequestLocked(summary.RequestID)
	c.Mu.RUnlock()
	_, err := ts.OnChatEnd(ctx, summary)
	return err
}

// OnChatEnd 把自然停止转换为可审计完成/交接，返回可见任务状态。
func (c *Coordinator) OnChatEnd(ctx context.Context, summary ChatEndSummary) (model.TaskState, error) {
	c.Mu.RLock()
	ts := c.taskServiceForRequestLocked(summary.RequestID)
	c.Mu.RUnlock()
	return ts.OnChatEnd(ctx, summary)
}

// CurrentTaskResumeRecord 返回活跃会话当前任务的终态恢复记录。
func (c *Coordinator) CurrentTaskResumeRecord() TaskResumeRecord {
	return c.currentTaskService().ResumeRecord()
}

// SetTaskProjectionFlushLocked 注入活跃会话 TaskService 的 Plan 投影 flush
// 钩子（测试模拟延迟投影；调用方持有 Core.Mu）。
func (c *Coordinator) SetTaskProjectionFlushLocked(flush func(context.Context) error) {
	if ts := c.activeSessionLocked().taskService; ts != nil {
		ts.projection = &planProjectionReader{Core: c.Core, flush: flush}
	}
}

// ObserveTool 记录工具执行观测（调用方持有 Core.Mu；observation requestID
// 反查会话）。
func (c *Coordinator) ObserveTool(observation ToolObservation) {
	if st := c.sessionForRequestLocked(observation.RequestID); st != nil {
		ts := st.taskService
		state := st.taskExecution
		if ts == nil || ts.state != state {
			ts = newTaskService(c.SessionIDForRequest(observation.RequestID), c.Core, state, c.queuedInputRefs)
		}
		ts.ObserveTool(observation)
		return
	}
	c.currentTaskServiceLocked().ObserveTool(observation)
}

// ObservePlanEvent 记录 plan 事件投影观测（调用方持有 Core.Mu；活跃会话）。
func (c *Coordinator) ObservePlanEvent(event PlanEvent) {
	c.currentTaskServiceLocked().ObservePlanEvent(event)
}

// ObserveModelOutput 记录模型回复观测（自行加锁；output requestID 反查
// 会话）。
func (c *Coordinator) ObserveModelOutput(ctx context.Context, output ModelOutput) error {
	c.Mu.RLock()
	ts := c.taskServiceForRequestLocked(output.RequestID)
	c.Mu.RUnlock()
	return ts.ObserveModelOutput(ctx, output)
}

// taskServiceForContextLocked 按 ctx 会话 ID 返回任务的 TaskService（未注入
// → 活跃会话；调用方持有 Core.Mu）。
func (c *Coordinator) taskServiceForContextLocked(ctx context.Context) *TaskService {
	if sessionID := SessionIDFromContext(ctx); sessionID != "" {
		return c.currentTaskServiceForLocked(sessionID)
	}
	return c.currentTaskServiceForLocked(c.activeSessionIDLocked())
}
