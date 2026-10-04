package adapters

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestSessionPortCompactionRecordsRoundTrip：压缩记录跨重启链路的**存储侧**验收。
//
// 现场：右栏「上下文压缩」只读 `record.Execution.Task.ContextCompactions`，而 v8/S20
// 的 record 通道停写停读 execution（见
// docs/research/2026-10-02-compaction-record-and-failure-trace-survey.md §4.2），于是
// 重启后「压缩帧在、压缩记录不在」。这条用例用真存储走完整条链：写快照（record 里
// 带压缩记录）→ 重建端口（模拟重启）→ 读回。
func TestSessionPortCompactionRecordsRoundTrip(t *testing.T) {
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })

	const projectID = "project-compaction-facts"
	const sessionID = "sess-compaction-facts"
	if err := router.SaveCommitWorkspace(projectID, sessionID, sessionstore.Commit{
		Events: []sessionstore.Event{{Seq: 1, Role: "user", Content: "seed"}},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	compactedAt := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	record := model.SessionRecord{
		Version: 3, ID: sessionID,
		Execution: model.SessionExecutionRecord{Task: &model.TaskState{
			ContextCompactions: []model.ContextCompaction{
				{
					Version: 3, Reason: "context_budget", Origin: model.CompactionOriginExplicit,
					EstimatedTokens: 190_000, CompactedAt: compactedAt,
					MessageFrom: "message-1", MessageTo: "message-88",
					EventFrom: 1, EventTo: 88, SegmentID: "compact-3",
					FrameRef: "compaction_frame:abc", FrameBytes: 2048, FrameTokens: 512,
				},
				{
					Version: 3, Reason: "context_budget", Origin: model.CompactionOriginAuto,
					Failed: true, Note: "no_model_summary estimated=281424 budget=163616 window=200000",
					CompactedAt: compactedAt.Add(time.Minute),
				},
			},
		}},
	}
	port := SessionPort{Manager: session.NewManager().WithRouter(router)}
	port.SetWorkspaceResolver(func(string) string { return projectID })
	if err := port.SaveSessionSnapshotWorkspace(projectID, sessionID, nil, record, nil, nil); err != nil {
		t.Fatalf("SaveSessionSnapshotWorkspace: %v", err)
	}
	// record 通道本身（v8）不承载压缩记录：这正是"为什么必须另开一条通道"的证据。
	loaded, err := port.granular().LoadRecordRaw(projectID, sessionID)
	if err != nil {
		t.Fatalf("LoadRecordRaw: %v", err)
	}
	if string(loaded) != "" && containsField(loaded, "context_compactions") {
		t.Fatalf("record 通道竟然承载了压缩记录（用例前提失效）：%s", loaded)
	}

	// ── 进程重启：同一份存储上重建端口 ────────────────────────────────────
	restarted := SessionPort{Manager: session.NewManager().WithRouter(router)}
	restarted.SetWorkspaceResolver(func(string) string { return projectID })
	facts, err := restarted.LoadCompactionRecordsWorkspace(projectID, sessionID)
	if err != nil {
		t.Fatalf("LoadCompactionRecordsWorkspace: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("重启后应读回 2 条压缩记录，实际 %d：%+v", len(facts), facts)
	}
	success := facts[0]
	if success.Version != 3 || success.EventTo != 88 || success.MessageTo != "message-88" ||
		success.FrameRef != "compaction_frame:abc" || success.FrameTokens != 512 ||
		!success.CompactedAt.Equal(compactedAt) {
		t.Fatalf("成功记录没有原样读回（前端右栏据此渲染区间/帧正文入口）：%+v", success)
	}
	failure := facts[1]
	if !failure.Failed || failure.Note == "" || failure.EventTo != 0 || failure.FrameRef != "" {
		t.Fatalf("失败痕没有原样读回（前端据此渲染失败行；它不是一次压缩）：%+v", failure)
	}

	// 重复提交同一份快照是幂等空操作（每次回合收尾都会写一次同一条记录列表）。
	if err := restarted.SaveSessionSnapshotWorkspace(projectID, sessionID, nil, record, nil, nil); err != nil {
		t.Fatalf("重复落盘: %v", err)
	}
	again, err := restarted.LoadCompactionRecordsWorkspace(projectID, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 2 {
		t.Fatalf("重复落盘把压缩记录写成了 %d 条（want 2）：%+v", len(again), again)
	}
}

func containsField(payload []byte, field string) bool {
	for index := 0; index+len(field) <= len(payload); index++ {
		if string(payload[index:index+len(field)]) == field {
			return true
		}
	}
	return false
}
