package sessionstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
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

// TestForkToolResultsConcurrentDivergenceStayIsolated（D6b 发散式 -race）：
// 多对 parent/child fork 并发做"父通道全量 tool-results 深拷贝 → 子提交 →
// 删父 → 子读回"，同时覆盖 resolver 重装与活跃写作用域漂移。每对的 content
// 只属于自己（发散式隔离，不串写他域）。
func TestForkToolResultsConcurrentDivergenceStayIsolated(t *testing.T) {
	router := newSessionGranularRouter(t, BackendJSON)
	store := NewSessionGranularStore(router)
	const project = "project-fork-race"
	resolver := func(string) string { return project }
	store.SetWorkspaceResolver(resolver)

	var churn sync.WaitGroup
	churn.Add(1)
	go func() {
		defer churn.Done()
		for round := 0; round < 300; round++ {
			store.SetWorkspaceResolver(resolver)
			router.SetWorkspace("project-noise")
		}
	}()

	const pairs = 6
	const rounds = 8
	var group sync.WaitGroup
	for index := 0; index < pairs; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			for round := 0; round < rounds; round++ {
				parent := Key{ProjectID: project, SessionID: fmt.Sprintf("fork-p%d-r%d-parent", index, round)}
				child := Key{ProjectID: project, SessionID: fmt.Sprintf("fork-p%d-r%d-child", index, round)}
				marker := fmt.Sprintf("p%d-r%d", index, round)
				parentState := []byte(fmt.Sprintf(`{"version":3,"id":"%s","forked_from":null}`, parent.SessionID))
				if err := store.SaveCommit(project, parent.SessionID, Commit{
					ProviderHistory: messages(2, marker),
					Events: []Event{
						{Seq: 1, Role: "user", Content: marker + " q", MessageID: "m1"},
					},
					State: parentState,
					ToolResults: []ToolResult{
						{Ref: "result:1", Tool: "bash", Content: marker + "-one", Digest: "d1", Size: 3, TokenCount: 1},
						{Ref: CompressedTurnRefPrefix + "seg-1", Tool: "compact_frame", Content: marker + "-archived", Digest: "d2", Size: 8, TokenCount: 2},
					},
				}); err != nil {
					t.Errorf("parent commit %s: %v", parent.SessionID, err)
					return
				}
				results, err := store.ListToolResults(project, parent.SessionID)
				if err != nil {
					t.Errorf("list parent results %s: %v", parent.SessionID, err)
					return
				}
				childState := []byte(fmt.Sprintf(`{"version":3,"id":"%s","forked_from":"%s"}`, child.SessionID, parent.SessionID))
				if err := store.SaveCommit(project, child.SessionID, Commit{
					Events:      []Event{{Seq: 1, Role: "user", Content: marker + " q", MessageID: "m1"}},
					State:       childState,
					ToolResults: results,
				}); err != nil {
					t.Errorf("child commit %s: %v", child.SessionID, err)
					return
				}
				if err := store.Delete(project, parent.SessionID); err != nil {
					t.Errorf("delete parent %s: %v", parent.SessionID, err)
					return
				}
				result, err := store.ToolResult(project, child.SessionID, "result:1")
				if err != nil || result.Content != marker+"-one" {
					t.Errorf("child result %s = %#v err=%v, want %q", child.SessionID, result, err, marker+"-one")
					return
				}
				archived, err := store.ToolResult(project, child.SessionID, CompressedTurnRefPrefix+"seg-1")
				if err != nil || archived.Content != marker+"-archived" {
					t.Errorf("child archived %s = %#v err=%v", child.SessionID, archived, err)
					return
				}
				state, err := router.LoadStateWorkspace(project, child.SessionID)
				if err != nil || string(state) != string(childState) {
					t.Errorf("child state %s = %s err=%v", child.SessionID, state, err)
					return
				}
				events, err := store.EventRange(project, child.SessionID, 1, 1)
				if err != nil || len(events) != 1 || events[0].Content != marker+" q" {
					t.Errorf("child events %s = %#v err=%v", child.SessionID, events, err)
					return
				}
			}
		}(index)
	}
	group.Wait()
	churn.Wait()
}
