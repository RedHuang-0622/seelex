package core

import (
	"testing"
)

// TestActiveSkillsProjectionForSession（G1-A）：skill/目标可见性按会话取
// 任务级投影——后台会话 A 的 goal skill 激活不因视图切到 B 而丢失。
func TestActiveSkillsProjectionForSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})

	service.ViewMu.Lock()
	taskA := service.components.tasks.BeginTaskFor("session-a", "req-a", "goal task", "high", nil, TaskCheckpoint{})
	service.components.tasks.ActivateTaskSkillsLocked(taskA, []PromptLayer{
		{Name: "goal", Text: "goal layer"},
		{Name: "review", Text: "review layer"},
	})
	taskB := service.components.tasks.BeginTaskFor("session-b", "req-b", "coding task", "high", nil, TaskCheckpoint{})
	service.components.tasks.ActivateTaskSkillsLocked(taskB, []PromptLayer{
		{Name: "review", Text: "review layer"},
	})
	service.ViewMu.Unlock()

	idsA := service.components.tasks.ActiveSkillIDsFor("session-a")
	if len(idsA) != 2 || idsA[0] != "goal" || idsA[1] != "review" {
		t.Fatalf("ActiveSkillIDsFor(session-a) = %v, want [goal review]", idsA)
	}
	idsB := service.components.tasks.ActiveSkillIDsFor("session-b")
	if len(idsB) != 1 || idsB[0] != "review" {
		t.Fatalf("ActiveSkillIDsFor(session-b) = %v, want [review]", idsB)
	}
	if !service.components.tasks.GoalSkillActiveFor("session-a") {
		t.Fatal("GoalSkillActiveFor(session-a) = false, want true")
	}
	if service.components.tasks.GoalSkillActiveFor("session-b") {
		t.Fatal("GoalSkillActiveFor(session-b) = true, want false")
	}
}
