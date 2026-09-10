package agentteam

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// Registry 是角色管理的读写面：RoleSpec CRUD + 工作顺序设置。
//
// registry 是角色编排的唯一权威（角色清单 + order_policy/order_roles）；前端只提交
// 字段更新，本包不做第二份顺序事实：顺序统一落在 lifecycle head。
type Registry struct {
	port Port
}

// NewRegistry 构造注册表读写面；port 为 nil 时显式报错。
func NewRegistry(port Port) (*Registry, error) {
	if port == nil {
		return nil, errors.New("agentteam: assembly port is required")
	}
	return &Registry{port: port}, nil
}

// View 返回成员表（供右侧栏「状态 → Agent Team」子页与角色管理设置读取）。
func (registry *Registry) View(mainSessionID string) (dto.TeamView, error) {
	if registry == nil || registry.port == nil {
		return dto.TeamView{}, errors.New("agentteam: registry is not assembled")
	}
	mainSessionID = strings.TrimSpace(mainSessionID)
	if mainSessionID == "" {
		return dto.TeamView{}, errors.New("agentteam: main session ID is required")
	}
	stored, err := registry.port.ReadTeamRegistry(mainSessionID)
	if err != nil {
		return dto.TeamView{}, err
	}
	policy, orderRoles, err := registry.port.ReadLifecycleOrder(mainSessionID)
	if err != nil {
		return dto.TeamView{}, err
	}
	if len(orderRoles) == 0 && len(stored.Roles) > 0 {
		// 注册表有角色但 lifecycle 尚无顺序：按注册顺序推导只读视图，不写盘
		// （写路径只走 Materialize/SetOrder，避免读操作产生第二份事实）。
		spec, err := Normalize(dto.TeamSpec{
			TeamID:      stored.TeamID,
			TeamKind:    stored.TeamKind,
			OrderPolicy: firstNonEmpty(policy, stored.OrderPolicy),
			Roles:       stored.Roles,
		})
		if err != nil {
			return dto.TeamView{}, err
		}
		orderRoles = spec.OrderRoles
	}
	if policy == "" {
		policy = firstNonEmpty(stored.OrderPolicy, dto.DefaultOrderPolicy)
	}
	return assembleView(mainSessionID, stored, policy, orderRoles)
}

// PutRole 新增或覆盖一个角色配置（按 role_name 幂等）。
func (registry *Registry) PutRole(mainSessionID string, role dto.RoleSpec) (dto.TeamRegistry, error) {
	if registry == nil || registry.port == nil {
		return dto.TeamRegistry{}, errors.New("agentteam: registry is not assembled")
	}
	mainSessionID = strings.TrimSpace(mainSessionID)
	if mainSessionID == "" {
		return dto.TeamRegistry{}, errors.New("agentteam: main session ID is required")
	}
	role.RoleName = strings.TrimSpace(role.RoleName)
	if role.RoleName == "" {
		return dto.TeamRegistry{}, errors.New("agentteam: role_name is required")
	}
	if _, ok := builtinKinds[role.RoleName]; ok {
		return dto.TeamRegistry{}, fmt.Errorf("agentteam: role %q is builtin and cannot be reconfigured", role.RoleName)
	}
	role.RoleKind = resolveRoleKind(role.RoleName, role.RoleKind)

	stored, err := registry.port.ReadTeamRegistry(mainSessionID)
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	roles := make([]dto.RoleSpec, 0, len(stored.Roles)+1)
	replaced := false
	for _, existing := range stored.Roles {
		if existing.RoleName == role.RoleName {
			roles = append(roles, role)
			replaced = true
			continue
		}
		roles = append(roles, existing)
	}
	if !replaced {
		roles = append(roles, role)
	}
	stored.Roles = roles
	stored.Configured = true
	if err := registry.port.WriteTeamRegistry(mainSessionID, stored); err != nil {
		return dto.TeamRegistry{}, err
	}
	return stored, nil
}

// DeleteRole 删除一个角色配置；角色仍留在工作顺序时同步摘除，避免顺序里挂着
// 一个没有配置的角色（定时角色直接删除即可）。
func (registry *Registry) DeleteRole(mainSessionID, roleName string) (dto.TeamRegistry, error) {
	if registry == nil || registry.port == nil {
		return dto.TeamRegistry{}, errors.New("agentteam: registry is not assembled")
	}
	roleName = strings.TrimSpace(roleName)
	if roleName == "" {
		return dto.TeamRegistry{}, errors.New("agentteam: role_name is required")
	}
	if _, ok := builtinKinds[roleName]; ok {
		return dto.TeamRegistry{}, fmt.Errorf("agentteam: role %q is builtin and cannot be deleted", roleName)
	}
	stored, err := registry.port.ReadTeamRegistry(mainSessionID)
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	roles := make([]dto.RoleSpec, 0, len(stored.Roles))
	found := false
	for _, existing := range stored.Roles {
		if existing.RoleName == roleName {
			found = true
			continue
		}
		roles = append(roles, existing)
	}
	if !found {
		return stored, nil
	}
	stored.Roles = roles
	if err := registry.port.WriteTeamRegistry(mainSessionID, stored); err != nil {
		return dto.TeamRegistry{}, err
	}

	policy, orderRoles, err := registry.port.ReadLifecycleOrder(mainSessionID)
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	if len(orderRoles) > 0 {
		trimmed := make([]string, 0, len(orderRoles))
		for _, name := range orderRoles {
			if name == roleName {
				continue
			}
			trimmed = append(trimmed, name)
		}
		if len(trimmed) != len(orderRoles) {
			if err := registry.port.SetLifecycleOrder(mainSessionID, policy, trimmed); err != nil {
				return dto.TeamRegistry{}, err
			}
		}
	}
	return stored, nil
}

// SetOrder 写工作顺序策略（`order_roles`）；校验角色已注册、定时角色不入顺序、
// user/main 必须在列。前端拖拽/上下移只提交这个字段。
func (registry *Registry) SetOrder(mainSessionID, policy string, orderRoles []string) (dto.TeamView, error) {
	if registry == nil || registry.port == nil {
		return dto.TeamView{}, errors.New("agentteam: registry is not assembled")
	}
	stored, err := registry.port.ReadTeamRegistry(mainSessionID)
	if err != nil {
		return dto.TeamView{}, err
	}
	spec, err := Normalize(dto.TeamSpec{
		TeamID:      stored.TeamID,
		TeamKind:    stored.TeamKind,
		OrderPolicy: firstNonEmpty(strings.TrimSpace(policy), stored.OrderPolicy),
		OrderRoles:  orderRoles,
		Roles:       stored.Roles,
	})
	if err != nil {
		return dto.TeamView{}, err
	}
	if err := registry.port.SetLifecycleOrder(mainSessionID, spec.OrderPolicy, spec.OrderRoles); err != nil {
		return dto.TeamView{}, err
	}
	return assembleView(mainSessionID, stored, spec.OrderPolicy, spec.OrderRoles)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
