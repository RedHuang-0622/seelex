package core

import (
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/view_state"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

// publishRuntimeProjections 在 service.ViewMu 下拷贝应用自有状态，释放锁后发布
// 不可变值。Runtime 因此不会从工具可见性或子代理路径回调 Application。
func (service *Service) publishRuntimeProjections() {
	service.ViewMu.RLock()
	var goalGovernance *dto.GoalGovernanceView
	if service.components.goal != nil {
		goalGovernance = service.components.goal.GoalGovernanceViewFor(service.Core.Snapshot.Session.ID)
	}
	projection := seelebridge.RuntimeVisibilityProjection{
		GoalSkillActive: service.components.tasks.GoalSkillActive(),
		GoalGovernance:  goalGovernance,
	}
	evidence := seelebridge.ParentEvidenceProjection{
		SessionID:         service.Core.Snapshot.Session.ID,
		Goal:              latestVisibleUserGoal(service.Core.Snapshot.Conversation),
		ConversationCount: service.Core.Snapshot.TotalMessages,
	}
	service.ViewMu.RUnlock()
	service.Deps.Runtime.SetRuntimeVisibilityProjection(projection)
	service.Deps.Runtime.SetParentEvidenceProjection(evidence)
}

func latestVisibleUserGoal(messages []Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role == "user" && strings.TrimSpace(message.Content) != "" && !strings.HasPrefix(message.Content, view_state.SubagentContextMarker) {
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
