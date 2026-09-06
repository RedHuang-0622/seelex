package seelexctx

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
	frameworktypes "github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// countingReplaySummarizer 记录重放调用次数并按脚本返回厚摘要/错误。
type countingReplaySummarizer struct {
	calls   atomic.Int32
	chapter string
	err     error
}

func (s *countingReplaySummarizer) Summarize(_ context.Context, _ ReplayRequest) (ReplayResult, error) {
	s.calls.Add(1)
	if s.err != nil {
		return ReplayResult{}, s.err
	}
	return ReplayResult{Chapter2: s.chapter}, nil
}

func dagInput(messages []frameworktypes.Message) CompactionInput {
	return CompactionInput{
		Record:   sessionstore.SessionContextRecord{},
		Messages: messages,
		Kind:     CompactFoldOverflow,
	}
}

// TestCompactionDAGLocalFold 无重放素材（Summarizer=nil）→ 本地折叠：
// 两章节 Summary、summary_source=local、request 覆盖标签 chat-N。
func TestCompactionDAGLocalFold(t *testing.T) {
	dag := NewCompactionDAG(CompactionDAGOptions{SessionIDProvider: func() string { return "sess-dag" }})
	frame, err := dag.Execute(context.Background(), dagInput(roundHistory(10)))
	if err != nil {
		t.Fatal(err)
	}
	if frame.SegmentID == "" || !strings.HasPrefix(frame.SegmentID, "compact-sess-dag-") {
		t.Fatalf("segment id = %q", frame.SegmentID)
	}
	if frame.From != 0 || frame.To != 9 {
		t.Fatalf("frame range = [%d,%d], want [0,9] (10 units)", frame.From, frame.To)
	}
	if frame.RequestFrom != "chat-1" || frame.RequestTo != "chat-10" {
		t.Fatalf("request labels = %q..%q", frame.RequestFrom, frame.RequestTo)
	}
	if frame.SummarySource != CompactSummarySourceLocal {
		t.Fatalf("summary source = %q, want local", frame.SummarySource)
	}
	for _, want := range []string{
		"## " + CompactChapter1Title, "## " + CompactChapter2Title,
		"### " + Chapter2SectionGoal, "溢出轮次: 10 个完整协议单元",
	} {
		if !strings.Contains(frame.Summary, want) {
			t.Fatalf("summary must contain %q:\n%s", want, frame.Summary)
		}
	}
	// Chapter 2 视图只含厚内容：requestID/锚点不进模型可见正文。
	chapter2 := FrameChapter2(frame)
	if strings.Contains(chapter2, "request: chat-") || strings.Contains(chapter2, "segment_id:") {
		t.Fatalf("chapter2 view must not carry anchor/request index:\n%s", chapter2)
	}
	if strings.Contains(chapter2, "## "+CompactChapter1Title) {
		t.Fatalf("chapter2 view must not include chapter1 title:\n%s", chapter2)
	}
}

// TestCompactionDAGChainFields 第二次压缩的帧携带链锚字段：PrevSegmentID
// 指向上一栈顶、PrevRequest* 与栈顶一致、PrevSummaryOneLine 非空。
func TestCompactionDAGChainFields(t *testing.T) {
	dag := NewCompactionDAG(CompactionDAGOptions{SessionIDProvider: func() string { return "sess-chain" }})
	first, err := dag.Execute(context.Background(), dagInput(roundHistory(6)))
	if err != nil {
		t.Fatal(err)
	}
	if first.PrevSegmentID != "" {
		t.Fatalf("first frame must have no prev_segment_id, got %q", first.PrevSegmentID)
	}
	secondInput := CompactionInput{
		Record:   sessionstore.SessionContextRecord{CompactStack: []sessionstore.CompactFrame{first}},
		Messages: roundHistory(3),
		Kind:     CompactFoldOverflow,
	}
	second, err := dag.Execute(context.Background(), secondInput)
	if err != nil {
		t.Fatal(err)
	}
	if second.PrevSegmentID != first.SegmentID {
		t.Fatalf("prev_segment_id = %q, want %q", second.PrevSegmentID, first.SegmentID)
	}
	if second.PrevRequestFrom != first.RequestFrom || second.PrevRequestTo != first.RequestTo {
		t.Fatalf("prev request = [%q,%q], want [%q,%q]",
			second.PrevRequestFrom, second.PrevRequestTo, first.RequestFrom, first.RequestTo)
	}
	if second.PrevSummaryOneLine == "" {
		t.Fatal("prev summary one line must be set on merged frame")
	}
	// 合并帧覆盖连续段：首帧 [0,5]（6 单元）→ 合并 [0,8]（+3）。
	if second.From != 0 || second.To != 8 {
		t.Fatalf("merged range = [%d,%d], want [0,8]", second.From, second.To)
	}
	if second.RequestFrom != "chat-1" || second.RequestTo != "chat-9" {
		t.Fatalf("merged request labels = %q..%q", second.RequestFrom, second.RequestTo)
	}
}

