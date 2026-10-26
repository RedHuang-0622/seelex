package history

import (
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type stubWindowAssembler struct{}

func (stubWindowAssembler) ReplaceTail(pageRows, _ []model.Message, _, _, _ int) ([]model.Message, int) {
	return pageRows, 0
}

func (stubWindowAssembler) Prepend(pageRows, _ []model.Message, start, _ int) ([]model.Message, int) {
	return pageRows, start
}

func (stubWindowAssembler) DurableRows(messages []model.Message) []model.Message { return messages }

func (stubWindowAssembler) IndexOfSameRow([]model.Message, model.Message) int { return -1 }

func (stubWindowAssembler) ContainsSameRow([]model.Message, model.Message) bool { return false }

func (stubWindowAssembler) HasRowIdentity(model.Message) bool { return false }

func TestLoadMoreHistoryPrependsEarlierPageOutsideViewLock(t *testing.T) {
	offset := 10
	reloaded := false
	installed := false
	assembler := stubWindowAssembler{}
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
		WindowAssembler: assembler,
		VisibleWindowFirstRow: func(_ string, got WindowAssembler) model.Message {
			if got != assembler {
				t.Fatal("VisibleWindowFirstRow received a different WindowAssembler")
			}
			return model.Message{ID: "seam"}
		},
		LoadEarlierVisiblePage: func(workspaceID, sessionID string, gotOffset, gotLimit, gotWindow int, seam model.Message, got WindowAssembler) (Page, error) {
			if workspaceID != "workspace-a" || sessionID != "session-a" ||
				gotOffset != 8 || gotLimit != 5 || gotWindow != 5 || seam.ID != "seam" || got != assembler {
				t.Fatalf("load args = %q, %q, %d, %d, %d, %+v",
					workspaceID, sessionID, gotOffset, gotLimit, gotWindow, seam)
			}
			return Page{Rows: []model.Message{{ID: "older"}}, Start: 3}, nil
		},
		InstallVisibleHistory: func(sessionID string, page Page, window int, mode InstallMode, got WindowAssembler) error {
			installed = true
			if sessionID != "session-a" || len(page.Rows) != 1 || window != 5 || mode != InstallPrepend || got != assembler {
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
	assembler := stubWindowAssembler{}
	application := New(Deps{
		HistoryWindow: func() int { return 5 },
		ReadViewState: func() (string, string, int) {
			return "session-a", "workspace-a", 0
		},
		ContentUnloaded: func(string) bool { return false },
		WindowAssembler: assembler,
		LoadEarlierVisiblePage: func(string, string, int, int, int, model.Message, WindowAssembler) (Page, error) {
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
	assembler := stubWindowAssembler{}
	application := New(Deps{
		HistoryWindow: func() int { return 3 },
		ReadViewState: func() (string, string, int) {
			return "session-a", "workspace-a", 4
		},
		ContentUnloaded: func(string) bool { return false },
		WindowAssembler: assembler,
		VisibleTailInMemory: func(_ string, got WindowAssembler) bool {
			if got != assembler {
				t.Fatal("VisibleTailInMemory received a different WindowAssembler")
			}
			return true
		},
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
	assembler := stubWindowAssembler{}
	application := New(Deps{
		HistoryWindow: func() int { return 3 },
		ReadViewState: func() (string, string, int) {
			return "session-a", "workspace-a", 1
		},
		ContentUnloaded:     func(string) bool { return false },
		WindowAssembler:     assembler,
		VisibleTailInMemory: func(string, WindowAssembler) bool { return false },
		LoadConversationTail: func(workspaceID, sessionID string, window int) (Page, error) {
			if workspaceID != "workspace-a" || sessionID != "session-a" || window != 3 {
				t.Fatalf("tail args = %q, %q, %d", workspaceID, sessionID, window)
			}
			return Page{Rows: []model.Message{{ID: "latest"}}, Start: 4}, nil
		},
		InstallVisibleHistory: func(sessionID string, page Page, window int, mode InstallMode, got WindowAssembler) error {
			installed = true
			if sessionID != "session-a" || page.Rows[0].ID != "latest" || mode != InstallReplace || got != assembler {
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
	assembler := stubWindowAssembler{}
	application := New(Deps{
		HistoryWindow: func() int { return 1 },
		ReadViewState: func() (string, string, int) {
			return "session-a", "workspace-a", 1
		},
		ContentUnloaded:       func(string) bool { return false },
		WindowAssembler:       assembler,
		VisibleWindowFirstRow: func(string, WindowAssembler) model.Message { return model.Message{ID: "seam"} },
		LoadEarlierVisiblePage: func(string, string, int, int, int, model.Message, WindowAssembler) (Page, error) {
			return Page{}, loadErr
		},
	})
	if err := application.LoadMoreHistory(1); !errors.Is(err, loadErr) {
		t.Fatalf("LoadMoreHistory error = %v", err)
	}
}
