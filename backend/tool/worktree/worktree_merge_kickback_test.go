package worktree

// worktree_merge_kickback_test.go — 红灯用例：**后一个单元做 merge back 时，不许把
// 先前已经合回来的那件「踢成另一个分支」**。
//
// 现象（待验证的目击描述）：两个单元 A、B 各自在自己现场里提交，先收尾 A
// （rebase → 提交判定 → 审批 → merge 回主分支 → cleanup），再收尾 B；收尾 B 之后
// 主分支上只剩 B，A 已经合回来的那件不见了 / 落到了另一个分支上。
//
// 复现条件（任务给的三条线索里的第三条：「merge 发生在主工作区当前分支不是 main 的
// 时机」）：**主工作区的当前分支在两次收尾之间漂走**。这在真实现场里并不罕见——
// leader 自己切分支、另一次会话/人工把项目根绑到别的工作区、人工 rebase 都会让它发生；
// 而现场在 begin 时就把"这次收尾要合到哪条分支"记下来了（`NodeWorktree.MainBranch`）。
//
// 用例要证的是 git 自己的事实（分支指针、祖先关系、现场目录），所以用**真实 git 仓 +
// 真 worktree**（`reproGitRepo`）：fakeGit 会把要证的那件事假设成假的。夹具沿用
// worktree_merge_serial_test.go：同一个 WorktreeManager、同一个总是批准的审批门，
// 只是把收尾从"并发"换成"先后"。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
)

// mergeKickbackManager 造一个绑定到 root 的收尾管理器（真实 git + 总是批准的审批门）。
func mergeKickbackManager(t *testing.T, root string) *WorktreeManager {
	t.Helper()
	gate := &approveGateStub{choice: "approve"}
	mgr := NewWorktreeManager(WorktreeManagerDeps{
		Root:  func() string { return root },
		Phase: func(context.Context, string, string) {},
		Gate:  func() approve.ApprovalGate { return gate },
	})
	t.Cleanup(mgr.Close)
	return mgr
}

// commitProduced 在 dir 里落一份产出并提交，返回这次提交的 sha。
func commitProduced(t *testing.T, dir, name, content, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", message}} {
		if _, err := GitRunner(dir, args...); err != nil {
			t.Fatalf("git %v（cwd=%s）：%v", args, dir, err)
		}
	}
	out, err := GitRunner(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(out)
}

// checkoutIn 在主工作区切分支（模拟"主工作区的当前分支漂走"）。
func checkoutIn(t *testing.T, root, branch string) {
	t.Helper()
	if _, err := GitRunner(root, "checkout", branch); err != nil {
		t.Fatalf("主工作区 checkout %s 失败：%v", branch, err)
	}
}

