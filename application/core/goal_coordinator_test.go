package core

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// TestGoalCoordinatorSessionIsolation 验证 P1 会话级协调器：两会话各自
// Begin/Status 不串；无 goal 的会话视图为 nil（前端隐藏）。
func TestGoalCoordinatorSessionIsolation(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{})
	if view := coordinator.GoalGovernanceViewFor("session-a"); view != nil {
		t.Fatalf("未 begin 的会话不应有治理视图: %+v", view)
	}
	if _, err := coordinator.Begin(context.Background(), "session-a", goaldomain.BeginRequest{
		Title: "A 的治理目标",
	}); err != nil {
		t.Fatalf("begin A: %v", err)
	}
	if _, err := coordinator.Begin(context.Background(), "session-b", goaldomain.BeginRequest{
		Title: "B 的治理目标",
	}); err != nil {
		t.Fatalf("begin B: %v", err)
	}
	viewA := coordinator.GoalGovernanceViewFor("session-a")
	if viewA == nil || !viewA.Active || viewA.Title != "A 的治理目标" {
		t.Fatalf("A 治理视图 = %+v", viewA)
	}
	viewB := coordinator.GoalGovernanceViewFor("session-b")
	if viewB == nil || viewB.Title != "B 的治理目标" {
		t.Fatalf("B 治理视图 = %+v", viewB)
	}
	if status := coordinator.StatusFor("session-a"); status.Active == nil || status.Active.Title != "A 的治理目标" {
		t.Fatalf("A 状态 = %+v", status)
	}
	if status := coordinator.StatusFor("session-b"); status.Active == nil || status.Active.Title != "B 的治理目标" {
		t.Fatalf("B 状态 = %+v", status)
	}
}

// failingTLEvaluator 让 ADVISOR 回合总是失败（模拟裁决不可用 / 429 / 超时）。
type failingTLEvaluator struct{}

func (failingTLEvaluator) Evaluate(context.Context, goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error) {
	return goaldomain.TLDirective{}, goaldomain.ErrBadDirective
}

// TestGoalCoordinatorRoundErrorVisible 钉住「本轮治理未完成」的可见面：治理回合
// 失败时 goal 保持 active（安全默认：不拿不可用的裁决收口），但失败原因必须进只读
// 视图；新 goal 上线时清除，不让旧错误解释新目标。
//
// 修复前这条错误在 goalAdvanceAfterChat 里被丢弃，面板只剩 heartbeat_at 可比，于是
// 用墙钟印出一个既不区分"空闲等你输入"也不区分"回合被中止"的 governance stalled。
func TestGoalCoordinatorRoundErrorVisible(t *testing.T) {
	ctx := context.Background()
	coordinator := newGoalCoordinator(goalCoordinatorDeps{Evaluator: failingTLEvaluator{}})
	if _, err := coordinator.Begin(ctx, "session-fail", goaldomain.BeginRequest{Title: "失败回合目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := coordinator.AdvanceAfterChat(ctx, "session-fail", "本轮工作正文"); err == nil {
		t.Fatal("ADVISOR 回合失败应把错误返回给调用方")
	}
	failed := coordinator.GoalGovernanceViewFor("session-fail")
	if failed == nil || !failed.Active {
		t.Fatalf("回合失败不应改变 goal 的 active 态: %+v", failed)
	}
	if failed.RoundError == "" {
		t.Fatal("失败原因应进只读视图 RoundError")
	}
	// 收口旧目标（等价 goal_abort）后压入新目标：面板不能拿着上一目标的错误解释新目标。
	if _, err := coordinator.bundleFor("session-fail").ctl.Abort(ctx, goaldomain.FinishRequest{Reason: "测试收口"}); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if _, err := coordinator.Begin(ctx, "session-fail", goaldomain.BeginRequest{Title: "新目标"}); err != nil {
		t.Fatalf("begin 新目标: %v", err)
	}
	fresh := coordinator.GoalGovernanceViewFor("session-fail")
	if fresh == nil || fresh.Title != "新目标" {
		t.Fatalf("新目标应成为 active 帧: %+v", fresh)
	}
	if fresh.RoundError != "" {
		t.Fatalf("新 goal 上线应清除旧失败记录: %+v", fresh)
	}
}

// TestSessionRuntimeCarriesGoalGovernance 验证 GoalGovernanceView 进入
// SessionRuntime 槽并随 clone 深拷贝（前端快照字段归属）。
func TestSessionRuntimeCarriesGoalGovernance(t *testing.T) {
	runtime := RuntimeState{
		GoalGovernance: &dto.GoalGovernanceView{Active: true, GoalID: "g-1", Round: 7},
	}
	cloned := cloneRuntimeState(runtime)
	if cloned.GoalGovernance == nil || cloned.GoalGovernance.GoalID != "g-1" {
		t.Fatalf("clone 丢失治理视图: %+v", cloned.GoalGovernance)
	}
	cloned.GoalGovernance.Round = 8
	if runtime.GoalGovernance.Round != 7 {
		t.Fatalf("治理视图应深拷贝（互不影响）: %+v", runtime.GoalGovernance)
	}
	session := sessionRuntimeOf(cloned)
	if session.GoalGovernance == nil || session.GoalGovernance.Round != 8 {
		t.Fatalf("sessionRuntimeOf 应携带治理视图: %+v", session.GoalGovernance)
	}
}

// stubTLEvaluator 是一次性 TL 评估器（测试用）。
type stubTLEvaluator struct {
	directives []goaldomain.TLDirective
}

func (e *stubTLEvaluator) Evaluate(context.Context, goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error) {
	if len(e.directives) == 0 {
		return goaldomain.TLDirective{Kind: goaldomain.DirectiveCheckpointOK, Content: "继续"}, nil
	}
	directive := e.directives[0]
	e.directives = e.directives[1:]
	return directive, nil
}

// TestGoalCoordinatorAdvanceAfterChatRunsTLRound 验证 A2A 在真实会话边界
// 可驱动：ChatStream 结束后 AdvanceAfterChat 触发一轮 Governor，Round≥1，
// TL 指令进入待注入队列，且成功回合不留失败记录。
func TestGoalCoordinatorAdvanceAfterChatRunsTLRound(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		Evaluator: &stubTLEvaluator{directives: []goaldomain.TLDirective{{
			Kind: goaldomain.DirectiveCorrect, Content: "先补负路径单测再收口",
		}}},
	})
	if _, err := coordinator.Begin(context.Background(), "session-a2a", goaldomain.BeginRequest{
		Title: "审查 goal 域",
	}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := coordinator.AdvanceAfterChat(context.Background(), "session-a2a", "本轮工作正文"); err != nil {
		t.Fatalf("advance after chat: %v", err)
	}
	view := coordinator.GoalGovernanceViewFor("session-a2a")
	if view == nil || view.Round < 1 {
		t.Fatalf("治理视图应体现 TL 回合（Round≥1）: %+v", view)
	}
	if view.RoundError != "" {
		t.Fatalf("成功回合不应留下失败记录: %+v", view)
	}
	directives := coordinator.DrainDirectives("session-a2a")
	if len(directives) != 1 || directives[0].Content != "先补负路径单测再收口" {
		t.Fatalf("TL 指令应进入待注入队列: %+v", directives)
	}
}

