package agentteam

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// Port 是工厂与注册表需要的装配面。实现方（application/core 的适配器）负责
// 项目作用域解析与存储落地；本包不 import 存储实现，也不写 message。
type Port interface {
	// EnsureRoleSession 幂等创建角色会话（已存在返回 created=false）。
	EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error)
	// ReadLifecycleOrder 读主会话的群聊顺序策略（运行时权威）。
	ReadLifecycleOrder(sessionID string) (policy string, roles []string, err error)
	// SetLifecycleOrder 写群聊顺序策略（只改 lifecycle 字段，不动 message）。
	SetLifecycleOrder(sessionID, policy string, roles []string) error
	// ReadTeamRegistry / WriteTeamRegistry 读写角色注册表（整份替换型）。
	ReadTeamRegistry(mainSessionID string) (dto.TeamRegistry, error)
	WriteTeamRegistry(mainSessionID string, registry dto.TeamRegistry) error
}

// Factory 由 TeamSpec 装配一支 AgentTeam：建角色会话 → 写注册表 → 写顺序策略。
//
// 幂等：同一个 (team_id, role_name) 派生同一个 role_session_id，重复装配不会产生
// 第二个角色会话；顺序与注册表整份替换，重复装配结果一致。
type Factory struct {
	port Port
}

// NewFactory 构造工厂；port 为 nil 时显式报错（不允许静默空转）。
func NewFactory(port Port) (*Factory, error) {
	if port == nil {
		return nil, errors.New("agentteam: assembly port is required")
	}
	return &Factory{port: port}, nil
}

// Materialize 装配 TeamSpec。joinSeq 是本次装配把角色挂到主会话的可见起点
// （main message seq），写入各角色会话的 join_seq_id。
func (factory *Factory) Materialize(mainSessionID string, spec dto.TeamSpec, joinSeq uint64) (dto.TeamMaterializeResult, error) {
	if factory == nil || factory.port == nil {
		return dto.TeamMaterializeResult{}, errors.New("agentteam: factory is not assembled")
	}
	mainSessionID = strings.TrimSpace(mainSessionID)
	if mainSessionID == "" {
		return dto.TeamMaterializeResult{}, errors.New("agentteam: main session ID is required")
	}
	normalized, err := Normalize(spec)
	if err != nil {
		return dto.TeamMaterializeResult{}, err
	}

	sessions := make([]dto.TeamRoleSession, 0, len(normalized.Roles))
	for _, role := range registeredRoles(normalized) {
		roleSessionID := RoleSessionID(normalized.TeamID, role.RoleName)
		created, err := factory.port.EnsureRoleSession(mainSessionID, role.RoleName, roleSessionID, joinSeq)
		if err != nil {
			return dto.TeamMaterializeResult{}, fmt.Errorf("agentteam: ensure role session %s: %w", role.RoleName, err)
		}
		sessions = append(sessions, dto.TeamRoleSession{
			RoleName:      role.RoleName,
			RoleSessionID: roleSessionID,
			Exists:        true,
			Created:       created,
		})
	}

	registry := registryFromSpec(normalized)
	if err := factory.port.WriteTeamRegistry(mainSessionID, registry); err != nil {
		return dto.TeamMaterializeResult{}, fmt.Errorf("agentteam: write registry: %w", err)
	}
	if err := factory.port.SetLifecycleOrder(mainSessionID, normalized.OrderPolicy, normalized.OrderRoles); err != nil {
		return dto.TeamMaterializeResult{}, fmt.Errorf("agentteam: write order policy: %w", err)
	}

	view, err := assembleView(mainSessionID, registry, normalized.OrderPolicy, normalized.OrderRoles)
	if err != nil {
		return dto.TeamMaterializeResult{}, err
	}
	return dto.TeamMaterializeResult{Spec: normalized, View: view, Sessions: sessions, Registry: registry}, nil
}

// registryFromSpec 把 TeamSpec 投影成注册表（角色配置的持久事实）。
func registryFromSpec(spec dto.TeamSpec) dto.TeamRegistry {
	roles := make([]dto.RoleSpec, 0, len(spec.Roles))
	roles = append(roles, spec.Roles...)
	return dto.TeamRegistry{
		TeamID:      spec.TeamID,
		TeamKind:    spec.TeamKind,
		OrderPolicy: spec.OrderPolicy,
		Roles:       roles,
		Configured:  true,
	}
}

