package query

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type queryWorkspace struct {
	binding model.WorkspaceInfo
	ok      bool
}

func (workspace queryWorkspace) Create(string, string, string) (model.WorkspaceInfo, error) {
	return model.WorkspaceInfo{}, nil
}
func (workspace queryWorkspace) Get(string) (model.WorkspaceInfo, error) {
	return model.WorkspaceInfo{}, nil
}
func (workspace queryWorkspace) List() []model.WorkspaceInfo { return nil }
func (workspace queryWorkspace) Delete(string) error         { return nil }
func (workspace queryWorkspace) BindSession(string, string)  {}
func (workspace queryWorkspace) UnbindSession(string)        {}
func (workspace queryWorkspace) SessionWorkspace(sessionID string) (model.WorkspaceInfo, bool) {
	return workspace.binding, workspace.ok
}
func (workspace queryWorkspace) AllBindings() map[string]string { return nil }
func (workspace queryWorkspace) DetectGitRemote(string) string  { return "" }

func TestListSessionsKeepsReadLockAndComposesRows(t *testing.T) {
	var mu sync.RWMutex
	catalog := []model.SessionInfo{{ID: "session-a"}}
	application := New(Deps{
		ViewMu: &mu,
		CatalogRows: func() []model.SessionInfo {
			return catalog
		},
		WithLiveRows: func(rows []model.SessionInfo) []model.SessionInfo {
			if mu.TryLock() {
				mu.Unlock()
				t.Fatal("ListSessions composition ran without the shared read lock")
			}
			return append(rows, model.SessionInfo{ID: "session-b"})
		},
		EnrichRows: func(rows []model.SessionInfo) []model.SessionInfo {
			rows[0].Name = "enriched"
			return rows
		},
	})

	rows := application.ListSessions()
	if len(rows) != 2 || rows[0].ID != "session-a" || rows[0].Name != "enriched" || rows[1].ID != "session-b" {
		t.Fatalf("ListSessions = %+v", rows)
	}
	if catalog[0].Name != "" {
		t.Fatalf("ListSessions mutated the catalog source: %+v", catalog)
	}
	rows[0].ID = "mutated"
	if catalog[0].ID != "session-a" {
		t.Fatalf("result aliases catalog storage: %+v", catalog)
	}
}

func TestProjectRootForUsesBindingOnly(t *testing.T) {
	workspace := queryWorkspace{
		binding: model.WorkspaceInfo{ID: "workspace-a", RootPath: "  /repo/project  "},
		ok:      true,
	}
	application := New(Deps{Workspace: workspace})
	if got := application.ProjectRootFor(" session-a "); got != "/repo/project" {
		t.Fatalf("ProjectRootFor = %q", got)
	}
	if got := application.ProjectRootFor(" "); got != "" {
		t.Fatalf("empty ProjectRootFor = %q", got)
	}
	application.deps.Workspace = queryWorkspace{ok: false}
	if got := application.ProjectRootFor("session-a"); got != "" {
		t.Fatalf("unbound ProjectRootFor = %q", got)
	}
}

func TestDurableReadsDelegate(t *testing.T) {
	snapshotErr := errors.New("snapshot")
	transcriptErr := errors.New("transcript")
	indexErr := errors.New("index")
	application := New(Deps{
		SnapshotOf: func(sessionID string) (model.SessionSnapshot, error) {
			if sessionID != "session-a" {
				t.Fatalf("SnapshotOf session = %q", sessionID)
			}
			return model.SessionSnapshot{Session: model.SessionState{ID: "session-a"}}, snapshotErr
		},
		Transcript: func(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error) {
			if sessionID != "session-a" || fromSeq != 2 || toSeq != 5 {
				t.Fatalf("transcript args = %q, %d, %d", sessionID, fromSeq, toSeq)
			}
			return []model.TranscriptEvent{{Seq: 2}}, transcriptErr
		},
		InputIndex: func(sessionID string) (model.SessionInputIndex, error) {
			if sessionID != "session-a" {
				t.Fatalf("input index session = %q", sessionID)
			}
			return model.SessionInputIndex{SessionID: "session-a"}, indexErr
		},
	})
	if snapshot, err := application.SnapshotOf("session-a"); snapshot.Session.ID != "session-a" ||
		!errors.Is(err, snapshotErr) {
		t.Fatalf("SnapshotOf = %+v, %v", snapshot, err)
	}
	if transcript, err := application.GetSessionTranscript("session-a", 2, 5); len(transcript) != 1 ||
		!errors.Is(err, transcriptErr) {
		t.Fatalf("GetSessionTranscript = %+v, %v", transcript, err)
	}
	if index, err := application.SessionInputIndex("session-a"); index.SessionID != "session-a" ||
		!errors.Is(err, indexErr) {
		t.Fatalf("SessionInputIndex = %+v, %v", index, err)
	}
}

func TestWaitCatalogRefreshDelegatesWithoutState(t *testing.T) {
	want := errors.New("catalog")
	application := New(Deps{WaitCatalog: func(ctx context.Context) error {
		if ctx == nil {
			t.Fatal("nil context")
		}
		return want
	}})
	if err := application.WaitCatalogRefresh(context.Background()); !errors.Is(err, want) {
		t.Fatalf("WaitCatalogRefresh = %v", err)
	}
}

var _ contract.WorkspacePort = queryWorkspace{}
