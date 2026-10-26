// Package workspace owns workspace-project administration and read-only
// workspace query adapters. Workspace persistence, environment storage, git
// inspection and file access remain behind narrow host ports.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

// ProjectionPort is the application view surface used after batch workspace
// creation. The host owns locking, snapshot mutation, revision allocation and
// event publication; this package only fixes their order.
type ProjectionPort interface {
	Projection() switchworkspace.Projection
	LockView()
	ApplyWorkspaceProjectionLocked(switchworkspace.Projection)
	BumpLocked() uint64
	UnlockView()
	CurrentViewSessionID() string
	PublishSnapshotChanged(revision uint64, sessionID string)
}

// Deps contains the workspace ports consumed by this application. The
// environment and active-view root are intentionally separate: project
// environment administration addresses a project ID directly, while git
// queries use the authoritative root of the active view.
type Deps struct {
	Environment      func() contract.WorkspacePort
	WorkspaceRoot    func() (contract.WorkspacePort, string, error)
	CreateWorkspaces func([]dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error)
	Projection       ProjectionPort
}

// Application coordinates workspace use cases without owning persistence,
// environment credentials, git inspection or file-read implementations.
type Application struct {
	deps Deps
}

// New constructs the workspace application from consumer-defined ports.
func New(deps Deps) *Application {
	return &Application{deps: deps}
}

// WorkspaceEnvironments reads one project's environment table and default name.
func (application *Application) WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	port, workspaceID, err := application.environmentPort(projectID)
	if err != nil {
		return nil, "", err
	}
	return port.WorkspaceEnvironments(workspaceID)
}

// SetWorkspaceEnvironment writes one project environment.
func (application *Application) SetWorkspaceEnvironment(projectID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	port, workspaceID, err := application.environmentPort(projectID)
	if err != nil {
		return model.WorkspaceEnvironmentInfo{}, err
	}
	if strings.TrimSpace(name) == "" {
		return model.WorkspaceEnvironmentInfo{}, errors.New("workspace environments: environment name is required")
	}
	return port.SetWorkspaceEnvironment(workspaceID, strings.TrimSpace(name), change)
}

// DeleteWorkspaceEnvironment removes one project environment and its secret.
func (application *Application) DeleteWorkspaceEnvironment(projectID, name string) error {
	port, workspaceID, err := application.environmentPort(projectID)
	if err != nil {
		return err
	}
	return port.DeleteWorkspaceEnvironment(workspaceID, strings.TrimSpace(name))
}

// SetDefaultWorkspaceEnvironment selects or clears a project's default
// environment.
func (application *Application) SetDefaultWorkspaceEnvironment(projectID, name string) error {
	port, workspaceID, err := application.environmentPort(projectID)
	if err != nil {
		return err
	}
	return port.SetDefaultWorkspaceEnvironment(workspaceID, strings.TrimSpace(name))
}

// WorkspaceGitRepos discovers git repositories under the active workspace root.
func (application *Application) WorkspaceGitRepos() (dto.GitReposResult, error) {
	port, root, err := application.workspaceRoot()
	if err != nil {
		return dto.GitReposResult{Repos: []dto.GitRepoRef{}, Error: err.Error()}, nil
	}
	reposPort, ok := port.(contract.WorkspaceGitReposPort)
	if !ok {
		return dto.GitReposResult{
			Repos: []dto.GitRepoRef{},
			Error: "worktree: workspace backend does not support repo discovery",
		}, nil
	}
	return reposPort.GitRepos(root, 0)
}

// GitLog reads recent commits from one repository inside the workspace.
func (application *Application) GitLog(repo string, limit int) (dto.GitLogResult, error) {
	port, root, err := application.workspaceRepoPort(repo)
	if err != nil {
		return dto.GitLogResult{}, err
	}
	treePort, err := workspaceTreePort(port)
	if err != nil {
		return dto.GitLogResult{}, err
	}
	return treePort.GitLog(root, limit)
}

// Changes reads uncommitted changes from one repository inside the workspace.
func (application *Application) Changes(repo string, limit int) (dto.WorkspaceChangesResult, error) {
	port, root, err := application.workspaceRepoPort(repo)
	if err != nil {
		return dto.WorkspaceChangesResult{}, err
	}
	treePort, err := workspaceTreePort(port)
	if err != nil {
		return dto.WorkspaceChangesResult{}, err
	}
	return treePort.GitChanges(root, limit)
}

