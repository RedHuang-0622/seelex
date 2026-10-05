package worktree

// worktree_release_judgment_test.go — 钉住「现场释放」只有一份判据。
//
// 现场释放今天有两条入口、两套语义：
//
//	验收释放（leader 的 team_accept）：AcceptItem → releaseItem → ReleaseWorkspaceItem
//	    → 包级 `CleanupWorktree(root, wt)`      ← 幂等：目录/分支/git 登记任一不在 = 已释放
//	收尾自动释放（worker 跑完的尾插）：Finish 的成功路径（无提交且干净 / 合并成功）
//	    → 组件 `w.cleanup(root, wt)`            ← 非幂等：照旧跑 `git worktree remove --force`
//
// 2026-10-03 缺陷 A 的形状正是"上游先收走、下游再动一次手"：尾插已经把现场
// remove 掉（却没配一次 Release，注册表与账本里的绑定还在），随后验收释放对同一份
// 绑定再动手一次——**幂等口径当时只落在包级那一份**，于是一侧报错、工作项卡在
// review、下游里程碑闸门打不开。判据重复的代价就是"同一个现场，两条入口两个结论"。
//
// 本用例把两条入口摆在一起，对**同一份现场形状**要求同一个结论，并用真实 git
// （fake git 会照着脚本返回，永远报不出"现场已被收走"的语义，等于把要证的事
// 假设成假的）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
)

// releaseScene 是一份待释放的现场（真实 git 仓库 + 真实 worktree）。
type releaseScene struct {
	root string
	mgr  *WorktreeManager
	wt   *NodeWorktree
}

// TestSceneReleaseJudgmentIsSingleAcrossEntries 对五种现场形状各跑两条入口：
// ① 连续释放两次 ② 目录已被上游收走 ③ 分支已被上游删掉 ④ 目录还在但 git 登记没了
// ⑤ 目录还在且仍登记、remove 失败。要求：(a) 每条入口单独看结论正确；
// (b) 两条入口对同一形状给出**同一个结论**（这就是"只剩一份判据"的用例）。
func TestSceneReleaseJudgmentIsSingleAcrossEntries(t *testing.T) {
	gitIn := func(t *testing.T, root string, args ...string) {
		t.Helper()
		if _, err := GitRunner(root, args...); err != nil {
			t.Fatalf("前置 git %v 失败：%v", args, err)
		}
	}

	newScene := func(t *testing.T) releaseScene {
		t.Helper()
		root := reproGitRepo(t)
		mgr := NewWorktreeManager(WorktreeManagerDeps{
			Root:  func() string { return root },
			Phase: func(context.Context, string, string) {},
			Gate:  func() approve.ApprovalGate { return &approveGateStub{choice: "approve"} },
		})
		t.Cleanup(mgr.Close)
		wt := mgr.BeginNamed("wi-release-judgment")
		if wt == nil {
			t.Fatal("真实 git 仓库里应能建出现场")
		}
		if _, err := os.Stat(wt.Path); err != nil {
			t.Fatalf("前置：现场目录应存在：%v", err)
		}
		return releaseScene{root: root, mgr: mgr, wt: wt}
	}

	entries := []struct {
		name string
		run  func(s releaseScene) error
	}{
		{
			name: "包级 CleanupWorktree（验收释放）",
			run:  func(s releaseScene) error { return CleanupWorktree(s.root, s.wt) },
		},
		{
			name: "组件 w.cleanup（收尾自动释放）",
			run:  func(s releaseScene) error { return s.mgr.cleanup(s.root, s.wt) },
		},
	}

	cases := []struct {
		name string
		// preRelease：先释放一次，再测第二次（幂等）。
		preRelease bool
		prepare    func(t *testing.T, s releaseScene)
		wantErr    bool
	}{
		{
			name:       "① 同一现场连续释放两次：第二次必须是「已释放」",
			preRelease: true,
		},
		{
			name: "② 目录已被上游收走 = 已释放",
			prepare: func(t *testing.T, s releaseScene) {
				if err := os.RemoveAll(s.wt.Path); err != nil {
					t.Fatal(err)
				}
				gitIn(t, s.root, "worktree", "prune")
			},
		},
		{
			// 分支被检出的 worktree 还在时 `git branch -D` 会拒绝（"cannot delete
			// branch ... used by worktree"），所以用 update-ref 直接摘掉引用：形状就是
			// "上游把现场连分支一起收走了，验收释放这一侧只剩绑定"。
			name: "③ 分支已被上游删掉 = 已释放",
			prepare: func(t *testing.T, s releaseScene) {
				gitIn(t, s.root, "update-ref", "-d", "refs/heads/"+s.wt.Branch)
			},
		},
		{
			name: "④ 目录还在但 git 登记没了（prune / 手工移走）= 已释放",
			prepare: func(t *testing.T, s releaseScene) {
				admin := filepath.Join(s.root, ".git", "worktrees", filepath.Base(s.wt.Path))
				if err := os.RemoveAll(admin); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "⑤ 目录还在且仍登记、remove 失败 → 照旧返回 git 原文（不许被幂等口径抹掉）",
			prepare: func(t *testing.T, s releaseScene) {
				gitIn(t, s.root, "worktree", "lock", s.wt.Path)
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome := make(map[string]error, len(entries))
			for _, entry := range entries {
				scene := newScene(t)
				if tc.prepare != nil {
					tc.prepare(t, scene)
				}
				if tc.preRelease {
					// 第一次释放的成败由被测实现自己决定，这里只建立"已经被释放过"的形状。
					_ = entry.run(scene)
				}
				err := entry.run(scene)
				outcome[entry.name] = err
				if gotErr := err != nil; gotErr != tc.wantErr {
					t.Errorf("[%s] err = %v，wantErr = %v", entry.name, err, tc.wantErr)
				}
				if tc.wantErr && err != nil && !strings.Contains(strings.ToLower(err.Error()), "lock") {
					t.Errorf("[%s] ⑤ 必须把 git 原文交回调用方（应含 lock），得到：%v", entry.name, err)
				}
			}
			// 「一份判据」的断言：同一份现场形状，两条入口必须给同一个结论。
			if (outcome[entries[0].name] == nil) != (outcome[entries[1].name] == nil) {
				t.Errorf("两条入口结论不一致（判据不是一份）：%s → %v；%s → %v",
					entries[0].name, outcome[entries[0].name],
					entries[1].name, outcome[entries[1].name])
			}
		})
	}
}
