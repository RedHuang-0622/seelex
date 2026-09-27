package core

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// stopQueueEngine 模拟"被停止按钮命中的那一轮"：第一轮阻塞到 ctx 取消（= 用户点
// 停止），之后每一轮立即返回；它记录每次收到的模型输入，供"排队消息全部发送"断言。
type stopQueueEngine struct {
	*fakeEngine
	mu      sync.Mutex
	inputs  []string
	calls   int
	started chan struct{}
	once    sync.Once
}

func (engine *stopQueueEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.mu.Lock()
	engine.calls++
	call := engine.calls
	engine.inputs = append(engine.inputs, input)
	engine.mu.Unlock()
	if call == 1 {
		engine.once.Do(func() { close(engine.started) })
		<-ctx.Done()
		return "", ctx.Err()
	}
	onChunk("done")
	return "done", nil
}

// ChatStreamFor 显式转发到自身 ChatStream（覆盖内嵌 fakeEngine 的提升方法，
// 保证会话路由面下停止/续跑语义仍生效）。
func (engine *stopQueueEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	return engine.ChatStream(ctx, input, onChunk)
}

func (engine *stopQueueEngine) recordedInputs() []string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return append([]string(nil), engine.inputs...)
}

// TestStopFlushesQueuedInputsIntoTheNextTurn 钉住停止按钮的第三条语义：点停止后清空
// 消息队列，队列里的消息**全部发送出去**（整批提升为下一轮），不是随回合一起丢弃。
func TestStopFlushesQueuedInputsIntoTheNextTurn(t *testing.T) {
	engine := &stopQueueEngine{fakeEngine: &fakeEngine{}, started: make(chan struct{})}
	service := newTestService(t, engine)

	if err := service.Submit(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.started:
	case <-time.After(time.Second):
		t.Fatal("首轮没有起步")
	}
	enqueueQueuedInputs(t, service, "queued-a", "queued-b")
	if got := service.Snapshot().Chat.QueuedCount; got != 2 {
		t.Fatalf("排队计数 = %d, want 2", got)
	}

	// 停止按钮。
	if !service.CancelChat("") {
		t.Fatal("CancelChat returned false for the running turn")
	}
	waitContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := service.WaitForIdle(waitContext); err != nil {
		t.Fatal(err)
	}

	inputs := engine.recordedInputs()
	if len(inputs) != 2 {
		t.Fatalf("回合数 = %d, want 2（停止后队列整批发送）: %#v", len(inputs), inputs)
	}
	second := displayUserInput(inputs[1])
	for _, want := range []string{"queued-a", "queued-b"} {
		if !strings.Contains(second, want) {
			t.Fatalf("下一轮输入缺少排队消息 %q: %q", want, second)
		}
	}

	chat := service.Snapshot().Chat
	if chat.QueuedCount != 0 || len(chat.InputQueue) != 0 {
		t.Fatalf("停止后队列必须清空: %#v", chat)
	}
	if chat.Running {
		t.Fatal("停止并发送完排队消息后会话必须空闲")
	}
}
