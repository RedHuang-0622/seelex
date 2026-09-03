package core

// 未发送输入草稿（composer）的归属与持久化（G4 先行第 2 片）：
// composer 是"新建会话草稿"持有者——草稿早分配 SID 后，用户在草稿里输入但
// 尚未提交的正文随会话 record 落盘（Status=draft），重启后由装配器恢复为
// 当前视图草稿。提交（materialize）成功后清空草稿 record，避免残留 draft 行。
//
// 持久化是可选能力（SessionRecordPort 由生产 SessionPort 实现；测试桩缺省时
// 草稿仅内存态，行为与装配前一致）。跨重启恢复覆盖两类草稿：未绑定工作区的
// 任务会话草稿（默认项目 ""）与工作区草稿（BindWorkspace 后按绑定项目写
// record + 项目索引，重启后跨项目枚举找回——G 收口）。

import (
	"errors"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/model"
)

// composerRecordPort 是草稿 record 的可选持久化端口（生产
// internal/adapters.SessionPort 实现；测试桩缺省为内存态草稿）。
type composerRecordPort interface {
	SaveSessionRecordWorkspace(string, string, SessionRecord) error
}

// SaveComposerDraft 保存当前视图草稿会话的未发送输入（仅 draft 会话允许；
// 草稿正文随会话 record 落盘，跨重启恢复）。提交后由 materialize 清空。
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
	return store.SaveSessionRecordWorkspace(projectID, sessionID, record)
}

// clearComposerDraft 在草稿物化成功后清空 composer（内存 + 落盘）。
// projectID 为物化时确定的项目（工作区草稿 = 绑定项目；任务草稿 = ""），
// 保证清理写回同一个 record 键，不跨项目留幽灵草稿。
func (service *Service) clearComposerDraft(sessionID, projectID string) {
	service.ViewMu.Lock()
	hadText := false
	if unit := service.sessions.Unit(sessionID); unit != nil {
		hadText = unit.ComposerText() != ""
		unit.SetComposerText("", time.Now())
	}
	service.Core.Snapshot.Session.Composer = ""
	service.ViewMu.Unlock()
	if hadText {
		_ = service.persistComposerDraftIn(projectID, sessionID, "")
	}
}

// restorePersistedDraft 在冷启动（引擎未建 bundle）时恢复最近一个持久化的
// 草稿会话：跨项目枚举候选（工作区草稿按绑定项目落盘后仍可找回），Composer
// 回填单元与快照，工作区草稿恢复 CurrentWorkspace/draft 槽绑定，视图指针切到
// 该草稿；未找到草稿或无 record 端口时保持装配器生成的空白草稿。不发布事件
// （启动期无订阅者）。
func (service *Service) restorePersistedDraft() {
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
		return
	}
	location := service.components.sessions.LocateSession(bestID)
	record, ok, err := service.components.sessions.LoadSessionRecord(location, bestID)
	if err != nil || !ok || record.ID != bestID {
		return
	}
	service.ViewMu.Lock()
	if service.draft == nil {
		service.draft = &draftSlot{ID: bestID, CreatedAt: record.UpdatedAt}
	}
	service.draft.UpdatedAt = record.UpdatedAt
	unit := service.sessionUnitLocked(bestID)
	unit.SetComposerText(record.Composer.Text, record.Composer.UpdatedAt)
	service.sessions.SetActive(bestID)
	service.Core.Snapshot.Session = SessionState{
		ID: bestID, Name: draftSessionName, Draft: true,
		Status: SessionStatusDraft, Composer: record.Composer.Text,
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
}
