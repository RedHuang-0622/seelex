package agentteam

import "testing"

// TestTurnSchedulerChainOrderAndPrefixHandoff 验证存活的链表原语：整表替换顺序
// （SetOrder / Order）、按环推进一格（Advance，跳过环内不该占位的成员）以及
// team work 前缀随推进交接。
//
// 2026-10-01（M4 §9 #3）：channel 投递路径（Requests / Request / Next）、顺序编辑
// 三件（Move / Remove / Restore）与只读 getter（Prefix）**已退场**（没有生产消费者，
// 见 scheduler.go 的文件头）。本用例因此不再钉那些形状，只钉留下的那一份顺序事实，
// 且判据一条不少：顺序、会话号绑定、skip 的跳过与"一圈全跳过 = 收束"、前缀交接。
func TestTurnSchedulerChainOrderAndPrefixHandoff(t *testing.T) {
	scheduler := NewTurnScheduler(
		[]string{"user", "main", "tl"},
		map[string]string{"main": "sess-main", "tl": "goal-a2a-tl"},
	)
	if got := scheduler.Order(); len(got) != 3 || got[0] != "user" || got[2] != "tl" {
		t.Fatalf("初始顺序 = %v", got)
	}

	// 推进：闭链轮转，会话号取自链表上的绑定。
	for index, want := range []string{"user", "main", "tl", "user"} {
		request, ok := scheduler.Advance(nil)
		if !ok {
			t.Fatalf("第 %d 次 Advance 应返回可发言成员", index)
		}
		if request.RoleName != want {
			t.Fatalf("第 %d 次 Advance = %q, want %q", index, request.RoleName, want)
		}
		switch want {
		case "main":
			if request.RoleSessionID != "sess-main" {
				t.Fatalf("main 会话号 = %q", request.RoleSessionID)
			}
		case "tl":
			if request.RoleSessionID != "goal-a2a-tl" {
				t.Fatalf("tl 会话号 = %q", request.RoleSessionID)
			}
		}
	}

	// SetOrder 是顺序的唯一写入口：整表替换后，下一次推进就落到新顺序上。
	scheduler.SetOrder([]string{"tl", "main"}, nil)
	if got := scheduler.Order(); len(got) != 2 || got[0] != "tl" {
		t.Fatalf("替换后顺序 = %v", got)
	}
	request, ok := scheduler.Advance(nil)
	if !ok || request.RoleName != "tl" {
		t.Fatalf("替换后第一次 Advance = %+v ok=%v, want tl", request, ok)
	}

	// skip：环里挂着没有执行者的角色要跳过；一圈全被跳过 = 环内无人可发言。
	partial := NewTurnScheduler([]string{"reviewer", "main"}, nil)
	request, ok = partial.Advance(func(roleName string) bool { return roleName == "reviewer" })
	if !ok || request.RoleName != "main" {
		t.Fatalf("跳过 reviewer 后 = %+v ok=%v, want main", request, ok)
	}
	silent := NewTurnScheduler([]string{"reviewer", "researcher"}, nil)
	if request, ok := silent.Advance(func(string) bool { return true }); ok {
		t.Fatalf("一圈全被跳过时应收束，却返回 %+v", request)
	}

	// 交接上下文前缀：team work 起点 → 当前位置的文本随指针一起给下一名成员。
	scheduler.SetPrefix("R1 user: 目标… / main: 已读 README 首行")
	request, ok = scheduler.Advance(nil)
	if !ok || request.Prefix != "R1 user: 目标… / main: 已读 README 首行" {
		t.Fatalf("交接前缀 = %q ok=%v", request.Prefix, ok)
	}

	// 空环与未装配环（nil 接收者）：都返回 ok=false，调用方按逃生路径收束。
	if request, ok := NewTurnScheduler(nil, nil).Advance(nil); ok {
		t.Fatalf("空环应返回 ok=false，却返回 %+v", request)
	}
	var unassembled *TurnScheduler
	if request, ok := unassembled.Advance(nil); ok {
		t.Fatalf("nil 环应返回 ok=false，却返回 %+v", request)
	}
}
