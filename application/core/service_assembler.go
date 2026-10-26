package core

import (
	"context"
	"fmt"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	context_runtime "github.com/RedHuang-0622/seelex/application/conversation/context"
	"github.com/RedHuang-0622/seelex/application/event"
	subagentview "github.com/RedHuang-0622/seelex/application/execution/subagentview"
	applicationtable "github.com/RedHuang-0622/seelex/application/execution/table"
	"github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	applicationgoal "github.com/RedHuang-0622/seelex/application/goal"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/application/prompt"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	applicationsessionfork "github.com/RedHuang-0622/seelex/application/session/fork"
	applicationsessionhistory "github.com/RedHuang-0622/seelex/application/session/history"
	applicationsessionresidency "github.com/RedHuang-0622/seelex/application/session/residency"
	"github.com/RedHuang-0622/seelex/application/session/runtime"
	session "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/application/session/viewstate"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/state"
	worktable "github.com/RedHuang-0622/seelex/kernel/worktable"
	"time"
)

// serviceAssembler is the composition root for the application service. It
// supplies infrastructure defaults before wiring stateful collaborators.
type serviceAssembler struct {
	deps  contract.Dependencies
	ports ExternalPorts
}

// taskPromptPort 是 task_context.PromptPort 的根包适配（prompt 域尚未下沉
// 时的装配适配层）。
type taskPromptPort struct {
	promptStack   *prompt.PromptStack
	effortManager *prompt.EffortManager
}

func (p taskPromptPort) CurrentEffort() string {
	return p.effortManager.Current()
}

func (p taskPromptPort) ClearSkillLayers() {
	p.promptStack.ClearKind("skill")
}

func (p taskPromptPort) PushSkillLayer(kind, name, text string) {
	p.promptStack.Push(kind, name, text)
}

