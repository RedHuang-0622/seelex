//go:build manualsmoke

package main

// 「作业做完了，但没人调 job_manage」是作业面的**常态**，不是异常路径：模型派发
// 之后继续干自己的活、回合结束，然后才想起来（或者永远想不起来）那条作业。本用例用
// 真实 API 把这条常态跑一遍，并把它到底"靠什么回填"钉死在断言里。
//
// 三段：
//
//	第一轮：真实模型调 bash_bg 派发一条"3 行输出后就退出"的命令，**全程不调
//	        job_manage**（受理回执之外它拿不到任何输出字节）。
//	终态：  作业自己收尾。没有任何人调用工具，登记表自己把终态**有界摘要**
//	        （exit / 行数 / 字节数 / 有界末行）算出来并置上 notified 位。
//	第二轮：模型**不许调任何工具**，只凭上下文报告句柄、终态、退出码与输出末行。
//	        末行标记只存在于终态摘要里 —— 它出现在回复里，就是"结果已经自行 append
//	        到请求尾部"的直接证据；而首行标记必须**不**出现（摘要只到末行）。
//	第三轮：真实模型调 job_manage(op=fetch) —— 首行标记这时才第一次进上下文。这就是
//	        fetch 与打点块之间**唯一**的语义差：全文（消费式增量） vs 有界摘要。
//
// 判据全部落在 wire/DTO 事实上。唯一依赖模型输出的是第二轮那句"凭上下文报告"，它被
// 「本回合零工具调用」钉住——不允许它用 observe/fetch 把答案问出来。三轮都有工具调用
// 记录与快照可引用，失败时能看出是装配错了、回填没发生，还是模型没照做。
//
// 运行（config/accounts.yaml 的内容不会被读取/打印）：
//
//	$env:SEELEX_SMOKE_ACCOUNTS = (Resolve-Path config/accounts.yaml)
//	go test -tags manualsmoke . -run TestManualSmokeRealAccountJobBackfillWithoutManage -count=1 -v -timeout=10m

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

const (
	// 两个标记只出现在**命令输出**里：首行标记只在全文里（fetch 才能拿到），
	// 末行标记在终态摘要里（打点块每轮重播）。两者的出现/缺席就是回填口径的判据。
	backfillFirstToken = "SELEX_BACKFILL_FIRST_7C41"
	backfillLastToken  = "SELEX_BACKFILL_LAST_7C41"
)

