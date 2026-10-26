package core

import (
	"context"
	"errors"
	"fmt"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	context_runtime "github.com/RedHuang-0622/seelex/application/conversation/context"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	"github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
	"log"
	"os"
	"strings"
	"time"
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
// Core.ViewMu）。时间戳 + 单调序号：仅时间戳在 Windows（UnixNano 分辨率约
// 0.5ms）下并行会话启动会碰撞。
func (service *Service) nextChatRequestIDLocked() string {
	service.chatSeq++
	return fmt.Sprintf("chat-%d-%d", time.Now().UnixNano(), service.chatSeq)
}

// startChat 在**当前视图会话**上启动 ReAct 对话。
//
// 会话 ID 必须持 Core.ViewMu 读取：视图指针由热挂载/冷加载在持锁段内整体改写
// （session_lifecycle.go 的 `service.Core.Snapshot.Session = SessionState{...}`），
// 无锁读会与「切换会话」并发构成数据竞争——CI 的 race-and-coverage 作业已在
// TestWorkspaceSwitchConcurrentWithBackgroundPersist 上复现（Write 在 hotAttachSession、
// 无锁 Read 就在本函数）。这里只取一次 ID；后续校验与状态机仍由 startChatFor
// 自己持锁完成，两段之间的切换不会让本函数写错状态，只是这一轮落在调用发生时的
// 那个会话上。
func (service *Service) startChat(parent context.Context, request chatRequest) error {
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	return service.startChatFor(sessionID, parent, request)
}

// startChatFor 在指定会话启动 ReAct 对话（多会话并行：后台会话不写活跃
// 快照，只维护会话级任务/plan 状态与按会话路由的事件）。
func (service *Service) startChatFor(sessionID string, parent context.Context, request chatRequest) error {
	service.ViewMu.Lock()
	if service.closed {
		service.ViewMu.Unlock()
		return fmt.Errorf("application is shut down")
	}
	if service.draining {
		service.ViewMu.Unlock()
		return ErrApplicationDraining
	}
	// 恢复门兜底（TOCTOU 收口）：submitConversation/For 在释放 ViewMu 后调用
	// 本函数，切换可能落在两次取锁之间。restoring 期间不得启动新回合。
	if service.isRestoringLocked(sessionID) {
		service.ViewMu.Unlock()
		return ErrSessionRestoring
	}
	active := service.isActiveSessionLocked(sessionID)
	runtime := service.sessionUnitLocked(sessionID)
	if runtime.ChatState().Running {
		// TOCTOU 收口：调用方（提交路径）在释放 ViewMu 后重新取锁的窗口里，
		// 另一个回合可能刚好起跑。输入语义是"要么开跑、要么排队"——已受理的
		// 提交不能因为晚了一步就被丢掉（deferred submit 只记日志）。
		revision := service.enqueueChatLocked(sessionID, runtime, request, active)
		service.ViewMu.Unlock()
		service.afterChatEnqueued(sessionID, revision, request)
		return nil
	}
	chatContext, requestID, userEvent := service.components.conversationTurnProcess.BeginLocked(
		parent, sessionID, runtime, request, chatTurnStartOptions{
			MarkBusy: true, TaskProgressAfterMirror: active,
		},
	)
	runChatDebug("startChatFor session=%s request=%s input=%q active=%v", sessionID, requestID, request.DisplayInput, active)
	// L2：标题按会话设置（后台会话首次请求也归属自己的标题，不读活跃槽）；
	// 仅活跃会话同步到快照展示名。
	if service.components.sessions.SessionTitleFor(sessionID).Value == "" {
		title := model.SessionTitle{Value: session_runtime.SessionTitle(request.DisplayInput), Source: "first_request", FinalizedAt: time.Now()}
		service.components.sessions.SetSessionTitleLocked(sessionID, title)
		if active {
			service.Core.Snapshot.Session.Name = title.Value
		}
	}
	var user, assistant model.Message
	userOrigin := model.MessageOrigin{
		RoleName: userEvent.RoleName, RoleSessionID: userEvent.RoleSessionID,
		RoundID: userEvent.RoundID, UnitSeq: userEvent.UnitSeq, EventSeq: userEvent.Seq,
	}
	// EXEC（main）的回合行：角色会话即主会话，round 与用户行同轮；正文随后由
	// 流式增量写入同一条消息，归属字段不会丢。
	mainOrigin := model.MessageOrigin{RoleName: RoleNameMain, RoleSessionID: sessionID, RoundID: userEvent.RoundID}
	if active {
		user = *service.appendMessageWithOriginLocked("user", request.DisplayInput, nil, userOrigin)
		assistant = *service.appendMessageWithOriginLocked("assistant", "", nil, mainOrigin)
	} else {
		// 阶段 1：后台会话也维护自己的可见投影（hot_attach 回看有数据）。
		user = *service.appendSessionMessageWithOriginLocked(sessionID, "user", request.DisplayInput, nil, userOrigin)
		assistant = *service.appendSessionMessageWithOriginLocked(sessionID, "assistant", "", nil, mainOrigin)
	}
	revision := uint64(0)
	if active {
		revision = service.bumpLocked()
	}
	service.ViewMu.Unlock()
	// 新批次：此后创建的 todo/task/plan/subagent 条目自动归属当前 chat
	// 请求（requestID），工作表格按批次分片。
	service.Deps.Runtime.SetCurrentTaskBatch(sessionID, requestID)
	service.publishRuntimeProjections()
	// 子代理 mailbox 排空并丢弃（锁外安全点）：子代理结论已由工具结果
	// 回传，mailbox 内容不再注入引擎历史/可见会话/transcript。
	service.discardPendingSubagentContextsFor(sessionID)
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
	service.components.conversationTurnRunner.Run(ctx, sessionID, requestID, request)
}

func (service *Service) recordUnhandledTaskErrorLocked(sessionID, requestID string, err error) {
	// 读权威任务状态（TaskStateFor），不再依赖 Snapshot.Task 镜像是否新鲜
	// （镜像写已收敛到 runChat 收尾的 ViewMu 段）。
	task := service.components.tasks.TaskStateFor(sessionID)
	if task == nil || task.RequestID != requestID || task.Status != TurnProgressing {
		return
	}
	if errors.Is(err, context.Canceled) {
		service.components.tasks.InterruptTaskLocked(requestID, "The task was canceled before completion.")
		return
	}
	if kind := contract.ClassifyProviderFailure(err); kind != contract.ProviderFailureNone {
		service.components.tasks.InterruptTaskLocked(requestID, "The provider interrupted the task before completion.")
		return
	}
	service.components.tasks.FailTaskLocked(requestID, "The task stopped before a verified completion could be delivered.")
}

// reactBudgetFinalizationInput 是预算耗尽后的**+1 次调用**输入（用户裁决 P2：
// 这一次只做总结，不再调查/执行/验证）。前缀来自契约层唯一一份标记——判定侧
// （backend/engine/context）用同一个常量把这条内部行从模型上下文里过滤掉。
const reactBudgetFinalizationInput = contract.ReactBudgetFinalizationMarker + "\n" +
	"The execution budget is exhausted. Do not call any tools again. " +
	"In one final text response, summarize what was actually completed within the budget, " +
	"what remains unfinished, and the evidence collected for both. " +
	"If the task is complete, state the delivered result; if not, state the verified gap. " +
	"Do not claim work that was not done."

// finalizeReActBudget 在工具预算耗尽后保留一次纯文本交付回合。常规循环在
// 此之前已停止；本回合让用户收到结果，而非裸预算错误。
func (service *Service) finalizeReActBudget(ctx context.Context, requestID string) error {
	return service.finalizeReActBudgetWithSink(ctx, requestID, func(chunk string) {
		service.appendDelta(requestID, chunk)
	})
}

// TaskTerminalHandler 返回面向 Runtime 的终态工具 handler，同时把请求状态
// 所有权保留在 Application。handler 无外部副作用，终态工具经
// task_context.Coordinator.VerifyAndApply 路由。
func (service *Service) TaskTerminalHandler(kind string) func(context.Context, string) (string, error) {
	return func(ctx context.Context, argsJSON string) (string, error) {
		service.ensureViewPlanProjection(ctx)
		result, err := service.components.tasks.VerifyAndApply(ctx, kind, argsJSON)
		if err == nil {
			service.mirrorActiveTaskAfterTerminal(ctx)
			if kind == task_context.ToolCheckNode || kind == task_context.ToolComplete {
				service.mirrorActivePlanAfterTerminal(ctx)
			}
		}
		return result, err
	}
}

// ensureViewPlanProjection 在进入终态工具前确保当前视图会话的协调器投影有
// 基线（迁移期：直设 Snapshot.Runtime.Plan 的路径/旧装配在 plan_load 前
// 没有种子时，从镜像补种；已有投影不覆盖）。
func (service *Service) ensureViewPlanProjection(ctx context.Context) {
	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		service.ViewMu.RLock()
		sessionID = service.Core.Snapshot.Session.ID
		service.ViewMu.RUnlock()
	}
	if sessionID == "" {
		return
	}
	service.ViewMu.RLock()
	plan := service.Core.Snapshot.Runtime.Plan
	service.ViewMu.RUnlock()
	service.components.tasks.EnsurePlanProjection(sessionID, plan)
}

