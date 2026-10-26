package core

// session_ssh.go keeps only the core-side adapter and Service compatibility
// entries. Session SSH rules live in application/session.

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// ErrSessionSSHUnavailable preserves the error identity for old callers.
var ErrSessionSSHUnavailable = applicationsession.ErrSessionSSHUnavailable

// SessionSSHState reads a session's SSH state.
func (service *Service) SessionSSHState(sessionID string) (model.SessionSSHInfo, error) {
	return service.sessionSSHApplicationComponent().State(sessionID)
}

// SetSessionSSHAllowed replaces the legacy allowed-environment set.
func (service *Service) SetSessionSSHAllowed(sessionID string, envs []string) (model.SessionSSHInfo, error) {
	return service.sessionSSHApplicationComponent().SetAllowed(sessionID, envs)
}

// SetSessionSSHEnv fixes the session environment on its first assignment.
func (service *Service) SetSessionSSHEnv(sessionID, projectID, env string) (model.SessionSSHInfo, error) {
	return service.sessionSSHApplicationComponent().SetEnv(sessionID, projectID, env)
}

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
