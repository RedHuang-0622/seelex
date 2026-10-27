package context

import (
	"os"
	"path/filepath"
	"testing"

	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
)

func TestContextStateRoundTrip(t *testing.T) {
	layout := storelayout.New(t.TempDir())
	scope := storelayout.Scope{ProjectID: "p", SessionID: "s"}
	store := New(layout)
	payload := []byte(`{"schema_version":3}`)
	if err := store.WriteState(scope, payload); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadState(scope)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("state=%s err=%v", got, err)
	}
	if filepath.Base(store.StatePath(scope)) != "context.json" {
		t.Fatalf("state path=%s", store.StatePath(scope))
	}
	if _, err := os.Stat(store.StatePath(scope)); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointStorageKey(t *testing.T) {
	if CheckpointStorageKey("") != "checkpoint" {
		t.Fatal("empty key changed")
	}
	if CheckpointStorageKey("same") != CheckpointStorageKey("same") {
		t.Fatal("checkpoint key not deterministic")
	}
	if CheckpointStorageKey("a") == CheckpointStorageKey("b") {
		t.Fatal("different checkpoint IDs collided")
	}
}
