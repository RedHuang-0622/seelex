package core

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/event"
)

// chunkCaptureEngine 在 multiSessionEngine 之上捕获每个会话进行中
// ChatStream 的 onChunk，使测试能在任意时刻（含视图切换后）向运行中会话
// 注入流式文本块，复现「切换运行中会话后文本/工具顺序混乱」。
type chunkCaptureEngine struct {
	*multiSessionEngine
	mu     sync.Mutex
	chunks map[string]func(string)
}

func newChunkCaptureEngine() *chunkCaptureEngine {
	return &chunkCaptureEngine{multiSessionEngine: newMultiSessionEngine()}
}

func (engine *chunkCaptureEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.mu.Lock()
	if engine.chunks == nil {
		engine.chunks = map[string]func(string){}
	}
	engine.chunks[sessionID] = onChunk
	engine.mu.Unlock()
	return engine.multiSessionEngine.ChatStreamFor(sessionID, ctx, input, onChunk)
}

// emit 向指定会话进行中的 ChatStream 注入一个文本块（会话未启动时静默
// 丢弃，避免测试挂死）。
func (engine *chunkCaptureEngine) emit(sessionID, chunk string) {
	engine.mu.Lock()
	onChunk := engine.chunks[sessionID]
	engine.mu.Unlock()
	if onChunk != nil {
		onChunk(chunk)
	}
}

// viewRoles 返回指定会话可见对话的角色序列（含文本内容），用于断言顺序。
func viewRoles(service *Service, sessionID string) []string {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	view := service.sessionViewLocked(sessionID)
	if view == nil {
		return nil
	}
	roles := make([]string, 0, len(view.Conversation))
	for _, message := range view.Conversation {
		role := message.Role
		if message.Tool != nil && message.Tool.ID != "" {
			role += "[" + message.Tool.ID + "]"
		}
		if message.Content != "" {
			role += "(" + message.Content + ")"
		}
		roles = append(roles, role)
	}
	return roles
}

// indexOf 返回序列中首个以 prefix 开头的下标；不存在返回 -1。
func indexOf(roles []string, prefix string) int {
	for index, role := range roles {
		if len(role) >= len(prefix) && role[:len(prefix)] == prefix {
			return index
		}
	}
	return -1
}

// TestToolOrderingStableAfterSwitchToRunningSession 复现「切换运行中会话后
// 工具调用与 LLM 文本排序混乱」：A 后台运行并持有尚未落地的流式文本，视图
// 切到 A 后 A 触发工具调用——流式文本必须出现在工具消息之前，不得在切换
// 后被尾插到工具结果之后。
func TestToolOrderingStableAfterSwitchToRunningSession(t *testing.T) {
	engine := newChunkCaptureEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	const aID = "sess-ordering-A"
	const bID = "sess-ordering-B"
	engine.register(aID)
	engine.register(bID)

	// A 后台启动并阻塞（活跃视图仍是初始会话）。
	if err := service.SubmitToSession(ctx, aID, "task-A"); err != nil {
		t.Fatalf("SubmitToSession(A): %v", err)
	}
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatalf("A did not start: %s", dumpParallelState(service, engine.multiSessionEngine, aID))
	}

	// 视图切到 B（空闲），A 继续后台运行——与「先切走再切回运行中会话」
	// 场景等价，且保证切换路径先执行一次。
	if err := service.ResumeSession(bID); err != nil {
		t.Fatalf("switch to B: %v", err)
	}
	// 切回运行中的 A（热挂载）。
	if err := service.ResumeSession(aID); err != nil {
		t.Fatalf("switch back to running A: %v", err)
	}
	if got := service.Snapshot().Session.ID; got != aID {
		t.Fatalf("active view = %q, want %q", got, aID)
	}

	// A 仍在运行：注入一段流式文本（走 batcher 缓冲），随后触发工具调用。
	engine.emit(aID, "alpha")
	service.handleToolStart(withSessionID(ctx, aID), "tool-1", "t1", `{}`)
	service.handleToolComplete("tool-1", "t1", "result-1", nil, time.Millisecond)
	service.handleToolStart(withSessionID(ctx, aID), "tool-2", "t2", `{}`)
	service.handleToolComplete("tool-2", "t2", "result-2", nil, time.Millisecond)

	// 等待 batcher 把缓冲文本落地（修复后 handleToolStart 同步 flush；
	// 未修复时只能等 interval，顺序已错）。
	deadline := time.Now().Add(3 * time.Second)
	for {
		roles := viewRoles(service, aID)
		if indexOf(roles, "assistant(alpha)") >= 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("alpha never landed in A view: %v", roles)
		}
		time.Sleep(5 * time.Millisecond)
	}

	roles := viewRoles(service, aID)
	alphaAt := indexOf(roles, "assistant(alpha)")
	tool1At := indexOf(roles, "tool[t1]")
	tool2At := indexOf(roles, "tool[t2]")
	if alphaAt < 0 || tool1At < 0 || tool2At < 0 {
		t.Fatalf("missing messages in A view: %v", roles)
	}
	if alphaAt > tool1At {
		t.Fatalf("streamed text tail-appended after tool call (order chaos): %v", roles)
	}
	if tool1At > tool2At {
		t.Fatalf("tool call order inverted: %v", roles)
	}
	if indexOf(roles, "tool_result[t1]") < 0 || indexOf(roles, "tool_result[t2]") < 0 {
		t.Fatalf("missing tool results in A view: %v", roles)
	}
}

