package assembly

import (
	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	applicationscheduler "github.com/RedHuang-0622/seelex/application/admin/scheduler"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	conversationcontext "github.com/RedHuang-0622/seelex/application/conversation/context"
	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	conversationsearch "github.com/RedHuang-0622/seelex/application/conversation/search"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	subagentview "github.com/RedHuang-0622/seelex/application/execution/subagentview"
	applicationtable "github.com/RedHuang-0622/seelex/application/execution/table"
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationtoolhooks "github.com/RedHuang-0622/seelex/application/execution/toolhooks"
	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	applicationgoal "github.com/RedHuang-0622/seelex/application/goal"
	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationlifecycle "github.com/RedHuang-0622/seelex/application/lifecycle"
	applicationobservability "github.com/RedHuang-0622/seelex/application/observability"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	applicationprompt "github.com/RedHuang-0622/seelex/application/prompt"
	applicationreference "github.com/RedHuang-0622/seelex/application/reference"
	applicationreview "github.com/RedHuang-0622/seelex/application/review"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	applicationsessionhistory "github.com/RedHuang-0622/seelex/application/session/history"
	applicationsessionquery "github.com/RedHuang-0622/seelex/application/session/query"
	sessionresidency "github.com/RedHuang-0622/seelex/application/session/residency"
	sessionruntime "github.com/RedHuang-0622/seelex/application/session/runtime"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract"
)

type Components struct {
	Admin        AdminComponents
	Conversation ConversationComponents
	Session      SessionComponents
	Execution    ExecutionComponents
	Workspace    WorkspaceComponents
	Policy       PolicyComponents
	Runtime      RuntimeComponents
}
type AdminComponents struct {
	Account     *applicationadmin.AccountApplication
	Goal        *applicationgoal.Application
	GoalRuntime *applicationgoal.Coordinator
	Review      *applicationreview.Application
	Perf        *applicationobservability.PerfApplication
}
type ConversationComponents struct {
	Unified          *applicationconversation.UnifiedApplication
	Queue            *applicationconversation.QueueApplication
	Stream           *applicationconversation.StreamRuntime
	Input            contract.InputDispatcher
	Submission       *applicationinput.SubmissionApplication
	Completion       *applicationinput.Application
	Context          *conversationcontext.EngineCoordinator
	ProviderRecovery *conversationcontext.RecoveryApplication
	Compaction       *conversationcontext.Application
	CompactionGate   *conversationcontext.CompactionGate
	ContextHistory   *conversationcontext.EngineHistoryCoordinator
	History          *applicationhistory.Application
	HistorySearch    *conversationsearch.Application
	EditFork         *applicationedit.EditApplication
	Annotation       *applicationedit.AnnotationApplication
	TurnStart        *applicationconversation.TurnStartApplication
	TurnBudget       *applicationconversation.TurnBudgetFinalizerApplication
	PromptStack      *applicationprompt.PromptStack
	Prompts          *applicationprompt.Coordinator
	TurnProjection   *applicationtoolhooks.ProjectionApplication
	Reference        *applicationreference.Application
}
type SessionComponents struct {
	Query                *applicationsessionquery.Application
	Interaction          *applicationsession.InteractionApplication
	InteractionObserver  *applicationsession.InteractionObserverApplication
	Fork                 *applicationsession.ForkApplication
	Submit               *applicationsession.SubmitApplication
	Archive              *applicationsession.ArchiveApplication
	Delete               *applicationsession.DeleteApplication
	DraftEntry           *applicationsession.DraftEntryApplication
	DraftSubmission      *applicationsession.DraftSubmissionApplication
	DraftMaterialization *applicationsession.DraftMaterializationApplication
	Lifecycle            *applicationsession.LifecycleApplication
	Meta                 *applicationsession.MetaApplication
	Resume               *applicationsession.ResumeApplication
	SSH                  *applicationsession.SSHApplication
	Release              *applicationsession.ReleaseApplication
	HistoryRuntime       *applicationsessionhistory.Runtime
	Domain               *sessionstate.Domain
	Residency            *sessionresidency.Runtime
	Runtime              *sessionruntime.Coordinator
	View                 *view_state.Coordinator
	Snapshot             *view_state.SnapshotApplication
}
type ExecutionComponents struct {
	WorkItem         *applicationexecution.WorkItemApplication
	RoleActivity     *applicationexecution.RoleActivityApplication
	ProjectBoard     *applicationexecution.ProjectBoardApplication
	ProjectBoardSink *ProjectBoardSinkPort
	WorkTable        *applicationtable.Runtime
	Subagent         *applicationexecution.SubagentApplication
	SubagentView     *subagentview.Coordinator
	Tasks            *task_context.Coordinator
	TerminalHandler  *task_context.TerminalHandlerApplication
	ToolRecords      *applicationtoolrecord.Application
}
type WorkspaceComponents struct {
	Projects *applicationworkspace.Application
	Switch   *switchworkspace.UseCase
}
type PolicyComponents struct {
	Switch        *applicationpolicy.SwitchApplication
	Role          *applicationpolicy.RoleApplication
	RoleSessions  contract.RoleSessionsPort
	EffortManager *applicationprompt.EffortManager
	Knobs         *applicationpolicy.Knobs
}
type RuntimeComponents struct {
	Runtime          *applicationadmin.RuntimeApplication
	Commands         contract.CommandRegistryPort
	Scheduler        *applicationscheduler.SchedulerApplication
	ScheduledRun     *applicationscheduler.RunApplication
	ScheduledSession *applicationscheduler.SessionApplication
	Lifecycle        *applicationlifecycle.Application
	FaultGuard       *applicationlifecycle.FaultGuardApplication
}

