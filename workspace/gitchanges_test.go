package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ── parseGitStatusOutput（纯函数）────────────────────────────
// 记录形态（NUL 分隔）："## branch" / "XY<space>path" / 重命名后紧跟 "<oldpath>"

// zRecords 把记录切片拼成 -z 的原始输出（每条记录以 NUL 结尾，含末尾那条）。
func zRecords(records ...string) []byte {
	var builder strings.Builder
	for _, record := range records {
		builder.WriteString(record)
		builder.WriteByte(0)
	}
	return []byte(builder.String())
}

func TestParseGitStatusOutputKindsAndRename(t *testing.T) {
	output := zRecords(
		"## main",
		" M a.txt",
		"A  staged-new.txt",
		"R  sub/renamed.txt",
		"sub/inner.txt",
		" D gone.txt",
		"?? untracked.txt",
		"UU conflict.txt",
		"T  linked",
		"C  copy-new.txt",
		"copy-old.txt",
	)
	parsed := parseGitStatusOutput(output, gitChangesScanBudget)
	if parsed.branch != "main" {
		t.Fatalf("branch = %q, want main", parsed.branch)
	}
	if parsed.truncated {
		t.Fatal("9 records with the full budget must not be truncated")
	}
	if len(parsed.entries) != 8 {
		t.Fatalf("expected 8 entries, got %d: %+v", len(parsed.entries), parsed.entries)
	}
	// 重命名是两条记录：新路径进 path、旧路径进 oldPath，且不额外产生一条。
	rename := parsed.entries[2]
	if rename.path != "sub/renamed.txt" || rename.oldPath != "sub/inner.txt" {
		t.Fatalf("rename must carry both paths: %+v", rename)
	}
	if rename.index != 'R' || rename.worktree != ' ' {
		t.Fatalf("rename XY must be preserved: %+v", rename)
	}
	copyEntry := parsed.entries[7]
	if copyEntry.path != "copy-new.txt" || copyEntry.oldPath != "copy-old.txt" {
		t.Fatalf("copy must carry both paths: %+v", copyEntry)
	}
}

func TestParseGitStatusOutputKeepsRawPaths(t *testing.T) {
	// -z 不做引号转义：含空格、中文、反斜杠的路径原样返回（前端不需要
	// 反解 C 风格转义；parcelain 的非 -z 形态在中文路径上会给出 \344\270 形式）。
	output := zRecords(
		"## No commits yet on main",
		"?? 中文 文件.txt",
		"?? dir with space/leaf.txt",
		"?? back\\slash.txt",
	)
	parsed := parseGitStatusOutput(output, gitChangesScanBudget)
	if parsed.branch != "main" {
		t.Fatalf("empty-repo branch header must be unwrapped, got %q", parsed.branch)
	}
	want := []string{"中文 文件.txt", "dir with space/leaf.txt", `back\slash.txt`}
	if len(parsed.entries) != len(want) {
		t.Fatalf("expected %d entries, got %+v", len(want), parsed.entries)
	}
	for index, path := range want {
		if parsed.entries[index].path != path {
			t.Fatalf("entry %d path = %q, want %q", index, parsed.entries[index].path, path)
		}
	}
}

func TestParseGitStatusOutputSkipsJunkAndTruncatesAtBudget(t *testing.T) {
	output := zRecords(
		"## main",
		"",
		"XY",                // 长度不足
		"MMnot-a-separator", // 第 3 字符不是空格
		"?? kept-1.txt",
		"?? kept-2.txt",
		"?? kept-3.txt",
	)
	parsed := parseGitStatusOutput(output, 2)
	if len(parsed.entries) != 2 {
		t.Fatalf("budget 2 must cap entries, got %+v", parsed.entries)
	}
	if !parsed.truncated {
		t.Fatal("hitting the budget must set truncated")
	}
	if parsed.entries[0].path != "kept-1.txt" || parsed.entries[1].path != "kept-2.txt" {
		t.Fatalf("unexpected entries: %+v", parsed.entries)
	}
}

