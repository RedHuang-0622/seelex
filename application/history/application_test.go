package history

import (
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestLoadMoreHistoryPrependsEarlierPageOutsideViewLock(t *testing.T) {
	offset := 10
	reloaded := false
	installed := false
	application := New(Deps{
		HistoryWindow: func() int { return 5 },
		ReadViewState: func() (string, string, int) {
			return "session-a", "workspace-a", offset
		},
		ContentUnloaded: func(string) bool { return !reloaded },
		ReloadContent: func(string) error {
			reloaded = true
			offset = 8
			return nil
		},
		VisibleWindowFirstRow: func(string) model.Message {
			return model.Message{ID: "seam"}
		},
		LoadEarlierVisiblePage: func(workspaceID, sessionID string, gotOffset, gotLimit, gotWindow int, seam model.Message) (Page, error) {
			if workspaceID != "workspace-a" || sessionID != "session-a" ||
				gotOffset != 8 || gotLimit != 5 || gotWindow != 5 || seam.ID != "seam" {
				t.Fatalf("load args = %q, %q, %d, %d, %d, %+v",
					workspaceID, sessionID, gotOffset, gotLimit, gotWindow, seam)
			}
			return Page{Rows: []model.Message{{ID: "older"}}, Start: 3}, nil
		},
		InstallVisibleHistory: func(sessionID string, page Page, window int, mode InstallMode) error {
			installed = true
			if sessionID != "session-a" || len(page.Rows) != 1 || window != 5 || mode != InstallPrepend {
				t.Fatalf("install = %q, %+v, %d, %d", sessionID, page, window, mode)
			}
			return nil
		},
	})
	if err := application.LoadMoreHistory(9); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Fatal("LoadMoreHistory did not install the page")
	}
}

func TestLoadMoreHistoryStopsAtBeginning(t *testing.T) {
	called := false
	application := New(Deps{
		HistoryWindow: func() int { return 5 },
		ReadViewState: func() (string, string, int) {
			return "session-a", "workspace-a", 0
		},
		ContentUnloaded: func(string) bool { return false },
		LoadEarlierVisiblePage: func(string, string, int, int, int, model.Message) (Page, error) {
			called = true
			return Page{}, nil
		},
	})
	if err := application.LoadMoreHistory(0); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("LoadMoreHistory read a page at the beginning")
	}
}

func TestLoadLatestHistoryUsesMemoryTailShortCircuit(t *testing.T) {
	called := false
	application := New(Deps{
		HistoryWindow: func() int { return 3 },
		ReadViewState: func() (string, string, int) {
			return "session-a", "workspace-a", 4
		},
		ContentUnloaded:     func(string) bool { return false },
		VisibleTailInMemory: func(string) bool { return true },
		LoadConversationTail: func(string, string, int) (Page, error) {
			called = true
			return Page{}, nil
		},
	})
	if err := application.LoadLatestHistory(); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("LoadLatestHistory read a tail page despite an in-memory tail")
	}
}

func TestLoadLatestHistoryLoadsAndReplacesTail(t *testing.T) {
	installed := false
	application := New(Deps{
		HistoryWindow: func() int { return 3 },
		ReadViewState: func() (string, string, int) {
			return "session-a", "workspace-a", 1
		},
		ContentUnloaded:     func(string) bool { return false },
		VisibleTailInMemory: func(string) bool { return false },
		LoadConversationTail: func(workspaceID, sessionID string, window int) (Page, error) {
			if workspaceID != "workspace-a" || sessionID != "session-a" || window != 3 {
				t.Fatalf("tail args = %q, %q, %d", workspaceID, sessionID, window)
			}
			return Page{Rows: []model.Message{{ID: "latest"}}, Start: 4}, nil
		},
		InstallVisibleHistory: func(sessionID string, page Page, window int, mode InstallMode) error {
			installed = true
			if sessionID != "session-a" || page.Rows[0].ID != "latest" || mode != InstallReplace {
				t.Fatalf("install = %q, %+v, %d, %d", sessionID, page, window, mode)
			}
			return nil
		},
	})
	if err := application.LoadLatestHistory(); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Fatal("LoadLatestHistory did not install the tail")
	}
}

func TestPageReachesPublishedTail(t *testing.T) {
	if !(Page{RawRows: 2, Start: 3, DiskTotal: 5}).ReachesPublishedTail() {
		t.Fatal("tail page not recognized")
	}
	if (Page{RawRows: 0, Start: 5, DiskTotal: 5}).ReachesPublishedTail() {
		t.Fatal("empty page recognized as published tail")
	}
}

func TestHistoryErrorsPropagate(t *testing.T) {
	loadErr := errors.New("load")
	application := New(Deps{
		HistoryWindow: func() int { return 1 },
		ReadViewState: func() (string, string, int) {
			return "session-a", "workspace-a", 1
		},
		ContentUnloaded:       func(string) bool { return false },
		VisibleWindowFirstRow: func(string) model.Message { return model.Message{ID: "seam"} },
		LoadEarlierVisiblePage: func(string, string, int, int, int, model.Message) (Page, error) {
			return Page{}, loadErr
		},
	})
	if err := application.LoadMoreHistory(1); !errors.Is(err, loadErr) {
		t.Fatalf("LoadMoreHistory error = %v", err)
	}
}
