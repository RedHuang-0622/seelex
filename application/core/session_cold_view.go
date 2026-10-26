package core

import (
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServiceSessionColdView(service *Service) *applicationsessioncold.ViewApplication {
	return applicationsessioncold.NewView(applicationsessioncold.ViewDeps{
		LoadForkRef: func(location session_runtime.Location, sessionID string) *model.SessionForkRef {
			return service.components.sessionForkLineage.Load(location, sessionID)
		},
		AdvanceSeq: func(sessionID string, messages []model.Message) {
			service.components.view.AdvanceMessageSeqForLocked(sessionID, messages)
		},
		RecordTail: func(record model.SessionRecord, window int) []model.Message {
			return service.components.sessions.RecordConversationTail(record, window)
		},
		ViewEmpty: service.sessionViewEmptyLocked,
		SetView: func(sessionID string, view *sessioncase.View) {
			service.components.view.SetSessionViewLocked(sessionID, view)
		},
		AppendHistory: service.appendHistoryLockedFor,
		MutateView: func(sessionID string, update func(*sessioncase.View)) {
			service.components.view.SessionViewMutateLocked(sessionID, update)
		},
		MirrorActiveView: func() {
			service.components.view.MirrorActiveViewLocked()
		},
	})
}
