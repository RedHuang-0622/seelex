package sessionstore

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"
)

func TestListToolResultsAndCurrentGenerationAcrossBackends(t *testing.T) {
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
			first := ToolResult{Ref: "result:1", Tool: "bash", Content: "one", Digest: "d1", Size: 3, TokenCount: 1, CreatedAt: time.Unix(1, 0)}
			second := ToolResult{Ref: CompressedTurnRefPrefix + "seg-1", Tool: "compact_frame", Content: "archived", Digest: "d2", Size: 8, TokenCount: 2, CreatedAt: time.Unix(2, 0)}
			if err := repository.WriteCommit(context.Background(), key, Commit{
				ProviderHistory: messages(1, "seed"),
				Events:          []Event{{Seq: 1, Role: "user", Content: "hi"}},
				ToolResults:     []ToolResult{first},
			}); err != nil {
				t.Fatal(err)
			}
			if err := repository.WriteCommit(context.Background(), key, Commit{ToolResults: []ToolResult{second}}); err != nil {
				t.Fatal(err)
			}
			results, err := repository.ListToolResults(context.Background(), key)
			if err != nil {
				t.Fatal(err)
			}
			byRef := make(map[string]ToolResult, len(results))
			for _, result := range results {
				byRef[result.Ref] = result
			}
			if len(byRef) != 2 || byRef["result:1"].Content != "one" || byRef[CompressedTurnRefPrefix+"seg-1"].Content != "archived" {
				t.Fatalf("listed results = %#v", byRef)
			}
			generation, err := repository.CurrentGeneration(context.Background(), key)
			if err != nil || generation == "" {
				t.Fatalf("current generation = %q err=%v", generation, err)
			}
			if _, err := repository.CurrentGeneration(context.Background(), Key{ProjectID: "project", SessionID: "missing"}); !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("missing generation error = %v, want not found", err)
			}
		})
	}
}

// TestForkCommitDeepCopiesToolResultsAfterParentDelete 验证 fork 落盘数据面
// 不变量：子会话提交携带父通道全量 tool-results（含压缩原文）物理复制；
// 删父后子会话的 state/events/tool-results 全部可读（删父安全前提）。
func TestForkCommitDeepCopiesToolResultsAfterParentDelete(t *testing.T) {
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
			ctx := context.Background()
			parent := Key{ProjectID: "project", SessionID: "parent"}
			child := Key{ProjectID: "project", SessionID: "child"}
			parentState := []byte(`{"version":3,"id":"parent","conversation":{"messages":[]}}`)
			if err := repository.WriteCommit(ctx, parent, Commit{
				ProviderHistory: messages(2, "parent"),
				Events: []Event{
					{Seq: 1, TaskID: "chat-1", Role: "user", Content: "hello", MessageID: "m1"},
					{Seq: 2, TaskID: "chat-1", Role: "assistant", Content: "world", MessageID: "m2"},
				},
				State: parentState,
				ToolResults: []ToolResult{
					{Ref: "result:1", Tool: "bash", Content: "out", Digest: "d", Size: 3, TokenCount: 1},
					{Ref: CompressedTurnRefPrefix + "seg-1", Tool: "compact_frame", Content: "archived turns", Digest: "c", Size: 14, TokenCount: 2},
				},
			}); err != nil {
				t.Fatal(err)
			}
			// 子会话 = 截断事件 + 子身份 state + 父通道全量 tool-results。
			results, err := repository.ListToolResults(ctx, parent)
			if err != nil {
				t.Fatal(err)
			}
			childEvents := []Event{{Seq: 1, TaskID: "chat-1", Role: "user", Content: "hello", MessageID: "m1"}}
			childState := []byte(`{"version":3,"id":"child","conversation":{"messages":[]}}`)
			if err := repository.WriteCommit(ctx, child, Commit{
				ProviderHistory: nil,
				Events:          childEvents,
				State:           childState,
				ToolResults:     results,
			}); err != nil {
				t.Fatal(err)
			}
			// 删父安全：删除 parent 后 child 的 state/events/tool-results 可读。
			if err := repository.Delete(ctx, parent); err != nil {
				t.Fatal(err)
			}
			state, err := repository.ReadState(ctx, child)
			if err != nil || string(state) != string(childState) {
				t.Fatalf("child state = %s err=%v", state, err)
			}
			events, err := repository.ReadEventRange(ctx, child, 1, 2)
			if err != nil || len(events) != 1 || events[0].Seq != 1 {
				t.Fatalf("child events = %#v err=%v", events, err)
			}
			result, err := repository.ReadToolResult(ctx, child, "result:1")
			if err != nil || result.Content != "out" {
				t.Fatalf("child result = %#v err=%v", result, err)
			}
			archived, err := repository.ReadToolResult(ctx, child, CompressedTurnRefPrefix+"seg-1")
			if err != nil || archived.Content != "archived turns" {
				t.Fatalf("child archived turns = %#v err=%v", archived, err)
			}
		})
	}
}
