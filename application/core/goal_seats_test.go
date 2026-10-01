package core

// goal_seats_test.go — 治理座位必须按**角色 kind** 派生，不能按角色名字面量匹配。
//
// 修复前的行为：`newGovernor` 用 `roleName == "main"` / `roleName == "tl"` 决定谁有
// 座位。TL 角色一旦改名（用户改名，或自定义预设用了别的名字），ADVISOR 座位直接
// 消失——治理循环只剩 EXEC 一座，ADVISOR 被噤声且不报错（只是"永远不评估"）。
// 这里把"按 kind 派生"钉成契约。

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
	"github.com/RedHuang-0622/seelex/application/core/govern"
)

func seatExecAct(context.Context) (govern.TurnAction, error) { return govern.TurnAction{}, nil }

func seatKindsOf(seats []govern.Seat) []govern.AgentKind {
	out := make([]govern.AgentKind, 0, len(seats))
	for _, seat := range seats {
		out = append(out, seat.Kind())
	}
	return out
}

func seatNamesOf(seats []govern.Seat) []string {
	out := make([]string, 0, len(seats))
	for _, seat := range seats {
		out = append(out, seat.Name())
	}
	return out
}

func equalSeatKinds(left, right []govern.AgentKind) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// TestSeatPlanFollowsRoleKindNotRoleName：座位由 kind 决定，与角色名无关。
func TestSeatPlanFollowsRoleKindNotRoleName(t *testing.T) {
	cases := []struct {
		name  string
		seats []RoleSeat
		want  []govern.AgentKind
	}{
		{
			name: "内置 main + tl",
			seats: []RoleSeat{
				{RoleName: "main", RoleKind: dto.RoleKindMain},
				{RoleName: "tl", RoleKind: dto.RoleKindTechlead},
			},
			want: []govern.AgentKind{govern.AgentKindExec, govern.AgentKindAdvisor},
		},
		{
			name: "TL 改名（自定义预设）：仍须有 ADVISOR 座位",
			seats: []RoleSeat{
				{RoleName: "main", RoleKind: dto.RoleKindMain},
				{RoleName: "architect", RoleKind: dto.RoleKindTechlead},
			},
			want: []govern.AgentKind{govern.AgentKindExec, govern.AgentKindAdvisor},
		},
		{
			name: "改名 + 链序倒置：座位存在性与顺序无关",
			seats: []RoleSeat{
				{RoleName: "architect", RoleKind: dto.RoleKindTechlead},
				{RoleName: "main", RoleKind: dto.RoleKindMain},
			},
			want: []govern.AgentKind{govern.AgentKindAdvisor, govern.AgentKindExec},
		},
		{
			name: "没有 techlead 角色：只有 EXEC 座位（不凭空长出 ADVISOR）",
			seats: []RoleSeat{
				{RoleName: "main", RoleKind: dto.RoleKindMain},
			},
			want: []govern.AgentKind{govern.AgentKindExec},
		},
		{
			name: "agent 角色不占治理座位（员工走 leader 派发的 worker 作业）",
			seats: []RoleSeat{
				{RoleName: "main", RoleKind: dto.RoleKindMain},
				{RoleName: "tl", RoleKind: dto.RoleKindTechlead},
				{RoleName: "reviewer", RoleKind: dto.RoleKindAgent},
			},
			want: []govern.AgentKind{govern.AgentKindExec, govern.AgentKindAdvisor},
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			seats := seatPlan{Seats: item.seats, ExecAct: seatExecAct}.seats()
			got := seatKindsOf(seats)
			if !equalSeatKinds(got, item.want) {
				t.Fatalf("座位 kinds = %v, want %v", got, item.want)
			}
		})
	}
}

