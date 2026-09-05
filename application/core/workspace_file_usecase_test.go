package core

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/adapters"
	"github.com/RedHuang-0622/seelex/workspace"
)

// fileFakeWorkspace 内嵌 fakeWorkspace（WorkspacePort）并实现
// WorkspaceFilePort，供文件预览用例测试。
type fileFakeWorkspace struct {
	*fakeWorkspace
	content   dto.FileContent
	fileErr   error
	lastRoot  string
	lastRel   string
	lastLimit int64
}

func (fake *fileFakeWorkspace) ReadFile(root, relPath string, limit int64) (dto.FileContent, error) {
	fake.lastRoot = root
	fake.lastRel = relPath
	fake.lastLimit = limit
	return fake.content, fake.fileErr
}

func TestWorkspaceFileContentForwardsCurrentWorkspaceRoot(t *testing.T) {
	fake := &fileFakeWorkspace{
		fakeWorkspace: newFakeWorkspace(),
		content: dto.FileContent{
			Name: "main.go", Path: "src/main.go", Size: 4, Limit: 16, TextLike: true,
			Base64: base64.StdEncoding.EncodeToString([]byte("code")),
		},
	}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = fake
	})

	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}

	got, err := service.WorkspaceFileContent("src/main.go", 16)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "src/main.go" || got.TextLike != true {
		t.Fatalf("unexpected content: %+v", got)
	}
	if fake.lastRoot != root || fake.lastRel != "src/main.go" || fake.lastLimit != 16 {
		t.Fatalf("forwarded root=%q rel=%q limit=%d", fake.lastRoot, fake.lastRel, fake.lastLimit)
	}
}

func TestWorkspaceFileContentRejectsWithoutBoundWorkspace(t *testing.T) {
	fake := &fileFakeWorkspace{fakeWorkspace: newFakeWorkspace()}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = fake
	})
	if _, err := service.WorkspaceFileContent("README.md", 0); err == nil {
		t.Fatal("WorkspaceFileContent succeeded without a workspace")
	}
}

func TestWorkspaceFileContentFallsBackWhenBackendLacksFilePort(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = newFakeWorkspace()
	})
	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.WorkspaceFileContent("README.md", 0); err == nil {
		t.Fatal("WorkspaceFileContent succeeded without file-capable backend")
	}
}

// TestWorkspaceFileContentUseCaseReadsRealFilesystem 走真实 workspace.Repo
// （实现 WorkspaceFilePort），验证读取内容与敏感路径拒绝。
func TestWorkspaceFileContentUseCaseReadsRealFilesystem(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, "README.md")
	if err := os.WriteFile(full, []byte("# 项目\n正文\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "config")
	if err := os.MkdirAll(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "accounts.yaml"), []byte("token: x"), 0o600); err != nil {
		t.Fatal(err)
	}

	repo := adapters.WorkspacePort{Repo: workspace.NewRepo()}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = repo
	})
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}

	got, err := service.WorkspaceFileContent("README.md", 0)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := base64.StdEncoding.DecodeString(got.Base64)
	if string(decoded) != "# 项目\n正文\n" || !got.TextLike {
		t.Fatalf("unexpected content: %+v", got)
	}

	if _, err := service.WorkspaceFileContent("config/accounts.yaml", 0); err == nil {
		t.Fatal("WorkspaceFileContent read a sensitive file")
	}
}
