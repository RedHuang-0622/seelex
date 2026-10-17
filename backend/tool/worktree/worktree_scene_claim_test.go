package worktree

// worktree_scene_claim_test.go — F4/F5 的回归守卫：**重启不是团队现场的坟墓**。
//
// 三条用例逐条对应探针实测的缺陷（实现面见 worktree_manager.go 的 beginNamed / Adopt）：
//
//	F4 ① 重启（新 WorktreeManager = 空注册表）+ 认领之后，现场必须**在册**、目录与
//	      `seelex/<nodeID>` 分支都保留、`Prune` 不得把它当孤儿删掉；
//	F5 ② 重启后重派同一 nodeID：现场里已有的**未提交产出**不得被
//	      `worktree remove --force` 丢掉；
//	F5 ③ 分支已在而目录不在：用**既有分支**建现场（`worktree add <path> <branch>`，
//	      不加 `-b`），分支上已提交的产出必须能找回。
//
// 修复前三条都是红的：① 认领入口不存在（现场不在册 → Prune 的"不在册 + 干净"判据命中）；
// ②③ 落到残留清理分支（`worktree remove --force` + `branch -D` + 重建）。
// 这里要用**真实 git**（reproGitRepo）而不是 fakeGit：这三条要证的正是 git 自己的行为
// （worktree add 撞已存在的分支必然失败），fakeGit 会把要证的事假设成假的。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sceneManager 造一个绑定到 root 的 worktree 管理器（真实 git）。每次调用 = 一个新进程
// 的管理器：注册表是内存态，重启即空——这正是 F4/F5 的前提。
func sceneManager(root string) *WorktreeManager {
	return NewWorktreeManager(WorktreeManagerDeps{
		Root:  func() string { return root },
		Phase: func(context.Context, string, string) {},
	})
}

// TestAdoptClaimsExistingSceneAndPruneKeepsIt —— F4 ①：认领既有现场 + Prune 不删它。
func TestAdoptClaimsExistingSceneAndPruneKeepsIt(t *testing.T) {
	root := reproGitRepo(t)
	nodeID := "exec-wi-clean"
	branch := "seelex/" + nodeID

	// 旧进程：建出现场（目录 + 分支）。注意它**干净**——探针实测里被 Prune 删掉的正是
	// 这种"干净但还没合并"的现场。
	before := sceneManager(root)
	wt := before.BeginNamed(nodeID)
	if wt == nil {
		t.Fatal("真实 git 仓库里应能建出现场")
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("前置：现场目录应存在：%v", err)
	}

	// 重启：新的管理器 = 空注册表（现场不在册）。
	restarted := sceneManager(root)
	if restarted.RegisteredCount() != 0 {
		t.Fatal("前置条件：重启后的注册表必须是空的")
	}

	// 宿主恢复链的认领一步（RestoreSubagentAnchors 在 Prune **之前**调用它）。
	adopted := restarted.Adopt(nodeID)
	if adopted == nil {
		t.Fatalf("既有现场必须被认领（%s 是本 repo 的 worktree）：%v", wt.Path, adopted)
	}
	if !worktreePathEqual(adopted.Path, wt.Path) {
		t.Fatalf("认领必须沿用同一现场，不得重建：got %q want %q", adopted.Path, wt.Path)
	}
	if info, ok := restarted.Info(nodeID); !ok || !worktreePathEqual(info.Path, wt.Path) {
		t.Fatalf("认领后现场必须在册（绑根才落得到它）：%+v ok=%v", info, ok)
	}

	// 清理在后：在册的现场不是孤儿，一个也不许删。
	result, err := restarted.Prune()
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	for _, removed := range result.Removed {
		if worktreePathEqual(removed, wt.Path) {
			t.Fatalf("在册现场被 Prune 当孤儿删了：Removed=%v", result.Removed)
		}
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("认领后的现场目录不得被 Prune 删掉：%v", err)
	}
	branches, err := GitRunner(root, "branch", "--list", branch)
	if err != nil || !strings.Contains(branches, branch) {
		t.Fatalf("现场分支 %q 不得被删掉：out=%q err=%v", branch, branches, err)
	}
}

