package seelebridge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/seelectx"

	"github.com/RedHuang-0622/seelex/seelebridge/plan"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// resumeTestFixture 组装一个带持久化 Router/事件库/节点记录存储的 Runtime。
type resumeTestFixture struct {
	runtime    *Runtime
	router     *sessionstore.Router
	eventStore *sessionstore.EventStore
	nodeStore  *sessionstore.NodeSessionStore
	projectID  string
	mainID     string
}

func newResumeTestFixture(t *testing.T) *resumeTestFixture {
	t.Helper()
	runtime := newTestRuntime(t)
	router, err := sessionstore.NewRouter(filepath.Join(t.TempDir(), "session-storage.json"), t.TempDir())
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	eventStore := sessionstore.NewEventStore(router)
	nodeStore := sessionstore.NewNodeSessionStore(router)
	runtime.AttachHistoryRouter(router)
	runtime.AttachSubSessionStore(nodeStore)
	runtime.SetEventPersister(eventStore.Append)
	if _, err := runtime.NewMainSessionWithID("sess_main", nil); err != nil {
		t.Fatalf("new main session: %v", err)
	}
	fixture := &resumeTestFixture{
		runtime: runtime, router: router, eventStore: eventStore,
		nodeStore: nodeStore, projectID: router.Workspace(), mainID: "sess_main",
	}
	t.Cleanup(func() {
		runtime.Shutdown()
		_ = router.Close()
	})
	return fixture
}

