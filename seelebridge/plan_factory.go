package seelebridge

import (
	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
)

// 节点工厂逻辑已迁入 seelebridge/plan（BuildNode/NodeFactory，跨域构造经
// NodeFactoryDeps 闭包注入）。本文件只保留 Runtime 侧的闭包绑定。

// nodeFactoryDeps 返回绑定到 Runtime 的跨域构造回调（测试与装配共用）。
func (r *Runtime) nodeFactoryDeps() plan.NodeFactoryDeps {
	return plan.NodeFactoryDeps{
		NewAgentNode: func(spec codec.NodeSpec[plan.SeelexNodeInput]) (node.Node, error) {
			return seenode.NewAgentNode(spec, r.nodeDeps()), nil
		},
		CurrentApprovalGate: r.currentApprovalGate,
		NewSummaryNode: func(spec codec.NodeSpec[plan.SeelexNodeInput]) (node.Node, error) {
			return fork.NewSummaryNode(spec), nil
		},
	}
}

// nodeFactory 返回绑定到 Runtime 的 codec.NodeFactory，供 codec.Import/Render 使用。
func (r *Runtime) nodeFactory() codec.NodeFactory[plan.SeelexNodeInput] {
	return plan.NodeFactory(r.nodeFactoryDeps())
}
