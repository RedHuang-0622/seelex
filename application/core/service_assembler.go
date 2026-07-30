package core

import "context"

// serviceAssembler is the composition root for the application service. It
// supplies infrastructure defaults before wiring stateful collaborators.
type serviceAssembler struct {
	deps Dependencies
}

func (assembler serviceAssembler) assemble() *Service {
	assembler.applyInfrastructureDefaults()

	promptStack := NewPromptStack()
	state := &serviceState{
		infrastructureState: infrastructureState{
			deps: assembler.deps, events: assembler.deps.Events,
			approval: assembler.deps.Approval, commands: NewCommandRegistry(),
		},
		promptRuntimeState: promptRuntimeState{promptStack: promptStack},
		sessionRuntimeState: sessionRuntimeState{
			sessionNames: make(map[string]sessionNameCacheEntry),
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
	service.deps.Runtime.SetPlanPolicy(service.effortManager.PlanPolicy())
	service.idle = closedSignal()
	service.snapshot = Snapshot{
		ProtocolVersion: ProtocolVersion,
		Session:         SessionState{ID: service.deps.Engine.SessionID()},
		Runtime:         RuntimeState{Model: service.deps.Runtime.Model(), Effort: service.effortManager.Current()},
		Capabilities:    Capabilities{SessionResume: true},
	}
	service.components.tasks.importEngineHistoryAsTranscriptLocked(service.deps.Engine.History())
	service.registerBuiltinCommands()
	service.refreshRuntimeLocked(context.Background())
	service.restoreInitialWorkspace()
	service.components.prompts.buildSystemPrompt()
	service.snapshot.Revision = 1
	service.approval.SetObserver(service.observeInteraction)
	return service
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
	service.refreshWorkspaceLocked()
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
