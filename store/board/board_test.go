package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

func boardRow(kind, key string, seq uint64, removed bool) dto.ProjectBoardRow {
	payload := map[string]any{"label": key}
	if removed {
		payload[removedField] = true
	}
	return dto.ProjectBoardRow{
		Schema: Schema, Kind: kind, Key: key, Seq: seq, At: int64(seq), Payload: payload,
	}
}

func withSession(row dto.ProjectBoardRow, sessionID string) dto.ProjectBoardRow {
	row.Scope = dto.ProjectBoardScope{Session: sessionID}
	return row
}

func lineCount(t *testing.T, path string) int {
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

func TestFoldLatestWinsAndDropsTombstones(t *testing.T) {
	rows := []dto.ProjectBoardRow{
		withSession(boardRow(KindWork, "same", 1, false), "a"),
		withSession(boardRow(KindWork, "same", 5, false), "a"),
		withSession(boardRow(KindWork, "same", 3, false), "b"),
		withSession(boardRow(KindWork, "gone", 1, false), "a"),
		withSession(boardRow(KindWork, "gone", 2, true), "a"),
	}
	folded := Fold(rows, nil)
	if len(folded) != 2 {
		t.Fatalf("折叠结果 = %d 行，want 2: %#v", len(folded), folded)
	}
	for _, row := range folded {
		if row.Key != "same" {
			t.Fatalf("tombstone 没有消失：%#v", row)
		}
	}
	if folded[0].Scope.Session != "a" || folded[0].Seq != 5 || folded[1].Scope.Session != "b" {
		t.Fatalf("跨会话折叠或最新行选择错误：%#v", folded)
	}
}

func TestFoldSkipsUnknownRows(t *testing.T) {
	var logs []string
	rows := []dto.ProjectBoardRow{
		{Schema: 99, Kind: KindWork, Key: "bad-schema", Seq: 1},
		boardRow("half-written-kind", "bad-kind", 1, false),
		boardRow(KindWork, "ok", 1, false),
	}
	folded := Fold(rows, func(format string, args ...any) { logs = append(logs, format) })
	if len(folded) != 1 || folded[0].Key != "ok" || len(logs) != 2 {
		t.Fatalf("未知行处理错误：rows=%#v logs=%d", folded, len(logs))
	}
}

func TestAppendIdempotentAndCrashTail(t *testing.T) {
	projectRoot := t.TempDir()
	const sessionID = "session-a"
	rows := []dto.ProjectBoardRow{
		withSession(boardRow(KindWork, "w1", 1, false), sessionID),
		withSession(boardRow(KindWork, "w2", 2, false), sessionID),
	}
	first, err := Append(projectRoot, sessionID, WorkFile, rows)
	if err != nil {
		t.Fatal(err)
	}
	if first != 2 {
		t.Fatalf("watermark=%d, want 2", first)
	}
	path, err := sessionPath(projectRoot, sessionID, WorkFile)
	if err != nil {
		t.Fatal(err)
	}
	if before := lineCount(t, path); before != 2 {
		t.Fatalf("line count=%d, want 2", before)
	}
	again, err := Append(projectRoot, sessionID, WorkFile, rows)
	if err != nil || again != first {
		t.Fatalf("idempotent append=(%d,%v), want (%d,nil)", again, err, first)
	}
	if after := lineCount(t, path); after != 2 {
		t.Fatalf("idempotent append wrote a duplicate: %d", after)
	}

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
	next, err := Append(projectRoot, sessionID, WorkFile, []dto.ProjectBoardRow{
		withSession(boardRow(KindWork, "w3", 3, false), sessionID),
	})
	if err != nil {
		t.Fatalf("append after crash tail: %v", err)
	}
	if next != 3 {
		t.Fatalf("watermark after crash tail=%d, want 3", next)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "torn") || !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("crash tail was not repaired: %q", data)
	}
}

