package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/session"
)

// transitionView 返回视图过渡锁（G5）：影响视图指针/当前视图会话生命周期
// 的命令共用视图 key（同会话串行、不同会话并行目前只适用于不触碰视图的
// 生命周期段；视图命令保持单例过渡，见 transitionForKey 注释）。
func (service *Service) transitionView() sync.Locker {
	return service.transitionForKey("")
}

// perSessionExecution 报告宿主是否具备逐会话执行能力（生产 seelebridge
// Runtime.PerSessionExecution=true；测试桩缺省 false 走旧语义）。
func (service *Service) perSessionExecution() bool {
	if provider, ok := service.Deps.Runtime.(interface{ PerSessionExecution() bool }); ok {
		return provider.PerSessionExecution()
	}
	return false
}

// transitionForSession 返回目标会话生命周期的过渡锁：逐会话宿主按会话 key
// 串行（不同会话并行），其余宿主回退视图 key。
func (service *Service) transitionForSession(sessionID string) sync.Locker {
	if !service.perSessionExecution() {
		return service.transitionView()
	}
	return service.transitionForKey(sessionID)
}

// newGeneratedSessionID 生成显式会话 ID（逐会话宿主 fork/切项目新建用；
// 原子序号防跨会话碰撞，不依赖引擎 StartSession 活跃别名）。
func (service *Service) newGeneratedSessionID(prefix string) string {
	seq := service.sessionIDSeq.Add(1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), seq)
}

// setWorkspaceWriteScope 设置 legacy Router 写作用域；逐会话宿主跳过（存储
// 键已按会话显式解析，F-4：视图命令放开 per-session key 的前提）。
func (service *Service) setWorkspaceWriteScope(workspaceID string) {
	if service.perSessionExecution() {
		return
	}
	service.Deps.Sessions.SetWorkspace(workspaceID)
}

// bindGlobalProjectRoot 设置进程级项目根；逐会话宿主跳过（per-session
// workspace binding 由 Runtime.SetSessionWorkspace 承担）。
func (service *Service) bindGlobalProjectRoot(rootPath string) error {
	if service.perSessionExecution() {
		return nil
	}
	return service.Deps.Runtime.BindProjectRoot(rootPath)
}

// unbindGlobalProjectRoot 清空进程级项目根；逐会话宿主跳过。
func (service *Service) unbindGlobalProjectRoot() {
	if service.perSessionExecution() {
		return
	}
	service.Deps.Runtime.UnbindProjectRoot()
}

// transitionForKey 返回指定 key 的会话过渡锁（G5 per-session keyed）：会
// 话路由引擎（SessionChatEngine）下，key=会话 ID 表示该会话生命周期命令
// 串行、不同会话可并行；非路由单会话引擎一律归一到视图 key——该宿主没有
// 跨会话并行面，任何 per-session 拆分都会重新引入进程级引擎串写。
func (service *Service) transitionForKey(key string) sync.Locker {
	if _, routed := service.Deps.Engine.(contract.SessionChatEngine); !routed {
		return service.components.sessions.TransitionLock("")
	}
	return service.components.sessions.TransitionLock(key)
}

// sessionUnitLocked 返回指定会话的会话单元（聊天运行态已收进 SessionUnit，
// 9.5 起 core 直接经会话单元接入；按需创建）。
// 调用方必须持有 Core.ViewMu；域内锁序 Core.ViewMu → Domain.mu → Unit.mu，不反向。
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

// currentViewSessionID 返回当前视图会话 ID（读锁内快照；供解锁后发布
// 视图级事件时携带 sid——早分配 SID 后订阅键恒等于视图 ID，空 sid 不再
// 是"跟随视图"的隐式通配）。
func (service *Service) currentViewSessionID() string {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	return service.Core.Snapshot.Session.ID
}

// effortForSession 返回指定会话生效的 effort 级别（G4：Unit 内选择优先；
// 未选择回退进程级默认 effortManager.Current）。不持有 Core.ViewMu——Unit
// 自带锁，进程默认由 EffortManager 自身锁保护。
func (service *Service) effortForSession(sessionID string) string {
	if unit := service.sessions.Unit(sessionID); unit != nil {
		if level := unit.EffortLevel(); level != "" {
			return level
		}
	}
	return service.effortManager.Current()
}

// syncPlanPolicyFor 按会话 effort 向引擎写入该会话的 plan 策略槽（G1-C：
// plan_load/plan_run 在引擎内按执行 ctx 的会话读取自己的槽；chat 起点同步，
// 保证每个会话（含后台）都携带自己的额度，而不是继承进程默认槽）。
func (service *Service) syncPlanPolicyFor(sessionID string) {
	if service == nil || service.Deps.Runtime == nil {
		return
	}
	service.Deps.Runtime.SetPlanPolicyFor(sessionID, prompt.PlanningPolicy(service.effortForSession(sessionID)))
}

