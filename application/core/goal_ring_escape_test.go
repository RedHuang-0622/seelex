package core

// goal_ring_escape_test.go —— 环逃生 × goal 收口的**应用层接线**用例。
//
// 域层语义（逃生判据本身）钉在 `application/core/goal/escape_test.go`，那一条未动。
// 这里原本钉的是**接线**：环的记账判定逃生之后，coordinator 必须真的把 goal 收口，
// 并且把归档行经 tl 角色 draft 通道落下去（role_name=tl、kind=goal_archive）。
//
// **2026-10-12（J-1a）之后这条接线不可达**，因此本文件只留一条守卫把它记下来：
//
//   - 环的唯一来路是会话团队装配面（`MaterializeAgentTeam` → `teamRuntimeFor` →
//     `teamRuntimes.put`），装配面已随 Agent Team 用户面整条退场；
//   - 于是 `teamRuntimeBySession` 恒 nil ⇒ goal 的 `TeamRuntimeFor` 恒 nil ⇒
//     `NoteTurn`（唯一的生产逃生记账入口）没有调用者；
//   - 相应地，`goalTLRecorder` 的角色行写入也在"本会话已装配团队"这一读面处返回
//     （J-1a 之后该读面恒报未装配，见 `sessionAgentTeamViewJ1A`）。
//
// 这三条一起构成"不可达但无害"：goal 治理自身的收敛（终态 gate / 审批预筛里的真实
// TL 回合）不依赖环，本条目没有改任何 goal 逻辑。

import (
	"testing"
)

// TestRingEscapeWiringIsUnreachableAfterJ1A 钉住"环逃生 → goal 收口"这条应用层接线
// 在 J-1a 之后**没有来路**：任何会话都拿不到环。
//
// 为什么值得一条用例：这正是本条目退场的东西（用户拍板删掉 Agent Team 用户面）。
// 没有它，下一个人只看到 `goal_coordinator.go` 里那个 `TeamRuntimeFor` 读面还在，
// 就会以为环逃生仍是活的接线，于是再去"修"一条永远不触发的路径。
func TestRingEscapeWiringIsUnreachableAfterJ1A(t *testing.T) {
	service := newTestService(t, &fakeEngine{})

	for _, sessionID := range []string{"sess-ring-escape", "sess-any", ""} {
		if ring := service.teamRuntimeBySession(sessionID); ring != nil {
			t.Fatalf("会话 %q 不该有环：会话团队装配面已退场（J-1a）", sessionID)
		}
	}
}
