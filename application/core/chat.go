package core

import (
	"context"
	"errors"
	"fmt"
	context_runtime "github.com/RedHuang-0622/seelex/application/conversation/context"
	"github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	"github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
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
	if service.components.view.IsRestoringLocked(sessionID) {
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
		user = *service.components.view.AppendMessageWithOriginLocked("user", request.DisplayInput, nil, userOrigin)
		assistant = *service.components.view.AppendMessageWithOriginLocked("assistant", "", nil, mainOrigin)
	} else {
		// 阶段 1：后台会话也维护自己的可见投影（hot_attach 回看有数据）。
		user = *service.components.view.AppendMessageWithOriginLockedFor(sessionID, "user", request.DisplayInput, nil, userOrigin)
		assistant = *service.components.view.AppendMessageWithOriginLockedFor(sessionID, "assistant", "", nil, mainOrigin)
	}
	revision := uint64(0)
	if active {
		revision = service.components.view.BumpLocked()
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
	go service.components.conversationTurnRunner.Run(chatContext, sessionID, requestID, request)
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
		service.components.conversationStreamRuntime.AppendDelta(requestID, chunk)
	})
}

// ensureViewPlanProjection 在进入终态工具前确保当前视图会话的协调器投影有
// 基线（迁移期：直设 Snapshot.Runtime.Plan 的路径/旧装配在 plan_load 前
// 没有种子时，从镜像补种；已有投影不覆盖）。
func (service *Service) ensureViewPlanProjection(ctx context.Context) {
	sessionID := task_context.SessionIDFromContext(ctx)
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
	sessionID := task_context.SessionIDFromContext(ctx)
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
	sessionID := task_context.SessionIDFromContext(ctx)
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
	sessionID := task_context.SessionIDFromContext(ctx)
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

func (service *Service) appendHistoryLocked(history []contract.EngineMessage) {
	service.appendHistoryLockedFor(service.Core.Snapshot.Session.ID, history)
}

// appendHistoryLockedFor 把引擎历史追加为指定会话的可见消息（冷加载无
// record 的旧格式会话恢复路径；阶段 1：写会话 view）。
func (service *Service) appendHistoryLockedFor(sessionID string, history []contract.EngineMessage) {
	for _, historyMessage := range history {
		if !service.components.sessionHistoryRuntime.IsVisibleHistoryMessage(historyMessage) {
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
			appended := service.components.view.AppendMessageWithOriginLockedFor(sessionID, historyMessage.Role, content, nil, origin)
			if historyMessage.ReasoningContent != "" && appended != nil {
				appended.ReasoningContent = historyMessage.ReasoningContent
			}
		}
		for _, call := range historyMessage.ToolCalls {
			service.components.view.AppendMessageWithOriginLockedFor(sessionID, "tool", "", &model.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: dto.ToolEventSuccess}, origin)
		}
		if historyMessage.Role == "tool" {
			visible, ref, truncated, totalChars := service.boundToolResultForSnapshot(historyMessage.Name, historyMessage.Content)
			service.components.view.AppendMessageWithOriginLockedFor(sessionID, "tool_result", visible, &model.ToolCall{
				ID: historyMessage.ToolCallID, Name: historyMessage.Name,
				Result: visible, Status: dto.ToolEventSuccess,
				ResultRef: ref, Truncated: truncated, TotalChars: totalChars,
			}, origin)
		}
	}
}
