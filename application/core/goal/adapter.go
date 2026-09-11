package goal

// adapter.go — 把既有 DS-A2A 目标域实现适配到治理循环抽象
// （application/core/govern）。治理抽象与协议语义解耦：本文件只负责
// 把 goal 域的 Controller/Supervisor/TLDirective 翻译成 govern.Seat 的
// 具体动作；治理循环本身（座次/轮次/断环）由 govern 包提供。
// 目标：不让治理抽象成为"纸面接口"——现有 Supervisor/TLDirective/Controller
// 语义直接作为桌游座位的具体动作，供 headless/装配方以同一循环驱动。

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/core/govern"
)

// DirectiveBreaksLoop 报告一条 TLDirective 是否应打破治理循环。
// 终态/升级/审批代答是"停止本轮治理"的强信号；中间纠偏/打点通过
// 只让位（不打断），让 EXEC 继续执行后再进入下一轮。
func DirectiveBreaksLoop(directive TLDirective) bool {
	switch directive.Kind {
	case DirectiveVerdictDone, DirectiveEscalateHuman, DirectiveApprove, DirectiveDeny:
		return true
	default:
		return false
	}
}

// advisorSeat 是 Advisor(b) 的治理座位：每次 Act 触发一次真实 TL 回合
// （等价 headless goal_tl_eval），并把裁决语义翻译为治理循环动作。
type advisorSeat struct {
	supervisor *Supervisor
	name       string
	trigger    string
}

// NewAdvisorSeat 构造 Advisor 座位。supervisor 为 nil 或未启用时，Act
// 返回 ErrTLDisabled（由治理循环透传，调用方按缺席矩阵处理）。
// name 缺省为 "advisor-b"。
func NewAdvisorSeat(supervisor *Supervisor, name string) govern.Seat {
	if name == "" {
		name = "advisor-b"
	}
	trigger := "govern:" + name
	return &advisorSeat{supervisor: supervisor, name: name, trigger: trigger}
}

func (s *advisorSeat) Name() string { return s.name }
func (s *advisorSeat) Kind() govern.AgentKind {
	return govern.AgentKindAdvisor
}

func (s *advisorSeat) Act(ctx context.Context) (govern.TurnAction, error) {
	if s.supervisor == nil {
		return govern.TurnAction{}, ErrTLDisabled
	}
	directive, err := s.supervisor.RunEval(ctx, s.trigger)
	if err != nil {
		return govern.TurnAction{}, err
	}
	return govern.TurnAction{
		BreakLoop: DirectiveBreaksLoop(directive),
		Note:      directive.Summary(),
	}, nil
}

// NewTurnGovernorForDSA2A 装配"EXEC + ADVISOR"两座位的治理循环：
//   - EXEC 座位由 execAct 注入（外部驱动执行动作：投信号/登记进度等）；
//   - ADVISOR 座位包装 supervisor（真实 TL 回合）。
//   - maxRounds≤0 表示不设轮次上限（由裁决/外部 Break 收束）。
//
// execAct 返回的 TurnAction.BreakLoop=true 时，治理循环在 EXEC 侧即收束
// （例如 EXEC 声明"无可推进"）；否则让位给 ADVISOR 评审。
func NewTurnGovernorForDSA2A(
	execName string,
	execAct func(context.Context) (govern.TurnAction, error),
	supervisor *Supervisor,
	maxRounds int,
) govern.Governor {
	exec := funcSeat{name: execName, kind: govern.AgentKindExec, act: execAct}
	return govern.NewTurnGovernor([]govern.Seat{exec, NewAdvisorSeat(supervisor, "")}, maxRounds)
}

// funcSeat 把闭包包装成 Seat（EXEC 侧常用形态：由装配方注入执行动作）。
type funcSeat struct {
	name string
	kind govern.AgentKind
	act  func(context.Context) (govern.TurnAction, error)
}

func (f funcSeat) Name() string           { return f.name }
func (f funcSeat) Kind() govern.AgentKind { return f.kind }
func (f funcSeat) Act(ctx context.Context) (govern.TurnAction, error) {
	if f.act == nil {
		return govern.TurnAction{}, nil
	}
	return f.act(ctx)
}
