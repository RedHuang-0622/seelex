// role_session.go 把 R2/R4 的会话存储基建接到 Application 可选能力面。
//
// 边界：Application 只做窄转发，不解释 role draft/顺序/floor 语义；真正的
// 排序、幂等、同步即删与 head 发布仍由 sessionstore 的 sequencer 入口执行。
package core

import (
	"errors"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// roleSessionPort 是会话端口可选实现的群聊角色能力面。生产实现为
// internal/adapters.SessionPort；测试桩未实现时显式返回不可用。
type roleSessionPort interface {
	CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (sessionstore.RoleSessionInfo, error)
	AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []sessionstore.RoleDraftRow) error
	ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]sessionstore.RoleDraftRow, error)
	SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (sessionstore.RoleDraftSyncResult, error)
	AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []sessionstore.Event) error
	ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]sessionstore.Event, error)
	RoleSnapshot(mainSessionID, roleName, roleSessionID string) (sessionstore.RoleSnapshot, error)
	AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (sessionstore.RoleWireSnapshot, error)
	SetLifecycleOrder(sessionID, policy string, roles []string) error
	SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *sessionstore.CompactRef) error
	ListRoleSessions(mainSessionID string) ([]string, error)
	ScheduleRegister(sessionID string, payload sessionstore.ScheduleEventPayload) error
	ScheduleCancel(sessionID string, payload sessionstore.ScheduleEventPayload) error
	ScheduleFire(sessionID string, payload sessionstore.ScheduleEventPayload) error
}

func (service *Service) roleSessionPort() (roleSessionPort, error) {
	if service == nil || service.Deps.Sessions == nil {
		return nil, errors.New("role session storage is not assembled")
	}
	port, ok := service.Deps.Sessions.(roleSessionPort)
	if !ok {
		return nil, errors.New("session port does not expose role session storage")
	}
	return port, nil
}

func (service *Service) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (sessionstore.RoleSessionInfo, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return sessionstore.RoleSessionInfo{}, err
	}
	return port.CreateRoleSession(mainSessionID, roleName, roleSessionID, joinSeq)
}

func (service *Service) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []sessionstore.RoleDraftRow) error {
	port, err := service.roleSessionPort()
	if err != nil {
		return err
	}
	return port.AppendRoleDraft(mainSessionID, roleName, roleSessionID, rows)
}

func (service *Service) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]sessionstore.RoleDraftRow, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return nil, err
	}
	return port.ReadRoleDraft(mainSessionID, roleName, roleSessionID)
}

func (service *Service) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (sessionstore.RoleDraftSyncResult, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return sessionstore.RoleDraftSyncResult{}, err
	}
	return port.SyncRoleDraft(mainSessionID, roleName, roleSessionID, order)
}

func (service *Service) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []sessionstore.Event) error {
	port, err := service.roleSessionPort()
	if err != nil {
		return err
	}
	return port.AppendRoleSessionRows(mainSessionID, roleName, roleSessionID, rows)
}

func (service *Service) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]sessionstore.Event, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return nil, err
	}
	return port.ReadRoleSessionRows(mainSessionID, roleName, roleSessionID)
}

func (service *Service) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (sessionstore.RoleSnapshot, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return sessionstore.RoleSnapshot{}, err
	}
	return port.RoleSnapshot(mainSessionID, roleName, roleSessionID)
}

func (service *Service) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (sessionstore.RoleWireSnapshot, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return sessionstore.RoleWireSnapshot{}, err
	}
	return port.AssembleRoleWire(mainSessionID, roleName, roleSessionID, budget, k)
}

func (service *Service) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	port, err := service.roleSessionPort()
	if err != nil {
		return err
	}
	return port.SetLifecycleOrder(sessionID, policy, roles)
}

func (service *Service) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *sessionstore.CompactRef) error {
	port, err := service.roleSessionPort()
	if err != nil {
		return err
	}
	return port.SetRoleLifecycle(mainSessionID, roleName, roleSessionID, joinSeq, ref)
}

func (service *Service) ListRoleSessions(mainSessionID string) ([]string, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return nil, err
	}
	return port.ListRoleSessions(mainSessionID)
}

func (service *Service) ScheduleRegister(sessionID string, payload sessionstore.ScheduleEventPayload) error {
	port, err := service.roleSessionPort()
	if err != nil {
		return err
	}
	return port.ScheduleRegister(sessionID, payload)
}

func (service *Service) ScheduleCancel(sessionID string, payload sessionstore.ScheduleEventPayload) error {
	port, err := service.roleSessionPort()
	if err != nil {
		return err
	}
	return port.ScheduleCancel(sessionID, payload)
}

func (service *Service) ScheduleFire(sessionID string, payload sessionstore.ScheduleEventPayload) error {
	port, err := service.roleSessionPort()
	if err != nil {
		return err
	}
	return port.ScheduleFire(sessionID, payload)
}
