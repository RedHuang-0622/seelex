package sessionhistory

import (
	appsession "github.com/RedHuang-0622/seelex/application/session"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// AttachHostDeps supplies host mechanisms consumed by the session attach use
// case. Blocking workspace and engine work is deliberately callback-based so
// the runtime keeps the view lock strategy in one owner.
type AttachHostDeps struct {
	LookupSessionUnit         func(sessionID string) *sessionstate.SessionUnit
	SetActive                 func(sessionID string)
	AnyChatRunningLocked      func() bool
	BindProjectRootIfSafe     func(sessionID, rootPath string) bool
	SetWorkspaceWriteScope    func(workspaceID string)
	ApplyStoredPermissionTier func(sessionID, tier string)
	VisibleTask               func(sessionID string) *model.TaskState
	ActivePlan                func(sessionID string) *model.PlanState
	MirrorPlan                func(sessionID string, plan *model.PlanState)
	MirrorApprovals           func(sessionID string)
	ApplyWorkspace            func(workspace *model.WorkspaceInfo, projection switchworkspace.Projection)
	SystemPrompt              func() string
	WriteSessionPrompt        func(sessionID string)
	WriteGlobalPrompt         func(prompt string)
	PromptRoutesBySession     bool
	PublishSnapshot           func(sessionID string, revision uint64)
	PublishRuntimeProjections func()
	RefreshCatalog            func()
}

type attachHost struct {
	runtime *Runtime
	deps    AttachHostDeps
}

func (host attachHost) EnsureContent(sessionID string) error {
	return host.runtime.components.residencyRuntime.EnsureContent(sessionID)
}

func (host attachHost) TargetRunning(sessionID string) bool {
	if host.deps.LookupSessionUnit == nil {
		return false
	}
	unit := host.deps.LookupSessionUnit(sessionID)
	return unit != nil && unit.ChatState().Running
}

func (host attachHost) Workspace(sessionID string) (*model.WorkspaceInfo, bool) {
	if host.runtime.Deps.Workspace == nil {
		return nil, false
	}
	workspace, ok := host.runtime.Deps.Workspace.SessionWorkspace(sessionID)
	if !ok {
		return nil, false
	}
	return &workspace, true
}

func (host attachHost) BindProjectRootIfSafe(sessionID, rootPath string) bool {
	if host.deps.BindProjectRootIfSafe == nil {
		return false
	}
	return host.deps.BindProjectRootIfSafe(sessionID, rootPath)
}

func (host attachHost) SetWorkspaceWriteScope(workspaceID string) {
	if host.deps.SetWorkspaceWriteScope != nil {
		host.deps.SetWorkspaceWriteScope(workspaceID)
	}
}

func (host attachHost) SetRuntimeWorkspace(sessionID, workspaceID string) {
	host.runtime.Deps.Runtime.SetSessionWorkspace(sessionID, workspaceID)
}

func (host attachHost) SwitchTasks(sessionID string) {
	host.runtime.Deps.Runtime.SwitchSessionTasks(sessionID, host.runtime.Deps.Runtime.TaskSnapshotFor(sessionID))
}

func (host attachHost) RestoreSubagentAnchors(sessionID string) {
	_ = host.runtime.Deps.Runtime.RestoreSubagentAnchors(sessionID)
}

func (host attachHost) WorkspaceProjection() (switchworkspace.Projection, bool) {
	if host.runtime.components.workspace == nil {
		return switchworkspace.Projection{}, false
	}
	return host.runtime.components.workspace.Projection(), true
}

func (host attachHost) StoredPermissionTier(sessionID string) string {
	return host.runtime.readStoredPermissionTier(sessionID)
}

func (host attachHost) SessionTitle(sessionID string) string {
	return host.runtime.components.sessions.SessionTitleFor(sessionID).Value
}

func (host attachHost) SetActive(sessionID string) {
	if host.deps.SetActive != nil {
		host.deps.SetActive(sessionID)
	}
}

func (host attachHost) Unit(sessionID string) *sessionstate.SessionUnit {
	return host.runtime.sessionUnitLocked(sessionID)
}

func (host attachHost) ApplyStoredPermissionTier(sessionID, tier string) {
	if host.deps.ApplyStoredPermissionTier != nil {
		host.deps.ApplyStoredPermissionTier(sessionID, tier)
	}
}

func (host attachHost) SetSessionChat(sessionID string, chat model.ChatState) {
	host.runtime.components.view.SetSessionChatLockedFor(sessionID, chat)
}

func (host attachHost) MirrorActiveView() {
	host.runtime.components.view.MirrorActiveViewLocked()
}

func (host attachHost) VisibleTask(sessionID string) *model.TaskState {
	if host.deps.VisibleTask == nil {
		return nil
	}
	return host.deps.VisibleTask(sessionID)
}

func (host attachHost) ActivePlan(sessionID string) *model.PlanState {
	if host.deps.ActivePlan == nil {
		return nil
	}
	return host.deps.ActivePlan(sessionID)
}

func (host attachHost) MirrorPlan(sessionID string, plan *model.PlanState) {
	if host.deps.MirrorPlan != nil {
		host.deps.MirrorPlan(sessionID, plan)
	}
}

func (host attachHost) MirrorApprovals(sessionID string) {
	if host.deps.MirrorApprovals != nil {
		host.deps.MirrorApprovals(sessionID)
	}
}

func (host attachHost) ApplyWorkspace(workspace *model.WorkspaceInfo, projection switchworkspace.Projection) {
	if host.deps.ApplyWorkspace != nil {
		host.deps.ApplyWorkspace(workspace, projection)
	}
}

func (host attachHost) SystemPrompt() string {
	if host.deps.SystemPrompt == nil {
		return ""
	}
	return host.deps.SystemPrompt()
}

func (host attachHost) PromptRoutesBySession() bool {
	return host.deps.PromptRoutesBySession
}

func (host attachHost) BumpRevision() uint64 {
	return host.runtime.components.view.BumpLocked()
}

func (host attachHost) WriteSessionPrompt(sessionID string) {
	if host.deps.WriteSessionPrompt != nil {
		host.deps.WriteSessionPrompt(sessionID)
	}
}

func (host attachHost) WriteGlobalPrompt(prompt string) {
	if host.deps.WriteGlobalPrompt != nil {
		host.deps.WriteGlobalPrompt(prompt)
	}
}

func (host attachHost) RefreshRuntime(sessionID string) {
	host.runtime.components.view.RefreshRuntimeProjectionForSession(sessionID)
}

func (host attachHost) PublishSnapshot(sessionID string, revision uint64) {
	if host.deps.PublishSnapshot != nil {
		host.deps.PublishSnapshot(sessionID, revision)
	}
}

func (host attachHost) PublishRuntimeProjections() {
	if host.deps.PublishRuntimeProjections != nil {
		host.deps.PublishRuntimeProjections()
	}
}

func (host attachHost) RefreshCatalog() {
	if host.deps.RefreshCatalog != nil {
		host.deps.RefreshCatalog()
	}
}

func (host attachHost) TouchResident(sessionID string) {
	host.runtime.components.residencyRuntime.TouchResident(sessionID)
}

func (host attachHost) TouchContent(sessionID string) {
	host.runtime.components.residencyRuntime.TouchContent(sessionID)
}

var _ appsession.AttachPreparationPort = attachHost{}
var _ appsession.AttachCommitPort = attachHost{}
var _ appsession.AttachPublishPort = attachHost{}
