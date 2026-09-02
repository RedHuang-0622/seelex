package core

import (
	"context"
	"testing"
	"time"
)

// TestAnyChatRunningReflectsBackgroundSessions G0c 判定面：视图快照只镜像
// 当前会话，AnyChatRunning 必须看到后台会话仍在运行（视图空闲 ≠ 进程空闲）。
func TestAnyChatRunningReflectsBackgroundSessions(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	if service.AnyChatRunning() {
		t.Fatal("AnyChatRunning must be false before any chat starts")
	}
	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])
	if !service.AnyChatRunning() {
		t.Fatal("AnyChatRunning must be true while the view session runs")
	}

	bID := "sess-close-B"
	engine.register(bID)
	if err := service.SubmitToSession(ctx, bID, "task B"); err != nil {
		t.Fatal(err)
	}
	waitChatStarted(t, engine.started[bID])
	if !service.AnyChatRunning() {
		t.Fatal("AnyChatRunning must stay true while the background session runs")
	}

	// A 结束（视图转 idle）后 B 仍在跑：AnyChatRunning 必须仍为 true。
	close(engine.release[aID])
	waitUnitIdle(t, service, aID)
	if !service.AnyChatRunning() {
		t.Fatal("AnyChatRunning must remain true while only the background session runs")
	}

	close(engine.release[bID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if service.AnyChatRunning() {
		t.Fatal("AnyChatRunning must be false after all sessions finish")
	}
}

// TestCancelAllChatsDrainsEveryRunningSession G0c 超时路径：取消必须覆盖
// 全部 running sid（不只视图会话），且每个 runChat 的收尾（逐会话 flush）
// 让 WaitForIdle 在取消后收敛。
func TestCancelAllChatsDrainsEveryRunningSession(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	bID := "sess-close-B"
	engine.register(bID)
	if err := service.SubmitToSession(ctx, bID, "task B"); err != nil {
		t.Fatal(err)
	}
	waitChatStarted(t, engine.started[bID])

	service.CancelAllChats()

	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := service.WaitForIdle(waitCtx); err != nil {
		t.Fatalf("WaitForIdle did not converge after CancelAllChats: %v", err)
	}
	if service.AnyChatRunning() {
		t.Fatal("sessions still running after CancelAllChats + WaitForIdle")
	}
	// 两个会话都实际执行过（取消发生在引擎阻塞点上，runChat 收尾完整）。
	engine.mu.Lock()
	callsA := engine.streamCalls[aID]
	callsB := engine.streamCalls[bID]
	engine.mu.Unlock()
	if callsA < 1 || callsB < 1 {
		t.Fatalf("stream calls after cancel: A=%d B=%d, want both >= 1", callsA, callsB)
	}
}
