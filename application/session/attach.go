package session

import (
	"sync"

	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// AttachPreparationPort performs all potentially blocking target-session work
// before the shared view lock is taken.
type AttachPreparationPort interface {
	EnsureContent(sessionID string) error
	TargetRunning(sessionID string) bool
	Workspace(sessionID string) (*model.WorkspaceInfo, bool)
	BindProjectRootIfSafe(sessionID, rootPath string) bool
	SetWorkspaceWriteScope(workspaceID string)
	SetRuntimeWorkspace(sessionID, workspaceID string)
	SwitchTasks(sessionID string)
	RestoreSubagentAnchors(sessionID string)
	WorkspaceProjection() (switchworkspace.Projection, bool)
	StoredPermissionTier(sessionID string) string
}

// AttachCommitPort mutates the shared snapshot while the view lock is held.
type AttachCommitPort interface {
	SessionTitle(sessionID string) string
	SetActive(sessionID string)
	Unit(sessionID string) *sessioncase.SessionUnit
	ApplyStoredPermissionTier(sessionID, tier string)
	SetSessionChat(sessionID string, chat model.ChatState)
	MirrorActiveView()
	VisibleTask(sessionID string) *model.TaskState
	ActivePlan(sessionID string) *model.PlanState
	MirrorPlan(sessionID string, plan *model.PlanState)
	MirrorApprovals(sessionID string)
	ApplyWorkspace(workspace *model.WorkspaceInfo, projection switchworkspace.Projection)
	SystemPrompt() string
	PromptRoutesBySession() bool
	BumpRevision() uint64
}

// AttachPublishPort performs the post-lock publication and residency tail.
type AttachPublishPort interface {
	WriteSessionPrompt(sessionID string)
	WriteGlobalPrompt(prompt string)
	RefreshRuntime(sessionID string)
	PublishSnapshot(sessionID string, revision uint64)
	PublishRuntimeProjections()
	RefreshCatalog()
	TouchResident(sessionID string)
	TouchContent(sessionID string)
}

// AttachDeps supplies the lock and consumer-owned ports for one hot attach.
type AttachDeps struct {
	Lock        sync.Locker
	Preparation AttachPreparationPort
	Commit      AttachCommitPort
	Publish     AttachPublishPort
}

// AttachApplication owns the current-session switch invariant used by hot
// attach, draft materialization and restore activation hosts.
type AttachApplication struct {
	deps AttachDeps
}

// NewAttach constructs the session attach use case.
func NewAttach(deps AttachDeps) *AttachApplication {
	return &AttachApplication{deps: deps}
}

// Attach points the current snapshot at one resident session. The caller does
// not hold the view lock.
func (application *AttachApplication) Attach(sessionID string, snapshot *model.Snapshot) (uint64, error) {
	if err := application.deps.Preparation.EnsureContent(sessionID); err != nil {
		return 0, err
	}
	targetRunning := application.deps.Preparation.TargetRunning(sessionID)
	if !targetRunning {
		application.deps.Publish.WriteSessionPrompt(sessionID)
	}
	workspace, hasWorkspace := application.deps.Preparation.Workspace(sessionID)
	if hasWorkspace {
		if application.deps.Preparation.BindProjectRootIfSafe(sessionID, workspace.RootPath) {
			application.deps.Preparation.SetWorkspaceWriteScope(workspace.ID)
		}
		application.deps.Preparation.SetRuntimeWorkspace(sessionID, workspace.ID)
	}
	application.deps.Preparation.SwitchTasks(sessionID)
	application.deps.Preparation.RestoreSubagentAnchors(sessionID)
	workspaceProjection, hasWorkspaceProjection := application.deps.Preparation.WorkspaceProjection()
	storedTier := application.deps.Preparation.StoredPermissionTier(sessionID)

	application.deps.Lock.Lock()
	application.commitLocked(sessionID, storedTier, workspace, hasWorkspace, workspaceProjection, hasWorkspaceProjection, snapshot)
	systemPrompt := application.deps.Commit.SystemPrompt()
	routedPrompt := application.deps.Commit.PromptRoutesBySession()
	revision := application.deps.Commit.BumpRevision()
	snapshot.Revision = revision
	application.deps.Lock.Unlock()

	if !targetRunning && !routedPrompt {
		application.deps.Publish.WriteGlobalPrompt(systemPrompt)
	}
	application.deps.Publish.RefreshRuntime(sessionID)
	application.deps.Publish.PublishSnapshot(sessionID, revision)
	application.deps.Publish.PublishRuntimeProjections()
	application.deps.Publish.RefreshCatalog()
	application.deps.Publish.TouchResident(sessionID)
	application.deps.Publish.TouchContent(sessionID)
	return revision, nil
}

func (application *AttachApplication) commitLocked(
	sessionID, storedTier string,
	workspace *model.WorkspaceInfo,
	hasWorkspace bool,
	workspaceProjection switchworkspace.Projection,
	hasWorkspaceProjection bool,
	snapshot *model.Snapshot,
) {
	name := application.deps.Commit.SessionTitle(sessionID)
	snapshot.Session = model.SessionState{ID: sessionID, Name: name}
	application.deps.Commit.SetActive(sessionID)
	unit := application.deps.Commit.Unit(sessionID)
	application.deps.Commit.ApplyStoredPermissionTier(sessionID, storedTier)
	application.deps.Commit.SetSessionChat(sessionID, unit.ChatState())
	application.deps.Commit.MirrorActiveView()
	snapshot.Task = application.deps.Commit.VisibleTask(sessionID)
	plan := application.deps.Commit.ActivePlan(sessionID)
	application.deps.Commit.MirrorPlan(sessionID, plan)
	snapshot.Runtime.Plan = plan
	snapshot.Interaction = nil
	application.deps.Commit.MirrorApprovals(sessionID)
	if hasWorkspace {
		snapshot.CurrentWorkspace = workspace
		if hasWorkspaceProjection {
			application.deps.Commit.ApplyWorkspace(workspace, workspaceProjection)
		}
	}
}
