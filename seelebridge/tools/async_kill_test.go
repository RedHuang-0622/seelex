package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// job_manage(op=kill) 与"会话销毁即杀"的行为判据（台账 §10 J4/J5）。
//
// 进程树是否真死，不看进程表（跨平台不可靠），看**行为**：派发一条"0.5 秒后由后台子
// shell 写一行 GRANDCHILD"的命令，若在 0.15 秒杀掉 bash 后那行仍然出现在日志里，说明
// 只杀了 bash、孙进程还活着并继续持有写端。

// killForTest 调 job_manage(op=kill) 并解出回执。
func killForTest(t *testing.T, router *Router, ctx context.Context, handle string) (asyncPayload, error) {
	t.Helper()
	args, err := json.Marshal(map[string]interface{}{"op": "kill", "handle": handle})
	if err != nil {
		t.Fatal(err)
	}
	output, err := router.scopedJobManage(ctx, string(args))
	if err != nil {
		return asyncPayload{}, err
	}
	return decodeAsyncPayload(t, output), nil
}

func TestAsyncKillTerminatesProcessTree(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-kill")

	receipt := dispatchForTest(t, router, ctx, "(sleep 0.5; echo GRANDCHILD) & sleep 25")
	waitAsync := func() asyncRun {
		t.Helper()
		run, ok := router.async.snapshot(receipt.Handle)
		if !ok {
			t.Fatalf("句柄 %s 不在登记表里", receipt.Handle)
		}
		select {
		case <-run.done:
		case <-time.After(10 * time.Second):
			t.Fatalf("句柄 %s 未在预算内收尾", receipt.Handle)
		}
		fresh, _ := router.async.snapshot(receipt.Handle)
		return fresh
	}

	// 抢在子 shell 醒来之前杀。
	time.Sleep(150 * time.Millisecond)
	acked, err := killForTest(t, router, ctx, receipt.Handle)
	if err != nil {
		t.Fatalf("job_manage(op=kill): %v", err)
	}
	if acked.Status != "killed" {
		t.Fatalf("终止回执状态 = %q，want killed", acked.Status)
	}

	// 树真死了才可能这么快收敛：只杀 bash 的话，cmd.Wait 要等孙进程松开管道
	// （即 asyncWaitDelay 之后）才返回。
	terminal := waitAsync()
	if terminal.state != asyncStateKilled {
		t.Fatalf("终态 = %q，want %q", terminal.state, asyncStateKilled)
	}
	if terminal.exit != asyncKilledExit {
		t.Fatalf("终态退出码 = %d，want %d", terminal.exit, asyncKilledExit)
	}

	// 再等一个孙进程本来会醒来的窗口：它若活着，这行一定会落进日志。
	time.Sleep(800 * time.Millisecond)
	body, err := os.ReadFile(terminal.logPath)
	if err != nil {
		t.Fatalf("读日志: %v", err)
	}
	if strings.Contains(string(body), "GRANDCHILD") {
		t.Fatal("孙进程活过了 op=kill：只杀了 bash，没杀整棵树")
	}
	if !strings.Contains(string(body), "exit=137") {
		t.Fatalf("终止注记没进正文，模型将只看到\"命令突然结束\": %q", string(body))
	}

	// 终态之后取回：带回 killed 状态与 **kill 前已落盘的字节**（kill 不是丢结果）。
	final := pollForTest(t, router, ctx, receipt.Handle, -1)
	if final.Status != "finished" || final.State != asyncStateKilled || final.ExitCode != asyncKilledExit {
		t.Fatalf("杀后取回 = %+v，want finished/killed/137", final)
	}
	if !strings.Contains(final.Output, "exit=137") {
		t.Fatalf("kill 前的收尾注记必须能取回（已产出内容不丢）: %+v", final)
	}
}

func TestAsyncKillAlreadyFinishedIsNotAnError(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-kill-done")

	receipt := dispatchForTest(t, router, ctx, "echo already")
	waitAsyncTerminalForTest(t, router, receipt.Handle)

	acked, err := killForTest(t, router, ctx, receipt.Handle)
	if err != nil {
		t.Fatalf("杀一个已结束的句柄不该报错: %v", err)
	}
	if acked.Status != "already_finished" {
		t.Fatalf("状态 = %q，want already_finished", acked.Status)
	}
}

func TestAsyncKillRejectsUnknownAndForeignHandle(t *testing.T) {
	router := asyncTestRouter(t, true)
	owner := asyncTestCtx(t.TempDir(), "sess-owner")
	other := asyncTestCtx(t.TempDir(), "sess-other")

	if _, err := killForTest(t, router, owner, "a404"); err == nil {
		t.Fatal("未知句柄必须报错，不得谎报已杀")
	}
	receipt := dispatchForTest(t, router, owner, "echo x")
	if _, err := killForTest(t, router, other, receipt.Handle); err == nil {
		t.Fatal("跨会话句柄必须拒绝")
	} else if !strings.Contains(err.Error(), "不属于本会话") {
		t.Fatalf("拒绝原因不对: %v", err)
	}
	waitAsyncTerminalForTest(t, router, receipt.Handle)
}

