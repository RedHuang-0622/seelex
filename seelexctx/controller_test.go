package seelexctx

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// heavyTokenCounter 放大 token 估算（len×100），让测试用小历史即可跨过
// 软/硬阈值（注入 TokenCounter 契约；独立实现避免值接收者嵌入陷阱）。
type heavyTokenCounter struct{}

func (heavyTokenCounter) Name() string { return "heavy-test-v1" }

func (heavyTokenCounter) CountText(value string) int { return len(value) * 100 }

func (c heavyTokenCounter) CountMessage(message types.Message) int {
	tokens := 4 + c.CountText(message.Role) + c.CountText(messageContent(message)) + c.CountText(message.ReasoningContent)
	if message.ToolCallID != "" {
		tokens += 2 + c.CountText(message.ToolCallID)
	}
	if message.Name != "" {
		tokens += 2 + c.CountText(message.Name)
	}
	for _, call := range message.ToolCalls {
		tokens += 8 + c.CountText(call.ID) + c.CountText(call.Function.Name) + c.CountText(call.Function.Arguments)
	}
	return tokens
}

func (c heavyTokenCounter) CountHistory(history []types.Message) int {
	total := 0
	for _, message := range history {
		total += c.CountMessage(message)
	}
	return total
}

// fixedWindowPolicy 固定窗口轮数 N（测试注入 WindowPolicy）。
type fixedWindowPolicy struct{ rounds int }

func (p fixedWindowPolicy) WindowRounds(context.Context, ProviderContextInfo) (int, error) {
	return p.rounds, nil
}

func textMessage(role, content string) types.Message {
	return types.Message{Role: role, Content: &content}
}

// roundHistory 构造 rounds 轮完整协议单元（user + assistant 文本）。
func roundHistory(rounds int) []types.Message {
	var history []types.Message
	for i := 0; i < rounds; i++ {
		history = append(history,
			textMessage("user", fmt.Sprintf("round-%d-user-input-xxxxxxxxxx", i)),
			textMessage("assistant", fmt.Sprintf("round-%d-assistant-reply-yyyyyyyy", i)),
		)
	}
	return history
}

// newController 构造注入齐全的控制器（窗口固定 n、内存压缩栈、放大计数）。
func newController(window int, stacks CompactStackStore) *seelexContextController {
	return &seelexContextController{
		opts: ControllerOptions{
			Policy:    NewContextWindowPolicy(100_000, 8_192),
			Window:    fixedWindowPolicy{rounds: window},
			Tokens:    heavyTokenCounter{},
			Stacks:    stacks,
			SessionID: "sess-test",
		},
	}
}

// newDefaultWindowController 使用 DefaultWindowPolicy（ratio=0.5、min=2、
// max=40），用于硬阈值收缩窗口的推导测试。
func newDefaultWindowController() *seelexContextController {
	stacks := NewMemoryCompactStack()
	return &seelexContextController{
		opts: ControllerOptions{
			Policy: NewContextWindowPolicy(100_000, 8_192),
			Window: NewDefaultWindowPolicy(WindowConfig{Ratio: 0.5, MinRounds: 2, MaxRounds: 40}),
			Tokens: heavyTokenCounter{},
			Stacks: stacks,
		},
	}
}

