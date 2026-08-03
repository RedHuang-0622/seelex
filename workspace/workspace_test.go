package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoPersistsWorkspaceAndBinding(t *testing.T) {
	store := t.TempDir()
	root := t.TempDir()
	repo, err := NewRepoWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	created, err := repo.Create("demo", root, "https://example.invalid/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	repo.BindSession("session-1", created.ID)

	reloaded, err := NewRepoWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RootPath != created.RootPath || got.GitRemote != created.GitRemote {
		t.Fatalf("reloaded workspace = %#v, want %#v", got, created)
	}
	bound, ok := reloaded.SessionWorkspace("session-1")
	if !ok || bound.ID != created.ID {
		t.Fatalf("binding = %#v, %v", bound, ok)
	}
}

func TestRepoCreateDeduplicatesAbsoluteRoot(t *testing.T) {
	repo := NewRepo()
	root := t.TempDir()
	first, err := repo.Create("first", root, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create("second", filepath.Join(root, "."), "")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || len(repo.List()) != 1 {
		t.Fatalf("dedup failed: first=%s second=%s list=%d", first.ID, second.ID, len(repo.List()))
	}
}

func TestRepoRejectsInvalidCreateAndMissingLookup(t *testing.T) {
	repo := NewRepo()
	if _, err := repo.Create("", t.TempDir(), ""); err == nil {
		t.Fatal("expected empty name error")
	}
	if _, err := repo.Create("missing", filepath.Join(t.TempDir(), "missing"), ""); err == nil {
		t.Fatal("expected missing root error")
	}
	if _, err := repo.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get error = %v, want ErrNotFound", err)
	}
	if err := repo.Delete("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete error = %v, want ErrNotFound", err)
	}
}

func TestRepoDeleteRemovesBindingsAndPersists(t *testing.T) {
	store := t.TempDir()
	repo, err := NewRepoWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	created, err := repo.Create("demo", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	repo.BindSession("session-1", created.ID)
	if err := repo.Delete(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := repo.SessionWorkspace("session-1"); ok {
		t.Fatal("binding survived workspace deletion")
	}
	reloaded, err := NewRepoWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 0 || len(reloaded.AllBindings()) != 0 {
		t.Fatalf("reloaded state = %#v %#v", reloaded.List(), reloaded.AllBindings())
	}
}

func TestRepoUpdateGitRemotePersists(t *testing.T) {
	store := t.TempDir()
	repo, err := NewRepoWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	created, err := repo.Create("demo", t.TempDir(), "old")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateGitRemote(created.ID, " new "); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewRepoWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Get(created.ID)
	if err != nil || got.GitRemote != "new" {
		t.Fatalf("remote = %q, err=%v", got.GitRemote, err)
	}
}

func TestRepoSavePublishesValidJSONWithoutTempResidue(t *testing.T) {
	store := t.TempDir()
	repo, err := NewRepoWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create("one", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create("two", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(store, "workspace_index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot repoSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("published index is invalid JSON: %v", err)
	}
	entries, err := os.ReadDir(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary file leaked: %s", entry.Name())
		}
	}
}

func TestRepoLoadRejectsCorruptIndex(t *testing.T) {
	store := t.TempDir()
	if err := os.WriteFile(filepath.Join(store, "workspace_index.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepoWithStore(store); err == nil || !strings.Contains(err.Error(), "unmarshal index") {
		t.Fatalf("error = %v, want corrupt-index error", err)
	}
}

func TestWorkspaceSessionsAreSortedAndUnbindWorks(t *testing.T) {
	repo := NewRepo()
	created, err := repo.Create("demo", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	repo.BindSession("z", created.ID)
	repo.BindSession("a", created.ID)
	if got := repo.WorkspaceSessions(created.ID); len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Fatalf("sessions = %v", got)
	}
	repo.UnbindSession("a")
	if got := repo.WorkspaceSessions(created.ID); len(got) != 1 || got[0] != "z" {
		t.Fatalf("sessions after unbind = %v", got)
	}
}
