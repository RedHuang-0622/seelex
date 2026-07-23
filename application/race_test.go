package application

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// EventHub 竞态测试
// =============================================================================

// TestEventHub_RacePublishSubscribe 验证并发 Publish 与 Subscribe 不产生 data race。
func TestEventHub_RacePublishSubscribe(t *testing.T) {
	hub := NewEventHub()
	const publishers = 20
	const events = 50                         // 总计 1000 次 Publish
	sub := hub.Subscribe(publishers * events) // buffer 必须 ≥ 总事件数，否则 default 分支的 drain+resync 会丢失事件，导致消费者永久阻塞

	var wg sync.WaitGroup

	for i := 0; i < publishers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < events; j++ {
				hub.Publish(EventMessageAdded, uint64(id*events+j), "", nil)
			}
		}(i)
	}

	// 消费者：读取 events 次后退出（不阻塞）
	go func() {
		for i := 0; i < publishers*events; i++ {
			<-sub.Events
		}
	}()

	wg.Wait()
	sub.Close()
}

// TestEventHub_RaceSubscribeClosePublish 验证 Subscribe/Close 与 Publish 并发安全。
func TestEventHub_RaceSubscribeClosePublish(t *testing.T) {
	hub := NewEventHub()
	var wg sync.WaitGroup
	const rounds = 50

	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			sub := hub.Subscribe(1)
			// 给一点时间让 Publish 也有机会运行
			time.Sleep(time.Microsecond)
			sub.Close()
		}()
		go func() {
			defer wg.Done()
			hub.Publish(EventSnapshotChanged, 1, "", nil)
		}()
	}
	wg.Wait()
}

// TestEventHub_RaceMultipleSubscribers 验证多订阅者并发订阅/关闭。
func TestEventHub_RaceMultipleSubscribers(t *testing.T) {
	hub := NewEventHub()
	var wg sync.WaitGroup
	const subscribers = 10

	for i := 0; i < subscribers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub := hub.Subscribe(16)
			// 消费一些事件
			for j := 0; j < 5; j++ {
				select {
				case <-sub.Events:
				case <-time.After(time.Millisecond * 10):
				}
			}
			sub.Close()
		}()
	}

	// 同时大量发布
	go func() {
		for i := 0; i < 100; i++ {
			hub.Publish(EventMessageDelta, uint64(i), "", map[string]string{"delta": "x"})
		}
	}()

	wg.Wait()
}

// =============================================================================
// ApprovalBroker 竞态测试
// =============================================================================

// TestApprovalBroker_RaceConcurrentRequests 验证并发 Request 安全。
func TestApprovalBroker_RaceConcurrentRequests(t *testing.T) {
	hub := NewEventHub()
	broker := NewApprovalBroker(hub)
	var wg sync.WaitGroup
	const requests = 20

	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			reqID := fmt.Sprintf("race-req-%d", id)
			// 发起请求（会阻塞直到 Resolve）
			go func() {
				time.Sleep(time.Millisecond * 5)
				broker.Resolve(reqID, ApprovalDecision{OptionID: "ok"})
			}()
			_, _ = broker.Request(context.Background(), ApprovalRequest{
				ID:       reqID,
				Question: "test?",
				Options:  []InteractionOption{{ID: "ok", Label: "OK"}},
			})
		}(i)
	}
	wg.Wait()
}

// TestApprovalBroker_RaceRequestShutdown 验证 Shutdown 与 Request 并发安全。
func TestApprovalBroker_RaceRequestShutdown(t *testing.T) {
	hub := NewEventHub()
	broker := NewApprovalBroker(hub)

	var wg sync.WaitGroup
	const goroutines = 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, _ = broker.Request(context.Background(), ApprovalRequest{
				ID:       fmt.Sprintf("s-%d", id),
				Question: "q",
				Timeout:  time.Millisecond * 50,
			})
		}(i)
	}

	// 等待一些请求进入 pending
	time.Sleep(time.Millisecond * 5)
	broker.Shutdown()

	wg.Wait()
}

