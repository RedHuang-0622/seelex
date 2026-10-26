package core

import (
	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
)

// newServiceConversationHistoryQuery adapts the host's visible view, cold reads
// and window installation to the conversation-history application.
func newServiceConversationHistoryQuery(service *Service, assembler applicationhistory.WindowAssembler) *applicationhistory.Application {
	return applicationhistory.New(applicationhistory.Deps{
		HistoryWindow: func() int {
			return applicationpolicy.Get().HistoryWindow
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
		WindowAssembler:        assembler,
		VisibleWindowFirstRow:  service.visibleWindowFirstRow,
		LoadEarlierVisiblePage: service.loadEarlierVisiblePage,
		InstallVisibleHistory:  service.installVisibleHistory,
		VisibleTailInMemory:    service.visibleTailServedFromMemory,
		LoadConversationTail:   service.loadConversationTailPage,
	})
}
