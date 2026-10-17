package core

import (
	"context"
	"github.com/RedHuang-0622/seelex/backend/limit"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"testing"
	"time"
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
	sessions.setInfos([]model.SessionInfo{{ID: "sess-1"}, {ID: "sess-2"}, {ID: "sess-3"}})

	for _, sessionID := range []string{"sess-1", "sess-2", "sess-3"} {
		engine.register(sessionID)
		if err := service.ResumeSession(sessionID); err != nil {
			t.Fatalf("resume %s: %v", sessionID, err)
		}
	}
	// 驱逐在 touchResident 内同步完成；-race 全量/高负载下给调度一个极短
	// 的收敛窗（不改断言语义，仅容忍环境抖动）。
	deadline := time.Now().Add(2 * time.Second)
	for engine.HasSession("sess-1") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
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
	// 目录行随 worker 异步刷新：Snapshot 前等一轮收敛，避免高负载下读到
	// 未含全部行的联合镜像（resident 标记由行数据源叠加）。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.WaitCatalogRefresh(ctx); err != nil {
		t.Fatalf("catalog settle: %v", err)
	}
	snapshot := service.Snapshot()
	byID := map[string]model.SessionInfo{}
	for _, item := range snapshot.Sessions {
		if item.Status == SessionStatusDraft {
			// 冷启动草稿行（"新会话"槽位）不属于本用例的驻留目录面：它与
			// 目录里 sess-* 是两层数据（草稿行由草稿槽位注入）。
			continue
		}
		byID[item.ID] = item
	}
	if len(byID) != 3 {
		t.Fatalf("snapshot rows after settle = %d, want 3", len(byID))
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
	sessions.setInfos([]model.SessionInfo{{ID: aID}, {ID: "sess-b"}, {ID: "sess-c"}})
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

// TestSessionReleaseDefaults 释放面默认口径（2026-10-14）：统一窗口默认 1
// （受保护集之外只保留上一个），两个分层覆盖键默认**未配置**（0 = 交给统一窗口
// 管辖）——在这里补 6/12 老默认会让统一窗口永远失效。
func TestSessionReleaseDefaults(t *testing.T) {
	if got := limit.DefaultLimits().ResidentSessionLimit; got != 0 {
		t.Fatalf("default resident layer limit = %d, want 0（未配置 = 交给统一窗口）", got)
	}
	if got := limit.DefaultLimits().ReleaseWindow(); got != 1 {
		t.Fatalf("default release window = %d, want 1（只保留上一个）", got)
	}
	if got := Limits().ReleaseWindow(); got != 1 {
		t.Fatalf("active release window = %d, want 1", got)
	}
	if got := Limits().ResidentSessionLimit; got != 0 {
		t.Fatalf("active resident layer limit = %d, want 0", got)
	}
	// 窗口是配置项：显式配置生效，<=0 回落默认。
	if got := (limit.Limits{SessionReleaseWindow: 4}).ReleaseWindow(); got != 4 {
		t.Fatalf("configured release window = %d, want 4", got)
	}
	if got := (limit.Limits{}).ReleaseWindow(); got != 1 {
		t.Fatalf("zero-value release window = %d, want 1（零值回落默认）", got)
	}
	// 分层覆盖（老口径）折算：额度 = 上限 − 受保护数（下限 0）。
	if got := releaseBudget(0, 3); got != 1 {
		t.Fatalf("releaseBudget(0, 3) = %d, want 1（窗口口径不看受保护数）", got)
	}
	if got := releaseBudget(6, 2); got != 4 {
		t.Fatalf("releaseBudget(6, 2) = %d, want 4（分层口径折算）", got)
	}
	if got := releaseBudget(1, 2); got != 0 {
		t.Fatalf("releaseBudget(1, 2) = %d, want 0（受保护数已达上限）", got)
	}
}
