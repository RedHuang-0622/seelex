package session

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestStorePortAdapterRoundTrip 验证 session.StorePort 适配器把
// sessionstore.SessionGranularStore 暴露为端口契约（9.3.1 装配线）。
func TestStorePortAdapterRoundTrip(t *testing.T) {
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	router.SetWorkspace("project-adapter")

	store := NewStorePort(sessionstore.NewSessionGranularStore(router))
	record := SessionRecord{
		ID:     "sess-adapter",
		Kind:   KindSubagent,
		Title:  "适配器",
		Status: StatusIdle,
		Binding: SessionBinding{
			WorkspaceID: "project-adapter",
			Kind:        KindSubagent,
		},
	}
	if err := store.SaveSession(record); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, ok, err := store.LoadSession("sess-adapter")
	if err != nil || !ok {
		t.Fatalf("LoadSession ok=%v err=%v", ok, err)
	}
	if loaded.Kind != KindSubagent || loaded.Title != "适配器" {
		t.Fatalf("loaded = %+v", loaded)
	}

	history := store.History("sess-adapter")
	if err := history.Save(context.Background(), []types.Message{
		{Role: "user", Content: stringPtr("hi")},
	}); err != nil {
		t.Fatalf("history save: %v", err)
	}
	loadedHistory, err := history.Load(context.Background())
	if err != nil || len(loadedHistory) != 1 {
		t.Fatalf("history load len=%d err=%v", len(loadedHistory), err)
	}

	if err := store.Bind("sess-adapter", SessionBinding{
		WorkspaceID: "project-adapter",
		ParentID:    "sess-parent",
		Kind:        KindSubagent,
	}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	bound, _, err := store.LoadSession("sess-adapter")
	if err != nil || bound.ParentID != "sess-parent" || bound.Binding.ParentID != "sess-parent" {
		t.Fatalf("bound = %+v err=%v", bound, err)
	}
}

func stringPtr(value string) *string { return &value }
