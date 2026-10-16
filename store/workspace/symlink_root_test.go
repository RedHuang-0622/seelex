package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// linkDirOrSkip 建一个指向 target 的目录链接，并确认它真能被 filepath.EvalSymlinks 解掉；
// 建不出来、或解不掉就跳过。
//
// 为什么要确认：Windows 上 os.Symlink 要特权 / 开发者模式，退化成 mklink /J 的 junction，
// 而 Go 的 EvalSymlinks 不解析 junction（Lstat 报 ModeIrregular，不当符号链接走），于是
// 本机造不出 macOS 那种"真符号链接祖先"，跳过而不是误报红。
func linkDirOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS != "windows" {
			t.Skipf("目录链接不可用，跳过: %v", err)
		}
		if out, jerr := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); jerr != nil {
			t.Skipf("目录链接不可用，跳过: %v %s", jerr, out)
		}
	}
	want, werr := filepath.EvalSymlinks(target)
	got, gerr := filepath.EvalSymlinks(link)
	if werr != nil || gerr != nil || filepath.Clean(got) != filepath.Clean(want) {
		t.Skipf("本机建不出 EvalSymlinks 可解的目录链接（Windows 无特权时得到的是 junction），跳过: %v", gerr)
	}
}

// 工作区根的祖先被符号链接过时，子树前缀仍要按真实路径算。
//
// macOS 的 t.TempDir() 就落在 /var/folders/...（→ /private/var/folders/...）里，而
// `git rev-parse --show-toplevel` 报的是真实路径：两边不归一，filepath.Rel 会算出一串
// ".."，工作区根被误判成"不在该仓库内"，剥前缀整个失效——CI 的 macOS 腿就是这么红的
// （期望 nest/leaf.txt，拿到 deep/nest/leaf.txt）。这条用例把同一形状显式造出来钉口径。
func TestRepoGitChangesSymlinkedWorkspaceRootStripsPrefix(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in test environment")
	}
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	repoRoot := filepath.Join(realRoot, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, "deep", "nest"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(base, "link")
	linkDirOrSkip(t, realRoot, linkRoot)

	runGitChanges(t, repoRoot, "init", "-q", "-b", "main")
	runGitChanges(t, repoRoot, "config", "user.email", "test@example.com")
	runGitChanges(t, repoRoot, "config", "user.name", "Test Dev")
	writeFile(t, repoRoot, "deep/nest/leaf.txt", "v1\n")
	runGitChanges(t, repoRoot, "add", "-A")
	runGitChanges(t, repoRoot, "commit", "-qm", "base")
	writeFile(t, repoRoot, "deep/nest/leaf.txt", "v2\n")

	// 工作区根走链接写法：realRoot/repo/deep 的链接写法是 base/link/repo/deep。
	viaLink := filepath.Join(linkRoot, "repo", "deep")
	result, err := NewRepo().GitChanges(viaLink, 0)
	if err != nil {
		t.Fatalf("GitChanges(%q): %v", viaLink, err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected exactly one change under the sub-root, got %+v", result.Entries)
	}
	if result.Entries[0].Path != "nest/leaf.txt" {
		t.Fatalf("symlinked workspace root must strip the repo prefix: got %q, want %q",
			result.Entries[0].Path, "nest/leaf.txt")
	}
}