func (assembler serviceAssembler) assemble() (*Service, error) {
	if err := validateDependencies(assembler.deps); err != nil {
		return nil, err
	}
	if assembler.ports.HistoryWindow == nil {
		return nil, fmt.Errorf("application: history window assembler is required")
	}
	if assembler.ports.Residency == nil {
		return nil, fmt.Errorf("application: residency port is required")
	}
	if assembler.ports.ConversationStream == nil {
		return nil, fmt.Errorf("application: conversation stream port is required")
	}
	if assembler.ports.AccountWindow == nil {
		return nil, fmt.Errorf("application: account window policy is required")
	}
	if assembler.ports.SkillActivation == nil {
		return nil, fmt.Errorf("application: skill activation port is required")
	}
	if assembler.ports.SessionRecords == nil {
		return nil, fmt.Errorf("application: session records port is required")
	}
	if assembler.ports.ErrorPresentation == nil {
		return nil, fmt.Errorf("application: error presentation port is required")
	}
	if assembler.ports.ApprovalFactory == nil {
		return nil, fmt.Errorf("application: approval broker factory is required")
	}
	if assembler.ports.CommandRegistry == nil {
		return nil, fmt.Errorf("application: command registry factory is required")
	}
	if assembler.ports.InputRouter == nil {
		return nil, fmt.Errorf("application: input router factory is required")
	}
	if assembler.ports.RoleSessions == nil {
		return nil, fmt.Errorf("application: role sessions factory is required")
	}
	if assembler.ports.TeamworkBoardReader == nil {
		return nil, fmt.Errorf("application: teamwork board reader factory is required")
	}
	if assembler.ports.HostSessionForToolCall == nil {
		return nil, fmt.Errorf("application: host session reader is required")
	}
	if assembler.ports.ReviewProcessor == nil {
		return nil, fmt.Errorf("application: review processor factory is required")
	}
	if err := prompt.ValidateAssets(); err != nil {
		return nil, fmt.Errorf("application prompts: %w", err)
	}
	assembler.applyInfrastructureDefaults()

	promptStack := prompt.NewPromptStack()
	kernel := state.New(assembler.deps)
	sessionDomain := session.NewDomain()
	svcState := &serviceState{
		Core:               kernel,
		commands:           assembler.ports.CommandRegistry(),
		promptRuntimeState: promptRuntimeState{promptStack: promptStack},
		sessions:           sessionDomain,
	}
	service := &Service{serviceState: svcState}
	service.components.contextCompactionGate = context_runtime.NewCompactionGate(context_runtime.CompactionGateDeps{
		Core: kernel,
		DeferredSubmit: func(ctx context.Context, sessionID, text string) error {
			return service.submitConversationFor(ctx, sessionID, text)
		},
	})
	service.assembleExecutionApplication(assembler.ports.TeamworkBoardReader(service.Deps.Runtime))
	service.assembleAdminApplication()
	service.assembleSessionSSHApplication()
	service.assembleRoleApplication()
	service.assembleSchedulerApplications()
	service.assembleRuntimeApplication()
	service.assembleReviewApplication(assembler.ports.ReviewProcessor)
	service.assembleLifecycleApplication()
	service.assembleHistorySearchApplication()
	service.assembleCompletionApplication()
	service.assembleReferenceApplication()
	service.assembleSessionMetaApplication()
	service.assembleProjectBoardQuery()
	service.assembleSessionReleaseApplication()
	service.assembleContextCompactionApplication()
	service.assembleFaultGuardApplication()
	service.assemblePerfStatsApplication()
	service.assembleAsyncCompletionApplication()
	service.assembleSessionReadApplications(assembler.ports.HistoryWindow)
	service.components.historyWindowAssembler = assembler.ports.HistoryWindow
	service.components.residency = assembler.ports.Residency
	service.components.conversationStreams = assembler.ports.ConversationStream
	service.components.accountWindowPolicy = assembler.ports.AccountWindow
	service.components.skillActivation = assembler.ports.SkillActivation
	service.components.sessionRecords = assembler.ports.SessionRecords
	service.assembleConversationSessionApplication()
	service.components.errorPresentation = assembler.ports.ErrorPresentation
	service.components.roleSessions = assembler.ports.RoleSessions(assembler.deps.Sessions)
	service.components.conversationTurnState = newConversationTurnState()
	service.assembleConversationQueueApplication()
	service.assembleSessionUseCaseApplications()
	service.assembleInteractionApplication()
	service.assembleForkApplication()
	service.assembleSessionDraftComponents()
	service.assembleSubmitApplication()
	service.assembleSessionColdComponents()
	service.assembleEditForkComponents()
	// 两条独立拉条（思考强度 / 循环次数）的状态与判据归位 kernel/limit.Knobs（M5-R-limitb），
	// 宿主就是本 Service：视图锁、快照写面、引擎与档位读数经适配注入。
	service.assembleKnobs()
	service.assemblePolicySwitchApplication()
	// 项目看板订阅口槽位在装配期建好（M3）：写路径"判 nil 才是漏锁的老毛病"，
	// 槽位自己持有锁，注入方（wiring/bootstrap / 测试）随时可 SetProjectBoardSink。
	service.components.projectBoard = &projectBoardPort{}
	// Tool-record state and its optional ports are one extracted application
	// object. Host callbacks keep draft/view policy inside core assembly.
	service.toolRecords = applicationtoolrecord.New(applicationtoolrecord.Deps{
		SaveEnabled: func(enabled bool) error {
			if service.Deps.ToolRecordStore == nil {
				return nil
			}
			return service.Deps.ToolRecordStore.SaveToolRecordEnabled(enabled)
		},
		WritableSession: func(sessionID string) bool {
			if service == nil || service.Core == nil {
				return true
			}
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return !service.draftTargetLocked().Matches(sessionID)
		},
		ViewSessionID: service.currentViewSessionID,
	})
	service.assembleRoleActivityApplication()
	service.effortManager = prompt.NewEffortManager(promptStack, service.Deps.Engine)
	// 两条独立拉条的进程级读数在装配期播种成默认档（high）派生值：否则首个快照里
	// reasoning_effort / loop_limit 是空/0，前端两条拉条会显示成 Low / 1，与引擎
	// 实际生效的 high（384）不一致。
	service.knobs.SeedReadings(
		prompt.ReasoningEffortFor(service.effortManager.Current()),
		prompt.MaxLoops(service.effortManager.Current()),
	)
	// 进程级 fullAccess 默认在装配期捕获一次（配置/初始引擎门值），会话
	// 未选择时回退该默认，而不是读取其它会话遗留的引擎门状态（G4）。
	if service.Deps.Runtime != nil {
		service.fullAccessDefault = service.Deps.Runtime.FullAccess()
		// 进程级权限档位默认同样在装配期捕获一次（配置/初始引擎门值），会话
		// 未选择时回退该默认（G4）。
		service.permissionTierDefault = service.Deps.Runtime.PermissionTier()
	}
	service.components.tasks = task_context.NewCoordinator(task_context.Deps{
		Core: kernel,
		Prompt: taskPromptPort{
			promptStack: promptStack, effortManager: service.effortManager,
		},
		Limits: applicationpolicy.Get,
		IsInternalContent: func(content string) bool {
			return context_runtime.IsTaskContextCheckpoint(content) || context_runtime.IsProviderOnlyHistoryContent(content) || context_runtime.IsActiveSkillContent(content)
		},
		IsOversizedToolResult:      context_runtime.IsOversizedToolResult,
		OversizedToolResultWarning: context_runtime.OversizedToolResultWarning,
		PresentToolError:           service.components.errorPresentation.PresentToolError,
		QueuedInputRefs: func(sessionID string) []string {
			// F-3b：按会话取排队输入引用（不读视图 Snapshot；unit 自锁）。
			unit := service.sessions.Unit(sessionID)
			if unit == nil {
				return nil
			}
			return queuedInputRefs(service.components.conversationTurnState.QueuedRequests(unit.PendingRequests()))
		},
		CurrentSessionID: func() string {
			return service.sessions.ActiveID()
		},
		AsyncPending: func(sessionID string) int {
			if service == nil || service.Deps.Runtime == nil {
				return 0
			}
			return service.Deps.Runtime.AsyncPendingFor(sessionID)
		},
	})
	service.assembleTerminalHandlerApplication()
	// The composition root supplies the optional Goal store and board-archive
	// adapters. Absence keeps the in-memory fallback.
	service.components.goal = assembler.ports.Goal
	if service.components.goal == nil {
		service.components.goal = applicationgoal.NewCoordinator(applicationgoal.CoordinatorDeps{})
	}
	service.assembleGoalApplication(assembler.ports.HostSessionForToolCall)
	service.components.prompts = prompt.NewCoordinator(prompt.CoordinatorDeps{
		Core:          kernel,
		PromptStack:   promptStack,
		EffortManager: service.effortManager,
		Tasks:         service.components.tasks,
		// 技能目录的激活纪律句要引用应用侧 Skill 前缀：从 sigil 表注入，
		// 避免提示文案与前缀契约漂移（见 prompt_layer.Deps.SkillSigil）。
		SkillSigil: applicationinput.SigilSkill,
		// 环境段来自组合根（执行面的实际事实：这台机器上 bash 工具落到哪个壳）。
		Environment: assembler.deps.EnvironmentSection,
	})
	service.components.history = context_runtime.NewEngineHistoryCoordinator(kernel)
	service.components.sessions = session_runtime.NewCoordinator(session_runtime.Deps{
		Core:  kernel,
		Tasks: service.components.tasks,
		Closed: func() bool {
			return service.closed
		},
		TranscriptTailBudget: RetainedReadTailBudget,
		IsInternalContent: func(content string) bool {
			return context_runtime.IsTaskContextCheckpoint(content) || context_runtime.IsProviderOnlyHistoryContent(content) || context_runtime.IsActiveSkillContent(content)
		},
		TailHistory:                task_context.TranscriptTailHistory,
		OversizedToolResultWarning: context_runtime.OversizedToolResultWarning,
		ContentReferenceWarning:    context_runtime.ContentReferenceWarning,
		Limits:                     applicationpolicy.Get,
		DisplayUserInput:           displayUserInput,
	})
	service.components.view = view_state.NewCoordinator(view_state.Deps{
		Core:                   kernel,
		Units:                  sessionDomain,
		CurrentEffort:          service.effortForSession,
		CurrentReasoningEffort: service.knobs.ReasoningEfforts,
		CurrentLoopLimit:       service.knobs.LoopLimit,
		CurrentFullAccess:      service.fullAccessForSession,
		CurrentPermissionTier:  service.permissionTierForSession,
		CurrentSessionID: func() string {
			return service.sessions.ActiveID()
		},
		Tasks: service.components.tasks,
		DraftView: func() (string, string) {
			if service.draft == nil {
				return "", ""
			}
			workspaceID := ""
			if service.draft.Workspace != nil {
				workspaceID = service.draft.Workspace.ID
			}
			return service.draft.ID, workspaceID
		},
		TitleForSession: func(sessionID string) string {
			return service.components.sessions.CatalogSessionTitle(sessionID).Value
		},
		Goals: goalGovernanceViewPort{service: service},
		// 团队看板读面（计划 + 作业行 + 审计流水）：与 Goals 同构——Deps 收窄成
		// 「会话 → 只读投影」，转发与缓存的责任留给桥（未装配 teamwork 时
		// TeamworkBoardViewFor 走类型断言失败分支返回 nil）。
		Teamwork: teamworkBoardViewPort{service: service},
		RefreshWorkTableLocked: func(tasks []dto.TaskRecord, asyncRuns []dto.AsyncRunRecord) {
			if runtime := service.components.workTableRuntime; runtime != nil {
				runtime.RefreshLocked(tasks, asyncRuns)
			}
		},
		Limits: func() dto.ViewLimits { return applicationpolicy.Get().ViewLimits() },
	})
	service.components.sessions.BindView(service.components.view)
	service.components.conversationStreamRuntime = applicationconversation.NewStreamRuntime(applicationconversation.StreamRuntimeDeps{
		Core:                kernel,
		Units:               sessionDomain,
		View:                service.components.view,
		Streams:             service.components.conversationStreams,
		SessionIDForRequest: service.components.tasks.SessionIDForRequest,
		RoleRoundFor:        service.components.tasks.RoleRoundFor,
		EngineHistory:       service.engineHistoryFor,
		TakeServedRoute: func(sessionID string) (dto.ServedRoute, bool) {
			if service.Deps.Runtime == nil {
				return dto.ServedRoute{}, false
			}
			return service.Deps.Runtime.TakeServedRoute(sessionID)
		},
		StampServedRoute: func(sessionID, requestID, role, model, provider string) {
			_ = service.components.tasks.StampAssistantRouteFor(sessionID, requestID, role, model, provider)
		},
		PublishSessionEvent: service.publishSessionEvent,
	})
	service.components.residencyRuntime = applicationsessionresidency.NewRuntime(applicationsessionresidency.RuntimeDeps{
		Core:                 kernel,
		Units:                sessionDomain,
		View:                 service.components.view,
		Port:                 service.components.residency,
		ResidentSessionLimit: func() int { return applicationpolicy.Get().ResidentSessionLimit },
		LoadedContentLimit:   func() int { return applicationpolicy.Get().LoadedContentLimit },
		ReleaseWindow:        func() int { return applicationpolicy.Get().ReleaseWindow() },
		FlushSession: func(sessionID string) error {
			return service.components.sessions.PersistCurrentSession(service.components.sessions.LocateSession(sessionID), sessionID)
		},
		ReleaseResident: func(sessionID string) error {
			return service.components.sessionReleaseApplication.Release(sessionID, applicationsession.ReleaseIdle)
		},
		RequestCatalogRefresh: func() { _ = service.components.sessions.RequestCatalogRefresh() },
		ReloadContent:         service.reloadSessionContent,
	})
	service.assembleFrontendSnapshotApplication()
	// TurnProcess captures the view coordinator method value, so it must be
	// assembled after the coordinator exists. This is composition order, not a
	// forwarding wrapper.
	service.assembleConversationTurnCoreComponents()
	// 工作区用例（plan §2.4）：创建/绑定/解绑与当前工作区读面归位
	// application/workspace/switch；视图投影写面仍由本 Service 持有，经窄 Host 注入。
	service.components.workspace = switchworkspace.New(switchworkspace.Deps{
		Store:    service.Deps.Workspace,
		Sessions: service.Deps.Sessions,
		Runtime:  service.Deps.Runtime,
		Engine:   service.Deps.Engine,
		View:     workspaceHost{service: service},
		Prompt:   func() string { return service.promptStack.Render() },
		PerSessionExecution: func() bool {
			return service.perSessionExecution()
		},
		NewSessionID: func(prefix string) string {
			return service.newGeneratedSessionID(prefix)
		},
		AttachSessionContext: func(workspaceID, sessionID string) error {
			return service.components.sessionHistoryRuntime.AttachSessionContextFor(workspaceID, sessionID)
		},
		RefreshWorkTable: func() {
			service.components.workTableRuntime.RefreshFromSources()
		},
		WorkspaceIDForSession: func(sessionID string) string {
			return service.components.sessions.LocateSession(sessionID).WorkspaceID
		},
		ChatRunningError: ErrChatRunning,
	})
	service.assembleWorkspaceApplication()
	service.components.sessionHistoryRuntime = applicationsessionhistory.NewRuntime(applicationsessionhistory.RuntimeDeps{
		Core:                kernel,
		Deps:                service.Deps,
		ConversationStreams: service.components.conversationStreams,
		Workspace:           service.components.workspace,
		Residency:           service.components.residencyRuntime,
		Sessions:            service.components.sessions,
		View:                service.components.view,
		ColdRead:            service.components.sessionColdRead,
		ColdHistory:         service.components.sessionColdHistory,
		ColdRestore:         service.components.sessionColdRestore,
		ColdWorkspace:       service.components.sessionColdWorkspace,
		ColdActivation:      service.components.sessionColdActivation,
		ColdTask:            service.components.sessionColdTask,
		ColdView:            service.components.sessionColdView,
		ColdPublish:         service.components.sessionColdPublish,
		ColdCommit:          service.components.sessionColdCommit,
		RestoreRecovery:     service.components.sessionRestoreRecovery,
		RestoringShell:      service.components.sessionRestoringShell,
		Closed:              func() bool { return service.closed },
		SessionUnit:         service.sessionUnitLocked,
		PublishEvent:        service.publishSessionEvent,
		StoredTier:          service.readStoredPermissionTier,
		DisplayInput:        displayUserInput,
		ResetTurnState: func(unit *session.SessionUnit) {
			service.components.conversationTurnState.ResetLocked(unit)
		},
	})
	service.components.sessionForkRuntime = applicationsessionfork.NewRuntime(applicationsessionfork.RuntimeDeps{
		Core:                kernel,
		Deps:                service.Deps,
		SessionsRuntime:     service.components.sessions,
		Tasks:               service.components.tasks,
		Sessions:            sessionDomain,
		Closed:              func() bool { return service.closed },
		Draining:            func() bool { return service.draining },
		PerSessionExecution: service.perSessionExecution,
		NewSessionID:        service.newGeneratedSessionID,
		SetWorkspaceScope:   service.setWorkspaceWriteScope,
		ErrDraining:         ErrApplicationDraining,
		ErrRunning:          ErrChatRunning,
	})
	// 索引面（压缩帧进会话压缩栈）是**窄可选**能力：Runtime 实现了
	// context_runtime.CompactionIndexPort 才注入。不塞进 contract.RuntimePort，
	// 也不必让每个 fake/harness 长出空方法——探测失败的正确行为本来就是"不索引"，
	// 压缩照常成立（见 context_runtime.CompactionIndexPort 的注释）。
	var compactionIndex context_runtime.CompactionIndexPort
	if provider, ok := assembler.deps.Runtime.(context_runtime.CompactionIndexPort); ok {
		compactionIndex = provider
	}
	service.components.context = context_runtime.NewEngineCoordinator(context_runtime.EngineDeps{
		Core:     kernel,
		Tasks:    service.components.tasks,
		Sessions: service.components.sessions,
		Prompts:  service.components.prompts,
		View:     service.components.view,
		History:  service.components.history,
		WorkTableTraceBlock: func(sessionID string) string {
			if runtime := service.components.workTableRuntime; runtime != nil {
				return runtime.TraceBlockFor(sessionID)
			}
			return ""
		},
		EnvironmentTraceBlock: func(sessionID string) string {
			return prompt.EnvironmentTraceBlockFor(prompt.EnvironmentTraceDeps{
				ProjectEnvironments: service.components.sessionSSHApplication.ProjectEnvironments,
				CurrentEnvironment: func(sessionID string) (string, error) {
					state, err := service.components.sessionSSHApplication.State(sessionID)
					return state.CurrentEnv, err
				},
				ProjectRoot: func(sessionID string) string {
					if service.Deps.Workspace == nil {
						return ""
					}
					workspace, _ := service.Deps.Workspace.SessionWorkspace(sessionID)
					return workspace.RootPath
				},
			}, sessionID)
		},
		CompactionIndex: compactionIndex,
	})
	service.components.providerRecovery = context_runtime.NewRecovery(context_runtime.RecoveryDeps{
		Core:                kernel,
		Tasks:               service.components.tasks,
		View:                service.components.view,
		Context:             service.components.context,
		EngineHistory:       service.engineHistoryFor,
		ReplaceHistory:      service.replaceEngineHistory,
		ChatStream:          service.chatStream,
		PublishSessionEvent: service.publishSessionEvent,
	})
	service.assembleConversationTurnRuntimeComponents()
	service.components.subagent = subagentview.NewCoordinator(subagentview.Deps{
		Core:   kernel,
		View:   service.components.view,
		Limits: func() dto.ViewLimits { return applicationpolicy.Get().ViewLimits() },
		// 工具记录走 toolrecord 唯一判据并先进入执行单元暂存；core 只注入单元键折算与主侧合并。
		RecordToolOutcome: func(ownerSessionID string, event dto.SubagentToolEvent) {
			if service.toolRecords != nil {
				service.toolRecords.StageSubagentEvent(ownerSessionID, event)
			}
		},
		// 节点收口（终态）：把暂存一次性提交给主会话累计（同步、返回即入账）。
		CommitToolRecord: func(ownerSessionID, nodeID string) {
			if service.toolRecords != nil {
				service.toolRecords.CloseSubagentUnit(ownerSessionID, nodeID)
			}
		},
	})
	service.assembleSubagentApplication()
	service.components.input = assembler.ports.InputRouter(contract.InputRouteHandlers{
		Command: service.submitCommand,
		Plugin:  service.submitPluginSwitch,
		Skill:   service.submitSkill,
		Conversation: func(ctx context.Context, input string) error {
			service.prepareCompletedTaskBoundary()
			return service.submitConversation(ctx, input)
		},
	})
	service.assembleSubmissionApplication()
	// WorkTable runtime owns source synchronization, lifecycle consumers and
	// latest-wins publication. Core only supplies the host state and narrow
	// callbacks.
	service.components.workTableRuntime = applicationtable.NewRuntime(applicationtable.RuntimeDeps{
		Core:                  kernel,
		Tasks:                 service.components.tasks,
		View:                  service.components.view,
		Limits:                workTableBounds,
		TaskRecordsForSession: service.taskRecordsForSession,
		CloseTerminalSubagentToolRecords: func(nodes []dto.SubAgentTreeNode) {
			if service.toolRecords != nil {
				service.toolRecords.CloseTerminalSubagents(nodes)
			}
		},
		SubAgentTree: func() []dto.SubAgentTreeNode {
			service.ViewMu.RLock()
			tree := worktable.CloneSubAgentTreeForSync(service.Core.Snapshot.Runtime.SubAgentTree)
			service.ViewMu.RUnlock()
			return tree
		},
		PublishWorkTable: func(update worktable.WorkTableUpdate, payload model.WorkTableEvent) {
			service.ViewMu.RLock()
			sessionID := service.Core.Snapshot.Session.ID
			service.ViewMu.RUnlock()
			service.publishSessionEvent(EventWorkTableChanged, update.Revision, update.RequestID, sessionID, payload)
		},
		PublishSessionEvent: service.publishSessionEvent,
		HandleLifecycleFault: func(recovered any) {
			service.components.faultGuardApplication.HandleLifecycleFault(recovered)
		},
		TriggerAsync: func(triggered map[string]struct{}) int {
			return service.components.asyncCompletionApplication.TriggerAsyncCompletions(triggered)
		},
		TriggerTeamwork: func(triggered map[string]struct{}) int {
			return service.components.asyncCompletionApplication.TriggerTeamworkJobCompletions(triggered)
		},
		AsyncTriggerEnabled: func() bool {
			return applicationpolicy.Get().AsyncExec.TriggerConversation
		},
		RuntimeSnapshot: func() model.RuntimeState {
			return service.components.frontendSnapshot.Snapshot().Runtime
		},
	})
	// CSP 生命周期消费者：子代理树信号 / plan 节点事件 / task 变更经
	// channel 流转（取代同步回调嵌套，避免锁序事故）。
	service.components.workTableRuntime.StartLifecycleConsumers()
	service.Deps.Runtime.SetSubagentPolicy(service.effortManager.SubagentPolicy())
	service.idle = closedSignal()
	initialSessionID := service.Deps.Engine.SessionID()
	initialDraft := initialSessionID == ""
	if initialDraft {
		// 冷启动：引擎尚未建 bundle（HasSession=false）。早分配草稿 SID，
		// 使"启动即草稿"也持有真实会话身份（订阅键/事件路由/物化复用同一 ID）。
		initialSessionID = service.newDraftSessionIDLocked()
		// 同步落草稿槽位（service.draft）：Snapshot 的草稿行注入、切换后草稿行
		// 保留、BeginNewSession 的幂等复用、显式提交的物化路径都以该槽位为唯一
		// 责任源（与 BeginNewSession / resetViewToDraftAfterRestoreFailure 同构）。
		// 只分配 ID 不落槽位时，"启动即草稿"的会话行仅靠前端"当前会话兜底行"
		// 显示，一旦切换视图就再无来源——初始会话从会话树"消失"的回归现场。
		now := time.Now()
		service.draft = &draftSlot{ID: initialSessionID, CreatedAt: now, UpdatedAt: now}
	}
	initialSession := model.SessionState{ID: initialSessionID, Draft: initialDraft}
	if initialDraft {
		initialSession.Name = draftSessionName
	}
	service.Core.Snapshot = model.Snapshot{
		ProtocolVersion:    ProtocolVersion,
		Session:            initialSession,
		Runtime:            model.RuntimeState{Model: service.Deps.Runtime.Model(), Effort: service.effortManager.Current(), ReasoningEffort: prompt.ReasoningEffortFor(service.effortManager.Current()), LoopLimit: prompt.MaxLoops(service.effortManager.Current())},
		Capabilities:       model.Capabilities{SessionResume: true},
		ConversationWindow: applicationpolicy.Get().HistoryWindow,
	}
	service.sessions.SetActive(initialSessionID)
	service.sessionUnitLocked(initialSessionID)
	service.mirrorActiveChatLocked()
	service.components.tasks.ImportEngineHistoryAsTranscriptLocked(service.Deps.Engine.History())
	// 需求变更（P1-1）：冷启动读回视图会话的权限档位。必须在运行时投影之前——
	// 投影按会话读档位，恢复晚了首个快照会显示进程默认档位（重启即"丢设置"的观感）。
	service.restorePermissionTierFor(service.currentViewSessionID())
	// 会话加载处恢复"会话自持角色"（权责表 root 主体的主体名；与 gui/bridge.go 的
	// sessionSelfRole 同值）的权限向量：**登记优先，缺失按五级链路解析**
	//（user_allow > agent_dispatch > session_default > global_default）。
	// 不写回盘、不缓存第二份向量（事实源在存储层的登记格，判据在 dto），因此这次调用
	// 的作用是"装配缺口与坏配置在加载时暴露 + 把该会话的登记格热起来"；未装配登记能力的
	// 宿主（CLI / 测试假实现 / headless）不因此启动失败，只有显式读面才报
	// ErrRolePermissionsUnavailable。
	_, _ = service.components.roleApplication.RestoreRolePermissions(service.currentViewSessionID(), []string{"root"})
	// 角色账号与权限同属项目级默认；恢复失败不回滚会话打开（此时还没有回合）。
	_ = service.components.roleApplication.RestoreRoleAccounts(service.currentViewSessionID())
	if err := service.registerBuiltinCommands(); err != nil {
		return nil, err
	}
	service.components.view.ApplyRuntimeProjectionLocked(service.components.view.CollectRuntimeProjection(context.Background()))
	service.restoreInitialWorkspace()
	service.components.prompts.BuildSystemPrompt()
	service.Core.Snapshot.Revision = 1
	service.Approval.SetObserver(service.observeInteraction)
	service.components.sessions.StartCatalogRefresh()
	service.publishRuntimeProjections()
	return service, nil
}

