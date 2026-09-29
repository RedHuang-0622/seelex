package workspace

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ── parseGitNameStatus / parseGitNumstat（纯函数）────────────────────
//
// 样例字节取自真实 git（2.51）输出，不是手写想象：
//
//	git show --format= --name-status -z --find-renames --diff-merges=first-parent <hash> -- .
//	→ A\0added.txt\0M\0bin.dat\0R062\0old name.txt\0new name.txt\0D\0中文名.txt\0
//	git show --format= --numstat -z --find-renames --diff-merges=first-parent <hash> -- .
//	→ 2\t0\tadded.txt\0-\t-\tbin.dat\0 1\t0\t\0old name.txt\0new name.txt\0 0\t1\t中文名.txt\0

func TestParseGitNameStatusRecords(t *testing.T) {
	output := []byte("A\x00added.txt\x00" +
		"M\x00bin.dat\x00" +
		"R062\x00old name.txt\x00new name.txt\x00" +
		"C100\x00copy.txt\x00copy 2.txt\x00" +
		"D\x00\u4e2d\u6587\u540d.txt\x00")
	files, truncated := parseGitNameStatus(output, 100)
	if truncated {
		t.Fatal("5 records with budget 100 must not truncate")
	}
	if len(files) != 5 {
		t.Fatalf("expected 5 files, got %d: %+v", len(files), files)
	}
	if files[0].status != "A" || files[0].path != "added.txt" || files[0].oldPath != "" {
		t.Fatalf("unexpected added record: %+v", files[0])
	}
	// 重命名：原路径在前、新路径在后（-z 实测顺序，不能颠倒）。
	if files[2].status != "R062" || files[2].oldPath != "old name.txt" || files[2].path != "new name.txt" {
		t.Fatalf("unexpected rename record: %+v", files[2])
	}
	if files[3].status != "C100" || files[3].oldPath != "copy.txt" || files[3].path != "copy 2.txt" {
		t.Fatalf("unexpected copy record: %+v", files[3])
	}
	if files[4].status != "D" || files[4].path != "\u4e2d\u6587\u540d.txt" {
		t.Fatalf("unexpected delete record: %+v", files[4])
	}
}

func TestParseGitNameStatusJunkAndBudget(t *testing.T) {
	// 半条重命名（缺新路径）整条丢弃，并且**不带偏后续记录**：原路径已被它占用，
	// 一起跳过。空记录忽略。
	output := []byte("R100\x00old.txt\x00" +
		"\x00" +
		"D\x00gone.txt\x00" +
		"M\x00kept.txt\x00")
	files, _ := parseGitNameStatus(output, 100)
	if len(files) != 2 || files[0].status != "D" || files[0].path != "gone.txt" ||
		files[1].status != "M" || files[1].path != "kept.txt" {
		t.Fatalf("dangling rename must not desync the parser: %+v", files)
	}

	// 结尾缺路径的状态不产生半条改动。
	trailing, _ := parseGitNameStatus([]byte("M\x00a.txt\x00D\x00"), 100)
	if len(trailing) != 1 || trailing[0].path != "a.txt" {
		t.Fatalf("trailing status without path must be dropped: %+v", trailing)
	}

	// 预算截断：3 条记录、预算 2 → 前 2 条 + truncated。
	budget := []byte("M\x00a.txt\x00M\x00b.txt\x00M\x00c.txt\x00")
	capped, truncated := parseGitNameStatus(budget, 2)
	if !truncated || len(capped) != 2 || capped[1].path != "b.txt" {
		t.Fatalf("budget not applied: truncated=%v files=%+v", truncated, capped)
	}
}

