package core

import (
	"context"
	"errors"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	conversationsearch "github.com/RedHuang-0622/seelex/application/conversation/search"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	applicationtable "github.com/RedHuang-0622/seelex/application/execution/table"
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationgoal "github.com/RedHuang-0622/seelex/application/goal"
	applicationlifecycle "github.com/RedHuang-0622/seelex/application/lifecycle"
	applicationobservability "github.com/RedHuang-0622/seelex/application/observability"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/application/prompt"
	applicationreview "github.com/RedHuang-0622/seelex/application/review"
	switching "github.com/RedHuang-0622/seelex/application/session/switchpolicy"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

var ErrRolePermissionsUnavailable = applicationpolicy.ErrRolePermissionsUnavailable

func (service *Service) assembleExecutionApplication(reader applicationexecution.BoardReader) {
	service.components.executionApplication = applicationexecution.NewWorkItem(applicationexecution.WorkItemDeps{
		Runtime: service.Deps.Runtime, BoardView: reader.BoardViewFor,
		SessionLive: reader.SessionLiveFor, SessionPage: reader.SessionLivePageFor,
		RefreshTodo: service.refreshRuntimeAfterTodoChange,
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

func (service *Service) assembleReviewApplication() {
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
			plan := service.sessionActivePlanLocked(sessionID)
			tree := applicationtable.CloneSubAgentTreeForSync(service.Core.Snapshot.Runtime.SubAgentTree)
			service.ViewMu.RUnlock()
			return plan, service.taskRecordsForSession(sessionID, viewSessionID), tree, service.asyncRunsForTable()
		},
		WorkTableLimits: workTableBounds,
		ProjectRoot: func(sessionID string) string {
			return service.components.sessionQuery.ProjectRootFor(sessionID)
		},
		TeamPlan: func(sessionID string) *dto.TeamworkPlan {
			if service.Deps.Runtime == nil || sessionID == "" {
				return nil
			}
			source, ok := service.Deps.Runtime.(applicationexecution.TeamworkPlanSource)
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
		TurnState:        service.components.conversationTurnState,
		DrainingError:    ErrApplicationDraining,
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
		StopConsumers: service.stopLifecycleConsumers,
		CloseWorkTable: func() {
			if service.workTable != nil {
				service.workTable.Close()
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

func (service *Service) assemblePolicySwitchApplication() {
	service.components.policyApplication = applicationpolicy.NewSwitch(applicationpolicy.SwitchDeps{
		ActivateSkill: func(name string) (model.SkillInfo, error) {
			if service == nil {
				return model.SkillInfo{}, errors.New("service is not initialized")
			}
			return service.components.skillActivation.Activate(
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
			if routed, ok := service.Deps.Engine.(interface{ SetSystemPromptFor(string, string) }); ok {
				routed.SetSystemPromptFor(sessionID, prompt)
			} else {
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

func (service *Service) assembleSubagentApplication() {
	service.components.subagentApplication = applicationexecution.NewSubagent(applicationexecution.SubagentDeps{
		Runtime: service.Deps.Runtime,
		Detail:  service.components.subagent.SubagentDetail,
		Refresh: func() {
			service.components.runtimeApplication.Refresh()
		},
	})
}
