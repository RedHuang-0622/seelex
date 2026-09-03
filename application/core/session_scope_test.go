package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// perSessionFakeRuntime 声明宿主具备逐会话执行能力（生产 seelebridge
// Runtime.PerSessionExecution=true 的镜像；内嵌 fakeRuntime 提供其余端口）。
type perSessionFakeRuntime struct {
	*fakeRuntime
}

func (perSessionFakeRuntime) PerSessionExecution() bool { return true }

// TestPerSessionHostSkipsGlobalScopeSideEffects F-4：逐会话宿主下
// bindGlobalProjectRoot/setWorkspaceWriteScope 为空操作（不写进程级根与
// Router 写作用域），fork 类显式会话 ID 唯一且不再依赖引擎 StartSession。
func TestPerSessionHostSkipsGlobalScopeSideEffects(t *testing.T) {
	runtime := &perSessionFakeRuntime{fakeRuntime: &fakeRuntime{}}
	sessions := &scopedSessions{}
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions,
	})
	defer service.Shutdown()
	if !service.perSessionExecution() {
		t.Fatal("per-session capability not detected")
	}
	if err := service.bindGlobalProjectRoot("C:\\proj"); err != nil {
		t.Fatalf("bindGlobalProjectRoot: %v", err)
	}
	if runtime.projectRoot != "" {
		t.Fatalf("per-session host mutated global project root: %q", runtime.projectRoot)
	}
	service.setWorkspaceWriteScope("project-1")
	if got := sessions.Workspace(); got != "" {
		t.Fatalf("per-session host mutated router write scope: %q", got)
	}
	first := service.newGeneratedSessionID("fork")
	second := service.newGeneratedSessionID("fork")
	if first == second || !strings.HasPrefix(first, "fork_") || !strings.HasPrefix(second, "fork_") {
		t.Fatalf("generated ids = %q/%q, want unique fork_ ids", first, second)
	}
	if !service.bindProjectRootIfSafe("sess-a", "C:\\proj") {
		t.Fatal("per-session host must allow project binding without global root")
	}
}

// TestCrossSessionSubmitWhileRunningNoLongerBusy 验证 M2 多会话并行语义：
// 同会话运行中，向其它会话提交不再返回 ErrSessionBusy——未加载的会话走
// 切换恢复路径（兼容 M1），已加载的会话在后台并行执行。
func TestCrossSessionSubmitWhileRunningNoLongerBusy(t *testing.T) {
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

	// M2：跨会话提交不再被单飞门控拒绝（未加载会话回退切换恢复路径）。
	err := service.SubmitToSession(context.Background(), "other-session", "to other session")
	if errors.Is(err, ErrSessionBusy) {
		t.Fatalf("SubmitToSession(other) returned ErrSessionBusy, want M2 background/activate semantics")
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

// TestActivateSessionAllowedForIdleTargetWhileOtherRunning 验证 M2 会话级
// 门控：运行中的会话不再阻止切换/恢复一个空闲目标会话（M1 的全局拒绝已
// 收窄为目标会话自身）。目标会话恢复成功后，原运行会话继续独立完成。
func TestActivateSessionAllowedForIdleTargetWhileOtherRunning(t *testing.T) {
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
	// M2：目标会话自身空闲即可切换/恢复（不再返回 ErrChatRunning）。
	if err := service.ActivateSession("other-session"); errors.Is(err, ErrChatRunning) {
		t.Fatalf("ActivateSession(idle target) while other running = ErrChatRunning, want M2 per-session gate")
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