func TestControllerWindowOutsideCompression(t *testing.T) {
	stacks := NewMemoryCompactStack()
	controller := newController(3, stacks)
	history := roundHistory(10) // 10 轮 > 窗口 3 → 溢出 7 轮
	decision, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: "继续", History: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.ReplaceHistory {
		t.Fatal("window overflow must replace history")
	}
	// 投影 = 压缩帧块 + 窗口内 3 轮（6 条消息）。
	if len(decision.History) != 1+6 {
		t.Fatalf("projected history length = %d, want 7", len(decision.History))
	}
	if !strings.HasPrefix(*decision.History[0].Content, compactContextMarker) {
		t.Fatal("first projected message must be the compact frame block")
	}
	// 窗口内轮次原样保留（窗口外被压、窗口内原样）。
	windowText := decision.History[1].Content
	if !strings.Contains(*windowText, "round-7-user") {
		t.Fatalf("window-in round missing: %q", *windowText)
	}
	if !strings.Contains(*decision.History[6].Content, "round-9-assistant") {
		t.Fatalf("last window round missing: %q", *decision.History[6].Content)
	}
	for _, message := range decision.History[1:] {
		if strings.Contains(*message.Content, "round-0-user") {
			t.Fatal("window-out round leaked into projected history")
		}
	}
	// CompactFrame.From/To 单元索引断言：溢出 = units[:10-3] → From=0, To=6。
	frames := stacks.Snapshot().CompactStack
	if len(frames) != 1 {
		t.Fatalf("compact stack frames = %d, want 1", len(frames))
	}
	frame := frames[0]
	if frame.From != 0 || frame.To != 6 {
		t.Fatalf("frame range = [%d,%d], want [0,6]", frame.From, frame.To)
	}
	if frame.SegmentID == "" {
		t.Fatal("frame requires segment_id")
	}
}

func TestControllerNoCompressionWithinWindow(t *testing.T) {
	controller := newController(3, NewMemoryCompactStack())
	history := roundHistory(2) // 2 轮 ≤ 窗口 3 → 无溢出
	decision, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: "", History: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.ReplaceHistory {
		t.Fatal("history within window must not be compressed")
	}
}

func TestControllerNoCompressionBelowSoftThreshold(t *testing.T) {
	// 轻量计数（len/3）：10 轮历史远低于软阈值 → 不触发压缩。
	controller := &seelexContextController{
		opts: ControllerOptions{
			Policy: NewContextWindowPolicy(100_000, 8_192),
			Window: fixedWindowPolicy{rounds: 3},
			Tokens: ConservativeTokenCounter{},
			Stacks: NewMemoryCompactStack(),
		},
	}
	history := roundHistory(10)
	decision, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: "继续", History: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.ReplaceHistory {
		t.Fatal("below soft threshold must not compress")
	}
}

// recordStack 是测试用 CompactStackStore：Snapshot 返回预置的完整会话
// 记录（含任务/计划栈帧），PushCompact 追加压缩帧。
type recordStack struct {
	mu     sync.Mutex
	record sessionstore.SessionContextRecord
}

func (s *recordStack) Snapshot() sessionstore.SessionContextRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.record
	record.CompactStack = append([]sessionstore.CompactFrame(nil), s.record.CompactStack...)
	return record
}

func (s *recordStack) PushCompact(frame sessionstore.CompactFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record.CompactStack = append(s.record.CompactStack, frame)
	return nil
}

