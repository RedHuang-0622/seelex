package core

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/core/prompt_layer"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/core/subagent_view"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/core/view_state"
	"github.com/RedHuang-0622/seelex/application/core/worktable"
	"github.com/RedHuang-0622/seelex/internal/promptassets"
	"github.com/RedHuang-0622/seelex/session"
)

// serviceAssembler is the composition root for the application service. It
// supplies infrastructure defaults before wiring stateful collaborators.
type serviceAssembler struct {
	deps Dependencies
}

// taskPromptPort 是 task_context.PromptPort 的根包适配（prompt 域尚未下沉
// 时的装配适配层）。
type taskPromptPort struct {
	promptStack   *PromptStack
	effortManager *EffortManager
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
	if err := promptassets.Validate(); err != nil {
		return nil, fmt.Errorf("application prompts: %w", err)
	}
	assembler.applyInfrastructureDefaults()

	promptStack := NewPromptStack()
	kernel := state.New(assembler.deps)
	sessionDomain := session.NewDomain()
	svcState := &serviceState{
		Core:               kernel,
		commands:           NewCommandRegistry(),
		promptRuntimeState: promptRuntimeState{promptStack: promptStack},
		sessions:           sessionDomain,
		restoring:          make(map[string]struct{}),
	}
	service := &Service{serviceState: svcState}
	service.effortManager = NewEffortManager(promptStack, service.Deps.Engine)
	// 进程级 fullAccess 默认在装配期捕获一次（配置/初始引擎门值），会话
	// 未选择时回退该默认，而不是读取其它会话遗留的引擎门状态（G4）。
	if service.Deps.Runtime != nil {
		service.fullAccessDefault = service.Deps.Runtime.FullAccess()
	}
	service.components.tasks = task_context.NewCoordinator(task_context.Deps{
		Core: kernel,
		Prompt: taskPromptPort{
			promptStack: promptStack, effortManager: service.effortManager,
		},
		Limits: Limits,
		IsInternalContent: func(content string) bool {
			return context_runtime.IsTaskContextCheckpoint(content) || context_runtime.IsProviderOnlyHistoryContent(content) || context_runtime.IsActiveSkillContent(content)
		},
		IsOversizedToolResult:      context_runtime.IsOversizedToolResult,
		OversizedToolResultWarning: context_runtime.OversizedToolResultWarning,
		PresentToolError:           presentToolError,
		QueuedInputRefs: func(sessionID string) []string {
			// F-3b：按会话取排队输入引用（不读视图 Snapshot；unit 自锁）。
			unit := service.sessions.Unit(sessionID)
			if unit == nil {
				return nil
			}
			return queuedInputRefs(queuedChatRequests(unit.PendingRequests()))
		},
		CurrentSessionID: func() string {
			return service.sessions.ActiveID()
		},
	})
	service.components.prompts = prompt_layer.NewCoordinator(prompt_layer.Deps{
		Core:          kernel,
		PromptStack:   promptStack,
		EffortManager: service.effortManager,
		Tasks:         service.components.tasks,
	})
	service.components.history = context_runtime.NewHistoryCoordinator(kernel)
	service.components.sessions = session_runtime.NewCoordinator(session_runtime.Deps{
		Core:  kernel,
		Tasks: service.components.tasks,
		Closed: func() bool {
			return service.closed
		},
		TranscriptTailBudget: func(runtime any) int {
			return task_context.ContextBudgetFor(runtime).TargetAfterCompaction
		},
		IsInternalContent: func(content string) bool {
			return context_runtime.IsTaskContextCheckpoint(content) || context_runtime.IsProviderOnlyHistoryContent(content) || context_runtime.IsActiveSkillContent(content)
		},
		TailHistory:                task_context.TranscriptTailHistory,
		OversizedToolResultWarning: context_runtime.OversizedToolResultWarning,
		ContentReferenceWarning:    context_runtime.ContentReferenceWarning,
		Limits:                     Limits,
		DisplayUserInput:           displayUserInput,
	})
	service.components.view = view_state.NewCoordinator(view_state.Deps{
		Core:              kernel,
		Units:             sessionDomain,
		CurrentEffort:     service.effortForSession,
		CurrentFullAccess: service.fullAccessForSession,
		CurrentSessionID: func() string {
			return service.sessions.ActiveID()
		},
		Tasks: service.components.tasks,
		RefreshWorkTableLocked: func(tasks []dto.TaskRecord) {
			service.refreshWorkTableLocked(tasks)
		},
		Limits: Limits,
	})
	service.components.sessions.BindView(service.components.view)
	service.components.context = context_runtime.NewCoordinator(context_runtime.Deps{
		Core:     kernel,
		Tasks:    service.components.tasks,
		Sessions: service.components.sessions,
		Prompts:  service.components.prompts,
		View:     service.components.view,
		History:  service.components.history,
		WorkTableTraceBlock: func(sessionID string) string {
			return service.workTableTraceBlockFor(sessionID)
		},
	})
	service.components.subagent = subagent_view.NewCoordinator(subagent_view.Deps{
		Core:   kernel,
		View:   service.components.view,
		Limits: Limits,
	})
	service.components.input = newInputRouter(inputRouteHandlers{
		Command: service.submitCommand,
		Skill:   service.submitSkill,
		Plugin:  service.SwitchPlugin,
		Conversation: func(ctx context.Context, input string) error {
			service.prepareCompletedTaskBoundary()
			return service.submitConversation(ctx, input)
		},
	})
	// worktable.changed 汇聚发布器：与事件 hub 解耦，突发时 latest-wins。
	service.workTablePublisher = worktable.NewWorkTablePublisher(func(update worktable.WorkTableUpdate) {
		service.ViewMu.RLock()
		sessionID := service.Core.Snapshot.Session.ID
		service.ViewMu.RUnlock()
		service.publishSessionEvent(EventWorkTableChanged, update.Revision, update.RequestID, sessionID, WorkTableEvent{
			Items: update.Items, Batches: update.Batches,
		})
	})
	// CSP 生命周期消费者：子代理树信号 / plan 节点事件 / task 变更经
	// channel 流转（取代同步回调嵌套，避免锁序事故）。
	service.startLifecycleConsumers()
	service.Deps.Runtime.SetPlanPolicy(service.effortManager.PlanPolicy())
	service.idle = closedSignal()
	initialSessionID := service.Deps.Engine.SessionID()
	initialDraft := initialSessionID == ""
	if initialDraft {
		// 冷启动：引擎尚未建 bundle（HasSession=false）。早分配草稿 SID，
		// 使"启动即草稿"也持有真实会话身份（订阅键/事件路由/物化复用同一 ID）。
		initialSessionID = service.newDraftSessionIDLocked()
	}
	initialSession := SessionState{ID: initialSessionID, Draft: initialDraft}
	if initialDraft {
		initialSession.Name = draftSessionName
	}
	service.Core.Snapshot = Snapshot{
		ProtocolVersion:    ProtocolVersion,
		Session:            initialSession,
		Runtime:            RuntimeState{Model: service.Deps.Runtime.Model(), Effort: service.effortManager.Current()},
		Capabilities:       Capabilities{SessionResume: true},
		ConversationWindow: Limits().HistoryWindow,
	}
	service.sessions.SetActive(initialSessionID)
	service.sessionUnitLocked(initialSessionID)
	service.mirrorActiveChatLocked()
	if initialDraft {
		// 冷启动恢复持久化的草稿会话（composer 跨重启恢复）；无草稿记录时
		// 保持装配器生成的空白草稿。
		service.restorePersistedDraft()
	}
	service.components.tasks.ImportEngineHistoryAsTranscriptLocked(service.Deps.Engine.History())
	if err := service.registerBuiltinCommands(); err != nil {
		return nil, err
	}
	service.applyRuntimeProjectionLocked(service.collectRuntimeProjection(context.Background()))
	service.restoreInitialWorkspace()
	service.components.prompts.BuildSystemPrompt()
	service.Core.Snapshot.Revision = 1
	service.Approval.SetObserver(service.observeInteraction)
	service.components.sessions.StartCatalogRefresh()
	service.publishRuntimeProjections()
	return service, nil
}

func validateDependencies(deps Dependencies) error {
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
		assembler.deps.Events = NewEventHub()
	}
	if assembler.deps.Approval == nil {
		assembler.deps.Approval = NewApprovalBroker(assembler.deps.Events)
	}
}

func (service *Service) restoreInitialWorkspace() {
	if service.Deps.Workspace == nil {
		return
	}
	workspaceProjection := service.collectWorkspaceProjection()
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
