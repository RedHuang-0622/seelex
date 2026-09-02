package sessionstore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestDurableHistoryConcurrentSessionsStaySeparate（阶段 D6b，-race）：多会话
// 并发 Save/Load provider 历史，同时重装解析器并漂移 Router 活跃写作用域。
// 断言每个会话读回的是自己的内容（不串写他域）且读取不受视图影响。
func TestDurableHistoryConcurrentSessionsStaySeparate(t *testing.T) {
	router := newSessionGranularRouter(t, BackendJSON)
	store := NewSessionGranularStore(router)
	const project = "project-h"
	resolver := func(string) string { return project }
	store.SetWorkspaceResolver(resolver)

	var churn sync.WaitGroup
	churn.Add(1)
	go func() {
		defer churn.Done()
		for round := 0; round < 400; round++ {
			// 等价重装：覆盖 resolverMu 的读写竞争；活跃作用域漂移不得影响读取键。
			store.SetWorkspaceResolver(resolver)
			router.SetWorkspace("project-noise")
		}
	}()

	const sessions = 5
	const rounds = 12
	var group sync.WaitGroup
	for index := 0; index < sessions; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			ctx := context.Background()
			history := store.HistoryForProject(project, fmt.Sprintf("sess-h-%d", index))
			for round := 0; round < rounds; round++ {
				marker := fmt.Sprintf("s%d-r%d", index, round)
				if err := history.Save(ctx, messages(2, marker)); err != nil {
					t.Errorf("save sess-h-%d/%d: %v", index, round, err)
					return
				}
				loaded, err := history.Load(ctx)
				if err != nil {
					t.Errorf("load sess-h-%d/%d: %v", index, round, err)
					return
				}
				if len(loaded) != 2 {
					t.Errorf("sess-h-%d round %d loaded %d messages, want 2", index, round, len(loaded))
					return
				}
				if got := *loaded[0].Content; got != marker+"-0" {
					t.Errorf("sess-h-%d round %d first content = %q, want %q（串写到别的会话）", index, round, got, marker+"-0")
					return
				}
			}
			if err := history.Save(ctx, messages(3, fmt.Sprintf("s%d-final", index))); err != nil {
				t.Errorf("final save sess-h-%d: %v", index, err)
				return
			}
			loaded, err := history.LoadEventTail(ctx, 1<<20, 1<<20)
			if err != nil {
				t.Errorf("event tail sess-h-%d: %v", index, err)
				return
			}
			_ = loaded
		}(index)
	}
	group.Wait()
	churn.Wait()

	for index := 0; index < sessions; index++ {
		sessionID := fmt.Sprintf("sess-h-%d", index)
		router.SetWorkspace("project-elsewhere")
		storedHistory, err := store.HistoryForProject(project, sessionID).Load(context.Background())
		if err != nil || len(storedHistory) != 3 || *storedHistory[0].Content != fmt.Sprintf("s%d-final-0", index) {
			t.Fatalf("%s after scope churn = %+v err=%v, want its own 3 messages", sessionID, storedHistory, err)
		}
	}
}

