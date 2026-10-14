package sessionstore

// project_board_test.go — 项目看板投影存储面验收（M1，T1.1–T1.11 + 落点搬迁）。
//
// 口径：投影是**派生数据**（可删可重建），所以这里既查"落得对"（信封 / 落点 /
// 追加 / 水位 / 幂等），也查"读得对"（折叠读是唯一读法），还查"不越界不乱写"
// （有界 / 空批不建文件 / 独立锁 / 删除绑定回收）。
//
// 落点（2026-10-15 搬迁）：`<项目根>/.seelex/board/{worktable,teamwork}.jsonl`
// + `<项目根>/.seelex/board/BOARD.md`（**项目目录**，不是数据根的 hash 目录）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// projectBoardRow 造一条投影行。
func projectBoardRow(kind, key string, seq uint64, removed bool) ProjectBoardRow {
	payload := map[string]any{"label": key}
	if removed {
		payload["removed"] = true
	}
	return ProjectBoardRow{
		Schema:  ProjectBoardSchema,
		Kind:    kind,
		Key:     key,
		Seq:     seq,
		At:      int64(seq),
		Payload: payload,
	}
}

// projectBoardFixture 提供一个 router（存储面宿主）与一个临时项目根。
//
// 落点已与数据根解耦：router 只提供方法宿主，项目根就是"看板该落在哪个项目里"。
func projectBoardFixture(t *testing.T) (*Router, string) {
	t.Helper()
	router := newLockProfileRouter(t, t.TempDir())
	return router, t.TempDir()
}

// projectBoardLineCount 数投影文件里的非空行数（-1 表示文件不存在）。
func projectBoardLineCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return -1
	}
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// TestProjectBoardAppendIdempotent 对应 T1.1：同一批行重复追加是幂等空操作。
func TestProjectBoardAppendIdempotent(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	rows := []ProjectBoardRow{
		projectBoardRow(ProjectBoardKindWork, "w1", 1, false),
		projectBoardRow(ProjectBoardKindWork, "w2", 2, false),
	}
	watermark, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile, rows)
	if err != nil {
		t.Fatalf("首次追加失败: %v", err)
	}
	if watermark != 2 {
		t.Fatalf("水位 = %d，期望 2", watermark)
	}
	path := projectBoardPath(projectRoot, ProjectBoardWorkFile)
	before := projectBoardLineCount(t, path)
	if before != 2 {
		t.Fatalf("落盘行数 = %d，期望 2", before)
	}
	again, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile, rows)
	if err != nil {
		t.Fatalf("重复追加报错: %v", err)
	}
	if again != watermark {
		t.Fatalf("重复追加水位 = %d，期望 %d（幂等）", again, watermark)
	}
	if after := projectBoardLineCount(t, path); after != before {
		t.Fatalf("重复追加把行写了两遍：%d → %d", before, after)
	}
}

// TestFoldProjectBoardRowsLatestWins 对应 T1.2：同一 (会话, Key) 取 Seq 最大的行。
func TestFoldProjectBoardRowsLatestWins(t *testing.T) {
	rows := []ProjectBoardRow{
		projectBoardRow(ProjectBoardKindWork, "k", 1, false),
		projectBoardRow(ProjectBoardKindWork, "k", 5, false),
		projectBoardRow(ProjectBoardKindWork, "k", 3, false),
	}
	folded := FoldProjectBoardRows(rows, nil)
	if len(folded) != 1 {
		t.Fatalf("折叠结果 = %d 行，期望 1", len(folded))
	}
	if folded[0].Seq != 5 {
		t.Fatalf("取胜行 Seq = %d，期望 5", folded[0].Seq)
	}
}

// TestFoldProjectBoardRowsKeepsCrossSessionKeys 是本轮新增的口径：落点是**项目级
// 平铺文件**，两个会话的 `milestone:M1` 不是同一条事实——折叠必须按 (会话, Key) 分桶。
func TestFoldProjectBoardRowsKeepsCrossSessionKeys(t *testing.T) {
	rows := []ProjectBoardRow{
		withBoardSession(projectBoardRow(ProjectBoardKindMilestone, "milestone:M1", 1, false), "session-a"),
		withBoardSession(projectBoardRow(ProjectBoardKindMilestone, "milestone:M1", 2, false), "session-b"),
	}
	folded := FoldProjectBoardRows(rows, nil)
	if len(folded) != 2 {
		t.Fatalf("折叠结果 = %d 行，期望 2（跨会话同名不是同一条）", len(folded))
	}
	if folded[0].Scope.Session != "session-a" || folded[1].Scope.Session != "session-b" {
		t.Fatalf("折叠结果会话位不对：%#v", folded)
	}
}

