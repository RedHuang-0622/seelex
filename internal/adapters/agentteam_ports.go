// agentteam_ports.go：SessionPort 的 A2A 角色管理面（角色注册表 + 顺序读 +
// 全局母本：团队库 / 员工库 / 默认顺序）。
//
// 边界：这里只做 sessionstore ↔ application/contract/dto 的形态转换与作用域路由；
// 排序、幂等与落盘语义全在 sessionstore。application 不 import sessionstore。
//
// 作用域：会话在编员工表与发言顺序是**会话级**；团队库/员工库/默认顺序是**全局级**
// （数据根下 `team/`，见 sessionstore/team_global.go）。会话号只用于解析数据根与
// 旧布局回退的锚定项目。
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

// ReadFloorRole 读主会话 message head 的 floor 角色名（当前发言角色）。空串 =
// 该会话还没发生过一次角色 draft sync（"还没有人发言"），不是错误。
func (port SessionPort) ReadFloorRole(mainSessionID string) (string, error) {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return "", err
	}
	floor, err := router.ReadMessageFloorWorkspace(projectID, mainSessionID)
	if err != nil {
		return "", err
	}
	if floor == nil {
		return "", nil
	}
	return floor.RoleName, nil
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

// RemoveTeamRegistry 删除角色注册表（团队离场；幂等）。与 WriteTeamRegistry 同一份
// 项目作用域解析：离场改变的只是"本会话还有没有在编团队"，不是角色会话本身。
func (port SessionPort) RemoveTeamRegistry(mainSessionID string) error {
	if strings.TrimSpace(mainSessionID) == "" {
		return errors.New("main session ID is required")
	}
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return err
	}
	return router.RemoveTeamRegistryWorkspace(projectID, mainSessionID)
}

// teamRoleSpecToDTO / teamRoleSpecFromDTO 是角色配置的唯一映射点：注册表、团队库
// 条目、员工库三处都复用，避免同一份字段口径散成多份（提示词/权限最容易漏）。
func teamRoleSpecToDTO(role sessionstore.TeamRoleSpec) dto.RoleSpec {
	return dto.RoleSpec{
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
	}
}

func teamRoleSpecFromDTO(role dto.RoleSpec) sessionstore.TeamRoleSpec {
	return sessionstore.TeamRoleSpec{
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
	}
}

func teamRoleSpecsToDTO(roles []sessionstore.TeamRoleSpec) []dto.RoleSpec {
	out := make([]dto.RoleSpec, 0, len(roles))
	for _, role := range roles {
		out = append(out, teamRoleSpecToDTO(role))
	}
	return out
}

func teamRoleSpecsFromDTO(roles []dto.RoleSpec) []sessionstore.TeamRoleSpec {
	out := make([]sessionstore.TeamRoleSpec, 0, len(roles))
	for _, role := range roles {
		out = append(out, teamRoleSpecFromDTO(role))
	}
	return out
}

func teamRegistryToDTO(stored sessionstore.TeamRegistry) dto.TeamRegistry {
	return dto.TeamRegistry{
		TeamID:      stored.TeamID,
		TeamKind:    stored.TeamKind,
		OrderPolicy: stored.OrderPolicy,
		Roles:       teamRoleSpecsToDTO(stored.Roles),
		Configured:  stored.Configured,
		UpdatedAt:   stored.UpdatedAt,
	}
}

// ReadTeamLibrary 读**全局**团队库（全局粒度，见 sessionstore/team_global.go）。
// 会话号只用于解析数据根与旧布局回退的锚定项目，不改变"全局库"的读语义。
func (port SessionPort) ReadTeamLibrary(mainSessionID string) (dto.TeamLibrary, error) {
	router, projectID, err := port.roleRouter(mainSessionID)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	stored, err := router.ReadTeamLibraryGlobal(projectID)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	return teamLibraryToDTO(stored), nil
}

// WriteTeamLibrary 整份写入**全局**团队库。
func (port SessionPort) WriteTeamLibrary(mainSessionID string, library dto.TeamLibrary) error {
	router, _, err := port.roleRouter(mainSessionID)
	if err != nil {
		return err
	}
	return router.WriteTeamLibraryGlobal(teamLibraryFromDTO(library))
}

// ReadEmployeeLibrary 读全局员工库（全局粒度的员工名册）。与会话在编员工表
// （session/team/roles.json）是"母本 vs 副本"的关系。
func (port SessionPort) ReadEmployeeLibrary(mainSessionID string) (dto.EmployeeLibrary, error) {
	router, _, err := port.roleRouter(mainSessionID)
	if err != nil {
		return dto.EmployeeLibrary{}, err
	}
	stored, err := router.ReadEmployeeLibraryGlobal()
	if err != nil {
		return dto.EmployeeLibrary{}, err
	}
	return employeeLibraryToDTO(stored), nil
}

