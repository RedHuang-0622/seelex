package core

// teamwork_completion_trigger_test.go — 钉住「teammate 干完了要像 subagent 一样**自动
// 返回**」（2026-10-04 现场：「workitem done 了，内容我取回来了，但是没有像 subagent
// 一样的自动返回」）。
//
// 根因：两条链只接了一条——subagent / bash_bg / read_batch 活在 tools 的后台执行登记表
// （AsyncRunsSnapshot + AsyncRunEvents），teammate 作业活在 Seele 的 jobs.Manager 里
// （contract.TeamworkJobCompletion）。同一件事（跑完了）在 teammate 侧既不在投影里、
// 也不在信号口上。
//
// 这里逐条钉住与 subagent 侧**对齐的口径**：done/failed 触发、running/killed 不触发、
// 绝不唤醒忙会话（跳过的条目还要留待重试）、一个句柄一次、两张表的句柄空间**互不压制**。

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// teamworkCompletionHarness 造一个装配好 teammate 触发路径的 Service。
func teamworkCompletionHarness(t *testing.T, trigger bool, engine ChatEngine) (*Service, *fakeRuntime) {
	t.Helper()
	applyTestLimits(t, func(applied *seelexctx.Limits) {
		applied.AsyncExec.Enabled = true
		applied.AsyncExec.TriggerConversation = trigger
	})
	runtime := &fakeRuntime{teamworkEvents: make(chan struct{}, 1)}
	service := newTestService(t, engine, withTestRuntime(runtime))
	return service, runtime
}

// completedTeammateRecord 造一条已落到终态的 teammate 作业记录。
func completedTeammateRecord(handle, state string) dto.TeamworkJobCompletionRecord {
	return dto.TeamworkJobCompletionRecord{
		Handle: handle, Kind: "worker", State: state, ExitCode: 0,
		SessionID: "session-a", Role: "exec", WorkItem: "wi-impl",
		Description: "exec/wi-impl: 实现渲染件",
		Summary:     "done · 12 行 · 未提交改动已合并",
	}
}

func TestTeamworkCompletionTriggersIdleSessionTurn(t *testing.T) {
	service, runtime := teamworkCompletionHarness(t, true, &fakeEngine{sessionID: "session-a"})

	runtime.teamworkRuns = []dto.TeamworkJobCompletionRecord{completedTeammateRecord("ab7", asyncStateDone)}
	runtime.teamworkEvents <- struct{}{}

	body := waitForTriggeredTurn(t, service, "ab7")
	// 正文必须自带"谁跑完了、去哪收口"：它是一条用户行，模型与人都只有它可读。
	for _, want := range []string{
		"teammate: exec（工作项 wi-impl）", "handle: ab7", "状态: done",
		"team_context", `jobs_manage(op=observe|fetch, handle="ab7")`, "team_accept / team_fail",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("触发回合的正文缺少 %q：%q", want, body)
		}
	}
	// **不能**指向 job_manage：那是 tools 那张表的取回工具，拿它取 teammate 作业会取不到。
	if strings.Contains(body, "job_manage(op=fetch") {
		t.Fatalf("teammate 作业的正文指错了取回工具（job_manage 属于另一张表）：%q", body)
	}
}

func TestTeamworkCompletionTriggersOnFailureToo(t *testing.T) {
	service, runtime := teamworkCompletionHarness(t, true, &fakeEngine{sessionID: "session-a"})

	record := completedTeammateRecord("ab8", asyncStateFailed)
	record.ExitCode = 1
	record.Summary = "failed · exit=1 · 交作业时 rebase 冲突"
	runtime.teamworkRuns = []dto.TeamworkJobCompletionRecord{record}
	runtime.teamworkEvents <- struct{}{}

	body := waitForTriggeredTurn(t, service, "ab8")
	if !strings.Contains(body, "状态: failed · exit=1") {
		t.Fatalf("失败作业的触发正文没有如实带上状态/退出码：%q", body)
	}
}

func TestTeamworkCompletionIgnoresRunningAndKilled(t *testing.T) {
	service, runtime := teamworkCompletionHarness(t, true, &fakeEngine{sessionID: "session-a"})

	running := completedTeammateRecord("ab9", dto.AsyncStateRunning)
	killed := completedTeammateRecord("ab10", "killed")
	runtime.teamworkRuns = []dto.TeamworkJobCompletionRecord{running, killed}
	runtime.teamworkEvents <- struct{}{}

	assertNoTurn(t, service, "ab9")
	assertNoTurn(t, service, "ab10")
}

