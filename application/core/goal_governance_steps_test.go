package core

// goal_governance_steps_test.go — 钉住「ADVISOR 评审过程」进入只读治理视图这条
// **端到端投影**（Evaluator 上报步骤 → Supervisor 保留 → coordinator 投影成
// dto.GoalStepView → 前端渲染）。
//
// 为什么单独立文件：goal_coordinator_test.go 钉的是治理循环的推进语义；这里钉的是
// "评审过程能不能被前端看见"这一条新增的读面。前端此前只有终局裁决（tl_directive），
// 评审者调了哪些只读工具完全不可见。

import (
	"context"
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// stubStepEvaluator 在评审回合里上报"工具步骤"（模拟 ADVISOR 在角色会话里调只读工具）。
type stubStepEvaluator struct {
	steps []goaldomain.TLStep
}

func (e *stubStepEvaluator) Evaluate(ctx context.Context, _ goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error) {
	if sink := goaldomain.TLStepSinkFrom(ctx); sink != nil {
		for _, step := range e.steps {
			sink(step)
		}
	}
	return goaldomain.TLDirective{Kind: goaldomain.DirectiveCorrect, Content: "继续"}, nil
}

func TestGoalGovernanceViewCarriesRoundSteps(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		Evaluator: &stubStepEvaluator{steps: []goaldomain.TLStep{
			{Kind: "tool", Name: "read_file", Args: `{"path":"README.md"}`},
			{Kind: "tool_result", Name: "read_file", Result: "命中 3 处"},
		}},
	})
	ctx := context.Background()
	if _, err := coordinator.Begin(ctx, "session-steps", goaldomain.BeginRequest{Title: "过程可见"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	// 席位轮转退场后回合尾不再自动跑 ADVISOR；这里显式驱动一轮 TL 回合（终态 gate /
	// 审批预筛在真实链路里走的就是这条），验证评审过程仍能投影到只读视图。
	if _, err := coordinator.bundleFor("session-steps").sup.RunEval(ctx, "test"); err != nil {
		t.Fatalf("RunEval: %v", err)
	}
	view := coordinator.GoalGovernanceViewFor("session-steps")
	if view == nil {
		t.Fatal("治理视图不应为空（goal 仍在 active）")
	}
	if len(view.RoundSteps) != 2 {
		t.Fatalf("治理视图应携带评审过程步骤（否则前端看不到评审者做了什么）: %+v", view.RoundSteps)
	}
	if view.RoundSteps[0].Kind != "tool" || view.RoundSteps[0].Name != "read_file" ||
		view.RoundSteps[0].Args != `{"path":"README.md"}` {
		t.Fatalf("调用步骤投影形状不对: %+v", view.RoundSteps[0])
	}
	if view.RoundSteps[1].Kind != "tool_result" || view.RoundSteps[1].Result != "命中 3 处" {
		t.Fatalf("返回步骤投影形状不对: %+v", view.RoundSteps[1])
	}
}

// TestGoalGovernanceViewWithoutStepsHasNoEmptyShell：没有步骤时不产生空壳
// （面板隐藏过程区，不显示"评审过程：无"）。
func TestGoalGovernanceViewWithoutStepsHasNoEmptyShell(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		Evaluator: &stubTLEvaluator{directives: []goaldomain.TLDirective{{
			Kind: goaldomain.DirectiveCorrect, Content: "继续",
		}}},
	})
	ctx := context.Background()
	if _, err := coordinator.Begin(ctx, "session-nosteps", goaldomain.BeginRequest{Title: "无过程"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := coordinator.bundleFor("session-nosteps").sup.RunEval(ctx, "test"); err != nil {
		t.Fatalf("RunEval: %v", err)
	}
	if view := coordinator.GoalGovernanceViewFor("session-nosteps"); view == nil || len(view.RoundSteps) != 0 {
		t.Fatalf("没有步骤时不应产生过程空壳: %+v", view)
	}
}
