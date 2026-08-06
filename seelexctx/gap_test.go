package seelexctx

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// gapEvents 构造 N 个完整协议单元的事件流（user → assistant 文本轮）。
func gapEvents(units int) []sessionstore.Event {
	var events []sessionstore.Event
	for index := 0; index < units; index++ {
		content := "round " + string(rune('A'+index))
		events = append(events,
			sessionstore.Event{Seq: uint64(index*2 + 1), Role: "user", Content: content},
			sessionstore.Event{Seq: uint64(index*2 + 2), Role: "assistant", Content: "reply to " + content},
		)
	}
	return events
}

func gapStackRecord(t *testing.T, frames ...sessionstore.CompactFrame) sessionstore.SessionContextRecord {
	t.Helper()
	return sessionstore.SessionContextRecord{CompactStack: frames}
}

func TestCoverHistoryGapCoversUncompactedRegion(t *testing.T) {
	// 10 个单元（索引 0..9），压缩栈覆盖到 To=2，尾窗装载最后 3 个单元
	// （7..9）→ 真空区 = 单元 3..6（索引 2+1 .. 10-3-1）。
	allEvents := gapEvents(10)
	tailEvents := gapEvents(3)
	stack := NewMemoryCompactStack()
	record := gapStackRecord(t, sessionstore.CompactFrame{
		SegmentID: "compact-a-1", From: 0, To: 2, Summary: "先前压缩内容",
	})
	result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: allEvents, TailEvents: tailEvents,
		Record: record, Stacks: stack, SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("cover gap: %v", err)
	}
	if !result.Covered {
		t.Fatal("gap must be detected")
	}
	if result.UncoveredUnits != 4 {
		t.Fatalf("want 4 uncovered units, got %d", result.UncoveredUnits)
	}
	if result.Frame.To != 6 || result.Frame.From != 0 {
		t.Fatalf("frame range must be [0..6], got [%d..%d]", result.Frame.From, result.Frame.To)
	}
	snapshot := stack.Snapshot()
	if len(snapshot.CompactStack) != 1 {
		t.Fatalf("want 1 pushed frame, got %d", len(snapshot.CompactStack))
	}
	pushed := snapshot.CompactStack[0]
	if pushed.SegmentID != result.Frame.SegmentID {
		t.Fatalf("segment mismatch: %s vs %s", pushed.SegmentID, result.Frame.SegmentID)
	}
	for _, want := range []string{"真空区轮次: 4 个完整协议单元", "round D", "round G", "先前压缩摘要: 先前压缩内容"} {
		if !strings.Contains(pushed.Summary, want) {
			t.Fatalf("summary must contain %q, got:\n%s", want, pushed.Summary)
		}
	}
	if !strings.HasPrefix(pushed.SegmentID, "compact-gap-sess-1-") {
		t.Fatalf("segment must carry session prefix, got %s", pushed.SegmentID)
	}
}

func TestCoverHistoryGapNoStackCoversHead(t *testing.T) {
	// 无压缩栈（从未压缩）：真空区从 0 开始，覆盖尾窗前的全部单元。
	allEvents := gapEvents(6)
	tailEvents := gapEvents(2)
	stack := NewMemoryCompactStack()
	result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: allEvents, TailEvents: tailEvents,
		Record: sessionstore.SessionContextRecord{}, Stacks: stack,
	})
	if err != nil {
		t.Fatalf("cover gap: %v", err)
	}
	if !result.Covered || result.Frame.From != 0 || result.Frame.To != 3 {
		t.Fatalf("want covered [0..3], got covered=%v range=[%d..%d]",
			result.Covered, result.Frame.From, result.Frame.To)
	}
}

func TestCoverHistoryGapNoGapWhenStackCoversWindowStart(t *testing.T) {
	// 栈顶 To=7 已覆盖到尾窗（8..10）起点 → 无真空区。
	allEvents := gapEvents(10)
	tailEvents := gapEvents(3)
	record := gapStackRecord(t, sessionstore.CompactFrame{SegmentID: "compact-a-1", From: 0, To: 7, Summary: "全覆盖"})
	result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: allEvents, TailEvents: tailEvents, Record: record,
		Stacks: NewMemoryCompactStack(),
	})
	if err != nil {
		t.Fatalf("cover gap: %v", err)
	}
	if result.Covered {
		t.Fatalf("no gap expected, got %+v", result)
	}
}

