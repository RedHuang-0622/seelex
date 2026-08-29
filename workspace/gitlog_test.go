package workspace

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// ── parseGitLogOutput（纯函数）──────────────────────────────
// 行格式：graph \x00 fullHash \x01 shortHash \x01 author \x01 date \x01 subject

func TestParseGitLogOutputCommitLines(t *testing.T) {
	output := "* \x00aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\x01abcdef1\x01Alice\x0108-29 10:00\x01feat: first\n" +
		"* \x00bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\x01abcdef2\x01Bob\x0108-29 11:30\x01fix: second"
	result := parseGitLogOutput(output, 20)
	if len(result.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(result.Lines))
	}
	if result.Lines[0].Graph != "*" {
		t.Fatalf("graph prefix: %q", result.Lines[0].Graph)
	}
	commit := result.Lines[0].Commit
	if commit == nil {
		t.Fatal("first line missing commit")
	}
	if commit.Hash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || commit.ShortHash != "abcdef1" ||
		commit.Author != "Alice" || commit.Date != "08-29 10:00" || commit.Subject != "feat: first" {
		t.Fatalf("unexpected commit: %+v", commit)
	}
	if len(result.Commits) != 2 || result.Commits[1].Author != "Bob" {
		t.Fatalf("unexpected commits: %+v", result.Commits)
	}
	if result.Truncated {
		t.Fatal("2 commits with limit 20 must not be truncated")
	}
}

func TestParseGitLogOutputContinuationLines(t *testing.T) {
	// merge 拓扑的延续线（无 \x00）：只含 graph 字符。
	output := "* \x00aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\x01abcdef1\x01Alice\x0108-29 10:00\x01merge branch\n" +
		"|\\\n" +
		"| * \x00bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\x01abcdef2\x01Bob\x0108-29 09:00\x01side work\n" +
		"|/\n" +
		"* \x00cccccccccccccccccccccccccccccccccccccccc\x01abcdef3\x01Carol\x0108-28 18:00\x01base"
	result := parseGitLogOutput(output, 20)
	if len(result.Lines) != 5 {
		t.Fatalf("expected 5 lines (3 commits + 2 continuation), got %d: %+v", len(result.Lines), result.Lines)
	}
	// 延续线保留 graph、无 commit。
	if result.Lines[1].Graph != "|\\" || result.Lines[1].Commit != nil {
		t.Fatalf("continuation line: %+v", result.Lines[1])
	}
	if result.Lines[3].Graph != "|/" || result.Lines[3].Commit != nil {
		t.Fatalf("continuation line: %+v", result.Lines[3])
	}
	if result.Lines[2].Commit == nil || result.Lines[2].Commit.Subject != "side work" {
		t.Fatalf("side work line: %+v", result.Lines[2])
	}
	if len(result.Commits) != 3 {
		t.Fatalf("expected 3 commits, got %d", len(result.Commits))
	}
}

func TestParseGitLogOutputTruncationAndJunk(t *testing.T) {
	// limit=2 截断；畸形行（字段不足、空 hash、纯空行）跳过。
	output := "\n" +
		"* \x00aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\x01abcdef1\x01Alice\x0108-29 10:00\x01one\n" +
		"* \x00bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\x01abcdef2\x01Bob\x0108-29 11:00\x01two\n" +
		"* \x00cccccccccccccccccccccccccccccccccccccccc\x01abcdef3\x01Carol\x0108-29 12:00\x01three\n" +
		"* \x00dddddddddddddddddddddddddddddddddddddddd\x01onlythreefields\n" +
		"* \x00\x00\x01\x01\x01\n"
	result := parseGitLogOutput(output, 2)
	if len(result.Commits) != 2 || result.Commits[0].Subject != "one" || result.Commits[1].Subject != "two" {
		t.Fatalf("expected first 2 commits, got %+v", result.Commits)
	}
	if !result.Truncated {
		t.Fatal("limit=2 with more commits must be truncated")
	}
	// 空行不产生行；畸形行不产生 commit。
	if len(result.Lines) != 2 {
		t.Fatalf("expected 2 parsed lines (junk skipped), got %d", len(result.Lines))
	}
}

