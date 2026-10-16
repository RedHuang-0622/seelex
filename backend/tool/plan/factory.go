package plan

import (
	"fmt"

	"github.com/RedHuang-0622/Seele/workplan/codec"
	frameworknode "github.com/RedHuang-0622/Seele/workplan/core/node"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
)

// NodeFactoryDeps 是 BuildNode 需要的跨域构造回调：agent/summary 节点由根包
// 注入构造器，避免 plan→node/fork 反向依赖。
type NodeFactoryDeps struct {
	// NewAgentNode 构造子代理节点（seelebridge/node.AgentNode 包装）。
	NewAgentNode func(spec codec.NodeSpec[SeelexNodeInput]) (frameworknode.Node, error)
	// CurrentApprovalGate 返回当前审批门（approve/manual 节点读取）。
	CurrentApprovalGate func() approve.ApprovalGate
	// NewSummaryNode 构造 fork 汇总节点（仅 fork DAG 使用）。
	NewSummaryNode func(spec codec.NodeSpec[SeelexNodeInput]) (frameworknode.Node, error)
}

// BuildNode 把 codec 节点规格实例化为可执行 frameworknode.Node（plan.md §3.3）：
//   - agent：子代理节点（deps.NewAgentNode；注入节点作用域 + 节点级 PromptBlocks）；
//   - approve/manual：审批门控节点；
//   - auto/function/verify/deliver：确定性执行节点（不调用 LLM，输出=节点 input）；
//   - summary：fork 汇总节点（deps.NewSummaryNode）。
func BuildNode(spec codec.NodeSpec[SeelexNodeInput], deps NodeFactoryDeps) (frameworknode.Node, error) {
	kind := spec.Input.Kind
	if kind == "" {
		kind = spec.Kind
	}
	if kind == "" {
		kind = "auto" // 旧契约：默认节点为确定性 auto 执行
	}
	switch kind {
	case "agent":
		return deps.NewAgentNode(spec)
	case "approve", "manual":
		return NewApprovalGateNode(spec, deps.CurrentApprovalGate), nil
	case "auto", "function", "verify", "deliver":
		return NewProductNode(spec, kind), nil
	case "summary":
		return deps.NewSummaryNode(spec)
	default:
		return nil, fmt.Errorf("plan_load: node %q has unsupported kind %q (want agent|auto|function|approve|verify|deliver|summary)", spec.ID, kind)
	}
}

// NodeFactory 返回绑定到 deps 的 codec.NodeFactory，供 codec.Import/Render 使用。
func NodeFactory(deps NodeFactoryDeps) codec.NodeFactory[SeelexNodeInput] {
	return codec.NodeFactoryFunc[SeelexNodeInput](func(spec codec.NodeSpec[SeelexNodeInput]) (frameworknode.Node, error) {
		return BuildNode(spec, deps)
	})
}
