package store

import (
	"os"
	"path/filepath"
	"testing"

	storeboard "github.com/RedHuang-0622/seelex/store/board"
)

func TestProjectBoardRouterCompatibilityForwardsToBoardOwner(t *testing.T) {
	router := newLockProfileRouter(t, t.TempDir())
	projectRoot := t.TempDir()
	const sessionID = "session-compat"
	row := ProjectBoardRow{
		Schema: ProjectBoardSchema, Kind: ProjectBoardKindWork, Key: "work:1", Seq: 1,
		Scope: ProjectBoardScope{Session: sessionID}, Payload: map[string]any{"task": "compat"},
	}
	if _, err := router.AppendProjectBoardRows(projectRoot, sessionID, ProjectBoardWorkFile, []ProjectBoardRow{row}); err != nil {
		t.Fatal(err)
	}
	rows, err := router.ReadProjectBoardRows(projectRoot, sessionID, ProjectBoardWorkFile)
	if err != nil || len(rows) != 1 || rows[0].Key != "work:1" {
		t.Fatalf("compat read rows=%#v err=%v", rows, err)
	}
	if _, err := router.ReadProjectBoardMarkdown(projectRoot); err != nil {
		t.Fatalf("compat markdown read: %v", err)
	}
}

func TestProjectBoardReclaimUsesInjectedProjectRootButLeavesModuleLocksAlone(t *testing.T) {
	router := newLockProfileRouter(t, t.TempDir())
	projectRoot := t.TempDir()
	const sessionID = "session-reclaim"
	router.SetProjectBoardRootFor(func(got string) string {
		if got != sessionID {
			t.Fatalf("resolver got session %q, want %q", got, sessionID)
		}
		return projectRoot
	})
	row := ProjectBoardRow{
		Schema: ProjectBoardSchema, Kind: ProjectBoardKindWork, Key: "work:1", Seq: 1,
		Scope: ProjectBoardScope{Session: sessionID}, Payload: map[string]any{"task": "compat"},
	}
	if _, err := router.AppendProjectBoardRows(projectRoot, sessionID, ProjectBoardWorkFile, []ProjectBoardRow{row}); err != nil {
		t.Fatal(err)
	}
	boardLock := projectBoardLock(filepath.Join(storeboard.Dir(projectRoot), sessionID, ProjectBoardWorkFile))
	repository, ok := router.jsonRepository()
	if !ok {
		t.Fatal("router is not JSON repository")
	}
	key := Key{ProjectID: "project-lock", SessionID: "session-lock"}
	for _, module := range allStorageModules() {
		if repository.layout.mu(key, module) == boardLock {
			t.Fatalf("board lock aliases module %q", module)
		}
	}
	if err := router.ReclaimProjectBoard("ignored", sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storeboard.SessionDir(projectRoot, sessionID)); !os.IsNotExist(err) {
		t.Fatalf("reclaimed directory remains: %v", err)
	}
}
