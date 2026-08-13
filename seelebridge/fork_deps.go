package seelebridge

import "github.com/RedHuang-0622/seelex/seelebridge/fork"

// forkDeps 把 Runtime 能力面注入 fork 域（Deps 全部为闭包，域内不依赖根包）。
func (r *Runtime) forkDeps() fork.Deps {
	return fork.Deps{
		CurrentPlanPolicy:        r.currentPlanPolicy,
		NodeFactory:              r.nodeFactory,
		TaskResolveByKey:         r.ResolveTaskByKey,
		TaskAdd:                  r.TaskAdd,
		TaskSetStatus:            r.TaskSetStatus,
		TaskAttachParticipant:    r.TaskAttachParticipant,
		SubagentTreeRegisterFork: r.subagentTree.RegisterFork,
		SubagentTreeSummaryFor:   r.subagentTree.SummaryFor,
		RunPlan:                  r.planExecutor.RunPlan,
		ForkTimeoutSec:           r.limits.ForkTimeoutSec,
		PlanNodeMaxLoops:         r.limits.PlanNodeMaxLoops,
	}
}
