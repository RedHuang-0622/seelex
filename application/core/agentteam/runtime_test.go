package agentteam

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// runtime_test.go 钉住「会话级发言调度运行态」的三个产品口径：
//  1. 环维护成员：注册表顺序变一次，链表跟着变一次；
//  2. user 席位：缺省 queued（只有队列里有输入才占位），不阻塞 agent 循环；
//  3. 逃生路径：轮次上限 / 连续无进展 / 无执行者 / 空环 / 外部显式停止。

func newTestRuntime(order []string, policy string, opts RuntimeOptions) *Runtime {
	return NewRuntime(order, map[string]string{"tl": "sess-tl"}, policy, opts)
}

// TestRuntimeMaintainsRingFromRegistryOrder：环里的员工就是注册表顺序里的员工
// （顺序的唯一事实是 lifecycle；环只是运行时的镜像，不新增第二份）。
func TestRuntimeMaintainsRingFromRegistryOrder(t *testing.T) {
	runtime := newTestRuntime([]string{"user", "main", "tl"}, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	if got := runtime.Order(); strings.Join(got, ",") != "user,main,tl" {
		t.Fatalf("环初始顺序 = %v", got)
	}
	// 增加员工：reviewer 入职后顺序同步，且因没有执行者被跳过（不占位）。
	runtime.SyncOrder([]string{"user", "main", "tl", "reviewer"}, nil, dto.OrderPolicyGoalLoop)
	if got := strings.Join(runtime.Order(), ","); got != "user,main,tl,reviewer" {
		t.Fatalf("增加员工后环顺序 = %q", got)
	}
	// 摘除员工：环跟着变。
	runtime.SyncOrder([]string{"user", "main", "tl"}, nil, dto.OrderPolicyGoalLoop)
	if got := strings.Join(runtime.Order(), ","); got != "user,main,tl" {
		t.Fatalf("摘除员工后环顺序 = %q", got)
	}
}

// TestRuntimeRingsThroughExecutorsOnly：按链表转一圈，只落在有执行者的角色上；
// 没有执行者的角色被跳过并如实报出（不是静默忽略）。
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
		t.Fatalf("环绕次序 = %q（user 无排队输入应被跳过、reviewer 无执行者应被跳过）", got)
	}
	if unexecuted := runtime.Snapshot().Unexecuted; len(unexecuted) != 1 || unexecuted[0] != "reviewer" {
		t.Fatalf("无执行者角色应如实报出: %v", unexecuted)
	}
}

// TestRuntimeUserSeatPolicy：user 到底算不算环里的一环，由席位口径决定——
// queued（缺省）只在有排队输入时占位；member 每轮固定占位；absent 永不占位。
func TestRuntimeUserSeatPolicy(t *testing.T) {
	queued := newTestRuntime([]string{"user", "main", "tl"}, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	if next, _ := queued.Next(); next.RoleName != "main" {
		t.Fatalf("queued 口径下 user 无排队输入应被跳过，得到 %q", next.RoleName)
	}
	// 链表顺序决定"下一个谁"：user 是环头，一轮走到环尾后自然轮到 user。
	queued.NoteUserQueued(true)
	if next, ok := queued.Next(); !ok || next.RoleName != "tl" {
		t.Fatalf("继续环绕应先到 tl，得到 %q ok=%v", next.RoleName, ok)
	}
	if next, ok := queued.Next(); !ok || next.RoleName != "user" {
		t.Fatalf("绕回环头时排队输入应让 user 占位，得到 %q ok=%v", next.RoleName, ok)
	}
	if next, ok := queued.Next(); !ok || next.RoleName != "main" {
		t.Fatalf("user 发言一次后排队输入应被消费（再绕一圈不再占位），得到 %q ok=%v", next.RoleName, ok)
	}

	member := newTestRuntime([]string{"user", "main", "tl"}, dto.OrderPolicyGoalLoop, RuntimeOptions{UserSeat: UserSeatMember})
	first, _ := member.Next()
	if first.RoleName != "user" {
		t.Fatalf("member 口径下 user 应每轮固定占位，得到 %q", first.RoleName)
	}

	absent := newTestRuntime([]string{"user", "main", "tl"}, dto.OrderPolicyGoalLoop, RuntimeOptions{UserSeat: UserSeatAbsent})
	absent.NoteUserQueued(true)
	if next, _ := absent.Next(); next.RoleName != "main" {
		t.Fatalf("absent 口径下 user 永不占位（即使有排队输入），得到 %q", next.RoleName)
	}
}

// TestUserSeatPolicyDerivesFromOrderPolicy：user 席位口径由 order_policy 推导，
// 不新增第二个人工配置项（两处开关会打架）。
func TestUserSeatPolicyDerivesFromOrderPolicy(t *testing.T) {
	if got := UserSeatPolicyFor(dto.OrderPolicyGoalLoop); got != UserSeatQueued {
		t.Fatalf("goal_loop → %q, want queued", got)
	}
	if got := UserSeatPolicyFor(dto.OrderPolicyUserMainDecided); got != UserSeatMember {
		t.Fatalf("user_main_decided → %q, want member", got)
	}
	if got := UserSeatPolicyFor(dto.OrderPolicyScheduledOnly); got != UserSeatAbsent {
		t.Fatalf("scheduled_only → %q, want absent", got)
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

// TestRuntimeEscapeNoExecutor：环里一个能发言的都没有时显式收束（no_executor /
// empty_ring），不是"转一圈返回空"让调用方自己猜。
func TestRuntimeEscapeNoExecutor(t *testing.T) {
	noExecutor := newTestRuntime([]string{"reviewer", "researcher"}, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	if _, ok := noExecutor.Next(); ok {
		t.Fatal("全员无执行者时不应返回发言者")
	}
	if stopped, reason := noExecutor.Stopped(); !stopped || reason != StopNoExecutor {
		t.Fatalf("全员无执行者应显式收束，得到 stopped=%v reason=%q", stopped, reason)
	}

	empty := newTestRuntime(nil, dto.OrderPolicyGoalLoop, RuntimeOptions{})
	if _, ok := empty.Next(); ok {
		t.Fatal("空环不应返回发言者")
	}
	if stopped, reason := empty.Stopped(); !stopped || reason != StopEmptyRing {
		t.Fatalf("空环应显式收束，得到 stopped=%v reason=%q", stopped, reason)
	}
}

// TestRuntimeEscapeExternalStop：用户中断 / TL 裁决收口 / goal.gov_break 走同一
// 个显式停止入口，原因原样保留。
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
