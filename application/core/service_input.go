package core

import (
	"context"
	"errors"
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/session"
)

// discardPendingSubagentContexts 排空 Runtime 持有的有界邮箱（活跃会话兼容
// 包装）。
func (service *Service) discardPendingSubagentContexts() {
	// 会话 ID 在 Core.ViewMu 读锁下取值：本方法是锁外入口（测试/活跃会话兼容
	// 包装），裸读会与热切换写 Snapshot.Session 竞争。
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	service.discardPendingSubagentContextsFor(sessionID)
}

// discardPendingSubagentContextsFor 排空 Runtime 持有的有界邮箱（单一来源 =
// Runtime mailbox）并**丢弃全部内容**：子代理结论经工具结果（tool_result /
// summary / NodeSemanticResult）回传父代理，mailbox 不再作为“注入引擎历史
// + 写入可见会话/transcript”的通道。保留排空是为了让 Runtime 的 mailbox
// 保持有界；任何残余载荷都不进入前端、上下文或持久化存储。
func (service *Service) discardPendingSubagentContextsFor(_ string) {
	_ = service.Deps.Runtime.DrainSubagentContexts()
}

// chatStream 向指定会话引擎提交流式对话（会话路由引擎用 ChatStreamFor，
// 否则回退活跃引擎）。
func (service *Service) chatStream(ctx context.Context, sessionID, input string, onChunk func(string)) (string, error) {
	if routed, ok := service.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.ChatStreamFor(sessionID, ctx, input, onChunk)
	}
	return service.Deps.Engine.ChatStream(ctx, input, onChunk)
}

// appendEngineMessage 追加消息到指定会话引擎历史。
func (service *Service) appendEngineMessage(sessionID string, msg types.Message) {
	if routed, ok := service.Deps.Engine.(contract.SessionChatEngine); ok {
		routed.AppendHistoryFor(sessionID, msg)
		return
	}
	service.Deps.Engine.AppendHistory(msg)
}

// replaceEngineHistory 会话内替换指定会话引擎历史（会话路由引擎用
// ReplaceHistoryFor，不切活跃；否则回退契约 ReplaceHistory）。
func (service *Service) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error {
	if routed, ok := service.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.ReplaceHistoryFor(sessionID, history)
	}
	return service.Deps.Engine.ReplaceHistory(sessionID, history)
}

// engineHistoryFor 返回指定会话引擎历史（只读拷贝）。
func (service *Service) engineHistoryFor(sessionID string) []contract.EngineMessage {
	if routed, ok := service.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.HistoryFor(sessionID)
	}
	return service.Deps.Engine.History()
}

// clearEngineHistoryFor 清空指定会话引擎历史（会话路由引擎用 ClearHistoryFor，
// 不切换活跃别名）。不要用 Engine.ClearHistory() 代替：进程级别名指向哪个会话
// 不可预期，可能是另一个正在运行的会话，其 framework Session 锁被 ChatStream
// 全程持有——清空会排在它后面，并在它释放后清掉它的运行历史。
func (service *Service) clearEngineHistoryFor(sessionID string) {
	if routed, ok := service.Deps.Engine.(contract.SessionChatEngine); ok {
		routed.ClearHistoryFor(sessionID)
		return
	}
	service.Deps.Engine.ClearHistory()
}

func (service *Service) Submit(ctx context.Context, text string) error {
	service.ViewMu.RLock()
	draining := service.draining
	closed := service.closed
	service.ViewMu.RUnlock()
	if closed {
		return errors.New("application is shut down")
	}
	if draining {
		return ErrApplicationDraining
	}
	input := strings.TrimSpace(text)
	if input == "" {
		return nil
	}
	return service.components.input.Dispatch(ctx, input)
}

