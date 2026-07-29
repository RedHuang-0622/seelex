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
	service := &Service{
		deps:           assembler.deps,
		events:         assembler.deps.Events,
		approval:       assembler.deps.Approval,
		commands:       NewCommandRegistry(),
		promptStack:    promptStack,
		sessionNames:   make(map[string]sessionNameCacheEntry),
		replanInFlight: make(map[string]struct{}),
	}
	service.inputDispatcher = newInputRouter(service)
	service.effortManager = NewEffortManager(promptStack, service.deps.Engine)
	service.deps.Runtime.SetPlanPolicy(service.effortManager.PlanPolicy())
	service.idle = closedSignal()
	service.snapshot = Snapshot{
		ProtocolVersion: ProtocolVersion,
		Session:         SessionState{ID: service.deps.Engine.SessionID()},
		Runtime:         RuntimeState{Model: service.deps.Runtime.Model(), Effort: service.effortManager.Current()},
		Capabilities:    Capabilities{SessionResume: true},
	}
	service.registerBuiltinCommands()
	service.refreshRuntimeLocked(context.Background())
	service.restoreInitialWorkspace()
	service.buildSystemPrompt()
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