func TestParseGitStatusOutputDropsDanglingRenameRecord(t *testing.T) {
	// 重命名缺旧路径记录（git 不会这样输出，输入被截断时才可能出现）：整条丢弃，
	// 不能把下一条改动行误吞成原路径、也不能造出半条改动。
	parsed := parseGitStatusOutput(zRecords("R  only-new.txt"), gitChangesScanBudget)
	if len(parsed.entries) != 0 {
		t.Fatalf("dangling rename must be dropped, got %+v", parsed.entries)
	}
	if parsed.truncated {
		t.Fatal("a dropped malformed record must not mark truncation")
	}
}

func TestParseBranchHeader(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"## main", "main"},
		{"## main...origin/main [ahead 1, behind 2]", "main"},
		{"## feature/tree-ui...origin/feature/tree-ui", "feature/tree-ui"},
		{"## No commits yet on main", "main"},
		{"## HEAD (no branch)", "HEAD"},
		{"## ", ""},
	}
	for _, testCase := range cases {
		if got := parseBranchHeader(testCase.line); got != testCase.want {
			t.Fatalf("parseBranchHeader(%q) = %q, want %q", testCase.line, got, testCase.want)
		}
	}
}

func TestClassifyChange(t *testing.T) {
	cases := []struct {
		index    byte
		worktree byte
		want     string
	}{
		{' ', 'M', "modified"},
		{'M', ' ', "modified"},
		{'M', 'M', "modified"}, // 暂存侧优先
		{'A', ' ', "added"},
		{'A', 'M', "added"},
		{' ', 'D', "deleted"},
		{'D', ' ', "deleted"},
		{'R', ' ', "renamed"},
		{'C', ' ', "copied"},
		{'T', ' ', "type_changed"},
		{' ', 'T', "type_changed"},
		{'?', '?', "untracked"},
		{'U', 'U', "conflicted"},
		{'U', 'A', "conflicted"},
		{'A', 'U', "conflicted"},
		{'A', 'A', "conflicted"},
		{'D', 'D', "conflicted"},
		{' ', ' ', ""}, // 无改动：不产生条目
		{'!', '!', ""}, // ignored（默认不请求，防御性）
	}
	for _, testCase := range cases {
		if got := classifyChange(testCase.index, testCase.worktree); got != testCase.want {
			t.Fatalf("classifyChange(%q,%q) = %q, want %q",
				string(testCase.index), string(testCase.worktree), got, testCase.want)
		}
	}
}

func TestRelativeToWorkspace(t *testing.T) {
	cases := []struct {
		path   string
		prefix string
		want   string
		ok     bool
	}{
		{"a.txt", "", "a.txt", true},
		{"sub/inner.txt", "", "sub/inner.txt", true},
		{"deep/nest/leaf.txt", "deep/", "nest/leaf.txt", true},
		{"DEEP/nest/leaf.txt", "deep/", "nest/leaf.txt", true}, // Windows 大小写不敏感
		{"deep/", "deep/", "", false},                          // 目录自身不是改动
		{"sibling.txt", "deep/", "", false},                    // 工作区之外的兄弟路径
		{"../escape.txt", "", "", false},
		{"/abs.txt", "", "", false},
		{"", "", "", false},
	}
	for _, testCase := range cases {
		got, ok := relativeToWorkspace(testCase.path, testCase.prefix)
		if got != testCase.want || ok != testCase.ok {
			t.Fatalf("relativeToWorkspace(%q,%q) = (%q,%v), want (%q,%v)",
				testCase.path, testCase.prefix, got, ok, testCase.want, testCase.ok)
		}
	}
}

func TestWorkspacePathPrefix(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		topLevel string
		root     string
		want     string
	}{
		{"", root, ""},
		{root, root, ""},
		{filepath.Dir(root), root, filepath.Base(root) + "/"},
		// 工作区根不在该仓库内：不剥前缀（由越界判定逐条丢弃）。
		{filepath.Join(root, "missing"), filepath.Join(root, "other"), ""},
	}
	for _, testCase := range cases {
		if got := workspacePathPrefix(testCase.topLevel, testCase.root); got != testCase.want {
			t.Fatalf("workspacePathPrefix(%q,%q) = %q, want %q",
				testCase.topLevel, testCase.root, got, testCase.want)
		}
	}
}

