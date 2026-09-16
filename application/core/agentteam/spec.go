// Package agentteam 是 A2A 角色团队的通用装配能力面。
//
// 生态位：goal 的 TL 编排只是本包内置的一个 preset（`goal-a2a`）；本包把
// 「RoleSpec/TeamSpec → 角色会话 + 顺序策略 + 成员表」抽成可复用装配，供
// 后续 agent-team 实例与前端角色管理设置共用。权威边界见
// docs/arch/a2a-agent-team-factory.md。
//
// 非职责：subagent 是 tool calling 能力，不属于 AgentTeam，本包不接纳、不排序、
// 不为其建角色会话；message 的写入仍只由 sequencer 负责，本包不写 message。
package agentteam

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ErrUnknownPreset 表示请求的团队 preset 未注册。
var ErrUnknownPreset = errors.New("agentteam: unknown team preset")

// Normalize 把 TeamSpec 规整成可装配形态：补默认值、去重、推导 order_roles、
// 校正 user/main 的内置 kind。它不做角色会话创建，只做纯函数规整。
func Normalize(spec dto.TeamSpec) (dto.TeamSpec, error) {
	spec.TeamKind = strings.TrimSpace(spec.TeamKind)
	if spec.TeamKind == "" {
		spec.TeamKind = dto.DefaultTeamKind
	}
	spec.TeamID = strings.TrimSpace(spec.TeamID)
	if spec.TeamID == "" {
		spec.TeamID = spec.TeamKind
	}
	spec.OrderPolicy = strings.TrimSpace(spec.OrderPolicy)
	if spec.OrderPolicy == "" {
		spec.OrderPolicy = dto.DefaultOrderPolicy
	}
	switch spec.OrderPolicy {
	case dto.OrderPolicyGoalLoop, dto.OrderPolicyUserMainDecided, dto.OrderPolicyScheduledOnly:
	default:
		return dto.TeamSpec{}, fmt.Errorf("agentteam: unsupported order policy %q", spec.OrderPolicy)
	}

	roles := make([]dto.RoleSpec, 0, len(spec.Roles))
	seen := make(map[string]struct{}, len(spec.Roles))
	for _, role := range spec.Roles {
		normalized, err := NormalizeRole(role)
		if err != nil {
			return dto.TeamSpec{}, err
		}
		role = normalized
		if _, ok := seen[role.RoleName]; ok {
			return dto.TeamSpec{}, fmt.Errorf("agentteam: duplicate role %q", role.RoleName)
		}
		seen[role.RoleName] = struct{}{}
		role.RoleKind = resolveRoleKind(role.RoleName, role.RoleKind)
		roles = append(roles, role)
	}
	spec.Roles = roles

	order, err := resolveOrderRoles(spec, seen)
	if err != nil {
		return dto.TeamSpec{}, err
	}
	spec.OrderRoles = order
	return spec, nil
}

// NormalizeRole 规整单个角色（TeamSpec 装配与"一步实例化一个角色"共用同一套
// 口径，避免两条路径对角色配置的默认值/校验产生分歧）：
//
//   - role_name 必填并去空白；
//   - 内置角色名（user/main）永远取内置 kind；
//   - 其它角色 kind 缺省按 techlead 之外的通用 agent 处理；
//   - join_policy / presence_policy / model_policy / tools_policy 留空 = 继承
//     preset 默认（不在这里编造默认值）。
func NormalizeRole(role dto.RoleSpec) (dto.RoleSpec, error) {
	role.RoleName = strings.TrimSpace(role.RoleName)
	if role.RoleName == "" {
		return dto.RoleSpec{}, errors.New("agentteam: role_name is required")
	}
	role.RoleKind = resolveRoleKind(role.RoleName, role.RoleKind)
	role.JoinPolicy = strings.TrimSpace(role.JoinPolicy)
	role.PresencePolicy = strings.TrimSpace(role.PresencePolicy)
	role.ModelPolicy = strings.TrimSpace(role.ModelPolicy)
	role.ToolsPolicy = strings.TrimSpace(role.ToolsPolicy)
	if !ValidToolPolicy(role.ToolsPolicy) {
		return dto.RoleSpec{}, fmt.Errorf(
			"agentteam: unsupported tools policy %q（取值：%q / %q / %q，空 = 继承宿主默认）",
			role.ToolsPolicy, dto.ToolPolicyReadonly, dto.ToolPolicyReadWrite, dto.ToolPolicyFull)
	}
	return role, nil
}

