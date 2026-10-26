package core

import (
	"errors"

	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

// releaseSessionAsync tells the execution domain that a session's background
// commands must be terminated.
func (service *Service) releaseSessionAsync(sessionID string) {
	if service == nil || service.Deps.Runtime == nil || sessionID == "" {
		return
	}
	service.Deps.Runtime.ReleaseSessionAsync(sessionID)
}

// DeleteSession deletes a session record and runs the full release path.
func (service *Service) DeleteSession(sessionID string) error {
	return service.components.sessionDelete.Delete(sessionID)
}

// CreateWorkspace creates a workspace and binds it to the current view.
func (service *Service) CreateWorkspace(name, rootPath, gitRemote string) error {
	return service.components.workspace.CreateWorkspace(name, rootPath, gitRemote)
}

// CreateWorkspaces registers projects without binding the current session.
func (service *Service) CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
	return service.workspaceApplicationComponent().CreateWorkspaces(requests)
}

// BindWorkspace binds a workspace to the current view session or draft.
func (service *Service) BindWorkspace(workspaceID string) error {
	return service.components.workspace.BindWorkspace(workspaceID)
}

// UnbindWorkspace removes the current view session's workspace binding.
func (service *Service) UnbindWorkspace() {
	service.components.workspace.UnbindWorkspace()
}

// WorkspaceTree lists children of one directory under the current workspace.
func (service *Service) WorkspaceTree(relPath string, depth int) (dto.TreeListing, error) {
	return service.components.workspace.Tree(relPath, depth)
}

// WorkspaceFileCount counts visible files and directories in the current
// workspace.
func (service *Service) WorkspaceFileCount() (dto.TreeCount, error) {
	return service.components.workspace.FileCount()
}

// WorkspaceGitLog returns recent commits for one repository in the workspace.
func (service *Service) WorkspaceGitLog(repo string, limit int) (dto.GitLogResult, error) {
	return service.workspaceApplicationComponent().GitLog(repo, limit)
}

// WorkspaceChanges returns uncommitted changes for one repository.
func (service *Service) WorkspaceChanges(repo string, limit int) (dto.WorkspaceChangesResult, error) {
	return service.workspaceApplicationComponent().Changes(repo, limit)
}

// WorkspaceGitCommitDetail returns the changed-file summary of one commit.
func (service *Service) WorkspaceGitCommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error) {
	return service.workspaceApplicationComponent().CommitDetail(repo, hash, limit)
}

// WorkspaceFileContent reads controlled bytes from the current workspace.
func (service *Service) WorkspaceFileContent(relPath string, limit int64) (dto.FileContent, error) {
	return service.components.workspace.FileContent(relPath, limit)
}

// WorkspaceGitCommitFileContent reads one file from a commit's git object.
func (service *Service) WorkspaceGitCommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error) {
	return service.workspaceApplicationComponent().CommitFileContent(repo, hash, relPath, limit)
}

// WorkspaceWriteFile overwrites one workspace file from the GUI editor.
func (service *Service) WorkspaceWriteFile(relPath, content string) (dto.FileWriteResult, error) {
	return service.components.workspace.WriteFile(relPath, content)
}

// applyWorkspaceProjectionLocked copies a middle-layer projection into the
// authoritative application snapshot. The caller holds Core.ViewMu.
func (service *Service) applyWorkspaceProjectionLocked(projection switchworkspace.Projection) {
	service.Core.Snapshot.Workspaces = append([]model.WorkspaceInfo(nil), projection.Workspaces...)
	service.Core.Snapshot.SessionWorkspaces = make(map[string]string, len(projection.Bindings))
	for sessionID, workspaceID := range projection.Bindings {
		service.Core.Snapshot.SessionWorkspaces[sessionID] = workspaceID
	}
}

func (service *Service) workspaceApplicationComponent() *applicationworkspace.Application {
	if service == nil {
		return nil
	}
	return service.components.workspaceApplication
}

// newServiceWorkspaceApplication adapts Service's authoritative workspace,
// view and publication hosts to the workspace domain application.
func newServiceWorkspaceApplication(service *Service) *applicationworkspace.Application {
	return applicationworkspace.New(applicationworkspace.Deps{
		Environment: func() contract.WorkspacePort {
			if service == nil {
				return nil
			}
			return service.Deps.Workspace
		},
		WorkspaceRoot: func() (contract.WorkspacePort, string, error) {
			if service == nil || service.components.workspace == nil {
				return nil, "", errors.New("worktree: workspace root port is not assembled")
			}
			return service.components.workspace.WorkspaceRootPort()
		},
		CreateWorkspaces: func(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
			return service.components.workspace.CreateWorkspaces(requests)
		},
		Projection: serviceWorkspaceProjectionPort{service: service},
	})
}

type serviceWorkspaceProjectionPort struct {
	service *Service
}

func (port serviceWorkspaceProjectionPort) Projection() switchworkspace.Projection {
	return port.service.components.workspace.Projection()
}

func (port serviceWorkspaceProjectionPort) LockView() {
	port.service.ViewMu.Lock()
}

func (port serviceWorkspaceProjectionPort) ApplyWorkspaceProjectionLocked(projection switchworkspace.Projection) {
	port.service.applyWorkspaceProjectionLocked(projection)
}

func (port serviceWorkspaceProjectionPort) BumpLocked() uint64 {
	return port.service.bumpLocked()
}

func (port serviceWorkspaceProjectionPort) UnlockView() {
	port.service.ViewMu.Unlock()
}

func (port serviceWorkspaceProjectionPort) CurrentViewSessionID() string {
	return port.service.currentViewSessionID()
}

func (port serviceWorkspaceProjectionPort) PublishSnapshotChanged(revision uint64, sessionID string) {
	port.service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
}
