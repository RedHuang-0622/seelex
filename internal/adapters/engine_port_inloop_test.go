package adapters

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract"
)

// 本文件钉住环内通道的正面事实：回合内的工具 handler 凭 ChatStream 注入的 ctx 就能
// 读/写引擎历史，**当场生效**——同回合的下一次模型请求读到的已是替换后的历史，不经
// 过「下一次装载」。反向事实（回合内 Session.History() 自锁）由
// engine_port_reentrance_test.go 钉住；两条一起说明"为什么只能走这条门"。

// foldProbe 在工具派发点（ChatStream 同一 goroutine、会话锁已持有）执行，返回值即
// 本次工具结果正文。
type foldProbe func(t *testing.T, port *EnginePort, ctx context.Context) string

// foldAgent 把 probe 挂在一次工具调用上；completer 负责第一轮发起工具调用、第二轮
// 收尾，并记录每次请求收到的消息序列。
type foldAgent struct {
	llm      *foldCompleter
	toolName string
	t        *testing.T
	port     **EnginePort
	probe    foldProbe
}

func (a foldAgent) VisibleTools(context.Context) []types.Tool {
	return []types.Tool{{Type: "function", Function: types.ToolFunction{
		Name: a.toolName, Description: "in-loop fold probe",
		Parameters: map[string]any{"type": "object"},
	}}}
}

func (a foldAgent) Dispatch(ctx context.Context, name, _ string) (string, error) {
	if name != a.toolName {
		return "", nil
	}
	return a.probe(a.t, *a.port, ctx), nil
}

func (a foldAgent) LLM() types.ChatCompleter { return a.llm }

// foldCompleter：第 1 次请求返回一个工具调用，之后返回收尾正文；requests 记录每次
// 请求实际带的历史（用来判断「第 N 次请求看到的是哪份历史」）。
type foldCompleter struct {
	mu       sync.Mutex
	toolName string
	turns    int
	requests [][]types.Message
}

func (c *foldCompleter) next(messages []types.Message) (string, []types.ToolCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turns++
	c.requests = append(c.requests, append([]types.Message(nil), messages...))
	if c.turns == 1 {
		return "", []types.ToolCall{{
			ID: "call-fold-1", Type: "function",
			Function: types.ToolCallFunction{Name: c.toolName, Arguments: "{}"},
		}}
	}
	return "done", nil
}

func (c *foldCompleter) Complete(_ context.Context, messages []types.Message, _ []types.Tool) (types.Message, error) {
	content, calls := c.next(messages)
	return types.Message{Role: "assistant", Content: &content, ToolCalls: calls}, nil
}

func (c *foldCompleter) CompleteStream(_ context.Context, messages []types.Message, _ []types.Tool, _ func(string)) (string, string, []types.ToolCall, error) {
	content, calls := c.next(messages)
	return content, "", calls, nil
}

func (c *foldCompleter) CompleteStreamEvents(ctx context.Context, messages []types.Message, tools []types.Tool, _ func(types.StreamEvent)) (string, string, []types.ToolCall, error) {
	return c.CompleteStream(ctx, messages, tools, nil)
}

func (c *foldCompleter) snapshot() [][]types.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]types.Message(nil), c.requests...)
}

func requestHas(messages []types.Message, want string) bool {
	for _, message := range messages {
		if message.Content != nil && strings.Contains(*message.Content, want) {
			return true
		}
	}
	return false
}