func TestIsSensitiveChangePath(t *testing.T) {
	sensitive := []string{
		"accounts.yaml",
		"config/accounts.yaml",
		"config/seelex.local.yaml",
		"deep/nest/ACCOUNTS.YAML",
	}
	for _, path := range sensitive {
		if !isSensitiveChangePath(path) {
			t.Fatalf("%q must be filtered as sensitive", path)
		}
	}
	// 目录噪音不在此过滤：被仓库跟踪的 dist/ 改动是真改动，必须展示。
	for _, path := range []string{"dist/bundle.js", "node_modules/pkg/index.js", "src/main.go"} {
		if isSensitiveChangePath(path) {
			t.Fatalf("%q must not be filtered as sensitive", path)
		}
	}
}

// ── Repo.GitChanges（集成；git 不可用时跳过）──────────────────

func TestRepoGitChangesIntegration(t *testing.T) {
	root := newChangesTestRepo(t)
	writeFile(t, root, "a.txt", "modified")
	writeFile(t, root, "staged-new.txt", "new")
	runGitChanges(t, root, "add", "staged-new.txt")
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatalf("remove gone.txt: %v", err)
	}
	runGitChanges(t, root, "mv", "sub/inner.txt", "sub/renamed.txt")
	writeFile(t, root, "未跟踪 中文.txt", "u")
	writeFile(t, root, "deep/nest/leaf.txt", "l")

	result, err := NewRepo().GitChanges(root, 0)
	if err != nil {
		t.Fatalf("GitChanges: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("GitChanges returned error payload: %s", result.Error)
	}
	if result.Branch == "" {
		t.Fatalf("branch missing: %+v", result)
	}
	byPath := map[string]int{}
	for index, entry := range result.Entries {
		byPath[entry.Path] = index
	}
	expectKind := map[string]string{
		"a.txt":              "modified",
		"staged-new.txt":     "added",
		"gone.txt":           "deleted",
		"sub/renamed.txt":    "renamed",
		"未跟踪 中文.txt":         "untracked",
		"deep/nest/leaf.txt": "untracked",
	}
	for path, kind := range expectKind {
		index, ok := byPath[path]
		if !ok {
			t.Fatalf("path %q missing from changes: %+v", path, result.Entries)
		}
		if result.Entries[index].Kind != kind {
			t.Fatalf("path %q kind = %q, want %q", path, result.Entries[index].Kind, kind)
		}
	}
	renameIndex := byPath["sub/renamed.txt"]
	if result.Entries[renameIndex].OldPath != "sub/inner.txt" {
		t.Fatalf("rename old path = %q", result.Entries[renameIndex].OldPath)
	}
	// 统计口径：暂存 2（新增 + 重命名）、未暂存 2（修改 + 删除）、未跟踪 2、冲突 0。
	if result.Total != len(result.Entries) {
		t.Fatalf("Total %d must equal entries %d", result.Total, len(result.Entries))
	}
	if result.Staged != 2 || result.Unstaged != 2 || result.Untracked != 2 || result.Conflicted != 0 {
		t.Fatalf("unexpected counts: %+v", result)
	}
	if result.Root == "" {
		t.Fatal("root missing")
	}
}