func TestCoverHistoryGapNoGapWhenEverythingLoaded(t *testing.T) {
	// 尾窗装载了全部单元（gapEnd < 0）→ 无真空区。
	allEvents := gapEvents(4)
	tailEvents := gapEvents(4)
	result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: allEvents, TailEvents: tailEvents,
		Record: sessionstore.SessionContextRecord{}, Stacks: NewMemoryCompactStack(),
	})
	if err != nil {
		t.Fatalf("cover gap: %v", err)
	}
	if result.Covered {
		t.Fatal("no gap expected when window loads everything")
	}
}

func TestCoverHistoryGapRepeatedCoverageIsIdempotent(t *testing.T) {
	// 同一事件流重复覆盖：第二次栈顶 To 已到真空区终点 → 不再推帧。
	allEvents := gapEvents(10)
	tailEvents := gapEvents(3)
	stack := NewMemoryCompactStack()
	opts := GapCoverageOptions{
		AllEvents: allEvents, TailEvents: tailEvents,
		Record: sessionstore.SessionContextRecord{}, Stacks: stack,
	}
	first, err := CoverHistoryGap(context.Background(), opts)
	if err != nil || !first.Covered {
		t.Fatalf("first coverage: covered=%v err=%v", first.Covered, err)
	}
	opts.Record = stack.Snapshot()
	second, err := CoverHistoryGap(context.Background(), opts)
	if err != nil {
		t.Fatalf("second coverage: %v", err)
	}
	if second.Covered {
		t.Fatalf("second coverage must be idempotent no-op, got %+v", second)
	}
	if got := len(stack.Snapshot().CompactStack); got != 1 {
		t.Fatalf("want 1 frame after idempotent coverage, got %d", got)
	}
}

func TestCoverHistoryGapEmptyInputs(t *testing.T) {
	stack := NewMemoryCompactStack()
	if result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: nil, TailEvents: nil, Record: sessionstore.SessionContextRecord{}, Stacks: stack,
	}); err != nil || result.Covered {
		t.Fatalf("empty events must not cover: %+v %v", result, err)
	}
	// 有事件但无 tail（装载为空）→ 全部单元都是真空区，覆盖 0..N-1。
	all := gapEvents(3)
	result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: all, TailEvents: nil, Record: sessionstore.SessionContextRecord{}, Stacks: stack,
	})
	if err != nil || !result.Covered || result.Frame.To != 2 {
		t.Fatalf("empty tail must cover everything: %+v %v", result, err)
	}
}

func TestCoverHistoryGapEvidenceAndTurns(t *testing.T) {
	events := []sessionstore.Event{
		{Seq: 1, Role: "user", Content: "请读取窗口大小"},
		{Seq: 2, Role: "assistant", ToolCalls: []sessionstore.EventToolCall{{ID: "call-1", Name: "read_window"}}},
		{Seq: 3, Role: "tool", ToolCallID: "call-1", Name: "read_window", ResultRef: "result:abc"},
		{Seq: 4, Role: "user", Content: "好的"},
		{Seq: 5, Role: "assistant", Content: "继续"},
		{Seq: 6, Role: "user", Content: "窗口内容"},
		{Seq: 7, Role: "assistant", Content: "处理"},
		{Seq: 8, Role: "user", Content: "最后"},
		{Seq: 9, Role: "assistant", Content: "收尾"},
	}
	tail := events[6:] // 最后 2 个单元
	stack := NewMemoryCompactStack()
	archiver := &recordingTurnArchiver{}

	result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: events, TailEvents: tail,
		Record: sessionstore.SessionContextRecord{}, Stacks: stack,
		SessionID: "sess-2", Turns: archiver,
	})
	if err != nil {
		t.Fatalf("cover gap: %v", err)
	}
	if !result.Covered {
		t.Fatal("gap expected")
	}
	pushed := stack.Snapshot().CompactStack[0]
	if len(pushed.Evidence) == 0 || pushed.Evidence[0].Ref != "result:abc" {
		t.Fatalf("evidence must carry result:abc, got %+v", pushed.Evidence)
	}
	if !strings.Contains(pushed.Summary, "read_compressed_turn") {
		t.Fatalf("summary must advertise read_compressed_turn, got:\n%s", pushed.Summary)
	}
	if len(archiver.segmentIDs) != 1 || len(archiver.messageN) != 1 || archiver.messageN[0] != 5 {
		t.Fatalf("turns must be archived once with 5 gap messages, got segments=%v messages=%v",
			archiver.segmentIDs, archiver.messageN)
	}
}
