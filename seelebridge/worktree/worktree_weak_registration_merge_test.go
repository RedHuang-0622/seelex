package worktree

// worktree_weak_registration_merge_test.go — U2 第三半（「弱登记先到」）+ 审计 §5 路径 1：
// **teammate 记录只带 Path/Branch 时，收尾「要合回哪条分支」这件事没有来源**。
//
// 现场有两个登记来源：① 记录投影 `Restore(records)`（读 NodeSessionStore，teammate 的
// `saveTeamUnitRecord` 往同一张表写 `Worktree` 一栏）；② 计划/账本 `Adopt`。恢复链的顺序是
// **固定的**（`runtime_subagent_recovery.go`：`Restore` 先、`adoptTeamworkScenes` → `Adopt` 后），
// 而 teammate 记录只写 Path/Branch（`workunit_team_records.go` 的 `teamUnitWorktreeRecord`）
// ⇒ **先到的是「缺栏」的那一份**，`Adopt` 进门被 `existing != nil` 挡回。缺的两栏正是收尾要用
// 的事实：`MainBranch`（这次合回哪条分支 = M2 的判据）与 `BaseCommit`（变基/提交判定的基线）。
//
// 两条用例：
//
//	① `TestWeakSceneRegistrationRefusesToMergeBack` —— 弱登记先到之后收尾这条路。
//	   红灯实测（原文见 _logs/wi6_red.txt，**与审计 §5 的写法不同，以实测为准**）：本机 git 对
//	   空的一侧**不报错**——`git rev-list --count "HEAD.."` 与 `"..HEAD"` 都返回 `0`（exit 0）。
//	   于是缺栏的现场被读成「没有落后、没有提交」：`alignMergeTarget` 仍静默 return nil，
//	   `Finish` **报成功**，一路走到 `cleanup`——现场目录与 `seelex/<nodeID>` 分支被删、
//	   已提交的产出一个字节都没合回 main（静默丢产出，而不是「现场保留」）。
//	   本用例断言**收口后的形状**：缺栏一律显式发声（且点明缺哪一栏）、空 ref 一个都不许拼、
//	   现场/分支/产出/主工作区四处原样。
//	② `TestAdoptFirstFillsSceneColumnsSoFinishMergesBack` —— 反证对照：把「记录投影」这一步
//	   拿掉（只有计划/账本认领先到）时，同一个现场的两栏是**齐的**、收尾照常合回 main。
//	   ⇒ 缺栏的根在「弱登记先到 + 已在册不刷新」，不在 `Adopt` 的推导。
//
// 要证的是 git 自己的事实（ref 实参、分支指针、祖先关系），所以用**真实 git 仓 + 真
// worktree**（`reproGitRepo`）：fakeGit 会把要证的那件事假设成假的。

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// gitCallLog 记录交给 git 的实参（诊断「实现到底拼出了什么字符串」）。
// 收尾跑在单写者 goroutine 上，因此读写都要上锁。
type gitCallLog struct {
	mu    sync.Mutex
	lines []string
}

// wrap 把真实 git 执行器包一层（记录后转调）。
func (l *gitCallLog) wrap(real gitFn) gitFn {
	return func(dir string, args ...string) (string, error) {
		l.mu.Lock()
		l.lines = append(l.lines, strings.Join(args, " "))
		l.mu.Unlock()
		return real(dir, args...)
	}
}