// failingTree 是"终止不成的进程树"替身。
type failingTree struct{ err error }

func (f failingTree) Terminate() error { return f.err }
func (f failingTree) Close()           {}

// Degraded 报 true：这棵替身表达的就是"进程树挂不上"，此时终止只能打到直接子进程。
func (f failingTree) Degraded() bool { return true }

// TestAsyncKillDoesNotLieWhenKillerFails 钉住"杀不掉就说杀不掉"：终止实现报错时
// handler 必须返回错误，且不得把 killed 意图落进表里（否则一个还在跑的进程会被
// 收尾成 killed 终态）。
func TestAsyncKillDoesNotLieWhenKillerFails(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-kill-fail")

	receipt := dispatchForTest(t, router, ctx, "sleep 25")
	working, _ := router.async.snapshot(receipt.Handle)
	router.async.attach(receipt.Handle, failingTree{err: errors.New("Job 挂不上")})

	if _, err := killForTest(t, router, ctx, receipt.Handle); err == nil {
		router.async.attach(receipt.Handle, working.tree)
		t.Fatal("终止失败必须报错")
	}
	if run, _ := router.async.snapshot(receipt.Handle); run.killRequested || run.state != asyncStateRunning {
		router.async.attach(receipt.Handle, working.tree)
		t.Fatalf("终止失败却已落下 killed 痕迹: state=%q killRequested=%v", run.state, run.killRequested)
	}

	// 换回可用的实现再杀一次：同一句柄可重试，且这次要真收敛成 killed。
	router.async.attach(receipt.Handle, working.tree)
	if _, err := killForTest(t, router, ctx, receipt.Handle); err != nil {
		t.Fatalf("重试 op=kill: %v", err)
	}
	waitAsyncTerminalForTest(t, router, receipt.Handle)
	if run, _ := router.async.snapshot(receipt.Handle); run.state != asyncStateKilled {
		t.Fatalf("重试后的终态 = %q，want killed", run.state)
	}
}

func TestCloseSessionAsyncKillsOnlyOwnSession(t *testing.T) {
	router := asyncTestRouter(t, true)
	a := asyncTestCtx(t.TempDir(), "sess-a")
	b := asyncTestCtx(t.TempDir(), "sess-b")

	runA := dispatchForTest(t, router, a, "sleep 25")
	runB := dispatchForTest(t, router, b, "sleep 25")

	if got := router.CloseSessionAsync("sess-a"); got != 1 {
		t.Fatalf("CloseSessionAsync 杀了 %d 条，want 1", got)
	}
	waitAsyncTerminalForTest(t, router, runA.Handle)
	if state, _ := router.async.snapshot(runA.Handle); state.state != asyncStateKilled {
		t.Fatalf("sess-a 的句柄终态 = %q，want killed", state.state)
	}
	if still, _ := router.async.snapshot(runB.Handle); still.state != asyncStateRunning {
		t.Fatalf("sess-b 的句柄被连带杀掉了: %q", still.state)
	}

	// 现场自己收干净：不杀 sess-b，它会把 sleep 跑到 25 秒并漏一个目录。
	router.CloseSessionAsync("sess-b")
	waitAsyncTerminalForTest(t, router, runB.Handle)
	if got := router.CloseSessionAsync("sess-empty"); got != 0 {
		t.Fatalf("没有句柄的会话该返回 0，got %d", got)
	}
}

func TestJobKillEntryFollowsCapability(t *testing.T) {
	ctx := asyncTestCtx(t.TempDir(), "sess-gate")
	if _, err := NewRouter(Deps{}).scopedJobManage(ctx, `{"op":"kill","handle":"a1"}`); err == nil {
		t.Fatal("能力关闭时 job_manage 必须直接报错，不得静默做任何事")
	}

	off := captureRegisteredTools(t, false)
	if _, present := off["job_manage"]; present {
		t.Fatal("能力关闭时不该注册 job_manage")
	}
	on := captureRegisteredTools(t, true)
	tool, present := on["job_manage"]
	if !present {
		t.Fatal("能力常驻时必须注册 job_manage")
	}
	if !strings.Contains(tool.description, "process tree") {
		t.Fatalf("job_manage 描述必须点明终止整棵进程树: %q", tool.description)
	}
	if !strings.Contains(on["bash_bg"].description, "job_manage") {
		t.Fatalf("bash_bg 描述必须把管理口告诉模型: %q", on["bash_bg"].description)
	}
}
