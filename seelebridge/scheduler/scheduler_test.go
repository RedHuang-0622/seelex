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

func TestScheduledOneShotTaskRunsOnceThenDisables(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	if err := state.RegisterCommand(helperCommand(t, t.TempDir())); err != nil {
		t.Fatal(err)
	}
	runAt := time.Now().Add(80 * time.Millisecond)
	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "一次性任务", Kind: ScheduledTaskCommand, Command: "helper",
		RunAt: runAt, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created.OneShot || created.RunAt.IsZero() || !created.RunAt.Equal(runAt) {
		t.Fatalf("one-shot fields not carried: %+v", created)
	}
	if !created.Enabled || created.NextRunAt.IsZero() || !created.NextRunAt.Equal(runAt) {
		t.Fatalf("one-shot must be enabled with next run at RunAt: %+v", created)
	}

	status := waitForStatus(t, state, created.ID, func(status ScheduledTaskStatus) bool {
		return status.RunCount >= 1 && status.LastStatus == dto.ScheduleRunOK
	})
	if status.Enabled {
		t.Fatalf("one-shot must auto-disable after run: %+v", status)
	}
	if !status.NextRunAt.IsZero() {
		t.Fatalf("one-shot must clear next run after execution: %+v", status)
	}
	if status.RunCount != 1 {
		t.Fatalf("one-shot must run exactly once: %+v", status)
	}
	time.Sleep(150 * time.Millisecond)
	for _, snapshot := range state.Snapshot() {
		if snapshot.ID == created.ID && snapshot.RunCount != 1 {
			t.Fatalf("one-shot ran more than once: %+v", snapshot)
		}
	}
}

