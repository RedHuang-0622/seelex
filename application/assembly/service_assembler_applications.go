package assembly

import (
	"context"
	"errors"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	conversationsearch "github.com/RedHuang-0622/seelex/application/conversation/search"
	"github.com/RedHuang-0622/seelex/application/event"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationgoal "github.com/RedHuang-0622/seelex/application/goal"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationlifecycle "github.com/RedHuang-0622/seelex/application/lifecycle"
	applicationobservability "github.com/RedHuang-0622/seelex/application/observability"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/application/prompt"
	applicationreview "github.com/RedHuang-0622/seelex/application/review"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/board"
	switching "github.com/RedHuang-0622/seelex/kernel/sessionpolicy"
	worktable "github.com/RedHuang-0622/seelex/kernel/worktable"
)

var ErrRolePermissionsUnavailable = applicationpolicy.ErrRolePermissionsUnavailable

func (service *Service) assembleExecutionApplication(reader applicationexecution.BoardReader) {
	service.components.executionApplication = applicationexecution.NewWorkItem(applicationexecution.WorkItemDeps{
		Runtime: service.Deps.Runtime, BoardView: reader.BoardViewFor,
		SessionLive: reader.SessionLiveFor, SessionPage: reader.SessionLivePageFor,
		RefreshTodo: func() {
			if service.components.workTableRuntime != nil {
				service.components.workTableRuntime.RefreshAfterTodoChange()
			}
		},
	})
}

func (service *Service) assembleRoleActivityApplication() {
	service.components.roleActivity = applicationexecution.NewRoleActivity(applicationexecution.RoleActivityDeps{
		RequestID: func() string {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.Core.Snapshot.Chat.RequestID
		},
		EvidenceChars: func() int { return applicationpolicy.Get().EvidenceChars },
		Truncate:      worktable.TruncateWorkEvidence,
		Stage: func(ownerSessionID, unitID, name, status, errorText string) {
			service.toolRecords.StageStatus(ownerSessionID, unitID, name, status, errorText)
		},
		Publish: func(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) {
			service.publishSessionEvent(kind, revision, requestID, sessionID, payload)
		},
		NoteDropped: func(reason string) {
			service.toolRecords.NoteDropped(reason)
		},
		StartedKind:   EventTeammateToolStarted,
		CompletedKind: EventTeammateToolCompleted,
	})
}

func (service *Service) assembleRoleApplication() {
	service.components.roleApplication = applicationpolicy.NewRoleApplication(applicationpolicy.Deps{
		RoleDefaults: func() applicationpolicy.RoleDefaultsPort {
			if service == nil || service.Deps.Sessions == nil {
				return nil
			}
			port, _ := service.Deps.Sessions.(applicationpolicy.RoleDefaultsPort)
			return port
		},
		Runtime: func() contract.AccountRoleSelectionPort {
			if service == nil || service.Deps.Runtime == nil {
				return nil
			}
			port, _ := service.Deps.Runtime.(contract.AccountRoleSelectionPort)
			return port
		},
		Registry: func() applicationpolicy.PermissionRegistryPort {
			if service == nil || service.Deps.Sessions == nil {
				return nil
			}
			port, _ := service.Deps.Sessions.(applicationpolicy.PermissionRegistryPort)
			return port
		},
		Layers: func() applicationpolicy.PermissionLayerPort {
			if service == nil || service.Deps.Sessions == nil {
				return nil
			}
			port, _ := service.Deps.Sessions.(applicationpolicy.PermissionLayerPort)
			return port
		},
	})
}

func (service *Service) assembleReviewApplication(newProcessor applicationreview.ProcessorFactory) {
	service.components.reviewApplication = applicationreview.New(applicationreview.Deps{
		WorkspaceFile: func() (contract.WorkspaceFilePort, string, error) {
			return service.components.workspace.FilePort()
		},
		SessionIDFromContext: task_context.SessionIDFromContext,
		ViewSessionID:        service.currentViewSessionID,
		SetReviewRequest: func(sessionID string, request *dto.ReviewRequest) uint64 {
			service.ViewMu.Lock()
			defer service.ViewMu.Unlock()
			service.components.view.SetReviewRequestForLocked(sessionID, request)
			return service.components.view.BumpLocked()
		},
		ClearReviewRequest: func(sessionID string) {
			service.ViewMu.Lock()
			defer service.ViewMu.Unlock()
			if service.components.view.ReviewRequestForLocked(sessionID) != nil {
				service.components.view.ClearReviewRequestForLocked(sessionID)
				service.components.view.BumpLocked()
			}
		},
		PublishSnapshotChanged: func(sessionID string, revision uint64) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		NewProcessor: newProcessor,
	})
}