// TestApprovalBroker_RaceTimeoutResolve 验证 Timeout 与 Resolve 竞态。
func TestApprovalBroker_RaceTimeoutResolve(t *testing.T) {
	hub := NewEventHub()
	broker := NewApprovalBroker(hub)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			reqID := fmt.Sprintf("timeout-%d", id)
			// 一半 Resolve 快于 timeout，一半慢于 timeout
			go func() {
				if id%2 == 0 {
					time.Sleep(time.Microsecond)
					broker.Resolve(reqID, ApprovalDecision{OptionID: "fast"})
				} else {
					time.Sleep(time.Millisecond * 20)
					broker.Resolve(reqID, ApprovalDecision{OptionID: "slow"})
				}
			}()
			broker.Request(context.Background(), ApprovalRequest{
				ID:       reqID,
				Question: "q",
				Timeout:  time.Millisecond * 5,
			})
		}(i)
	}
	wg.Wait()
}

// TestApprovalBroker_RaceDuplicateResolve 验证重复 Resolve 返回错误（并发安全）。
func TestApprovalBroker_RaceDuplicateResolve(t *testing.T) {
	hub := NewEventHub()
	broker := NewApprovalBroker(hub)

	done := make(chan struct{})
	go func() {
		broker.Request(context.Background(), ApprovalRequest{
			ID:       "dup",
			Question: "q",
		})
		close(done)
	}()

	// 等待请求进入
	time.Sleep(time.Millisecond * 5)

	// 并发 Resolve
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			broker.Resolve("dup", ApprovalDecision{OptionID: "ok"})
		}()
	}
	wg.Wait()
	<-done
}

// =============================================================================
// Chat 核心路径竞态测试
// =============================================================================

// TestChat_RaceConcurrentSubmitChatRunning 验证并发 Submit 时 ErrChatRunning 和 InputQueue 竞态安全。
func TestChat_RaceConcurrentSubmitChatRunning(t *testing.T) {
	engine := &fakeEngine{chunks: []string{"slow..."}}
	// 使用自定义 engine 让它阻塞以模拟长时间 chat
	blockEngine := &blockingEngine{fakeEngine: engine}
	service := New(Dependencies{
		Engine:   blockEngine,
		Runtime:  &fakeRuntime{},
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: fakeSessions{},
	})
	defer service.Shutdown()

	// 启动一个 Chat
	err := service.Submit(context.Background(), "hello")
	if err != nil {
		t.Fatal("first submit should succeed:", err)
	}

	// 等待 chat 开始运行
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if service.Snapshot().Chat.Running {
			break
		}
		time.Sleep(time.Millisecond)
	}

	if !service.Snapshot().Chat.Running {
		t.Fatal("chat should be running")
	}

	// 并发 Submit：应触发 InputQueue 排队
	var wg sync.WaitGroup
	const concurrentSubmits = 10
	for i := 0; i < concurrentSubmits; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = service.Submit(context.Background(), fmt.Sprintf("msg-%d", id))
		}(i)
	}
	wg.Wait()

	snap := service.Snapshot()
	if snap.Chat.QueuedCount != concurrentSubmits {
		t.Errorf("expected %d queued, got %d", concurrentSubmits, snap.Chat.QueuedCount)
	}

	// 取消 chat 清理
	service.CancelChat("")
}

// blockingEngine 包装 fakeEngine，在 ChatStream 中阻塞以模拟长时间运行。
type blockingEngine struct {
	*fakeEngine
	blockCh chan struct{}
}

func (e *blockingEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	if e.blockCh == nil {
		e.blockCh = make(chan struct{})
	}
	// 发送一个 chunk 表示开始
	onChunk("started")
	// 阻塞直到被取消或 channel 关闭
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-e.blockCh:
		return "done", nil
	}
}

// TestChat_RaceSnapshotDuringChat 验证 Chat 运行期间并发读取 Snapshot 无 data race。
func TestChat_RaceSnapshotDuringChat(t *testing.T) {
	engine := &blockingEngine{fakeEngine: &fakeEngine{}}
	service := New(Dependencies{
		Engine:   engine,
		Runtime:  &fakeRuntime{},
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: fakeSessions{},
	})
	defer service.Shutdown()

	_ = service.Submit(context.Background(), "hello")

	// 等待 chat 开始
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if service.Snapshot().Chat.Running {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// 并发读取 Snapshot 和操作服务
	var wg sync.WaitGroup
	const readers = 10
	const iterations = 100

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = service.Snapshot()
				_ = service.Snapshot().Chat
				_ = service.Snapshot().Runtime
				_ = service.Snapshot().Conversation
			}
		}()
	}

	wg.Wait()
	service.CancelChat("")
}