// TestControllerSegmentClosePreservesGoalPlanEvidence 验证片段闭合压缩
// 后，goal/plan/evidence 保留在栈帧与压缩帧 Summary/Evidence 中。
func TestControllerSegmentClosePreservesGoalPlanEvidence(t *testing.T) {
	store := &recordStack{record: sessionstore.SessionContextRecord{
		TaskStack: []sessionstore.TaskFrame{{
			TaskID: "task-1", Objective: "迁移上下文控制", Status: "active",
			Evidence: []sessionstore.EvidenceRef{{Ref: "result:ev-1", Summary: "证据1"}},
		}},
		PlanStack: []sessionstore.PlanFrame{{PlanID: "plan-1", Title: "重构计划", Status: "active"}},
	}}
	controller := &seelexContextController{
		opts: ControllerOptions{
			Policy:    NewContextWindowPolicy(100_000, 8_192),
			Window:    fixedWindowPolicy{rounds: 3},
			Tokens:    heavyTokenCounter{},
			Stacks:    store,
			SessionID: "sess-goal",
		},
	}
	history := roundHistory(10)
	decision, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: "继续", History: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.ReplaceHistory {
		t.Fatal("expected compaction decision")
	}
	// 压缩帧 Summary 保留 goal/plan；Evidence 保留任务证据引用。
	frames := store.Snapshot().CompactStack
	if len(frames) != 1 {
		t.Fatalf("compact frames = %d, want 1", len(frames))
	}
	if !strings.Contains(frames[0].Summary, "迁移上下文控制") {
		t.Fatalf("summary must preserve task goal: %s", frames[0].Summary)
	}
	if !strings.Contains(frames[0].Summary, "重构计划") {
		t.Fatalf("summary must preserve plan: %s", frames[0].Summary)
	}
	foundEvidence := false
	for _, ref := range frames[0].Evidence {
		if ref.Ref == "result:ev-1" {
			foundEvidence = true
		}
	}
	if !foundEvidence {
		t.Fatalf("frame evidence must include task evidence, got %+v", frames[0].Evidence)
	}
	// 栈帧本身不被压缩触碰（goal/plan 仍在原帧中）。
	record := store.Snapshot()
	if len(record.TaskStack) != 1 || record.TaskStack[0].Objective != "迁移上下文控制" {
		t.Fatal("task stack frame must be preserved")
	}
	if len(record.PlanStack) != 1 || record.PlanStack[0].Title != "重构计划" {
		t.Fatal("plan stack frame must be preserved")
	}
}

