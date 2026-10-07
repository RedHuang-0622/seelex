package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ── 全局 JSONL 任务定义（粒度 + 冷启动重建）─────────────────────────────
//
// 这一族用例钉住三件事：
//  1. **全局单文件**：任务定义落在一个 JSONL 里，一行一条变更，不按项目分区、
//     不按会话分片（触发产生的会话记录才是会话存储的事）；
//  2. **冷启动重建**：重启后任务还在（周期重算、启用状态保留、工作区保留）；
//  3. **恢复判据**：过期的一次性任务、失效的周期/命令/执行器一律逐条跳过，
//     一条坏记录不挡住其余任务。

// TestScheduledTaskDefinitionLivesInOneGlobalJSONL 钉住"全局粒度"这件事本身：
// 登记写一行，文件里没有项目/会话的痕迹，重启后同一条任务从同一个文件回来。
func TestScheduledTaskDefinitionLivesInOneGlobalJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduled-tasks.jsonl")
	store := NewFileStore(path)
	state := newSchedulerTestStateWithStore(t, store)
	defer state.Stop()
	state.SetPromptExecutor(okPromptExecutor("ok"))

	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "每天九点巡检", Kind: ScheduledTaskPrompt, Prompt: "巡检一遍",
		PeriodUnit: dto.PeriodDay, PeriodValue: 1, StartClock: "09:00",
		WorkspaceID: "ws_1", Enabled: true,
	})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("任务定义没有落到全局 JSONL：%v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("登记应写一行，实得 %d 行：%q", len(lines), string(raw))
	}
	for _, want := range []string{created.ID, `"ws_1"`, "每天九点巡检"} {
		if !strings.Contains(lines[0], want) {
			t.Fatalf("定义行缺少 %q：%s", want, lines[0])
		}
	}
	// 任务定义是进程级资产：行里带的是**工作区**（可选装配目标），不是项目/
	// 会话作用域——粒度是"任务定义全局一份"，跟哪个项目存无关。
	if strings.Contains(strings.ToLower(lines[0]), "project") {
		t.Fatalf("任务定义不该带项目作用域字段：%s", lines[0])
	}
	state.Stop()

	// 冷启动：同一个文件读回同一条任务（ID/工作区/锚点/周期都还在）。
	restored := newSchedulerTestStateWithStore(t, store)
	defer restored.Stop()
	restored.SetPromptExecutor(okPromptExecutor("ok"))
	count, skipped, err := restored.Restore()
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if count != 1 || skipped != 0 {
		t.Fatalf("Restore = (%d, %d), want (1, 0)", count, skipped)
	}
	snapshot := restored.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("恢复后任务数 = %d, want 1", len(snapshot))
	}
	got := snapshot[0]
	if got.ID != created.ID || got.Name != "每天九点巡检" || got.WorkspaceID != "ws_1" {
		t.Fatalf("恢复出来的任务不对：%+v", got)
	}
	if got.StartClock != "09:00" || got.PeriodUnit != string(dto.PeriodDay) || got.PeriodValue != 1 {
		t.Fatalf("恢复丢了周期锚点：%+v", got)
	}
	if !got.Enabled || got.NextRunAt.IsZero() || !got.NextRunAt.After(time.Now()) {
		t.Fatalf("恢复没有重新排期：%+v", got)
	}
	if got.RunCount != 0 || got.LastStatus != scheduledStatusPending {
		t.Fatalf("运行期读数不该跨重启：%+v", got)
	}
}

