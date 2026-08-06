package search

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelexctx/memory"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// fakeStack 是 StackSource 测试替身：返回固定压缩栈。
type fakeStack struct{ record sessionstore.SessionContextRecord }

func (s *fakeStack) Snapshot() sessionstore.SessionContextRecord { return s.record }

// fakeEvents 是 EventSource 测试替身：返回固定事件流。
type fakeEvents struct{ events []sessionstore.Event }

func (s *fakeEvents) LoadAllEvents(context.Context) ([]sessionstore.Event, error) {
	return s.events, nil
}

// unit 构造一个事件单元：user 输入 + assistant 工具链（调用 + 结果）。
func unit(seqBase int, userText, toolName string) []sessionstore.Event {
	return []sessionstore.Event{
		{Seq: uint64(seqBase), Role: "user", Content: userText},
		{Seq: uint64(seqBase + 1), Role: "assistant", ToolCalls: []sessionstore.EventToolCall{{ID: "call-1", Name: toolName}}},
		{Seq: uint64(seqBase + 2), Role: "tool", Name: toolName, ToolCallID: "call-1", Content: "工具结果内容", ResultRef: "result:call-1"},
	}
}

func frame(segment string, from, to int, summary string) sessionstore.CompactFrame {
	return sessionstore.CompactFrame{
		SegmentID: segment, From: from, To: to, Summary: summary, CompressedAt: time.Now(),
	}
}

func TestSearchSelectsRelevantFramesAndReadsRecords(t *testing.T) {
	events := append(unit(0, "聊聊数据库索引", "grep_search"), unit(3, "讨论单元测试覆盖率", "bash")...)
	events = append(events, unit(6, "继续讨论数据库优化方案", "read_file")...)
	stack := &fakeStack{record: sessionstore.SessionContextRecord{CompactStack: []sessionstore.CompactFrame{
		frame("compact-a", 0, 1, "数据库索引优化讨论"),
		frame("compact-b", 2, 2, "单元测试覆盖率统计"),
	}}}
	searcher := New(stack, &fakeEvents{events: events})

	result, err := searcher.Search(context.Background(), "数据库优化", Options{Limit: 1, TokenBudget: 2000})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.IndexedFrames != 2 || result.TotalUnits != 3 {
		t.Fatalf("indexed/total = %d/%d, want 2/3", result.IndexedFrames, result.TotalUnits)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("hits = %d, want 1: %+v", len(result.Hits), result.Hits)
	}
	hit := result.Hits[0]
	if hit.SegmentID != "compact-a" {
		t.Fatalf("hit segment = %q, want compact-a", hit.SegmentID)
	}
	if hit.Score <= 0 {
		t.Fatalf("hit score = %v, want > 0", hit.Score)
	}
	if hit.From != 0 || hit.To != 1 || hit.Units != 2 {
		t.Fatalf("hit range = [%d..%d] units=%d, want [0..1] units=2", hit.From, hit.To, hit.Units)
	}
	// 真实聊天记录：帧范围内两个单元的 user 输入 + assistant 工具 + tool 结果。
	var contents []string
	for _, record := range hit.Records {
		contents = append(contents, record.Role, record.Content, record.ToolName, record.ResultRef)
	}
	joined := strings.Join(contents, "|")
	for _, want := range []string{
		"user", "聊聊数据库索引", "grep_search", "工具结果内容", "result:call-1",
		"讨论单元测试覆盖率", "bash",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("records missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "继续讨论数据库优化方案") {
		t.Errorf("records must be bounded to frame range [0..1], got out-of-range unit content")
	}
}

func TestSearchClampsFrameRangeToEventStream(t *testing.T) {
	events := unit(0, "唯一的对话", "bash")
	stack := &fakeStack{record: sessionstore.SessionContextRecord{CompactStack: []sessionstore.CompactFrame{
		frame("compact-out", -5, 99, "对话越界范围"), // 帧范围远超事件流
	}}}
	searcher := New(stack, &fakeEvents{events: events})
	result, err := searcher.Search(context.Background(), "对话", Options{Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(result.Hits))
	}
	hit := result.Hits[0]
	if hit.From != 0 || hit.To != 0 {
		t.Fatalf("clamped range = [%d..%d], want [0..0]", hit.From, hit.To)
	}
	if len(hit.Records) != 3 || hit.Records[0].Role != "user" || hit.Records[0].Content != "唯一的对话" {
		t.Fatalf("clamped records = %+v, want 单元内全部事件（user 在前）", hit.Records)
	}
	if hit.Truncated {
		t.Fatalf("clamped hit must not be truncated（预算充足）")
	}
}

func TestSearchRejectsEmptyQuery(t *testing.T) {
	searcher := New(nil, &fakeEvents{events: unit(0, "x", "bash")})
	if _, err := searcher.Search(context.Background(), "  ", Options{}); !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("empty query error = %v, want ErrEmptyQuery", err)
	}
}