func (service *Service) submitConversation(ctx context.Context, input string) error {
	// fork 执行期**不再拒绝输入**（2026-10-05 退役 fork 门控）。fork_subagents 派发
	// 即后台化之后，"父回合还在、子代理作业在跑"是常态，把用户挡在门外只是把
	// "并发"换成"拒绝"——输入照样要发，只是发得晚一点。
	//
	// 现在走**既有那条链路**：本会话有回合在跑 → 入队（下面的 Enqueue 分支），
	// 回合收尾整批提升为下一轮（chat.go 的 queuedChatRequests/combineChatRequests，
	// durable queue 与调序/撤回同源）；没有回合在跑 → 照常开新回合。
	//
	// 并发写主工作区的风险改由合并侧收口，而不是靠拒绝输入回避：
	// seelebridge/worktree 的收尾单写者 actor 把"可能的并行 merge"串行化，
	// 主工作区脏/真冲突时落 ErrMergeBlockedByMain 待合并（保留现场 + 提示），
	// 不再把一个子代理的产出判死。
	request := newChatRequest(input, service.promptStack.Layers())
	effort := service.effortForSession(service.currentViewSessionID())
	request.budget = reactBudgetFor(effort)
	transition := service.transitionView()
	transition.Lock()
	defer transition.Unlock()
	if err := service.materializeDraftSession(request.displayInput); err != nil {
		return err
	}
	service.ViewMu.Lock()
	if service.closed {
		service.ViewMu.Unlock()
		return errors.New("application is shut down")
	}
	if service.draining {
		service.ViewMu.Unlock()
		return ErrApplicationDraining
	}
	sessionID := service.Core.Snapshot.Session.ID
	// 恢复门（延后语义）：restoring 期间视图指针已切到目标、内容尚未装载完成。
	// 在此开新回合会让输入落在空壳上，并与随后安装的恢复基线争用同一份可见
	// 会话（基线守卫「仍为空才安装」被破坏 → 静默丢历史）。这里既不拒绝也不
	// 阻塞：把提交挂到装载完成点，装载完成后在**同一目标会话**上启动——既满足
	// 不变式，也保证「消息最终发得出去」。前端 prompt 禁用只是呈现，后端才是
	// 不变量。
	if service.isRestoringLocked(sessionID) {
		service.ViewMu.Unlock()
		service.deferSubmitUntilRestored(ctx, sessionID, input)
		return nil
	}
	runtime := service.sessionUnitLocked(sessionID)
	if runtime.ChatState().Running {
		runtime.Enqueue(session.QueuedRequest{DisplayInput: request.displayInput, Payload: request})
		// 队列投影 = 会话域给出的同一套下标空间（见 SessionUnit.QueueProjection）：
		// 展示的行序号就是后续 ReorderQueuedInput/RecallQueuedInput 作用的下标。
		displays, count := runtime.QueueProjection()
		runtime.UpdateChat(func(chat *ChatState) {
			chat.InputQueue = displays
			chat.QueuedCount = count
		}, nil)
		service.setSessionChatLockedFor(sessionID, runtime.ChatState())
		revision := service.bumpLocked()
		service.ViewMu.Unlock()
		service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		service.publishChatStateFor(sessionID)
		// durable queue 镜像（先队列后草稿）：排队项正文落盘，崩溃/重启后按
		// 「该轮是否已发布」的消费凭据恢复重发（sessionstore/queue_consume.go）。
		// 放在 ViewMu 之外：文件 IO 不占内核锁。落盘失败不回滚内存队列——
		// 权威是会话域队列，durable queue 只是崩溃兜底。
		if enqueueErr := service.components.sessions.QueueEnqueueInput(service.components.sessions.LocateSession(sessionID), sessionID, request.displayInput); enqueueErr != nil {
			runChatDebug("durable queue enqueue session=%s err=%v", sessionID, enqueueErr)
		}
		return nil
	}
	// 同会话压缩门（延后语义，判据与上面的 restoring 门同形）：一轮显式压缩可以
	// 在**没有在飞回合**的会话上运行，而它刻意不写 ChatState.Running（伪造 Running
	// 会把"有人在跑这个会话"漏进快照与停止按钮），所以这里的 busy 判据必须自己
	// 看见它——否则新回合的装配与压缩读写同一份引擎历史，后写的那份把压缩丢掉。
	// 不排队：显式压缩没有回合，队列的正常提升点（回合结束）永远不会到来。
	if service.isCompactingLocked(sessionID) {
		service.ViewMu.Unlock()
		service.deferSubmitUntilCompacted(ctx, sessionID, input)
		return nil
	}
	// 非运行分支：队列里可能遗留待发项（如重启回填的「已发送未确认」输入），
	// 与本次提交合并为同一轮，否则它们永远等不到排空点。
	merged, drained := service.drainRecoveredQueueLocked(runtime, request)
	if drained {
		request = merged
		service.setSessionChatLockedFor(sessionID, runtime.ChatState())
	}
	service.ViewMu.Unlock()
	if drained {
		service.publishChatStateFor(sessionID)
	}
	return service.startChat(ctx, request)
}

