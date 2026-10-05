package worktree

// worktree_registration_sources_test.go — U2：**teammate 现场的两个登记来源**不许把同一件事
// 登记成两样。
//
// 原始问题（docs/arch/workunit-duplication-inventory.md §四 U2）：同一个队友现场有两个登记
// 来源——① 记录投影：`Restore(records)` 读 NodeSessionStore（teammate 的 `saveTeamUnitRecord`
// 往**同一张表**写 `Worktree` 一栏）；② 计划+账本：`adoptTeamworkScenes` → `Adopt`。注册表的键
// 是 nodeID、现场目录名/分支名也只按 nodeID 拼（`sceneDirName`），所以两个来源指向**同一个
// 目录**；"哪一份先到决定注册表内容"就是这三条用例要钉的事。
//
//	① 两个来源、两种顺序：注册表里都只有**一条**登记，且指向同一个现场（幂等），现场本身
//	   （目录 / 未提交产出 / `seelex/<nodeID>` 分支）一件都不许少——这条路径上一句
//	   `worktree remove --force` 都不许出现；
//	② 已有在册（活）登记时，`Restore` 不得用记录里**缺失的栏位**把它降级：teammate 记录只写
//	   Path/Branch（`teamUnitWorktreeRecord`），MainBranch/BaseCommit 天生是空的——而这两栏正是
//	   收尾要用的（`alignMergeTarget` 读 MainBranch 决定合回哪条分支；变基读 BaseCommit）；
//	③ 注册表为空的"重启"场景下 `Restore` 仍要能重建登记（首判复用不许把恢复语义弄丢），
//	   且"目录不在不登记"的防幽灵语义仍在。
//
// 真 git（`reproGitRepo`）：这三条要证的正是 git 自己的登记与分支行为，fakeGit 会把要证的事
// 假设成假的。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// sceneRecordFor 造一条**记录投影来源**的会话记录（形态与 teammate 落盘一致）。
func sceneRecordFor(nodeID string, worktree sessionstore.NodeWorktreeRecord) sessionstore.NodeSessionRecord {
	return sessionstore.NodeSessionRecord{
		SchemaVersion: sessionstore.NodeSessionSchemaVersion,
		NodeID:        nodeID,
		Worktree:      worktree,
	}
}

