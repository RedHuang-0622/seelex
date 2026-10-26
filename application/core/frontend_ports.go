package core

import (
	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	applicationscheduler "github.com/RedHuang-0622/seelex/application/admin/scheduler"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	conversationcontext "github.com/RedHuang-0622/seelex/application/conversation/context"
	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	conversationsearch "github.com/RedHuang-0622/seelex/application/conversation/search"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	applicationgoal "github.com/RedHuang-0622/seelex/application/goal"
	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationlifecycle "github.com/RedHuang-0622/seelex/application/lifecycle"
	applicationobservability "github.com/RedHuang-0622/seelex/application/observability"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	applicationreference "github.com/RedHuang-0622/seelex/application/reference"
	applicationreview "github.com/RedHuang-0622/seelex/application/review"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	applicationsessionquery "github.com/RedHuang-0622/seelex/application/session/query"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	"github.com/RedHuang-0622/seelex/contract"
)

// FrontendPorts is the assembly snapshot consumed by application/compat.
// It exposes only already-extracted application components; core keeps state,
// locking, publication, and host adapters.
type FrontendPorts struct {
	Admin               *applicationadmin.AccountApplication
	Completion          *applicationinput.Application
	ContextCompaction   *conversationcontext.Application
	ConversationQueue   *applicationconversation.QueueApplication
	Execution           *applicationexecution.WorkItemApplication
	Goal                *applicationgoal.Application
	History             *applicationhistory.Application
	HistorySearch       *conversationsearch.Application
	Perf                *applicationobservability.PerfApplication
	Policy              *applicationpolicy.SwitchApplication
	ProjectBoard        *applicationexecution.ProjectBoardApplication
	Reference           *applicationreference.Application
	Review              *applicationreview.Application
	Role                *applicationpolicy.RoleApplication
	RoleSessions        contract.RoleSessionsPort
	SessionDelete       *applicationsession.DeleteApplication
	SessionArchive      *applicationsession.ArchiveApplication
	SessionDraftEntry   *applicationsession.DraftEntryApplication
	SessionLifecycle    *applicationsession.LifecycleApplication
	SessionMeta         *applicationsession.MetaApplication
	SessionQuery        *applicationsessionquery.Application
	SessionResume       *applicationsession.ResumeApplication
	SessionSSH          *applicationsession.SSHApplication
	EditFork            *applicationedit.EditApplication
	Subagent            *applicationexecution.SubagentApplication
	Scheduler           *applicationscheduler.SchedulerApplication
	ScheduledRun        *applicationscheduler.RunApplication
	ScheduledSession    *applicationscheduler.SessionApplication
	Lifecycle           *applicationlifecycle.Application
	Runtime             *applicationadmin.RuntimeApplication
	Tasks               *task_context.Coordinator
	View                *view_state.Coordinator
	SubagentView        *applicationexecution.SubagentViewCoordinator
	Workspace           *applicationworkspace.Application
	WorkspaceUseCase    *applicationworkspace.SwitchUseCase
	AddNotice           func(string)
	PublishRuntime      func()
	CloseRoleToolRecord func(mainSessionID, roleSessionID string)
}

func (service *Service) roleSessions() contract.RoleSessionsPort {
	if service == nil {
		return nil
	}
	return service.components.roleSessions
}

// NewFrontendPorts returns a read-only snapshot of components needed by the
// frontend compatibility adapter. It must not be used by core state machines.
func NewFrontendPorts(service *Service) FrontendPorts {
	if service == nil {
		return FrontendPorts{}
	}
	return FrontendPorts{
		Admin:             service.components.adminApplication,
		Completion:        service.components.completionApplication,
		ContextCompaction: service.components.contextCompactionApplication,
		ConversationQueue: service.components.conversationQueue,
		Execution:         service.components.executionApplication,
		Goal:              service.components.goalApplication,
		History:           service.components.conversationHistoryQuery,
		HistorySearch:     service.components.historySearchApplication,
		Perf:              service.components.perfStatsApplication,
		Policy:            service.components.policyApplication,
		ProjectBoard:      service.components.projectBoardQuery,
		Reference:         service.components.referenceApplication,
		Review:            service.components.reviewApplication,
		Role:              service.components.roleApplication,
		RoleSessions:      service.components.roleSessions,
		SessionDelete:     service.components.sessionDelete,
		SessionArchive:    service.components.sessionArchive,
		SessionDraftEntry: service.components.sessionDraftEntry,
		SessionLifecycle:  service.components.sessionLifecycle,
		SessionMeta:       service.components.sessionMetaApplication,
		SessionQuery:      service.components.sessionQuery,
		SessionResume:     service.components.sessionResume,
		SessionSSH:        service.components.sessionSSHApplication,
		EditFork:          service.components.editForkApplication,
		Subagent:          service.components.subagentApplication,
		Scheduler:         service.components.schedulerApplication,
		ScheduledRun:      service.components.scheduledRunApplication,
		ScheduledSession:  service.components.scheduledSessionApplication,
		Lifecycle:         service.components.lifecycleApplication,
		Runtime:           service.components.runtimeApplication,
		Tasks:             service.components.tasks,
		View:              service.components.view,
		SubagentView:      service.components.subagent,
		Workspace:         service.components.workspaceApplication,
		WorkspaceUseCase:  service.components.workspace,
		AddNotice:         service.addNotice,
		PublishRuntime:    service.publishRuntimeProjections,
		CloseRoleToolRecord: func(mainSessionID, roleSessionID string) {
			service.closeUnitToolRecord(mainSessionID, applicationtoolrecord.RoleUnitID(roleSessionID, ""))
		},
	}
}
