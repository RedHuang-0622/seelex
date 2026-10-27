package assembly

import (
	"context"
	"sync"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	context_runtime "github.com/RedHuang-0622/seelex/application/conversation/context"
	"github.com/RedHuang-0622/seelex/application/event"
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	"github.com/RedHuang-0622/seelex/application/prompt"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// assembleConversationTurnComponents builds the complete turn pipeline after
// the view and context coordinators exist. Only Process and Runner outlive
// composition; the remaining stages are builder-local values.
type conversationTurnComponents struct {
	process *applicationconversation.TurnProcessApplication
	runner  *applicationconversation.TurnRunnerApplication
}

func (service *Service) assembleConversationTurnComponents() {
	process := applicationconversation.NewTurnProcess(applicationconversation.TurnProcessDeps{
		Tasks:          service.components.tasks,
		TurnState:      service.components.conversationTurnState,
		NextRequestID:  service.nextChatRequestIDLocked,
		EffortFor:      service.effortForSession,
		FallbackBudget: prompt.ReActBudgetFor,
		MarkBusy:       service.markBusyLocked,
		MarkIdle:       service.markIdleLocked,
		AnyChatRunning: service.anyChatRunningLocked,
		SetSessionChat: service.components.view.SetSessionChatLockedFor,
	})
	releaseWorkingHistory := func(string) {}
	if releaser, ok := service.Deps.Engine.(interface{ ReleaseWorkingHistoryFor(string) }); ok {
		releaseWorkingHistory = releaser.ReleaseWorkingHistoryFor
	}
	completion := applicationconversation.NewTurnCompletion(applicationconversation.TurnCompletionDeps{
		LocateSession:          service.components.sessions.LocateSession,
		PersistSession:         service.components.sessions.PersistCurrentSession,
		QueueFailConsumedInput: service.components.sessions.QueueFailConsumedInput,
		QueueConfirmConsumed:   service.components.sessions.QueueConfirmConsumedInput,
		ReleaseWorkingHistory:  releaseWorkingHistory,
	})

	outcome := applicationconversation.NewTurnOutcome(applicationconversation.TurnOutcomeDeps{
		ClearError: service.components.conversationTurnState.ClearErrorLocked,
		SetError:   service.components.conversationTurnState.SetErrorLocked,
		AppendErrorLine: func(sessionID, visibleError string) {
			service.components.view.AppendMessageLockedFor(sessionID, "error", visibleError, nil)
		},
		ApplyRuntimeProjection: service.components.view.ApplyRuntimeProjectionForLocked,
		SetSessionChat:         service.components.view.SetSessionChatLockedFor,
		MirrorTask: func(sessionID string) {
			if task := service.components.tasks.VisibleTaskStateFor(sessionID); task != nil {
				service.Core.Snapshot.Task = task
			}
		},
		BumpRevision: service.components.view.BumpLocked,
	})

	publisher := applicationconversation.NewTurnPublisher(applicationconversation.TurnPublisherDeps{
		PublishChatState: service.publishChatStateFor,
		PublishEvent: func(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) {
			service.publishSessionEvent(kind, revision, requestID, sessionID, payload)
		},
		SetCurrentTaskBatch:       service.Deps.Runtime.SetCurrentTaskBatch,
		PublishRuntimeProjections: service.publishRuntimeProjections,
	})

	var runner *applicationconversation.TurnRunnerApplication
	tail := applicationconversation.NewTurnTail(applicationconversation.TurnTailDeps{
		RebindWorkspaceWhenIdle: service.rebindViewWorkspaceWhenIdle,
		MarkPromotedConsumed:    service.components.sessions.QueueMarkConsumedInput,
		Publish:                 publisher.Publish,
		StartNext: func(ctx context.Context, sessionID, requestID string, request applicationconversation.Request) {
			go runner.Run(ctx, sessionID, requestID, request)
		},
		ReconcileSessionRelease: service.components.sessionReleaseApplication.Reconcile,
		FlushToolRecord: func(sessionID string) {
			service.toolRecords.Flush(sessionID)
		},
		Debug: runChatDebug,
	})

	commit := applicationconversation.NewTurnCommit(applicationconversation.TurnCommitDeps{
		Persist:  completion.Persist,
		Lock:     func() sync.Locker { return &service.ViewMu },
		Unit:     service.sessionUnitLocked,
		IsActive: service.isActiveSessionLocked,
		ApplyOutcome: func(sessionID, requestID string, unit *sessioncase.SessionUnit, projection view_state.RuntimeStateProjection, turnErr error) (string, bool) {
			return outcome.ApplyLocked(
				sessionID, requestID, unit, projection, turnErr,
			)
		},
		Promote: func(sessionID string, unit *sessioncase.SessionUnit) applicationconversation.Promotion {
			return process.PromoteLocked(sessionID, unit)
		},
		AppendNext: func(sessionID string, active bool, promotion applicationconversation.Promotion) (*model.Message, *model.Message) {
			if !promotion.Promoted {
				return nil, nil
			}
			userOrigin := model.MessageOrigin{
				RoleName: promotion.UserEvent.RoleName, RoleSessionID: promotion.UserEvent.RoleSessionID,
				RoundID: promotion.UserEvent.RoundID, UnitSeq: promotion.UserEvent.UnitSeq, EventSeq: promotion.UserEvent.Seq,
			}
			mainOrigin := model.MessageOrigin{RoleName: model.RoleNameMain, RoleSessionID: sessionID, RoundID: promotion.UserEvent.RoundID}
			if active {
				return service.components.view.AppendMessageWithOriginLocked("user", promotion.Request.DisplayInput, nil, userOrigin),
					service.components.view.AppendMessageWithOriginLocked("assistant", "", nil, mainOrigin)
			}
			return service.components.view.AppendMessageWithOriginLockedFor(sessionID, "user", promotion.Request.DisplayInput, nil, userOrigin),
				service.components.view.AppendMessageWithOriginLockedFor(sessionID, "assistant", "", nil, mainOrigin)
		},
		Mirror: func(sessionID string, unit *sessioncase.SessionUnit, active bool) uint64 {
			return outcome.MirrorLocked(sessionID, unit, active)
		},
		Stale: func(sessionID, requestID string, unit *sessioncase.SessionUnit) {
			runChatDebug(
				"runChat stale request session=%s request=%s runtimeRequest=%s (superseded)",
				sessionID, requestID, unit.ChatState().RequestID,
			)
		},
	})

	finalize := applicationconversation.NewTurnFinalize(applicationconversation.TurnFinalizeDeps{
		TailComplete: tail.Complete,
	})

	input := applicationconversation.NewTurnInput(applicationconversation.TurnInputDeps{
		ApplyPrompt: func(sessionID, requestID string) {
			service.components.prompts.ApplyActiveTaskSystemPromptFor(sessionID, requestID)
		},
		PrepareContext: func(sessionID, requestID, modelInput string) (string, error) {
			return service.components.context.PrepareExecutionContextFor(sessionID, requestID, modelInput)
		},
		Normalize: context_runtime.NonEmptyProviderInput,
	})

	execution := applicationconversation.NewTurnExecution(applicationconversation.TurnExecutionDeps{
		BindProjectRoot:     service.bindSessionProjectRoot,
		SyncSubagentPolicy:  service.syncSubagentPolicyFor,
		SyncReasoningEffort: service.syncSessionReasoningEffortFor,
		SyncFullAccess:      service.syncFullAccessFor,
	})

	response := applicationconversation.NewTurnResponse(applicationconversation.TurnResponseDeps{
		AttachReasoning: service.components.conversationStreamRuntime.AttachLatestReasoning,
		ObserveOutput: func(ctx context.Context, requestID, reply string, err error) error {
			return service.components.tasks.ObserveModelOutput(ctx, task_context.ModelOutput{
				RequestID: requestID, Reply: reply, Err: err,
			})
		},
		EnsureFinal: func(requestID, reply string) {
			service.components.tasks.EnsureFinalAssistantTranscript(requestID, reply)
		},
		HistoryFor: service.engineHistoryFor,
		BackfillReasoning: func(sessionID string, history []contract.EngineMessage) {
			service.components.tasks.BackfillAssistantReasoning(sessionID, history)
		},
	})

	recovery := applicationconversation.NewTurnRecovery(applicationconversation.TurnRecoveryDeps{
		TakeContextFailure: func(requestID string) error {
			return service.components.context.TakeContextControlFailure(requestID)
		},
		Recover:        service.components.providerRecovery.RecoverProviderFailureFor,
		Retryable:      context_runtime.RetryableAfterRecovery,
		Retry:          service.components.providerRecovery.RetryContextRecovery,
		FinalizeBudget: service.finalizeReActBudgetWithSink,
		FinalizeTask: func(ctx context.Context, requestID, reply string) error {
			return service.components.tasks.FinalizeTask(ctx, task_context.ChatEndSummary{
				RequestID: requestID, Reply: reply,
			})
		},
	})

	cleanup := applicationconversation.NewTurnCleanup(applicationconversation.TurnCleanupDeps{
		StampRoute:     service.components.conversationStreamRuntime.StampServedRoute,
		DiscardMailbox: service.discardPendingSubagentContextsFor,
		CollectProjection: func(sessionID string) view_state.RuntimeStateProjection {
			return service.components.view.CollectRuntimeProjectionFor(context.Background(), sessionID)
		},
		RemoveCheckpoints: func(sessionID string) error {
			return service.components.context.RemoveTaskContextCheckpointsFor(sessionID)
		},
		RemoveRecovery: service.components.providerRecovery.RemoveProviderContextRecoveryFor,
		RecordError: func(sessionID, requestID string, err error) {
			service.ViewMu.Lock()
			service.recordUnhandledTaskErrorLocked(sessionID, requestID, err)
			service.ViewMu.Unlock()
		},
	})

	runner = applicationconversation.NewTurnRunner(applicationconversation.TurnRunnerDeps{
		WithSessionID:      task_context.WithSessionID,
		StartDraftSnapshot: service.startDraftSnapshotLoop,
		PrepareExecution:   execution.Prepare,
		ClearReActBudget:   service.components.tasks.ClearReActBudget,
		Debug:              runChatDebug,
		NewBatchedDeltaSink: func(requestID string) (applicationconversation.StreamBatcher, func(string)) {
			batcher, onChunk := service.components.conversationStreamRuntime.NewBatchedDeltaSink(requestID)
			return batcher, onChunk
		},
		Stream:   service.chatStream,
		Input:    input,
		Response: response,
		Recovery: recovery,
		Cleanup:  cleanup,
		Commit:   commit,
		Finalize: finalize,
	})
	service.components.conversationTurn = &conversationTurnComponents{
		process: process,
		runner:  runner,
	}
}
