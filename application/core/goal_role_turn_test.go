package core

// goal_role_turn_test.go — 员工执行面（RoleTurnRunner）的契约。
//
// 背景：试水形态里 agent 角色只在环里占发言位（没有执行面，也不占治理座位）。
// V 模型团队循环（pm → exec → test case）要的是**员工真的干活**：每个角色座位
// 的一轮 = 一次该角色带工具的回合。这里把这条路的契约钉住：
//   - 请求必须带齐角色身份（角色会话 + 权责口径 + 链上位次），执行面才可能
//     在角色会话上按权责跑回合；
//   - 结论必须回到面板（Note），且"没跑起来 / 跑了没产出"必须可见；
//   - 执行面报错要向上抛（不能吞成"无产出"，否则环会把它记成无进展并逃生）；
//   - 一座一轮：座位数 > 2 时，一次 AdvanceAfterChat 必须走完一整轮。

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
	"github.com/RedHuang-0622/seelex/application/core/govern"
)

// stubRoleTurnRunner 是员工执行面桩：记录收到的请求，按脚本返回结论。
type stubRoleTurnRunner struct {
	mu        sync.Mutex
	requests  []RoleTurnRequest
	outcome   RoleTurnOutcome
	err       error
	callCount int
}

func (runner *stubRoleTurnRunner) RunRoleTurn(_ context.Context, request RoleTurnRequest) (RoleTurnOutcome, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.requests = append(runner.requests, request)
	runner.callCount++
	if runner.err != nil {
		return RoleTurnOutcome{}, runner.err
	}
	return runner.outcome, nil
}

func (runner *stubRoleTurnRunner) recorded() []RoleTurnRequest {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return append([]RoleTurnRequest(nil), runner.requests...)
}

// TestRoleTurnSeatPassesRoleIdentityToExecutionFace：座位必须把"在哪个角色会话上、
// 以什么权责、链上第几位"交给执行面——少了这些，执行面只能按主会话跑，员工权责
// （readonly 的 pm 不该写盘）就无从落地。
func TestRoleTurnSeatPassesRoleIdentityToExecutionFace(t *testing.T) {
	runner := &stubRoleTurnRunner{outcome: RoleTurnOutcome{Ran: true, Progress: true, Note: "拆出 3 个子任务"}}
	seat := newRoleTurnSeat(RoleSeat{
		RoleName:      "pm",
		RoleKind:      dto.RoleKindAgent,
		RoleSessionID: "goal-a2a-pm",
		ToolsPolicy:   dto.ToolPolicyReadonly,
		// 逐格装配的权限也必须在座位 → 请求这条路上透传：丢字段等于丢权限
		// （执行体按请求分配主体条目，少一份就等于"装配了但没生效"）。
		PermissionGroups: map[string]uint8{
			dto.PermissionGroupRO:        dto.PermissionBitRead,
			dto.PermissionGroupRW:        0,
			dto.PermissionGroupRWDesktop: dto.PermissionBitRead,
		},
	}, 2, "sess-v", runner)

	if seat.Name() != "pm" {
		t.Fatalf("座位名 = %q", seat.Name())
	}
	if seat.Kind() != govern.AgentKindExec {
		t.Fatalf("员工座位是「做工的座位」（AgentKindExec），得到 %v", seat.Kind())
	}
	action, err := seat.Act(context.Background())
	if err != nil {
		t.Fatalf("act: %v", err)
	}
	if action.BreakLoop {
		t.Fatal("员工回合不该自己断环（收口是评审者的裁决）")
	}
	if action.Note != "拆出 3 个子任务" {
		t.Fatalf("Note = %q", action.Note)
	}
	requests := runner.recorded()
	if len(requests) != 1 {
		t.Fatalf("执行面调用次数 = %d", len(requests))
	}
	got := requests[0]
	want := RoleTurnRequest{
		SessionID:     "sess-v",
		RoleName:      "pm",
		RoleSessionID: "goal-a2a-pm",
		ToolsPolicy:   dto.ToolPolicyReadonly,
		OrderIndex:    2,
		PermissionGroups: map[string]uint8{
			dto.PermissionGroupRO:        dto.PermissionBitRead,
			dto.PermissionGroupRW:        0,
			dto.PermissionGroupRWDesktop: dto.PermissionBitRead,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("执行面收到的请求 = %+v, want %+v", got, want)
	}
}

// TestRoleTurnNoteSurfacesSilentAndEmptyRounds：座位一直空着不能被误读成"员工干完了"。
func TestRoleTurnNoteSurfacesSilentAndEmptyRounds(t *testing.T) {
	cases := []struct {
		name    string
		outcome RoleTurnOutcome
		want    string
	}{
		{"正常产出", RoleTurnOutcome{Ran: true, Progress: true, Note: "补齐负路径"}, "补齐负路径"},
		{"跑了但无产出", RoleTurnOutcome{Ran: true, Progress: false, Note: "读了任务卡"}, "读了任务卡（本轮无产出）"},
		{"没跑起来", RoleTurnOutcome{Ran: false}, "（员工未跑：无输入或被权限拦下）"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := roleTurnNote(item.outcome); got != item.want {
				t.Fatalf("note = %q, want %q", got, item.want)
			}
		})
	}
}

