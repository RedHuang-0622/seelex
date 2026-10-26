package core

import (
	"context"

	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	applicationsessionquery "github.com/RedHuang-0622/seelex/application/session/query"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func (service *Service) assembleSessionReadApplications(historyWindowAssembler applicationhistory.WindowAssembler) {
	service.components.sessionQuery = applicationsessionquery.New(applicationsessionquery.Deps{
		ViewMu: &service.ViewMu,
		CatalogRows: func() []model.SessionInfo {
			return service.Core.Snapshot.Sessions
		},
		WithLiveRows: func(rows []model.SessionInfo) []model.SessionInfo {
			return service.components.view.WithLiveSessionRowsLocked(rows)
		},
		EnrichRows: func(rows []model.SessionInfo) []model.SessionInfo {
			return service.components.view.EnrichDirectoryRowsLocked(rows)
		},
		Meta:      service.metaDeps,
		Workspace: service.Deps.Workspace,
		WaitCatalog: func(ctx context.Context) error {
			return service.components.residency.WaitCatalogRefresh(ctx, service.components.sessions)
		},
		SnapshotOf: service.readSnapshotOf,
		Transcript: service.readSessionTranscript,
		InputIndex: service.readSessionInputIndex,
	})

	service.components.conversationHistoryQuery = applicationhistory.New(applicationhistory.Deps{
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
		WindowAssembler:        historyWindowAssembler,
		VisibleWindowFirstRow:  service.visibleWindowFirstRow,
		LoadEarlierVisiblePage: service.loadEarlierVisiblePage,
		InstallVisibleHistory:  service.installVisibleHistory,
		VisibleTailInMemory:    service.visibleTailServedFromMemory,
		LoadConversationTail:   service.loadConversationTailPage,
	})
}
