package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/chat"
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/session"
)

// runChatDebug 是 SEELEX_TEST_DEBUG=1 门控的临时诊断日志（复跑噪音点时
// 定位会话级执行路径；定位完成后清理）。
func runChatDebug(format string, args ...any) {
	if os.Getenv("SEELEX_TEST_DEBUG") == "1" {
		log.Printf("[runChat-debug] "+format, args...)
	}
}

// isActiveSessionLocked 判定指定会话是否为共享快照归属会话（锁内调用；
// 空会话 ID 视为活跃，兼容 draft 状态）。
func (service *Service) isActiveSessionLocked(sessionID string) bool {
	return sessionID == "" || sessionID == service.Core.Snapshot.Session.ID
}

// nextChatRequestIDLocked 生成跨会话唯一的聊天请求 ID（调用方持有
// Core.Mu）。时间戳 + 单调序号：仅时间戳在 Windows（UnixNano 分辨率约
// 0.5ms）下并行会话启动会碰撞。
func (service *Service) nextChatRequestIDLocked() string {
	service.chatSeq++
	return fmt.Sprintf("chat-%d-%d", time.Now().UnixNano(), service.chatSeq)
}

func (service *Service) startChat(parent context.Context, request chatRequest) error {
	return service.startChatFor(service.Core.Snapshot.Session.ID, parent, request)
}

// startChatFor 在指定会话启动 ReAct 对话（多会话并行：后台会话不写活跃
// 快照，只维护会话级任务/plan 状态与按会话路由的事件）。
func (service *Service) startChatFor(sessionID string, parent context.Context, request chatRequest) error {
	service.Mu.Lock()
	if service.closed {
		service.Mu.Unlock()
		return fmt.Errorf("application is shut down")
	}
	if service.draining {
		service.Mu.Unlock()
		return ErrApplicationDraining
	}
	active := service.isActiveSessionLocked(sessionID)
	runtime := service.sessionUnitLocked(sessionID)
	if runtime.ChatState().Running {
		service.Mu.Unlock()
		return ErrChatRunning
	}
	requestID := service.nextChatRequestIDLocked()
	runChatDebug("startChatFor session=%s request=%s input=%q active=%v", sessionID, requestID, request.displayInput, active)
	budget := request.budget
	if budget.MaxToolRounds <= 0 && budget.MaxToolCalls <= 0 {
		budget = reactBudgetFor(service.effortManager.Current())
	}
	chatContext, cancel := context.WithCancel(parent)
	runtime.SetCancel(cancel)
	service.components.tasks.StartReActBudgetForLocked(sessionID, requestID, budget)
	previousTask := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	previousCheckpoint := TaskCheckpoint{}
	if previousTask != nil && task_context.IsContinuableStatus(previousTask.Status) {
		previousCheckpoint = service.components.tasks.BuildTaskCheckpointLocked(previousTask)
	}
	taskState := service.components.tasks.BeginTaskFor(sessionID, requestID, request.displayInput, service.effortManager.Current(), previousTask, previousCheckpoint)
	service.components.tasks.ActivateTaskSkillsLocked(taskState, request.skills)
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: requestID, Role: "user", Content: request.displayInput})
	runtime.SetStream(chat.NewVisibleOutputStream(requestID))
	service.markBusyLocked()
	runtime.SetChatState(ChatState{Running: true, RequestID: requestID, StartedAt: time.Now()}, nil)
	// 阶段 1：聊天运行态投影写会话 scope（活跃会话镜像 Snapshot.Chat）。
	service.setSessionChatLockedFor(sessionID, runtime.ChatState())
	if active {
		service.components.tasks.SetTaskStateLocked(requestID, TaskProgressing, "Task is in progress.")
	}
	// L2：标题按会话设置（后台会话首次请求也归属自己的标题，不读活跃槽）；
	// 仅活跃会话同步到快照展示名。
	if service.components.sessions.SessionTitleFor(sessionID).Value == "" {
		title := SessionTitle{Value: session_runtime.SessionTitle(request.displayInput), Source: "first_request", FinalizedAt: time.Now()}
		service.components.sessions.SetSessionTitleLocked(sessionID, title)
		if active {
			service.Core.Snapshot.Session.Name = title.Value
		}
	}
	var user, assistant Message
	if active {
		user = *service.appendMessageLocked("user", request.displayInput, nil)
		assistant = *service.appendMessageLocked("assistant", "", nil)
	} else {
		// 阶段 1：后台会话也维护自己的可见投影（hot_attach 回看有数据）。
		user = *service.appendSessionMessageLocked(sessionID, "user", request.displayInput, nil)
		assistant = *service.appendSessionMessageLocked(sessionID, "assistant", "", nil)
	}
	revision := uint64(0)
	if active {
		revision = service.bumpLocked()
	}
	service.Mu.Unlock()
	// 新批次：此后创建的 todo/task/plan/subagent 条目自动归属当前 chat
	// 请求（requestID），工作表格按批次分片。
	service.Deps.Runtime.SetCurrentTaskBatch(sessionID, requestID)
	service.publishRuntimeProjections()
	// 子代理 merge-back 排队内容注入（锁外、ChatStream 开始前）：节点执行
	// 期间主会话被持锁无法回写，只能在此时补注入。
	service.injectPendingSubagentContextsFor(sessionID)
	service.publishChatStateFor(sessionID)
	service.publishSessionEvent(EventMessageAdded, revision, requestID, sessionID, user)
	service.publishSessionEvent(EventMessageAdded, revision, requestID, sessionID, assistant)
	// 会话列表状态机：chat 启动即发布 snapshot.changed，前端刷新左侧栏
	// 显示"运行中"（完成路径 runChat 尾部已有对应发布，恢复到 idle）。
	service.publishSessionEvent(EventSnapshotChanged, revision, requestID, sessionID, nil)
	go service.runChat(chatContext, sessionID, requestID, request)
	return nil
}