func TestScheduledOneShotValidation(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	if err := state.RegisterCommand(helperCommand(t, t.TempDir())); err != nil {
		t.Fatal(err)
	}
	past := ScheduledTaskSpec{
		Name: "过期定时", Kind: ScheduledTaskCommand, Command: "helper",
		RunAt: time.Now().Add(-time.Minute), Enabled: true,
	}
	if _, err := state.Schedule(context.Background(), past); err == nil {
		t.Fatal("one-shot in the past must be rejected")
	}
	future := ScheduledTaskSpec{
		Name: "未来定时", Kind: ScheduledTaskCommand, Command: "helper",
		RunAt: time.Now().Add(time.Hour), Enabled: false,
	}
	created, err := state.Schedule(context.Background(), future)
	if err != nil {
		t.Fatalf("future one-shot rejected: %v", err)
	}
	if !created.Enabled {
		t.Fatal("one-shot must be force-enabled regardless of input flag")
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
		{dto.PeriodMinute, 5, 5 * time.Minute},
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
		return status.RunCount >= 1 && status.LastStatus == dto.ScheduleRunOK
	})
	if status.LastStatus != dto.ScheduleRunOK {
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
		return status.RunCount >= 1 && status.LastStatus == dto.ScheduleRunFailed
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
		return status.RunCount >= 1 && status.LastStatus == dto.ScheduleRunOK
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
		return status.RunCount >= 1 && status.LastStatus == dto.ScheduleRunFailed
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

// ── 周期锚点（起始时刻 / 星期几）────────────────────────────────────────
//
// 口径（2026-10-07 用户现场）：「时间可以说是一天的几点开始的周期，同样的一周
// 的星期几开始的几点开始的也可以添加限制条件；如果没明说，那么需要勾选一个
// 每个周期按当前时间」。于是周期有两个锚点口径：
//   - 给了开始时间 → 墙钟对齐：每天 09:00 / 每周一 09:00 / 每月同日 09:00；
//   - 没给开始时间 → 滚动：now + 周期，即"每个周期走当前时间"。
// 锚点算的是"严格晚于 now 的第一个锚点时刻"，所以创建的当刻不会立刻触发。

// TestNextScheduledAtAnchoredPeriods 是锚点排期的表驱动钉子。
// now 一律钉在 2026-10-07（星期三）这一天，期望值写成字面日历，
// 不用被测函数自己推出来的东西当期望。
func TestNextScheduledAtAnchoredPeriods(t *testing.T) {
	base := time.Date(2026, 10, 7, 8, 0, 0, 0, time.Local) // 星期三 08:00
	if base.Weekday() != time.Wednesday {
		t.Fatalf("基准日不是星期三：%v（日历口径变了，期望值需要重算）", base.Weekday())
	}
	at := func(spec ScheduledTaskSpec, now time.Time) time.Time { return nextScheduledAt(now, spec) }

	cases := []struct {
		name string
		now  time.Time
		spec ScheduledTaskSpec
		want time.Time
	}{
		{
			"每天 09:00：创建时还没到点，今天就跑",
			base,
			ScheduledTaskSpec{PeriodUnit: dto.PeriodDay, PeriodValue: 1, StartClock: "09:00"},
			time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local),
		},
		{
			"每天 09:00：创建时已过点，顺延到明天",
			time.Date(2026, 10, 7, 10, 0, 0, 0, time.Local),
			ScheduledTaskSpec{PeriodUnit: dto.PeriodDay, PeriodValue: 1, StartClock: "09:00"},
			time.Date(2026, 10, 8, 9, 0, 0, 0, time.Local),
		},
		{
			"每 2 天 09:00：以锚点日历天为基准跳 2 天",
			time.Date(2026, 10, 7, 10, 0, 0, 0, time.Local),
			ScheduledTaskSpec{PeriodUnit: dto.PeriodDay, PeriodValue: 2, StartClock: "09:00"},
			time.Date(2026, 10, 9, 9, 0, 0, 0, time.Local),
		},
		{
			"每周一 09:00：本週一已过，取下周一的同一墙钟",
			time.Date(2026, 10, 7, 10, 0, 0, 0, time.Local),
			ScheduledTaskSpec{PeriodUnit: dto.PeriodWeek, PeriodValue: 1, StartClock: "09:00", StartWeekday: dto.WeekdayMonday},
			time.Date(2026, 10, 12, 9, 0, 0, 0, time.Local),
		},
		{
			"每周三 09:00：今天就是星期三、还没到点，今天跑",
			base,
			ScheduledTaskSpec{PeriodUnit: dto.PeriodWeek, PeriodValue: 1, StartClock: "09:00", StartWeekday: 3},
			time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local),
		},
		{
			"每周三 09:00：今天已过点，顺延一周",
			time.Date(2026, 10, 7, 10, 0, 0, 0, time.Local),
			ScheduledTaskSpec{PeriodUnit: dto.PeriodWeek, PeriodValue: 1, StartClock: "09:00", StartWeekday: 3},
			time.Date(2026, 10, 14, 9, 0, 0, 0, time.Local),
		},
		{
			"每周日 09:00：星期天是 ISO 7，跨周边界要对",
			time.Date(2026, 10, 7, 10, 0, 0, 0, time.Local),
			ScheduledTaskSpec{PeriodUnit: dto.PeriodWeek, PeriodValue: 1, StartClock: "09:00", StartWeekday: dto.WeekdaySunday},
			time.Date(2026, 10, 11, 9, 0, 0, 0, time.Local),
		},
		{
			"每 2 周周一 09:00：首个锚点仍是最近的周一",
			time.Date(2026, 10, 7, 10, 0, 0, 0, time.Local),
			ScheduledTaskSpec{PeriodUnit: dto.PeriodWeek, PeriodValue: 2, StartClock: "09:00", StartWeekday: dto.WeekdayMonday},
			time.Date(2026, 10, 12, 9, 0, 0, 0, time.Local),
		},
		{
			"每周 09:00（不给星期）：按创建那一周的当天墙钟滚动",
			base,
			ScheduledTaskSpec{PeriodUnit: dto.PeriodWeek, PeriodValue: 1, StartClock: "09:00"},
			time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local),
		},
		{
			"每月 09:00：本月同日还没到点，本月跑",
			base,
			ScheduledTaskSpec{PeriodUnit: dto.PeriodMonth, PeriodValue: 1, StartClock: "09:00"},
			time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local),
		},
		{
			"每月 09:00：本月同日已过点，顺延到下月同日",
			time.Date(2026, 10, 7, 10, 0, 0, 0, time.Local),
			ScheduledTaskSpec{PeriodUnit: dto.PeriodMonth, PeriodValue: 1, StartClock: "09:00"},
			time.Date(2026, 11, 7, 9, 0, 0, 0, time.Local),
		},
		{
			"每月 09:00：1-31 加一月按月末钳制（复算 addCalendarMonths 的日历语义）",
			time.Date(2026, 1, 31, 10, 0, 0, 0, time.Local),
			ScheduledTaskSpec{PeriodUnit: dto.PeriodMonth, PeriodValue: 1, StartClock: "09:00"},
			time.Date(2026, 2, 28, 9, 0, 0, 0, time.Local),
		},
		{
			"没给开始时间：滚动口径（每天 = now + 24h）",
			time.Date(2026, 10, 7, 10, 0, 0, 0, time.Local),
			ScheduledTaskSpec{PeriodUnit: dto.PeriodDay, PeriodValue: 1},
			time.Date(2026, 10, 8, 10, 0, 0, 0, time.Local),
		},
		{
			"没给开始时间：滚动口径（每周 = now + 7 天，同墙钟）",
			time.Date(2026, 10, 7, 10, 0, 0, 0, time.Local),
			ScheduledTaskSpec{PeriodUnit: dto.PeriodWeek, PeriodValue: 1},
			time.Date(2026, 10, 14, 10, 0, 0, 0, time.Local),
		},
		{
			"每分钟：now + 1 分钟（子日周期不接受锚点）",
			time.Date(2026, 10, 7, 10, 0, 0, 0, time.Local),
			ScheduledTaskSpec{PeriodUnit: dto.PeriodMinute, PeriodValue: 1},
			time.Date(2026, 10, 7, 10, 1, 0, 0, time.Local),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := at(tc.spec, tc.now)
			if !got.Equal(tc.want) {
				t.Fatalf("nextScheduledAt = %v, want %v（差 %v）", got, tc.want, got.Sub(tc.want))
			}
		})
	}
}