// TestSeatPlanGivesNoSeatToAgentRoles：员工角色**不再**占治理座位——员工干活由 leader
// 派 worker 作业（team_dispatch → jobs.KindWorker），治理环只为 main 派生 EXEC 让位座、
// 为 techlead 派生 ADVISOR 评审座（2026-10-01 M4：席位制轮回的退场点）。名单里
// agent / user 角色再多也不改变座位集合，链序也不受影响。
func TestSeatPlanGivesNoSeatToAgentRoles(t *testing.T) {
	plan := seatPlan{
		Seats: []RoleSeat{
			{RoleName: "user", RoleKind: dto.RoleKindUser},
			{RoleName: "pm", RoleKind: dto.RoleKindAgent},
			{RoleName: "main", RoleKind: dto.RoleKindMain},
			{RoleName: "test_case", RoleKind: dto.RoleKindAgent},
			{RoleName: "tl", RoleKind: dto.RoleKindTechlead},
		},
		ExecAct: seatExecAct,
	}
	seats := plan.seats()
	// EXEC 让位座 + ADVISOR 评审座；员工角色一座都不占。
	want := []string{"exec-a", "advisor-b"}
	if got := seatNamesOf(seats); !equalStrings(got, want) {
		t.Fatalf("座位名 = %v, want %v", got, want)
	}
	wantKinds := []govern.AgentKind{govern.AgentKindExec, govern.AgentKindAdvisor}
	if got := seatKindsOf(seats); !equalSeatKinds(got, wantKinds) {
		t.Fatalf("座位 kinds = %v, want %v", got, wantKinds)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// TestCoordinatorSeatsFromRegistryKinds：协调器在有注册表读面时必须走 kind 派生，
// 即使链表顺序读不到（TeamRuntimeFor = nil）也不会掉回名字匹配。
func TestCoordinatorSeatsFromRegistryKinds(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		RoleSeatsFor: func(string) []RoleSeat {
			return []RoleSeat{
				{RoleName: "main", RoleKind: dto.RoleKindMain},
				{RoleName: "architect", RoleKind: dto.RoleKindTechlead},
			}
		},
	})
	seats := coordinator.seatsFor("sess-seats", nil, seatExecAct)
	want := []govern.AgentKind{govern.AgentKindExec, govern.AgentKindAdvisor}
	if got := seatKindsOf(seats); !equalSeatKinds(got, want) {
		t.Fatalf("座位 kinds = %v, want %v（改名不该丢 ADVISOR 座位）", got, want)
	}
}

// TestSeatsFallBackToOrderNamesWithoutRegistry：没有注册表读面时退回老路径（按
// 链表顺序 + 角色名匹配），保证老宿主/桩不因新增读面而失去座位。
func TestSeatsFallBackToOrderNamesWithoutRegistry(t *testing.T) {
	seats := seatsFromOrder([]string{"main", "tl"}, nil, seatExecAct)
	want := []govern.AgentKind{govern.AgentKindExec, govern.AgentKindAdvisor}
	if got := seatKindsOf(seats); !equalSeatKinds(got, want) {
		t.Fatalf("退化路径座位 kinds = %v, want %v", got, want)
	}
	// 空链表不该凭空长出座位（调用方据此退到内置双座位）。
	if got := seatKindsOf(seatsFromOrder(nil, nil, seatExecAct)); len(got) != 0 {
		t.Fatalf("空链表不应长出座位，得到 %v", got)
	}
}

// TestCoordinatorSeatsFallBackWhenRegistryUnavailable：没有任何读面时不长座位
// （上层退到内置 EXEC+ADVISOR 双座位，保持"没有团队也在治理"的旧行为）。
func TestCoordinatorSeatsFallBackWhenRegistryUnavailable(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		RoleSeatsFor:   func(string) []RoleSeat { return nil },
		TeamRuntimeFor: nil,
	})
	if seats := coordinator.seatsFor("sess-none", nil, seatExecAct); len(seats) != 0 {
		t.Fatalf("没有任何读面时不应长出座位，得到 %d 个", len(seats))
	}
}

// TestTeamRoleSeatsFromRegistryView 钉住装配层的座位来源：角色按发言链顺序
// （lifecycle.order_roles 是唯一顺序事实）给出，且带 role_kind。座位只回答
// "谁在链上、是什么 kind"——员工作业要的角色会话/权责由 leader 派发时的计划给，
// 不再经座位透传（2026-10-01 M4）。
func TestTeamRoleSeatsFromRegistryView(t *testing.T) {
	sessions := &teamRecordingSessions{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	sessionID := "sess-seat-source"
	if _, err := service.GoalBeginFor(context.Background(), sessionID, goaldomain.BeginRequest{Title: "座位来源"}); err != nil {
		t.Skipf("goal 装配不可用于该夹具: %v", err)
	}
	specs := service.teamRoleSeatsFor(sessionID)
	if len(specs) == 0 {
		t.Skip("该夹具未物化团队注册表（无座位来源可读）")
	}
	kinds := make(map[dto.RoleKind]bool, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.RoleName) == "" {
			t.Fatalf("座位来源必须带角色名：%+v", specs)
		}
		kinds[spec.RoleKind] = true
	}
	if !kinds[dto.RoleKindMain] || !kinds[dto.RoleKindTechlead] {
		t.Fatalf("座位来源必须包含 main 与 techlead 两种 kind，得到 %+v", specs)
	}

	// 顺序必须与 order_roles 一致（夹具顺序读走加锁入口：读写两侧同一把锁）。
	order := sessions.orderSnapshot()
	if len(order) == len(specs) {
		for index := range order {
			if specs[index].RoleName != order[index] {
				t.Fatalf("座位来源顺序 = %+v, want %v（lifecycle 顺序是唯一顺序事实）", specs, order)
			}
		}
	}
}