// TestCompactionDAGReplayThick 注入 Summarizer 且带 History → 前缀重放生成
// Chapter 2（summary_source=replay）；失败两次 → 回退本地折叠。
func TestCompactionDAGReplayThick(t *testing.T) {
	replay := &countingReplaySummarizer{
		chapter: "## " + CompactChapter2Title + "\n### 目标 (Goal)\n厚摘要完成",
	}
	dag := NewCompactionDAG(CompactionDAGOptions{
		Summarizer:        replay,
		SystemPrompt:      func() string { return "system" },
		Tools:             func() []frameworktypes.Tool { return nil },
		Chapter2MaxTokens: 1024,
		SessionIDProvider: func() string { return "sess-replay" },
	})
	input := dagInput(roundHistory(10))
	input.History = roundHistory(10)
	frame, err := dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if replay.calls.Load() != 1 {
		t.Fatalf("replay calls = %d, want 1", replay.calls.Load())
	}
	if frame.SummarySource != CompactSummarySourceReplay {
		t.Fatalf("summary source = %q, want replay", frame.SummarySource)
	}
	if body := FrameChapter2(frame); !strings.Contains(body, "厚摘要完成") {
		t.Fatalf("chapter2 must carry replay output:\n%s", body)
	}

	// 失败两次（一次重试后）→ 本地折叠，不抛错、不再消耗模型 token。
	failing := &countingReplaySummarizer{err: errors.New("replay boom")}
	dag = NewCompactionDAG(CompactionDAGOptions{Summarizer: failing})
	input = dagInput(roundHistory(4))
	input.History = roundHistory(4)
	frame, err = dag.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if failing.calls.Load() != 2 {
		t.Fatalf("replay attempts = %d, want 2", failing.calls.Load())
	}
	if frame.SummarySource != CompactSummarySourceLocal {
		t.Fatalf("summary source = %q, want local fallback", frame.SummarySource)
	}
	if !strings.Contains(FrameChapter2(frame), "溢出轮次: 4 个完整协议单元") {
		t.Fatalf("local fallback body missing:\n%s", FrameChapter2(frame))
	}
}

// TestCompactionDAGUsesUnitCountFromMessages 未显式给 UnitCount 时按
// Messages 切分的完整协议单元数推导。
func TestCompactionDAGUsesUnitCountFromMessages(t *testing.T) {
	dag := NewCompactionDAG(CompactionDAGOptions{})
	frame, err := dag.Execute(context.Background(), dagInput(roundHistory(3)))
	if err != nil {
		t.Fatal(err)
	}
	if frame.From != 0 || frame.To != 2 {
		t.Fatalf("frame range = [%d,%d], want [0,2]", frame.From, frame.To)
	}
}

// TestControllerCompactionDAGIntegration 生产路径接线：控制器注入
// CompactionDAG 后，Handle 压缩产出两章节 + 链锚字段的契约帧，投影历史
// 与去重语义不变。
func TestControllerCompactionDAGIntegration(t *testing.T) {
	stacks := NewMemoryCompactStack()
	dag := NewCompactionDAG(CompactionDAGOptions{SessionIDProvider: func() string { return "sess-dag-ctrl" }})
	controller := &seelexContextController{
		opts: ControllerOptions{
			Policy:            NewContextWindowPolicy(100_000, 8_192),
			Window:            fixedWindowPolicy{rounds: 3},
			Tokens:            heavyTokenCounter{},
			Stacks:            stacks,
			SessionIDProvider: func() string { return "sess-dag-ctrl" },
			Compaction:        dag,
		},
		lastCompactedTo: -1,
	}
	decision, err := controller.Handle(context.Background(), seelectx.ContextEvent{
		Kind: seelectx.ContextAfterAssistant, Turn: 1, Query: "继续", History: roundHistory(10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.ReplaceHistory || len(decision.History) != 1+6 {
		t.Fatalf("projected history length = %d, replace=%v", len(decision.History), decision.ReplaceHistory)
	}
	frames := stacks.Snapshot().CompactStack
	if len(frames) != 1 || frames[0].To != 6 {
		t.Fatalf("dag frames = %+v, want 1 frame To=6", frames)
	}
	frame := frames[0]
	if frame.SummarySource != CompactSummarySourceLocal {
		t.Fatalf("summary source = %q, want local", frame.SummarySource)
	}
	if !strings.Contains(frame.Summary, "## "+CompactChapter1Title) ||
		!strings.Contains(frame.Summary, "## "+CompactChapter2Title) {
		t.Fatalf("frame must be two-chapter:\n%s", frame.Summary)
	}
	if frame.RequestFrom != "chat-1" || frame.RequestTo != "chat-7" {
		t.Fatalf("request labels = %q..%q", frame.RequestFrom, frame.RequestTo)
	}
	if frame.PrevSegmentID != "" {
		t.Fatalf("first dag frame must not chain, got %q", frame.PrevSegmentID)
	}
}
