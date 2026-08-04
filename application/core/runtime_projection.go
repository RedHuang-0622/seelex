package core

import (
	"strings"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

// publishRuntimeProjections copies application-owned state under service.mu,
// then publishes immutable values after releasing the lock. Runtime therefore
// never calls back into Application from tool visibility or subagent paths.
func (service *Service) publishRuntimeProjections() {
	service.mu.RLock()
	projection := seelebridge.RuntimeVisibilityProjection{
		GoalSkillActive: service.goalSkillActive.Load(),
	}
	evidence := seelebridge.ParentEvidenceProjection{
		SessionID:         service.snapshot.Session.ID,
		Goal:              latestVisibleUserGoal(service.snapshot.Conversation),
		ConversationCount: service.snapshot.TotalMessages,
	}
	service.mu.RUnlock()
	service.deps.Runtime.SetRuntimeVisibilityProjection(projection)
	service.deps.Runtime.SetParentEvidenceProjection(evidence)
}

func latestVisibleUserGoal(messages []Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role == "user" && strings.TrimSpace(message.Content) != "" && !strings.HasPrefix(message.Content, subagentContextMarker) {
			return truncateRuntimeProjectionGoal(message.Content)
		}
	}
	return ""
}

func truncateRuntimeProjectionGoal(content string) string {
	const maxGoalRunes = 200
	runes := []rune(content)
	if len(runes) <= maxGoalRunes {
		return content
	}
	return string(runes[:maxGoalRunes]) + "…"
}