func TestParseGitLogOutputGraphWhitespaceTrimmed(t *testing.T) {
	output := "*  \x00aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\x01abcdef1\x01Alice\x0108-29 10:00\x01padded graph"
	result := parseGitLogOutput(output, 20)
	if len(result.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(result.Lines))
	}
	// graph 前缀尾部空格去除（git --graph 在字段前会补一个空格）。
	if result.Lines[0].Graph != "*" {
		t.Fatalf("graph should be trimmed to %q, got %q", "*", result.Lines[0].Graph)
	}
}

// ── Repo.GitLog（集成；git 不可用时跳过）────────────────────

func TestRepoGitLogIntegration(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available in test environment")
	}
	_ = git
	repo := NewRepo()
	root := t.TempDir()
	if err := initTestGitRepo(root); err != nil {
		t.Skipf("cannot initialize git repo: %v", err)
	}
	result, err := repo.GitLog(root, 10)
	if err != nil {
		t.Fatalf("GitLog: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("GitLog returned error payload: %s", result.Error)
	}
	if len(result.Commits) < 2 {
		t.Fatalf("expected at least 2 commits, got %d: %+v", len(result.Commits), result.Commits)
	}
	if len(result.Lines) == 0 {
		t.Fatal("expected graph lines")
	}
	first := result.Commits[0]
	if first.Hash == "" || first.ShortHash == "" || first.Author == "" || first.Date == "" || first.Subject == "" {
		t.Fatalf("commit fields incomplete: %+v", first)
	}
	if result.Root == "" {
		t.Fatal("root missing")
	}
}

func TestRepoGitLogRejectsNonGitDir(t *testing.T) {
	repo := NewRepo()
	root := t.TempDir()
	result, err := repo.GitLog(root, 10)
	if err != nil {
		t.Fatalf("GitLog should return Result.Error for non-git dir, got Go error: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected Error payload for non-git directory")
	}
}

func TestRepoGitLogRejectsBadRoot(t *testing.T) {
	repo := NewRepo()
	if _, err := repo.GitLog("", 10); err == nil {
		t.Fatal("empty root must be a Go error")
	}
	if _, err := repo.GitLog(filepath.Join(t.TempDir(), "missing"), 10); err == nil {
		t.Fatal("missing root must be a Go error")
	}
}

func TestRepoGitLogClampsLimit(t *testing.T) {
	repo := NewRepo()
	root := t.TempDir()
	if err := initTestGitRepo(root); err != nil {
		t.Skipf("cannot initialize git repo: %v", err)
	}
	// limit=0 → 默认 20；超大 limit 钳制到 200（不报错即可）。
	if result, err := repo.GitLog(root, 0); err != nil || result.Error != "" {
		t.Fatalf("limit=0 failed: err=%v payload=%s", err, result.Error)
	}
	if result, err := repo.GitLog(root, 5000); err != nil || result.Error != "" {
		t.Fatalf("large limit failed: err=%v payload=%s", err, result.Error)
	}
}

// initTestGitRepo 在临时目录初始化 git 仓库并提交两个文件。
func initTestGitRepo(root string) error {
	run := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		_, err := cmd.CombinedOutput()
		return err
	}
	if err := run("init", "-q"); err != nil {
		return err
	}
	// 提交前设置本地身份（无全局 git config 的环境也能提交）。
	if err := run("config", "user.email", "test@example.com"); err != nil {
		return err
	}
	if err := run("config", "user.name", "Test Dev"); err != nil {
		return err
	}
	if err := run("commit", "--allow-empty", "-q", "-m", "first commit"); err != nil {
		return err
	}
	if err := run("commit", "--allow-empty", "-q", "-m", "second commit"); err != nil {
		return err
	}
	return nil
}
