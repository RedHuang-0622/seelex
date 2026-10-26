package core

import (
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	switching "github.com/RedHuang-0622/seelex/application/session/switchpolicy"
)

func newServiceConversationQueue(service *Service) *applicationconversation.QueueApplication {
	return applicationconversation.NewQueue(applicationconversation.QueueDeps{
		ViewMu: &service.ViewMu,
		Lifecycle: func() (bool, bool) {
			return service.closed, service.draining
		},
		ResolveUnit: func(sessionID string) (string, applicationconversation.QueueUnit, error) {
			viewID := service.Core.Snapshot.Session.ID
			resolvedID := switching.QueueTargetSession(sessionID, viewID)
			unit := service.sessions.Unit(resolvedID)
			running := unit != nil && unit.ChatState().Running
			if err := switching.QueueTargetError(resolvedID, viewID, unit != nil, running); err != nil {
				return "", nil, err
			}
			return resolvedID, unit, nil
		},
		IsActive:        service.isActiveSessionLocked,
		MirrorChatState: service.setSessionChatLockedFor,
		BumpRevision:    service.bumpLocked,
		PublishSnapshot: func(revision uint64, sessionID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		PublishChatState: service.publishChatStateFor,
		TurnState:        service.components.conversationTurnState,
		DrainingError:    ErrApplicationDraining,
	})
}

func (service *Service) queueComponent() *applicationconversation.QueueApplication {
	if service == nil {
		return nil
	}
	return service.components.conversationQueue
}