func TestPathGuardsAndForeignSessionRejection(t *testing.T) {
	projectRoot := t.TempDir()
	path, err := sessionPath(projectRoot, " session-a ", WorkFile)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(projectRoot, ".seelex", "board", "session-a", WorkFile)
	if path != want {
		t.Fatalf("path=%q, want %q", path, want)
	}
	for _, bad := range []string{
		"", " ", ".", "..", "../escape", "a/b", `a\b`, "C:evil", "a:b", "a*b", "a?b", "a|b",
		"a<b", "a>b", `a"b`, "a\x00b", "trailing.", strings.Repeat("x", 129),
	} {
		if _, err := sessionPath(projectRoot, bad, WorkFile); err == nil {
			t.Fatalf("session ID %q must be rejected", bad)
		}
	}
	foreign := []dto.ProjectBoardRow{withSession(boardRow(KindWork, "w1", 1, false), "other")}
	if _, err := Append(projectRoot, "owner", WorkFile, foreign); err == nil {
		t.Fatal("foreign session row must be rejected")
	}
	if _, err := os.Stat(SessionDir(projectRoot, "owner")); !os.IsNotExist(err) {
		t.Fatalf("rejected batch created a directory: %v", err)
	}
	stamped := []dto.ProjectBoardRow{boardRow(KindWork, "w2", 1, false)}
	if _, err := Append(projectRoot, "owner", WorkFile, stamped); err != nil {
		t.Fatalf("empty session scope must be stamped: %v", err)
	}
	rows, err := ReadRows(projectRoot, "owner", WorkFile)
	if err != nil || len(rows) != 1 || rows[0].Scope.Session != "owner" {
		t.Fatalf("stamped rows=%#v err=%v", rows, err)
	}
}

func TestBoundsEmptyAndCompaction(t *testing.T) {
	projectRoot := t.TempDir()
	const sessionID = "session-bounded"
	if watermark, err := Append(projectRoot, sessionID, WorkFile, nil); err != nil || watermark != 0 {
		t.Fatalf("empty batch=(%d,%v), want (0,nil)", watermark, err)
	}
	if _, err := os.Stat(Dir(projectRoot)); !os.IsNotExist(err) {
		t.Fatalf("empty batch created board dir: %v", err)
	}
	tooMany := make([]dto.ProjectBoardRow, 0, MaxRows+1)
	for index := 0; index < MaxRows+1; index++ {
		tooMany = append(tooMany, withSession(
			boardRow(KindWork, "w"+strings.Repeat("x", index+1), uint64(index+1), false), sessionID))
	}
	if _, err := Append(projectRoot, sessionID, WorkFile, tooMany); err == nil {
		t.Fatal("oversized batch must fail explicitly")
	}
	if _, err := os.Stat(SessionDir(projectRoot, sessionID)); !os.IsNotExist(err) {
		t.Fatalf("oversized batch created a directory: %v", err)
	}

	rounds := MaxRows/2 + 2
	var current uint64
	for round := 1; round <= rounds; round++ {
		rows := []dto.ProjectBoardRow{
			withSession(boardRow(KindWork, "work:a", current+1, false), sessionID),
			withSession(boardRow(KindWork, "work:b", current+2, false), sessionID),
			withSession(boardRow(KindWork, "work:gone", current+3, round >= 3), sessionID),
		}
		rows[1].Payload["round"] = round
		next, err := Append(projectRoot, sessionID, WorkFile, rows)
		if err != nil {
			t.Fatalf("round %d append: %v", round, err)
		}
		if next <= current {
			t.Fatalf("watermark did not advance: %d -> %d", current, next)
		}
		current = next
	}
	rows, err := ReadRows(projectRoot, sessionID, WorkFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("folded rows=%d, want 2: %#v", len(rows), rows)
	}
	path, err := sessionPath(projectRoot, sessionID, WorkFile)
	if err != nil {
		t.Fatal(err)
	}
	if lines := lineCount(t, path); lines > MaxRows {
		t.Fatalf("compaction did not bound file: %d > %d", lines, MaxRows)
	}
}

