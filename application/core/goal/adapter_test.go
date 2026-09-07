package goal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/govern"
)

// TestGovernorDrivesAdvisorRound 验证治理循环能驱动真实 TL 回合：
// EXEC 行动（登记信号）→ ADVISOR 座位触发 RunEval → stub 裁决 correct
// （不打断）→ 下一轮；快照反映轮次推进与座位次序。
func TestGovernorDrivesAdvisorRound(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	stub := newStubEvaluator(TLDirective{Kind: DirectiveCorrect, Content: "先补测试再继续"})
	sup := NewSupervisor(ctl, stub, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "发布 v1"}); err != nil {
		t.Fatalf("begin: %v", err)
	}

	var turns int
	execAct := func(ctx context.Context) (govern.TurnAction, error) {
		turns++
		// EXEC 推进一个里程碑（checkpoint 信号 → b 自动回合）。
		if err := sup.Notify(ctx, TLEvalSignal{Kind: SignalStepCheckpoint, Source: "n1"}); err != nil {
			return govern.TurnAction{}, err
		}
		return govern.TurnAction{Note: "exec advance"}, nil
	}

	g := NewTurnGovernorForDSA2A("exec-a", execAct, sup, 0)
	ctx := context.Background()
	// 第 1 轮：exec（notify 触发自动回合）→ advisor（RunEval 再补一回合）。
	for i := 0; i < 4; i++ {
		more, err := g.Next(ctx)
		if err != nil {
			t.Fatalf("Next#%d: %v", i, err)
		}
		if !more {
			break
		}
	}
	snap := sup.Snapshot()
	if snap.RoundCount < 2 {
		t.Fatalf("治理循环应驱动至少 2 次 TL 回合, 得 %d", snap.RoundCount)
	}
	if turns != 2 {
		t.Fatalf("EXEC 应行动 2 轮, 得 %d", turns)
	}
	if g.Round() != 2 {
		t.Fatalf("治理循环应推进到第 2 轮, 得 %d", g.Round())
	}
}

// TestGovernorBreaksOnVerdictDone 验证完整收口闭环：
// EXEC 多次推进 → ADVISOR verdict_done → 治理循环断环、goal completed。
func TestGovernorBreaksOnVerdictDone(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	replies := []TLDirective{
		{Kind: DirectiveVerdictNotDone, Content: "缺负路径单测"},
		{Kind: DirectiveVerdictDone, Content: "全绿收口"},
	}
	stub := newStubEvaluator(replies...)
	sup := NewSupervisor(ctl, stub, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	if _, err := ctl.Begin(testCtx, BeginRequest{
		Title: "发布 v1", Acceptance: []string{"go test 全绿"},
	}); err != nil {
		t.Fatalf("begin: %v", err)
	}

	proposals := 0
	execAct := func(ctx context.Context) (govern.TurnAction, error) {
		if proposals == 0 {
			proposals++
			if _, err := ctl.Update(ctx, UpdateRequest{
				ProgressKind: ProgressMilestone, ProgressContent: "已补负路径单测",
			}); err != nil {
				return govern.TurnAction{}, err
			}
			return govern.TurnAction{Note: "补测完成"}, nil
		}
		proposals++
		// EXEC 经终态 gate 提议收口：gate 内部触发 TL 回合并裁决。
		result, err := sup.ProposeFinish(ctx, FinishRequest{Result: "已补负路径单测，go test 全绿"})
		if err != nil {
			return govern.TurnAction{}, err
		}
		if result.Outcome != OutcomeCompleted {
			return govern.TurnAction{}, errors.New("gate 应裁决 completed")
		}
		return govern.TurnAction{BreakLoop: true, Note: "TL verdict_done → 收口出栈"}, nil
	}

	g := NewTurnGovernorForDSA2A("exec-a", execAct, sup, 0)
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		more, err := g.Next(ctx)
		if err != nil {
			t.Fatalf("Next#%d: %v", i, err)
		}
		if !more {
			break
		}
	}
	broken, reason := g.Broken()
	if !broken {
		t.Fatalf("verdict_done 后治理循环应断环: round=%d", g.Round())
	}
	// TL 收口出栈：goal completed 不再 active。
	active, ok := ctl.ActiveGoal()
	if ok {
		t.Fatalf("verdict_done 后 goal 应已出栈, 仍 active: %+v", active)
	}
	if history := ctl.History(); len(history) != 1 || history[0].Status != StatusCompleted {
		t.Fatalf("历史应含 1 条 completed 记录: %+v", history)
	}
	if !strings.Contains(reason, "verdict_done") && reason == "" {
		t.Fatalf("断环原因应记录 TL 裁决: %q", reason)
	}
}

// TestAdvisorSeatDisabledReportsTLDisabled 验证无评估器（TL 缺席）时
// 治理循环透传 ErrTLDisabled——B4 缺席由装配方决定，不静默。
func TestAdvisorSeatDisabledReportsTLDisabled(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup := NewSupervisor(ctl, nil, TechLeaderConfig{Enabled: true})
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	seat := NewAdvisorSeat(sup, "")
	_, err := seat.Act(testCtx)
	if !errors.Is(err, ErrTLDisabled) {
		t.Fatalf("无评估器时 Act 应返回 ErrTLDisabled, 得 %v", err)
	}
}
