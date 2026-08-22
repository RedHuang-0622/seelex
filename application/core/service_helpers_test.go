package core

import (
	"context"
	"sync"
	"testing"
	"time"
)

// testServiceOption 定制 newTestService 的依赖注入。
type testServiceOption func(*Dependencies)

func withTestSessions(sessions SessionPort) testServiceOption {
	return func(deps *Dependencies) { deps.Sessions = sessions }
}

func withTestRuntime(runtime RuntimePort) testServiceOption {
	return func(deps *Dependencies) { deps.Runtime = runtime }
}

// newTestService 构造一个带默认 fakes 的 Service（测试夹具），并在测试
// 结束时自动 Shutdown（catalog worker 随 teardown 停止）。
func newTestService(t testing.TB, engine ChatEngine, options ...testServiceOption) *Service {
	t.Helper()
	deps := Dependencies{
		Engine:   engine,
		Runtime:  &fakeRuntime{},
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: &fakeSessions{},
	}
	for _, option := range options {
		option(&deps)
	}
	service := mustNew(t, deps)
	t.Cleanup(service.Shutdown)
	return service
}

func mustNew(t testing.TB, deps Dependencies) *Service {
	t.Helper()
	service, err := New(deps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return service
}

// waitForSnapshot 轮询 Snapshot 直到 ready 条件满足，返回满足条件的快照。
func waitForSnapshot(t *testing.T, service *Service, ready func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := service.Snapshot()
		if ready(snapshot) {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("snapshot condition not met within deadline")
	return Snapshot{}
}

// sessionBackedEngine 模拟生产 session-backed 引擎（OnIterationComplete 在
// Session 锁内执行，不可重入历史操作）。
type sessionBackedEngine struct {
	*fakeEngine
}

func (*sessionBackedEngine) SessionBacked() bool { return true }

// gracefulShutdownEngine 是优雅关闭用例的引擎桩：第一次 ChatStream 阻塞到
// releaseFirst，第二次阻塞到 releaseSecond，用于验证排队输入在 graceful
// shutdown 下仍被消费。
type gracefulShutdownEngine struct {
	*fakeEngine
	mu            sync.Mutex
	calls         int
	firstStarted  chan struct{}
	secondStarted chan struct{}
	releaseFirst  chan struct{}
	releaseSecond chan struct{}
	firstOnce     sync.Once
	secondOnce    sync.Once
}

func newGracefulShutdownEngine() *gracefulShutdownEngine {
	return &gracefulShutdownEngine{
		fakeEngine:    &fakeEngine{},
		firstStarted:  make(chan struct{}),
		secondStarted: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
		releaseSecond: make(chan struct{}),
	}
}

func (engine *gracefulShutdownEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.mu.Lock()
	engine.calls++
	call := engine.calls
	engine.mu.Unlock()
	if call == 1 {
		engine.firstOnce.Do(func() { close(engine.firstStarted) })
		select {
		case <-engine.releaseFirst:
			return "first", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	engine.secondOnce.Do(func() { close(engine.secondStarted) })
	select {
	case <-engine.releaseSecond:
		return "second", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// waitForChatCompletion 轮询直到当前 chat 结束。
func waitForChatCompletion(t *testing.T, service *Service) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !service.Snapshot().Chat.Running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("chat did not complete")
}
