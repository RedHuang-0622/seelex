package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/adapters"
	"github.com/RedHuang-0622/seelex/workspace"
)

// treeFakeWorkspace 内嵌 fakeWorkspace（WorkspacePort）并实现
// WorkspaceTreePort，供工作树用例测试。
type treeFakeWorkspace struct {
	*fakeWorkspace
	listing   dto.TreeListing
	count     dto.TreeCount
	gitLog    dto.GitLogResult
	changes   dto.WorkspaceChangesResult
	gitCommit dto.GitCommitDetail
	listErr   error
	countErr  error
	lastRoot  string
	lastRel   string
	lastDepth int
	lastLimit int
	lastHash  string
}

func (fake *treeFakeWorkspace) ListTree(root, relPath string, depth int) (dto.TreeListing, error) {
	fake.lastRoot = root
	fake.lastRel = relPath
	fake.lastDepth = depth
	return fake.listing, fake.listErr
}

func (fake *treeFakeWorkspace) CountFiles(root string) (dto.TreeCount, error) {
	fake.lastRoot = root
	return fake.count, fake.countErr
}

func (fake *treeFakeWorkspace) GitLog(root string, limit int) (dto.GitLogResult, error) {
	fake.lastRoot = root
	fake.lastLimit = limit
	return fake.gitLog, nil
}

func (fake *treeFakeWorkspace) GitChanges(root string, limit int) (dto.WorkspaceChangesResult, error) {
	fake.lastRoot = root
	fake.lastLimit = limit
	return fake.changes, nil
}

func (fake *treeFakeWorkspace) GitCommitDetail(root, hash string, limit int) (dto.GitCommitDetail, error) {
	fake.lastRoot = root
	fake.lastHash = hash
	fake.lastLimit = limit
	return fake.gitCommit, nil
}

func TestWorkspaceTreeForwardsCurrentWorkspaceRoot(t *testing.T) {
	fake := &treeFakeWorkspace{
		fakeWorkspace: newFakeWorkspace(),
		listing: dto.TreeListing{Entries: []dto.TreeEntry{
			{Name: "src", Path: "src", Type: "dir", Count: 2},
			{Name: "README.md", Path: "README.md", Type: "file", Size: 10},
		}},
		count: dto.TreeCount{Files: 3, Dirs: 1},
	}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = fake
	})

	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}

	listing, err := service.WorkspaceTree("src", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 2 || listing.Entries[0].Name != "src" {
		t.Fatalf("unexpected listing: %+v", listing.Entries)
	}
	if fake.lastRoot != root || fake.lastRel != "src" || fake.lastDepth != 1 {
		t.Fatalf("forwarded root=%q rel=%q depth=%d", fake.lastRoot, fake.lastRel, fake.lastDepth)
	}

	count, err := service.WorkspaceFileCount()
	if err != nil {
		t.Fatal(err)
	}
	if count.Files != 3 || fake.lastRoot != root {
		t.Fatalf("unexpected count=%+v root=%q", count, fake.lastRoot)
	}
}

func TestWorkspaceTreeRejectsWithoutBoundWorkspace(t *testing.T) {
	fake := &treeFakeWorkspace{fakeWorkspace: newFakeWorkspace()}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = fake
	})
	if _, err := service.WorkspaceTree("", 1); err == nil {
		t.Fatal("WorkspaceTree succeeded without a workspace")
	}
	if _, err := service.WorkspaceFileCount(); err == nil {
		t.Fatal("WorkspaceFileCount succeeded without a workspace")
	}
	if _, err := service.WorkspaceGitLog(20); err == nil {
		t.Fatal("WorkspaceGitLog succeeded without a workspace")
	}
	if _, err := service.WorkspaceChanges(20); err == nil {
		t.Fatal("WorkspaceChanges succeeded without a workspace")
	}
	if _, err := service.WorkspaceGitCommitDetail("abc", 20); err == nil {
		t.Fatal("WorkspaceGitCommitDetail succeeded without a workspace")
	}
}

func TestWorkspaceGitLogForwardsCurrentWorkspaceRoot(t *testing.T) {
	fake := &treeFakeWorkspace{
		fakeWorkspace: newFakeWorkspace(),
		gitLog: dto.GitLogResult{Commits: []dto.GitCommitNode{
			{Hash: "abc", ShortHash: "abc", Author: "dev", Date: "08-29 10:00", Parents: []string{"def"}, Subject: "fix: git log"},
		}},
	}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = fake
	})

	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}

	result, err := service.WorkspaceGitLog(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Commits) != 1 || result.Commits[0].Subject != "fix: git log" || len(result.Commits[0].Parents) != 1 {
		t.Fatalf("unexpected git log result: %+v", result.Commits)
	}
	if fake.lastRoot != root || fake.lastLimit != 10 {
		t.Fatalf("forwarded root=%q limit=%d", fake.lastRoot, fake.lastLimit)
	}
}

