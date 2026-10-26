package core

import (
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

func newServiceSessionColdCommit(service *Service) *applicationsessioncold.CommitApplication {
	return applicationsessioncold.NewCommit(applicationsessioncold.CommitDeps{
		AppendPendingTail: service.appendPendingTailNoticeLocked,
		SetSessionChat:    service.setSessionChatLockedFor,
		MirrorPlan: func(sessionID string, plan *model.PlanState) {
			service.mirrorPlanProjectionForSessionLocked(sessionID, func() *model.PlanState { return plan })
		},
		SystemPrompt: func() string {
			return service.components.prompts.SystemPromptForActiveTaskLocked()
		},
		ApplyWorkspace: func(_ *model.WorkspaceInfo, projection switchworkspace.Projection) {
			service.applyWorkspaceProjectionLocked(projection)
		},
		BumpRevision: service.bumpLocked,
	})
}
