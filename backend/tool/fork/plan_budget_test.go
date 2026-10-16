package fork

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/workplan/codec"
	frameworknode "github.com/RedHuang-0622/Seele/workplan/core/node"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
	"github.com/RedHuang-0622/seelex/backend/tool/node"
	"github.com/RedHuang-0622/seelex/backend/tool/plan"
)

// stubAutoNode 是最小 auto 节点（buildForkPlan 渲染 start 用）。
type stubAutoNode struct {
	frameworknode.BaseNode
}

func (stubAutoNode) Run(context.Context, *workplanTypes.WorkflowContext) (string, error) {
	return "", nil
}

// forkLoopHarness 造一个 fork 工具：effort 策略（只判主代理自己的 plan_load 节点）
// + 配置层的子代理节点循环上限（limits.plan_node_max_loops）。
func forkLoopHarness(policy plan.PlanPolicy, planNodeMaxLoops int) *Tool {
	nodeFactory := codec.NodeFactoryFunc[plan.SeelexNodeInput](func(spec codec.NodeSpec[plan.SeelexNodeInput]) (frameworknode.Node, error) {
		switch spec.Input.Kind {
		case "agent":
			return node.NewAgentNode(spec, node.Deps{}), nil
		case "summary":
			return NewSummaryNode(spec), nil
		default:
			return &stubAutoNode{BaseNode: frameworknode.NewBaseNode(spec.ID, frameworknode.KindAuto)}, nil
		}
	})
	return NewTool(Deps{
		CurrentPlanPolicy: func() plan.PlanPolicy { return policy },
		NodeFactory:       func() codec.NodeFactory[plan.SeelexNodeInput] { return nodeFactory },
		PlanNodeMaxLoops:  planNodeMaxLoops,
	})
}

// nodeLoops 按节点 id 读该节点的预算循环上限（只读 agent 节点）。
func nodeLoops(t *testing.T, loaded *plan.LoadedPlanDoc) map[string]int {
	t.Helper()
	loops := make(map[string]int)
	for _, id := range loaded.Plan.AllNodes() {
		agentNode, ok := loaded.Plan.GetNode(id).(*node.AgentNode)
		if !ok {
			continue
		}
		if agentNode.Input().Budget == nil {
			t.Fatalf("fork node %q must carry a budget", id)
		}
		loops[id] = agentNode.Input().Budget.MaxLoops
	}
	return loops
}

func buildLoops(t *testing.T, tool *Tool, specs ...SubagentSpec) map[string]int {
	t.Helper()
	loaded, err := tool.buildForkPlan(Input{Subagents: specs}, map[string]string{})
	if err != nil {
		t.Fatalf("buildForkPlan: %v", err)
	}
	return nodeLoops(t, loaded)
}

// TestForkPlanNodeLoopsIgnoreEffortPolicy：子代理节点循环预算**与 effort 档位无关**
// （2026-10-08 口径：设置 reasoning effort 不得连带改变工作单元的次数）。
//
// 档位手里的 PlanPolicy.MaxNodeLoops 是**主代理自己的 plan_load 节点**判据；拿它
// 当子代理的默认值（或当 max_loops 的上限）就等于"切一次档位，子代理的次数跟着
// 变"。这里把四个档位逐个过一遍：缺省一律取配置 plan_node_max_loops。
func TestForkPlanNodeLoopsIgnoreEffortPolicy(t *testing.T) {
	policies := []plan.PlanPolicy{
		{Effort: "high", MaxNodeLoops: 48},
		{Effort: "max", MaxNodeLoops: 96},
		{Effort: "medium", MaxNodes: 4, RequireSerial: true},
		{Effort: "lite"},
	}
	for _, policy := range policies {
		tool := forkLoopHarness(policy, 15)
		got := buildLoops(t, tool,
			SubagentSpec{ID: "s1", Goal: "fix"},
			SubagentSpec{ID: "s2", Goal: "fix"},
		)
		if len(got) != 2 || got["s1"] != 15 || got["s2"] != 15 {
			t.Fatalf("effort %q（MaxNodeLoops=%d）下子代理循环预算 = %v, want s1=s2=15（配置默认）",
				policy.Effort, policy.MaxNodeLoops, got)
		}
	}
}

// TestForkPlanNodeLoopsHonourDeclaredOverride：单元自己声明的那一条优先于配置默认，
// 且**不因档位不同而被改写**（声明是唯一的事实源，档位不是）。
func TestForkPlanNodeLoopsHonourDeclaredOverride(t *testing.T) {
	for _, policy := range []plan.PlanPolicy{
		{Effort: "high", MaxNodeLoops: 48},
		{Effort: "max", MaxNodeLoops: 96},
	} {
		tool := forkLoopHarness(policy, 15)
		got := buildLoops(t, tool,
			SubagentSpec{ID: "read", Goal: "只读审查", MaxLoops: 6},
			SubagentSpec{ID: "migrate", Goal: "长链条迁移", MaxLoops: 60},
		)
		if got["read"] != 6 || got["migrate"] != 60 {
			t.Fatalf("effort %q 下声明的 max_loops 被改写：%v, want read=6 migrate=60（60 越过档位 48 也不再被拦）",
				policy.Effort, got)
		}
	}
}