// TestManualSmokeRealAccountJobBackfillWithoutManage 见文件头注。
func TestManualSmokeRealAccountJobBackfillWithoutManage(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("set SEELEX_SMOKE_ACCOUNTS to an accounts.yaml path to run the live smoke test")
	}
	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	copyOpaqueFile(t, accountsSource, accountsPath)

	// 作业面必须显式打开（limits.async_exec.enabled）；工具超时给足，别让派发回合
	// 被超时掐掉。
	harness := newFullChainHarnessWithLimits(t, accountsPath, projectRoot, 60*time.Second, true,
		seelexctx.Limits{AsyncExec: seelexctx.AsyncExecLimits{Enabled: true}, ForkTimeoutSec: 120})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	subscription := harness.events.Subscribe(1024)
	defer subscription.Close()
	collector := newBackfillCollector(subscription)

	// ── 第一轮：派发，且**不**取回 ──────────────────────────────────────
	// 命令在 bash 与 PowerShell 下都是合法的三行输出（本机 bash 工具无 Git Bash 时
	// 走 PowerShell 回退）；末行是标记，摘要取的就是它。
	command := "echo " + backfillFirstToken + "; echo middle-line; echo " + backfillLastToken
	dispatchPrompt := "Call bash_bg exactly once with " +
		`{"command":"` + command + `","description":"live smoke: backfill probe"}` +
		". Do NOT call job_manage in this turn. Do not call any other tool. " +
		"Reply with one short sentence that contains the handle."
	if err := harness.app.Submit(ctx, dispatchPrompt); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("dispatch turn did not become idle: %v\n%s", err, allGoroutineStacks())
	}
	firstTurn := collector.take()
	if snapshot := harness.app.Snapshot(); snapshot.Chat.Error != "" {
		t.Fatalf("dispatch turn failed: %s", snapshot.Chat.Error)
	}

	handle := jobHandleFromCalls(t, firstTurn)
	for _, call := range firstTurn {
		if call.name == "job_manage" {
			t.Fatalf("第一轮出现了 job_manage 调用，「没人取回」这条前提不成立："+
				"status=%s result=%.200s（重跑一次；本用例要的就是不取回的那条路径）", call.status, call.result)
		}
		if call.name == "bash_bg" && strings.Contains(call.result, backfillFirstToken) {
			t.Fatalf("受理回执里出现了命令输出（派发即返回的纪律被破坏）：%q", call.result)
		}
	}
	t.Logf("第一轮：handle=%s，工具调用 %d 次（无 job_manage）", handle, len(firstTurn))

	// ── 终态：没有任何人调用工具，回填位与摘要由收尾自己产生 ────────────
	record := waitForAsyncTerminal(t, harness.runtime, handle, 30*time.Second)
	t.Logf("终态（无人调 job_manage）：state=%s exit=%d notified=%v lines=%d bytes=%d summary=%q",
		record.State, record.ExitCode, record.Notified, record.Lines, record.LogBytes, record.Summary)

	if record.State != "done" {
		t.Fatalf("作业终态 = %q, want done（summary=%q）", record.State, record.Summary)
	}
	if record.ExitCode != 0 {
		t.Fatalf("退出码 = %d, want 0（summary=%q）", record.ExitCode, record.Summary)
	}
	if !record.Notified {
		t.Fatal("终态没有置上回填位（notified=false）：没人调 job_manage 就回填不了——" +
			"这正是本轮要查的问题")
	}
	if !strings.Contains(record.Summary, backfillLastToken) {
		t.Fatalf("终态摘要不含输出末行标记：summary=%q（末行没进摘要，"+
			"打点块就只能报个状态，模型看不到结果）", record.Summary)
	}
	if record.Lines < 3 {
		t.Fatalf("摘要在的行数 = %d, want >= 3（命令输出三行）：summary=%q", record.Lines, record.Summary)
	}
	// 摘要只到末行：首行标记**不得**出现在摘要里（全文只走 fetch）。
	if strings.Contains(record.Summary, backfillFirstToken) {
		t.Fatalf("摘要里出现了首行全文（摘要必须是**有界**的：exit/行数/字节/末行）：%q", record.Summary)
	}
	// 没人销项，行就一直在（这就是"做完了没人管"的样子：它没丢，只是没被销项）。
	if !asyncHandlePresent(harness.runtime, handle) {
		t.Fatal("终态行不见了：没人销项时它本该留在登记表里")
	}
	// 工作表格投影里也该有这条完成行（GUI 读的就是这张表）。
	if row := waitForAsyncWorkRow(harness.app, handle, 5*time.Second); row == "" {
		t.Fatalf("工作表格投影里没有 async:%s 行", handle)
	}

	// ── 第二轮：不许调工具，只凭上下文报告（= 回填证据）────────────────
	reportPrompt := "Do not call any tool in this turn. From the context you already have, " +
		"report the background job's handle, its terminal state, its exit code, and the last line " +
		"of its output. One short sentence."
	if err := harness.app.Submit(ctx, reportPrompt); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("report turn did not become idle: %v\n%s", err, allGoroutineStacks())
	}
	secondTurn := collector.take()
	for _, call := range secondTurn {
		t.Fatalf("第二轮调用了工具 %s（status=%s）：这样「凭上下文报告」就不是回填证据了", call.name, call.status)
	}
	reply := latestVisibleAssistant(harness.app.Snapshot())
	t.Logf("第二轮回复（零工具调用）：%q", reply)

	if !strings.Contains(reply, handle) {
		t.Fatalf("第二轮回复没有句柄 %q：完成行没有回到上下文：%q", handle, reply)
	}
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "done") && !strings.Contains(reply, "完成") {
		t.Fatalf("第二轮回复没有报终态（done/完成）：%q", reply)
	}
	if !strings.Contains(reply, backfillLastToken) {
		t.Fatalf("第二轮回复没有末行标记 %q —— 终态摘要没有 append 到请求尾部（回填缺失）：%q",
			backfillLastToken, reply)
	}
	if strings.Contains(reply, backfillFirstToken) {
		t.Fatalf("第二轮回复出现了首行标记 —— 只有 fetch 才该拿到全文，摘要不该带它：%q", reply)
	}
	t.Logf("第二轮证据：末行标记 %q 在零工具调用的回合里被报出 ⇒ 终态摘要自行回填到了请求尾部",
		backfillLastToken)

	// ── 第三轮：全文只有 fetch 拿得到 ─────────────────────────────────
	fetchPrompt := "Call job_manage exactly once with " +
		`{"op":"fetch","handle":"` + handle + `","wait_ms":5000}` +
		". Then reply with one short sentence containing the first line of its output. " +
		"Do not call any other tool."
	if err := harness.app.Submit(ctx, fetchPrompt); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("fetch turn did not become idle: %v\n%s", err, allGoroutineStacks())
	}
	thirdTurn := collector.take()
	var fetchedOutput, fetchedStatus string
	for _, call := range thirdTurn {
		if call.name != "job_manage" {
			continue
		}
		var payload struct {
			Status string `json:"status"`
			State  string `json:"state"`
			Output string `json:"output"`
		}
		if err := json.Unmarshal([]byte(call.result), &payload); err != nil {
			t.Fatalf("job_manage 回执不是合法 JSON: %v (%q)", err, call.result)
		}
		fetchedOutput, fetchedStatus = payload.Output, payload.Status
	}
	if fetchedStatus != "finished" {
		t.Fatalf("取回回执 status = %q, want finished（工具调用：%+v）", fetchedStatus, thirdTurn)
	}
	if !strings.Contains(fetchedOutput, backfillFirstToken) {
		t.Fatalf("取回的全文不含首行标记：output=%q（fetch 是全文的唯一入口）", fetchedOutput)
	}
	t.Logf("第三轮：fetch 回执 status=%s，全文含首行标记 %q ⇒ 摘要/全文的分工成立",
		fetchedStatus, backfillFirstToken)

	// 终态取回即销项：登记表里不该再有它（打点块随之消失）。
	if asyncHandlePresent(harness.runtime, handle) {
		t.Fatal("终态取回之后句柄仍在登记表里（销项没发生）")
	}
	t.Logf("=== 真实 API 回填冒烟通过：终态摘要无人取回即回填，全文只在 fetch ===")
}

