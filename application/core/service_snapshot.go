package core

import (
	"context"
	"encoding/json"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
	"github.com/RedHuang-0622/seelex/middle/view/state"
	"sort"
	"strings"
	"time"
)

// Service 门面快照/订阅/投影/消息写入委托（实现位于 view_state 域包）。

// Snapshot 返回视图快照（内部自取 `Core.ViewMu` 读锁）。
//
// **不可在持有 `Core.ViewMu` 时调用**：`sync.RWMutex` 不可重入，而目录刷新 worker
// （`Coordinator.publishCatalogMirror`）会排队等同一把写锁——有写者排队时新读者一律阻塞，
// 于是"持读锁再调 Snapshot"会永久自锁（P20 现场：3 并发跑本包，第一轮即挂死）。
// 需要同临界区一致性读的调用方，请持锁后直接用 `service.Core.Snapshot` 或各 `*Locked` 读面。
func (service *Service) Snapshot() model.Snapshot {
	snapshot := service.components.view.SnapshotView()
	// 会话状态属性富化：draft / running / queued / idle。
	// SnapshotView 已释放 Core.ViewMu，这里重新取读锁补状态。
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	if snapshot.Session.Draft {
		snapshot.Session.Status = SessionStatusDraft
	} else {
		snapshot.Session.Status = service.sessionStatusLocked(snapshot.Session.ID)
	}
	if service.draft != nil {
		// 保留的草稿槽位在会话树中始终可见（切换后不再"消失"）。
		alreadyListed := false
		for _, item := range snapshot.Sessions {
			if service.draft.ID != "" && item.ID == service.draft.ID {
				alreadyListed = true
				break
			}
		}
		if !alreadyListed {
			slot := *service.draft
			snapshot.Sessions = append([]model.SessionInfo{{
				ID:        slot.ID,
				Name:      draftSessionName,
				UpdatedAt: slot.UpdatedAt,
				Status:    SessionStatusDraft,
			}}, snapshot.Sessions...)
		}
	}
	snapshot.Conversation = service.annotateConversationForEditLocked(
		snapshot.Session.ID,
		snapshot.Conversation,
		snapshot.Chat.Running,
	)
	snapshot.ConversationRevision = service.components.tasks.LatestTranscriptSeqFor(snapshot.Session.ID)
	service.projectDraftWorkspaceLocked(&snapshot)
	snapshot.Sessions = service.withLiveSessionRowsLocked(snapshot.Sessions)
	snapshot.Sessions = service.enrichDirectoryRowsLocked(snapshot.Sessions)
	return snapshot
}

// projectDraftWorkspaceLocked 把草稿的工作区绑定补进 `session_workspaces`
// 投影（调用方持有 Core.ViewMu）。
//
// 为什么必须有这一格：草稿的绑定是**视图级事实**——`workspace.bindInfo` 的草稿
// 分支刻意不写 `Store.BindSession`（绑定要等首次提交物化才落到存储，见那里的
// "G" 注释），于是 `Projection().Bindings` 里没有草稿这一行。左侧栏按
// `session_workspaces[session.id]` 分组，缺这一格就只能把刚发起的**工作区会话**
// 归进「未关联会话」，等首轮落盘（存储绑定那时才有）才跳回工作区分组——用户
// 视角就是"新建的会话一开始不在工作区里"。
//
// 视图停在草稿上时，权威绑定就是 `CurrentWorkspace`；视图移开后（草稿槽仍保留），
// 用槽位自己记的 workspace，绝不借用当前视图会话的项目。
func (service *Service) projectDraftWorkspaceLocked(snapshot *model.Snapshot) {
	if service.draft == nil || service.draft.ID == "" {
		return
	}
	workspaceID := ""
	switch {
	case snapshot.Session.Draft && snapshot.Session.ID == service.draft.ID:
		if snapshot.CurrentWorkspace != nil {
			workspaceID = snapshot.CurrentWorkspace.ID
		}
	case service.draft.Workspace != nil:
		workspaceID = service.draft.Workspace.ID
	}
	if workspaceID == "" {
		return
	}
	if snapshot.SessionWorkspaces == nil {
		snapshot.SessionWorkspaces = make(map[string]string, 1)
	}
	snapshot.SessionWorkspaces[service.draft.ID] = workspaceID
}

