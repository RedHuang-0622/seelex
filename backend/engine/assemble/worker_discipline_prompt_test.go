package assemble

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
)

// TestWorkerSystemPromptCarriesTheWorkItemDiscipline：teammate 的**收尾纪律**必须落在它自己的
// 系统提示里，而不是只写在 leader 的 SKILL 里（2026-10-15 审计 D6）。
//
// 现象：真实会话里 leader 为 4 件以上工作项手工补提交、变基、解冲突、合并——说明"完成即提交"
// 在执行面完全没约束，尾插一遇到未提交改动就把现场留给 leader。约束的位置只能是 teammate
// 必然读到的那一处（系统提示），且必须盖得住已登记的员工提示词。
func TestWorkerSystemPromptCarriesTheWorkItemDiscipline(t *testing.T) {
	fixture := newTeamHostFixture(t, teamHostPlan(), teamHostBindings())
	request := teamwork.WorkerRequest{
		MainSessionID: "sess-1", Role: "exec", RoleSessionID: "sess-1-t-exec-wi-1",
		WorkItemID: "wi-1", Worktree: "seelex/exec-wi-1", Goal: "把现场生命周期接进契约",
	}

	spec := fixture.runtime.workerRoleRoundSpec(request, "sess-1", 4)
	for _, want := range []string{
		"git add", "git commit", "收尾前必须提交", "不碰主工作区",
	} {
		if !strings.Contains(spec.SystemPrompt, want) {
			t.Fatalf("worker 系统提示缺作业纪律 %q（未提交改动会把现场留给 leader）：\n%s", want, spec.SystemPrompt)
		}
	}

	// 已登记的员工提示词也盖不掉它：纪律是系统事实，不是角色可选项。
	fixture.runtime.SetRolePromptProvider(func(string) string {
		return "你是员工：只做被交代的事。"
	})
	registered := fixture.runtime.workerRoleRoundSpec(request, "sess-1", 4)
	if !strings.Contains(registered.SystemPrompt, "你是员工：只做被交代的事。") {
		t.Fatalf("登记的员工提示词必须仍然生效：\n%s", registered.SystemPrompt)
	}
	if !strings.Contains(registered.SystemPrompt, "收尾前必须提交") {
		t.Fatalf("作业纪律不因登记提示词而消失（它是系统事实）：\n%s", registered.SystemPrompt)
	}
}
