package layout

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPathsGuideHeadAndLocks(t *testing.T) {
	layout := New(t.TempDir())
	scope := Scope{ProjectID: "project", SessionID: "session"}
	if !strings.HasSuffix(layout.SessionRoot(scope), filepath.Join("project-"+hash16("project"), "session-"+hash16("session"))) {
		t.Fatalf("unexpected session root: %s", layout.SessionRoot(scope))
	}
	if _, err := layout.EnsureGuide(scope); err != nil {
		t.Fatal(err)
	}
	guideData, err := os.ReadFile(layout.GuidePath(scope))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"layout_version", "schema_version", "session_id", "created_at", "updated_at"} {
		if !strings.Contains(string(guideData), `"`+key+`"`) {
			t.Fatalf("guide missing %s: %s", key, guideData)
		}
	}
	if _, err := layout.Publish(scope, "message", "commit-1", map[string]any{"last_seq": 7}, time.Unix(1, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	head, err := layout.ReadHead(scope, "message")
	if err != nil {
		t.Fatal(err)
	}
	if head.CommitID != "commit-1" || !strings.Contains(string(head.Payload), `"last_seq": 7`) {
		t.Fatalf("head=%+v", head)
	}
	if layout.Lock(scope, "message") != layout.Lock(scope, "message") {
		t.Fatal("same module lock identity changed")
	}
	if layout.Lock(scope, "message") == layout.Lock(scope, "event") {
		t.Fatal("different module IDs aliased")
	}
}

func hash16(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
