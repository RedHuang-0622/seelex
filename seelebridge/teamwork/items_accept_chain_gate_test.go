package teamwork

// items_accept_chain_gate_test.go — 责任链的闸门（对应 items.go 的 AcceptItem 注释块）。
//
//	合并（尾插步 1：MergeWorkspace）→ 回执（步 2）→ 状态（步 3）→ leader 审查 → 验收入账
//
// 复现记录：这条链在此之前是**没有闸门**的——AcceptItem 对 running 与 review 同权放行，
// 于是验收的释放（`git worktree remove --force` + `git branch -D`）可以和尾插的合并在
// **同一份现场**上并发动手，真实 git 上就是
// `git [rev-list --count <base>..HEAD]: fork/exec …git.exe: The directory name is invalid`
//（报错原文与"哪一拍动手"的指纹见 seelebridge/worktree/worktree_vanished_scene_repro_test.go）。
//
// 本文件钉住闸门的两面：在跑 ⇒ 拒绝且**不许动现场**；handle 已作废 ⇒ 放行（清理动作）。

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// blockWorker 让 worker 停在半路：工作项因此**稳定地**处于"在跑"——这正是尾插要用现场
// 的那段窗口（现实里是毫秒级，所以这条竞态看起来像偶发）。
func blockWorker(fixture *itemFixture) chan struct{} {
	block := make(chan struct{})
	fixture.runner.mu.Lock()
	fixture.runner.block = block
	fixture.runner.mu.Unlock()
	return block
}

// runningItem 派一件事并等它**稳定在跑**（执行体已收下这一轮，但没跑完）。
func runningItem(t *testing.T, fixture *itemFixture, id string) {
	t.Helper()
	if _, err := fixture.coordinator.DispatchItem(context.Background(), id); err != nil {
		t.Fatalf("DispatchItem(%s): %v", id, err)
	}
	waitRequests(t, fixture, 1)
	if status := itemState(t, fixture, id).StatusOrPending(); status != sessionstore.TeamworkItemRunning {
		t.Fatalf("前置：worker 还在飞时 %s 应在跑，得到 %q", id, status)
	}
}

// TestAcceptRefusesItemStillRunning —— 闸门正面：还在跑 ⇒ 拒绝，且**不许动现场**。
func TestAcceptRefusesItemStillRunning(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	block := blockWorker(fixture)
	runningItem(t, fixture, "wi-req")

	err := fixture.coordinator.AcceptItem(ctx, "wi-req", "抢在尾插前面验收")
	if err == nil || !strings.Contains(err.Error(), "还在跑") {
		t.Fatalf("在跑的工作项必须被拒绝验收（否则会对尾插正在用的那份现场动手），得到 %v", err)
	}
	if _, _, released := fixture.spaces.snapshot(); len(released) != 0 {
		t.Fatalf("被拒的验收不许动现场，却释放了 %v", released)
	}

	// 让它跑完：尾插（合并 → 回执 → 状态）走完，链尾才轮到验收。
	close(block)
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemReview)
	bound, merged, _ := fixture.spaces.snapshot()
	if len(merged) != 1 || merged[0] != "wi-req" {
		t.Fatalf("尾插必须先合并（顺序即责任），得到 %v", merged)
	}
	if len(bound) == 0 {
		t.Fatal("前置：这件事该有一份现场")
	}
	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "通过"); err != nil {
		t.Fatalf("尾插走完之后的验收必须通过: %v", err)
	}
	if _, _, released := fixture.spaces.snapshot(); len(released) != 1 || released[0] != "wi-req" {
		t.Fatalf("验收应释放这份现场，得到 %v", released)
	}
}

// TestAcceptAllowsRunningItemWhoseHandleIsGone —— 闸门反面：handle 已作废（进程重启 /
// 已被回收）= 这一件事的尾插不会再跑，验收于是就只是清理动作，必须放行（"重启后收尾"）。
func TestAcceptAllowsRunningItemWhoseHandleIsGone(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	_ = blockWorker(fixture)
	runningItem(t, fixture, "wi-req")

	// 重启：新协调器 + 空作业表——句柄只在内存，重启必然作废；状态照旧停在 running。
	restarted := newItemFixtureOver(t, fixture)
	if status := itemState(t, restarted, "wi-req").StatusOrPending(); status != sessionstore.TeamworkItemRunning {
		t.Fatalf("前置：重启后状态仍是 running（句柄作废不是错误，是「可重派」的信号），得到 %q", status)
	}
	if err := restarted.coordinator.AcceptItem(context.Background(), "wi-req", "重启后清理现场"); err != nil {
		t.Fatalf("handle 已作废时验收必须放行（清理动作）: %v", err)
	}
	if _, _, released := restarted.spaces.snapshot(); len(released) != 1 || released[0] != "wi-req" {
		t.Fatalf("验收应释放现场，得到 %v", released)
	}
}
