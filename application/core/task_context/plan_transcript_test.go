package task_context

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// TestTranscriptProtocolUnitsKeepsActiveSkillAsOwnUnit：激活技能 internal 事件
// 是独立指令轮次 —— 不因"其后紧跟 user 而无 assistant 回复"被 transcriptUserUnit
// 的收敛规则丢弃，也不与后续真实轮次合并。
func TestTranscriptProtocolUnitsKeepsActiveSkillAsOwnUnit(t *testing.T) {
	skill := model.TranscriptEvent{Role: "user", Content: ActiveSkillMarker + "\n## Trusted Active Skill: review\nbody"}
	events := []model.TranscriptEvent{
		skill,
		{Role: "user", Content: "inspect the repo"},
		{Role: "assistant", Content: "let me read"},
	}
	units := transcriptProtocolUnits(events)
	if len(units) != 2 {
		t.Fatalf("units = %d, want 2 (skill turn + real turn)", len(units))
	}
	if len(units[0]) != 1 || units[0][0].Content != skill.Content {
		t.Fatalf("first unit must be the standalone skill event: %#v", units[0])
	}
	if len(units[1]) != 2 || units[1][0].Content != "inspect the repo" {
		t.Fatalf("second unit must be the real user turn: %#v", units[1])
	}
}

// TestTranscriptTailHistoryEmitsActiveSkillTurn：装配输出在真实轮次之前包含
// 技能正文消息（internal 标记保留在 content 里供可见性/存档过滤），技能事件
// 与真实轮次一并计入预算。
func TestTranscriptTailHistoryEmitsActiveSkillTurn(t *testing.T) {
	skill := model.TranscriptEvent{Role: "user", Content: ActiveSkillMarker + "\n## Trusted Active Skill: review\nbody"}
	events := []model.TranscriptEvent{
		skill,
		{Role: "user", Content: "inspect the repo"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "second turn"},
		{Role: "assistant", Content: "ok"},
	}
	history := TranscriptTailHistory(events, 1_000_000, 0)
	if len(history) < 2 {
		t.Fatalf("history too short: %#v", history)
	}
	if history[0].Role != "user" || !strings.HasPrefix(history[0].Content, ActiveSkillMarker) {
		t.Fatalf("first provider message must be the skill turn: %+v", history[0])
	}
	if history[1].Role != "user" || history[1].Content != "inspect the repo" {
		t.Fatalf("second provider message must be the real first user input: %+v", history[1])
	}
}

// TestTranscriptTailHistorySkipsSkillWhenDroppedFromBudget：压缩窗口/预算不足
// 时技能事件随旧轮次一起被裁掉（append-only 语义：不单独保留、不重建）。
func TestTranscriptTailHistorySkipsSkillWhenDroppedFromBudget(t *testing.T) {
	skill := model.TranscriptEvent{Role: "user", Content: ActiveSkillMarker + "\n## Trusted Active Skill: review\nbody"}
	events := []model.TranscriptEvent{
		skill,
		{Role: "user", Content: "inspect the repo"},
		{Role: "assistant", Content: "done"},
	}
	// 窗口上限 1 个协议单元 → 只保留最后定稿轮次（user+assistant）；技能事件
	// （最早的单元）被裁掉——与普通旧轮次同等对待，不单独保留。
	history := TranscriptTailHistory(events, 1_000_000, 1)
	for _, message := range history {
		if strings.HasPrefix(message.Content, ActiveSkillMarker) {
			t.Fatalf("skill event must drop with the bounded window: %#v", history)
		}
	}
	if len(history) == 0 || history[len(history)-1].Content != "done" {
		t.Fatalf("recent settled context must survive: %#v", history)
	}
}
