package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ── 定时周期任务（scheduler）测试 ────────────────────────────────────────
// 包级变量（tick 粒度/最小周期）由各测试下调并恢复；不用 t.Parallel。

// helperEnvMarker 标记白名单命令子进程入口（被调度的"外部脚本"）。
const helperEnvMarker = "SEELEX_SCHED_HELPER"

// TestScheduledCommandHelperProcess 是白名单命令测试的子进程入口：
// 正常测试运行直接返回；被调度器拉起时打印标记后退出（可配置失败/睡眠）。
func TestScheduledCommandHelperProcess(t *testing.T) {
	if os.Getenv(helperEnvMarker) != "1" {
		return
	}
	if sleep := os.Getenv("SEELEX_SCHED_SLEEP_MS"); sleep != "" {
		if ms, err := strconv.Atoi(sleep); err == nil && ms > 0 {
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
	}
	fmt.Println("helper-output-ok")
	if os.Getenv("SEELEX_SCHED_FAIL") == "1" {
		os.Exit(3)
	}
	os.Exit(0)
}

func newSchedulerTestState(t *testing.T) *State {
	t.Helper()
	oldTick, oldMin := schedulerTick, minScheduledInterval
	schedulerTick = 15 * time.Millisecond
	minScheduledInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		schedulerTick = oldTick
		minScheduledInterval = oldMin
	})
	return NewState()
}

// helperCommand 构造指向测试二进制的白名单命令（子进程入口见
// TestScheduledCommandHelperProcess）。标记环境变量随 exec 继承
// （SEELEX_SCHED_HELPER 不在凭据清洗名单内，会原样传给子进程）。
func helperCommand(t *testing.T, dir string) ScheduledCommand {
	t.Helper()
	t.Setenv(helperEnvMarker, "1")
	return ScheduledCommand{
		Key: "helper", Label: "测试助手", WorkingDir: dir,
		Argv: []string{os.Args[0], "-test.run=TestScheduledCommandHelperProcess", "-test.count=1"},
	}
}

func waitForStatus(t *testing.T, state *State, id string, want func(ScheduledTaskStatus) bool) ScheduledTaskStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, status := range state.Snapshot() {
			if status.ID == id && want(status) {
				return status
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for task %s", id)
	return ScheduledTaskStatus{}
}

func TestScheduledTaskValidation(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	if err := state.RegisterCommand(helperCommand(t, t.TempDir())); err != nil {
		t.Fatal(err)
	}

	base := ScheduledTaskSpec{Name: "任务", Kind: ScheduledTaskCommand, Command: "helper", Interval: time.Second, Enabled: true}
	if _, err := state.Schedule(context.Background(), base); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}

	emptyName := base
	emptyName.Name = "   "
	if _, err := state.Schedule(context.Background(), emptyName); err == nil {
		t.Fatal("empty name must be rejected")
	}

	shortInterval := base
	shortInterval.Interval = time.Millisecond
	if _, err := state.Schedule(context.Background(), shortInterval); err == nil {
		t.Fatal("interval below minimum must be rejected")
	}

	unknownKind := base
	unknownKind.Kind = "cron"
	if _, err := state.Schedule(context.Background(), unknownKind); err == nil {
		t.Fatal("unknown kind must be rejected")
	}

	unknownCommand := base
	unknownCommand.Command = "rm -rf"
	if _, err := state.Schedule(context.Background(), unknownCommand); err == nil {
		t.Fatal("command not in allowlist must be rejected")
	}

	prompt := ScheduledTaskSpec{Name: "P", Kind: ScheduledTaskPrompt, Prompt: "定时检查", Interval: time.Second, Enabled: true}
	if _, err := state.Schedule(context.Background(), prompt); err == nil {
		t.Fatal("prompt task without executor must be rejected")
	}

	promptEmpty := prompt
	promptEmpty.Prompt = "  "
	state.mu.Lock()
	state.executor = func(context.Context, string, string) (string, error) { return "ok", nil }
	state.mu.Unlock()
	if _, err := state.Schedule(context.Background(), promptEmpty); err == nil {
		t.Fatal("empty prompt must be rejected")
	}
	if _, err := state.Schedule(context.Background(), prompt); err != nil {
		t.Fatalf("valid prompt schedule rejected: %v", err)
	}
}