// mirrorActivePlanAfterTerminal 在 check/complete 终态把协调器投影的最新
// plan 镜像进 Snapshot（这些工具以投影为权威变更点）。
func (service *Service) mirrorActivePlanAfterTerminal(ctx context.Context) {
	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		service.ViewMu.RLock()
		sessionID = service.Core.Snapshot.Session.ID
		service.ViewMu.RUnlock()
	}
	if sessionID == "" {
		return
	}
	plan := service.components.tasks.PlanProjectionCopy(sessionID)
	if plan == nil {
		return
	}
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	if sessionID == service.Core.Snapshot.Session.ID {
		service.Core.Snapshot.Runtime.Plan = plan
	}
}

// mirrorActiveTaskAfterTerminal 在终态工具落地后把活跃会话的权威任务状态
// 镜像进 Snapshot.Task（TaskService 不再直写快照；工具路径没有 runChat 收尾
// 的镜像段，须在此补齐，见 task_service.go lastTaskState 说明）。
func (service *Service) mirrorActiveTaskAfterTerminal(ctx context.Context) {
	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		service.ViewMu.RLock()
		sessionID = service.Core.Snapshot.Session.ID
		service.ViewMu.RUnlock()
	}
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	if sessionID == "" || sessionID != service.Core.Snapshot.Session.ID {
		return
	}
	if task := service.components.tasks.VisibleTaskStateFor(sessionID); task != nil {
		service.Core.Snapshot.Task = task
	}
}