func TestWorkspaceGitCommitDetailForwardsCurrentWorkspaceRoot(t *testing.T) {
	fake := &treeFakeWorkspace{
		fakeWorkspace: newFakeWorkspace(),
		gitCommit: dto.GitCommitDetail{
			Hash: "abc", ShortHash: "abc", Author: "dev", Date: "08-29 10:00", Subject: "feat: commit view",
			Total: 1,
			Files: []dto.GitCommitFileEntry{{Path: "src/main.go", Kind: dto.ChangeModified, Status: "M", Letter: "M", Additions: 3}},
		},
	}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = fake
	})

	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}

	result, err := service.WorkspaceGitCommitDetail("abc", 200)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Files) != 1 || result.Files[0].Path != "src/main.go" || result.Files[0].Additions != 3 {
		t.Fatalf("unexpected commit detail: %+v", result)
	}
	// hash 原样下发（客户端只能从提交列表带回来，不能自己拼路径）。
	if fake.lastRoot != root || fake.lastHash != "abc" || fake.lastLimit != 200 {
		t.Fatalf("forwarded root=%q hash=%q limit=%d", fake.lastRoot, fake.lastHash, fake.lastLimit)
	}
}

func TestWorkspaceTreeFallsBackWhenBackendLacksTreePort(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = newFakeWorkspace()
	})
	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.WorkspaceTree("", 1); err == nil {
		t.Fatal("WorkspaceTree succeeded without tree-capable backend")
	}
}

// TestWorkspaceTreeUseCaseReadsRealFilesystem 走真实 workspace.Repo（实现
// WorkspaceTreePort），验证忽略规则、敏感文件过滤与计数。
func TestWorkspaceTreeUseCaseReadsRealFilesystem(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		"README.md", "src/main.go", "src/util.go", "docs/guide.md",
		"node_modules/pkg/index.js", ".git/config", ".seelex/session.json",
		"config/accounts.yaml", "config/accounts.local.yaml",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	repo := adapters.WorkspacePort{Repo: workspace.NewRepo()}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = repo
	})
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}

	count, err := service.WorkspaceFileCount()
	if err != nil {
		t.Fatal(err)
	}
	// 可见文件：README.md、src/main.go、src/util.go、docs/guide.md（4）；
	// config/local.yaml 为 *.local.yaml 敏感名被排除。
	if count.Files != 4 || count.Dirs != 3 || count.Truncated {
		t.Fatalf("unexpected count=%+v", count)
	}

	listing, err := service.WorkspaceTree("", 1)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(listing.Entries))
	for _, entry := range listing.Entries {
		names = append(names, entry.Path)
	}
	want := []string{"config", "docs", "src", "README.md"}
	if len(names) != len(want) {
		t.Fatalf("listing=%v, want %v", names, want)
	}
	for index := range want {
		if names[index] != want[index] {
			t.Fatalf("listing=%v, want %v", names, want)
		}
	}
	// docs 直接文件计数 = 1（guide.md）；node_modules/.git/.seelex 均被忽略。
	for _, entry := range listing.Entries {
		if entry.Path == "docs" && entry.Count != 1 {
			t.Fatalf("docs count=%d, want 1", entry.Count)
		}
		if entry.Path == "src" && entry.Count != 2 {
			t.Fatalf("src count=%d, want 2", entry.Count)
		}
	}
}

func TestWorkspaceChangesForwardsCurrentWorkspaceRoot(t *testing.T) {
	fake := &treeFakeWorkspace{
		fakeWorkspace: newFakeWorkspace(),
		changes: dto.WorkspaceChangesResult{
			Branch: "main",
			Total:  1,
			Entries: []dto.WorkspaceChangeEntry{{
				Path: "src/main.go", Kind: dto.ChangeModified, Status: " M", Index: " ", Worktree: "M",
			}},
			Unstaged: 1,
		},
	}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = fake
	})

	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}

	result, err := service.WorkspaceChanges(50)
	if err != nil {
		t.Fatal(err)
	}
	if result.Branch != "main" || result.Total != 1 || len(result.Entries) != 1 {
		t.Fatalf("unexpected changes result: %+v", result)
	}
	if result.Entries[0].Kind != dto.ChangeModified || result.Entries[0].Worktree != "M" {
		t.Fatalf("unexpected entry: %+v", result.Entries[0])
	}
	// root 只由后端给出（客户端不能指定路径），limit 原样转发。
	if fake.lastRoot != root || fake.lastLimit != 50 {
		t.Fatalf("forwarded root=%q limit=%d", fake.lastRoot, fake.lastLimit)
	}
}

// TestWorkspaceChangesUseCaseReadsRealGitRepo 走真实 workspace.Repo（实现
// WorkspaceTreePort），验证用例层给出的 root/limit 能被真正执行、并把
// 未提交改动如实带回（含未跟踪文件与状态字符）。
func TestWorkspaceChangesUseCaseReadsRealGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in test environment")
	}
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	runGit("init", "-q", "-b", "main")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test Dev")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "-A")
	runGit("commit", "-qm", "first")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fresh.txt"), []byte("n"), 0o600); err != nil {
		t.Fatal(err)
	}

	repo := adapters.WorkspacePort{Repo: workspace.NewRepo()}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = repo
	})
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}

	result, err := service.WorkspaceChanges(0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("changes error payload: %s", result.Error)
	}
	if result.Branch != "main" {
		t.Fatalf("branch = %q, want main", result.Branch)
	}
	kinds := map[string]string{}
	for _, entry := range result.Entries {
		kinds[entry.Path] = entry.Kind
	}
	if kinds["tracked.txt"] != dto.ChangeModified || kinds["fresh.txt"] != dto.ChangeUntracked {
		t.Fatalf("unexpected entries: %+v", result.Entries)
	}
	if result.Unstaged != 1 || result.Untracked != 1 || result.Total != 2 {
		t.Fatalf("unexpected counts: %+v", result)
	}
}
