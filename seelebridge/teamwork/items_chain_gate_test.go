package teamwork

// items_chain_gate_test.go — 责任链两端的闸门（对应 items.go 的 AcceptItem 与
// coordinator.go 的 Close 注释块）。
//
//	合并（尾插步 1：MergeWorkspace）→ 回执（步 2）→ 状态（步 3）→ leader 审查 → 验收入账
//
// 两端各堵一处，因为收口是**唯一**会拆 per-item 现场的地方（releaseAllItems 把每件活绑定的
// worktree 目录与分支一并删掉）：
//
//	验收端：还在跑（handle 还在册）⇒ 拒收——否则验收的释放会对尾插正在用的现场动手；
//	收口端：还有没落定的工作项（在跑且 handle 还在册 / 失败）⇒ 拒收——先收账再收口
//	       （待验收不拦：尾插已走完、合并已落地，释放它的现场是安全的）。
//
// 复现记录：这条链在补闸门之前**两端都没有**。缺验收端时，真实 git 上得到
// `git [rev-list --count <base>..HEAD]: fork/exec …git.exe: The directory name is invalid`
//（报错原文与"哪一拍动手"的指纹见 seelebridge/worktree/worktree_vanished_scene_repro_test.go）；
// 缺收口端时，一次 `team_close` 会把"还没来得及人工变基的那份现场"连同分支一起删掉。

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// blockWorker 让 worker 停在半路：工作项因此**稳定地**处于"在跑"——这正是尾插要用现场
// 的那段窗口（冲突时是"等 leader 人工解冲突"，慢变基时是"变基还在跑"，现实里都只有一瞬，
// 所以这类竞态看起来像偶发）。
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

// ── 验收端 ──────────────────────────────────────────────────────────

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

// ── 收口端 ──────────────────────────────────────────────────────────

// TestCloseRefusesWhileItemRunning 收口闸门之一：还在跑（handle 还在册）⇒ 拒收，连作业
// 都不许动；等尾插走完（review，合并已落地）再收口 ⇒ 活绑定一并结束。
func TestCloseRefusesWhileItemRunning(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	block := blockWorker(fixture)
	runningItem(t, fixture, "wi-req")

	// ① 在跑：尾插还在飞（慢变基就落在这段窗口里）——收口不许动手。
	if _, err := fixture.coordinator.Close(ctx); err == nil || !strings.Contains(err.Error(), "wi-req(running)") {
		t.Fatalf("收口必须拦住还在跑的工作项，得到 %v", err)
	}
	if _, _, released := fixture.spaces.snapshot(); len(released) != 0 {
		t.Fatalf("被拒的收口不许动现场，却释放了 %v", released)
	}
	if !fixture.coordinator.handleAlive(itemState(t, fixture, "wi-req").Handle) {
		t.Fatal("被拒的收口不许动那份还在飞的作业（handle 应仍在册）")
	}

	// ② 尾插走完（review）：合并已落地，释放它的现场是安全的——收口放行。
	close(block)
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemReview)
	alreadyClosed, err := fixture.coordinator.Close(ctx)
	if err != nil || alreadyClosed {
		t.Fatalf("落定之后收口必须通过，得到 alreadyClosed=%v err=%v", alreadyClosed, err)
	}
	if _, _, released := fixture.spaces.snapshot(); len(released) == 0 {
		t.Fatal("收口应把活绑定一并释放")
	}
	if again, err := fixture.coordinator.Close(ctx); err != nil || !again {
		t.Fatalf("重复收口应返回 alreadyClosed=true，得到 %v err=%v", again, err)
	}
}

// TestCloseRefusesFailedItemUntilSettled 收口闸门之二：失败项的现场是留给 leader 人工处置
// 的（解冲突 / 变基 / 合并），收口拆掉它等于把 leader 要用的东西删了、分支也一并删——所以
// 拒收，直到它被销项（销项 = 链尾唯一的出口，人工处置完或判定放弃时动手）。
func TestCloseRefusesFailedItemUntilSettled(t *testing.T) {
	fixture := newItemFixture(t, 6)
	setupItemPlan(t, fixture)
	ctx := context.Background()
	block := blockWorker(fixture)
	runningItem(t, fixture, "wi-req")
	close(block)
	waitItemStatus(t, fixture, "wi-req", sessionstore.TeamworkItemReview)
	if err := fixture.coordinator.FailItem(ctx, "wi-req", "这一轮跑歪了，现场留着人工处置"); err != nil {
		t.Fatalf("FailItem: %v", err)
	}

	if _, err := fixture.coordinator.Close(ctx); err == nil || !strings.Contains(err.Error(), "wi-req(failed)") {
		t.Fatalf("收口必须拦住未处置的失败项，得到 %v", err)
	}
	if _, _, released := fixture.spaces.snapshot(); len(released) != 0 {
		t.Fatalf("被拒的收口不许动现场，却释放了 %v", released)
	}

	// 人工处置完（或判定放弃）→ 销项 → 收口通过。
	if err := fixture.coordinator.AcceptItem(ctx, "wi-req", "人工处置完毕，销项"); err != nil {
		t.Fatalf("失败项处置完必须能销项（否则账永远收不干净、整队收不了）: %v", err)
	}
	alreadyClosed, err := fixture.coordinator.Close(ctx)
	if err != nil || alreadyClosed {
		t.Fatalf("账收干净之后收口必须通过，得到 alreadyClosed=%v err=%v", alreadyClosed, err)
	}
}
