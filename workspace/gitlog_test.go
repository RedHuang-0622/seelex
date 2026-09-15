package workspace

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ── parseGitLogOutput（纯函数）──────────────────────────────
// 行格式：hash \x01 shortHash \x01 author \x01 date \x01 parents \x01 subject

func TestParseGitLogOutputCommitLines(t *testing.T) {
	output := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\x01abcdef1\x01Alice\x0108-29 10:00\x01\x01feat: first\n" +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\x01abcdef2\x01Bob\x0108-29 11:30\x01aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\x01fix: second"
	result := parseGitLogOutput(output, 20)
	if len(result.Commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(result.Commits))
	}
	first := result.Commits[0]
	if first.Hash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || first.ShortHash != "abcdef1" ||
		first.Author != "Alice" || first.Date != "08-29 10:00" || first.Subject != "feat: first" {
		t.Fatalf("unexpected commit: %+v", first)
	}
	// 根提交（%P 为空）→ 无父提交，前端会画成泳道终点。
	if len(first.Parents) != 0 {
		t.Fatalf("root commit must have no parents: %+v", first.Parents)
	}
	// 父提交关系随提交下发（前端据此算泳道分叉）。
	if len(result.Commits[1].Parents) != 1 || result.Commits[1].Parents[0] != first.Hash {
		t.Fatalf("parent topology missing: %+v", result.Commits[1].Parents)
	}
	if result.Truncated {
		t.Fatal("2 commits with limit 20 must not be truncated")
	}
}

func TestParseGitLogOutputMergeKeepsParentOrder(t *testing.T) {
	// merge 提交的 %P 是空格分隔的多个父：顺序必须原样保留（首父决定泳道）。
	hash := strings.Repeat("a", 40)
	output := hash + "\x01abcdef1\x01Alice\x0108-29 10:00\x01" +
		strings.Repeat("b", 40) + " " + strings.Repeat("c", 40) + "\x01merge branch"
	result := parseGitLogOutput(output, 20)
	if len(result.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(result.Commits))
	}
	parents := result.Commits[0].Parents
	if len(parents) != 2 || parents[0] != strings.Repeat("b", 40) || parents[1] != strings.Repeat("c", 40) {
		t.Fatalf("unexpected parents: %+v", parents)
	}
}

func TestParseGitLogOutputKeepsSubjectSeparators(t *testing.T) {
	// 主题是最后一个字段：里面出现 \x01 也不能截断字段边界。
	output := strings.Repeat("a", 40) + "\x01abcdef1\x01Alice\x0108-29 10:00\x01\x01fix \x01 odd subject"
	result := parseGitLogOutput(output, 20)
	if len(result.Commits) != 1 || result.Commits[0].Subject != "fix \x01 odd subject" {
		t.Fatalf("subject with separators must survive: %+v", result.Commits)
	}
}

func TestParseGitLogOutputTruncationAndJunk(t *testing.T) {
	// limit=2 截断；畸形行（字段不足、空 hash、纯空行）跳过。
	line := func(hash, subject string) string {
		return hash + "\x01abcdef1\x01Alice\x0108-29 10:00\x01\x01" + subject + "\n"
	}
	output := "\n" +
		line(strings.Repeat("a", 40), "one") +
		line(strings.Repeat("b", 40), "two") +
		line(strings.Repeat("c", 40), "three") +
		"onlythreefields\x01x\x01y\n" +
		"\x01\x01\x01\x01\x01\n"
	result := parseGitLogOutput(output, 2)
	if len(result.Commits) != 2 || result.Commits[0].Subject != "one" || result.Commits[1].Subject != "two" {
		t.Fatalf("expected first 2 commits, got %+v", result.Commits)
	}
	if !result.Truncated {
		t.Fatal("limit=2 with more commits must be truncated")
	}
}

func TestSplitParentsIgnoresBlankSeparators(t *testing.T) {
	if parents := splitParents("   "); parents != nil {
		t.Fatalf("blank parents must be nil, got %+v", parents)
	}
	parents := splitParents("  " + strings.Repeat("a", 40) + "  " + strings.Repeat("b", 40) + " ")
	if len(parents) != 2 || parents[0] != strings.Repeat("a", 40) || parents[1] != strings.Repeat("b", 40) {
		t.Fatalf("unexpected parents: %+v", parents)
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
	first := result.Commits[0]
	if first.Hash == "" || first.ShortHash == "" || first.Author == "" || first.Date == "" || first.Subject == "" {
		t.Fatalf("commit fields incomplete: %+v", first)
	}
	// 拓扑事实来自 %P：最新提交有父，最老提交是根（无父）。
	if len(first.Parents) != 1 {
		t.Fatalf("newest commit must carry one parent: %+v", first.Parents)
	}
	last := result.Commits[len(result.Commits)-1]
	if len(last.Parents) != 0 {
		t.Fatalf("root commit must carry no parents: %+v", last.Parents)
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
