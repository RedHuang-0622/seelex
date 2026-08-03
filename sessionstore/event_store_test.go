package sessionstore

import (
	"context"
	"encoding/json"
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