func (fixture *resumeTestFixture) save(t *testing.T, record sessionstore.NodeSessionRecord) {
	t.Helper()
	if record.SchemaVersion == 0 {
		record.SchemaVersion = sessionstore.NodeSessionSchemaVersion
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	if err := fixture.nodeStore.Save(fixture.projectID, fixture.mainID, record); err != nil {
		t.Fatalf("save node record: %v", err)
	}
}

// TestSubagentResumeRestartsInterruptedUnit（T-A-01/T-A-04）：残留 running
// 记录且无结论 → 冷恢复重建现场、system 注入恢复说明、同键重跑并收敛；
// main 历史不被写入任何 subagent active/恢复说明。
func TestSubagentResumeRestartsInterruptedUnit(t *testing.T) {
	fixture := newResumeTestFixture(t)
	runtime := fixture.runtime
	record := sessionstore.NodeSessionRecord{
		NodeID: "sub-1", SessionID: "node-hash-1", MainSessionID: fixture.mainID,
		Goal: "inspect the storage layer", Status: "running",
		StagesJSON: []byte(`[{"stage":"running","preview":"read message_rows.go"}]`),
	}
	fixture.save(t, record)

	views, err := runtime.ListSubagentRecovery(fixture.mainID)
	if err != nil {
		t.Fatalf("ListSubagentRecovery: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("views = %+v", views)
	}
	if !views[0].Active || !views[0].Resumable || views[0].ConclusionFound {
		t.Fatalf("view = %+v, want active+resumable without conclusion", views[0])
	}

	var dispatched []string
	var note string
	var blocks []seelectx.PromptBlock
	runtime.setSubagentRedispatchHook(func(_ context.Context, nodeID, goal string) (string, error) {
		dispatched = append(dispatched, nodeID+":"+goal)
		note = runtime.SubagentResumeNote(nodeID)
		blocks = runtime.node.PromptBlocks(plan.SeelexNodeInput{ID: nodeID, Input: goal})
		// 模拟真实运行期收尾：结论跟随 main（记录由收敛步骤删除）。
		done := record
		done.Status = "done"
		done.Summary = "resumed and finished"
		runtime.persistSubagentConclusion(fixture.mainID, done)
		return `{"subagents":[{"id":"sub-1","output":"ok"}]}`, nil
	})

	report, err := runtime.ResumeInterruptedSubagents(context.Background(), fixture.mainID)
	if err != nil {
		t.Fatalf("ResumeInterruptedSubagents: %v", err)
	}
	if report.RecoveryNoteRole != "system" {
		t.Fatalf("recovery note role = %q, want system", report.RecoveryNoteRole)
	}
	if resumed := strings.Join(report.Resumed, ","); resumed != "sub-1" {
		t.Fatalf("resumed = %q (report=%+v)", resumed, report)
	}
	if len(dispatched) != 1 || dispatched[0] != "sub-1:inspect the storage layer" {
		t.Fatalf("redispatch = %v（同键重跑必须复用节点 ID 与 goal）", dispatched)
	}
	if !strings.HasPrefix(note, subagentRecoveryNotePrefix) {
		t.Fatalf("recovery note = %q", note)
	}
	if !strings.Contains(note, "inspect the storage layer") ||
		!strings.Contains(note, "read message_rows.go") {
		t.Fatalf("recovery note 必须带目标与已知进展: %q", note)
	}
	noteRole := ""
	for _, block := range blocks {
		if block.Name != "node-resume-note" {
			continue
		}
		if len(block.Messages) == 1 {
			noteRole = block.Messages[0].Role
		}
	}
	if noteRole != SubagentRecoveryNoteRole {
		t.Fatalf("恢复说明必须走 %s provider role，得到 %q（blocks=%+v）",
			SubagentRecoveryNoteRole, noteRole, blocks)
	}
	if got := runtime.SubagentResumeNote("sub-1"); got != "" {
		t.Fatalf("收敛后恢复说明必须清除，仍有 %q", got)
	}
	if _, err := fixture.nodeStore.Load(fixture.projectID, fixture.mainID, "node-hash-1"); err == nil {
		t.Fatal("收敛后残留记录必须删除")
	}
	conclusions, err := runtime.subagentConclusionSet(context.Background(), fixture.projectID, fixture.mainID)
	if err != nil {
		t.Fatalf("conclusion set: %v", err)
	}
	if _, ok := conclusions["sub-1"]; !ok {
		t.Fatalf("结论必须跟随 main：%v", conclusions)
	}
	if count := mainHistoryLen(t, fixture.router, fixture.mainID); count != 0 {
		t.Fatalf("main 历史不得出现 subagent active/恢复说明：len=%d", count)
	}

	// T-A-03：再跑一轮不得产生第二条派发或结论。
	second, err := runtime.ResumeInterruptedSubagents(context.Background(), fixture.mainID)
	if err != nil {
		t.Fatalf("ResumeInterruptedSubagents(second): %v", err)
	}
	if second.Located != 0 || len(second.Resumed) != 0 {
		t.Fatalf("second run = %+v", second)
	}
	if len(dispatched) != 1 {
		t.Fatalf("重跑必须幂等，dispatch=%v", dispatched)
	}
}

// TestSubagentResumeSkipsTerminalResidual（T-A-02）：done 记录（结论丢失的
// 半同步残留）只补历史并按下记录重放结论，不重启。
func TestSubagentResumeSkipsTerminalResidual(t *testing.T) {
	fixture := newResumeTestFixture(t)
	runtime := fixture.runtime
	fixture.save(t, sessionstore.NodeSessionRecord{
		NodeID: "sub-done", SessionID: "node-hash-done", MainSessionID: fixture.mainID,
		Goal: "already finished", Status: "done", Summary: "finished before crash",
	})
	dispatched := 0
	runtime.setSubagentRedispatchHook(func(context.Context, string, string) (string, error) {
		dispatched++
		return "", nil
	})
	report, err := runtime.ResumeInterruptedSubagents(context.Background(), fixture.mainID)
	if err != nil {
		t.Fatalf("ResumeInterruptedSubagents: %v", err)
	}
	if dispatched != 0 {
		t.Fatalf("终态残留不得重启，dispatch=%d", dispatched)
	}
	if len(report.Resumed) != 0 || len(report.Skipped) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if _, err := fixture.nodeStore.Load(fixture.projectID, fixture.mainID, "node-hash-done"); err == nil {
		t.Fatal("终态残留记录必须被幂等清理")
	}
	conclusions, err := runtime.subagentConclusionSet(context.Background(), fixture.projectID, fixture.mainID)
	if err != nil {
		t.Fatalf("conclusion set: %v", err)
	}
	if _, ok := conclusions["sub-done"]; !ok {
		t.Fatalf("终态事实必须按记录重放为结论：%v", conclusions)
	}
}

// TestSubagentResumeSingleNodeFailureKeepsRecordRetryable：定点重跑失败必须
// 显式报错、保留残留记录（可重试），不得伪造结论。
func TestSubagentResumeSingleNodeFailureKeepsRecordRetryable(t *testing.T) {
	fixture := newResumeTestFixture(t)
	runtime := fixture.runtime
	fixture.save(t, sessionstore.NodeSessionRecord{
		NodeID: "sub-boom", SessionID: "node-hash-boom", MainSessionID: fixture.mainID,
		Goal: "will fail", Status: "running",
	})
	runtime.setSubagentRedispatchHook(func(context.Context, string, string) (string, error) {
		return "", context.DeadlineExceeded
	})
	result, err := runtime.ResumeSubagent(context.Background(), fixture.mainID, "sub-boom")
	if err != nil {
		t.Fatalf("ResumeSubagent: %v", err)
	}
	if result.Resumed || !strings.Contains(result.Error, "deadline exceeded") {
		t.Fatalf("result = %+v", result)
	}
	if runtime.SubagentResumeNote("sub-boom") != "" {
		t.Fatal("失败后恢复说明必须清除")
	}
	if _, err := fixture.nodeStore.Load(fixture.projectID, fixture.mainID, "node-hash-boom"); err != nil {
		t.Fatalf("失败必须保留残留记录以便重试: %v", err)
	}
}

// mainHistoryLen 读取主会话持久化历史长度（断言恢复链路不污染 main）。
func mainHistoryLen(t *testing.T, router *sessionstore.Router, sessionID string) int {
	t.Helper()
	history := sessionstore.NewDurableHistory(router, sessionID)
	messages, err := history.Load(context.Background())
	if err != nil {
		t.Fatalf("load main history: %v", err)
	}
	return len(messages)
}