// finalizeTaskExecution 把自然停止转换为可审计的完成/交接
// （TaskService.OnChatEnd 入口）。
func (service *Service) finalizeTaskExecution(requestID string) error {
	service.ensureViewPlanProjection(context.Background())
	err := service.components.tasks.FinalizeTask(context.Background(), task_context.ChatEndSummary{RequestID: requestID})
	if err == nil {
		service.mirrorActiveTaskAfterTerminal(context.Background())
	}
	return err
}

func (service *Service) finalizeReActBudgetWithSink(ctx context.Context, requestID string, onChunk func(string)) error {
	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		// 回退读「当前会话」必须持 Core.ViewMu：视图指针由热切换在持锁段内整体
		// 改写（application/core/session_lifecycle.go 的 hotAttachSession），裸读
		// 与切换并发即数据竞争——同族的 startChat 竞争已在 CI 的 race-and-coverage
		// 作业里复现，这里一并收口。
		service.ViewMu.RLock()
		sessionID = service.Core.Snapshot.Session.ID
		service.ViewMu.RUnlock()
	}
	budgetErr := service.components.tasks.ReActBudgetError(requestID)
	if budgetErr != nil {
		budgetErr = service.components.errorPresentation.Wrap(budgetErr, contract.ErrorCodeReActBudget)
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
	filtered := make([]contract.EngineMessage, 0, len(history))
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

func (service *Service) newBatchedDeltaSink(requestID string) (applicationconversation.StreamBatcher, func(string)) {
	batcher := service.components.conversationStreams.NewBatcher(func(batch []string) {
		service.appendVisibleDelta(requestID, strings.Join(batch, ""))
	}, applicationconversation.StreamBatcherOptions{FlushSize: 32, BufferSize: 128, Interval: 40 * time.Millisecond})
	service.ViewMu.Lock()
	// batcher 一律挂到所属会话单元（不再只看活跃快照 requestID）：后台会话
	// 流式文本也必须能在工具钩子边界被按会话 flush，否则视图切到运行中会话
	// 后，缓冲文本会在工具消息之后才落地（排序混乱）。
	if sessionID := service.components.tasks.SessionIDForRequest(requestID); sessionID != "" {
		if unit := service.sessions.Unit(sessionID); unit != nil {
			unit.SetBatcher(batcher)
		}
	}
	service.ViewMu.Unlock()
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
	service.ViewMu.RLock()
	var batcher session.StreamBatcherSink
	if unit := service.sessions.Unit(sessionID); unit != nil {
		batcher = unit.BatcherSink()
	}
	service.ViewMu.RUnlock()
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
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
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
		runtime.SetStream(service.components.conversationStreams.NewVisibleOutput(requestID))
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
		runtime.SetStream(service.components.conversationStreams.NewVisibleOutput(requestID))
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
	service.ViewMu.Lock()
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
		service.ViewMu.Unlock()
		return
	}
	// 回看历史（窗口未贴尾）：窗口里最后一条不是本轮消息，写增量会挂到旧
	// 消息上（内容串写）。正文仍在引擎历史/transcript 中，回到最新后由基线
	// 重建；前端据窗口游标提示「下方还有新内容」。
	if service.components.view.SessionViewBrowsingHistoryLocked(sessionID) {
		service.ViewMu.Unlock()
		return
	}
	messageID := ""
	// G5 访问器化：会话 View 的写一律经 View.mu（Mutate），不再在
	// ViewMu 下裸字段穿越——后台/活跃同会话切换期间 View.mu 是叶子锁，
	// 镜像（view.Read）与增量写互斥，天然无竞态。
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		for index := len(view.Conversation) - 1; index >= 0; index-- {
			if view.Conversation[index].Role == "assistant" && view.Conversation[index].Tool == nil {
				view.Conversation[index].Content += chunk
				messageID = view.Conversation[index].ID
				break
			}
		}
	})
	service.mirrorActiveViewLocked()
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventMessageDelta, revision, requestID, sessionID, event.MessageDelta{MessageID: messageID, Delta: chunk})
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
	// 后台会话同样遵守窗口锚点：回看历史时不把增量写进旧消息。
	browsing := false
	unit.View.Read(func(view *session.View) {
		browsing = view.HistoryOffset+view_state.DurableConversationCount(view.Conversation) < view.TotalMessages
	})
	if browsing {
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
	service.publishSessionEvent(EventMessageDelta, revision, requestID, sessionID, event.MessageDelta{MessageID: messageID, Delta: chunk})
}

