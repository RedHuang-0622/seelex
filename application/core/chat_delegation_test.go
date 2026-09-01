package core

import (
	"context"
	"sync"
	"testing"
)

// singleLoopEngine 模拟 Seele loop 在单次 ChatStream 内完成多轮
// （工具调用轮 + 最终回复轮）：core 若自编 ReAct 循环会再次提交引擎，
// 测试通过提交次数断言委托边界（UC6 结构断言）。
type singleLoopEngine struct {
	*fakeEngine
	mu     sync.Mutex
	calls  int
	inputs []string
}

func (engine *singleLoopEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.mu.Lock()
	engine.calls++
	engine.inputs = append(engine.inputs, input)
	engine.mu.Unlock()
	for _, chunk := range []string{"tool-round", "final-round"} {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
			onChunk(chunk)
		}
	}
	return "final answer", nil
}

// ChatStreamFor 显式转发到自身 ChatStream（会话路由面下仍单次提交）。
func (engine *singleLoopEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	return engine.ChatStream(ctx, input, onChunk)
}

// TestLoopDelegatedToSeele（UC6）：core 无自编 ReAct 循环——一次提交只
// 调用一次引擎提交（ChatStreamFor 会话路由面），工具轮次由 Seele loop 在
// 单次 ChatStream 内完成；core 只做视图/事件投影。
func TestLoopDelegatedToSeele(t *testing.T) {
	engine := &singleLoopEngine{fakeEngine: &fakeEngine{}}
	service := newTestService(t, engine)

	if err := service.Submit(context.Background(), "do the work"); err != nil {
		t.Fatal(err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}

	engine.mu.Lock()
	calls := engine.calls
	inputs := append([]string(nil), engine.inputs...)
	engine.mu.Unlock()
	if calls != 1 {
		t.Fatalf("core submitted to engine %d times, want exactly 1 (loop delegated to Seele)", calls)
	}
	if len(inputs) != 1 || inputs[0] == "" {
		t.Fatalf("engine inputs = %+v, want a single non-empty submission", inputs)
	}

	snapshot := service.Snapshot()
	if len(snapshot.Conversation) < 2 {
		t.Fatalf("conversation = %+v, want user + assistant", snapshot.Conversation)
	}
	last := snapshot.Conversation[len(snapshot.Conversation)-1]
	if last.Role != "assistant" {
		t.Fatalf("last message role = %q, want assistant", last.Role)
	}
}
