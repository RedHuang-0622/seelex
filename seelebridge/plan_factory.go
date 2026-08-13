package seelebridge

import (
	"fmt"

	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
)

// buildNode 把 codec 节点规格实例化为可执行 node.Node（plan.md §3.3）：
//   - agent：SeelexAgentNode（包装 bridge.NewAgentFactory 产物，注入节点
//     作用域 + 节点级 PromptBlocks，agent_node.go）；
//   - approve/manual：审批门控节点（经 Runtime.SetPlanApprovalGate 注入的门）；
//   - auto/function/verify/deliver：确定性执行节点（不调用 LLM，输出=节点 input）。
//
// 节点实现类型（SeelexNodeInput/NodeBudgetInput/productNode/approvalGateNode/
// canonicalPlanDocument 等）已下沉 seelebridge/plan；本文件只保留绑定
// Runtime 的构造方法（SeelexAgentNode 依赖 Runtime 的节点作用域服务）。
func (r *Runtime) buildNode(spec codec.NodeSpec[SeelexNodeInput]) (node.Node, error) {
	kind := spec.Input.Kind
	if kind == "" {
		kind = spec.Kind
	}
	if kind == "" {
		kind = "auto" // 旧契约：缺省节点为确定性 auto 执行
	}
	switch kind {
	case "agent":
		// 子代理节点：SeelexAgentNode 包装 bridge.NewAgentFactory 产物，
		// Run 时注入节点作用域 + 节点级 PromptBlocks（agent_node.go）。
		return seenode.NewAgentNode(spec, r.nodeDeps()), nil
	case "approve", "manual":
		return newApprovalGateNode(spec, r.currentApprovalGate), nil
	case "auto", "function", "verify", "deliver":
		return newProductNode(spec, kind), nil
	case "summary":
		// fork 汇总节点：拼接全部前驱输出（fork_tool.go；仅 fork DAG 使用）。
		return newForkSummaryNode(spec), nil
	default:
		return nil, fmt.Errorf("plan_load: node %q has unsupported kind %q (want agent|auto|function|approve|verify|deliver|summary)", spec.ID, kind)
	}
}

// nodeFactory 返回绑定到 Runtime 的 codec.NodeFactory，供 codec.Import/Render 使用。
func (r *Runtime) nodeFactory() codec.NodeFactory[SeelexNodeInput] {
	return codec.NodeFactoryFunc[SeelexNodeInput](r.buildNode)
}
