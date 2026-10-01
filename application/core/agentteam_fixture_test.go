package core

import "github.com/RedHuang-0622/seelex/application/contract/dto"

// agentteam_fixture_test.go — application 层的团队夹具。
//
// 这里的团队曾经来自产品里的内置形态目录（`agentteam/presets.go` 的 goal-a2a /
// review-team / research-team）。形态目录已于 2026-10-01 删除：一支团队有谁、什么
// 顺序由数据说（TeamSpec / 团队库条目），不由代码里的模板说。测试需要一份具体
// 团队时，就地写一份夹具即可。

// goalTeamFixture 是一支 goal 形态的团队：user → main → tl（tl 是 techlead，
// 装配后由 seatPlan 派生 ADVISOR 评审座位）。
func goalTeamFixture() dto.TeamSpec {
	return dto.TeamSpec{
		TeamID:        "goal-a2a",
		TeamKind:      "goal-a2a",
		OrderPolicy:   dto.OrderPolicyGoalLoop,
		GatePolicy:    "goal_finish_gate",
		CompactPolicy: "main_authoritative",
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser, JoinPolicy: "builtin"},
			{RoleName: "main", RoleKind: dto.RoleKindMain, JoinPolicy: "builtin"},
			{RoleName: "tl", RoleKind: dto.RoleKindTechlead, OrderPriority: 1,
				JoinPolicy: "on_goal_create", PresencePolicy: "online_when_goal_active",
				ToolsPolicy: dto.ToolPolicyReadonly, SystemPrompt: "技术负责人提示词"},
		},
	}
}
