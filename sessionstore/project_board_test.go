package sessionstore

// project_board_test.go — 项目看板投影存储面验收（M1，T1.1–T1.11）。
//
// 口径：投影是**派生数据**（可删可重建），所以这里既查"落得对"（信封 / 路径 /
// 追加 / 水位 / 幂等），也查"读得对"（折叠读是唯一读法），还查"不越界不乱写"
// （有界 / 空批不建文件 / 独立锁 / 删除绑定回收）。

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

// projectBoardFixture 提供一个指向 tempdir 的 router 与其 jsonRepository。
func projectBoardFixture(t *testing.T) (*Router, *jsonRepository, string) {
	t.Helper()
	router := newLockProfileRouter(t, t.TempDir())
	repository, ok := router.jsonRepository()
	if !ok {
		t.Fatal("router 不是 JSON 会话存储布局")
	}
	return router, repository, repository.root
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
	router, repository, _ := projectBoardFixture(t)
	const projectID, sessionID = "ws-board", "session-board"
	rows := []ProjectBoardRow{
		projectBoardRow(ProjectBoardKindWork, "w1", 1, false),
		projectBoardRow(ProjectBoardKindWork, "w2", 2, false),
	}
	watermark, err := router.AppendProjectBoardRows(projectID, sessionID, ProjectBoardWorkFile, rows)
	if err != nil {
		t.Fatalf("首次追加失败: %v", err)
	}
	if watermark != 2 {
		t.Fatalf("水位 = %d，期望 2", watermark)
	}
	path := repository.projectBoardPath(projectID, sessionID, ProjectBoardWorkFile)
	before := projectBoardLineCount(t, path)
	if before != 2 {
		t.Fatalf("落盘行数 = %d，期望 2", before)
	}
	again, err := router.AppendProjectBoardRows(projectID, sessionID, ProjectBoardWorkFile, rows)
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

// TestFoldProjectBoardRowsLatestWins 对应 T1.2：同一 Key 取 Seq 最大的行。
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
	router, repository, _ := projectBoardFixture(t)
	const projectID, sessionID = "ws-board", "session-crash"
	if _, err := router.AppendProjectBoardRows(projectID, sessionID, ProjectBoardWorkFile,
		[]ProjectBoardRow{projectBoardRow(ProjectBoardKindWork, "w1", 1, false)}); err != nil {
		t.Fatal(err)
	}
	path := repository.projectBoardPath(projectID, sessionID, ProjectBoardWorkFile)
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
	watermark, err := router.AppendProjectBoardRows(projectID, sessionID, ProjectBoardWorkFile,
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
	rows, err := router.ReadProjectBoardRows(projectID, sessionID, ProjectBoardWorkFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("折叠读 = %d 行，期望 2", len(rows))
	}
}

// TestProjectBoardPathFollowsStoreRoot 对应 T1.5：路径钉在既有数据根之下。
func TestProjectBoardPathFollowsStoreRoot(t *testing.T) {
	router, repository, root := projectBoardFixture(t)
	const projectID, sessionID = "ws-path", "session-path"
	path := repository.projectBoardPath(projectID, sessionID, ProjectBoardWorkFile)
	want := filepath.Join(root, "project-board", "project-"+hash(projectID), "session-"+hash(sessionID), ProjectBoardWorkFile)
	if path != want {
		t.Fatalf("路径 = %q，期望 %q", path, want)
	}
	if !strings.HasPrefix(path, root) {
		t.Fatalf("路径不在数据根之下：%q vs %q", path, root)
	}
	if _, err := router.AppendProjectBoardRows(projectID, sessionID, ProjectBoardWorkFile,
		[]ProjectBoardRow{projectBoardRow(ProjectBoardKindWork, "w1", 1, false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("投影没有落在期望路径上：%v", err)
	}
}

// TestProjectBoardPathNoAbsoluteNoUser 对应 T1.6：目录名走 hash，路径与行内
// 不得出现绝对路径 / 用户名。
func TestProjectBoardPathNoAbsoluteNoUser(t *testing.T) {
	router, repository, root := projectBoardFixture(t)
	// 故意用"看起来像绝对路径 / 带用户名"的字面量当 projectID / sessionID。
	projectID := filepath.Join(string(filepath.Separator)+"Users", "secret-user", "project")
	sessionID := "session-of-secret-user"
	path := repository.projectBoardPath(projectID, sessionID, ProjectBoardWorkFile)

	if strings.Contains(path, "secret-user") {
		t.Fatalf("路径泄漏了原字面量：%q", path)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	segments := strings.Split(filepath.ToSlash(relative), "/")
	if len(segments) != 4 {
		t.Fatalf("路径层级 = %#v，期望 [project-board project-<hash> session-<hash> file]", segments)
	}
	if segments[0] != "project-board" || segments[1] != "project-"+hash(projectID) || segments[2] != "session-"+hash(sessionID) {
		t.Fatalf("路径不是 hash 派生：%#v", segments)
	}
	for _, segment := range segments {
		if strings.Contains(segment, ":") || strings.Contains(segment, "Users") {
			t.Fatalf("路径段 %q 里出现了绝对路径痕迹", segment)
		}
	}
	// 行内也不得出现绝对路径 / 用户名。
	if _, err := router.AppendProjectBoardRows(projectID, sessionID, ProjectBoardWorkFile,
		[]ProjectBoardRow{projectBoardRow(ProjectBoardKindWork, "w1", 1, false)}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-user") || strings.Contains(string(data), root) {
		t.Fatalf("行内出现了绝对路径 / 用户名：%s", data)
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
	router, repository, _ := projectBoardFixture(t)
	const projectID, sessionID = "ws-lock", "session-lock"
	path := repository.projectBoardPath(projectID, sessionID, ProjectBoardWorkFile)
	held := projectBoardLock(path)
	held.Lock()
	done := make(chan error, 1)
	go func() {
		done <- router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: []Event{
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
	router, repository, _ := projectBoardFixture(t)
	const projectID, sessionID = "ws-bounded", "session-bounded"
	rows := make([]ProjectBoardRow, 0, ProjectBoardMaxRows+1)
	for index := 0; index < ProjectBoardMaxRows+1; index++ {
		rows = append(rows, projectBoardRow(ProjectBoardKindWork, "w"+hash(string(rune(index))), uint64(index+1), false))
	}
	if _, err := router.AppendProjectBoardRows(projectID, sessionID, ProjectBoardWorkFile, rows); err == nil {
		t.Fatal("超过上限的批次应当显式报错")
	}
	path := repository.projectBoardPath(projectID, sessionID, ProjectBoardWorkFile)
	if count := projectBoardLineCount(t, path); count != -1 {
		t.Fatalf("越界批次仍然建了文件（行数 %d）——不许静默截断", count)
	}
}

// TestProjectBoardEmptyNoFile 对应 T1.9：空批不建文件（空则退场）。
func TestProjectBoardEmptyNoFile(t *testing.T) {
	router, repository, _ := projectBoardFixture(t)
	const projectID, sessionID = "ws-empty", "session-empty"
	watermark, err := router.AppendProjectBoardRows(projectID, sessionID, ProjectBoardWorkFile, nil)
	if err != nil {
		t.Fatalf("空批追加报错: %v", err)
	}
	if watermark != 0 {
		t.Fatalf("空批水位 = %d，期望 0", watermark)
	}
	path := repository.projectBoardPath(projectID, sessionID, ProjectBoardWorkFile)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("空批不该建文件：stat err = %v", err)
	}
	// 水位读也照常工作（文件不存在 ⇒ 0）。
	if got, err := router.ProjectBoardWatermark(projectID, sessionID, ProjectBoardWorkFile); err != nil || got != 0 {
		t.Fatalf("水位 = %d, err = %v，期望 0, nil", got, err)
	}
}

// TestProjectBoardDeleteBindsSession 对应 T1.10：删除会话同批回收投影。
func TestProjectBoardDeleteBindsSession(t *testing.T) {
	router, repository, _ := projectBoardFixture(t)
	const projectID, sessionID = "ws-delete", "session-delete"
	if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: []Event{
		{Role: "user", Content: "seed", MessageID: "seed"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := router.AppendProjectBoardRows(projectID, sessionID, ProjectBoardWorkFile,
		[]ProjectBoardRow{projectBoardRow(ProjectBoardKindWork, "w1", 1, false)}); err != nil {
		t.Fatal(err)
	}
	directory := repository.projectBoardSessionDir(projectID, sessionID)
	if _, err := os.Stat(directory); err != nil {
		t.Fatalf("投影目录未落盘：%v", err)
	}
	if err := router.DeleteWorkspace(projectID, sessionID); err != nil {
		t.Fatalf("删除会话失败: %v", err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("删除会话后投影目录仍在：stat err = %v", err)
	}
	// 幂等：再回收一次不算错。
	if err := router.ReclaimProjectBoard(projectID, sessionID); err != nil {
		t.Fatalf("重复回收报错: %v", err)
	}
}

// TestProjectBoardReadProjectBoardAggregates 覆盖聚合读：跨会话合并 + 有界标注。
func TestProjectBoardReadProjectBoardAggregates(t *testing.T) {
	router, _, _ := projectBoardFixture(t)
	const projectID = "ws-aggregate"
	sessionA, sessionB, sessionC := "session-a", "session-b", "session-c"
	for _, sessionID := range []string{sessionA, sessionB, sessionC} {
		if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: []Event{
			{Role: "user", Content: "seed", MessageID: "seed-" + sessionID},
		}}); err != nil {
			t.Fatal(err)
		}
		if _, err := router.AppendProjectBoardRows(projectID, sessionID, ProjectBoardWorkFile,
			[]ProjectBoardRow{projectBoardRow(ProjectBoardKindWork, "w-"+sessionID, 1, false)}); err != nil {
			t.Fatal(err)
		}
	}
	rows, truncated, err := router.ReadProjectBoard(projectID, ProjectBoardReadOptions{MaxSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("MaxSessions=2 扫 3 个会话应当标注截断")
	}
	if len(rows) != 2 {
		t.Fatalf("聚合读 = %d 行，期望 2（截到 2 个会话）", len(rows))
	}
	rows, truncated, err = router.ReadProjectBoard(projectID, ProjectBoardReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("不设上限时不该标注截断")
	}
	if len(rows) != 3 {
		t.Fatalf("全量聚合读 = %d 行，期望 3", len(rows))
	}
	// 排除某会话：只剩另外两个。
	rows, _, err = router.ReadProjectBoard(projectID, ProjectBoardReadOptions{ExcludeSession: sessionA})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Key == "w-"+sessionA {
			t.Fatalf("ExcludeSession 没生效：%#v", row)
		}
	}
}
