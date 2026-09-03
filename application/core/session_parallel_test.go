package core

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/testutil"
)

// debugLog 是 SEELEX_TEST_DEBUG=1 门控的临时诊断日志（复跑噪音点时用，
// 定位完成后随断点一并清理）。
func debugLog(format string, args ...any) {
	if os.Getenv("SEELEX_TEST_DEBUG") == "1" {
		log.Printf("[parallel-debug] "+format, args...)
	}
}

// dumpServiceState 在断言失败时输出 service 侧会话状态（断点现场）。
// 调用方需保证在 service.Mu 可安全获取的上下文中执行。
func dumpServiceState(service *Service, ids ...string) string {
	service.Mu.RLock()
	defer service.Mu.RUnlock()
	var b strings.Builder
	for _, sid := range ids {
		unit := service.sessions.Unit(sid)
		if unit == nil {
			fmt.Fprintf(&b, "session %s: <nil unit>\n", sid)
			continue
		}
		chat := unit.ChatState()
		fmt.Fprintf(&b, "session %s: Running=%v RequestID=%s Queued=%d queueLen=%d\n",
			sid, chat.Running, chat.RequestID, chat.QueuedCount, len(unit.PendingRequests()))
		if st := service.components.tasks.CurrentTaskExecutionFor(sid); st != nil {
			fmt.Fprintf(&b, "  task: RequestID=%s Status=%s Objective=%q\n", st.RequestID, st.Status, st.Objective)
		} else {
			fmt.Fprintf(&b, "  task: <nil>\n")
		}
	}
	return b.String()
}

// dumpParallelState 汇总 service + engine 双侧状态，供失败断点输出。
func dumpParallelState(service *Service, engine *multiSessionEngine, ids ...string) string {
	return dumpServiceState(service, ids...) + engine.debugSnapshot(ids...)
}

// multiSessionEngine 是多会话测试引擎：按 sessionID 维护独立历史，支持
// 会话路由执行（SessionChatEngine）。每个会话的 ChatStreamFor 可阻塞等待
// release，用于验证并行执行。
type multiSessionEngine struct {
	*testutil.EmbeddedChatEngine
	mu          sync.Mutex
	active      string
	seq         int
	sessions    map[string][]EngineMessage
	started     map[string]chan struct{}
	release     map[string]chan struct{}
	streamCalls map[string]int
}

// 编译期断言：测试引擎必须满足 SessionChatEngine，否则 Service 的会话路由
// 全部回退到活跃会话路径，后台会话的 ChatStreamFor 永远不会被调用，相关
// 测试会永久阻塞（挂死而非失败）。
var _ contract.SessionChatEngine = (*multiSessionEngine)(nil)

func newMultiSessionEngine() *multiSessionEngine {
	return &multiSessionEngine{
		sessions:    map[string][]EngineMessage{},
		started:     map[string]chan struct{}{},
		release:     map[string]chan struct{}{},
		streamCalls: map[string]int{},
	}
}

// register 注册一个已加载会话（模拟 fork/resume 后的子会话）。
func (e *multiSessionEngine) register(sessionID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessions[sessionID] = nil
	e.started[sessionID] = make(chan struct{})
	e.release[sessionID] = make(chan struct{})
	debugLog("register session=%s", sessionID)
}

// UnloadSession 释放指定会话的引擎状态（阶段 2 生命周期）。
func (e *multiSessionEngine) UnloadSession(sessionID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.sessions, sessionID)
	delete(e.started, sessionID)
	delete(e.release, sessionID)
	delete(e.streamCalls, sessionID)
	if e.active == sessionID {
		e.active = ""
	}
	return nil
}

// debugSnapshot 返回引擎侧诊断快照（断点现场；自动加锁）。
func (e *multiSessionEngine) debugSnapshot(sessionIDs ...string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var b strings.Builder
	for _, sid := range sessionIDs {
		_, has := e.sessions[sid]
		startedClosed, releaseClosed := false, false
		if ch := e.started[sid]; ch != nil {
			select {
			case <-ch:
				startedClosed = true
			default:
			}
		}
		if ch := e.release[sid]; ch != nil {
			select {
			case <-ch:
				releaseClosed = true
			default:
			}
		}
		fmt.Fprintf(&b, "engine[%s]: hasSession=%v startedClosed=%v releaseClosed=%v streamCalls=%d historyLen=%d\n",
			sid, has, startedClosed, releaseClosed, e.streamCalls[sid], len(e.sessions[sid]))
	}
	return b.String()
}

func (e *multiSessionEngine) HasSession(sessionID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.sessions[sessionID]
	return ok
}

func (e *multiSessionEngine) StartSession() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq++
	sessionID := fmt.Sprintf("sess-%d", e.seq)
	e.sessions[sessionID] = nil
	e.started[sessionID] = make(chan struct{})
	e.release[sessionID] = make(chan struct{})
	e.active = sessionID
	return sessionID
}

// ActivateSession 以显式会话 ID 创建（如缺）并激活引擎实例。
func (e *multiSessionEngine) ActivateSession(sessionID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.sessions[sessionID]; !ok {
		e.sessions[sessionID] = nil
		e.started[sessionID] = make(chan struct{})
		e.release[sessionID] = make(chan struct{})
	}
	e.active = sessionID
	return nil
}

func (e *multiSessionEngine) SessionID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.active
}

func (e *multiSessionEngine) History() []EngineMessage {
	return e.HistoryFor(e.SessionID())
}

func (e *multiSessionEngine) HistoryFor(sessionID string) []EngineMessage {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]EngineMessage(nil), e.sessions[sessionID]...)
}

func (e *multiSessionEngine) ReplaceHistory(sessionID string, history []EngineMessage) error {
	return e.ReplaceHistoryFor(sessionID, history)
}