func TestSearchTokenBudgetTruncatesAndDropsLaterHits(t *testing.T) {
	events := append(unit(0, "预算一", "bash"), unit(3, "预算二", "bash")...)
	stack := &fakeStack{record: sessionstore.SessionContextRecord{CompactStack: []sessionstore.CompactFrame{
		frame("compact-a", 0, 0, "预算一相关"),
		frame("compact-b", 1, 1, "预算二相关"),
	}}}
	searcher := New(stack, &fakeEvents{events: events})
	// 预算极小：第一帧只能渲染 0 条记录（截断），第二帧被丢弃。
	result, err := searcher.Search(context.Background(), "预算", Options{Limit: 2, TokenBudget: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("hits = %d, want 1（第二帧被预算丢弃）", len(result.Hits))
	}
	if !result.Hits[0].Truncated {
		t.Fatalf("hit[0].Truncated = false, want true")
	}
	if !result.Truncated {
		t.Fatalf("result.Truncated = false, want true")
	}
}

func TestSearchBudgetHardCapClamps(t *testing.T) {
	result, err := New(nil, &fakeEvents{events: unit(0, "x", "bash")}).Search(context.Background(), "x", Options{TokenBudget: MaxTokenBudget * 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Budget != MaxTokenBudget {
		t.Fatalf("budget = %d, want hard cap %d", result.Budget, MaxTokenBudget)
	}
}

func TestSearchNoStackFallsBackToTailScan(t *testing.T) {
	events := append(unit(0, "尾部扫描第一轮", "bash"), unit(3, "尾部扫描第二轮", "read_file")...)
	searcher := New(nil, &fakeEvents{events: events}) // stack = nil → 兜底
	result, err := searcher.Search(context.Background(), "第二轮", Options{Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.Contains(result.Note, "未压缩") {
		t.Fatalf("note = %q, want 未压缩提示", result.Note)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(result.Hits))
	}
	if result.Hits[0].SegmentID != "unit-1" || result.Hits[0].From != 1 || result.Hits[0].To != 1 {
		t.Fatalf("fallback hit = %+v, want unit-1 [1..1]", result.Hits[0])
	}
	if len(result.Hits[0].Records) != 3 {
		t.Fatalf("fallback records = %d, want 3（该单元全部事件）", len(result.Hits[0].Records))
	}
}

func TestSearchFallbackScanCap(t *testing.T) {
	var events []sessionstore.Event
	for index := 0; index < MaxFallbackScanUnits+10; index++ {
		events = append(events, unit(index*3, "历史轮次", "bash")...)
	}
	searcher := New(nil, &fakeEvents{events: events})
	result, err := searcher.Search(context.Background(), "历史轮次", Options{Limit: 20, TokenBudget: MaxTokenBudget})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.TotalUnits != MaxFallbackScanUnits+10 {
		t.Fatalf("total units = %d, want %d", result.TotalUnits, MaxFallbackScanUnits+10)
	}
	if !strings.Contains(result.Note, "最近 300 个轮次") {
		t.Fatalf("note = %q, want 扫描上限提示", result.Note)
	}
	// 兜底只扫描尾部 300 轮：扫描起点之前的轮次（unit-0 起）不可能命中。
	scanStart := len(events)/3 - MaxFallbackScanUnits
	for _, hit := range result.Hits {
		if hit.From < scanStart {
			t.Fatalf("fallback hit from = %d, want >= %d（只扫描尾部）", hit.From, scanStart)
		}
	}
}

func TestSearchNoEventsReturnsNote(t *testing.T) {
	searcher := New(&fakeStack{}, &fakeEvents{events: nil})
	result, err := searcher.Search(context.Background(), "任何查询", Options{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Hits) != 0 || !strings.Contains(result.Note, "暂无事件记录") {
		t.Fatalf("result = %+v, want 空命中 + 暂无事件记录提示", result)
	}
}

func TestSearchEventSourceUnavailable(t *testing.T) {
	if _, err := New(nil, nil).Search(context.Background(), "查询", Options{}); !errors.Is(err, ErrNoEventSource) {
		t.Fatalf("nil event source error = %v, want ErrNoEventSource", err)
	}
}

func TestSearchToolResultRefFallbackFromCallID(t *testing.T) {
	events := []sessionstore.Event{
		{Seq: 0, Role: "user", Content: "查一下工具结果"},
		{Seq: 1, Role: "assistant", ToolCalls: []sessionstore.EventToolCall{{ID: "call-9", Name: "bash"}}},
		{Seq: 2, Role: "tool", Name: "bash", ToolCallID: "call-9", Content: "输出内容"}, // 无 ResultRef
	}
	result, err := New(nil, &fakeEvents{events: events}).Search(context.Background(), "工具结果", Options{Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var ref string
	for _, record := range result.Hits[0].Records {
		if record.Role == "tool" {
			ref = record.ResultRef
		}
	}
	if ref != "result:call-9" {
		t.Fatalf("tool result ref = %q, want result:call-9（按调用 ID 推导）", ref)
	}
}

func TestMemoryScoreMatchesSelectOrdering(t *testing.T) {
	// search 复用 memory.Score：分数与 Select 同款（更高分者排序在前）。
	candidates := []memory.Candidate{
		{SegmentID: "a", Summary: "数据库索引优化方案", From: 0, To: 0},
		{SegmentID: "b", Summary: "单元测试覆盖率统计", From: 1, To: 1},
	}
	scoreA := memory.Score("数据库", candidates[0])
	scoreB := memory.Score("数据库", candidates[1])
	if scoreA <= 0 || scoreB > 0 {
		t.Fatalf("scores = %v/%v, want a>0 且 b 无命中", scoreA, scoreB)
	}
	if scoreA <= scoreB {
		t.Fatalf("score(a)=%v, score(b)=%v, want a 更高", scoreA, scoreB)
	}
	selected := memory.Select("数据库", candidates, memory.Options{Limit: 2})
	if len(selected) != 1 || selected[0].SegmentID != "a" {
		t.Fatalf("select = %+v, want 仅 a 命中", selected)
	}
}
