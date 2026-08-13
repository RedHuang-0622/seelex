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
		AppendNodePhase:          r.node.AppendPhase,
		BeginNodeWorktree:        r.beginNodeWorktree,
		FinishNodeWorktree:       r.finishNodeWorktree,
		ReleaseNodeWorktree:      r.releaseNodeWorktree,
		RegisterNodeSession:      r.node.RegisterSession,
		UnregisterNodeSession:    r.node.UnregisterSession,
		CompleteSubagentNode:     r.node.CompleteSubagentNode,
		NodeParentEvidence:       r.node.ParentEvidence,
		MergeBackIntoParent:      r.mergeBackIntoParent,
		EnqueueSubagentContext:   r.enqueueSubagentContext,
		NodeBudget:               r.node.Budget,
		NodePromptBlocks:         r.node.PromptBlocks,
		Tracer: func() provider.TraceSource {
			return r.Tracer()
		},
	}
}