// attachLatestReasoning 在聊天回合结束后，把引擎历史里**每个 assistant 步骤**
// 的推理内容挂到可见窗口里与之对应的那条 assistant 消息（ReasoningContent
// 字段）。推理内容与 content 分离：聊天区一行带过，轨迹区完整查看；不写入
// content，避免与可见回复混排。
//
// 为什么必须按步骤挂：一个回合的「过程」由多次助手步骤组成（工具轮之间各一条），
// 落盘回填（task_context.BackfillAssistantReasoning）给每个步骤都补了推理、
// durable rows 因此每个步骤都带 reasoning；而热挂载只换视图指针、不重建正文
// （session_lifecycle.go hotAttachSession），可见窗口是唯一来源——只挂最后
// 一次会让工具轮的思考过程在热挂载后消失（冷加载/分页回读看得到，热路径看不到）。
//
// 配对口径与落盘回填一致（工具调用 ID 集合 + 归一化正文，逐条顺序消费）；一条
// 都配不上时退回老口径（最后一条推理挂窗口最后一条 assistant 消息），既有语义
// 与"已有推理不覆盖"的保守策略都保留。
func (service *Service) attachLatestReasoning(sessionID, requestID string) {
	history := service.engineHistoryFor(sessionID)
	steps := applicationconversation.ReasoningStepsFromHistory(history)
	if len(steps) == 0 {
		return
	}
	service.ViewMu.Lock()
	if service.components.view.SessionViewBrowsingHistoryLocked(sessionID) {
		// 回看历史：窗口最后一条不是本轮 assistant 消息，挂推理会写错消息。
		service.ViewMu.Unlock()
		return
	}
	var attached []event.MessageDelta
	// G5 访问器化：经 View.mu 读写可见消息（Mutate 内完成查找+写入，
	// 避免 ViewMu 下的裸字段穿越）。
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		attached = applicationconversation.AttachStepReasoningLocked(view, steps)
		if len(attached) > 0 {
			return
		}
		if delta, ok := applicationconversation.AttachLastReasoningLocked(view, applicationconversation.LastReasoningFromHistory(history)); ok {
			attached = append(attached, delta)
		}
	})
	if len(attached) == 0 {
		service.ViewMu.Unlock()
		return
	}
	service.mirrorActiveViewLocked()
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	for _, delta := range attached {
		if delta.MessageID == "" {
			continue
		}
		service.publishSessionEvent(EventMessageDelta, revision, requestID, sessionID, delta)
	}
}

