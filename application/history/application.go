// Package history owns conversation-history window paging decisions. Durable
// reads, visible-window installation and content LRU remain injectable hosts.
package history

import "github.com/RedHuang-0622/seelex/contract/protocol"

// WindowAssembler 是历史窗口安装需要的消费方端口。
//
// 实现负责行身份、接缝、durable 行和窗口边界判据；Application 只决定分页
// 顺序与何时调用。ContainsSameRow / HasRowIdentity 来自真实冷读扩窗调用，
// 不能由 core 重写字段判断。
type WindowAssembler interface {
	ReplaceTail(pageRows, memoryRows []model.Message, memoryStart, memoryTotal, window int) ([]model.Message, int)
	Prepend(pageRows, memoryRows []model.Message, memoryStart, window int) ([]model.Message, int)
	DurableRows(messages []model.Message) []model.Message
	IndexOfSameRow(rows []model.Message, want model.Message) int
	ContainsSameRow(rows []model.Message, want model.Message) bool
	HasRowIdentity(message model.Message) bool
}

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
	WindowAssembler        WindowAssembler
	VisibleWindowFirstRow  func(sessionID string, assembler WindowAssembler) model.Message
	LoadEarlierVisiblePage func(workspaceID, sessionID string, offset, limit, window int, seam model.Message, assembler WindowAssembler) (Page, error)
	InstallVisibleHistory  func(sessionID string, page Page, window int, mode InstallMode, assembler WindowAssembler) error
	VisibleTailInMemory    func(sessionID string, assembler WindowAssembler) bool
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
	seam := application.deps.VisibleWindowFirstRow(sessionID, application.deps.WindowAssembler)
	page, err := application.deps.LoadEarlierVisiblePage(
		workspaceID,
		sessionID,
		offset,
		offset-loadOffset,
		window,
		seam,
		application.deps.WindowAssembler,
	)
	if err != nil {
		return err
	}
	return application.deps.InstallVisibleHistory(sessionID, page, window, InstallPrepend, application.deps.WindowAssembler)
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
	if application.deps.VisibleTailInMemory(sessionID, application.deps.WindowAssembler) {
		return nil
	}

	page, err := application.deps.LoadConversationTail(workspaceID, sessionID, window)
	if err != nil {
		return err
	}
	return application.deps.InstallVisibleHistory(sessionID, page, window, InstallReplace, application.deps.WindowAssembler)
}
