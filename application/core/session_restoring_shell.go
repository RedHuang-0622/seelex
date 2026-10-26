package core

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
)

func newServiceSessionRestoringShell(service *Service) *applicationsession.RestoringShellApplication {
	return applicationsession.NewRestoringShell(applicationsession.RestoringShellDeps{
		Title:            func(sessionID string) string { return service.components.sessions.SessionTitleFor(sessionID).Value },
		SetActive:        service.sessions.SetActive,
		SetSessionChat:   service.setSessionChatLockedFor,
		MirrorActiveView: service.mirrorActiveViewLocked,
		BumpRevision:     service.bumpLocked,
	})
}