// CommitDetail reads the file-change summary for one commit.
func (application *Application) CommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error) {
	port, root, err := application.workspaceRepoPort(repo)
	if err != nil {
		return dto.GitCommitDetail{}, err
	}
	treePort, err := workspaceTreePort(port)
	if err != nil {
		return dto.GitCommitDetail{}, err
	}
	return treePort.GitCommitDetail(root, hash, limit)
}

// CommitFileContent reads one file from a commit's git object data.
func (application *Application) CommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error) {
	port, root, err := application.workspaceRepoPort(repo)
	if err != nil {
		return dto.FileContent{}, err
	}
	filePort, ok := port.(contract.WorkspaceFilePort)
	if !ok {
		return dto.FileContent{}, errors.New("worktree: workspace backend does not support file preview")
	}
	return filePort.GitCommitFileContent(root, hash, relPath, limit)
}

// CreateWorkspaces registers projects without changing the current session
// binding, then applies and publishes the refreshed workspace projection.
func (application *Application) CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
	if application == nil {
		return dto.WorkspaceCreateBatchResult{}, errors.New("workspace application: service is unavailable")
	}
	result, err := application.deps.CreateWorkspaces(requests)
	if err != nil {
		return result, err
	}
	projectionPort := application.deps.Projection
	projection := projectionPort.Projection()
	projectionPort.LockView()
	projectionPort.ApplyWorkspaceProjectionLocked(projection)
	revision := projectionPort.BumpLocked()
	projectionPort.UnlockView()
	projectionPort.PublishSnapshotChanged(revision, projectionPort.CurrentViewSessionID())
	return result, nil
}

func (application *Application) environmentPort(projectID string) (contract.WorkspaceEnvironmentPort, string, error) {
	if application == nil || application.deps.Environment == nil || application.deps.Environment() == nil {
		return nil, "", errors.New("workspace environments: workspace port is not assembled")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, "", errors.New("workspace environments: project id is required")
	}
	port, ok := application.deps.Environment().(contract.WorkspaceEnvironmentPort)
	if !ok {
		return nil, "", errors.New("workspace environments: workspace port does not expose the environment table")
	}
	return port, projectID, nil
}

func (application *Application) workspaceRoot() (contract.WorkspacePort, string, error) {
	if application == nil || application.deps.WorkspaceRoot == nil {
		return nil, "", errors.New("worktree: workspace root port is not assembled")
	}
	return application.deps.WorkspaceRoot()
}

func (application *Application) workspaceRepoPort(repo string) (contract.WorkspacePort, string, error) {
	port, root, err := application.workspaceRoot()
	if err != nil {
		return nil, "", err
	}
	repoRoot, err := resolveRepo(root, repo)
	if err != nil {
		return nil, "", err
	}
	return port, repoRoot, nil
}

func workspaceTreePort(port contract.WorkspacePort) (contract.WorkspaceTreePort, error) {
	treePort, ok := port.(contract.WorkspaceTreePort)
	if !ok {
		return nil, errors.New("worktree: workspace backend does not support tree listing")
	}
	return treePort, nil
}

// resolveRepo converts a client repository path into an absolute repository
// root contained by the workspace. Absolute paths, lexical escapes symlink
// escapes, and non-directories are caller errors.
func resolveRepo(root, repo string) (string, error) {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		return "", errors.New("worktree: no workspace bound to current session")
	}
	rootAbs, err := filepath.Abs(trimmedRoot)
	if err != nil {
		return "", fmt.Errorf("worktree: resolve workspace root: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	raw := strings.TrimSpace(repo)
	if raw == "" || raw == "." {
		return rootAbs, nil
	}
	if filepath.IsAbs(raw) {
		return "", fmt.Errorf("worktree: repo path must be relative to the workspace root: %q", raw)
	}
	candidate := filepath.Clean(filepath.Join(rootAbs, filepath.FromSlash(raw)))
	if !withinRoot(rootAbs, candidate) {
		return "", fmt.Errorf("worktree: repo path escapes the workspace root: %q", raw)
	}
	info, statErr := os.Stat(candidate)
	if statErr != nil {
		return "", fmt.Errorf("worktree: inspect repo %q: %w", raw, statErr)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("worktree: repo %q is not a directory", raw)
	}
	realRoot, rootErr := filepath.EvalSymlinks(rootAbs)
	if rootErr != nil {
		realRoot = rootAbs
	}
	realCandidate, candidateErr := filepath.EvalSymlinks(candidate)
	if candidateErr != nil {
		return "", fmt.Errorf("worktree: resolve repo %q: %w", raw, candidateErr)
	}
	if !withinRoot(realRoot, realCandidate) {
		return "", fmt.Errorf("worktree: repo path escapes the workspace root: %q", raw)
	}
	return candidate, nil
}

func withinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