// withLiveSessionRowsLocked 把「内存里活着、目录里还没有」的会话补进目录行
// （调用方持有 Core.ViewMu）。
//
// 目录的事实源是存储层枚举（`SessionsOf` = 会话头），而会话只在**回合收尾**
// 落盘：首轮在跑的那整段时间里，这个会话在磁盘上还不存在。以前目录因此整段
// 缺行——视图会话靠前端补一行"当前会话"勉强可见，后台会话（定时任务新建的
// 会话）则完全看不到，直到这一轮跑完落盘才冒出来（用户口径："会话条目消失，
// 直到完成才重新出现"/"看不到定时任务的会话条目"）。
//
// 判据只取"活着的运行态"：running / queued / awaiting_approval。idle 不补——
// 空闲会话要么已经落盘（目录本来就有），要么是一次没跑完/被放弃的单元，不该
// 被当成会话树里的一行。已落盘的会话以目录行为准（不重复、不覆盖它的
// UpdatedAt/token/meta）。
func (service *Service) withLiveSessionRowsLocked(rows []model.SessionInfo) []model.SessionInfo {
	ids := service.sessions.UnitIDs()
	if len(ids) == 0 {
		return rows
	}
	listed := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		listed[row.ID] = struct{}{}
	}
	live := make([]model.SessionInfo, 0, len(ids))
	for _, sessionID := range ids {
		if sessionID == "" {
			continue
		}
		// 草稿槽自己那一行由 Snapshot 的草稿分支给出（status=draft），不在这里补。
		if service.draft != nil && service.draft.ID == sessionID {
			continue
		}
		if _, ok := listed[sessionID]; ok {
			continue
		}
		switch service.sessionStatusLocked(sessionID) {
		case SessionStatusRunning, SessionStatusQueued, SessionStatusAwaitingApproval:
		default:
			continue
		}
		row := model.SessionInfo{ID: sessionID, UpdatedAt: time.Now()}
		// 标题取标题表的原始值（不回退活跃会话名）：后台会话不得借视图会话的标题。
		if title := service.components.sessions.CatalogSessionTitle(sessionID); title.Value != "" {
			row.Name = title.Value
		}
		live = append(live, row)
	}
	if len(live) == 0 {
		return rows
	}
	// 行序确定性（快照是可比对的发布面，不能随 actor 返回顺序抖动）。
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })
	return append(live, rows...)
}

// enrichDirectoryRowsLocked 给目录行补会话级可见状态（调用方持有
// Core.ViewMu）：草稿槽位行恒为 draft；其余行按单元运行态叠加状态/
// 待批计数/resident 标记。
func (service *Service) enrichDirectoryRowsLocked(rows []model.SessionInfo) []model.SessionInfo {
	// 草稿行幂等防重：同一时刻只允许一个“新建会话草稿”出现在列表——当前
	// 视图草稿（service.draft）持有责任；历史遗留的其它 draft record（如
	// 崩溃前保存的旧草稿）隐藏但保留在磁盘，不造成列表多值。
	if service.draft != nil && service.draft.ID != "" {
		filtered := rows[:0]
		for _, row := range rows {
			if row.Status == SessionStatusDraft && row.ID != service.draft.ID {
				continue
			}
			filtered = append(filtered, row)
		}
		rows = filtered
	}
	for index := range rows {
		if service.draft != nil && service.draft.ID != "" && rows[index].ID == service.draft.ID {
			// 草稿槽位行：未发送输入期间恒为 draft（单元 ChatState 空闲，
			// 不能被运行态叠加成 idle）。
			rows[index].Status = SessionStatusDraft
			continue
		}
		sessionID := rows[index].ID
		rows[index].Status = service.sessionStatusLocked(sessionID)
		if unit := service.sessions.Unit(sessionID); unit != nil {
			rows[index].ApprovalCount = unit.PendingApprovalCount()
			rows[index].Resident = unit.Resident()
		}
	}
	return rows
}

