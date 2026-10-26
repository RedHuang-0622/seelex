package scheduler

import (
	"context"
	"errors"
	"strings"

	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// SessionDeps supplies session creation, workspace projection, publication and
// submission callbacks.
type SessionDeps struct {
	LifecycleState                 func() (closed, draining bool)
	ResolveWorkspace               func(workspaceID string) (*model.WorkspaceInfo, error)
	NewSessionID                   func(prefix string) string
	OpenSession                    func(sessionID string, workspace *model.WorkspaceInfo) (string, error)
	Projection                     func() switchworkspace.Projection
	LockView                       func()
	ApplyWorkspaceProjectionLocked func(switchworkspace.Projection)
	BumpLocked                     func() uint64
	UnlockView                     func()
	PublishSnapshotChanged         func(revision uint64, sessionID string)
	TouchResident                  func(sessionID string)
	RequestCatalogRefresh          func()
	AssembleRun                    func(context.Context, string, dto.ScheduledTaskSpec) (context.Context, error)
	SubmitConversation             func(context.Context, string, string) error
	DrainingError                  error
}

// SessionApplication owns the scheduled trigger's new-session top-level flow.
type SessionApplication struct {
	deps SessionDeps
}

// NewSession constructs the scheduled-session application.
func NewSession(deps SessionDeps) *SessionApplication {
	return &SessionApplication{deps: deps}
}

// StartScheduledSession creates a background session, applies run assembly and
// submits the prompt without switching the user's active view.
func (application *SessionApplication) StartScheduledSession(ctx context.Context, task dto.ScheduledTaskSpec) (string, error) {
	input := strings.TrimSpace(task.Prompt)
	if input == "" {
		return "", errors.New("提示词内容不能为空")
	}
	closed, draining := application.deps.LifecycleState()
	if closed {
		return "", errors.New("application is shut down")
	}
	if draining {
		return "", application.deps.DrainingError
	}
	workspace, err := application.deps.ResolveWorkspace(task.WorkspaceID)
	if err != nil {
		return "", err
	}
	sessionID := application.deps.NewSessionID("sched")
	newID, err := application.deps.OpenSession(sessionID, workspace)
	if err != nil {
		return "", err
	}
	projection := application.deps.Projection()
	application.deps.LockView()
	application.deps.ApplyWorkspaceProjectionLocked(projection)
	revision := application.deps.BumpLocked()
	application.deps.UnlockView()
	application.deps.PublishSnapshotChanged(revision, newID)
	application.deps.TouchResident(newID)
	application.deps.RequestCatalogRefresh()
	ctx, err = application.deps.AssembleRun(ctx, newID, task)
	if err != nil {
		return newID, err
	}
	if err := application.deps.SubmitConversation(ctx, newID, input); err != nil {
		return newID, err
	}
	return newID, nil
}
