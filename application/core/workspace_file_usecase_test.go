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
// WorkspaceFilePort + WorkspaceFileWritePort，供文件预览/编辑用例测试。
type fileFakeWorkspace struct {
	*fakeWorkspace
	content   dto.FileContent
	fileErr   error
	lastRoot  string
	lastRel   string
	lastLimit int64
	// 提交内文件内容（「提交记录 → 点开某个文件」）的面。
	commitContent dto.FileContent
	commitRoot    string
	commitHash    string
	commitRel     string
	commitLimit   int64
	// 写入面记录（编辑保存用例）。
	writeRoot    string
	writeRel     string
	writeContent string
	writeResult  dto.FileWriteResult
	writeErr     error
}

func (fake *fileFakeWorkspace) ReadFile(root, relPath string, limit int64) (dto.FileContent, error) {
	fake.lastRoot = root
	fake.lastRel = relPath
	fake.lastLimit = limit
	return fake.content, fake.fileErr
}

func (fake *fileFakeWorkspace) GitCommitFileContent(root, hash, relPath string, limit int64) (dto.FileContent, error) {
	fake.commitRoot = root
	fake.commitHash = hash
	fake.commitRel = relPath
	fake.commitLimit = limit
	return fake.commitContent, fake.fileErr
}

func (fake *fileFakeWorkspace) WriteFile(root, relPath string, content []byte) (dto.FileWriteResult, error) {
	fake.writeRoot = root
	fake.writeRel = relPath
	fake.writeContent = string(content)
	return fake.writeResult, fake.writeErr
}

// readOnlyFileFakeWorkspace 只实现**读**端口：编辑入口必须显式报错，而不是让
// 只读后端被动升级成可写。
type readOnlyFileFakeWorkspace struct{ *fakeWorkspace }

func (fake *readOnlyFileFakeWorkspace) ReadFile(root, relPath string, limit int64) (dto.FileContent, error) {
	return dto.FileContent{}, nil
}

func (fake *readOnlyFileFakeWorkspace) GitCommitFileContent(root, hash, relPath string, limit int64) (dto.FileContent, error) {
	return dto.FileContent{}, nil
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

func TestWorkspaceGitCommitFileContentForwardsHashPathAndLimit(t *testing.T) {
	fake := &fileFakeWorkspace{
		fakeWorkspace: newFakeWorkspace(),
		commitContent: dto.FileContent{
			Name: "main.go", Path: "src/main.go", Size: 4, Limit: 8, TextLike: true,
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

	got, err := service.WorkspaceGitCommitFileContent("abc1234", "src/main.go", 8)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "src/main.go" || !got.TextLike {
		t.Fatalf("unexpected content: %+v", got)
	}
	if fake.commitRoot != root || fake.commitHash != "abc1234" || fake.commitRel != "src/main.go" || fake.commitLimit != 8 {
		t.Fatalf("forwarded root=%q hash=%q rel=%q limit=%d",
			fake.commitRoot, fake.commitHash, fake.commitRel, fake.commitLimit)
	}
}

func TestWorkspaceGitCommitFileContentRejectsWithoutBoundWorkspace(t *testing.T) {
	fake := &fileFakeWorkspace{fakeWorkspace: newFakeWorkspace()}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = fake
	})
	if _, err := service.WorkspaceGitCommitFileContent("abc1234", "README.md", 0); err == nil {
		t.Fatal("WorkspaceGitCommitFileContent succeeded without a workspace")
	}
}

func TestWorkspaceGitCommitFileContentFallsBackWhenBackendLacksFilePort(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = newFakeWorkspace()
	})
	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.WorkspaceGitCommitFileContent("abc1234", "README.md", 0); err == nil {
		t.Fatal("WorkspaceGitCommitFileContent succeeded without file-capable backend")
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

// TestWorkspaceWriteFileForwardsCurrentWorkspaceRoot：编辑保存走的是与预览读取
// **同一条 root 解析**（当前会话绑定的工作区），客户端只能传相对路径。
func TestWorkspaceWriteFileForwardsCurrentWorkspaceRoot(t *testing.T) {
	fake := &fileFakeWorkspace{
		fakeWorkspace: newFakeWorkspace(),
		writeResult:   dto.FileWriteResult{Path: "src/main.go", Size: 5},
	}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = fake
	})

	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	got, err := service.WorkspaceWriteFile("src/main.go", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "src/main.go" || got.Size != 5 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if fake.writeRoot != root || fake.writeRel != "src/main.go" || fake.writeContent != "hello" {
		t.Fatalf("forwarded root=%q rel=%q content=%q", fake.writeRoot, fake.writeRel, fake.writeContent)
	}
}

func TestWorkspaceWriteFileRejectsWithoutBoundWorkspace(t *testing.T) {
	fake := &fileFakeWorkspace{fakeWorkspace: newFakeWorkspace()}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = fake
	})
	if _, err := service.WorkspaceWriteFile("README.md", "x"); err == nil {
		t.Fatal("WorkspaceWriteFile succeeded without a workspace")
	}
}

func TestWorkspaceWriteFileRejectsReadOnlyBackend(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = &readOnlyFileFakeWorkspace{fakeWorkspace: newFakeWorkspace()}
	})
	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.WorkspaceWriteFile("README.md", "x"); err == nil {
		t.Fatal("WorkspaceWriteFile succeeded on a read-only backend（只读后端不得被动升级成可写）")
	}
}

// TestWorkspaceWriteFileUseCaseWritesRealFilesystem：真实 workspace.Repo 上的
// 端到端口径——保存落地后读回同一路径就是刚写的内容（前端基线同步的前提），
// 敏感路径仍然拒绝。
func TestWorkspaceWriteFileUseCaseWritesRealFilesystem(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, "README.md")
	if err := os.WriteFile(full, []byte("# 旧\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secretDir := filepath.Join(root, "config")
	if err := os.MkdirAll(secretDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretDir, "accounts.yaml"), []byte("token: x"), 0o600); err != nil {
		t.Fatal(err)
	}

	repo := adapters.WorkspacePort{Repo: workspace.NewRepo()}
	service := newTestService(t, &fakeEngine{}, func(deps *Dependencies) {
		deps.Workspace = repo
	})
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := service.WorkspaceWriteFile("README.md", "# 新正文\n第二行\n"); err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "# 新正文\n第二行\n" {
		t.Fatalf("disk content = %q", onDisk)
	}

	got, err := service.WorkspaceFileContent("README.md", 0)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := base64.StdEncoding.DecodeString(got.Base64)
	if string(decoded) != "# 新正文\n第二行\n" {
		t.Fatalf("读回内容 = %q（保存后读回必须就是刚写的那一份）", decoded)
	}

	if _, err := service.WorkspaceWriteFile("config/accounts.yaml", "token: pwned"); err == nil {
		t.Fatal("WorkspaceWriteFile wrote a sensitive file")
	}
	if raw, err := os.ReadFile(filepath.Join(secretDir, "accounts.yaml")); err != nil || string(raw) != "token: x" {
		t.Fatalf("敏感文件被改写：%q err=%v", raw, err)
	}
}
