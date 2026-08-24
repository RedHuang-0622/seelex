package session

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// newTestNodeSessionStore 构造 JSON 后端节点会话记录存储。
func newTestNodeSessionStore(t *testing.T) *sessionstore.NodeSessionStore {
	t.Helper()
	router, err := sessionstore.NewRouter(filepath.Join(t.TempDir(), "session-storage.json"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return sessionstore.NewNodeSessionStore(router)
}

// waitForRecords 轮询等待主会话索引下出现 want 条子会话记录
// （SubagentSessions 命令异步投递，断言前需等 actor 落盘）。
func waitForRecords(t *testing.T, store *sessionstore.NodeSessionStore, mainID string, want int) []sessionstore.NodeSessionRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		records, err := store.List("", mainID)
		if err == nil && len(records) >= want {
			return records
		}
		time.Sleep(10 * time.Millisecond)
	}
	records, _ := store.List("", mainID)
	t.Fatalf("node session records = %d, want >= %d", len(records), want)
	return nil
}

// waitForRecordState 轮询等待首条记录满足 predicate（阶段/结果/终态落盘）。
func waitForRecordState(t *testing.T, store *sessionstore.NodeSessionStore, mainID string, predicate func(sessionstore.NodeSessionRecord) bool) sessionstore.NodeSessionRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		records, err := store.List("", mainID)
		if err == nil && len(records) > 0 && predicate(records[0]) {
			return records[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	records, _ := store.List("", mainID)
	t.Fatalf("node session record never reached state (records=%+v)", records)
	return sessionstore.NodeSessionRecord{}
}

// TestSubagentSessionsLifecyclePersistConclusionDelete 验证生命周期策略：
// 运行期落盘（<main>-<sub>.json）→ 结束结论回传 mainagent → 记录文件删除。
func TestSubagentSessionsLifecyclePersistConclusionDelete(t *testing.T) {
	nodeStore := newTestNodeSessionStore(t)
	var conclusions []sessionstore.NodeSessionRecord
	subagents := NewSubagentSessions(nil,
		WithNodeSessionStore(nodeStore),
		WithMainSessionID(func() string { return "sess_main" }),
		WithConclusionSink(func(mainID string, record sessionstore.NodeSessionRecord) {
			if mainID != "sess_main" {
				t.Fatalf("conclusion main session = %q, want sess_main", mainID)
			}
			conclusions = append(conclusions, record)
		}),
	)
	defer subagents.Close()

	sess := newTestSubagentSession(t, "node-abc")
	subagents.Register("n1", sess, "inspect scope")
	// Register 同步落盘：主会话索引可列举。
	records := waitForRecords(t, nodeStore, "sess_main", 1)
	if records[0].Status != "running" || records[0].SessionID != "node-abc" {
		t.Fatalf("record after register = %+v", records[0])
	}

	subagents.RecordStage("n1", model.NodeStageLog{Stage: model.NodeStageTurn, Preview: "turn-1"})
	subagents.RecordResult("n1", &model.NodeSemanticResult{
		SchemaVersion: model.NodeSemanticSchemaVersion,
		NodeID:        "n1", SessionID: "node-abc", Status: "completed",
		Summary: "ok", Findings: []string{"f1"},
	})
	subagents.NoteOutcome("n1", "done", "ok", "")

	// 运行期记录持续更新（阶段/结果/终态落盘）。
	records = waitForRecords(t, nodeStore, "sess_main", 1)
	_ = records
	record := waitForRecordState(t, nodeStore, "sess_main", func(record sessionstore.NodeSessionRecord) bool {
		return record.Status == "done" && len(record.StagesJSON) > 0 && len(record.ResultJSON) > 0
	})
	if record.Summary != "ok" {
		t.Fatalf("record summary = %q, want ok", record.Summary)
	}

	// 结束收敛：结论回传 mainagent，记录文件删除。
	subagents.Unregister("n1")
	if len(conclusions) != 1 {
		t.Fatalf("conclusion sink calls = %d, want 1", len(conclusions))
	}
	if conclusions[0].Status != "done" || conclusions[0].Summary != "ok" || len(conclusions[0].ResultJSON) == 0 {
		t.Fatalf("conclusion = %+v", conclusions[0])
	}
	if _, err := nodeStore.Load("", "sess_main", "node-abc"); !errors.Is(err, sessionstore.ErrNodeSessionNotFound) {
		t.Fatalf("record after unregister = %v, want ErrNodeSessionNotFound", err)
	}
	// 内存详情数据面保留（进程存活期内仍可读）。
	if _, ok := subagents.Conversation("n1"); !ok {
		t.Fatal("conversation must remain readable after unregister")
	}
}

// TestSubagentSessionsRestoreRebuildsDetail 验证重启后可从残留记录重建
// 详情数据面（History/上下文快照/阶段/结果）。
func TestSubagentSessionsRestoreRebuildsDetail(t *testing.T) {
	nodeStore := newTestNodeSessionStore(t)
	subagents := NewSubagentSessions(nil,
		WithNodeSessionStore(nodeStore),
		WithMainSessionID(func() string { return "sess_main" }),
	)
	sess := newTestSubagentSession(t, "node-xyz")
	subagents.Register("n2", sess, "goal-two")
	subagents.RecordStage("n2", model.NodeStageLog{Stage: model.NodeStageSpawn, Preview: "running"})
	subagents.RecordResult("n2", &model.NodeSemanticResult{
		SchemaVersion: model.NodeSemanticSchemaVersion,
		NodeID:        "n2", SessionID: "node-xyz", Status: "completed", Summary: "done-ok",
	})
	// 进程被杀（不 Unregister）：记录仍在。
	_ = waitForRecordState(t, nodeStore, "sess_main", func(record sessionstore.NodeSessionRecord) bool {
		return len(record.StagesJSON) > 0 && len(record.ResultJSON) > 0
	})
	records, err := nodeStore.List("", "sess_main")
	if err != nil || len(records) != 1 {
		t.Fatalf("records before restart = %d err=%v", len(records), err)
	}
	subagents.Close()

	// 模拟重启：新建 actor，Restore 后详情数据面可读。
	restored := NewSubagentSessions(nil)
	defer restored.Close()
	restored.Restore(records)
	if _, ok := restored.Conversation("n2"); !ok {
		t.Fatal("restored conversation must be readable (ok=true)")
	}
	if snap, ok := restored.ContextSnapshot("n2"); !ok || snap == nil {
		t.Fatalf("restored context snapshot ok=%v", ok)
	}
	if logs := restored.StageLogs("n2"); len(logs) == 0 {
		t.Fatal("restored stage logs must be readable")
	}
	if result := restored.Result("n2"); result == nil || result.Summary != "done-ok" {
		t.Fatalf("restored result = %+v", result)
	}
}

// TestSubagentSessionsPersistFailedOutcome 验证失败节点同样收敛：
// 结论含错误信息，记录删除。
func TestSubagentSessionsPersistFailedOutcome(t *testing.T) {
	nodeStore := newTestNodeSessionStore(t)
	var conclusions []sessionstore.NodeSessionRecord
	subagents := NewSubagentSessions(nil,
		WithNodeSessionStore(nodeStore),
		WithMainSessionID(func() string { return "sess_main" }),
		WithConclusionSink(func(_ string, record sessionstore.NodeSessionRecord) {
			conclusions = append(conclusions, record)
		}),
	)
	defer subagents.Close()

	sess := newTestSubagentSession(t, "node-fail")
	subagents.Register("n3", sess, "goal-fail")
	subagents.NoteOutcome("n3", "failed", "", "llm completion failed")
	subagents.Unregister("n3")
	if len(conclusions) != 1 || conclusions[0].Status != "failed" || conclusions[0].Error != "llm completion failed" {
		t.Fatalf("failed conclusion = %+v", conclusions)
	}
	if _, err := nodeStore.Load("", "sess_main", "node-fail"); !errors.Is(err, sessionstore.ErrNodeSessionNotFound) {
		t.Fatalf("failed record after unregister = %v, want ErrNodeSessionNotFound", err)
	}
}
