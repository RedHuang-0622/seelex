package store

// project_board_test.go — 项目看板投影存储面验收（M1，T1.1–T1.11 + 落点搬迁）。
//
// 口径：投影是**派生数据**（可删可重建），所以这里既查"落得对"（信封 / 落点 /
// 追加 / 水位 / 幂等），也查"读得对"（折叠读是唯一读法），还查"不越界不乱写"
// （有界 / 空批不建文件 / 独立锁 / 删除绑定回收）。
//
// 落点（2026-10-15 起）：`<项目根>/.seelex/board/<会话 ID>/{worktable,teamwork}.jsonl`
// **按会话分目录**（一个会话一份文件：防串写、幂等可判、回收即摘目录），
// 汇总人读件 `<项目根>/.seelex/board/BOARD.md` 与项目摘要在看板根下（不分会话）。
// 项目目录，不是数据根的 hash 目录。

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

// projectBoardTestPath 取某会话某投影文件的路径（走存储面**唯一**的带校验派生口；
// 用例不自己拼路径，否则"落点变了"这类回归在用例里读不出来）。
func projectBoardTestPath(t *testing.T, projectRoot, sessionID, file string) string {
	t.Helper()
	path, err := projectBoardSessionPath(projectRoot, sessionID, file)
	if err != nil {
		t.Fatalf("派生投影路径: %v", err)
	}
	return path
}

// TestProjectBoardAppendIdempotent 对应 T1.1：同一批行重复追加是幂等空操作。
func TestProjectBoardAppendIdempotent(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	const session = "session-a"
	rows := []ProjectBoardRow{
		withBoardSession(projectBoardRow(ProjectBoardKindWork, "w1", 1, false), session),
		withBoardSession(projectBoardRow(ProjectBoardKindWork, "w2", 2, false), session),
	}
	watermark, err := router.AppendProjectBoardRows(projectRoot, session, ProjectBoardWorkFile, rows)
	if err != nil {
		t.Fatalf("首次追加失败: %v", err)
	}
	if watermark != 2 {
		t.Fatalf("水位 = %d，期望 2", watermark)
	}
	path := projectBoardTestPath(t, projectRoot, session, ProjectBoardWorkFile)
	before := projectBoardLineCount(t, path)
	if before != 2 {
		t.Fatalf("落盘行数 = %d，期望 2", before)
	}
	again, err := router.AppendProjectBoardRows(projectRoot, session, ProjectBoardWorkFile, rows)
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
	const session = "session-a"
	if _, err := router.AppendProjectBoardRows(projectRoot, session, ProjectBoardWorkFile,
		[]ProjectBoardRow{withBoardSession(projectBoardRow(ProjectBoardKindWork, "w1", 1, false), session)}); err != nil {
		t.Fatal(err)
	}
	path := projectBoardTestPath(t, projectRoot, session, ProjectBoardWorkFile)
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
	watermark, err := router.AppendProjectBoardRows(projectRoot, session, ProjectBoardWorkFile,
		[]ProjectBoardRow{withBoardSession(projectBoardRow(ProjectBoardKindWork, "w2", 2, false), session)})
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
	rows, err := router.ReadProjectBoardRows(projectRoot, session, ProjectBoardWorkFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("折叠读 = %d 行，期望 2", len(rows))
	}
}

