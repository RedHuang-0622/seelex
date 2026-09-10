package sessionstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
)

// TestRetiredChannelsAbsentAfterFullCommit 对应 T-DP-01/T-DP-02/T-DP-03
// （S11/S12/S13/S19/S20）：一次完整提交（message+EVENT+栈+compact+blob+
// checkpoint+record）后，metadata 白名单外文件与退役通道都不再产生；
// 压缩只有 compact.jsonl + metadata/compact.json 一处来源。
func TestRetiredChannelsAbsentAfterFullCommit(t *testing.T) {
	repository, err := Open(context.Background(), Config{Backend: BackendJSON, Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	router := &Router{repository: repository}
	router.SetWorkspace("project")
	key := Key{ProjectID: "project", SessionID: "session-gate"}

	rows := make([]Event, 0, 3)
	for index := 1; index <= 3; index++ {
		rows = append(rows, messageRow(uint64(index), "m-"+string(rune('0'+index)),
			"user", EventKindUserInput, "x"))
	}
	if err := repository.WriteCommit(context.Background(), key, Commit{
		Events:      rows,
		State:       []byte(`{"version":3,"id":"session-gate"}`),
		ToolResults: []ToolResult{{Ref: "result:1", Tool: "bash", Content: "out", Size: 3}},
	}); err != nil {
		t.Fatal(err)
	}
	store := NewSessionContextStore(router, key.SessionID)
	if err := store.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSystemPrompt("system"); err != nil {
		t.Fatal(err)
	}
	if err := store.PushGoal(GoalFrame{GoalID: "g-1", Title: "gate", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PushCompact(CompactFrame{
		SegmentID: "seg-gate", From: 0, To: 1, EventFrom: 1, EventTo: 2,
		Summary: "s", CompressedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendGoalAudit(GoalAuditEntry{Kind: "goal.begin", GoalID: "g-1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := NewCheckpointStore(router, "project").Save("plan:gate", &workplanTypes.Snapshot{
		NodeID: "start", Context: workplanTypes.NewWorkflowContext(),
		Status: workplanTypes.StatusRunning, Timestamp: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	sessionRoot := repository.(*jsonRepository).sessionDir(key)
	retired := []string{
		"history.json", "context.json", "state.json", "transcript.log", "manifest.json",
		"metadata/toolresult.json",
	}
	for _, name := range retired {
		if _, err := os.Stat(filepath.Join(sessionRoot, filepath.FromSlash(name))); err == nil {
			t.Fatalf("退役通道文件仍存在: %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(sessionRoot, "tool-results")); err == nil {
		t.Fatal("退役目录 tool-results/ 仍存在")
	}
	entries, err := os.ReadDir(filepath.Join(sessionRoot, "metadata"))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"guide.json": true, "message.json": true, "event.json": true, "compact.json": true,
		"stack_plan.json": true, "stack_task.json": true, "stack_goal.json": true,
		"subagent.json": true, "lifecycle.json": true, "retention.json": true,
		"system.json": true, "checkpoint.json": true, "media.json": true,
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("metadata 下出现目录（白名单外）: %s", entry.Name())
		}
		if !allowed[entry.Name()] {
			t.Fatalf("metadata 白名单外文件: %s", entry.Name())
		}
	}
	// T-DP-03：压缩唯一来源 = compact.jsonl + compact head。
	if _, err := os.Stat(filepath.Join(sessionRoot, "compact.jsonl")); err != nil {
		t.Fatalf("compact.jsonl missing: %v", err)
	}
	// 旧布局目录名不再产生。
	if strings.Contains(strings.Join(dirNames(t, sessionRoot), ","), "generation-") {
		t.Fatalf("generation-* 目录仍存在: %s", sessionRoot)
	}
}

func dirNames(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