func (l *gitCallLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// emptyRefArgs 找出「被拼空的 ref」（`HEAD..` / `..HEAD` 这一类：`..` 的左边或右边是空的）。
// 合法的 `HEAD..main` 不算——判据是**空的一侧**，不是 `..` 本身。
func emptyRefArgs(lines []string) []string {
	var bad []string
	for _, line := range lines {
		for _, token := range strings.Fields(line) {
			if strings.HasSuffix(token, "..") || strings.HasPrefix(token, "..") {
				bad = append(bad, token)
			}
		}
	}
	return bad
}

// weakSceneRecord 造一条**teammate 落盘形态**的记录：现场栏只有 Path/Branch，
// MainBranch/BaseCommit 空——与 `teamUnitWorktreeRecord` 写出的形状逐字一致。
func weakSceneRecord(nodeID, path, branch string) sessionstore.NodeSessionRecord {
	return sceneRecordFor(nodeID, sessionstore.NodeWorktreeRecord{Path: path, Branch: branch})
}

// weakSceneFixture 搭出「旧进程建现场 + 现场里一条已提交产出 + 主工作区自己也在前进」的现场：
// 旧进程收工后重启，按恢复链顺序把**弱登记**（记录投影）先登记进新管理器。
func weakSceneFixture(t *testing.T, nodeID string) (string, *WorktreeManager, *gitCallLog, *NodeWorktree, string, string) {
	t.Helper()
	root := reproGitRepo(t)

	old := sceneManager(root)
	created := old.BeginNamed(nodeID)
	if created == nil {
		t.Fatal("真实 git 仓库里应能建出现场")
	}
	if created.MainBranch == "" || created.BaseCommit == "" {
		t.Fatalf("前置：活登记必须带全两栏（否则本用例的前提不成立）：%+v", created)
	}
	sceneCommit := commitProduced(t, created.Path, "produced.txt", "现场产出\n", "seelex/"+nodeID+": 产出")
	// 主工作区同时前进一条提交：模拟「工作途中主工作区在动」（分叉背景）。
	mainCommit := commitProduced(t, root, "main-only.txt", "主工作区自己的产出\n", "main: 与现场无关的一条提交")
	old.Close()

	log := &gitCallLog{}
	mgr := sceneManager(root)
	t.Cleanup(mgr.Close)
	mgr.git = log.wrap(mgr.git)
	// 恢复链的固定顺序：记录投影（弱登记）先到。
	mgr.Restore([]sessionstore.NodeSessionRecord{weakSceneRecord(nodeID, created.Path, created.Branch)})
	return root, mgr, log, created, sceneCommit, mainCommit
}

// TestWeakSceneRegistrationRefusesToMergeBack —— ① 缺栏登记的收尾必须**显式**拒绝：
// 不静默维持「合进当前 HEAD」，也不把空栏拼进 ref 字符串。
func TestWeakSceneRegistrationRefusesToMergeBack(t *testing.T) {
	const nodeID = "exec-wi-weak"
	root, mgr, log, created, sceneCommit, mainCommit := weakSceneFixture(t, nodeID)

	// 计划/账本那份后到：被「已在册」挡回，注册表里留着的仍是弱登记。
	adopted := mgr.Adopt(nodeID)
	info, ok := mgr.Info(nodeID)
	t.Logf("① 弱登记先到：Info=%+v ok=%v；Adopt 拿到的那一份=%+v", info, ok, adopted)
	if !ok || !worktreePathEqual(info.Path, created.Path) {
		t.Fatalf("前置：弱登记必须把现场登记进来（在册才挡得住 Adopt）：%+v ok=%v", info, ok)
	}
	if info.MainBranch != "" {
		t.Fatalf("前置：弱登记就是缺栏的那一份（MainBranch 空），实际 %q", info.MainBranch)
	}
	if adopted == nil || adopted.MainBranch != "" {
		t.Fatalf("Adopt 被「已在册」挡回后拿到的仍应是弱登记：%+v", adopted)
	}

	// ── 实测读数（红灯先行：下面几行就是「现状原文」）──
	wt := mgr.WorktreeForNode(nodeID)
	behind, behindErr := mgr.branchBehindBase(nodeID, wt)
	commits, commitsErr := mgr.commitCountSince(nodeID, wt)
	alignErr := mgr.alignMergeTarget(root, nodeID, wt)
	t.Logf("② 实测 branchBehindBase → behind=%v err=%v", behind, behindErr)
	t.Logf("② 实测 commitCountSince → commits=%d err=%v", commits, commitsErr)
	t.Logf("② 实测 alignMergeTarget → err=%v（nil = 静默维持「合进当前 HEAD」）", alignErr)
	t.Logf("② 实测实现拼给 git 的实参 = %v", log.snapshot())
	finishErr := mgr.Finish(context.Background(), nodeID, wt)
	t.Logf("③ 实测 Finish → %v", finishErr)

	// ── 收口后的形状（本用例的断言）──
	// 判据侧不许静默降级：无目标时必须显式发声。
	if alignErr == nil {
		t.Errorf("alignMergeTarget 对缺栏登记返回 nil：M2（合并前把主工作区切回现场记录的那条分支）在这条路上等于没装")
	}
	if finishErr == nil {
		t.Errorf("缺栏登记的收尾必须显式失败（现场 %s），实际 Finish 返回 nil", created.Path)
	} else {
		if !errors.Is(finishErr, errSceneFactsIncomplete) {
			t.Errorf("缺栏失败必须可分类（errors.Is errSceneFactsIncomplete），实际 %v", finishErr)
		}
		if !strings.Contains(finishErr.Error(), "MainBranch") {
			t.Errorf("诊断必须点明缺的是哪一栏（MainBranch），实际 %v", finishErr)
		}
		if strings.Contains(finishErr.Error(), "ambiguous argument") {
			t.Errorf("报的是 git 的 ambiguous argument（空栏被拼进了 ref），不是「登记缺栏」这句话：%v", finishErr)
		}
	}
	// 空栏一个都不许拼进 ref（`HEAD..` / `..HEAD`）。
	if bad := emptyRefArgs(log.snapshot()); len(bad) > 0 {
		t.Errorf("空栏被拼成了 ref 字符串：%v（实参全表 %v）", bad, log.snapshot())
	}

	// 人的资产四处都要原样：现场目录、现场分支指针、主分支、主工作区产出。
	if _, err := os.Stat(created.Path); err != nil {
		t.Errorf("失败路径必须保留现场：%v", err)
	}
	if got := refSha(root, "refs/heads/"+created.Branch); got != sceneCommit {
		t.Errorf("现场分支指针不得被动：got %q want %q", got, sceneCommit)
	}
	if got := refSha(root, "refs/heads/main"); got != mainCommit {
		t.Errorf("主分支不得被改写：got %q want %q", got, mainCommit)
	}
	if isAncestor(root, sceneCommit, "main") {
		t.Errorf("缺栏登记的产出被合进了 main（静默降级成「合进当前 HEAD」）")
	}
	if got := fileInRoot(root, "produced.txt"); got != "" {
		t.Errorf("缺栏登记不得把产出合进主工作区：produced.txt=%q", got)
	}
	if _, ok := mgr.Info(nodeID); !ok {
		t.Errorf("失败后现场登记必须保留（人工恢复入口）")
	}
}

// TestAdoptFirstFillsSceneColumnsSoFinishMergesBack —— ② 反证对照。
//
// 同一个现场、同一条收尾链，只把「记录投影」那一步拿掉（只有计划/账本的 `Adopt` 先到）：
// 两栏由 `Adopt` 从 git 现算（`MainBranch` = 主工作区当前分支，`BaseCommit` = merge-base），
// 收尾照常把产出合回 main。⇒ 缺栏这件事的根在「弱登记先到」，不在 `Adopt` 的推导。
func TestAdoptFirstFillsSceneColumnsSoFinishMergesBack(t *testing.T) {
	const nodeID = "exec-wi-adopt-first"
	root := reproGitRepo(t)

	old := sceneManager(root)
	created := old.BeginNamed(nodeID)
	if created == nil {
		t.Fatal("真实 git 仓库里应能建出现场")
	}
	sceneCommit := commitProduced(t, created.Path, "produced.txt", "现场产出\n", "seelex/"+nodeID+": 产出")
	old.Close()

	// 重启：空注册表，只有计划/账本这一份来源（没有 Restore 的记录投影）。
	mgr := mergeKickbackManager(t, root)
	adopted := mgr.Adopt(nodeID)
	if adopted == nil {
		t.Fatal("既有现场必须能被认领")
	}
	t.Logf("② 对照：Adopt 补出的两栏 MainBranch=%q BaseCommit=%q", adopted.MainBranch, adopted.BaseCommit)
	if adopted.MainBranch == "" || adopted.BaseCommit == "" {
		t.Fatalf("Adopt 认领时必须把收尾要用的两栏现算出来：%+v", adopted)
	}
	if err := mgr.Finish(context.Background(), nodeID, adopted); err != nil {
		t.Fatalf("认领补栏之后收尾必须成功：%v", err)
	}
	if !isAncestor(root, sceneCommit, "main") {
		t.Fatalf("产出必须合回 main（现场记录的 MainBranch=%q）", adopted.MainBranch)
	}
	if got := fileInRoot(root, "produced.txt"); got != "现场产出" {
		t.Fatalf("主工作区应看得见现场产出：%q", got)
	}
}
