package core

import (
	"errors"
	"fmt"
	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	"github.com/RedHuang-0622/seelex/backend/engine/context"
	"github.com/RedHuang-0622/seelex/backend/engine/react"
	"github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
)

// resumeSession 是会话切换的应用边界：目标已驻留（含运行中）热加载；目标
// 未驻留时冷加载。冷加载一律先激活目标会话的 restoring 空壳（视图立即指向
// 目标、会话树显示“恢复中”），装载——磁盘三读 / wire 装配 / 引擎恢复 / 队列
// 回填——在**视图过渡 key 之外**完成：键只保护「判定视图意图」与「按意图序号
// 发布基线」两段，一次冷加载不再把别的会话的切换与提交排在自己的磁盘读之后
// （“一个会话冷加载，另一个会话就没反应”）。
//
// 空闲（无会话运行中）走同一条链路，只是前台调用**同步**等装载收口，保持
// 「ResumeSession 返回 ⇒ 目标已装载（失败则视图回退到切换前会话）」的契约；
// 运行中则立即返回、装载在后台完成（前端 RPC 不再长期不返回，“恢复中”不等于
// 卡住）。
func (service *Service) resumeSession(sessionID string) error {
	return service.components.sessionResume.Resume(sessionID)
}

// beginAsyncRestore 激活目标会话的 restoring 空壳：视图指针立即切到目标，
// 会话树/当前快照呈现 SessionStatusRestoring，内容由后台 resumeSessionCold
// 完成后发布。调用方已持有视图过渡锁；内部自取 Core.ViewMu。
func (service *Service) beginAsyncRestore(sessionID string) (uint64, error) {
	service.ViewMu.Lock()
	if service.closed {
		service.ViewMu.Unlock()
		return 0, errors.New("application is shut down")
	}
	epoch := service.markSessionViewIntentLocked(sessionID)
	service.setRestoringLocked(sessionID)
	unit := service.sessionUnitLocked(sessionID)
	service.components.conversationTurnState.ResetLocked(unit)
	unit.SetCancel(nil)
	unit.SetRequests(nil)
	// 空壳：清空该会话单元的可见区（新建单元默认空，此处幂等），避免把
	// 上一视图会话 A 的内容留在 B 的投影里。
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		view.Conversation = nil
		view.Chat = model.ChatState{}
		view.ReadFiles = nil
		view.TotalMessages = 0
		view.HistoryOffset = 0
		view.HasMoreHistory = false
		view.ConversationWindow = 0
	})
	service.Core.Snapshot.Conversation = nil
	revision := service.components.sessionRestoringShell.PointLocked(sessionID, unit, &service.Core.Snapshot)
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	service.components.sessions.RequestCatalogRefresh()
	return epoch, nil
}

// pointViewAtRestoringShell 把视图重指到一个"装载在途"会话的空壳上（幂等，
// 自取 Core.ViewMu）。用在对同一目标的重复 ResumeSession：**不重复装载、不推进
// 该会话的视图意图**（在途装载守的就是这份意图，推了它就再也不会发布，视图会停在
// 空壳上），只把视图与快照投影重新指回目标。
//
// 与 beginAsyncRestore 的关键差别：**不清目标会话单元的可见区**——在途装载可能
// 已经把回读出来的内容写进单元了，清空会把它抹掉；这里只换视图指针与快照里的
// 上一会话投影。
//
// 返回值：true = 视图已指向目标（本次归位或本来就在）；false = 装载已收口，
// 调用方应改走正常热挂载（"返回时视图 = 目标"的契约不能靠静默返回维持）。
func (service *Service) pointViewAtRestoringShell(sessionID string) bool {
	if service == nil || sessionID == "" {
		return false
	}
	service.ViewMu.Lock()
	if service.closed {
		service.ViewMu.Unlock()
		return true // 已关闭：保持既有静默语义，不再动视图
	}
	if service.Core.Snapshot.Session.ID == sessionID {
		service.ViewMu.Unlock()
		return true
	}
	if !service.isRestoringLocked(sessionID) {
		// 装载已收口（restoring 已清）：视图归位交给正常的切换路径，这里不越权
		// 冒充一次激活——否则会把已发布的内容换成空壳。
		service.ViewMu.Unlock()
		return false
	}
	unit := service.sessionUnitLocked(sessionID)
	revision := service.components.sessionRestoringShell.PointLocked(sessionID, unit, &service.Core.Snapshot)
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	return true
}

