package cold

import "github.com/RedHuang-0622/seelex/middle/protocol/model"

// WorkspaceDeps supplies project-root and session-binding callbacks.
type WorkspaceDeps struct {
	WorkspaceAvailable    func() bool
	BindProjectRootIfSafe func(sessionID, rootPath string) bool
	SetWriteScope         func(workspaceID string)
	BindSession           func(sessionID, workspaceID string)
	AnyChatRunning        func() bool
	UnbindGlobalRoot      func()
	UnbindSession         func(sessionID string)
	UnbindRuntimeRoot     func(sessionID string)
}

// WorkspaceApplication owns cold-load workspace binding rules.
type WorkspaceApplication struct {
	deps WorkspaceDeps
}

// NewWorkspace constructs the cold-workspace application.
func NewWorkspace(deps WorkspaceDeps) *WorkspaceApplication {
	return &WorkspaceApplication{deps: deps}
}

// Bind restores the project root, session binding or unbind state.
func (application *WorkspaceApplication) Bind(sessionID, workspaceID string, current *model.WorkspaceInfo) {
	if application.deps.WorkspaceAvailable != nil && !application.deps.WorkspaceAvailable() {
		return
	}
	if current != nil {
		if application.deps.BindProjectRootIfSafe(sessionID, current.RootPath) {
			application.deps.SetWriteScope(current.ID)
		}
		application.deps.BindSession(sessionID, current.ID)
		return
	}
	if workspaceID != "" {
		return
	}
	if application.deps.AnyChatRunning != nil && !application.deps.AnyChatRunning() {
		application.deps.UnbindGlobalRoot()
		application.deps.SetWriteScope("")
	}
	application.deps.UnbindSession(sessionID)
	if application.deps.UnbindRuntimeRoot != nil {
		application.deps.UnbindRuntimeRoot(sessionID)
	}
}
