package execution

import subagentview "github.com/RedHuang-0622/seelex/middle/view/subagent"

type SubagentViewCoordinator = subagentview.Coordinator
type SubagentViewDeps = subagentview.Deps

// NewSubagentView constructs the Middle-owned subagent projection coordinator.
func NewSubagentView(deps SubagentViewDeps) *SubagentViewCoordinator {
	return subagentview.NewCoordinator(deps)
}
