package sessionstore

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

func messages(count int, marker string) []types.Message {
	result := make([]types.Message, count)
	for index := range result {
		content := fmt.Sprintf("%s-%d", marker, index)
		result[index] = types.Message{Role: "user", Content: &content}
	}
	return result
}

func TestJSONRepositoryCommitsShardGenerationsAtomically(t *testing.T) {
	repository, err := newJSONRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session"}
	first, second := messages(205, "first"), messages(121, "second")
	if err := repository.WriteAtomic(context.Background(), key, first); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errors := make(chan error, 40)
	for index := 0; index < 40; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			history, readErr := repository.Read(context.Background(), key)
			if readErr != nil {
				errors <- readErr
				return
			}
			if len(history) != len(first) && len(history) != len(second) {
				errors <- fmt.Errorf("mixed history count %d", len(history))
			}
		}()
	}
	if err := repository.WriteAtomic(context.Background(), key, second); err != nil {
		t.Fatal(err)
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	history, err := repository.Read(context.Background(), key)
	if err != nil || len(history) != len(second) || *history[0].Content != "second-0" {
		t.Fatalf("history=%d err=%v", len(history), err)
	}
}

func TestSQLiteRepositoryRoundTrip(t *testing.T) {
	repository, err := Open(context.Background(), Config{Backend: BackendSQLite, Path: filepath.Join(t.TempDir(), "sessions.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	key := Key{ProjectID: "project", SessionID: "session"}
	if err := repository.WriteAtomic(context.Background(), key, messages(3, "sqlite")); err != nil {
		t.Fatal(err)
	}
	history, err := repository.Read(context.Background(), key)
	if err != nil || len(history) != 3 || *history[2].Content != "sqlite-2" {
		t.Fatalf("history=%v err=%v", history, err)
	}
	listed, err := repository.List(context.Background(), "project")
	if err != nil || len(listed) != 1 || listed[0].SessionID != "session" {
		t.Fatalf("list=%v err=%v", listed, err)
	}
}

func TestRouterPersistsAndSwitchesConfiguredBackend(t *testing.T) {
	root := t.TempDir()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	router.SetWorkspace("project")
	if err := router.Save("session", messages(1, "json")); err != nil {
		t.Fatal(err)
	}
	if err := router.Configure(context.Background(), Config{Backend: BackendSQLite, Path: filepath.Join(root, "sessions.db")}); err != nil {
		t.Fatal(err)
	}
	if router.Config().Backend != BackendSQLite {
		t.Fatalf("backend=%q", router.Config().Backend)
	}
	if err := router.Save("session", messages(1, "sqlite")); err != nil {
		t.Fatal(err)
	}
	history, err := router.Load("session")
	if err != nil || *history[0].Content != "sqlite-0" {
		t.Fatalf("history=%v err=%v", history, err)
	}
}

func TestRouterReadsExplicitWorkspaceWithoutChangingActiveScope(t *testing.T) {
	root := t.TempDir()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()

	router.SetWorkspace("project-a")
	if err := router.Save("shared", messages(1, "a")); err != nil {
		t.Fatal(err)
	}
	router.SetWorkspace("project-b")
	if err := router.Save("shared", messages(1, "b")); err != nil {
		t.Fatal(err)
	}
	router.SetWorkspace("project-a")

	history, err := router.LoadWorkspace("project-b", "shared")
	if err != nil || len(history) != 1 || *history[0].Content != "b-0" {
		t.Fatalf("project-b history=%v err=%v", history, err)
	}
	if router.Workspace() != "project-a" {
		t.Fatalf("explicit read changed active workspace to %q", router.Workspace())
	}
	listed := router.ListWorkspace("project-b")
	if len(listed) != 1 || listed[0].SessionID != "shared" {
		t.Fatalf("project-b sessions=%v", listed)
	}
}
