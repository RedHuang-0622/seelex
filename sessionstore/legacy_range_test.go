package sessionstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

func TestJSONLegacyReadRangeUsesBoundedTailAfterCountProbe(t *testing.T) {
	root := t.TempDir()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()

	key := Key{ProjectID: "project", SessionID: "legacy-session"}
	router.SetWorkspace(key.ProjectID)
	messages := make([]types.Message, 250)
	for index := range messages {
		content := "message-" + string(rune('a'+index%26))
		messages[index] = types.Message{Role: "user", Content: &content}
	}
	if err := router.Save(key.SessionID, messages); err != nil {
		t.Fatal(err)
	}
	repository, ok := router.repository.(*jsonRepository)
	if !ok {
		t.Fatalf("repository type = %T, want JSON repository", router.repository)
	}
	directory := repository.sessionDir(key)
	manifestPath := filepath.Join(directory, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest jsonManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.HistoryShardCounts = nil
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	_, total, err := router.LoadRange(key.SessionID, 0, 0)
	if err != nil || total != len(messages) {
		t.Fatalf("legacy total=%d err=%v, want %d", total, err, len(messages))
	}
	tail, total, err := router.LoadRange(key.SessionID, len(messages)-10, 10)
	if err != nil || total != len(messages) || len(tail) != 10 {
		t.Fatalf("legacy tail len=%d total=%d err=%v", len(tail), total, err)
	}
	if tail[0].Content == nil || *tail[0].Content != *messages[len(messages)-10].Content {
		t.Fatalf("legacy tail first=%#v, want %#v", tail[0], messages[len(messages)-10])
	}
	jsonRepo := repository
	jsonRepo.legacyMu.Lock()
	cached := len(jsonRepo.legacyCounts)
	jsonRepo.legacyMu.Unlock()
	if cached != 1 {
		t.Fatalf("legacy count cache entries=%d, want one generation entry", cached)
	}

}
