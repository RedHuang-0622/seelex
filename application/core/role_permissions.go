package core

import (
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// role_permissions.go 是「角色权限登记」的应用层读取面：会话加载时把每个角色的
// 权限恢复出来，**没有登记的按四级链路解析**。
//
// 生态位（为什么这里只有读面、没有第二份内存表）：
//   - 登记的**事实源**只有一处——存储层的会话级角色权限登记（键 (sessionID, role)，
//     见 store 的 RolePermissionStore）。那里每个角色一格、各自持锁，跨角色并发
//     读写不会落到同一把锁或同一张裸 map 上；
//   - 四级优先级的**判据**也只有一处——`dto.ResolvePermissionVector` /
//     `dto.ResolveRoleSettings`；
//   - 本文件因此只做装配缝：把会话端口上可选的两项能力（登记读写、四层来源）类型
//     断言出来，按"登记优先、缺失走链路"合成为每角色的生效设置。
//
// 刻意**不**在应用层缓存一份权限向量：那会变成第二份事实源，且两份之间没有任何
// 失效协议（改一处不改另一处 = 判定链前后不一致）。需要提速就在存储层的登记格里
// 缓存（本进程权威、写入即更新，已实现）。
//
// 解析结果也**不**写回盘：配置文件改了应当在下一次加载生效，而不是被一次解析
// 冻结成用户数据。

// ErrRolePermissionsUnavailable 表示会话端口没有暴露角色权限登记能力（装配缺口）。
//
// 显式失败而不是静默返回空权限：空权限与"装配漏了"在界面上长得一样，但一个是
// 用户的选择、一个是宿主的缺陷。与 `connectionPort()` 缺失时的口径一致。
var ErrRolePermissionsUnavailable = errors.New("role permissions: session port does not expose the role permission registry")

// rolePermissionPort 是会话端口上的**可选**能力：会话级角色权限登记（键 (sessionID, role)）。
//
// 接口定义在消费方（本包）：契约层的 `contract.SessionPort` 是稳定面，不为一个
// 尚在接入的能力扩成员；装配处（wiring/adapters）在实现会话端口的同时实现本接口，
// 与 `contract.RoleSessionPort` 的发现方式同款（类型断言，缺失即不可用）。
type rolePermissionPort interface {
	// RolePermissionVectors 返回该会话下所有**显式登记过**的角色 → 权限向量。
	RolePermissionVectors(sessionID string) (map[string][]string, error)
	// RolePermissionVector 读单个角色；第二个返回值报告"是否登记过"（区分
	// "显式空集"与"从未定过"）。
	RolePermissionVector(sessionID, role string) ([]string, bool, error)
	// SetRolePermissionVector 写入一个角色的权限向量（空集合法，含义是显式不给）。
	SetRolePermissionVector(sessionID, role string, vector []string) error
}

// rolePermissionLayerPort 是四级来源的读取面（user_allow / agent_dispatch /
// session_default / global_default）。
//
// 由装配侧按会话与角色给出：user_allow 来自用户设置、agent_dispatch 来自派活的
// 那一方、session_default 来自会话设置、global_default 来自配置派生。本包只消费
// 已经排好序的一份，不自己读配置——否则"默认值从哪来"就会有第二份答案。
type rolePermissionLayerPort interface {
	RolePermissionLayers(sessionID, role string) dto.RoleSettingsLayers
}

// rolePermissionStore 取登记端口（未装配返回 nil）。
func (service *Service) rolePermissionStore() rolePermissionPort {
	if service == nil || service.Deps.Sessions == nil {
		return nil
	}
	port, _ := service.Deps.Sessions.(rolePermissionPort)
	return port
}

// rolePermissionLayers 取四层来源端口（未装配返回 nil）。
func (service *Service) rolePermissionLayers() rolePermissionLayerPort {
	if service == nil || service.Deps.Sessions == nil {
		return nil
	}
	port, _ := service.Deps.Sessions.(rolePermissionLayerPort)
	return port
}

// RestoreRolePermissions 是**会话加载点**的恢复入口：为 roles 里的每个角色恢复
// 生效设置——先读登记，登记缺失则按四级链路解析。
//
// 参数 roles 是该会话在装配角色时认定的角色名列表（去重由调用方保证；这里只跳过
// 空白项）。返回的 map 键是规整后的角色名，每份设置的 PermissionVector 非 nil。
//
// 未装配登记能力时返回 ErrRolePermissionsUnavailable（不静默给"空权限"）。
func (service *Service) RestoreRolePermissions(sessionID string, roles []string) (map[string]dto.RoleSettings, error) {
	port := service.rolePermissionStore()
	if port == nil {
		return nil, ErrRolePermissionsUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("role permissions: session id is required")
	}
	stored, err := port.RolePermissionVectors(sessionID)
	if err != nil {
		return nil, err
	}
	layers := service.rolePermissionLayers()
	restored := make(map[string]dto.RoleSettings, len(roles))
	for _, raw := range roles {
		role := strings.TrimSpace(raw)
		if role == "" {
			continue
		}
		settings := dto.RoleSettings{}
		if layers != nil {
			// 层序（高→低）由 dto.RoleSettingsLayers.Ordered 定义，本包不重复一遍。
			settings = dto.ResolveRoleSettings(layers.RolePermissionLayers(sessionID, role).Ordered()...)
		}
		// 登记优先：登记过就是"显式定过"，哪怕向量是空集——不能回落到链路
		// （回落方向是权限放大）。
		if vector, ok := stored[role]; ok {
			settings.PermissionVector = vector
		}
		if settings.PermissionVector == nil {
			settings.PermissionVector = []string{}
		}
		restored[role] = settings
	}
	return restored, nil
}

// RolePermissionVector 读单个角色的生效权限向量（登记优先、缺失走链路）。
//
// 与 RestoreRolePermissions 同一判据的**单角色形态**：工具判定这类高频读点用它，
// 避免为了一个角色把整会话的角色都算一遍。
func (service *Service) RolePermissionVector(sessionID, role string) ([]string, error) {
	port := service.rolePermissionStore()
	if port == nil {
		return nil, ErrRolePermissionsUnavailable
	}
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return nil, errors.New("role permissions: session id and role are required")
	}
	vector, stored, err := port.RolePermissionVector(sessionID, role)
	if err != nil {
		return nil, err
	}
	if stored {
		if vector == nil {
			return []string{}, nil
		}
		return vector, nil
	}
	layers := service.rolePermissionLayers()
	if layers == nil {
		return []string{}, nil
	}
	resolved := dto.ResolveRoleSettings(layers.RolePermissionLayers(sessionID, role).Ordered()...)
	return resolved.PermissionVector, nil
}

// SetRolePermissionVector 写入一个角色的权限向量（空切片 = 显式不给任何能力，
// 与"删除登记"是两件事：删除由存储层的 Drop 负责）。
//
// 写入侧先规整再落盘：非法项显式报错，绝不静默丢项（丢项等于用户以为给了能力）。
func (service *Service) SetRolePermissionVector(sessionID, role string, vector []string) error {
	port := service.rolePermissionStore()
	if port == nil {
		return ErrRolePermissionsUnavailable
	}
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return errors.New("role permissions: session id and role are required")
	}
	normalized, err := dto.NormalizePermissionVector(vector)
	if err != nil {
		return err
	}
	return port.SetRolePermissionVector(sessionID, role, normalized)
}