// stampServedRoute 在回合收尾把本轮请求的实际承接路由（config role / config
// model / 厂商类型）回填进真相源（2026-10-10 用户决定：路由通知不进会话，
// 元信息直接长在消息上）：assistant 的 transcript 事件行（冷恢复/轨迹审计）
// + 可见窗口里本轮的 assistant 消息（前端 message 块元数据行）。路由事实由
// 请求解析中间件（账号池 completer 租约路径）得出，经 RuntimePort.TakeServedRoute
// 读即清——本轮没有请求真正派发就没有事实，残留不会串轮。
func (service *Service) stampServedRoute(sessionID, requestID string) {
	route, ok := service.Deps.Runtime.TakeServedRoute(sessionID)
	if !ok {
		return
	}
	service.components.tasks.StampAssistantRouteFor(sessionID, requestID, route.Role, route.Model, route.Provider)
	service.ViewMu.Lock()
	if service.components.view.SessionViewBrowsingHistoryLocked(sessionID) {
		// 回看历史：窗口最后一条不是本轮 assistant 消息，不挂——回最新后由
		// 基线重建（真相源事件行已带路由）带回。
		service.ViewMu.Unlock()
		return
	}
	var stamped model.Message
	found := false
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		for index := len(view.Conversation) - 1; index >= 0; index-- {
			message := &view.Conversation[index]
			if message.Role != "assistant" || message.Tool != nil {
				continue
			}
			if message.ConfigModel != "" {
				// 最后一条 assistant 已带路由 = 不是本轮消息（占位缺失等异常
				// 形状），保守不写，避免把本轮路由串到上一轮。
				return
			}
			message.ConfigRole = route.Role
			message.ConfigModel = route.Model
			message.ConfigProvider = route.Provider
			stamped = *message
			found = true
			return
		}
	})
	if !found {
		service.ViewMu.Unlock()
		return
	}
	service.mirrorActiveViewLocked()
	revision := uint64(0)
	if service.isActiveSessionLocked(sessionID) {
		revision = service.bumpLocked()
	}
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventMessageAdded, revision, requestID, sessionID, stamped)
}

