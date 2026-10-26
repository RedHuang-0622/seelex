package adapters

import (
	"github.com/RedHuang-0622/seelex/backend/multiagent/team"
	"github.com/RedHuang-0622/seelex/contract"
)

var _ contract.RoleSessionsPort = team.RoleSessions{}

// NewRoleSessionsFactory discovers the optional role-session and schedule
// capabilities and returns the Backend-owned facade.
func NewRoleSessionsFactory() func(contract.SessionPort) contract.RoleSessionsPort {
	return func(sessions contract.SessionPort) contract.RoleSessionsPort {
		if sessions == nil {
			return team.RoleSessions{}
		}
		roles, _ := sessions.(contract.RoleSessionPort)
		schedule, _ := sessions.(contract.SchedulePort)
		return team.RoleSessions{Roles: roles, Schedule: schedule, Assembled: true}
	}
}
