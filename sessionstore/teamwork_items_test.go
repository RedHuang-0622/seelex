package sessionstore

import (
	"context"
	"strings"
	"testing"
)

// itemPlan 是一份**里程碑 + Work Item** 口径的计划：里程碑之间靠屏障串行，
// 里程碑内部的 exec → test_case 是 V 模型的依赖边。
func itemPlan() TeamworkPlan {
	return TeamworkPlan{
		TeamID:  "v-model",
		Version: 1,
		Members: []TeamworkMember{
			{Role: "pm", RoleSessionID: "s-v-model-pm"},
			{Role: "exec", RoleSessionID: "s-v-model-exec"},
			{Role: "test_case", RoleSessionID: "s-v-model-test_case"},
		},
		Milestones: []TeamworkMilestone{
			{
				ID: "m-build",
				Items: []TeamworkWorkItem{
					{ID: "wi-req", Milestone: "m-build", Role: "pm", Name: "需求澄清", Goal: "边界写清"},
					{ID: "wi-impl", Milestone: "m-build", Role: "exec", Name: "实现", DependsOn: []string{"wi-req"}},
					{ID: "wi-test", Milestone: "m-build", Role: "test_case", Name: "用例", DependsOn: []string{"wi-impl"}},
				},
			},
			{ID: "m-ship", DependsOn: []string{"m-build"}},
		},
	}
}

func TestValidateTeamworkPlanAcceptsMilestoneWorkItems(t *testing.T) {
	if err := ValidateTeamworkPlan(itemPlan(), 6); err != nil {
		t.Fatalf("里程碑 + Work Item 口径的计划应当合法: %v", err)
	}
}

func TestValidateTeamworkPlanRejectsCrossMilestoneItemDependency(t *testing.T) {
	plan := itemPlan()
	plan.Milestones[1].Items = []TeamworkWorkItem{
		{ID: "wi-ship", Role: "exec", Name: "发布", DependsOn: []string{"wi-impl"}},
	}
	err := ValidateTeamworkPlan(plan, 6)
	if err == nil || !strings.Contains(err.Error(), "跨里程碑") {
		t.Fatalf("跨里程碑的 item 依赖必须被拒（顺序只能由里程碑屏障表达），得到 %v", err)
	}
}

func TestValidateTeamworkPlanRejectsItemCycles(t *testing.T) {
	plan := itemPlan()
	plan.Milestones[0].Items = []TeamworkWorkItem{
		{ID: "a", Role: "pm", Name: "A", DependsOn: []string{"b"}},
		{ID: "b", Role: "exec", Name: "B", DependsOn: []string{"a"}},
	}
	err := ValidateTeamworkPlan(plan, 6)
	if err == nil || !strings.Contains(err.Error(), "存在环") {
		t.Fatalf("里程碑内 item 依赖成环必须被拒，得到 %v", err)
	}
}

func TestValidateTeamworkPlanRejectsMilestoneCycles(t *testing.T) {
	plan := itemPlan()
	plan.Milestones[0].DependsOn = []string{"m-ship"}
	err := ValidateTeamworkPlan(plan, 6)
	if err == nil || !strings.Contains(err.Error(), "存在环") {
		t.Fatalf("里程碑屏障成环必须被拒，得到 %v", err)
	}
}

func TestValidateTeamworkPlanRejectsItemWithUnenrolledRole(t *testing.T) {
	plan := itemPlan()
	plan.Milestones[0].Items[0].Role = "ghost"
	err := ValidateTeamworkPlan(plan, 6)
	if err == nil || !strings.Contains(err.Error(), "不在编") {
		t.Fatalf("工作项的执行角色不在编必须被拒，得到 %v", err)
	}
}

func TestValidateTeamworkPlanRejectsDuplicateItemIDAcrossMilestones(t *testing.T) {
	plan := itemPlan()
	plan.Milestones[1].Items = []TeamworkWorkItem{{ID: "wi-req", Role: "pm", Name: "重复"}}
	err := ValidateTeamworkPlan(plan, 6)
	if err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("跨里程碑的工作项 id 重复必须被拒（依赖以 id 为准），得到 %v", err)
	}
}

// TestTeamworkBindingLedgerIsAppendOnlyKV 钉住绑定账本的 KV 语义：
// 追加一行 = 建立绑定，再追加一行（Released）= 结束绑定；读侧按 work_item 取最后一行。
func TestTeamworkBindingLedgerIsAppendOnlyKV(t *testing.T) {
	repository, key := teamworkFixture(t)
	ctx := context.Background()
	bind := func(binding TeamworkBinding) {
		t.Helper()
		if err := repository.AppendTeamworkBinding(ctx, key, binding); err != nil {
			t.Fatalf("AppendTeamworkBinding: %v", err)
		}
	}
	bind(TeamworkBinding{WorkItem: "wi-impl", Role: "exec", SessionID: "s-wi-impl", Worktree: "seelex/exec-wi-impl"})
	bind(TeamworkBinding{WorkItem: "wi-test", Role: "test_case", SessionID: "s-wi-test", Worktree: "seelex/test_case-wi-test"})

	rows, err := repository.ReadTeamworkBindings(ctx, key)
	if err != nil {
		t.Fatalf("ReadTeamworkBindings: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("账本应有两行绑定，得到 %d", len(rows))
	}
	current := TeamworkBindings(rows)
	if current["wi-impl"].SessionID != "s-wi-impl" || current["wi-test"].Worktree != "seelex/test_case-wi-test" {
		t.Fatalf("绑定折叠错了: %+v", current)
	}

	// 结束 wi-impl 的绑定：再追加一行（不删旧行）。
	bind(TeamworkBinding{WorkItem: "wi-impl", Role: "exec", SessionID: "s-wi-impl", Worktree: "seelex/exec-wi-impl", Released: true, Reason: "accept"})
	rows, err = repository.ReadTeamworkBindings(ctx, key)
	if err != nil {
		t.Fatalf("ReadTeamworkBindings: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("释放是**追加**而不是重写：应有三行，得到 %d", len(rows))
	}
	current = TeamworkBindings(rows)
	if _, ok := current["wi-impl"]; ok {
		t.Fatalf("已释放的绑定不该出现在当前绑定里: %+v", current)
	}
	if current["wi-test"].WorkItem != "wi-test" {
		t.Fatalf("释放一个 work item 不该影响另一个: %+v", current)
	}
}

func TestAppendTeamworkBindingRequiresWorkItem(t *testing.T) {
	repository, key := teamworkFixture(t)
	if err := repository.AppendTeamworkBinding(context.Background(), key, TeamworkBinding{Role: "exec"}); err == nil {
		t.Fatal("缺 work_item 的绑定必须被拒")
	}
}
