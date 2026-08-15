package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/session"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seelplan "github.com/RedHuang-0622/seelex/seelebridge/plan"
)

func (service *Service) startChat(parent context.Context, request chatRequest) error {
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return fmt.Errorf("application is shut down")
	}
	if service.draining {
		service.mu.Unlock()
		return ErrApplicationDraining
	}
	if service.snapshot.Chat.Running {
		service.mu.Unlock()
		return ErrChatRunning
	}
	requestID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
	budget := request.budget
	if budget.MaxToolRounds <= 0 && budget.MaxToolCalls <= 0 {
		budget = reactBudgetFor(service.effortManager.Current())
	}
	chatContext, cancel := context.WithCancel(parent)
	service.cancelChat = cancel
	service.startReActBudgetLocked(requestID, budget)
	previousTask := service.taskExecution
	previousCheckpoint := TaskCheckpoint{}
	if previousTask != nil && isContinuableTaskStatus(previousTask.status) {
		previousCheckpoint = service.components.tasks.buildTaskCheckpointLocked(previousTask)
	}
	service.taskExecution = continuationTaskExecutionState(requestID, request.displayInput, service.effortManager.Current(), previousTask, previousCheckpoint)
	service.taskService = newTaskService(service.serviceState, service.taskExecution)
	service.components.tasks.activateTaskSkillsLocked(service.taskExecution, request.skills)
	service.components.tasks.appendTranscriptEventLocked(TranscriptEvent{TaskID: requestID, Role: "user", Content: request.displayInput})
	service.streamOutput = newVisibleOutputStream(requestID)
	service.markBusyLocked()
	service.snapshot.Chat = ChatState{Running: true, RequestID: requestID, StartedAt: time.Now()}
	service.setTaskStateLocked(requestID, TaskProgressing, "Task is in progress.")
	if service.snapshot.Session.Name == "" {
		service.sessionTitle = SessionTitle{Value: sessionTitle(request.displayInput), Source: "first_request", FinalizedAt: time.Now()}
		service.snapshot.Session.Name = service.sessionTitle.Value
	}
	user := *service.appendMessageLocked("user", request.displayInput, nil)
	assistant := *service.appendMessageLocked("assistant", "", nil)
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.publishRuntimeProjections()
	// 子代理 merge-back 排队内容注入（锁外、ChatStream 开始前）：节点执行
	// 期间主会话被持锁无法回写，只能在此时补注入。
	service.injectPendingSubagentContexts()
	service.events.Publish(EventMessageAdded, revision, requestID, user)
	service.events.Publish(EventMessageAdded, revision, requestID, assistant)
	go service.runChat(chatContext, requestID, request)
	return nil
}

