package fork

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/workplan/codec"
	frameworknode "github.com/RedHuang-0622/Seele/workplan/core/node"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
	"github.com/RedHuang-0622/seelex/seelebridge/node"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
)

// stubAutoNode 是最小 auto 节点（buildForkPlan 渲染 start 用）。
type stubAutoNode struct {
	frameworknode.BaseNode
}

func (stubAutoNode) Run(context.Context, *workplanTypes.WorkflowContext) (string, error) {
	return "", nil
}

// TestForkPlanNodesCarryEffortLoopBudget 验证 fork 子代理节点循环预算复用
// effort 调节值（PlanPolicy.MaxNodeLoops）：high=48 → 节点 48 轮；未设置
// （lite/medium）→ 回退通用 PlanNodeMaxLoops。
func TestForkPlanNodesCarryEffortLoopBudget(t *testing.T) {
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
	makeTool := func(policy plan.PlanPolicy, maxLoops int) *Tool {
		return NewTool(Deps{
			CurrentPlanPolicy: func() plan.PlanPolicy { return policy },
			NodeFactory:       func() codec.NodeFactory[plan.SeelexNodeInput] { return nodeFactory },
			PlanNodeMaxLoops:  maxLoops,
		})
	}
	collect := func(t *testing.T, loaded *plan.LoadedPlanDoc) []int {
		t.Helper()
		var loops []int
		for _, id := range loaded.Plan.AllNodes() {
			agentNode, ok := loaded.Plan.GetNode(id).(*node.AgentNode)
			if !ok {
				continue
			}
			if agentNode.Input().Budget == nil {
				t.Fatalf("fork node %q must carry a budget", id)
			}
			loops = append(loops, agentNode.Input().Budget.MaxLoops)
		}
		return loops
	}

	tool := makeTool(plan.PlanPolicy{Effort: "high", MaxNodeLoops: 48}, 0)
	loaded, err := tool.buildForkPlan(Input{
		Subagents: []SubagentSpec{{ID: "s1", Goal: "fix"}, {ID: "s2", Goal: "fix"}},
	}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got := collect(t, loaded); len(got) != 2 || got[0] != 48 || got[1] != 48 {
		t.Fatalf("high-effort fork node loops = %v, want [48 48]", got)
	}

	tool = makeTool(plan.PlanPolicy{Effort: "lite"}, 15)
	loaded, err = tool.buildForkPlan(Input{
		Subagents: []SubagentSpec{{ID: "s1", Goal: "fix"}},
	}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got := collect(t, loaded); len(got) != 1 || got[0] != 15 {
		t.Fatalf("lite-effort fork node loops = %v, want [15]", got)
	}
}
