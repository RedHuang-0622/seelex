package adapters

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract"
)

// 本文件钉住「回合进行中写历史」的正面事实与下界（Seele 方案 B 之后的行为）：
//
//   - 工具 handler（ChatStream 同一 goroutine）在回合内调**普通公开方法**
//     （EnginePort.ReplaceHistoryFor → session.Session.ReplaceHistory）无需任何
//     「环内把手」：替换被排队到循环的下一个检查点，**本回合的下一次模型请求**
//     读到的就是折叠后的历史（当场生效，不经过"下一次装载"）。
//   - 引擎仍拒收会丢掉在飞 tool_call 单元的替换（ErrInFlightToolCallDropped），
//     且被拒时历史不动——core 侧 withInFlightTail 正是为了不触发这条拒绝。
//
// 旧模型（回合从进函数持锁到出函数 + session.InLoop 把手）下，这里测的是
// HistoryInLoop/ReplaceHistoryInLoop；把手与整条环内通道已随 Seele 升级删除，
// 对应用例改为直接钉"新模型下同一件事仍然成立"。

// foldProbe 在工具派发点（ChatStream 同一 goroutine）执行，返回值即本次工具结果正文。
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
		Name: a.toolName, Description: "in-turn fold probe",
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

// runFoldTurn 起一个真实 Session（回合闸门与工作状态只在它身上存在，替身引擎复现
// 不了），让模型在第一轮调用 compact_context，probe 就在该次派发内跑。
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

// inFlightFold 返回一次折叠产物：压缩帧 + 当前历史里**正在飞的那一截**
// （assistant 带 tool_calls、其结果尚未 append）。尾部必须保留，否则引擎拒收。
func inFlightFold(history []contract.EngineMessage) []contract.EngineMessage {
	folded := []contract.EngineMessage{{Role: "user", Content: "COMPACTED-FRAME", ContentSet: true}}
	for _, message := range history {
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			folded = append(folded, message)
		}
	}
	return folded
}

// TestReplaceHistoryInsideTurnTakesEffectInSameTurn 是要交付的行为：回合内的工具
// handler 用**普通公开方法**提交替换（不再需要环内把手，也不需要 ctx 透传），替换
// 在循环的下一个检查点落地，同回合的下一次请求读到的已是折叠后的历史。
func TestReplaceHistoryInsideTurnTakesEffectInSameTurn(t *testing.T) {
	var (
		readCount int
		writeErr  error
	)
	sess, completer := runFoldTurn(t, func(t *testing.T, port *EnginePort, _ context.Context) string {
		// 读：会话当前工作历史（永不阻塞，随时可读）。
		history := port.HistoryFor(port.SessionID())
		readCount = len(history)
		writeErr = port.ReplaceHistoryFor(port.SessionID(), inFlightFold(history))
		return "folded in-turn"
	})

	if readCount != 2 {
		t.Fatalf("回合内读到的历史长度 = %d, want 2（user + assistant tool_calls）", readCount)
	}
	if writeErr != nil {
		t.Fatalf("回合内替换被拒：%v", writeErr)
	}
	requests := completer.snapshot()
	if len(requests) != 2 {
		t.Fatalf("模型调用次数 = %d, want 2", len(requests))
	}
	if requestHas(requests[0], "COMPACTED-FRAME") {
		t.Fatal("折叠不该出现在第一次请求里")
	}
	if !requestHas(requests[1], "COMPACTED-FRAME") {
		t.Fatal("回合内替换未即时生效：同回合的下一次请求看不到折叠帧")
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

// TestReplaceHistoryDropsInFlightTailIsRefused 钉住引擎侧下界：替换若会丢掉正在飞的
// tool_call 单元（其结果尚未 append），必须被拒且不改历史——否则紧随其后 append 的
// 结果行成孤儿。core 侧 withInFlightTail 正是为了不触发这条拒绝。
func TestReplaceHistoryDropsInFlightTailIsRefused(t *testing.T) {
	var (
		refuseErr  error
		lenAfter   int
		restoreErr error
	)
	_, _ = runFoldTurn(t, func(t *testing.T, port *EnginePort, _ context.Context) string {
		history := port.HistoryFor(port.SessionID())
		refuseErr = port.ReplaceHistoryFor(port.SessionID(), []contract.EngineMessage{
			{Role: "user", Content: "COMPACTED-FRAME", ContentSet: true},
		})
		lenAfter = len(port.HistoryFor(port.SessionID()))
		// 接回在飞尾部，让本轮正常收尾。
		restoreErr = port.ReplaceHistoryFor(port.SessionID(), inFlightFold(history))
		return "tail preserved"
	})

	if refuseErr == nil {
		t.Fatal("丢掉在飞尾部的替换被接受了（引擎下界失效）")
	}
	if !errors.Is(refuseErr, frameworkSession.ErrInFlightToolCallDropped) {
		t.Fatalf("拒收理由不是「会丢在飞 tool_call 单元」：%v", refuseErr)
	}
	if lenAfter != 2 {
		t.Fatalf("被拒的替换改动了历史：len=%d want 2", lenAfter)
	}
	if restoreErr != nil {
		t.Fatalf("保留尾部的替换被拒：%v", restoreErr)
	}
}

// TestReplaceHistoryOutsideTurnAppliesImmediately 是锁外路径的对照：没有回合在飞时
// 替换当场落地（不需要 ctx、不需要把手）。
func TestReplaceHistoryOutsideTurnAppliesImmediately(t *testing.T) {
	completer := &foldCompleter{toolName: "compact_context"}
	created, err := frameworkSession.NewSession(frameworkSession.SessionComponents{
		Agent: foldAgent{llm: completer, toolName: "compact_context", t: t},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	port := NewEnginePort(created, nil, nil)
	created.AppendHistory(types.Message{Role: "user", Content: ptrText("seed")})

	if err := port.ReplaceHistoryFor(created.SessionID(), []contract.EngineMessage{
		{Role: "user", Content: "COMPACTED-FRAME", ContentSet: true},
	}); err != nil {
		t.Fatalf("锁外替换: %v", err)
	}
	history := port.HistoryFor(created.SessionID())
	if len(history) != 1 || history[0].Content != "COMPACTED-FRAME" {
		t.Fatalf("锁外替换没有当场生效：%+v", history)
	}
}

func ptrText(value string) *string { return &value }