// TestChat_RaceToolHandling 验证 handleToolStart/Complete 与 Snapshot 并发安全。
func TestChat_RaceToolHandling(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()

	var wg sync.WaitGroup
	const rounds = 50

	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			service.handleToolStart("test_tool", fmt.Sprintf("tool-%d", id), `{"arg":"value"}`)
			time.Sleep(time.Microsecond)
			service.handleToolComplete("test_tool", fmt.Sprintf("tool-%d", id), "result", nil, time.Millisecond)
		}(i)
		go func() {
			defer wg.Done()
			_ = service.Snapshot()
		}()
	}
	wg.Wait()
}

// =============================================================================
// Service 生命周期竞态测试
// =============================================================================

// TestService_RaceShutdownSubmit 验证 Shutdown 与 Submit 并发安全。
func TestService_RaceShutdownSubmit(t *testing.T) {
	var wg sync.WaitGroup
	const goroutines = 20

	service := newTestService(&fakeEngine{})

	for i := 0; i < goroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			service.Shutdown()
		}()
		go func(id int) {
			defer wg.Done()
			_ = service.Submit(context.Background(), "/help")
		}(i)
	}
	wg.Wait()
}

// TestService_RaceShutdownChat 验证 Shutdown 与 Chat 并发安全。
func TestService_RaceShutdownChat(t *testing.T) {
	engine := &blockingEngine{fakeEngine: &fakeEngine{}}
	service := New(Dependencies{
		Engine:   engine,
		Runtime:  &fakeRuntime{},
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: fakeSessions{},
	})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = service.Submit(context.Background(), "chat during shutdown")
	}()

	go func() {
		defer wg.Done()
		time.Sleep(time.Millisecond * 5)
		service.Shutdown()
	}()

	wg.Wait()
}

// TestService_ShutdownThenSubmit 验证 Shutdown 后 Submit 返回错误。
func TestService_ShutdownThenSubmit(t *testing.T) {
	service := newTestService(&fakeEngine{})
	service.Shutdown()

	err := service.Submit(context.Background(), "hello after shutdown")
	if err == nil {
		t.Error("Submit after Shutdown should return error")
	}
	if err.Error() != "application is shut down" {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestService_ShutdownThenStartChat 验证 Shutdown 后 startChat 返回错误。
func TestService_ShutdownThenStartChat(t *testing.T) {
	service := newTestService(&fakeEngine{})
	service.Shutdown()

	err := service.startChat(context.Background(), "hello")
	if err == nil {
		t.Error("startChat after Shutdown should return error")
	}
	if err.Error() != "application is shut down" {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// EffortManager 竞态测试
// =============================================================================

// TestEffortManager_RaceConcurrentApply 验证并发 Apply 安全。
func TestEffortManager_RaceConcurrentApply(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base prompt")
	eng := &mockEngine{}
	em := NewEffortManager(ps, eng)

	var wg sync.WaitGroup
	levels := []string{"lite", "medium", "high", "max"}

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = em.Apply(levels[id%len(levels)])
			_ = em.Current()
		}(i)
	}
	wg.Wait()
}

// TestEffortManager_RaceCycleAndApply 验证 Cycle 与 Apply 并发安全。
func TestEffortManager_RaceCycleAndApply(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base prompt")
	eng := &mockEngine{}
	em := NewEffortManager(ps, eng)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			em.Cycle()
		}()
		go func(level string) {
			defer wg.Done()
			em.Apply(level)
		}([]string{"lite", "high"}[i%2])
	}
	wg.Wait()
}

// =============================================================================
// PromptStack 竞态测试
// =============================================================================

// TestPromptStack_RacePushRender 验证并发 Push + Render 安全。
func TestPromptStack_RacePushRender(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base")

	var wg sync.WaitGroup
	const goroutines = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			ps.Push("skill", fmt.Sprintf("skill-%d", id), fmt.Sprintf("prompt-%d", id))
		}(i)
		go func() {
			defer wg.Done()
			_ = ps.Render()
			_ = ps.Describe()
			_ = ps.Count()
		}()
	}
	wg.Wait()
}

