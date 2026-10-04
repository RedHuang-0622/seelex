package teamwork

// items_accept_running_repro_test.go — 复现「偶发 `The directory name is invalid`」那族的
// **成因**（协调器层）。
//
// AcceptItem 的判据把 review 与 **running 同权**放行：
//
//	case TeamworkItemReview, TeamworkItemRunning:
//
// 而 running 的含义是「worker 还在飞 ⇒ 尾插（合并 + 回执）还没跑完」。于是两条路会对
// **同一份现场**动手：
//
//	尾插合并：SettleWorkItem → MergeWorkspace → WorktreeManager.Finish（git 跑在 wt.Path）
//	验收释放：AcceptItem → releaseItem → ReleaseWorkspaceItem → git worktree remove --force
//
// 谁先谁后决定结局：尾插先读到计划（running）就去合并一份**刚被释放**的现场——真实 git 上
// 就是 `fork/exec …git.exe: The directory name is invalid`（报错原文与指纹见
// seelebridge/worktree/worktree_vanished_scene_repro_test.go）；accept 先写 done，尾插的
// 状态收敛就会幂等跳过整段。本用例把 worker 停在半路，让"在跑"这段窗口**稳定可测**。

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestAcceptWhileItemRunningReleasesLiveScene 钉住竞态成立的两个前提：
// ① 在跑的工作项**被放行**验收；② 那次验收**释放了**尾插正在用的现场。
//
// **注意这条用例钉的是现状（判据洞），不是"应该如此"**：谁要是把 AcceptItem 的判据
// 收紧成"handle 还活着就不许验收"，这条会红——那是**有意的**，改判据时请一并改这里
// 并说明新口径（现状见 items.go 的 `case TeamworkItemReview, TeamworkItemRunning:`）。
func TestAcceptWhileItemRunningReleasesLiveScene(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()

	// 让 worker 停在半路：工作项因此**稳定地**处于"在跑"——这正是尾插要用现场的那段窗口。
	// 现实里这段窗口是毫秒级（worker 一跑完就尾插），所以这条竞态看起来像"偶发"。
	block := make(chan struct{})
	fixture.runner.mu.Lock()
	fixture.runner.block = block
	fixture.runner.mu.Unlock()

	if _, err := fixture.coordinator.DispatchItem(ctx, "wi-req"); err != nil {
		t.Fatalf("DispatchItem: %v", err)
	}
	waitRequests(t, fixture, 1) // 执行体已收下这一轮，但没跑完
	if status := itemState(t, fixture, "wi-req").StatusOrPending(); status != sessionstore.TeamworkItemRunning {
		t.Fatalf("前置：worker 还在飞时工作项应在跑，得到 %q", status)
	}

	// ① 前提一：在跑的工作项被放行验收（这就是"偶发"能被制造出来的原因）。
	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "抢在尾插前面验收"); err != nil {
		t.Fatalf("AcceptItem（在跑）应被放行，否则本竞态不成立：%v", err)
	}
	// ② 前提二：那次验收把现场释放了——尾插若是先读到计划（running），随后合并的就是
	// 一份已被收走的现场。
	_, _, released := fixture.spaces.snapshot()
	if len(released) != 1 || released[0] != "wi-req" {
		t.Fatalf("验收应已释放这份仍被尾插持有的现场，得到 %v", released)
	}

	// worker 跑完 → 尾插：状态已是 done，尾插的状态收敛幂等返回（整段合并被跳过）。
	// 这条断言是"另一条交错"的对照：本用例证明的是两条路都能成立，结局取决于先后。
	close(block)
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemDone)
}
