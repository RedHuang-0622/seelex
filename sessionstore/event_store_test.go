package sessionstore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
)

// TestFrameworkEventLogRoundTripAcrossLocalBackends 验证执行事实事件库
// （event.Sink → sessionstore 事件库；slice 8 双轨事件的事实轨）在
// JSON/SQLite 后端的追加-读取往返：追加顺序保持、Seq 排序、空库读取。
func TestFrameworkEventLogRoundTripAcrossLocalBackends(t *testing.T) {
	for _, config := range []Config{
		{Backend: BackendJSON, Path: filepath.Join(t.TempDir(), "json")},
		{Backend: BackendSQLite, Path: filepath.Join(t.TempDir(), "sessions.db")},
	} {
		t.Run(string(config.Backend), func(t *testing.T) {
			repository, err := Open(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			key := Key{ProjectID: "project", SessionID: "session"}

			entries := []EventLogEntry{
				eventLogEntry(1, frameworkevent.StatusRunning),
				eventLogEntry(2, frameworkevent.StatusCompleted),
			}
			for _, entry := range entries {
				if err := repository.AppendFrameworkEvent(context.Background(), key, entry); err != nil {
					t.Fatal(err)
				}
			}
			got, err := repository.ReadFrameworkEvents(context.Background(), key)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 {
				t.Fatalf("event log length = %d, want 2", len(got))
			}
			for index, want := range entries {
				if got[index].Seq != want.Seq {
					t.Fatalf("event %d seq = %d, want %d", index, got[index].Seq, want.Seq)
				}
				if string(got[index].Payload) != string(want.Payload) {
					t.Fatalf("event %d payload mismatch:\n got %s\nwant %s", index, got[index].Payload, want.Payload)
				}
			}

			// 追加顺序保持（Seq 排序读取）。
			if err := repository.AppendFrameworkEvent(context.Background(), key, eventLogEntry(0, frameworkevent.StatusQueued)); err != nil {
				t.Fatal(err)
			}
			got, err = repository.ReadFrameworkEvents(context.Background(), key)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 3 || got[0].Seq != 0 || got[2].Seq != 2 {
				t.Fatalf("append order lost: %#v", got)
			}
		})
	}
}

// TestFrameworkEventSurvivesGenerationRollover 契约测试（plan.md 验收：
// 连续两次 Commit 后旧 execution facts 仍可读）。framework events 是独立
// append-only 模块，不随 generation rollover 失效。
func TestFrameworkEventSurvivesGenerationRollover(t *testing.T) {
	repository, err := Open(context.Background(), Config{Backend: BackendJSON, Path: filepath.Join(t.TempDir(), "json")})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	key := Key{ProjectID: "project", SessionID: "session"}

	// 第一轮：Commit 创建 generation G1，追加 fact #1。
	if err := repository.WriteCommit(context.Background(), key, Commit{ProviderHistory: messages(1, "first")}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AppendFrameworkEvent(context.Background(), key, eventLogEntry(1, frameworkevent.StatusRunning)); err != nil {
		t.Fatal(err)
	}
	// 第二轮：Commit 创建 generation G2（rollover），追加 fact #2。
	if err := repository.WriteCommit(context.Background(), key, Commit{ProviderHistory: messages(1, "second")}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AppendFrameworkEvent(context.Background(), key, eventLogEntry(2, frameworkevent.StatusCompleted)); err != nil {
		t.Fatal(err)
	}

	got, err := repository.ReadFrameworkEvents(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("event log after rollover = %#v, want seq 1 and 2", got)
	}
	// 第三轮：再次 rollover 后旧 facts 仍可见。
	if err := repository.WriteCommit(context.Background(), key, Commit{ProviderHistory: messages(1, "third")}); err != nil {
		t.Fatal(err)
	}
	got, err = repository.ReadFrameworkEvents(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("event log after second rollover = %#v, want seq 1 and 2", got)
	}
}

// TestFrameworkEventLegacyMigration 验证首次追加时迁移 v1 布局遗留的
// events.json（含已被 rollover 隐藏的旧 generation），旧执行事实与新增
// 事件合并可见，重复 Seq 幂等去重。
func TestFrameworkEventLegacyMigration(t *testing.T) {
	repository, err := Open(context.Background(), Config{Backend: BackendJSON, Path: filepath.Join(t.TempDir(), "json")})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	jsonRepo, ok := repository.(*jsonRepository)
	if !ok {
		t.Fatal("expected JSON repository")
	}
	key := Key{ProjectID: "project", SessionID: "session"}

	// 手工构造 v1 布局：G1 内写入 events.json，然后 rollover 到 G2。
	if err := jsonRepo.WriteCommit(context.Background(), key, Commit{ProviderHistory: messages(1, "first")}); err != nil {
		t.Fatal(err)
	}
	directory := jsonRepo.sessionDir(key)
	manifest, err := jsonRepo.readManifest(directory)
	if err != nil {
		t.Fatal(err)
	}
	legacy := []EventLogEntry{eventLogEntry(1, frameworkevent.StatusRunning)}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(filepath.Join(directory, manifest.Generation, frameworkEventLegacyFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := jsonRepo.WriteCommit(context.Background(), key, Commit{ProviderHistory: messages(1, "second")}); err != nil {
		t.Fatal(err)
	}

	// 首次追加触发迁移：旧 G1 facts 与新增 entry 合并到根 framework-events.json。
	if err := jsonRepo.AppendFrameworkEvent(context.Background(), key, eventLogEntry(2, frameworkevent.StatusCompleted)); err != nil {
		t.Fatal(err)
	}
	got, err := jsonRepo.ReadFrameworkEvents(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("migrated event log = %#v, want seq 1 and 2", got)
	}
	// 幂等：重复追加同一 Seq 不产生重复条目。
	if err := jsonRepo.AppendFrameworkEvent(context.Background(), key, eventLogEntry(2, frameworkevent.StatusCompleted)); err != nil {
		t.Fatal(err)
	}
	got, err = jsonRepo.ReadFrameworkEvents(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("duplicate Seq produced %d entries, want 2", len(got))
	}
	// 未迁移会话的读取回退：直接删除根文件后旧 generation facts 仍可读。
	rootPath := filepath.Join(directory, frameworkEventLogFile)
	if err := os.Remove(rootPath); err != nil {
		t.Fatal(err)
	}
	got, err = jsonRepo.ReadFrameworkEvents(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("legacy fallback = %#v, want seq 1", got)
	}
}

// TestFrameworkEventLogMissingSessionReturnsEmpty 验证空库读取返回空切片。
func TestFrameworkEventLogMissingSessionReturnsEmpty(t *testing.T) {
	repository, err := Open(context.Background(), Config{Backend: BackendJSON, Path: filepath.Join(t.TempDir(), "json")})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	got, err := repository.ReadFrameworkEvents(context.Background(), Key{ProjectID: "project", SessionID: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty log, got %#v", got)
	}
}

func eventLogEntry(seq uint64, status frameworkevent.Status) EventLogEntry {
	payload, err := json.Marshal(frameworkevent.Event{
		ID: "evt", Sequence: seq, Source: "workplan.runner",
		Type: frameworkevent.TypeLifecycle, Status: status,
	})
	if err != nil {
		panic(err)
	}
	return EventLogEntry{Seq: seq, Payload: payload}
}
