package core

import (
	"strings"

	"github.com/RedHuang-0622/seelex/internal/promptassets"
)

// buildSystemPrompt owns only system-layer assembly. Skill content remains in
// request envelopes and never becomes a persistent system prompt layer.
func (service *Service) buildSystemPrompt() {
	service.promptStack.ClearKind("identity")
	service.promptStack.ClearKind("instructions")
	service.promptStack.Push("identity", "identity", promptassets.SystemIdentity())
	service.promptStack.ClearKind("base")
	if current, ok := service.deps.Plugins.Current(); ok {
		if prompt := strings.TrimSpace(current.Prompt); prompt != "" {
			service.promptStack.Push("base", "plugin-"+current.Name, prompt)
		}
	}
	_ = service.effortManager.Apply(service.effortManager.Current())
	service.promptStack.Push("instructions", "instructions", promptassets.SystemInstructions())
	service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
}
