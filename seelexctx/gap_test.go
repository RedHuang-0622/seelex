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
	// 10 个单元（索引 0..9），压缩栈覆盖到 To=2，尾窗从单元 7 开始
	// → 真空区 = 单元 3..6（栈顶To+1 .. 尾窗起点-1）。
	stack := NewMemoryCompactStack()
	record := gapStackRecord(t, sessionstore.CompactFrame{
		SegmentID: "compact-a-1", From: 0, To: 2, Summary: "先前压缩内容",
	})
	result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: gapEvents(10), TailStartUnit: 7,
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
	stack := NewMemoryCompactStack()
	result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: gapEvents(6), TailStartUnit: 4,
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
	// 栈顶 To=7 已覆盖到尾窗起点 7 → 无真空区。
	record := gapStackRecord(t, sessionstore.CompactFrame{SegmentID: "compact-a-1", From: 0, To: 7, Summary: "全覆盖"})
	result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: gapEvents(10), TailStartUnit: 7, Record: record,
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
	// 尾窗从单元 0 开始（装载了全部单元）→ gapEnd < 0，无真空区。
	result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: gapEvents(4), TailStartUnit: 0,
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
	// 同一事件流重复覆盖：第二次栈顶 To 已到真空区终点 → gapStart > gapEnd，
	// 不再推帧。精确下标使该结论可证，不依赖事后去重分支。
	stack := NewMemoryCompactStack()
	opts := GapCoverageOptions{
		AllEvents: gapEvents(10), TailStartUnit: 7,
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

// TestCoverHistoryGapRestoresCoverageContiguity 守的是本模块的不变量：补压之后
// 「压缩栈覆盖 ∪ 尾窗」必须连续，即栈顶 To 恰等于尾窗起点减一。断档一旦出现，
// 落在断档里的轮次既不在窗口也不在任何压缩帧，会从模型请求中永久丢失。
func TestCoverHistoryGapRestoresCoverageContiguity(t *testing.T) {
	all := gapEvents(12)
	stack := NewMemoryCompactStack()
	opts := GapCoverageOptions{
		AllEvents: all, TailStartUnit: 9, Stacks: stack, SessionID: "sess-c",
		Record: gapStackRecord(t, sessionstore.CompactFrame{
			SegmentID: "compact-a-1", From: 0, To: 3, Summary: "基线帧",
		}),
	}
	result, err := CoverHistoryGap(context.Background(), opts)
	if err != nil {
		t.Fatalf("cover gap: %v", err)
	}
	if !result.Covered || result.UncoveredUnits != 5 || result.Frame.To != 8 {
		t.Fatalf("want 5 uncovered units ending at 8, got %+v", result)
	}
	// 不变量：栈顶覆盖终点 == 尾窗起点 - 1。
	top := stack.Snapshot().CompactStack[len(stack.Snapshot().CompactStack)-1]
	if top.To != opts.TailStartUnit-1 {
		t.Fatalf("coverage must abut the window: top.To=%d, want %d", top.To, opts.TailStartUnit-1)
	}
	// 尾窗再前移（下一轮 Load 装载更多）：新的未覆盖区间只有 9..10。
	opts.Record = stack.Snapshot()
	opts.TailStartUnit = 11
	again, err := CoverHistoryGap(context.Background(), opts)
	if err != nil {
		t.Fatalf("second cover: %v", err)
	}
	if !again.Covered || again.UncoveredUnits != 2 {
		t.Fatalf("want 2 newly uncovered units, got %+v", again)
	}
	snapshot := stack.Snapshot()
	finalTop := snapshot.CompactStack[len(snapshot.CompactStack)-1]
	if finalTop.To != 10 || finalTop.From != 0 {
		t.Fatalf("final top frame must be [0..10], got [%d..%d]", finalTop.From, finalTop.To)
	}
	if finalTop.PrevSegmentID == "" {
		t.Fatal("gap frame must chain to the previous frame (prev_segment_id)")
	}
}

// TestCoverHistoryGapRejectsInputsOutsideUnitSpace 守的是输入契约：尾窗起点由
// 存储层回报、栈顶 To 来自持久化记录，两者都在 AllEvents 的单元空间里；一旦
// 越界，按索引切片就是越界访问——旧实现靠「只补明确未覆盖区间」的 clamp 兜着，
// 现在边界直接从存储层传入，必须显式拒绝，不能 panic 打断整次 Load。
func TestCoverHistoryGapRejectsInputsOutsideUnitSpace(t *testing.T) {
	const units = 10
	for _, tc := range []struct {
		name      string
		tailStart int
		topTo     int
	}{
		{name: "tail start just past the unit count", tailStart: units + 1, topTo: 0},
		{name: "tail start far beyond the unit count", tailStart: 99, topTo: 0},
		{name: "negative tail start", tailStart: -1, topTo: 0},
		{name: "stack top below the unit origin", tailStart: units, topTo: -2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stack := NewMemoryCompactStack()
			result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
				AllEvents:     gapEvents(units),
				TailStartUnit: tc.tailStart,
				Record: gapStackRecord(t, sessionstore.CompactFrame{
					SegmentID: "compact-a-1", From: 0, To: tc.topTo, Summary: "基线帧",
				}),
				Stacks: stack,
			})
			if err == nil {
				t.Fatalf("out-of-space inputs must fail the contract, got %+v", result)
			}
			if result.Covered {
				t.Fatalf("out-of-space inputs must not cover anything: %+v", result)
			}
			if pushed := stack.Snapshot().CompactStack; len(pushed) != 0 {
				t.Fatalf("no frame may be pushed on contract violation, got %+v", pushed)
			}
		})
	}
	// 边界值本身合法，守卫不得误伤：尾窗起点 == 单元总数（尾窗为空）与栈顶
	// To == -1（等效于尚无覆盖）都仍是正常的真空区覆盖。
	for _, tc := range []struct {
		name      string
		topTo     int
		wantUnits int
	}{
		{name: "tail start equals the unit count", topTo: 0, wantUnits: units - 1},
		{name: "stack top at the unit origin", topTo: -1, wantUnits: units},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stack := NewMemoryCompactStack()
			result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
				AllEvents:     gapEvents(units),
				TailStartUnit: units,
				Record: gapStackRecord(t, sessionstore.CompactFrame{
					SegmentID: "compact-a-1", From: 0, To: tc.topTo, Summary: "基线帧",
				}),
				Stacks: stack,
			})
			if err != nil {
				t.Fatalf("boundary inputs must stay valid: %v", err)
			}
			if !result.Covered || result.UncoveredUnits != tc.wantUnits || result.Frame.To != units-1 {
				t.Fatalf("want %d uncovered units ending at %d, got %+v", tc.wantUnits, units-1, result)
			}
		})
	}
}

func TestCoverHistoryGapEmptyInputs(t *testing.T) {
	stack := NewMemoryCompactStack()
	if result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: nil, TailStartUnit: 0, Record: sessionstore.SessionContextRecord{}, Stacks: stack,
	}); err != nil || result.Covered {
		t.Fatalf("empty events must not cover: %+v %v", result, err)
	}
	// 有事件但尾窗为空（startUnit == 单元总数）→ 全部单元都是真空区。
	result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: gapEvents(3), TailStartUnit: 3,
		Record: sessionstore.SessionContextRecord{}, Stacks: stack,
	})
	if err != nil || !result.Covered || result.Frame.To != 2 {
		t.Fatalf("empty tail must cover everything: %+v %v", result, err)
	}
}

func TestCoverHistoryGapEvidenceAndTurns(t *testing.T) {
	// 4 个单元：unit0 = user + 工具链（含 tool 结果），unit1..3 = user+assistant 文本轮。
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
	stack := NewMemoryCompactStack()
	archiver := &recordingTurnArchiver{}

	result, err := CoverHistoryGap(context.Background(), GapCoverageOptions{
		AllEvents: events, TailStartUnit: 2,
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