// resumeSessionColdInBackground 后台执行冷加载：完成/失败后按 epoch 判定
// 是否仍可发布视图基线；被更新的切换取代时只完成目标会话自身装载，不抢占
// 视图。
func (service *Service) resumeSessionColdInBackground(sessionID, previousID string, epoch uint64) {
	if err := service.resumeSessionCold(sessionID, epoch); err != nil {
		service.handleColdRestoreFailure(sessionID, previousID, epoch, err)
	}
}

// handleColdRestoreFailure 后台冷加载失败的降级：用户若仍停留在失败的恢复
// 会话上，尽力回退到切换前的会话（运行中源会话通常驻留，hot_attach 不触碰
// 其引擎锁）；已被更新的切换接管时静默结束。
func (service *Service) handleColdRestoreFailure(sessionID, previousID string, epoch uint64, cause error) {
	service.components.sessionRestoreRecovery.Handle(sessionID, previousID, epoch, cause)
}

// resetViewToDraftAfterRestoreFailure 在“无前一会话可回退”时把视图重置到
// 新建草稿空壳（不做任何持久化，避免用空壳覆盖目标会话记录）。
func (service *Service) resetViewToDraftAfterRestoreFailure() {
	service.components.sessionRestoreDraftReset.Reset()
}

// resumeSessionCold 是 resumeSession 的冷加载主体：目标未驻留时重建引擎、
// 恢复 workspace 绑定与可见会话，然后发布一致快照。
//
// activateEpoch 是本次装载对应的视图意图序号（调用方在激活 restoring 空壳时
// 推进）：装载完成时若不是最新序号、或视图已不再指向目标，本次只完成目标会话
// 自身的状态装载（引擎驻留、可见投影、任务槽），不发布基线、不抢占当前视图。
func (service *Service) resumeSessionCold(sessionID string, activateEpoch uint64) error {
	loaded, err := service.components.sessionColdRead.Read(sessionID)
	if err != nil {
		return err
	}
	pendingTailNotice := loaded.PendingTailNotice
	location := loaded.Location
	record := loaded.Record
	hasRecord := loaded.HasRecord
	recordTotal := loaded.RecordTotal
	history := loaded.History
	historyTotal := loaded.HistoryTotal
	transcript := loaded.Transcript
	assembledHistory, err := service.components.sessionColdHistory.Assemble(sessionID, loaded)
	if err != nil {
		return err
	}
	transcript = assembledHistory.Transcript
	transcriptRenumbered := assembledHistory.Renumbered
	engineHistory := assembledHistory.EngineHistory
	if err := service.components.sessionColdRestore.Restore(sessionID, location, engineHistory); err != nil {
		return err
	}

	total := historyTotal // 尾部窗口读返回的真实总数（无 record 的旧格式会话）
	if hasRecord {
		total = len(service.components.sessions.RecordConversation(record))
		if recordTotal > total {
			total = recordTotal
		}
	}
	offset := total - Limits().HistoryWindow
	if offset < 0 {
		offset = 0
	}
	// 标题说明（审查 #5）：hasRecord 时标题取 record.Title（权威）；无 record
	// 的旧格式会话标题由 SessionTitleFromHistory 取窗口内首条 user 消息。
	visibleHistory := history
	currentWorkspace := location.Workspace
	service.components.sessionColdWorkspace.Bind(sessionID, location.WorkspaceID, currentWorkspace)

	workspaceProjection := service.components.workspace.Projection()
	// 需求变更（P1-1）：冷加载读回目标会话的权限档位（含磁盘读，在锁外完成；
	// 内存态落地在下面拿到会话单元之后）。
	storedTier := service.readStoredPermissionTier(sessionID)

	service.ViewMu.Lock()
	activation := service.components.sessionColdActivation.PrepareLocked(
		sessionID,
		activateEpoch,
		&service.Core.Snapshot,
		history,
		record,
		hasRecord,
		storedTier,
	)
	mayActivate := activation.MayActivate
	resumedRuntime := activation.Unit
	service.components.sessionColdTask.RestoreLocked(
		sessionID, location, record, transcript, transcriptRenumbered, hasRecord,
	)
	service.components.sessionColdView.InstallLocked(
		sessionID,
		location,
		record,
		hasRecord,
		visibleHistory,
		historyTotal,
		total,
		offset,
		Limits().HistoryWindow,
		mayActivate,
		&service.Core.Snapshot,
	)
	committed := service.components.sessionColdCommit.CommitLocked(
		sessionID,
		mayActivate,
		service.Deps.Workspace != nil,
		record,
		resumedRuntime,
		currentWorkspace,
		workspaceProjection,
		pendingTailNotice,
		&service.Core.Snapshot,
	)
	service.ViewMu.Unlock()
	return service.components.sessionColdPublish.Publish(
		sessionID, location, record, mayActivate, committed.Revision, committed.SystemPrompt,
	)
}

