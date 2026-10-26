package contract

import "github.com/RedHuang-0622/seelex/contract/dto"

// RoleSessionsPort is the Application-facing role-session and scheduled-
// interjection surface. The Backend implementation owns optional-port
// discovery and the exact unavailable errors.
type RoleSessionsPort interface {
	CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error)
	AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error
	ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error)
	SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error)
	AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error
	ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error)
	RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error)
	AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error)
	SetLifecycleOrder(sessionID, policy string, roles []string) error
	SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error
	ListRoleSessions(mainSessionID string) ([]string, error)
	ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error
}