// TestRoleTurnSeatPropagatesExecutionError：执行面出错必须向上抛。吞成"没产出"会让
// 治理循环按无进展逃生，把真正的故障（模型/权限/存储）掩盖成"团队不干活"。
func TestRoleTurnSeatPropagatesExecutionError(t *testing.T) {
	sentinel := errors.New("执行面炸了")
	seat := newRoleTurnSeat(RoleSeat{RoleName: "exec", RoleKind: dto.RoleKindAgent}, 0, "sess-v", &stubRoleTurnRunner{err: sentinel})
	if _, err := seat.Act(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("执行面错误必须向上抛，得到 %v", err)
	}
}

// TestAdvanceAfterChatCompletesRoundWithExecutionSeats 是"员工执行面接入后环仍能走完
// 一轮"的端到端钉住：4 座团队（pm / exec / test_case / advisor）下，一次
// AdvanceAfterChat 必须让 Round 递增，且 pm 与 test_case 的执行面各被调用一次。
//
// 修复前：推进上限写死 2 次 Next()，4 座团队每回合只走前两座 → Round 永不递增
// （轮次上限形同虚设、评审者永远轮不到、执行面只在第一回合跑过）。
func TestAdvanceAfterChatCompletesRoundWithExecutionSeats(t *testing.T) {
	runner := &stubRoleTurnRunner{outcome: RoleTurnOutcome{Ran: true, Progress: true, Note: "员工做过活"}}
	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		Evaluator: &stubTLEvaluator{directives: []goaldomain.TLDirective{{Kind: goaldomain.DirectiveCheckpointOK, Content: "继续"}}},
		RoleSeatsFor: func(string) []RoleSeat {
			return []RoleSeat{
				{RoleName: "user", RoleKind: dto.RoleKindUser},
				{RoleName: "pm", RoleKind: dto.RoleKindAgent, RoleSessionID: "goal-a2a-pm", ToolsPolicy: dto.ToolPolicyReadonly},
				{RoleName: "main", RoleKind: dto.RoleKindMain},
				{RoleName: "test_case", RoleKind: dto.RoleKindAgent, RoleSessionID: "goal-a2a-test_case", ToolsPolicy: dto.ToolPolicyReadWrite},
				{RoleName: "tl", RoleKind: dto.RoleKindTechlead},
			}
		},
		RoleTurnFor: func(string) RoleTurnRunner { return runner },
	})
	if _, err := coordinator.Begin(context.Background(), "sess-v", goaldomain.BeginRequest{Title: "走 V 模型一轮"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := coordinator.AdvanceAfterChat(context.Background(), "sess-v", "本轮工作正文"); err != nil {
		t.Fatalf("advance after chat: %v", err)
	}
	view := coordinator.GoalGovernanceViewFor("sess-v")
	if view == nil || view.Round < 1 {
		t.Fatalf("4 座团队必须能在一次推进里走完一轮（Round≥1）: %+v", view)
	}
	requests := runner.recorded()
	if len(requests) != 2 {
		t.Fatalf("pm 与 test_case 的执行面各应被调用一次，得到 %d 次: %+v", len(requests), requests)
	}
	if requests[0].RoleName != "pm" || requests[1].RoleName != "test_case" {
		t.Fatalf("员工回合须按链序（pm → test_case）: %+v", requests)
	}

	// 第二轮：执行面继续被调用（每回合都真的干活，而不是只在第一回合跑过）。
	if err := coordinator.AdvanceAfterChat(context.Background(), "sess-v", "第二轮工作正文"); err != nil {
		t.Fatalf("second advance: %v", err)
	}
	if got := len(runner.recorded()); got != 4 {
		t.Fatalf("第二轮后执行面调用次数 = %d, want 4", got)
	}
	if second := coordinator.GoalGovernanceViewFor("sess-v"); second == nil || second.Round < 2 {
		t.Fatalf("第二轮 Round 应 ≥2: %+v", second)
	}
}

// TestAdvanceAfterChatWithoutExecutionFaceKeepsPilotShape 是试水形态的回归钉：
// 未装配执行面时 agent 角色不占座位，环仍是 EXEC + ADVISOR 两座，一轮即收。
func TestAdvanceAfterChatWithoutExecutionFaceKeepsPilotShape(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		Evaluator: &stubTLEvaluator{},
		RoleSeatsFor: func(string) []RoleSeat {
			return []RoleSeat{
				{RoleName: "user", RoleKind: dto.RoleKindUser},
				{RoleName: "pm", RoleKind: dto.RoleKindAgent, RoleSessionID: "goal-a2a-pm"},
				{RoleName: "main", RoleKind: dto.RoleKindMain},
				{RoleName: "tl", RoleKind: dto.RoleKindTechlead},
			}
		},
	})
	if _, err := coordinator.Begin(context.Background(), "sess-pilot", goaldomain.BeginRequest{Title: "试水形态"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := coordinator.AdvanceAfterChat(context.Background(), "sess-pilot", "本轮工作正文"); err != nil {
		t.Fatalf("advance after chat: %v", err)
	}
	if view := coordinator.GoalGovernanceViewFor("sess-pilot"); view == nil || view.Round < 1 {
		t.Fatalf("试水形态必须仍然走得完一轮: %+v", view)
	}
}

// TestRoleTurnNoteTrimmed 保证空 Note 不产生悬空空格（面板拼接用）。
func TestRoleTurnNoteTrimmed(t *testing.T) {
	if got := roleTurnNote(RoleTurnOutcome{Ran: true, Progress: true, Note: "   "}); got != "" {
		t.Fatalf("空摘要 = %q, want 空", got)
	}
	if got := roleTurnNote(RoleTurnOutcome{Ran: false, Note: " "}); !strings.HasPrefix(got, "（") {
		t.Fatalf("未跑起来时摘要应带提示，得到 %q", got)
	}
}