// submitConversationFor 在指定（后台）会话提交对话：目标会话运行中则投递
// 到该会话自己的队列，否则在其上下文中后台启动（不切换活跃会话）。
func (service *Service) submitConversationFor(ctx context.Context, sessionID, input string) error {
	// fork 执行期不再拒绝输入（同 submitConversation 的退役口径）：目标会话有回合
	// 在跑 → 投进该会话自己的队列，回合收尾整批提升；没有回合在跑 → 在它的上下文
	// 里后台启动。门控退役之后，teammate/fork 两条链在输入面上走的是同一条链路。
	request := newChatRequest(input, service.promptStack.Layers())
	effort := service.effortForSession(sessionID)
	request.budget = reactBudgetFor(effort)
	service.ViewMu.Lock()
	if service.closed {
		service.ViewMu.Unlock()
		return errors.New("application is shut down")
	}
	if service.draining {
		service.ViewMu.Unlock()
		return ErrApplicationDraining
	}
	// 恢复门（延后语义，按显式 sessionID）：与 submitConversation 同一条判据，
	// 后台提交路径（SubmitToSession）也经此。restoring 期间不开新回合，改为挂到
	// 装载完成点再启动。
	if service.isRestoringLocked(sessionID) {
		service.ViewMu.Unlock()
		service.deferSubmitUntilRestored(ctx, sessionID, input)
		return nil
	}
	active := service.isActiveSessionLocked(sessionID)
	runtime := service.sessionUnitLocked(sessionID)
	if runtime.ChatState().Running {
		runtime.Enqueue(session.QueuedRequest{DisplayInput: request.displayInput, Payload: request})
		// 队列投影 = 会话域给出的同一套下标空间（见 SessionUnit.QueueProjection）。
		displays, count := runtime.QueueProjection()
		runtime.UpdateChat(func(chat *ChatState) {
			chat.InputQueue = displays
			chat.QueuedCount = count
		}, nil)
		if active {
			service.setSessionChatLockedFor(sessionID, runtime.ChatState())
			revision := service.bumpLocked()
			service.ViewMu.Unlock()
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
			service.publishChatStateFor(sessionID)
			if enqueueErr := service.components.sessions.QueueEnqueueInput(service.components.sessions.LocateSession(sessionID), sessionID, request.displayInput); enqueueErr != nil {
				runChatDebug("durable queue enqueue session=%s err=%v", sessionID, enqueueErr)
			}
			return nil
		}
		service.ViewMu.Unlock()
		service.publishSessionEvent(EventSnapshotChanged, 0, "", sessionID, nil)
		service.publishChatStateFor(sessionID)
		if enqueueErr := service.components.sessions.QueueEnqueueInput(service.components.sessions.LocateSession(sessionID), sessionID, request.displayInput); enqueueErr != nil {
			runChatDebug("durable queue enqueue session=%s err=%v", sessionID, enqueueErr)
		}
		return nil
	}
	// 同会话压缩门：与 submitConversation 同一条判据（见那里的注释），后台提交
	// 路径也经此。挂起后在同一 sessionID 上重放，不改目标会话。
	if service.isCompactingLocked(sessionID) {
		service.ViewMu.Unlock()
		service.deferSubmitUntilCompacted(ctx, sessionID, input)
		return nil
	}
	// 非运行分支：与 submitConversation 同一条提升规则（队列遗留待发项并入
	// 本轮），后台提交路径（SubmitToSession）也经此。
	merged, drained := service.drainRecoveredQueueLocked(runtime, request)
	if drained {
		request = merged
		service.setSessionChatLockedFor(sessionID, runtime.ChatState())
	}
	service.ViewMu.Unlock()
	if drained {
		service.publishChatStateFor(sessionID)
	}
	return service.startChatFor(sessionID, ctx, request)
}