// runChat 在独立 goroutine 中执行一次会话提交：委托 Seele loop（9.2 边界，
// thin-wrapper-session-design.md §1.2/§6）——
//   - 提交：session.Engine.ChatStreamFor（显式 sid 路由，切换不串写）；
//   - 执行：Seele ReAct loop 在单次 ChatStream 内完成全部工具轮次，core
//     不自编循环（UC6 结构断言，见 chat_delegation_test.go）；
//   - 观察：Seele LoopHooks 链（seelebridge Chain：DiagnosticHook →
//     StageHook → SummaryHook）只做记录/透传，不改变 loop 控制流；
//     core 侧仅投影 ΔV/事件（appendDelta / appendVisibleDeltaBackground）。
//   - 收尾：context 恢复、task 终态、会话粒度 persist、ReleaseWorkingHistory。
//
// 事件指纹方法：相同输入序列 → 相同事件序列（kind + session/request/message
// ID 序数归一化），见 session_decoupling_test.go TestEventFingerprintStable。
func (service *Service) runChat(ctx context.Context, sessionID, requestID string, request chatRequest) {
	ctx = withSessionID(ctx, sessionID)
	defer service.components.tasks.ClearReActBudget(requestID)
	var err error
	runChatDebug("runChat start session=%s request=%s input=%q", sessionID, requestID, request.displayInput)
	defer func() { runChatDebug("runChat end session=%s request=%s err=%v", sessionID, requestID, err) }()
	recovered := false
	modelInput := request.modelInput
	batcher, onChunk := service.newBatchedDeltaSink(requestID)
	service.components.prompts.ApplyActiveTaskSystemPromptFor(sessionID, requestID)
	modelInput, err = service.components.context.PrepareExecutionContextFor(sessionID, requestID, modelInput)
	if err == nil {
		modelInput = nonEmptyProviderInput(modelInput)
		runChatDebug("runChat entering chatStream session=%s request=%s", sessionID, requestID)
		var reply string
		reply, err = service.chatStream(ctx, sessionID, modelInput, onChunk)
		runChatDebug("runChat chatStream returned session=%s request=%s replyLen=%d err=%v", sessionID, requestID, len(reply), err)
		if err == nil {
			// 回合结束后把引擎历史中最后一次 assistant 的推理内容挂到可见消息：
			// 聊天区一行带过，轨迹区完整查看（reasoning_content 与 content 分离）。
			service.attachLatestReasoning(sessionID, requestID)
		}
		// 模型输出观测（自然终态判定输入面）
		_ = service.components.tasks.ObserveModelOutput(ctx, task_context.ModelOutput{RequestID: requestID, Reply: reply, Err: err})
		if reply != "" {
			service.components.tasks.EnsureFinalAssistantTranscript(requestID, reply)
		}
		if contextErr := service.components.context.TakeContextControlFailure(requestID); contextErr != nil {
			err = contextErr
		}
		var recoveryErr error
		recovered, recoveryErr = service.recoverProviderFailureFor(ctx, err, request.displayInput)
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
	service.injectPendingSubagentContextsFor(sessionID)
	runtimeProjection := service.collectRuntimeProjectionFor(context.Background(), sessionID)
	if cleanupErr := service.components.context.RemoveTaskContextCheckpointsFor(sessionID); cleanupErr != nil && err == nil {
		err = cleanupErr
	}
	if err == nil {
		if cleanupErr := service.removeProviderContextRecoveryFor(sessionID); cleanupErr != nil {
			err = cleanupErr
		}
	}
	if err != nil {
		service.Mu.Lock()
		service.recordUnhandledTaskErrorLocked(requestID, err)
		service.Mu.Unlock()
	}
	location := service.components.sessions.LocateSession(sessionID)
	saveErr := service.components.sessions.PersistCurrentSession(location, sessionID)
	if saveErr != nil {
		if err != nil {
			err = wrapError(fmt.Errorf("%w; persistence failed and recovery is not guaranteed: %v", err, saveErr), errorCodePersistenceFailed)
		} else {
			err = wrapError(fmt.Errorf("persistence failed and recovery is not guaranteed: %w", saveErr), errorCodePersistenceFailed)
		}
	} else if releaser, ok := service.Deps.Engine.(interface{ ReleaseWorkingHistoryFor(string) }); ok {
		releaser.ReleaseWorkingHistoryFor(sessionID)
	}
	service.Mu.Lock()
	active := service.isActiveSessionLocked(sessionID)
	runtime := service.sessionUnitLocked(sessionID)
	if runtime.ChatState().RequestID != requestID {
		runChatDebug("runChat stale request session=%s request=%s runtimeRequest=%s (superseded)", sessionID, requestID, runtime.ChatState().RequestID)
		service.Mu.Unlock()
		return
	}
	runtime.UpdateChat(func(chat *ChatState) { chat.Error = "" }, nil)
	visibleError := ""
	if err != nil {
		if isUnclassifiedRunChatError(err) {
			log.Printf("[runChat] request_id=%s unclassified_error=%v", requestID, err)
		}
		visibleError = presentUserError(err)
		runtime.UpdateChat(func(chat *ChatState) { chat.Error = visibleError }, nil)
		service.appendSessionMessageLocked(sessionID, "error", visibleError, nil)
	}
	// 不在此处从 Engine.History() 重建 conversation——增量构建已在
	// startChat/handleToolStart/handleToolComplete/appendDelta 中完成，
	// 全量重建可能带入跨会话的残留消息。
	// 每会话投影写回本会话槽（G1）：后台会话的运行态不再丢弃——回看/切换
	// 时 SnapshotOf 直接读槽；活跃会话由协调器镜像 Snapshot.Runtime。
	service.applyRuntimeProjectionForLocked(sessionID, runtimeProjection)
	// 处理输入队列（单一消费点）：取排队输入合并为一条，批量发送并起下一轮
	pendingQueue := queuedChatRequests(runtime.PendingRequests())
	processQueue := len(pendingQueue) > 0
	var batchRequest chatRequest
	var nextContext context.Context
	var nextCancel context.CancelFunc
	nextRequestID := ""
	var nextUser, nextAssistant *Message
	if processQueue {
		// UI 展示原始输入，模型输入使用每次 Submit 时固化的 Skill 上下文。
		batchRequest = combineChatRequests(pendingQueue)
		runtime.SetRequests(nil)
		runtime.UpdateChat(func(chat *ChatState) {
			chat.QueuedCount = 0
			chat.InputQueue = nil
		}, nil)
		nextRequestID = service.nextChatRequestIDLocked()
		budget := batchRequest.budget
		if budget.MaxToolRounds <= 0 && budget.MaxToolCalls <= 0 {
			budget = reactBudgetFor(service.effortManager.Current())
		}
		nextContext, nextCancel = context.WithCancel(context.Background())
		runtime.SetCancel(nextCancel)
		service.components.tasks.StartReActBudgetForLocked(sessionID, nextRequestID, budget)
		previousTask := service.components.tasks.CurrentTaskExecutionFor(sessionID)
		previousCheckpoint := TaskCheckpoint{}
		if previousTask != nil && task_context.IsContinuableStatus(previousTask.Status) {
			previousCheckpoint = service.components.tasks.BuildTaskCheckpointLocked(previousTask)
		}
		taskState := service.components.tasks.BeginTaskFor(sessionID, nextRequestID, batchRequest.displayInput, service.effortManager.Current(), previousTask, previousCheckpoint)
		service.components.tasks.ActivateTaskSkillsLocked(taskState, batchRequest.skills)
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: nextRequestID, Role: "user", Content: batchRequest.displayInput})
		runtime.SetStream(chat.NewVisibleOutputStream(nextRequestID))
		runtime.SetChatState(ChatState{Running: true, RequestID: nextRequestID, StartedAt: time.Now()}, nil)
		service.components.tasks.SetTaskStateLocked(nextRequestID, TaskProgressing, "Task is in progress.")
		service.setSessionChatLockedFor(sessionID, runtime.ChatState())
		if active {
			nextUser = service.appendMessageLocked("user", batchRequest.displayInput, nil)
			nextAssistant = service.appendMessageLocked("assistant", "", nil)
		} else {
			nextUser = service.appendSessionMessageLocked(sessionID, "user", batchRequest.displayInput, nil)
			nextAssistant = service.appendSessionMessageLocked(sessionID, "assistant", "", nil)
		}
	} else {
		runtime.UpdateChat(func(chat *ChatState) { chat.Running = false }, nil)
		runtime.SetCancel(nil)
		if !service.anyChatRunningLocked() {
			service.markIdleLocked()
		}
	}
	service.setSessionChatLockedFor(sessionID, runtime.ChatState())
	revision := uint64(0)
	if active {
		revision = service.bumpLocked()
	}
	service.Mu.Unlock()
	runChatDebug("runChat tail session=%s request=%s err=%v processQueue=%v nextRequest=%q", sessionID, requestID, err, processQueue, nextRequestID)
	service.publishChatStateFor(sessionID)
	if err != nil {
		service.publishSessionEvent(EventError, revision, requestID, sessionID, map[string]string{"message": visibleError})
	} else {
		service.publishSessionEvent(EventSnapshotChanged, revision, requestID, sessionID, nil)
	}
	// 批量发送：所有排队消息一次发给 LLM
	if processQueue {
		service.publishSessionEvent(EventMessageAdded, revision, nextRequestID, sessionID, *nextUser)
		service.publishSessionEvent(EventMessageAdded, revision, nextRequestID, sessionID, *nextAssistant)
		service.Deps.Runtime.SetCurrentTaskBatch(sessionID, nextRequestID)
		service.publishRuntimeProjections()
		go service.runChat(nextContext, sessionID, nextRequestID, batchRequest)
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
	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		sessionID = service.Core.Snapshot.Session.ID
	}
	budgetErr := service.components.tasks.ReActBudgetError(requestID)
	if budgetErr != nil {
		budgetErr = wrapError(budgetErr, errorCodeReActBudget)
	}
	if budgetErr == nil {
		return nil
	}
	finalizationInput, prepareErr := service.components.context.PrepareExecutionContextFor(sessionID, requestID, reactBudgetFinalizationInput)
	if prepareErr != nil {
		return fmt.Errorf("%w; prepare final delivery context: %v", budgetErr, prepareErr)
	}
	result, err := service.chatStream(ctx, sessionID, finalizationInput, onChunk)
	cleanupErr := service.removeReActBudgetFinalizationInput(sessionID)
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

