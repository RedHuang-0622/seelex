package core

// goal_loop_turn_order_test.go — 端到端「回合顺序」用例：一轮 goal 循环跑下来，
// 真实 Submit + 真实回合尾治理推进给出的发言次序是
//
//	user（起手）→ exec（主会话回合）→ tl（ADVISOR 回合）→ … → 裁决收口(over)
//
// 两件必须成立的事（2026-09-17 口径）：
//  1. 每个 EXEC 回合的尾巴**必有一次** tl/ADVISOR 回合——缺了它，teammate 就只是
//     名单上的一员，goal 会一直挂在 active；
//  2. 发言环里**没有 user**：用户的发言机会是队列提升那一条输入（起手/收口），
//     所以环成员是 `order_roles − user`，「下一个发言」永远不会指向人。
//
// 裁决脚本：第一轮 continue（循环继续），第二轮 verdict_done（收口 → history +1）。

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// scriptedTLEvaluator 按脚本给裁决：第一次「继续」，第二次「收口」。
type scriptedTLEvaluator struct {
	mu    sync.Mutex
	calls int
}

func (e *scriptedTLEvaluator) Evaluate(_ context.Context, _ goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	if e.calls == 1 {
		return goaldomain.TLDirective{Kind: goaldomain.DirectiveCheckpointOK, Content: "继续"}, nil
	}
	return goaldomain.TLDirective{Kind: goaldomain.DirectiveVerdictDone, Content: "收口"}, nil
}

func (e *scriptedTLEvaluator) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// waitUntil 轮询直到条件成立（回合切换是异步的，用一次有界等待而不是 sleep 猜）。
func waitUntil(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s", what)
}

func TestGoalLoopTurnsAlternateExecAdvisorUntilVerdictCloses(t *testing.T) {
	sessions := &teamRecordingSessions{mainHeadSeq: 1}
	service := summonService(t, sessions)
	evaluator := &scriptedTLEvaluator{}
	service.SetGoalTLEvaluator(evaluator)

	const sessionID = "sess-summon"
	// 起手：user 的发言机会就是这一条输入（队列提升），不是环里的座位。
	if err := service.Submit(context.Background(), "@goal-a2a 跑出 exec/tl 交替再收口"); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// 第 1 轮：exec 回合结束后，回合尾治理必须已经跑过一次 tl/ADVISOR 回合。
	waitUntil(t, "第 1 轮的 tl（ADVISOR）回合未发生", func() bool { return evaluator.count() >= 1 })

	view, err := service.AgentTeamView(sessionID)
	if err != nil {
		t.Fatalf("AgentTeamView: %v", err)
	}
	if !hasRoleName(view.OrderRoles, "user") {
		t.Fatalf("顺序事实仍应含 user（起手与收口）：%v", view.OrderRoles)
	}
	schedule := service.teamScheduleFor(sessionID)
	if schedule == nil {
		t.Fatal("goal 循环应有团队环投影")
	}
	if hasRoleName(schedule.Order, "user") || schedule.NextRole == "user" {
		t.Fatalf("环里不该有 user、下一个也不该指向 user：order=%v next=%q", schedule.Order, schedule.NextRole)
	}
	if strings.Join(schedule.Order, ",") != "main,tl" {
		t.Fatalf("环成员 = %v, want [main tl]（goal-a2a 的顺序是 user→main→tl，去掉 user）", schedule.Order)
	}

	// 第 2 轮：再一条 user 输入驱动下一轮 exec（TL 指令在 ChatStream 回合边界注入），
	// 回合尾再跑一次 tl，这次裁决 verdict_done。
	if err := service.Submit(context.Background(), "继续推进到收口"); err != nil {
		t.Fatalf("第二次 Submit: %v", err)
	}
	waitUntil(t, "第 2 轮的 tl（ADVISOR）回合未发生", func() bool { return evaluator.count() >= 2 })

	// over：裁决收口 → 栈里不再有 active goal，但历史留痕。
	waitUntil(t, "裁决后 goal 未收口", func() bool {
		status, err := service.GoalStatusFor(sessionID)
		return err == nil && status.Active == nil
	})
	status, err := service.GoalStatusFor(sessionID)
	if err != nil {
		t.Fatalf("GoalStatusFor: %v", err)
	}
	if status.History == 0 {
		t.Fatalf("收口应落进 history：%+v", status)
	}
	if evaluator.count() != 2 {
		t.Fatalf("tl 回合次数 = %d, want 2（每个 exec 回合尾一次，不多不少）", evaluator.count())
	}
}
