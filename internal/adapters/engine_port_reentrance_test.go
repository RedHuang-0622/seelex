package adapters

import (
	"context"
	"sync"
	"testing"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
)

// 本文件只回答一个问题：**回合内**（ReActLoop 正在跑、Session 锁被 ChatStream
// 全程持有）的工具处理函数能不能读/写引擎历史。答案是"不能"，且这条边界由
// compact_context 这个工具踩到——它把 /compact 的落点（context_runtime 装配）
// 交给模型在回合内调用，而那条路径的第一件事就是 engineHistory()。
//
// 判据用真实 Session（不是 fake engine）：Session.mu 的持锁范围只在
// frameworkSession.Session 上存在，替身引擎复现不了。

// scriptedCompleter 第一轮返回一个工具调用，第二轮返回收尾正文。
type scriptedCompleter struct {
	mu       sync.Mutex
	toolName string
	turns    int
}

func (c *scriptedCompleter) next() (string, []types.ToolCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turns++
	if c.turns == 1 {
		return "", []types.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: types.ToolCallFunction{
				Name:      c.toolName,
				Arguments: "{}",
			},
		}}
	}
	return "done", nil
}

func (c *scriptedCompleter) Complete(context.Context, []types.Message, []types.Tool) (types.Message, error) {
	content, calls := c.next()
	return types.Message{Role: "assistant", Content: &content, ToolCalls: calls}, nil
}

func (c *scriptedCompleter) CompleteStream(ctx context.Context, _ []types.Message, _ []types.Tool, _ func(string)) (string, string, []types.ToolCall, error) {
	content, calls := c.next()
	return content, "", calls, nil
}

func (c *scriptedCompleter) CompleteStreamEvents(ctx context.Context, messages []types.Message, tools []types.Tool, _ func(types.StreamEvent)) (string, string, []types.ToolCall, error) {
	return c.CompleteStream(ctx, messages, tools, nil)
}

// probingAgent 在工具派发点（同一 goroutine、Session 锁内）调用宿主注入的
// probe，并把 probe 是否返回如实记下来。
type probingAgent struct {
	llm      types.ChatCompleter
	toolName string
	probe    func()
}

func (a probingAgent) VisibleTools(context.Context) []types.Tool {
	return []types.Tool{{Type: "function", Function: types.ToolFunction{
		Name:        a.toolName,
		Description: "probe",
		Parameters:  map[string]any{"type": "object"},
	}}}
}

func (a probingAgent) Dispatch(ctx context.Context, name, _ string) (string, error) {
	if name != a.toolName {
		return "", nil
	}
	a.probe()
	return "probe returned", nil
}

func (a probingAgent) LLM() types.ChatCompleter { return a.llm }

// TestEngineHistoryFromToolHandlerSelfBlocks 钉住回合内的重入事实：
// ChatStream 从进函数持锁到出函数，工具派发在同一 goroutine 内联执行，因此工具
// 处理函数里的 History()/ReplaceHistory() 排在同一把非重入锁后面——永远等不到。
//
// 用例是"反向"的：它断言 probe **不会**在超时前返回（即确实自锁）。这条断言
// 一旦变绿失败，说明引擎锁的持锁范围变了，compact_context 的回合内语义也要重估。
func TestEngineHistoryFromToolHandlerSelfBlocks(t *testing.T) {
	probeReturned := make(chan struct{})
	completer := &scriptedCompleter{toolName: "compact_context"}
	var sess *frameworkSession.Session
	agent := probingAgent{
		llm:      completer,
		toolName: "compact_context",
		probe: func() {
			// 与 context_runtime.Coordinator.engineHistory 同一条调用：
			// EnginePort.HistoryFor → engine.History() → Session.mu.Lock()。
			_ = sess.History()
			close(probeReturned)
		},
	}
	created, err := frameworkSession.NewSession(frameworkSession.SessionComponents{
		Agent: agent,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess = created
	port := NewEnginePort(sess, nil, nil)
	if !port.SessionBacked() {
		t.Fatalf("期望 session-backed 引擎（生产装配），拿到的是替身")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := port.ChatStreamFor(sess.SessionID(), ctx, "go", nil)
		done <- err
	}()

	select {
	case <-probeReturned:
		t.Fatalf("回合内工具真的读到了引擎历史：Session 锁的持锁范围已变，需重估压缩入口")
	case err := <-done:
		t.Fatalf("ChatStream 提前返回，工具未被派发：err=%v", err)
	case <-time.After(2 * time.Second):
	}
	// 观测点：自锁成立。取消让 loop 退出（工具 goroutine 留在锁上，用例结束）。
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Logf("取消后 ChatStream 未退出（工具卡在 Session 锁内，无法响应 ctx）——这本身就是要记录的事实")
	}
}