// attachSessionContextFor 把目标会话的 context 模块（system prompt + 四栈）挂到
// Runtime 上，供**下一轮 prompt 组装前**就绪。损坏的 context 显式失败，不静默降级成
// 内存栈。
//
// 为什么必须有这一条共用挂接：生产上有三条路径要让某个会话成为**当前会话**——
// 冷恢复/热挂载（resumeSession）、新建会话（materializeDraftSession，草稿物化）、
// 切项目时另起的独立会话（SwitchSessionWorkspace 的 startFreshSession）——它们此前
// 各自为政：只有 resume 挂接，另两条**只解绑不挂接**。
//
// 未绑定的代价（2026-10-04 现场）：Runtime 的会话上下文存储是会话级四栈与装配层推帧
// 的共同依赖。未绑定状态下 seelebridge 的 PushCompactionFrame /
// ReadbackCompactionSummary 直接报"会话上下文存储未绑定（压缩栈不可用）"——装配层
// 压缩推不了帧、拿不到可回读的 segment_id，帧正文只能写一句"没有模型生成的读后感"；
// stackBlocks（plan/task/skill/compact）与 relatedMemoryBlocks 也一律为空。而 resume
// 之后一切自愈，于是症状只在"新开的会话"里出现（现场两帧所属的会话键都是新建会话的
// 早分配 SID）。
//
// 全新会话键上挂接是安全的：store 的 Load 是惰性的，各通道 not-found 都按 S19 口径
// 静默初始化成空记录，既不继承上一个会话的四栈，也不多写任何东西——写只发生在
// Persist。挂接落点是**当前活跃 bundle**（会话槽化后按会话隔离），因此不会串台。
func (service *Service) attachSessionContextFor(workspaceID, sessionID string) error {
	store, ok := service.Deps.Sessions.(session_runtime.SessionContextPort)
	if !ok {
		return nil
	}
	if err := store.AttachSessionContext(workspaceID, sessionID); err != nil {
		return fmt.Errorf("attach session context %q: %w", sessionID, err)
	}
	return nil
}

// ResumeSession 是 GUI/TUI 会话选择的直接应用边界。它刻意绕过命令文本解析，
// 让一次点击有同步结果：恢复的快照或返回的错误。
func (service *Service) ResumeSession(sessionID string) error {
	return service.resumeSession(sessionID)
}