func (service *Service) removeReActBudgetFinalizationInput(sessionID string) error {
	history := service.engineHistoryFor(sessionID)
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
	if err := service.replaceEngineHistory(sessionID, filtered); err != nil {
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
	// batcher 一律挂到所属会话单元（不再只看活跃快照 requestID）：后台会话
	// 流式文本也必须能在工具钩子边界被按会话 flush，否则视图切到运行中会话
	// 后，缓冲文本会在工具消息之后才落地（排序混乱）。
	if sessionID := service.components.tasks.SessionIDForRequest(requestID); sessionID != "" {
		if unit := service.sessions.Unit(sessionID); unit != nil {
			unit.SetBatcher(batcher)
		}
	}
	service.Mu.Unlock()
	return batcher, func(chunk string) {
		if visible := service.consumeVisibleChunk(requestID, chunk); visible != "" {
			batcher.OnChunk(visible)
		}
	}
}

// flushStreamBatcherFor 把指定会话的流式缓冲同步落地（工具钩子边界调用：
// 文本必须先于工具消息进入可见会话，保持「文本 → 工具 → 结果 → 下一条
// 文本」顺序）。目标会话即工具所在会话，与活跃视图无关。
func (service *Service) flushStreamBatcherFor(sessionID string) {
	if sessionID == "" {
		return
	}
	service.Mu.RLock()
	var batcher session.StreamBatcherSink
	if unit := service.sessions.Unit(sessionID); unit != nil {
		batcher = unit.BatcherSink()
	}
	service.Mu.RUnlock()
	if batcher != nil {
		_ = batcher.FlushPending()
	}
}

func (service *Service) consumeVisibleChunk(requestID, chunk string) string {
	sessionID := service.components.tasks.SessionIDForRequest(requestID)
	if sessionID != "" && sessionID != service.sessions.ActiveID() {
		// 后台会话：只写自身流状态，不取全局锁（线程隔离）。
		return service.consumeVisibleChunkBackground(sessionID, requestID, chunk)
	}
	service.Mu.Lock()
	defer service.Mu.Unlock()
	runtime := service.sessionUnitLocked(sessionID)
	running := runtime.ChatState().Running && runtime.ChatState().RequestID == requestID
	if !running && service.Core.Snapshot.Chat.Running && service.Core.Snapshot.Chat.RequestID == requestID {
		// 兼容测试直写 Snapshot.Chat（活跃会话便捷构造）；生产路径 runtime 权威。
		running = true
		sessionID = service.Core.Snapshot.Session.ID
		runtime = service.sessionUnitLocked(sessionID)
	}
	if !running {
		return ""
	}
	if stream := runtime.StreamSink(); stream == nil || stream.RequestID() != requestID {
		runtime.SetStream(chat.NewVisibleOutputStream(requestID))
	}
	return runtime.StreamSink().Consume(chunk)
}

// consumeVisibleChunkBackground 后台会话的流式消费：仅触碰该会话单元自身的
// 锁与流，不获取全局锁。
func (service *Service) consumeVisibleChunkBackground(sessionID, requestID, chunk string) string {
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return ""
	}
	runtime := unit
	chatState := runtime.ChatState()
	if !chatState.Running || chatState.RequestID != requestID {
		return ""
	}
	if stream := runtime.StreamSink(); stream == nil || stream.RequestID() != requestID {
		runtime.SetStream(chat.NewVisibleOutputStream(requestID))
	}
	return runtime.StreamSink().Consume(chunk)
}

