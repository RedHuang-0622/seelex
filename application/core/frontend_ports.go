package core

import (
	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
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
	History           *applicationhistory.Application
	Policy            *applicationpolicy.SwitchApplication
	Role              *applicationpolicy.RoleApplication
	RoleSessions      team.RoleSessions
	SessionDelete     *applicationsession.DeleteApplication
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
		History:           service.components.conversationHistoryQuery,
		Policy:            service.components.policyApplication,
		Role:              service.components.roleApplication,
		RoleSessions:      service.roleSessions(),
		SessionDelete:     service.components.sessionDelete,
		Workspace:         service.components.workspaceApplication,
		WorkspaceUseCase:  service.components.workspace,
	}
}
