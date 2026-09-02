package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	selexsession "github.com/RedHuang-0622/seelex/session"
)

func TestReActBudgetStopsOnlyAfterItsToolBudget(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	service.Mu.Lock()
	service.components.tasks.StartReActBudgetLocked("budget-request", ReActBudget{MaxToolRounds: 3, MaxToolCalls: 2})
	service.Mu.Unlock()

	bridge := NewToolHookBridge()
	bridge.Bind(service)
	hooks := bridge.Hooks()
	hooks.OnToolStart(context.Background(), session.ToolCallInfo{Turn: 0, Name: "write_file", Arguments: `{"path":"report.md","content":"done"}`})
	if !hooks.OnIterationComplete(context.Background(), 0) {
		t.Fatal("first delivery tool should remain within budget")
	}
	hooks.OnToolStart(context.Background(), session.ToolCallInfo{Turn: 1, Name: "read_file", Arguments: `{"path":"report.md"}`})
	if hooks.OnIterationComplete(context.Background(), 1) {
		t.Fatal("tool-call budget should stop the next ReAct iteration")
	}
	if err := service.components.tasks.ReActBudgetError("budget-request"); !errors.Is(err, task_context.ErrReActBudgetExceeded) {
		t.Fatalf("budget error = %v, want task_context.ErrReActBudgetExceeded", err)
	}
}

func TestReActBudgetUsesReservedFinalDeliveryTurn(t *testing.T) {
	rawResult := strings.Repeat("oversized-result", task_context.DefaultToolResultLimit())
	engine := &fakeEngine{history: []EngineMessage{
		{Role: "assistant", ToolCalls: []EngineToolCall{{ID: "call-large", Name: "bash"}}},
		{Role: "tool", ToolCallID: "call-large", Name: "bash", Content: rawResult, ContentSet: true},
	}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.Mu.Lock()
	service.components.tasks.StartReActBudgetLocked("budget-request", ReActBudget{MaxToolRounds: 1})
	service.components.tasks.SetReActBudgetExhaustedLocked("budget-request", "tool-round limit reached (1)")
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "budget-request"}
	service.components.tasks.BeginTask("budget-request", "deliver result", "high", nil, TaskCheckpoint{})
	stored := service.components.tasks.StoreToolResultLocked("bash", rawResult)
	service.components.tasks.SetResultRefByCallIDLocked("call-large", stored.Ref)
	service.Mu.Unlock()

	if err := service.finalizeReActBudget(context.Background(), "budget-request"); err != nil {
		t.Fatal(err)
	}
	for _, message := range engine.History() {
		if message.Content == reactBudgetFinalizationInput {
			t.Fatal("internal budget-finalization input leaked into history")
		}
	}
	engine.mu.Lock()
	prepared := append([]EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	for _, message := range prepared {
		if strings.Contains(message.Content, "oversized-result") {
			t.Fatal("reserved final delivery turn received raw oversized tool output")
		}
	}
}

func TestRuntimeMailboxDrainsIntoHistoryOutsideServiceLock(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{mailbox: []string{"child conclusion"}}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: fakeSessions{},
	})
	defer service.Shutdown()

	service.injectPendingSubagentContexts()
	history := engine.History()
	if len(history) != 1 || !strings.Contains(history[0].Content, "child conclusion") {
		t.Fatalf("merge-back was not injected into Engine history: %#v", history)
	}
	snapshot := service.Snapshot()
	for _, message := range snapshot.Conversation {
		if strings.Contains(message.Content, "child conclusion") {
			t.Fatalf("merge-back must stay out of the visible conversation: %#v", snapshot.Conversation)
		}
	}
	if pending := runtime.DrainSubagentContexts(); len(pending) != 0 {
		t.Fatalf("runtime mailbox was not drained: %#v", pending)
	}
}

// sessionBackedEngine 模拟生产 session-backed 引擎（OnIterationComplete 在
// Session 锁内执行，不可重入历史操作）。

func TestSessionBackedIterationInterruptsOnQueuedInput(t *testing.T) {
	service := newTestService(t, &sessionBackedEngine{fakeEngine: &fakeEngine{}})
	bridge := NewToolHookBridge()
	bridge.Bind(service)
	hooks := bridge.Hooks()
	ctx := context.Background()

	// 队列为空 → 继续。
	if !hooks.OnIterationComplete(ctx, 1) {
		t.Fatal("empty input queue must not interrupt the loop")
	}

	// 运行中入队一条 → 本轮结束中断（一轮一消费，队列随后清空提升）。
	service.Mu.Lock()
	runtime := service.sessionUnitLocked(service.Core.Snapshot.Session.ID)
	runtime.Enqueue(selexsession.QueuedRequest{
		DisplayInput: "临时补充需求",
		Payload:      chatRequest{displayInput: "临时补充需求", modelInput: "临时补充需求"},
	})
	service.Mu.Unlock()
	if hooks.OnIterationComplete(ctx, 2) {
		t.Fatal("queued input must interrupt the loop at the round boundary")
	}

	// 中断后队列保留在会话域 runtime（不在此处消费），由 runChat 结尾 drain。
	service.Mu.RLock()
	queued := len(service.sessions.Unit(service.Core.Snapshot.Session.ID).PendingRequests())
	service.Mu.RUnlock()
	if queued != 1 {
		t.Fatalf("after interrupt: inputQueue=%d, want 1", queued)
	}
}