// TestBackgroundToolEventsCarryOwnRequestID 复现后台会话工具事件携带活跃
// 会话 requestID 的错误：EventToolStarted/Completed 的 request_id 必须属于
// 工具所在会话，否则前端按 request_id 关联/排序会串到活跃会话。
func TestBackgroundToolEventsCarryOwnRequestID(t *testing.T) {
	engine := newChunkCaptureEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	const aID = "sess-evt-A"
	const bID = "sess-evt-B"
	engine.register(aID)
	engine.register(bID)

	if err := service.SubmitToSession(ctx, aID, "task-A"); err != nil {
		t.Fatalf("SubmitToSession(A): %v", err)
	}
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatalf("A did not start")
	}
	if err := service.ResumeSession(bID); err != nil {
		t.Fatalf("switch to B: %v", err)
	}

	sub := service.Events.Subscribe(64)
	defer sub.Close()
	service.handleToolStart(withSessionID(ctx, aID), "tool-1", "t1", `{}`)
	service.handleToolCompleteObserved(withSessionID(ctx, aID), "tool-1", "t1", "", "result-1", nil, time.Millisecond, nil)

	aRequest := ""
	service.ViewMu.RLock()
	if task := service.components.tasks.CurrentTaskExecutionFor(aID); task != nil {
		aRequest = task.RequestID
	}
	service.ViewMu.RUnlock()
	if aRequest == "" {
		t.Fatal("A task request ID missing")
	}

	deadline := time.After(2 * time.Second)
	var started, completed *event.Event
	for started == nil || completed == nil {
		select {
		case ev, ok := <-sub.Events:
			if !ok {
				t.Fatal("event subscription closed")
			}
			if ev.SessionID != aID {
				continue
			}
			switch ev.Kind {
			case event.EventToolStarted:
				started = &ev
			case event.EventToolCompleted:
				completed = &ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for A tool events: started=%v completed=%v", started, completed)
		}
	}
	if started.RequestID != aRequest {
		t.Fatalf("EventToolStarted request_id = %q, want A's own %q", started.RequestID, aRequest)
	}
	if completed.RequestID != aRequest {
		t.Fatalf("EventToolCompleted request_id = %q, want A's own %q", completed.RequestID, aRequest)
	}
}

// TestSubmitPromptWhileOtherSessionRuns 复现输入阻塞：A 运行中（引擎阻塞），
// 向活跃空闲会话提交必须立即返回，不得等待 A 的引擎/全局锁。
func TestSubmitPromptWhileOtherSessionRuns(t *testing.T) {
	engine := newChunkCaptureEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	const aID = "sess-busy-A"
	const bID = "sess-busy-B"
	engine.register(aID)
	engine.register(bID)

	if err := service.SubmitToSession(ctx, aID, "long-A"); err != nil {
		t.Fatalf("SubmitToSession(A): %v", err)
	}
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatalf("A did not start")
	}
	// 活跃视图切到空闲 B（A 继续后台运行）。
	if err := service.ResumeSession(bID); err != nil {
		t.Fatalf("switch to B: %v", err)
	}

	started := time.Now()
	submitDone := make(chan error, 1)
	go func() { submitDone <- service.Submit(ctx, "prompt-to-B") }()
	select {
	case err := <-submitDone:
		if err != nil {
			t.Fatalf("Submit to B: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Submit blocked while A is running (global lock contention)")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Submit to B took %v while A running（输入被阻塞）", elapsed)
	}
	close(engine.release[aID])
}

// TestDeltasKeepFlowingAfterSwitchToRunningSession 回归守卫：切到运行中
// 会话后，后续流式文本仍持续进入该会话视图（视图保持动态），且事件修订号
// 单调推进。
func TestDeltasKeepFlowingAfterSwitchToRunningSession(t *testing.T) {
	engine := newChunkCaptureEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	const aID = "sess-dyn-A"
	engine.register(aID)
	if err := service.SubmitToSession(ctx, aID, "task-A"); err != nil {
		t.Fatalf("SubmitToSession(A): %v", err)
	}
	select {
	case <-engine.started[aID]:
	case <-time.After(3 * time.Second):
		t.Fatalf("A did not start")
	}
	if err := service.ResumeSession(aID); err != nil {
		t.Fatalf("switch to running A: %v", err)
	}

	engine.emit(aID, "delta-one")
	deadline := time.Now().Add(3 * time.Second)
	for {
		if indexOf(viewRoles(service, aID), "assistant(delta-one") >= 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("view stopped updating after switch: %v", viewRoles(service, aID))
		}
		time.Sleep(5 * time.Millisecond)
	}
	engine.emit(aID, "delta-two")
	deadline = time.Now().Add(3 * time.Second)
	for {
		roles := viewRoles(service, aID)
		if strings.Contains(strings.Join(roles, "|"), "delta-one") && strings.Contains(strings.Join(roles, "|"), "delta-two") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("view stopped updating after second delta: %v", viewRoles(service, aID))
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(engine.release[aID])
}