// WriteEmployeeLibrary 整份写入全局员工库。
func (port SessionPort) WriteEmployeeLibrary(mainSessionID string, library dto.EmployeeLibrary) error {
	router, _, err := port.roleRouter(mainSessionID)
	if err != nil {
		return err
	}
	return router.WriteEmployeeLibraryGlobal(employeeLibraryFromDTO(library))
}

// ReadDefaultOrder 读全局默认顺序。
func (port SessionPort) ReadDefaultOrder(mainSessionID string) (dto.DefaultOrder, error) {
	router, _, err := port.roleRouter(mainSessionID)
	if err != nil {
		return dto.DefaultOrder{}, err
	}
	stored, err := router.ReadDefaultOrderGlobal()
	if err != nil {
		return dto.DefaultOrder{}, err
	}
	return defaultOrderToDTO(stored), nil
}

// WriteDefaultOrder 整份写入全局默认顺序。
func (port SessionPort) WriteDefaultOrder(mainSessionID string, order dto.DefaultOrder) error {
	router, _, err := port.roleRouter(mainSessionID)
	if err != nil {
		return err
	}
	return router.WriteDefaultOrderGlobal(defaultOrderFromDTO(order))
}

func teamLibraryToDTO(stored sessionstore.TeamLibrary) dto.TeamLibrary {
	teams := make([]dto.TeamLibraryEntry, 0, len(stored.Teams))
	for _, entry := range stored.Teams {
		teams = append(teams, teamLibraryEntryToDTO(entry))
	}
	return dto.TeamLibrary{Teams: teams, Configured: stored.Configured}
}

func teamLibraryEntryToDTO(entry sessionstore.TeamLibraryEntry) dto.TeamLibraryEntry {
	return dto.TeamLibraryEntry{
		TeamID:        entry.TeamID,
		TeamKind:      entry.TeamKind,
		Name:          entry.Name,
		OrderPolicy:   entry.OrderPolicy,
		OrderRoles:    append([]string(nil), entry.OrderRoles...),
		Roles:         teamRoleSpecsToDTO(entry.Roles),
		GatePolicy:    entry.GatePolicy,
		CompactPolicy: entry.CompactPolicy,
		Origin:        entry.Origin,
		UpdatedAt:     entry.UpdatedAt,
	}
}

func teamLibraryFromDTO(library dto.TeamLibrary) sessionstore.TeamLibrary {
	teams := make([]sessionstore.TeamLibraryEntry, 0, len(library.Teams))
	for _, entry := range library.Teams {
		teams = append(teams, sessionstore.TeamLibraryEntry{
			TeamID:        entry.TeamID,
			TeamKind:      entry.TeamKind,
			Name:          entry.Name,
			OrderPolicy:   entry.OrderPolicy,
			OrderRoles:    append([]string(nil), entry.OrderRoles...),
			Roles:         teamRoleSpecsFromDTO(entry.Roles),
			GatePolicy:    entry.GatePolicy,
			CompactPolicy: entry.CompactPolicy,
			Origin:        entry.Origin,
		})
	}
	return sessionstore.TeamLibrary{Teams: teams}
}

func teamRegistryFromDTO(registry dto.TeamRegistry) sessionstore.TeamRegistry {
	return sessionstore.TeamRegistry{
		TeamID:      registry.TeamID,
		TeamKind:    registry.TeamKind,
		OrderPolicy: registry.OrderPolicy,
		Roles:       teamRoleSpecsFromDTO(registry.Roles),
	}
}

func employeeLibraryToDTO(stored sessionstore.EmployeeLibrary) dto.EmployeeLibrary {
	return dto.EmployeeLibrary{
		Employees:  teamRoleSpecsToDTO(stored.Employees),
		Configured: stored.Configured,
		UpdatedAt:  stored.UpdatedAt,
	}
}

func employeeLibraryFromDTO(library dto.EmployeeLibrary) sessionstore.EmployeeLibrary {
	return sessionstore.EmployeeLibrary{Employees: teamRoleSpecsFromDTO(library.Employees)}
}

func defaultOrderToDTO(stored sessionstore.DefaultOrder) dto.DefaultOrder {
	return dto.DefaultOrder{
		OrderPolicy: stored.OrderPolicy,
		OrderRoles:  append([]string(nil), stored.OrderRoles...),
		Configured:  stored.Configured,
		UpdatedAt:   stored.UpdatedAt,
	}
}

func defaultOrderFromDTO(order dto.DefaultOrder) sessionstore.DefaultOrder {
	return sessionstore.DefaultOrder{
		OrderPolicy: order.OrderPolicy,
		OrderRoles:  append([]string(nil), order.OrderRoles...),
	}
}
