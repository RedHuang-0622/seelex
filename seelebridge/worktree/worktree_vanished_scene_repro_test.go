package worktree

// worktree_vanished_scene_repro_test.go — 复现 2026-10-04 那条偶发报错：
//
//	worktree 合并失败：git [rev-list --count <base>..HEAD]:
//	    fork/exec G:\Tools\Git\Git\cmd\git.exe: The directory name is invalid
//
// **成因**（一条链上的两个动作对同一份现场并发动手）：
//
//	尾插（worker 跑完自动合并）：SettleWorkItem → MergeWorkspace → WorktreeManager.Finish
//	    → branchBehindBase（git 跑在 wt.Path）→ commitCountSince（git 也跑在 wt.Path）
//	验收释放（leader 的 team_accept）：AcceptItem → releaseItem → ReleaseWorkspaceItem
//	    → CleanupWorktree → `git worktree remove --force <wt.Path>`   ← 把目录删掉
//
// 目录一没，后面那条 git 连子进程都起不来（cwd 不存在），Go 在 CreateProcess 上报
// ERROR_DIRECTORY；**git 自己一句话都没说**（报错里只有 Go 的 fork/exec 文本）。
//
// **报错指向哪一条 git 命令，就是对端在哪一拍动手的指纹**：现场报的是
// `commitCountSince`（Finish 里的第二条），所以释放发生在两条命令**之间**——
// 这正是并发交错，而不是"目录一开始就不在"。本用例把两种时序各跑一遍对照。
//
// 让这条路成立的是 AcceptItem 的判据：review 与 **running 同权**
//（seelebridge/teamwork/items.go 的 `case TeamworkItemReview, TeamworkItemRunning:`）。
// 工作项在跑 = 作业还在飞 = 尾插还在同一条链上。

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
)

// TestWorktreeFinishReportsVanishedScene 复现"现场被对端收走"的报错形状。
func TestWorktreeFinishReportsVanishedScene(t *testing.T) {
	root := reproGitRepo(t)
	cases := []struct {
		name       string
		nodeID     string
		releaseMid bool
	}{
		// 对端在 Finish **开始之前**已经收走现场：第一条 git 就起不来。
		{name: "释放发生在 Finish 之前", nodeID: "wi-impl-before", releaseMid: false},
		// 现场形状：第一条 git 成功之后、第二条之前被收走。
		{name: "释放发生在两条 git 之间（现场形状）", nodeID: "wi-impl-midway", releaseMid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gate := &approveGateStub{choice: "approve"}
			mgr := NewWorktreeManager(WorktreeManagerDeps{
				Root:  func() string { return root },
				Phase: func(context.Context, string, string) {},
				Gate:  func() approve.ApprovalGate { return gate },
			})
			wt := mgr.BeginNamed(tc.nodeID)
			if wt == nil {
				t.Fatal("真实 git 仓库里应能建出现场")
			}
			if _, err := os.Stat(wt.Path); err != nil {
				t.Fatalf("前置：现场目录应存在：%v", err)
			}

			// 对端的动作 = team_accept 的释放（走同一条真实实现：git worktree remove --force）。
			release := func() {
				t.Helper()
				if _, err := GitRunner(root, "worktree", "remove", "--force", wt.Path); err != nil {
					t.Fatalf("对端释放失败（这条不是被复现的那条报错）：%v", err)
				}
			}
			real := mgr.git
			mgr.git = func(dir string, args ...string) (string, error) {
				out, err := real(dir, args...)
				// 「两条 git 之间」那一拍：第一条命令（branchBehindBase）已经成功返回，
				// 对端此刻把现场收走——并发交错在这里被钉成确定性的一拍。
				if tc.releaseMid && err == nil && dir == wt.Path &&
					strings.HasPrefix(strings.Join(args, " "), "rev-list --count HEAD..") {
					release()
				}
				return out, err
			}
			if !tc.releaseMid {
				release()
			}

			err := mgr.Finish(context.Background(), tc.nodeID, wt)
			if err == nil {
				t.Fatal("现场已被收走时 Finish 必须报错——不能假装合并成功")
			}
			t.Logf("复现到的报错：%v", err)

			// 指纹：报错指向哪条命令，说明对端是在哪一拍动手的。
			wantCommand := "rev-list --count " + wt.BaseCommit + "..HEAD"
			if !tc.releaseMid {
				wantCommand = "rev-list --count HEAD.." + wt.MainBranch
			}
			if !strings.Contains(err.Error(), wantCommand) {
				t.Fatalf("报错应指向 %q，得到：%v", wantCommand, err)
			}
			if !isVanishedCwdError(err) {
				t.Fatalf("报错应是「子进程 cwd 不存在」这一类（不是 git 的输出）：%v", err)
			}
			// 对端确实把现场收干净了：目录没了、git 登记也没了。
			if _, statErr := os.Stat(wt.Path); !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("现场目录应已被对端收走：stat err = %v", statErr)
			}
			if gitWorktreeRegistered(root, wt.Path) {
				t.Fatal("对端释放应连 git 登记一起清掉（CleanupWorktree 的幂等口径）")
			}
		})
	}
}

// isVanishedCwdError 报告这条 git 报错是不是"子进程的工作目录不存在"那一类。
//
// Windows：CreateProcess 报 ERROR_DIRECTORY（"The directory name is invalid"）；
// 其它平台：fork/exec 之后 chdir 失败。两者都是 **Go 的**报错，不是 git 的输出——
// 这正是"目录不见了"与"git 拒绝了这条命令"的分界。
func isVanishedCwdError(err error) bool {
	text := strings.ToLower(err.Error())
	if runtime.GOOS == "windows" {
		return strings.Contains(text, "directory name is invalid")
	}
	return strings.Contains(text, "chdir") || strings.Contains(text, "no such file or directory")
}

// reproGitRepo 建一个**真实** git 仓库。
//
// 这条复现要的正是"真 git 起不来子进程"的语义：fake git 会照着脚本返回，永远报不出
// ERROR_DIRECTORY——用它复现，等于把要证的那件事假设成假的。
func reproGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("需要真实 git（PATH 里没有）")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, strings.TrimSpace(string(out)))
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "repro@seelex.local")
	run("config", "user.name", "seelex repro")
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "base")
	return root
}
