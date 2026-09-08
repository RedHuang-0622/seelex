package task_context

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// interruptedTurn 构造"残缺工具链"回合：assistant 请求 t1/t2，只记录了 t1
// 的结果（t2 因中断丢失）。probeTail 为 true 时该轮位于事件流末尾（无后续
// 文本收尾、无下一个 user）；false 时其后接一个完整的 assistant 文本答复
// （同轮收尾）与下一个 user 轮。
func interruptedTurnEvents(withSuffixText, atTail bool) []model.TranscriptEvent {
	events := []model.TranscriptEvent{
		{Role: "user", Content: "任务1：重构并验证", Seq: 1},
		{Role: "assistant", Kind: model.TranscriptEventKindToolCall,
			ToolCalls: []model.TranscriptToolCall{
				{ID: "t1", Name: "read_file", Arguments: `{"path":"a.go"}`},
				{ID: "t2", Name: "grep_search", Arguments: `{"pattern":"x"}`},
			}, Seq: 2},
		{Role: "tool", ToolCallID: "t1", Name: "read_file", Content: "file body", Seq: 3},
	}
	if withSuffixText {
		events = append(events, model.TranscriptEvent{Role: "assistant", Content: "已完成第一步，继续", Seq: 4})
	}
	if !atTail {
		events = append(events,
			model.TranscriptEvent{Role: "user", Content: "任务2：继续", Seq: 5},
			model.TranscriptEvent{Role: "assistant", Content: "任务2完成", Seq: 6},
		)
	}
	return events
}

// TestTranscriptProtocolUnitsKeepsSuffixTextOfInterruptedChain：残缺工具链
// 轮不能被整体作废、也不能连坐其后同轮内的完整文本答复与后续轮次——修复前
// userEventUnit 在链不完整时跳到下一个 user，把该文本答复一并吞掉。
func TestTranscriptProtocolUnitsKeepsSuffixTextOfInterruptedChain(t *testing.T) {
	events := interruptedTurnEvents(true, false)
	units := transcriptProtocolUnits(events)
	if len(units) != 2 {
		t.Fatalf("units = %d, want 2 (interrupted turn with suffix text + task2 turn): %#v", len(units), units)
	}
	first := units[0]
	if len(first) != 4 {
		t.Fatalf("first unit must keep user+tc+t1+text, got %d events: %#v", len(first), first)
	}
	if first[0].Content != "任务1：重构并验证" || first[3].Role != "assistant" || first[3].Content != "已完成第一步，继续" {
		t.Fatalf("interrupted turn content must be fully retained: %#v", first)
	}
	if units[1][0].Content != "任务2：继续" {
		t.Fatalf("next user turn must survive: %#v", units[1])
	}
}

// TestTranscriptProtocolUnitsKeepsTailInterruptedChainAsOpenUnit：以残缺工具
// 链收尾且前面无纯文本终止点（会话尾中断）时，整轮（user + 已记录工具部分）
// 必须仍构成单元 —— UI 可见的轮次不得在冷加载 provider 上下文中静默消失。
func TestTranscriptProtocolUnitsKeepsTailInterruptedChainAsOpenUnit(t *testing.T) {
	events := interruptedTurnEvents(false, true)
	units := transcriptProtocolUnits(events)
	if len(units) != 1 {
		t.Fatalf("units = %d, want 1 open tail unit: %#v", len(units), units)
	}
	if len(units[0]) != 3 {
		t.Fatalf("tail unit must keep user+tc+partial tool result, got %d: %#v", len(units[0]), units[0])
	}
	history := TranscriptTailHistory(events, 1_000_000, 0)
	if len(history) != 3 {
		t.Fatalf("cold-load history must include the interrupted round, got %d: %#v", len(history), history)
	}
	if history[0].Content != "任务1：重构并验证" {
		t.Fatalf("provider history lost the visible user request: %#v", history)
	}
}
