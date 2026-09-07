package goal

import (
	"errors"
	"testing"
)

// TestProposeFinishVerdictNotDoneBlocks 验证负向：mainagent 提前 finish，
// TL verdict_not_done → 目标保持 active + 纠偏指令（design §5.6/P2 验收负向）。
func TestProposeFinishVerdictNotDoneBlocks(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, _ := newTestSupervisor(t, ctl, 0,
		TLDirective{Kind: DirectiveVerdictNotDone, Content: "acceptance 未满足：测试未全绿，回 active 补验证", Severity: SeverityP1})
	if _, err := ctl.Begin(testCtx, BeginRequest{
		Title: "发布 v1", Acceptance: []string{"go test 全绿"},
	}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	result, err := sup.ProposeFinish(testCtx, FinishRequest{Result: "做完了"})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if result.Outcome != OutcomeNotDone {
		t.Fatalf("应 verdict_not_done 拦截, 得 %s", result.Outcome)
	}
	if result.Directive == nil || result.Directive.Kind != DirectiveVerdictNotDone {
		t.Fatalf("应携带纠偏指令: %+v", result.Directive)
	}
	// 目标仍在栈上（未弹栈）、指令环记录了纠偏。
	if active, _ := ctl.ActiveGoal(); active == nil || active.Title != "发布 v1" || len(active.Directives) == 0 {
		t.Fatalf("goal 应保持 active 且带纠偏: %+v", active)
	}
	if projection := ctl.Projection(); projection.Active == nil || len(projection.Goals) != 1 {
		t.Fatalf("投影应保持 1 个 active: %+v", projection)
	}
}

// TestProposeFinishVerdictDonePops 验证正向：TL verdict_done → completed 弹栈。
func TestProposeFinishVerdictDonePops(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, _ := newTestSupervisor(t, ctl, 0,
		TLDirective{Kind: DirectiveVerdictDone, Content: "acceptance 已满足，收口"})
	if _, err := ctl.Begin(testCtx, BeginRequest{
		Title: "发布 v1", Acceptance: []string{"go test 全绿"},
	}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	result, err := sup.ProposeFinish(testCtx, FinishRequest{Result: "全绿"})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if result.Outcome != OutcomeCompleted || result.Goal == nil || result.Goal.Status != StatusCompleted {
		t.Fatalf("应 completed 收口, 得 %+v", result)
	}
	if projection := ctl.Projection(); projection.Active != nil || len(projection.Goals) != 0 {
		t.Fatalf("收口后投影应为空: %+v", projection)
	}
	if len(ctl.History()) != 1 {
		t.Fatalf("History 审计应 +1: %d", len(ctl.History()))
	}
}

// TestProposeFinishEscalateKeepsActive 验证 escalate_human：保持 active，转人工。
func TestProposeFinishEscalateKeepsActive(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, _ := newTestSupervisor(t, ctl, 0,
		TLDirective{Kind: DirectiveEscalateHuman, Content: "涉及外部发布，需人工确认", Severity: SeverityP0})
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "发布 v1"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	result, err := sup.ProposeFinish(testCtx, FinishRequest{Result: "准备发布"})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if result.Outcome != OutcomeEscalate {
		t.Fatalf("应 escalate_human, 得 %s", result.Outcome)
	}
	if active, _ := ctl.ActiveGoal(); active == nil {
		t.Fatal("escalate 后 goal 应保持 active")
	}
}

// TestProposeFinishNoGoalAndNoTLFallback 验证边界：无 goal → no_goal；TL 未启用 → 直连。
func TestProposeFinishNoGoalAndNoTLFallback(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, _ := newTestSupervisor(t, ctl, 0)
	if result, err := sup.ProposeFinish(testCtx, FinishRequest{}); err != nil || result.Outcome != OutcomeNoGoal {
		t.Fatalf("空栈应 no_goal: %+v err=%v", result, err)
	}

	ctl2 := newTestController(t, DefaultStackDepth)
	sup2 := NewSupervisor(ctl2, nil, TechLeaderConfig{Enabled: true})
	if _, err := ctl2.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	result, err := sup2.ProposeFinish(testCtx, FinishRequest{Result: "收口"})
	if err != nil {
		t.Fatalf("propose(no TL): %v", err)
	}
	if result.Outcome != OutcomeNoTL || result.Goal == nil || result.Goal.Status != StatusCompleted {
		t.Fatalf("未启用 TL 应直连收口, 得 %+v", result)
	}
	if projection := ctl2.Projection(); projection.Active != nil {
		t.Fatalf("直连收口后投影应为空: %+v", projection)
	}
}

// TestProposeFinishRejectsWrongVerdict 验证 gate 只接受终态裁决 kinds。
func TestProposeFinishRejectsWrongVerdict(t *testing.T) {
	ctl := newTestController(t, DefaultStackDepth)
	sup, _ := newTestSupervisor(t, ctl, 0,
		TLDirective{Kind: DirectiveCorrect, Content: "这不是裁决"})
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := sup.ProposeFinish(testCtx, FinishRequest{}); !errors.Is(err, ErrBadDirective) {
		t.Fatalf("非裁决指令应报 ErrBadDirective, 得 %v", err)
	}
}

// TestPreScreenApproval 验证审批预筛（design §5.5）：
// low → TL approve/deny 代答；high → escalate_human（默认人工兜底）。
func TestPreScreenApproval(t *testing.T) {
	approved := newStubEvaluator(TLDirective{Kind: DirectiveApprove, Content: "低风险读操作，代答放行"})
	ctl := newTestController(t, DefaultStackDepth)
	if _, err := ctl.Begin(testCtx, BeginRequest{Title: "目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	sup := NewSupervisor(ctl, approved, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	verdict, err := sup.PreScreenApproval(testCtx, ApprovalScreenRequest{
		Summary: "读取 README 列表", RiskLevel: "low",
	})
	if err != nil {
		t.Fatalf("prescreen: %v", err)
	}
	if verdict.Outcome != ApprovalOutcomeApproved || verdict.Directive == nil || verdict.Directive.Kind != DirectiveApprove {
		t.Fatalf("low 应被 TL approve 代答: %+v", verdict)
	}

	// deny。
	denyStub := newStubEvaluator(TLDirective{Kind: DirectiveDeny, Content: "越界读，拒绝"})
	sup2 := NewSupervisor(ctl, denyStub, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	verdict2, err := sup2.PreScreenApproval(testCtx, ApprovalScreenRequest{
		Summary: "读取敏感文件", RiskLevel: "low",
	})
	if err != nil {
		t.Fatalf("prescreen(deny): %v", err)
	}
	if verdict2.Outcome != ApprovalOutcomeDenied {
		t.Fatalf("应 deny: %+v", verdict2)
	}

	// high 一律转人工，TL 不代答。
	highStub := newStubEvaluator(TLDirective{Kind: DirectiveApprove, Content: "x"})
	sup3 := NewSupervisor(ctl, highStub, TechLeaderConfig{Enabled: true, EvalWindow: 0})
	verdict3, err := sup3.PreScreenApproval(testCtx, ApprovalScreenRequest{
		Summary: "写账号配置", RiskLevel: "high",
	})
	if err != nil {
		t.Fatalf("prescreen(high): %v", err)
	}
	if verdict3.Outcome != ApprovalOutcomeEscalate {
		t.Fatalf("high 应 escalate_human（TL 不放行高危）: %+v", verdict3)
	}

	// TL 未启用 → escalate（默认人工兜底）。
	sup4 := NewSupervisor(ctl, nil, TechLeaderConfig{Enabled: true})
	verdict4, err := sup4.PreScreenApproval(testCtx, ApprovalScreenRequest{
		Summary: "任意", RiskLevel: "low",
	})
	if err != nil {
		t.Fatalf("prescreen(noTL): %v", err)
	}
	if verdict4.Outcome != ApprovalOutcomeEscalate {
		t.Fatalf("TL 未启用应 escalate: %+v", verdict4)
	}
}