// runFoldTurn 起一个真实 Session（会话锁语义只在这里存在，替身引擎复现不了），让
// 模型在第一轮调用 compact_context，probe 就在该次派发内跑。
func runFoldTurn(t *testing.T, probe foldProbe) (*frameworkSession.Session, *foldCompleter) {
	t.Helper()
	completer := &foldCompleter{toolName: "compact_context"}
	var port *EnginePort
	agent := foldAgent{llm: completer, toolName: "compact_context", t: t, port: &port, probe: probe}
	created, err := frameworkSession.NewSession(frameworkSession.SessionComponents{Agent: agent})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	port = NewEnginePort(created, nil, nil)
	if !port.SessionBacked() {
		t.Fatalf("期望 session-backed 引擎（生产装配），拿到的是替身")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := port.ChatStreamFor(created.SessionID(), ctx, "go", nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ChatStreamFor: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("回合未在超时内收尾（疑似自锁）")
	}
	return created, completer
}

// TestInLoopReplaceTakesEffectInSameTurn 就是这次要交付的行为：一次调用当场压完，
// 同回合的下一次请求读到的已是折叠后的历史。
func TestInLoopReplaceTakesEffectInSameTurn(t *testing.T) {
	var (
		readOK    bool
		readCount int
		handled   bool
		writeErr  error
	)
	sess, completer := runFoldTurn(t, func(t *testing.T, port *EnginePort, ctx context.Context) string {
		history, ok := port.HistoryInLoop(ctx)
		readOK, readCount = ok, len(history)
		if !ok {
			return "no in-loop handle"
		}
		// 折叠产物 = 压缩帧 + 正在飞的那一截（尾部保留）。
		folded := []contract.EngineMessage{{Role: "user", Content: "COMPACTED-FRAME", ContentSet: true}}
		for _, message := range history {
			if message.Role == "assistant" && len(message.ToolCalls) > 0 {
				folded = append(folded, message)
			}
		}
		handled, writeErr = port.ReplaceHistoryInLoop(ctx, folded)
		return "folded in-loop"
	})

	if !readOK {
		t.Fatal("回合内取不到环内把手（ctx 未携带本轮能力）")
	}
	if readCount != 2 {
		t.Fatalf("环内读到的历史长度 = %d, want 2（user + assistant tool_calls）", readCount)
	}
	if !handled || writeErr != nil {
		t.Fatalf("环内替换未被处理：handled=%v err=%v", handled, writeErr)
	}
	requests := completer.snapshot()
	if len(requests) != 2 {
		t.Fatalf("模型调用次数 = %d, want 2", len(requests))
	}
	if requestHas(requests[0], "COMPACTED-FRAME") {
		t.Fatal("折叠不该出现在第一次请求里")
	}
	if !requestHas(requests[1], "COMPACTED-FRAME") {
		t.Fatal("环内替换未即时生效：同回合的下一次请求看不到折叠帧")
	}
	if requestHas(requests[1], "go") {
		t.Fatal("折叠后旧输入仍在请求里")
	}
	final := sess.History()
	// 帧 + 在飞 assistant + 循环随后 append 的 tool 结果 + 收尾 assistant。
	if len(final) != 4 {
		t.Fatalf("终态历史长度 = %d, want 4：%+v", len(final), final)
	}
	if final[0].Content == nil || *final[0].Content != "COMPACTED-FRAME" {
		t.Fatalf("折叠帧没有落在历史首位：%+v", final[0])
	}
	if final[1].Role != "assistant" || len(final[1].ToolCalls) == 0 {
		t.Fatal("在飞 assistant 被丢了")
	}
	if final[2].Role != "tool" || final[2].ToolCallID != "call-fold-1" {
		t.Fatalf("tool 结果成孤儿：role=%s callID=%s", final[2].Role, final[2].ToolCallID)
	}
}

// TestInLoopDropsInFlightTailIsRefused 钉住引擎侧下界：环内替换若会丢掉正在飞的
// tool_call 单元（其结果尚未 append），必须被拒且不动历史——否则紧随其后 append 的
// 结果行成孤儿。core 侧 withInFlightTail 正是为了不触发这条拒绝。
func TestInLoopDropsInFlightTailIsRefused(t *testing.T) {
	var (
		handled    bool
		refuseErr  error
		lenAfter   int
		restoredOK bool
	)
	_, _ = runFoldTurn(t, func(t *testing.T, port *EnginePort, ctx context.Context) string {
		history, ok := port.HistoryInLoop(ctx)
		if !ok {
			t.Error("探针取不到环内把手")
			return "no handle"
		}
		handled, refuseErr = port.ReplaceHistoryInLoop(ctx, []contract.EngineMessage{
			{Role: "user", Content: "COMPACTED-FRAME", ContentSet: true},
		})
		after, _ := port.HistoryInLoop(ctx)
		lenAfter = len(after)
		// 接回在飞尾部，让本轮正常收尾。
		restoredOK, _ = port.ReplaceHistoryInLoop(ctx, history)
		return "tail preserved"
	})

	if refuseErr == nil {
		t.Fatal("丢掉在飞尾部的环内替换被接受了（引擎下界失效）")
	}
	if !handled {
		t.Fatalf("端口应回报「在环内但被拒」而不是「不在环内」：handled=%v", handled)
	}
	if lenAfter != 2 {
		t.Fatalf("被拒的替换改动了历史：len=%d want 2", lenAfter)
	}
	if !restoredOK {
		t.Fatal("保留尾部的替换没被受理")
	}
}

// TestInLoopEngineUnavailableOutsideTurn 是回落判据：环外 ctx 一律 ok=false /
// handled=false 且无错误，调用方据此回落到取锁方法（不得凭此伪造「已折叠」）。
func TestInLoopEngineUnavailableOutsideTurn(t *testing.T) {
	completer := &foldCompleter{toolName: "compact_context"}
	created, err := frameworkSession.NewSession(frameworkSession.SessionComponents{
		Agent: foldAgent{llm: completer, toolName: "compact_context", t: t},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	port := NewEnginePort(created, nil, nil)

	if _, ok := port.HistoryInLoop(context.Background()); ok {
		t.Fatal("环外不该取到环内历史")
	}
	history := []contract.EngineMessage{{Role: "user", Content: "x", ContentSet: true}}
	if handled, err := port.ReplaceHistoryInLoop(context.Background(), history); handled || err != nil {
		t.Fatalf("环外替换应回报「未处理、无错误」：handled=%v err=%v", handled, err)
	}
	if handled, err := port.SetSystemPromptInLoop(context.Background(), "p"); handled || err != nil {
		t.Fatalf("环外 prompt 写入应回报「未处理、无错误」：handled=%v err=%v", handled, err)
	}
}
