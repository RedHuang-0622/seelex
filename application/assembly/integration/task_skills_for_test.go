package integration

import (
	. "github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"testing"
)

// TestActiveSkillsProjectionForSession（G1-A）：skill/目标可见性按会话取
// 任务级投影——后台会话 A 的 goal skill 激活不因视图切到 B 而丢失。

func TestActiveSkillsProjectionForSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})

	service.ViewMu.Lock()
	taskA := ComponentsFor(service).Execution.Tasks.BeginTaskFor("session-a", "req-a", "goal task", "high", nil, model.TaskCheckpoint{})
	ComponentsFor(service).Execution.Tasks.ActivateTaskSkillsLocked(taskA, []prompt.PromptLayer{
		{Name: "goal", Text: "goal layer"},
		{Name: "review", Text: "review layer"},
	})
	taskB := ComponentsFor(service).Execution.Tasks.BeginTaskFor("session-b", "req-b", "coding task", "high", nil, model.TaskCheckpoint{})
	ComponentsFor(service).Execution.Tasks.ActivateTaskSkillsLocked(taskB, []prompt.PromptLayer{
		{Name: "review", Text: "review layer"},
	})
	service.ViewMu.Unlock()

	idsA := ComponentsFor(service).Execution.Tasks.ActiveSkillIDsFor("session-a")
	if len(idsA) != 2 || idsA[0] != "goal" || idsA[1] != "review" {
		t.Fatalf("ActiveSkillIDsFor(session-a) = %v, want [goal review]", idsA)
	}
	idsB := ComponentsFor(service).Execution.Tasks.ActiveSkillIDsFor("session-b")
	if len(idsB) != 1 || idsB[0] != "review" {
		t.Fatalf("ActiveSkillIDsFor(session-b) = %v, want [review]", idsB)
	}
	if !ComponentsFor(service).Execution.Tasks.GoalSkillActiveFor("session-a") {
		t.Fatal("GoalSkillActiveFor(session-a) = false, want true")
	}
	if ComponentsFor(service).Execution.Tasks.GoalSkillActiveFor("session-b") {
		t.Fatal("GoalSkillActiveFor(session-b) = true, want false")
	}
}
