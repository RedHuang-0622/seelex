package core

import (
	"context"
	"fmt"
	context_runtime "github.com/RedHuang-0622/seelex/application/conversation/context"
	"github.com/RedHuang-0622/seelex/application/event"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	applicationtable "github.com/RedHuang-0622/seelex/application/execution/table"
	"github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	applicationgoal "github.com/RedHuang-0622/seelex/application/goal"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/application/session/runtime"
	session "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/application/session/viewstate"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/state"
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
		restoring:          make(map[string]struct{}),
		restoreSig:         make(chan struct{}),
	}
	service := &Service{serviceState: svcState}
	service.components.executionApplication = newServiceExecutionApplication(service, assembler.ports.TeamworkBoardReader(service.Deps.Runtime))
	service.components.adminApplication = newServiceAdminApplication(service)
	service.components.sessionSSHApplication = newServiceSessionSSHApplication(service)
	service.components.roleApplication = newServiceRoleApplication(service)
	service.components.schedulerApplication = newServiceSchedulerApplication(service)
	service.components.scheduledRunApplication = newServiceScheduledRunApplication(service)
	service.components.scheduledSessionApplication = newServiceScheduledSessionApplication(service)
	service.components.runtimeApplication = newServiceRuntimeApplication(service)
	service.components.reviewApplication = newServiceReviewApplication(service)
	service.components.lifecycleApplication = newServiceLifecycleApplication(service)
	service.components.historySearchApplication = newServiceHistorySearchApplication(service)
	service.components.completionApplication = newServiceCompletionApplication(service)
	service.components.referenceApplication = newServiceReferenceApplication(service)
	service.components.sessionMetaApplication = newServiceSessionMetaApplication(service)
	service.components.projectBoardQuery = newServiceProjectBoardQuery(service)
	service.components.sessionReleaseApplication = newServiceSessionReleaseApplication(service)
	service.components.contextCompactionApplication = newServiceContextCompactionApplication(service)
	service.components.faultGuardApplication = newServiceFaultGuardApplication(service)
	service.components.perfStatsApplication = newServicePerfStatsApplication(service)
	service.components.asyncCompletionApplication = newServiceAsyncCompletionApplication(service)
	service.components.sessionQuery = newServiceSessionQuery(service)
	service.components.historyWindowAssembler = assembler.ports.HistoryWindow
	service.components.residency = assembler.ports.Residency
	service.components.conversationStreams = assembler.ports.ConversationStream
	service.components.accountWindowPolicy = assembler.ports.AccountWindow
	service.components.skillActivation = assembler.ports.SkillActivation
	service.components.sessionRecords = assembler.ports.SessionRecords
	service.components.errorPresentation = assembler.ports.ErrorPresentation
	service.components.roleSessions = assembler.ports.RoleSessions(assembler.deps.Sessions)
	service.components.conversationHistoryQuery = newServiceConversationHistoryQuery(service, assembler.ports.HistoryWindow)
	service.components.conversationTurnState = newConversationTurnState()
	service.components.conversationQueue = newServiceConversationQueue(service)
	service.components.sessionLifecycle = newServiceSessionLifecycle(service)
	service.components.sessionArchive = newServiceSessionArchive(service)
	service.components.sessionDelete = newServiceSessionDelete(service)
	service.components.sessionDraftSubmission = newServiceSessionDraftSubmission(service)
	service.components.sessionDraftMaterialization = newServiceSessionDraftMaterialization(service)
	service.components.sessionDraftEntry = newServiceSessionDraftEntry(service)
	service.components.sessionResume = newServiceSessionResume(service)
	service.components.sessionRestoringShell = newServiceSessionRestoringShell(service)
	service.components.sessionRestoreRecovery = newServiceSessionRestoreRecovery(service)
	service.components.sessionRestoreDraftReset = newServiceSessionRestoreDraftReset(service)
	service.components.sessionColdRead = newServiceSessionColdRead(service)
	service.components.sessionColdHistory = newServiceSessionColdHistory(service)
	service.components.sessionColdRestore = newServiceSessionColdRestore(service)
	service.components.sessionColdWorkspace = newServiceSessionColdWorkspace(service)
	service.components.sessionColdActivation = newServiceSessionColdActivation(service)
	service.components.sessionColdTask = newServiceSessionColdTask(service)
	service.components.sessionColdView = newServiceSessionColdView(service)
	service.components.sessionColdPublish = newServiceSessionColdPublish(service)
	service.components.sessionColdCommit = newServiceSessionColdCommit(service)
	service.components.editForkOperationRegistry = newServiceEditForkOperationRegistry(service)
	service.components.editForkValidator = newServiceEditForkValidator(service)
	service.components.editForkTargetResolver = newServiceEditForkTargetResolver(service)
	service.components.editForkRollback = newServiceEditForkRollback(service)
	service.components.editForkProcess = newServiceEditForkProcess(service)
	service.components.editForkApplication = newServiceEditForkApplication(service)
	service.components.editForkAnnotation = newServiceEditForkAnnotation(service)
	service.components.sessionForkLineage = newServiceSessionForkLineage(service)
	// 两条独立拉条（思考强度 / 循环次数）的状态与判据归位 backend/limit.Knobs（M5-R-limitb），
	// 宿主就是本 Service：视图锁、快照写面、引擎与档位读数经适配注入。
	service.knobs = newServiceKnobs(service)
	service.components.policyApplication = newServicePolicyApplication(service)
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
		WritableSession: service.toolRecordWritableSession,
		ViewSessionID:   service.currentViewSessionID,
	})
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
	// P1：会话级 goal 治理协调器（第五栈持久化经 Runtime 按会话注入；
	// Runtime 未实现 SessionContextStoreFor 时退化为内存态）。
	// 座位作业面与角色座位读面已随席位轮转退场（2026-10-01 阶段三 W3）。
	service.components.goal = applicationgoal.NewCoordinatorForRuntime(assembler.deps.Runtime)
	service.components.goalApplication = newServiceGoalApplication(service, assembler.ports.HostSessionForToolCall)
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
		Goals: goalGovernanceViewPort{service: service},
		// 团队看板读面（计划 + 作业行 + 审计流水）：与 Goals 同构——Deps 收窄成
		// 「会话 → 只读投影」，转发与缓存的责任留给桥（未装配 teamwork 时
		// TeamworkBoardViewFor 走类型断言失败分支返回 nil）。
		Teamwork: teamworkBoardViewPort{service: service},
		RefreshWorkTableLocked: func(tasks []dto.TaskRecord, asyncRuns []dto.AsyncRunRecord) {
			service.refreshWorkTableLocked(tasks, asyncRuns)
		},
		Limits: func() dto.ViewLimits { return applicationpolicy.Get().ViewLimits() },
	})
	service.components.sessions.BindView(service.components.view)
	// TurnProcess captures the view coordinator method value, so it must be
	// assembled after the coordinator exists. This is composition order, not a
	// forwarding wrapper.
	service.components.conversationTurnProcess = newServiceConversationTurnProcess(service)
	service.components.conversationTurnCompletion = newServiceConversationTurnCompletion(service)
	service.components.conversationTurnOutcome = newServiceConversationTurnOutcome(service)
	service.components.conversationTurnPublisher = newServiceConversationTurnPublisher(service)
	service.components.conversationTurnTail = newServiceConversationTurnTail(service)
	service.components.conversationTurnCommit = newServiceConversationTurnCommit(service)
	service.components.conversationTurnFinalize = newServiceConversationTurnFinalize(service)
	// 工作区用例（plan §2.4）：创建/绑定/解绑与当前工作区读面归位
	// middle/switch/workspace；视图投影写面仍由本 Service 持有，经窄 Host 注入。
	service.components.workspace = applicationworkspace.NewSwitch(applicationworkspace.SwitchDeps{
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
			return service.attachSessionContextFor(workspaceID, sessionID)
		},
		RefreshWorkTable: func() {
			service.refreshWorkTableFromSources()
		},
		WorkspaceIDForSession: func(sessionID string) string {
			return service.components.sessions.LocateSession(sessionID).WorkspaceID
		},
		ChatRunningError: ErrChatRunning,
	})
	service.components.workspaceApplication = newServiceWorkspaceApplication(service)
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
			return service.workTableTraceBlockFor(sessionID)
		},
		EnvironmentTraceBlock: func(sessionID string) string {
			return service.environmentTraceBlockFor(sessionID)
		},
		CompactionIndex: compactionIndex,
	})
	service.components.conversationTurnInput = newServiceConversationTurnInput(service)
	service.components.conversationTurnExecution = newServiceConversationTurnExecution(service)
	service.components.conversationTurnResponse = newServiceConversationTurnResponse(service)
	service.components.conversationTurnRecovery = newServiceConversationTurnRecovery(service)
	service.components.conversationTurnCleanup = newServiceConversationTurnCleanup(service)
	service.components.conversationTurnRunner = newServiceConversationTurnRunner(service)
	service.components.subagent = applicationexecution.NewSubagentView(applicationexecution.SubagentViewDeps{
		Core:   kernel,
		View:   service.components.view,
		Limits: func() dto.ViewLimits { return applicationpolicy.Get().ViewLimits() },
		// 工具记录走 toolrecord 的**唯一**判据（Failed + Reason），
		// 且**先落进执行单元自己的暂存**（阶梯提交的第一级台阶）：本包只报
		// "哪个单元、谁、什么状态、错误原文"，单元键折算与主侧合并都在 core 里。
		RecordToolOutcome: func(ownerSessionID string, event dto.SubagentToolEvent) {
			service.stageSubagentToolOutcome(ownerSessionID, event)
		},
		// 节点收口（终态）：把暂存一次性提交给主会话累计（同步、返回即入账）。
		CommitToolRecord: func(ownerSessionID, nodeID string) {
			service.closeSubagentToolRecordUnit(ownerSessionID, nodeID)
		},
	})
	service.components.subagentApplication = newServiceSubagentApplication(service)
	service.components.input = assembler.ports.InputRouter(contract.InputRouteHandlers{
		Command: service.submitCommand,
		Plugin:  service.submitPluginSwitch,
		Skill:   service.submitSkill,
		Conversation: func(ctx context.Context, input string) error {
			service.prepareCompletedTaskBoundary()
			return service.submitConversation(ctx, input)
		},
	})
	// worktable.changed publisher: latest-wins coalescing lives in the extracted
	// table application; core supplies session identity and tree snapshot.
	service.workTable = applicationtable.New(applicationtable.Deps{
		SubAgentTree: func() []dto.SubAgentTreeNode {
			service.ViewMu.RLock()
			tree := applicationtable.CloneSubAgentTreeForSync(service.Core.Snapshot.Runtime.SubAgentTree)
			service.ViewMu.RUnlock()
			return tree
		},
		Publish: func(update applicationtable.WorkTableUpdate, payload model.WorkTableEvent) {
			service.ViewMu.RLock()
			sessionID := service.Core.Snapshot.Session.ID
			service.ViewMu.RUnlock()
			service.publishSessionEvent(EventWorkTableChanged, update.Revision, update.RequestID, sessionID, payload)
		},
	})
	// CSP 生命周期消费者：子代理树信号 / plan 节点事件 / task 变更经
	// channel 流转（取代同步回调嵌套，避免锁序事故）。
	service.startLifecycleConsumers()
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
