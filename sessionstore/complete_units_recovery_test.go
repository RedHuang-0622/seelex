package sessionstore

import (
	"testing"
)

// interruptedEventTurn 构造残缺工具链事件流（同 task_context 探针语义）：
// user 任务 → assistant 请求 t1/t2 → 只有 t1 结果被记录。withSuffixText 时
// 其后跟同轮 assistant 文本答复与下一个 user 轮；否则事件流以残缺链收尾。
func interruptedEventTurn(withSuffixText, atTail bool) []Event {
	events := []Event{
		{Seq: 1, Role: "user", Content: "任务1：重构并验证"},
		{Seq: 2, Role: "assistant", ToolCalls: []EventToolCall{
			{ID: "t1", Name: "read_file"}, {ID: "t2", Name: "grep_search"},
		}},
		{Seq: 3, Role: "tool", ToolCallID: "t1", Name: "read_file", Content: "file body"},
	}
	if withSuffixText {
		events = append(events, Event{Seq: 4, Role: "assistant", Content: "已完成第一步，继续"})
	}
	if !atTail {
		events = append(events,
			Event{Seq: 5, Role: "user", Content: "任务2：继续"},
			Event{Seq: 6, Role: "assistant", Content: "任务2完成"},
		)
	}
	return events
}

// TestCompleteEventUnitsKeepsSuffixTextOfInterruptedChain：残缺工具链之后的
// 同轮完整文本与下一轮不得被 nextUserIndex 连坐丢弃。
func TestCompleteEventUnitsKeepsSuffixTextOfInterruptedChain(t *testing.T) {
	events := interruptedEventTurn(true, false)
	units := CompleteEventUnits(events)
	if len(units) != 2 {
		t.Fatalf("units = %d, want 2: %#v", len(units), units)
	}
	first := units[0]
	if len(first) != 4 || first[0].Content != "任务1：重构并验证" || first[3].Role != "assistant" {
		t.Fatalf("interrupted turn with suffix text must be fully retained: %#v", first)
	}
	if units[1][0].Content != "任务2：继续" {
		t.Fatalf("next user turn must survive: %#v", units[1])
	}
}

// TestCompleteEventUnitsKeepsTailInterruptedChainAsOpenUnit：以残缺工具链
// 收尾且前面无文本终止点（会话尾）时，整轮必须仍构成单元进入冷加载尾窗。
func TestCompleteEventUnitsKeepsTailInterruptedChainAsOpenUnit(t *testing.T) {
	events := interruptedEventTurn(false, true)
	units := CompleteEventUnits(events)
	if len(units) != 1 {
		t.Fatalf("units = %d, want 1 open tail unit: %#v", len(units), units)
	}
	if len(units[0]) != 3 {
		t.Fatalf("tail unit must keep user+tc+partial result, got %d: %#v", len(units[0]), units[0])
	}
	tail := selectEventTail(events, 1<<20, 100)
	if len(tail) != 3 || tail[0].Content != "任务1：重构并验证" {
		t.Fatalf("selectEventTail must include the interrupted tail round: %#v", tail)
	}
}
