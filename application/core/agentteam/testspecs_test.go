package agentteam

import "github.com/RedHuang-0622/seelex/application/contract/dto"

// testspecs_test.go — 测试用的 TeamSpec 夹具。
//
// 这里的三支"团队"曾经是产品里的内置形态目录（`presets.go`：goal-a2a /
// review-team / research-team）。形态目录已于 2026-10-01 删除（用户口径：内置形态对
// teamwork 已经过时——一支团队应该有谁、什么顺序，由数据说，不由代码里的模板说）。
//
// 它们搬到测试里继续用的理由：**证明的是工厂的通用性，不是产品提供的模板**。
// `TestSecondTeamThroughSameFactory`（AT8）要的正是"同一个工厂 + 换一份 TeamSpec
// 就能装配第二支团队"，这份证据与"产品是否内置了模板"无关，所以夹具留在测试侧。

// testGoalSpec 是一支 goal 形态的团队：user → main → tl（tl 是 techlead，角色回合
// 由 leader 派发的 worker 作业驱动；tl 的 ADVISOR 裁决来自 goal 域的终态 gate）。
func testGoalSpec() dto.TeamSpec {
	return dto.TeamSpec{
		TeamID:        "goal-a2a",
		TeamKind:      "goal-a2a",
		OrderPolicy:   dto.OrderPolicyGoalLoop,
		GatePolicy:    "goal_finish_gate",
		CompactPolicy: "main_authoritative",
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser, JoinPolicy: "builtin"},
			{RoleName: "main", RoleKind: dto.RoleKindMain, JoinPolicy: "builtin"},
			{
				RoleName:        "tl",
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

// testReviewSpec 是第二支团队（AT8 证据）：换角色集与顺序策略，代码路径不变。
func testReviewSpec() dto.TeamSpec {
	return dto.TeamSpec{
		TeamID:        "review-team",
		TeamKind:      "review-team",
		OrderPolicy:   dto.OrderPolicyUserMainDecided,
		GatePolicy:    "review_signoff",
		CompactPolicy: "main_authoritative",
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser, JoinPolicy: "builtin"},
			{RoleName: "main", RoleKind: dto.RoleKindMain, JoinPolicy: "builtin"},
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

// testResearchSpec 是第三支团队：演示"定时角色不入 order_roles"的分区。
func testResearchSpec() dto.TeamSpec {
	return dto.TeamSpec{
		TeamID:        "research-team",
		TeamKind:      "research-team",
		OrderPolicy:   dto.OrderPolicyUserMainDecided,
		GatePolicy:    "research_report",
		CompactPolicy: "main_authoritative",
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser, JoinPolicy: "builtin"},
			{RoleName: "main", RoleKind: dto.RoleKindMain, JoinPolicy: "builtin"},
			{RoleName: "researcher", RoleKind: dto.RoleKindAgent, OrderPriority: 1, JoinPolicy: "on_team_create"},
			{RoleName: "digest", RoleKind: dto.RoleKindTimer, OrderPriority: 9, JoinPolicy: "scheduled"},
		},
	}
}