func TestRepoGitChangesFiltersSensitiveAndOutsideWorkspace(t *testing.T) {
	root := newChangesTestRepo(t)
	// 被仓库跟踪的敏感文件（未被 .gitignore 挡住）：展示层不能出现它的路径。
	writeFile(t, root, "accounts.yaml", "secret-v1")
	runGitChanges(t, root, "add", "accounts.yaml")
	runGitChanges(t, root, "-c", "user.name=T", "-c", "user.email=t@e.com", "commit", "-qm", "track config")
	writeFile(t, root, "accounts.yaml", "secret-v2")
	writeFile(t, root, "visible.txt", "v")

	// 工作区根取仓库子目录：只应看到该子树，且路径以工作区根为基准。
	subRoot := filepath.Join(root, "sub")
	writeFile(t, root, "sub/nested/leaf.txt", "n")

	result, err := NewRepo().GitChanges(subRoot, 0)
	if err != nil {
		t.Fatalf("GitChanges: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected only the sub-tree change, got %+v", result.Entries)
	}
	if result.Entries[0].Path != "nested/leaf.txt" {
		t.Fatalf("path must be relative to the workspace root, got %q", result.Entries[0].Path)
	}

	full, err := NewRepo().GitChanges(root, 0)
	if err != nil {
		t.Fatalf("GitChanges(root): %v", err)
	}
	for _, entry := range full.Entries {
		if strings.Contains(strings.ToLower(entry.Path), "accounts.yaml") {
			t.Fatalf("sensitive path leaked: %+v", entry)
		}
	}
	if full.Filtered != 1 {
		t.Fatalf("Filtered = %d, want 1 (the tracked accounts.yaml)", full.Filtered)
	}
}

func TestRepoGitChangesTruncatesAtLimit(t *testing.T) {
	root := newChangesTestRepo(t)
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		writeFile(t, root, name, "x")
	}
	result, err := NewRepo().GitChanges(root, 2)
	if err != nil {
		t.Fatalf("GitChanges: %v", err)
	}
	if len(result.Entries) != 2 || !result.Truncated {
		t.Fatalf("limit=2 must truncate: %+v", result)
	}
	// 统计是过滤后的全量（3 条未跟踪），不是本屏行数（2 条）。
	if result.Untracked != 3 || result.Total != 3 {
		t.Fatalf("counts must cover all kept entries: %+v", result)
	}
}

func TestRepoGitChangesRejectsNonGitDir(t *testing.T) {
	result, err := NewRepo().GitChanges(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("GitChanges should return Result.Error for non-git dir, got Go error: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected Error payload for non-git directory")
	}
}

func TestRepoGitChangesRejectsBadRoot(t *testing.T) {
	if _, err := NewRepo().GitChanges("", 0); err == nil {
		t.Fatal("empty root must be a Go error")
	}
	if _, err := NewRepo().GitChanges(filepath.Join(t.TempDir(), "missing"), 0); err == nil {
		t.Fatal("missing root must be a Go error")
	}
}

func TestRepoGitChangesClampsLimit(t *testing.T) {
	root := newChangesTestRepo(t)
	writeFile(t, root, "clamp.txt", "x")
	// limit <= 0 → 默认 200；超大 limit 钳制到 1000（不报错即可）。
	if result, err := NewRepo().GitChanges(root, 0); err != nil || result.Error != "" {
		t.Fatalf("limit=0 failed: err=%v payload=%s", err, result.Error)
	}
	if result, err := NewRepo().GitChanges(root, 99999); err != nil || result.Error != "" {
		t.Fatalf("large limit failed: err=%v payload=%s", err, result.Error)
	}
}

// newChangesTestRepo 建一个带一次提交的临时 git 仓库（含 sub/、gone.txt）。
func newChangesTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in test environment")
	}
	root := t.TempDir()
	runGitChanges(t, root, "init", "-q", "-b", "main")
	runGitChanges(t, root, "config", "user.email", "test@example.com")
	runGitChanges(t, root, "config", "user.name", "Test Dev")
	writeFile(t, root, "a.txt", "a")
	writeFile(t, root, "gone.txt", "g")
	writeFile(t, root, "sub/inner.txt", "s")
	runGitChanges(t, root, "add", "-A")
	runGitChanges(t, root, "commit", "-qm", "first")
	return root
}

// runGitChanges 在指定目录跑 git 子命令；失败即测试失败（不用 CombinedOutput 吞错）。
func runGitChanges(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func writeFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("mkdir for %s: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}