// TestScheduledAnchorContractValidation 钉住锚点与周期单位的搭配：
// 子日周期不收锚点、固定间隔不收锚点、星期只归周周期、星期必须配 HH:MM、
// HH:MM 必须真的是 HH:MM。
//
// 反面写法（"忽略不认识的就当没给"）在这里是**故意拒绝**：静默吞掉锚点会让
// 「每天 09:00」悄悄退化成「创建时刻起每 24 小时」，用户看不到任何提示。
func TestScheduledAnchorContractValidation(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	state.SetPromptExecutor(func(context.Context, string, string) (string, error) { return "ok", nil })

	base := ScheduledTaskSpec{Name: "锚点", Kind: ScheduledTaskPrompt, Prompt: "巡检", PeriodValue: 1, Enabled: true}
	rejected := []struct {
		name string
		spec ScheduledTaskSpec
	}{
		{"子日周期不给锚点", ScheduledTaskSpec{PeriodUnit: dto.PeriodDay, StartWeekday: dto.WeekdayMonday}},
		{"小时周期不给开始时间", ScheduledTaskSpec{PeriodUnit: dto.PeriodHour, StartClock: "09:00"}},
		{"分钟周期不给开始时间", ScheduledTaskSpec{PeriodUnit: dto.PeriodMinute, StartClock: "09:00"}},
		{"固定间隔不给开始时间", ScheduledTaskSpec{StartClock: "09:00"}},
		{"固定间隔不给星期", ScheduledTaskSpec{StartWeekday: dto.WeekdayMonday}},
		{"星期只归周周期（天）", ScheduledTaskSpec{PeriodUnit: dto.PeriodDay, StartClock: "09:00", StartWeekday: 1}},
		{"星期只归周周期（月）", ScheduledTaskSpec{PeriodUnit: dto.PeriodMonth, StartClock: "09:00", StartWeekday: 1}},
		{"给了星期却没给开始时间", ScheduledTaskSpec{PeriodUnit: dto.PeriodWeek, StartWeekday: 1}},
		{"星期越界", ScheduledTaskSpec{PeriodUnit: dto.PeriodWeek, StartClock: "09:00", StartWeekday: 8}},
		{"开始时间不是时刻", ScheduledTaskSpec{PeriodUnit: dto.PeriodDay, StartClock: "9点半"}},
		{"开始时间小时越界", ScheduledTaskSpec{PeriodUnit: dto.PeriodDay, StartClock: "25:00"}},
		{"开始时间分钟越界", ScheduledTaskSpec{PeriodUnit: dto.PeriodDay, StartClock: "09:60"}},
		{"开始时间带秒", ScheduledTaskSpec{PeriodUnit: dto.PeriodDay, StartClock: "09:00:00"}},
		{"一次性任务不给锚点", ScheduledTaskSpec{RunAt: time.Now().Add(time.Hour), StartClock: "09:00"}},
		{"一次性任务不给星期", ScheduledTaskSpec{RunAt: time.Now().Add(time.Hour), StartWeekday: dto.WeekdayMonday}},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			spec := base
			spec.PeriodUnit, spec.StartClock, spec.StartWeekday = tc.spec.PeriodUnit, tc.spec.StartClock, tc.spec.StartWeekday
			spec.RunAt = tc.spec.RunAt
			created, err := state.Schedule(context.Background(), spec)
			if err == nil {
				t.Fatalf("锚点搭配非法却排上了期：%+v（next=%v）", tc.spec, created.NextRunAt)
			}
		})
	}
}

