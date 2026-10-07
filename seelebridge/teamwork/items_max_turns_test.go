package teamwork

// items_max_turns_test.go — 成员级角色回合上限（max_turns）走完派发链的钉子：
// **计划成员条目 → WorkerRequest.MaxTurns → 作业载荷**。
//
// 与 items_plugins_test.go 同源（同一条"派发时带了、执行时丢了"的失败模式，权限/能力
// 两轴各有一份钉子）：载荷是执行体唯一的输入，所以断言的是**载荷本身**。
// 判别力：把成员声明换成另一个数，载荷必须跟着变——实现退化成常量（直接用编排层的
// c.maxTurns）时本用例失败。

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

func TestDispatchCarriesPerMemberMaxTurns(t *testing.T) {
	ctx := context.Background()
	fixture := newItemFixture(t, 6) // 夹具默认：编排层未设 maxTurns，装配层兜 4 轮
	plan := itemPlan()
	plan.Members[1].MaxTurns = 20 // exec 这一位自己声明了 20 轮
	if err := fixture.coordinator.SetPlan(ctx, plan); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}

	handle, err := fixture.coordinator.Dispatch(ctx, "exec", "跑一轮")
	if err != nil {
		t.Fatalf("Dispatch(exec): %v", err)
	}
	waitTerminal(t, fixture.jobs, handle)

	// 没声明的成员必须回退编排层默认（6），而不是被别人的 20 串过去。
	defaultHandle, err := fixture.coordinator.Dispatch(ctx, "pm", "跑一轮")
	if err != nil {
		t.Fatalf("Dispatch(pm): %v", err)
	}
	waitTerminal(t, fixture.jobs, defaultHandle)

	requests := fixture.runner.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("两次派发应各送一份载荷，得 %d 份", len(requests))
	}
	byRole := map[string]WorkerRequest{}
	for _, request := range requests {
		byRole[request.Role] = request
	}
	if got := byRole["exec"].MaxTurns; got != 20 {
		t.Fatalf("声明了 max_turns 的成员载荷 = %d，want 20（派发时带了、执行时丢了）", got)
	}
	if got := byRole["pm"].MaxTurns; got != 4 {
		// 没声明的成员：maxTurnsFor 回退编排层默认（本夹具的 coordinator 未设
		// maxTurns=0），再被 WorkerExecutor 的装配默认 4 补上——链条没断，
		// 只是默认值来自装配层。
		t.Fatalf("没声明的成员载荷 = %d，want 4（回退默认，且不许串别人的值）", got)
	}
	if byRole["exec"].MaxTurns == byRole["pm"].MaxTurns {
		t.Fatalf("两个成员的上限必须各按自己的条目：exec=%d pm=%d", byRole["exec"].MaxTurns, byRole["pm"].MaxTurns)
	}
}

// TestMaxTurnsForPrefersMemberDeclaration：抉择点本身（0 = 没声明，不是"零轮"）。
func TestMaxTurnsForPrefersMemberDeclaration(t *testing.T) {
	if got := maxTurnsFor(sessionstore.TeamworkMember{MaxTurns: 20}, 6); got != 20 {
		t.Fatalf("成员声明优先：got %d, want 20", got)
	}
	if got := maxTurnsFor(sessionstore.TeamworkMember{}, 6); got != 6 {
		t.Fatalf("0 = 没声明 → 回退默认：got %d, want 6", got)
	}
}