// TestSceneRegistrationSourcesAgreeInBothOrders —— ① 两个来源、两种顺序，只有一条登记。
func TestSceneRegistrationSourcesAgreeInBothOrders(t *testing.T) {
	for _, order := range []string{"先认领后恢复", "先恢复后认领"} {
		t.Run(order, func(t *testing.T) {
			root := reproGitRepo(t)
			nodeID := "exec-wi-sources"

			// 旧进程建出现场，并在里面留下未提交产出（现场是人的资产）。
			created := sceneManager(root).BeginNamed(nodeID)
			if created == nil {
				t.Fatal("真实 git 仓库里应能建出现场")
			}
			produced := filepath.Join(created.Path, "produced.txt")
			if err := os.WriteFile(produced, []byte("未提交的产出\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			// 重启：新管理器 = 空注册表，随后两个来源各跑一次（顺序互换）。
			restarted := sceneManager(root)
			record := sceneRecordFor(nodeID, sessionstore.NodeWorktreeRecord{
				Path: created.Path, Branch: created.Branch,
				MainBranch: created.MainBranch, BaseCommit: created.BaseCommit,
			})
			switch order {
			case "先认领后恢复":
				restarted.Adopt(nodeID)
				restarted.Restore([]sessionstore.NodeSessionRecord{record})
			case "先恢复后认领":
				restarted.Restore([]sessionstore.NodeSessionRecord{record})
				restarted.Adopt(nodeID)
			}

			if restarted.RegisteredCount() != 1 {
				t.Fatalf("同一 nodeID 只该有一条登记，实际 %d 条", restarted.RegisteredCount())
			}
			info, ok := restarted.Info(nodeID)
			if !ok || !worktreePathEqual(info.Path, created.Path) {
				t.Fatalf("两个来源必须指向同一个现场：got %+v want %q", info, created.Path)
			}
			// 现场本身不许被这条路径动过：目录、未提交产出、分支三件都在。
			if _, err := os.Stat(produced); err != nil {
				t.Fatalf("两个来源跑一遍不得丢掉现场里的未提交产出：%v", err)
			}
			if out, err := GitRunner(root, "branch", "--list", created.Branch); err != nil || !strings.Contains(out, created.Branch) {
				t.Fatalf("分支 %q 不得被删：out=%q err=%v", created.Branch, out, err)
			}
		})
	}
}

// TestRestoreDoesNotDowngradeLiveRegistration —— ② 记录里缺的栏位不许把活登记降级。
//
// 收尾要用的事实是 MainBranch（合回哪条分支）与 BaseCommit（变基的落地目标）。记录是 teammate
// 形态时这两栏是空的（`teamUnitWorktreeRecord` 只写 Path/Branch），而"记录里没有"不等于"这件事
// 没有"——本进程在册的那一份才是活的、更准的那一份。
func TestRestoreDoesNotDowngradeLiveRegistration(t *testing.T) {
	root := reproGitRepo(t)
	nodeID := "exec-wi-live"

	mgr := sceneManager(root)
	live := mgr.BeginNamed(nodeID)
	if live == nil {
		t.Fatal("真实 git 仓库里应能建出现场")
	}
	if live.MainBranch == "" || live.BaseCommit == "" {
		t.Fatalf("前置失败：活登记必须带着收尾要用的事实：%+v", live)
	}

	// 会话恢复链在同一个进程里跑一次（同一个管理器、这个 nodeID 已在册）。
	mgr.Restore([]sessionstore.NodeSessionRecord{sceneRecordFor(nodeID, sessionstore.NodeWorktreeRecord{
		Path: live.Path, Branch: live.Branch,
	})})

	info, ok := mgr.Info(nodeID)
	if !ok {
		t.Fatal("现场必须在册")
	}
	if info.MainBranch != live.MainBranch {
		t.Fatalf("记录里缺的栏位不得覆盖在册事实：MainBranch got %q want %q（收尾会静默改成「合进当前 HEAD」）",
			info.MainBranch, live.MainBranch)
	}
	if registered := mgr.WorktreeForNode(nodeID); registered == nil || registered.BaseCommit != live.BaseCommit {
		t.Fatalf("BaseCommit（变基目标）同理不得被空栏位覆盖：%+v", registered)
	}
	if registered := mgr.WorktreeForNode(nodeID); registered == nil || !worktreePathEqual(registered.Path, live.Path) {
		t.Fatalf("现场路径不得被改写：%+v", registered)
	}
}

// TestRestoreStillRebuildsRegistrationAfterRestart —— ③ 首判复用不许弄丢"重启恢复"本身。
func TestRestoreStillRebuildsRegistrationAfterRestart(t *testing.T) {
	root := reproGitRepo(t)
	nodeID := "exec-wi-restart"

	created := sceneManager(root).BeginNamed(nodeID)
	if created == nil {
		t.Fatal("真实 git 仓库里应能建出现场")
	}

	restarted := sceneManager(root)
	if restarted.RegisteredCount() != 0 {
		t.Fatal("前置条件：重启后的注册表必须是空的")
	}
	restarted.Restore([]sessionstore.NodeSessionRecord{sceneRecordFor(nodeID, sessionstore.NodeWorktreeRecord{
		Path: created.Path, Branch: created.Branch,
		MainBranch: created.MainBranch, BaseCommit: created.BaseCommit,
	})})

	info, ok := restarted.Info(nodeID)
	if !ok {
		t.Fatal("空注册表上 Restore 必须把现场重建进注册表（重启恢复就靠它）")
	}
	if !worktreePathEqual(info.Path, created.Path) || info.MainBranch != created.MainBranch {
		t.Fatalf("记录是持久化事实，重建时按记录原样进来：got %+v want path=%q main=%q", info, created.Path, created.MainBranch)
	}

	// 防幽灵语义仍在：目录不在 = 不登记（否则 Info 会报一个不存在的现场）。
	ghost := sceneManager(root)
	ghost.Restore([]sessionstore.NodeSessionRecord{sceneRecordFor("exec-wi-ghost", sessionstore.NodeWorktreeRecord{
		Path: filepath.Join(filepath.Dir(root), "exec-wi-ghost-does-not-exist"), Branch: "seelex/exec-wi-ghost",
	})})
	if _, ok := ghost.Info("exec-wi-ghost"); ok {
		t.Fatal("目录不在的现场不得被登记（幽灵条目会让 team_close 对着不存在的路径跑 git status）")
	}
}
