package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

const activeSkillVersion = "installed-v1"

func (service *taskContextCoordinator) activateTaskSkillsLocked(state *taskExecutionState, layers []PromptLayer) {
	if state == nil {
		return
	}
	state.trustedSkillLayers = append([]PromptLayer(nil), layers...)
	state.activeSkills = make([]ActiveSkill, 0, len(layers))
	for _, layer := range layers {
		digest := sha256.Sum256([]byte(layer.Text))
		state.activeSkills = append(state.activeSkills, ActiveSkill{
			SkillID: layer.Name, Version: activeSkillVersion,
			ContentHash: hex.EncodeToString(digest[:]), Scope: "task",
			ActivatedAt: time.Now(), SourceEvent: service.transcriptSeq + 1,
		})
	}
}

func (service *taskContextCoordinator) appendTranscriptEventLocked(event TranscriptEvent) TranscriptEvent {
	service.transcriptSeq++
	event.Seq = service.transcriptSeq
	if event.TaskID == "" && service.taskExecution != nil {
		event.TaskID = service.taskExecution.requestID
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	event.TokenCount = service.countTranscriptEvent(event)
	service.transcript = append(service.transcript, event)
	return event
}

func (service *taskContextCoordinator) importEngineHistoryAsTranscriptLocked(history []EngineMessage) {
	for _, message := range history {
		if message.Role == "system" || isTaskContextCheckpoint(message.Content) || isProviderOnlyHistoryContent(message.Content) {
			continue
		}
		event := TranscriptEvent{
			Role: message.Role, ReasoningContent: message.ReasoningContent, Content: message.Content,
			ToolCallID: message.ToolCallID, Name: message.Name,
		}
		if event.Role == "tool" && isOversizedToolResult(event.Content, defaultToolResultLimit()) {
			stored := service.storeToolResultLocked(event.Name, event.Content)
			event.Content = oversizedToolResultWarning(event.Name, stored.Ref)
			event.ResultRef = stored.Ref
			service.resultRefsByToolCallID[event.ToolCallID] = stored.Ref
		}
		for _, call := range message.ToolCalls {
			event.ToolCalls = append(event.ToolCalls, TranscriptToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
		}
		service.appendTranscriptEventLocked(event)
	}
}

func (service *taskContextCoordinator) countTranscriptEvent(event TranscriptEvent) int {
	message := EngineMessage{
		Role: event.Role, ReasoningContent: event.ReasoningContent, Content: event.Content,
		ContentSet: true, ToolCallID: event.ToolCallID, Name: event.Name,
	}
	for _, call := range event.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, EngineToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	return service.tokenCounter.CountMessage(message)
}

func (service *taskContextCoordinator) recordLLMComplete(info session.LLMInfo) {
	if info.Response == "" && len(info.ToolCalls) == 0 && info.Usage == nil {
		return
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	state := service.taskExecution
	if state == nil || state.requestID != service.snapshot.Chat.RequestID {
		return
	}
	if info.Usage != nil {
		state.tokenAudit.ActualPromptTokens = info.Usage.PromptTokens
		state.tokenAudit.UpdatedAt = time.Now()
	}
	if info.Response == "" && len(info.ToolCalls) == 0 {
		return
	}
	event := TranscriptEvent{Role: "assistant", Content: info.Response}
	for _, call := range info.ToolCalls {
		item := TranscriptToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments}
		event.ToolCalls = append(event.ToolCalls, item)
		service.pendingProviderCalls = append(service.pendingProviderCalls, item)
	}
	service.appendTranscriptEventLocked(event)
}

func (service *taskContextCoordinator) ensureToolCallTranscriptLocked(name, fallbackID, arguments string) {
	for _, call := range service.pendingProviderCalls {
		if call.Name == name && call.Arguments == arguments {
			return
		}
	}
	call := TranscriptToolCall{ID: fallbackID, Name: name, Arguments: arguments}
	service.pendingProviderCalls = append(service.pendingProviderCalls, call)
	service.appendTranscriptEventLocked(TranscriptEvent{Role: "assistant", ToolCalls: []TranscriptToolCall{call}})
}

func (service *taskContextCoordinator) recordToolTranscriptLocked(name, fallbackID, arguments, result string, toolErr error) (string, string) {
	callID := fallbackID
	for index, call := range service.pendingProviderCalls {
		if call.Name != name || (arguments != "" && call.Arguments != arguments) {
			continue
		}
		callID = call.ID
		service.pendingProviderCalls = append(service.pendingProviderCalls[:index], service.pendingProviderCalls[index+1:]...)
		break
	}
	content := result
	resultRef := ""
	if toolErr != nil {
		content = presentToolError(name, toolErr)
	} else if isOversizedToolResult(result, defaultToolResultLimit()) {
		stored := service.storeToolResultLocked(name, result)
		resultRef = stored.Ref
		content = oversizedToolResultWarning(name, resultRef)
		service.resultRefsByToolCallID[callID] = resultRef
	}
	service.appendTranscriptEventLocked(TranscriptEvent{
		Role: "tool", Content: content, ToolCallID: callID, Name: name, ResultRef: resultRef,
	})
	return content, resultRef
}

func defaultToolResultLimit() int {
	return seelexctx.DefaultContextConfig().MaxToolResultChars
}

func (service *taskContextCoordinator) storeToolResultLocked(name, content string) StoredToolResult {
	digest := sha256.Sum256([]byte(name + "\x00" + content))
	digestText := hex.EncodeToString(digest[:])
	result := StoredToolResult{
		ToolResultRef: ToolResultRef{
			Ref: "tr-" + digestText[:24], Tool: name, Digest: "sha256:" + digestText,
			Size: len([]byte(content)), TokenCount: service.tokenCounter.CountText(content), CreatedAt: time.Now(),
		},
		Content: content,
	}
	for _, existing := range service.toolResultRefs {
		if existing.Ref == result.Ref {
			return result
		}
	}
	service.toolResultRefs = append(service.toolResultRefs, result.ToolResultRef)
	service.pendingToolResults = append(service.pendingToolResults, result)
	return result
}

func (service *taskContextCoordinator) ensureFinalAssistantTranscript(requestID, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if state := service.taskExecution; state == nil || state.requestID != requestID {
		return
	}
	if len(service.transcript) > 0 {
		last := service.transcript[len(service.transcript)-1]
		if last.TaskID == requestID && last.Role == "assistant" && last.Content == content && len(last.ToolCalls) == 0 {
			return
		}
	}
	service.appendTranscriptEventLocked(TranscriptEvent{TaskID: requestID, Role: "assistant", Content: content})
}

func (service *taskContextCoordinator) taskProjectionLocked(sessionID string) *TaskContextProjection {
	state := service.taskExecution
	if state == nil {
		return nil
	}
	projectID := ""
	if service.snapshot.CurrentWorkspace != nil {
		projectID = service.snapshot.CurrentWorkspace.ID
	}
	checkpoint := service.buildTaskCheckpointLocked(state)
	objectiveRef := ""
	if checkpoint.CoversEventRange.Start != 0 {
		objectiveRef = fmt.Sprintf("event:%d", checkpoint.CoversEventRange.Start)
	}
	projection := &TaskContextProjection{
		SchemaVersion: 1, ProjectID: projectID, SessionID: sessionID, TaskID: state.requestID,
		Status: state.status, ObjectiveRef: objectiveRef,
		ActiveSkills: append([]ActiveSkill(nil), state.activeSkills...), ActivePlan: service.activePlanProjectionLocked(),
		Checkpoint: checkpoint, TokenAudit: state.tokenAudit, UpdatedAt: time.Now(),
	}
	return projection
}

func (service *taskContextCoordinator) buildTaskCheckpointLocked(state *taskExecutionState) TaskCheckpoint {
	checkpoint := TaskCheckpoint{}
	if state.inheritedCheckpoint != nil {
		checkpoint = cloneTaskCheckpoint(*state.inheritedCheckpoint)
	}
	checkpoint.Version = state.contextVersion
	checkpoint.CoversEventRange = extendEventRange(checkpoint.CoversEventRange, eventRangeForTask(service.transcript, state.requestID))
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
		case string(NodeCompleted):
			checkpoint.CompletedWork = appendUniqueStrings(checkpoint.CompletedWork, record)
		case string(NodeFailed), string(NodeAborted), string(NodeCanceled), string(NodePanicked):
			checkpoint.Failures = appendUniqueStrings(checkpoint.Failures, record+boundedFailure(node.Failure))
		default:
			checkpoint.PendingWork = appendUniqueStrings(checkpoint.PendingWork, record)
		}
		checkpoint.ChangedFiles = appendUniqueStrings(checkpoint.ChangedFiles, node.ChangedFiles...)
		checkpoint.Artifacts = appendUniqueStrings(checkpoint.Artifacts, node.Artifacts...)
	}
	if state.terminal != nil {
		checkpoint.Artifacts = appendUniqueStrings(checkpoint.Artifacts, state.terminal.Artifacts...)
		checkpoint.Decisions = appendUniqueStrings(checkpoint.Decisions, state.terminal.DecisionQuestion)
	}
	for _, event := range service.transcript {
		if event.TaskID == state.requestID && event.ResultRef != "" {
			checkpoint.ToolResultRefs = appendUniqueStrings(checkpoint.ToolResultRefs, event.ResultRef)
		}
	}
	return checkpoint
}