// streamedAssistantTextLocked 返回会话本轮的可见 assistant 正文累积
// （= 本次请求截至当前的可见流式正文）。工具轮说明正文的**按迭代归位**以它为
// 基准（见 task_context.AttributeToolNarrationLocked）。
//
// 取值以**请求作用域的流缓冲**为准：回看历史（可见窗口未贴尾）时流式增量按
// 设计不进窗口（appendVisibleDelta 早退，避免把增量挂到旧消息上），此时从可见
// 窗口读到的是**别的轮次**的正文；而工具轮正文在框架 wire 上被丢弃（Seele
// session/loop.go callLLM 在带 tool_calls 时构造 Content:nil 的消息），视图之外
// 没有第二份——读错就是把本轮正文丢掉、把别的轮次正文挂到本工具轮上。
// 无流缓冲（非流式提供方/未开始流式）时回退可见窗口，保持旧语义。
// 调用方持有 Core.ViewMu（工具钩子边界）。
func (service *Service) streamedAssistantTextLocked(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	if text := service.streamedRequestTextLocked(sessionID); text != "" {
		return text
	}
	text := ""
	service.components.view.SessionViewReadLocked(sessionID, func(view *session.View) {
		for index := len(view.Conversation) - 1; index >= 0; index-- {
			message := view.Conversation[index]
			if message.Role != "assistant" || message.Tool != nil {
				continue
			}
			text = message.Content
			return
		}
	})
	return text
}

// streamedRequestTextLocked 返回会话当前请求已累积的可见流式正文（会话单元无
// 流缓冲、或流实现不累积正文时为空——调用方回退可见窗口）。调用方持有
// Core.ViewMu。
func (service *Service) streamedRequestTextLocked(sessionID string) string {
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return ""
	}
	stream := unit.StreamSink()
	if stream == nil {
		return ""
	}
	if texted, ok := stream.(interface{ Text() string }); ok {
		return texted.Text()
	}
	return ""
}

func (service *Service) appendHistoryLocked(history []contract.EngineMessage) {
	service.appendHistoryLockedFor(service.Core.Snapshot.Session.ID, history)
}

// appendHistoryLockedFor 把引擎历史追加为指定会话的可见消息（冷加载无
// record 的旧格式会话恢复路径；阶段 1：写会话 view）。
func (service *Service) appendHistoryLockedFor(sessionID string, history []contract.EngineMessage) {
	for _, historyMessage := range history {
		if !isVisibleHistoryMessage(historyMessage) {
			continue
		}
		// 引擎历史只有 provider role（这里的历史全部属于 EXEC 会话），按
		// task_context 的同一口径补默认归属，避免恢复后的旧会话又变成 AGENT。
		origin := model.MessageOrigin{RoleName: RoleNameMain, RoleSessionID: sessionID}
		if historyMessage.Role == "user" {
			origin.RoleName = RoleNameUser
		}
		if historyMessage.Role != "tool" && historyMessage.Content != "" && !context_runtime.IsProviderOnlyHistoryContent(historyMessage.Content) {
			content := historyMessage.Content
			if historyMessage.Role == "user" {
				content = displayUserInput(content)
			}
			appended := service.appendSessionMessageWithOriginLocked(sessionID, historyMessage.Role, content, nil, origin)
			if historyMessage.ReasoningContent != "" && appended != nil {
				appended.ReasoningContent = historyMessage.ReasoningContent
			}
		}
		for _, call := range historyMessage.ToolCalls {
			service.appendSessionMessageWithOriginLocked(sessionID, "tool", "", &model.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: dto.ToolEventSuccess}, origin)
		}
		if historyMessage.Role == "tool" {
			visible, ref, truncated, totalChars := service.boundToolResultForSnapshot(historyMessage.Name, historyMessage.Content)
			service.appendSessionMessageWithOriginLocked(sessionID, "tool_result", visible, &model.ToolCall{
				ID: historyMessage.ToolCallID, Name: historyMessage.Name,
				Result: visible, Status: dto.ToolEventSuccess,
				ResultRef: ref, Truncated: truncated, TotalChars: totalChars,
			}, origin)
		}
	}
}