// TestScheduledAnchoredTaskCarriesAnchor 钉住锚点真的进了状态快照：面板要按
// start_clock / start_weekday 显示「每 1 周 周一 09:00」这一行。
func TestScheduledAnchoredTaskCarriesAnchor(t *testing.T) {
	state := newSchedulerTestState(t)
	defer state.Stop()
	state.SetPromptExecutor(func(context.Context, string, string) (string, error) { return "ok", nil })

	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "周一巡检", Kind: ScheduledTaskPrompt, Prompt: "巡检",
		PeriodUnit: dto.PeriodWeek, PeriodValue: 1, StartClock: "09:00", StartWeekday: dto.WeekdayMonday,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("周锚点任务被拒：%v", err)
	}
	if created.StartClock != "09:00" || created.StartWeekday != dto.WeekdayMonday {
		t.Fatalf("锚点没进状态快照：%+v", created)
	}
	if created.NextRunAt.Hour() != 9 || created.NextRunAt.Minute() != 0 {
		t.Fatalf("首个触发点没有落在 09:00：%v", created.NextRunAt)
	}
	if created.NextRunAt.Weekday() != time.Monday {
		t.Fatalf("首个触发点不是周一：%v", created.NextRunAt)
	}
	snapshot := state.Snapshot()
	if len(snapshot) != 1 || snapshot[0].StartClock != "09:00" || snapshot[0].StartWeekday != dto.WeekdayMonday {
		t.Fatalf("快照丢了锚点：%+v", snapshot)
	}

	// 宽松输入归一：`9:00` 与 `09:00` 在快照里只有一种写法。
	loose, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "短横写法", Kind: ScheduledTaskPrompt, Prompt: "巡检",
		PeriodUnit: dto.PeriodDay, PeriodValue: 1, StartClock: "9:05", Enabled: true,
	})
	if err != nil {
		t.Fatalf("`9:05` 应当被接受（time.Parse 收紧放）：%v", err)
	}
	if loose.StartClock != "09:05" {
		t.Fatalf("锚点没有归一成 HH:MM：%q", loose.StartClock)
	}
}