// TestSystemPromptStableAcrossPlanNodeChanges 缓存前缀稳定性回归：
// system prompt 不得嵌入随节点变化的 current_node（节点状态由请求尾部的
// plan 上下文消息/read_plan 提供），否则每次节点推进都会使整段前缀缓存
// 失效（对照 codex-cli 的 ~99.8% 命中率）。

func TestChatPublishesSnapshotWithoutUI(t *testing.T) {
	engine := &fakeEngine{chunks: []string{"an", "swer"}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot := service.Snapshot()
		if !snapshot.Chat.Running {
			if len(snapshot.Conversation) < 2 || snapshot.Conversation[len(snapshot.Conversation)-1].Content != "answer" {
				t.Fatalf("unexpected conversation: %#v", snapshot.Conversation)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("chat did not complete")
}

func TestGracefulShutdownWaitsForQueuedChat(t *testing.T) {
	engine := newGracefulShutdownEngine()
	service := newTestService(t, engine)

	if err := service.Submit(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first chat did not start")
	}
	if err := service.Submit(context.Background(), "queued"); err != nil {
		t.Fatal(err)
	}
	service.BeginGracefulShutdown()
	if err := service.Submit(context.Background(), "rejected"); !errors.Is(err, ErrApplicationDraining) {
		t.Fatalf("Submit after graceful shutdown = %v, want ErrApplicationDraining", err)
	}

	idle := make(chan error, 1)
	go func() { idle <- service.WaitForIdle(context.Background()) }()
	close(engine.releaseFirst)
	select {
	case <-engine.secondStarted:
	case <-time.After(time.Second):
		t.Fatal("queued chat did not start")
	}
	select {
	case err := <-idle:
		t.Fatalf("WaitForIdle returned before queued chat completed: %v", err)
	default:
	}
	if !service.Snapshot().Chat.Running {
		t.Fatal("queued chat left an observable idle gap")
	}
	close(engine.releaseSecond)
	select {
	case err := <-idle:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForIdle did not return after queued chat completed")
	}
}

func TestSessionBackedQueueIsConsumedAtRunChatEnd(t *testing.T) {
	engine := &sessionBackedBlockingEngine{
		fakeEngine: &fakeEngine{},
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	sessions := &blockingSaveSessions{entered: make(chan struct{}), release: make(chan struct{})}
	service := newTestService(t, engine, withTestSessions(sessions))

	if err := service.Submit(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.started:
	case <-time.After(time.Second):
		t.Fatal("first chat did not start")
	}
	if err := service.Submit(context.Background(), "queued"); err != nil {
		t.Fatal(err)
	}
	close(engine.release)
	select {
	case <-sessions.entered:
	case <-time.After(time.Second):
		t.Fatal("current turn did not reach persistence")
	}
	if got := service.Snapshot().Chat.QueuedCount; got != 1 {
		t.Fatalf("queued count while persistence is draining = %d, want 1", got)
	}
	// CurrentTaskResumeRecord 自行加 Core.Mu.RLock（"供无锁调用点"），外层
	// 不能再包 service.Mu.RLock——Go RWMutex 不可重入，目录刷新 worker 在
	// 两次 RLock 之间排队写锁时会造成永久死锁（-race + 并发加载下偶发）。
	resume := service.components.tasks.CurrentTaskResumeRecord()
	if len(resume.QueuedRefs) != 1 || resume.QueuedRefs[0] != "queued" {
		t.Fatalf("persistence resume refs = %#v, want queued input", resume.QueuedRefs)
	}
	close(sessions.release)
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCancelChatInterruptsContextAwareEngine(t *testing.T) {
	service := newTestService(t, &blockingEngine{fakeEngine: &fakeEngine{}})
	if err := service.Submit(context.Background(), "interrupt me"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !service.Snapshot().Chat.Running {
		time.Sleep(time.Millisecond)
	}
	if !service.Snapshot().Chat.Running {
		t.Fatal("chat did not start")
	}
	if !service.CancelChat("") {
		t.Fatal("CancelChat returned false for the active request")
	}
	waitContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.WaitForIdle(waitContext); err != nil {
		t.Fatal(err)
	}
	if task := service.Snapshot().Task; task == nil || task.Status != TaskInterrupted {
		t.Fatalf("cancelled task = %#v, want interrupted", task)
	}
}

// TestCancelChatWithStaleRequestID 验证取消语义归属：request_id 只是参考，动作
// 对象是本会话当前运行中的回合（渲染层可能持有一拍前的 id）。此前该兜底由桌面
// Bridge 用空 id 重试实现，属于业务判断，现收在本服务内。
func TestCancelChatWithStaleRequestID(t *testing.T) {
	service := newTestService(t, &blockingEngine{fakeEngine: &fakeEngine{}})
	if err := service.Submit(context.Background(), "stop the spinner"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !service.Snapshot().Chat.Running {
		time.Sleep(time.Millisecond)
	}
	if !service.Snapshot().Chat.Running {
		t.Fatal("chat did not start")
	}
	if !service.CancelChat("request-from-a-previous-turn") {
		t.Fatal("CancelChat with a stale request id returned false, want the current turn cancelled")
	}
	waitContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.WaitForIdle(waitContext); err != nil {
		t.Fatal(err)
	}
	// 空闲后不得"顺手取消"任何东西。
	if service.CancelChat("request-from-a-previous-turn") {
		t.Fatal("CancelChat succeeded while idle")
	}
}