func (service *Service) assembleHistorySearchApplication() {
	service.components.historySearchApplication = conversationsearch.New(conversationsearch.Deps{
		Available: func() bool {
			return service != nil && service.Deps.Runtime != nil
		},
		Search: func(ctx context.Context, query string, limit int) (dto.SearchResult, error) {
			return service.Deps.Runtime.SearchHistory(ctx, query, limit)
		},
	})
}

func (service *Service) assembleProjectBoardQuery() {
	service.components.projectBoardQuery = applicationexecution.NewProjectBoard(applicationexecution.ProjectBoardDeps{
		WorkTableInputs: func(sessionID string) (*model.PlanState, []dto.TaskRecord, []dto.SubAgentTreeNode, []dto.AsyncRunRecord) {
			service.ViewMu.RLock()
			viewSessionID := service.Core.Snapshot.Session.ID
			plan := service.components.workTableRuntime.SessionActivePlanLocked(sessionID)
			tree := worktable.CloneSubAgentTreeForSync(service.Core.Snapshot.Runtime.SubAgentTree)
			service.ViewMu.RUnlock()
			var records []dto.TaskRecord
			if service.Deps.Runtime != nil {
				records = service.Deps.Runtime.TaskSnapshotFor(view_state.TaskScope(sessionID, viewSessionID))
			}
			return plan, records, tree, service.components.workTableRuntime.AsyncRuns()
		},
		WorkTableLimits: workTableBounds,
		ProjectRoot: func(sessionID string) string {
			return service.components.sessionQuery.ProjectRootFor(sessionID)
		},
		TeamPlan: func(sessionID string) *dto.TeamworkPlan {
			if service.Deps.Runtime == nil || sessionID == "" {
				return nil
			}
			source, ok := service.Deps.Runtime.(board.TeamworkPlanSource)
			if !ok {
				return nil
			}
			plan, ok := source.TeamworkPlanFor(sessionID)
			if !ok {
				return nil
			}
			return &plan
		},
	})
}

func (service *Service) assemblePerfStatsApplication() {
	service.components.perfStatsApplication = applicationobservability.NewPerfApplication(applicationobservability.PerfDeps{
		LockView:   service.ViewMu.RLock,
		UnlockView: service.ViewMu.RUnlock,
		Snapshot: func() model.Snapshot {
			return service.Core.Snapshot
		},
		HistoryWindow: func() int {
			return applicationpolicy.Get().HistoryWindow
		},
		ToolResultRefs: func() []model.ToolResultRef {
			return service.components.tasks.ToolResultRefs()
		},
	})
}

