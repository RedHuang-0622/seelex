package assembly

import (
	"time"

	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// workspaceHost 把 Service 的视图锁、快照写面与发布面实现成 switchworkspace.ViewPort。
// 工作区投影 owner 仍是应用视图：workspace/switch 只产出 Projection，本适配器在
// ViewMu 内写入 Snapshot；锁次序保持 ViewMu → 组件锁（只单向）。
type workspaceHost struct{ service *Service }

func (host workspaceHost) LockTransition() func() {
	lock := host.service.transitionView()
	lock.Lock()
	return lock.Unlock
}

func (host workspaceHost) State() switchworkspace.ViewState {
	service := host.service
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	state := switchworkspace.ViewState{
		SessionID: service.Core.Snapshot.Session.ID,
		Draft:     service.Core.Snapshot.Session.Draft,
		Running:   service.Core.Snapshot.Chat.Running,
	}
	if workspace := service.Core.Snapshot.CurrentWorkspace; workspace != nil {
		state.CurrentWorkspaceID = workspace.ID
		state.CurrentWorkspaceRoot = workspace.RootPath
	}
	return state
}

func (host workspaceHost) ApplyDraftBinding(info model.WorkspaceInfo, projection switchworkspace.Projection) uint64 {
	service := host.service
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	service.Core.Snapshot.CurrentWorkspace = &model.WorkspaceInfo{
		ID: info.ID, Name: info.Name, RootPath: info.RootPath, GitRemote: info.GitRemote,
	}
	if service.draft != nil {
		service.draft.Workspace = &model.WorkspaceInfo{
			ID: info.ID, Name: info.Name, RootPath: info.RootPath, GitRemote: info.GitRemote,
		}
		service.draft.UpdatedAt = time.Now()
	}
	service.applyWorkspaceProjectionLocked(projection)
	return service.components.view.BumpLocked()
}

func (host workspaceHost) ApplyBinding(info model.WorkspaceInfo, projection switchworkspace.Projection, freshSessionID, notice string) uint64 {
	service := host.service
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	if freshSessionID != "" {
		service.Core.Snapshot.Session.ID = freshSessionID
		service.Core.Snapshot.Session.Name = ""
		service.Core.Snapshot.Conversation = nil
		service.Core.Snapshot.HistoryOffset = 0
		service.Core.Snapshot.TotalMessages = 0
		service.Core.Snapshot.HasMoreHistory = false
		service.Core.Snapshot.Runtime.Plan = nil
		service.Core.Snapshot.Interaction = nil
		// 会话域活跃指针跟着换（与 /new、热挂载、冷恢复同序）：只改快照里的
		// Session.ID 而不动这一格，新项目的回合会按旧指针登记到上一个项目。
		service.sessions.SetActive(freshSessionID)
		service.components.view.AppendMessageLocked("system", notice, nil)
	}
	service.Core.Snapshot.CurrentWorkspace = &model.WorkspaceInfo{
		ID: info.ID, Name: info.Name, RootPath: info.RootPath, GitRemote: info.GitRemote,
	}
	service.applyWorkspaceProjectionLocked(projection)
	return service.components.view.BumpLocked()
}

func (host workspaceHost) ApplyUnbind(sessionID string, projection switchworkspace.Projection) uint64 {
	service := host.service
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	service.Core.Snapshot.CurrentWorkspace = nil
	if service.draft != nil && service.draft.ID == sessionID {
		service.draft.Workspace = nil
		service.draft.UpdatedAt = time.Now()
	}
	service.applyWorkspaceProjectionLocked(projection)
	return service.components.view.BumpLocked()
}

func (host workspaceHost) PublishSnapshotChanged(revision uint64) {
	service := host.service
	service.publishSessionEvent(EventSnapshotChanged, revision, "", service.currentViewSessionID(), nil)
}

func (host workspaceHost) RequestCatalogRefresh() {
	host.service.components.sessions.RequestCatalogRefresh()
}

func (host workspaceHost) ClearReviewRequest() {
	host.service.components.reviewApplication.ClearReviewRequest("")
}