// BeginGracefulShutdown 停止接收新输入，同时允许活跃 chat 及其已排队输入
// 自然完成。
func (service *Service) BeginGracefulShutdown() {
	service.ViewMu.Lock()
	service.draining = true
	service.ViewMu.Unlock()
}

// WaitForIdle 等待全部已接受的 chat 工作完成。它从不取消活跃 chat；调用方
// 通过 ctx 控制放弃。
func (service *Service) WaitForIdle(ctx context.Context) error {
	for {
		service.ViewMu.RLock()
		idle := service.idle
		service.ViewMu.RUnlock()
		select {
		case <-idle:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// AnyChatRunning 报告是否存在任一会话的运行中回合（G0c 关闭语义：视图空闲
// 而后台会话仍在跑也必须在 graceful drain 中等待）。多会话并行下视图快照
// 的 Chat.Running 只反映当前会话，不是进程空闲判据。
func (service *Service) AnyChatRunning() bool {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	return service.anyChatRunningLocked()
}

// CancelAllChats 取消全部会话的运行中回合（G0c 关闭超时路径：后台会话同样
// 占用引擎与持久化通道，不能只取消视图会话）。取消后每个 runChat 走正常
// 收尾（逐会话 persist），随后 WaitForIdle 自然收敛。
func (service *Service) CancelAllChats() {
	service.ViewMu.RLock()
	cancels := make([]context.CancelFunc, 0, 4)
	for _, sid := range service.sessions.UnitIDs() {
		if unit := service.sessions.Unit(sid); unit != nil {
			if cancel := unit.CancelFunc(); cancel != nil {
				cancels = append(cancels, cancel)
			}
		}
	}
	service.ViewMu.RUnlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// CancelChat 是"停止按钮"的终止原语，语义按以下顺序成立（顺序即语义）：
//
//  1. 先停在跑的前台工具调用：取消会话回合的 ctx，引擎 ReAct 循环与正在执行的前台
//     工具（同步 bash 及其派生的整棵进程树）随之中止——工具调用不会在停止之后继续
//     跑完，也不会再按住收尾等它（见 seelebridge/tools 的进程树终止原语）。
//  2. 不动后台子进程：bash_bg / read_batch / subagent 作业用 context.WithoutCancel
//     摘掉了回合 ctx，生死只由 job_manage(op=kill) 与硬上限决定；点停止不杀它们。
//  3. 清空消息队列并全部发出去：排队输入在回合收尾整批提升为下一轮（见 runChat），
//     停止不会把队列一起丢掉——用户已经发过的消息一条都不会少发。
//
// requestID 只作参考，不作为否决条件：渲染层持有的 request_id 可能滞后一个事件
// tick（排队回合刚提升时尤其明显），而一个会话同时只有一个运行中回合，"停掉我
// 看到在跑的那个回合"与"停掉本会话当前回合"是同一件事。此前这一判断由桌面 Bridge
// 用空 id 重试兜底，属于业务语义，收在本服务内（TUI/CLI/headless 同样受益）。
func (service *Service) CancelChat(requestID string) bool {
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	sessionID := service.Core.Snapshot.Session.ID
	runtime := service.sessionUnitLocked(sessionID)
	chat := runtime.ChatState()
	if !chat.Running || runtime.CancelFunc() == nil {
		return false
	}
	if requestID != "" && requestID != chat.RequestID {
		runChatDebug("cancel request id %q is stale; cancelling current request %q of session %s", requestID, chat.RequestID, sessionID)
	}
	runtime.CancelFunc()()
	return true
}

func (service *Service) Shutdown() {
	service.ViewMu.Lock()
	if service.closed {
		service.ViewMu.Unlock()
		return
	}
	service.closed = true
	// 取消所有运行中会话的执行（会话域持有 cancel；core 不再持有全局镜像）。
	for _, sid := range service.sessions.UnitIDs() {
		if unit := service.sessions.Unit(sid); unit != nil {
			if cancel := unit.CancelFunc(); cancel != nil {
				cancel()
			}
		}
	}
	service.ViewMu.Unlock()
	service.components.sessions.StopCatalogRefresh()
	service.sessions.Close() // 会话域 actor 收尾（注册表 + V 指针）
	service.stopLifecycleConsumers()
	if service.workTablePublisher != nil {
		service.workTablePublisher.Close()
	}
	service.Approval.Shutdown()
}
