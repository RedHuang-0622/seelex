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
		teamID, teamKind := registryIdentity(stored)
		spec, err := Normalize(dto.TeamSpec{
			TeamID:      teamID,
			TeamKind:    teamKind,
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
	view, err := assembleView(mainSessionID, stored, policy, orderRoles)
	if err != nil {
		return dto.TeamView{}, err
	}
	// floor 是运行态事实（message head），每次读视图都重新取值——不落盘、不缓存，
	// 否则前端「floor 高亮」会停在装配那一刻。
	applyFloor(registry.port, mainSessionID, &view)
	return view, nil
}

// Stored 返回注册表原文（团队库"把当前团队存进库"的数据源）：成员表视图只带
// 展示需要的字段，而库条目需要完整 RoleSpec（含提示词/权限/mirror/directive）。
// 本方法只读，不写盘、不推导第二份事实。
func (registry *Registry) Stored(mainSessionID string) (dto.TeamRegistry, error) {
	if registry == nil || registry.port == nil {
		return dto.TeamRegistry{}, errors.New("agentteam: registry is not assembled")
	}
	mainSessionID = strings.TrimSpace(mainSessionID)
	if mainSessionID == "" {
		return dto.TeamRegistry{}, errors.New("agentteam: main session ID is required")
	}
	return registry.port.ReadTeamRegistry(mainSessionID)
}

// PromptFor 读某个角色登记的提示词（空串 = 未登记 → 调用方用内置提示词兜底）。
// 只读：ADVISOR 回合每次评审都要取一次，所以这里不建环、不同步运行态。
func (registry *Registry) PromptFor(mainSessionID, roleName string) (string, error) {
	stored, err := registry.Stored(mainSessionID)
	if err != nil {
		return "", err
	}
	roleName = strings.TrimSpace(roleName)
	for _, role := range stored.Roles {
		if role.RoleName == roleName {
			return strings.TrimSpace(role.SystemPrompt), nil
		}
	}
	return "", nil
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
	// 规整走与装配/入职/员工库**同一个入口** NormalizeRole：权限口径与权限格子
	// 都必须在写入侧拦下（拼写错误在运行时等价于"继承宿主默认"或"这一格没分配"，
	// 而两条路都是静默的）。此前 PutRole 只 trim + resolve kind，是这条
	// "唯一规整入口"承诺上的一个缺口。
	normalized, err := NormalizeRole(role)
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	role = normalized

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
	// 顺序校验只关心角色集与顺序，但 Normalize 要求团队身份齐全：注册表可能来自
	// "未装配团队就直接入职"的会话（没有 team_id），故先补身份（只用于校验，不写盘）。
	teamID, teamKind := registryIdentity(stored)
	spec, err := Normalize(dto.TeamSpec{
		TeamID:      teamID,
		TeamKind:    teamKind,
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
	view, err := assembleView(mainSessionID, stored, spec.OrderPolicy, spec.OrderRoles)
	if err != nil {
		return dto.TeamView{}, err
	}
	// 与 View 同口径：顺序设置的回执视图也带 floor（前端据此重绘高亮）。
	applyFloor(registry.port, mainSessionID, &view)
	return view, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// registryIdentity 返回注册表可用的团队身份（team_id / team_kind）。
//
// 为什么需要兜底：注册表可能来自"未装配团队就直接入职"的会话（没有 team_id）。
// View / SetOrder 两条路径要用 Normalize 校验角色集与顺序，而 Normalize 现在要求
// 团队身份齐全（形态目录删掉后不再有"缺省形态"这种回退，见 spec.go）。兜底值只用于
// **校验**，不写盘：身份的落盘仍只发生在 Materialize / InstantiateRole 路径。
func registryIdentity(registry dto.TeamRegistry) (teamID, teamKind string) {
	teamID = firstNonEmpty(strings.TrimSpace(registry.TeamID), strings.TrimSpace(registry.TeamKind), dto.DefaultTeamID)
	teamKind = firstNonEmpty(strings.TrimSpace(registry.TeamKind), teamID)
	return teamID, teamKind
}
