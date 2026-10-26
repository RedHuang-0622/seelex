package adapters

import (
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/kernel/rolesessions"
)

var _ contract.RoleSessionsPort = rolesessions.RoleSessions{}

// NewRoleSessionsFactory discovers the optional role-session and schedule
// capabilities and returns the Backend-owned facade.
func NewRoleSessionsFactory() func(contract.SessionPort) contract.RoleSessionsPort {
	return func(sessions contract.SessionPort) contract.RoleSessionsPort {
		if sessions == nil {
			return rolesessions.RoleSessions{}
		}
		roles, _ := sessions.(contract.RoleSessionPort)
		schedule, _ := sessions.(contract.SchedulePort)
		return rolesessions.RoleSessions{Roles: roles, Schedule: schedule, Assembled: true}
	}
}
