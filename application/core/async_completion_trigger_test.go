package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// 后台作业终态触发对话（第五个生命周期消费者，async_completion.go）。
//
// 判据是**用户口径**（2026-10-01）：作业做完没人取回是轮询型的固有缺口，终态该自己
// 把回合起起来。这里钉住四件事：done/failed 触发、running/killed 不触发、忙会话不被
// 唤醒（铁律 §6.1）、一个句柄只触发一次。

// asyncCompletionHarness 造一个装配好该触发路径的 Service。
//
// limits 必须在**信号到达之前**改：开关是每次信号现读的（关着就一次扫描都不做），
// 因此在构造前改最省事。
func asyncCompletionHarness(t *testing.T, trigger bool, engine ChatEngine) (*Service, *fakeRuntime) {
	t.Helper()
	applyTestLimits(t, func(applied *seelexctx.Limits) {
		applied.AsyncExec.Enabled = true
		applied.AsyncExec.TriggerConversation = trigger
	})
	runtime := &fakeRuntime{asyncEvents: make(chan struct{}, 1)}
	service := newTestService(t, engine, withTestRuntime(runtime))
	return service, runtime
}

// waitForTriggeredTurn 轮询可见会话，直到出现一条含 needle 的用户行（= 触发回合开
// 起来了），返回它的正文。
func waitForTriggeredTurn(t *testing.T, service *Service, needle string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, message := range service.Snapshot().Conversation {
			if message.Role == "user" && strings.Contains(message.Content, needle) {
				return message.Content
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("后台作业终态没有触发对话（找不到含 %q 的用户行）", needle)
	return ""
}

// assertNoTurn 报告可见会话里有没有含 needle 的用户行——"不该触发"的用例用它。
//
// 等待窗口是给**消费者 goroutine** 的：信号是"有事发生"的广播，判定是异步的，立刻
// 断言只能证明"还没轮到它跑"。
func assertNoTurn(t *testing.T, service *Service, needle string) {
	t.Helper()
	time.Sleep(200 * time.Millisecond)
	for _, message := range service.Snapshot().Conversation {
		if strings.Contains(message.Content, needle) {
			t.Fatalf("不该触发的作业把回合起起来了：%q", message.Content)
		}
	}
}

// completedRecord 造一条已落到终态的作业投影记录。
func completedRecord(handle, state string) dto.AsyncRunRecord {
	return dto.AsyncRunRecord{
		SessionID: "session-a", Handle: handle, Kind: "process", State: state,
		ExitCode: 0, LogBytes: 128, Lines: 3, Notified: true,
		Description: "跑一遍前端用例",
		Summary:     "done · exit=0 · 3 行 · 128B · 末行: 602 pass",
	}
}

func TestAsyncCompletionTriggersIdleSessionTurn(t *testing.T) {
	service, runtime := asyncCompletionHarness(t, true, &fakeEngine{sessionID: "session-a"})

	runtime.asyncRuns = []dto.AsyncRunRecord{completedRecord("a7", asyncStateDone)}
	runtime.asyncEvents <- struct{}{}

	body := waitForTriggeredTurn(t, service, "a7")
	// 正文必须自带"去取哪一条"与"怎么取"：它是一条用户行，模型与人都只有它可读。
	for _, want := range []string{"handle: a7", "状态: done", "job_manage(op=fetch, handle=\"a7\")"} {
		if !strings.Contains(body, want) {
			t.Fatalf("触发回合的正文缺少 %q：%q", want, body)
		}
	}
	// 有界：进上下文的是摘要，不是日志全文/路径（与打点块同一份字段口径）。
	if strings.Contains(body, "\\AppData\\") || strings.Contains(body, "LastWriteTime") {
		t.Fatalf("触发正文泄漏了探针字段（路径/末行原文）：%q", body)
	}
}

func TestAsyncCompletionTriggersOnFailureToo(t *testing.T) {
	service, runtime := asyncCompletionHarness(t, true, &fakeEngine{sessionID: "session-a"})

	record := completedRecord("a8", asyncStateFailed)
	record.ExitCode = 1
	record.Summary = "failed · exit=1 · 12 行 · 900B · 末行: 1 failing"
	runtime.asyncRuns = []dto.AsyncRunRecord{record}
	runtime.asyncEvents <- struct{}{}

	body := waitForTriggeredTurn(t, service, "a8")
	if !strings.Contains(body, "状态: failed · exit=1") {
		t.Fatalf("失败作业的触发正文没有如实带上状态/退出码：%q", body)
	}
}

func TestAsyncCompletionIgnoresRunningAndKilled(t *testing.T) {
	if asyncCompletionTriggers(dto.AsyncStateRunning) {
		t.Fatal("running 不是终态，不该触发")
	}
	if asyncCompletionTriggers("killed") {
		t.Fatal("killed 是被终止而不是有了结果，不该触发")
	}
	if !asyncCompletionTriggers(asyncStateDone) || !asyncCompletionTriggers(asyncStateFailed) {
		t.Fatal("done / failed 是触发口径认的两个终态")
	}

	// 投影面上再走一遍：两类"不触发"的记录同时在册，也不该起回合。
	service, runtime := asyncCompletionHarness(t, true, &fakeEngine{sessionID: "session-a"})
	running := completedRecord("a9", dto.AsyncStateRunning)
	running.ExitCode = -1
	runtime.asyncRuns = []dto.AsyncRunRecord{running, completedRecord("a10", "killed")}
	runtime.asyncEvents <- struct{}{}

	assertNoTurn(t, service, "a9")
	assertNoTurn(t, service, "a10")
}

// 铁律 §6.1「绝不唤醒忙会话」：正在跑的会话不被打断、也不被塞队列——条目刻意不记账，
// 于是**会话回到空闲后的下一次信号**还能把它触发起来（这就是"留待重试"）。
func TestAsyncCompletionDoesNotWakeBusySession(t *testing.T) {
	engine := &sessionBackedBlockingEngine{
		fakeEngine: &fakeEngine{sessionID: "session-a"},
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	service, runtime := asyncCompletionHarness(t, true, engine)

	if err := service.Submit(context.Background(), "先占住这个会话"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	select {
	case <-engine.started:
	case <-time.After(5 * time.Second):
		t.Fatal("占位回合没有启动")
	}

	// 会话忙：作业终态只发信号，不起回合。
	runtime.asyncRuns = []dto.AsyncRunRecord{completedRecord("a11", asyncStateDone)}
	runtime.asyncEvents <- struct{}{}
	assertNoTurn(t, service, "a11")

	// 放行占位回合 → 会话空闲 → 再发一次信号就必须补上（被跳过的条目不记账）。
	close(engine.release)
	waitForChatCompletion(t, service)
	runtime.asyncEvents <- struct{}{}
	waitForTriggeredTurn(t, service, "a11")
}

func TestAsyncCompletionTriggersOncePerHandle(t *testing.T) {
	service, runtime := asyncCompletionHarness(t, true, &fakeEngine{sessionID: "session-a"})

	runtime.asyncRuns = []dto.AsyncRunRecord{completedRecord("a12", asyncStateDone)}
	runtime.asyncEvents <- struct{}{}
	waitForTriggeredTurn(t, service, "a12")
	waitForChatCompletion(t, service)

	// 记录还在册（还没被取回/驱逐），再发一次信号不得再起一轮。
	runtime.asyncEvents <- struct{}{}
	time.Sleep(200 * time.Millisecond)
	rounds := 0
	for _, message := range service.Snapshot().Conversation {
		if message.Role == "user" && strings.Contains(message.Content, "a12") {
			rounds++
		}
	}
	if rounds != 1 {
		t.Fatalf("同一个句柄触发了 %d 轮，want 1（幂等键 = 句柄）", rounds)
	}
}

// 开关默认关：不置 trigger_conversation 时信号只驱动工作表格重投影，终态不会起任何回合。
func TestAsyncCompletionStaysOffWhenDisabled(t *testing.T) {
	service, runtime := asyncCompletionHarness(t, false, &fakeEngine{sessionID: "session-a"})

	runtime.asyncRuns = []dto.AsyncRunRecord{completedRecord("a13", asyncStateDone)}
	runtime.asyncEvents <- struct{}{}

	assertNoTurn(t, service, "a13")
}

// 没有会话归属的作业（别的进程的作业归属、或会话已被删除）不该触发：没有可起的回合
// 落点，信号本身是广播，不当作错误。
func TestAsyncCompletionIgnoresJobWithoutSession(t *testing.T) {
	service, runtime := asyncCompletionHarness(t, true, &fakeEngine{sessionID: "session-a"})

	orphan := completedRecord("a14", asyncStateDone)
	orphan.SessionID = ""
	unknown := completedRecord("a15", asyncStateDone)
	unknown.SessionID = "session-not-loaded"
	runtime.asyncRuns = []dto.AsyncRunRecord{orphan, unknown}
	runtime.asyncEvents <- struct{}{}

	assertNoTurn(t, service, "a14")
	assertNoTurn(t, service, "a15")
}