func (service *Service) runChat(ctx context.Context, requestID string, request chatRequest) {
	defer service.clearReActBudget(requestID)
	var err error
	recovered := false
	modelInput := request.modelInput
	batcher, onChunk := service.newBatchedDeltaSink(requestID)
	if err == nil {
		service.components.prompts.applyActiveTaskSystemPrompt(requestID)
	}
	if err == nil {
		modelInput, err = service.components.context.prepareExecutionContext(requestID, modelInput)
	}
	if err == nil {
		modelInput = nonEmptyProviderInput(modelInput)
		var reply string
		reply, err = service.deps.Engine.ChatStream(ctx, modelInput, onChunk)
		// 模型输出观测（自然终态判定输入面）
		service.currentTaskService().ObserveModelOutput(ctx, ModelOutput{RequestID: requestID, Reply: reply, Err: err})
		if reply != "" {
			service.components.tasks.ensureFinalAssistantTranscript(requestID, reply)
		}
		if contextErr := service.components.context.takeContextControlFailure(requestID); contextErr != nil {
			err = contextErr
		}
		var recoveryErr error
		recovered, recoveryErr = service.recoverProviderFailure(err, request.displayInput)
		if recoveryErr != nil {
			err = fmt.Errorf("%w; context recovery failed: %v", err, recoveryErr)
		}
		if recovered && recoveryErr == nil && isProviderContextExhaustion(err) {
			if retryErr := service.retryContextRecovery(ctx, requestID, onChunk); retryErr != nil {
				err = fmt.Errorf("%w; bounded context recovery turn failed: %v", err, retryErr)
			} else {
				err = nil
			}
		}
		if err == nil {
			err = service.finalizeReActBudgetWithSink(ctx, requestID, onChunk)
		}
		if err == nil {
			// 自然停止 → 自动终态（finalizeTaskExecution 演进为 OnChatEnd）
			var endErr error
			_, endErr = service.currentTaskService().OnChatEnd(ctx, ChatEndSummary{RequestID: requestID, Reply: reply})
			err = endErr
		}
	}
	if flushErr := batcher.Flush(); flushErr != nil && err == nil {
		err = fmt.Errorf("flush streamed response: %w", flushErr)
	}
	// plan_run may have completed child agents while the main framework session
	// was locked. Drain their Runtime-owned mailbox only after ChatStream has
	// returned, so every subsequently queued turn sees the merge-back history.
	service.injectPendingSubagentContexts()
	runtimeProjection := service.collectRuntimeProjection(context.Background())
	service.mu.Lock()
	if service.streamBatcher == batcher {
		service.streamBatcher = nil
	}
	service.mu.Unlock()
	if cleanupErr := service.components.context.removeTaskContextCheckpoints(); cleanupErr != nil && err == nil {
		err = cleanupErr
	}
	if err == nil {
		if cleanupErr := service.removeProviderContextRecovery(); cleanupErr != nil {
			err = cleanupErr
		}
	}
	if err != nil {
		service.mu.Lock()
		service.recordUnhandledTaskErrorLocked(requestID, err)
		service.mu.Unlock()
	}
	saveErr := service.components.sessions.persistCurrentSession(service.deps.Engine.SessionID())
	if saveErr != nil {
		if err != nil {
			err = wrapError(fmt.Errorf("%w; persistence failed and recovery is not guaranteed: %v", err, saveErr), errorCodePersistenceFailed)
		} else {
			err = wrapError(fmt.Errorf("persistence failed and recovery is not guaranteed: %w", saveErr), errorCodePersistenceFailed)
		}
	} else if releaser, ok := service.deps.Engine.(interface{ ReleaseWorkingHistory() }); ok {
		releaser.ReleaseWorkingHistory()
	}
	service.mu.Lock()
	if service.snapshot.Chat.RequestID != requestID {
		service.mu.Unlock()
		return
	}
	service.snapshot.Chat.Error = ""
	visibleError := ""
	if err != nil {
		if isUnclassifiedRunChatError(err) {
			log.Printf("[runChat] request_id=%s unclassified_error=%v", requestID, err)
		}
		visibleError = presentUserError(err)
		service.snapshot.Chat.Error = visibleError
		service.appendMessageLocked("error", visibleError, nil)
	}
	// 不在此处从 Engine.History() 重建 conversation——增量构建已在
	// startChat/handleToolStart/handleToolComplete/appendDelta 中完成，
	// 全量重建可能带入跨会话的残留消息。
	service.applyRuntimeProjectionLocked(runtimeProjection)
	// 处理输入队列（单一消费点）：取排队输入合并为一条，批量发送并起下一轮
	pendingQueue := append([]chatRequest(nil), service.inputQueue...)
	processQueue := len(pendingQueue) > 0
	var batchRequest chatRequest
	var nextContext context.Context
	nextRequestID := ""
	var nextUser, nextAssistant *Message
	if processQueue {
		// UI 展示原始输入，模型输入使用每次 Submit 时固化的 Skill 上下文。
		batchRequest = combineChatRequests(pendingQueue)
		service.inputQueue = nil
		service.snapshot.Chat.QueuedCount = 0
		service.snapshot.Chat.InputQueue = nil
		nextRequestID = fmt.Sprintf("chat-%d", time.Now().UnixNano())
		budget := batchRequest.budget
		if budget.MaxToolRounds <= 0 && budget.MaxToolCalls <= 0 {
			budget = reactBudgetFor(service.effortManager.Current())
		}
		nextContext, service.cancelChat = context.WithCancel(context.Background())
		service.startReActBudgetLocked(nextRequestID, budget)
		previousTask := service.taskExecution
		previousCheckpoint := TaskCheckpoint{}
		if previousTask != nil && isContinuableTaskStatus(previousTask.status) {
			previousCheckpoint = service.components.tasks.buildTaskCheckpointLocked(previousTask)
		}
		service.taskExecution = continuationTaskExecutionState(nextRequestID, batchRequest.displayInput, service.effortManager.Current(), previousTask, previousCheckpoint)
		service.taskService = newTaskService(service.serviceState, service.taskExecution)
		service.components.tasks.activateTaskSkillsLocked(service.taskExecution, batchRequest.skills)
		service.components.tasks.appendTranscriptEventLocked(TranscriptEvent{TaskID: nextRequestID, Role: "user", Content: batchRequest.displayInput})
		service.streamOutput = newVisibleOutputStream(nextRequestID)
		service.snapshot.Chat = ChatState{Running: true, RequestID: nextRequestID, StartedAt: time.Now()}
		service.setTaskStateLocked(nextRequestID, TaskProgressing, "Task is in progress.")
		nextUser = service.appendMessageLocked("user", batchRequest.displayInput, nil)
		nextAssistant = service.appendMessageLocked("assistant", "", nil)
	} else {
		service.snapshot.Chat.Running = false
		service.cancelChat = nil
		service.markIdleLocked()
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	if err != nil {
		service.events.Publish(EventError, revision, requestID, map[string]string{"message": visibleError})
	} else {
		service.events.Publish(EventSnapshotChanged, revision, requestID, nil)
	}
	// 批量发送：所有排队消息一次发给 LLM
	if processQueue {
		service.events.Publish(EventMessageAdded, revision, nextRequestID, *nextUser)
		service.events.Publish(EventMessageAdded, revision, nextRequestID, *nextAssistant)
		service.publishRuntimeProjections()
		go service.runChat(nextContext, nextRequestID, batchRequest)
	}
}

func (service *Service) recordUnhandledTaskErrorLocked(requestID string, err error) {
	task := service.snapshot.Task
	if task == nil || task.RequestID != requestID || task.Status != TaskProgressing {
		return
	}
	if errors.Is(err, context.Canceled) {
		service.setTaskStateLocked(requestID, TaskInterrupted, "The task was canceled before completion.")
		if state := service.taskExecution; state != nil && state.requestID == requestID {
			state.status = taskStatusInterrupted
		}
		return
	}
	if kind := classifyProviderFailure(err); kind != providerFailureNone {
		service.setTaskStateLocked(requestID, TaskInterrupted, "The provider interrupted the task before completion.")
		if state := service.taskExecution; state != nil && state.requestID == requestID {
			state.status = taskStatusInterrupted
		}
		return
	}
	service.setTaskStateLocked(requestID, TaskFailed, "The task stopped before a verified completion could be delivered.")
	if state := service.taskExecution; state != nil && state.requestID == requestID {
		state.status = taskStatusFailed
	}
}

const reactBudgetFinalizationInput = "<!-- seelex:react-budget-finalize:v1 -->\n" +
	"The execution budget is exhausted. Do not call investigation, execution, or verification tools again. " +
	"Use task_complete if the evidence supports delivery, or task_failed if it does not; then provide the user-facing result from the evidence already collected."

// finalizeReActBudget reserves one text-only delivery turn after a tool budget
// is reached. The normal loop has already stopped before this point; this turn
// exists so the user receives the result rather than a bare budget error.
func (service *Service) finalizeReActBudget(ctx context.Context, requestID string) error {
	return service.finalizeReActBudgetWithSink(ctx, requestID, func(chunk string) {
		service.appendDelta(requestID, chunk)
	})
}

func (service *Service) finalizeReActBudgetWithSink(ctx context.Context, requestID string, onChunk func(string)) error {
	budgetErr := service.reactBudgetError(requestID)
	if budgetErr == nil {
		return nil
	}
	finalizationInput, prepareErr := service.components.context.prepareExecutionContext(requestID, reactBudgetFinalizationInput)
	if prepareErr != nil {
		return fmt.Errorf("%w; prepare final delivery context: %v", budgetErr, prepareErr)
	}
	result, err := service.deps.Engine.ChatStream(ctx, finalizationInput, onChunk)
	cleanupErr := service.removeReActBudgetFinalizationInput()
	if err != nil {
		return fmt.Errorf("%w; final delivery failed: %v", budgetErr, err)
	}
	if strings.TrimSpace(result) == "" {
		return fmt.Errorf("%w; final delivery returned no text", budgetErr)
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	return nil
}

func (service *Service) removeReActBudgetFinalizationInput() error {
	history := service.deps.Engine.History()
	filtered := make([]EngineMessage, 0, len(history))
	removed := false
	for _, message := range history {
		if message.Role == "user" && message.Content == reactBudgetFinalizationInput {
			removed = true
			continue
		}
		filtered = append(filtered, message)
	}
	if !removed {
		return nil
	}
	if err := service.deps.Engine.ReplaceHistory(service.deps.Engine.SessionID(), filtered); err != nil {
		return fmt.Errorf("remove ReAct budget finalization input: %w", err)
	}
	return nil
}

func closedSignal() chan struct{} {
	idle := make(chan struct{})
	close(idle)
	return idle
}

func (service *Service) markBusyLocked() {
	select {
	case <-service.idle:
		service.idle = make(chan struct{})
	default:
	}
}

func (service *Service) markIdleLocked() {
	select {
	case <-service.idle:
	default:
		close(service.idle)
	}
}

func (service *Service) appendDelta(requestID, chunk string) {
	visible := service.consumeVisibleChunk(requestID, chunk)
	if visible != "" {
		service.appendVisibleDelta(requestID, visible)
	}
}

func (service *Service) newBatchedDeltaSink(requestID string) (*StreamBatcher, func(string)) {
	batcher := NewStreamBatcher(func(batch []string) {
		service.appendVisibleDelta(requestID, strings.Join(batch, ""))
	}, StreamBatcherOptions{FlushSize: 32, BufferSize: 128, Interval: 40 * time.Millisecond})
	service.mu.Lock()
	if service.snapshot.Chat.RequestID == requestID {
		service.streamBatcher = batcher
	}
	service.mu.Unlock()
	return batcher, func(chunk string) {
		if visible := service.consumeVisibleChunk(requestID, chunk); visible != "" {
			batcher.OnChunk(visible)
		}
	}
}

func (service *Service) flushStreamBatcher(requestID string) {
	service.mu.RLock()
	batcher := service.streamBatcher
	active := service.snapshot.Chat.RequestID == requestID
	service.mu.RUnlock()
	if active && batcher != nil {
		_ = batcher.FlushPending()
	}
}

func (service *Service) consumeVisibleChunk(requestID, chunk string) string {
	service.mu.Lock()
	defer service.mu.Unlock()
	if !service.snapshot.Chat.Running || service.snapshot.Chat.RequestID != requestID {
		return ""
	}
	if service.streamOutput == nil || service.streamOutput.requestID != requestID {
		service.streamOutput = newVisibleOutputStream(requestID)
	}
	return service.streamOutput.Consume(chunk)
}

func (service *Service) appendVisibleDelta(requestID, chunk string) {
	if chunk == "" {
		return
	}
	service.mu.Lock()
	if !service.snapshot.Chat.Running || service.snapshot.Chat.RequestID != requestID {
		service.mu.Unlock()
		return
	}
	messageID := ""
	for index := len(service.snapshot.Conversation) - 1; index >= 0; index-- {
		if service.snapshot.Conversation[index].Role == "assistant" && service.snapshot.Conversation[index].Tool == nil {
			service.snapshot.Conversation[index].Content += chunk
			messageID = service.snapshot.Conversation[index].ID
			break
		}
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventMessageDelta, revision, requestID, MessageDelta{MessageID: messageID, Delta: chunk})
}

func (service *Service) appendHistoryLocked(history []EngineMessage) {
	for _, historyMessage := range history {
		if !isVisibleHistoryMessage(historyMessage) {
			continue
		}
		if historyMessage.Role != "tool" && historyMessage.Content != "" && !isProviderOnlyHistoryContent(historyMessage.Content) {
			content := historyMessage.Content
			if historyMessage.Role == "user" {
				content = displayUserInput(content)
			}
			service.appendMessageLocked(historyMessage.Role, content, nil)
		}
		for _, call := range historyMessage.ToolCalls {
			service.appendMessageLocked("tool", "", &ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: "success"})
		}
		if historyMessage.Role == "tool" {
			service.appendMessageLocked("tool_result", historyMessage.Content, &ToolCall{ID: historyMessage.ToolCallID, Name: historyMessage.Name, Result: historyMessage.Content, Status: "success"})
		}
	}
}

func (service *Service) handleToolStart(name, id, arguments string) {
	service.mu.RLock()
	activeRequestID := service.snapshot.Chat.RequestID
	service.mu.RUnlock()
	service.flushStreamBatcher(activeRequestID)
	service.mu.Lock()
	var planBinding *dto.PlanBranchBinding
	service.components.tasks.ensureToolCallTranscriptLocked(name, id, arguments)
	tool := &ToolCall{ID: id, Name: name, Arguments: arguments, Status: "running"}
	message := *service.appendMessageLocked("tool", "", tool)

	// plan_load 启动时：解析 DAG 并初始化 PlanState
	if name == "plan_load" {
		service.updatePlanFromLoad(arguments)
		if state := service.taskExecution; state != nil && state.requestID == service.snapshot.Chat.RequestID {
			state.planArguments = arguments
		}
	}
	// plan_clear 启动时：清空 PlanState
	if name == "plan_clear" {
		service.snapshot.Runtime.Plan = nil
		if state := service.taskExecution; state != nil && state.requestID == service.snapshot.Chat.RequestID {
			state.planArguments = ""
		}
	}
	if name == "plan_run" {
		binding := service.planBranchBindingLocked()
		planBinding = &binding
	}

	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	service.mu.Unlock()
	if planBinding != nil {
		service.deps.Runtime.SetPlanBranchBinding(*planBinding)
	}
	service.events.Publish(EventToolStarted, revision, requestID, message)
	// plan_load/plan_clear/plan_run 已改 PlanState：被动同步 plan → task
	// 注册表并发布 worktable/task 增量，再发最新 runtime.changed。
	service.refreshWorkTableFromSources()
	service.events.Publish(EventRuntimeChanged, service.Snapshot().Revision, requestID, service.Snapshot().Runtime)
}

func (service *Service) planBranchBindingLocked() dto.PlanBranchBinding {
	binding := dto.PlanBranchBinding{
		SessionID: service.snapshot.Session.ID,
		AccountID: service.snapshot.Runtime.Account,
		TraceID:   service.snapshot.Chat.RequestID,
	}
	if workspace := service.snapshot.CurrentWorkspace; workspace != nil {
		binding.WorkspaceID = workspace.ID
	}
	if plan := service.snapshot.Runtime.Plan; plan != nil {
		binding.PlanID = plan.EntryNodeID
		binding.EntryNodeID = plan.EntryNodeID
	}
	return binding
}

func (service *Service) handleToolComplete(name, id, result string, toolErr error, duration time.Duration) {
	service.handleToolCompleteObserved(name, id, result, toolErr, duration, nil)
}

// handleToolCompleteObserved keeps the production completion projection in one
// place while allowing ToolHookBridge to bracket the few blocking boundaries
// during an explicitly enabled backend diagnostic run.
func (service *Service) handleToolCompleteObserved(name, id, result string, toolErr error, duration time.Duration, observe func(string)) {
	emit := func(stage string) {
		if observe != nil {
			observe(stage)
		}
	}
	emit("toolhook.complete.flush.start")
	service.mu.RLock()
	activeRequestID := service.snapshot.Chat.RequestID
	service.mu.RUnlock()
	service.flushStreamBatcher(activeRequestID)
	emit("toolhook.complete.flush.done")
	runtimeProjection := service.collectRuntimeProjection(context.Background())
	emit("toolhook.complete.lock.start")
	service.mu.Lock()
	emit("toolhook.complete.lock.done")
	toolArguments := ""
	for index := len(service.snapshot.Conversation) - 1; index >= 0; index-- {
		tool := service.snapshot.Conversation[index].Tool
		if tool != nil && tool.ID == id {
			toolArguments = tool.Arguments
			break
		}
	}
	emit("toolhook.complete.transcript.start")
	providerResult, _ := service.components.tasks.recordToolTranscriptLocked(name, id, toolArguments, result, toolErr)
	emit("toolhook.complete.transcript.done")
	emit("toolhook.complete.task.start")
	service.currentTaskServiceLocked().ObserveTool(ToolObservation{
		RequestID: service.snapshot.Chat.RequestID, Name: name, Result: providerResult, Err: toolErr,
	})
	emit("toolhook.complete.task.done")
	status, errorText := "success", ""
	if toolErr != nil {
		status, errorText = "error", presentToolError(name, toolErr)
	}
	for index := len(service.snapshot.Conversation) - 1; index >= 0; index-- {
		tool := service.snapshot.Conversation[index].Tool
		if tool != nil && tool.ID == id {
			toolArguments = tool.Arguments
			tool.Status, tool.Result, tool.Error, tool.Duration = status, result, errorText, duration
			break
		}
	}
	if name == "read_file" && toolErr == nil {
		service.components.sessions.recordReadFileLocked(toolArguments)
	}
	if name == "plan_load" && toolErr == nil {
		service.components.sessions.pushLoadedPlanLocked(toolArguments, time.Now())
	}
	if name == "plan_clear" && toolErr == nil {
		service.activePlanID = ""
	}
	content := result
	if toolErr != nil {
		content = errorText
	}

	var planFailure *Interaction
	if name == "plan_run" {
		switch {
		case toolErr != nil:
			planFailure = service.handlePlanRunFailureLocked(toolErr.Error(), result)
		default:
			if failure := planRunFailure(result); failure != "" {
				planFailure = service.handlePlanRunFailureLocked(failure, result)
			} else {
				service.updatePlanFromRunResult(result)
			}
		}
	}

	message := *service.appendMessageLocked("tool_result", content, &ToolCall{ID: id, Name: name, Result: result, Error: errorText, Status: status, Duration: duration})
	// Only append empty assistant if the last message isn't already an empty assistant
	var assistant *Message
	if n := len(service.snapshot.Conversation); n == 0 || service.snapshot.Conversation[n-1].Role != "assistant" || service.snapshot.Conversation[n-1].Content != "" || service.snapshot.Conversation[n-1].Tool != nil {
		appended := *service.appendMessageLocked("assistant", "", nil)
		assistant = &appended
	}
	emit("toolhook.complete.runtime.start")
	service.applyRuntimeProjectionLocked(runtimeProjection)
	emit("toolhook.complete.runtime.done")
	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	service.mu.Unlock()
	emit("toolhook.complete.unlock.done")
	emit("toolhook.complete.event.start")
	service.events.Publish(EventToolCompleted, revision, requestID, message)
	if planFailure != nil {
		service.events.Publish(EventInteractionOpened, revision, planFailure.ID, planFailure)
	}
	if assistant != nil {
		service.events.Publish(EventMessageAdded, revision, requestID, *assistant)
	}
	// 工具完成：todo/taskadd 已写注册表，plan/subagent 状态已更新——
	// 统一走被动同步 + 增量发布，再发最新 runtime.changed。
	service.refreshWorkTableFromSources()
	service.events.Publish(EventRuntimeChanged, service.Snapshot().Revision, requestID, service.Snapshot().Runtime)
	emit("toolhook.complete.event.done")
}

// updatePlanFromLoad 从 plan_load 的参数 JSON 初始化 PlanState。
func (service *Service) updatePlanFromLoad(argsJSON string) {
	type planNodeSpec struct {
		Input string `json:"input"`
		Kind  string `json:"kind,omitempty"` // "auto" (default) or "manual"
	}
	var input struct {
		Entry string                  `json:"entry"`
		Nodes map[string]planNodeSpec `json:"nodes"`
		Edges map[string][]string     `json:"edges"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil || len(input.Nodes) == 0 {
		return
	}
	if _, ok := input.Nodes[input.Entry]; !ok || seelplan.DetectCycle(input.Edges) != nil {
		return
	}

	// 构建所有节点集合，供 TopoSort 使用
	allNodes := make(map[string]struct{}, len(input.Nodes))
	for id := range input.Nodes {
		allNodes[id] = struct{}{}
	}

	// 拓扑排序 → 稳定节点顺序
	order := seelplan.TopoSort(input.Entry, input.Edges, allNodes)

	nodes := make([]PlanNode, 0, len(input.Nodes))
	for _, id := range order {
		spec := input.Nodes[id]
		kind := spec.Kind
		if kind == "" {
			kind = "auto"
		}
		nodes = append(nodes, PlanNode{ID: id, Label: id, Kind: kind, Status: NodePending})
		if state := service.taskExecution; state != nil && state.requestID == service.snapshot.Chat.RequestID {
			state.checkpoint(id, spec.Input, string(NodePending), "", "")
		}
	}

	// 邻接表 → []PlanEdge
	planEdges := seelplan.AdjacencyToEdges(input.Edges)

	service.snapshot.Runtime.Plan = &PlanState{
		Name:        input.Entry,
		EntryNodeID: input.Entry,
		Status:      PlanPending,
		Nodes:       nodes,
		Edges:       planEdges,
	}
}

// updatePlanFromRunResult 从 plan_run 返回的 JSON 更新 PlanState。
// 解析格式对齐框架 NodeBase 的 snake_case JSON 标签（平铺，非嵌套）。
func (service *Service) updatePlanFromRunResult(resultJSON string) {
	var out struct {
		Status      string `json:"status"`
		NodeCount   int    `json:"node_count"`
		FinalOutput string `json:"final_output"`
		AbortReason string `json:"abort_reason,omitempty"`
		Nodes       []struct {
			NodeID    string `json:"node_id"`
			Kind      string `json:"kind"`
			Status    string `json:"status"`
			Output    string `json:"output,omitempty"`
			Skipped   bool   `json:"skipped"`
			Aborted   bool   `json:"aborted"`
			StartedAt string `json:"started_at,omitempty"`
			EndedAt   string `json:"ended_at,omitempty"`
		} `json:"nodes,omitempty"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &out); err != nil {
		return
	}
	if service.snapshot.Runtime.Plan == nil {
		service.snapshot.Runtime.Plan = &PlanState{}
	}
	plan := service.snapshot.Runtime.Plan

	switch out.Status {
	case "completed":
		plan.Status = PlanCompleted
		plan.Progress = 1.0
	case "failed":
		plan.Status = PlanFailed
	case "aborted":
		plan.Status = PlanAborted
	default:
		plan.Status = PlanRunning
	}

	if out.NodeCount > 0 && len(plan.Nodes) == 0 {
		// 没有 plan_load 数据的情况下，用 node_count 创建占位节点
		for i := range out.NodeCount {
			plan.Nodes = append(plan.Nodes, PlanNode{
				ID:     fmt.Sprintf("node-%d", i+1),
				Label:  fmt.Sprintf("step-%d", i+1),
				Status: resolveNodeStatus(out.Nodes, fmt.Sprintf("node-%d", i+1)),
			})
		}
	}
	// 如果 framework 返回了 per-node 结果，更新详细信息
	if len(out.Nodes) > 0 {
		for i := range plan.Nodes {
			for _, on := range out.Nodes {
				if plan.Nodes[i].ID == on.NodeID {
					plan.Nodes[i].Status = PlanNodeStatus(on.Status)
					plan.Nodes[i].Kind = mapKindForDisplay(on.Kind)
					if on.Output != "" {
						plan.Nodes[i].Output = on.Output
					}
					if on.Skipped {
						plan.Nodes[i].Status = NodeSkipped
					}
					// 从 started_at/ended_at 计算耗时
					if on.StartedAt != "" && on.EndedAt != "" {
						if start, err := time.Parse(time.RFC3339, on.StartedAt); err == nil {
							if end, err2 := time.Parse(time.RFC3339, on.EndedAt); err2 == nil {
								plan.Nodes[i].Elapsed = end.Sub(start).String()
							}
						}
					}
					break
				}
			}
		}
	}

	// 计算已完成节点比例
	done := 0
	for _, n := range plan.Nodes {
		if n.Status == NodeCompleted || n.Status == NodeSkipped {
			done++
		}
	}
	if len(plan.Nodes) > 0 {
		plan.Progress = float64(done) / float64(len(plan.Nodes))
	}

	// 计算总耗时（最早 start → 最晚 end）
	var planStart, planEnd time.Time
	for _, n := range plan.Nodes {
		for _, on := range out.Nodes {
			if n.ID == on.NodeID && on.StartedAt != "" && on.EndedAt != "" {
				s, _ := time.Parse(time.RFC3339, on.StartedAt)
				e, _ := time.Parse(time.RFC3339, on.EndedAt)
				if planStart.IsZero() || s.Before(planStart) {
					planStart = s
				}
				if e.After(planEnd) {
					planEnd = e
				}
				break
			}
		}
	}
	if plan.Elapsed == "" && !planStart.IsZero() && !planEnd.IsZero() {
		plan.Elapsed = planEnd.Sub(planStart).String()
	}
}

// resolveNodeStatus 辅助：从框架返回的 nodes 列表中查找 nodeID 的状态。
func resolveNodeStatus(nodes []struct {
	NodeID    string `json:"node_id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Output    string `json:"output,omitempty"`
	Skipped   bool   `json:"skipped"`
	Aborted   bool   `json:"aborted"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
}, nodeID string) NodeStatus {
	for _, n := range nodes {
		if n.NodeID == nodeID {
			return PlanNodeStatus(n.Status)
		}
	}
	return NodePending
}

// HandlePlanNodeComplete 是 plan 执行事实的投影订阅（由 Runtime 经
// SetPlanNodeCallback 注册）：planEventSink 把 workplan 事件投影为
// PlanNodeEvent 后回调本方法，实时更新节点/计划状态并通知 TUI/GUI 重绘。
// NodeID 为空表示计划级投影（PlanStatus），否则为节点级投影（NodeStatus）。
func (service *Service) HandlePlanNodeComplete(event dto.PlanNodeEvent) {
	service.mu.Lock()
	plan := service.snapshot.Runtime.Plan
	if plan == nil {
		service.mu.Unlock()
		return
	}
	if event.NodeID == "" {
		// 计划级投影（PlanStatus）：终态最终仍以 plan_run 结果 JSON 为准，
		// 此处提前反映运行期状态。
		switch event.Status {
		case "running":
			if plan.Status == PlanPending {
				plan.Status = PlanRunning
			}
		case "completed":
			plan.Status = PlanCompleted
			plan.Progress = 1.0
		case "failed":
			plan.Status = PlanFailed
		case "canceled", "aborted":
			plan.Status = PlanAborted
		}
	}
	var changedNode *PlanNode
	if event.NodeID != "" {
		if node := findPlanNodeByID(plan.Nodes, event.NodeID); node != nil {
			node.Status = PlanNodeStatus(event.Status)
			if event.Kind != "" {
				node.Kind = mapKindForDisplay(event.Kind)
			}
			if event.Elapsed != "" {
				node.Elapsed = event.Elapsed
			}
			if event.Output != "" {
				node.Output = event.Output
			}
			appendPlanNodeEvent(node, event)
			// checkpoint 只对终态生效（旧 HandlePlanNodeComplete 只在节点完成时调用）；
			// 观测经 TaskService.ObservePlanEvent 写入功能打点快照
			if isTerminalNodeStatus(event.Status) {
				service.currentTaskServiceLocked().ObservePlanEvent(PlanEvent{
					NodeID: event.NodeID, Status: event.Status, Output: event.Output, Objective: node.Label,
				})
			}
			changedNode = node
		}
		recalculatePlanProgress(plan)
	}
	// 子代理树投影：fork 子代理生命周期与 plan 节点事件同源（queued/running/
	// completed），树状态随权威 Snapshot 增量刷新（内存态，不落盘）。
	service.snapshot.Runtime.SubAgentTree = service.deps.Engine.SubAgentTree()
	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	var changed SubagentEvent
	if changedNode != nil {
		changed = subagentChangedPayload(plan, event.PlanID, event.RunID, *changedNode)
	}
	service.mu.Unlock()
	if changedNode != nil {
		service.events.Publish(EventSubagentChanged, revision, requestID, changed)
		service.refreshWorkTableFromSources()
		return
	}
	service.events.Publish(EventSnapshotChanged, revision, requestID, nil)
	service.refreshWorkTableFromSources()
}

// appendPlanNodeEvent 把一次节点事件追加到节点时间线（详情页数据源）。
// 上限由 seele.yaml limits 段 plan_node_events 配置（默认 30，防心跳刷屏）。
// 同状态合并：running 心跳只刷新最后一条的时间戳与输出，不追加新条目，
// 时间线保持状态变迁序列（queued → running → completed/...）。
// 输出快照截断到 200 字符。
func appendPlanNodeEvent(node *PlanNode, event dto.PlanNodeEvent) {
	if node.Events == nil {
		node.Events = make([]PlanNodeEventInfo, 0, 8)
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
	node.Events = append(node.Events, PlanNodeEventInfo{Status: status, At: event.At, Output: output})
	if limit := Limits().PlanNodeEvents; limit > 0 && len(node.Events) > limit {
		node.Events = node.Events[len(node.Events)-limit:]
	}
}

// HandlePlanBranchEvent applies a branch lifecycle transition received from
// the bridge and publishes the updated runtime snapshot for both frontends.
func (service *Service) HandlePlanBranchEvent(event seelplan.PlanBranchEvent) {
	service.mu.Lock()
	plan := service.snapshot.Runtime.Plan
	if plan == nil {
		service.mu.Unlock()
		return
	}
	node := findPlanNodeByID(plan.Nodes, event.NodeID)
	if node != nil {
		node.Status = PlanNodeStatus(event.Type)
		appendPlanNodeEvent(node, dto.PlanNodeEvent{NodeID: event.NodeID, Status: event.Type, Output: event.Error, At: event.At})
	}
	switch event.Type {
	case "queued", "started":
		if plan.Status == PlanPending {
			plan.Status = PlanRunning
		}
	case "failed", "panicked":
		plan.Status = PlanFailed
	}
	recalculatePlanProgress(plan)
	// 子代理树投影：分支生命周期（queued/started/failed）同样刷新树状态。
	service.snapshot.Runtime.SubAgentTree = service.deps.Engine.SubAgentTree()
	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	var changed SubagentEvent
	if node != nil {
		changed = subagentChangedPayload(plan, "", "", *node)
	}
	service.mu.Unlock()
	if node != nil {
		service.events.Publish(EventSubagentChanged, revision, requestID, changed)
		service.refreshWorkTableFromSources()
		return
	}
	service.events.Publish(EventSnapshotChanged, revision, requestID, nil)
	service.refreshWorkTableFromSources()
}

// mapKindForDisplay 将框架节点 kind 映射为 seelex PlanNode 展示值。
// 框架内部使用 "approve"（KindApprove），但在用户侧展示为 "manual"。
func mapKindForDisplay(kind string) string {
	if kind == "approve" || kind == "" {
		if kind == "approve" {
			return "manual"
		}
		return "auto"
	}
	return kind
}

// isTerminalNodeStatus 判定节点状态是否为终态（checkpoint 只对终态生效）。
func isTerminalNodeStatus(status string) bool {
	switch status {
	case "completed", "failed", "aborted", "skipped", "canceled", "panicked":
		return true
	default:
		return false
	}
}

// PlanNodeStatus 将字符串转为 NodeStatus。
func PlanNodeStatus(s string) NodeStatus {
	switch s {
	case "queued":
		return NodeQueued
	case "running", "started":
		return NodeRunning
	case "worktree_creating":
		return NodeWorktreeCreating
	case "rebasing":
		return NodeRebasing
	case "merging":
		return NodeMerging
	case "completed":
		return NodeCompleted
	case "failed":
		return NodeFailed
	case "aborted":
		return NodeAborted
	case "skipped":
		return NodeSkipped
	case "canceled":
		return NodeCanceled
	case "panicked":
		return NodePanicked
	default:
		return NodePending
	}
}

func recalculatePlanProgress(plan *PlanState) {
	done := 0
	for _, node := range plan.Nodes {
		if node.Status == NodeCompleted || node.Status == NodeSkipped {
			done++
		}
	}
	if len(plan.Nodes) > 0 {
		plan.Progress = float64(done) / float64(len(plan.Nodes))
	}
}

// handlePlanRunFailure 处理 plan_run 执行失败的情况。
// 更新 PlanState 中失败节点的状态，弹出 retry/skip/abort 交互。
func (service *Service) handlePlanRunFailureLocked(errMsg, resultJSON string) *Interaction {
	plan := service.snapshot.Runtime.Plan
	if plan == nil {
		return nil
	}

	// 更新计划整体状态
	plan.Status = PlanFailed
	service.currentTaskServiceLocked().ObservePlanEvent(PlanEvent{
		NodeID: extractFailedNodeID(errMsg), Status: "failed", Output: resultJSON,
		Failure: errMsg, Objective: "authoritative plan node",
	})

	// 尝试解析 resultJSON 中的部分节点结果（framework 返回失败点之前的节点）
	if resultJSON != "" {
		var out struct {
			Status      string `json:"status"`
			NodeCount   int    `json:"node_count"`
			FinalOutput string `json:"final_output"`
			AbortReason string `json:"abort_reason,omitempty"`
			Nodes       []struct {
				NodeID    string `json:"node_id"`
				Kind      string `json:"kind"`
				Status    string `json:"status"`
				Output    string `json:"output,omitempty"`
				Skipped   bool   `json:"skipped"`
				Aborted   bool   `json:"aborted"`
				StartedAt string `json:"started_at,omitempty"`
				EndedAt   string `json:"ended_at,omitempty"`
			} `json:"nodes,omitempty"`
		}
		if err := json.Unmarshal([]byte(resultJSON), &out); err == nil && len(out.Nodes) > 0 {
			// 从 result 中的 status 更新计划状态
			switch out.Status {
			case "completed":
				plan.Status = PlanCompleted
				plan.Progress = 1.0
			case "aborted":
				plan.Status = PlanAborted
			default:
				plan.Status = PlanFailed
			}

			// 更新各节点状态
			for i := range plan.Nodes {
				for _, on := range out.Nodes {
					if plan.Nodes[i].ID == on.NodeID {
						plan.Nodes[i].Status = PlanNodeStatus(on.Status)
						plan.Nodes[i].Kind = mapKindForDisplay(on.Kind)
						if on.Output != "" {
							plan.Nodes[i].Output = on.Output
						}
						if on.Skipped {
							plan.Nodes[i].Status = NodeSkipped
						}
						if on.StartedAt != "" && on.EndedAt != "" {
							if start, err := time.Parse(time.RFC3339, on.StartedAt); err == nil {
								if end, err2 := time.Parse(time.RFC3339, on.EndedAt); err2 == nil {
									plan.Nodes[i].Elapsed = end.Sub(start).String()
								}
							}
						}
						break
					}
				}
			}

			// 重新计算进度
			done := 0
			for _, n := range plan.Nodes {
				if n.Status == NodeCompleted || n.Status == NodeSkipped {
					done++
				}
			}
			if len(plan.Nodes) > 0 {
				plan.Progress = float64(done) / float64(len(plan.Nodes))
			}
		}
	}

	// 提取失败节点 ID
	failedNodeID := extractFailedNodeID(errMsg)
	if failedNodeID != "" {
		for i := range plan.Nodes {
			if plan.Nodes[i].ID == failedNodeID {
				plan.Nodes[i].Status = NodeFailed
				break
			}
		}
	}

	// 创建 retry/skip/abort 交互
	interaction := &Interaction{
		ID:       fmt.Sprintf("plan-fail-%d", time.Now().UnixNano()),
		Kind:     "plan_retry",
		Title:    "节点执行失败",
		Question: fmt.Sprintf("节点 %s 执行失败：%s", failedNodeID, errMsg),
		Options: []InteractionOption{
			{ID: "replan", Label: "Replan", Description: "Load a reviewed recovery plan without executing it.", Style: "primary"},
			{ID: "retry", Label: "重试", Description: "重新执行整个工作流", Style: "warning"},
			{ID: "skip", Label: "跳过", Description: "修改工作流跳过失败节点再执行", Style: "secondary"},
			{ID: "abort", Label: "终止", Description: "终止当前工作流", Style: "danger"},
		},
		OpenedAt: time.Now(),
	}
	service.snapshot.Interaction = interaction
	return interaction
}

// replanRequestLocked extracts the smallest useful recovery context from the
// authoritative snapshot. It must be called with service.mu held.
func (service *Service) replanRequestLocked(failure, idempotencyKey string) dto.ReplanRequest {
	request := dto.ReplanRequest{Failure: failure, IdempotencyKey: idempotencyKey}
	for index := len(service.snapshot.Conversation) - 1; index >= 0; index-- {
		message := service.snapshot.Conversation[index]
		if request.Objective == "" && message.Role == "user" {
			request.Objective = message.Content
		}
		if request.PreviousPlan == "" && message.Tool != nil && message.Tool.Name == "plan_load" {
			request.PreviousPlan = message.Tool.Arguments
		}
		if request.Objective != "" && request.PreviousPlan != "" {
			break
		}
	}
	if request.Objective == "" {
		request.Objective = "Recover the failed plan safely."
	}
	if plan := service.snapshot.Runtime.Plan; plan != nil {
		var evidence strings.Builder
		for _, node := range plan.Nodes {
			if node.Status != NodeCompleted && node.Status != NodeSkipped && node.Status != NodeFailed {
				continue
			}
			fmt.Fprintf(&evidence, "node=%s status=%s", node.ID, node.Status)
			if node.Output != "" {
				fmt.Fprintf(&evidence, " output=%q", node.Output)
			}
			evidence.WriteByte('\n')
		}
		request.Evidence = evidence.String()
	}
	if state := service.taskExecution; state != nil {
		if checkpointEvidence := state.evidenceText(); checkpointEvidence != "" {
			request.Evidence += "checkpoint evidence:\n" + checkpointEvidence
		}
	}
	if limit := Limits().ReplanEvidenceBytes; limit > 0 && len(request.Evidence) > limit {
		request.Evidence = request.Evidence[:limit] + "\n[evidence truncated]"
	}
	return request
}

// replanFailedWork replaces a failed plan without running it. This preserves
// the user's review point between recovery planning and any new side effect.
func (service *Service) replanFailedWork(ctx context.Context, interactionID, failure string) (resultErr error) {
	service.mu.Lock()
	if _, exists := service.replanInFlight[interactionID]; exists {
		service.mu.Unlock()
		return fmt.Errorf("replan: duplicate interaction %q is already in progress", interactionID)
	}
	planAttempts := 0
	if plan := service.snapshot.Runtime.Plan; plan != nil {
		planAttempts = plan.ReplanCount
		if planAttempts >= Limits().MaxReplansPerPlanChain {
			service.mu.Unlock()
			return fmt.Errorf("replan: plan recovery limit of %d reached", Limits().MaxReplansPerPlanChain)
		}
	}
	service.replanInFlight[interactionID] = struct{}{}
	request := service.replanRequestLocked(failure, interactionID)
	requestID := service.snapshot.Chat.RequestID
	service.mu.Unlock()
	succeeded := false
	defer func() {
		if succeeded {
			return
		}
		runtimeProjection := service.collectRuntimeProjection(context.Background())
		service.mu.Lock()
		delete(service.replanInFlight, interactionID)
		service.applyRuntimeProjectionLocked(runtimeProjection)
		revision := service.bumpLocked()
		runtime := cloneRuntimeState(service.snapshot.Runtime)
		service.mu.Unlock()
		service.events.Publish(EventRuntimeChanged, revision, requestID, runtime)
	}()

	result, err := service.deps.Runtime.PrepareReplan(ctx, request)
	if err != nil {
		return fmt.Errorf("replan: %w", err)
	}
	if result.Arguments == "" {
		return fmt.Errorf("replan: runtime returned no plan_load arguments")
	}
	toolID := fmt.Sprintf("%s:plan-replan-%d", requestID, time.Now().UnixNano())
	service.handleToolStart("plan_load", toolID, result.Arguments)
	service.handleToolComplete("plan_load", toolID, result.Result, nil, 0)
	runtimeProjection := service.collectRuntimeProjection(context.Background())
	service.mu.Lock()
	if plan := service.snapshot.Runtime.Plan; plan != nil {
		plan.ReplanCount = planAttempts + 1
	}
	service.applyRuntimeProjectionLocked(runtimeProjection)
	revision := service.bumpLocked()
	runtime := cloneRuntimeState(service.snapshot.Runtime)
	service.mu.Unlock()
	service.events.Publish(EventRuntimeChanged, revision, requestID, runtime)
	service.addNotice("Recovery plan loaded. Review it before calling plan_run.")
	succeeded = true
	return nil
}

func planRunFailure(resultJSON string) string {
	var result struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil || result.Status != "failed" {
		return ""
	}
	if result.Error != "" {
		return result.Error
	}
	return "plan_run failed"
}

// extractFailedNodeID 从 scheduler 错误消息中提取失败节点的 ID。
// 错误格式: node "X": reason
func extractFailedNodeID(errMsg string) string {
	if strings.Contains(errMsg, `node "`) {
		start := strings.Index(errMsg, `node "`) + len(`node "`)
		end := strings.Index(errMsg[start:], `"`)
		if end > 0 {
			return errMsg[start : start+end]
		}
	}
	return ""
}

type ToolHookBridge struct {
	mu         sync.Mutex
	service    *Service
	toolSeq    uint64
	pending    map[string][]string
	diagnostic ToolHookDiagnosticObserver
}

// ToolHookDiagnosticEvent is metadata-only instrumentation for the boundary
// between the framework session loop and application event projection.
// Arguments and tool output are intentionally excluded.
type ToolHookDiagnosticEvent struct {
	Stage string
	Name  string
	Err   error
}

// ToolHookDiagnosticObserver receives best-effort ToolHookBridge stages.
type ToolHookDiagnosticObserver func(ToolHookDiagnosticEvent)

func NewToolHookBridge() *ToolHookBridge { return &ToolHookBridge{} }
func (bridge *ToolHookBridge) Bind(service *Service) {
	bridge.mu.Lock()
	bridge.service = service
	bridge.mu.Unlock()
}

// SetDiagnosticObserver installs optional, best-effort lifecycle diagnostics.
// Passing nil disables them.
func (bridge *ToolHookBridge) SetDiagnosticObserver(observer ToolHookDiagnosticObserver) {
	bridge.mu.Lock()
	bridge.diagnostic = observer
	bridge.mu.Unlock()
}

func (bridge *ToolHookBridge) observeDiagnostic(event ToolHookDiagnosticEvent) {
	bridge.mu.Lock()
	observer := bridge.diagnostic
	bridge.mu.Unlock()
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer(event)
}
func (bridge *ToolHookBridge) Hooks() *session.LoopHooks {
	return &session.LoopHooks{
		OnLLMComplete: func(_ context.Context, info session.LLMInfo) {
			bridge.mu.Lock()
			svc := bridge.service
			bridge.mu.Unlock()
			if svc != nil {
				svc.components.tasks.recordLLMComplete(info)
			}
		},
		OnToolStart: func(_ context.Context, info session.ToolCallInfo) {
			info = normalizePlanToolCallInfo(info)
			bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.start.enter", Name: info.Name})
			service, id := bridge.beginTool(info)
			bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.start.matched", Name: info.Name})
			if service != nil {
				bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.start.project.start", Name: info.Name})
				service.recordReActToolCall()
				service.handleToolStart(info.Name, id, info.Arguments)
				bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.start.project.done", Name: info.Name})
			}
		},
		OnToolComplete: func(_ context.Context, info session.ToolCallInfo) {
			info = normalizePlanToolCallInfo(info)
			bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.complete.enter", Name: info.Name, Err: info.Error})
			service, id := bridge.completeTool(info)
			bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.complete.matched", Name: info.Name, Err: info.Error})
			if service != nil {
				bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.complete.project.start", Name: info.Name, Err: info.Error})
				service.handleToolCompleteObserved(info.Name, id, info.Result, info.Error, info.Duration, func(stage string) {
					bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: stage, Name: info.Name, Err: info.Error})
				})
				bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.complete.project.done", Name: info.Name, Err: info.Error})
			}
		},
		OnIterationComplete: func(_ context.Context, turn int) bool {
			bridge.mu.Lock()
			svc := bridge.service
			bridge.mu.Unlock()
			if svc == nil {
				return true
			}
			if !svc.allowNextReActIteration(turn) {
				return false
			}
			// 新 Session 装配（session.NewSession + Session.ChatStream）下，
			// OnIterationComplete 在 Session 锁内同步执行：回调不得重入 Session
			// 的历史操作（History/ReplaceHistory/AppendHistory），否则死锁。
			// 压缩决策移交 ContextController（seelectx.ContextController，
			// plan.md §3.5：OnIterationComplete 不再触发 compactTaskContext）；
			// 配对修复由 chat 边界（prepareProviderHistory / 批处理路径）
			// 承担，进度回调与事件流保持不变。
			if reentrant, ok := svc.deps.Engine.(interface{ SessionBacked() bool }); ok && reentrant.SessionBacked() {
				// Session 锁内不可重入 AppendHistory（死锁）；每轮 ReAct
				// 迭代结束检查输入队列：非空 → 返回 false 中断本轮（本轮
				// 工具已全部完成，是安全边界），由 runChat 结尾的队列提升
				// 自动开启下一轮并清空队列——一轮一消费，无需等整条 loop。
				svc.mu.RLock()
				queued := len(svc.inputQueue) > 0
				svc.mu.RUnlock()
				return !queued
			}
			svc.mu.RLock()
			activeRequestID := svc.snapshot.Chat.RequestID
			svc.mu.RUnlock()
			// The engine adds assistant/tool records after the initial preflight.
			// Repair them before its next provider request, not only before loop 0.
			if err := svc.components.history.prepareProviderHistory(); err != nil {
				svc.components.context.recordContextControlFailure(activeRequestID, err)
				return false
			}
			// 非 Session-backed 引擎（仅测试）：队列输入统一由 runChat 结尾
			// 单点消费并开启下一轮，不在此处注入引擎历史。
			return true
		},
	}
}

