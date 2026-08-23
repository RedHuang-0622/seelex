package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/chat"
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

func (service *Service) startChat(parent context.Context, request chatRequest) error {
	service.Mu.Lock()
	if service.closed {
		service.Mu.Unlock()
		return fmt.Errorf("application is shut down")
	}
	if service.draining {
		service.Mu.Unlock()
		return ErrApplicationDraining
	}
	if service.Core.Snapshot.Chat.Running {
		service.Mu.Unlock()
		return ErrChatRunning
	}
	requestID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
	budget := request.budget
	if budget.MaxToolRounds <= 0 && budget.MaxToolCalls <= 0 {
		budget = reactBudgetFor(service.effortManager.Current())
	}
	chatContext, cancel := context.WithCancel(parent)
	service.cancelChat = cancel
	service.components.tasks.StartReActBudgetLocked(requestID, budget)
	previousTask := service.components.tasks.CurrentTaskExecution()
	previousCheckpoint := TaskCheckpoint{}
	if previousTask != nil && task_context.IsContinuableStatus(previousTask.Status) {
		previousCheckpoint = service.components.tasks.BuildTaskCheckpointLocked(previousTask)
	}
	taskState := service.components.tasks.BeginTask(requestID, request.displayInput, service.effortManager.Current(), previousTask, previousCheckpoint)
	service.components.tasks.ActivateTaskSkillsLocked(taskState, request.skills)
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: requestID, Role: "user", Content: request.displayInput})
	service.streamOutput = chat.NewVisibleOutputStream(requestID)
	service.markBusyLocked()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: requestID, StartedAt: time.Now()}
	service.components.tasks.SetTaskStateLocked(requestID, TaskProgressing, "Task is in progress.")
	if service.Core.Snapshot.Session.Name == "" {
		service.components.sessions.SetSessionTitleLocked(SessionTitle{Value: session_runtime.SessionTitle(request.displayInput), Source: "first_request", FinalizedAt: time.Now()})
		service.Core.Snapshot.Session.Name = service.components.sessions.SessionTitle().Value
	}
	user := *service.appendMessageLocked("user", request.displayInput, nil)
	assistant := *service.appendMessageLocked("assistant", "", nil)
	revision := service.bumpLocked()
	service.Mu.Unlock()
	// 新批次：此后创建的 todo/task/plan/subagent 条目自动归属当前 chat
	// 请求（requestID），工作表格按批次分片。
	service.Deps.Runtime.SetCurrentTaskBatch(requestID)
	service.publishRuntimeProjections()
	// 子代理 merge-back 排队内容注入（锁外、ChatStream 开始前）：节点执行
	// 期间主会话被持锁无法回写，只能在此时补注入。
	service.injectPendingSubagentContexts()
	service.Events.Publish(EventMessageAdded, revision, requestID, user)
	service.Events.Publish(EventMessageAdded, revision, requestID, assistant)
	go service.runChat(chatContext, requestID, request)
	return nil
}