func TestScheduledPeriodValidationAndStatus(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	if err := state.RegisterCommand(helperCommand(t, t.TempDir())); err != nil {
		t.Fatal(err)
	}

	badUnit := ScheduledTaskSpec{Name: "坏单位", Kind: ScheduledTaskCommand, Command: "helper",
		Interval: time.Second, PeriodUnit: "year", PeriodValue: 1, Enabled: true}
	if _, err := state.Schedule(context.Background(), badUnit); err == nil {
		t.Fatal("unknown period unit must be rejected")
	}

	zeroValue := ScheduledTaskSpec{Name: "零周期", Kind: ScheduledTaskCommand, Command: "helper",
		Interval: time.Second, PeriodUnit: "week", PeriodValue: 0, Enabled: true}
	if _, err := state.Schedule(context.Background(), zeroValue); err == nil {
		t.Fatal("period value below 1 must be rejected")
	}

	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "每月任务", Kind: ScheduledTaskCommand, Command: "helper",
		PeriodUnit: "month", PeriodValue: 1, Enabled: true,
	})
	if err != nil {
		t.Fatalf("monthly schedule rejected: %v", err)
	}
	if created.PeriodUnit != "month" || created.PeriodValue != 1 {
		t.Fatalf("period fields not carried to status: %+v", created)
	}
	if created.IntervalSec != int64(30*24*time.Hour/time.Second) {
		t.Fatalf("monthly nominal interval = %d", created.IntervalSec)
	}
	want := addCalendarMonths(time.Now(), 1)
	if diff := created.NextRunAt.Sub(want); diff > time.Minute || diff < -time.Minute {
		t.Fatalf("next run = %v, want ~ %v", created.NextRunAt, want)
	}
	if snapshot := state.Snapshot(); len(snapshot) != 1 || snapshot[0].PeriodValue != 1 {
		t.Fatalf("snapshot period fields missing: %+v", snapshot)
	}
}

func TestCalendarMonthClamping(t *testing.T) {
	loc := time.Local
	cases := []struct {
		from time.Time
		want time.Time
	}{
		{time.Date(2026, 1, 31, 10, 30, 0, 0, loc), time.Date(2026, 2, 28, 10, 30, 0, 0, loc)},
		{time.Date(2026, 8, 31, 10, 30, 0, 0, loc), time.Date(2026, 9, 30, 10, 30, 0, 0, loc)},
		{time.Date(2026, 12, 31, 23, 59, 59, 0, loc), time.Date(2027, 1, 31, 23, 59, 59, 0, loc)},
	}
	for _, tc := range cases {
		if got := addCalendarMonths(tc.from, 1); !got.Equal(tc.want) {
			t.Fatalf("addCalendarMonths(%v) = %v, want %v", tc.from, got, tc.want)
		}
	}
}

func TestNextScheduledAtPeriods(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		unit dto.PeriodUnit
		n    int
		want time.Duration
	}{
		{dto.PeriodHour, 3, 3 * time.Hour},
		{dto.PeriodDay, 2, 48 * time.Hour},
		{dto.PeriodWeek, 1, 7 * 24 * time.Hour},
	}
	for _, tc := range cases {
		got := nextScheduledAt(now, ScheduledTaskSpec{PeriodUnit: tc.unit, PeriodValue: tc.n})
		if !got.Equal(now.Add(tc.want)) {
			t.Fatalf("nextScheduledAt(%s, %d) = %v, want %v", tc.unit, tc.n, got, now.Add(tc.want))
		}
	}
	if got := nextScheduledAt(now, ScheduledTaskSpec{Interval: 5 * time.Minute}); !got.Equal(now.Add(5 * time.Minute)) {
		t.Fatalf("interval fallback = %v", got)
	}
}

func TestScheduledCommandTaskRunsAndRecordsResult(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	dir := t.TempDir()
	if err := state.RegisterCommand(helperCommand(t, dir)); err != nil {
		t.Fatal(err)
	}
	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "抓职位", Kind: ScheduledTaskCommand, Command: "helper",
		Interval: 100 * time.Millisecond, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Kind != "command" || created.RunCount != 0 {
		t.Fatalf("created status = %+v", created)
	}

	status := waitForStatus(t, state, created.ID, func(status ScheduledTaskStatus) bool {
		return status.RunCount >= 1 && status.LastStatus == "ok"
	})
	if status.LastStatus != "ok" {
		t.Fatalf("last status = %q", status.LastStatus)
	}
	if status.LastResult == "" || !containsText(status.LastResult, "helper-output-ok") {
		t.Fatalf("last result = %q, want helper output", status.LastResult)
	}
	if status.NextRunAt.IsZero() || !status.NextRunAt.After(time.Now().Add(-time.Second)) {
		t.Fatalf("next run not scheduled: %v", status.NextRunAt)
	}
	if len(status.LogTail) == 0 {
		t.Fatal("log tail must record run events")
	}
	// 快照拷贝隔离：外部改动不得影响调度器内部状态。
	snapshot := state.Snapshot()
	snapshot[0].RunCount = 999
	if fresh := state.Snapshot(); fresh[0].RunCount != status.RunCount {
		t.Fatalf("snapshot copy is not isolated: %d vs %d", fresh[0].RunCount, status.RunCount)
	}
}

func TestScheduledCommandFailureExit(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	t.Setenv("SEELEX_SCHED_FAIL", "1")
	command := helperCommand(t, t.TempDir())
	command.Key = "failing"
	if err := state.RegisterCommand(command); err != nil {
		t.Fatal(err)
	}
	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "失败任务", Kind: ScheduledTaskCommand, Command: "failing",
		Interval: 100 * time.Millisecond, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	status := waitForStatus(t, state, created.ID, func(status ScheduledTaskStatus) bool {
		return status.RunCount >= 1 && status.LastStatus == "failed"
	})
	if status.LastError == "" {
		t.Fatal("failure must record last error")
	}
}