// LoadMoreHistory 把更早的一页历史前置到可见会话（GUI 顶部 sentinel 与
// 「加载更早」按钮的应用边界）。
//
// 分页契约（2026-09-11 修复）：
//   - 一页 = 一整窗（limits.history_window；入参只当上限建议）：半页会把
//     「窗口」和「页」两个尺寸混在一起——翻一次只多出半屏又丢掉半屏；
//   - 分页态写进**会话可见投影**（唯一事实源）后再镜像 Snapshot：只写
//     Snapshot 会在下一次镜像（新消息/工具事件/切换）被整体抹掉，offset
//     退回尾部，前端表现为「点了加载更早，内容回卷，再点还是同一页」；
//   - 窗口 = 从新 HistoryOffset 起的连续一段（上限 window）：窗口整体后退
//     一页，而不是把可见列表无限加长（WebView 渲染内存有硬上限）；
//   - 冷读面只到发布点：滑出窗口的本轮在飞行行要等这次落盘（回合收尾
//     PersistCurrentSession）才读得回来。窗口因此不贴尾，安装点保持
//     TotalMessages 如实前推，前端据「total − (offset+窗口条数)」提示
//     「下方还有新内容」，而不是把内容抹平；
//   - 冷读下标是**未过滤**空间（内部标记行也占位），可见下标与它之间隔着那些
//     行：一页读回来先按**行身份**找接缝（窗口首行在页里的位置），只把接缝
//     之前的那段可见行前置进窗口；接缝对不上就不假装连续（见 installVisibleHistory）。
//
// LoadLatestHistory 把可见会话拉回最新一页（历史浏览后的「回到最新」）。
// 分页只移动窗口、不动数据：回到最新 = 重新读尾部窗口并贴尾；回看期间
// 错过的新消息由这次基线一并带回。
//
// 热读短路：内存窗口已经贴着有效尾（窗口右界 = 已可见总数）时，「回到最新」
// 没有任何可加载的东西——冷读面的右界是发布点，换上来的必然是更旧的一页，
// 把本轮尚未落盘的尾部整窗删掉。此时直接返回，内存即答案。
// visibleTailServedFromMemory 报告内存窗口是否已经把有效尾部整窗带到（非空且
// 贴尾）。贴尾 = 用户已经在最新处；窗口为空（草稿/刚被卸载）时按「未带到」
// 处理，必须走冷读。
func (service *Service) visibleTailServedFromMemory(sessionID string, assembler applicationhistory.WindowAssembler) bool {
	atTail := false
	service.components.view.SessionViewReadLocked(sessionID, func(view *session.View) {
		durable := len(assembler.DurableRows(view.Conversation))
		atTail = durable > 0 && view.HistoryOffset+durable >= view.TotalMessages
	})
	return atTail
}

// loadConversationTailPage 读发布点处的尾部窗口：先探总数（未过滤空间，只用来
// 定位发布点），再从发布点向左读一整段。尾部整段被内部标记行占掉时（回合收尾刚
// 落的 checkpoint），一窗未过滤行里可能一条可见行都读不出来——「回到最新」会
// 拿到空页，所以这里按几何扩读向左补齐，直到可见行攒够一窗或读到序列开头。
//
// 探测与窗口读是两次独立加锁操作（与 session_runtime.LoadHistoryTailWindow 同一
// 观察项）：两读之间会话并发增长时窗口会短一两行，下一次加载自我纠正。
func (service *Service) loadConversationTailPage(workspaceID, sessionID string, window int) (applicationhistory.Page, error) {
	// limit=1 只为拿总数（区间读把 total 一并带回）。
	probe, err := service.loadConversationPage(workspaceID, sessionID, 0, 1)
	if err != nil {
		return applicationhistory.Page{}, err
	}
	diskTotal := probe.DiskTotal
	if diskTotal <= 0 {
		return applicationhistory.Page{DiskTotal: diskTotal}, nil
	}
	span := window
	if span <= 0 {
		span = 1
	}
	for {
		start := diskTotal - span
		if start < 0 {
			start = 0
		}
		page, err := service.loadConversationPage(workspaceID, sessionID, start, diskTotal-start)
		if err != nil {
			return applicationhistory.Page{}, err
		}
		if len(page.Rows) >= window || start == 0 {
			return page, nil
		}
		span *= 2
	}
}

// loadEarlierVisiblePage 读可见窗口之前的一页历史（「加载更早」的冷读面）。
//
// 冷读面的下标是**未过滤**空间，可见下标不能直接当冷读下标用：按可见下标读回来
// 的一页可能整段落在窗口之前，与窗口之间留一道谁也不显示的洞。这里从「可见下标
// 减一页」起读、按几何向右扩读，直到页里出现窗口首行的对位（接缝找到）——此时
// 页尾正好落在窗口之前，页里的可见行就是紧挨着窗口的那一页。
//
// 扩到「已发布的行全读回来」仍没有对位（窗口首行是在飞行，磁盘上根本没有），或
// 窗口为空（没有对位锚）时，返回读到的整段，交给安装路径按「宁可窗口短一页，也
// 不在列表中间留一段谁都没有的下标区间」处理。
func (service *Service) loadEarlierVisiblePage(
	workspaceID, sessionID string,
	offset, limit, window int,
	seam model.Message,
	assembler applicationhistory.WindowAssembler,
) (applicationhistory.Page, error) {
	start := offset - limit
	if start < 0 {
		start = 0
	}
	span := limit + window
	if span <= 0 {
		span = 1
	}
	for {
		page, err := service.loadConversationPage(workspaceID, sessionID, start, span)
		if err != nil {
			return applicationhistory.Page{}, err
		}
		if assembler.ContainsSameRow(page.Rows, seam) || (start == 0 && page.ReachesPublishedTail()) {
			return page, nil
		}
		if !assembler.HasRowIdentity(seam) {
			// 没有对位锚（窗口为空）：扩读没有意义。
			return page, nil
		}
		span *= 2
	}
}