func TestParseGitNumstatRecords(t *testing.T) {
	output := []byte("2\t0\tadded.txt\x00" +
		"-\t-\tbin.dat\x00" +
		"1\t0\t\x00old name.txt\x00new name.txt\x00" +
		"0\t1\t\u4e2d\u6587\u540d.txt\x00")
	stats, truncated := parseGitNumstat(output, 100)
	if truncated {
		t.Fatal("4 records with budget 100 must not truncate")
	}
	if len(stats) != 4 {
		t.Fatalf("expected 4 stat records, got %d: %+v", len(stats), stats)
	}
	if stat := stats["added.txt"]; stat.additions != 2 || stat.deletions != 0 || stat.binary {
		t.Fatalf("unexpected added stat: %+v", stat)
	}
	// 二进制：git 不给行数（- -），标记 binary 且计数保持 0（不臆造）。
	if stat := stats["bin.dat"]; !stat.binary || stat.additions != 0 || stat.deletions != 0 {
		t.Fatalf("unexpected binary stat: %+v", stat)
	}
	// 重命名按**新路径**登记（与 name-status 对齐），行数落在这一条上。
	if stat := stats["new name.txt"]; stat.additions != 1 || stat.binary {
		t.Fatalf("rename stat must be keyed by the new path: %+v", stats)
	}
	if stat := stats["\u4e2d\u6587\u540d.txt"]; stat.deletions != 1 || stat.additions != 0 {
		t.Fatalf("unexpected delete stat: %+v", stat)
	}
}

func TestParseGitNumstatJunk(t *testing.T) {
	output := []byte("x\ty\tbad.txt\x00" + // 计数不是数字
		"1\t0\x00" + // 字段不足
		"-\t2\tmixed.txt\x00" + // 半二进制（git 不会这么写）也按畸形丢弃
		"3\t4\tok.txt\x00")
	stats, _ := parseGitNumstat(output, 100)
	if len(stats) != 1 {
		t.Fatalf("junk records must be dropped, got %+v", stats)
	}
	if stat := stats["ok.txt"]; stat.additions != 3 || stat.deletions != 4 {
		t.Fatalf("unexpected stat: %+v", stat)
	}
}

func TestParseGitNumstatBudget(t *testing.T) {
	output := []byte("1\t0\ta.txt\x001\t0\tb.txt\x001\t0\tc.txt\x00")
	stats, truncated := parseGitNumstat(output, 2)
	if !truncated || len(stats) != 2 {
		t.Fatalf("budget not applied: truncated=%v stats=%+v", truncated, stats)
	}
}

func TestParseGitCommitFields(t *testing.T) {
	hash := strings.Repeat("a", 40)
	commit, ok := parseGitCommitFields(hash + "\x01abc1234\x01Alice\x0108-29 10:00\x01\x01fix \x01 odd")
	if !ok {
		t.Fatal("well-formed line must parse")
	}
	if commit.Hash != hash || commit.ShortHash != "abc1234" || commit.Author != "Alice" ||
		commit.Date != "08-29 10:00" || commit.Subject != "fix \x01 odd" || len(commit.Parents) != 0 {
		t.Fatalf("unexpected commit: %+v", commit)
	}
	if _, ok := parseGitCommitFields("onlythree\x01x\x01y"); ok {
		t.Fatal("short line must not parse")
	}
	if _, ok := parseGitCommitFields("\x01\x01\x01\x01\x01"); ok {
		t.Fatal("empty hash must not parse")
	}
}

