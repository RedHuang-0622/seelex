package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

type fakeWorkspace struct {
	root string

	environmentProject string
	environmentName    string
	defaultEnvironment string
	change             model.WorkspaceEnvironmentChange

	reposRoot      string
	repos          dto.GitReposResult
	gitRoot        string
	gitLog         dto.GitLogResult
	changes        dto.WorkspaceChangesResult
	commitDetail   dto.GitCommitDetail
	commitFileRoot string
}

func (workspace *fakeWorkspace) Create(string, string, string) (model.WorkspaceInfo, error) {
	return model.WorkspaceInfo{}, nil
}
func (workspace *fakeWorkspace) Get(string) (model.WorkspaceInfo, error) {
	return model.WorkspaceInfo{}, nil
}
func (workspace *fakeWorkspace) List() []model.WorkspaceInfo {
	return nil
}
func (workspace *fakeWorkspace) Delete(string) error {
	return nil
}
func (workspace *fakeWorkspace) BindSession(string, string) {}
func (workspace *fakeWorkspace) UnbindSession(string)       {}
func (workspace *fakeWorkspace) SessionWorkspace(string) (model.WorkspaceInfo, bool) {
	return model.WorkspaceInfo{}, false
}
func (workspace *fakeWorkspace) AllBindings() map[string]string {
	return nil
}
func (workspace *fakeWorkspace) DetectGitRemote(string) string {
	return ""
}
func (workspace *fakeWorkspace) WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	workspace.environmentProject = projectID
	return []model.WorkspaceEnvironmentInfo{{Name: "dev"}}, "dev", nil
}
func (workspace *fakeWorkspace) SetWorkspaceEnvironment(projectID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	workspace.environmentProject, workspace.environmentName, workspace.change = projectID, name, change
	return model.WorkspaceEnvironmentInfo{Name: name}, nil
}
func (workspace *fakeWorkspace) DeleteWorkspaceEnvironment(projectID, name string) error {
	workspace.environmentProject, workspace.environmentName = projectID, name
	return nil
}
func (workspace *fakeWorkspace) SetDefaultWorkspaceEnvironment(projectID, name string) error {
	workspace.environmentProject, workspace.defaultEnvironment = projectID, name
	return nil
}
func (workspace *fakeWorkspace) GitRepos(root string, limit int) (dto.GitReposResult, error) {
	workspace.reposRoot = root
	if limit != 0 {
		return dto.GitReposResult{}, errors.New("limit must be delegated as zero")
	}
	return workspace.repos, nil
}
func (workspace *fakeWorkspace) ListTree(string, string, int) (dto.TreeListing, error) {
	return dto.TreeListing{}, nil
}
func (workspace *fakeWorkspace) CountFiles(string) (dto.TreeCount, error) {
	return dto.TreeCount{}, nil
}
func (workspace *fakeWorkspace) GitLog(root string, limit int) (dto.GitLogResult, error) {
	workspace.gitRoot = root
	return workspace.gitLog, nil
}
func (workspace *fakeWorkspace) GitChanges(root string, limit int) (dto.WorkspaceChangesResult, error) {
	workspace.gitRoot = root
	return workspace.changes, nil
}
func (workspace *fakeWorkspace) GitCommitDetail(root, hash string, limit int) (dto.GitCommitDetail, error) {
	workspace.gitRoot = root
	return workspace.commitDetail, nil
}
func (workspace *fakeWorkspace) ReadFile(string, string, int64) (dto.FileContent, error) {
	return dto.FileContent{}, nil
}
func (workspace *fakeWorkspace) GitCommitFileContent(root, hash, relPath string, limit int64) (dto.FileContent, error) {
	workspace.commitFileRoot = root
	return dto.FileContent{Path: relPath}, nil
}

type workspaceBasePort struct{}

func (workspaceBasePort) Create(string, string, string) (model.WorkspaceInfo, error) {
	return model.WorkspaceInfo{}, nil
}
func (workspaceBasePort) Get(string) (model.WorkspaceInfo, error) {
	return model.WorkspaceInfo{}, nil
}
func (workspaceBasePort) List() []model.WorkspaceInfo { return nil }
func (workspaceBasePort) Delete(string) error         { return nil }
func (workspaceBasePort) BindSession(string, string)  {}
func (workspaceBasePort) UnbindSession(string)        {}
func (workspaceBasePort) SessionWorkspace(string) (model.WorkspaceInfo, bool) {
	return model.WorkspaceInfo{}, false
}
func (workspaceBasePort) AllBindings() map[string]string { return nil }
func (workspaceBasePort) DetectGitRemote(string) string  { return "" }

