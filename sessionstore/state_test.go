package sessionstore

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

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
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Fatalf("state = %s, want %s", got, want)
			}
			history := testMessages(messageShardSize+5, string(config.Backend))
			if err := repository.WriteAtomic(context.Background(), key, history); err != nil {
				t.Fatal(err)
			}
			window, total, err := repository.ReadRange(context.Background(), key, messageShardSize-2, 4)
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

func TestSQLitePersistsHistoryAsShards(t *testing.T) {
	repository, err := Open(context.Background(), Config{Backend: BackendSQLite, Path: filepath.Join(t.TempDir(), "sessions.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	key := Key{ProjectID: "project", SessionID: "session"}
	if err := repository.WriteAtomic(context.Background(), key, testMessages(messageShardSize+1, "sqlite")); err != nil {
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
