package seelebridge

// 回归守卫：teammate 的「工作区归属」必须落在它自己那份 worktree 上（不是 main）。
//
// 2026-10-05 缺陷：同一个现场有两个名字，建现场用「裸 nodeID」登记，worker 回合开始却拿
// 「带 `seelex/` 前缀的指派名」去查 → 查空 → 静默回退主会话项目根 → teammate 的工具全落在
// main 上（现场建出来却从未被用过）。本用例钉住「问的键」与「登记的键」必须是同一个。
//
// 代码面事实：
//   - 工作项派发时派生指派名 `seelex/<role>-<item>`（teamwork.WorkItemWorktreeName），
//     它就是 WorkspaceBinding.Worktree，同一条进了计划与账本，也进了 WorkerRequest.Worktree；
//   - Runtime.BindWorkspace 建现场时用 workItemNodeID（去掉 `seelex/` 前缀）作为
//     WorktreeManager 的注册键 → 注册表里是裸 nodeID `<role>-<item>`；
//   - worker 回合开始处（RunWorker → bindWorkerProjectRoot）必须做同一条换算，否则
//     "一个 Work Item 一个 worktree" 的隔离不成立。

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkItemWorktreeIsBoundAsTeammateRoot(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}

	binding := workItemBinding("sess-root", "exec", "wi-root")
	bound, err := runtime.BindWorkspace(context.Background(), binding)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if bound.Path == "" {
		t.Fatal("git 仓库里必须建出 worktree 现场")
	}
	if _, ok := runtime.worktreeMgr.Info("exec-wi-root"); !ok {
		t.Fatalf("现场应登记在裸 nodeID 下（指派名 %q）", bound.Worktree)
	}

	// worker 回合开始：把角色会话的工具根绑到这一轮该看到的工作区。
	runtime.bindWorkerProjectRoot("sess-root", bound.SessionID, bound.Worktree)

	got := runtime.projectScope.RootFor(bound.SessionID)
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(bound.Path)) {
		t.Fatalf("teammate 工具根必须落在自己的 worktree：\n got  = %q\n want = %q\n main = %q\n（got==main 即「归属总是去到 main」）",
			got, bound.Path, repo)
	}

	// 工具真的按这条根解析（read_file/write_file/bash 都走 resolveNodePath →
	// ProjectScope.ResolveWriteFor(会话键)）：相对路径必须落在 worktree 内，而不是 main。
	resolved, err := runtime.projectScope.ResolveWriteFor(bound.SessionID, "wi-note.txt")
	if err != nil {
		t.Fatalf("解析写路径: %v", err)
	}
	rel, err := filepath.Rel(filepath.Clean(bound.Path), resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("write 路径必须落在 worktree 内：resolved=%q worktree=%q（落在 main 即隔离不成立）", resolved, bound.Path)
	}
}
