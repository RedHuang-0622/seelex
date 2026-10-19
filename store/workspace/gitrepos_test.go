package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// writeFakeRepo 造一个"看起来像仓库"的目录：`.git` 目录 + HEAD（不跑 git：
// 仓库发现只读目录结构与 HEAD，不需要真仓库）。
func writeFakeRepo(t *testing.T, dir, branch string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if branch == "" {
		branch = "main"
	}
	head := "ref: refs/heads/" + branch + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
}

// requireGit 在没有 git 的环境里跳过依赖真实 git 的用例。
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in test environment")
	}
}

// runGitIn 在 dir 里跑一条 git 命令（测试夹具用；失败即 Fatal）。
func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

// relsOf 取仓库清单的相对路径序列（断言顺序用）。
func relsOf(repos []dto.GitRepoRef) []string {
	rels := make([]string, 0, len(repos))
	for _, repo := range repos {
		rels = append(rels, repo.Rel)
	}
	return rels
}

// 案例 1：根自己是仓库 → 恰好 1 条，Rel="."、Primary、分支可读。
func TestGitReposRootIsRepo(t *testing.T) {
	root := t.TempDir()
	writeFakeRepo(t, root, "main")

	result, err := NewRepo().GitRepos(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error payload: %s", result.Error)
	}
	if len(result.Repos) != 1 {
		t.Fatalf("repos=%v, want exactly the root repo", relsOf(result.Repos))
	}
	rootRef := result.Repos[0]
	if rootRef.Rel != "." || !rootRef.Primary {
		t.Fatalf("root ref=%+v, want Rel=\".\" Primary=true", rootRef)
	}
	if rootRef.Name != filepath.Base(root) {
		t.Fatalf("root name=%q, want %q", rootRef.Name, filepath.Base(root))
	}
	if rootRef.Branch != "main" {
		t.Fatalf("branch=%q, want main", rootRef.Branch)
	}
	if result.Truncated || result.Scanned < 1 {
		t.Fatalf("truncated=%v scanned=%d", result.Truncated, result.Scanned)
	}
}

