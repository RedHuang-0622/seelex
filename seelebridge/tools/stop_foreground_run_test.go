package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 停止按钮（终止原语）在工具层的语义判据。
//
// 停止按钮 = 取消回合 ctx（application 层 CancelChat）。落到工具层只有两条要求：
//
//  1. 前台工具调用（同步 bash）的 run 挂在回合 ctx 上——取消它就该停掉这次 run，而且
//     是**整棵进程树**。只杀直接子进程（shell）时，它派出来的孙进程会继续跑、继续
//     持有输出管道：cmd.Wait 于是要等孙进程自己退出才返回，"停止工具调用"就变成
//     "点了停止还要再等几十秒"。
//  2. 后台受管作业（bash_bg / read_batch / subagent）用 context.WithoutCancel 摘掉回合
//     ctx——停止按钮不得连累它们（生死只有 job_manage 与硬上限说了算）。
//
// 判据不看进程表（跨平台不可靠），看**行为**：让"构建"派生一个 2 秒后写标记文件的
// 孙进程。停止后标记仍出现 ⇒ 孙进程活过了停止；工具调用迟迟不返回 ⇒ run 被孙进程
// 按住了输出管道。
//
// 构建式命令只用 `node <脚本> <参数...>` 这一种形状（无 shell 元字符），因此在 bash /
// PowerShell / cmd 下同形：用例不绑死在某个 shell 的行语法上。

const (
	// stopBuildScript 是"构建"本体：起一个孙进程后自己等 10 秒（长命令）。
	// argv[2] = 孙进程脚本，argv[3] = 标记文件。
	stopBuildScript = `require('child_process').spawn(process.execPath, [process.argv[2], process.argv[3]], { stdio: 'inherit' });
setTimeout(() => {}, 10000);
`

	// stopGrandchildScript 是孙进程：2 秒后写标记，再活 20 秒。
	// 它会握住构建的 stdout 管道——这正是"只杀直接子进程"停不下来的原因。
	stopGrandchildScript = `setTimeout(() => require('fs').writeFileSync(process.argv[2], 'GRANDCHILD_SURVIVED'), 2000);
setTimeout(() => {}, 20000);
`
)

// buildCommandForTest 在临时目录里放好两个脚本，返回一条构建式长命令与它的标记文件。
func buildCommandForTest(t *testing.T, root string) (command, marker string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("需要 node 才能构造构建式长命令: %v", err)
	}
	slashed := filepath.ToSlash(root)
	build := slashed + "/build.js"
	grandchild := slashed + "/grandchild.js"
	marker = slashed + "/grandchild.marker"
	if err := os.WriteFile(filepath.FromSlash(build), []byte(stopBuildScript), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.FromSlash(grandchild), []byte(stopGrandchildScript), 0o600); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(`node "%s" "%s" "%s"`, build, grandchild, marker), marker
}

// TestStopTerminatesForegroundToolRun 钉住第 1 条：停止按钮要停掉整个前台工具调用的
// run（进程树），不是只杀它直接起来的那个 shell。工具自己带 timeout（30s）也不影响
// 停止按钮先一步生效。
func TestStopTerminatesForegroundToolRun(t *testing.T) {
	router := asyncTestRouter(t, true)
	root := t.TempDir()
	command, marker := buildCommandForTest(t, root)
	payload, err := json.Marshal(map[string]interface{}{"command": command, "timeout": 30})
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(asyncTestCtx(root, "sess-stop"))

	result := make(chan error, 1)
	go func() {
		_, err := router.scopedBash(ctx, string(payload))
		result <- err
	}()

	// 先让构建把孙进程派生出来，再按停止按钮。
	time.Sleep(900 * time.Millisecond)
	stop()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("停止后工具调用必须返回取消错误，不得报成功")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("停止后工具调用返回 %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("停止后工具调用没有收敛：run 仍被孙进程持有的输出管道按住（只杀了直接子进程，没停整棵进程树）")
	}

	// 再等一个孙进程本该写标记的窗口：它若活着，这份标记一定会出现。
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(filepath.FromSlash(marker)); err == nil {
		t.Fatal("孙进程活过了停止：前台工具调用的进程树没有被终止")
	}
}

// TestStopLeavesBackgroundSubprocessRunning 钉住第 2 条：停止按钮不影响后台子进程。
// 后台命令在取消回合 ctx 之前派发，取消之后它必须照常跑完并留下输出。
func TestStopLeavesBackgroundSubprocessRunning(t *testing.T) {
	router := asyncTestRouter(t, true)
	root := t.TempDir()
	ctx, stop := context.WithCancel(asyncTestCtx(root, "sess-stop"))

	ack := dispatchForTest(t, router, ctx, "sleep 1; echo BACKGROUND_SURVIVED")
	if ack.Handle == "" {
		t.Fatalf("后台派发没有拿到句柄: %+v", ack)
	}

	// 停止按钮：只取消回合 ctx，不碰后台句柄表。
	stop()

	// 取回走同一会话的新 ctx：停止命中的是回合，不是会话。
	final := pollForTest(t, router, asyncTestCtx(root, "sess-stop"), ack.Handle, 10000)
	if final.State != asyncStateDone || final.ExitCode != 0 {
		t.Fatalf("后台作业被停止按钮连累了: %+v", final)
	}
	if !strings.Contains(final.Output, "BACKGROUND_SURVIVED") {
		t.Fatalf("后台作业没有跑完（停止按钮不该影响后台子进程）: %+v", final)
	}
}
