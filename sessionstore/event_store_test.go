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

// TestEventStoreResolvesSessionBindingProject（R3 键漂移回归）：执行事实
// 按会话绑定项目落盘，不随 Router active write scope 复位拆到默认项目。
func TestEventStoreResolvesSessionBindingProject(t *testing.T) {
	router := newTestRouter(t)
	router.SetWorkspace("project-active")

	store := NewEventStore(router)
	store.SetWorkspaceResolver(func(sessionID string) string {
		if sessionID == "sess-bound" {
			return "project-bound"
		}
		return ""
	})
	event := frameworkevent.Event{
		ID: "evt", Sequence: 1, Source: "seelex.telemetry.summary",
		Type: frameworkevent.TypeLifecycle, Status: frameworkevent.StatusFailed,
		Locations: []frameworkevent.Location{{
			Kind: "agent.runtime", IDs: map[string]string{"session_id": "sess-bound"},
		}},
	}
	if err := store.Append(context.Background(), event); err != nil {
		t.Fatalf("append: %v", err)
	}

	// 事件落在绑定项目，而非 active scope。
	bound, err := router.ReadFrameworkEventsWorkspace(context.Background(), "project-bound", "sess-bound")
	if err != nil || len(bound) != 1 {
		t.Fatalf("bound project events = %d err=%v, want 1", len(bound), err)
	}
	active, err := router.ReadFrameworkEventsWorkspace(context.Background(), "project-active", "sess-bound")
	if err != nil || len(active) != 0 {
		t.Fatalf("active project events = %d err=%v, want 0（不得拆到 active scope）", len(active), err)
	}

	// Load 走同一解析器，能读回绑定项目事件。
	loaded, err := store.Load(context.Background(), "sess-bound")
	if err != nil || len(loaded) != 1 {
		t.Fatalf("load via resolver = %d err=%v, want 1", len(loaded), err)
	}
}

// TestEventStoreWithoutResolverFallsBackToActiveScope 验证未注入解析器时
// 保持旧语义（active write scope），不破坏现有装配。
func TestEventStoreWithoutResolverFallsBackToActiveScope(t *testing.T) {
	router := newTestRouter(t)
	router.SetWorkspace("project-active")
	store := NewEventStore(router)
	event := frameworkevent.Event{
		ID: "evt", Sequence: 1, Source: "workplan.runner",
		Type: frameworkevent.TypeLifecycle, Status: frameworkevent.StatusRunning,
		Locations: []frameworkevent.Location{{
			Kind: "agent.runtime", IDs: map[string]string{"session_id": "sess-unbound"},
		}},
	}
	if err := store.Append(context.Background(), event); err != nil {
		t.Fatalf("append: %v", err)
	}
	active, err := router.ReadFrameworkEventsWorkspace(context.Background(), "project-active", "sess-unbound")
	if err != nil || len(active) != 1 {
		t.Fatalf("active project events = %d err=%v, want 1", len(active), err)
	}
}

// TestEventStoreLoadRange（波 4 G7）：事件库按会话/Seq 区间读回（含端点），
// 顺序保持；倒置区间显式报错；空区间返回空不报错。
func TestEventStoreLoadRange(t *testing.T) {
	router := newTestRouter(t)
	store := NewEventStore(router)
	store.SetWorkspaceResolver(func(sessionID string) string {
		if sessionID == "sess-range" {
			return "project-range"
		}
		return ""
	})
	for seq := uint64(1); seq <= 5; seq++ {
		if err := store.Append(context.Background(), frameworkevent.Event{
			ID: "evt", Sequence: seq, Source: "workplan.runner",
			Type: frameworkevent.TypeLifecycle, Status: frameworkevent.StatusRunning,
			Locations: []frameworkevent.Location{{
				Kind: "agent.runtime", IDs: map[string]string{"session_id": "sess-range"},
			}},
		}); err != nil {
			t.Fatalf("append seq %d: %v", seq, err)
		}
	}
	got, err := store.LoadRange(context.Background(), "sess-range", 2, 4)
	if err != nil {
		t.Fatalf("LoadRange: %v", err)
	}
	if len(got) != 3 || got[0].Sequence != 2 || got[1].Sequence != 3 || got[2].Sequence != 4 {
		t.Fatalf("range 2..4 = %#v, want seq 2,3,4 按序", got)
	}
	empty, err := store.LoadRange(context.Background(), "sess-range", 1000, 2000)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty range = %d err=%v, want 0", len(empty), err)
	}
	if _, err := store.LoadRange(context.Background(), "sess-range", 5, 2); err == nil {
		t.Fatal("inverted range must fail explicitly")
	}
	// (0,0) 保持 Load 全量语义。
	all, err := store.LoadRange(context.Background(), "sess-range", 0, 0)
	if err != nil || len(all) != 5 {
		t.Fatalf("full range = %d err=%v, want 5", len(all), err)
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
