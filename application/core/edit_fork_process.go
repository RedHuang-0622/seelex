package core

import (
	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServiceEditForkProcess(service *Service) *applicationedit.ProcessApplication {
	return applicationedit.NewProcess(applicationedit.ProcessDeps{
		Resolve: func(parentID string, targetSeq uint64) (protocol.TranscriptEvent, uint64, uint64, error) {
			return service.components.editForkTargetResolver.Resolve(parentID, targetSeq)
		},
		Validate:         service.components.editForkValidator.Validate,
		TransitionForKey: service.transitionForKey,
		Fork:             service.forkSessionLocked,
		Resume:           service.components.sessionResume.Resume,
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
