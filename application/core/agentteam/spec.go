// Package agentteam 是 A2A 角色团队的通用装配能力面。
//
// 生态位：把「RoleSpec/TeamSpec → 角色会话 + 成员表」抽成可复用装配，供 Agent Team
// 的各个来路（团队库条目装配 / 员工入职 / `@` 召唤）共用。权威边界见
// docs/arch/a2a-agent-team-factory.md。
//
// **没有内置形态目录**（2026-10-01，用户口径：内置形态对 teamwork 已经过时）：
// 本包不再提供任何"预置团队"（旧的 goal-a2a / review-team / research-team 三支已随
// presets.go 一起删除）。一支团队就是调用方给的一份 TeamSpec（或团队库里的一个条目）：
// 有谁、什么顺序，由数据说，不由代码里的模板说。leader-worker 目标态里顺序归 leader
// 的 team plan（docs/arch/teamwork-leader-worker-architecture.md §4.6/D4）。
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

// ErrUnknownPreset 已随内置形态目录删除（2026-10-01）：团队只从数据来，不再有
// "按形态名解析"这条路径，所以没有"未知形态"这个错误类别。

// Normalize 把 TeamSpec 规整成可装配形态：补默认值、去重、推导 order_roles、
// 校正 user/main 的内置 kind。它不做角色会话创建，只做纯函数规整。
//
// team_id / team_kind 都必填其一（缺省互为镜像）：team_id 是身份（role_session_id
// 的派生分量），team_kind 是展示别名——**不再有"缺省形态"这种回退**，一份没有名字的
// TeamSpec 就是调用方的错误。
func Normalize(spec dto.TeamSpec) (dto.TeamSpec, error) {
	spec.TeamID = strings.TrimSpace(spec.TeamID)
	spec.TeamKind = strings.TrimSpace(spec.TeamKind)
	if spec.TeamID == "" && spec.TeamKind != "" {
		spec.TeamID = spec.TeamKind
	}
	if spec.TeamKind == "" {
		// team_kind 是 team_id 的展示别名，缺省等于 id：不塞一个假形态名，也不留空
		// ——角色提示词里那行 <team_kind> 要说得出"这是哪支团队"。
		spec.TeamKind = spec.TeamID
	}
	if spec.TeamID == "" {
		return dto.TeamSpec{}, errors.New("agentteam: team_id is required")
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
//     宿主默认（不在这里编造默认值）。
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
	// 逐格装配的权限同样在**写入侧**校验：组名拼错 / 位值越界必须在这里报错，
	// 而不是写进注册表后由运行时解释成"没分配"（见 dto.NormalizePermissionGroups）。
	groups, err := dto.NormalizePermissionGroups(role.PermissionGroups)
	if err != nil {
		return dto.RoleSpec{}, fmt.Errorf("agentteam: 角色 %q 的权限格子非法: %w", role.RoleName, err)
	}
	role.PermissionGroups = groups
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

// RoleTechlead 是 goal 团队的 techleader 逻辑角色名（与 sessionstore.RoleTL /
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
		seen := map[string]struct{}{
			string(dto.RoleKindUser): {},
			string(dto.RoleKindMain): {},
		}
		rest := make([]dto.RoleSpec, 0, len(spec.Roles))
		for _, role := range spec.Roles {
			if role.RoleKind == dto.RoleKindTimer {
				continue
			}
			if _, ok := seen[role.RoleName]; ok {
				// user/main 已经在链首：Roles 里显式声明了内置角色时不得重复入链
				// （旧写法只在顺序显式给定时才走这里，所以这条重复一直藏着）。
				continue
			}
			seen[role.RoleName] = struct{}{}
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

// RoleSessionID 派生角色会话号：同一个 (主会话, team_id, role_name) 永远得到同一个
// 值，这是重复装配幂等的键（不是展示名）。
//
// **主会话身份编进角色会话号**（2026-10-01，用例 2「团队会话粒度」）：改前只由
// (team_id, role_name) 决定，于是"两个会话召唤了同一支团队"会得到同一个角色会话号
// ——存储面侥幸没串（角色子树挂在各自主会话下），但**运行面**按这个号做键的地方全串：
// 角色引擎槽（`roleTurnState.sessions`）、项目根绑定（`ProjectScope.BindFor`）、
// 权责反查（`agentteam_role_index`）都会把两个会话的同名员工当成同一个人。把主会话
// 编进来，隔离由**标识**保证，而不是靠下游各自记得再拼一次主会话。
//
// mainSessionID 为空 = 无会话归属的退化形态（仅桩/测试构造用）；生产调用方一律带会话号。
func RoleSessionID(mainSessionID, teamID, roleName string) string {
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		// 没有团队身份的会话（未装配团队就直接入职）也要能派生出稳定号：用缺省
		// 团队名兜底。**这个字面量是身份分量，不是形态名**——改它 = 既有角色会话号
		// 全体分裂（同名角色被当成新员工），所以它在形态目录删掉之后仍然保留。
		teamID = dto.DefaultTeamID
	}
	roleName = strings.TrimSpace(roleName)
	mainSessionID = strings.TrimSpace(mainSessionID)
	if mainSessionID == "" {
		return teamID + "-" + roleName
	}
	return mainSessionID + "-" + teamID + "-" + roleName
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
