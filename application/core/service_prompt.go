package core

import (
	"strings"

	"github.com/RedHuang-0622/seelex/internal/promptassets"
)

// promptCoordinator owns prompt-layer composition and provider synchronization.
// It depends only on assembled application state, never on the Service facade.
type promptCoordinator struct {
	state *serviceState
}

func newPromptCoordinator(state *serviceState) *promptCoordinator {
	return &promptCoordinator{state: state}
}

// buildSystemPrompt owns only system-layer assembly. Skill content remains in
// request envelopes and never becomes a persistent system prompt layer.
func (coordinator *promptCoordinator) buildSystemPrompt() {
	state := coordinator.state
	state.promptStack.ClearKind("identity")
	state.promptStack.ClearKind("instructions")
	state.promptStack.Push("identity", "identity", promptassets.SystemIdentity())
	state.promptStack.ClearKind("base")
	if current, ok := state.deps.Plugins.Current(); ok {
		if prompt := strings.TrimSpace(current.Prompt); prompt != "" {
			state.promptStack.Push("base", "plugin-"+current.Name, prompt)
		}
	}
	_ = state.effortManager.Apply(state.effortManager.Current())
	state.promptStack.Push("instructions", "instructions", promptassets.SystemInstructions())
	state.deps.Engine.SetSystemPrompt(state.promptStack.Render())
}

func (coordinator *promptCoordinator) applyActiveTaskSystemPrompt(requestID string) {
	state := coordinator.state
	state.mu.RLock()
	if task := state.taskExecution; task == nil || task.requestID != requestID {
		state.mu.RUnlock()
		return
	}
	prompt := coordinator.systemPromptForActiveTaskLocked()
	state.mu.RUnlock()
	state.deps.Engine.SetSystemPrompt(prompt)
}

func (coordinator *promptCoordinator) systemPromptForActiveTaskLocked() string {
	state := coordinator.state
	parts := []string{state.promptStack.Render()}
	if task := state.taskExecution; task != nil {
		for _, layer := range task.trustedSkillLayers {
			text := strings.TrimSpace(layer.Text)
			if text == "" {
				continue
			}
			parts = append(parts, "## Trusted Active Skill: "+layer.Name+"\n"+text)
		}
	}
	if plan := activePlanProjection(state.snapshot.Runtime.Plan, state.activePlanID, state.planSequence); plan != nil && plan.Status != string(PlanCompleted) {
		parts = append(parts, "## Active Plan Execution Policy\n"+
			"The Plan is validated and authoritative for this task. Do not silently replace or reorder it. "+
			"Execute the current node and its declared dependencies in stable order. Use read_plan for omitted node detail. "+
			"plan_ref="+plan.CanonicalPlanRef+" current_node="+plan.CurrentNode)
	}
	filtered := parts[:0]
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, "\n\n---\n\n")
}
