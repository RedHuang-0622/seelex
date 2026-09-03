package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

// withResidentLimit 把进程级 resident_limit 临时改成 limit，返回还原函数
// （core 测试串行执行；并行用例全部在串行段结束后才开始，无竞态）。
func withResidentLimit(limit int) func() {
	previous := Limits()
	applied := previous
	applied.ResidentSessionLimit = limit
	ApplyLimits(applied)
	return func() { ApplyLimits(previous) }
}

func residentOf(t *testing.T, service *Service, sessionID string) bool {
	t.Helper()
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	if unit := service.sessions.Unit(sessionID); unit != nil {
		return unit.Resident()
	}
	return false
}

// TestResidentLimitEvictsLeastRecentlyUsedIdle 波 4 G6 INV-G8：驻留上限 2，
// 热激活第三个会话时最旧的空闲驻留被驱逐（engine UnloadSession + Unit
// Resident=false），其余保留。
func TestResidentLimitEvictsLeastRecentlyUsedIdle(t *testing.T) {
	restore := withResidentLimit(2)
	defer restore()

	engine := newMultiSessionEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	defer service.Shutdown()
	sessions.setInfos([]SessionInfo{{ID: "sess-1"}, {ID: "sess-2"}, {ID: "sess-3"}})

	for _, sessionID := range []string{"sess-1", "sess-2", "sess-3"} {
		engine.register(sessionID)
		if err := service.ResumeSession(sessionID); err != nil {
			t.Fatalf("resume %s: %v", sessionID, err)
		}
	}
	if engine.HasSession("sess-1") {
		t.Fatalf("sess-1 engine still resident after exceeding limit（LRU 最旧应被驱逐）\n%s", engine.debugSnapshot())
	}
	if !engine.HasSession("sess-2") || !engine.HasSession("sess-3") {
		t.Fatalf("recent residents evicted unexpectedly\n%s", engine.debugSnapshot())
	}
	if residentOf(t, service, "sess-1") {
		t.Fatal("sess-1 unit resident flag still true after eviction")
	}
	if !residentOf(t, service, "sess-2") || !residentOf(t, service, "sess-3") {
		t.Fatal("sess-2/sess-3 unit resident flag lost")
	}
	snapshot := service.Snapshot()
	byID := map[string]SessionInfo{}
	for _, item := range snapshot.Sessions {
		byID[item.ID] = item
	}
	if byID["sess-1"].Resident {
		t.Fatal("snapshot row sess-1 still marks resident")
	}
	if !byID["sess-2"].Resident || !byID["sess-3"].Resident {
		t.Fatal("snapshot rows sess-2/sess-3 lost resident mark")
	}
}

// TestResidentLimitKeepsBusySessions 波 4 G6 INV-G8：运行中会话不可驱逐
// （即使超限）；LRU 只在空闲会话中挑选。
func TestResidentLimitKeepsBusySessions(t *testing.T) {
	restore := withResidentLimit(2)
	defer restore()

	engine := newMultiSessionEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	defer service.Shutdown()

	if err := service.Submit(t.Context(), "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	// 空闲 B、C 依次热激活：B 先驻留，C 激活时超限 → 应驱逐空闲 B 而保留
	// 运行中的 A（A 是当前视图且 busy，双保险）。
	sessions.setInfos([]SessionInfo{{ID: aID}, {ID: "sess-b"}, {ID: "sess-c"}})
	for _, sessionID := range []string{"sess-b", "sess-c"} {
		engine.register(sessionID)
		if err := service.ResumeSession(sessionID); err != nil {
			t.Fatalf("resume %s: %v", sessionID, err)
		}
	}
	if !engine.HasSession(aID) {
		t.Fatalf("running session A was evicted\n%s", engine.debugSnapshot())
	}
	if engine.HasSession("sess-b") {
		t.Fatalf("LRU idle sess-b should have been evicted（A busy 不可驱逐）\n%s", engine.debugSnapshot())
	}
	if !engine.HasSession("sess-c") {
		t.Fatalf("current view sess-c evicted\n%s", engine.debugSnapshot())
	}
	close(engine.release[aID])
	if err := service.WaitForIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// TestResidentLimitDefaultSix 默认 resident_limit = 6（INV-G8 口径）。
func TestResidentLimitDefaultSix(t *testing.T) {
	if got := seelexctx.DefaultLimits().ResidentSessionLimit; got != 6 {
		t.Fatalf("default resident limit = %d, want 6", got)
	}
	if got := Limits().ResidentSessionLimit; got != 6 {
		t.Fatalf("active resident limit = %d, want 6", got)
	}
}