// TestBeginNamedAfterRestartKeepsUncommittedScene —— F5 ②：重派同一 nodeID 不丢未提交产出。
func TestBeginNamedAfterRestartKeepsUncommittedScene(t *testing.T) {
	root := reproGitRepo(t)
	nodeID := "exec-wi-uncommitted"

	before := sceneManager(root)
	wt := before.BeginNamed(nodeID)
	if wt == nil {
		t.Fatal("真实 git 仓库里应能建出现场")
	}
	produced := filepath.Join(wt.Path, "produced.txt")
	if err := os.WriteFile(produced, []byte("未提交的产出\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 重启（新管理器、空注册表）后重派同一 nodeID：修复前 `worktree add -b` 撞上已存在
	// 的分支 → 落到残留清理分支 → remove --force + branch -D + 重建，产出消失。
	restarted := sceneManager(root)
	again := restarted.BeginNamed(nodeID)
	if again == nil {
		t.Fatal("重派同一 nodeID 必须认领既有现场，而不是失败/重建")
	}
	if !worktreePathEqual(again.Path, wt.Path) {
		t.Fatalf("重派必须沿用同一现场：got %q want %q", again.Path, wt.Path)
	}
	if _, err := os.Stat(produced); err != nil {
		t.Fatalf("重派同一 nodeID 不得丢掉现场里已有的未提交产出：%v", err)
	}
	// 现场是人的资产：重派路径上一句 `worktree remove --force` 都不许出现。
	if info, ok := restarted.Info(nodeID); !ok || !worktreePathEqual(info.Path, wt.Path) {
		t.Fatalf("重派后现场必须在册：%+v ok=%v", info, ok)
	}
}

// TestBeginNamedRebuildsSceneFromExistingBranch —— F5 ③：分支已在、目录不在。
func TestBeginNamedRebuildsSceneFromExistingBranch(t *testing.T) {
	root := reproGitRepo(t)
	nodeID := "exec-wi-branch"
	branch := "seelex/" + nodeID

	before := sceneManager(root)
	wt := before.BeginNamed(nodeID)
	if wt == nil {
		t.Fatal("真实 git 仓库里应能建出现场")
	}
	// 现场里留一份**已提交**的产出（分支上）。
	if err := os.WriteFile(filepath.Join(wt.Path, "committed.txt"), []byte("已提交产出\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", branch + ": committed"}} {
		if _, err := GitRunner(wt.Path, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}

	// 目录丢了（git 登记也顺手 prune 掉），但 `seelex/<nodeID>` 分支还在。
	if err := os.RemoveAll(wt.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := GitRunner(root, "worktree", "prune"); err != nil {
		t.Fatalf("worktree prune: %v", err)
	}
	if _, err := os.Stat(wt.Path); err == nil {
		t.Fatal("前置条件失败：现场目录竟然还在")
	}
	if out, err := GitRunner(root, "branch", "--list", branch); err != nil || !strings.Contains(out, branch) {
		t.Fatalf("前置条件失败：分支 %q 应还在：out=%q err=%v", branch, out, err)
	}

	restarted := sceneManager(root)
	again := restarted.BeginNamed(nodeID)
	if again == nil {
		t.Fatal("分支已在而目录不在时，必须用既有分支建现场（worktree add <path> <branch>）")
	}
	if !worktreePathEqual(again.Path, wt.Path) {
		t.Fatalf("应回到同一个现场路径：got %q want %q", again.Path, wt.Path)
	}
	data, err := os.ReadFile(filepath.Join(again.Path, "committed.txt"))
	if err != nil || !strings.Contains(string(data), "已提交产出") {
		t.Fatalf("既有分支上已提交的产出必须能找回：err=%v content=%q", err, string(data))
	}
	if info, ok := restarted.Info(nodeID); !ok || !worktreePathEqual(info.Path, again.Path) {
		t.Fatalf("用既有分支建出的现场必须在册：%+v ok=%v", info, ok)
	}
}
