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
			Policy:            NewContextWindowPolicy(100_000, 8_192),
			Window:            fixedWindowPolicy{rounds: window},
			Tokens:            heavyTokenCounter{},
			Stacks:            stacks,
			SessionIDProvider: func() string { return "sess-test" },
		},
		lastCompactedTo: -1, // 与 NewContextController 初值一致（首帧 To=0 不被去重误杀）
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
	// SegmentID 溯源到 provider 给出的会话 ID（compact-<sessionID>-<ms>）。
	if !strings.HasPrefix(frame.SegmentID, "compact-sess-test-") {
		t.Fatalf("frame segment_id = %q, want compact-sess-test- prefix", frame.SegmentID)
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
			Policy:            NewContextWindowPolicy(100_000, 8_192),
			Window:            fixedWindowPolicy{rounds: 3},
			Tokens:            heavyTokenCounter{},
			Stacks:            store,
			SessionIDProvider: func() string { return "sess-goal" },
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
			Raw: strings.Repeat("x", 30_000), // 超过 seelex 默认 20000 字符预算
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 硬阈值路径：超大工具输出先归档为 result_ref（processor 路径）。
	raw, ok := archiver.Read("result:call-oversized")
	if !ok || len(raw) != 30_000 {
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

	// 第二次压缩：真实流程输入 = 首次压缩的投影历史（窗口 3 轮）
	// + 2 新轮 = 5 单元 / 窗口 3 → 新溢出 2 轮；累计 To = 6 + 2 = 8
	// （ChatQueue 稳定索引，审计 R1 修正：不再传完整 12 轮——那是旧
	// 坐标空间模型）。消息加长以跨过软阈值（heavy 计数 len×100）。
	secondHistory := make([]types.Message, 0, 10)
	for i := 0; i < 5; i++ {
		pad := strings.Repeat("x", 40)
		secondHistory = append(secondHistory,
			textMessage("user", fmt.Sprintf("round-%d-user-input-%s", i, pad)),
			textMessage("assistant", fmt.Sprintf("round-%d-assistant-reply-%s", i, pad)))
	}
	second, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 2, Query: "继续", History: secondHistory,
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

// TestControllerEqualSizedOverflowBatchesBothCompress 审计 R2 回归：
// 两次溢出批次尺寸相同（各 2 轮）但内容不同（新溢出轮次），第二次
// 必须压缩——去重基准是"是否有新溢出内容"（累计 To 边界），
// 而非溢出批次尺寸。
func TestControllerEqualSizedOverflowBatchesBothCompress(t *testing.T) {
	stacks := NewMemoryCompactStack()
	controller := newController(3, stacks)

	// 第一次：5 轮长消息（跨软阈值），窗口 3 → 溢出 2 → 首帧 To=1。
	firstHistory := make([]types.Message, 0, 10)
	for i := 0; i < 5; i++ {
		pad := strings.Repeat("x", 40)
		firstHistory = append(firstHistory,
			textMessage("user", fmt.Sprintf("round-%d-user-input-%s", i, pad)),
			textMessage("assistant", fmt.Sprintf("round-%d-assistant-reply-%s", i, pad)))
	}
	first, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: "继续", History: firstHistory,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.ReplaceHistory {
		t.Fatal("first compaction must replace history")
	}
	firstTo := stacks.Snapshot().CompactStack[0].To // 5 轮/窗口 3 → 溢出 2 → To=1

	// 第二次：投影窗口 3 轮 + 2 新轮（同样溢出 2 轮 = 同尺寸批次）。
	secondHistory := make([]types.Message, 0, 10)
	for i := 0; i < 5; i++ {
		pad := strings.Repeat("y", 40)
		secondHistory = append(secondHistory,
			textMessage("user", fmt.Sprintf("round-%d-user-input-%s", i, pad)),
			textMessage("assistant", fmt.Sprintf("round-%d-assistant-reply-%s", i, pad)))
	}
	second, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 2, Query: "继续", History: secondHistory,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.ReplaceHistory {
		t.Fatal("equal-sized overflow batch with new content must compress (R2)")
	}
	frames := stacks.Snapshot().CompactStack
	if len(frames) != 2 {
		t.Fatalf("compact stack frames = %d, want 2", len(frames))
	}
	// 累计 To：首帧 To=1，合并帧 To = 1 + 2 = 3（ChatQueue 稳定索引）。
	if merged := frames[1]; merged.From != 0 || merged.To != firstTo+2 {
		t.Fatalf("merged frame range = [%d,%d], want [0,%d]", merged.From, merged.To, firstTo+2)
	}
}

// TestControllerOrphanMessagesBetweenOverflowAndWindow 审计 R3 回归：
// 溢出区与窗口起点之间的非单元消息（未闭合工具链，tool 结果缺失）
// 必须随窗口保留，不静默丢弃。
func TestControllerOrphanMessagesBetweenOverflowAndWindow(t *testing.T) {
	controller := newController(2, NewMemoryCompactStack())
	big := strings.Repeat("数据内容", 50)
	// 单元：轮0（user+assistant）、轮1（user+assistant）、轮2（user+assistant）
	// 孤儿 = assistant(c0)（工具调用无 tool 结果配对 → 不构成单元），
	// 位于溢出区（轮0）与窗口（轮1 起）之间。
	history := []types.Message{
		textMessage("user", "轮0-用户"+big),
		textMessage("assistant", "轮0-回复"+big),
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "c0", Function: types.ToolCallFunction{Name: "read_file"}}}},
		textMessage("user", "轮1-用户"+big),
		textMessage("assistant", "轮1-回复"+big),
		textMessage("user", "轮2-用户"+big),
		textMessage("assistant", "轮2-回复"+big),
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
	// 投影 = 帧块 + 孤儿（assistant c0）+ 窗口 2 轮（轮1、轮2）。
	if len(decision.History) != 1+1+4 {
		t.Fatalf("projected length = %d, want 6 (frame + orphan + 2 window rounds)", len(decision.History))
	}
	foundOrphan := false
	for _, message := range decision.History {
		if message.Role == "assistant" && len(message.ToolCalls) > 0 && message.ToolCalls[0].ID == "c0" {
			foundOrphan = true
			break
		}
	}
	if !foundOrphan {
		t.Fatal("orphan assistant message between overflow and window must be preserved (R3)")
	}
}

