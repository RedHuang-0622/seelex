//go:build manualsmoke

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// liveToolCall 是本轮真实 API 调用留下的一条工具完成记录（名字 / 状态 / 结果原文）。
type liveToolCall struct {
	name   string
	status string
	result string
}

// liveLongCommand 给出一条"跑几分钟、每秒钟产出一行"的命令。
//
// 按平台给语法：`bash` 工具在本机没有 Git Bash 时会退回 PowerShell（实测就是这条路径），
// 于是 POSIX 的 `$(seq …)` 会立刻语法报错、作业一秒钟就 failed——那样测的就不是 kill 了。
// PowerShell 那一版用 `[Console]::Out.WriteLine`：它逐行落到重定向的 stdout 上，
// 因此 kill 之前确实有字节落盘（这正是"kill 不丢已产出内容"要断言的东西）。
func liveLongCommand() string {
	if runtime.GOOS == "windows" {
		return "1..300 | ForEach-Object { [Console]::Out.WriteLine('tick-' + $_); Start-Sleep -Seconds 1 }"
	}
	return "for i in $(seq 1 300); do echo tick-$i; sleep 1; done"
}

// TestManualSmokeRealAccountJobContract 是**真实 API** 上的"子进程调用系"冒烟：
// 让模型自己去跑一遍派发 → 观察 → 取回 → 终止，四个动作全部是真实 provider 的
// tool_call（不 mock、不预置脚本）。
//
// 判据（全部来自真实 wire 行为）：
//  1. `bash_bg` 派发 → 受理回执（status=accepted，带 handle/log_path，不含输出）；
//  2. `job_manage(op=observe)` → status=observed（只读读数，不消费输出）；
//  3. `job_manage(op=kill)` → status=killed（终止回执）；
//  4. `job_manage(op=fetch)` → status=finished + state=killed + exit_code=137
//     （kill 前的产出仍在）；
//  5. 作业行真的进过工作表格投影（`async:<handle>`），终态取回（销项）后消失。
//
// 运行（config/accounts.yaml 的内容不会被读取/打印）：
//
//	$env:SEELEX_SMOKE_ACCOUNTS = (Resolve-Path config/accounts.yaml)
//	go test -tags manualsmoke . -run TestManualSmokeRealAccountJobContract -count=1 -v -timeout=10m
//
// 分两轮：第一轮派发 + 观察（句柄从真实回执里取），第二轮把句柄明文回喂给模型，
// 由它 kill + fetch。这样"三个管理动作由真实模型执行"是确定的，而不是靠模型记住句柄。
func TestManualSmokeRealAccountJobContract(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("set SEELEX_SMOKE_ACCOUNTS to an accounts.yaml path to run the live smoke test")
	}
	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	copyOpaqueFile(t, accountsSource, accountsPath)

	// 作业面必须显式打开（limits.async_exec.enabled）；工具超时给足，让 10s 的长命令
	// 在 kill 之前一直活着。
	harness := newFullChainHarnessWithLimits(t, accountsPath, projectRoot, 60*time.Second, true,
		seelexctx.Limits{AsyncExec: seelexctx.AsyncExecLimits{Enabled: true}, ForkTimeoutSec: 120})
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	subscription := harness.events.Subscribe(1024)
	defer subscription.Close()

	var mu sync.Mutex
	var calls []liveToolCall
	observedRow := ""
	stopSampling := make(chan struct{})
	var sampler sync.WaitGroup

	// 事件排空（订阅口容量有限，堵住会让工具调用看起来"没完成"）。
	go func() {
		for {
			select {
			case <-stopSampling:
				return
			case event, ok := <-subscription.Events:
				if !ok {
					return
				}
				if event.Kind != application.EventToolCompleted {
					continue
				}
				var message application.Message
				if err := json.Unmarshal(event.Payload, &message); err != nil || message.Tool == nil {
					continue
				}
				mu.Lock()
				calls = append(calls, liveToolCall{name: message.Tool.Name, status: message.Tool.Status.String(), result: message.Tool.Result})
				mu.Unlock()
			}
		}
	}()

	// 投影采样：作业行必须真的出现过（`async:<handle>`），不能只是执行域里有。
	sampler.Add(1)
	go func() {
		defer sampler.Done()
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopSampling:
				return
			case <-ticker.C:
				snapshot := harness.app.Snapshot()
				mu.Lock()
				for _, row := range snapshot.Runtime.WorkTable {
					if strings.HasPrefix(row.SourceID, "async:") {
						observedRow = row.SourceID
					}
				}
				mu.Unlock()
			}
		}
	}()

	// 两轮，而不是一轮四步：kill/fetch 需要**句柄**，而句柄只存在于第一轮的受理回执里。
	// 让模型在一轮里"记住并回填"句柄是它最容易漏的一步（实测就漏在 kill 上）；把句柄
	// 由测试在第二轮明文回喂，三个管理动作才**必然**由真实模型执行——被测的是工具与
	// 契约，不是模型的多步记忆。
	longCommand := liveLongCommand()
	dispatchPrompt := "Call bash_bg exactly once with " +
		`{"command":"` + longCommand + `","description":"live smoke: tick forever"}` +
		", then read the handle from the acceptance receipt and call job_manage with " +
		`{"op":"observe","handle":"<that handle>"}` +
		". Do not call any other tool. Reply with one short sentence containing the handle."
	if err := harness.app.Submit(ctx, dispatchPrompt); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		close(stopSampling)
		sampler.Wait()
		t.Fatalf("live job turn did not become idle: %v\n%s", err, allGoroutineStacks())
	}

	// 从第一轮的真实回执里取句柄（不猜、不预置）：它同时也证明"派发即返回"。
	mu.Lock()
	firstRound := append([]liveToolCall(nil), calls...)
	mu.Unlock()
	handle := jobHandleFromCalls(t, firstRound)
	t.Logf("第一轮：模型派发并观察了 handle=%s（%d 次工具调用）", handle, len(firstRound))

	killPrompt := "The background job handle is " + handle + ". Call job_manage with " +
		`{"op":"kill","handle":"` + handle + `"}` +
		" exactly once, then call job_manage with " +
		`{"op":"fetch","handle":"` + handle + `","wait_ms":5000}` +
		" exactly once. Do not call any other tool. Reply with one short sentence stating the terminal state and the exit_code."
	if err := harness.app.Submit(ctx, killPrompt); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		close(stopSampling)
		sampler.Wait()
		t.Fatalf("live kill turn did not become idle: %v\n%s", err, allGoroutineStacks())
	}
	close(stopSampling)
	sampler.Wait()
	if snapshot := harness.app.Snapshot(); snapshot.Chat.Error != "" {
		t.Fatalf("live job turn failed: %s", snapshot.Chat.Error)
	}

	mu.Lock()
	collected := append([]liveToolCall(nil), calls...)
	projected := observedRow
	mu.Unlock()

	// 分类本轮的真实调用：派发 / 观察 / 终止 / 取回。
	type jobPayload struct {
		Status   string `json:"status"`
		Handle   string `json:"handle"`
		Kind     string `json:"kind"`
		State    string `json:"state"`
		ExitCode int    `json:"exit_code"`
		Output   string `json:"output"`
	}
	var (
		sawObserve     bool
		sawKill        bool
		sawTerminal    bool
		terminalOutput string
		observedKinds  []string
	)
	for _, call := range collected {
		t.Logf("真实调用：%s status=%s result=%.200s", call.name, call.status, call.result)
		switch call.name {
		case "bash_bg":
			if call.status != "success" {
				t.Fatalf("bash_bg 失败: %+v", call)
			}
			var payload jobPayload
			if err := json.Unmarshal([]byte(call.result), &payload); err != nil {
				t.Fatalf("bash_bg 回执不是合法 JSON: %v (%q)", err, call.result)
			}
			if payload.Status != "accepted" || payload.Handle == "" {
				t.Fatalf("bash_bg 回执 = %+v, want accepted + handle", payload)
			}
			if payload.Output != "" {
				t.Fatalf("受理回执不得携带输出: %q", payload.Output)
			}
			if payload.Handle != handle {
				t.Fatalf("回执句柄 = %q，与第一轮解析出的 %q 不一致", payload.Handle, handle)
			}
		case "job_manage":
			var payload jobPayload
			if err := json.Unmarshal([]byte(call.result), &payload); err != nil {
				t.Fatalf("job_manage 回执不是合法 JSON: %v (%q)", err, call.result)
			}
			observedKinds = append(observedKinds, payload.Kind)
			switch payload.Status {
			case "observed":
				sawObserve = true
			case "killed":
				sawKill = true
			case "finished":
				if payload.State == "killed" && payload.ExitCode == 137 {
					sawTerminal = true
					terminalOutput = payload.Output
				}
			}
		}
	}
	t.Logf("真实 API 的子进程调用系：calls=%d handle=%q observe=%v kill=%v terminal=%v 投影行=%q kinds=%v",
		len(collected), handle, sawObserve, sawKill, sawTerminal, projected, observedKinds)

	if !sawObserve {
		t.Fatal("模型没有用 job_manage(op=observe) 观察作业")
	}
	if !sawKill {
		t.Fatal("模型没有用 job_manage(op=kill) 终止作业")
	}
	if !sawTerminal {
		t.Fatal("模型没有用 job_manage(op=fetch) 取回 killed/137 的终态")
	}
	// kill 不丢结果：kill 前命令已经 echo 出来的 tick 行必须仍能取回。
	if !strings.Contains(terminalOutput, "tick-") {
		t.Fatalf("kill 前的产出丢了：取回的 output = %q", terminalOutput)
	}
	if projected != "async:"+handle {
		t.Fatalf("作业行没有进过工作表格投影：投影行 = %q, want %q", projected, "async:"+handle)
	}
	// 终态取回即销项：登记表与投影里都不该再有它。
	for _, record := range harness.runtime.AsyncRunsSnapshot() {
		if record.Handle == handle {
			t.Fatalf("终态取回之后句柄仍在投影里: %+v", record)
		}
	}
	t.Logf("=== 真实 API 子进程调用系冒烟通过（派发 / 观察 / 终止 / 取回）===")

	if reply := latestVisibleAssistant(harness.app.Snapshot()); reply != "" {
		t.Logf("模型最终回复：%s", reply)
	}
}

// jobHandleFromCalls 从本轮真实调用里取出 bash_bg 的受理回执句柄。
// 它是"派发即返回"的直接证据：回执里有 handle 与 log_path，且不含任何命令输出。
func jobHandleFromCalls(t *testing.T, calls []liveToolCall) string {
	t.Helper()
	for _, call := range calls {
		if call.name != "bash_bg" {
			continue
		}
		if call.status != "success" {
			t.Fatalf("bash_bg 失败: %+v", call)
		}
		var payload struct {
			Status string `json:"status"`
			Handle string `json:"handle"`
			Output string `json:"output"`
		}
		if err := json.Unmarshal([]byte(call.result), &payload); err != nil {
			t.Fatalf("bash_bg 回执不是合法 JSON: %v (%q)", err, call.result)
		}
		if payload.Status != "accepted" || payload.Handle == "" {
			t.Fatalf("bash_bg 回执 = %+v, want accepted + handle", payload)
		}
		if payload.Output != "" {
			t.Fatalf("受理回执不得携带命令输出: %q", payload.Output)
		}
		return payload.Handle
	}
	t.Fatalf("模型这一轮没有调用 bash_bg：%+v", calls)
	return ""
}
