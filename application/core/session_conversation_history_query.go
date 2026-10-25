package core

import "github.com/RedHuang-0622/seelex/middle/protocol/model"

// conversationHistoryQueryDeps 是历史窗口查询所需的窄读能力。所有涉及磁盘
// 的调用都在共享写锁之外完成；查询对象不持有 *Service。
type conversationHistoryQueryDeps struct {
	HistoryWindow          func() int
	ReadViewState          func() (sessionID, workspaceID string, offset int)
	ContentUnloaded        func(sessionID string) bool
	ReloadContent          func(sessionID string) error
	VisibleWindowFirstRow  func(sessionID string) model.Message
	LoadEarlierVisiblePage func(workspaceID, sessionID string, offset, limit, window int, seam model.Message) (conversationPage, error)
	InstallVisibleHistory  func(sessionID string, page conversationPage, window int, mode historyPageInstall) error
	VisibleTailInMemory    func(sessionID string) bool
	LoadConversationTail   func(workspaceID, sessionID string, window int) (conversationPage, error)
}

// conversationHistoryQuery 是 AP-005 的历史窗口查询实现。窗口写回只改会话
// 可见投影，不写持久化事实。
type conversationHistoryQuery struct {
	deps conversationHistoryQueryDeps
}

func newConversationHistoryQuery(deps conversationHistoryQueryDeps) *conversationHistoryQuery {
	return &conversationHistoryQuery{deps: deps}
}

// LoadMoreHistory 把更早的一页历史前置到当前可见窗口。
func (query *conversationHistoryQuery) LoadMoreHistory(limit int) error {
	window := query.deps.HistoryWindow()
	if window <= 0 {
		window = 1
	}
	if limit <= 0 || limit > window {
		limit = window
	}

	sessionID, workspaceID, offset := query.deps.ReadViewState()
	if query.deps.ContentUnloaded(sessionID) {
		// 内容 LRU 卸载后再分页：窗口当前不在内存，先把尾部窗口从磁盘回读
		// （offset 回到尾部），再按本页请求向更早推进；否则「加载更早」会从
		// 一个不存在的窗口出发（页错位）。
		if err := query.deps.ReloadContent(sessionID); err != nil {
			return err
		}
		_, _, offset = query.deps.ReadViewState()
	}
	if offset <= 0 {
		return nil
	}

	loadOffset := offset - limit
	if loadOffset < 0 {
		loadOffset = 0
	}
	seam := query.deps.VisibleWindowFirstRow(sessionID)
	page, err := query.deps.LoadEarlierVisiblePage(
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
	return query.deps.InstallVisibleHistory(sessionID, page, window, historyPagePrepend)
}

// LoadLatestHistory 把可见会话拉回最新一页。
func (query *conversationHistoryQuery) LoadLatestHistory() error {
	window := query.deps.HistoryWindow()
	if window <= 0 {
		window = 1
	}
	sessionID, workspaceID, _ := query.deps.ReadViewState()
	if query.deps.ContentUnloaded(sessionID) {
		// 内容 LRU 卸载后「回到最新」= 从磁盘整窗回读。
		return query.deps.ReloadContent(sessionID)
	}
	if query.deps.VisibleTailInMemory(sessionID) {
		return nil
	}

	page, err := query.deps.LoadConversationTail(workspaceID, sessionID, window)
	if err != nil {
		return err
	}
	return query.deps.InstallVisibleHistory(sessionID, page, window, historyPageReplace)
}

// newServiceConversationHistoryQuery 复用现有唯一历史窗口判据与安装路径。
func newServiceConversationHistoryQuery(service *Service) *conversationHistoryQuery {
	return newConversationHistoryQuery(conversationHistoryQueryDeps{
		HistoryWindow: func() int {
			return Limits().HistoryWindow
		},
		ReadViewState: func() (sessionID, workspaceID string, offset int) {
			service.ViewMu.RLock()
			sessionID = service.Core.Snapshot.Session.ID
			workspaceID = currentWorkspaceIDLocked(service)
			offset = service.Core.Snapshot.HistoryOffset
			service.ViewMu.RUnlock()
			return sessionID, workspaceID, offset
		},
		ContentUnloaded:        service.sessionContentUnloaded,
		ReloadContent:          service.reloadSessionContent,
		VisibleWindowFirstRow:  service.visibleWindowFirstRow,
		LoadEarlierVisiblePage: service.loadEarlierVisiblePage,
		InstallVisibleHistory:  service.installVisibleHistory,
		VisibleTailInMemory:    service.visibleTailServedFromMemory,
		LoadConversationTail:   service.loadConversationTailPage,
	})
}
