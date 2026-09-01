package core

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// cancelAwareEngine 是阻塞到 ctx 取消的引擎（R5：Shutdown 取消运行中会话）。
type cancelAwareEngine struct {
	*fakeEngine
	mu      sync.Mutex
	started int
}

func (engine *cancelAwareEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.mu.Lock()
	engine.started++
	engine.mu.Unlock()
	<-ctx.Done()
	return "", ctx.Err()
}

func (engine *cancelAwareEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	return engine.ChatStream(ctx, input, onChunk)
}

func (engine *cancelAwareEngine) count() int {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.started
}

// TestShutdownConcurrentRunningSessions（R5）：运行中多会话 + 并发 Shutdown
// 可终止、无死锁、无 panic。
func TestShutdownConcurrentRunningSessions(t *testing.T) {
	engine := &cancelAwareEngine{fakeEngine: &fakeEngine{}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	ctx := context.Background()

	const sessions = 4
	var submitWG sync.WaitGroup
	for index := 0; index < sessions; index++ {
		submitWG.Add(1)
		go func(n int) {
			defer submitWG.Done()
			_ = service.SubmitToSession(ctx, fmt.Sprintf("sess-shutdown-%d", n), fmt.Sprintf("task-%d", n))
		}(index)
	}
	startedDeadline := time.After(10 * time.Second)
	for engine.count() < sessions {
		select {
		case <-startedDeadline:
			submitWG.Wait()
			t.Fatalf("only %d/%d sessions started", engine.count(), sessions)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	submitWG.Wait()

	before := runtime.NumGoroutine()
	var shutdownWG sync.WaitGroup
	for index := 0; index < 4; index++ {
		shutdownWG.Add(1)
		go func() {
			defer shutdownWG.Done()
			service.Shutdown()
		}()
	}
	done := make(chan struct{})
	go func() {
		shutdownWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		// 通过：并发 Shutdown 可终止（无死锁/panic）
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent Shutdown deadlock (timeout)")
	}

	// P4 粗粒度：退出后 goroutine 有界（允许少量稳定后台协程抖动）。
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+16 {
		t.Fatalf("goroutine count after shutdown = %d, before = %d（资源疑似泄漏）", after, before)
	}
}
