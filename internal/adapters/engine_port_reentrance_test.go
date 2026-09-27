package adapters

import (
	"context"
	"sync"
	"testing"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
)

// 本文件只回答一个问题：**回合内**（ReActLoop 正在跑）的工具处理函数能不能读/写引擎
// 历史。Seele 方案 B 之后的答案是"能"，且不需要任何环内把手：
//
//   - `Session.History()` 只取工作状态的短临界区，任何时刻都立即返回；
//   - `Session.ReplaceHistory()` 在回合忙时排队到循环的下一个检查点。
//
// 这条边界正是 compact_context 走的路径（它把 /compact 的落点交给模型在回合内调用，
// 那条路径的第一件事就是 engineHistory()）。判据用真实 Session（不是替身引擎）：
// 回合闸门与工作状态只在 frameworkSession.Session 上存在。
//
// 历史沿革：旧实现里 ChatStream 从进函数持锁到出函数，这条路径是**自锁**的
// （TestEngineHistoryFromToolHandlerSelfBlocks，需要 ctx 把手绕开）。升级后那条断言
// 反转成"必须立刻返回"——它同时是「引擎模型换了」的报警器：一旦这里又开始阻塞，
// 说明回合又持上了跨整轮的锁。

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

// probingAgent 在工具派发点调用宿主注入的 probe，并把 probe 看到的数量记下来。
type probingAgent struct {
	llm         types.ChatCompleter
	toolName    string
	probe       func() int
	probeReturn chan int
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
	select {
	case a.probeReturn <- a.probe():
	default:
	}
	return "probe returned", nil
}

func (a probingAgent) LLM() types.ChatCompleter { return a.llm }

// TestEngineHistoryFromToolHandlerReturnsPromptly 钉住回合内读历史**立刻返回**：
// 回合内不再持有跨整轮的会话锁，工具 handler 读到的就是当前工作历史（旧实现在这里
// 永久自锁，只能靠 ctx 把手绕开——那条把手已随 Seele 升级删除）。
func TestEngineHistoryFromToolHandlerReturnsPromptly(t *testing.T) {
	probeReturn := make(chan int, 1)
	completer := &scriptedCompleter{toolName: "compact_context"}
	var sess *frameworkSession.Session
	agent := probingAgent{
		llm:         completer,
		toolName:    "compact_context",
		probe:       func() int { return len(sess.History()) },
		probeReturn: probeReturn,
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
	case count := <-probeReturn:
		// 此刻历史里应有：user("go") + assistant(tool_calls)。
		if count < 2 {
			t.Fatalf("回合内读到的历史长度 = %d, want ≥2", count)
		}
	case err := <-done:
		t.Fatalf("ChatStream 提前返回，工具未被派发：err=%v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("回合内工具读历史没有返回：回合又持上了跨整轮的锁（自锁回归）")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ChatStreamFor: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("回合未在超时内收尾")
	}
}
