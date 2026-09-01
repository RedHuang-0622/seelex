package core

import (
	"context"
	"sync"
	"testing"
	"time"
)

// frameworkLockEngine 模拟框架 Session（Seele session.Session）的锁语义：
// ChatStream 全程持有该会话的锁，SetSystemPrompt/SetSystemPromptFor 需要
// 同一把锁——对运行中会话设置 prompt 会阻塞到其跑完。用于复现「热挂载
// 运行中会话卡死、切换后消息发不出、也切不走」的真实生产死锁。
type frameworkLockEngine struct {
	*multiSessionEngine
	mu           sync.Mutex
	sessionLocks map[string]*sync.Mutex
	promptCalls  map[string]int
}

func newFrameworkLockEngine() *frameworkLockEngine {
	return &frameworkLockEngine{
		multiSessionEngine: newMultiSessionEngine(),
		sessionLocks:       map[string]*sync.Mutex{},
		promptCalls:        map[string]int{},
	}
}

func (engine *frameworkLockEngine) lockFor(sessionID string) *sync.Mutex {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	lock := engine.sessionLocks[sessionID]
	if lock == nil {
		lock = &sync.Mutex{}
		engine.sessionLocks[sessionID] = lock
	}
	return lock
}

// ChatStreamFor 持锁到 release（模拟 framework Session.ChatStream 全程持锁）。
func (engine *frameworkLockEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	lock := engine.lockFor(sessionID)
	lock.Lock()
	defer lock.Unlock()
	return engine.multiSessionEngine.ChatStreamFor(sessionID, ctx, input, onChunk)
}

// SetSystemPromptFor 需要会话锁：运行中会话调用会阻塞（生产死锁源）。
func (engine *frameworkLockEngine) SetSystemPromptFor(sessionID, prompt string) {
	engine.mu.Lock()
	engine.promptCalls[sessionID]++
	engine.mu.Unlock()
	lock := engine.lockFor(sessionID)
	lock.Lock()
	defer lock.Unlock()
}

func (engine *frameworkLockEngine) promptCallCount(sessionID string) int {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.promptCalls[sessionID]
}

// TestHotAttachRunningSessionDoesNotBlockOnEngineLock（生产死锁回归）：
// 长会话 A 运行中 → 切到 B（正常）→ 切回运行中的 A：热挂载不得对 A 调
// SetSystemPrompt（否则被 framework Session 锁卡死，TransitionLock 被攥住，
// 之后消息发不出、也切不到其它会话）。
func TestHotAttachRunningSessionDoesNotBlockOnEngineLock(t *testing.T) {
	engine := newFrameworkLockEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	ctx := context.Background()

	if err := service.Submit(ctx, "长会话 A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatal("A did not start")
	}

	// 切到空闲 B（引擎已注册 = 驻留，走 hot_attach）。
	const bID = "sess-hot-B"
	engine.register(bID)
	if err := service.ResumeSession(bID); err != nil {
		t.Fatalf("switch to B: %v", err)
	}
	if got := service.Snapshot().Session.ID; got != bID {
		t.Fatalf("active = %q, want B", got)
	}

	// 切回运行中的 A：必须在超时内返回，且不得对 A 设置 prompt。
	promptCallsBefore := engine.promptCallCount(aID)
	done := make(chan error, 1)
	go func() { done <- service.ResumeSession(aID) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("resume running A: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hot attach to running session blocked on engine lock（生产死锁复现）")
	}
	if got := engine.promptCallCount(aID); got != promptCallsBefore {
		t.Fatalf("SetSystemPromptFor called on running A during hot attach: before=%d after=%d",
			promptCallsBefore, got)
	}
	if got := service.Snapshot().Session.ID; got != aID {
		t.Fatalf("active = %q, want A", got)
	}

	// 关键：切换后消息仍然发得出去、也还能切到其它会话（TransitionLock 未被攥住）。
	if err := service.Submit(ctx, "B 的消息（A 运行中入队/后台）"); err != nil {
		t.Fatalf("submit after hot attach blocked: %v", err)
	}
	const cID = "sess-hot-C"
	engine.register(cID)
	done2 := make(chan error, 1)
	go func() { done2 <- service.ResumeSession(cID) }()
	select {
	case err := <-done2:
		if err != nil {
			t.Fatalf("switch to C: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("switch to C blocked after hot attach（TransitionLock 死锁）")
	}

	// 释放 A：全部收敛。
	close(engine.release[aID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
}
