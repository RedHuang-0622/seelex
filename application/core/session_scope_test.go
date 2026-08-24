package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestCrossSessionSubmitWhileRunningReturnsSessionBusy 验证 M1 单飞执行
// 边界：同会话运行中，向其它会话提交返回 ErrSessionBusy（而非全局
// ErrChatRunning——保护粒度已收窄到会话级，真并行 = M2）。
func TestCrossSessionSubmitWhileRunningReturnsSessionBusy(t *testing.T) {
	engine := &sessionBackedBlockingEngine{
		fakeEngine: &fakeEngine{},
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	service := newTestService(t, engine)

	if err := service.Submit(context.Background(), "first"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	select {
	case <-engine.started:
	case <-time.After(3 * time.Second):
		t.Fatal("chat did not start")
	}

	err := service.SubmitToSession(context.Background(), "other-session", "to other session")
	if !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("SubmitToSession(other) while running = %v, want ErrSessionBusy", err)
	}

	close(engine.release)
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestSubmitToSessionDelegatesForActiveSession 验证同会话提交走既有
// Submit 路径（运行中排队、空闲直跑）。
func TestSubmitToSessionDelegatesForActiveSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	if err := service.Submit(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessionID := service.Snapshot().Session.ID
	if sessionID == "" {
		t.Fatal("expected materialized session ID")
	}
	if err := service.SubmitToSession(context.Background(), sessionID, "second"); err != nil {
		t.Fatalf("SubmitToSession(active): %v", err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(service.Snapshot().Conversation); got < 4 {
		t.Fatalf("conversation messages = %d, want >= 4 (two exchanges)", got)
	}
}

// TestSubmitToSessionRejectsEmptyID 验证会话级 API 拒绝空会话 ID。
func TestSubmitToSessionRejectsEmptyID(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	if err := service.SubmitToSession(context.Background(), "  ", "hi"); err == nil {
		t.Fatal("expected error for empty session ID")
	}
}

// TestActivateSessionRejectedWhileRunning 验证运行中切换会话被拒绝
// （共享 Snapshot 不串写；多页签并行驻留 = M2）。
func TestActivateSessionRejectedWhileRunning(t *testing.T) {
	engine := &sessionBackedBlockingEngine{
		fakeEngine: &fakeEngine{},
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	service := newTestService(t, engine)
	if err := service.Submit(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.started:
	case <-time.After(3 * time.Second):
		t.Fatal("chat did not start")
	}
	if err := service.ActivateSession("other-session"); !errors.Is(err, ErrChatRunning) {
		t.Fatalf("ActivateSession while running = %v, want ErrChatRunning", err)
	}
	close(engine.release)
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestSnapshotOfOnlyActiveSessionAvailable 验证 M1 快照粒度：仅活跃会话
// 有驻留快照。
func TestSnapshotOfOnlyActiveSessionAvailable(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	if err := service.Submit(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessionID := service.Snapshot().Session.ID
	if _, err := service.SnapshotOf(sessionID); err != nil {
		t.Fatalf("SnapshotOf(active) = %v, want nil", err)
	}
	if _, err := service.SnapshotOf("other-session"); !errors.Is(err, ErrSessionSnapshotUnavailable) {
		t.Fatalf("SnapshotOf(other) = %v, want ErrSessionSnapshotUnavailable", err)
	}
}

// TestChatEventsCarrySessionID 验证 chat 生命周期事件携带会话路由键。
func TestChatEventsCarrySessionID(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	sub := service.Subscribe(64)
	defer sub.Close()

	if err := service.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessionID := service.Snapshot().Session.ID
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-sub.Events:
			if event.SessionID == sessionID {
				return
			}
		case <-deadline:
			t.Fatalf("no event carried session_id %q", sessionID)
		}
	}
}

// TestSubscribeSessionFiltersBySessionID 验证会话级订阅只投递目标会话
// （及全局空 SessionID）事件。
func TestSubscribeSessionFiltersBySessionID(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	initialSessionID := service.Snapshot().Session.ID
	target, err := service.SubscribeSession(initialSessionID, 16)
	if err != nil {
		t.Fatalf("SubscribeSession(target): %v", err)
	}
	defer target.Close()
	other, err := service.SubscribeSession("does-not-exist", 16)
	if err != nil {
		t.Fatalf("SubscribeSession(other): %v", err)
	}
	defer other.Close()

	if err := service.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-target.Events:
			if event.SessionID == initialSessionID {
				goto targetSeen
			}
		case <-deadline:
			t.Fatal("target subscription never received scoped event")
		}
	}
targetSeen:
	deadline = time.After(500 * time.Millisecond)
	for {
		select {
		case event := <-other.Events:
			if event.SessionID == "does-not-exist" {
				t.Fatalf("other subscription received scoped event: %+v", event)
			}
		case <-deadline:
			return
		}
	}
}
