// role_session.go 是角色会话 / 定时插话门面的**端口解析 + 一行转发**。
//
// 判据与转发实现已在 M5-R8d 随接收者转移归位 `backend/multiagent/team.RoleSessions`
// （plan §2.11.1）；本文件只剩门面自己的职责：把 `Deps.Sessions` 上的两个**可选**能力
// 类型断言出来交给 team 侧（断言本身是装配缝，不是域判据）。
//
// S27 收口（历史结论，保留）：本文件的端口与 DTO 全部来自 contract，本包不再出现
// 存储类型（store.*），GUI/headless 也随之只消费应用层 DTO。
package core

import (
	"github.com/RedHuang-0622/seelex/backend/multiagent/team"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// roleSessions 把会话端口上可选的两项能力断言出来（两者都由 wiring/adapters.SessionPort
// 在装配期一并提供）。`Assembled` 区分"会话端口本身没装配"与"装配了但没暴露这一项能力"——
// 两者错误文案不同，判据在 team 侧。
func (service *Service) roleSessions() team.RoleSessions {
	if service == nil || service.Deps.Sessions == nil {
		return team.RoleSessions{}
	}
	roles, _ := service.Deps.Sessions.(contract.RoleSessionPort)
	schedule, _ := service.Deps.Sessions.(contract.SchedulePort)
	return team.RoleSessions{Roles: roles, Schedule: schedule, Assembled: true}
}

func (service *Service) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error) {
	return service.roleSessions().CreateRoleSession(mainSessionID, roleName, roleSessionID, joinSeq)
}

func (service *Service) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error {
	return service.roleSessions().AppendRoleDraft(mainSessionID, roleName, roleSessionID, rows)
}

func (service *Service) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error) {
	return service.roleSessions().ReadRoleDraft(mainSessionID, roleName, roleSessionID)
}

func (service *Service) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error) {
	return service.roleSessions().SyncRoleDraft(mainSessionID, roleName, roleSessionID, order)
}

func (service *Service) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error {
	return service.roleSessions().AppendRoleSessionRows(mainSessionID, roleName, roleSessionID, rows)
}

func (service *Service) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error) {
	return service.roleSessions().ReadRoleSessionRows(mainSessionID, roleName, roleSessionID)
}

func (service *Service) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	return service.roleSessions().RoleSnapshot(mainSessionID, roleName, roleSessionID)
}

func (service *Service) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error) {
	return service.roleSessions().AssembleRoleWire(mainSessionID, roleName, roleSessionID, budget, k)
}

func (service *Service) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	return service.roleSessions().SetLifecycleOrder(sessionID, policy, roles)
}

func (service *Service) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error {
	return service.roleSessions().SetRoleLifecycle(mainSessionID, roleName, roleSessionID, joinSeq, ref)
}

func (service *Service) ListRoleSessions(mainSessionID string) ([]string, error) {
	return service.roleSessions().ListRoleSessions(mainSessionID)
}

func (service *Service) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error {
	return service.roleSessions().ScheduleRegister(sessionID, payload)
}

func (service *Service) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error {
	return service.roleSessions().ScheduleCancel(sessionID, payload)
}

func (service *Service) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error {
	return service.roleSessions().ScheduleFire(sessionID, payload)
}
