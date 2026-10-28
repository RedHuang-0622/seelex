package assembly

import (
	"context"
	"errors"
	"sync"
	"time"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	context_runtime "github.com/RedHuang-0622/seelex/application/conversation/context"
	"github.com/RedHuang-0622/seelex/application/event"
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationtoolhooks "github.com/RedHuang-0622/seelex/application/execution/toolhooks"
	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	"github.com/RedHuang-0622/seelex/application/prompt"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// assembleConversationTurnComponents builds the complete turn pipeline after
// the view and context coordinators exist. Only Process and Runner outlive
// composition; the remaining stages are builder-local values.
type conversationTurnComponents struct {
	submission *applicationconversation.SubmissionApplication
	start      *applicationconversation.TurnStartApplication
	budget     *applicationconversation.TurnBudgetFinalizerApplication
	projection *applicationtoolhooks.ProjectionApplication
	process    *applicationconversation.TurnProcessApplication
	runner     *applicationconversation.TurnRunnerApplication
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
	releaseWorkingHistory := func(sessionID string) {
		contract.ReleaseWorkingHistoryFor(service.Deps.Engine, sessionID)
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

	budgetFinalizer := applicationconversation.NewTurnBudgetFinalizer(applicationconversation.TurnBudgetFinalizerDeps{
		SessionIDFromContext: task_context.SessionIDFromContext,
		FallbackSessionID: func() string {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.Core.Snapshot.Session.ID
		},
		BudgetError: service.components.tasks.ReActBudgetError,
		WrapError: func(err error) error {
			return service.components.errorPresentation.Wrap(err, contract.ErrorCodeReActBudget)
		},
		PrepareContext: service.components.context.PrepareExecutionContextFor,
		Stream:         service.chatStream,
		RemoveInjectedInput: func(sessionID string) error {
			return service.removeReActBudgetFinalizationInput(sessionID)
		},
	})

	recovery := applicationconversation.NewTurnRecovery(applicationconversation.TurnRecoveryDeps{
		TakeContextFailure: func(requestID string) error {
			return service.components.context.TakeContextControlFailure(requestID)
		},
		Recover:        service.components.providerRecovery.RecoverProviderFailureFor,
		Retryable:      context_runtime.RetryableAfterRecovery,
		Retry:          service.components.providerRecovery.RetryContextRecovery,
		FinalizeBudget: budgetFinalizer.Finalize,
		FinalizeTask: func(ctx context.Context, requestID, reply string) error {
			return service.components.tasks.FinalizeTask(ctx, task_context.ChatEndSummary{
				RequestID: requestID, Reply: reply,
			})
		},
	})

	turnErrors := applicationconversation.NewTurnErrorApplication(applicationconversation.TurnErrorDeps{
		TaskFor:                 service.components.tasks.TaskStateFor,
		ClassifyProviderFailure: contract.ClassifyProviderFailure,
		Interrupt:               service.components.tasks.InterruptTaskLocked,
		Fail:                    service.components.tasks.FailTaskLocked,
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
			turnErrors.Record(sessionID, requestID, err)
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
	start := applicationconversation.NewTurnStart(applicationconversation.TurnStartDeps{
		ViewMu: &service.ViewMu,
		Lifecycle: func() (bool, bool) {
			return service.closed, service.draining
		},
		CurrentSessionIDLocked: func() string {
			return service.Core.Snapshot.Session.ID
		},
		SessionUnit:        service.sessionUnitLocked,
		IsActive:           service.isActiveSessionLocked,
		IsRestoringLocked:  service.components.view.IsRestoringLocked,
		IsCompactingLocked: service.components.contextCompactionGate.IsCompactingLocked,
		Queue:              service.components.conversationQueue,
		Process:            process,
		View: turnStartViewHost{
			view:   service.components.view,
			titles: service.components.sessions,
		},
		TitleFor: func(input string) model.SessionTitle {
			return model.SessionTitle{
				Value: session_runtime.SessionTitle(input), Source: "first_request", FinalizedAt: time.Now(),
			}
		},
		SetActiveSessionName: func(name string) {
			service.Core.Snapshot.Session.Name = name
		},
		SetSessionChat:            service.components.view.SetSessionChatLockedFor,
		PublishChatState:          service.publishChatStateFor,
		SetCurrentTaskBatch:       service.Deps.Runtime.SetCurrentTaskBatch,
		PublishRuntimeProjections: service.publishRuntimeProjections,
		DiscardMailbox:            service.discardPendingSubagentContextsFor,
		PublishEvent: func(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) {
			service.publishSessionEvent(kind, revision, requestID, sessionID, payload)
		},
		Dispatch: func(ctx context.Context, sessionID, requestID string, request applicationconversation.Request) {
			go runner.Run(ctx, sessionID, requestID, request)
		},
		DeferUntilRestored:   service.deferSubmitUntilRestored,
		DeferAfterCompaction: service.components.contextCompactionGate.DeferSubmitAfterCompaction,
		Debug:                runChatDebug,
		ClosedError:          errors.New("application is shut down"),
		DrainingError:        ErrApplicationDraining,
	})
	submission := applicationconversation.NewSubmission(applicationconversation.SubmissionDeps{
		TransitionLock:   service.transitionView,
		CurrentSessionID: service.currentViewSessionID,
		MaterializeDraft: service.components.sessionDrafts.materialization.Materialize,
		Request:          service.components.conversationTurnState.RequestApplication,
		Layers:           service.promptStack.Layers,
		EffortFor:        service.effortForSession,
		BudgetFor:        prompt.ReActBudgetFor,
		Start:            start,
	})
	projection := applicationtoolhooks.NewProjection(applicationtoolhooks.ProjectionDeps{
		Lock:                 &service.ViewMu,
		SessionIDFromContext: task_context.SessionIDFromContext,
		CurrentSessionIDLocked: func() string {
			return service.Core.Snapshot.Session.ID
		},
		IsActiveLocked: service.isActiveSessionLocked,
		Flush:          service.components.conversationStreamRuntime.FlushStreamBatcherFor,
		EnsureToolTranscript: func(sessionID, name, id, arguments string) {
			service.components.tasks.EnsureToolCallTranscriptLocked(sessionID, name, id, arguments)
		},
		AttributeNarration: func(sessionID, name, id, arguments, streamed string) {
			service.components.tasks.AttributeToolNarrationLocked(sessionID,
				model.TranscriptToolCall{ID: id, Name: name, Arguments: arguments}, streamed)
		},
		StreamedAssistantText: service.components.conversationStreamRuntime.StreamedAssistantTextLocked,
		AppendToolLocked: func(sessionID, name, id, arguments string, active bool) model.Message {
			tool := &model.ToolCall{ID: id, Name: name, Arguments: arguments, Status: dto.ToolEventRunning}
			origin := model.MessageOrigin{
				RoleName: model.RoleNameMain, RoleSessionID: sessionID,
				RoundID: service.components.tasks.RoleRoundFor(sessionID),
			}
			if active {
				return *service.components.view.AppendMessageWithOriginLocked("tool", "", tool, origin)
			}
			return *service.components.view.AppendMessageWithOriginLockedFor(sessionID, "tool", "", tool, origin)
		},
		BumpRevision: service.components.view.BumpLocked,
		CurrentRequestID: func() string {
			return service.Core.Snapshot.Chat.RequestID
		},
		BackgroundRequestID: func(sessionID string) string {
			if task := service.components.tasks.CurrentTaskExecutionFor(sessionID); task != nil {
				return task.RequestID
			}
			return ""
		},
		CollectProjection: func(sessionID string) view_state.RuntimeStateProjection {
			return service.components.view.CollectRuntimeProjectionFor(context.Background(), sessionID)
		},
		ToolArguments: func(sessionID, id string) string {
			arguments := ""
			service.components.view.SessionViewReadLocked(sessionID, func(view *sessioncase.View) {
				for index := len(view.Conversation) - 1; index >= 0; index-- {
					tool := view.Conversation[index].Tool
					if tool != nil && tool.ID == id {
						arguments = tool.Arguments
						return
					}
				}
			})
			return arguments
		},
		RecordToolTranscript: func(sessionID, name, id, arguments, result string, toolErr error) string {
			providerResult, _ := service.components.tasks.RecordToolTranscriptLocked(
				sessionID, name, id, arguments, result, toolErr,
			)
			return providerResult
		},
		ObserveTool: service.components.tasks.ObserveTool,
		ToolStatus: func(name string, toolErr error) (dto.ToolEventStatus, string) {
			if toolErr == nil {
				return dto.ToolEventSuccess, ""
			}
			return dto.ToolEventError, service.components.errorPresentation.PresentToolError(name, toolErr)
		},
		BoundToolResult: service.boundToolResultForSnapshot,
		MutateTool: func(sessionID, id string, update *model.ToolCall) {
			service.components.view.SessionViewMutateLocked(sessionID, func(view *sessioncase.View) {
				for index := len(view.Conversation) - 1; index >= 0; index-- {
					tool := view.Conversation[index].Tool
					if tool != nil && tool.ID == id {
						tool.Status, tool.Result, tool.Error, tool.Duration = update.Status, update.Result, update.Error, update.Duration
						tool.ResultRef, tool.Truncated, tool.TotalChars = update.ResultRef, update.Truncated, update.TotalChars
						return
					}
				}
			})
		},
		MirrorActiveView: service.components.view.MirrorActiveViewLocked,
		RecordReadFile:   service.components.view.RecordReadFileForSessionLocked,
		AppendToolResultLocked: func(sessionID, name, id, content, errorText string, status dto.ToolEventStatus, duration time.Duration, ref string, truncated bool, totalChars int) model.Message {
			return *service.components.view.AppendMessageWithOriginLockedFor(sessionID, "tool_result", content, &model.ToolCall{
				ID: id, Name: name, Result: content, Error: errorText, Status: status,
				Duration: duration, ResultRef: ref, Truncated: truncated, TotalChars: totalChars,
			}, model.MessageOrigin{
				RoleName: model.RoleNameMain, RoleSessionID: sessionID,
				RoundID: service.components.tasks.RoleRoundFor(sessionID),
			})
		},
		AppendAssistantPlaceholder: service.components.view.AppendAssistantPlaceholderAfterToolLocked,
		ApplyProjection:            service.components.view.ApplyRuntimeProjectionForLocked,
		PublishEvent: func(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) {
			service.publishSessionEvent(kind, revision, requestID, sessionID, payload)
		},
		RecordOutcome: func(sessionID, name string, status dto.ToolEventStatus, toolErr error) {
			service.toolRecords.RecordOutcome(
				sessionID, name, applicationtoolrecord.Failed(status), applicationtoolrecord.ErrorText(toolErr),
			)
		},
		RefreshWorkTable: service.components.workTableRuntime.RefreshFromSources,
		NotifyProjectBoard: func(name string, toolErr error, sessionID string) {
			if sink := service.projectBoardSink(); sink != nil {
				sink.OnToolComplete(name, toolErr, sessionID)
			}
		},
		PublishRuntime: func(sessionID, requestID string) {
			snapshot := service.components.frontendSnapshot.Snapshot()
			service.publishSessionEvent(event.EventRuntimeChanged, snapshot.Revision, requestID, sessionID, snapshot.Runtime)
		},
		Observe: nil,
	})
	service.components.conversationTurn = &conversationTurnComponents{
		submission: submission,
		start:      start,
		budget:     budgetFinalizer,
		projection: projection,
		process:    process,
		runner:     runner,
	}
}

type turnStartViewHost struct {
	view   *view_state.Coordinator
	titles *session_runtime.Coordinator
}

func (host turnStartViewHost) SessionTitleFor(sessionID string) model.SessionTitle {
	return host.titles.SessionTitleFor(sessionID)
}

func (host turnStartViewHost) SetSessionTitleLocked(sessionID string, title model.SessionTitle) {
	host.titles.SetSessionTitleLocked(sessionID, title)
}

func (host turnStartViewHost) AppendMessageWithOriginLocked(
	role, content string,
	tool *model.ToolCall,
	origin model.MessageOrigin,
) *model.Message {
	return host.view.AppendMessageWithOriginLocked(role, content, tool, origin)
}

func (host turnStartViewHost) AppendMessageWithOriginLockedFor(
	sessionID, role, content string,
	tool *model.ToolCall,
	origin model.MessageOrigin,
) *model.Message {
	return host.view.AppendMessageWithOriginLockedFor(sessionID, role, content, tool, origin)
}

func (host turnStartViewHost) BumpLocked() uint64 {
	return host.view.BumpLocked()
}
