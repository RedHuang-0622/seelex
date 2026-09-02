package core

import (
	"context"
	"sync"
	"testing"
	"time"
)

// addCatalogSession 在会话端口的项目索引里追加一个会话（目录刷新会读到它）。
func addCatalogSession(sessions *scopedSessions, projectID, sessionID string, updatedAt time.Time) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.catalog[projectID] = append(sessions.catalog[projectID], SessionInfo{
		ID: sessionID, UpdatedAt: updatedAt,
	})
	if sessions.histories[projectID] == nil {
		sessions.histories[projectID] = map[string][]EngineMessage{}
	}
	sessions.histories[projectID][sessionID] = []EngineMessage{{Role: "user", Content: sessionID}}
}

func newCatalogTestService(t *testing.T, sessions *scopedSessions) *Service {
	t.Helper()
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: &fakeRuntime{},
		Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:  fakeSkills{}, Sessions: sessions, Workspace: newFakeWorkspace(),
	})
	return service
}

// TestWaitCatalogRefreshSettlesFreshCatalog 是 C3 回执的核心契约：一轮
// WaitCatalogRefresh 返回时，目录里新出现的会话必须已随权威快照可见。
// 前端因此不需要"首轮列表为空就把上一次列表写回快照"的伪造状态。
func TestWaitCatalogRefreshSettlesFreshCatalog(t *testing.T) {
	updatedAt := time.Unix(2, 0)
	sessions := &scopedSessions{
		catalog:   map[string][]SessionInfo{"": {{ID: "session-a", UpdatedAt: updatedAt}}},
		histories: map[string]map[string][]EngineMessage{"": {"session-a": {{Role: "user", Content: "a"}}}},
	}
	service := newCatalogTestService(t, sessions)
	defer service.Shutdown()
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 1 })

	addCatalogSession(sessions, "", "session-b", updatedAt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.WaitCatalogRefresh(ctx); err != nil {
		t.Fatalf("WaitCatalogRefresh: %v", err)
	}
	if got := len(service.Snapshot().Sessions); got != 2 {
		t.Fatalf("catalog right after settle = %d sessions, want 2", got)
	}
}

// TestWaitCatalogRefreshServesCoalescedRequests 覆盖唤醒槽位被丢弃时的回执
// 完整性：目录变更与多个并发等待者都必须在收敛后返回，没有一个请求被静默吞掉。
func TestWaitCatalogRefreshServesCoalescedRequests(t *testing.T) {
	updatedAt := time.Unix(2, 0)
	sessions := &scopedSessions{
		catalog:   map[string][]SessionInfo{"": {{ID: "session-a", UpdatedAt: updatedAt}}},
		histories: map[string]map[string][]EngineMessage{"": {"session-a": {{Role: "user", Content: "a"}}}},
	}
	service := newCatalogTestService(t, sessions)
	defer service.Shutdown()
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 1 })

	addCatalogSession(sessions, "", "session-b", updatedAt)
	const waiters = 16
	var group sync.WaitGroup
	errs := make([]error, waiters)
	group.Add(waiters)
	for index := 0; index < waiters; index++ {
		go func(index int) {
			defer group.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errs[index] = service.WaitCatalogRefresh(ctx)
		}(index)
	}
	group.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("waiter %d: WaitCatalogRefresh: %v", index, err)
		}
	}
	if got := len(service.Snapshot().Sessions); got != 2 {
		t.Fatalf("catalog after %d coalesced waits = %d sessions, want 2", waiters, got)
	}
}

// TestWaitCatalogRefreshConvergesAfterShutdown 钉住关闭路径：worker 退出时
// 释放等待者，之后的请求立即收敛，不会把退出路径上的调用挂到超时。
func TestWaitCatalogRefreshConvergesAfterShutdown(t *testing.T) {
	updatedAt := time.Unix(2, 0)
	sessions := &scopedSessions{
		catalog:   map[string][]SessionInfo{"": {{ID: "session-a", UpdatedAt: updatedAt}}},
		histories: map[string]map[string][]EngineMessage{"": {"session-a": {{Role: "user", Content: "a"}}}},
	}
	service := newCatalogTestService(t, sessions)
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 1 })
	service.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	if err := service.WaitCatalogRefresh(ctx); err != nil {
		t.Fatalf("WaitCatalogRefresh after shutdown: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("WaitCatalogRefresh after shutdown waited %s; receipt must converge immediately", elapsed)
	}
}
