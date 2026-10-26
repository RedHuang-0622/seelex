package core

import (
	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	conversationsearch "github.com/RedHuang-0622/seelex/application/conversation/search"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationobservability "github.com/RedHuang-0622/seelex/application/observability"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	applicationreference "github.com/RedHuang-0622/seelex/application/reference"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	applicationsessionquery "github.com/RedHuang-0622/seelex/application/session/query"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	"github.com/RedHuang-0622/seelex/backend/multiagent/team"
	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

// FrontendPorts is the assembly snapshot consumed by application/compat.
// It exposes only already-extracted application components; core keeps state,
// locking, publication, and host adapters.
type FrontendPorts struct {
	Admin             *applicationadmin.AccountApplication
	Completion        *applicationinput.Application
	ConversationQueue *applicationconversation.QueueApplication
	Execution         *applicationexecution.WorkItemApplication
	History           *applicationhistory.Application
	HistorySearch     *conversationsearch.Application
	Perf              *applicationobservability.PerfApplication
	Policy            *applicationpolicy.SwitchApplication
	ProjectBoard      *applicationexecution.ProjectBoardApplication
	Reference         *applicationreference.Application
	Role              *applicationpolicy.RoleApplication
	RoleSessions      team.RoleSessions
	SessionDelete     *applicationsession.DeleteApplication
	SessionMeta       *applicationsession.MetaApplication
	SessionQuery      *applicationsessionquery.Application
	Workspace         *applicationworkspace.Application
	WorkspaceUseCase  *switchworkspace.UseCase
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
		ConversationQueue: service.components.conversationQueue,
		Execution:         service.components.executionApplication,
		History:           service.components.conversationHistoryQuery,
		HistorySearch:     service.components.historySearchApplication,
		Perf:              service.components.perfStatsApplication,
		Policy:            service.components.policyApplication,
		ProjectBoard:      service.components.projectBoardQuery,
		Reference:         service.components.referenceApplication,
		Role:              service.components.roleApplication,
		RoleSessions:      service.roleSessions(),
		SessionDelete:     service.components.sessionDelete,
		SessionMeta:       service.components.sessionMetaApplication,
		SessionQuery:      service.components.sessionQuery,
		Workspace:         service.components.workspaceApplication,
		WorkspaceUseCase:  service.components.workspace,
	}
}
