package core

import applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"

func newServiceSessionColdWorkspace(service *Service) *applicationsessioncold.WorkspaceApplication {
	return applicationsessioncold.NewWorkspace(applicationsessioncold.WorkspaceDeps{
		WorkspaceAvailable:    func() bool { return service.Deps.Workspace != nil },
		BindProjectRootIfSafe: service.bindProjectRootIfSafe,
		SetWriteScope:         service.setWorkspaceWriteScope,
		BindSession: func(sessionID, workspaceID string) {
			service.Deps.Workspace.BindSession(sessionID, workspaceID)
		},
		AnyChatRunning:   service.anyChatRunningLocked,
		UnbindGlobalRoot: service.unbindGlobalProjectRoot,
		UnbindSession: func(sessionID string) {
			service.Deps.Workspace.UnbindSession(sessionID)
		},
		UnbindRuntimeRoot: func(sessionID string) {
			if service.Deps.Runtime != nil {
				service.Deps.Runtime.UnbindProjectRootFor(sessionID)
			}
		},
	})
}
