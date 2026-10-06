package agentteam

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// runtime_test.go 钉住「会话级发言调度运行态」的三个产品口径：
//  1. 环维护成员：注册表顺序变一次，链表跟着变一次；环成员 = 顺序 − user；
//  2. user 不在环里：它的发言机会是回合尾消息队列被整批提升为下一轮（chat 轮次），
//     不是环里的一个排班位——队友的「下一个」永远不会是 user；
//  3. 逃生路径：轮次上限 / 连续无进展 / 没有自动回合 / 空环 / 外部显式停止。

func newTestRuntime(order []string, policy string, opts RuntimeOptions) *Runtime {
	return NewRuntime(order, map[string]string{"tl": "sess-tl"}, policy, opts)
}

// TestRuntimeMaintainsRingFromRegistryOrder：环里的员工就是注册表顺序里的员工
// （顺序的唯一事实是 lifecycle；环只是运行时的镜像 + 「减去 user」的一次投影，
// 不新增第二份顺序事实）。
func TestRuntimeMaintainsRingFromRegistryOrder(t *testing.T) {
	runtime := newTestRuntime([]string{"user", "main", "tl"}, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	if got := runtime.Order(); strings.Join(got, ",") != "main,tl" {
		t.Fatalf("环初始顺序 = %v（user 不落环）", got)
	}
	// 增加员工：reviewer 入职后顺序同步，且因没有自动回合被跳过（不占位）。
	runtime.SyncOrder([]string{"user", "main", "tl", "reviewer"}, nil, dto.OrderPolicyGoalLoop)
	if got := strings.Join(runtime.Order(), ","); got != "main,tl,reviewer" {
		t.Fatalf("增加员工后环顺序 = %q", got)
	}
	// 摘除员工：环跟着变。
	runtime.SyncOrder([]string{"user", "main", "tl"}, nil, dto.OrderPolicyGoalLoop)
	if got := strings.Join(runtime.Order(), ","); got != "main,tl" {
		t.Fatalf("摘除员工后环顺序 = %q", got)
	}
}

// TestRuntimeRingsThroughExecutorsOnly：按链表转一圈，只落在有执行者的角色上；
// 没有自动回合的角色被跳过并如实报出（不是静默忽略）。
func TestRuntimeRingsThroughExecutorsOnly(t *testing.T) {
	runtime := newTestRuntime([]string{"user", "main", "tl", "reviewer"}, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	var spoke []string
	for i := 0; i < 3; i++ {
		request, ok := runtime.Next()
		if !ok {
			t.Fatalf("第 %d 次环绕没拿到发言者", i+1)
		}
		spoke = append(spoke, request.RoleName)
	}
	if got := strings.Join(spoke, ","); got != "main,tl,main" {
		t.Fatalf("环绕次序 = %q（reviewer 没有自动回合应被跳过；环里没有 user）", got)
	}
	if withoutAutoTurn := runtime.Snapshot().NoAutomaticTurn; len(withoutAutoTurn) != 1 || withoutAutoTurn[0] != "reviewer" {
		t.Fatalf("没有自动回合的角色应如实报出: %v", withoutAutoTurn)
	}
}

// TestRuntimeRingExcludesUser：环里没有 user——user 的发言机会是回合尾消息队列
// 被整批提升为下一轮（chat 轮次），不是一个排班位。
//
// 这是产品口径的硬断言：队友的「下一个」永远不会是 user。order_roles 仍然含
// user（它是群聊的起手与收口，spec 校验也要求在场），环只是它减去 user 的投影，
// 且与顺序策略无关（不新增第二个人工开关）。
func TestRuntimeRingExcludesUser(t *testing.T) {
	for _, policy := range []string{dto.OrderPolicyGoalLoop, dto.OrderPolicyUserMainDecided, dto.OrderPolicyScheduledOnly} {
		runtime := newTestRuntime([]string{"user", "main", "tl"}, policy, RuntimeOptions{})
		if got := strings.Join(runtime.Order(), ","); got != "main,tl" {
			t.Fatalf("order_policy=%s 环成员 = %q，want main,tl（user 不落环）", policy, got)
		}
		var spoke []string
		for i := 0; i < 3; i++ {
			request, ok := runtime.Next()
			if !ok {
				t.Fatalf("order_policy=%s 第 %d 次环绕没拿到发言者", policy, i+1)
			}
			spoke = append(spoke, request.RoleName)
		}
		if got := strings.Join(spoke, ","); got != "main,tl,main" {
			t.Fatalf("order_policy=%s 环绕次序 = %q，want main,tl,main（环里没有 user）", policy, got)
		}
		if next := runtime.Snapshot().NextRole; next == string(dto.RoleKindUser) {
			t.Fatalf("order_policy=%s 快照的下一个发言者不应是 user", policy)
		}
	}
}

// TestRuntimeEscapeRoundLimit：轮次上限是逃生路径第一道——到达即停，且原因是
// 可读的 round_limit（不是"悄悄地不转了"）。
func TestRuntimeEscapeRoundLimit(t *testing.T) {
	runtime := newTestRuntime([]string{"user", "main", "tl"}, dto.OrderPolicyGoalLoop, RuntimeOptions{RoundLimit: 2})
	runtime.NoteTurn(true)
	if stopped, _ := runtime.Stopped(); stopped {
		t.Fatal("未到上限不应停止")
	}
	if stopped, reason := runtime.NoteTurn(true); !stopped || reason != StopRoundLimit {
		t.Fatalf("到达轮次上限应停止并给出原因，得到 stopped=%v reason=%q", stopped, reason)
	}
	if _, ok := runtime.Next(); ok {
		t.Fatal("已收束的环不应再有人发言")
	}
	if snapshot := runtime.Snapshot(); !snapshot.Stopped || snapshot.StopReason != StopRoundLimit || snapshot.NextRole != "" {
		t.Fatalf("运行态投影应显示已收束: %+v", snapshot)
	}
}

// TestRuntimeEscapeNoProgress：连续无进展是逃生路径第二道——推进一次即清零，
// 连续空转到达上限即停（"不能不休止地转"）。
func TestRuntimeEscapeNoProgress(t *testing.T) {
	runtime := newTestRuntime([]string{"user", "main", "tl"}, dto.OrderPolicyGoalLoop, RuntimeOptions{NoProgressLimit: 3})
	runtime.NoteTurn(false)
	runtime.NoteTurn(false)
	if stopped, _ := runtime.Stopped(); stopped {
		t.Fatal("未到无进展上限不应停止")
	}
	// 中途有进展 → 计数清零，之前的两轮空转不算。
	runtime.NoteTurn(true)
	runtime.NoteTurn(false)
	runtime.NoteTurn(false)
	if stopped, _ := runtime.Stopped(); stopped {
		t.Fatal("进展后计数应清零")
	}
	if stopped, reason := runtime.NoteTurn(false); !stopped || reason != StopNoProgress {
		t.Fatalf("连续无进展到上限应停止，得到 stopped=%v reason=%q", stopped, reason)
	}
}

// TestRuntimeEscapeNoAutomaticTurn：环里一个能发言的都没有时显式收束（no_automatic_turn /
// empty_ring），不是"转一圈返回空"让调用方自己猜。
func TestRuntimeEscapeNoAutomaticTurn(t *testing.T) {
	noExecutor := newTestRuntime([]string{"reviewer", "researcher"}, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	if _, ok := noExecutor.Next(); ok {
		t.Fatal("全员没有自动回合时不应返回发言者")
	}
	if stopped, reason := noExecutor.Stopped(); !stopped || reason != StopNoAutomaticTurn {
		t.Fatalf("全员没有自动回合应显式收束，得到 stopped=%v reason=%q", stopped, reason)
	}

	empty := newTestRuntime(nil, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	if _, ok := empty.Next(); ok {
		t.Fatal("空环不应返回发言者")
	}
	if stopped, reason := empty.Stopped(); !stopped || reason != StopEmptyRing {
		t.Fatalf("空环应显式收束，得到 stopped=%v reason=%q", stopped, reason)
	}

	// user 不落环：只有 user 的会话没有发言环（用户经队列提升发言，不走环）。
	userOnly := newTestRuntime([]string{"user"}, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	if _, ok := userOnly.Next(); ok {
		t.Fatal("user 不落环，只有 user 的会话不应返回发言者")
	}
	if stopped, reason := userOnly.Stopped(); !stopped || reason != StopEmptyRing {
		t.Fatalf("user 不落环后只剩空环，应显式收束，得到 stopped=%v reason=%q", stopped, reason)
	}
}

// TestRuntimeEscapeExternalStop：用户中断 / goal 收口走同一个显式停止入口，原因
// 原样保留。
func TestRuntimeEscapeExternalStop(t *testing.T) {
	runtime := newTestRuntime([]string{"user", "main", "tl"}, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	runtime.Stop("verdict_done")
	if stopped, reason := runtime.Stopped(); !stopped || reason != "verdict_done" {
		t.Fatalf("外部停止应保留原因，得到 stopped=%v reason=%q", stopped, reason)
	}
	if stopped, _ := runtime.NoteTurn(true); !stopped {
		t.Fatal("已停止的环不应被一次记账复活")
	}
	// 同步顺序（增删员工）也不应把已收束的环"复活"：逃生是显式结论。
	runtime.SyncOrder([]string{"user", "main", "tl", "reviewer"}, nil, dto.OrderPolicyGoalLoop)
	if stopped, _ := runtime.Stopped(); !stopped {
		t.Fatal("同步顺序不应复活已收束的环")
	}
}

// TestRuntimeResetRevivesEscapeState：逃生是显式结论，但**复活也必须是显式可达
// 的**——Reset 清停止态与轮次/无进展记账，顺序与成员不动。
//
// 缺口背景：停止态此前没有任何复活口（SyncOrder 不碰 stopped、NewRuntime 只在槽
// 为空时发生、drop 无调用者），于是"上一轮 goal 的逃生结论"会传染给同会话的每一
// 轮后续 goal：环一上来就是 stopped=true，收口/派活等不到任何环上的信号。
func TestRuntimeResetRevivesEscapeState(t *testing.T) {
	runtime := newTestRuntime([]string{"user", "main", "tl"}, dto.OrderPolicyGoalLoop,
		RuntimeOptions{RoundLimit: 1, NoProgressLimit: 0})
	if stopped, _ := runtime.NoteTurn(true); !stopped {
		t.Fatal("round_limit=1 时第一次记账就应收束")
	}
	if _, ok := runtime.Next(); ok {
		t.Fatal("已收束的环不应继续发牌")
	}

	runtime.Reset()

	if stopped, reason := runtime.Stopped(); stopped {
		t.Fatalf("Reset 后不应仍是停止态（reason=%q）", reason)
	}
	schedule := runtime.Snapshot()
	if schedule.Stopped || schedule.StopReason != "" {
		t.Fatalf("Reset 后逃生投影未清零: %+v", schedule)
	}
	if schedule.Round != 0 || schedule.NoProgress != 0 {
		t.Fatalf("Reset 后轮次/无进展记账未清零: round=%d noProgress=%d", schedule.Round, schedule.NoProgress)
	}
	request, ok := runtime.Next()
	if !ok {
		t.Fatal("Reset 后环应能继续发牌")
	}
	if request.RoleName != "main" {
		t.Fatalf("Reset 后第一个可发言成员 = %q，want main（环里没有 user 的排班位）", request.RoleName)
	}
	// Reset 不是重建环：顺序与成员必须原样保留（顺序事实只有 lifecycle 一份）。
	if got := strings.Join(runtime.Order(), ","); got != "main,tl" {
		t.Fatalf("Reset 不应改动环顺序，得到 %q", got)
	}
	// Reset 之后轮次上限重新计时（逃生记账按"这一次治理循环"独立）。
	if stopped, _ := runtime.NoteTurn(true); !stopped {
		t.Fatal("Reset 后应重新按上限计时（round_limit=1 应再次收束）")
	}
}

// TestRuntimeResetOnNilIsSafe：Reset 走 nil 接收者安全（未装配团队环的会话在
// goal 上线时会调到它）。
func TestRuntimeResetOnNilIsSafe(t *testing.T) {
	var runtime *Runtime
	runtime.Reset()
	if stopped, _ := runtime.Stopped(); stopped {
		t.Fatal("nil 环不应报告已停止")
	}
}
