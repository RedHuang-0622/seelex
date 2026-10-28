package integration

import (
	"context"
	"testing"
	"time"
)

func TestParallelSessionsExecuteConcurrently(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newLegacyTestService(t, engine)
	ctx := context.Background()

	if err := service.components.submission.Submit(ctx, "task A"); err != nil {
		t.Fatalf("Submit A: %v", err)
	}
	aID := engine.SessionID()
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Logf("BREAKPOINT session A did not start:\n%s", dumpParallelState(service, engine, aID))
		t.Fatal("session A chat did not start")
	}

	// 注册一个已加载的后台会话（fork 子会话语义）。
	bID := "sess-forked-B"
	engine.register(bID)

	submitted := make(chan error, 1)
	go func() {
		submitted <- service.components.submitApplication.SubmitToSession(ctx, bID, "task B")
	}()

	// B 的后台提交应立即开始（不等 A 完成）。
	select {
	case <-engine.started[bID]:
	case <-time.After(3 * time.Second):
		t.Logf("BREAKPOINT session B did not start while A running:\n%s", dumpParallelState(service, engine, aID, bID))
		t.Fatal("session B chat did not start while A was still running")
	}
	select {
	case err := <-submitted:
		if err != nil {
			t.Fatalf("SubmitToSession(B): %v", err)
		}
	default:
		t.Logf("BREAKPOINT SubmitToSession(B) blocked:\n%s", dumpParallelState(service, engine, aID, bID))
		t.Fatal("SubmitToSession(B) blocked; want non-blocking background start")
	}

	// 两个会话同时 Running（会话级状态分片）。
	service.ViewMu.RLock()
	chatA := service.sessions.Unit(aID).ChatState().Running
	chatB := service.sessions.Unit(bID).ChatState().Running
	taskA := service.components.tasks.CurrentTaskExecutionFor(aID)
	taskB := service.components.tasks.CurrentTaskExecutionFor(bID)
	service.ViewMu.RUnlock()
	if !chatA || !chatB {
		t.Logf("BREAKPOINT not both running:\n%s", dumpParallelState(service, engine, aID, bID))
		t.Fatalf("expected both sessions running: A=%v B=%v", chatA, chatB)
	}
	if taskA == nil || taskB == nil || taskA.RequestID == taskB.RequestID {
		t.Logf("BREAKPOINT task states not session-isolated:\n%s", dumpParallelState(service, engine, aID, bID))
		t.Fatalf("task states are not session-isolated: A=%+v B=%+v", taskA, taskB)
	}

	// 释放 A 和 B。
	close(engine.release[aID])
	close(engine.release[bID])
	if err := service.components.lifecycleApplication.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}

	// 活跃会话快照只包含会话 A 的消息（B 的后台执行不污染）。
	snapshot := service.components.frontendSnapshot.Snapshot()
	for _, message := range snapshot.Conversation {
		if message.Content == "task B" {
			t.Logf("BREAKPOINT background session B polluted active snapshot:\n%s", dumpParallelState(service, engine, aID, bID))
			t.Fatalf("background session B polluted the active snapshot: %+v", message)
		}
	}

	// 两个会话各自的引擎都收到了独立请求。
	engine.mu.Lock()
	callsA := engine.streamCalls[aID]
	callsB := engine.streamCalls[bID]
	engine.mu.Unlock()
	if callsA < 1 || callsB < 1 {
		t.Logf("BREAKPOINT stream calls missing:\n%s", dumpParallelState(service, engine, aID, bID))
		t.Fatalf("expected stream calls on both sessions: A=%d B=%d", callsA, callsB)
	}
}

// TestParallelSessionsQueuedPerSession 验证每个会话维护自己的输入队列：A 运行
// 中向 A 排队，B 运行中向 B 排队，互不干扰。
func TestParallelSessionsQueuedPerSession(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newLegacyTestService(t, engine)
	ctx := context.Background()

	if err := service.components.submission.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Logf("BREAKPOINT session A did not start:\n%s", dumpParallelState(service, engine, aID))
		t.Fatal("session A chat did not start")
	}

	// A 运行中再向 A 提交 → 进 A 的队列。
	if err := service.components.submission.Submit(ctx, "queued A"); err != nil {
		t.Fatalf("Submit queued A: %v", err)
	}
	service.ViewMu.RLock()
	queuedA := len(service.sessions.Unit(aID).PendingRequests())
	service.ViewMu.RUnlock()
	if queuedA != 1 {
		t.Logf("BREAKPOINT session A queue wrong:\n%s", dumpParallelState(service, engine, aID))
		t.Fatalf("session A queue = %d, want 1", queuedA)
	}

	// 后台会话 B 提交（已加载）→ 并行执行，队列独立。
	bID := "sess-queued-B"
	engine.register(bID)
	go func() {
		_ = service.components.submitApplication.SubmitToSession(ctx, bID, "task B")
	}()
	select {
	case <-engine.started[bID]:
	case <-time.After(3 * time.Second):
		t.Logf("BREAKPOINT session B did not start:\n%s", dumpParallelState(service, engine, aID, bID))
		t.Fatal("session B chat did not start")
	}
	if err := service.components.submitApplication.SubmitToSession(ctx, bID, "queued B"); err != nil {
		t.Fatalf("SubmitToSession queued B: %v", err)
	}
	service.ViewMu.RLock()
	queuedB := len(service.sessions.Unit(bID).PendingRequests())
	service.ViewMu.RUnlock()
	if queuedB != 1 {
		t.Logf("BREAKPOINT session B queue wrong:\n%s", dumpParallelState(service, engine, aID, bID))
		t.Fatalf("session B queue = %d, want 1", queuedB)
	}

	close(engine.release[aID])
	close(engine.release[bID])
	if err := service.components.lifecycleApplication.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
}
