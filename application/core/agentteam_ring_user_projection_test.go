package core

// agentteam_ring_user_projection_test.go — 端到端守卫：「顺序事实」与「发言环」是
// 两个概念，不能混。
//
// 产品口径（2026-09-17）：user 永远在 order_roles 里（它是群聊的起手与收口），但
// **不在发言环里**——它的发言机会是回合尾消息队列被整批提升为下一轮（chat 轮次），
// 不是一个排班位。若环里还留着 user，「队友的下一个」就会指向人，面板与巡检面都会
// 读出错误的排班。
//
// 断言方式是**相对**的（环 = 顺序 − user），不依赖某个 preset 的具体成员：装配事实
// 由 view.OrderRoles 给出、环投影由 schedule.Order 给出，两者必须只差 user。

import (
	"context"
	"strings"
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

func TestTeamScheduleOrderDropsUserFromOrderRoles(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, withTestSessions(&teamRecordingSessions{}))
	sessionID := "sess-ring-user-projection"
	ctx := withSessionID(context.Background(), sessionID)
	if _, err := service.GoalBeginFor(ctx, sessionID, goaldomain.BeginRequest{Title: "环投影"}); err != nil {
		t.Skipf("goal 装配不可用于该夹具: %v", err)
	}
	view, err := service.AgentTeamView(sessionID)
	if err != nil {
		t.Fatalf("AgentTeamView: %v", err)
	}
	if !hasRoleName(view.OrderRoles, "user") {
		t.Fatalf("顺序事实仍应含 user（起手与收口）：%v", view.OrderRoles)
	}
	schedule := service.teamScheduleFor(sessionID)
	if schedule == nil {
		t.Fatal("该会话应有发言调度投影（团队环）")
	}
	want := make([]string, 0, len(view.OrderRoles))
	for _, name := range view.OrderRoles {
		if name != "user" {
			want = append(want, name)
		}
	}
	if got := strings.Join(schedule.Order, ","); got != strings.Join(want, ",") {
		t.Fatalf("环成员 = %q，want %q（= order_roles − user）", got, strings.Join(want, ","))
	}
	if hasRoleName(schedule.Order, "user") {
		t.Fatalf("环里不该有 user：%v", schedule.Order)
	}
	if schedule.NextRole == "user" {
		t.Fatal("「下一个发言」不该指向 user")
	}
}

// hasRoleName 报告名单里有没有某个角色名（测试内的最小集合判据）。
func hasRoleName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
