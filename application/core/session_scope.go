package core

import (
	"context"
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/session"
)

// sessionUnitLocked 返回指定会话的会话单元（聊天运行态已收进 SessionUnit，
// 9.5 起 core 直接经会话单元接入；按需创建）。
// 调用方必须持有 Core.Mu；域内锁序 Core.Mu → Domain.mu → Unit.mu，不反向。
func (service *Service) sessionUnitLocked(sessionID string) *session.SessionUnit {
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		if sessionID == "" {
			unit = session.NewDraftUnit()
		} else {
			unit, _ = session.NewSessionUnit(sessionID)
		}
		service.sessions.Register(unit)
	}
	return unit
}

// anyChatRunningLocked 报告是否存在任意会话的运行中聊天。M1 单飞执行
// 闸门依赖它：切换会话/新建会话必须等所有会话空闲；同会话二次提交仍走
// 会话内队列。
func (service *Service) anyChatRunningLocked() bool {
	for _, sid := range service.sessions.UnitIDs() {
		unit := service.sessions.Unit(sid)
		if unit != nil && unit.ChatState().Running {
			return true
		}
	}
	return false
}

// mirrorActiveChatLocked 把当前活跃会话的聊天运行态写入会话 view（阶段 1：
// Snapshot.Chat 由 view 统一镜像；切换会话后调用保证前端读到活跃会话状态）。
func (service *Service) mirrorActiveChatLocked() {
	sessionID := service.Core.Snapshot.Session.ID
	if unit := service.sessions.Unit(sessionID); unit != nil {
		service.setSessionChatLockedFor(sessionID, unit.ChatState())
	} else {
		service.setSessionChatLockedFor(sessionID, ChatState{})
	}
}

// queuedChatRequests 把会话域排队输入（不透明载荷）还原为执行内核的
// chatRequest 列表（排序一致；无法还原的载荷跳过）。
func queuedChatRequests(requests []session.QueuedRequest) []chatRequest {
	out := make([]chatRequest, 0, len(requests))
	for _, request := range requests {
		if payload, ok := request.Payload.(chatRequest); ok {
			out = append(out, payload)
		}
	}
	return out
}

// activeQueuedChatRequestsLocked 返回当前会话域的排队输入（还原为执行内核
// 的 chatRequest；调用方持有 Core.Mu）。会话域是队列唯一所有者。
func (service *Service) activeQueuedChatRequestsLocked() []chatRequest {
	unit := service.sessions.Unit(service.Core.Snapshot.Session.ID)
	if unit == nil {
		return nil
	}
	return queuedChatRequests(unit.PendingRequests())
}

// publishSessionEvent 发布事件；装配的 EventHub 支持会话路由时携带
// sessionID，否则退化为普通 Publish。
func (service *Service) publishSessionEvent(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) event.Event {
	if hub, ok := service.Events.(event.SessionAwareHub); ok {
		return hub.PublishSession(kind, revision, requestID, sessionID, payload)
	}
	return service.Events.Publish(kind, revision, requestID, payload)
}

// publishChatStateFor 下发指定会话的权威聊天运行态（chat.changed）。运行/排队
// 是后端口径，客户端不得从"收到增量事件"反推自己是否在跑。
//
// 载荷刻意不带 revision（=0）：本事件是运行态的整体替换，而同一批转换往往先
// 发 snapshot.changed（触发客户端重拉并把 revision floor 抬到当前值），带
// revision 的 chat.changed 会被协议层判为"已由权威快照表示"而丢弃。
// 只读会话单元自有状态，因此必须在释放 Core.Mu 之后调用。
func (service *Service) publishChatStateFor(sessionID string) {
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return
	}
	chat := unit.ChatState()
	service.publishSessionEvent(EventChatChanged, 0, chat.RequestID, sessionID, chat)
}

// bindProjectRootIfSafe 在安全条件下重绑全局项目根（P3/G5 收口）：
// - 无任何会话运行中 → 可安全重绑（当前视图会话的工具需要正确根）；
// - 有会话运行中 → 仅当目标是当前会话时重绑——后台运行中的会话不得因视图
//   切换被改根，否则 A 的后续路径工具会解析到 B 的项目根（跨会话串写）。
// 返回是否已绑定；未绑定时调用方必须跳过全局 SetWorkspace（Router 写作用域
// 同样全局，不能为后台会话切换）。
func (service *Service) bindProjectRootIfSafe(sessionID, rootPath string) bool {
	service.Mu.RLock()
	anyRunning := service.anyChatRunningLocked()
	current := service.Core.Snapshot.Session.ID
	service.Mu.RUnlock()
	if anyRunning && sessionID != current {
		return false
	}
	if service.Deps.Runtime == nil {
		return false
	}
	if err := service.Deps.Runtime.BindProjectRoot(rootPath); err != nil {
		return false
	}
	return true
}

// SubmitToSession 是会话级提交 API（M2：多会话并行执行）。目标会话即活跃
// 会话时等价 Submit（同会话运行中排队）；目标会话为其它会话且已加载（引擎
// 已实例化）时，在该会话上下文中后台并行启动（不切换活跃会话）；未加载的
// 会话先切换恢复再提交（兼容 M1 语义）。
func (service *Service) SubmitToSession(ctx context.Context, sessionID, text string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	// 路由只认显式 sessionID，绝不重读「当前会话」：SubmitToSession 与
	// 切换（ResumeSession/hot_attach）之间存在 TOCTOU 窗口，若先读 current
	// 再委托 Submit（内部再读一次 current），切换落在两次读之间会把 A 的
	// 输入路由进 B 的队列（压力测试 TestStressConcurrentSessionsDoNotPollute
	// 抓到：queued-2 进入 sess-4 视图）。
	if !service.sessionLoaded(sessionID) {
		// 目标会话未加载：切换恢复后提交（旧 M1 语义；会话级门控允许运行中
		// 恢复空闲会话）。ActivateSession 持 TransitionLock，完成后目标即
		// 当前会话，后续显式路由不依赖 current。
		if err := service.ActivateSession(sessionID); err != nil {
			return err
		}
	}
	return service.submitConversationFor(ctx, sessionID, text)
}