func TestCommitFileKindLetters(t *testing.T) {
	cases := map[string]string{
		"M":    "modified",
		"A":    "added",
		"D":    "deleted",
		"R062": "renamed",
		"C100": "copied",
		"T":    "type_changed",
		// U 只出现在 `git status` 的 XY（未解决冲突）：一个已入库的提交里
		// 不存在这种状态，所以它落回空分类（不编造分类）。
		"U": "",
		"X": "",
		"":  "",
	}
	for status, want := range cases {
		if got := commitFileKind(status); got != want {
			t.Fatalf("commitFileKind(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestGitCommitHashPatternRejectsOptionShapes(t *testing.T) {
	for _, bad := range []string{"", "HEAD", "main~1", "--output=x", "-c", "abc", "zzzz", "abc:path"} {
		if gitCommitHashPattern.MatchString(bad) {
			t.Fatalf("%q must not pass hash validation", bad)
		}
	}
	for _, good := range []string{"abcd", "ABCD", strings.Repeat("f", 40), strings.Repeat("0", 64)} {
		if !gitCommitHashPattern.MatchString(good) {
			t.Fatalf("%q must pass hash validation", good)
		}
	}
}

// ── 受控历史的测试仓库 ─────────────────────────────────────────────

// gitCommitTestRepo 建一个最小 git 仓库并逐次提交，用来钉住"某个提交那一刻"的
// 事实（文件清单、行数、内容），不依赖调用方的系统 git 配置。
type gitCommitTestRepo struct {
	t    *testing.T
	root string
}

func newGitCommitTestRepo(t *testing.T) *gitCommitTestRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in test environment")
	}
	repo := &gitCommitTestRepo{t: t, root: t.TempDir()}
	repo.git("init", "-q", "-b", "main")
	repo.git("config", "user.email", "test@example.com")
	repo.git("config", "user.name", "Test Dev")
	// 行尾转换关掉：内容是断言对象，不能被 autocrlf 改写。
	repo.git("config", "core.autocrlf", "false")
	return repo
}

func (repo *gitCommitTestRepo) git(args ...string) string {
	repo.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo.root}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		repo.t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func (repo *gitCommitTestRepo) write(rel, content string) {
	repo.t.Helper()
	repo.writeBytes(rel, []byte(content))
}

func (repo *gitCommitTestRepo) writeBytes(rel string, content []byte) {
	repo.t.Helper()
	path := filepath.Join(repo.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		repo.t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		repo.t.Fatal(err)
	}
}

func (repo *gitCommitTestRepo) remove(rel string) {
	repo.t.Helper()
	if err := os.Remove(filepath.Join(repo.root, filepath.FromSlash(rel))); err != nil {
		repo.t.Fatal(err)
	}
}

func (repo *gitCommitTestRepo) rename(oldRel, newRel string) {
	repo.t.Helper()
	repo.git("mv", oldRel, newRel)
}

func (repo *gitCommitTestRepo) commit(message string) string {
	repo.t.Helper()
	repo.git("add", "-A")
	repo.git("commit", "-q", "-m", message)
	return repo.git("rev-parse", "HEAD")
}

// seedBaseCommit 写第一批文件并提交（这批文件的身份在下一个提交里被改/删/改名）。
func (repo *gitCommitTestRepo) seedBaseCommit() string {
	repo.write("keep.txt", "keep\n")
	repo.write("old name.txt", "rename me\n")
	repo.write("\u4e2d\u6587\u540d.txt", "\u4e2d\u6587\u5185\u5bb9\n")
	repo.writeBytes("bin.dat", []byte{0, 1, 2, 3, 0, 255})
	repo.write("deep/nest/leaf.txt", "leaf\n")
	repo.write("deep/sibling.txt", "sibling\n")
	repo.write("accounts.yaml", "token: base\n")
	return repo.commit("base")
}

// secondCommit 在基准之上做一次"什么都有"的提交：改 / 删 / 增 / 重命名（带内容
// 变化）/ 二进制改 / 敏感文件改。返回它的 hash。
func (repo *gitCommitTestRepo) secondCommit() string {
	repo.write("keep.txt", "keep\nchanged\nmore\n")
	repo.remove("\u4e2d\u6587\u540d.txt")
	repo.write("added.txt", "new file\nsecond line\n")
	repo.rename("old name.txt", "new name.txt")
	repo.write("new name.txt", "rename me\nextra\n")
	repo.writeBytes("bin.dat", []byte{9, 8, 7})
	repo.write("deep/nest/leaf.txt", "leaf\nleaf2\n")
	repo.write("accounts.yaml", "token: changed\n")
	return repo.commit("second: \u4e2d\u6587\u4e3b\u9898")
}

// ── Repo.GitCommitDetail（集成）────────────────────────────────────

func TestRepoGitCommitDetailIntegration(t *testing.T) {
	repo := NewRepo()
	fixture := newGitCommitTestRepo(t)
	fixture.seedBaseCommit()
	head := fixture.secondCommit()

	detail, err := repo.GitCommitDetail(fixture.root, head, 200)
	if err != nil {
		t.Fatalf("GitCommitDetail: %v", err)
	}
	if detail.Error != "" {
		t.Fatalf("unexpected error payload: %s", detail.Error)
	}
	if detail.Hash != head || detail.ShortHash == "" || detail.Author != "Test Dev" ||
		detail.Date == "" || detail.Subject != "second: \u4e2d\u6587\u4e3b\u9898" {
		t.Fatalf("commit header incomplete: %+v", detail)
	}
	if len(detail.Parents) != 1 {
		t.Fatalf("expected one parent: %+v", detail.Parents)
	}
	// accounts.yaml 是敏感文件名 → 不展示，但必须计入 Filtered（不静默）。
	if detail.Filtered != 1 {
		t.Fatalf("sensitive path must be filtered and counted: filtered=%d", detail.Filtered)
	}
	if detail.Total != 6 || len(detail.Files) != 6 {
		t.Fatalf("expected 6 files, got total=%d files=%+v", detail.Total, detail.Files)
	}
	if detail.Truncated {
		t.Fatal("6 files with limit 200 must not truncate")
	}

	// 顺序即 git 输出顺序（路径字节序）——前端按它渲染，不重排。
	gotOrder := make([]string, 0, len(detail.Files))
	for _, file := range detail.Files {
		gotOrder = append(gotOrder, file.Path)
	}
	wantOrder := []string{"added.txt", "bin.dat", "deep/nest/leaf.txt", "keep.txt", "new name.txt", "\u4e2d\u6587\u540d.txt"}
	if strings.Join(gotOrder, "|") != strings.Join(wantOrder, "|") {
		t.Fatalf("unexpected file order: %v", gotOrder)
	}

	byPath := make(map[string]dtoCommitFile, len(detail.Files))
	for _, file := range detail.Files {
		byPath[file.Path] = dtoCommitFile{
			kind: file.Kind, status: file.Status, letter: file.Letter, oldPath: file.OldPath,
			additions: file.Additions, deletions: file.Deletions, binary: file.Binary,
		}
	}
	if added := byPath["added.txt"]; added.kind != "added" || added.status != "A" || added.letter != "A" ||
		added.additions != 2 || added.deletions != 0 {
		t.Fatalf("unexpected added.txt: %+v", added)
	}
	if binary := byPath["bin.dat"]; binary.kind != "modified" || !binary.binary {
		t.Fatalf("binary change must be marked binary (git gives no line counts): %+v", binary)
	}
	if renamed := byPath["new name.txt"]; renamed.kind != "renamed" || renamed.letter != "R" ||
		renamed.oldPath != "old name.txt" || !strings.HasPrefix(renamed.status, "R") {
		t.Fatalf("unexpected rename entry: %+v", renamed)
	}
	if deleted := byPath["\u4e2d\u6587\u540d.txt"]; deleted.kind != "deleted" || deleted.deletions != 1 {
		t.Fatalf("unexpected delete entry: %+v", deleted)
	}
	if modified := byPath["keep.txt"]; modified.kind != "modified" || modified.additions != 2 {
		t.Fatalf("unexpected modify entry: %+v", modified)
	}
	if _, leaked := byPath["accounts.yaml"]; leaked {
		t.Fatal("sensitive path must not appear in the file list")
	}
}

func TestRepoGitCommitDetailRootCommitListsEverythingAdded(t *testing.T) {
	repo := NewRepo()
	fixture := newGitCommitTestRepo(t)
	base := fixture.seedBaseCommit()

	detail, err := repo.GitCommitDetail(fixture.root, base, 200)
	if err != nil || detail.Error != "" {
		t.Fatalf("GitCommitDetail(root commit): err=%v payload=%s", err, detail.Error)
	}
	if len(detail.Parents) != 0 {
		t.Fatalf("root commit must have no parents: %+v", detail.Parents)
	}
	if detail.Total != 6 || detail.Filtered != 1 {
		t.Fatalf("root commit must list every file as added (minus sensitive): total=%d filtered=%d", detail.Total, detail.Filtered)
	}
	for _, file := range detail.Files {
		if file.Kind != "added" || file.Letter != "A" {
			t.Fatalf("root commit entry must be added: %+v", file)
		}
	}
}

func TestRepoGitCommitDetailMergeUsesFirstParent(t *testing.T) {
	repo := NewRepo()
	fixture := newGitCommitTestRepo(t)
	base := fixture.seedBaseCommit()
	// 侧支加一个文件，回到 main 合并：没带 --diff-merges=first-parent 时 merge
	// 提交会给出**空清单**（git show 对合并提交默认不给差异）。
	fixture.git("checkout", "-q", "-b", "side", base)
	fixture.write("side.txt", "side\n")
	fixture.commit("side work")
	fixture.git("checkout", "-q", "main")
	fixture.git("merge", "-q", "--no-ff", "-m", "merge side", "side")

	detail, err := repo.GitCommitDetail(fixture.root, fixture.git("rev-parse", "HEAD"), 200)
	if err != nil || detail.Error != "" {
		t.Fatalf("GitCommitDetail(merge): err=%v payload=%s", err, detail.Error)
	}
	if len(detail.Parents) != 2 {
		t.Fatalf("merge commit must carry two parents: %+v", detail.Parents)
	}
	if detail.Total != 1 || len(detail.Files) != 1 || detail.Files[0].Path != "side.txt" {
		t.Fatalf("merge must diff against the first parent: total=%d files=%+v", detail.Total, detail.Files)
	}
}

func TestRepoGitCommitDetailSubdirWorkspaceRootStripsPrefix(t *testing.T) {
	repo := NewRepo()
	fixture := newGitCommitTestRepo(t)
	fixture.seedBaseCommit()
	head := fixture.secondCommit()

	// 工作区根 = 仓库的 deep/ 子树：git 侧 `-- .` 已把范围钉在子树内（同一仓库里
	// 兄弟目录的改动**根本不会出现**，因此不计入 Filtered），路径剥掉 deep/ 前缀。
	detail, err := repo.GitCommitDetail(filepath.Join(fixture.root, "deep"), head, 200)
	if err != nil || detail.Error != "" {
		t.Fatalf("GitCommitDetail(subdir): err=%v payload=%s", err, detail.Error)
	}
	if detail.Total != 1 || len(detail.Files) != 1 || detail.Files[0].Path != "nest/leaf.txt" {
		t.Fatalf("subdir workspace must strip the repo prefix: total=%d files=%+v", detail.Total, detail.Files)
	}
	if detail.Filtered != 0 {
		t.Fatalf("paths outside the subtree are scoped out by git, not filtered: filtered=%d", detail.Filtered)
	}
}

func TestRepoGitCommitDetailTruncatesAtLimit(t *testing.T) {
	repo := NewRepo()
	fixture := newGitCommitTestRepo(t)
	fixture.seedBaseCommit()
	head := fixture.secondCommit()

	detail, err := repo.GitCommitDetail(fixture.root, head, 2)
	if err != nil || detail.Error != "" {
		t.Fatalf("GitCommitDetail(limit=2): err=%v payload=%s", err, detail.Error)
	}
	if len(detail.Files) != 2 || !detail.Truncated {
		t.Fatalf("limit=2 must truncate the list: files=%d truncated=%v", len(detail.Files), detail.Truncated)
	}
	// 头部说的是"这个提交改了多少个文件"，不是"本屏列了几行"。
	if detail.Total != 6 {
		t.Fatalf("total must count the full filtered set: %d", detail.Total)
	}
}

func TestRepoGitCommitDetailRejectsBadInput(t *testing.T) {
	repo := NewRepo()
	fixture := newGitCommitTestRepo(t)
	head := fixture.seedBaseCommit()

	// hash 形状非法 = 调用方传错参数：Go error（不进 argv）。
	for _, bad := range []string{"", "HEAD", "--output=x", "not-a-hash"} {
		if _, err := repo.GitCommitDetail(fixture.root, bad, 20); err == nil {
			t.Fatalf("hash %q must be rejected before running git", bad)
		}
	}
	// 合法形状但不存在的提交 = 展示态（Result.Error），不中断调用。
	missing, err := repo.GitCommitDetail(fixture.root, strings.Repeat("a", 40), 20)
	if err != nil {
		t.Fatalf("missing commit must return Result.Error, got Go error: %v", err)
	}
	if missing.Error == "" {
		t.Fatal("missing commit must carry a display message")
	}
	// 非 git 仓库：同样是展示态。
	plain, err := repo.GitCommitDetail(t.TempDir(), head, 20)
	if err != nil {
		t.Fatalf("non-git dir must return Result.Error, got Go error: %v", err)
	}
	if plain.Error == "" {
		t.Fatal("non-git dir must carry a display message")
	}
	// root 本身不合法（空/不存在）= Go error。
	if _, err := repo.GitCommitDetail("", head, 20); err == nil {
		t.Fatal("empty root must be a Go error")
	}
	if _, err := repo.GitCommitDetail(filepath.Join(t.TempDir(), "missing"), head, 20); err == nil {
		t.Fatal("missing root must be a Go error")
	}
}

// ── Repo.GitCommitFileContent（集成）───────────────────────────────

func TestRepoGitCommitFileContentReadsObjectDatabase(t *testing.T) {
	repo := NewRepo()
	fixture := newGitCommitTestRepo(t)
	base := fixture.seedBaseCommit()
	head := fixture.secondCommit()

	// 磁盘上是新内容，但读的是 base 那一刻的对象：两者必须不同（证明读的不是工作区）。
	content, err := repo.GitCommitFileContent(fixture.root, base, "keep.txt", 0)
	if err != nil {
		t.Fatalf("GitCommitFileContent: %v", err)
	}
	if content.Name != "keep.txt" || content.Path != "keep.txt" || content.Truncated {
		t.Fatalf("unexpected content meta: %+v", content)
	}
	if got := decodeBase64(t, content.Base64); got != "keep\n" {
		t.Fatalf("must read the blob at that commit, got %q", got)
	}
	if content.Size != int64(len("keep\n")) || !content.TextLike {
		t.Fatalf("unexpected size/text-like: size=%d textLike=%v", content.Size, content.TextLike)
	}

	// 后来被删除的文件，在历史提交里仍可读（对象库不关心工作区现状）。
	deleted, err := repo.GitCommitFileContent(fixture.root, base, "\u4e2d\u6587\u540d.txt", 0)
	if err != nil {
		t.Fatalf("deleted-later file must stay readable at its commit: %v", err)
	}
	if got := decodeBase64(t, deleted.Base64); got != "\u4e2d\u6587\u5185\u5bb9\n" {
		t.Fatalf("unexpected historical content: %q", got)
	}

	// 重命名后的新路径在 HEAD 上可读，内容含改名时补写的那一行。
	renamed, err := repo.GitCommitFileContent(fixture.root, head, "new name.txt", 0)
	if err != nil {
		t.Fatalf("renamed file content: %v", err)
	}
	if got := decodeBase64(t, renamed.Base64); got != "rename me\nextra\n" {
		t.Fatalf("unexpected renamed content: %q", got)
	}

	// 二进制的字节原样带回（同一通道），并标记 text_like=false。
	binary, err := repo.GitCommitFileContent(fixture.root, base, "bin.dat", 0)
	if err != nil {
		t.Fatalf("binary content: %v", err)
	}
	if binary.TextLike {
		t.Fatal("binary blob must not be reported as text")
	}
	if raw := decodeBase64Bytes(t, binary.Base64); len(raw) != 6 || raw[0] != 0 || raw[5] != 255 {
		t.Fatalf("binary bytes must survive base64 round-trip: %v", raw)
	}
}

func TestRepoGitCommitFileContentTruncatesLargeBlob(t *testing.T) {
	repo := NewRepo()
	fixture := newGitCommitTestRepo(t)
	fixture.seedBaseCommit()
	big := make([]byte, 64<<10)
	for index := range big {
		big[index] = byte('a' + index%26)
	}
	fixture.writeBytes("big.txt", big)
	head := fixture.commit("big file")

	content, err := repo.GitCommitFileContent(fixture.root, head, "big.txt", 1024)
	if err != nil {
		t.Fatalf("GitCommitFileContent(big): %v", err)
	}
	if !content.Truncated || content.Size != int64(len(big)) || content.Limit != 1024 {
		t.Fatalf("unexpected truncation facts: size=%d truncated=%v limit=%d", content.Size, content.Truncated, content.Limit)
	}
	// 早停路径：读到的正好是前 limit 字节（不为读完整个对象而等待）。
	if raw := decodeBase64Bytes(t, content.Base64); len(raw) != 1024 {
		t.Fatalf("truncated read must return exactly limit bytes, got %d", len(raw))
	}
}

func TestRepoGitCommitFileContentRejectsNonFilesAndBadPaths(t *testing.T) {
	repo := NewRepo()
	fixture := newGitCommitTestRepo(t)
	fixture.seedBaseCommit()
	head := fixture.secondCommit()

	// 该提交里不存在的路径（提交内被删除）→ Go error，文案来自 git。
	if _, err := repo.GitCommitFileContent(fixture.root, head, "\u4e2d\u6587\u540d.txt", 0); err == nil {
		t.Fatal("path deleted in that commit must fail")
	}
	// 目录（tree 对象）不是文件：不能把目录清单当"文件内容"渲染。
	if _, err := repo.GitCommitFileContent(fixture.root, head, "deep", 0); err == nil {
		t.Fatal("directory path must be rejected")
	}
	// 与工作区读取同一条可见性边界：敏感文件名不给。
	if _, err := repo.GitCommitFileContent(fixture.root, head, "accounts.yaml", 0); err == nil {
		t.Fatal("sensitive file must be rejected")
	}
	// 逃逸、绝对路径、hash 形状非法都在进 git 之前拒绝。
	if _, err := repo.GitCommitFileContent(fixture.root, head, "../outside.txt", 0); err == nil {
		t.Fatal("escaping path must be rejected")
	}
	if _, err := repo.GitCommitFileContent(fixture.root, head, filepath.Join(fixture.root, "keep.txt"), 0); err == nil {
		t.Fatal("absolute path must be rejected")
	}
	if _, err := repo.GitCommitFileContent(fixture.root, "--output=x", "keep.txt", 0); err == nil {
		t.Fatal("option-shaped hash must be rejected")
	}
	// 非 git 仓库同样失败（Go error：这是读取失败，不是"展示态"）。
	if _, err := repo.GitCommitFileContent(t.TempDir(), head, "keep.txt", 0); err == nil {
		t.Fatal("non-git dir must fail")
	}
}

// dtoCommitFile 是断言用的扁平面（只留用例关心的几格）。
type dtoCommitFile struct {
	kind      string
	status    string
	letter    string
	oldPath   string
	additions int
	deletions int
	binary    bool
}

func decodeBase64(t *testing.T, encoded string) string {
	t.Helper()
	return string(decodeBase64Bytes(t, encoded))
}

func decodeBase64Bytes(t *testing.T, encoded string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	return raw
}