func ComponentsFor(service *Service) Components {
	if service == nil {
		return Components{}
	}
	return Components{
		Admin: AdminComponents{
			Account:     service.components.adminApplication,
			Goal:        service.components.goalApplication,
			GoalRuntime: service.components.goal,
			Review:      service.components.reviewApplication,
			Perf:        service.components.perfStatsApplication,
		},
		Conversation: ConversationComponents{
			Unified:          service.components.conversationSession,
			Queue:            service.components.conversationQueue,
			Stream:           service.components.conversationStreamRuntime,
			Input:            service.components.input,
			Submission:       service.components.submission,
			Completion:       service.components.completionApplication,
			Context:          service.components.context,
			ProviderRecovery: service.components.providerRecovery,
			Compaction:       service.components.contextCompactionApplication,
			CompactionGate:   service.components.contextCompactionGate,
			ContextHistory:   service.components.history,
			History:          service.components.conversationHistoryQuery,
			HistorySearch:    service.components.historySearchApplication,
			EditFork:         service.components.editFork.application,
			Annotation:       service.components.editFork.annotation,
			TurnStart:        service.components.conversationTurn.start,
			TurnBudget:       service.components.conversationTurn.budget,
			PromptStack:      service.promptStack,
			Prompts:          service.components.prompts,
			TurnProjection:   service.components.conversationTurn.projection,
			Reference:        service.components.referenceApplication,
		},
		Session: SessionComponents{
			Query:                service.components.sessionQuery,
			Interaction:          service.components.interactions.resolve,
			InteractionObserver:  service.components.interactions.observe,
			Fork:                 service.components.forkApplication,
			Submit:               service.components.submitApplication,
			Archive:              service.components.sessionArchive,
			Delete:               service.components.sessionDelete,
			DraftEntry:           service.components.sessionDrafts.entry,
			DraftSubmission:      service.components.sessionDrafts.submission,
			DraftMaterialization: service.components.sessionDrafts.materialization,
			Lifecycle:            service.components.sessionLifecycle,
			Meta:                 service.components.sessionMetaApplication,
			Resume:               service.components.sessionResume,
			SSH:                  service.components.sessionSSHApplication,
			Release:              service.components.sessionReleaseApplication,
			HistoryRuntime:       service.components.sessionHistoryRuntime,
			Domain:               service.sessions,
			Residency:            service.components.residencyRuntime,
			Runtime:              service.components.sessions,
			View:                 service.components.view,
			Snapshot:             service.components.frontendSnapshot,
		},
		Execution: ExecutionComponents{
			WorkItem:         service.components.executionApplication,
			RoleActivity:     service.components.roleActivity,
			ProjectBoard:     service.components.projectBoardQuery,
			ProjectBoardSink: service.components.projectBoard,
			WorkTable:        service.components.workTableRuntime,
			Subagent:         service.components.subagentApplication,
			SubagentView:     service.components.subagent,
			Tasks:            service.components.tasks,
			TerminalHandler:  service.components.terminalHandler,
			ToolRecords:      service.toolRecords,
		},
		Workspace: WorkspaceComponents{
			Projects: service.components.workspaceApplication,
			Switch:   service.components.workspace,
		},
		Policy: PolicyComponents{
			Switch:        service.components.policyApplication,
			Role:          service.components.roleApplication,
			RoleSessions:  service.components.roleSessions,
			EffortManager: service.effortManager,
			Knobs:         service.knobs,
		},
		Runtime: RuntimeComponents{
			Runtime:          service.components.runtimeApplication,
			Commands:         service.commands,
			Scheduler:        service.components.schedulerApplication,
			ScheduledRun:     service.components.scheduledRunApplication,
			ScheduledSession: service.components.scheduledSessionApplication,
			Lifecycle:        service.components.lifecycleApplication,
			FaultGuard:       service.components.faultGuardApplication,
		},
	}
}
