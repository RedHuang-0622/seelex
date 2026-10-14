package seelebridge

// project_board_read_test.go — 项目看板按需读取面（M4）的验收。
//
// 三件事：单会话折叠读（真存储面）、跨会话聚合的有界与截断传播（替身，只为钉住选项）、
// 边界显式报错（未知 file / 超上限 limit / 没有工作区绑定）。

import (
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

const projectBoardReadProject = "ws-read-test"

func projectBoardReadRouter(t *testing.T) *sessionstore.Router {
	t.Helper()
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatalf("new session router: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

func projectBoardReadRow(kind, key string, seq uint64, payload map[string]any) sessionstore.ProjectBoardRow {
	return sessionstore.ProjectBoardRow{
		Schema: sessionstore.ProjectBoardSchema, Kind: kind, Key: key, Seq: seq, At: int64(seq),
		Scope:   sessionstore.ProjectBoardScope{Project: projectBoardReadProject, Session: "session-a"},
		Payload: payload,
	}
}

// TestProjectBoardReadSingleSessionFoldsBothFiles：单会话读两份文件、折叠、稳定排序。
func TestProjectBoardReadSingleSessionFoldsBothFiles(t *testing.T) {
	router := projectBoardReadRouter(t)
	if _, err := router.AppendProjectBoardRows(projectBoardReadProject, "session-a", sessionstore.ProjectBoardWorkFile,
		[]sessionstore.ProjectBoardRow{
			projectBoardReadRow(sessionstore.ProjectBoardKindWork, "work:todo:1", 1, map[string]any{"task": "调研", "status": "doing"}),
			// 同一 key 的第二行：折叠读只保留 Seq 最大的一条。
			projectBoardReadRow(sessionstore.ProjectBoardKindWork, "work:todo:1", 2, map[string]any{"task": "调研", "status": "done"}),
			// tombstone：该 key 从结果里消失。
			projectBoardReadRow(sessionstore.ProjectBoardKindWork, "work:todo:2", 3, map[string]any{"removed": true}),
		}); err != nil {
		t.Fatalf("写 worktable 投影: %v", err)
	}
	if _, err := router.AppendProjectBoardRows(projectBoardReadProject, "session-a", sessionstore.ProjectBoardTeamworkFile,
		[]sessionstore.ProjectBoardRow{
			projectBoardReadRow(sessionstore.ProjectBoardKindMilestone, "milestone:m1", 4, map[string]any{"id": "m1", "status": "active"}),
			projectBoardReadRow(sessionstore.ProjectBoardKindMilestoneItem, "milestone:m1/item:wi-1", 5, map[string]any{"id": "wi-1", "roll": "core"}),
		}); err != nil {
		t.Fatalf("写 teamwork 投影: %v", err)
	}

	result, err := ReadProjectBoard(router, ProjectBoardReadRequest{Session: "session-a"}, projectBoardReadProject, "")
	if err != nil {
		t.Fatalf("ReadProjectBoard: %v", err)
	}
	if result.Truncated {
		t.Fatal("三行不该触发截断")
	}
	if len(result.Files) != 2 {
		t.Fatalf("缺省应当两份都读，得到 %v", result.Files)
	}
	want := []struct{ kind, key string }{
		{sessionstore.ProjectBoardKindMilestone, "milestone:m1"},
		{sessionstore.ProjectBoardKindMilestoneItem, "milestone:m1/item:wi-1"},
		{sessionstore.ProjectBoardKindWork, "work:todo:1"},
	}
	if len(result.Rows) != len(want) {
		t.Fatalf("行数 = %d，希望 %d（%+v）", len(result.Rows), len(want), result.Rows)
	}
	for index, expected := range want {
		if result.Rows[index].Kind != expected.kind || result.Rows[index].Key != expected.key {
			t.Fatalf("第 %d 行 = (%s, %s)，希望 (%s, %s)", index,
				result.Rows[index].Kind, result.Rows[index].Key, expected.kind, expected.key)
		}
	}
	// 折叠读取最新一版：状态是 done（不是第一行的 doing）。
	if status, ok := result.Rows[2].Payload["status"].(string); !ok || status != "done" {
		t.Fatalf("折叠读没取最新一版：%+v", result.Rows[2].Payload)
	}
}

// TestProjectBoardReadSingleFile：file 简名只读一份，且接受白名单文件名本身。
func TestProjectBoardReadSingleFile(t *testing.T) {
	router := projectBoardReadRouter(t)
	if _, err := router.AppendProjectBoardRows(projectBoardReadProject, "session-a", sessionstore.ProjectBoardWorkFile,
		[]sessionstore.ProjectBoardRow{
			projectBoardReadRow(sessionstore.ProjectBoardKindWork, "work:todo:1", 1, map[string]any{"task": "调研"}),
		}); err != nil {
		t.Fatalf("写 worktable 投影: %v", err)
	}
	for _, file := range []string{ProjectBoardReadFileWorktable, sessionstore.ProjectBoardWorkFile, " Worktable "} {
		result, err := ReadProjectBoard(router, ProjectBoardReadRequest{Session: "session-a", File: file}, projectBoardReadProject, "")
		if err != nil {
			t.Fatalf("file=%q: %v", file, err)
		}
		if len(result.Files) != 1 || result.Files[0] != sessionstore.ProjectBoardWorkFile {
			t.Fatalf("file=%q 归一化结果 = %v", file, result.Files)
		}
		if len(result.Rows) != 1 {
			t.Fatalf("file=%q 读到 %d 行，希望 1", file, len(result.Rows))
		}
	}
	// teamwork 那份是空的 ⇒ 空投影（不是错误，I6：空则退场）。
	result, err := ReadProjectBoard(router, ProjectBoardReadRequest{Session: "session-a", File: ProjectBoardReadFileTeamwork}, projectBoardReadProject, "")
	if err != nil {
		t.Fatalf("空投影不该报错: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Fatalf("空投影读到 %d 行", len(result.Rows))
	}
}

// TestProjectBoardReadRejectsOutOfBounds：边界**显式报错**（不静默截断/不静默降级）。
func TestProjectBoardReadRejectsOutOfBounds(t *testing.T) {
	router := projectBoardReadRouter(t)

	if _, err := ReadProjectBoard(router, ProjectBoardReadRequest{Session: "session-a", File: "not-a-file"}, projectBoardReadProject, ""); err == nil {
		t.Fatal("未知 file 必须显式报错")
	}
	if _, err := ReadProjectBoard(router, ProjectBoardReadRequest{Session: "session-a", Limit: ProjectBoardReadMaxLimit + 1}, projectBoardReadProject, ""); err == nil {
		t.Fatal("超过 limit 硬上限必须显式报错")
	}
	if _, err := ReadProjectBoard(router, ProjectBoardReadRequest{Session: "session-a"}, "", ""); err == nil {
		t.Fatal("没有工作区绑定必须显式报错")
	}
	if _, err := ReadProjectBoard(nil, ProjectBoardReadRequest{Session: "session-a"}, projectBoardReadProject, ""); err == nil {
		t.Fatal("读取面未装配必须显式报错")
	}
	// 缺省会话由调用方补：两边都空 ⇒ 显式报错，而不是读成"全项目"。
	if _, err := ReadProjectBoard(router, ProjectBoardReadRequest{}, projectBoardReadProject, ""); err == nil {
		t.Fatal("没有会话（也没缺省）必须显式报错")
	}
}

// countingReadReader 是只读面替身：记下聚合读收到的选项，返回可控的截断信号。
type countingReadReader struct {
	rows       []sessionstore.ProjectBoardRow
	truncated  bool
	options    []sessionstore.ProjectBoardReadOptions
	singleRead int
}

func (reader *countingReadReader) ReadProjectBoard(_ string, options sessionstore.ProjectBoardReadOptions) ([]sessionstore.ProjectBoardRow, bool, error) {
	reader.options = append(reader.options, options)
	return reader.rows, reader.truncated, nil
}

func (reader *countingReadReader) ReadProjectBoardRows(_, _, _ string) ([]sessionstore.ProjectBoardRow, error) {
	reader.singleRead++
	return nil, nil
}

// TestProjectBoardReadAggregateIsBounded：`session: all` 走聚合读，选项有界，
// 截断信号**原样上抛**（"读到的比在册的少"必须让人看见）。
func TestProjectBoardReadAggregateIsBounded(t *testing.T) {
	rows := []sessionstore.ProjectBoardRow{
		projectBoardReadRow(sessionstore.ProjectBoardKindWork, "work:a", 1, map[string]any{"task": "a"}),
		projectBoardReadRow(sessionstore.ProjectBoardKindWork, "work:b", 2, map[string]any{"task": "b"}),
	}
	reader := &countingReadReader{rows: rows, truncated: true}
	result, err := ReadProjectBoard(reader, ProjectBoardReadRequest{
		Session: ProjectBoardReadSessionAll, File: ProjectBoardReadFileWorktable,
	}, projectBoardReadProject, "")
	if err != nil {
		t.Fatalf("ReadProjectBoard: %v", err)
	}
	if reader.singleRead != 0 {
		t.Fatal("聚合读不该走单会话路径")
	}
	if len(reader.options) != 1 {
		t.Fatalf("聚合读调用次数 = %d，希望 1（只读一份文件时）", len(reader.options))
	}
	options := reader.options[0]
	if options.MaxSessions != ProjectBoardReadMaxSessions || options.MaxRows != ProjectBoardReadDefaultLimit {
		t.Fatalf("聚合读选项 = %+v，希望 MaxSessions=%d / MaxRows=%d",
			options, ProjectBoardReadMaxSessions, ProjectBoardReadDefaultLimit)
	}
	if !result.Truncated {
		t.Fatal("截断信号必须上抛（否则读侧会以为这就是全部）")
	}
	if len(result.Rows) != 2 {
		t.Fatalf("行数 = %d，希望 2", len(result.Rows))
	}

	// limit 生效：取一行 + 标注截断。
	reader = &countingReadReader{rows: rows}
	result, err = ReadProjectBoard(reader, ProjectBoardReadRequest{
		Session: ProjectBoardReadSessionAll, File: ProjectBoardReadFileWorktable, Limit: 1,
	}, projectBoardReadProject, "")
	if err != nil {
		t.Fatalf("ReadProjectBoard: %v", err)
	}
	if len(result.Rows) != 1 || !result.Truncated {
		t.Fatalf("limit=1 结果 = %d 行 / truncated=%v，希望 1 行且标截断", len(result.Rows), result.Truncated)
	}
	if reader.options[0].MaxRows != 1 {
		t.Fatalf("聚合读的 MaxRows = %d，希望 1", reader.options[0].MaxRows)
	}
}
