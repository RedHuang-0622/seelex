package core

import "github.com/RedHuang-0622/seelex/middle/protocol/model"

// WorkspaceEnvironments reads a project's environment table and default name.
func (service *Service) WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	return service.workspaceApplicationComponent().WorkspaceEnvironments(projectID)
}

// SetWorkspaceEnvironment writes one project environment.
func (service *Service) SetWorkspaceEnvironment(projectID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	return service.workspaceApplicationComponent().SetWorkspaceEnvironment(projectID, name, change)
}

// DeleteWorkspaceEnvironment removes one project environment and its secret.
func (service *Service) DeleteWorkspaceEnvironment(projectID, name string) error {
	return service.workspaceApplicationComponent().DeleteWorkspaceEnvironment(projectID, name)
}

// SetDefaultWorkspaceEnvironment selects or clears a project's default
// environment.
func (service *Service) SetDefaultWorkspaceEnvironment(projectID, name string) error {
	return service.workspaceApplicationComponent().SetDefaultWorkspaceEnvironment(projectID, name)
}
