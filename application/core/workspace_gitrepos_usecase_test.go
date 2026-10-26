package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// gitReposFakeWorkspace 内嵌 treeFakeWorkspace（WorkspacePort + WorkspaceTreePort）
// 并实现 WorkspaceGitReposPort，供仓库发现用例测试。
type gitReposFakeWorkspace struct {
	*treeFakeWorkspace
	repos     dto.GitReposResult
	reposRoot string
	reposErr  error
}

func (fake *gitReposFakeWorkspace) GitRepos(root string, limit int) (dto.GitReposResult, error) {
	fake.reposRoot = root
	return fake.repos, fake.reposErr
}

// makeDir 在工作区根下造目录（仓库路径解析要求目录真实存在）。
func makeDir(t *testing.T, root, rel string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	return full
}

// TestWorkspaceGitQueriesForwardResolvedRepoRoot 钉住四条查询：repo 先解析成
// 绝对仓库根，再原样转发给既有 workspace 实现（limit/hash/relPath 不变）。
func TestWorkspaceGitQueriesForwardResolvedRepoRoot(t *testing.T) {
	fake := &gitReposFakeWorkspace{
		treeFakeWorkspace: &treeFakeWorkspace{
			fakeWorkspace: newFakeWorkspace(),
			gitLog:        dto.GitLogResult{Commits: []dto.GitCommitNode{{Hash: "aaaa", Subject: "fix"}}},
			changes:       dto.WorkspaceChangesResult{Branch: "dev", Total: 1},
			gitCommit:     dto.GitCommitDetail{Hash: "aaaa", Total: 1},
		},
	}
	service := newTestService(t, &fakeEngine{}, func(deps *contract.Dependencies) {
		deps.Workspace = fake
	})
	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	repoDir := makeDir(t, root, "vendor/lib")

	if _, err := service.WorkspaceGitLog("vendor/lib", 7); err != nil {
		t.Fatal(err)
	}
	if fake.lastRoot != repoDir || fake.lastLimit != 7 {
		t.Fatalf("git log forwarded root=%q limit=%d", fake.lastRoot, fake.lastLimit)
	}

	if _, err := service.WorkspaceChanges("vendor/lib", 200); err != nil {
		t.Fatal(err)
	}
	if fake.lastRoot != repoDir || fake.lastLimit != 200 {
		t.Fatalf("changes forwarded root=%q limit=%d", fake.lastRoot, fake.lastLimit)
	}

	if _, err := service.WorkspaceGitCommitDetail("vendor/lib", "aaaa", 150); err != nil {
		t.Fatal(err)
	}
	if fake.lastRoot != repoDir || fake.lastHash != "aaaa" || fake.lastLimit != 150 {
		t.Fatalf("commit detail forwarded root=%q hash=%q limit=%d", fake.lastRoot, fake.lastHash, fake.lastLimit)
	}

	// repo 省略（"."）时仍按工作区根收发：既有语义不变。
	if _, err := service.WorkspaceGitLog(".", 20); err != nil {
		t.Fatal(err)
	}
	if fake.lastRoot != filepath.Clean(root) {
		t.Fatalf("dot repo forwarded root=%q, want workspace root", fake.lastRoot)
	}

	// 越界入参是 Go error，且一次端口调用都不该发生。
	fake.lastRoot = ""
	if _, err := service.WorkspaceGitLog("../outside", 20); err == nil {
		t.Fatal("escaping repo must be a Go error")
	}
	if fake.lastRoot != "" {
		t.Fatalf("escaping repo must not reach the port (root=%q)", fake.lastRoot)
	}
}

// TestWorkspaceGitCommitFileContentForwardsResolvedRepoRoot 钉住提交内文件内容
// 也走同一个仓库根与文件端口。
func TestWorkspaceGitCommitFileContentForwardsResolvedRepoRoot(t *testing.T) {
	fake := &fileFakeWorkspace{
		fakeWorkspace: newFakeWorkspace(),
		commitContent: dto.FileContent{Path: "src/main.go", TextLike: true},
	}
	service := newTestService(t, &fakeEngine{}, func(deps *contract.Dependencies) {
		deps.Workspace = fake
	})
	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}
	repoDir := makeDir(t, root, "vendor/lib")

	if _, err := service.WorkspaceGitCommitFileContent("vendor/lib", "aaaa", "src/main.go", 16); err != nil {
		t.Fatal(err)
	}
	if fake.commitRoot != repoDir || fake.commitHash != "aaaa" || fake.commitRel != "src/main.go" || fake.commitLimit != 16 {
		t.Fatalf("forwarded root=%q hash=%q rel=%q limit=%d",
			fake.commitRoot, fake.commitHash, fake.commitRel, fake.commitLimit)
	}
}

