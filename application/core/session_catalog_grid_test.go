package core

import (
	"context"
	"testing"
	"time"
)

// TestCatalogRefreshScopedByProjectKeepsOtherProjectGrids 钉住 G6 目录按
// projectID 分格：只刷新项目 A 的目录请求不得顺带重算/覆盖项目 B 的格子，
// 项目 B 新增的会话必须等 B 自己的刷新轮次才进入联合快照镜像。
func TestCatalogRefreshScopedByProjectKeepsOtherProjectGrids(t *testing.T) {
	updatedAt := time.Unix(2, 0)
	sessions := &scopedSessions{
		catalog:   map[string][]SessionInfo{"": {{ID: "session-a", UpdatedAt: updatedAt}}},
		histories: map[string]map[string][]EngineMessage{"": {"session-a": {{Role: "user", Content: "a"}}}},
	}
	service := newCatalogTestService(t, sessions)
	defer service.Shutdown()
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 1 })

	if _, err := service.Deps.Workspace.Create("proj", "C:\\proj", ""); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	// 两个项目各自新增：默认项目的范围刷新只更新默认项目格，项目 B 的格子
	// 不被顺带重算/覆盖。
	addCatalogSession(sessions, "", "session-a2", updatedAt)
	addCatalogSession(sessions, "project-1", "session-b", updatedAt)

	done := service.components.sessions.RequestCatalogRefreshProject("")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("project-scoped catalog refresh did not converge")
	}
	snapshot := service.Snapshot()
	ids := sessionIDsOf(snapshot.Sessions)
	if containsSessionID(ids, "session-b") {
		t.Fatalf("scoped refresh of default project must not touch project-1 grid; got %v", ids)
	}
	if !containsSessionID(ids, "session-a2") {
		t.Fatalf("default-project rows must update in their own scoped round; got %v", ids)
	}

	done = service.components.sessions.RequestCatalogRefreshProject("project-1")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("project-1 catalog refresh did not converge")
	}
	snapshot = service.Snapshot()
	ids = sessionIDsOf(snapshot.Sessions)
	if !containsSessionID(ids, "session-b") || !containsSessionID(ids, "session-a2") {
		t.Fatalf("catalog after both scoped rounds must merge both project grids; got %v", ids)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.WaitCatalogRefresh(ctx); err != nil {
		t.Fatalf("WaitCatalogRefresh: %v", err)
	}
	snapshot = service.Snapshot()
	ids = sessionIDsOf(snapshot.Sessions)
	if !containsSessionID(ids, "session-b") || !containsSessionID(ids, "session-a2") {
		t.Fatalf("full catalog after scoped rounds must keep both project grids; got %v", ids)
	}
}

// TestCatalogFullRefreshReplacesProjectGrid 钉住全量刷新按项目整格替换：
// 项目内会话删除后，一次全量刷新不再出现在任何缓存与快照中（旧行不会被
// 保留在别的项目格里冒充幽灵）。
func TestCatalogFullRefreshReplacesProjectGrid(t *testing.T) {
	updatedAt := time.Unix(2, 0)
	sessions := &scopedSessions{
		catalog: map[string][]SessionInfo{
			"": {{ID: "session-a", UpdatedAt: updatedAt}},
		},
		histories: map[string]map[string][]EngineMessage{
			"": {"session-a": {{Role: "user", Content: "a"}}},
		},
	}
	service := newCatalogTestService(t, sessions)
	defer service.Shutdown()
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 1 })

	addCatalogSession(sessions, "", "session-removed", updatedAt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.WaitCatalogRefresh(ctx); err != nil {
		t.Fatalf("WaitCatalogRefresh: %v", err)
	}
	if got := len(service.Snapshot().Sessions); got != 2 {
		t.Fatalf("catalog after add = %d sessions, want 2", got)
	}

	if err := sessions.Delete("session-removed"); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if err := service.WaitCatalogRefresh(ctx); err != nil {
		t.Fatalf("WaitCatalogRefresh after delete: %v", err)
	}
	ids := sessionIDsOf(service.Snapshot().Sessions)
	if containsSessionID(ids, "session-removed") || !containsSessionID(ids, "session-a") {
		t.Fatalf("full refresh must replace grid rows after deletion; got %v", ids)
	}
	cached, _ := service.components.sessions.CatalogCache()
	if containsSessionID(sessionIDsOf(cached), "session-removed") {
		t.Fatalf("catalog cache must drop deleted row after full refresh; got %v", sessionIDsOf(cached))
	}
}

func sessionIDsOf(sessions []SessionInfo) []string {
	ids := make([]string, 0, len(sessions))
	for _, item := range sessions {
		ids = append(ids, item.ID)
	}
	return ids
}

func containsSessionID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}