// recordingTurnArchiver 是 TurnArchiver 测试替身：记录归档调用。
type recordingTurnArchiver struct {
	segmentIDs []string
	messageN   []int
}

func (a *recordingTurnArchiver) StoreTurn(_ context.Context, segmentID string, messages []types.Message) (string, error) {
	a.segmentIDs = append(a.segmentIDs, segmentID)
	a.messageN = append(a.messageN, len(messages))
	return "compressed:" + segmentID, nil
}

// TestControllerCompressionArchivesTurnOriginal 审计修复验证：压缩时
// 溢出轮次原文经 TurnArchiver 持久化，帧 Evidence 携带读回句柄，
// Summary 提示 read_compressed_turn——压缩丢失可逆。
func TestControllerCompressionArchivesTurnOriginal(t *testing.T) {
	stacks := NewMemoryCompactStack()
	archiver := &recordingTurnArchiver{}
	controller := &seelexContextController{
		opts: ControllerOptions{
			Policy:            NewContextWindowPolicy(100_000, 8_192),
			Window:            fixedWindowPolicy{rounds: 3},
			Tokens:            heavyTokenCounter{},
			Stacks:            stacks,
			SessionIDProvider: func() string { return "sess-archive" },
			Turns:             archiver,
		},
		lastCompactedTo: -1,
	}
	decision, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: "继续", History: roundHistory(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.ReplaceHistory {
		t.Fatal("window overflow must compress")
	}
	// 归档被调用：1 段，7 个溢出单元的消息（10 轮 - 窗口 3 轮）。
	if len(archiver.segmentIDs) != 1 || archiver.messageN[0] != 14 {
		t.Fatalf("archive calls = %d (messages %v), want 1 segment with 14 messages", len(archiver.segmentIDs), archiver.messageN)
	}
	frames := stacks.Snapshot().CompactStack
	if len(frames) != 1 {
		t.Fatalf("compact stack frames = %d, want 1", len(frames))
	}
	frame := frames[0]
	// Evidence 携带读回句柄（ref = compressed:<segment_id>）。
	foundHandle := false
	for _, evidence := range frame.Evidence {
		if evidence.Ref == "compressed:"+frame.SegmentID {
			foundHandle = true
			break
		}
	}
	if !foundHandle {
		t.Fatalf("frame evidence = %+v, want read-back handle ref", frame.Evidence)
	}
	// Summary 提示模型可读回原文。
	if !strings.Contains(frame.Summary, "read_compressed_turn") {
		t.Fatalf("frame summary must advertise read_compressed_turn, got: %s", frame.Summary)
	}
}

// TestControllerCompressionSkipsArchiveWithoutArchiver 无 TurnArchiver 注入
// 时压缩照常进行（不持久化原文，仅摘要）。
func TestControllerCompressionSkipsArchiveWithoutArchiver(t *testing.T) {
	controller := newController(3, NewMemoryCompactStack())
	decision, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: "继续", History: roundHistory(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.ReplaceHistory {
		t.Fatal("window overflow must compress without archiver")
	}
}
