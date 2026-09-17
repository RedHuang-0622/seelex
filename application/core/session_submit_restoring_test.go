package core

import (
	"context"
	"testing"
	"time"
)

// streamCallsFor 读引擎在某会话上的 ChatStreamFor 调用次数（加锁；ChatStreamFor
// 在后台 goroutine 里写同一张表，直接读字段是数据竞争）。
func (engine *multiSessionEngine) streamCallsFor(sessionID string) int {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.streamCalls[sessionID]
}

// releaseSession 放行某会话的引擎回合（加锁取通道；幂等关闭）。
func (engine *multiSessionEngine) releaseSession(sessionID string) {
	engine.mu.Lock()
	ch := engine.release[sessionID]
	engine.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// coldRestoreFixture 复现「A 运行中、目标冷会话 B 的存储读被门闩限速」的
// 现场：B 未驻留 → 异步冷加载 → 视图指针立刻切到 B 的 restoring 空壳，
// 装载丢给后台 goroutine。
func coldRestoreFixture(t *testing.T) (*multiSessionEngine, *gatedHistorySessions, *Service) {
	t.Helper()
	engine := newMultiSessionEngine()
	now := time.Now()
	gated := &gatedHistorySessions{
		scopedSessions: &scopedSessions{
			catalog: map[string][]SessionInfo{"": {
				{ID: "sess-cold", Name: "cold B", UpdatedAt: now},
			}},
			histories: map[string]map[string][]EngineMessage{"": {
				"sess-cold": {{Role: "user", Content: "cold b content", ContentSet: true}},
			}},
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := newTestService(t, engine, withTestSessions(gated))
	t.Cleanup(gated.releaseNow)
	return engine, gated, service
}

// TestSubmitDuringColdRestoreDefersAndKeepsHistoryOrder 锁定 2026-09-17 根因
// （devlog 2026-09-17-submit-during-cold-restore-repro）修复后的不变量：
//
//	会话处于 restoring 时，应用层不得在该空壳上开新回合；
//	提交改为挂到装载完成点（延后），装载完成后在同一目标会话上启动。
//
// A 运行中切到冷 B：视图指针已指向 B 且 B 尚未装载。此窗口内提交必须
// ①被受理（不丢输入、不报错），②但不得在装载完成前开回合——否则输入落在空壳
// 上并与恢复基线争用同一份可见会话，基线守卫「仍为空才安装」被破坏，用户看到
// 「新消息在前、被恢复的历史挂在后面」。
//
// RED（修复前）：B 的 streamCalls 在装载完成前就变成 1，且可见会话顺序被颠倒。
func TestSubmitDuringColdRestoreDefersAndKeepsHistoryOrder(t *testing.T) {
	engine, gated, service := coldRestoreFixture(t)
	ctx := context.Background()

	// ① A 运行中（引擎回合保持未释放）。
	if err := service.Submit(ctx, "task in flight"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	// ② 冷 B 的存储读上门闩，切换到 B（异步冷加载）。
	gated.armMu.Lock()
	gated.armed = true
	gated.armMu.Unlock()
	if err := service.ResumeSession("sess-cold"); err != nil {
		t.Fatalf("ResumeSession(cold B): %v", err)
	}
	<-gated.entered
	shell := service.Snapshot()
	if shell.Session.ID != "sess-cold" || shell.Session.Status != SessionStatusRestoring {
		t.Fatalf("shell = %s/%s, want sess-cold/restoring", shell.Session.ID, shell.Session.Status)
	}

	// ③ restoring 窗口内提交：受理（延后），但装载完成前不得开回合。
	if err := service.Submit(ctx, "typed while restoring"); err != nil {
		t.Fatalf("Submit during restoring = %v, want nil（延后受理，不丢输入）", err)
	}
	if calls := engine.streamCallsFor("sess-cold"); calls != 0 {
		t.Fatalf("B streamCalls = %d while restoring, want 0（不得在空壳上开回合）", calls)
	}
	if calls := engine.streamCallsFor(aID); calls != 1 {
		t.Fatalf("A streamCalls = %d, want 1（输入不得落进 A）", calls)
	}

	// ④ 放行装载：挂起的提交被唤醒，且恢复基线**先**安装（历史在前、新消息在后）。
	gated.releaseNow()
	final := waitForSnapshot(t, service, func(snapshot Snapshot) bool {
		if snapshot.Session.ID != "sess-cold" || snapshot.Session.Status == SessionStatusRestoring {
			return false
		}
		for _, message := range snapshot.Conversation {
			if message.Content == "typed while restoring" {
				return true
			}
		}
		return false
	})
	baseline, typed := -1, -1
	for index, message := range final.Conversation {
		switch message.Content {
		case "cold b content":
			if baseline < 0 {
				baseline = index
			}
		case "typed while restoring":
			if typed < 0 {
				typed = index
			}
		}
	}
	if baseline < 0 {
		t.Fatalf("恢复基线未安装（缺少被恢复的历史）：%+v", final.Conversation)
	}
	if typed < 0 {
		t.Fatalf("延后的提交没有启动（输入丢失）：%+v", final.Conversation)
	}
	if baseline > typed {
		t.Fatalf("顺序被颠倒（新消息在前、历史在后）：baseline@%d typed@%d %+v", baseline, typed, final.Conversation)
	}

	// 收尾：释放 A / B 的回合并等待空闲。
	engine.releaseSession(aID)
	engine.releaseSession("sess-cold")
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.WaitForIdle(waitCtx); err != nil {
		t.Fatalf("wait idle: %v", err)
	}
}

// TestSubmitToSessionDefersUntilRestoreCompletes 锁定「延后」语义的两条边界：
//   - 目标在异步冷加载中时，显式后台提交**立即受理**（非阻塞，见
//     TestParallelSessionsExecuteConcurrently 的契约）；
//   - 但在装载完成前**不得开新回合**（否则就是 restoring 抢跑），必须挂到
//     装载完成点再启动。
//
// RED（修复前）：SubmitToSession 在 restoring 期间直接开回合（streamCalls>0）。
func TestSubmitToSessionDefersUntilRestoreCompletes(t *testing.T) {
	engine, gated, service := coldRestoreFixture(t)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	gated.armMu.Lock()
	gated.armed = true
	gated.armMu.Unlock()

	accepted := make(chan error, 1)
	go func() { accepted <- service.SubmitToSession(ctx, "sess-cold", "task B") }()
	select {
	case err := <-accepted:
		if err != nil {
			t.Fatalf("SubmitToSession(cold, restoring) = %v, want nil（非阻塞受理）", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SubmitToSession blocked on async cold restore; want non-blocking deferral")
	}
	// 确认后台装载确实在途（门闩已进入、restoring 生效）。
	select {
	case <-gated.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("background cold load never entered gated history read")
	}
	// 受理不等于开回合：装载完成前 streamCalls 必须保持 0。这里给足 300ms 窗口
	// 让「未延后」的实现暴露——修复前会在毫秒级开出真回合（RED）。
	for probe := 0; probe < 30; probe++ {
		if calls := engine.streamCallsFor("sess-cold"); calls != 0 {
			t.Fatalf("B streamCalls = %d while restoring, want 0（提交须挂到装载完成点）", calls)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 放行装载：挂起的提交应被唤醒并启动 B 的回合。
	gated.releaseNow()
	deadline := time.Now().Add(5 * time.Second)
	for engine.streamCallsFor("sess-cold") == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls := engine.streamCallsFor("sess-cold"); calls == 0 {
		t.Fatal("deferred submit never started after restore completed")
	}

	engine.releaseSession("sess-cold")
	engine.releaseSession(aID)
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.WaitForIdle(waitCtx); err != nil {
		t.Fatalf("wait idle: %v", err)
	}
}