func (service *Service) appendVisibleDelta(requestID, chunk string) {
	if chunk == "" {
		return
	}
	sessionID := service.components.tasks.SessionIDForRequest(requestID)
	if sessionID != "" && sessionID != service.sessions.ActiveID() {
		// 后台会话：写自身 View（View.mu），零全局锁；revision 用会话本地计数。
		service.appendVisibleDeltaBackground(sessionID, requestID, chunk)
		return
	}
	service.Mu.Lock()
	// 阶段 1：流式增量按 requestID 反查会话，写该会话自己的 view（后台
	// 会话也实时维护可见投影；活跃会话镜像 Snapshot）。
	runtime := service.sessionUnitLocked(sessionID)
	running := runtime.ChatState().Running && runtime.ChatState().RequestID == requestID
	if !running && service.Core.Snapshot.Chat.Running && service.Core.Snapshot.Chat.RequestID == requestID {
		running = true
		sessionID = service.Core.Snapshot.Session.ID
		runtime = service.sessionUnitLocked(sessionID)
	}
	if !running {
		service.Mu.Unlock()
		return
	}
	view := service.components.view.SessionViewLocked(sessionID)
	messageID := ""
	for index := len(view.Conversation) - 1; index >= 0; index-- {
		if view.Conversation[index].Role == "assistant" && view.Conversation[index].Tool == nil {
			view.Conversation[index].Content += chunk
			messageID = view.Conversation[index].ID
			break
		}
	}
	service.mirrorActiveViewLocked()
	revision := service.bumpLocked()
	service.Mu.Unlock()
	service.publishSessionEvent(EventMessageDelta, revision, requestID, sessionID, MessageDelta{MessageID: messageID, Delta: chunk})
}