// TestRouterConcurrentProjectIsolation（阶段 D6b，-race）：两个项目并发提交与
// 枚举，断言目录互不见面（列表不污染）且删除只影响自己项目。
func TestRouterConcurrentProjectIsolation(t *testing.T) {
	router := newSessionGranularRouter(t, BackendJSON)
	store := NewSessionGranularStore(router)
	const rounds = 10
	var group sync.WaitGroup
	for _, project := range []string{"project-A", "project-B"} {
		for index := 0; index < 3; index++ {
			group.Add(1)
			go func(project string, index int) {
				defer group.Done()
				sessionID := fmt.Sprintf("%s-sess-%d", project, index)
				for round := 0; round < rounds; round++ {
					commit := Commit{ProviderHistory: messages(round+1, fmt.Sprintf("%s-%d", project, round))}
					if err := store.SaveCommit(project, sessionID, commit); err != nil {
						t.Errorf("save %s/%s: %v", project, sessionID, err)
						return
					}
					history, _, err := store.HistoryRange(project, sessionID, 0, rounds+5)
					if err != nil || len(history) != round+1 {
						t.Errorf("%s/%s round %d history = %d err=%v, want %d", project, sessionID, round, len(history), err, round+1)
						return
					}
					listed, err := store.SessionsOf(project)
					if err != nil {
						t.Errorf("SessionsOf %s: %v", project, err)
						return
					}
					for _, info := range listed {
						if len(info.ID) < len(project) || info.ID[:len(project)] != project {
							t.Errorf("project %s listed foreign session %q", project, info.ID)
							return
						}
					}
				}
			}(project, index)
		}
	}
	group.Wait()

	if err := store.Delete("project-A", "project-A-sess-0"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	listed, err := store.SessionsOf("project-A")
	if err != nil {
		t.Fatalf("SessionsOf project-A after delete: %v", err)
	}
	for _, info := range listed {
		if info.ID == "project-A-sess-0" {
			t.Fatal("deleted session still listed in its own project")
		}
	}
	foreign, err := store.SessionsOf("project-B")
	if err != nil {
		t.Fatalf("SessionsOf project-B: %v", err)
	}
	if len(foreign) != 3 {
		t.Fatalf("project-B sessions = %d, want 3（删除串到了别的项目）", len(foreign))
	}
}

// TestProjectRecordConcurrentReadDoesNotTear（阶段 D6b，-race）：单写多读项目语义
// 记录，读者每次拿到的必须是完整且属于该项目的记录（跨项目串读或半写都会显形）。
func TestProjectRecordConcurrentReadDoesNotTear(t *testing.T) {
	router := newSessionGranularRouter(t, BackendJSON)
	const projects = 3
	const rounds = 25
	var group sync.WaitGroup
	for index := 0; index < projects; index++ {
		projectID := fmt.Sprintf("project-%d", index)
		group.Add(2)
		go func(projectID string) {
			defer group.Done()
			for round := 0; round < rounds; round++ {
				record := ProjectRecord{
					Version:      fmt.Sprintf("%s-v%d", projectID, round),
					Modules:      []ModuleSemantics{{Name: "mod", Summary: fmt.Sprintf("round %d", round), Path: "mod/"}},
					SourceHashes: []string{fmt.Sprintf("hash-%d", round)},
				}
				if err := router.SaveProjectRecord(projectID, record); err != nil {
					t.Errorf("save %s: %v", projectID, err)
					return
				}
			}
		}(projectID)
		go func(projectID string) {
			defer group.Done()
			for round := 0; round < rounds*2; round++ {
				record, err := router.LoadProjectRecord(projectID)
				if err != nil {
					if isProjectRecordNotFound(err) {
						continue
					}
					t.Errorf("load %s: %v", projectID, err)
					return
				}
				if len(record.Version) < len(projectID) || record.Version[:len(projectID)] != projectID {
					t.Errorf("%s read foreign record version %q", projectID, record.Version)
					return
				}
				if len(record.Modules) != 1 || record.Modules[0].Name != "mod" {
					t.Errorf("%s read torn record: %+v", projectID, record.Modules)
					return
				}
			}
		}(projectID)
	}
	group.Wait()
}

// TestForkCopiesDoNotShareParentGeneration（阶段 D6b，-race）：父会话持续提交
// tool-results，子会话并发读取并复制。父侧不变量是一次 commit 的一对 ref 成对
// 可见；子侧不变量是复制集闭包于父 ref 且不丢已复制条目（fork 物理复制的底座）。
func TestForkCopiesDoNotShareParentGeneration(t *testing.T) {
	router := newSessionGranularRouter(t, BackendJSON)
	store := NewSessionGranularStore(router)
	const project = "project-fork"
	parent, child := "sess-parent", "sess-child"
	var group sync.WaitGroup

	group.Add(1)
	go func() {
		defer group.Done()
		for round := 0; round < 20; round++ {
			results := []ToolResult{
				{Ref: fmt.Sprintf("result-parent-%d-a", round), Tool: "bash", Content: "a", Digest: "da", Size: 1, TokenCount: 1},
				{Ref: fmt.Sprintf("result-parent-%d-b", round), Tool: "bash", Content: "b", Digest: "db", Size: 1, TokenCount: 1},
			}
			if err := store.SaveCommit(project, parent, Commit{
				ProviderHistory: messages(round+1, "parent"), ToolResults: results,
			}); err != nil {
				t.Errorf("parent commit %d: %v", round, err)
				return
			}
		}
	}()
	group.Add(1)
	go func() {
		defer group.Done()
		for round := 0; round < 20; round++ {
			listed, err := store.ListToolResults(project, parent)
			if err != nil {
				t.Errorf("list parent results: %v", err)
				return
			}
			if len(listed) == 0 {
				continue
			}
			// tool-results 跨代累加，条数会变；不变量是一次 commit 的一对 ref
			// 必须成对可见 —— 出现半对说明读到了撕裂的提交。
			seen := make(map[string]bool, len(listed))
			for _, result := range listed {
				seen[result.Ref] = true
			}
			for _, result := range listed {
				partner := strings.TrimSuffix(result.Ref, "-a") + "-b"
				if strings.HasSuffix(result.Ref, "-b") {
					partner = strings.TrimSuffix(result.Ref, "-b") + "-a"
				}
				if !seen[partner] {
					t.Errorf("parent refs torn: %q visible without %q", result.Ref, partner)
					return
				}
			}
			if err := store.SaveCommit(project, child, Commit{
				ProviderHistory: messages(1, "child"), ToolResults: listed,
			}); err != nil {
				t.Errorf("child commit %d: %v", round, err)
				return
			}
			copied, err := store.ListToolResults(project, child)
			if err != nil {
				t.Errorf("list child results: %v", err)
				return
			}
			if len(copied) < len(listed) {
				t.Errorf("child copy lost parent refs: %d < %d", len(copied), len(listed))
				return
			}
			for _, result := range copied {
				if !strings.HasPrefix(result.Ref, "result-parent-") {
					t.Errorf("child result %q is not a copied parent ref", result.Ref)
					return
				}
			}
		}
	}()
	group.Wait()
}