func (service *Service) assembleConversationQueueApplication() {
	service.components.conversationQueue = applicationconversation.NewQueue(applicationconversation.QueueDeps{
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
		IsActive: service.isActiveSessionLocked,
		MirrorChatState: func(sessionID string, chat model.ChatState) {
			service.components.view.SetSessionChatLockedFor(sessionID, chat)
		},
		BumpRevision: func() uint64 {
			return service.components.view.BumpLocked()
		},
		PublishSnapshot: func(revision uint64, sessionID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		PublishChatState: service.publishChatStateFor,
		DurableEnqueue: func(sessionID, input string) error {
			return service.components.sessions.QueueEnqueueInput(service.components.sessions.LocateSession(sessionID), sessionID, input)
		},
		Debug:      runChatDebug,
		NewRequest: service.components.conversationTurnState.NewRequest,
		SessionUnit: func(sessionID string) applicationconversation.QueueUnit {
			return service.sessionUnitLocked(sessionID)
		},
		TurnState:     service.components.conversationTurnState,
		Requests:      service.components.conversationTurnState,
		DrainingError: ErrApplicationDraining,
	})
}

func (service *Service) assembleConversationSessionApplication(sessionRecords applicationconversation.SessionRecordsPort) {
	var agents contract.AgentConversationPort
	if service.Deps.Runtime != nil {
		agents, _ = service.Deps.Runtime.(contract.AgentConversationPort)
	}
	service.components.conversationSession = applicationconversation.NewUnified(applicationconversation.UnifiedDeps{
		SnapshotOf: service.components.sessionQuery.SnapshotOf,
		Records:    sessionRecords,
		Agents:     agents,
		SubmitToSession: func(ctx context.Context, sessionID, text string) error {
			return service.components.submitApplication.SubmitToSession(ctx, sessionID, text)
		},
		SnapshotUnavailable: ErrSessionSnapshotUnavailable,
	})
}

func (service *Service) assembleSubmissionApplication() {
	service.components.submission = applicationinput.NewSubmission(applicationinput.SubmissionDeps{
		Lifecycle: func() (bool, bool) {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.closed, service.draining
		},
		ShutdownError: errors.New("application is shut down"),
		DrainingError: ErrApplicationDraining,
		Dispatch:      service.components.input.Dispatch,
	})
}

func (service *Service) assembleInteractionApplication() {
	resolve := applicationsession.NewInteraction(applicationsession.InteractionDeps{
		Current: func() *model.Interaction {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.Core.Snapshot.Interaction
		},
		PendingApprovalSession: func(id string) (string, bool) {
			if service.Approval == nil {
				return "", false
			}
			return service.pendingApprovalSession(id)
		},
		ResolveApproval: func(id, optionID string) error {
			return service.Approval.Resolve(id, contract.ApprovalDecision{OptionID: optionID})
		},
		CloseInteraction: service.closeInteraction,
		ResumeSession:    service.components.sessionResume.Resume,
		AddNotice:        service.components.view.AddNotice,
		SelectAccount:    service.components.adminApplication.SelectAccount,
	})
	observe := applicationsession.NewInteractionObserver(applicationsession.InteractionObserverDeps{
		Lock:          &service.ViewMu,
		Snapshot:      &service.Core.Snapshot,
		ViewSessionID: service.currentViewSessionID,
		Unit: func(sessionID string) applicationsession.ApprovalUnit {
			return service.sessions.Unit(sessionID)
		},
		PendingBySession: func(sessionID string) []model.Interaction {
			if service.Approval == nil {
				return nil
			}
			return service.Approval.PendingBySession(sessionID)
		},
		BumpRevision: func() uint64 {
			return service.components.view.BumpLocked()
		},
		PublishEvent: func(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) {
			service.publishSessionEvent(kind, revision, requestID, sessionID, payload)
		},
		PublishSessionStateChanged: service.publishSessionStateChanged,
	})
	service.components.interactions = &sessionInteractionComponents{
		resolve: resolve,
		observe: observe,
	}
}

func (service *Service) assembleForkApplication() {
	service.components.forkApplication = applicationsession.NewFork(applicationsession.ForkDeps{
		TransitionForKey: service.transitionForKey,
		ForkLocked: func(parentID string, request model.ForkRequest) (string, error) {
			return service.components.sessionForkRuntime.ForkSessionLocked(parentID, request)
		},
		LatestForkCut: func(parentID string) (uint64, error) {
			location := service.components.sessions.LocateSession(parentID)
			return service.components.sessions.LatestForkCut(location, parentID)
		},
		Resume: service.components.sessionResume.Resume,
	})
}

func (service *Service) assembleSubmitApplication() {
	service.components.submitApplication = applicationsession.NewSubmit(applicationsession.SubmitDeps{
		Materialize: service.components.sessionDrafts.submission.Materialize,
		Loaded: func(sessionID string) (bool, bool) {
			service.ViewMu.RLock()
			loaded := service.sessionLoaded(sessionID)
			service.ViewMu.RUnlock()
			return loaded, service.components.residencyRuntime.ContentUnloaded(sessionID)
		},
		Resume: service.components.sessionResume.Resume,
		// 闭包而不是方法值：装配顺序里 conversationTurn 尚未就位，方法值会在
		// 这里立刻解引用，闭包把它推迟到提交那一刻。
		Submit: func(ctx context.Context, sessionID, text string, images []dto.ComposerImage) error {
			return service.components.conversationTurn.submission.SubmitForWithImages(ctx, sessionID, text, images)
		},
	})
}

func (service *Service) assembleTerminalHandlerApplication() {
	service.components.terminalHandler = task_context.NewTerminalHandler(task_context.TerminalHandlerDeps{
		EnsurePlanProjection: service.ensureViewPlanProjection,
		VerifyAndApply:       service.components.tasks.VerifyAndApply,
		MirrorTask:           service.mirrorActiveTaskAfterTerminal,
		MirrorPlan:           service.mirrorActivePlanAfterTerminal,
	})
}

func (service *Service) assembleFaultGuardApplication() {
	service.components.faultGuardApplication = applicationlifecycle.NewFaultGuard(applicationlifecycle.FaultGuardDeps{
		EnterDegraded: func() bool {
			service.ViewMu.Lock()
			defer service.ViewMu.Unlock()
			service.closed = true
			service.draining = true
			for _, sid := range service.sessions.UnitIDs() {
				if unit := service.sessions.Unit(sid); unit != nil {
					if cancel := unit.CancelFunc(); cancel != nil {
						cancel()
					}
				}
			}
			return true
		},
		StopCatalog: func() {
			service.components.sessions.StopCatalogRefresh()
		},
		CloseSessions: service.sessions.Close,
		StopConsumers: func() {
			if service.components.workTableRuntime != nil {
				service.components.workTableRuntime.StopLifecycleConsumers()
			}
		},
		CloseWorkTable: func() {
			if service.components.workTableRuntime != nil {
				service.components.workTableRuntime.Close()
			}
		},
		ShutdownApproval: service.Approval.Shutdown,
		PublishExit: func() {
			service.Events.Publish(EventExitRequested, 0, "", nil)
		},
	})
}

func (service *Service) assembleGoalApplication(hostSessionForToolCall func(context.Context) string) {
	service.components.goalApplication = applicationgoal.New(applicationgoal.Deps{
		Coordinator: func() applicationgoal.CoordinatorPort {
			if service == nil {
				return nil
			}
			return service.components.goal
		},
		Refresh:        service.refreshGoalRuntimeProjection,
		WriteSessionID: task_context.SessionIDFromContext,
		ReadSessionID: func(ctx context.Context) string {
			if sessionID := task_context.SessionIDFromContext(ctx); sessionID != "" {
				return sessionID
			}
			return hostSessionForToolCall(ctx)
		},
	})
}

func (service *Service) assemblePolicySwitchApplication(skillActivation applicationpolicy.SkillActivationPort) {
	service.components.policyApplication = applicationpolicy.NewSwitch(applicationpolicy.SwitchDeps{
		ActivateSkill: func(name string) (model.SkillInfo, error) {
			if service == nil {
				return model.SkillInfo{}, errors.New("service is not initialized")
			}
			return skillActivation.Activate(
				name,
				service.Deps.Skills,
				service.applySkill,
			)
		},
		SetReasoningEffort: service.knobs.SetReasoningEffort,
		SetLoopLimit:       service.knobs.SetLoopLimit,
		SetReleaseWindow: func(window int) (int, error) {
			return switching.SetReleaseWindow(service.releaseWindowDeps(), window)
		},
		ReleaseWindow: func() int {
			return switching.ReleaseWindow(service.releaseWindowDeps())
		},
		SetPermissionTier: func(tier string) (string, error) {
			return service.components.policyApplication.SetPermissionTier(tier)
		},
		CurrentViewSessionID: service.currentViewSessionID,
		PersistTier:          service.persistPermissionTier,
		SetUnitTier: func(sessionID, tier string) {
			if unit := service.sessions.Unit(sessionID); unit != nil {
				unit.SetPermissionTier(tier)
			}
		},
		SetApprovalAuto: func(sessionID string, full bool) {
			if service.Approval != nil {
				service.Approval.SetPermissionAutoApprovalFor(sessionID, full)
			}
		},
		SetRuntimeTier: func(sessionID, tier string) error {
			if service.Deps.Runtime == nil {
				return nil
			}
			return service.Deps.Runtime.SetPermissionTierFor(sessionID, tier)
		},
		EffectiveTier: service.permissionTierForSession,
		ResolveApprovals: func(sessionID string) {
			if service.Approval != nil {
				service.Approval.ResolveAllFor(sessionID, contract.ApprovalDecision{OptionID: "allow"})
			}
		},
		PublishTier: func(sessionID, effective string) {
			service.ViewMu.Lock()
			service.Core.Snapshot.Runtime.PermissionTier = effective
			service.Core.Snapshot.Runtime.FullAccess = dto.PermissionTierIsFullAccess(effective)
			revision := service.components.view.BumpLocked()
			runtime := model.CloneRuntimeState(service.Core.Snapshot.Runtime)
			service.ViewMu.Unlock()
			service.publishSessionEvent(EventRuntimeChanged, revision, "", sessionID, runtime)
		},
		EnsureViewIdle: service.knobs.EnsureViewIdle,
		CycleEffort: func() (string, error) {
			return service.effortManager.Cycle()
		},
		ApplyEffort: func(level string) error {
			return service.effortManager.Apply(level)
		},
		SubagentPolicy: func() dto.SubagentPolicy {
			return service.effortManager.SubagentPolicy()
		},
		SetSubagentPolicy: func(policy dto.SubagentPolicy) {
			service.Deps.Runtime.SetSubagentPolicy(policy)
		},
		SyncReasoningEffort: service.syncSessionReasoningEffort,
		CurrentEffort: func() string {
			return service.effortManager.Current()
		},
		RenderPrompt: func() string { return service.promptStack.Render() },
		SetSystemPrompt: func(sessionID, prompt string) {
			if !contract.SetSystemPromptFor(service.Deps.Engine, sessionID, prompt) {
				service.Deps.Engine.SetSystemPrompt(prompt)
			}
		},
		CommitEffort: func(sessionID, effort, _ string, _ int) {
			service.ViewMu.Lock()
			if unit := service.sessions.Unit(sessionID); unit != nil {
				unit.SetEffortLevel(effort)
			}
			service.Core.Snapshot.Runtime.Effort = effort
			service.knobs.SeedReadings(prompt.ReasoningEffortFor(effort), prompt.MaxLoops(effort))
			reasoning := service.knobs.ReasoningEfforts()
			loop := service.knobs.LoopLimit()
			service.Core.Snapshot.Runtime.ReasoningEffort = reasoning
			service.Core.Snapshot.Runtime.LoopLimit = loop
			revision := service.components.view.BumpLocked()
			runtime := model.CloneRuntimeState(service.Core.Snapshot.Runtime)
			service.ViewMu.Unlock()
			service.publishSessionEvent(EventRuntimeChanged, revision, "", service.currentViewSessionID(), runtime)
		},
		AnyChatRunning: func() bool {
			service.ViewMu.RLock()
			running := service.anyChatRunningLocked()
			service.ViewMu.RUnlock()
			return running
		},
		ActivatePlugin:   service.Deps.Plugins.Activate,
		DeactivatePlugin: service.Deps.Plugins.Deactivate,
		ClearHistory:     service.Deps.Engine.ClearHistory,
		ResetPrompt:      service.promptStack.Reset,
		CurrentPlugin:    service.Deps.Plugins.Current,
		ReapplyEffort:    service.reapplyEffortAfterPluginSwitch,
		SetPluginSystemPrompt: func(prompt string) {
			service.Deps.Engine.SetSystemPrompt(prompt)
		},
		ResetConversation: func(message string) {
			service.components.view.ResetConversation(message)
		},
		CollectProjection: func(ctx context.Context) view_state.RuntimeStateProjection {
			return service.components.view.CollectRuntimeProjection(ctx)
		},
		CommitProjection: func(_ context.Context, projection view_state.RuntimeStateProjection) {
			service.ViewMu.Lock()
			service.components.view.ApplyRuntimeProjectionLocked(projection)
			revision := service.components.view.BumpLocked()
			runtime := model.CloneRuntimeState(service.Core.Snapshot.Runtime)
			service.ViewMu.Unlock()
			service.publishSessionEvent(EventRuntimeChanged, revision, "", service.currentViewSessionID(), runtime)
			service.publishRuntimeProjections()
		},
	})
}

func (service *Service) assembleFrontendSnapshotApplication() {
	service.components.frontendSnapshot = view_state.NewSnapshotApplication(service.components.view, view_state.SnapshotDeps{
		Status: func(sessionID string) model.SessionStatus {
			return service.components.view.SessionStatusLocked(sessionID)
		},
		DraftRow: func() *model.SessionInfo {
			if service.draft == nil || service.draft.ID == "" {
				return nil
			}
			slot := *service.draft
			return &model.SessionInfo{
				ID:        slot.ID,
				Name:      draftSessionName,
				UpdatedAt: slot.UpdatedAt,
				Status:    SessionStatusDraft,
			}
		},
		Annotate:            service.components.editFork.annotation.Annotate,
		LatestTranscriptSeq: service.components.tasks.LatestTranscriptSeqFor,
		ProjectDraftWorkspace: func(snapshot *model.Snapshot) {
			service.components.view.ProjectDraftWorkspaceLocked(snapshot)
		},
		WithLiveRows: func(rows []model.SessionInfo) []model.SessionInfo {
			return service.components.view.WithLiveSessionRowsLocked(rows)
		},
		EnrichRows: func(rows []model.SessionInfo) []model.SessionInfo {
			return service.components.view.EnrichDirectoryRowsLocked(rows)
		},
	})
	service.components.frontendSnapshot.BindToolResultBudget(view_state.NewToolResultBudget(view_state.ToolResultBudgetDeps{
		Limit: snapshotToolOutputLimit,
		Preview: func(content string, limit int) (string, bool) {
			return service.components.residency.Preview(content, limit)
		},
		Archive: func(name, content string) string {
			return service.components.tasks.StoreToolResultLocked(name, content).Ref
		},
	}))
}

func (service *Service) assembleSubagentApplication() {
	service.components.subagentApplication = applicationexecution.NewSubagent(applicationexecution.SubagentDeps{
		Runtime: service.Deps.Runtime,
		Detail:  service.components.subagent.SubagentDetail,
		Refresh: func() {
			service.components.runtimeApplication.Refresh()
		},
	})
}