// withBoardSession 给一行盖会话章（测试用）。
func withBoardSession(row ProjectBoardRow, sessionID string) ProjectBoardRow {
	row.Scope = ProjectBoardScope{Session: sessionID}
	return row
}

// TestFoldProjectBoardRowsRemovedDropped 对应 tombstone：最新行 removed=true ⇒ 该 Key 消失。
func TestFoldProjectBoardRowsRemovedDropped(t *testing.T) {
	rows := []ProjectBoardRow{
		projectBoardRow(ProjectBoardKindWork, "gone", 1, false),
		projectBoardRow(ProjectBoardKindWork, "gone", 2, true),
		projectBoardRow(ProjectBoardKindWork, "kept", 3, false),
	}
	folded := FoldProjectBoardRows(rows, nil)
	if len(folded) != 1 || folded[0].Key != "kept" {
		t.Fatalf("折叠结果 = %#v，期望只剩 kept", folded)
	}
}

// TestFoldProjectBoardRowsUnknownSkipped 对应 T1.3：未知 schema / kind 跳过且记一行日志。
func TestFoldProjectBoardRowsUnknownSkipped(t *testing.T) {
	var logs []string
	rows := []ProjectBoardRow{
		{Schema: 99, Kind: ProjectBoardKindWork, Key: "bad-schema", Seq: 1},
		projectBoardRow("half-written-kind", "bad-kind", 1, false),
		projectBoardRow(ProjectBoardKindWork, "ok", 1, false),
	}
	folded := FoldProjectBoardRows(rows, func(format string, args ...any) {
		logs = append(logs, format)
	})
	if len(folded) != 1 || folded[0].Key != "ok" {
		t.Fatalf("折叠结果 = %#v，期望只剩 ok", folded)
	}
	if len(logs) != 2 {
		t.Fatalf("跳过日志条数 = %d，期望 2（未知 schema / 未知 kind 各一条）", len(logs))
	}
}

// TestProjectBoardCrashTailSelfHeals 对应 T1.4：残尾行丢弃、追加从完整行边界续写。
func TestProjectBoardCrashTailSelfHeals(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	if _, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile,
		[]ProjectBoardRow{projectBoardRow(ProjectBoardKindWork, "w1", 1, false)}); err != nil {
		t.Fatal(err)
	}
	path := projectBoardPath(projectRoot, ProjectBoardWorkFile)
	// 手工制造崩溃残尾：追加半行、不换行收尾。
	handle, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.WriteString(`{"schema":1,"kind":"work","key":"torn`); err != nil {
		handle.Close()
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	// 下一次追加必须先丢残尾、再从完整行边界续写。
	watermark, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile,
		[]ProjectBoardRow{projectBoardRow(ProjectBoardKindWork, "w2", 2, false)})
	if err != nil {
		t.Fatalf("残尾后续写失败: %v", err)
	}
	if watermark != 2 {
		t.Fatalf("水位 = %d，期望 2", watermark)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "torn") {
		t.Fatalf("残尾没有被丢弃：%s", data)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("文件没有以换行收尾：%q", data)
	}
	rows, err := router.ReadProjectBoardRows(projectRoot, "", ProjectBoardWorkFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("折叠读 = %d 行，期望 2", len(rows))
	}
}

// TestProjectBoardPathFollowsProjectRoot 对应 T1.5：路径钉在**项目目录**之下
// （`<项目根>/.seelex/board/`），而不是数据根的 hash 目录。
func TestProjectBoardPathFollowsProjectRoot(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	path := projectBoardPath(projectRoot, ProjectBoardWorkFile)
	want := filepath.Join(projectRoot, ".seelex", "board", ProjectBoardWorkFile)
	if path != want {
		t.Fatalf("路径 = %q，期望 %q", path, want)
	}
	if !strings.HasPrefix(path, projectRoot) {
		t.Fatalf("路径不在项目根之下：%q vs %q", path, projectRoot)
	}
	if _, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile,
		[]ProjectBoardRow{projectBoardRow(ProjectBoardKindWork, "w1", 1, false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("投影没有落在期望路径上：%v", err)
	}
}

// TestProjectBoardPathNoAbsoluteNoUser 对应 T1.6：行内不出现工作区 ID / 绝对路径 /
// 用户名；路径就是"项目根 + 固定两级目录"，会话 id 不再出现在路径里。
func TestProjectBoardPathNoAbsoluteNoUser(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	secretSession := "session-of-secret-user"
	path := projectBoardPath(projectRoot, ProjectBoardWorkFile)
	if strings.Contains(path, secretSession) {
		t.Fatalf("路径泄漏了会话字面量：%q", path)
	}
	relative, err := filepath.Rel(projectRoot, path)
	if err != nil {
		t.Fatal(err)
	}
	segments := strings.Split(filepath.ToSlash(relative), "/")
	if len(segments) != 3 || segments[0] != ".seelex" || segments[1] != "board" {
		t.Fatalf("路径不是 <项目根>/.seelex/board/<file>：%#v", segments)
	}
	// 行内也不得出现工作区 ID（ws-<nano>）与绝对路径。
	row := withBoardSession(projectBoardRow(ProjectBoardKindWork, "w1", 1, false), secretSession)
	if _, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile, []ProjectBoardRow{row}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ws-") || strings.Contains(string(data), projectRoot) {
		t.Fatalf("行内出现了工作区 ID / 绝对路径：%s", data)
	}
}

