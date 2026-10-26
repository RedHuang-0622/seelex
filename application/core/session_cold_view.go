package core

import (
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServiceSessionColdView(service *Service) *applicationsessioncold.ViewApplication {
	return applicationsessioncold.NewView(applicationsessioncold.ViewDeps{
		LoadForkRef: service.loadSessionForkRef,
		AdvanceSeq:  service.advanceMessageSeqLocked,
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
		MirrorActiveView: service.mirrorActiveViewLocked,
	})
}
