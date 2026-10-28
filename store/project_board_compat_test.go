package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storeboard "github.com/RedHuang-0622/seelex/store/board"
)

func TestProjectBoardReclaimUsesInjectedProjectRootButLeavesModuleLocksAlone(t *testing.T) {
	router := newLockProfileRouter(t, t.TempDir())
	projectRoot := t.TempDir()
	const sessionID = "session-reclaim"
	ConfigureProjectBoardReclaimer(router, func(got string) error {
		if got != sessionID {
			t.Fatalf("reclaimer got session %q, want %q", got, sessionID)
		}
		return storeboard.Reclaim(projectRoot, got)
	})
	row := dto.ProjectBoardRow{
		Schema: dto.ProjectBoardSchema, Kind: dto.ProjectBoardKindWork, Key: "work:1", Seq: 1,
		Scope: dto.ProjectBoardScope{Session: sessionID}, Payload: map[string]any{"task": "compat"},
	}
	if _, err := storeboard.Append(projectRoot, sessionID, dto.ProjectBoardWorkFile, []dto.ProjectBoardRow{row}); err != nil {
		t.Fatal(err)
	}
	boardLock := storeboard.Mutex(filepath.Join(storeboard.Dir(projectRoot), sessionID, dto.ProjectBoardWorkFile))
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
	if err := router.projectBoardReclaim(sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storeboard.SessionDir(projectRoot, sessionID)); !os.IsNotExist(err) {
		t.Fatalf("reclaimed directory remains: %v", err)
	}
}