// TestProjectBoardLocksDistinct 对应 T1.7：投影用独立锁（不新增模块枚举、不与
// 模块锁共用）。
func TestProjectBoardLocksDistinct(t *testing.T) {
	lockA := projectBoardLock("p/a/worktable.jsonl")
	lockB := projectBoardLock("p/a/teamwork.jsonl")
	lockC := projectBoardLock("p/b/worktable.jsonl")
	if projectBoardLock("p/a/worktable.jsonl") != lockA {
		t.Fatal("同一路径应返回同一把锁")
	}
	if lockA == lockB || lockA == lockC || lockB == lockC {
		t.Fatal("不同投影路径必须各自一把锁")
	}
	// 不新增 storageModule 枚举：投影锁不得等于任何模块锁。
	store, key := messageFixture(t, 0)
	for _, mod := range allStorageModules() {
		if store.mu(key, mod) == lockA {
			t.Fatalf("投影锁与模块 %q 的锁是同一把", mod)
		}
	}
	// 行为面：持投影锁不得挡住 message 提交（不同串行点）。
	router, projectRoot := projectBoardFixture(t)
	path := projectBoardPath(projectRoot, ProjectBoardWorkFile)
	held := projectBoardLock(path)
	held.Lock()
	done := make(chan error, 1)
	go func() {
		done <- router.SaveCommitWorkspace("ws-lock", "session-lock", Commit{Events: []Event{
			{Role: "user", Content: "seed", MessageID: "seed"},
		}})
	}()
	select {
	case err := <-done:
		held.Unlock()
		if err != nil {
			t.Fatalf("持投影锁期间 message 提交失败: %v", err)
		}
	case <-time.After(5 * time.Second):
		held.Unlock()
		t.Fatal("持投影锁挡住了 message 提交——两把锁被串成了一个串行点")
	}
}

// TestProjectBoardRowsBounded 对应 T1.8：行数越界显式报错（不静默截断）。
func TestProjectBoardRowsBounded(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	rows := make([]ProjectBoardRow, 0, ProjectBoardMaxRows+1)
	for index := 0; index < ProjectBoardMaxRows+1; index++ {
		rows = append(rows, projectBoardRow(ProjectBoardKindWork, "w"+strings.Repeat("x", index+1), uint64(index+1), false))
	}
	if _, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile, rows); err == nil {
		t.Fatal("超过上限的批次应当显式报错")
	}
	path := projectBoardPath(projectRoot, ProjectBoardWorkFile)
	if count := projectBoardLineCount(t, path); count != -1 {
		t.Fatalf("越界批次仍然建了文件（行数 %d）——不许静默截断", count)
	}
}

// TestProjectBoardEmptyNoFile 对应 T1.9：空批不建文件（空则退场）。
func TestProjectBoardEmptyNoFile(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	watermark, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile, nil)
	if err != nil {
		t.Fatalf("空批追加报错: %v", err)
	}
	if watermark != 0 {
		t.Fatalf("空批水位 = %d，期望 0", watermark)
	}
	if _, err := os.Stat(ProjectBoardDir(projectRoot)); !os.IsNotExist(err) {
		t.Fatalf("空批不该建目录/文件：stat err = %v", err)
	}
	// 水位读也照常工作（文件不存在 ⇒ 0）。
	if got, err := router.ProjectBoardWatermark(projectRoot, ProjectBoardWorkFile); err != nil || got != 0 {
		t.Fatalf("水位 = %d, err = %v，期望 0, nil", got, err)
	}
}

