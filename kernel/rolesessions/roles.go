package rolesessions

// roles.go 把 R2/R4 的会话存储基建接到"角色会话 + 定时插话"两个**可选**端口。
//
// 边界：只做端口解析与窄转发，不解释 role draft/顺序/floor 语义；真正的排序、幂等、
// 同步即删与 head 发布仍由存储侧 sequencer 入口执行。
//
// 落点沿革：本文件于 2026-10-16 随 M5-R8d **接收者转移**从
// `application/core/role_session.go` 归位（plan §2.11.1 的 `role_session.go` → `backend/multiagent/team/`）。
// 端口与 DTO 全部来自 `contract`：本包不出现存储类型（`store.*`），GUI/headless 也随之
// 只消费应用层 DTO。门面侧只负责把 `Deps.Sessions` 上的两个可选能力**类型断言**出来再交进来。
//
// 错误文案（判据）只此一份：
//   - 会话端口本身没装配 → "role session storage is not assembled"；
//   - 装配了但没暴露对应能力 → "session port does not expose role session storage" /
//     "session port does not expose schedule events"。

import (
	"errors"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// RoleSessions 是"角色会话 + 定时插话"两个可选端口的窄门面。
//
// Assembled 表示会话端口**本身**是否装配（false = 两个端口都不可用，错误文案不同）；
// 两个端口各自可为 nil（装配了但没暴露那一项能力）。
type RoleSessions struct {
	Roles     contract.RoleSessionPort
	Schedule  contract.SchedulePort
	Assembled bool
}

// RolePort 返回角色会话端口。
func (r RoleSessions) RolePort() (contract.RoleSessionPort, error) {
	if !r.Assembled {
		return nil, errors.New("role session storage is not assembled")
	}
	if r.Roles == nil {
		return nil, errors.New("session port does not expose role session storage")
	}
	return r.Roles, nil
}

// SchedulePort 返回定时插话端口。
func (r RoleSessions) SchedulePort() (contract.SchedulePort, error) {
	if !r.Assembled {
		return nil, errors.New("role session storage is not assembled")
	}
	if r.Schedule == nil {
		return nil, errors.New("session port does not expose schedule events")
	}
	return r.Schedule, nil
}

func (r RoleSessions) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error) {
	port, err := r.RolePort()
	if err != nil {
		return dto.RoleSessionInfo{}, err
	}
	return port.CreateRoleSession(mainSessionID, roleName, roleSessionID, joinSeq)
}

func (r RoleSessions) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error {
	port, err := r.RolePort()
	if err != nil {
		return err
	}
	return port.AppendRoleDraft(mainSessionID, roleName, roleSessionID, rows)
}

func (r RoleSessions) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error) {
	port, err := r.RolePort()
	if err != nil {
		return nil, err
	}
	return port.ReadRoleDraft(mainSessionID, roleName, roleSessionID)
}

func (r RoleSessions) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error) {
	port, err := r.RolePort()
	if err != nil {
		return dto.RoleDraftSyncResult{}, err
	}
	return port.SyncRoleDraft(mainSessionID, roleName, roleSessionID, order)
}

func (r RoleSessions) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error {
	port, err := r.RolePort()
	if err != nil {
		return err
	}
	return port.AppendRoleSessionRows(mainSessionID, roleName, roleSessionID, rows)
}

func (r RoleSessions) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error) {
	port, err := r.RolePort()
	if err != nil {
		return nil, err
	}
	return port.ReadRoleSessionRows(mainSessionID, roleName, roleSessionID)
}

func (r RoleSessions) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	port, err := r.RolePort()
	if err != nil {
		return dto.RoleSnapshot{}, err
	}
	return port.RoleSnapshot(mainSessionID, roleName, roleSessionID)
}

func (r RoleSessions) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error) {
	port, err := r.RolePort()
	if err != nil {
		return dto.RoleWireSnapshot{}, err
	}
	return port.AssembleRoleWire(mainSessionID, roleName, roleSessionID, budget, k)
}

func (r RoleSessions) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	port, err := r.RolePort()
	if err != nil {
		return err
	}
	return port.SetLifecycleOrder(sessionID, policy, roles)
}

func (r RoleSessions) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error {
	port, err := r.RolePort()
	if err != nil {
		return err
	}
	return port.SetRoleLifecycle(mainSessionID, roleName, roleSessionID, joinSeq, ref)
}

func (r RoleSessions) ListRoleSessions(mainSessionID string) ([]string, error) {
	port, err := r.RolePort()
	if err != nil {
		return nil, err
	}
	return port.ListRoleSessions(mainSessionID)
}

func (r RoleSessions) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error {
	port, err := r.SchedulePort()
	if err != nil {
		return err
	}
	return port.ScheduleRegister(sessionID, payload)
}

func (r RoleSessions) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error {
	port, err := r.SchedulePort()
	if err != nil {
		return err
	}
	return port.ScheduleCancel(sessionID, payload)
}

func (r RoleSessions) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error {
	port, err := r.SchedulePort()
	if err != nil {
		return err
	}
	return port.ScheduleFire(sessionID, payload)
}
