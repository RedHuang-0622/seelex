package session

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ── 装配薄壳测试（存储桥已迁移到 SessionGranularStore，见
// sessionstore/session_granular_test.go）──────────────────────

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.Router() != nil {
		t.Fatal("fresh Manager must have nil router")
	}
	if m.Workspace() != "" {
		t.Fatal("fresh Manager workspace must be empty")
	}
	// 默认未注入回调
	if err := m.SaveCurrent("s1"); err == nil {
		t.Error("expected error when saveFn not injected")
	}
	if err := m.Resume("s1"); err == nil {
		t.Error("expected error when loadFn not injected")
	}
}

// TestSaveCommitWorkspaceRoundTrip 压缩归档写通道的端到端键一致性：经 Manager
// 显式作用域写面落盘的 commit，必须能被读面按同一个 (项目, 会话, ref) 键读回来
// （read_compressed_turn 走的就是 Router.LoadToolResultWorkspace）。此前装配方
// 把 Manager 注给压缩归档器，但 Manager 没有这个写方法 → 类型断言失败 →
// 压缩原文一律写不出去（read_compressed_turn 永远读不到）。
func TestSaveCommitWorkspaceRoundTrip(t *testing.T) {
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })

	manager := NewManager().WithRouter(router)
	// 真实顺序：会话先落盘（空 commit 建索引条目），压缩归档再追加原文。
	if err := router.SaveCommitWorkspace("", "sess-1", sessionstore.Commit{}); err != nil {
		t.Fatalf("init commit: %v", err)
	}
	archived := `[{"role":"user","content":"被压缩的原文"}]`
	commit := sessionstore.Commit{ToolResults: []sessionstore.ToolResult{{
		Ref: "compressed:seg-1", Tool: "compact_frame", Content: archived, Size: len(archived),
	}}}
	if err := manager.SaveCommitWorkspace("", "sess-1", commit); err != nil {
		t.Fatalf("SaveCommitWorkspace: %v", err)
	}
	result, err := router.LoadToolResultWorkspace("", "sess-1", "compressed:seg-1")
	if err != nil {
		t.Fatalf("LoadToolResultWorkspace: %v", err)
	}
	if result.Content != archived {
		t.Fatalf("content = %q, want %q", result.Content, archived)
	}
}

// TestSaveCommitWorkspaceWithoutRouterFailsLoud 未装配 Router 时显式报错：
// 宁可失败，也不静默丢掉压缩原文。
func TestSaveCommitWorkspaceWithoutRouterFailsLoud(t *testing.T) {
	if err := NewManager().SaveCommitWorkspace("", "sess-1", sessionstore.Commit{}); err == nil {
		t.Fatal("SaveCommitWorkspace without a router must fail loudly")
	}
}

func TestWithRouterAndWorkspace(t *testing.T) {
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })

	m := NewManager().WithRouter(router)
	if m.Router() == nil {
		t.Fatal("WithRouter must install router")
	}
	m.SetWorkspace("project-a")
	if got := m.Workspace(); got != "project-a" {
		t.Fatalf("Workspace = %q, want project-a", got)
	}
	if _, err := m.StorageConfig(); err != nil {
		t.Fatalf("StorageConfig: %v", err)
	}
}

func TestInjectSaveLoad(t *testing.T) {
	m := NewManager()
	saveCalled := false
	loadCalled := false
	m.InjectSaveLoad(
		func(id string) error { saveCalled = true; return nil },
		func(id string) error { loadCalled = true; return nil },
	)

	_ = m.SaveCurrent("s1")
	if !saveCalled {
		t.Error("saveFn should have been called")
	}
	_ = m.Resume("s1")
	if !loadCalled {
		t.Error("loadFn should have been called")
	}
}

func TestSaveCurrent_WithoutInjection(t *testing.T) {
	m := NewManager()
	err := m.SaveCurrent("s1")
	if err == nil || err.Error() != "session: saveFn not injected" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResume_WithoutInjection(t *testing.T) {
	m := NewManager()
	err := m.Resume("s1")
	if err == nil || err.Error() != "session: loadFn not injected" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveCurrent_SaveFnError(t *testing.T) {
	m := NewManager()
	m.InjectSaveLoad(
		func(id string) error { return errors.New("disk full") },
		nil,
	)
	err := m.SaveCurrent("s1")
	if err == nil || err.Error() != "disk full" {
		t.Fatalf("expected 'disk full', got %v", err)
	}
}

func TestResume_LoadFnError(t *testing.T) {
	m := NewManager()
	m.InjectSaveLoad(
		nil,
		func(id string) error { return errors.New("not found") },
	)
	err := m.Resume("s1")
	if err == nil || err.Error() != "not found" {
		t.Fatalf("expected 'not found', got %v", err)
	}
}