func (service *Service) runChat(ctx context.Context, requestID string, request chatRequest) {
	defer service.components.tasks.ClearReActBudget(requestID)
	var err error
	recovered := false
	modelInput := request.modelInput
	batcher, onChunk := service.newBatchedDeltaSink(requestID)
	service.components.prompts.ApplyActiveTaskSystemPrompt(requestID)
	if err == nil {
		modelInput, err = service.components.context.PrepareExecutionContext(requestID, modelInput)
	}
	if err == nil {
		modelInput = nonEmptyProviderInput(modelInput)
		var reply string
		reply, err = service.Deps.Engine.ChatStream(ctx, modelInput, onChunk)
		// 模型输出观测（自然终态判定输入面）
		_ = service.components.tasks.ObserveModelOutput(ctx, task_context.ModelOutput{RequestID: requestID, Reply: reply, Err: err})
		if reply != "" {
			service.components.tasks.EnsureFinalAssistantTranscript(requestID, reply)
		}
		if contextErr := service.components.context.TakeContextControlFailure(requestID); contextErr != nil {
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
			endErr = service.components.tasks.FinalizeTask(ctx, task_context.ChatEndSummary{RequestID: requestID, Reply: reply})
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
	service.Mu.Lock()
	if service.streamBatcher == batcher {
		service.streamBatcher = nil
	}
	service.Mu.Unlock()
	if cleanupErr := service.components.context.RemoveTaskContextCheckpoints(); cleanupErr != nil && err == nil {
		err = cleanupErr
	}
	if err == nil {
		if cleanupErr := service.removeProviderContextRecovery(); cleanupErr != nil {
			err = cleanupErr
		}
	}
	if err != nil {
		service.Mu.Lock()
		service.recordUnhandledTaskErrorLocked(requestID, err)
		service.Mu.Unlock()
	}
	saveErr := service.components.sessions.PersistCurrentSession(service.Deps.Engine.SessionID())
	if saveErr != nil {
		if err != nil {
			err = wrapError(fmt.Errorf("%w; persistence failed and recovery is not guaranteed: %v", err, saveErr), errorCodePersistenceFailed)
		} else {
			err = wrapError(fmt.Errorf("persistence failed and recovery is not guaranteed: %w", saveErr), errorCodePersistenceFailed)
		}
	} else if releaser, ok := service.Deps.Engine.(interface{ ReleaseWorkingHistory() }); ok {
		releaser.ReleaseWorkingHistory()
	}
	service.Mu.Lock()
	if service.Core.Snapshot.Chat.RequestID != requestID {
		service.Mu.Unlock()
		return
	}
	service.Core.Snapshot.Chat.Error = ""
	visibleError := ""
	if err != nil {
		if isUnclassifiedRunChatError(err) {
			log.Printf("[runChat] request_id=%s unclassified_error=%v", requestID, err)
		}
		visibleError = presentUserError(err)
		service.Core.Snapshot.Chat.Error = visibleError
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
		service.Core.Snapshot.Chat.QueuedCount = 0
		service.Core.Snapshot.Chat.InputQueue = nil
		nextRequestID = fmt.Sprintf("chat-%d", time.Now().UnixNano())
		budget := batchRequest.budget
		if budget.MaxToolRounds <= 0 && budget.MaxToolCalls <= 0 {
			budget = reactBudgetFor(service.effortManager.Current())
		}
		nextContext, service.cancelChat = context.WithCancel(context.Background())
		service.components.tasks.StartReActBudgetLocked(nextRequestID, budget)
		previousTask := service.components.tasks.CurrentTaskExecution()
		previousCheckpoint := TaskCheckpoint{}
		if previousTask != nil && task_context.IsContinuableStatus(previousTask.Status) {
			previousCheckpoint = service.components.tasks.BuildTaskCheckpointLocked(previousTask)
		}
		taskState := service.components.tasks.BeginTask(nextRequestID, batchRequest.displayInput, service.effortManager.Current(), previousTask, previousCheckpoint)
		service.components.tasks.ActivateTaskSkillsLocked(taskState, batchRequest.skills)
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: nextRequestID, Role: "user", Content: batchRequest.displayInput})
		service.streamOutput = chat.NewVisibleOutputStream(nextRequestID)
		service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: nextRequestID, StartedAt: time.Now()}
		service.components.tasks.SetTaskStateLocked(nextRequestID, TaskProgressing, "Task is in progress.")
		nextUser = service.appendMessageLocked("user", batchRequest.displayInput, nil)
		nextAssistant = service.appendMessageLocked("assistant", "", nil)
	} else {
		service.Core.Snapshot.Chat.Running = false
		service.cancelChat = nil
		service.markIdleLocked()
	}
	revision := service.bumpLocked()
	service.Mu.Unlock()
	if err != nil {
		service.Events.Publish(EventError, revision, requestID, map[string]string{"message": visibleError})
	} else {
		service.Events.Publish(EventSnapshotChanged, revision, requestID, nil)
	}
	// 批量发送：所有排队消息一次发给 LLM
	if processQueue {
		service.Events.Publish(EventMessageAdded, revision, nextRequestID, *nextUser)
		service.Events.Publish(EventMessageAdded, revision, nextRequestID, *nextAssistant)
		service.Deps.Runtime.SetCurrentTaskBatch(nextRequestID)
		service.publishRuntimeProjections()
		go service.runChat(nextContext, nextRequestID, batchRequest)
	}
}

func (service *Service) recordUnhandledTaskErrorLocked(requestID string, err error) {
	task := service.Core.Snapshot.Task
	if task == nil || task.RequestID != requestID || task.Status != TaskProgressing {
		return
	}
	if errors.Is(err, context.Canceled) {
		service.components.tasks.InterruptTaskLocked(requestID, "The task was canceled before completion.")
		return
	}
	if kind := classifyProviderFailure(err); kind != providerFailureNone {
		service.components.tasks.InterruptTaskLocked(requestID, "The provider interrupted the task before completion.")
		return
	}
	service.components.tasks.FailTaskLocked(requestID, "The task stopped before a verified completion could be delivered.")
}

const reactBudgetFinalizationInput = "<!-- seelex:react-budget-finalize:v1 -->\n" +
	"The execution budget is exhausted. Do not call investigation, execution, or verification tools again. " +
	"Use task_complete if the evidence supports delivery, or task_failed if it does not; then provide the user-facing result from the evidence already collected."

// finalizeReActBudget 在工具预算耗尽后保留一次纯文本交付回合。常规循环在
// 此之前已停止；本回合让用户收到结果，而非裸预算错误。
func (service *Service) finalizeReActBudget(ctx context.Context, requestID string) error {
	return service.finalizeReActBudgetWithSink(ctx, requestID, func(chunk string) {
		service.appendDelta(requestID, chunk)
	})
}