// appendVisibleDeltaBackground 后台会话的流式增量：仅 View.mu + 会话本地
// revision，不获取全局锁（切换后由基线重建，无需全局 revision 语义）。
func (service *Service) appendVisibleDeltaBackground(sessionID, requestID, chunk string) {
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return
	}
	chatState := unit.ChatState()
	if !chatState.Running || chatState.RequestID != requestID {
		return
	}
	var messageID string
	var revision uint64
	unit.View.Mutate(func(view *session.View) {
		for index := len(view.Conversation) - 1; index >= 0; index-- {
			if view.Conversation[index].Role == "assistant" && view.Conversation[index].Tool == nil {
				view.Conversation[index].Content += chunk
				messageID = view.Conversation[index].ID
				break
			}
		}
		view.Revision++
		revision = view.Revision
	})
	if messageID == "" {
		return
	}
	service.publishSessionEvent(EventMessageDelta, revision, requestID, sessionID, MessageDelta{MessageID: messageID, Delta: chunk})
}

// attachLatestReasoning 在聊天回合结束后，把引擎历史中最后一次 assistant
// 的推理内容挂到可见 assistant 消息（ReasoningContent 字段）。推理内容与
// content 分离：聊天区一行带过，轨迹区完整查看；不写入 content，避免与
// 可见回复混排。找不到目标消息或内容未变化时静默返回。
func (service *Service) attachLatestReasoning(sessionID, requestID string) {
	history := service.engineHistoryFor(sessionID)
	reasoning := ""
	for index := len(history) - 1; index >= 0; index-- {
		if history[index].Role == "assistant" && history[index].ReasoningContent != "" {
			reasoning = history[index].ReasoningContent
			break
		}
	}
	if reasoning == "" {
		return
	}
	service.Mu.Lock()
	view := service.sessionViewLocked(sessionID)
	messageID := ""
	for index := len(view.Conversation) - 1; index >= 0; index-- {
		if view.Conversation[index].Role != "assistant" || view.Conversation[index].Tool != nil {
			continue
		}
		if view.Conversation[index].ReasoningContent == reasoning {
			service.Mu.Unlock()
			return
		}
		view.Conversation[index].ReasoningContent = reasoning
		messageID = view.Conversation[index].ID
		break
	}
	if messageID == "" {
		service.Mu.Unlock()
		return
	}
	service.mirrorActiveViewLocked()
	revision := service.bumpLocked()
	service.Mu.Unlock()
	service.publishSessionEvent(EventMessageDelta, revision, requestID, sessionID, MessageDelta{
		MessageID:        messageID,
		ReasoningContent: reasoning,
	})
}