// ValidToolPolicy 报告 tools_policy 是否落在枚举内（dto.ToolPolicy*）。
//
// 为什么必须在**写入侧**拦：运行时把未识别的 policy 一律映射成 root（全权，见
// seelebridge/tools.ClassForToolsPolicy）——登记阶段的一个拼写错误（"read-only"、
// "readOnly"、"read_only"）会静默升级成最高权限，而不是报错。NormalizeRole 是
// 装配 / 一步入职 / 员工库共用的唯一规整入口，在这里把注水挡在外面之后，"登记的
// 事实"与"运行时能解释的事实"才是同一个集合：映射的 default 分支从此只表示
// "继承 / 全权"，不再兜未知值。
func ValidToolPolicy(policy string) bool {
	switch policy {
	case dto.ToolPolicyInherit, dto.ToolPolicyReadonly, dto.ToolPolicyReadWrite, dto.ToolPolicyFull:
		return true
	default:
		return false
	}
}

// resolveRoleKind 让内置角色名（user/main）永远取内置 kind；其它角色 kind 缺省
// 时按 techlead 之外的通用 agent 处理。
func resolveRoleKind(roleName string, kind dto.RoleKind) dto.RoleKind {
	switch roleName {
	case string(dto.RoleKindUser):
		return dto.RoleKindUser
	case string(dto.RoleKindMain):
		return dto.RoleKindMain
	}
	switch kind {
	case dto.RoleKindUser, dto.RoleKindMain, dto.RoleKindTechlead, dto.RoleKindAgent, dto.RoleKindTimer:
		return kind
	}
	if roleName == RoleTechlead {
		return dto.RoleKindTechlead
	}
	return dto.RoleKindAgent
}

// RoleTechlead 是 goal preset 的 techleader 逻辑角色名（与 sessionstore.RoleTL /
// 历史 message 行的 role_name 一致；"techlead" 是 kind，不是角色名）。
const RoleTechlead = "tl"

// resolveOrderRoles 决定工作顺序：显式给定时必须是 [user, main + 已注册角色] 的
// 子集且不含定时角色；未给定时按 user → main → 其余角色（OrderPriority 升序）。
func resolveOrderRoles(spec dto.TeamSpec, registered map[string]struct{}) ([]string, error) {
	allowed := map[string]struct{}{
		string(dto.RoleKindUser): {},
		string(dto.RoleKindMain): {},
	}
	scheduled := map[string]struct{}{}
	for _, role := range spec.Roles {
		if role.RoleKind == dto.RoleKindTimer {
			// 定时 agent 单独分区，不参与工作顺序（prompt §7.1）。
			scheduled[role.RoleName] = struct{}{}
			continue
		}
		allowed[role.RoleName] = struct{}{}
	}
	_ = registered

	if len(spec.OrderRoles) == 0 {
		order := []string{string(dto.RoleKindUser), string(dto.RoleKindMain)}
		rest := make([]dto.RoleSpec, 0, len(spec.Roles))
		for _, role := range spec.Roles {
			if role.RoleKind == dto.RoleKindTimer {
				continue
			}
			rest = append(rest, role)
		}
		for i := 1; i < len(rest); i++ {
			for j := i; j > 0; j-- {
				if rest[j-1].OrderPriority <= rest[j].OrderPriority {
					break
				}
				rest[j-1], rest[j] = rest[j], rest[j-1]
			}
		}
		for _, role := range rest {
			order = append(order, role.RoleName)
		}
		return order, nil
	}

	order := make([]string, 0, len(spec.OrderRoles))
	seen := make(map[string]struct{}, len(spec.OrderRoles))
	for _, name := range spec.OrderRoles {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("agentteam: duplicate order role %q", name)
		}
		if _, ok := scheduled[name]; ok {
			return nil, fmt.Errorf("agentteam: scheduled role %q must not join order_roles", name)
		}
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("agentteam: order role %q is not registered", name)
		}
		seen[name] = struct{}{}
		order = append(order, name)
	}
	if _, ok := seen[string(dto.RoleKindUser)]; !ok {
		return nil, errors.New("agentteam: order_roles must contain user")
	}
	if _, ok := seen[string(dto.RoleKindMain)]; !ok {
		return nil, errors.New("agentteam: order_roles must contain main")
	}
	return order, nil
}

// RoleSessionID 派生角色会话号：同一个 (team_id, role_name) 永远得到同一个值，
// 这是重复装配幂等的键（不是展示名）。
func RoleSessionID(teamID, roleName string) string {
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		teamID = dto.DefaultTeamKind
	}
	return teamID + "-" + strings.TrimSpace(roleName)
}

// needsRoleSession 判定该角色是否需要独立角色会话子树：user/main 复用主会话，
// 其余参与者（techlead/agent/timer）各有自己的会话。
func needsRoleSession(kind dto.RoleKind) bool {
	return kind != dto.RoleKindUser && kind != dto.RoleKindMain
}

// registeredRoles 返回需要角色会话的已注册角色。
func registeredRoles(spec dto.TeamSpec) []dto.RoleSpec {
	out := make([]dto.RoleSpec, 0, len(spec.Roles))
	for _, role := range spec.Roles {
		if needsRoleSession(role.RoleKind) {
			out = append(out, role)
		}
	}
	return out
}