// ── 观测助手 ────────────────────────────────────────────────────────

// backfillCollector 订阅工具完成事件，按轮次切分真实工具调用（不 mock、不预置）。
type backfillCollector struct {
	mu    sync.Mutex
	calls []liveToolCall
	done  chan struct{}
}

func newBackfillCollector(subscription application.Subscription) *backfillCollector {
	collector := &backfillCollector{done: make(chan struct{})}
	go func() {
		defer close(collector.done)
		for event := range subscription.Events {
			if event.Kind != application.EventToolCompleted {
				continue
			}
			var message application.Message
			if err := json.Unmarshal(event.Payload, &message); err != nil || message.Tool == nil {
				continue
			}
			collector.mu.Lock()
			collector.calls = append(collector.calls, liveToolCall{
				name: message.Tool.Name, status: message.Tool.Status.String(), result: message.Tool.Result,
			})
			collector.mu.Unlock()
		}
	}()
	return collector
}

// take 取走本轮的调用记录并清空（轮次边界由 submit/WaitForIdle 划定）。
func (c *backfillCollector) take() []liveToolCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	calls := append([]liveToolCall(nil), c.calls...)
	c.calls = nil
	return calls
}

// waitForAsyncTerminal 轮询登记表直到该句柄落到终态。**不调用任何工具**：这条路径
// 就是"没人取回"本身。
func waitForAsyncTerminal(t *testing.T, runtime *seelebridge.Runtime, handle string, budget time.Duration) dto.AsyncRunRecord {
	t.Helper()
	deadline := time.Now().Add(budget)
	for {
		for _, record := range runtime.AsyncRunsSnapshot() {
			if record.Handle == handle && record.State != dto.AsyncStateRunning {
				return record
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("作业 %s 在 %v 内没有落到终态（登记表：%+v）", handle, budget, runtime.AsyncRunsSnapshot())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// asyncHandlePresent 报告句柄是否仍在登记表里。
func asyncHandlePresent(runtime *seelebridge.Runtime, handle string) bool {
	for _, record := range runtime.AsyncRunsSnapshot() {
		if record.Handle == handle {
			return true
		}
	}
	return false
}

// waitForAsyncWorkRow 等投影发布器把 async:<handle> 行推出来（发布是异步的），
// 返回行 ID（超时返回空串）。
func waitForAsyncWorkRow(app *application.Service, handle string, budget time.Duration) string {
	rowID := "async:" + handle
	deadline := time.Now().Add(budget)
	for {
		for _, row := range app.Snapshot().Runtime.WorkTable {
			if row.SourceID == rowID {
				return row.ID
			}
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(100 * time.Millisecond)
	}
}
