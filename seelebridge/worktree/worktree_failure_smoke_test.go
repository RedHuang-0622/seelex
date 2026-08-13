package worktree

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// ── 工作区现场触发路径冒烟 ────────────────────────────────────────
// 「工作区现场（节点失败或合并被拒）」= worktree 注册表在失败后保留
// （NodeWorktreeInfoFor 可查）。本文件用 fakeGit 注入逐条验证 B 类
// 收尾失败路径（A 类 agent.Chat 失败见 merge_back/fork 冒烟测试）：
//   B1 rebase 冲突、B2 脏未提交、B3 merge 被拒、B4 merge 冲突。
// 每条断言：Finish 返回明确错误 + Info 仍可查（现场保留）。

// TestWorktreeScenePreservedOnAllFinishFailures 表驱动冒烟：所有 worktree
// 收尾失败路径都必须保留现场（Info 可查）且返回可识别的错误。
func TestWorktreeScenePreservedOnAllFinishFailures(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")

	// B1: rebase 冲突（branchBehindBase=true 且 rebase 失败）。
	rebaseConflict := func(mgr *WorktreeManager, fake *fakeGit) {
		fake.reply["rev-list --count HEAD..main"] = "1"
		fake.err["rebase main"] = &gitTestErr{}
		fake.reply["diff --name-only --diff-filter=U"] = "conflict.py"
	}
	// B2: worktree 脏且未提交。
	dirtyUncommitted := func(mgr *WorktreeManager, fake *fakeGit) {
		fake.reply["rev-list --count abc123..HEAD"] = "0"
		fake.reply["rev-list --count"] = "0"
		fake.reply["status --porcelain"] = " M produced.txt"
	}
	cases := []struct {
		name        string
		configure   func(*WorktreeManager, *fakeGit)
		wantErrPart string
	}{
		{"B1 rebase 冲突", rebaseConflict, "rebase onto"},
		{"B2 脏未提交", dirtyUncommitted, "uncommitted changes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, fake, gate := newTestWorktreeManager(root)
			gate.choice = "approve"
			tc.configure(mgr, fake)
			wt := mgr.Begin(model.NodeScope{NodeID: "n1", Role: model.RoleSubAgent, BranchID: "n1"}, "n1")
			if wt == nil {
				t.Fatal("begin must succeed with fake git")
			}
			err := mgr.Finish(context.Background(), "n1", wt)
			if err == nil || !strings.Contains(err.Error(), tc.wantErrPart) {
				t.Fatalf("Finish err = %v, want contains %q", err, tc.wantErrPart)
			}
			if _, ok := mgr.Info("n1"); !ok {
				t.Fatal("失败路径必须保留 worktree 现场（Info 可查）")
			}
		})
	}
}

// TestWorktreeScenePreservedOnMergeConflict B4: 主工作区 merge 冲突。
func TestWorktreeScenePreservedOnMergeConflict(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	mgr, fake, gate := newTestWorktreeManager(root)
	gate.choice = "approve"
	fake.err["merge --no-edit seelex/n1"] = &gitTestErr{}
	fake.reply["diff --name-only --diff-filter=U"] = "conflict.py"

	wt := mgr.Begin(model.NodeScope{NodeID: "n1", Role: model.RoleSubAgent, BranchID: "n1"}, "n1")
	if wt == nil {
		t.Fatal("begin must succeed")
	}
	err := mgr.Finish(context.Background(), "n1", wt)
	if err == nil || !strings.Contains(err.Error(), "merge") {
		t.Fatalf("merge conflict must error, got %v", err)
	}
	if _, ok := mgr.Info("n1"); !ok {
		t.Fatal("merge 冲突后现场必须保留")
	}
}

// TestWorktreeSceneReleasedOnSuccess 对照：成功路径 Finish 只清理 worktree
// 目录（cleanup），注册表由调用方（agent_node.Run 成功分支）Release；
// Release 后 Info 不可查——证明「现场保留」确实是失败专属信号。
func TestWorktreeSceneReleasedOnSuccess(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	mgr, fake, gate := newTestWorktreeManager(root)
	gate.choice = "approve"
	wt := mgr.Begin(model.NodeScope{NodeID: "n1", Role: model.RoleSubAgent, BranchID: "n1"}, "n1")
	if wt == nil {
		t.Fatal("begin must succeed")
	}
	if err := mgr.Finish(context.Background(), "n1", wt); err != nil {
		t.Fatalf("success finish must not error: %v", err)
	}
	if !containsCall(fake.snapshot(), "worktree remove --force "+wt.Path) {
		t.Fatal("成功路径必须清理 worktree 目录")
	}
	// 调用方成功分支 Release 后现场不可查。
	mgr.Release("n1")
	if _, ok := mgr.Info("n1"); ok {
		t.Fatal("Release 后现场必须不可查")
	}
}

// TestWorktreeSceneSurvivesRelease 验证 Release 只由成功路径调用：
// 失败后未 Release，Info 持续可查；Release 后不可查。
func TestWorktreeSceneSurvivesRelease(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	mgr, fake, _ := newTestWorktreeManager(root)
	fake.reply["status --porcelain"] = " M produced.txt"
	fake.reply["rev-list --count abc123..HEAD"] = "0"
	fake.reply["rev-list --count"] = "0"
	wt := mgr.Begin(model.NodeScope{NodeID: "n1", Role: model.RoleSubAgent, BranchID: "n1"}, "n1")
	if wt == nil {
		t.Fatal("begin must succeed")
	}
	_ = mgr.Finish(context.Background(), "n1", wt) // 失败，现场保留
	if _, ok := mgr.Info("n1"); !ok {
		t.Fatal("失败后现场应保留")
	}
	mgr.Release("n1")
	if _, ok := mgr.Info("n1"); ok {
		t.Fatal("Release 后现场应不可查")
	}
}