// TestGoalCoordinatorAdvanceAfterChatTLDisabledNoError 验证 TL 未启用时
// 回合结束推进不报错（B4：a 不因 b 缺席而阻塞）。
func TestGoalCoordinatorAdvanceAfterChatTLDisabledNoError(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{})
	if _, err := coordinator.Begin(context.Background(), "session-a2a-off", goaldomain.BeginRequest{
		Title: "无 TL 目标",
	}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := coordinator.AdvanceAfterChat(context.Background(), "session-a2a-off", ""); err != nil {
		t.Fatalf("TL 缺席不应阻塞回合结束: %v", err)
	}
}

// TestGoalCoordinatorRoutineVerdictDoneClosesGoal 钉住 2026-09-15 seq-5389 的
// 缺口：常规治理回合（AdvanceAfterChat → advisor 座位）的 verdict_done 必须
// 直接把 goal 收口出栈，治理视图随即 Active=false。修复前它只断 governor、
// goal 停在 active —— 面板永远"执行中"，loop 不结束。
func TestGoalCoordinatorRoutineVerdictDoneClosesGoal(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		Evaluator: &stubTLEvaluator{directives: []goaldomain.TLDirective{{
			Kind: goaldomain.DirectiveVerdictDone, Content: "验收通过，收口",
		}}},
	})
	ctx := context.Background()
	if _, err := coordinator.Begin(ctx, "session-done", goaldomain.BeginRequest{Title: "收口目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := coordinator.AdvanceAfterChat(ctx, "session-done", "本轮工作正文"); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if status := coordinator.StatusFor("session-done"); status.Active != nil {
		t.Fatalf("routine verdict_done 后 goal 应已出栈, 仍 active: %+v", status.Active)
	}
	if view := coordinator.GoalGovernanceViewFor("session-done"); view == nil || view.Active {
		t.Fatalf("治理视图应转为 Active=false: %+v", view)
	}
	// 收口后 session 处于空闲治理态：再推进不应报错，也不该复活 goal。
	if err := coordinator.AdvanceAfterChat(ctx, "session-done", "空跑"); err != nil {
		t.Fatalf("收口后推进不应报错: %v", err)
	}
	if status := coordinator.StatusFor("session-done"); status.Active != nil {
		t.Fatalf("收口后不应复活 goal: %+v", status.Active)
	}
}

// TestGoalCoordinatorBeginResetsBrokenGovernor 验证新 goal 拿回治理循环：
// 上一个 goal 收口断环后，同会话 begin 新 goal 仍能跑 ADVISOR 回合
// （不会因旧 governor 已断环而恒 0 轮、永不出裁决）。
func TestGoalCoordinatorBeginResetsBrokenGovernor(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{
		Evaluator: &stubTLEvaluator{directives: []goaldomain.TLDirective{
			{Kind: goaldomain.DirectiveVerdictDone, Content: "第一个目标收口"},
			{Kind: goaldomain.DirectiveCorrect, Content: "继续推进"},
		}},
	})
	ctx := context.Background()
	if _, err := coordinator.Begin(ctx, "session-regoal", goaldomain.BeginRequest{Title: "目标一"}); err != nil {
		t.Fatalf("begin#1: %v", err)
	}
	if err := coordinator.AdvanceAfterChat(ctx, "session-regoal", "工作一"); err != nil {
		t.Fatalf("advance#1: %v", err)
	}
	if status := coordinator.StatusFor("session-regoal"); status.Active != nil {
		t.Fatalf("目标一应已收口: %+v", status.Active)
	}
	if _, err := coordinator.Begin(ctx, "session-regoal", goaldomain.BeginRequest{Title: "目标二"}); err != nil {
		t.Fatalf("begin#2: %v", err)
	}
	if err := coordinator.AdvanceAfterChat(ctx, "session-regoal", "工作二"); err != nil {
		t.Fatalf("advance#2: %v", err)
	}
	directives := coordinator.DrainDirectives("session-regoal")
	if len(directives) == 0 || directives[len(directives)-1].Content != "继续推进" {
		t.Fatalf("新 goal 应能跑 ADVISOR 回合（旧 governor 已重置）: %+v", directives)
	}
}