func cloneTaskCheckpoint(checkpoint TaskCheckpoint) TaskCheckpoint {
	checkpoint.CompletedWork = append([]string(nil), checkpoint.CompletedWork...)
	checkpoint.PendingWork = append([]string(nil), checkpoint.PendingWork...)
	checkpoint.Decisions = append([]string(nil), checkpoint.Decisions...)
	checkpoint.Failures = append([]string(nil), checkpoint.Failures...)
	checkpoint.ChangedFiles = append([]string(nil), checkpoint.ChangedFiles...)
	checkpoint.Artifacts = append([]string(nil), checkpoint.Artifacts...)
	checkpoint.ToolResultRefs = append([]string(nil), checkpoint.ToolResultRefs...)
	return checkpoint
}

func extendEventRange(existing, current EventRange) EventRange {
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
	return " failure=" + boundedEvidence(value)
}

func eventRangeForTask(events []TranscriptEvent, taskID string) EventRange {
	rangeForTask := EventRange{}
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

func appendUniqueStrings(values []string, incoming ...string) []string {
	for _, value := range incoming {
		value = strings.TrimSpace(value)
		if value != "" && !containsString(values, value) {
			values = append(values, value)
		}
	}
	return values
}

func (service *taskContextCoordinator) activePlanProjectionLocked() *ActivePlanProjection {
	return activePlanProjection(service.snapshot.Runtime.Plan, service.activePlanID, service.planSequence)
}

func activePlanProjection(plan *PlanState, activePlanID string, planSequence uint64) *ActivePlanProjection {
	if plan == nil || activePlanID == "" {
		return nil
	}
	projection := &ActivePlanProjection{
		PlanID: activePlanID, Version: planSequence,
		CanonicalPlanRef: activePlanID, Status: string(plan.Status),
	}
	for _, node := range plan.Nodes {
		switch node.Status {
		case NodeCompleted, NodeSkipped:
			projection.CompletedNodes = append(projection.CompletedNodes, node.ID)
		case NodeFailed, NodeAborted, NodeCanceled, NodePanicked:
			projection.FailedNodes = append(projection.FailedNodes, node.ID)
		case NodeRunning:
			projection.CurrentNode = node.ID
		default:
			projection.PendingNodes = append(projection.PendingNodes, node.ID)
		}
	}
	if projection.CurrentNode == "" && len(projection.PendingNodes) > 0 {
		projection.CurrentNode = projection.PendingNodes[0]
	}
	return projection
}

func (service *taskContextCoordinator) restoreTaskProjectionLocked(projection *TaskContextProjection, fallbackObjective string) {
	service.promptStack.ClearKind("skill")
	if projection == nil {
		service.taskExecution = nil
		service.taskService = nil
		return
	}
	objective := service.resolveObjectiveRefLocked(projection.ObjectiveRef)
	if objective == "" {
		objective = strings.TrimSpace(fallbackObjective)
	}
	state := newTaskExecutionState(projection.TaskID, objective, service.effortManager.Current())
	state.status = projection.Status
	state.contextVersion = projection.Checkpoint.Version
	if state.contextVersion == 0 {
		state.contextVersion = 1
	}
	checkpoint := cloneTaskCheckpoint(projection.Checkpoint)
	state.inheritedCheckpoint = &checkpoint
	state.activeSkills = append([]ActiveSkill(nil), projection.ActiveSkills...)
	state.tokenAudit = projection.TokenAudit
	if frame := activePlanFrame(service.planStack, service.activePlanID); frame != nil {
		state.planArguments = frame.Arguments
	}
	for _, active := range projection.ActiveSkills {
		skill, ok := service.deps.Skills.Get(active.SkillID)
		if !ok {
			continue
		}
		digest := sha256.Sum256([]byte(skill.Prompt))
		if hex.EncodeToString(digest[:]) != active.ContentHash {
			continue
		}
		layer := PromptLayer{Kind: "skill", Name: skill.Name, Text: skill.Prompt}
		state.trustedSkillLayers = append(state.trustedSkillLayers, layer)
		service.promptStack.Push(layer.Kind, layer.Name, layer.Text)
	}
	service.taskExecution = state
	service.taskService = newTaskService(service.serviceState, state)
}

func (service *taskContextCoordinator) resolveObjectiveRefLocked(objectiveRef string) string {
	const eventPrefix = "event:"
	if !strings.HasPrefix(objectiveRef, eventPrefix) {
		return strings.TrimSpace(objectiveRef)
	}
	sequence, err := strconv.ParseUint(strings.TrimPrefix(objectiveRef, eventPrefix), 10, 64)
	if err != nil {
		return ""
	}
	for _, event := range service.transcript {
		if event.Seq == sequence && event.Role == "user" {
			return event.Content
		}
	}
	return ""
}

func transcriptTailHistory(events []TranscriptEvent, tokenBudget, maxUnits int) []EngineMessage {
	if len(events) == 0 || tokenBudget <= 0 || maxUnits <= 0 {
		return nil
	}
	units := transcriptProtocolUnits(events)
	selected := make([][]TranscriptEvent, 0, maxUnits)
	tokens := 0
	for index := len(units) - 1; index >= 0 && len(selected) < maxUnits; index-- {
		unitTokens := 0
		for _, event := range units[index] {
			unitTokens += event.TokenCount
		}
		if tokens+unitTokens > tokenBudget {
			break
		}
		selected = append(selected, units[index])
		tokens += unitTokens
	}
	history := make([]EngineMessage, 0)
	for index := len(selected) - 1; index >= 0; index-- {
		for _, event := range selected[index] {
			history = append(history, transcriptEventMessage(event))
		}
	}
	return history
}

func transcriptEventMessage(event TranscriptEvent) EngineMessage {
	message := EngineMessage{
		Role: event.Role, ReasoningContent: event.ReasoningContent, Content: event.Content,
		ContentSet: true, ToolCallID: event.ToolCallID, Name: event.Name,
	}
	for _, call := range event.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, EngineToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	return message
}

func transcriptProtocolUnits(events []TranscriptEvent) [][]TranscriptEvent {
	units := make([][]TranscriptEvent, 0, len(events))
	for index := 0; index < len(events); {
		event := events[index]
		switch {
		case event.Role == "user":
			unit, next, ok := transcriptUserUnit(events, index)
			if ok {
				units = append(units, unit)
			}
			index = next
		case event.Role == "assistant" && len(event.ToolCalls) == 0:
			units = append(units, []TranscriptEvent{event})
			index++
		case event.Role == "assistant" && len(event.ToolCalls) > 0:
			unit, next, ok := transcriptToolUnit(events, index)
			if ok {
				units = append(units, unit)
				index = next
			} else {
				index = nextTranscriptUserIndex(events, next)
			}
		default:
			index++
		}
	}
	return units
}

func transcriptUserUnit(events []TranscriptEvent, start int) ([]TranscriptEvent, int, bool) {
	unit := []TranscriptEvent{events[start]}
	index := start + 1
	hasAssistant := false
	for index < len(events) && events[index].Role != "user" {
		event := events[index]
		if event.Role != "assistant" {
			return nil, nextTranscriptUserIndex(events, index+1), false
		}
		hasAssistant = true
		if len(event.ToolCalls) == 0 {
			unit = append(unit, event)
			return unit, index + 1, true
		}
		toolUnit, next, ok := transcriptToolUnit(events, index)
		if !ok {
			return nil, nextTranscriptUserIndex(events, next), false
		}
		unit = append(unit, toolUnit...)
		index = next
	}
	return unit, index, hasAssistant
}

func nextTranscriptUserIndex(events []TranscriptEvent, start int) int {
	for start < len(events) && events[start].Role != "user" {
		start++
	}
	return start
}

func transcriptToolUnit(events []TranscriptEvent, start int) ([]TranscriptEvent, int, bool) {
	assistant := events[start]
	wanted := make(map[string]struct{}, len(assistant.ToolCalls))
	for _, call := range assistant.ToolCalls {
		if call.ID == "" {
			return nil, start + 1, false
		}
		if _, duplicate := wanted[call.ID]; duplicate {
			return nil, start + 1, false
		}
		wanted[call.ID] = struct{}{}
	}
	unit := []TranscriptEvent{assistant}
	seen := make(map[string]struct{}, len(wanted))
	index := start + 1
	for index < len(events) && len(seen) < len(wanted) {
		event := events[index]
		if event.Role != "tool" {
			break
		}
		if _, ok := wanted[event.ToolCallID]; !ok {
			break
		}
		if _, duplicate := seen[event.ToolCallID]; duplicate {
			break
		}
		seen[event.ToolCallID] = struct{}{}
		unit = append(unit, event)
		index++
	}
	return unit, index, len(seen) == len(wanted)
}