// TestProjectBoardPathFollowsProjectRoot 对应 T1.5：路径钉在**项目目录 + 会话目录**之下
// （`<项目根>/.seelex/board/<会话 ID>/`），而不是数据根的 hash 目录。
func TestProjectBoardPathFollowsProjectRoot(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	const session = "session-path"
	path := projectBoardTestPath(t, projectRoot, session, ProjectBoardWorkFile)
	want := filepath.Join(projectRoot, ".seelex", "board", session, ProjectBoardWorkFile)
	if path != want {
		t.Fatalf("路径 = %q，期望 %q", path, want)
	}
	if !strings.HasPrefix(path, projectRoot) {
		t.Fatalf("路径不在项目根之下：%q vs %q", path, projectRoot)
	}
	if _, err := router.AppendProjectBoardRows(projectRoot, session, ProjectBoardWorkFile,
		[]ProjectBoardRow{withBoardSession(projectBoardRow(ProjectBoardKindWork, "w1", 1, false), session)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("投影没有落在期望路径上：%v", err)
	}
	// 人读件与项目摘要留在**看板根**（不分会话）：它们是全项目汇总，不是某一会话的私产。
	if markdown := projectBoardMarkdownPath(projectRoot); strings.Contains(markdown, session) {
		t.Fatalf("BOARD.md 不该落在会话目录里：%q", markdown)
	}
}

// TestProjectBoardPathNoWorkspaceIDNoAbsolute 对应 T1.6：**行内**不出现工作区 ID
// （`ws-<nano>`）/ 绝对路径 —— 那两样才是"不可提交、不可归属"的来源。
//
// 路径里的会话 ID 是**有意**留的（用户口径：会话通过 sessionid 自维护自己的内容，
// 一个会话一个目录）。它既不是工作区 ID 也不是机器路径，产物照样可提交；
// 这里反过来钉住"目录名就是会话 ID"这条形状。
func TestProjectBoardPathNoWorkspaceIDNoAbsolute(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	secretSession := "session-of-secret-user"
	path := projectBoardTestPath(t, projectRoot, secretSession, ProjectBoardWorkFile)
	relative, err := filepath.Rel(projectRoot, path)
	if err != nil {
		t.Fatal(err)
	}
	segments := strings.Split(filepath.ToSlash(relative), "/")
	if len(segments) != 4 || segments[0] != ".seelex" || segments[1] != "board" || segments[2] != secretSession {
		t.Fatalf("路径不是 <项目根>/.seelex/board/<会话 ID>/<file>：%#v", segments)
	}
	if strings.Contains(filepath.ToSlash(relative), "ws-") {
		t.Fatalf("路径里出现了工作区 ID：%q", relative)
	}
	// 行内也不得出现工作区 ID（ws-<nano>）与绝对路径。
	row := withBoardSession(projectBoardRow(ProjectBoardKindWork, "w1", 1, false), secretSession)
	if _, err := router.AppendProjectBoardRows(projectRoot, secretSession, ProjectBoardWorkFile, []ProjectBoardRow{row}); err != nil {
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
	path := projectBoardTestPath(t, projectRoot, "session-lock", ProjectBoardWorkFile)
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
	const session = "session-bounded"
	rows := make([]ProjectBoardRow, 0, ProjectBoardMaxRows+1)
	for index := 0; index < ProjectBoardMaxRows+1; index++ {
		rows = append(rows, withBoardSession(
			projectBoardRow(ProjectBoardKindWork, "w"+strings.Repeat("x", index+1), uint64(index+1), false), session))
	}
	if _, err := router.AppendProjectBoardRows(projectRoot, session, ProjectBoardWorkFile, rows); err == nil {
		t.Fatal("超过上限的批次应当显式报错")
	}
	path := projectBoardTestPath(t, projectRoot, session, ProjectBoardWorkFile)
	if count := projectBoardLineCount(t, path); count != -1 {
		t.Fatalf("越界批次仍然建了文件（行数 %d）——不许静默截断", count)
	}
}

// TestProjectBoardEmptyNoFile 对应 T1.9：空批不建文件（空则退场）。
func TestProjectBoardEmptyNoFile(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	const session = "session-empty"
	watermark, err := router.AppendProjectBoardRows(projectRoot, session, ProjectBoardWorkFile, nil)
	if err != nil {
		t.Fatalf("空批追加报错: %v", err)
	}
	if watermark != 0 {
		t.Fatalf("空批水位 = %d，期望 0", watermark)
	}
	if _, err := os.Stat(ProjectBoardDir(projectRoot)); !os.IsNotExist(err) {
		t.Fatalf("空批不该建目录/文件：stat err = %v", err)
	}
	if _, err := os.Stat(ProjectBoardSessionDir(projectRoot, session)); !os.IsNotExist(err) {
		t.Fatalf("空批不该建会话目录：stat err = %v", err)
	}
	// 水位读也照常工作（文件不存在 ⇒ 0）。
	if got, err := router.ProjectBoardWatermark(projectRoot, session, ProjectBoardWorkFile); err != nil || got != 0 {
		t.Fatalf("水位 = %d, err = %v，期望 0, nil", got, err)
	}
}

// TestProjectBoardDeleteBindsSession 对应 T1.10：删除会话同批回收它**自己那个目录**
// （其它会话的目录与内容原样保留）。
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
		if _, err := router.AppendProjectBoardRows(projectRoot, sessionID, ProjectBoardWorkFile,
			[]ProjectBoardRow{withBoardSession(projectBoardRow(ProjectBoardKindWork, "w-"+sessionID, 1, false), sessionID)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := router.DeleteWorkspace(projectID, "session-drop"); err != nil {
		t.Fatalf("删除会话失败: %v", err)
	}
	if _, err := os.Stat(ProjectBoardSessionDir(projectRoot, "session-drop")); !os.IsNotExist(err) {
		t.Fatalf("被删会话的投影目录还在：stat err = %v", err)
	}
	rows, err := router.ReadProjectBoardRows(projectRoot, "session-keep", ProjectBoardWorkFile)
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

// TestProjectBoardReadProjectBoardAggregates 覆盖聚合读：逐会话目录读、按行数/会话数
// 有界、可排除会话。
func TestProjectBoardReadProjectBoardAggregates(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	sessionA, sessionB := "session-a", "session-b"
	for _, sessionID := range []string{sessionA, sessionB} {
		for index := 1; index <= 2; index++ {
			if _, err := router.AppendProjectBoardRows(projectRoot, sessionID, ProjectBoardWorkFile,
				[]ProjectBoardRow{withBoardSession(
					projectBoardRow(ProjectBoardKindWork, "w-"+sessionID+"-"+string(rune('0'+index)), uint64(index), false), sessionID)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// 一个会话一份文件：两份目录各自只有自己的行（防串写的最小判据）。
	for _, sessionID := range []string{sessionA, sessionB} {
		rows, err := router.ReadProjectBoardRows(projectRoot, sessionID, ProjectBoardWorkFile)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("会话 %s 自己的文件里 = %d 行，期望 2", sessionID, len(rows))
		}
		for _, row := range rows {
			if row.Scope.Session != sessionID {
				t.Fatalf("会话 %s 的文件里混进了 %s 的行：%#v", sessionID, row.Scope.Session, row)
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
	// 会话目录数也有界：只允许枚举 1 个会话 ⇒ 标注截断、只回一个会话的行。
	rows, truncated, err = router.ReadProjectBoard(projectRoot, ProjectBoardReadOptions{MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("MaxSessions=1 扫 2 个会话目录应当标注截断")
	}
	if len(rows) != 2 {
		t.Fatalf("只读 1 个会话 = %d 行，期望 2（另一个会话整份没读）", len(rows))
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
	if _, err := router.AppendProjectBoardRows(projectRoot, "sess-1", ProjectBoardWorkFile, rows[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := router.AppendProjectBoardRows(projectRoot, "sess-1", ProjectBoardTeamworkFile, rows[1:]); err != nil {
		t.Fatal(err)
	}
	path := projectBoardMarkdownPath(projectRoot)
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

// TestProjectBoardSessionsOwnTheirFiles 钉住"按会话分目录"要换来的三件事：
// **防串写**（两会话交错追加也不进同一个文件）、**幂等可判**（水位/批号按会话各自推进）、
// **会话自维护**（回收只摘自己那个目录）。
//
// 做法与现场同形：两个会话交替追加各自的全量行（同步器每轮就是"整份重盖自己的行"），
// 最后逐目录断言"这里面只有我的行"，并断言两个会话的水位互不影响。
func TestProjectBoardSessionsOwnTheirFiles(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	// 回收走"删会话"那条既有链路（`Router.DeleteWorkspace` → `ReclaimProjectBoard`）：
	// 它按注入的解析器取项目根，这里按生产装配注入一次。
	router.SetProjectBoardRootFor(func(string) string { return projectRoot })
	const sessionA, sessionB = "session-own-a", "session-own-b"
	for round := 1; round <= 6; round++ {
		for _, sessionID := range []string{sessionA, sessionB} {
			rows := []ProjectBoardRow{withBoardSession(
				projectBoardRow(ProjectBoardKindWork, "work:"+sessionID, uint64(round), false), sessionID)}
			watermark, err := router.AppendProjectBoardRows(projectRoot, sessionID, ProjectBoardWorkFile, rows)
			if err != nil {
				t.Fatalf("会话 %s 第 %d 轮追加失败: %v", sessionID, round, err)
			}
			if watermark != uint64(round) {
				t.Fatalf("会话 %s 第 %d 轮水位 = %d：水位必须按会话各自推进（两文件互不干扰）",
					sessionID, round, watermark)
			}
		}
	}
	for _, sessionID := range []string{sessionA, sessionB} {
		rows, err := router.ReadProjectBoardRows(projectRoot, sessionID, ProjectBoardWorkFile)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("会话 %s 折叠读 = %d 行，期望 1（本轮 value 已折叠到最新一版）", sessionID, len(rows))
		}
		if rows[0].Scope.Session != sessionID || rows[0].Key != "work:"+sessionID {
			t.Fatalf("会话 %s 的文件里是别人的行：%#v", sessionID, rows[0])
		}
		// 串写的硬判据：文件里逐行都只可能是这个会话的。
		raw, err := readProjectBoardFile(projectBoardTestPath(t, projectRoot, sessionID, ProjectBoardWorkFile))
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range raw {
			if row.Scope.Session != sessionID {
				t.Fatalf("会话 %s 的文件里混进了 %q 的行：%#v", sessionID, row.Scope.Session, row)
			}
		}
	}
	// 落点形状：一个会话一个目录，各持自己那份文件。
	entries, err := os.ReadDir(ProjectBoardDir(projectRoot))
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	if strings.Join(dirs, ",") != sessionA+","+sessionB {
		t.Fatalf("看板根下的会话目录 = %v，期望 [%s %s]", dirs, sessionA, sessionB)
	}
	// 回收只摘自己的目录：B 的目录与内容不受影响。
	if err := router.ReclaimProjectBoard("ws-own", sessionA); err != nil {
		t.Fatalf("回收会话 A 失败: %v", err)
	}
	if _, err := os.Stat(ProjectBoardSessionDir(projectRoot, sessionA)); !os.IsNotExist(err) {
		t.Fatalf("回收后 A 的目录还在：stat err = %v", err)
	}
	kept, err := router.ReadProjectBoardRows(projectRoot, sessionB, ProjectBoardWorkFile)
	if err != nil || len(kept) != 1 {
		t.Fatalf("回收 A 之后 B 的投影 = %d 行（err=%v），期望不受影响", len(kept), err)
	}
}

// TestProjectBoardRejectsForeignSessionRows：**串写防线**——一批行盖着别的会话的章，
// 存储面必须显式拒绝且一行都不落（否则就是"往别人的目录里塞行"）。
//
// 空章则相反：补成本目录的会话（调用方不必自己盖章）。
func TestProjectBoardRejectsForeignSessionRows(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	const session = "session-owner"
	foreign := []ProjectBoardRow{withBoardSession(projectBoardRow(ProjectBoardKindWork, "w1", 1, false), "session-other")}
	if _, err := router.AppendProjectBoardRows(projectRoot, session, ProjectBoardWorkFile, foreign); err == nil {
		t.Fatal("盖着别的会话章的批必须显式报错（拒绝串写）")
	}
	if _, err := os.Stat(ProjectBoardSessionDir(projectRoot, session)); !os.IsNotExist(err) {
		t.Fatalf("被拒的批仍然建了目录：stat err = %v", err)
	}
	// 空章 = 补成本目录的会话。
	stamped := []ProjectBoardRow{projectBoardRow(ProjectBoardKindWork, "w2", 1, false)}
	if _, err := router.AppendProjectBoardRows(projectRoot, session, ProjectBoardWorkFile, stamped); err != nil {
		t.Fatalf("空章批应当补成本会话的章: %v", err)
	}
	rows, err := router.ReadProjectBoardRows(projectRoot, session, ProjectBoardWorkFile)
	if err != nil || len(rows) != 1 || rows[0].Scope.Session != session {
		t.Fatalf("空章批落盘结果 = %#v（err=%v），期望归属 %q", rows, err, session)
	}
}

// TestProjectBoardSessionSegmentGuards：会话 ID 当目录名前的**唯一一道闸**。
//
// 会话 ID 从进程外流进来（配置 / RPC / 前端快照），直接当目录名就能用 `..`、`/`、盘符
// 逃出看板目录——那是"写别人的盘"。回收会 RemoveAll，所以这道闸必须先拦下。
func TestProjectBoardSessionSegmentGuards(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	// 注入项目根解析器，让回收那条路径真的走到校验闸（否则"没有落点"会先短路返回，
	// 这道闸就等于没被测到）。
	router.SetProjectBoardRootFor(func(string) string { return projectRoot })
	for _, bad := range []string{
		"", " ", ".", "..", "../escape", "a/b", `a\b`, "C:evil", "a:b", "a*b", "a?b", "a|b",
		"a<b", "a>b", `a"b`, "a\x00b", "trailing.", strings.Repeat("x", 129),
	} {
		rows := []ProjectBoardRow{withBoardSession(projectBoardRow(ProjectBoardKindWork, "w1", 1, false), bad)}
		if _, err := router.AppendProjectBoardRows(projectRoot, bad, ProjectBoardWorkFile, rows); err == nil {
			t.Fatalf("会话 ID %q 必须被拒绝（不能当目录名）", bad)
		}
		if _, err := router.ReadProjectBoardRows(projectRoot, bad, ProjectBoardWorkFile); err == nil {
			t.Fatalf("读路径也必须拒绝会话 ID %q", bad)
		}
		// 回收路径的危险输入才要求报错：空会话 = 没有东西可回收，短路返回是安全的；
		// 危险的是"看着有内容、却不是合法目录段"的那些（`..`、`/`、盘符……）。
		if strings.TrimSpace(bad) != "" {
			if err := router.ReclaimProjectBoard("ws-guard", bad); err == nil {
				t.Fatalf("回收路径必须拒绝会话 ID %q（它会 RemoveAll）", bad)
			}
		}
	}
	// 边界外的合法名照常可用（不是"一刀切拒绝"）。
	if _, err := router.AppendProjectBoardRows(projectRoot, "seelex-1792058182747766700-1", ProjectBoardWorkFile,
		[]ProjectBoardRow{withBoardSession(projectBoardRow(ProjectBoardKindWork, "w1", 1, false), "seelex-1792058182747766700-1")}); err != nil {
		t.Fatalf("正常会话 ID 应当可用: %v", err)
	}
	// 首尾空白走**归一化**（与全仓句柄口径一致），不是拒绝：`" s "` 就是会话 `s` 的目录。
	if got, err := sessionBoardSegment(" session-trim "); err != nil || got != "session-trim" {
		t.Fatalf("会话 ID 首尾空白应归一化，得到 (%q, %v)", got, err)
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

// TestProjectBoardKeepsUpdatingAfterBoundReached 是 R-1 的红灯用例：同一个项目文件在
// 内容**持续变化**的追加流下触顶后，投影必须继续跟上最新一版。
//
// 现场与同步器同形（`application/core/project_board.go` 的 flushFile）：每轮都是"当前
// 全量行 + Seq 从水位 +1 起"的整份重盖，所以行数按轮累加、迟早摸到 ProjectBoardMaxRows。
// 触顶后若只是"显式报错"，投影就**永久停在旧版本**——文件还在、人读件还在，读的人会当真，
// 比"什么都没有"更坏。判据因此写成"追加必须继续成功，且折叠读等于最新一版"。
func TestProjectBoardKeepsUpdatingAfterBoundReached(t *testing.T) {
	router, projectRoot := projectBoardFixture(t)
	const session = "session-flush"
	// 每轮两行 ⇒ 上限的一半多一点就触顶（不必真跑几十轮）。
	rounds := ProjectBoardMaxRows/2 + 2
	var watermark uint64
	for round := 1; round <= rounds; round++ {
		rows := []ProjectBoardRow{
			withBoardSession(projectBoardRow(ProjectBoardKindWork, "work:a", watermark+1, false), session),
			withBoardSession(projectBoardRow(ProjectBoardKindWork, "work:b", watermark+2, false), session),
		}
		rows[1].Payload["round"] = round // 内容每轮都变＝"模型又动了一下"
		// 中途删掉一个 Key（tombstone）：压实不许把它复活——否则"删掉的条目"会在触顶
		// 那一刻整批回来（upsert 语义下最隐蔽的一种回归）。它先真的存在过两轮，第 3 轮
		// 起才被删。
		rows = append(rows, withBoardSession(
			projectBoardRow(ProjectBoardKindWork, "work:gone", watermark+3, round >= 3), session))
		next, err := router.AppendProjectBoardRows(projectRoot, session, ProjectBoardWorkFile, rows)
		if err != nil {
			t.Fatalf("第 %d 轮追加失败（投影已经停止更新）：%v", round, err)
		}
		if next <= watermark {
			t.Fatalf("第 %d 轮水位没前进：%d → %d", round, watermark, next)
		}
		watermark = next
	}
	raw, err := router.ReadProjectBoardRows(projectRoot, session, ProjectBoardWorkFile)
	if err != nil {
		t.Fatal(err)
	}
	folded := FoldProjectBoardRows(raw, nil)
	if len(folded) != 2 {
		t.Fatalf("折叠读 = %d 行，期望 2（work:a / work:b）：%#v", len(folded), folded)
	}
	for _, row := range folded {
		if row.Payload["label"] != row.Key {
			t.Fatalf("折叠读拿到的是旧版本：%#v", row)
		}
		// payload 是从 JSONL 读回来的：数值字段是 float64。
		if row.Key == "work:b" {
			round, _ := row.Payload["round"].(float64)
			if int(round) != rounds {
				t.Fatalf("work:b 停在旧轮次 round=%v（want %d）——投影没跟上最新一版", row.Payload["round"], rounds)
			}
		}
	}
	// 有界仍然成立：压实不是"无界增长"的挡箭牌。
	if count := projectBoardLineCount(t, projectBoardTestPath(t, projectRoot, session, ProjectBoardWorkFile)); count > ProjectBoardMaxRows {
		t.Fatalf("文件行数 %d 超过上限 %d——压实没生效", count, ProjectBoardMaxRows)
	}
}
