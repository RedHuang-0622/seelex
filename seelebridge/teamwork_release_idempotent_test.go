package seelebridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
)

// ── 缺陷 A：accept 释放幂等（2026-10-03）───────────────────────────────
//
// 现场：`team_accept(wi-schema)` / `team_accept(wi-schema-check)` 都报
// `git worktree remove --force <path>: exit status 128` + `fatal: '<path>' is not a
// working tree` —— 因为**作业完成即回收 worktree**：worker 回合结束的自动尾插
// （MergeWorkspace → WorktreeManager.Finish → cleanup）在有提交已合并 / 无提交且干净
// 两条成功路径上都会 `git worktree remove`，却没配一次 Release，注册表与账本里的绑定
// 因此留在原处；accept 的释放步骤对同一份"已不存在的绑定"再动手一次，必然 128。
//
// 两条用例钉住修复：① 正常路径仍会释放（目录消失、无残留登记）；② 绑定已被回收
// （目录 / 分支 / git 登记都不在）时释放必须成功（幂等）。第三条钉住根因那一处
// 配对：MergeWorkspace 收尾成功后必须把注册表也清掉，不留幽灵绑定。

// workItemBinding 造一条 Work Item 口径的工作区绑定（与 coordinator 派发时的派生一致）。
func workItemBinding(mainSessionID, role, itemID string) teamwork.WorkspaceBinding {
	return teamwork.WorkspaceBinding{
		MainSessionID: mainSessionID,
		TeamID:        "team-1",
		Milestone:     "m1",
		WorkItem:      itemID,
		Role:          role,
		SessionID:     mainSessionID + "-team-1-" + role + "-wi-" + itemID,
		Worktree:      teamwork.WorkItemWorktreeName(role, itemID),
	}
}

// TestReleaseWorkspaceItemNormalReleasesScene —— ① 正常路径：工作区还在，释放步骤照常
// 清目录、删分支、清登记。
func TestReleaseWorkspaceItemNormalReleasesScene(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}

	binding := workItemBinding("sess-1", "exec", "wi-schema")
	bound, err := runtime.BindWorkspace(context.Background(), binding)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if bound.Path == "" {
		t.Fatal("git 仓库里必须建出 worktree 现场")
	}
	if _, err := os.Stat(bound.Path); err != nil {
		t.Fatalf("worktree 目录应存在: %v", err)
	}

	if err := runtime.ReleaseWorkspaceItem(context.Background(), bound); err != nil {
		t.Fatalf("正常释放必须成功: %v", err)
	}
	if _, err := os.Stat(bound.Path); err == nil {
		t.Fatalf("释放后 worktree 目录必须消失: %s", bound.Path)
	}
	if info, ok := runtime.worktreeMgr.Info("exec-wi-schema"); ok {
		t.Fatalf("释放后不得残留登记: %+v", info)
	}
	// 释放是幂等的：再放一次仍是成功。
	if err := runtime.ReleaseWorkspaceItem(context.Background(), bound); err != nil {
		t.Fatalf("重复释放必须幂等成功: %v", err)
	}
}

// TestReleaseWorkspaceItemIdempotentWhenBindingAlreadyReclaimed —— ② 绑定已被上游回收
// （目录 / 分支 / git 登记都不在了），accept 的释放步骤必须**成功**（视为已释放），
// 而不是报 128。
func TestReleaseWorkspaceItemIdempotentWhenBindingAlreadyReclaimed(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}

	binding := workItemBinding("sess-1", "exec", "wi-schema-check")
	bound, err := runtime.BindWorkspace(context.Background(), binding)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if bound.Path == "" {
		t.Fatal("git 仓库里必须建出 worktree 现场")
	}

	// 模拟上游（自动尾插 MergeWorkspace → Finish → cleanup）先回收了现场：目录没了、
	// 分支没了、git 登记也 prune 掉了——而注册表 / 账本里的绑定还在。
	if err := os.RemoveAll(bound.Path); err != nil {
		t.Fatal(err)
	}
	// 先 prune 掉已失效的登记（目录不在，git 才允许删那个"被 check out"的分支）。
	if _, err := worktree.GitRunner(repo, "worktree", "prune"); err != nil {
		t.Fatalf("prune 模拟失败: %v", err)
	}
	if _, err := worktree.GitRunner(repo, "branch", "-D", bound.Branch); err != nil {
		t.Fatalf("清理模拟失败: %v", err)
	}
	if _, err := os.Stat(bound.Path); err == nil {
		t.Fatalf("前置条件失败：目录 %q 竟然还在", bound.Path)
	}
	if _, ok := runtime.worktreeMgr.Info("exec-wi-schema-check"); !ok {
		t.Fatal("前置条件失败：注册表里应还留着那份已失效的绑定")
	}

	// 这一行在修复前就是 `exit status 128 / is not a working tree` 的来源。
	if err := runtime.ReleaseWorkspaceItem(context.Background(), bound); err != nil {
		t.Fatalf("对已回收的绑定释放必须幂等成功，而不是 128: %v", err)
	}
	if info, ok := runtime.worktreeMgr.Info("exec-wi-schema-check"); ok {
		t.Fatalf("释放后不得残留登记: %+v", info)
	}
	if err := runtime.ReleaseWorkspaceItem(context.Background(), bound); err != nil {
		t.Fatalf("重复释放必须幂等成功: %v", err)
	}
}

// TestMergeWorkspaceReleasesRegistryAfterSuccessfulSettle —— 钉住根因那一处配对：
// MergeWorkspace（自动尾插的合并步）收尾成功后，注册表也必须清干净——否则留下的是
// 一份"磁盘上已不存在"的幽灵绑定，正是 accept 再动手的依据。
func TestMergeWorkspaceReleasesRegistryAfterSuccessfulSettle(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}

	binding := workItemBinding("sess-1", "exec", "wi-schema")
	bound, err := runtime.BindWorkspace(context.Background(), binding)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if bound.Path == "" {
		t.Fatal("git 仓库里必须建出 worktree 现场")
	}
	// 模拟 worker 在 worktree 里干活并提交（有提交 → 合并路径）。
	if err := os.WriteFile(filepath.Join(bound.Path, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.GitRunner(bound.Path, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.GitRunner(bound.Path, "commit", "-m", "seelex/exec-wi-schema: add feature"); err != nil {
		t.Fatal(err)
	}

	if err := runtime.MergeWorkspace(context.Background(), bound); err != nil {
		t.Fatalf("合并必须成功: %v", err)
	}
	// 收尾成功 → 现场在磁盘上被回收，**且注册表不再留有幽灵绑定**。
	if _, err := os.Stat(bound.Path); err == nil {
		t.Fatalf("合并后 worktree 目录必须消失: %s", bound.Path)
	}
	if info, ok := runtime.worktreeMgr.Info("exec-wi-schema"); ok {
		t.Fatalf("合并成功后不得残留登记（幽灵绑定会让 accept 再动手一次）: %+v", info)
	}
	// accept 的释放步骤此时应是无现场 → 幂等成功。
	if err := runtime.ReleaseWorkspaceItem(context.Background(), bound); err != nil {
		t.Fatalf("合并后的 accept 释放必须成功: %v", err)
	}
}