// sessionStatusLocked 返回指定会话的可见状态（调用方持有 Core.ViewMu）。
func (service *Service) sessionStatusLocked(sessionID string) model.SessionStatus {
	if sessionID == "" {
		return SessionStatusDraft
	}
	if service.isRestoringLocked(sessionID) {
		return SessionStatusRestoring
	}
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return SessionStatusIdle
	}
	if unit.PendingApprovalCount() > 0 {
		// awaiting_approval 覆盖运行态：引擎阻塞在审批上（INV-G8 驱逐守卫
		// 的目录面；ChatState.Running 仍为 true，收尾/取消路径不变）。
		return SessionStatusAwaitingApproval
	}
	runtime := unit.ChatState()
	if runtime.Running {
		return SessionStatusRunning
	}
	if len(unit.PendingRequests()) > 0 || runtime.QueuedCount > 0 {
		return SessionStatusQueued
	}
	return SessionStatusIdle
}

// isRestoringLocked 报告目标会话是否处于后台冷加载（调用方持有
// Core.ViewMu）。
func (service *Service) isRestoringLocked(sessionID string) bool {
	if service == nil || service.restoring == nil {
		return false
	}
	_, ok := service.restoring[sessionID]
	return ok
}

// setRestoringLocked 标记目标会话进入后台冷加载（调用方持有 Core.ViewMu）。
func (service *Service) setRestoringLocked(sessionID string) {
	if service.restoring == nil {
		service.restoring = make(map[string]struct{})
	}
	service.restoring[sessionID] = struct{}{}
}

// clearRestoringLocked 移除目标会话的后台冷加载标记（调用方持有
// Core.ViewMu），并广播“restoring 已清除”信号唤醒 awaitRestore 等待方。
func (service *Service) clearRestoringLocked(sessionID string) {
	delete(service.restoring, sessionID)
	service.signalRestoreLocked()
}

// signalRestoreLocked 广播一次“restoring 集合已变化”（调用方持有
// Core.ViewMu）：关闭当前信号通道并重建，等待方在通道关闭后重读集合复判。
func (service *Service) signalRestoreLocked() {
	if service.restoreSig == nil {
		service.restoreSig = make(chan struct{})
		return
	}
	close(service.restoreSig)
	service.restoreSig = make(chan struct{})
}

// restoreSignalLocked 返回当前 restoring 变化信号（调用方持有 Core.ViewMu）。
// 惰性初始化保证直接构造 serviceState 的宿主也不会拿到 nil 通道。
func (service *Service) restoreSignalLocked() <-chan struct{} {
	if service.restoreSig == nil {
		service.restoreSig = make(chan struct{})
	}
	return service.restoreSig
}

// nextViewEpoch 推进全局视图序号时钟并返回新值（调用方持有 Core.ViewMu）。
func (service *Service) nextViewEpochLocked() uint64 {
	service.viewEpoch++
	return service.viewEpoch
}

// markSessionViewIntentLocked 给会话记一次新的视图意图并返回其值（调用方持有
// Core.ViewMu）：意图值取自全局序号时钟，比较口径是"该会话自己的上次意图"。
func (service *Service) markSessionViewIntentLocked(sessionID string) uint64 {
	if service.sessionViewIntent == nil {
		service.sessionViewIntent = make(map[string]uint64)
	}
	intent := service.nextViewEpochLocked()
	service.sessionViewIntent[sessionID] = intent
	return intent
}

// markSessionViewIntent 是 markSessionViewIntentLocked 的自取锁包装。
func (service *Service) markSessionViewIntent(sessionID string) {
	service.ViewMu.Lock()
	service.markSessionViewIntentLocked(sessionID)
	service.ViewMu.Unlock()
}

// sessionViewIntentLocked 返回会话当前的视图意图值（调用方持有 Core.ViewMu）。
// 无记录返回 0——任何一次激活取到的值都 > 0，因此不会与"最新意图"混淆。
func (service *Service) sessionViewIntentLocked(sessionID string) uint64 {
	return service.sessionViewIntent[sessionID]
}

