package schedule

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 本文件是从 `seelebridge/scheduler/persistence_test.go` 搬出来的**纯 FileStore 用例**
// （2026-10-16，M4 调度器持久化面拆分）：它们只碰落盘通道，不依赖调度器夹具；
// 依赖 `State` 夹具的那几条留在调度器包里（那边是"经 State 的集成面"）。

// TestFileStoreDropsTornTail 钉住 append-only 的崩溃恢复语义：没有收尾换行的
// 最后一行是没写完的一行，读侧按未提交丢弃（后面哪怕碰巧能解出来也不认）。
func TestFileStoreDropsTornTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduled-tasks.jsonl")
	store := NewFileStore(path)
	if err := store.Append(dto.ScheduledTaskRecord{ID: "sched_ok", Spec: dto.ScheduledTaskSpec{
		Name: "好任务", Kind: dto.ScheduledTaskPrompt, Prompt: "P", Interval: time.Hour, Enabled: true,
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