// refSha 读一个 ref 的 sha（不存在 → 空串）。
func refSha(root, ref string) string {
	out, err := GitRunner(root, "rev-parse", "--verify", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// isAncestor 报告 ancestor 是否还在 descendant 的历史里（判"产出有没有被踢走"）。
func isAncestor(root, ancestor, descendant string) bool {
	_, err := GitRunner(root, "merge-base", "--is-ancestor", ancestor, descendant)
	return err == nil
}

// fileInRoot 读主工作区里某个文件（不存在 → 空串）。
func fileInRoot(root, name string) string {
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// TestFinishSecondUnitKeepsFirstMerge 钉住"两个单元先后收尾"的三条判据：
//
//	① 主分支同时含 A、B 两件产出；
//	② A 的分支指针与 A 已合并的提交不被改写、不被移出主分支；
//	③ A 的现场清理不影响到 B（目录 / 分支指针）。
func TestFinishSecondUnitKeepsFirstMerge(t *testing.T) {
	root := reproGitRepo(t)
	mgr := mergeKickbackManager(t, root)

	const nodeA, nodeB = "unit-a", "unit-b"
	wtA := mgr.BeginNamed(nodeA)
	wtB := mgr.BeginNamed(nodeB)
	if wtA == nil || wtB == nil {
		t.Fatal("真 git 仓里两个单元都必须开得出自己的现场")
	}
	if wtA.MainBranch != "main" || wtB.MainBranch != "main" {
		t.Fatalf("前置：两条现场都从 main 切出，记录的主分支 = %q / %q", wtA.MainBranch, wtB.MainBranch)
	}
	aCommit := commitProduced(t, wtA.Path, "a.txt", "A 的产出\n", "seelex/"+nodeA+": 产出 a")
	bCommit := commitProduced(t, wtB.Path, "b.txt", "B 的产出\n", "seelex/"+nodeB+": 产出 b")

	// 收尾 A 之前主工作区的当前分支漂到 side：现场记录的目标分支仍是 main。
	// 收尾段必须把改动合回**现场记录的那条分支**（变基目标 / 落后判定读的都是它），
	// 不能"就近"合进主工作区此刻碰巧所在的分支——否则同批产出就散到不同分支上去了。
	if _, err := GitRunner(root, "checkout", "-b", "side"); err != nil {
		t.Fatalf("主工作区切到 side 失败：%v", err)
	}

	// ── 先收尾 A：rebase → 提交判定 → 审批 → merge 回记录的主分支 → cleanup ──
	if err := mgr.Finish(context.Background(), nodeA, wtA); err != nil {
		t.Fatalf("收尾 A 必须成功：%v", err)
	}
	if !isAncestor(root, aCommit, "main") {
		sideSha := refSha(root, "refs/heads/side")
		mainSha := refSha(root, "refs/heads/main")
		t.Fatalf("收尾 A 之后主分支 main 必须已含 A 的提交 %s：main=%s side=%s（现场记录的 MainBranch=%q）——A 被合进了主工作区当前所在的分支，而不是它自己被切出来的那条",
			aCommit, mainSha, sideSha, wtA.MainBranch)
	}
	// ③ A 的现场清理不许碰到 B 的现场与 B 的分支指针。
	if _, err := os.Stat(wtB.Path); err != nil {
		t.Fatalf("A 的现场清理不得动到 B 的现场：%v", err)
	}
	if got := refSha(root, "refs/heads/seelex/"+nodeB); got != bCommit {
		t.Fatalf("A 的清理不得改写 B 的分支指针：seelex/%s = %q, want %q", nodeB, got, bCommit)
	}

	// 主工作区切回 main，再收尾 B。
	checkoutIn(t, root, "main")
	if err := mgr.Finish(context.Background(), nodeB, wtB); err != nil {
		t.Fatalf("收尾 B 必须成功（B 自己 rebase 到含 A 的主分支）：%v", err)
	}

	// ① 主分支同时含 A、B 两件产出。
	if got := fileInRoot(root, "a.txt"); got != "A 的产出" {
		t.Fatalf("收尾 B 之后 A 的产出从主工作区消失了：%q", got)
	}
	if got := fileInRoot(root, "b.txt"); got != "B 的产出" {
		t.Fatalf("收尾 B 之后主工作区必须看得见 B 的产出：%q", got)
	}

	// ② A 已合并的提交不被改写、不被移出主分支；A 的分支指针要么已被清理，
	//    要么仍指向当初那一次提交——绝不允许被挪到别的提交上。
	mainSha := refSha(root, "refs/heads/main")
	if !isAncestor(root, aCommit, mainSha) {
		t.Fatalf("收尾 B 把 A 已合并的提交 %s 踢出了主分支：main=%s side=%s",
			aCommit, mainSha, refSha(root, "refs/heads/side"))
	}
	if got := refSha(root, "refs/heads/seelex/"+nodeA); got != "" && got != aCommit {
		t.Fatalf("A 的分支指针被改写到 %q（既不是已合并的 %s，也不是已清理）", got, aCommit)
	}

	// 两个现场都收干净了（成功路径 = 目录回收）。
	for _, wt := range []*NodeWorktree{wtA, wtB} {
		if _, err := os.Stat(wt.Path); err == nil {
			t.Fatalf("成功收尾后现场必须被回收：%s", wt.Path)
		}
	}
}