// sessionLoaded 报告目标会话引擎是否已实例化（后台提交前置检查）。
func (service *Service) sessionLoaded(sessionID string) bool {
	if routed, ok := service.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.HasSession(sessionID)
	}
	return sessionID == service.Core.Snapshot.Session.ID
}

// ActivateSession 切换当前展示/执行会话。M1 没有每会话驻留快照，切换即
// 恢复（resumeSession 从持久化重建）；运行中切换被拒绝，保证共享
// Snapshot 不串写。多页签并行驻留 = M2/M3。
func (service *Service) ActivateSession(sessionID string) error {
	return service.resumeSession(sessionID)
}

// SnapshotOf 返回指定会话的权威快照：活跃会话直接返回 Snapshot()；其它
// 已驻留（LIVE）会话从该会话 Unit 的可见投影与 per-session task/plan scope
// 组装（F5：运行中会话回看的数据面，不要求先切换）。
func (service *Service) SnapshotOf(sessionID string) (Snapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Snapshot{}, errors.New("session ID is required")
	}
	service.Mu.RLock()
	current := service.Core.Snapshot.Session.ID
	service.Mu.RUnlock()
	if sessionID == current {
		return service.Snapshot(), nil
	}
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return Snapshot{}, ErrSessionSnapshotUnavailable
	}
	service.Mu.RLock()
	defer service.Mu.RUnlock()
	view := service.components.view.SessionViewLocked(sessionID)
	name := service.components.sessions.SessionTitleFor(sessionID).Value
	snapshot := Snapshot{
		ProtocolVersion:    ProtocolVersion,
		Revision:           service.Core.Snapshot.Revision,
		Session:            SessionState{ID: sessionID, Name: name},
		Conversation:       append([]Message(nil), view.Conversation...),
		Chat:               unit.ChatState(),
		Runtime:            cloneRuntimeState(service.Core.Snapshot.Runtime),
		Capabilities:       Capabilities{SessionResume: true},
		HistoryOffset:      view.HistoryOffset,
		TotalMessages:      view.TotalMessages,
		HasMoreHistory:     view.HasMoreHistory,
		ConversationWindow: view.ConversationWindow,
		ReadFiles:          append([]ReadFileRef(nil), view.ReadFiles...),
	}
	if plan := service.planProjectionLocked(sessionID); plan != nil {
		snapshot.Runtime.Plan = clonePlanForSync(plan)
	}
	if task := service.components.tasks.TaskStateFor(sessionID); task != nil {
		taskCopy := *task
		taskCopy.ContextCompactions = append([]ContextCompaction(nil), task.ContextCompactions...)
		snapshot.Task = &taskCopy
	}
	if records := service.Deps.Runtime.TaskSnapshotFor(sessionID); len(records) > 0 {
		rows := buildWorkTable(snapshot.Runtime.Plan, records, nil)
		snapshot.Runtime.WorkTable = rows
		snapshot.Runtime.WorkTableBatches = buildWorkTableBatches(rows)
	}
	return snapshot, nil
}

// sessionEventFilter 构造会话级订阅谓词（口径见 SubscribeSession）。
func (service *Service) sessionEventFilter(sessionID string) func(event.Event) bool {
	if sessionID != "" {
		return func(event Event) bool {
			return event.SessionID == "" || event.SessionID == sessionID
		}
	}
	return func(event Event) bool {
		return event.SessionID == "" || event.SessionID == service.sessions.ActiveID()
	}
}

// SubscribeSessionWithReplay 与 SubscribeSession 同一归属口径，但订阅附带
// replayWindow 条重放窗口：缓冲写满时事件不丢，落后的消费者可按 delivery_seq
// 增量补取（Subscription.ReplaySince）。装配的 hub 不支持窗口时退化为
// SubscribeSession 的旧溢出语义。
func (service *Service) SubscribeSessionWithReplay(sessionID string, buffer, replayWindow int) (Subscription, error) {
	hub, ok := service.Events.(interface {
		SubscribeWithReplay(func(event.Event) bool, int, int) Subscription
	})
	if !ok {
		return service.SubscribeSession(sessionID, buffer)
	}
	return hub.SubscribeWithReplay(service.sessionEventFilter(strings.TrimSpace(sessionID)), buffer, replayWindow), nil
}

// SubscribeSession 返回按会话过滤的事件订阅（只投递该会话或全局事件）。
//
// sessionID 为空表示「跟随当前视图会话」：草稿尚无真实 ID（首次提交才由引擎
// 生成），客户端无从预知，因此视图归属只能由本服务判定——session.Domain 是视
// 图指针的唯一持有者，草稿物化/切换的瞬间谓词即随之改变，不存在事件空洞。
// 显式 sessionID 为严格过滤（多页签回看、headless 观察其它会话）。
// 底层 Hub 不支持会话订阅时返回错误。
func (service *Service) SubscribeSession(sessionID string, buffer int) (Subscription, error) {
	hub, ok := service.Events.(interface {
		SubscribeFiltered(func(event.Event) bool, int) Subscription
	})
	if !ok {
		return Subscription{}, errors.New("event hub does not support session-scoped subscriptions")
	}
	return hub.SubscribeFiltered(service.sessionEventFilter(strings.TrimSpace(sessionID)), buffer), nil
}
