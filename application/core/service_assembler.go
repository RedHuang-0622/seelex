package core

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/seelex/internal/promptassets"
)

// serviceAssembler is the composition root for the application service. It
// supplies infrastructure defaults before wiring stateful collaborators.
type serviceAssembler struct {
	deps Dependencies
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
	state := &serviceState{
		infrastructureState: infrastructureState{
			deps: assembler.deps, events: assembler.deps.Events,
			approval: assembler.deps.Approval, commands: NewCommandRegistry(),
		},
		promptRuntimeState: promptRuntimeState{promptStack: promptStack},
		sessionRuntimeState: sessionRuntimeState{
			sessionNames:       make(map[string]sessionNameCacheEntry),
			sessionCatalogWake: make(chan struct{}, 1),
			sessionCatalogStop: make(chan struct{}),
			sessionCatalogDone: make(chan struct{}),
		},
		planRuntimeState: planRuntimeState{
			replanInFlight: make(map[string]struct{}),
		},
		taskRuntimeState: taskRuntimeState{
			tokenCounter: conservativeTokenCounter{}, resultRefsByToolCallID: make(map[string]string),
		},
	}
	service := &Service{serviceState: state}
	service.effortManager = NewEffortManager(promptStack, service.deps.Engine)
	service.components.prompts = newPromptCoordinator(state)
	service.components.tasks = newTaskContextCoordinator(state)
	service.components.history = newHistorySafetyCoordinator(state)
	service.components.sessions = newSessionCoordinator(state)
	service.components.sessions.tasks = service.components.tasks
	service.components.view = newViewCoordinator(state, service.components.sessions)
	service.components.context = newContextCoordinator(state, contextCollaborators{
		prompts:  service.components.prompts,
		sessions: service.components.sessions,
		view:     service.components.view,
		tasks:    service.components.tasks,
		history:  service.components.history,
	})
	service.components.input = newInputRouter(inputRouteHandlers{
		command: service.submitCommand,
		skill:   service.submitSkill,
		plugin:  service.SwitchPlugin,
		conversation: func(ctx context.Context, input string) error {
			service.prepareCompletedTaskBoundary()
			return service.submitConversation(ctx, input)
		},
	})
	// worktable.changed 汇聚发布器：与事件 hub 解耦，突发时 latest-wins。
	service.workTablePublisher = newWorkTablePublisher(func(update worktableUpdate) {
		service.events.Publish(EventWorkTableChanged, update.revision, update.requestID, WorkTableEvent{Items: update.items})
	})
	// CSP 生命周期消费者：子代理树信号 / plan 节点事件 / task 变更经
	// channel 流转（取代同步回调嵌套，避免锁序事故）。
	service.startLifecycleConsumers()
	service.deps.Runtime.SetPlanPolicy(service.effortManager.PlanPolicy())
	service.idle = closedSignal()
	initialSessionID := service.deps.Engine.SessionID()
	service.snapshot = Snapshot{
		ProtocolVersion:    ProtocolVersion,
		Session:            SessionState{ID: initialSessionID, Draft: initialSessionID == ""},
		Runtime:            RuntimeState{Model: service.deps.Runtime.Model(), Effort: service.effortManager.Current()},
		Capabilities:       Capabilities{SessionResume: true},
		ConversationWindow: Limits().HistoryWindow,
	}
	service.components.tasks.importEngineHistoryAsTranscriptLocked(service.deps.Engine.History())
	if err := service.registerBuiltinCommands(); err != nil {
		return nil, err
	}
	service.applyRuntimeProjectionLocked(service.collectRuntimeProjection(context.Background()))
	service.restoreInitialWorkspace()
	service.components.prompts.buildSystemPrompt()
	service.snapshot.Revision = 1
	service.approval.SetObserver(service.observeInteraction)
	service.startSessionCatalogRefresh()
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
	if service.deps.Workspace == nil {
		return
	}
	workspaceProjection := service.collectWorkspaceProjection()
	service.applyWorkspaceProjectionLocked(workspaceProjection)
	workspace, ok := service.deps.Workspace.SessionWorkspace(service.snapshot.Session.ID)
	if !ok {
		return
	}
	if err := service.deps.Runtime.BindProjectRoot(workspace.RootPath); err != nil {
		return
	}
	service.deps.Sessions.SetWorkspace(workspace.ID)
	service.snapshot.CurrentWorkspace = &workspace
}
