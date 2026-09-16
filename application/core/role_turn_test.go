package core

// role_turn_test.go — 员工执行面（RoleTurnRunner）的**接线契约**。
//
// application 侧只做两件事：把座位请求翻成跨层 DTO（role_turn.go），以及把
// 本轮工作正文经 ctx 交给座位（goal_coordinator.go）。执行体本身（角色会话 +
// 工具面 + 权责）的验收在 seelebridge 侧（runtime_role_turn_test.go）。
//
// 这里钉的是"接线不丢字段"：座位派生的请求字段与 DTO 字段一一对应（丢
// ToolsPolicy 等于员工权责无从落地），以及本轮 detail 真的到得了执行面。

import (
	"context"
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
)

// recordingRoleTurnPort 记录跨层请求并按脚本返回结论。
type recordingRoleTurnPort struct {
	requests []dto.RoleTurnRequest
	outcome  dto.RoleTurnOutcome
	err      error
}

func (port *recordingRoleTurnPort) RunRoleTurn(_ context.Context, request dto.RoleTurnRequest) (dto.RoleTurnOutcome, error) {
	port.requests = append(port.requests, request)
	if port.err != nil {
		return dto.RoleTurnOutcome{}, port.err
	}
	return port.outcome, nil
}

// TestContractRoleTurnRunnerMapsEveryField：座位请求 ↔ 跨层 DTO 必须逐字段对应。
// 少一个字段就是执行面少一份依据（丢 ToolsPolicy = 员工权责无从落地；丢
// RoleSessionID = 回合跑到别的会话上）。
func TestContractRoleTurnRunnerMapsEveryField(t *testing.T) {
	port := &recordingRoleTurnPort{outcome: dto.RoleTurnOutcome{Ran: true, Progress: true, Note: "拆出 3 个子任务"}}
	runner := contractRoleTurnRunner{port: port}
	outcome, err := runner.RunRoleTurn(context.Background(), RoleTurnRequest{
		SessionID:     "sess-v",
		RoleName:      "pm",
		RoleSessionID: "goal-a2a-pm",
		ToolsPolicy:   dto.ToolPolicyReadonly,
		OrderIndex:    2,
		PermissionGroups: map[string]uint8{
			dto.PermissionGroupRO: dto.PermissionBitRead,
			dto.PermissionGroupRW: dto.PermissionBitRead | dto.PermissionBitWrite,
		},
		Input: "本轮工作正文",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(port.requests) != 1 {
		t.Fatalf("跨层请求数 = %d", len(port.requests))
	}
	want := dto.RoleTurnRequest{
		SessionID:     "sess-v",
		RoleName:      "pm",
		RoleSessionID: "goal-a2a-pm",
		ToolsPolicy:   dto.ToolPolicyReadonly,
		OrderIndex:    2,
		PermissionGroups: map[string]uint8{
			dto.PermissionGroupRO: dto.PermissionBitRead,
			dto.PermissionGroupRW: dto.PermissionBitRead | dto.PermissionBitWrite,
		},
		Input: "本轮工作正文",
	}
	if !reflect.DeepEqual(port.requests[0], want) {
		t.Fatalf("跨层请求 = %+v, want %+v", port.requests[0], want)
	}
	if outcome != (RoleTurnOutcome{Ran: true, Progress: true, Note: "拆出 3 个子任务"}) {
		t.Fatalf("回执 = %+v", outcome)
	}
}

// TestRoleTurnSeatTakesRoundInputFromContext：座位在装配期构造、不持有"这一轮发生
// 了什么"，本轮 detail 必须经 ctx 到达执行面——否则员工回合拿到空输入，等于用一次
// 空 prompt 烧一次模型调用。
func TestRoleTurnSeatTakesRoundInputFromContext(t *testing.T) {
	runner := &stubRoleTurnRunner{outcome: RoleTurnOutcome{Ran: true, Progress: true, Note: "做了活"}}
	seat := newRoleTurnSeat(RoleSeat{
		RoleName: "pm", RoleKind: dto.RoleKindAgent, RoleSessionID: "goal-a2a-pm", ToolsPolicy: dto.ToolPolicyReadonly,
	}, 1, "sess-v", runner)

	if _, err := seat.Act(withRoleTurnInput(context.Background(), "拆解目标")); err != nil {
		t.Fatalf("act: %v", err)
	}
	requests := runner.recorded()
	if len(requests) != 1 {
		t.Fatalf("执行面调用次数 = %d", len(requests))
	}
	if requests[0].Input != "拆解目标" {
		t.Fatalf("员工回合应拿到本轮工作正文，得到 %q", requests[0].Input)
	}
}

// TestRoleTurnSeatKeepsExplicitInput：请求里已显式给的输入优先于 ctx（显式选择
// 不被环境覆盖）。
func TestRoleTurnSeatKeepsExplicitInput(t *testing.T) {
	runner := &stubRoleTurnRunner{outcome: RoleTurnOutcome{Ran: true, Progress: true}}
	seat := newRoleTurnSeat(RoleSeat{RoleName: "pm", RoleKind: dto.RoleKindAgent}, 0, "sess-v", runner)
	seat.request.Input = "显式输入"

	if _, err := seat.Act(withRoleTurnInput(context.Background(), "ctx 输入")); err != nil {
		t.Fatalf("act: %v", err)
	}
	if got := runner.recorded()[0].Input; got != "显式输入" {
		t.Fatalf("显式输入应优先，得到 %q", got)
	}
}

// TestRoleTurnRunnerRequiresPort：未装配端口 = 试水形态（agent 角色只占发言位、
// 不占治理座位）。执行面在这里返回 nil 是**唯一**让座位派生产生"没有执行面"结论
// 的入口——它一旦悄悄返回一个空壳 runner，环就会每轮空转等一个不会发生的回合。
func TestRoleTurnRunnerRequiresPort(t *testing.T) {
	service := &Service{serviceState: &serviceState{Core: state.New(contract.Dependencies{})}}
	if runner := service.roleTurnRunnerFor("sess-v"); runner != nil {
		t.Fatalf("未装配 RoleTurn 端口时执行面应为 nil，得到 %+v", runner)
	}
	service.Deps.RoleTurn = &recordingRoleTurnPort{}
	if runner := service.roleTurnRunnerFor("sess-v"); runner == nil {
		t.Fatal("装配了 RoleTurn 端口后执行面必须可用")
	}
}
