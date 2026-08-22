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
	state.TrustedSkillLayers = append([]prompt.PromptLayer(nil), layers...)
	state.ActiveSkills = make([]model.ActiveSkill, 0, len(layers))
	for _, layer := range layers {
		digest := sha256.Sum256([]byte(layer.Text))
		state.ActiveSkills = append(state.ActiveSkills, model.ActiveSkill{
			SkillID: layer.Name, Version: activeSkillVersion,
			ContentHash: hex.EncodeToString(digest[:]), Scope: "task",
			ActivatedAt: time.Now(), SourceEvent: c.transcriptSeq + 1,
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
	if state := c.taskExecution; state != nil {
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
// 调用方持有 Core.Mu）。
func (c *Coordinator) AppendTranscriptEventLocked(event model.TranscriptEvent) model.TranscriptEvent {
	c.transcriptSeq++
	event.Seq = c.transcriptSeq
	if event.TaskID == "" && c.taskExecution != nil {
		event.TaskID = c.taskExecution.RequestID
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	event.TokenCount = c.CountTranscriptEvent(event)
	c.transcript = append(c.transcript, event)
	return event
}

// ImportEngineHistoryAsTranscriptLocked 把引擎既有历史导入 transcript
// （装配期/恢复路径；调用方持有 Core.Mu）。
func (c *Coordinator) ImportEngineHistoryAsTranscriptLocked(history []contract.EngineMessage) {
	for _, message := range history {
		if message.Role == "system" || c.isInternalContent(message.Content) {
			continue
		}
		event := model.TranscriptEvent{
			Role: message.Role, ReasoningContent: message.ReasoningContent, Content: message.Content,
			ToolCallID: message.ToolCallID, Name: message.Name,
		}
		if event.Role == "tool" && c.isOversized(event.Content, defaultToolResultLimit()) {
			stored := c.StoreToolResultLocked(event.Name, event.Content)
			event.Content = c.oversizedWarning(event.Name, stored.Ref)
			event.ResultRef = stored.Ref
			c.resultRefsByToolCallID[event.ToolCallID] = stored.Ref
		}
		for _, call := range message.ToolCalls {
			event.ToolCalls = append(event.ToolCalls, model.TranscriptToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
		}
		c.AppendTranscriptEventLocked(event)
	}
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
// 自行加锁）。
func (c *Coordinator) RecordLLMComplete(info session.LLMInfo) {
	if info.Response == "" && len(info.ToolCalls) == 0 && info.Usage == nil {
		return
	}
	c.Mu.Lock()
	defer c.Mu.Unlock()
	state := c.taskExecution
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
		c.pendingProviderCalls = append(c.pendingProviderCalls, item)
	}
	c.AppendTranscriptEventLocked(event)
}

// EnsureToolCallTranscriptLocked 保证工具调用宣告已入 transcript（缺失时
// 补一条 assistant 事件；调用方持有 Core.Mu）。
func (c *Coordinator) EnsureToolCallTranscriptLocked(name, fallbackID, arguments string) {
	for _, call := range c.pendingProviderCalls {
		if call.Name == name && call.Arguments == arguments {
			return
		}
	}
	call := model.TranscriptToolCall{ID: fallbackID, Name: name, Arguments: arguments}
	c.pendingProviderCalls = append(c.pendingProviderCalls, call)
	c.AppendTranscriptEventLocked(model.TranscriptEvent{Role: "assistant", ToolCalls: []model.TranscriptToolCall{call}})
}

// RecordToolTranscriptLocked 记录工具结果事件（错误呈现/超限引用；返回
// 可见内容与结果引用；调用方持有 Core.Mu）。
func (c *Coordinator) RecordToolTranscriptLocked(name, fallbackID, arguments, result string, toolErr error) (string, string) {
	callID := fallbackID
	for index, call := range c.pendingProviderCalls {
		if call.Name != name || (arguments != "" && call.Arguments != arguments) {
			continue
		}
		callID = call.ID
		c.pendingProviderCalls = append(c.pendingProviderCalls[:index], c.pendingProviderCalls[index+1:]...)
		break
	}
	content := result
	resultRef := ""
	if toolErr != nil {
		content = c.presentToolError(name, toolErr)
	} else if c.isOversized(result, defaultToolResultLimit()) {
		stored := c.StoreToolResultLocked(name, result)
		resultRef = stored.Ref
		content = c.oversizedWarning(name, resultRef)
		c.resultRefsByToolCallID[callID] = resultRef
	}
	c.AppendTranscriptEventLocked(model.TranscriptEvent{
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

// StoreToolResultLocked 把超限工具结果以引用形式存储（内容 + 摘要元数据）。
func (c *Coordinator) StoreToolResultLocked(name, content string) model.StoredToolResult {
	digest := sha256.Sum256([]byte(name + "\x00" + content))
	digestText := hex.EncodeToString(digest[:])
	result := model.StoredToolResult{
		ToolResultRef: model.ToolResultRef{
			Ref: "tr-" + digestText[:24], Tool: name, Digest: "sha256:" + digestText,
			Size: len([]byte(content)), TokenCount: c.tokenCounter.CountText(content), CreatedAt: time.Now(),
		},
		Content: content,
	}
	for _, existing := range c.toolResultRefs {
		if existing.Ref == result.Ref {
			return result
		}
	}
	c.toolResultRefs = append(c.toolResultRefs, result.ToolResultRef)
	c.pendingToolResults = append(c.pendingToolResults, result)
	return result
}

// EnsureFinalAssistantTranscript 在请求结束时补一条可见 assistant 终态事件
// （去重；自行加锁）。
func (c *Coordinator) EnsureFinalAssistantTranscript(requestID, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if state := c.taskExecution; state == nil || state.RequestID != requestID {
		return
	}
	if len(c.transcript) > 0 {
		last := c.transcript[len(c.transcript)-1]
		if last.TaskID == requestID && last.Role == "assistant" && last.Content == content && len(last.ToolCalls) == 0 {
			return
		}
	}
	c.AppendTranscriptEventLocked(model.TranscriptEvent{TaskID: requestID, Role: "assistant", Content: content})
}

// TaskProjectionLocked 构建任务的权威投影（会话归档用；调用方持有
// Core.Mu）。
func (c *Coordinator) TaskProjectionLocked(sessionID string) *model.TaskContextProjection {
	state := c.taskExecution
	if state == nil {
		return nil
	}
	projectID := ""
	if c.Snapshot.CurrentWorkspace != nil {
		projectID = c.Snapshot.CurrentWorkspace.ID
	}
	checkpoint := c.BuildTaskCheckpointLocked(state)
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
	checkpoint := model.TaskCheckpoint{}
	if state.InheritedCheckpoint != nil {
		checkpoint = CloneTaskCheckpoint(*state.InheritedCheckpoint)
	}
	checkpoint.Version = state.ContextVersion
	checkpoint.CoversEventRange = extendEventRange(checkpoint.CoversEventRange, eventRangeForTask(c.transcript, state.RequestID))
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
	for _, event := range c.transcript {
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

// ActivePlanProjectionLocked 返回当前激活 Plan 的只读投影（调用方持有
// Core.Mu）。
func (c *Coordinator) ActivePlanProjectionLocked() *model.ActivePlanProjection {
	return ActivePlanProjection(c.Snapshot.Runtime.Plan, c.activePlanID, c.planSequence)
}

func (c *Coordinator) restoreTaskProjectionLocked(projection *model.TaskContextProjection, fallbackObjective string) {
	c.prompt.ClearSkillLayers()
	if projection == nil {
		c.taskExecution = nil
		c.taskService = nil
		c.syncGoalSkillActiveLocked()
		return
	}
	objective := c.resolveObjectiveRefLocked(projection.ObjectiveRef)
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
	if frame := ActivePlanFrame(c.planStack, c.activePlanID); frame != nil {
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
	c.taskExecution = state
	c.taskService = newTaskService(c.Core, state, c.queuedInputRefs)
	c.syncGoalSkillActiveLocked()
}

func (c *Coordinator) resolveObjectiveRefLocked(objectiveRef string) string {
	const eventPrefix = "event:"
	if !strings.HasPrefix(objectiveRef, eventPrefix) {
		return strings.TrimSpace(objectiveRef)
	}
	sequence, err := strconv.ParseUint(strings.TrimPrefix(objectiveRef, eventPrefix), 10, 64)
	if err != nil {
		return ""
	}
	for _, event := range c.transcript {
		if event.Seq == sequence && event.Role == "user" {
			return event.Content
		}
	}
	return ""
}

// RecordContextCompactionLocked 记录一次上下文压缩（仅运行中任务；调用方
// 持有 Core.Mu）。
func (c *Coordinator) RecordContextCompactionLocked(requestID string, compaction model.ContextCompaction) bool {
	state := c.taskExecution
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

// SetTaskStateLocked 把任务可见状态写入快照（调用方持有 Core.Mu）。
func (c *Coordinator) SetTaskStateLocked(requestID string, status model.TaskStatus, summary string) {
	var compactions []model.ContextCompaction
	if taskState := c.taskExecution; taskState != nil && taskState.RequestID == requestID {
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

// InterruptTaskLocked 把任务置为中断（快照 + 内部状态；调用方持有 Core.Mu）。
func (c *Coordinator) InterruptTaskLocked(requestID, summary string) {
	c.SetTaskStateLocked(requestID, model.TaskInterrupted, summary)
	if state := c.taskExecution; state != nil && state.RequestID == requestID {
		state.Status = StatusInterrupted
	}
}

// FailTaskLocked 把任务置为失败（快照 + 内部状态；调用方持有 Core.Mu）。
func (c *Coordinator) FailTaskLocked(requestID, summary string) {
	c.SetTaskStateLocked(requestID, model.TaskFailed, summary)
	if state := c.taskExecution; state != nil && state.RequestID == requestID {
		state.Status = StatusFailed
	}
}

// ResumeTaskLocked 恢复被压缩/中断的任务（快照 + 内部状态 + epoch 推进；
// 调用方持有 Core.Mu）。
func (c *Coordinator) ResumeTaskLocked(requestID, summary string) {
	state := c.taskExecution
	if state == nil || state.RequestID != requestID {
		return
	}
	state.Status = StatusRunning
	state.ProgressEpoch++
	c.SetTaskStateLocked(requestID, model.TaskProgressing, summary)
}

// RememberCheckpointLocked 按 version 替换或追加 checkpoint（调用方持有
// Core.Mu）。
func (c *Coordinator) RememberCheckpointLocked(checkpoint model.TaskCheckpoint) {
	for index := range c.taskCheckpoints {
		if c.taskCheckpoints[index].Version == checkpoint.Version && checkpoint.Version != 0 {
			c.taskCheckpoints[index] = checkpoint
			return
		}
	}
	c.taskCheckpoints = append(c.taskCheckpoints, checkpoint)
}

// BeginTask 为当前请求创建任务执行状态与 TaskService（调用方持有
// Core.Mu）。
func (c *Coordinator) BeginTask(requestID, objective, effort string, previous *TaskExecutionState, checkpoint model.TaskCheckpoint) *TaskExecutionState {
	state := continuationTaskExecutionState(requestID, objective, effort, previous, checkpoint)
	c.taskExecution = state
	c.taskService = newTaskService(c.Core, state, c.queuedInputRefs)
	return state
}

// ContinuationSummary 返回当前任务的恢复摘要（requestID 不匹配 → ""）。
func (c *Coordinator) ContinuationSummary(requestID string) string {
	state := c.taskExecution
	if state == nil || state.RequestID != requestID {
		return ""
	}
	return state.ContextSummary()
}

// Transcript 返回 append-only 会话事件。
func (c *Coordinator) Transcript() []model.TranscriptEvent {
	return c.transcript
}

// PendingToolResults 返回尚未随会话原子提交的工具结果。
func (c *Coordinator) PendingToolResults() []model.StoredToolResult {
	return c.pendingToolResults
}

// TaskCheckpoints 返回任务 checkpoint 序列。
func (c *Coordinator) TaskCheckpoints() []model.TaskCheckpoint {
	return c.taskCheckpoints
}

// ToolResultRefs 返回工具结果引用表。
func (c *Coordinator) ToolResultRefs() []model.ToolResultRef {
	return c.toolResultRefs
}

// ToolResultRefByCallID 按工具调用 ID 查结果引用（未找到 → ""）。
func (c *Coordinator) ToolResultRefByCallID(callID string) string {
	return c.resultRefsByToolCallID[callID]
}

// ResultRefsByCallID 返回 callID → resultRef 全量拷贝（上下文拒绝路径）。
func (c *Coordinator) ResultRefsByCallID() map[string]string {
	refs := make(map[string]string, len(c.resultRefsByToolCallID))
	for callID, resultRef := range c.resultRefsByToolCallID {
		refs[callID] = resultRef
	}
	return refs
}

// SyncActivePlanFrameLocked 把当前快照 Plan 收敛进激活帧（调用方持有
// Core.Mu）。
func (c *Coordinator) SyncActivePlanFrameLocked(now time.Time) {
	if c.activePlanID == "" || len(c.planStack) == 0 {
		return
	}
	for index := range c.planStack {
		frame := &c.planStack[index]
		if frame.ID != c.activePlanID {
			continue
		}
		frame.Plan = model.CloneRuntimeState(model.RuntimeState{Plan: c.Snapshot.Runtime.Plan}).Plan
		frame.UpdatedAt = now
		return
	}
}

// PushLoadedPlanLocked 把 plan_load 参数追加为新的激活帧（调用方持有
// Core.Mu）。
func (c *Coordinator) PushLoadedPlanLocked(arguments string, now time.Time) {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" || c.Snapshot.Runtime.Plan == nil {
		return
	}
	c.planSequence++
	planID := fmt.Sprintf("plan-%d", c.planSequence)
	c.planStack = append(c.planStack, model.SessionPlanFrame{
		ID:        planID,
		Plan:      model.CloneRuntimeState(model.RuntimeState{Plan: c.Snapshot.Runtime.Plan}).Plan,
		Arguments: arguments,
		LoadedAt:  now,
		UpdatedAt: now,
	})
	c.activePlanID = planID
}

// RemoveCommittedToolResultsLocked 清理已随会话快照提交的待定工具结果。
func (c *Coordinator) RemoveCommittedToolResultsLocked(committed []model.StoredToolResult) {
	if len(committed) == 0 || len(c.pendingToolResults) == 0 {
		return
	}
	refs := make(map[string]struct{}, len(committed))
	for _, result := range committed {
		refs[result.Ref] = struct{}{}
	}
	pending := c.pendingToolResults[:0]
	for _, result := range c.pendingToolResults {
		if _, ok := refs[result.Ref]; !ok {
			pending = append(pending, result)
		}
	}
	c.pendingToolResults = pending
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

// VerifyAndApply 是终态/打点工具的入口（Registry handler 面）。
func (c *Coordinator) VerifyAndApply(ctx context.Context, kind, argsJSON string) (string, error) {
	return c.currentTaskService().VerifyAndApply(ctx, kind, argsJSON)
}

// FinalizeTask 把自然停止转换为可审计完成/交接（OnChatEnd 入口）。
func (c *Coordinator) FinalizeTask(ctx context.Context, summary ChatEndSummary) error {
	_, err := c.currentTaskService().OnChatEnd(ctx, summary)
	return err
}

// OnChatEnd 把自然停止转换为可审计完成/交接，返回可见任务状态。
func (c *Coordinator) OnChatEnd(ctx context.Context, summary ChatEndSummary) (model.TaskState, error) {
	return c.currentTaskService().OnChatEnd(ctx, summary)
}

// CurrentTaskResumeRecord 返回当前任务的终态恢复记录。
func (c *Coordinator) CurrentTaskResumeRecord() TaskResumeRecord {
	return c.currentTaskService().ResumeRecord()
}

// SetTaskProjectionFlushLocked 注入 TaskService 的 Plan 投影 flush 钩子
// （测试模拟延迟投影；调用方持有 Core.Mu）。
func (c *Coordinator) SetTaskProjectionFlushLocked(flush func(context.Context) error) {
	if c.taskService != nil {
		c.taskService.projection = &planProjectionReader{Core: c.Core, flush: flush}
	}
}

// ObserveTool 记录工具执行观测（调用方持有 Core.Mu）。
func (c *Coordinator) ObserveTool(observation ToolObservation) {
	c.currentTaskServiceLocked().ObserveTool(observation)
}

// ObservePlanEvent 记录 plan 事件投影观测（调用方持有 Core.Mu）。
func (c *Coordinator) ObservePlanEvent(event PlanEvent) {
	c.currentTaskServiceLocked().ObservePlanEvent(event)
}

// ObserveModelOutput 记录模型回复观测（自行加锁）。
func (c *Coordinator) ObserveModelOutput(ctx context.Context, output ModelOutput) error {
	return c.currentTaskService().ObserveModelOutput(ctx, output)
}
