package layout

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestReadWithHealRunsDomainRepairAfterBoundedRetry(t *testing.T) {
	layout := New(t.TempDir())
	scope := Scope{ProjectID: "project", SessionID: "session"}
	if _, err := layout.Publish(scope, "message", "c1", map[string]int{"last_seq": 1}, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	path := layout.ModulePath(scope, "message")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), `"checksum": "`, `"checksum": "bad`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	repaired := 0
	head, err := layout.ReadWithHeal(scope, "message", true, func() *sync.Mutex { return layout.Lock(scope, "message") }, nil, func(cause error) error {
		if cause == nil {
			t.Fatal("repair received nil cause")
		}
		repaired++
		_, publishErr := layout.Publish(scope, "message", "c1", map[string]int{"last_seq": 1}, time.Unix(2, 0))
		return publishErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 1 || head.CommitID != "c1" {
		t.Fatalf("repaired=%d head=%+v", repaired, head)
	}
}

func hash16(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
