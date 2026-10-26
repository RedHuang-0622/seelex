package core

import (
	"context"

	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	applicationsessionquery "github.com/RedHuang-0622/seelex/application/session/query"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func (service *Service) assembleSessionReadApplications(historyWindowAssembler applicationhistory.WindowAssembler) {
	inputIndex := applicationsessionquery.NewInputIndex(applicationsessionquery.InputIndexDeps{
		ViewMu: &service.ViewMu,
		Locate: func(sessionID string) session_runtime.Location {
			return service.components.sessions.LocateSession(sessionID)
		},
		LoadRange: func(workspaceID, sessionID string) ([]model.Message, int, bool, error) {
			store, ok := service.Deps.Sessions.(session_runtime.SessionConversationRangePort)
			if !ok {
				return nil, 0, false, nil
			}
			messages, total, err := store.LoadConversationRangeWorkspace(workspaceID, sessionID, 0, 0)
			return messages, total, true, err
		},
		LoadRecord: func(location session_runtime.Location, sessionID string) (model.SessionRecord, bool, error) {
			return service.components.sessions.LoadSessionRecord(location, sessionID)
		},
		Record: func(record model.SessionRecord) []model.Message {
			return service.components.sessions.RecordConversation(record)
		},
		ReadUnit: func(sessionID string, read func(*sessionstate.View)) {
			if unit := service.sessions.Unit(sessionID); unit != nil {
				unit.View.Read(read)
			}
		},
		BuildItems: func(conversation []model.Message, loaded []string, limit int, userText func(model.Message) string) []model.SessionInputIndexRow {
			return service.components.residency.BuildInputIndex(conversation, loaded, limit, userText)
		},
		SummaryLimit: func() int {
			return service.components.residency.InputIndexSummaryLimit()
		},
		HistoryWindow: func() int {
			return applicationpolicy.Get().HistoryWindow
		},
		DurableCount: view_state.DurableConversationCount,
		UserText:     inputIndexUserText,
	})
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
		InputIndex: inputIndex.Read,
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
		ContentUnloaded: func(sessionID string) bool {
			return service.components.residencyRuntime.ContentUnloaded(sessionID)
		},
		ReloadContent:          service.reloadSessionContent,
		WindowAssembler:        historyWindowAssembler,
		VisibleWindowFirstRow:  service.visibleWindowFirstRow,
		LoadEarlierVisiblePage: service.loadEarlierVisiblePage,
		InstallVisibleHistory:  service.installVisibleHistory,
		VisibleTailInMemory:    service.visibleTailServedFromMemory,
		LoadConversationTail:   service.loadConversationTailPage,
	})
}
