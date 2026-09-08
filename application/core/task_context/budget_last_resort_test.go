package task_context

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// TestTranscriptTailKeepsNewestUnitWhenItExceedsBudget：协议单元不可拆分，
// 最新已定稿轮次单条就超出预算时，尾窗不得静默返回空历史——降级保留最新
// 1 个完整单元，是否可发送交给上层全量预算门禁（拒绝优于“模型失忆”）。
func TestTranscriptTailKeepsNewestUnitWhenItExceedsBudget(t *testing.T) {
	events := []model.TranscriptEvent{
		{Seq: 1, Role: "user", Content: "old request", TokenCount: 200},
		{Seq: 2, Role: "assistant", Content: "old answer", TokenCount: 200},
		{Seq: 3, Role: "user", Content: "newest request", TokenCount: 200},
		{Seq: 4, Role: "assistant", Content: "newest answer", TokenCount: 200},
	}
	// 单轮 400 tokens，预算只有 100：旧实现自最新轮开始即 break → 空历史。
	history := TranscriptTailHistory(events, 100, 4)
	if len(history) != 2 {
		t.Fatalf("history = %d messages, want newest unit degraded to 2", len(history))
	}
	if history[0].Content != "newest request" || history[1].Content != "newest answer" {
		t.Fatalf("degraded history = %#v, want newest complete unit only", history)
	}
}

// TestTranscriptTailDropsOlderUnitsButKeepsNewestWithinBudget：最新单元能
// 放下、旧单元放不下时，行为不变——保留最新、裁旧（append-only 窗口语义）。
func TestTranscriptTailDropsOlderUnitsButKeepsNewestWithinBudget(t *testing.T) {
	events := []model.TranscriptEvent{
		{Seq: 1, Role: "user", Content: "old request", TokenCount: 60},
		{Seq: 2, Role: "assistant", Content: "old answer", TokenCount: 60},
		{Seq: 3, Role: "user", Content: "newest request", TokenCount: 60},
		{Seq: 4, Role: "assistant", Content: "newest answer", TokenCount: 60},
	}
	// 预算 150：最新轮（120）可放、加旧轮（240）超限 → 只保留最新轮。
	history := TranscriptTailHistory(events, 150, 4)
	if len(history) != 2 {
		t.Fatalf("history = %d messages, want newest unit only within budget", len(history))
	}
	if history[0].Content != "newest request" || history[1].Content != "newest answer" {
		t.Fatalf("window history = %#v, want newest complete unit", history)
	}
}
