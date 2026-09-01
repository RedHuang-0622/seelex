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