func TestControllerHardThresholdArchivesAndShrinksWindow(t *testing.T) {
	controller := newDefaultWindowController()
	history := roundHistory(10) // 10 轮，heavy 计数 → 跨软阈值
	archiver := NewInMemoryToolResultArchiver()
	controller.opts.Archive = archiver

	decision, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind:    seelectx.ContextAfterTool,
		Turn:    1,
		Query:   "查询",
		History: history,
		Tool: &seelectx.ToolResult{
			CallID: "call-oversized", Name: "bash",
			Raw: strings.Repeat("x", 10_000), // 超过 4000 字符预算
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 硬阈值路径：超大工具输出先归档为 result_ref（processor 路径）。
	raw, ok := archiver.Read("result:call-oversized")
	if !ok || len(raw) != 10_000 {
		t.Fatal("oversized tool result must be archived with result_ref")
	}
	if !decision.ReplaceHistory {
		t.Fatal("hard threshold must replace history")
	}
	// 窗口收缩：软路径 n=5（(50000−12500)/7008）→ 硬路径 n=3
	// （(HardThreshold×0.5−12500)/7008），不低于 MinRounds=2。
	// 10 轮 → 硬路径溢出 7 轮 → CompactFrame.To = 6。
	frames := controller.opts.Stacks.Snapshot().CompactStack
	if len(frames) != 1 {
		t.Fatalf("compact stack frames = %d, want 1", len(frames))
	}
	if frames[0].To != 6 {
		t.Fatalf("hard-path frame.To = %d, want 6 (window shrunk to 3)", frames[0].To)
	}
	// 窗口保留：最后 3 轮（6 条消息）+ 压缩帧块。
	if len(decision.History) != 1+6 {
		t.Fatalf("hard-path projected length = %d, want 7", len(decision.History))
	}
	if !strings.Contains(*decision.History[1].Content, "round-7-user") {
		t.Fatalf("shrunk window should keep round-7, got %q", *decision.History[1].Content)
	}
}

func TestControllerPreviousFrameMergedStackTopSelfSufficient(t *testing.T) {
	stacks := NewMemoryCompactStack()
	controller := newController(3, stacks)

	// 第一次压缩：10 轮 → 溢出 7 轮。
	first, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: "继续", History: roundHistory(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.ReplaceHistory {
		t.Fatal("first compaction must replace history")
	}
	frames := stacks.Snapshot().CompactStack
	if len(frames) != 1 || frames[0].To != 6 {
		t.Fatalf("first frame = %+v, want To=6", frames)
	}
	firstSummary := frames[0].Summary

	// 第二次压缩：12 轮 → 溢出 9 轮（相对上次 7 轮有新溢出）。
	second, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 2, Query: "继续", History: roundHistory(12),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.ReplaceHistory {
		t.Fatal("new overflow must compress again")
	}
	frames = stacks.Snapshot().CompactStack
	if len(frames) != 2 {
		t.Fatalf("compact stack frames = %d, want 2", len(frames))
	}
	merged := frames[1]
	// 帧范围接续上一帧起点（窗口外全部轮次的连续段，栈顶自足）。
	if merged.From != 0 || merged.To != 8 {
		t.Fatalf("merged frame range = [%d,%d], want [0,8]", merged.From, merged.To)
	}
	if !strings.Contains(merged.Summary, firstSummary) {
		t.Fatal("merged frame must include previous top summary (stack top self-sufficient)")
	}
}

func TestControllerCheckpointCleanupBeforeReplace(t *testing.T) {
	stacks := NewMemoryCompactStack()
	controller := newController(3, stacks)
	history := roundHistory(10)
	// 注入旧应用检查点标记（替换历史时必须清理，不进入投影）。
	checkpoint := textMessage("user", checkpointMarker+"\n{\"version\":1}")
	history = append([]types.Message{checkpoint}, history...)

	decision, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: "继续", History: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.ReplaceHistory {
		t.Fatal("expected compaction decision")
	}
	for _, message := range decision.History {
		if message.Content != nil && strings.HasPrefix(*message.Content, checkpointMarker) {
			t.Fatal("checkpoint marker must be cleaned before ReplaceHistory")
		}
	}
	// 压缩帧块只保留一个（新帧）。
	count := 0
	for _, message := range decision.History {
		if message.Content != nil && strings.HasPrefix(*message.Content, compactContextMarker) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("compact frame blocks in projection = %d, want 1", count)
	}
}

func TestControllerToolChainUnits(t *testing.T) {
	controller := newController(2, NewMemoryCompactStack())
	// 工具链轮（闭合于 assistant 文本）+ 2 文本轮 = 3 单元 > 窗口 2 → 溢出 1 单元。
	big := strings.Repeat("数据内容", 50) // 150 字符 → 15000 tokens（heavy 计数）
	history := []types.Message{
		textMessage("user", "执行工具"+big),
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "c1", Function: types.ToolCallFunction{Name: "read_file"}}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: stringPtr("文件内容" + big)},
		textMessage("assistant", "工具执行完成"+big),
		textMessage("user", "继续"+big),
		textMessage("assistant", "好的"+big),
		textMessage("user", "再来"+big),
		textMessage("assistant", "完成"+big),
	}
	units := controller.chatUnits(history)
	if len(units) != 3 {
		t.Fatalf("units = %d, want 3 (tool chain, text, text)", len(units))
	}
	decision, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: "", History: history,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.ReplaceHistory {
		t.Fatal("window overflow must compress")
	}
	frames := controller.opts.Stacks.Snapshot().CompactStack
	if len(frames) != 1 || frames[0].To != 0 {
		t.Fatalf("frame = %+v, want To=0 (1 overflow unit)", frames)
	}
	// 工具链单元（assistant + tool 配对）整体进入压缩帧 Summary，不拆散。
	if !strings.Contains(frames[0].Summary, "read_file") {
		t.Fatalf("frame summary must keep the tool chain, got: %s", frames[0].Summary)
	}
	// 窗口 = units[1:] 起始于消息 4（继续）→ 4 条消息 + 压缩帧块。
	if len(decision.History) != 1+4 {
		t.Fatalf("projected length = %d, want 5", len(decision.History))
	}
	if !strings.Contains(*decision.History[1].Content, "继续") {
		t.Fatalf("window should start at round-2 user, got %q", *decision.History[1].Content)
	}
}

func stringPtr(value string) *string { return &value }
