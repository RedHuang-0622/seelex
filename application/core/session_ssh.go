package core

// session_ssh.go keeps only the core-side adapter and Service compatibility
// entries. Session SSH rules live in application/session.

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
)

// ErrSessionSSHUnavailable preserves the error identity for old callers.
var ErrSessionSSHUnavailable = applicationsession.ErrSessionSSHUnavailable

func (service *Service) sessionSSHApplicationComponent() *applicationsession.SSHApplication {
	if service == nil {
		return nil
	}
	return service.components.sessionSSHApplication
}

func newServiceSessionSSHApplication(service *Service) *applicationsession.SSHApplication {
	return applicationsession.NewSSHApplication(applicationsession.SSHDeps{
		Sessions: func() applicationsession.SessionSSHPort {
			if service == nil || service.Deps.Sessions == nil {
				return nil
			}
			port, _ := service.Deps.Sessions.(applicationsession.SessionSSHPort)
			return port
		},
		Workspace: func() applicationsession.WorkspacePort {
			if service == nil || service.Deps.Workspace == nil {
				return nil
			}
			return service.Deps.Workspace
		},
	})
}