// queuedInputRefs 取排队输入的最小引用（displayInput），供任务终态恢复记录
// 使用（TaskService 经装配端口读取；调用方持有 Core.Mu）。
func queuedInputRefs(queue []chatRequest) []string {
	refs := make([]string, 0, len(queue))
	for _, request := range queue {
		if input := strings.TrimSpace(request.displayInput); input != "" {
			refs = append(refs, input)
		}
	}
	return refs
}

// TaskTerminalHandler 返回面向 Runtime 的终态工具 handler，同时把请求状态
// 所有权保留在 Application。handler 无外部副作用，终态工具经
// task_context.Coordinator.VerifyAndApply 路由。
func (service *Service) TaskTerminalHandler(kind string) func(context.Context, string) (string, error) {
	return func(ctx context.Context, argsJSON string) (string, error) {
		return service.components.tasks.VerifyAndApply(ctx, kind, argsJSON)
	}
}

// finalizeTaskExecution 把自然停止转换为可审计的完成/交接
// （TaskService.OnChatEnd 入口）。
func (service *Service) finalizeTaskExecution(requestID string) error {
	return service.components.tasks.FinalizeTask(context.Background(), task_context.ChatEndSummary{RequestID: requestID})
}

func (service *Service) finalizeReActBudgetWithSink(ctx context.Context, requestID string, onChunk func(string)) error {
	budgetErr := service.components.tasks.ReActBudgetError(requestID)
	if budgetErr != nil {
		budgetErr = wrapError(budgetErr, errorCodeReActBudget)
	}
	if budgetErr == nil {
		return nil
	}
	finalizationInput, prepareErr := service.components.context.PrepareExecutionContext(requestID, reactBudgetFinalizationInput)
	if prepareErr != nil {
		return fmt.Errorf("%w; prepare final delivery context: %v", budgetErr, prepareErr)
	}
	result, err := service.Deps.Engine.ChatStream(ctx, finalizationInput, onChunk)
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
	history := service.Deps.Engine.History()
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
	if err := service.Deps.Engine.ReplaceHistory(service.Deps.Engine.SessionID(), filtered); err != nil {
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

func (service *Service) newBatchedDeltaSink(requestID string) (*chat.StreamBatcher, func(string)) {
	batcher := chat.NewStreamBatcher(func(batch []string) {
		service.appendVisibleDelta(requestID, strings.Join(batch, ""))
	}, chat.StreamBatcherOptions{FlushSize: 32, BufferSize: 128, Interval: 40 * time.Millisecond})
	service.Mu.Lock()
	if service.Core.Snapshot.Chat.RequestID == requestID {
		service.streamBatcher = batcher
	}
	service.Mu.Unlock()
	return batcher, func(chunk string) {
		if visible := service.consumeVisibleChunk(requestID, chunk); visible != "" {
			batcher.OnChunk(visible)
		}
	}
}

func (service *Service) flushStreamBatcher(requestID string) {
	service.Mu.RLock()
	batcher := service.streamBatcher
	active := service.Core.Snapshot.Chat.RequestID == requestID
	service.Mu.RUnlock()
	if active && batcher != nil {
		_ = batcher.FlushPending()
	}
}

func (service *Service) consumeVisibleChunk(requestID, chunk string) string {
	service.Mu.Lock()
	defer service.Mu.Unlock()
	if !service.Core.Snapshot.Chat.Running || service.Core.Snapshot.Chat.RequestID != requestID {
		return ""
	}
	if service.streamOutput == nil || service.streamOutput.RequestID() != requestID {
		service.streamOutput = chat.NewVisibleOutputStream(requestID)
	}
	return service.streamOutput.Consume(chunk)
}

func (service *Service) appendVisibleDelta(requestID, chunk string) {
	if chunk == "" {
		return
	}
	service.Mu.Lock()
	if !service.Core.Snapshot.Chat.Running || service.Core.Snapshot.Chat.RequestID != requestID {
		service.Mu.Unlock()
		return
	}
	messageID := ""
	for index := len(service.Core.Snapshot.Conversation) - 1; index >= 0; index-- {
		if service.Core.Snapshot.Conversation[index].Role == "assistant" && service.Core.Snapshot.Conversation[index].Tool == nil {
			service.Core.Snapshot.Conversation[index].Content += chunk
			messageID = service.Core.Snapshot.Conversation[index].ID
			break
		}
	}
	revision := service.bumpLocked()
	service.Mu.Unlock()
	service.Events.Publish(EventMessageDelta, revision, requestID, MessageDelta{MessageID: messageID, Delta: chunk})
}

func (service *Service) appendHistoryLocked(history []EngineMessage) {
	for _, historyMessage := range history {
		if !isVisibleHistoryMessage(historyMessage) {
			continue
		}
		if historyMessage.Role != "tool" && historyMessage.Content != "" && !context_runtime.IsProviderOnlyHistoryContent(historyMessage.Content) {
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
