package admin

import (
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	view_state "github.com/RedHuang-0622/seelex/middle/view/state"
)

// RuntimeVisibility 构造「可见性投影」（goal 技能门控 + 当前会话的 goal 治理
// 只读视图）。投影是不可变值：宿主在锁下取数、释放锁后再发布（Runtime 因此
// 不会从工具可见性或子代理路径回调宿主）。
func RuntimeVisibility(goalSkillActive bool, governance *dto.GoalGovernanceView) dto.RuntimeVisibilityProjection {
	return dto.RuntimeVisibilityProjection{
		GoalSkillActive: goalSkillActive,
		GoalGovernance:  governance,
	}
}

// ParentEvidence 构造「父证据投影」：会话 ID、最近一条可见用户目标正文、
// 会话消息总数。三个值都由宿主在它自己的锁下读出。
func ParentEvidence(sessionID string, messages []model.Message, conversationCount int) dto.ParentEvidenceProjection {
	return dto.ParentEvidenceProjection{
		SessionID:         sessionID,
		Goal:              LatestVisibleUserGoal(messages),
		ConversationCount: conversationCount,
	}
}

// LatestVisibleUserGoal 返回最后一条**可见**用户输入正文作为"父会话在做什么"的
// 证据：跳过空正文，并跳过子代理产出标记行（那是子代理自己的话，不是父会话的
// 目标）。超过 200 个 rune 时截断并加省略号；没有可用输入时返回空串。
func LatestVisibleUserGoal(messages []model.Message) string {
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
