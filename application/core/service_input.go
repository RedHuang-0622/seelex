package core

import (
	"context"
	"errors"
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/view_state"
	"github.com/RedHuang-0622/seelex/session"
)

// injectPendingSubagentContexts 排空 Runtime 持有的有界邮箱（活跃会话兼容
// 包装）。
func (service *Service) injectPendingSubagentContexts() {
	service.injectPendingSubagentContextsFor(service.Core.Snapshot.Session.ID)
}

// injectPendingSubagentContextsFor 排空 Runtime 持有的有界邮箱（单一来源 =
// Runtime mailbox；无本地兼容队列），并在 service.ViewMu 之外把消息注入目标
// 会话引擎。快照变更是独立短临界区，Engine 不会与 Application 形成反向
// 等待环。
func (service *Service) injectPendingSubagentContextsFor(sessionID string) {
	pending := service.Deps.Runtime.DrainSubagentContexts()
	if len(pending) == 0 {
		return
	}

	for _, content := range pending {
		value := view_state.SubagentContextMarker + content
		service.appendEngineMessage(sessionID, types.Message{Role: "user", Content: &value})
		// G4/INV-G12：mainagent 实际接收的内容（合并回父的证据）以一条可见
		// 记录进入本会话视图与 transcript（持久化事实源），不再被可见区丢弃。
		service.recordSubagentEvidence(sessionID, value)
	}
}

// recordSubagentEvidence 把子代理合并回父的一条证据记录写入目标会话：
// transcript 事件（持久化/恢复重建用）+ 可见对话（活跃会话镜像快照）。
func (service *Service) recordSubagentEvidence(sessionID, content string) {
	service.ViewMu.Lock()
	active := service.isActiveSessionLocked(sessionID)
	requestID := ""
	if unit := service.sessions.Unit(sessionID); unit != nil {
		requestID = unit.ChatState().RequestID
	}
	service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{TaskID: requestID, Role: "user", Content: content})
	if active {
		service.appendMessageLocked("user", content, nil)
	} else {
		service.appendSessionMessageLocked(sessionID, "user", content, nil)
	}
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, requestID, sessionID, nil)
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
	request := newChatRequest(input, service.promptStack.Layers())
	effort := service.effortForSession(service.currentViewSessionID())
	request.budget = reactBudgetFor(effort)
	transition := service.components.sessions.TransitionLock()
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
	runtime := service.sessionUnitLocked(sessionID)
	if runtime.ChatState().Running {
		runtime.Enqueue(session.QueuedRequest{DisplayInput: request.displayInput, Payload: request})
		pending := runtime.PendingRequests()
		runtime.UpdateChat(func(chat *ChatState) {
			chat.InputQueue = chatRequestDisplays(queuedChatRequests(pending))
			chat.QueuedCount = len(pending)
		}, nil)
		service.setSessionChatLockedFor(sessionID, runtime.ChatState())
		revision := service.bumpLocked()
		service.ViewMu.Unlock()
		service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		service.publishChatStateFor(sessionID)
		return nil
	}
	service.ViewMu.Unlock()
	return service.startChat(ctx, request)
}

// submitConversationFor 在指定（后台）会话提交对话：目标会话运行中则投递
// 到该会话自己的队列，否则在其上下文中后台启动（不切换活跃会话）。
func (service *Service) submitConversationFor(ctx context.Context, sessionID, input string) error {
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
	active := service.isActiveSessionLocked(sessionID)
	runtime := service.sessionUnitLocked(sessionID)
	if runtime.ChatState().Running {
		runtime.Enqueue(session.QueuedRequest{DisplayInput: request.displayInput, Payload: request})
		pending := runtime.PendingRequests()
		runtime.UpdateChat(func(chat *ChatState) {
			chat.InputQueue = chatRequestDisplays(queuedChatRequests(pending))
			chat.QueuedCount = len(pending)
		}, nil)
		if active {
			service.setSessionChatLockedFor(sessionID, runtime.ChatState())
			revision := service.bumpLocked()
			service.ViewMu.Unlock()
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
			service.publishChatStateFor(sessionID)
			return nil
		}
		service.ViewMu.Unlock()
		service.publishSessionEvent(EventSnapshotChanged, 0, "", sessionID, nil)
		service.publishChatStateFor(sessionID)
		return nil
	}
	service.ViewMu.Unlock()
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

// CancelChat 取消当前视图会话正在运行的回合。
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
