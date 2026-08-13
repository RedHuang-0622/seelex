package seelebridge

import (
	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/seelex/seelebridge/node"
)

// SeelexAgentNode 是 plan kind:agent 节点包装的兼容别名（实现已下沉 seelebridge/node）。
type SeelexAgentNode = node.AgentNode

// newSeelexAgentNode 兼容构造：由 Runtime 注入 node.Deps（测试与旧调用面使用）。
func newSeelexAgentNode(spec codec.NodeSpec[SeelexNodeInput], runtime *Runtime) *node.AgentNode {
	return node.NewAgentNode(spec, runtime.nodeDeps())
}
