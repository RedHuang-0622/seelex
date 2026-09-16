package core

// agentteam_ring_revive_test.go — P0 回归：团队环的"逃生停止"只属于**上一轮**
// goal，不能传染给下一轮。
//
// 修复前的缺口（三步叠加）：
//  1. agentteam.Runtime.Stop 是终态：SyncOrder 只改成员与顺序、不碰 stopped；
//     NewRuntime 只在 teamRuntimeStore 槽为空时发生；store.drop 全仓无调用者。
//  2. goalCoordinator.AdvanceAfterChat 每次 chat 结束都先做逃生记账，见到
//     stopped=true 就立刻 runtime.gov.Break(reason) 并 return。
//  3. goalCoordinator.Begin 在新 goal 上线时只重置 gov（runtime.gov = nil），
//     没有重置团队环。
//
// 合起来就是：轮次上限（默认 24）或连续无进展（默认 3）一旦触发，该会话此后每
// 一个新 goal 都会在上线后的第一次 chat 结束被立刻断环——ADVISOR 再也不会被叫
// 起，goal 停在 active 无人收口，治理面板恒 0 轮。

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/agentteam"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// TestNewGoalRevivesStoppedTeamRing：环逃生后，新 goal 上线必须让它复活，并且
// 新 goal 的 chat 回合仍然能把 ADVISOR 叫起来。
func TestNewGoalRevivesStoppedTeamRing(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, withTestSessions(&teamRecordingSessions{}))
	evaluator := &capturingTLEvaluator{}
	service.SetGoalTLEvaluator(evaluator)

	sessionID := "sess-ring-revive"
	ctx := withSessionID(context.Background(), sessionID)

	if _, err := service.GoalBeginFor(ctx, sessionID, goaldomain.BeginRequest{Title: "第一轮 goal"}); err != nil {
		t.Skipf("goal 装配不可用于该夹具: %v", err)
	}
	// 读一次成员表建环（真实读路径同样会建环：装配路径与读路径都调
	// teamRuntimeFor）。
	if _, err := service.AgentTeamView(sessionID); err != nil {
		t.Fatalf("AgentTeamView: %v", err)
	}
	ring := service.teamRuntimeBySession(sessionID)
	if ring == nil {
		t.Skip("该夹具没有团队环（未装配 AgentTeam 存储）")
	}

	// 让环走到逃生终点（等价于轮次上限 / 连续无进展被触发）。
	ring.Stop(agentteam.StopRoundLimit)
	if stopped, reason := ring.Stopped(); !stopped || reason != agentteam.StopRoundLimit {
		t.Fatalf("逃生未生效: stopped=%v reason=%q", stopped, reason)
	}

	// 第一轮 goal 收口（会话是 goal 单例：DefaultStackDepth=1，新 goal 必须先等
	// 上一轮进入终态）。这里直接落终态而不走 gate：gate 需要 ADVISOR 给出
	// verdict_done，本用例关心的只是"新 goal 上线时环能否复活"。
	if _, err := service.components.goal.bundleFor(sessionID).ctl.Abort(
		context.Background(), goaldomain.FinishRequest{Reason: "第一轮收口"},
	); err != nil {
		t.Fatalf("收口第一轮 goal: %v", err)
	}

	// 新 goal 上线。
	if _, err := service.GoalBeginFor(ctx, sessionID, goaldomain.BeginRequest{Title: "第二轮 goal"}); err != nil {
		t.Fatalf("第二个 goal 上线失败: %v", err)
	}
	if stopped, reason := ring.Stopped(); stopped {
		t.Fatalf("新 goal 上线后团队环仍是停止态（reason=%q）：上一轮 goal 的逃生结论传染到了新 goal", reason)
	}
	if schedule := service.teamScheduleFor(sessionID); schedule == nil {
		t.Fatal("新 goal 上线后应仍有调度投影")
	} else if schedule.Round != 0 || schedule.Stopped {
		t.Fatalf("新 goal 的记账应从 0 重新开始，实际 round=%d stopped=%v", schedule.Round, schedule.Stopped)
	}

	// 端到端：新 goal 的一轮有产出 chat 必须还能驱动 ADVISOR 回合。
	// （修复前这里必然一次 Evaluate 都没有：AdvanceAfterChat 会先 Break 掉新 governor。）
	before := len(evaluator.embeds)
	service.ViewMu.Lock()
	service.appendSessionMessageLocked(sessionID, "user", "请继续推进", nil)
	service.appendSessionMessageLocked(sessionID, "assistant", "第二轮产出：RING-REVIVE-MARKER", nil)
	service.ViewMu.Unlock()
	service.goalAdvanceAfterChat(ctx)

	if len(evaluator.embeds) <= before {
		t.Fatal("新 goal 的 ADVISOR 回合被上一轮 goal 的逃生结论杀掉（治理静默）")
	}
	if view := service.GoalGovernanceViewFor(sessionID); view != nil && view.Broken {
		t.Fatalf("新 governor 不应在上线后立刻断环: reason=%q", view.BreakReason)
	}
}

// TestSameGoalBeginIsIdempotentAndKeepsRing：幂等 begin（同名返回既有 active）
// 不重置——环的记账不能被一次重复 begin 清零（否则轮次上限形同虚设）。
func TestSameGoalBeginIsIdempotentAndKeepsRing(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, withTestSessions(&teamRecordingSessions{}))
	evaluator := &capturingTLEvaluator{}
	service.SetGoalTLEvaluator(evaluator)

	sessionID := "sess-ring-idempotent"
	ctx := withSessionID(context.Background(), sessionID)
	if _, err := service.GoalBeginFor(ctx, sessionID, goaldomain.BeginRequest{Title: "同一个 goal"}); err != nil {
		t.Skipf("goal 装配不可用于该夹具: %v", err)
	}
	if _, err := service.AgentTeamView(sessionID); err != nil {
		t.Fatalf("AgentTeamView: %v", err)
	}
	ring := service.teamRuntimeBySession(sessionID)
	if ring == nil {
		t.Skip("该夹具没有团队环（未装配 AgentTeam 存储）")
	}

	// 走两轮记账（有产出 → 只推进轮次、不触发无进展）。
	ring.NoteTurn(true)
	ring.NoteTurn(true)
	if round := ring.Round(); round != 2 {
		t.Fatalf("记账后轮次 = %d, want 2", round)
	}

	// 同名 begin（幂等）不应清零记账。
	if _, err := service.GoalBeginFor(ctx, sessionID, goaldomain.BeginRequest{Title: "同一个 goal"}); err != nil {
		t.Fatalf("幂等 begin 失败: %v", err)
	}
	if round := ring.Round(); round != 2 {
		t.Fatalf("幂等 begin 不应重置环记账：轮次 = %d, want 2", round)
	}
}