// fullAccessForSession 返回指定会话生效的全权模式（G4：Unit 内选择优先；
// 未选择回退进程默认/引擎门值）。不持有 Core.ViewMu——Unit 自带锁，进程默认
// 由 Runtime.FullAccess 门值提供。
func (service *Service) fullAccessForSession(sessionID string) bool {
	if unit := service.sessions.Unit(sessionID); unit != nil {
		if on, ok := unit.FullAccessMode(); ok {
			return on
		}
	}
	// 未选择：回退装配期捕获的进程级默认（不读引擎门当前值——那可能残留
	// 别的会话的运行开关）。
	if service == nil {
		return false
	}
	return service.fullAccessDefault
}

// syncFullAccessFor 按会话全权模式同步引擎门（G4：chat 起点调用，保证每个
// 会话都按自己的选择运行——后台/新会话不继承其它会话的遗留开关；门是进程
// 单例执行面，单飞期间只镜像目标会话；SetFullAccess 的即时生效路径除外）。
func (service *Service) syncFullAccessFor(sessionID string) {
	if service == nil || service.Deps.Runtime == nil {
		return
	}
	service.Deps.Runtime.SetFullAccess(service.fullAccessForSession(sessionID))
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
// 的 chatRequest；调用方持有 Core.ViewMu）。会话域是队列唯一所有者。
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
// 只读会话单元自有状态，因此必须在释放 Core.ViewMu 之后调用。
func (service *Service) publishChatStateFor(sessionID string) {
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return
	}
	chat := unit.ChatState()
	service.publishSessionEvent(EventChatChanged, 0, chat.RequestID, sessionID, chat)
}

// bindProjectRootIfSafe 在安全条件下重绑全局项目根（P3/G5 收口）：
//   - 无任何会话运行中 → 可安全重绑（当前视图会话的工具需要正确根）；
//   - 有会话运行中 → 仅当目标是当前会话时重绑——后台运行中的会话不得因视图
//     切换被改根，否则 A 的后续路径工具会解析到 B 的项目根（跨会话串写）。
//
// 返回是否已绑定；未绑定时调用方必须跳过全局 SetWorkspace（Router 写作用域
// 同样全局，不能为后台会话切换）。
func (service *Service) bindProjectRootIfSafe(sessionID, rootPath string) bool {
	if service.perSessionExecution() {
		// 逐会话宿主：项目根随会话 binding，无进程级全局根副作用。
		return true
	}
	service.ViewMu.RLock()
	anyRunning := service.anyChatRunningLocked()
	current := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
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

// SnapshotOf 返回指定会话的权威**会话快照**（G3 分型：SessionSnapshot，
// 传输完备且只含本会话事实——不带进程级目录/工作区/能力清单；进程制品见
// ProcessSnapshot）。活跃会话与其它已驻留（LIVE）会话走同一条组装路径：
// 会话身份/可见对话/聊天运行态/会话运行原件（本会话槽）/task/工作表格/
// 子代理树/本会话 revision（INV-G5，与进程 revision 分离）。
func (service *Service) SnapshotOf(sessionID string) (SessionSnapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionSnapshot{}, errors.New("session ID is required")
	}
	// C1 冷读面：未驻留（unit 不存在或 Resident=false）的会话走持久化基线，
	// 不再 clone 视图 Runtime 冒充其它会话（M4 撤销）。
	service.ViewMu.RLock()
	unit := service.sessions.Unit(sessionID)
	resident := service.sessionResidentLocked(unit, sessionID)
	service.ViewMu.RUnlock()
	if !resident {
		return service.snapshotOfCold(sessionID)
	}
	return service.snapshotOfResident(sessionID)
}

// sessionResidentLocked 判定目标会话引擎是否驻留（SnapshotOf 热/冷分界；
// 调用方持有 Core.ViewMu）。会话路由宿主看 Unit.Resident + 引擎
// HasSession（驱逐/归档后 false → 冷读）；legacy 单会话宿主没有 bundle
// 概念，其唯一引擎实例挂在视图会话上，视图会话即视为驻留（不触发冷读，
// 维持既有 active SnapshotOf 语义）。
func (service *Service) sessionResidentLocked(unit *session.SessionUnit, sessionID string) bool {
	if unit == nil {
		return false
	}
	if routed, ok := service.Deps.Engine.(interface{ HasSession(string) bool }); ok {
		if routed.HasSession(sessionID) {
			// 引擎 bundle 在内存 = 驻留（驱逐/归档先 UnloadSession，
			// HasSession 随即为 false → 冷读）。Unit.Resident 只用于 LRU
			// 诊断序，不作为快照热/冷的事实源。
			return true
		}
	}
	// 引擎 bundle 缺失：当前视图会话（含初始占位与未物化草稿）仍有内存快照
	// 语义，按热路径处理（空 record 的占位会话也不该误报"不可用"）；其它
	// 会话单元（驱逐后 Resident=false）显式 SnapshotOf 走持久化基线。
	return sessionID == service.Core.Snapshot.Session.ID
}

