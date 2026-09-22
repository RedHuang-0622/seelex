package seelebridge

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// 复现（用户报告）：子代理判定有问题 —— "在会话 id 还是 draft 开头的时候诞生的
// subagent，在重启之后判定成不是当前会话的 subagent，也不能很好地从 active 归档"。
//
// 机制（持久化事实，不用内存 active 标记）：
//
//	子代理（node）记录的落盘键 = (projectID, mainSessionID)
//	  <repo>/<sessionDir(projectID, mainSessionID)>/subagents/<mainSessionID>-<subSessionID>.json
//	恢复/续跑都按这个键 List(projectID, mainSessionID)：
//	  seelebridge/runtime_subagent_recovery.go  RestoreSubagentAnchors
//	  seelebridge/runtime_subagent_resume.go    subagentResumePort.Locate
//
// 而主会话在物化/恢复前后可能不是一个字符串（早分配草稿 SID `draft_<nanos>_<seq>`
// 与物化/恢复后的会话 ID）。**只要两个时刻的 mainSessionID 不同，出生期落盘的子
// 代理记录就留在旧键下，重启 List(newID) 读不到** —— 于是被判定成"不属于当前会话
// 的子代理"，既进不了子代理树、也不在工作表格里被认领（无法从 active 归档）。
//
// 本用例钉住这条键空间事实：它现在**复现**缺陷（第二个 Runtime 读不到记录），
// 修复后应改为 1 条（把 `wantRecords` 改成 1 并去掉下面的"复现"日志即可）。
func TestSubagentRecordKeyedByMainSessionIDDriftsOnResume(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "session-storage.json")
	storageDir := t.TempDir()

	// ① 出生期：主会话 ID 还是早分配的草稿 SID（draft_…）。
	const draftMainID = "draft_1790073103846285100_1"
	born := newSessionKeyFixture(t, dsn, storageDir, draftMainID)
	record := sessionstore.NodeSessionRecord{
		SchemaVersion: sessionstore.NodeSessionSchemaVersion,
		NodeID:        "sub-draft", SessionID: "node-draft-1", MainSessionID: draftMainID,
		Goal: "inspect the storage layer", Status: "running", UpdatedAt: time.Now().UTC(),
	}
	if err := born.nodeStore.Save(born.projectID, draftMainID, record); err != nil {
		t.Fatalf("save draft-era node record: %v", err)
	}
	// 自证：在出生期自己的键下，记录是可见的（键本身没问题）。
	if got, err := born.nodeStore.List(born.projectID, draftMainID); err != nil || len(got) != 1 {
		t.Fatalf("baseline: draft-era record not visible under its own key: err=%v n=%d", err, len(got))
	}
	born.shutdown()

	// ② 重启后：同一份存储，会话以"物化/恢复后的 ID"打开。
	const materializedMainID = "session_1790073103846285100"
	resumed := newSessionKeyFixture(t, dsn, storageDir, materializedMainID)
	defer resumed.shutdown()

	if err := resumed.runtime.RestoreSubagentAnchors(materializedMainID); err != nil {
		t.Fatalf("RestoreSubagentAnchors: %v", err)
	}
	records, err := resumed.nodeStore.List(resumed.projectID, materializedMainID)
	if err != nil {
		t.Fatalf("list node records: %v", err)
	}
	t.Logf("REPRO: draft-era main id=%q materialized main id=%q -> records under resumed key=%d (want 1 after fix)",
		draftMainID, materializedMainID, len(records))

	// 复现（当前实现）：键空间漂移后读不到出生期的记录。
	// 修复后把期望改成 1：断言"出生在草稿 ID 下的子代理必须能被当前会话认领"。
	if len(records) != 0 {
		t.Fatalf("expected the reproduced orphan (0 records) under the drifted key, got %d — "+
			"若此断言失败且为 1，说明已修复：把期望改成 1", len(records))
	}
}

// sessionKeyFixture 是"按显式主会话 ID 打开一份持久化存储"的最小夹具：两个实例
// 先后用同一个 dsn/storageDir，就等价于"进程重启后换了主会话 ID"。
type sessionKeyFixture struct {
	runtime   *Runtime
	router    *sessionstore.Router
	nodeStore *sessionstore.NodeSessionStore
	projectID string
}

func newSessionKeyFixture(t *testing.T, dsn, storageDir, mainSessionID string) *sessionKeyFixture {
	t.Helper()
	runtime := newTestRuntime(t)
	router, err := sessionstore.NewRouter(dsn, storageDir)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	nodeStore := sessionstore.NewNodeSessionStore(router)
	runtime.AttachHistoryRouter(router)
	runtime.AttachSubSessionStore(nodeStore)
	runtime.SetEventPersister(sessionstore.NewEventStore(router).Append)
	if _, err := runtime.NewMainSessionWithID(mainSessionID, nil); err != nil {
		t.Fatalf("new main session %q: %v", mainSessionID, err)
	}
	return &sessionKeyFixture{runtime: runtime, router: router, nodeStore: nodeStore, projectID: router.Workspace()}
}

func (fixture *sessionKeyFixture) shutdown() {
	fixture.runtime.Shutdown()
	_ = fixture.router.Close()
}
