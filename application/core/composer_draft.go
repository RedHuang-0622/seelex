package core

// 未发送输入草稿（composer）的归属与持久化（G4 先行第 2 片）：
// composer 是"新建会话草稿"持有者——草稿早分配 SID 后，用户在草稿里输入但尚未
// 提交的正文随会话落盘，重启后由装配器恢复为当前视图草稿。提交（materialize）
// 成功后清空草稿，避免残留 draft 行。
//
// 落盘走两条通道：草稿正文进 sessionstore 的 lifecycle 草稿通道（会话目录下
// input/draft.json，与消息/事件通道分离——草稿编辑不写消息日志），目录行与项目
// 索引的登记仍走 record 通道（v8 下 record 通道只承担索引 + 标题/归档写穿，正文
// 会被丢弃）。目录枚举的 Status=draft 判据就是草稿文件的存在性（§2.5.4），因此
// 清空必须真删文件。
//
// 持久化是可选能力（SessionRecordPort / composerDraftPort 由生产 SessionPort
// 实现；测试桩缺省时草稿仅内存态，行为与装配前一致）。跨重启恢复覆盖两类草稿：
// 未绑定工作区的任务会话草稿（默认项目 ""）与工作区草稿（BindWorkspace 后按绑定
// 项目落盘 + 项目索引，重启后跨项目枚举找回——G 收口）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/model"
)

// composerRecordPort 是草稿 record 的可选持久化端口（生产
// internal/adapters.SessionPort 实现；测试桩缺省为内存态草稿）。
type composerRecordPort interface {
	SaveSessionRecordWorkspace(string, string, SessionRecord) error
}

// composerDraftPort 是 lifecycle 草稿通道的可选端口（生产
// internal/adapters.SessionPort 实现；测试桩缺省时草稿只活在内存与 record 通道）。
//
// 草稿与消息通道分离：未发送正文落在会话目录下的 lifecycle 草稿文件里，不进
// 消息日志（message 的 IO 密度不被输入框编辑污染）。v8 存储已退役 state/record
// 通道——record 里的 Composer 正文会被丢弃——而目录枚举的 Status=draft 判据正是
// 这份草稿文件的存在性（sessionstore §2.5.4），所以跨重启可恢复的草稿正文必须
// 走这条通道。
type composerDraftPort interface {
	SaveComposerDraftWorkspace(projectID, sessionID, content string) error
	LoadComposerDraftWorkspace(projectID, sessionID string) (string, bool, error)
}

// SaveComposerDraft 保存当前视图草稿会话的未发送输入（仅 draft 会话允许；
// 草稿正文随会话落盘并写 lifecycle 草稿通道，跨重启恢复）。提交后由
// materialize 清空。
func (service *Service) SaveComposerDraft(text string) error {
	transition := service.transitionView()
	transition.Lock()
	defer transition.Unlock()

	service.ViewMu.RLock()
	closed := service.closed
	draining := service.draining
	sessionID := service.Core.Snapshot.Session.ID
	draft := service.Core.Snapshot.Session.Draft
	service.ViewMu.RUnlock()
	if closed {
		return errors.New("application is shut down")
	}
	if draining {
		return ErrApplicationDraining
	}
	if !draft || sessionID == "" {
		return errors.New("composer draft requires an unmaterialized draft session")
	}
	service.ViewMu.Lock()
	// 第一份未发送正文落盘：把这门草稿会话登记成槽位（目录行的草稿身份判据）。
	if text != "" {
		service.registerDraftSlotLocked(sessionID)
	}
	if unit := service.sessions.Unit(sessionID); unit != nil {
		unit.SetComposerText(text, time.Now())
	}
	service.Core.Snapshot.Session.Composer = text
	service.ViewMu.Unlock()
	return service.persistComposerDraft(sessionID, text)
}

// persistComposerDraft 把草稿会话 record 落盘（Status=draft + Composer）。
// 未装配持久化端口时返回 nil（内存态草稿）。
func (service *Service) persistComposerDraft(sessionID, text string) error {
	return service.persistComposerDraftIn(service.draftWorkspaceID(sessionID), sessionID, text)
}

// draftWorkspaceID 返回当前草稿会话的绑定项目（draft 槽/视图携带；非草稿或
// 未绑定返回 ""，即默认项目）。
func (service *Service) draftWorkspaceID(sessionID string) string {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	if service.draft != nil && service.draft.ID == sessionID && service.draft.Workspace != nil {
		return service.draft.Workspace.ID
	}
	if service.Core.Snapshot.Session.ID == sessionID &&
		service.Core.Snapshot.Session.Draft &&
		service.Core.Snapshot.CurrentWorkspace != nil {
		return service.Core.Snapshot.CurrentWorkspace.ID
	}
	return ""
}

