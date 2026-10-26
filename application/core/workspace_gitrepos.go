package core

import "github.com/RedHuang-0622/seelex/contract/dto"

// WorkspaceGitRepos discovers git repositories under the current workspace.
func (service *Service) WorkspaceGitRepos() (dto.GitReposResult, error) {
	return service.workspaceApplicationComponent().WorkspaceGitRepos()
}
