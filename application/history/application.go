// Package history owns conversation-history window paging decisions. Durable
// reads, visible-window installation and content LRU remain injectable hosts.
package history

import "github.com/RedHuang-0622/seelex/middle/protocol/model"

// Page is one bounded conversation read with both coordinate spaces retained.
// Rows and Start are visible-space values; RawRows and DiskTotal are the
// unfiltered persisted-space values.
type Page struct {
	Rows      []model.Message
	RawRows   int
	DiskTotal int
	Start     int
}

// ReachesPublishedTail reports whether this page reached the unfiltered
// persisted tail.
func (page Page) ReachesPublishedTail() bool {
	return page.RawRows > 0 && page.Start+page.RawRows >= page.DiskTotal
}

// InstallMode describes how a loaded page enters the visible window.
type InstallMode int

const (
	// InstallPrepend inserts an earlier page and moves the window backwards.
	InstallPrepend InstallMode = iota
	// InstallReplace replaces the window with the latest tail page.
	InstallReplace
)

// Deps supplies the history paging primitives. The callbacks keep the host's
// existing lock boundaries and authoritative storage paths.
type Deps struct {
	HistoryWindow          func() int
	ReadViewState          func() (sessionID, workspaceID string, offset int)
	ContentUnloaded        func(sessionID string) bool
	ReloadContent          func(sessionID string) error
	VisibleWindowFirstRow  func(sessionID string) model.Message
	LoadEarlierVisiblePage func(workspaceID, sessionID string, offset, limit, window int, seam model.Message) (Page, error)
	InstallVisibleHistory  func(sessionID string, page Page, window int, mode InstallMode) error
	VisibleTailInMemory    func(sessionID string) bool
	LoadConversationTail   func(workspaceID, sessionID string, window int) (Page, error)
}

// Application owns the ordering and short-circuit decisions for history paging.
type Application struct {
	deps Deps
}

// New constructs the conversation-history application.
func New(deps Deps) *Application {
	return &Application{deps: deps}
}

// LoadMoreHistory prepends an earlier bounded page to the visible window.
func (application *Application) LoadMoreHistory(limit int) error {
	window := application.deps.HistoryWindow()
	if window <= 0 {
		window = 1
	}
	if limit <= 0 || limit > window {
		limit = window
	}

	sessionID, workspaceID, offset := application.deps.ReadViewState()
	if application.deps.ContentUnloaded(sessionID) {
		if err := application.deps.ReloadContent(sessionID); err != nil {
			return err
		}
		_, _, offset = application.deps.ReadViewState()
	}
	if offset <= 0 {
		return nil
	}

	loadOffset := offset - limit
	if loadOffset < 0 {
		loadOffset = 0
	}
	seam := application.deps.VisibleWindowFirstRow(sessionID)
	page, err := application.deps.LoadEarlierVisiblePage(
		workspaceID,
		sessionID,
		offset,
		offset-loadOffset,
		window,
		seam,
	)
	if err != nil {
		return err
	}
	return application.deps.InstallVisibleHistory(sessionID, page, window, InstallPrepend)
}

// LoadLatestHistory returns the visible session to the latest page.
func (application *Application) LoadLatestHistory() error {
	window := application.deps.HistoryWindow()
	if window <= 0 {
		window = 1
	}
	sessionID, workspaceID, _ := application.deps.ReadViewState()
	if application.deps.ContentUnloaded(sessionID) {
		return application.deps.ReloadContent(sessionID)
	}
	if application.deps.VisibleTailInMemory(sessionID) {
		return nil
	}

	page, err := application.deps.LoadConversationTail(workspaceID, sessionID, window)
	if err != nil {
		return err
	}
	return application.deps.InstallVisibleHistory(sessionID, page, window, InstallReplace)
}