// persistComposerDraftIn 把草稿 record 写到显式项目（"" = 默认项目）。
// 工作区草稿先确保项目索引条目存在（record-only 会话可被 SessionsOf 枚举），
// 再写 state 通道；任务会话草稿保持默认项目语义。
func (service *Service) persistComposerDraftIn(projectID, sessionID, text string) error {
	store, ok := service.Deps.Sessions.(composerRecordPort)
	if !ok {
		return nil
	}
	if projectID != "" {
		if ensure, ok := service.Deps.Sessions.(interface {
			EnsureSessionIndexed(string, string) error
		}); ok {
			if err := ensure.EnsureSessionIndexed(projectID, sessionID); err != nil {
				return err
			}
		}
	}
	record := SessionRecord{
		Version: session_runtime.SessionRecordVersion,
		ID:      sessionID,
		Composer: model.ComposerDraft{
			Text:      text,
			UpdatedAt: time.Now(),
		},
		UpdatedAt: time.Now(),
	}
	if text != "" {
		record.Status = SessionStatusDraft
	}
	if err := store.SaveSessionRecordWorkspace(projectID, sessionID, record); err != nil {
		return err
	}
	// 草稿正文另走 lifecycle 草稿通道：record 通道在 v8 已退役（Composer 正文被
	// 丢弃），且目录枚举的草稿行判据就是这份草稿文件的存在性——不写这里，重启后
	// 草稿既拿不回正文，也不会被认成草稿候选（清空时同理：真删文件，不留幽灵行）。
	if drafts, ok := service.Deps.Sessions.(composerDraftPort); ok {
		return drafts.SaveComposerDraftWorkspace(projectID, sessionID, text)
	}
	return nil
}

// clearComposerDraft 在草稿物化成功后清空 composer（内存 + 落盘）。
// projectID 为物化时确定的项目（工作区草稿 = 绑定项目；任务草稿 = ""）。
//
// 落盘按**残留（权威）状态**收敛，而不是按"内存里还有没有正文"：草稿 record
// 的项目键会随"草稿态改绑工作区"漂移——前端「在该工作区新建会话」在草稿上再
// BindWorkspace 一次（BeginNewSession 幂等、BindWorkspace 改绑，见
// gui/frontend/dist/app.js bindWorkspaceAndStart），旧键下那份 Status=draft 的
// record 不会被消费。物化只清 projectID 就会把它留成幽灵草稿：重启后
// DraftCandidates 仍把它当草稿候选，restorePersistedDraft 于是把这份**已物化**
// 会话当草稿槽恢复回页面（草稿正文与已落盘消息混在一页、会话树里反而看不到
// 既定消息）。因此这里清掉所有仍以 draft 标记该会话的项目键。
func (service *Service) clearComposerDraft(sessionID, projectID string) {
	service.ViewMu.Lock()
	hadText := false
	if unit := service.sessions.Unit(sessionID); unit != nil {
		hadText = unit.ComposerText() != ""
		unit.SetComposerText("", time.Now())
	}
	service.Core.Snapshot.Session.Composer = ""
	service.ViewMu.Unlock()

	keys := service.draftResidueProjects(sessionID)
	if hadText && !containsProjectID(keys, projectID) {
		keys = append(keys, projectID)
	}
	for _, key := range keys {
		_ = service.persistComposerDraftIn(key, sessionID, "")
	}
}

// draftResidueProjects 返回磁盘上仍以 Status=draft 标记该会话的项目键
// （clearComposerDraft 的权威来源）。目录不支持分项目枚举（非
// SessionGranularPort）时返回 nil：那种端口没有项目维度，清 projectID 即清
// 该会话的 record。
func (service *Service) draftResidueProjects(sessionID string) []string {
	granular, ok := service.Deps.Sessions.(session_runtime.SessionGranularPort)
	if !ok {
		return nil
	}
	var keys []string
	for _, projectID := range service.components.sessions.AllProjectIDs() {
		for _, info := range granular.SessionsOf(projectID) {
			if info.ID == sessionID && info.Status == SessionStatusDraft {
				keys = append(keys, projectID)
				break
			}
		}
	}
	return keys
}

// containsProjectID 报告项目键集合里是否已含 key（"" = 默认项目也是合法键）。
func containsProjectID(keys []string, key string) bool {
	for _, existing := range keys {
		if existing == key {
			return true
		}
	}
	return false
}