func TestScheduledTaskSkipsWhileRunning(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	t.Setenv("SEELEX_SCHED_SLEEP_MS", "600")
	command := helperCommand(t, t.TempDir())
	command.Key = "slow"
	if err := state.RegisterCommand(command); err != nil {
		t.Fatal(err)
	}
	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "慢任务", Kind: ScheduledTaskCommand, Command: "slow",
		Interval: 50 * time.Millisecond, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 周期远小于执行时长：运行中到期的 tick 必须被跳过（不重叠执行）。
	// 首轮运行中 run_count 尚未递增；300ms（≈6 个周期）内若允许重叠，
	// 早已出现第二次运行。
	waitForStatus(t, state, created.ID, func(status ScheduledTaskStatus) bool {
		return status.Running
	})
	time.Sleep(300 * time.Millisecond)
	fresh := waitForStatus(t, state, created.ID, func(status ScheduledTaskStatus) bool { return true })
	if fresh.Running != true || fresh.RunCount != 0 {
		t.Fatalf("overlapping runs detected while busy: %+v", fresh)
	}
	waitForStatus(t, state, created.ID, func(status ScheduledTaskStatus) bool {
		return !status.Running && status.RunCount >= 1
	})
}

func TestScheduledPromptTaskDelegatesToExecutor(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	var gotPrompt, gotSession string
	state.mu.Lock()
	state.executor = func(_ context.Context, prompt, sessionID string) (string, error) {
		gotPrompt, gotSession = prompt, sessionID
		return "submitted", nil
	}
	state.mu.Unlock()
	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "周期提醒", Kind: ScheduledTaskPrompt, Prompt: "每隔一小时检查发布状态",
		Interval: 100 * time.Millisecond, SessionID: "sess_main", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	status := waitForStatus(t, state, created.ID, func(status ScheduledTaskStatus) bool {
		return status.RunCount >= 1 && status.LastStatus == "ok"
	})
	if gotPrompt != "每隔一小时检查发布状态" || gotSession != "sess_main" {
		t.Fatalf("executor args = %q / %q", gotPrompt, gotSession)
	}
	if status.LastResult != "submitted" {
		t.Fatalf("last result = %q, want executor return", status.LastResult)
	}
	if status.Kind != "prompt" || status.SessionID != "sess_main" {
		t.Fatalf("status = %+v", status)
	}
}

func TestScheduledPromptTaskErrorPropagates(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	state.mu.Lock()
	state.executor = func(context.Context, string, string) (string, error) {
		return "", errors.New("会话已切换")
	}
	state.mu.Unlock()
	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "绑定任务", Kind: ScheduledTaskPrompt, Prompt: "P",
		Interval: 100 * time.Millisecond, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	status := waitForStatus(t, state, created.ID, func(status ScheduledTaskStatus) bool {
		return status.RunCount >= 1 && status.LastStatus == "failed"
	})
	if !containsText(status.LastError, "会话已切换") {
		t.Fatalf("last error = %q", status.LastError)
	}
}

func TestScheduledTaskCancelRemovesTask(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	if err := state.RegisterCommand(helperCommand(t, t.TempDir())); err != nil {
		t.Fatal(err)
	}
	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "待取消", Kind: ScheduledTaskCommand, Command: "helper",
		Interval: time.Hour, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Snapshot()) != 1 {
		t.Fatalf("snapshot = %+v", state.Snapshot())
	}
	if err := state.CancelTask(created.ID); err != nil {
		t.Fatal(err)
	}
	if len(state.Snapshot()) != 0 {
		t.Fatal("cancelled task must disappear from snapshot")
	}
	if err := state.CancelTask(created.ID); err == nil {
		t.Fatal("double cancel must fail")
	}
}

func TestScheduledTaskDisabledDoesNotRun(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	if err := state.RegisterCommand(helperCommand(t, t.TempDir())); err != nil {
		t.Fatal(err)
	}
	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "停用任务", Kind: ScheduledTaskCommand, Command: "helper",
		Interval: 30 * time.Millisecond, Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	for _, status := range state.Snapshot() {
		if status.ID == created.ID && status.RunCount != 0 {
			t.Fatalf("disabled task ran: %+v", status)
		}
	}
}

func TestSchedulerShutdownStopsExecution(t *testing.T) {
	state := newSchedulerTestState(t)
	if err := state.RegisterCommand(helperCommand(t, t.TempDir())); err != nil {
		t.Fatal(err)
	}
	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "停机任务", Kind: ScheduledTaskCommand, Command: "helper",
		Interval: 30 * time.Millisecond, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, state, created.ID, func(status ScheduledTaskStatus) bool {
		return status.RunCount >= 1
	})
	state.Stop()
	time.Sleep(150 * time.Millisecond)
	for _, status := range state.Snapshot() {
		if status.ID == created.ID && status.RunCount > 1 {
			t.Fatalf("task kept running after shutdown: %+v", status)
		}
	}
}

func containsText(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