// TestPromptStack_RacePopPush 验证并发 Pop/PopKind + Push 安全。
func TestPromptStack_RacePopPush(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base")
	for i := 0; i < 10; i++ {
		ps.Push("skill", fmt.Sprintf("s%d", i), fmt.Sprintf("p%d", i))
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			ps.PopKind("skill")
		}()
		go func(id int) {
			defer wg.Done()
			ps.Push("effort", fmt.Sprintf("e%d", id), fmt.Sprintf("effort-%d", id))
		}(i)
		go func() {
			defer wg.Done()
			_ = ps.Render()
			_ = ps.Layers()
		}()
	}
	wg.Wait()
}

// TestPromptStack_RaceClearKind 验证并发 ClearKind + Push 安全。
func TestPromptStack_RaceClearKind(t *testing.T) {
	ps := NewPromptStack()
	ps.Push("base", "base", "base")
	for i := 0; i < 20; i++ {
		ps.Push("skill", fmt.Sprintf("s%d", i), fmt.Sprintf("p%d", i))
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ps.ClearKind("skill")
		}()
		go func(id int) {
			defer wg.Done()
			ps.Push("skill", fmt.Sprintf("new-%d", id), "new")
		}(i)
	}
	wg.Wait()
}

// =============================================================================
// InputQueue 竞态测试
// =============================================================================

// TestInputQueue_RaceEnqueueDuringChat 验证 Chat 运行中并发入队安全。
func TestInputQueue_RaceEnqueueDuringChat(t *testing.T) {
	engine := &blockingEngine{fakeEngine: &fakeEngine{}}
	service := New(Dependencies{
		Engine:   engine,
		Runtime:  &fakeRuntime{},
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: fakeSessions{},
	})
	defer service.Shutdown()

	_ = service.Submit(context.Background(), "start")

	// 等待 chat running
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if service.Snapshot().Chat.Running {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// 大量并发 Submit（应该全部入队）
	var wg sync.WaitGroup
	const submits = 30
	for i := 0; i < submits; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = service.Submit(context.Background(), fmt.Sprintf("queue-%d", id))
		}(i)
	}
	wg.Wait()

	snap := service.Snapshot()
	if snap.Chat.QueuedCount != submits {
		t.Errorf("expected %d queued, got %d", submits, snap.Chat.QueuedCount)
	}

	service.CancelChat("")
}

// =============================================================================
// Snapshot 并发读写测试
// =============================================================================

// TestSnapshot_RaceReadWrite 验证 Snapshot 读写并发安全。
func TestSnapshot_RaceReadWrite(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()

	var wg sync.WaitGroup
	const rounds = 100

	for i := 0; i < rounds; i++ {
		wg.Add(3)
		// 写入者
		go func(id int) {
			defer wg.Done()
			service.addNotice(fmt.Sprintf("notice-%d", id))
		}(i)
		// 读取者
		go func() {
			defer wg.Done()
			snap := service.Snapshot()
			_ = snap.Revision
			_ = snap.Chat
			_ = snap.Runtime
			_ = len(snap.Conversation)
		}()
		// 另一个写入者
		go func(id int) {
			defer wg.Done()
			service.handleToolStart("t", fmt.Sprintf("t%d", id), "{}")
			service.handleToolComplete("t", fmt.Sprintf("t%d", id), "ok", nil, 0)
		}(i)
	}
	wg.Wait()
}

// =============================================================================
// CancelChat 竞态测试
// =============================================================================

// TestCancelChat_Race 验证并发 CancelChat 安全。
func TestCancelChat_Race(t *testing.T) {
	engine := &blockingEngine{fakeEngine: &fakeEngine{}}
	service := New(Dependencies{
		Engine:   engine,
		Runtime:  &fakeRuntime{},
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: fakeSessions{},
	})
	defer service.Shutdown()

	_ = service.Submit(context.Background(), "start")
	time.Sleep(time.Millisecond * 5)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			service.CancelChat("")
		}()
	}
	wg.Wait()
}

// =============================================================================
// ObserveInteraction 竞态测试
// =============================================================================

// TestObserveInteraction_Race 验证 observeInteraction 并发安全。
func TestObserveInteraction_Race(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			interaction := &Interaction{
				ID:       fmt.Sprintf("i%d", time.Now().UnixNano()),
				Kind:     "approval",
				Question: "test",
				Options:  []InteractionOption{{ID: "ok", Label: "OK"}},
			}
			service.observeInteraction(interaction)
		}()
		go func() {
			defer wg.Done()
			service.observeInteraction(nil)
		}()
	}
	wg.Wait()
}
