package agentteam

import "testing"

// TestTurnSchedulerChainsAndAdvances 验证 channel + 链表轮转：意向 struct 从
// channel 进，Next 按链表指定下一个 agent session，顺序编辑只改链表。
func TestTurnSchedulerChainsAndAdvances(t *testing.T) {
	scheduler := NewTurnScheduler(
		[]string{"user", "main", "tl"},
		map[string]string{"main": "sess-main", "tl": "goal-a2a-tl"},
		4,
	)
	if got := scheduler.Order(); len(got) != 3 || got[0] != "user" || got[2] != "tl" {
		t.Fatalf("初始顺序 = %v", got)
	}
	// 每个参与者投递意向 → Next 按链表推进并带回会话号。
	for index, want := range []string{"user", "main", "tl", "user"} {
		if !scheduler.Request(TurnRequest{RoleName: want, RoundID: uint64(index + 1)}) {
			t.Fatalf("投递 %s 失败", want)
		}
		next := scheduler.Next()
		if next.RoleName != want {
			t.Fatalf("第 %d 次 Next = %q, want %q", index, next.RoleName, want)
		}
		if want == "tl" && next.RoleSessionID != "goal-a2a-tl" {
			t.Fatalf("tl 会话号 = %q", next.RoleSessionID)
		}
		if want == "main" && next.RoleSessionID != "sess-main" {
			t.Fatalf("main 会话号 = %q", next.RoleSessionID)
		}
	}
	// 顺序调整：上移 tl → 下一个该发言的是它；摘除后回落到 main。
	if !scheduler.Move("tl", -1) {
		t.Fatal("上移 tl 应成功")
	}
	if got := scheduler.Order(); got[1] != "tl" || got[2] != "main" {
		t.Fatalf("上移后顺序 = %v", got)
	}
	if !scheduler.Remove("tl") {
		t.Fatal("摘除 tl 应成功")
	}
	if got := scheduler.Order(); len(got) != 2 || got[1] != "main" {
		t.Fatalf("摘除后顺序 = %v", got)
	}
	if !scheduler.Restore("tl") {
		t.Fatal("恢复 tl 应成功")
	}
	if got := scheduler.Order(); got[2] != "tl" {
		t.Fatalf("恢复后顺序 = %v", got)
	}
	if scheduler.Restore("tl") {
		t.Fatal("重复恢复应拒绝")
	}
}
