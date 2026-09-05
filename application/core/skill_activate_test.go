package core

import (
	"context"
	"strings"
	"testing"
)

// TestActivateSkillPushesLayerAndNextSubmissionTrustsIt：模型侧激活（ActivateSkill）
// 把技能压入会话 skill 层，后续普通提交自动带进任务的 Trusted Active Skill。
func TestActivateSkillPushesLayerAndNextSubmissionTrustsIt(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()

	skill, err := service.ActivateSkill("review")
	if err != nil {
		t.Fatalf("ActivateSkill: %v", err)
	}
	if skill.Name != "review" || skill.Prompt != "review prompt" {
		t.Fatalf("ActivateSkill returned %#v", skill)
	}
	if got := service.promptStack.Describe(); !strings.Contains(got, "review") {
		t.Fatalf("skill layer not pushed into prompt stack: %q", got)
	}

	// 普通提交（不带 #）→ newChatRequest 从 promptStack 收集 skill 层 →
	// 任务 TrustedSkillLayers → ActivateTaskSkillsLocked 把正文 append-only 落进
	// transcript（internal user 事件；system 保持稳定、不含技能正文）。
	if err := service.Submit(context.Background(), "继续审查这段代码"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	engine.mu.Lock()
	prompt := engine.prompt
	sentHistory := append([]EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	if strings.Contains(prompt, "## Trusted Active Skill") || strings.Contains(prompt, "review prompt") {
		t.Fatalf("activated skill body must not be embedded in system: %q", prompt)
	}
	if !trustedSkillInHistory(t, sentHistory, "review", "review prompt") {
		t.Fatalf("activated skill body must appear in assembled engine history: %#v", sentHistory)
	}
}

// TestActivateSkillIdempotent：同名重复激活不产生重复层（Push 同名覆盖）。
func TestActivateSkillIdempotent(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	if _, err := service.ActivateSkill("review"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ActivateSkill("review"); err != nil {
		t.Fatal(err)
	}
	var skills []string
	for _, layer := range service.promptStack.Layers() {
		if layer.Kind == "skill" && layer.Name == "review" {
			skills = append(skills, layer.Name)
		}
	}
	if len(skills) != 1 {
		t.Fatalf("review layer must appear exactly once, got %d (%v)", len(skills), skills)
	}
}

// TestActivateSkillRejectsBadNames：空名/未知技能/goal 均拒绝且不改状态。
func TestActivateSkillRejectsBadNames(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	for _, name := range []string{"", "  ", "ghost", "goal"} {
		if _, err := service.ActivateSkill(name); err == nil {
			t.Fatalf("ActivateSkill(%q) must error", name)
		}
	}
	if got := service.promptStack.Describe(); strings.Contains(got, "skill") && got != "base" && !strings.HasPrefix(got, "E:") {
		t.Fatalf("no skill layer may be pushed on error: %q", got)
	}
}

// TestActivateSkillCoexistsWithUserSlashSkill：模型激活与 #<name> 路径共享
// 同一层语义（# 激活后 ActivateSkill 同名覆盖不产生重复）。
func TestActivateSkillCoexistsWithUserSlashSkill(t *testing.T) {
	engine := &fakeEngine{}
	service := newTestService(t, engine)
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "/review strict"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	if _, err := service.ActivateSkill("review"); err != nil {
		t.Fatal(err)
	}
	var count int
	for _, layer := range service.promptStack.Layers() {
		if layer.Kind == "skill" && layer.Name == "review" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("slash + model activation must share one layer, got %d", count)
	}
}
