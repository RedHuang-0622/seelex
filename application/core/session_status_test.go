package core

import (
	"context"
	"sync"
	"testing"
	"time"
)

// liveCatalogSessions 是会话粒度目录桩：SessionsOf 返回测试注入的会话列表
// （模拟生产 Router 列表；状态由 Snapshot 运行时推导）。
type liveCatalogSessions struct {
	fakeSessions
	mu    sync.Mutex
	infos []SessionInfo
}

func (sessions *liveCatalogSessions) SessionsOf(string) []SessionInfo {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	return append([]SessionInfo(nil), sessions.infos...)
}

func (sessions *liveCatalogSessions) setInfos(infos []SessionInfo) {
	sessions.mu.Lock()
	sessions.infos = append([]SessionInfo(nil), infos...)
	sessions.mu.Unlock()
}

// catalogStatusOf 返回会话在 Snapshot 目录中的可见状态（等待目录刷新）。
func catalogStatusOf(t *testing.T, service *Service, sessionID string) SessionStatus {
	t.Helper()
	service.components.sessions.RequestCatalogRefresh()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, item := range service.Snapshot().Sessions {
			if item.ID == sessionID {
				return item.Status
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("session %q missing from catalog", sessionID)
	return ""
}

// TestSessionStatusReflectsRunningWhileChatActive（状态机回归）：chat 启动
// 后左侧栏条目应显示"运行中"，完成后回到 idle（9.5 状态机判断修复）。
func TestSessionStatusReflectsRunningWhileChatActive(t *testing.T) {
	engine := newMultiSessionEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	sessionID := engine.SessionID()
	select {
	case <-engine.started[sessionID]:
	case <-time.After(3 * time.Second):
		t.Fatal("session A did not start")
	}
	sessions.setInfos([]SessionInfo{{ID: sessionID}})
	if got := catalogStatusOf(t, service, sessionID); got != SessionStatusRunning {
		t.Fatalf("status while running = %q, want running", got)
	}

	close(engine.release[sessionID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	sessions.setInfos([]SessionInfo{{ID: sessionID}})
	if got := catalogStatusOf(t, service, sessionID); got != SessionStatusIdle {
		t.Fatalf("status after completion = %q, want idle", got)
	}
}

// TestSwitchToRunningSessionPreservesState（T4.5/运行中切换）：A 运行中切到
// B，再切回 A——A 保持运行、引擎历史不被改写；B 的执行态不受触碰。
func TestSwitchToRunningSessionPreservesState(t *testing.T) {
	engine := newMultiSessionEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatal("session A did not start")
	}
	sessions.setInfos([]SessionInfo{{ID: aID}})
	historyBefore := engine.HistoryFor(aID)

	// 切到空闲 B（A 继续后台运行）。
	bID := "sess-status-B"
	engine.register(bID)
	if err := service.ResumeSession(bID); err != nil {
		t.Fatalf("resume B while A running: %v", err)
	}
	if got := service.Snapshot().Session.ID; got != bID {
		t.Fatalf("active session = %q, want B", got)
	}
	if got := catalogStatusOf(t, service, aID); got != SessionStatusRunning {
		t.Fatalf("background A status = %q, want running（后台运行不因切换丢失）", got)
	}
	if got := service.sessions.Unit(aID).ChatState().Running; !got {
		t.Fatal("A stopped running after switching to B")
	}

	// 运行中切回 A：hot_attach 只移 V，不改写 A 执行态。
	if err := service.ResumeSession(aID); err != nil {
		t.Fatalf("resume A while running: %v", err)
	}
	if got := service.Snapshot().Session.ID; got != aID {
		t.Fatalf("active session after switch-back = %q, want A", got)
	}
	historyAfter := engine.HistoryFor(aID)
	if len(historyAfter) != len(historyBefore) {
		t.Fatalf("switch-back mutated A engine history: before=%d after=%d",
			len(historyBefore), len(historyAfter))
	}
	if got := catalogStatusOf(t, service, aID); got != SessionStatusRunning {
		t.Fatalf("A status after switch-back = %q, want running", got)
	}

	// 释放 A：全部收敛到 idle。
	close(engine.release[aID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	sessions.setInfos([]SessionInfo{{ID: aID}})
	if got := catalogStatusOf(t, service, aID); got != SessionStatusIdle {
		t.Fatalf("A status after completion = %q, want idle", got)
	}
}
