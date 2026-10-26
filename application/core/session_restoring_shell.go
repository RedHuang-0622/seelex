package core

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServiceSessionRestoringShell(service *Service) *applicationsession.RestoringShellApplication {
	return applicationsession.NewRestoringShell(applicationsession.RestoringShellDeps{
		Title:     func(sessionID string) string { return service.components.sessions.SessionTitleFor(sessionID).Value },
		SetActive: service.sessions.SetActive,
		SetSessionChat: func(sessionID string, chat model.ChatState) {
			service.components.view.SetSessionChatLockedFor(sessionID, chat)
		},
		MirrorActiveView: func() {
			service.components.view.MirrorActiveViewLocked()
		},
		BumpRevision: func() uint64 {
			return service.components.view.BumpLocked()
		},
	})
}