// snapshotOfResident 组装驻留会话（引擎 bundle 在内存）的会话快照：走单元
// Runtime 槽/View/协调器每会话投影。调用方无需持有 Core.ViewMu（本方法自取）。
func (service *Service) snapshotOfResident(sessionID string) (SessionSnapshot, error) {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return SessionSnapshot{}, ErrSessionSnapshotUnavailable
	}
	view := service.components.view.SessionViewLocked(sessionID)
	name := service.components.sessions.SessionTitleFor(sessionID).Value
	runtime := unit.RuntimeState()
	revision := unit.SnapshotRevision()
	if revision == 0 {
		revision = service.Core.Snapshot.Revision
	}
	snapshot := SessionSnapshot{
		ProtocolVersion:    ProtocolVersion,
		Revision:           revision,
		Session:            SessionState{ID: sessionID, Name: name},
		Conversation:       append([]Message(nil), view.Conversation...),
		Chat:               unit.ChatState(),
		Runtime:            sessionRuntimeOf(runtime),
		Capabilities:       Capabilities{SessionResume: true, SessionSnapshot: true},
		Resident:           true,
		HistoryOffset:      view.HistoryOffset,
		TotalMessages:      view.TotalMessages,
		HasMoreHistory:     view.HasMoreHistory,
		ConversationWindow: view.ConversationWindow,
		ReadFiles:          append([]ReadFileRef(nil), view.ReadFiles...),
	}
	if service.Approval != nil {
		if approvals := service.Approval.PendingBySession(sessionID); len(approvals) > 0 {
			snapshot.Approvals = append([]Interaction(nil), approvals...)
		}
	}
	if plan := service.planProjectionLocked(sessionID); plan != nil {
		snapshot.Runtime.Plan = clonePlanForSync(plan)
	}
	if task := service.components.tasks.VisibleTaskStateFor(sessionID); task != nil {
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

// sessionRuntimeOf 从全量 RuntimeState 投影提取会话专属运行原件（G3 字段
// 归属表：进程级字段 model/provider/plugin/accounts/skills/tools/scheduled
// 不进入 SessionSnapshot）。
func sessionRuntimeOf(runtime RuntimeState) SessionRuntime {
	return SessionRuntime{
		Effort:           runtime.Effort,
		FullAccess:       runtime.FullAccess,
		Tokens:           runtime.Tokens,
		Replan:           runtime.Replan,
		Plan:             cloneRuntimeState(RuntimeState{Plan: runtime.Plan}).Plan,
		TodoItems:        append([]dto.TodoItem(nil), runtime.TodoItems...),
		SubAgentTree:     append([]dto.SubAgentTreeNode(nil), runtime.SubAgentTree...),
		GoalSkillActive:  runtime.GoalSkillActive,
		ActiveSkills:     append([]string(nil), runtime.ActiveSkills...),
		WorkTable:        CloneWorkItems(runtime.WorkTable),
		WorkTableBatches: CloneWorkTableBatches(runtime.WorkTableBatches),
	}
}

// sessionEventFilter 构造会话级订阅谓词（口径见 SubscribeSession）。
func (service *Service) sessionEventFilter(sessionID string) func(event.Event) bool {
	if sessionID != "" {
		return func(event Event) bool {
			// G2/M2：进程类事件（resync.required / app.exit_requested）以空
			// sid 投递给全部订阅者；会话类事件必须 sid 精确匹配——空 sid
			// 不再作为显式会话订阅的通配（草稿占位由下方空订阅视图口径承接，
			// 待 G4 早分配 SID 后彻底移除）。
			if event.Kind == EventResyncRequired || event.Kind == EventExitRequested {
				return event.SessionID == ""
			}
			return event.SessionID == sessionID
		}
	}
	return func(event Event) bool {
		if event.Kind == EventResyncRequired || event.Kind == EventExitRequested {
			return event.SessionID == ""
		}
		// 草稿视图（空 sid）过渡口径：事件归属当前视图会话或仍为空占位。
		// G4 早分配 SID 后本分支只保留进程类判定。
		activeID := service.sessions.ActiveID()
		return event.SessionID == "" || event.SessionID == activeID
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