// persistedDraftCandidate 选出最近一份持久化的草稿候选：跨项目枚举目录里的
// Status=draft 行（工作区草稿按绑定项目落盘后仍可找回），再按落点复核 record
// （枚举面可能滞后于 record；record.ID 对不上就不认这一份）。
func (service *Service) persistedDraftCandidate() (session_runtime.Location, model.SessionRecord, bool) {
	infos := service.components.sessions.DraftCandidates()
	var bestID string
	var bestUpdated time.Time
	for _, item := range infos {
		if item.ID == "" {
			continue
		}
		if bestID == "" || item.UpdatedAt.After(bestUpdated) {
			bestID = item.ID
			bestUpdated = item.UpdatedAt
		}
	}
	if bestID == "" {
		return session_runtime.Location{}, model.SessionRecord{}, false
	}
	location := service.components.sessions.LocateSession(bestID)
	record, ok, err := service.components.sessions.LoadSessionRecord(location, bestID)
	if err != nil || !ok || record.ID != bestID {
		return session_runtime.Location{}, model.SessionRecord{}, false
	}
	return location, record, true
}

// draftTextFor 读回落盘草稿正文：优先 lifecycle 草稿通道（v8 下草稿的唯一载体，
// 也是目录草稿行的判据），端口缺省或该通道无草稿时回退 record.Composer（非 v8
// 后端/旧数据）。
func (service *Service) draftTextFor(projectID, sessionID string, record model.SessionRecord) string {
	if drafts, ok := service.Deps.Sessions.(composerDraftPort); ok {
		if text, found, err := drafts.LoadComposerDraftWorkspace(projectID, sessionID); err == nil && found {
			return text
		}
	}
	return record.Composer.Text
}

// restorePersistedDraft 在装配期恢复最近一份持久化的**非空**草稿会话：草稿正文
// 回填单元与快照，工作区草稿恢复 CurrentWorkspace/draft 槽绑定，任务台账与工作
// 表格投影一并装回；未找到草稿（或正文全空白）时保持装配器生成的原样。
//
// takeOverView 报告当前视图是不是装配器生成的空壳（引擎尚未建 bundle，或引擎虽
// 已带会话但那条会话没有任何内容）——只有空壳才允许被草稿接管。引擎已带有内容
// 的历史时（宿主在装配前预置了真实会话），那是用户正在看的会话，草稿不许抢视图：
// 只登记草稿槽位（目录行恒为 draft、会话树里看得见）并把落盘正文回读进该会话
// 单元，切回这份草稿时输入框能拿回正文。目录行与视图会话可分离（多草稿并行）
// 是另一档设计，见 devlog 2026-09-22-draft-page-wiring-and-smoke.md §3.4。
//
// 返回是否把视图切到了草稿。不发布快照事件（启动期无订阅者），但工作表格走
// 与 /new 同一口径的投影刷新（表格内容在恢复后同样可见）。
func (service *Service) restorePersistedDraft(takeOverView bool) bool {
	location, record, ok := service.persistedDraftCandidate()
	if !ok {
		return false
	}
	text := service.draftTextFor(location.WorkspaceID, record.ID, record)
	if strings.TrimSpace(text) == "" {
		// 草稿候选必须带未发送正文：正文空白的候选（旧数据/record 回退失败）
		// 不当草稿恢复，也不登记槽位——否则页面上会多出一条空草稿行。
		return false
	}
	if !takeOverView {
		service.ViewMu.Lock()
		service.registerDraftSlotLocked(record.ID)
		if service.draft != nil && service.draft.ID == record.ID {
			service.draft.UpdatedAt = record.UpdatedAt
		}
		unit := service.sessionUnitLocked(record.ID)
		unit.SetComposerText(text, record.Composer.UpdatedAt)
		service.ViewMu.Unlock()
		return false
	}
	bestID := record.ID
	service.ViewMu.Lock()
	if service.draft == nil {
		service.draft = &draftSlot{ID: bestID, CreatedAt: record.UpdatedAt}
	}
	service.draft.UpdatedAt = record.UpdatedAt
	unit := service.sessionUnitLocked(bestID)
	unit.SetComposerText(text, record.Composer.UpdatedAt)
	service.sessions.SetActive(bestID)
	service.Core.Snapshot.Session = SessionState{
		ID: bestID, Name: draftSessionName, Draft: true,
		Status: SessionStatusDraft, Composer: text,
	}
	service.Core.Snapshot.CurrentWorkspace = nil
	service.Core.Snapshot.Conversation = nil
	service.Core.Snapshot.Chat = unit.ChatState()
	var restoredWorkspace *WorkspaceInfo
	if location.WorkspaceID != "" && service.Deps.Workspace != nil {
		if workspace, err := service.Deps.Workspace.Get(location.WorkspaceID); err == nil {
			workspaceCopy := workspace
			restoredWorkspace = &workspaceCopy
			service.draft.Workspace = &workspaceCopy
		}
	}
	service.Core.Snapshot.CurrentWorkspace = restoredWorkspace
	service.Core.Snapshot.Task = nil
	service.Core.Snapshot.Runtime.Plan = nil
	service.Core.Snapshot.Interaction = nil
	service.Core.Snapshot.ReadFiles = nil
	unit.SetChatState(ChatState{}, nil)
	unit.SetCancel(nil)
	unit.SetRequests(nil)
	service.components.sessions.SetSessionTitleLocked(bestID, record.Title)
	service.components.tasks.ResetForNewSessionLocked()
	service.ViewMu.Unlock()
	// 与 /new、ResumeSession 同一口径：视图会话自己的 task 台账装回运行时注册表，
	// 再重建工作表格投影——否则草稿恢复后工作表格里看不到这份会话的一系列条目
	// （task 台账随 record 落盘，见 session_cold_read.go/session_history.go 的同源
	// 处理）。
	service.Deps.Runtime.SwitchSessionTasks(bestID, record.Tasks)
	service.refreshWorkTableFromSources()
	return true
}

