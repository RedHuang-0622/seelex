package teamwork

// role_session_id_consistency_test.go — 角色会话号两个派生实现的**跨实现一致性契约**
// （A12，2026-10-15）。
//
// 背景：角色会话号（`role_session_id`）是"重复装配幂等的键"，它有两个派生实现，本轮
// **不合并**（A12 裁决，见
// docs/2026-10-15-product-architecture-pre-design/legacy-capability-audit.md）：
//
//   - `agentteam.RoleSessionID`：空白 `team_id` 兜底 `dto.DefaultTeamID`（历史身份分量：
//     改它 = 既有角色会话号全体分裂），且三个分量都做 `TrimSpace`；
//   - `teamwork.DefaultRoleSessionID`：不做 team 兜底、只 trim 主会话号——它面对的输入
//     已经过计划校验（`sessionstore.ValidateTeamworkPlan` 要求 `team_id` 非空），多一层
//     兜底只会把"上游坏了"掩盖成"另一个员工的会话"。
//
// 判据不同（空值/空白语义不同）所以不合并；但**两者都拿到非空规范输入时必须逐字同解**
// ——否则同一名员工会在"角色记录"这条路与"teamwork 派发"这条路拿到两个会话号，历史与
// 作业键当场分叉。本用例只钉"同解"，不钉差异本身（差异见上面的说明）。

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/agentteam"
)

func TestRoleSessionIDHelpersAgreeOnCanonicalInputs(t *testing.T) {
	cases := []struct {
		name          string
		mainSessionID string
		teamID        string
		roleName      string
	}{
		{"普通三人组", "sess-1", "v-model", "exec"},
		{"评审角色", "sess-1", "v-model", "tl"},
		{"默认团队名", "sess-1", "goal-a2a", "worker"},
		{"Unicode 分量", "会话-甲", "团队-a", "角色-b"},
		{"下划线主体名", "sess-1", "v-model", "test_case"},
		// 无主会话归属是退化形态（仅桩/测试构造），team/role 仍规范——两者也必须同解。
		{"无主会话归属", "", "v-model", "exec"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := DefaultRoleSessionID(testCase.mainSessionID, testCase.teamID, testCase.roleName)
			want := agentteam.RoleSessionID(testCase.mainSessionID, testCase.teamID, testCase.roleName)
			if got != want {
				t.Fatalf("非空规范输入下两个派生必须同解：teamwork=%q agentteam=%q", got, want)
			}
			if got == "" {
				t.Fatal("非空规范输入不得派生出空号")
			}
		})
	}
}
