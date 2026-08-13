package seelebridge

import (
	"github.com/RedHuang-0622/seelex/seelebridge/node"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
)

// nodeDeps 把 Runtime 能力面注入 node 域（Deps 全部为闭包，域内不依赖根包）。
func (r *Runtime) nodeDeps() node.Deps {
	return node.Deps{
		CurrentAgentFactory:      r.currentAgentFactory,
		CurrentPlanBranchBinding: r.currentPlanBranchBinding,
		AppendNodePhase:          r.appendNodePhase,
		BeginNodeWorktree:        r.beginNodeWorktree,
		FinishNodeWorktree:       r.finishNodeWorktree,
		ReleaseNodeWorktree:      r.releaseNodeWorktree,
		RegisterNodeSession:      r.registerNodeSession,
		UnregisterNodeSession:    r.unregisterNodeSession,
		CompleteSubagentNode:     r.completeSubagentNode,
		NodeParentEvidence:       r.nodeParentEvidence,
		MergeBackIntoParent:      r.mergeBackIntoParent,
		EnqueueSubagentContext:   r.enqueueSubagentContext,
		NodeBudget:               r.nodeBudget,
		NodePromptBlocks:         r.nodePromptBlocks,
		Tracer: func() provider.TraceSource {
			return r.Tracer()
		},
	}
}
