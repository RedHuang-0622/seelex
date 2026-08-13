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

func names(ts []types.Tool) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Function.Name)
	}
	return out
}
