package assembly

import (
	"math"
	"time"

	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

func (service *Service) assembleEditForkComponents() {
	service.components.editForkOperationRegistry = applicationedit.NewOperationRegistry(editForkOperationTTL, time.Now)

	service.components.editForkValidator = applicationedit.NewValidator(applicationedit.ValidatorDeps{
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

	service.components.editForkTargetResolver = applicationedit.NewTargetResolver(applicationedit.TargetResolverDeps{
		LoadEvents: func(parentID string) ([]protocol.TranscriptEvent, bool, error) {
			location := service.components.sessions.LocateSession(parentID)
			port, ok := service.Deps.Sessions.(session_runtime.SessionForkPort)
			if !ok {
				return nil, false, nil
			}
			events, err := port.LoadEventRangeWorkspace(location.WorkspaceID, parentID, 1, math.MaxUint64)
			return events, true, err
		},
		IsUserText: func(message protocol.Message) bool {
			return inputIndexUserText(message) != ""
		},
	})

	service.components.editForkRollback = applicationedit.NewRollback(applicationedit.RollbackDeps{
		Delete: service.components.sessionDelete.Delete,
		Resume: service.components.sessionResume.Resume,
	})

	service.components.editForkProcess = applicationedit.NewProcess(applicationedit.ProcessDeps{
		Resolve: func(parentID string, targetSeq uint64) (protocol.TranscriptEvent, uint64, uint64, error) {
			return service.components.editForkTargetResolver.Resolve(parentID, targetSeq)
		},
		Validate:         service.components.editForkValidator.Validate,
		TransitionForKey: service.transitionForKey,
		Fork: func(parentID string, request model.ForkRequest) (string, error) {
			return service.components.sessionForkRuntime.ForkSessionLocked(parentID, request)
		},
		Resume:   service.components.sessionResume.Resume,
		Submit:   service.submitConversationFor,
		Rollback: service.components.editForkRollback.Rollback,
	})

	service.components.editForkApplication = applicationedit.NewEditApplication(applicationedit.EditApplicationDeps{
		Registry: service.components.editForkOperationRegistry,
		Run:      service.components.editForkProcess.Run,
	})

	service.components.editForkAnnotation = applicationedit.NewAnnotation(applicationedit.AnnotationDeps{
		Input: func(sessionID string) applicationedit.AnnotationInput {
			floor, hasWindow := service.components.tasks.RetainedWindowFirstEventSeqFor(sessionID)
			return applicationedit.AnnotationInput{
				Transcript: service.components.tasks.TranscriptFor(sessionID),
				Floor:      floor,
				HasWindow:  hasWindow,
			}
		},
		IsUserText: func(message model.Message) bool {
			return inputIndexUserText(message) != ""
		},
	})
}
