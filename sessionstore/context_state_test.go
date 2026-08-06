package sessionstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// sessionRecordStateV3 是生产 persistCurrentSession 写入 state 通道的
// SessionRecord（version=3，与 SessionContextRecord 的 schema 完全不同）。
const sessionRecordStateV3 = `{"version":3,"id":"session","title":{"value":"t"},"conversation":{"messages":[{"id":"message-1","role":"user","content":"hello","created_at":"2026-08-06T00:00:00Z"}]}}`

// TestContextStateIsolatedFromSessionState 契约测试：context 模块独立通道
// 与 SessionRecord state blob 物理隔离，互不覆盖（阶段 3 关键场景：此前
// SessionContextStore 与 SessionRecord 共用 state.json 会互相破坏）。
func TestContextStateIsolatedFromSessionState(t *testing.T) {
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

			// 生产写入路径：Commit 落 SessionRecord 到 state 通道。
			if err := repository.WriteCommit(context.Background(), key, Commit{
				ProviderHistory: messages(1, "provider"),
				State:           []byte(sessionRecordStateV3),
			}); err != nil {
				t.Fatal(err)
			}
			// context 模块经独立通道写入：与 SessionRecord 物理隔离。
			if err := repository.WriteContextState(context.Background(), key, []byte(`{"schema_version":1,"system_prompt":"p","compact_stack":[]}`)); err != nil {
				t.Fatal(err)
			}
			// 两个通道互不覆盖：state 仍是 SessionRecord，context 仍是 SessionContextRecord。
			state, err := repository.ReadState(context.Background(), key)
			if err != nil || string(state) != string([]byte(sessionRecordStateV3)) {
				t.Fatalf("state channel corrupted: %s err=%v", state, err)
			}
			contextState, err := repository.ReadContextState(context.Background(), key)
			if err != nil || string(contextState) != `{"schema_version":1,"system_prompt":"p","compact_stack":[]}` {
				t.Fatalf("context channel corrupted: %s err=%v", contextState, err)
			}
			// context 通道删除/隔离：删除会话后两者都不可读。
			if err := repository.Delete(context.Background(), key); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.ReadContextState(context.Background(), key); !isSessionNotFound(err) {
				t.Fatalf("context after delete err = %v, want not-found", err)
			}
		})
	}
}

// TestSessionContextStorePersistsToIsolatedChannel 验证 SessionContextStore
// 经独立通道持久化：Commit 写 SessionRecord 后，PushCompact + Persist 不
// 破坏 state 通道；新实例 Load 读回 compact 帧。
func TestSessionContextStorePersistsToIsolatedChannel(t *testing.T) {
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
			testRouter := &Router{repository: repository}
			testRouter.SetWorkspace("project")
			key := Key{ProjectID: "project", SessionID: "session"}

			if err := repository.WriteCommit(context.Background(), key, Commit{
				ProviderHistory: messages(1, "provider"),
				State:           []byte(sessionRecordStateV3),
			}); err != nil {
				t.Fatal(err)
			}

			store := NewSessionContextStore(testRouter, "session")
			if err := store.Load(context.Background()); err != nil {
				t.Fatalf("load over SessionRecord state must not fail: %v", err)
			}
			if err := store.PushCompact(CompactFrame{
				SegmentID: "compact-session-1", From: 0, To: 3, Summary: "s",
				RoundFrom: 1, RoundTo: 4, EventFrom: 1, EventTo: 21, CompressedAt: time.Now(),
			}); err != nil {
				t.Fatal(err)
			}
			// Persist 后 state 通道仍是 SessionRecord。
			if err := store.Persist(context.Background()); err != nil {
				t.Fatal(err)
			}
			state, err := repository.ReadState(context.Background(), key)
			if err != nil || string(state) != string([]byte(sessionRecordStateV3)) {
				t.Fatalf("state channel corrupted after context persist: %s err=%v", state, err)
			}
			// 新实例 Load 读回 compact 帧（跨实例持久）。
			reloaded := NewSessionContextStore(testRouter, "session")
			if err := reloaded.Load(context.Background()); err != nil {
				t.Fatal(err)
			}
			record := reloaded.Snapshot()
			if len(record.CompactStack) != 1 || record.CompactStack[0].SegmentID != "compact-session-1" || record.CompactStack[0].RoundTo != 4 {
				t.Fatalf("reloaded compact stack = %+v", record.CompactStack)
			}
		})
	}
}

// TestContextStateMissingReturnsNotFound 验证缺失 context 返回 not-found
// （SessionContextStore.Load 据此静默初始化为空记录）。
func TestContextStateMissingReturnsNotFound(t *testing.T) {
	repository, err := newJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReadContextState(context.Background(), Key{ProjectID: "project", SessionID: "missing"}); !isSessionNotFound(err) {
		t.Fatalf("missing context err = %v, want not-found", err)
	}
}