// TestWorkspaceGitReposForwardsWorkspaceRoot 钉住仓库发现：root 只来自后端
// 当前 workspace，结果原样交回。
func TestWorkspaceGitReposForwardsWorkspaceRoot(t *testing.T) {
	fake := &gitReposFakeWorkspace{
		treeFakeWorkspace: &treeFakeWorkspace{fakeWorkspace: newFakeWorkspace()},
		repos: dto.GitReposResult{
			Repos:   []dto.GitRepoRef{{Rel: ".", Name: "project", Primary: true, Branch: "main"}},
			Scanned: 3,
		},
	}
	service := newTestService(t, &fakeEngine{}, func(deps *contract.Dependencies) {
		deps.Workspace = fake
	})
	root := t.TempDir()
	if err := service.CreateWorkspace("project", root, ""); err != nil {
		t.Fatal(err)
	}

	result, err := service.WorkspaceGitRepos()
	if err != nil {
		t.Fatal(err)
	}
	if fake.reposRoot != root {
		t.Fatalf("forwarded root=%q, want %q", fake.reposRoot, root)
	}
	if len(result.Repos) != 1 || !result.Repos[0].Primary || result.Scanned != 3 {
		t.Fatalf("unexpected repos result: %+v", result)
	}
}

// TestWorkspaceGitReposDegradesWithoutPort 钉住两种"不可用"：后端没有仓库发现
// 端口、以及根本没绑定工作区。两者都只给展示文案，不是 Go error。
func TestWorkspaceGitReposDegradesWithoutPort(t *testing.T) {
	t.Run("no port", func(t *testing.T) {
		service := newTestService(t, &fakeEngine{}, func(deps *contract.Dependencies) {
			deps.Workspace = newFakeWorkspace()
		})
		if err := service.CreateWorkspace("project", t.TempDir(), ""); err != nil {
			t.Fatal(err)
		}
		result, err := service.WorkspaceGitRepos()
		if err != nil {
			t.Fatalf("missing port must not be a Go error: %v", err)
		}
		if !strings.Contains(result.Error, "does not support repo discovery") {
			t.Fatalf("error payload=%q, want unsupported-port message", result.Error)
		}
		if len(result.Repos) != 0 {
			t.Fatalf("repos=%+v, want empty", result.Repos)
		}
	})

	t.Run("no workspace", func(t *testing.T) {
		fake := &gitReposFakeWorkspace{
			treeFakeWorkspace: &treeFakeWorkspace{fakeWorkspace: newFakeWorkspace()},
		}
		service := newTestService(t, &fakeEngine{}, func(deps *contract.Dependencies) {
			deps.Workspace = fake
		})
		result, err := service.WorkspaceGitRepos()
		if err != nil {
			t.Fatalf("unbound workspace must not be a Go error: %v", err)
		}
		if !strings.Contains(result.Error, "no workspace bound") {
			t.Fatalf("error payload=%q, want unbound message", result.Error)
		}
	})
}

func TestWorkspaceGitApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("workspaceApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 workspaceApplication")
	}
	if field.Type != reflect.TypeOf((*applicationworkspace.Application)(nil)) {
		t.Fatalf("workspaceApplication 字段类型 = %s", field.Type)
	}
}

func TestWorkspaceGitFacadeOnlyDelegatesToApplication(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	dir := filepath.Dir(file)
	tests := []struct {
		file    string
		methods []string
	}{
		{file: "workspace_gitrepos.go", methods: []string{"WorkspaceGitRepos"}},
		{file: "workspace_usecase.go", methods: []string{
			"WorkspaceGitLog",
			"WorkspaceChanges",
			"WorkspaceGitCommitDetail",
			"WorkspaceGitCommitFileContent",
		}},
	}
	for _, test := range tests {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, test.file), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		want := make(map[string]bool, len(test.methods))
		for _, method := range test.methods {
			want[method] = false
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Name == nil || !functionHasReceiver(function, "Service") {
				continue
			}
			if _, tracked := want[function.Name.Name]; !tracked {
				continue
			}
			want[function.Name.Name] = true
			if len(function.Body.List) != 1 {
				t.Fatalf("%s 有 %d 条语句，want 一行 workspaceGitApplication 转发", function.Name.Name, len(function.Body.List))
			}
			returnStatement, ok := function.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(returnStatement.Results) != 1 {
				t.Fatalf("%s 不是单行 return 转发", function.Name.Name)
			}
		}
		for method, found := range want {
			if !found {
				t.Fatalf("%s 未找到 %s", test.file, method)
			}
		}
	}
}
