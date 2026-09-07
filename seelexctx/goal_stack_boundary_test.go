package seelexctx

import (
	"reflect"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestGoalStackNotRenderedIntoContext 固化 goal 第五栈的语义边界：GoalStack
// 随会话聊天记录同域持久化（sessionstore context 通道），但**不进模型上下文**
// ——不渲染为前缀/尾部栈块，也不做记忆前缀与匹配。空四栈 + 非空 GoalStack 时
// 渲染结果必须为零块；有 plan/task/skill/compact 时块集合不变。
func TestGoalStackNotRenderedIntoContext(t *testing.T) {
	goalOnly := sessionstore.SessionContextRecord{
		GoalStack: []sessionstore.GoalFrame{{
			GoalID: "goal-1", Title: "审查 goal 域（不入上下文）", Status: "active",
		}},
	}
	if blocks := RenderStackBlocks(goalOnly); len(blocks) != 0 {
		t.Fatalf("goal-only record must render zero context blocks, got %d: %+v", len(blocks), blocks)
	}
	if blocks := RenderStablePrefixBlocks(goalOnly); len(blocks) != 0 {
		t.Fatalf("goal-only record must render zero prefix blocks, got %d", len(blocks))
	}
	if blocks := RenderTailBlocks(goalOnly); len(blocks) != 0 {
		t.Fatalf("goal-only record must render zero tail blocks, got %d", len(blocks))
	}

	withStacks := sessionstore.SessionContextRecord{
		PlanStack:    []sessionstore.PlanFrame{{PlanID: "plan-1", Title: "重构", Status: "active"}},
		TaskStack:    []sessionstore.TaskFrame{{TaskID: "task-1", Objective: "迁移上下文", Status: "active"}},
		SkillStack:   []sessionstore.SkillFrame{{SkillID: "skill-1", Name: "go"}},
		CompactStack: []sessionstore.CompactFrame{{SegmentID: "compact-1", From: 0, To: 2, Summary: "先期摘要"}},
		GoalStack: []sessionstore.GoalFrame{{
			GoalID: "goal-1", Title: "审查 goal 域（不入上下文）", Status: "active",
		}},
	}
	if got := blockNames(RenderStackBlocks(withStacks)); !reflect.DeepEqual(got, []string{"skill", "compact", "plan", "task"}) {
		t.Fatalf("stack blocks must stay [skill compact plan task], got %v", got)
	}
	all := RenderStackBlocks(withStacks)
	for _, block := range all {
		if block.Name == "goal" {
			t.Fatalf("goal stack must not render a context block, got %+v", block)
		}
		if block.Messages != nil && len(block.Messages) > 0 && block.Messages[0].Content != nil &&
			strings.Contains(*block.Messages[0].Content, "审查 goal 域（不入上下文）") {
			t.Fatalf("goal stack title leaked into context block %q", block.Name)
		}
	}
}

// TestGoalStackNotUsedForCompactionSummary 固化压缩边界：栈内 goal 不做
// 压缩摘要的“目标 (Goal)”素材（该小节仍由 TaskStack 顶承担）；聊天记录里的
// #goal 文本作为普通转录内容经溢出单元（Overflow）照常被压缩。
func TestGoalStackNotUsedForCompactionSummary(t *testing.T) {
	record := sessionstore.SessionContextRecord{
		GoalStack: []sessionstore.GoalFrame{{
			GoalID: "goal-1", Title: "这是栈内目标，不得冒充压缩目标", Status: "active",
		}},
	}
	body := LocalChapter2(LocalFoldOptions{Record: record})
	section := sectionBodyBetween(body, Chapter2SectionGoal)
	if section != "" && strings.Contains(section, "这是栈内目标") {
		t.Fatalf("goal stack must not feed compaction summary Goal section:\n%s", body)
	}
	if !strings.Contains(section, compactEmptySection) && section == "" {
		t.Fatalf("empty task stack Goal section should stay (none):\n%s", body)
	}
}

// sectionBodyBetween 截取指定标题后至下一个 "### " 标题的正文（测试辅助）。
func sectionBodyBetween(body, title string) string {
	start := strings.Index(body, "### "+title)
	if start < 0 {
		return ""
	}
	start += len("### " + title)
	if next := strings.Index(body[start:], "\n### "); next >= 0 {
		return strings.TrimSpace(body[start : start+next])
	}
	return strings.TrimSpace(body[start:])
}