// assembleView 把注册表 + 生命周期顺序投影成前端消费的成员表。
func assembleView(sessionID string, registry dto.TeamRegistry, policy string, orderRoles []string) (dto.TeamView, error) {
	if strings.TrimSpace(policy) == "" {
		policy = registry.OrderPolicy
	}
	if policy == "" {
		policy = dto.DefaultOrderPolicy
	}
	byName := make(map[string]dto.RoleSpec, len(registry.Roles))
	for _, role := range registry.Roles {
		byName[role.RoleName] = role
	}

	members := make([]dto.TeamMember, 0, len(orderRoles)+2)
	scheduled := make([]dto.TeamMember, 0, len(registry.Roles))
	inOrder := make(map[string]struct{}, len(orderRoles))
	for index, name := range orderRoles {
		inOrder[name] = struct{}{}
		members = append(members, buildMember(registry.TeamID, name, index, true, byName))
	}
	for _, role := range registry.Roles {
		if role.RoleKind == dto.RoleKindTimer {
			scheduled = append(scheduled, buildMember(registry.TeamID, role.RoleName, -1, false, byName))
			continue
		}
		if _, ok := inOrder[role.RoleName]; ok {
			continue
		}
		if _, ok := builtinKinds[role.RoleName]; ok {
			continue
		}
		members = append(members, buildMember(registry.TeamID, role.RoleName, -1, false, byName))
	}

	view := dto.TeamView{
		SessionID:   sessionID,
		TeamID:      registry.TeamID,
		TeamKind:    registry.TeamKind,
		OrderPolicy: policy,
		OrderRoles:  append([]string(nil), orderRoles...),
		Members:     members,
		Scheduled:   scheduled,
		Configured:  registry.Configured,
	}
	view.DesignNotice = viewNotices(registry, orderRoles)
	return view, nil
}

// builtinKinds 是 user/main 的内置角色名：它们不注册角色配置、不建角色会话。
var builtinKinds = map[string]dto.RoleKind{
	string(dto.RoleKindUser): dto.RoleKindUser,
	string(dto.RoleKindMain): dto.RoleKindMain,
}

func buildMember(teamID, name string, orderIndex int, inOrder bool, byName map[string]dto.RoleSpec) dto.TeamMember {
	kind, ok := builtinKinds[name]
	role, registered := byName[name]
	if !ok {
		if registered {
			kind = role.RoleKind
		} else {
			kind = resolveRoleKind(name, "")
		}
	}
	member := dto.TeamMember{
		RoleName:   name,
		RoleKind:   kind,
		OrderIndex: orderIndex,
		InOrder:    inOrder,
	}
	if registered {
		member.OrderPriority = role.OrderPriority
		member.JoinPolicy = role.JoinPolicy
		member.ToolsPolicy = role.ToolsPolicy
		if needsRoleSession(kind) {
			member.RoleSessionID = RoleSessionID(teamID, name)
		}
	}
	return member
}

// viewNotices 只报事实，不自动修补：注册了但不在顺序里的角色、顺序里未注册的角色。
func viewNotices(registry dto.TeamRegistry, orderRoles []string) []string {
	inOrder := make(map[string]struct{}, len(orderRoles))
	for _, name := range orderRoles {
		inOrder[name] = struct{}{}
	}
	notices := make([]string, 0, 2)
	for _, role := range registry.Roles {
		if role.RoleKind == dto.RoleKindTimer {
			continue
		}
		if _, ok := builtinKinds[role.RoleName]; ok {
			continue
		}
		if _, ok := inOrder[role.RoleName]; !ok {
			notices = append(notices, fmt.Sprintf("角色 %s 已注册但不在工作顺序（order_roles）中", role.RoleName))
		}
	}
	for _, name := range orderRoles {
		if _, ok := builtinKinds[name]; ok {
			continue
		}
		found := false
		for _, role := range registry.Roles {
			if role.RoleName == name {
				found = true
				break
			}
		}
		if !found {
			notices = append(notices, fmt.Sprintf("工作顺序中的 %s 尚未注册角色配置", name))
		}
	}
	if len(notices) == 0 {
		return nil
	}
	return notices
}
