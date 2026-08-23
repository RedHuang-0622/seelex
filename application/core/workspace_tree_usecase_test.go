package core

import (
	"os"
	"path/filepath"
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
	listErr   error
	countErr  error
	lastRoot  string
	lastRel   string
	lastDepth int
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
