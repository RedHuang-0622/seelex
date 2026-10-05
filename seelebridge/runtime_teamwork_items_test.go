package seelebridge

// runtime_teamwork_items_test.go — teammate 级（老口径）现场的建 / 释回归守卫
// （2026-10-05，F2/F3）。
//
// 契约：现场 nodeID —— teammate 级 = `<role>`，Work Item 级 = `<role>-<itemID>`；
// 指派名 = `seelex/<nodeID>`；注册键一律是裸 nodeID（去 `seelex/` 前缀的换算只有
// workItemNodeID 一处，见 runtime_teamwork_items.go）。
//
// 本文件钉住 Runtime 侧的那一半：② `ReleaseWorkspace(role)` 真释放角色级现场
// （目录 / 注册表 / 分支三者都清，且幂等）；③ 脏现场释放**显式报错**且现场保留。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
)

// teammateSceneBinding 造一份 teammate 级（角色级）现场绑定：指派名 `seelex/<role>`，
// 与 Coordinator.Dispatch 建现场时的派生一致。
func teammateSceneBinding(mainSessionID, role string) teamwork.WorkspaceBinding {
	return teamwork.WorkspaceBinding{
		MainSessionID: mainSessionID,
		TeamID:        "team-1",
		Role:          role,
		SessionID:     mainSessionID + "-team-1-" + role,
		Worktree:      "seelex/" + role,
	}
}

// localBranchExists 报告本地是否还有某个分支（现场释放的另一半：分支要一并删掉）。
func localBranchExists(t *testing.T, repo, branch string) bool {
	t.Helper()
	out, err := worktree.GitRunner(repo, "branch", "--list", branch)
	if err != nil {
		t.Fatalf("git branch --list %s: %v", branch, err)
	}
	return strings.TrimSpace(out) != ""
}

// TestReleaseWorkspaceReleasesTeammateScene —— ② 角色级现场（老口径派发建出来的
// 那份）在退场时必须**真释放**：目录消失、注册表清空、分支删掉；再释放一次仍成功
// （幂等，且不因为"现场已不在"报错）。
func TestReleaseWorkspaceReleasesTeammateScene(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}

	binding := teammateSceneBinding("sess-scene", "exec")
	bound, err := runtime.BindWorkspace(context.Background(), binding)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if bound.Path == "" {
		t.Fatal("git 仓库里必须建出 teammate 级现场")
	}
	if _, err := os.Stat(bound.Path); err != nil {
		t.Fatalf("worktree 目录应存在: %v", err)
	}
	// 注册键是裸 nodeID（teammate 级 = 角色名）：与指派名去 `seelex/` 前缀同一个键。
	if info, ok := runtime.worktreeMgr.Info("exec"); !ok {
		t.Fatalf("teammate 级现场应登记在角色名下（指派名 %q）: %+v", bound.Worktree, info)
	}
	if !localBranchExists(t, repo, "seelex/exec") {
		t.Fatal("前置条件失败：分支 seelex/exec 应存在")
	}

	if err := runtime.ReleaseWorkspace(context.Background(), "exec"); err != nil {
		t.Fatalf("ReleaseWorkspace(role) 必须真释放，而不是空操作: %v", err)
	}
	if _, err := os.Stat(bound.Path); err == nil {
		t.Fatalf("释放后 worktree 目录必须消失: %s", bound.Path)
	}
	if info, ok := runtime.worktreeMgr.Info("exec"); ok {
		t.Fatalf("释放后不得残留登记: %+v", info)
	}
	if localBranchExists(t, repo, "seelex/exec") {
		t.Fatal("释放后分支 seelex/exec 必须删掉")
	}
	// 幂等：无现场 = 无可释放，再放一次仍是成功（不是错误路径）。
	if err := runtime.ReleaseWorkspace(context.Background(), "exec"); err != nil {
		t.Fatalf("重复释放必须幂等成功: %v", err)
	}
}

// TestReleaseWorkspaceRefusesDirtyTeammateScene —— ③ 现场有未提交改动时释放必须
// **显式报错**（ErrUncommittedChanges 语义），现场 / 注册表 / 分支原样保留——产出是
// 人的资产，框架不替人决定丢还是留。
func TestReleaseWorkspaceRefusesDirtyTeammateScene(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}

	binding := teammateSceneBinding("sess-dirty", "exec")
	bound, err := runtime.BindWorkspace(context.Background(), binding)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if bound.Path == "" {
		t.Fatal("git 仓库里必须建出 teammate 级现场")
	}
	// 未提交改动（未跟踪文件）：释放必须因此被拒。
	if err := os.WriteFile(filepath.Join(bound.Path, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = runtime.ReleaseWorkspace(context.Background(), "exec")
	if err == nil {
		t.Fatal("脏现场释放必须显式报错，不得静默丢弃")
	}
	if !errors.Is(err, worktree.ErrUncommittedChanges) {
		t.Fatalf("错误必须带 ErrUncommittedChanges 语义，得到 %v", err)
	}
	if _, statErr := os.Stat(bound.Path); statErr != nil {
		t.Fatalf("报错时现场必须保留: %v", statErr)
	}
	if _, ok := runtime.worktreeMgr.Info("exec"); !ok {
		t.Fatal("报错时不得清注册表（现场还在）")
	}
	if !localBranchExists(t, repo, "seelex/exec") {
		t.Fatal("报错时不得删分支（现场还在）")
	}
}
