package tools

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

func planTool(name string) types.Tool {
	return types.Tool{Type: "function", Function: types.ToolFunction{Name: name}}
}

// TestPolicyHidesComputerInputToolsFromSubagents 验证 computer use 的输入注入
// 只归主代理：并行子代理共享同一块桌面，同时注入输入会互相打断；观察类工具
// （截图/窗口枚举/等待）对子代理保持可见。
func TestPolicyHidesComputerInputToolsFromSubagents(t *testing.T) {
	policy := NewPolicy(PolicyDeps{})
	subCtx := model.WithNodeScope(context.Background(), model.NodeScope{NodeID: "s1", Role: model.RoleSubAgent})
	got := policy.Filter(subCtx, []types.Tool{
		planTool("computer_screenshot"), planTool("computer_windows"), planTool("computer_wait"),
		planTool("computer_click"), planTool("computer_move"), planTool("computer_drag"),
		planTool("computer_scroll"), planTool("computer_type"), planTool("computer_keys"),
		planTool("computer_focus"), planTool("bash"),
	})
	want := []string{"computer_screenshot", "computer_windows", "computer_wait", "bash"}
	if len(got) != len(want) {
		t.Fatalf("子代理可见工具 = %v, want %v", names(got), want)
	}
	for index, name := range want {
		if got[index].Function.Name != name {
			t.Fatalf("子代理可见工具 = %v, want %v", names(got), want)
		}
	}

	// 主代理：整族可见（权限门控另行把关）。
	mainGot := policy.Filter(context.Background(), []types.Tool{
		planTool("computer_click"), planTool("computer_type"), planTool("bash"),
	})
	if len(mainGot) != 3 {
		t.Fatalf("主代理可见工具 = %v, want 全部可见", names(mainGot))
	}
}

func TestPolicyFiltersSubagentExcludedAndGoalPlanTools(t *testing.T) {
	policy := NewPolicy(PolicyDeps{
		GoalSkillActive: func() bool { return false },
		PluginFilter:    func(ts []types.Tool) []types.Tool { return ts },
	})
	ctx := model.WithNodeScope(context.Background(), model.NodeScope{NodeID: "s1", Role: model.RoleSubAgent})
	got := policy.Filter(ctx, []types.Tool{
		planTool("plan_run"), planTool("task_complete"), planTool("fork_subagents"),
		planTool("switch_plugin"), planTool("switch_mode"), planTool("skill_activate"),
		planTool("bash"),
	})
	if len(got) != 1 || got[0].Function.Name != "bash" {
		t.Fatalf("subagent visible tools = %v, want only bash", names(got))
	}

	mainCtx := context.Background()
	gotMain := policy.Filter(mainCtx, []types.Tool{planTool("plan_run"), planTool("bash")})
	if len(gotMain) != 1 || gotMain[0].Function.Name != "bash" {
		t.Fatalf("goal-inactive main tools = %v, want only bash", names(gotMain))
	}
}

// TestPolicyGoalToolsGatedByGoalActive 验证 P1 goal 工具门控：goal_begin
// 是发球入口，主代理始终可见；其余 goal 工具在治理激活后可见；子代理一律
// 不可见。
func TestPolicyGoalToolsGatedByGoalActive(t *testing.T) {
	inactive := NewPolicy(PolicyDeps{GoalSkillActive: func() bool { return false }})
	mainCtx := context.Background()
	got := inactive.Filter(mainCtx, []types.Tool{
		planTool("goal_begin"), planTool("goal_update"), planTool("goal_status"),
		planTool("goal_propose_finish"), planTool("bash"),
	})
	if len(got) != 2 || got[0].Function.Name != "goal_begin" || got[1].Function.Name != "bash" {
		t.Fatalf("goal-inactive main tools = %v, want goal_begin + bash", names(got))
	}
	active := NewPolicy(PolicyDeps{GoalActive: func() bool { return true }})
	got = active.Filter(mainCtx, []types.Tool{
		planTool("goal_begin"), planTool("goal_status"), planTool("bash"),
	})
	if len(got) != 3 {
		t.Fatalf("goal-active main tools = %v, want goal_begin/goal_status/bash", names(got))
	}
	subCtx := model.WithNodeScope(context.Background(), model.NodeScope{NodeID: "s1", Role: model.RoleSubAgent})
	got = active.Filter(subCtx, []types.Tool{planTool("goal_begin"), planTool("bash")})
	if len(got) != 1 || got[0].Function.Name != "bash" {
		t.Fatalf("subagent must never see goal tools = %v", names(got))
	}
}

func names(ts []types.Tool) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Function.Name)
	}
	return out
}
