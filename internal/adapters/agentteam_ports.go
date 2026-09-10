// agentteam_ports.go：SessionPort 的 A2A 角色管理面（角色注册表 + 顺序读）。
//
// 边界：这里只做 sessionstore ↔ application/contract/dto 的形态转换与项目作用域
// 路由；排序、幂等与落盘语义全在 sessionstore。application 不 import sessionstore。
package adapters

import (
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// EnsureRoleSession 幂等创建角色会话（供 AgentTeamFactory 重复装配同一条 TeamSpec）。
func (port SessionPort) EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error) {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return false, err
	}
	return router.EnsureRoleSessionWorkspace(projectID, mainSessionID, roleName, roleSessionID, joinSeq)
}

// ReadLifecycleOrder 读群聊顺序策略（lifecycle head 是运行时权威）。
func (port SessionPort) ReadLifecycleOrder(sessionID string) (string, []string, error) {
	router, projectID, err := port.roleRouter(sessionID)
	if err != nil {
		return "", nil, err
	}
	return router.ReadLifecycleOrderWorkspace(projectID, sessionID)
}

// ReadTeamRegistry 读角色注册表并映射为应用层 DTO（不把 sessionstore 类型漏出去）。
func (port SessionPort) ReadTeamRegistry(mainSessionID string) (dto.TeamRegistry, error) {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	stored, err := router.ReadTeamRegistryWorkspace(projectID, mainSessionID)
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	return teamRegistryToDTO(stored), nil
}

// WriteTeamRegistry 把应用层 DTO 写回角色注册表（整份替换型，只有 sessionstore
// 决定落盘形态）。
func (port SessionPort) WriteTeamRegistry(mainSessionID string, registry dto.TeamRegistry) error {
	if strings.TrimSpace(mainSessionID) == "" {
		return errors.New("main session ID is required")
	}
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return err
	}
	return router.WriteTeamRegistryWorkspace(projectID, mainSessionID, teamRegistryFromDTO(registry))
}

func teamRegistryToDTO(stored sessionstore.TeamRegistry) dto.TeamRegistry {
	roles := make([]dto.RoleSpec, 0, len(stored.Roles))
	for _, role := range stored.Roles {
		roles = append(roles, dto.RoleSpec{
			RoleName:        role.RoleName,
			RoleKind:        dto.RoleKind(role.RoleKind),
			SystemPrompt:    role.SystemPrompt,
			ModelPolicy:     role.ModelPolicy,
			MirrorPolicy:    append([]string(nil), role.MirrorPolicy...),
			DirectiveSchema: append([]string(nil), role.DirectiveSchema...),
			OrderPriority:   role.OrderPriority,
			JoinPolicy:      role.JoinPolicy,
			PresencePolicy:  role.PresencePolicy,
			ToolsPolicy:     role.ToolsPolicy,
		})
	}
	return dto.TeamRegistry{
		TeamID:      stored.TeamID,
		TeamKind:    stored.TeamKind,
		OrderPolicy: stored.OrderPolicy,
		Roles:       roles,
		Configured:  stored.Configured,
		UpdatedAt:   stored.UpdatedAt,
	}
}

func teamRegistryFromDTO(registry dto.TeamRegistry) sessionstore.TeamRegistry {
	roles := make([]sessionstore.TeamRoleSpec, 0, len(registry.Roles))
	for _, role := range registry.Roles {
		roles = append(roles, sessionstore.TeamRoleSpec{
			RoleName:        role.RoleName,
			RoleKind:        string(role.RoleKind),
			SystemPrompt:    role.SystemPrompt,
			ModelPolicy:     role.ModelPolicy,
			MirrorPolicy:    append([]string(nil), role.MirrorPolicy...),
			DirectiveSchema: append([]string(nil), role.DirectiveSchema...),
			OrderPriority:   role.OrderPriority,
			JoinPolicy:      role.JoinPolicy,
			PresencePolicy:  role.PresencePolicy,
			ToolsPolicy:     role.ToolsPolicy,
		})
	}
	return sessionstore.TeamRegistry{
		TeamID:      registry.TeamID,
		TeamKind:    registry.TeamKind,
		OrderPolicy: registry.OrderPolicy,
		Roles:       roles,
	}
}
