package core

import (
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServiceSessionColdCommit(service *Service) *applicationsessioncold.CommitApplication {
	return applicationsessioncold.NewCommit(applicationsessioncold.CommitDeps{
		AppendPendingTail: service.appendPendingTailNoticeLocked,
		SetSessionChat: func(sessionID string, chat model.ChatState) {
			service.components.view.SetSessionChatLockedFor(sessionID, chat)
		},
		MirrorPlan: func(sessionID string, plan *model.PlanState) {
			service.mirrorPlanProjectionForSessionLocked(sessionID, func() *model.PlanState { return plan })
		},
		SystemPrompt: func() string {
			return service.components.prompts.SystemPromptForActiveTaskLocked()
		},
		ApplyWorkspace: func(_ *model.WorkspaceInfo, projection applicationworkspace.SwitchProjection) {
			service.applyWorkspaceProjectionLocked(projection)
		},
		BumpRevision: func() uint64 {
			return service.components.view.BumpLocked()
		},
	})
}