func validateDependencies(deps contract.Dependencies) error {
	checks := []struct {
		name       string
		dependency any
	}{
		{name: "engine", dependency: deps.Engine},
		{name: "runtime", dependency: deps.Runtime},
		{name: "plugins", dependency: deps.Plugins},
		{name: "skills", dependency: deps.Skills},
		{name: "sessions", dependency: deps.Sessions},
	}
	for _, check := range checks {
		if check.dependency == nil {
			return fmt.Errorf("application dependency %s is required", check.name)
		}
	}
	return nil
}

func (assembler *serviceAssembler) applyInfrastructureDefaults() {
	if assembler.deps.Events == nil {
		assembler.deps.Events = event.NewEventHub()
	}
	if assembler.deps.Approval == nil {
		assembler.deps.Approval = assembler.ports.ApprovalFactory(assembler.deps.Events)
	}
}

func (service *Service) restoreInitialWorkspace() {
	if service.Deps.Workspace == nil {
		return
	}
	workspaceProjection := service.components.workspace.Projection()
	service.applyWorkspaceProjectionLocked(workspaceProjection)
	workspace, ok := service.Deps.Workspace.SessionWorkspace(service.Core.Snapshot.Session.ID)
	if !ok {
		return
	}
	if err := service.Deps.Runtime.BindProjectRoot(workspace.RootPath); err != nil {
		return
	}
	service.Deps.Sessions.SetWorkspace(workspace.ID)
	service.Core.Snapshot.CurrentWorkspace = &workspace
}
