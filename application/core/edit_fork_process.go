package core

import (
	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	"github.com/RedHuang-0622/seelex/store"
)

func newServiceEditForkProcess(service *Service) *applicationedit.ProcessApplication {
	return applicationedit.NewProcess(applicationedit.ProcessDeps{
		Resolve: func(parentID string, targetSeq uint64) (store.Event, uint64, uint64, error) {
			return service.resolveEditForkTarget(parentID, targetSeq)
		},
		Validate:         service.validateEditForkParent,
		TransitionForKey: service.transitionForKey,
		Fork:             service.forkSessionLocked,
		Resume:           service.resumeSession,
		Submit:           service.submitConversationFor,
		Rollback:         service.components.editForkRollback.Rollback,
	})
}

func newServiceEditForkApplication(service *Service) *applicationedit.EditApplication {
	return applicationedit.NewEditApplication(applicationedit.EditApplicationDeps{
		Registry: service.components.editForkOperationRegistry,
		Run:      service.components.editForkProcess.Run,
	})
}
