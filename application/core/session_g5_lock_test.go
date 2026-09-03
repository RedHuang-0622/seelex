package core

// 波 3 G5 锁拆分验收靶场：流式输出进行中，两个会话在「活跃 ↔ 后台」之间
// 高频热切换，穿插 Snapshot 读与目录刷新，配合 -race 运行。钉住 View.mu
// 访问器化后的内存安全：会话 View 的增量写（后台路径 view.Mutate）与镜像
// 读（热挂载 view.Read）互斥，不再依赖"写者恒持 ViewMu"这一隐式前提。

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// streamEngine 是多会话路由引擎：ChatStreamFor 先按固定节奏吐出 chunk
// （模拟流式正文），再阻塞等待 release（与 multiSessionEngine 同构）。
type streamEngine struct {
	*multiSessionEngine
	chunks   []string
	interval time.Duration
}

// ChatStreamFor 覆盖内嵌引擎：先发流式块，再委托底层阻塞执行。
func (e *streamEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	e.mu.Lock()
	if _, ok := e.sessions[sessionID]; !ok {
		e.sessions[sessionID] = nil
		e.started[sessionID] = make(chan struct{})
		e.release[sessionID] = make(chan struct{})
		e.streamCalls[sessionID] = 0
	}
	started := e.started[sessionID]
	select {
	case <-started:
	default:
		close(started)
	}
	e.mu.Unlock()

	for _, chunk := range e.chunks {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(e.interval):
			onChunk(chunk)
		}
	}
	return e.multiSessionEngine.ChatStreamFor(sessionID, ctx, input, onChunk)
}

// TestConcurrentStreamingViewSwitchNoPollution 钉住两个会话在流式输出与热
// 切换并发下的竞态与归属：A 运行并输出 chunk，热挂载 B、后台提交 B，随后
// 高频在 A/B 间热切换并读 Snapshot、请求目录刷新；最后断言可见会话只含
// 各自输入（无跨会话污染）且无死锁/数据竞争。
func TestConcurrentStreamingViewSwitchNoPollution(t *testing.T) {
	engine := &streamEngine{
		multiSessionEngine: newMultiSessionEngine(),
		chunks:             []string{"chunk-A-1", "chunk-A-2", "chunk-A-3", "chunk-A-4"},
		interval:           time.Millisecond,
	}
	service := newTestService(t, engine)
	defer service.Shutdown()
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatalf("submit A: %v", err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	const bID = "sess-stream-B"
	engine.register(bID)
	if err := service.ResumeSession(bID); err != nil {
		t.Fatalf("hot attach B: %v", err)
	}
	if err := service.SubmitToSession(ctx, bID, "task B"); err != nil {
		t.Fatalf("background submit B: %v", err)
	}
	waitChatStarted(t, engine.started[bID])

	// A 在后台继续流式输出；高频热切换 A/B + Snapshot 读 + 目录刷新。
	stop := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = service.Snapshot()
			}
		}
	}()
	for round := 0; round < 20; round++ {
		if err := service.ResumeSession(aID); err != nil {
			t.Fatalf("hot attach A round %d: %v", round, err)
		}
		_ = service.Snapshot()
		if err := service.ResumeSession(bID); err != nil {
			t.Fatalf("hot attach B round %d: %v", round, err)
		}
		_ = service.Snapshot()
		service.components.sessions.RequestCatalogRefresh()
	}
	close(stop)
	readers.Wait()

	close(engine.release[aID])
	close(engine.release[bID])
	drainCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := service.WaitForIdle(drainCtx); err != nil {
		t.Fatalf("sessions did not drain (deadlock?): %v", err)
	}

	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	for _, sid := range []string{aID, bID} {
		unit := service.sessions.Unit(sid)
		if unit == nil {
			t.Fatalf("session %s unit missing after drain", sid)
		}
		for _, message := range unit.View.Conversation {
			if message.Role != "user" {
				continue
			}
			if strings.Contains(message.Content, "task A") && sid != aID {
				t.Fatalf("session %s view polluted with A input", sid)
			}
			if strings.Contains(message.Content, "task B") && sid != bID {
				t.Fatalf("session %s view polluted with B input", sid)
			}
		}
	}
}
