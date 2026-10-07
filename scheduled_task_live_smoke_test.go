//go:build manualsmoke

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/gui"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

// TestManualSmokeRealAccountScheduledPromptChain 是**真实 API** 上的定时任务链路
// 冒烟：GUI Bridge（前端 invoke("ScheduleTask", spec) 的同一落点）→ application
// 端口 → seelebridge 调度器 → 注入的 prompt 执行器（application.Submit）→ 真实
// provider 的一轮对话 → 回到快照与面板数据源。
//
// 为什么用 1 分钟周期：调度器的最小周期是 30s（minScheduledInterval），而"周期
// 真的按周期滚动"只能在**真的等过一个周期**之后才谈得上。1 分钟是能在一次冒烟里
// 看到"触发 → 跑完 → 下次运行时间推进"的最小粒度；这一条也正是"每个周期按当前
// 时间"那个口径（不给锚点 = Interval 滚动），锚点日历（每天 09:00 / 每周一 09:00）
// 由 seelebridge/scheduler 的表驱动用例覆盖（那边钉得住日历，不需要等一天）。
//
// 判据（全部来自真实 wire 行为）：
//  1. 创建即排期：IntervalSec=60、NextRunAt ≈ now+60s、面板数据源里能看到它；
//  2. 一个周期后**真的触发**：RunCount>=1、LastStatus=ok；
//  3. 提示词真的进了会话并被真实模型回答（会话里有提示词原文与助手回复）；
//  4. 周期语义成立：跑完后的 NextRunAt 晚于 LastRunAt（下一个周期在等着）；
//  5. 取消后从面板数据源里消失。
//
// 运行（config/accounts.yaml 的内容不会被读取/打印）：
//
//	$env:SEELEX_SMOKE_ACCOUNTS = (Resolve-Path config/accounts.yaml)
//	go test -tags manualsmoke . -run TestManualSmokeRealAccountScheduledPromptChain -count=1 -v -timeout=8m
func TestManualSmokeRealAccountScheduledPromptChain(t *testing.T) {
	accountsSource := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsSource == "" {
		t.Skip("set SEELEX_SMOKE_ACCOUNTS to an accounts.yaml path to run the live smoke test")
	}
	projectRoot := t.TempDir()
	accountsPath := filepath.Join(projectRoot, "accounts.yaml")
	copyOpaqueFile(t, accountsSource, accountsPath)

	// 工具超时给足：这一轮里模型只回一句话，但排队 + 流式 + 落盘都要时间。
	harness := newFullChainHarness(t, accountsPath, projectRoot, 90*time.Second)
	bridge, err := gui.NewBridge(harness.app, gui.Options{Title: "Seelex 定时任务冒烟"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	bridge.Start(ctx, func(context.Context, string, any) {})
	defer bridge.Stop()

	marker := "周期任务冒烟-" + time.Now().Format("150405")
	prompt := "只回复这一行，不要调用任何工具：" + marker

	created, err := bridge.ScheduleTask(seelebridge.ScheduledTaskSpec{
		Name: "冒烟·每分钟", Kind: seelebridge.ScheduledTaskPrompt,
		Prompt: prompt, Interval: time.Minute, Enabled: true,
	})
	if err != nil {
		t.Fatalf("真链路创建定时任务失败（前端 invoke 的同一落点）：%v", err)
	}
	t.Logf("已创建：id=%s kind=%s interval=%ds next_run_at=%s",
		created.ID, created.Kind, created.IntervalSec, created.NextRunAt.Format(time.RFC3339))

	// 判据 1：创建即排期，且面板数据源（GUI 定时任务表格）里立刻能看到它。
	if created.IntervalSec != 60 {
		t.Fatalf("interval_seconds = %d, want 60", created.IntervalSec)
	}
	delay := time.Until(created.NextRunAt)
	if delay < 50*time.Second || delay > 70*time.Second {
		t.Fatalf("首个触发点在 %v 之后，want ≈ 1 分钟", delay)
	}
	if !scheduledTaskVisible(bridge.Snapshot().Runtime.ScheduledTasks, created.ID) {
		t.Fatalf("面板数据源里看不到刚建的任务：%+v", bridge.Snapshot().Runtime.ScheduledTasks)
	}

	// 判据 2 + 3：等它真的触发，并等真实模型把这一轮跑完。
	deadlineFirstRun := time.Now().Add(3 * time.Minute)
	var fired seelebridge.ScheduledTaskStatus
	for {
		snapshot := bridge.Snapshot()
		for _, task := range snapshot.Runtime.ScheduledTasks {
			if task.ID == created.ID && task.RunCount >= 1 {
				fired = task
			}
		}
		if fired.RunCount >= 1 {
			break
		}
		if time.Now().After(deadlineFirstRun) {
			t.Fatalf("1 分钟周期任务在 3 分钟内没有触发：%+v", bridge.Snapshot().Runtime.ScheduledTasks)
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("已触发：run_count=%d last_status=%s last_run_at=%s",
		fired.RunCount, fired.LastStatus, fired.LastRunAt.Format(time.RFC3339))

	// 判据 3（落点）：每次触发默认**新建会话**，任务快照的 last_session_id 就是
	// 那一次真正落到的会话；它必须不是创建任务时用户正在看的那个会话。
	if fired.LastSessionID == "" {
		t.Fatalf("触发没有记录落点会话（last_session_id 为空）：%+v", fired)
	}
	if fired.LastSessionID == bridge.Snapshot().Session.ID {
		t.Fatalf("触发落到了用户当前会话 %q，want 新建会话", fired.LastSessionID)
	}

	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("触发后的会话没有回到 idle：%v\n%s", err, allGoroutineStacks())
	}
	// 触发那一下异步提交到**新会话**，模型回答可能还在路上：再等一小会直到那个
	// 会话里出现提示词。
	conversation := waitForConversationMarker(t, ctx, bridge, fired.LastSessionID, marker, 90*time.Second)
	if !strings.Contains(conversation, marker) {
		t.Fatalf("提示词没有进触发新建的会话 %s：%q", fired.LastSessionID, conversation)
	}
	t.Logf("新会话 %s 里已看到提示词与回复：%s", fired.LastSessionID, tailLine(conversation))

	// 判据 4：跑完之后下一个周期在等着（周期语义，不是"跑一次就停"）。
	status := scheduledTaskByID(bridge.Snapshot().Runtime.ScheduledTasks, created.ID)
	if status == nil {
		t.Fatal("跑完之后任务从面板数据源里消失了")
	}
	if status.LastStatus != dto.ScheduleRunOK {
		t.Fatalf("上一次运行结果 = %q（错误：%s），want ok", status.LastStatus, status.LastError)
	}
	if status.RunCount != 1 {
		t.Fatalf("一个周期内跑了 %d 次，want 1（周期没有重新排期或排重了？）", status.RunCount)
	}
	if !status.NextRunAt.After(status.LastRunAt) {
		t.Fatalf("下次运行时间 %v 没有推进到上次运行之后 %v", status.NextRunAt, status.LastRunAt)
	}
	if leftover := strings.TrimSpace(status.LastError); leftover != "" {
		t.Fatalf("任务残留错误：%s", leftover)
	}
	t.Logf("周期语义成立：last_run_at=%s next_run_at=%s（相差 %v）",
		status.LastRunAt.Format(time.RFC3339), status.NextRunAt.Format(time.RFC3339),
		status.NextRunAt.Sub(status.LastRunAt).Round(time.Second))

	// 判据 5：**编辑**走真实链路（GUI Bridge 的同一落点 UpdateScheduledTask）：
	// ID 不变、定义整体替换、下次运行按新定义重算，运行账目保留。
	renamed := "冒烟·每分钟（已编辑 " + time.Now().Format("150405") + "）"
	edited, err := bridge.UpdateScheduledTask(created.ID, seelebridge.ScheduledTaskSpec{
		Name: renamed, Kind: seelebridge.ScheduledTaskPrompt,
		Prompt: prompt + "（编辑后）", Interval: time.Minute, Enabled: true,
	})
	if err != nil {
		t.Fatalf("真链路编辑定时任务失败：%v", err)
	}
	if edited.ID != created.ID || edited.Name != renamed {
		t.Fatalf("编辑结果不对：id=%s name=%s（want id=%s name=%s）", edited.ID, edited.Name, created.ID, renamed)
	}
	current := scheduledTaskByID(bridge.Snapshot().Runtime.ScheduledTasks, created.ID)
	if current == nil || current.Name != renamed {
		t.Fatalf("面板数据源没有跟上编辑：%+v", current)
	}
	if current.RunCount < status.RunCount {
		t.Fatalf("编辑清掉了运行账目：before=%d after=%d", status.RunCount, current.RunCount)
	}
	if !current.NextRunAt.After(time.Now()) {
		t.Fatalf("编辑后没有重算下次运行：%v", current.NextRunAt)
	}
	t.Logf("编辑成立：id=%s name=%s run_count=%d next_run_at=%s",
		current.ID, current.Name, current.RunCount, current.NextRunAt.Format(time.RFC3339))

	// 判据 6：取消即从面板数据源消失（别让它继续每分钟烧一次真实 API）。
	if err := bridge.CancelScheduledTask(created.ID); err != nil {
		t.Fatalf("取消任务失败：%v", err)
	}
	if scheduledTaskVisible(bridge.Snapshot().Runtime.ScheduledTasks, created.ID) {
		t.Fatal("取消之后任务仍在面板数据源里")
	}
	t.Logf("=== 真实 API 定时任务链路冒烟通过（创建 → 排期 → 触发 → 新建会话 → 周期滚动 → 编辑 → 取消）===")
}

// waitForConversationMarker 等到**指定会话**里出现标记（提示词原文或助手回复），
// 返回拼起来的会话文本。轮询的是 Bridge.SnapshotOf——前端读的同一份会话快照
// （定时触发默认新建会话，正文不在当前视图会话里）。
func waitForConversationMarker(t *testing.T, ctx context.Context, bridge *gui.Bridge, sessionID, marker string, budget time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(budget)
	conversation := ""
	for {
		if snapshot, err := bridge.SnapshotOf(sessionID); err == nil {
			conversation = sessionConversationText(snapshot)
		}
		if strings.Contains(conversation, marker) {
			return conversation
		}
		if ctx.Err() != nil {
			t.Fatalf("等待会话出现提示词时上下文结束：%v", ctx.Err())
		}
		if time.Now().After(deadline) {
			return conversation
		}
		time.Sleep(time.Second)
	}
}

// sessionConversationText 把会话快照的可见会话拼成一段文本（判据只看"出现过没有"）。
func sessionConversationText(snapshot application.SessionSnapshot) string {
	parts := make([]string, 0, len(snapshot.Conversation))
	for _, message := range snapshot.Conversation {
		parts = append(parts, message.Content)
	}
	return strings.Join(parts, "\n")
}

func scheduledTaskByID(tasks []seelebridge.ScheduledTaskStatus, id string) *seelebridge.ScheduledTaskStatus {
	for index := range tasks {
		if tasks[index].ID == id {
			return &tasks[index]
		}
	}
	return nil
}

func scheduledTaskVisible(tasks []seelebridge.ScheduledTaskStatus, id string) bool {
	return scheduledTaskByID(tasks, id) != nil
}

// tailLine 取最后一行非空内容（日志用，避免把整段会话刷进测试输出）。
func tailLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return line
		}
	}
	return ""
}
