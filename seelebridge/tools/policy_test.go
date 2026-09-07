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

func TestPolicyFiltersSubagentExcludedAndGoalPlanTools(t *testing.T) {
	policy := NewPolicy(PolicyDeps{
		GoalSkillActive: func() bool { return false },
		PluginFilter:    func(ts []types.Tool) []types.Tool { return ts },
	})
	ctx := model.WithNodeScope(context.Background(), model.NodeScope{NodeID: "s1", Role: model.RoleSubAgent})
	got := policy.Filter(ctx, []types.Tool{
		planTool("plan_run"), planTool("task_complete"), planTool("fork_subagents"), planTool("bash"),
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

// TestPolicyGoalToolsGatedByGoalActive 验证 P1 goal 工具门控：goal 治理未
// 激活时主代理不可见 goal 工具族；激活后可见；子代理一律不可见。
func TestPolicyGoalToolsGatedByGoalActive(t *testing.T) {
	inactive := NewPolicy(PolicyDeps{GoalSkillActive: func() bool { return false }})
	mainCtx := context.Background()
	got := inactive.Filter(mainCtx, []types.Tool{
		planTool("goal_begin"), planTool("goal_update"), planTool("goal_status"),
		planTool("goal_propose_finish"), planTool("bash"),
	})
	if len(got) != 1 || got[0].Function.Name != "bash" {
		t.Fatalf("goal-inactive main tools = %v, want only bash", names(got))
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