// normalizePlanToolCallInfo keeps application snapshots in the same canonical
// DAG representation that Seele executes. Invalid adapter input is left intact
// so the tool error remains visible to the user.
func normalizePlanToolCallInfo(info session.ToolCallInfo) session.ToolCallInfo {
	if info.Name != "plan_load" {
		return info
	}
	canonical, err := seelplan.NormalizePlanLoadArguments(info.Arguments)
	if err == nil {
		info.Arguments = canonical
	}
	return info
}

func (bridge *ToolHookBridge) beginTool(info session.ToolCallInfo) (*Service, string) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	id := bridge.nextToolIDLocked()
	if bridge.pending == nil {
		bridge.pending = make(map[string][]string)
	}
	key := toolHookKey(info)
	bridge.pending[key] = append(bridge.pending[key], id)
	return bridge.service, id
}

func (bridge *ToolHookBridge) completeTool(info session.ToolCallInfo) (*Service, string) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	key := toolHookKey(info)
	ids := bridge.pending[key]
	if len(ids) == 0 {
		return bridge.service, bridge.nextToolIDLocked()
	}
	id := ids[0]
	if len(ids) == 1 {
		delete(bridge.pending, key)
	} else {
		bridge.pending[key] = ids[1:]
	}
	return bridge.service, id
}

func (bridge *ToolHookBridge) nextToolIDLocked() string {
	bridge.toolSeq++
	return fmt.Sprintf("tool-%d", bridge.toolSeq)
}

func toolHookKey(info session.ToolCallInfo) string {
	return fmt.Sprintf("%d\x00%s\x00%s", info.Turn, info.Name, info.Arguments)
}
