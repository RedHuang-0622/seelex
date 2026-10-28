package assembly

import (
	"context"
	"math"
	"time"

	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

type editForkComponents struct {
	application *applicationedit.EditApplication
	annotation  *applicationedit.AnnotationApplication
}

func (service *Service) assembleEditForkComponents() {
	registry := applicationedit.NewOperationRegistry(applicationedit.DefaultOperationTTL, time.Now)

	validator := applicationedit.NewValidator(applicationedit.ValidatorDeps{
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

	resolver := applicationedit.NewTargetResolver(applicationedit.TargetResolverDeps{
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

	rollback := applicationedit.NewRollback(applicationedit.RollbackDeps{
		Delete: service.components.sessionDelete.Delete,
		Resume: service.components.sessionResume.Resume,
	})

	process := applicationedit.NewProcess(applicationedit.ProcessDeps{
		Resolve: func(parentID string, targetSeq uint64) (protocol.TranscriptEvent, uint64, uint64, error) {
			return resolver.Resolve(parentID, targetSeq)
		},
		Validate:         validator.Validate,
		TransitionForKey: service.transitionForKey,
		Fork: func(parentID string, request model.ForkRequest) (string, error) {
			return service.components.sessionForkRuntime.ForkSessionLocked(parentID, request)
		},
		Resume: service.components.sessionResume.Resume,
		Submit: func(ctx context.Context, sessionID, text string) error {
			return service.components.conversationTurn.submission.SubmitFor(ctx, sessionID, text)
		},
		Rollback: rollback.Rollback,
	})

	application := applicationedit.NewEditApplication(applicationedit.EditApplicationDeps{
		Registry: registry,
		Run:      process.Run,
	})

	annotation := applicationedit.NewAnnotation(applicationedit.AnnotationDeps{
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
	service.components.editFork = &editForkComponents{
		application: application,
		annotation:  annotation,
	}
}