// 案例 2：根不是仓库、a/ 与 b/ 各是仓库 → 2 条、字典序、无 Primary。
func TestGitReposSiblingReposNoPrimary(t *testing.T) {
	root := t.TempDir()
	writeFakeRepo(t, filepath.Join(root, "b"), "main")
	writeFakeRepo(t, filepath.Join(root, "a"), "dev")

	result, err := NewRepo().GitRepos(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	rels := relsOf(result.Repos)
	if len(rels) != 2 || rels[0] != "a" || rels[1] != "b" {
		t.Fatalf("repos=%v, want [a b]", rels)
	}
	for _, repo := range result.Repos {
		if repo.Primary {
			t.Fatalf("no repo should be primary: %+v", repo)
		}
	}
	if result.Repos[0].Branch != "dev" || result.Repos[1].Branch != "main" {
		t.Fatalf("branches=%q/%q, want dev/main", result.Repos[0].Branch, result.Repos[1].Branch)
	}
	if result.Truncated {
		t.Fatal("two repos under a default limit must not report truncation")
	}
}

// 案例 3：仓库里套仓库 → 内层也出现（不因外层是仓库就停止下钻）。
func TestGitReposNestedRepoInnerVisible(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, "outer")
	writeFakeRepo(t, outer, "main")
	writeFakeRepo(t, filepath.Join(outer, "inner"), "main")

	result, err := NewRepo().GitRepos(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, repo := range result.Repos {
		got[repo.Rel] = true
	}
	if !got["outer"] || !got["outer/inner"] {
		t.Fatalf("repos=%v, want outer and outer/inner", relsOf(result.Repos))
	}
}

// 案例 4：`.git` 是**文件**（worktree/子模块形态）也识别，分支按 gitdir 跟随读到。
func TestGitReposGitFileFormRecognized(t *testing.T) {
	root := t.TempDir()
	worktree := filepath.Join(root, "wt")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	// 真 git 目录放在 `.git` 文件之外的另一处（worktree 形态）。
	gitDir := filepath.Join(root, "store", "worktrees", "wt")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/feat\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := "gitdir: " + filepath.ToSlash(gitDir) + "\n"
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := NewRepo().GitRepos(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	rels := relsOf(result.Repos)
	if len(rels) != 1 || rels[0] != "wt" {
		t.Fatalf("repos=%v, want [wt]", rels)
	}
	if result.Repos[0].Branch != "feat" {
		t.Fatalf("branch=%q, want feat", result.Repos[0].Branch)
	}
}

// 案例 5：忽略目录（node_modules/.venv/dist/tmp）与隐藏目录里的假仓库不出现。
func TestGitReposIgnoredAndHiddenDirsPruned(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"node_modules/pkg", ".venv/lib", "dist/app", "tmp/build", ".hidden/x", ".seelex/y"} {
		writeFakeRepo(t, filepath.Join(root, filepath.FromSlash(rel)), "main")
	}
	writeFakeRepo(t, filepath.Join(root, "src"), "main")

	result, err := NewRepo().GitRepos(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	rels := relsOf(result.Repos)
	if len(rels) != 1 || rels[0] != "src" {
		t.Fatalf("repos=%v, want [src] (ignored/hidden dirs pruned)", rels)
	}
}

// 案例 6：深度超限 → 深过上限的仓库不出现，且 Truncated=true 如实上报。
func TestGitReposDepthLimitTruncates(t *testing.T) {
	root := t.TempDir()
	deep := root
	for i := 0; i < gitReposMaxDepth+1; i++ {
		deep = filepath.Join(deep, "d")
	}
	writeFakeRepo(t, deep, "main")

	result, err := NewRepo().GitRepos(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Repos) != 0 {
		t.Fatalf("repos=%v, want none (deeper than max depth)", relsOf(result.Repos))
	}
	if !result.Truncated {
		t.Fatal("depth beyond limit must report Truncated=true")
	}
}

// 案例 7：条目上限钳制与截断——limit 生效、触顶报 Truncated；limit 入参按
// 1..256 口径收敛（<=0 用默认 64，>256 钳到 256）。
func TestGitReposLimitClampedAndTruncated(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		writeFakeRepo(t, filepath.Join(root, name), "main")
	}

	result, err := NewRepo().GitRepos(root, 2)
	if err != nil {
		t.Fatal(err)
	}
	rels := relsOf(result.Repos)
	if len(rels) != 2 || rels[0] != "a" || rels[1] != "b" {
		t.Fatalf("repos=%v, want [a b]", rels)
	}
	if !result.Truncated {
		t.Fatal("hitting the repo limit must report Truncated=true")
	}

	cases := map[int]int{
		0:                        gitReposDefaultLimit,
		-7:                       gitReposDefaultLimit,
		1:                        1,
		gitReposMaxLimit:         gitReposMaxLimit,
		gitReposMaxLimit + 10000: gitReposMaxLimit,
	}
	for input, want := range cases {
		if got := clampRepoLimit(input); got != want {
			t.Fatalf("clampRepoLimit(%d)=%d, want %d", input, got, want)
		}
	}
}

// 案例 8：空根 → 带 Error 文案的空结果，不报 Go error、不 panic。
func TestGitReposEmptyRootReportsErrorWithoutPanic(t *testing.T) {
	for _, root := range []string{"", "   "} {
		result, err := NewRepo().GitRepos(root, 0)
		if err != nil {
			t.Fatalf("GitRepos(%q) returned Go error: %v", root, err)
		}
		if result.Error == "" {
			t.Fatalf("GitRepos(%q) must carry a display message", root)
		}
		if len(result.Repos) != 0 {
			t.Fatalf("GitRepos(%q) repos=%v, want empty", root, result.Repos)
		}
	}
	// 不存在的根同样只给文案（删掉的工作区不该让面板崩）。
	missing := filepath.Join(t.TempDir(), "gone")
	result, err := NewRepo().GitRepos(missing, 0)
	if err != nil {
		t.Fatalf("missing root returned Go error: %v", err)
	}
	if result.Error == "" || len(result.Repos) != 0 {
		t.Fatalf("missing root result=%+v, want error payload with empty repos", result)
	}
}

// 案例 9（补充）：根不是仓库但落在祖先仓库里 → 合成一条 Rel="." Primary 记录
// （沿用"以工作区根为视角"的既有语义）。依赖真实 git，缺失则跳过。
func TestGitReposAncestorPrimary(t *testing.T) {
	requireGit(t)
	parent := t.TempDir()
	runGitIn(t, parent, "init", "-q", "-b", "trunk")
	nested := filepath.Join(parent, "inner", "area")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := NewRepo().GitRepos(nested, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error payload: %s", result.Error)
	}
	if len(result.Repos) != 1 || result.Repos[0].Rel != "." || !result.Repos[0].Primary {
		t.Fatalf("repos=%v, want single synthetic primary at \".\"", relsOf(result.Repos))
	}
	if result.Repos[0].Branch != "trunk" {
		t.Fatalf("branch=%q, want trunk", result.Repos[0].Branch)
	}
}

// 案例 10（补充）：分离头指针读不到分支名时不阻塞列表（Branch 留空）。
func TestGitReposDetachedHeadLeavesBranchEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := NewRepo().GitRepos(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Repos) != 1 || result.Repos[0].Branch != "" {
		t.Fatalf("repos=%+v, want one repo with empty branch", result.Repos)
	}
}