// applicationhistory.Page 是一段冷读结果，**两个坐标空间分开记**：
//
//   - 冷读面（store 由 message 行直接展开 conversation）把内部标记行
//     （上下文 checkpoint、运行期提示……）也当成普通会话行，行数与下标都是
//     **未过滤**空间的；
//   - 可见会话（view.Conversation / view.HistoryOffset / view.TotalMessages）
//     一律是**可见**空间：内部标记行不占位。
//
// 两个空间不能相减相加——「窗口永远差几格够不到尾」「回到最新看不到最新」那组
// 缺陷的根就是拿未过滤总数当可见总数。磁盘的未过滤总数因此只回答一个问题：
// 这一页读到发布点了吗。
// currentWorkspaceIDLocked 返回当前视图会话的 workspace ID（调用方持有
// Core.ViewMu）。
func currentWorkspaceIDLocked(service *Service) string {
	if service.Core.Snapshot.CurrentWorkspace == nil {
		return ""
	}
	return service.Core.Snapshot.CurrentWorkspace.ID
}

// loadConversationPage 读回一段历史：record conversation 模块优先（长会话翻页不
// 反序列化整份 state），旧格式会话回退 provider 历史区间。返回的 rows 已过滤到
// 可见空间，rawRows / diskTotal 则是**未过滤**空间的行数（读回面把内部标记行也
// 当普通 conversation 行，见 applicationhistory.Page）。
func (service *Service) loadConversationPage(workspaceID, sessionID string, offset, limit int) (applicationhistory.Page, error) {
	page := applicationhistory.Page{Start: offset}
	if limit <= 0 {
		return page, nil
	}
	if store, ok := service.Deps.Sessions.(session_runtime.SessionConversationRangePort); ok {
		messages, count, err := store.LoadConversationRangeWorkspace(workspaceID, sessionID, offset, limit)
		if err != nil {
			return applicationhistory.Page{}, fmt.Errorf("load conversation range: %w", err)
		}
		page.Rows = service.components.sessions.RecordConversation(model.SessionRecord{Conversation: model.ConversationRecord{Messages: messages}})
		page.RawRows = len(messages)
		page.DiskTotal = count
		return page, nil
	}
	history, count, err := service.components.sessions.LoadSessionHistoryRange(workspaceID, sessionID, offset, limit)
	if err != nil {
		return applicationhistory.Page{}, fmt.Errorf("load history range: %w", err)
	}
	page.Rows = make([]model.Message, 0, len(history))
	for _, msg := range history {
		if !isVisibleHistoryMessage(msg) {
			continue
		}
		page.Rows = append(page.Rows, adaptEngineMessage(msg))
	}
	page.RawRows = len(history)
	page.DiskTotal = count
	return page, nil
}

