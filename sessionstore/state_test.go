package sessionstore

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"
)

func TestStateRoundTripAcrossLocalBackends(t *testing.T) {
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
			want := []byte(`{"version":2,"id":"session","title":{"value":"Stable title"},"plan_stack":[{"id":"plan-1"}],"conversation":{"messages":[]}}`)
			if err := repository.WriteState(context.Background(), key, want); err != nil {
				t.Fatal(err)
			}
			got, err := repository.ReadState(context.Background(), key)
			if config.Backend == BackendJSON {
				// S20：state 通道在 v8 JSON 停写停读。
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("json ReadState err = %v, want fs.ErrNotExist", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != string(want) {
					t.Fatalf("state = %s, want %s", got, want)
				}
			}
			history := testMessages(defaultMessageShardSize+5, string(config.Backend))
			if config.Backend == BackendJSON {
				// v8 JSON 布局（S11）：事件行为正文事实源，ReadRange 由行派生。
				if err := repository.WriteCommit(context.Background(), key, Commit{
					Events: messagesToEventRows(history),
				}); err != nil {
					t.Fatal(err)
				}
			} else if err := repository.WriteAtomic(context.Background(), key, history); err != nil {
				t.Fatal(err)
			}
			window, total, err := repository.ReadRange(context.Background(), key, defaultMessageShardSize-2, 4)
			if err != nil || total != len(history) || len(window) != 4 || window[0].Content == nil {
				t.Fatalf("range=%#v total=%d err=%v", window, total, err)
			}
			if err := repository.Delete(context.Background(), key); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.ReadState(context.Background(), key); err == nil || (!errors.Is(err, fs.ErrNotExist) && !errors.Is(err, sql.ErrNoRows)) {
				t.Fatalf("state after delete error = %v, want not found", err)
			}
		})
	}
}

func TestCommitRoundTripIsAtomicAcrossLocalBackends(t *testing.T) {
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
			content := "stored raw result"
			commit := Commit{
				ProviderHistory: testMessages(2, "provider"),
				Events: []Event{
					{Seq: 1, Role: "user", Content: "inspect", MessageID: "message-1", TokenCount: 1},
					{Seq: 2, Role: "assistant", ToolCalls: []EventToolCall{{ID: "a", Name: "read"}, {ID: "b", Name: "status"}}, TokenCount: 1},
					{Seq: 3, Role: "tool", ToolCallID: "b", Name: "status", Content: "clean", TokenCount: 1},
					{Seq: 4, Role: "tool", ToolCallID: "a", Name: "read", Content: "found", TokenCount: 1},
					{Seq: 5, Role: "assistant", Content: "done", TokenCount: 1},
				},
				State: []byte(`{"version":3,"status":"interrupted"}`),
				ToolResults: []ToolResult{{
					Ref: "tr-result", Tool: "read", Content: content, Digest: "sha256:test",
					Size: len(content), TokenCount: 4, CreatedAt: time.Unix(1, 0),
				}},
			}
			if err := repository.WriteCommit(context.Background(), key, commit); err != nil {
				t.Fatal(err)
			}
			wantHistory := 2
			if config.Backend == BackendJSON {
				// v8 JSON 布局（S11）：Read 由事件行派生（ProviderHistory 与
				// history.json 缓存已退役），5 行事件映射为 5 条消息。
				wantHistory = len(rowsToProviderMessages(commit.Events))
			}
			history, err := repository.Read(context.Background(), key)
			if err != nil || len(history) != wantHistory {
				t.Fatalf("history=%#v err=%v", history, err)
			}
			state, err := repository.ReadState(context.Background(), key)
			if config.Backend == BackendJSON {
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("json state err=%v, want fs.ErrNotExist", err)
				}
			} else if err != nil || string(state) != string(commit.State) {
				t.Fatalf("state=%s err=%v", state, err)
			}
			result, err := repository.ReadToolResult(context.Background(), key, "tr-result")
			if err != nil || result.Content != content {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			tail, err := repository.ReadEventTail(context.Background(), key, 100, 4)
			if err != nil || len(tail) != len(commit.Events) {
				t.Fatalf("tail=%#v err=%v", tail, err)
			}
			if config.Backend == BackendJSON {
				// 公开读接口保留 commit_id 等凭据（S17b/T-EV-07），逐字段
				// 比较行身份与正文。
				if !sameRangeEvents(tail, commit.Events) {
					t.Fatalf("tail=%#v want events", tail)
				}
			} else if !reflect.DeepEqual(tail, commit.Events) {
				t.Fatalf("tail=%#v want events", tail)
			}

			failed := Commit{
				ProviderHistory: testMessages(1, "replacement"),
				State:           []byte(`{"version":3,"status":"completed"}`),
				ToolResults:     []ToolResult{{Tool: "read", Content: "invalid"}},
			}
			if err := repository.WriteCommit(context.Background(), key, failed); err == nil {
				t.Fatal("commit with an empty result ref succeeded")
			}
			history, _ = repository.Read(context.Background(), key)
			state, _ = repository.ReadState(context.Background(), key)
			if len(history) != wantHistory {
				t.Fatalf("failed commit became visible: history=%#v state=%s", history, state)
			}
			if config.Backend != BackendJSON && string(state) != string(commit.State) {
				t.Fatalf("failed commit changed state: %s", state)
			}
		})
	}
}

