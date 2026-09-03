package core

import (
	"context"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// sessionTokenEngine 在 multiSessionEngine 之上提供 per-session token
// 计数（G1：TokenCountFor 走会话标签后的查询面）。
type sessionTokenEngine struct {
	*multiSessionEngine
	mu     sync.Mutex
	tokens map[string]string
}

func (engine *sessionTokenEngine) TokenCountFor(sessionID string) string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.tokens == nil {
		return "0"
	}
	return engine.tokens[sessionID]
}

// TestBackgroundRuntimeProjectionLandsInOwnSlot（G1-A/B 验收）：后台会话 B
// 运行时投影（tokens + tasks）写进 B 自己的 SessionUnit 槽，不覆盖活跃会话
// A 的投影；SnapshotOf(B) 读 B 槽而非 clone 视图 Runtime。
func TestBackgroundRuntimeProjectionLandsInOwnSlot(t *testing.T) {
	engine := &sessionTokenEngine{
		multiSessionEngine: newMultiSessionEngine(),
		tokens:             map[string]string{},
	}
	runtime := &fakeRuntime{}
	service := newTestService(t, engine, withTestRuntime(runtime))
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])
	engine.tokens[aID] = "27"
	runtime.replanMetricsBySession = map[string]dto.ReplanMetrics{
		aID: {InFlight: 1, ConcurrentLimit: 1, Accepted: 3},
	}

	bID := "sess-slot-b"
	engine.register(bID)
	engine.tokens[bID] = "13"
	if err := service.SubmitToSession(ctx, bID, "task B"); err != nil {
		t.Fatal(err)
	}
	waitChatStarted(t, engine.started[bID])
	runtime.replanMetricsBySession[bID] = dto.ReplanMetrics{InFlight: 2, ConcurrentLimit: 1, Accepted: 5}

	// 会话分区的 task 注册表（S0 风格）：A 当前分区，B 独立分区。
	runtime.SwitchSessionTasks(aID, nil)
	if _, _, err := runtime.TaskAddFor(aID, dto.TaskSpec{Key: "plan:na", Task: "A 节点"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.TaskAddFor(bID, dto.TaskSpec{Key: "plan:nb", Task: "B 节点"}); err != nil {
		t.Fatal(err)
	}

	close(engine.release[aID])
	close(engine.release[bID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}

	// 每个会话的槽都已写入，且 token 计数互不串扰。
	unitA := service.sessions.Unit(aID)
	unitB := service.sessions.Unit(bID)
	if unitA == nil || !unitA.RuntimeStateLoaded() {
		t.Fatal("session A runtime slot not loaded after run")
	}
	if unitB == nil || !unitB.RuntimeStateLoaded() {
		t.Fatal("session B runtime slot not loaded after background run")
	}
	if got := unitA.RuntimeState().Tokens; got != "27" {
		t.Fatalf("A slot tokens = %q, want 27", got)
	}
	if got := unitB.RuntimeState().Tokens; got != "13" {
		t.Fatalf("B slot tokens = %q, want 13", got)
	}

	// replan 统计按会话入槽：A/B 各自预算互不混用。
	if got := unitA.RuntimeState().Replan.Accepted; got != 3 {
		t.Fatalf("A slot replan accepted = %d, want 3", got)
	}
	if got := unitB.RuntimeState().Replan.Accepted; got != 5 {
		t.Fatalf("B slot replan accepted = %d, want 5", got)
	}

	// SnapshotOf(B) 读 B 自身槽：token 属于 B，任务表只含 B 的节点。
	snapshotB, err := service.SnapshotOf(bID)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshotB.Runtime.Tokens; got != "13" {
		t.Fatalf("SnapshotOf(B).Runtime.Tokens = %q, want 13", got)
	}
	foundB, foundA := false, false
	for _, row := range snapshotB.Runtime.WorkTable {
		if row.Task == "B 节点" {
			foundB = true
		}
		if row.Task == "A 节点" {
			foundA = true
		}
	}
	if !foundB || foundA {
		t.Fatalf("SnapshotOf(B) worktable = %+v, want plan:nb without plan:na", snapshotB.Runtime.WorkTable)
	}

	// 活跃快照仍是 A 的投影（B 的后台运行不污染视图 Runtime.Tokens）。
	if got := service.Snapshot().Runtime.Tokens; got != "27" {
		t.Fatalf("active Snapshot tokens = %q, want 27 (A)", got)
	}
}
