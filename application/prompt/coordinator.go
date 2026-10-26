package prompt

import (
	promptlayer "github.com/RedHuang-0622/seelex/backend/engine/prompt"
	"github.com/RedHuang-0622/seelex/kernel/state"
)

// Coordinator is the Application-facing identity alias for the Engine-owned
// system prompt coordinator.
type Coordinator = promptlayer.Coordinator

// CoordinatorDeps is the assembly input needed by the prompt coordinator.
// PromptStack and EffortManager structurally satisfy the Engine-owned ports.
type CoordinatorDeps struct {
	Core          *state.Core
	PromptStack   *PromptStack
	EffortManager *EffortManager
	Tasks         promptlayer.TaskContextView
	SkillSigil    string
	Environment   string
}

// NewCoordinator constructs the one Engine-owned prompt coordinator.
func NewCoordinator(deps CoordinatorDeps) *Coordinator {
	return promptlayer.NewCoordinator(promptlayer.Deps{
		Core:          deps.Core,
		PromptStack:   deps.PromptStack,
		EffortManager: deps.EffortManager,
		Tasks:         deps.Tasks,
		SkillSigil:    deps.SkillSigil,
		Environment:   deps.Environment,
	})
}

// ValidateAssets validates the resolved embedded-or-overridden prompt set.
func ValidateAssets() error {
	return promptlayer.ValidateAssets()
}