// 铁律 §6.1「绝不唤醒忙会话」：忙的时候不起回合，跳过的条目**不记账**——会话回到空闲
// 后的下一次信号要能把它补上（与 subagent 那条同口径）。
func TestTeamworkCompletionDoesNotWakeBusySession(t *testing.T) {
	engine := &sessionBackedBlockingEngine{
		fakeEngine: &fakeEngine{sessionID: "session-a"},
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	service, runtime := teamworkCompletionHarness(t, true, engine)

	if err := service.Submit(context.Background(), "先占住这个会话"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	select {
	case <-engine.started:
	case <-time.After(5 * time.Second):
		t.Fatal("占位回合没有启动")
	}

	runtime.teamworkRuns = []dto.TeamworkJobCompletionRecord{completedTeammateRecord("ab11", asyncStateDone)}
	runtime.teamworkEvents <- struct{}{}
	assertNoTurn(t, service, "ab11")

	close(engine.release)
	waitForChatCompletion(t, service)
	runtime.teamworkEvents <- struct{}{}
	waitForTriggeredTurn(t, service, "ab11")
}

// 幂等：句柄在册期间只触发一次（句柄单调不复用，进程内一个集合就够）。
func TestTeamworkCompletionTriggersOncePerHandle(t *testing.T) {
	service, runtime := teamworkCompletionHarness(t, true, &fakeEngine{sessionID: "session-a"})

	runtime.teamworkRuns = []dto.TeamworkJobCompletionRecord{completedTeammateRecord("ab12", asyncStateDone)}
	runtime.teamworkEvents <- struct{}{}
	waitForTriggeredTurn(t, service, "ab12")
	waitForChatCompletion(t, service)

	runtime.teamworkEvents <- struct{}{}
	time.Sleep(200 * time.Millisecond)
	rounds := 0
	for _, message := range service.Snapshot().Conversation {
		if message.Role == "user" && strings.Contains(message.Content, "ab12") {
			rounds++
		}
	}
	if rounds != 1 {
		t.Fatalf("同一个句柄触发了 %d 轮，want 1", rounds)
	}
}

// 两张表的句柄空间**各自独立**（都是从 a<seq> 起步）：同一个字面量句柄在两张表里同时
// 终态时，两条链都要各触发一次——幂等账不做前缀就会互相压制（后一条永远静默）。
func TestTeamworkCompletionDoesNotCollideWithToolHandles(t *testing.T) {
	applyTestLimits(t, func(applied *seelexctx.Limits) {
		applied.AsyncExec.Enabled = true
		applied.AsyncExec.TriggerConversation = true
	})
	runtime := &fakeRuntime{
		asyncEvents:    make(chan struct{}, 1),
		teamworkEvents: make(chan struct{}, 1),
	}
	service := newTestService(t, &fakeEngine{sessionID: "session-a"}, withTestRuntime(runtime))

	runtime.asyncRuns = []dto.AsyncRunRecord{completedRecord("a7", asyncStateDone)}
	runtime.teamworkRuns = []dto.TeamworkJobCompletionRecord{completedTeammateRecord("a7", asyncStateDone)}
	runtime.asyncEvents <- struct{}{}
	waitForTriggeredTurn(t, service, "a7")
	waitForChatCompletion(t, service)

	runtime.teamworkEvents <- struct{}{}
	body := waitForTriggeredTurn(t, service, "teammate: exec")
	if !strings.Contains(body, "handle: a7") {
		t.Fatalf("teammate 那条链被 tools 的同号句柄压制了（幂等账缺前缀）：%q", body)
	}
}

// 开关关闭时连扫描都不做（与 subagent 那条同一个开关）。
func TestTeamworkCompletionStaysOffWhenDisabled(t *testing.T) {
	service, runtime := teamworkCompletionHarness(t, false, &fakeEngine{sessionID: "session-a"})

	runtime.teamworkRuns = []dto.TeamworkJobCompletionRecord{completedTeammateRecord("ab13", asyncStateDone)}
	runtime.teamworkEvents <- struct{}{}

	assertNoTurn(t, service, "ab13")
}

// 忙会话的读法：**回合边界打点块**里的 teammate 完成行（这是"回执被看见"的另一半——
// 终态触发绝不唤醒忙会话，忙的时候只剩这条路）。
func TestTeamworkTraceLinesCarryCompletionReceipt(t *testing.T) {
	lines := teamworkTraceLines([]dto.TeamworkJobCompletionRecord{
		completedTeammateRecord("ab20", asyncStateDone),
		{Handle: "ab21", Kind: "worker", State: dto.AsyncStateRunning, SessionID: "session-a", Role: "verify", WorkItem: "wi-verify", Description: "复核渲染件"},
		{Handle: "ab22", Kind: "worker", State: asyncStateDone, SessionID: "session-b", Role: "other", WorkItem: "wi-other"},
	}, "session-a")

	if len(lines) != 2 {
		t.Fatalf("打点块只取本会话的 teammate 行（另一会话的行不得混进来）：%+v", lines)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"teamwork:ab20", "exec/wi-impl", "done", "teamwork:ab21", "复核渲染件"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("teammate 打点行缺少 %q：\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "ab22") {
		t.Fatalf("别的会话的 teammate 作业不得出现在本会话的打点块里：\n%s", joined)
	}
	if strings.Contains(joined, "job_manage") {
		t.Fatalf("打点行本身不该带取回工具名（提示行由调用方给）：\n%s", joined)
	}
}