// installVisibleHistory 安装一页可见历史：写会话可见投影（事实源）→ 收敛
// 窗口 → 镜像 Snapshot → bump 并发布快照变更。分页态因此随会话走，后续任何
// 镜像（新消息/工具事件/切换）都不会把它抹掉。
//
// 坐标口径（2026-09-29 修复）：view.Conversation / view.HistoryOffset /
// view.TotalMessages 一律是**可见**空间；冷读面回的行数与下标是**未过滤**空间
// （内部标记行也占位）。两者不能相减相加，因此这里只认三条事实：
//   - 可见总数以会话可见投影为准（冷加载基线 + 追加路径共同维护），读数不倒退
//     ——它是前端「下方还有 N 条新内容」的唯一依据，倒退既谎报到底，又让一个
//     并不贴尾的窗口被 append 路径判成贴尾，下一条消息就插进列表中间（断层）。
//     磁盘的未过滤总数只回答「这一页读到发布点了吗」，不参与总数；
//   - 窗口位置由**行身份**（接缝）推出：前置页接在窗口首行之前，尾页把窗口里
//     发布点之后的热尾（本轮尚未落盘的行）接回页尾。冷读一页短于请求时按请求量
//     和估计起点算出来的位置会与内容各说各话（旧实现的「窗口起点与内容脱节」）；
//   - 只拼得上的才拼：接缝对不上（窗口首行不在磁盘上——整窗都是在飞行）时不
//     假装连续，宁可窗口原地不动，也不在列表中间留一段谁都没有的下标区间；
//     一页可见行都没读到时保持现有窗口。
func (service *Service) installVisibleHistory(
	sessionID string,
	page applicationhistory.Page,
	window int,
	mode applicationhistory.InstallMode,
	assembler applicationhistory.WindowAssembler,
) error {
	if window <= 0 {
		window = 1
	}
	service.ViewMu.Lock()
	for index := range page.Rows {
		if page.Rows[index].ID == "" {
			page.Rows[index].ID = fmt.Sprintf("message-%d", service.components.view.NextMessageSeqForLocked(sessionID))
		}
	}
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		memoryRows := assembler.DurableRows(view.Conversation)
		memoryStart := view.HistoryOffset
		var rows []model.Message
		start := memoryStart
		switch mode {
		case applicationhistory.InstallReplace:
			rows, start = assembler.ReplaceTail(page.Rows, memoryRows, memoryStart, view.TotalMessages, window)
		case applicationhistory.InstallPrepend:
			rows, start = assembler.Prepend(page.Rows, memoryRows, memoryStart, window)
		}
		if len(rows) == 0 {
			// 一页可见行都没读到（尾段整段是内部标记行、或会话还没有可见正文）：
			// 保持现有窗口与总数，只如实校正「还有更早历史」。
			view.HasMoreHistory = memoryStart > 0
			return
		}
		view.Conversation = rows
		view.HistoryOffset = start
		view.HasMoreHistory = start > 0
		view.ConversationWindow = window
		// 本路径安装了可见正文（分页/冷回读）：「内容未加载」标志随之清除。
		view.ContentUnloaded = false
		// 窗口不得越过可见总数：可见总数只增不减（镜像/追加路径同一口径）。
		if tail := start + len(assembler.DurableRows(view.Conversation)); tail > view.TotalMessages {
			view.TotalMessages = tail
		}
	})
	service.mirrorActiveViewLocked()
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	// 内容 LRU：分页/回读都是一次正文使用（移动到使用序最前并收敛超限）。
	service.touchContent(sessionID)
	return nil
}

// visibleWindowFirstRow 返回目标会话可见窗口的首条 durable 行——前置一页要接在它
// 前面，所以它是「更早一页」的接缝对位锚（窗口为空时返回零值行：没有对位锚）。
func (service *Service) visibleWindowFirstRow(sessionID string, assembler applicationhistory.WindowAssembler) model.Message {
	var first model.Message
	service.components.view.SessionViewReadLocked(sessionID, func(view *session.View) {
		rows := assembler.DurableRows(view.Conversation)
		if len(rows) > 0 {
			first = rows[0]
		}
	})
	return first
}

func adaptEngineMessage(msg contract.EngineMessage) model.Message {
	content := msg.Content
	if msg.Role == "user" {
		content = displayUserInput(content)
	} else if msg.Role == "assistant" || msg.Role == "tool" {
		content = chat.StripThoughtBlocks(content)
	}
	if context_runtime.IsProviderOnlyHistoryContent(content) {
		content = ""
	}
	message := model.Message{Role: msg.Role, Content: content, ReasoningContent: msg.ReasoningContent}
	for _, toolCall := range msg.ToolCalls {
		message.Tool = &model.ToolCall{
			ID: toolCall.ID, Name: toolCall.Name, Arguments: toolCall.Arguments, Status: dto.ToolEventSuccess,
		}
	}
	return message
}

func isVisibleHistoryMessage(message contract.EngineMessage) bool {
	if message.Role == "system" {
		return false
	}
	if message.Role != "user" {
		return true
	}
	return displayUserInput(message.Content) != ""
}