// TestProjectBoardDeleteBindsSession 对应 T1.10：删除会话同批回收它在这个项目的行
// （其它会话的行原样保留）。
func TestProjectBoardDeleteBindsSession(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	const projectID = "ws-delete"
	router.SetProjectBoardRootFor(func(string) string { return projectRoot })
	for _, sessionID := range []string{"session-drop", "session-keep"} {
		if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: []Event{
			{Role: "user", Content: "seed", MessageID: "seed-" + sessionID},
		}}); err != nil {
			t.Fatal(err)
		}
		if _, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile,
			[]ProjectBoardRow{withBoardSession(projectBoardRow(ProjectBoardKindWork, "w-"+sessionID, 1, false), sessionID)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := router.DeleteWorkspace(projectID, "session-drop"); err != nil {
		t.Fatalf("删除会话失败: %v", err)
	}
	rows, err := router.ReadProjectBoardRows(projectRoot, "", ProjectBoardWorkFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Scope.Session != "session-keep" {
		t.Fatalf("删除后剩下的行 = %#v，期望只剩 session-keep", rows)
	}
	// 幂等：再回收一次不算错。
	if err := router.ReclaimProjectBoard(projectID, "session-drop"); err != nil {
		t.Fatalf("重复回收报错: %v", err)
	}
}

// TestProjectBoardReadProjectBoardAggregates 覆盖聚合读：按行数有界 + 可排除会话。
func TestProjectBoardReadProjectBoardAggregates(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	sessionA, sessionB := "session-a", "session-b"
	for _, sessionID := range []string{sessionA, sessionB} {
		for index := 1; index <= 2; index++ {
			if _, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile,
				[]ProjectBoardRow{withBoardSession(
					projectBoardRow(ProjectBoardKindWork, "w-"+sessionID+"-"+string(rune('0'+index)), uint64(index), false), sessionID)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	rows, truncated, err := router.ReadProjectBoard(projectRoot, ProjectBoardReadOptions{MaxRows: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("MaxRows=3 扫 4 行应当标注截断")
	}
	if len(rows) != 3 {
		t.Fatalf("聚合读 = %d 行，期望 3（截到 3 行）", len(rows))
	}
	rows, truncated, err = router.ReadProjectBoard(projectRoot, ProjectBoardReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("不设上限时不该标注截断")
	}
	if len(rows) != 4 {
		t.Fatalf("全量聚合读 = %d 行，期望 4", len(rows))
	}
	// 排除某会话：只剩另一个会话的两行。
	rows, _, err = router.ReadProjectBoard(projectRoot, ProjectBoardReadOptions{ExcludeSession: sessionA})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("排除 %s 后 = %d 行，期望 2", sessionA, len(rows))
	}
	for _, row := range rows {
		if row.Scope.Session != sessionB {
			t.Fatalf("ExcludeSession 没生效：%#v", row)
		}
	}
}

// TestProjectBoardMarkdownRenderedAndDeterministic 是新落点的**人读件**判据：
// BOARD.md 出现在 <项目根>/.seelex/board/ 下，确定性渲染（含里程碑与工作项），
// 且不含工作区 ID / 绝对路径 / 时刻。
func TestProjectBoardMarkdownRenderedAndDeterministic(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	rows := []ProjectBoardRow{
		withBoardSession(workRow(ProjectBoardKindWork, "work:w1", map[string]any{
			"id": "w1", "kind": "todo", "task": "把落点搬到项目目录", "status": "in_progress", "session_id": "sess-1",
		}), "sess-1"),
		withBoardSession(workRow(ProjectBoardKindMilestone, "milestone:M1", map[string]any{
			"id": "M1", "name": "落点", "status": "in_progress",
		}), "sess-1"),
		withBoardSession(workRow(ProjectBoardKindMilestoneItem, "milestone_item:wi-1", map[string]any{
			"id": "wi-1", "milestone": "M1", "role": "coder", "name": "参数化路径", "status": "running", "session_id": "sess-1",
		}), "sess-1"),
	}
	if _, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardWorkFile, rows[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := router.AppendProjectBoardRows(projectRoot, ProjectBoardTeamworkFile, rows[1:]); err != nil {
		t.Fatal(err)
	}
	path := projectBoardPath(projectRoot, ProjectBoardMarkdownFile)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("BOARD.md 没有落在 %s：%v", path, err)
	}
	for _, want := range []string{"# 项目看板", "把落点搬到项目目录", "### 里程碑 `M1`： 落点", "wi-1"} {
		if !strings.Contains(string(first), want) {
			t.Fatalf("BOARD.md 缺少 %q：\n%s", want, first)
		}
	}
	for _, forbidden := range []string{"ws-", projectRoot, "`at`"} {
		if strings.Contains(string(first), forbidden) {
			t.Fatalf("BOARD.md 含不该有的东西 %q：\n%s", forbidden, first)
		}
	}
	// 确定性：同一份行两次渲染字节相同。
	if second := ProjectBoardMarkdown(rows); string(second) != string(ProjectBoardMarkdown(rows)) {
		t.Fatal("同一份行两次渲染字节不同")
	}
}

// workRow 造一条带完整载荷的投影行（渲染用例用）。
func workRow(kind, key string, payload map[string]any) ProjectBoardRow {
	return ProjectBoardRow{
		Schema:  ProjectBoardSchema,
		Kind:    kind,
		Key:     key,
		Seq:     1,
		At:      1760000000,
		Payload: payload,
	}
}
