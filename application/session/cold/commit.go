package cold

import (
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	task_context "github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

// CommitResult is the locked cold-load commit result.
type CommitResult struct {
	Revision     uint64
	SystemPrompt string
}

// CommitDeps supplies locked snapshot and workspace commit callbacks.
type CommitDeps struct {
	AppendPendingTail func(sessionID, notice string)
	SetSessionChat    func(sessionID string, chat model.ChatState)
	MirrorPlan        func(sessionID string, plan *model.PlanState)
	SystemPrompt      func() string
	ApplyWorkspace    func(current *model.WorkspaceInfo, projection switchworkspace.Projection)
	BumpRevision      func() uint64
}

// CommitApplication owns the locked cold-load commit.
type CommitApplication struct {
	deps CommitDeps
}

// NewCommit constructs the cold-commit application.
func NewCommit(deps CommitDeps) *CommitApplication {
	return &CommitApplication{deps: deps}
}

// CommitLocked applies pending tail, chat, plan, workspace and revision.
func (application *CommitApplication) CommitLocked(
	sessionID string,
	mayActivate bool,
	workspaceAvailable bool,
	record model.SessionRecord,
	unit *sessioncase.SessionUnit,
	currentWorkspace *model.WorkspaceInfo,
	workspaceProjection switchworkspace.Projection,
	pendingTailNotice string,
	snapshot *model.Snapshot,
) CommitResult {
	application.deps.AppendPendingTail(sessionID, pendingTailNotice)
	application.deps.SetSessionChat(sessionID, unit.ChatState())
	if mayActivate {
		application.deps.MirrorPlan(sessionID, task_context.ActivePlanFromStack(record.PlanStack, record.ActivePlanID))
		snapshot.Interaction = nil
	}
	systemPrompt := application.deps.SystemPrompt()
	if mayActivate && workspaceAvailable {
		snapshot.CurrentWorkspace = currentWorkspace
		application.deps.ApplyWorkspace(currentWorkspace, workspaceProjection)
	}
	revision := uint64(0)
	if mayActivate {
		revision = application.deps.BumpRevision()
	}
	return CommitResult{Revision: revision, SystemPrompt: systemPrompt}
}
