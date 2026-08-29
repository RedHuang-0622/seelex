package core

// 临时复现测试：会话运行中切换/新建会话是否死锁、消息入队是否安全。
// 结论：测试仅供诊断；通过后由维护者决定保留为回归用例或删除。

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

// dumpAllGoroutines 返回全量 goroutine 栈（死锁断点现场）。
func dumpAllGoroutines() string {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return string(buf[:n])
}

// waitOrDump 等待 done；超时则 dump 全量 goroutine 并 Fatal。
func waitOrDump(t *testing.T, done <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(8 * time.Second):
		t.Fatalf("DEADLOCK REPRO HIT: %s blocked >8s\n===== GOROUTINE DUMP =====\n%s\n===== END DUMP =====", what, dumpAllGoroutines())
		return nil
	}
}

// TestSwitchToOtherSessionWhileChattingRepro 场景 1：会话 A 运行中切换到会话 B。
func TestSwitchToOtherSessionWhileChattingRepro(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatalf("session A did not start:\n%s", dumpAllGoroutines())
	}

	// 注册空闲目标会话 B（模拟已加载的另一会话），运行中切换。
	bID := "sess-switch-B"
	engine.register(bID)
	resumeDone := make(chan error, 1)
	go func() {
		resumeDone <- service.ResumeSession(bID)
	}()
	if err := waitOrDump(t, resumeDone, "ResumeSession(B) while A running"); err != nil {
		t.Fatalf("ResumeSession(B) = %v", err)
	}

	service.Mu.RLock()
	runningA := service.sessionChat[aID].chat.Running
	active := service.Core.Snapshot.Session.ID
	service.Mu.RUnlock()
	if active != bID {
		t.Fatalf("active session = %q, want %q", active, bID)
	}
	if !runningA {
		t.Fatalf("session A stopped running after switching to B (want background continue)")
	}

	// 向运行中的 A 发送消息 → 应进入 A 自己的队列（不污染活跃快照）。
	if err := service.SubmitToSession(ctx, aID, "queued-to-A"); err != nil {
		t.Fatalf("SubmitToSession(A): %v", err)
	}
	service.Mu.RLock()
	queuedA := len(service.sessionChat[aID].inputQueue)
	snapQueued := service.Core.Snapshot.Chat.QueuedCount
	service.Mu.RUnlock()
	if queuedA != 1 {
		t.Fatalf("session A queue = %d, want 1", queuedA)
	}
	if snapQueued != 0 {
		t.Fatalf("active snapshot queue = %d, want 0 (background queue leaked into snapshot)", snapQueued)
	}

	close(engine.release[aID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
	history := engine.HistoryFor(aID)
	found := false
	for _, msg := range history {
		if strings.Contains(msg.Content, "queued-to-A") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("session A did not process queued message; history=%+v", history)
	}
}

// TestBeginNewSessionWhileChattingRepro 场景 2：会话 A 运行中尝试新建会话。
func TestBeginNewSessionWhileChattingRepro(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)

	if err := service.Submit(context.Background(), "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatalf("session A did not start:\n%s", dumpAllGoroutines())
	}

	done := make(chan error, 1)
	go func() {
		done <- service.BeginNewSession()
	}()
	if err := waitOrDump(t, done, "BeginNewSession while A running"); err != nil && !errors.Is(err, ErrChatRunning) {
		t.Fatalf("BeginNewSession = %v (want ErrChatRunning or nil, no deadlock)", err)
	}

	close(engine.release[aID])
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
}

// TestQueueSendDuringSessionSwitchRepro 场景 3：运行中切换与入队并发（-race 检测）。
func TestQueueSendDuringSessionSwitchRepro(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatalf("session A did not start:\n%s", dumpAllGoroutines())
	}

	bID := "sess-race-B"
	engine.register(bID)
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- service.ResumeSession(bID)
	}()
	if err := waitOrDump(t, done, "ResumeSession(B) race"); err != nil {
		t.Fatalf("ResumeSession(B) = %v", err)
	}

	var enqueueErr error
	for i := 0; i < 50; i++ {
		if err := service.SubmitToSession(ctx, aID, "msg"); err != nil {
			enqueueErr = err
			break
		}
	}
	if enqueueErr != nil {
		t.Fatalf("SubmitToSession(A) = %v", enqueueErr)
	}
	close(stop)
	close(engine.release[aID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
}