func TestWriteStateAfterCommitReturnsLatestState(t *testing.T) {
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
			if err := repository.WriteCommit(context.Background(), key, Commit{ProviderHistory: testMessages(1, "first"), State: []byte("first")}); err != nil {
				t.Fatal(err)
			}
			if err := repository.WriteState(context.Background(), key, []byte("second")); err != nil {
				t.Fatal(err)
			}
			state, err := repository.ReadState(context.Background(), key)
			if config.Backend == BackendJSON {
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("json state err=%v, want fs.ErrNotExist", err)
				}
			} else if err != nil || string(state) != "second" {
				t.Fatalf("state=%q err=%v", state, err)
			}
		})
	}
}

func TestEventTailKeepsCompleteUserAndParallelToolUnits(t *testing.T) {
	events := []Event{
		{Seq: 1, Role: "user", Content: "incomplete", TokenCount: 1},
		{Seq: 2, Role: "assistant", ToolCalls: []EventToolCall{{ID: "a"}, {ID: "b"}}, TokenCount: 1},
		{Seq: 3, Role: "tool", ToolCallID: "a", TokenCount: 1},
		{Seq: 4, Role: "user", Content: "complete", TokenCount: 1},
		{Seq: 5, Role: "assistant", ToolCalls: []EventToolCall{{ID: "c"}, {ID: "d"}}, TokenCount: 1},
		{Seq: 6, Role: "tool", ToolCallID: "d", TokenCount: 1},
		{Seq: 7, Role: "tool", ToolCallID: "c", TokenCount: 1},
		{Seq: 8, Role: "assistant", Content: "finished", TokenCount: 1},
		{Seq: 9, Role: "tool", ToolCallID: "orphan", TokenCount: 1},
		{Seq: 10, Role: "user", Content: "plain", TokenCount: 1},
		{Seq: 11, Role: "assistant", Content: "answer", TokenCount: 1},
	}
	tail := selectEventTail(events, 100, 4)
	// 单元 1-3（user + 中断工具链 a/b，只记录了 a）是 UI 可见的开放轮次，
	// 必须进入冷加载尾窗（缺失结果由装配层补齐）；孤儿 tool 9 仍不构成单元。
	wantSeq := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 10, 11}
	gotSeq := make([]uint64, len(tail))
	for index := range tail {
		gotSeq[index] = tail[index].Seq
	}
	if !reflect.DeepEqual(gotSeq, wantSeq) {
		t.Fatalf("tail seq=%v, want %v", gotSeq, wantSeq)
	}
}

func TestEventTailKeepsTrailingUnansweredUserInput(t *testing.T) {
	events := []Event{
		{Seq: 1, Role: "user", Content: "first", TokenCount: 1},
		{Seq: 2, Role: "assistant", Content: "answer", TokenCount: 1},
		{Seq: 3, Role: "user", Content: "please continue from the report", TokenCount: 1},
	}
	tail := selectEventTail(events, 100, 2)
	gotSeq := make([]uint64, len(tail))
	for index := range tail {
		gotSeq[index] = tail[index].Seq
	}
	if want := []uint64{1, 2, 3}; !reflect.DeepEqual(gotSeq, want) {
		t.Fatalf("tail seq=%v, want %v", gotSeq, want)
	}
}

func TestSQLitePersistsHistoryAsShards(t *testing.T) {
	repository, err := Open(context.Background(), Config{Backend: BackendSQLite, Path: filepath.Join(t.TempDir(), "sessions.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	key := Key{ProjectID: "project", SessionID: "session"}
	if err := repository.WriteAtomic(context.Background(), key, testMessages(defaultMessageShardSize+1, "sqlite")); err != nil {
		t.Fatal(err)
	}
	sqlRepo, ok := repository.(*sqlRepository)
	if !ok {
		t.Fatalf("repository type = %T", repository)
	}
	var shards int
	if err := sqlRepo.db.QueryRow(`SELECT COUNT(*) FROM seelex_session_shard WHERE project_id=? AND session_id=?`, key.ProjectID, key.SessionID).Scan(&shards); err != nil {
		t.Fatal(err)
	}
	if shards != 2 {
		t.Fatalf("SQL shard count = %d, want 2", shards)
	}
}

func TestRedisConfigAndKeysUseProjectScopedShardStrategy(t *testing.T) {
	if _, err := (Config{Backend: BackendRedis}).Normalize(t.TempDir()); err == nil {
		t.Fatal("Redis config without DSN succeeded")
	}
	repository := &redisRepository{namespace: "seelex"}
	first := Key{ProjectID: "project-a", SessionID: "session-1"}
	second := Key{ProjectID: "project-a", SessionID: "session-2"}
	if projectKey := repository.projectKey(first.ProjectID); !containsHashTag(repository.shardKey(first, "generation-a", 0), projectKey) || !containsHashTag(repository.shardKey(second, "generation-b", 1), projectKey) {
		t.Fatalf("Redis shard keys do not share project hash tag: %q / %q", repository.shardKey(first, "generation-a", 0), repository.shardKey(second, "generation-b", 1))
	}
}

func testMessages(count int, marker string) []types.Message {
	messages := make([]types.Message, count)
	for index := range messages {
		content := marker + "-message"
		messages[index] = types.Message{Role: "user", Content: &content}
	}
	return messages
}

func containsHashTag(key, tag string) bool {
	return len(tag) > 0 && len(key) > len(tag) && key[:len(tag)] == tag
}
