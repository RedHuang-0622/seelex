package agentteam

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// goalA2APreset 是第一个实例：goal 的 user→main↔tl 固定循环。
//
// 它是 preset 而不是特例：顺序策略、角色集合、gate 都由 TeamSpec 描述，换成
// review/research 实例时只换这份描述，不换工厂、sequencer 或恢复形态。
func goalA2APreset() dto.TeamSpec {
	return dto.TeamSpec{
		TeamID:        string(dto.TeamKindGoalA2A),
		TeamKind:      string(dto.TeamKindGoalA2A),
		OrderPolicy:   dto.OrderPolicyGoalLoop,
		OrderRoles:    []string{string(dto.RoleKindUser), string(dto.RoleKindMain), RoleTechlead},
		GatePolicy:    "goal_finish_gate",
		CompactPolicy: "main_authoritative",
		Roles: []dto.RoleSpec{
			{RoleName: string(dto.RoleKindUser), RoleKind: dto.RoleKindUser, JoinPolicy: "builtin"},
			{RoleName: string(dto.RoleKindMain), RoleKind: dto.RoleKindMain, JoinPolicy: "builtin"},
			{
				RoleName:        RoleTechlead,
				RoleKind:        dto.RoleKindTechlead,
				OrderPriority:   1,
				JoinPolicy:      "on_goal_create",
				PresencePolicy:  "online_when_goal_active",
				ToolsPolicy:     "readonly",
				DirectiveSchema: []string{"verdict_done", "verdict_not_done", "escalate_human"},
			},
		},
	}
}

// reviewTeamPreset 是第二个实例（AT8 证据）：同一工厂、同一 sequencer、同一恢复
// 形态，换的只是角色集与顺序策略。
func reviewTeamPreset() dto.TeamSpec {
	return dto.TeamSpec{
		TeamID:        string(dto.TeamKindReview),
		TeamKind:      string(dto.TeamKindReview),
		OrderPolicy:   dto.OrderPolicyUserMainDecided,
		OrderRoles:    []string{string(dto.RoleKindUser), string(dto.RoleKindMain), "reviewer"},
		GatePolicy:    "review_signoff",
		CompactPolicy: "main_authoritative",
		Roles: []dto.RoleSpec{
			{RoleName: string(dto.RoleKindUser), RoleKind: dto.RoleKindUser, JoinPolicy: "builtin"},
			{RoleName: string(dto.RoleKindMain), RoleKind: dto.RoleKindMain, JoinPolicy: "builtin"},
			{
				RoleName:        "reviewer",
				RoleKind:        dto.RoleKindAgent,
				OrderPriority:   1,
				JoinPolicy:      "on_team_create",
				PresencePolicy:  "online_when_reviewing",
				DirectiveSchema: []string{"approve", "request_changes", "escalate_human"},
			},
		},
	}
}

// researchTeamPreset 演示「定时 agent 不入 order_roles」的第三形态。
func researchTeamPreset() dto.TeamSpec {
	return dto.TeamSpec{
		TeamID:        string(dto.TeamKindResearch),
		TeamKind:      string(dto.TeamKindResearch),
		OrderPolicy:   dto.OrderPolicyUserMainDecided,
		OrderRoles:    []string{string(dto.RoleKindUser), string(dto.RoleKindMain), "researcher"},
		GatePolicy:    "research_report",
		CompactPolicy: "main_authoritative",
		Roles: []dto.RoleSpec{
			{RoleName: string(dto.RoleKindUser), RoleKind: dto.RoleKindUser, JoinPolicy: "builtin"},
			{RoleName: string(dto.RoleKindMain), RoleKind: dto.RoleKindMain, JoinPolicy: "builtin"},
			{RoleName: "researcher", RoleKind: dto.RoleKindAgent, OrderPriority: 1, JoinPolicy: "on_team_create"},
			{RoleName: "digest", RoleKind: dto.RoleKindTimer, OrderPriority: 9, JoinPolicy: "scheduled"},
		},
	}
}

// Preset 返回内置团队实例（goal-a2a / review-team / research-team）。
func Preset(teamKind string) (dto.TeamSpec, error) {
	switch teamKind {
	case string(dto.TeamKindGoalA2A), "":
		return goalA2APreset(), nil
	case string(dto.TeamKindReview):
		return reviewTeamPreset(), nil
	case string(dto.TeamKindResearch):
		return researchTeamPreset(), nil
	default:
		return dto.TeamSpec{}, fmt.Errorf("%w: %s", ErrUnknownPreset, teamKind)
	}
}

// Presets 返回全部内置 preset（供前端角色管理页列出可选团队形态）。
func Presets() []dto.TeamSpec {
	return []dto.TeamSpec{goalA2APreset(), reviewTeamPreset(), researchTeamPreset()}
}
