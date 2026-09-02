package sessionstore

import (
	"context"
	"fmt"
	"sync"
	"testing"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
)

// TestSessionGranularStoreConcurrentProjectScope 发散式并发（-race）：多 goroutine
// 并发 SaveSession/LoadSession/SaveCommit/SaveHistory/ResolveProjectForSession/
// SessionsOf/Delete，同时翻转 workspaceResolver 与 Router 活跃写作用域。
//
// 关注两件事：注入点与解析读取不得裸字段竞争；已绑定会话的归属不得随活跃写
// 作用域漂移（漂移会让 record/事件拆到两个项目，manifest 错键、会话打不开）。
func TestSessionGranularStoreConcurrentProjectScope(t *testing.T) {
	for _, backend := range []Backend{BackendJSON, BackendSQLite} {
		t.Run(string(backend), func(t *testing.T) {
			router := newSessionGranularRouter(t, backend)
			store := NewSessionGranularStore(router)
			const sessionCount = 6
			const rounds = 30

			var group sync.WaitGroup
			group.Add(1)
			go func() {
				defer group.Done()
				for round := 0; round < rounds*4; round++ {
					store.SetWorkspaceResolver(func(sessionID string) string {
						if sessionID == "sess-bound" {
							return "project-A"
						}
						return ""
					})
					if round%2 == 0 {
						router.SetWorkspace("project-A")
					} else {
						router.SetWorkspace("project-B")
					}
				}
			}()

			for index := 0; index < sessionCount; index++ {
				group.Add(1)
				go func(index int) {
					defer group.Done()
					sessionID := fmt.Sprintf("sess-%d", index)
					if index == 0 {
						sessionID = "sess-bound"
					}
					for round := 0; round < rounds; round++ {
						record := Record{ID: sessionID, Status: StatusIdle, Binding: Binding{WorkspaceID: "project-A"}}
						if err := store.SaveSession("project-A", record); err != nil {
							t.Errorf("save %s: %v", sessionID, err)
							return
						}
						if err := store.SaveCommit("project-A", sessionID, Commit{ProviderHistory: messages(round+1, "turn")}); err != nil {
							t.Errorf("commit %s: %v", sessionID, err)
							return
						}
						if err := store.SaveHistory("project-A", sessionID, messages(round+1, "history")); err != nil {
							t.Errorf("history %s: %v", sessionID, err)
							return
						}
						if _, ok, err := store.LoadSession("project-A", sessionID); err != nil || !ok {
							t.Errorf("load %s: ok=%v err=%v", sessionID, ok, err)
							return
						}
						if _, err := store.SessionsOf("project-A"); err != nil {
							t.Errorf("sessions of project-A: %v", err)
							return
						}
						if got := store.ResolveProjectForSession(sessionID); sessionID == "sess-bound" && got != "project-A" {
							// 绑定是唯一权威来源：活跃写作用域翻转不得改变它。
							t.Errorf("resolve bound session = %q, want project-A", got)
							return
						} else if got == "project-B" {
							// project-B 里从来没有这个会话的数据：未绑定一律默认项目，
							// 解析到它就是按活跃作用域命名乱猜。
							t.Errorf("resolve %s = project-B, which holds no data of it", sessionID)
							return
						}
					}
					if index == sessionCount-1 {
						if err := store.Delete("project-A", sessionID); err != nil {
							t.Errorf("delete %s: %v", sessionID, err)
						}
					}
				}(index)
			}
			group.Wait()

			if got := store.ResolveProjectForSession("sess-bound"); got != "project-A" {
				t.Fatalf("final resolve of bound session = %q, want project-A", got)
			}
			record, ok, err := store.LoadSession("project-A", "sess-bound")
			if err != nil || !ok || record.Binding.WorkspaceID != "project-A" {
				t.Fatalf("bound record after churn = %+v ok=%v err=%v", record, ok, err)
			}
		})
	}
}

// TestEventStoreConcurrentAppendAndLoad 发散式并发（-race）：多会话并发追加与
// 读取执行事实事件库，同时翻转解析器与活跃写作用域。断言每个会话读回自己的
// 全部事件、Seq 严格递增、且不串到别会话的分片。
func TestEventStoreConcurrentAppendAndLoad(t *testing.T) {
	router := newSessionGranularRouter(t, BackendJSON)
	store := NewEventStore(router)
	const sessionCount = 4
	const perSession = 25

	// 解析器必须在并发前安装：否则最早的 Append 会落到 active scope，事件被
	// 分到两个项目（那是测试夹具的问题，不是存储的行为）。
	resolver := func(sessionID string) string { return "project-events" }
	store.SetWorkspaceResolver(resolver)

	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		for round := 0; round < sessionCount*perSession*2; round++ {
			// 安装等价实现：覆盖解析器字段的读写竞争，同时保持路由稳定。
			store.SetWorkspaceResolver(resolver)
			router.SetWorkspace("project-noise")
		}
	}()

	for index := 0; index < sessionCount; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			sessionID := fmt.Sprintf("sess-%d", index)
			for seq := uint64(1); seq <= perSession; seq++ {
				event := frameworkevent.Event{
					ID: fmt.Sprintf("evt-%d", seq), Sequence: seq, Source: "seelex.test",
					Type: frameworkevent.TypeLifecycle, Status: frameworkevent.StatusRunning,
					Locations: []frameworkevent.Location{{
						Kind: "agent.runtime", IDs: map[string]string{"session_id": sessionID},
					}},
				}
				if err := store.Append(context.Background(), event); err != nil {
					t.Errorf("append %s/%d: %v", sessionID, seq, err)
					return
				}
			}
		}(index)
	}
	group.Wait()

	for index := 0; index < sessionCount; index++ {
		sessionID := fmt.Sprintf("sess-%d", index)
		events, err := store.Load(context.Background(), sessionID)
		if err != nil {
			t.Fatalf("load %s: %v", sessionID, err)
		}
		if len(events) != perSession {
			t.Fatalf("%s events = %d, want %d（分片串写或丢事件）", sessionID, len(events), perSession)
		}
		var last uint64
		for _, event := range events {
			if event.Sequence <= last {
				t.Fatalf("%s seq not monotonic: %d after %d", sessionID, event.Sequence, last)
			}
			last = event.Sequence
			if got := sessionIDFromEvent(event); got != sessionID {
				t.Fatalf("event %d belongs to %q, loaded under %q", event.Sequence, got, sessionID)
			}
		}
	}
}
