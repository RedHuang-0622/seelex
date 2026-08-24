package sessionstore

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestNodeSessionStoreRoundTrip 验证子代理会话记录按
// `<mainSessionID>-<subSessionID>.json` 落盘并可读回。
func TestNodeSessionStoreRoundTrip(t *testing.T) {
	router := newTestRouter(t)
	store := NewNodeSessionStore(router)
	mainID := "sess_main_test"
	subID := "node-deadbeef"

	record := NodeSessionRecord{
		SchemaVersion: NodeSessionSchemaVersion,
		NodeID:        "fork-a",
		SessionID:     subID,
		MainSessionID: mainID,
		Goal:          "inspect scope",
		Status:        "done",
		Summary:       "ok",
		History:       messages(2, "sub"),
		ContextJSON:   []byte(`{"goal":"inspect scope"}`),
		StagesJSON:    []byte(`[{"stage":"running"}]`),
		ResultJSON:    []byte(`{"status":"completed"}`),
		StartedAt:     time.Now().UTC().Add(-time.Minute),
		EndedAt:       time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := store.Save("project-a", mainID, record); err != nil {
		t.Fatalf("save node session: %v", err)
	}

	// 文件路径符合用户约定：主会话目录 subagents/ 下 <main>-<sub>.json。
	mainDir := filepath.Join(routerConfigPath(t, router), "project-"+hash("project-a"), "session-"+hash(mainID))
	filePath := filepath.Join(mainDir, nodeSessionDirName, nodeSessionFileName(mainID, subID))
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected node session file at %s: %v", filePath, err)
	}
	if filepath.Base(filePath) != mainID+"-"+subID+".json" {
		t.Fatalf("file name = %s, want %s-%s.json", filepath.Base(filePath), mainID, subID)
	}

	loaded, err := store.Load("project-a", mainID, subID)
	if err != nil {
		t.Fatalf("load node session: %v", err)
	}
	if loaded.NodeID != record.NodeID || loaded.Status != "done" || len(loaded.History) != 2 {
		t.Fatalf("loaded = %+v", loaded)
	}
	if string(loaded.ContextJSON) != `{"goal":"inspect scope"}` {
		t.Fatalf("context json = %s", loaded.ContextJSON)
	}
}

// TestNodeSessionStoreListFromMainIndex 验证从主会话索引直接列举全部子会话。
func TestNodeSessionStoreListFromMainIndex(t *testing.T) {
	router := newTestRouter(t)
	store := NewNodeSessionStore(router)
	mainID := "sess_main_list"
	for _, sub := range []string{"node-aaaa", "node-bbbb", "node-cccc"} {
		if err := store.Save("project-a", mainID, NodeSessionRecord{
			SchemaVersion: NodeSessionSchemaVersion, NodeID: sub, SessionID: sub,
			MainSessionID: mainID, Status: "done", UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("save %s: %v", sub, err)
		}
	}
	records, err := store.List("project-a", mainID)
	if err != nil {
		t.Fatalf("list node sessions: %v", err)
	}
	got := make([]string, 0, len(records))
	for _, record := range records {
		got = append(got, record.SessionID)
	}
	want := []string{"node-aaaa", "node-bbbb", "node-cccc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list = %v, want %v", got, want)
	}
	// 其它主会话索引下为空（互不串写）。
	other, err := store.List("project-a", "sess_other")
	if err != nil || len(other) != 0 {
		t.Fatalf("other main index = %v err=%v, want empty", other, err)
	}
}

// TestNodeSessionStoreMissingAndDelete 验证缺失语义与删除。
func TestNodeSessionStoreMissingAndDelete(t *testing.T) {
	router := newTestRouter(t)
	store := NewNodeSessionStore(router)
	mainID := "sess_main_del"
	if _, err := store.Load("project-a", mainID, "node-missing"); !errors.Is(err, ErrNodeSessionNotFound) {
		t.Fatalf("load missing = %v, want ErrNodeSessionNotFound", err)
	}
	if err := store.Save("project-a", mainID, NodeSessionRecord{
		SchemaVersion: NodeSessionSchemaVersion, NodeID: "n", SessionID: "node-1",
		MainSessionID: mainID, Status: "failed", UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("project-a", mainID, "node-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Load("project-a", mainID, "node-1"); !errors.Is(err, ErrNodeSessionNotFound) {
		t.Fatalf("load after delete = %v, want ErrNodeSessionNotFound", err)
	}
}

// TestNodeSessionStoreProjectIsolation 验证跨项目隔离。
func TestNodeSessionStoreProjectIsolation(t *testing.T) {
	router := newTestRouter(t)
	store := NewNodeSessionStore(router)
	record := NodeSessionRecord{
		SchemaVersion: NodeSessionSchemaVersion, NodeID: "n", SessionID: "node-x",
		MainSessionID: "sess_main", Status: "running", UpdatedAt: time.Now().UTC(),
	}
	if err := store.Save("project-a", "sess_main", record); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("project-b", "sess_main", "node-x"); !errors.Is(err, ErrNodeSessionNotFound) {
		t.Fatalf("cross-project load = %v, want ErrNodeSessionNotFound", err)
	}
}

// routerConfigPath 返回测试 Router 的 JSON 存储根目录（sessions-json）。
func routerConfigPath(t *testing.T, router *Router) string {
	t.Helper()
	repository, ok := router.repository.(*jsonRepository)
	if !ok {
		t.Fatal("test router must use JSON backend")
	}
	return repository.root
}