// ReplaceHistoryFor 是 SessionChatEngine 接口要求的会话内历史替换：替换
// 指定会话引擎历史，但不切换活跃会话（后台并行执行的 context 装配/恢复
// 路径用）。缺失该方法会使 *multiSessionEngine 不满足 SessionChatEngine，
// Service 的会话路由全部回退到活跃会话路径，导致后台会话的 ChatStreamFor
// 从未被调用、测试永久阻塞。
func (e *multiSessionEngine) ReplaceHistoryFor(sessionID string, history []EngineMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	// 会话内历史替换（M2）：不切换活跃会话。
	e.sessions[sessionID] = append([]EngineMessage(nil), history...)
	return nil
}

func (e *multiSessionEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	return e.ChatStreamFor(e.SessionID(), ctx, input, onChunk)
}

func (e *multiSessionEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	e.mu.Lock()
	e.streamCalls[sessionID]++
	e.sessions[sessionID] = append(e.sessions[sessionID],
		EngineMessage{Role: "user", Content: input, ContentSet: true},
		EngineMessage{Role: "assistant", Content: "answer", ContentSet: true},
	)
	// 会话通道按需创建（未注册/已卸载会话也能安全执行测试桩），并在锁内
	// 完成首次 close（并发流不会 double-close）。
	started := e.started[sessionID]
	release := e.release[sessionID]
	if started == nil {
		started = make(chan struct{})
		e.started[sessionID] = started
	}
	if release == nil {
		release = make(chan struct{})
		e.release[sessionID] = release
	}
	callCount := e.streamCalls[sessionID]
	select {
	case <-started:
	default:
		close(started)
	}
	e.mu.Unlock()
	debugLog("ChatStreamFor session=%s input=%q call=%d enter (started=%v release=%v)", sessionID, input, callCount, started != nil, release != nil)
	debugLog("ChatStreamFor session=%s input=%q signaled started, waiting release", sessionID, input)
	select {
	case <-release:
	case <-ctx.Done():
		debugLog("ChatStreamFor session=%s input=%q ctx.Done: %v", sessionID, input, ctx.Err())
		return "", ctx.Err()
	}
	debugLog("ChatStreamFor session=%s input=%q released, returning answer", sessionID, input)
	return "answer", nil
}

func (e *multiSessionEngine) AppendHistoryFor(sessionID string, msg types.Message) {
	e.mu.Lock()
	defer e.mu.Unlock()
	content := ""
	if msg.Content != nil {
		content = *msg.Content
	}
	e.sessions[sessionID] = append(e.sessions[sessionID], EngineMessage{Role: msg.Role, Content: content, ContentSet: true})
}
func (e *multiSessionEngine) ClearHistoryFor(sessionID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessions[sessionID] = nil
}
func (e *multiSessionEngine) SetSystemPromptFor(sessionID, prompt string) {}

func (e *multiSessionEngine) ClearHistory() { e.ClearHistoryFor(e.SessionID()) }

func (e *multiSessionEngine) TokenCount() string                   { return "0" }
func (e *multiSessionEngine) TraceText() string                    { return "" }
func (e *multiSessionEngine) SubAgentTree() []dto.SubAgentTreeNode { return nil }
func (e *multiSessionEngine) SetSystemPrompt(string)               {}
func (e *multiSessionEngine) SetMaxLoops(int)                      {}

// TestParallelSessionsExecuteConcurrently 验证 M2 核心语义：活跃会话运行中，
// 向已加载的其它会话后台提交 → 两个会话并行执行（互不阻塞、状态分片隔离、
// 活跃快照不被后台会话污染）。
func TestParallelSessionsExecuteConcurrently(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
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
		submitted <- service.SubmitToSession(ctx, bID, "task B")
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
	service.Mu.RLock()
	chatA := service.sessions.Unit(aID).ChatState().Running
	chatB := service.sessions.Unit(bID).ChatState().Running
	taskA := service.components.tasks.CurrentTaskExecutionFor(aID)
	taskB := service.components.tasks.CurrentTaskExecutionFor(bID)
	service.Mu.RUnlock()
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
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}

	// 活跃会话快照只包含会话 A 的消息（B 的后台执行不污染）。
	snapshot := service.Snapshot()
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
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
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
	if err := service.Submit(ctx, "queued A"); err != nil {
		t.Fatalf("Submit queued A: %v", err)
	}
	service.Mu.RLock()
	queuedA := len(service.sessions.Unit(aID).PendingRequests())
	service.Mu.RUnlock()
	if queuedA != 1 {
		t.Logf("BREAKPOINT session A queue wrong:\n%s", dumpParallelState(service, engine, aID))
		t.Fatalf("session A queue = %d, want 1", queuedA)
	}

	// 后台会话 B 提交（已加载）→ 并行执行，队列独立。
	bID := "sess-queued-B"
	engine.register(bID)
	go func() {
		_ = service.SubmitToSession(ctx, bID, "task B")
	}()
	select {
	case <-engine.started[bID]:
	case <-time.After(3 * time.Second):
		t.Logf("BREAKPOINT session B did not start:\n%s", dumpParallelState(service, engine, aID, bID))
		t.Fatal("session B chat did not start")
	}
	if err := service.SubmitToSession(ctx, bID, "queued B"); err != nil {
		t.Fatalf("SubmitToSession queued B: %v", err)
	}
	service.Mu.RLock()
	queuedB := len(service.sessions.Unit(bID).PendingRequests())
	service.Mu.RUnlock()
	if queuedB != 1 {
		t.Logf("BREAKPOINT session B queue wrong:\n%s", dumpParallelState(service, engine, aID, bID))
		t.Fatalf("session B queue = %d, want 1", queuedB)
	}

	close(engine.release[aID])
	close(engine.release[bID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
}