// refreshRuntimeProjectionForSession 是会话（重）激活时的**重建**一步：按目标会话从
// 宿主读面重采一次 runtime 投影，落到它的会话槽（视图会话同时镜像 Snapshot）。
//
// 为什么必须在这里采一次（2026-10-04 现场：切走再切回来，团队看板整块消失——数据在
// 磁盘上，是"重建"路径断了）：
//   - 会话运行原件（`Snapshot.Runtime`，含 teamwork_board / async_runs / todos /
//     subagent_tree / goal_governance）只在**回合尾**（chat.go）与**工具边界**
//     （tool_hooks.go）被写进会话槽；
//   - 切换会话会把槽换掉：热挂载换视图指针（槽还是它自己的），冷加载则先
//     `sessions.Remove(sessionID)`（UnloadSession）再重建单元——新建的单元 runtime 槽
//     是**空的**；
//   - 于是"切回来第一帧"的投影里 `teamwork_board` 缺键，前端按"没有计划即整块退场"
//     隐藏看板，**要等用户再发一句话**（下一轮回合尾）才回来。
//
// 采集在 ViewMu 之外（宿主读面含文件 I/O：AsyncRunsSnapshot 会对每条作业 stat + 读日志
// 末窗），应用在锁内——与 chat.go / tool_hooks.go 同一调用约定。
func (service *Service) refreshRuntimeProjectionForSession(sessionID string) {
	if service == nil || service.Deps.Runtime == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	projection := service.collectRuntimeProjectionFor(context.Background(), sessionID)
	service.ViewMu.Lock()
	service.applyRuntimeProjectionForLocked(sessionID, projection)
	service.ViewMu.Unlock()
}

func (service *Service) collectRuntimeProjection(ctx context.Context) view_state.RuntimeStateProjection {
	return service.components.view.CollectRuntimeProjection(ctx)
}

// collectRuntimeProjectionFor 按显式会话收集运行时投影（G1：后台会话的
// 槽写入方；tasks/tokens/skills 走 per-session 端口）。
func (service *Service) collectRuntimeProjectionFor(ctx context.Context, sessionID string) view_state.RuntimeStateProjection {
	return service.components.view.CollectRuntimeProjectionFor(ctx, sessionID)
}

func (service *Service) applyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection) {
	service.components.view.ApplyRuntimeProjectionLocked(projection)
}

// applyRuntimeProjectionForLocked 应用运行时投影到指定会话槽（活跃会话由
// 视图协调器镜像 Snapshot；调用方持有 Core.ViewMu）。
func (service *Service) applyRuntimeProjectionForLocked(sessionID string, projection view_state.RuntimeStateProjection) {
	service.components.view.ApplyRuntimeProjectionForLocked(sessionID, projection)
}

func (service *Service) appendMessageLocked(role, content string, tool *model.ToolCall) *model.Message {
	return service.components.view.AppendMessageLocked(role, content, tool)
}

// appendMessageWithOriginLocked 追加一条带群聊归属的可见消息到活跃会话
// （origin 由调用方按 transcript 的 R4 归属传入，见 model.MessageOrigin）。
func (service *Service) appendMessageWithOriginLocked(role, content string, tool *model.ToolCall, origin model.MessageOrigin) *model.Message {
	return service.components.view.AppendMessageWithOriginLockedFor(service.Core.Snapshot.Session.ID, role, content, tool, origin)
}

// appendSessionMessageLocked 追加一条可见消息到指定会话（阶段 1：后台会话
// 也维护自己的可见投影；活跃会话同步镜像 Snapshot）。
func (service *Service) appendSessionMessageLocked(sessionID, role, content string, tool *model.ToolCall) *model.Message {
	return service.components.view.AppendMessageLockedFor(sessionID, role, content, tool)
}

// appendSessionMessageWithOriginLocked 追加一条带群聊归属的可见消息到指定
// 会话（后台会话同样保留归属，hot_attach 回看不会退化成 AGENT）。
func (service *Service) appendSessionMessageWithOriginLocked(sessionID, role, content string, tool *model.ToolCall, origin model.MessageOrigin) *model.Message {
	return service.components.view.AppendMessageWithOriginLockedFor(sessionID, role, content, tool, origin)
}