// shellDraftRestoreBudget 是"空壳视图补装持久草稿"等待目录收敛的上限：目录刷新
// 是后台行为，超时即放弃本次补装（草稿仍在磁盘上，不丢数据），绝不阻塞装配、
// Snapshot 或收尾。
const shellDraftRestoreBudget = 5 * time.Second

// scheduleShellDraftRestore 给"宿主在装配前已预置引擎会话"的启动形状补装持久化
// 的非空草稿（冷启动形状已在装配期同步做过，不经这里）。
//
// 为什么不在装配期同步做：会话目录的读取只由目录 worker 承担——装配/Snapshot/
// Shutdown 都不能被阻塞的目录 IO 卡住（application/core 的
// TestSnapshotDoesNotReadBlockedSessionCatalog）。宿主预置了引擎会话时装配器还
// 不知道目录里有没有非空草稿，于是等目录收敛后判断；动手前复核"视图仍停在那个
// 空壳上"，用户已经切走/开始用了就完全不碰。
func (service *Service) scheduleShellDraftRestore(shellSessionID string) {
	if shellSessionID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), shellDraftRestoreBudget)
		defer cancel()
		if err := service.WaitCatalogRefresh(ctx); err != nil {
			return
		}
		takeOver := service.claimShellView(shellSessionID)
		if !takeOver && !service.shellViewUntouched(shellSessionID) {
			return
		}
		service.restorePersistedDraft(takeOver)
	}()
}

// shellViewUntouched 报告视图是否仍停在装配器生成的那个空壳会话上、且启动窗口内
// 还没有任何用户动作（调用方不得持有 Core.ViewMu）。
func (service *Service) shellViewUntouched(shellSessionID string) bool {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	return !service.closed && !service.draining &&
		service.draft == nil && service.viewEpoch == 0 &&
		service.Core.Snapshot.Session.ID == shellSessionID &&
		!service.Core.Snapshot.Session.Draft &&
		len(service.Core.Snapshot.Conversation) == 0
}

// claimShellView 在"视图仍是装配器空壳"且"那条引擎会话没有任何用户可见内容"时允许
// 草稿接管视图：引擎已带有内容的历史意味着宿主预置的是用户正在用的会话，草稿只
// 登记槽位、不抢视图。
//
// 判据只看 user/assistant/tool：装配期预置的空会话常常已经带一条 system 提示词，
// 那不构成"用户在用的会话"。
func (service *Service) claimShellView(shellSessionID string) bool {
	if !service.shellViewUntouched(shellSessionID) {
		return false
	}
	return !service.engineSessionCarriesContent(shellSessionID)
}

// engineSessionCarriesContent 报告该引擎会话是否已有用户可见内容（system 提示词
// 不算）。
func (service *Service) engineSessionCarriesContent(sessionID string) bool {
	for _, message := range service.engineHistoryFor(sessionID) {
		switch message.Role {
		case "user", "assistant", "tool":
			return true
		}
	}
	return false
}

// registerDraftSlotLocked 在"当前视图就是一份尚未物化的草稿会话"时登记草稿
// 槽位（调用方持有 Core.ViewMu）。//
// 存在理由：目录行的草稿身份只由草稿槽位判定——Snapshot 按槽位补草稿行、草稿
// 行恒为 draft，其余行按单元运行态叠加（service_snapshot.go）。而"启动即草稿"
// 的早分配 SID 不建槽位，于是用户在这份草稿里敲了未发送正文、record 也以
// Status=draft 落盘之后，会话树里那一行仍被叠成 idle：草稿会话在页面上看不见
// （草稿正文只存在于输入框，页面 context 里没有它的位置）。首个未发送正文落盘
// 即登记槽位，恰好与 record 的 Status=draft 判据（text 非空才标 draft，见
// persistComposerDraftIn）一致；已存在的槽位（含持久化草稿恢复的）不覆盖。
func (service *Service) registerDraftSlotLocked(sessionID string) {
	if service.draft != nil || sessionID == "" {
		return
	}
	now := time.Now()
	service.draft = &draftSlot{ID: sessionID, CreatedAt: now, UpdatedAt: now}
}
