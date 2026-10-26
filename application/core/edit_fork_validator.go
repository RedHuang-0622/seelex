package core

import applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"

func newServiceEditForkValidator(service *Service) *applicationedit.Validator {
	return applicationedit.NewValidator(applicationedit.ValidatorDeps{
		Snapshot: func(parentID string) applicationedit.ParentState {
			service.ViewMu.RLock()
			state := applicationedit.ParentState{CurrentSessionID: service.Core.Snapshot.Session.ID}
			if unit := service.sessions.Unit(parentID); unit != nil {
				chat := unit.ChatState()
				state.Running = chat.Running
				state.Queued = chat.QueuedCount > 0 || len(unit.PendingRequests()) > 0
			}
			state.Floor, state.HasWindow = service.components.tasks.RetainedWindowFirstEventSeqFor(parentID)
			service.ViewMu.RUnlock()
			return state
		},
		ChatRunningError: ErrChatRunning,
	})
}