// appendAssistantPlaceholderAfterToolLocked 在指定会话的 tool_result 之后补一条
// 空 assistant 占位（活跃与后台同一语义；调用方持有 Core.ViewMu）。末条已是
// 空 assistant（无内容、无工具）时不重复补，避免并行工具轮产生多余空行。
func (service *Service) appendAssistantPlaceholderAfterToolLocked(sessionID string) *model.Message {
	emptyAssistantTail := false
	service.components.view.SessionViewReadLocked(sessionID, func(view *session.View) {
		if n := len(view.Conversation); n > 0 {
			last := view.Conversation[n-1]
			emptyAssistantTail = last.Role == "assistant" && last.Content == "" && last.Tool == nil
		}
	})
	if emptyAssistantTail {
		return nil
	}
	// 工具轮后的续写正文同样由 EXEC 主持：没有归属会让同一回合的后半段在聊天区
	// 从 EXEC 掉回 AGENT。
	return service.appendSessionMessageWithOriginLocked(sessionID, "assistant", "", nil, model.MessageOrigin{
		RoleName: RoleNameMain, RoleSessionID: sessionID,
		RoundID: service.components.tasks.RoleRoundFor(sessionID),
	})
}

// setSessionChatLockedFor 写指定会话的聊天运行态投影（活跃会话镜像
// Snapshot.Chat）。
func (service *Service) setSessionChatLockedFor(sessionID string, chat model.ChatState) {
	service.components.view.SetSessionChatLockedFor(sessionID, chat)
}

// mirrorActiveViewLocked 把当前活跃会话 scope 镜像到 Snapshot。
func (service *Service) mirrorActiveViewLocked() {
	service.components.view.MirrorActiveViewLocked()
}

// sessionViewLocked 返回指定会话的可见投影（core 域工具/恢复路径用；
// 调用方持有 Core.ViewMu）。
func (service *Service) sessionViewLocked(sessionID string) *session.View {
	return service.components.view.SessionViewLocked(sessionID)
}

// sessionViewEmptyLocked 报告指定会话的可见会话是否为空（调用方持有
// Core.ViewMu）。冷恢复用它判断"目标会话在我装载期间是否已经积累了更新
// 的活消息"——有则不覆盖。会话域单元不存在时视为空。
func (service *Service) sessionViewEmptyLocked(sessionID string) bool {
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return true
	}
	empty := true
	unit.View.Read(func(view *session.View) {
		empty = len(view.Conversation) == 0
	})
	return empty
}

// recordReadFileForSessionLocked 记录指定会话的 read 文件引用（阶段 1：
// ReadFiles 收进会话 view，不再写全局 Snapshot.ReadFiles）。
func (service *Service) recordReadFileForSessionLocked(sessionID, arguments string) {
	var input struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(arguments), &input) != nil {
		return
	}
	input.Path = strings.TrimSpace(input.Path)
	if input.Path == "" {
		return
	}
	now := time.Now()
	// G5 访问器化：ReadFiles 写经 View.mu（Mutate），避免 ViewMu 下裸字段
	// 穿越；镜像由调用方在 ViewMu 上下文统一执行。
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		for index := range view.ReadFiles {
			if view.ReadFiles[index].Path == input.Path {
				view.ReadFiles[index].ReadAt = now
				return
			}
		}
		view.ReadFiles = append(view.ReadFiles, model.ReadFileRef{Path: input.Path, ReadAt: now})
	})
	service.mirrorActiveViewLocked()
}

func (service *Service) bumpLocked() uint64 {
	return service.components.view.BumpLocked()
}

func (service *Service) addNotice(notice string) {
	service.components.view.AddNotice(notice)
}

// AddNotice 追加一条系统通知（以 system 消息进入可见会话并发布
// message.added 事件）。启动期配置警告等非致命错误用它呈现给前端。

func (service *Service) resetConversation(notice string) {
	service.components.view.ResetConversation(notice)
}

// advanceMessageSeqLocked 按既有消息 ID 推进**该会话**的消息派号（恢复路径委托）。
func (service *Service) advanceMessageSeqLocked(sessionID string, messages []model.Message) {
	service.components.view.AdvanceMessageSeqForLocked(sessionID, messages)
}
