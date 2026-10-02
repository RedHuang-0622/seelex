package core

// goal_done_test.go — 钉住 **goal_done = 真收口** 的口径（2026-10-02 用户裁决 B）。
//
// 为什么要有这条用例：原来的口径（contract_review 的 U5）要求 agent 面只能**提议**收口、
// 终态归 TL 裁决。裁决 B 推翻了它——主代理在团队里就是 TL 角色（team_close 与 goal_done
// 是同一个人拍板），所以 goal_done 走 Controller.Finish/Abort **直连**，不过终态 gate。
// "直连"这件事必须有一条事实钉住：它落终态、它弹栈、它照常报错。

import (
	"context"
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

func TestGoalDoneFinishesDirectly(t *testing.T) {
	coordinator := newGoalCoordinator(goalCoordinatorDeps{})
	ctx := context.Background()
	if _, err := coordinator.Begin(ctx, "session-a", goaldomain.BeginRequest{Title: "收口目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}

	record, err := coordinator.FinishDirect(ctx, "session-a", goaldomain.FinishRequest{Reason: "做完", Result: "证据"}, false)
	if err != nil {
		t.Fatalf("FinishDirect(finish): %v", err)
	}
	if record == nil || record.Status != goaldomain.StatusCompleted {
		t.Fatalf("goal_done finish 必须直接落 completed（不是提议）：%+v", record)
	}
	if status := coordinator.StatusFor("session-a"); status.Active != nil {
		t.Fatalf("收口必须弹栈（栈里不该再挂着 active goal）：%+v", status.Active)
	}

	if _, err := coordinator.Begin(ctx, "session-a", goaldomain.BeginRequest{Title: "放弃目标"}); err != nil {
		t.Fatalf("begin 2: %v", err)
	}
	aborted, err := coordinator.FinishDirect(ctx, "session-a", goaldomain.FinishRequest{Reason: "放弃"}, true)
	if err != nil {
		t.Fatalf("FinishDirect(abort): %v", err)
	}
	if aborted == nil || aborted.Status != goaldomain.StatusAborted {
		t.Fatalf("goal_done abort 必须直接落 aborted：%+v", aborted)
	}

	// 空栈收口是错误——收口动作不许"静默成功"。
	if _, err := coordinator.FinishDirect(ctx, "session-a", goaldomain.FinishRequest{}, false); err == nil {
		t.Fatal("栈空时收口必须报错，不得静默成功")
	}
}