// TestScheduledTaskCancelWritesTombstone 钉住取消：写一行墓碑，重启后这条任务
// 不再回来（内存删掉、盘上也删掉，不是"面板没了但重启又冒出来"）。
func TestScheduledTaskCancelWritesTombstone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduled-tasks.jsonl")
	store := NewFileStore(path)
	state := newSchedulerTestStateWithStore(t, store)
	state.SetPromptExecutor(okPromptExecutor("ok"))
	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "临时巡检", Kind: ScheduledTaskPrompt, Prompt: "巡检", Interval: time.Hour, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.CancelTask(created.ID); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	state.Stop()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], `"deleted":true`) {
		t.Fatalf("取消应追加一行墓碑：%q", string(raw))
	}

	restored := newSchedulerTestStateWithStore(t, store)
	defer restored.Stop()
	restored.SetPromptExecutor(okPromptExecutor("ok"))
	count, skipped, err := restored.Restore()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 || skipped != 0 || len(restored.Snapshot()) != 0 {
		t.Fatalf("取消过的任务不该恢复：count=%d skipped=%d snapshot=%+v", count, skipped, restored.Snapshot())
	}
}

// TestScheduledTaskUpdatePersistsAsNewRowWithSameID 钉住编辑的持久化形状：同一 ID
// 追加一行新定义（不是新 ID、不是墓碑），冷启动按后写的那行恢复——编辑过的任务
// 重启后是**编辑后**的样子，而不是"忘掉编辑、退回旧定义"。
func TestScheduledTaskUpdatePersistsAsNewRowWithSameID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduled-tasks.jsonl")
	store := NewFileStore(path)
	state := newSchedulerTestStateWithStore(t, store)
	state.SetPromptExecutor(okPromptExecutor("ok"))
	created, err := state.Schedule(context.Background(), ScheduledTaskSpec{
		Name: "旧名字", Kind: ScheduledTaskPrompt, Prompt: "旧提示词", Interval: time.Hour, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Update(context.Background(), created.ID, ScheduledTaskSpec{
		Name: "新名字", Kind: ScheduledTaskPrompt, Prompt: "新提示词",
		WorkspaceID: "ws_2", Interval: 2 * time.Hour, Enabled: true,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	state.Stop()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("登记 + 编辑应各写一行，实得 %d 行：%q", len(lines), string(raw))
	}
	if strings.Contains(lines[1], `"deleted":true`) {
		t.Fatalf("编辑不该写墓碑：%s", lines[1])
	}
	if !strings.Contains(lines[1], created.ID) {
		t.Fatalf("编辑行必须带原 ID（ID 是任务身份）：%s", lines[1])
	}

	restored := newSchedulerTestStateWithStore(t, store)
	defer restored.Stop()
	restored.SetPromptExecutor(okPromptExecutor("ok"))
	count, skipped, err := restored.Restore()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || skipped != 0 {
		t.Fatalf("Restore = (%d, %d), want (1, 0)", count, skipped)
	}
	got := restored.Snapshot()[0]
	if got.ID != created.ID || got.Name != "新名字" || got.Prompt != "新提示词" || got.WorkspaceID != "ws_2" {
		t.Fatalf("恢复出来的是旧定义：%+v", got)
	}
	if got.IntervalSec != int64((2 * time.Hour).Seconds()) {
		t.Fatalf("编辑后的周期没有生效：%+v", got)
	}
}

// TestScheduledTaskRestoreSkipsWhatCannotRun 钉住恢复的**逐条跳过**判据：
// 过期的一次性任务、命令不在白名单、周期非法、执行器未装配，都只跳过自己。
func TestScheduledTaskRestoreSkipsWhatCannotRun(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "scheduled-tasks.jsonl"))
	appendRecord := func(record ScheduledTaskRecord) {
		t.Helper()
		if err := store.Append(record); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	// 过期的一次性任务：进程当时没在跑，时刻过了就不追补。
	appendRecord(ScheduledTaskRecord{ID: "sched_expired", Spec: ScheduledTaskSpec{
		Name: "过期一次性", Kind: ScheduledTaskPrompt, Prompt: "P", RunAt: time.Now().Add(-time.Hour), Enabled: true,
	}, Enabled: true})
	// 命令白名单里没有的键。
	appendRecord(ScheduledTaskRecord{ID: "sched_unknown_cmd", Spec: ScheduledTaskSpec{
		Name: "未知命令", Kind: ScheduledTaskCommand, Command: "nope", Interval: time.Hour, Enabled: true,
	}, Enabled: true})
	// 周期非法（数值为 0）。
	appendRecord(ScheduledTaskRecord{ID: "sched_bad_period", Spec: ScheduledTaskSpec{
		Name: "坏周期", Kind: ScheduledTaskPrompt, Prompt: "P", PeriodUnit: dto.PeriodDay, PeriodValue: 0, Enabled: true,
	}, Enabled: true})
	// 名称为空。
	appendRecord(ScheduledTaskRecord{ID: "sched_no_name", Spec: ScheduledTaskSpec{
		Name: "  ", Kind: ScheduledTaskPrompt, Prompt: "P", Interval: time.Hour, Enabled: true,
	}, Enabled: true})
	// 好任务：它必须在坏记录中间活下来（逐条跳过，不连坐）。
	appendRecord(ScheduledTaskRecord{ID: "sched_future", Spec: ScheduledTaskSpec{
		Name: "明天九点", Kind: ScheduledTaskPrompt, Prompt: "P", RunAt: time.Now().Add(time.Hour), Enabled: true,
	}, Enabled: true})

	state := newSchedulerTestStateWithStore(t, store)
	defer state.Stop()
	state.SetPromptExecutor(okPromptExecutor("ok"))
	count, skipped, err := state.Restore()
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if count != 1 || skipped != 4 {
		t.Fatalf("Restore = (%d, %d), want (1, 4)", count, skipped)
	}
	snapshot := state.Snapshot()
	if len(snapshot) != 1 || snapshot[0].ID != "sched_future" || !snapshot[0].OneShot {
		t.Fatalf("恢复出来的任务集不对：%+v", snapshot)
	}
}

// TestScheduledTaskRestoreSkipsPromptTasksWithoutExecutor 钉住"执行器未装配 =
// 提示词任务不可用"这条装配口径在恢复路径上同样成立（不能只在创建时拦）。
func TestScheduledTaskRestoreSkipsPromptTasksWithoutExecutor(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "scheduled-tasks.jsonl"))
	if err := store.Append(ScheduledTaskRecord{ID: "sched_p", Spec: ScheduledTaskSpec{
		Name: "巡检", Kind: ScheduledTaskPrompt, Prompt: "P", Interval: time.Hour, Enabled: true,
	}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	state := newSchedulerTestStateWithStore(t, store)
	defer state.Stop()
	count, skipped, err := state.Restore()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 || skipped != 1 {
		t.Fatalf("Restore = (%d, %d), want (0, 1)", count, skipped)
	}
}

// TestFileStoreDropsTornTail 钉住 append-only 的崩溃恢复语义：没有收尾换行的
// 最后一行是没写完的一行，读侧按未提交丢弃（后面哪怕碰巧能解出来也不认）。
func TestFileStoreDropsTornTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduled-tasks.jsonl")
	store := NewFileStore(path)
	if err := store.Append(ScheduledTaskRecord{ID: "sched_ok", Spec: ScheduledTaskSpec{
		Name: "好任务", Kind: ScheduledTaskPrompt, Prompt: "P", Interval: time.Hour, Enabled: true,
	}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"id":"sched_torn","spec":{"name":"半行"}`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	records, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(records) != 1 || records[0].ID != "sched_ok" {
		t.Fatalf("残尾没有被丢弃：%+v", records)
	}
}

// TestFileStoreMissingFileIsEmpty 钉住"没有文件 = 空任务集"，而不是错误：
// 首次启动、从未建过任务都不该在启动日志里留一条失败。
func TestFileStoreMissingFileIsEmpty(t *testing.T) {
	records, err := NewFileStore(filepath.Join(t.TempDir(), "nope.jsonl")).Load()
	if err != nil || len(records) != 0 {
		t.Fatalf("Load 缺失文件 = (%v, %v), want (空, nil)", records, err)
	}
	if records, err := NewFileStore("").Load(); err != nil || records != nil {
		t.Fatalf("未配置路径的 Load 应为空操作：(%v, %v)", records, err)
	}
}