func (service *Service) appendHistoryLocked(history []EngineMessage) {
	service.appendHistoryLockedFor(service.Core.Snapshot.Session.ID, history)
}

// appendHistoryLockedFor 把引擎历史追加为指定会话的可见消息（冷加载无
// record 的旧格式会话恢复路径；阶段 1：写会话 view）。
func (service *Service) appendHistoryLockedFor(sessionID string, history []EngineMessage) {
	for _, historyMessage := range history {
		if !isVisibleHistoryMessage(historyMessage) {
			continue
		}
		if historyMessage.Role != "tool" && historyMessage.Content != "" && !context_runtime.IsProviderOnlyHistoryContent(historyMessage.Content) {
			content := historyMessage.Content
			if historyMessage.Role == "user" {
				content = displayUserInput(content)
			}
			appended := service.appendSessionMessageLocked(sessionID, historyMessage.Role, content, nil)
			if historyMessage.ReasoningContent != "" && appended != nil {
				appended.ReasoningContent = historyMessage.ReasoningContent
			}
		}
		for _, call := range historyMessage.ToolCalls {
			service.appendSessionMessageLocked(sessionID, "tool", "", &ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: "success"})
		}
		if historyMessage.Role == "tool" {
			visible, ref, truncated, totalChars := service.boundToolResultForSnapshot(historyMessage.Name, historyMessage.Content)
			service.appendSessionMessageLocked(sessionID, "tool_result", visible, &ToolCall{
				ID: historyMessage.ToolCallID, Name: historyMessage.Name,
				Result: visible, Status: "success",
				ResultRef: ref, Truncated: truncated, TotalChars: totalChars,
			})
		}
	}
}