type projectionFake struct {
	calls      []string
	projection switchworkspace.Projection
}

func (fake *projectionFake) Projection() switchworkspace.Projection {
	fake.calls = append(fake.calls, "projection")
	return fake.projection
}
func (fake *projectionFake) LockView() {
	fake.calls = append(fake.calls, "lock")
}
func (fake *projectionFake) ApplyWorkspaceProjectionLocked(switchworkspace.Projection) {
	fake.calls = append(fake.calls, "apply")
}
func (fake *projectionFake) BumpLocked() uint64 {
	fake.calls = append(fake.calls, "bump")
	return 9
}
func (fake *projectionFake) UnlockView() {
	fake.calls = append(fake.calls, "unlock")
}
func (fake *projectionFake) CurrentViewSessionID() string {
	fake.calls = append(fake.calls, "session")
	return "session-a"
}
func (fake *projectionFake) PublishSnapshotChanged(revision uint64, sessionID string) {
	fake.calls = append(fake.calls, "publish")
	if revision != 9 || sessionID != "session-a" {
		panic("invalid publication")
	}
}

func TestEnvironmentUseCasesTrimIDsAndDelegate(t *testing.T) {
	fake := &fakeWorkspace{}
	application := New(Deps{Environment: func() contract.WorkspacePort { return fake }})
	change := model.WorkspaceEnvironmentChange{Host: "host"}

	if _, defaultEnvironment, err := application.WorkspaceEnvironments(" project "); err != nil ||
		fake.environmentProject != "project" || defaultEnvironment != "dev" {
		t.Fatalf("WorkspaceEnvironments = %q, %v, project=%q",
			defaultEnvironment, err, fake.environmentProject)
	}
	if _, err := application.SetWorkspaceEnvironment(" project ", " dev ", change); err != nil ||
		fake.environmentProject != "project" || fake.environmentName != "dev" {
		t.Fatalf("SetWorkspaceEnvironment = %v, project=%q env=%q",
			err, fake.environmentProject, fake.environmentName)
	}
	if err := application.DeleteWorkspaceEnvironment(" project ", " dev "); err != nil ||
		fake.environmentProject != "project" || fake.environmentName != "dev" {
		t.Fatalf("DeleteWorkspaceEnvironment = %v, project=%q env=%q",
			err, fake.environmentProject, fake.environmentName)
	}
	if err := application.SetDefaultWorkspaceEnvironment(" project ", " dev "); err != nil ||
		fake.environmentProject != "project" || fake.defaultEnvironment != "dev" {
		t.Fatalf("SetDefaultWorkspaceEnvironment = %v, project=%q env=%q",
			err, fake.environmentProject, fake.defaultEnvironment)
	}
}

func TestEnvironmentUseCasesPreserveMissingPortErrors(t *testing.T) {
	var nilApplication *Application
	if _, _, err := nilApplication.WorkspaceEnvironments("project"); err == nil ||
		!strings.Contains(err.Error(), "workspace port is not assembled") {
		t.Fatalf("nil error = %v", err)
	}
	withoutEnvironment := New(Deps{
		Environment: func() contract.WorkspacePort { return workspaceBasePort{} },
	})
	if _, _, err := withoutEnvironment.WorkspaceEnvironments("project"); err == nil ||
		!strings.Contains(err.Error(), "does not expose the environment table") {
		t.Fatalf("missing environment error = %v", err)
	}
	application := New(Deps{Environment: func() contract.WorkspacePort { return &fakeWorkspace{} }})
	if _, _, err := application.WorkspaceEnvironments(" "); err == nil ||
		!strings.Contains(err.Error(), "project id is required") {
		t.Fatalf("empty project error = %v", err)
	}
}

func TestGitQueriesResolveRepoInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "vendor", "lib")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := &fakeWorkspace{
		gitLog:       dto.GitLogResult{Commits: []dto.GitCommitNode{{Hash: "abc"}}},
		changes:      dto.WorkspaceChangesResult{Branch: "dev"},
		commitDetail: dto.GitCommitDetail{Hash: "abc"},
	}
	application := New(Deps{
		WorkspaceRoot: func() (contract.WorkspacePort, string, error) {
			return fake, root, nil
		},
	})
	if _, err := application.GitLog("vendor/lib", 7); err != nil || fake.gitRoot != repo {
		t.Fatalf("GitLog = %v, root=%q want %q", err, fake.gitRoot, repo)
	}
	if _, err := application.Changes("vendor/lib", 8); err != nil || fake.gitRoot != repo {
		t.Fatalf("Changes = %v, root=%q want %q", err, fake.gitRoot, repo)
	}
	if _, err := application.CommitDetail("vendor/lib", "abc", 9); err != nil || fake.gitRoot != repo {
		t.Fatalf("CommitDetail = %v, root=%q want %q", err, fake.gitRoot, repo)
	}
	if _, err := application.CommitFileContent("vendor/lib", "abc", "main.go", 10); err != nil ||
		fake.commitFileRoot != repo {
		t.Fatalf("CommitFileContent = %v, root=%q want %q", err, fake.commitFileRoot, repo)
	}
}

func TestGitQueriesRejectEscapes(t *testing.T) {
	root := t.TempDir()
	application := New(Deps{
		WorkspaceRoot: func() (contract.WorkspacePort, string, error) {
			return &fakeWorkspace{}, root, nil
		},
	})
	for _, repo := range []string{"../outside", root} {
		if _, err := application.GitLog(repo, 1); err == nil {
			t.Fatalf("GitLog(%q) succeeded", repo)
		}
	}
}

func TestGitQueriesRejectSymlinkEscapeWhenSupported(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	application := New(Deps{
		WorkspaceRoot: func() (contract.WorkspacePort, string, error) {
			return &fakeWorkspace{}, root, nil
		},
	})
	if _, err := application.GitLog("escape", 1); err == nil ||
		!strings.Contains(err.Error(), "escapes the workspace root") {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestWorkspaceGitReposDegradesAndForwardsRoot(t *testing.T) {
	root := t.TempDir()
	fake := &fakeWorkspace{repos: dto.GitReposResult{
		Repos: []dto.GitRepoRef{{Rel: ".", Name: "root"}},
	}}
	application := New(Deps{
		WorkspaceRoot: func() (contract.WorkspacePort, string, error) {
			return fake, root, nil
		},
	})
	result, err := application.WorkspaceGitRepos()
	if err != nil || fake.reposRoot != root || len(result.Repos) != 1 {
		t.Fatalf("WorkspaceGitRepos = %+v, %v, root=%q", result, err, fake.reposRoot)
	}

	withoutPort := New(Deps{
		WorkspaceRoot: func() (contract.WorkspacePort, string, error) {
			return workspaceBasePort{}, root, nil
		},
	})
	result, err = withoutPort.WorkspaceGitRepos()
	if err != nil || result.Error == "" || len(result.Repos) != 0 {
		t.Fatalf("missing repository port = %+v, %v", result, err)
	}
}

func TestCreateWorkspacesAppliesProjectionAfterSuccess(t *testing.T) {
	projection := &projectionFake{projection: switchworkspace.Projection{
		Bindings: map[string]string{"session-a": "workspace-a"},
	}}
	application := New(Deps{
		CreateWorkspaces: func(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
			projection.calls = append(projection.calls, "create")
			return dto.WorkspaceCreateBatchResult{Workspaces: []dto.WorkspaceCreateResult{{ID: "workspace-a"}}}, nil
		},
		Projection: projection,
	})
	result, err := application.CreateWorkspaces([]dto.WorkspaceCreateRequest{{Name: "project"}})
	if err != nil || len(result.Workspaces) != 1 {
		t.Fatalf("CreateWorkspaces = %+v, %v", result, err)
	}
	want := "create,projection,lock,apply,bump,unlock,session,publish"
	if got := strings.Join(projection.calls, ","); got != want {
		t.Fatalf("calls = %s, want %s", got, want)
	}
}

func TestCreateWorkspacesStopsOnCreateError(t *testing.T) {
	createErr := errors.New("create failed")
	projection := &projectionFake{}
	application := New(Deps{
		CreateWorkspaces: func([]dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
			return dto.WorkspaceCreateBatchResult{}, createErr
		},
		Projection: projection,
	})
	if _, err := application.CreateWorkspaces(nil); !errors.Is(err, createErr) {
		t.Fatalf("CreateWorkspaces error = %v", err)
	}
	if len(projection.calls) != 0 {
		t.Fatalf("projection called after create error: %v", projection.calls)
	}
}