func TestReadAggregatesAndBoundsSessions(t *testing.T) {
	projectRoot := t.TempDir()
	for _, sessionID := range []string{"session-a", "session-b"} {
		for index := 1; index <= 2; index++ {
			row := withSession(boardRow(KindWork, "w-"+sessionID+"-"+string(rune('0'+index)), uint64(index), false), sessionID)
			if _, err := Append(projectRoot, sessionID, WorkFile, []dto.ProjectBoardRow{row}); err != nil {
				t.Fatal(err)
			}
		}
	}
	rows, truncated, err := Read(projectRoot, dto.ProjectBoardReadOptions{MaxRows: 3})
	if err != nil || !truncated || len(rows) != 3 {
		t.Fatalf("bounded aggregate=(%d,%v,%v), want (3,true,nil)", len(rows), truncated, err)
	}
	rows, _, err = Read(projectRoot, dto.ProjectBoardReadOptions{ExcludeSession: "session-a"})
	if err != nil || len(rows) != 2 {
		t.Fatalf("excluded aggregate rows=%d err=%v", len(rows), err)
	}
	for _, row := range rows {
		if row.Scope.Session != "session-b" {
			t.Fatalf("exclude failed: %#v", row)
		}
	}
	rows, truncated, err = Read(projectRoot, dto.ProjectBoardReadOptions{MaxSessions: 1})
	if err != nil || !truncated || len(rows) != 2 {
		t.Fatalf("session bound=(%d,%v,%v), want (2,true,nil)", len(rows), truncated, err)
	}
}

func TestMarkdownDeterministicAndSanitized(t *testing.T) {
	rows := []dto.ProjectBoardRow{
		withSession(workRow(KindWork, "work:w1", map[string]any{
			"id": "w1", "kind": "todo", "task": "C:\\Users\\alice\\secret task", "status": "in_progress",
			"description": "ws-1792058182747766700 visible",
		}), "sess-1"),
		withSession(workRow(KindMilestone, "milestone:M1", map[string]any{
			"id": "M1", "name": "落点", "status": "in_progress",
		}), "sess-1"),
		withSession(workRow(KindMilestoneItem, "milestone_item:wi-1", map[string]any{
			"id": "wi-1", "milestone": "M1", "role": "coder", "name": "参数化路径",
			"status": "running", "session_id": "sess-1",
		}), "sess-1"),
	}
	first := Markdown(rows)
	second := Markdown(rows)
	if string(first) != string(second) {
		t.Fatal("same rows rendered different bytes")
	}
	text := string(first)
	for _, want := range []string{"# 项目看板", "### 里程碑 `M1`： 落点", "wi-1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("BOARD.md missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"C:\\Users", "ws-1792058182747766700", "`at`"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("BOARD.md contains %q:\n%s", forbidden, text)
		}
	}
}

func TestReclaimOnlyRemovesOneSessionAndMarkdownRefreshes(t *testing.T) {
	projectRoot := t.TempDir()
	for _, sessionID := range []string{"drop", "keep"} {
		row := withSession(boardRow(KindWork, "work:"+sessionID, 1, false), sessionID)
		row.Payload["task"] = "work:" + sessionID
		if _, err := Append(projectRoot, sessionID, WorkFile, []dto.ProjectBoardRow{row}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(Dir(projectRoot), MarkdownFile)); err != nil {
		t.Fatalf("BOARD.md missing: %v", err)
	}
	if err := Reclaim(projectRoot, "drop"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(SessionDir(projectRoot, "drop")); !os.IsNotExist(err) {
		t.Fatalf("drop directory remains: %v", err)
	}
	rows, err := ReadRows(projectRoot, "keep", WorkFile)
	if err != nil || len(rows) != 1 || rows[0].Scope.Session != "keep" {
		t.Fatalf("kept rows=%#v err=%v", rows, err)
	}
	markdown, err := ReadMarkdown(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(markdown), "work:drop") || !strings.Contains(string(markdown), "work:keep") {
		t.Fatalf("BOARD.md was not refreshed after reclaim:\n%s", markdown)
	}
}

func TestPathLocksHaveFileIdentity(t *testing.T) {
	lockA := Mutex("p/a/worktable.jsonl")
	lockB := Mutex("p/a/teamwork.jsonl")
	lockC := Mutex("p/b/worktable.jsonl")
	if Mutex("p/a/worktable.jsonl") != lockA {
		t.Fatal("same path returned different locks")
	}
	if lockA == lockB || lockA == lockC || lockB == lockC {
		t.Fatal("different paths shared one lock")
	}
}

func workRow(kind, key string, payload map[string]any) dto.ProjectBoardRow {
	return dto.ProjectBoardRow{
		Schema: Schema, Kind: kind, Key: key, Seq: 1, At: 1760000000, Payload: payload,
	}
}
