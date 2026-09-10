// role_session.go 把 R2/R4 的会话存储基建接到 Application 可选能力面。
//
// 边界：Application 只做窄转发，不解释 role draft/顺序/floor 语义；真正的
// 排序、幂等、同步即删与 head 发布仍由存储侧 sequencer 入口执行。
//
// S27 收口：本文件的端口与 DTO 全部来自 application/contract，本包不再出现
// 存储类型（sessionstore.*），GUI/headless 也随之只消费应用层 DTO。
package core

import (
	"errors"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// rolePorts 聚合角色会话与定时插话两个可选端口；两者都由会话端口实现在
// 装配期一并提供（internal/adapters.SessionPort）。
type rolePorts struct {
	roles    contract.RoleSessionPort
	schedule contract.SchedulePort
}

func (service *Service) rolePorts() (rolePorts, error) {
	if service == nil || service.Deps.Sessions == nil {
		return rolePorts{}, errors.New("role session storage is not assembled")
	}
	var ports rolePorts
	if roles, ok := service.Deps.Sessions.(contract.RoleSessionPort); ok {
		ports.roles = roles
	}
	if schedule, ok := service.Deps.Sessions.(contract.SchedulePort); ok {
		ports.schedule = schedule
	}
	if ports.roles == nil && ports.schedule == nil {
		return rolePorts{}, errors.New("session port does not expose role session storage")
	}
	return ports, nil
}

func (service *Service) roleSessionPort() (contract.RoleSessionPort, error) {
	ports, err := service.rolePorts()
	if err != nil {
		return nil, err
	}
	if ports.roles == nil {
		return nil, errors.New("session port does not expose role session storage")
	}
	return ports.roles, nil
}

func (service *Service) schedulePort() (contract.SchedulePort, error) {
	ports, err := service.rolePorts()
	if err != nil {
		return nil, err
	}
	if ports.schedule == nil {
		return nil, errors.New("session port does not expose schedule events")
	}
	return ports.schedule, nil
}

func (service *Service) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return dto.RoleSessionInfo{}, err
	}
	return port.CreateRoleSession(mainSessionID, roleName, roleSessionID, joinSeq)
}

func (service *Service) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error {
	port, err := service.roleSessionPort()
	if err != nil {
		return err
	}
	return port.AppendRoleDraft(mainSessionID, roleName, roleSessionID, rows)
}

func (service *Service) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return nil, err
	}
	return port.ReadRoleDraft(mainSessionID, roleName, roleSessionID)
}

func (service *Service) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return dto.RoleDraftSyncResult{}, err
	}
	return port.SyncRoleDraft(mainSessionID, roleName, roleSessionID, order)
}

func (service *Service) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error {
	port, err := service.roleSessionPort()
	if err != nil {
		return err
	}
	return port.AppendRoleSessionRows(mainSessionID, roleName, roleSessionID, rows)
}

func (service *Service) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return nil, err
	}
	return port.ReadRoleSessionRows(mainSessionID, roleName, roleSessionID)
}

func (service *Service) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return dto.RoleSnapshot{}, err
	}
	return port.RoleSnapshot(mainSessionID, roleName, roleSessionID)
}

func (service *Service) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error) {
	port, err := service.roleSessionPort()
	if err != nil {
		return dto.RoleWireSnapshot{}, err
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

func (service *Service) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error {
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

func (service *Service) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error {
	port, err := service.schedulePort()
	if err != nil {
		return err
	}
	return port.ScheduleRegister(sessionID, payload)
}

func (service *Service) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error {
	port, err := service.schedulePort()
	if err != nil {
		return err
	}
	return port.ScheduleCancel(sessionID, payload)
}

func (service *Service) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error {
	port, err := service.schedulePort()
	if err != nil {
		return err
	}
	return port.ScheduleFire(sessionID, payload)
}
