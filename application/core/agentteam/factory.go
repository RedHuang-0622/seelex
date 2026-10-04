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

// FloorPort 是 Port 的**可选**扩展：读主会话当前发言角色（message head.floor，
// 唯一写者 = sequencer）。实现方按需提供；未实现时成员表的 floor 高亮保持空
// （旧宿主/测试桩），不报错、不伪造。
//
// 为什么可选而不是塞进 Port：floor 是运行态读面，装配面（Port）不需要它的
// 实现者承担额外编译期义务（否则每个桩都要跟着改）。
type FloorPort interface {
	// ReadFloorRole 返回当前发言的角色名；空串 = 尚无发言（不是错误）。
	ReadFloorRole(mainSessionID string) (string, error)
}

// DismissPort 是 Port 的**可选**扩展：删除角色注册表（让团队离场）。
//
// 与 FloorPort 同样的取舍：离场是"取消装配"，不是每条装配路径都必须承担的
// 义务，因此做成可选面——未实现它的宿主照常装配与读取，只是不支持"走人"，
// Factory.Dismiss 会显式报 ErrDismissUnsupported 而不是静默成功。
type DismissPort interface {
	// RemoveTeamRegistry 删除主会话的角色注册表（幂等：不存在不算错误）。
	RemoveTeamRegistry(mainSessionID string) error
}

// ErrDismissUnsupported 表示宿主的装配面没有实现团队离场（DismissPort）。
var ErrDismissUnsupported = errors.New("agentteam: 宿主未装配团队离场面（DismissPort）")

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
		roleSessionID := RoleSessionID(mainSessionID, normalized.TeamID, role.RoleName)
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
	// 装配回执视图也带 floor（运行态读面；装配本身不写 floor）。
	applyFloor(factory.port, mainSessionID, &view)
	return dto.TeamMaterializeResult{Spec: normalized, View: view, Sessions: sessions, Registry: registry}, nil
}

// Dismiss 让一支已装配的团队离场（"干完就走人"）：删除会话角色注册表 + 复位
// 群聊顺序策略。
//
// 角色会话子树留在盘上不动——装配的幂等键是 (team_id, role_name)，同一个团队
// 再次召唤/上线时复用同一棵子树，所以"离场"改变的是"本会话还有没有在编团队"，
// 不是把角色会话的历史抹掉。
//
// 顺序也一起清：只删注册表而留着 order_roles，会让"顺序里挂着未注册角色"这条
// 设计偏差常驻成员表（viewNotices 会一直报），而它其实已经是历史结论。
func (factory *Factory) Dismiss(mainSessionID string) error {
	if factory == nil || factory.port == nil {
		return errors.New("agentteam: factory is not assembled")
	}
	mainSessionID = strings.TrimSpace(mainSessionID)
	if mainSessionID == "" {
		return errors.New("agentteam: main session ID is required")
	}
	remover, ok := factory.port.(DismissPort)
	if !ok {
		return ErrDismissUnsupported
	}
	if err := remover.RemoveTeamRegistry(mainSessionID); err != nil {
		return fmt.Errorf("agentteam: remove team registry: %w", err)
	}
	if err := factory.port.SetLifecycleOrder(mainSessionID, "", nil); err != nil {
		return fmt.Errorf("agentteam: reset order policy: %w", err)
	}
	return nil
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

// InstantiateRole 一步实例化一个角色：规整/校验配置 → 幂等创建角色会话 →
// 落注册表 → 按 join_policy 决定是否进入工作顺序 → 报告执行者绑定。
//
// 与 Materialize 的关系：Materialize 是"整队装配"（TeamSpec），本方法是"单个
// 员工入职"，两者共用同一套 Normalize/顺序事实（lifecycle.order_policy/
// order_roles 仍是唯一持久顺序，不新增第二份）。
//
// 内置角色（user/main）由会话本身提供，不能实例化——它们不在"员工"范围内。
// 定时角色（timer）按约定不进工作顺序，落在 scheduled 分区（join_policy=
// scheduled 只记录，由调度器触发）。
func (factory *Factory) InstantiateRole(mainSessionID string, role dto.RoleSpec, joinSeq uint64) (dto.RoleInstantiation, error) {
	if factory == nil || factory.port == nil {
		return dto.RoleInstantiation{}, errors.New("agentteam: factory is not assembled")
	}
	mainSessionID = strings.TrimSpace(mainSessionID)
	if mainSessionID == "" {
		return dto.RoleInstantiation{}, errors.New("agentteam: main session ID is required")
	}
	normalized, err := NormalizeRole(role)
	if err != nil {
		return dto.RoleInstantiation{}, err
	}
	if _, ok := builtinKinds[normalized.RoleName]; ok {
		return dto.RoleInstantiation{}, fmt.Errorf("agentteam: role %q is provided by the session and cannot be instantiated", normalized.RoleName)
	}

	registry, err := factory.port.ReadTeamRegistry(mainSessionID)
	if err != nil {
		return dto.RoleInstantiation{}, fmt.Errorf("agentteam: read registry: %w", err)
	}
	teamID := strings.TrimSpace(registry.TeamID)
	if teamID == "" {
		// 未装配团队的会话也要能入职：用缺省团队名派生角色会话号（身份分量，
		// 不是形态名，见 dto.DefaultTeamID）。
		teamID = dto.DefaultTeamID
	}
	roleSessionID := RoleSessionID(mainSessionID, teamID, normalized.RoleName)
	created, err := factory.port.EnsureRoleSession(mainSessionID, normalized.RoleName, roleSessionID, joinSeq)
	if err != nil {
		return dto.RoleInstantiation{}, fmt.Errorf("agentteam: ensure role session %s: %w", normalized.RoleName, err)
	}

	// 注册表：整份替换语义下按 role_name 覆盖（新增与修改同一路径）。
	roles := make([]dto.RoleSpec, 0, len(registry.Roles)+1)
	replaced := false
	for _, existing := range registry.Roles {
		if existing.RoleName == normalized.RoleName {
			roles = append(roles, normalized)
			replaced = true
			continue
		}
		roles = append(roles, existing)
	}
	if !replaced {
		roles = append(roles, normalized)
	}
	registry.TeamID = teamID
	registry.Roles = roles
	registry.Configured = true
	if strings.TrimSpace(registry.TeamKind) == "" {
		// team_kind 只是 team_id 的展示别名（形态目录已删）：缺省即 id。
		registry.TeamKind = teamID
	}
	if err := factory.port.WriteTeamRegistry(mainSessionID, registry); err != nil {
		return dto.RoleInstantiation{}, fmt.Errorf("agentteam: write registry: %w", err)
	}

	policy, orderRoles, err := factory.port.ReadLifecycleOrder(mainSessionID)
	if err != nil {
		return dto.RoleInstantiation{}, fmt.Errorf("agentteam: read order: %w", err)
	}
	if strings.TrimSpace(policy) == "" {
		policy = registry.OrderPolicy
	}
	if strings.TrimSpace(policy) == "" {
		policy = dto.DefaultOrderPolicy
	}
	orderRoles, placed, notice := placeRoleInOrder(normalized, orderRoles)
	if placed {
		if err := factory.port.SetLifecycleOrder(mainSessionID, policy, orderRoles); err != nil {
			return dto.RoleInstantiation{}, fmt.Errorf("agentteam: write order policy: %w", err)
		}
	}

	index := -1
	for position, name := range orderRoles {
		if name == normalized.RoleName {
			index = position
			break
		}
	}
	result := dto.RoleInstantiation{
		Role:        normalized,
		Session:     dto.TeamRoleSession{RoleName: normalized.RoleName, RoleSessionID: roleSessionID, Exists: true, Created: created},
		OrderPolicy: policy,
		OrderRoles:  orderRoles,
		InOrder:     index >= 0,
		OrderIndex:  index,
		Notice:      notice,
	}
	if RolesWithExecutor[normalized.RoleName] {
		result.Executor = normalized.RoleName
	} else if normalized.RoleKind == dto.RoleKindTimer {
		result.Executor = "scheduler"
	} else {
		result.Notice = append(result.Notice,
			fmt.Sprintf("角色 %s 已入职，但**当前没有运行时执行者**（不会自动产生回合）；接入执行者前请把它当只读成员。", normalized.RoleName))
	}
	return result, nil
}

// placeRoleInOrder 按 join_policy 决定新角色是否自动进入工作顺序：
//
//   - on_team_create（缺省）/ builtin：直接排到顺序末尾（幂等：已在顺序内不动）；
//   - scheduled / timer：不进工作顺序（定时角色单独分区，由调度器触发）；
//   - on_goal_create / on_demand / 其它：不自动排入，交由调用方显式 SetOrder
//     （返回提示，避免"入职了却悄悄插队"）。
func placeRoleInOrder(role dto.RoleSpec, orderRoles []string) ([]string, bool, []string) {
	for _, name := range orderRoles {
		if name == role.RoleName {
			return orderRoles, false, nil
		}
	}
	policy := strings.TrimSpace(role.JoinPolicy)
	switch {
	case role.RoleKind == dto.RoleKindTimer || policy == "scheduled":
		return orderRoles, false, []string{
			fmt.Sprintf("角色 %s 是定时 agent：不进工作顺序，由调度器按计划触发。", role.RoleName)}
	case policy == "" || policy == "on_team_create" || policy == "builtin":
		next := append(append([]string(nil), orderRoles...), role.RoleName)
		return next, true, nil
	default:
		return orderRoles, false, []string{
			fmt.Sprintf("角色 %s 的 join_policy=%s：不自动排入工作顺序，需要显式设置顺序。", role.RoleName, policy)}
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
		members = append(members, buildMember(sessionID, registry.TeamID, name, index, true, byName))
	}
	for _, role := range registry.Roles {
		if role.RoleKind == dto.RoleKindTimer {
			scheduled = append(scheduled, buildMember(sessionID, registry.TeamID, role.RoleName, -1, false, byName))
			continue
		}
		if _, ok := inOrder[role.RoleName]; ok {
			continue
		}
		if _, ok := builtinKinds[role.RoleName]; ok {
			continue
		}
		members = append(members, buildMember(sessionID, registry.TeamID, role.RoleName, -1, false, byName))
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

// applyFloor 用可选的 floor 读端口填充成员表的当前发言角色（只读事实，不写盘）。
// 未实现 FloorPort 的宿主不填充；读取失败只进 DesignNotice，不阻断成员表。
func applyFloor(port Port, mainSessionID string, view *dto.TeamView) {
	if view == nil {
		return
	}
	floor, ok := port.(FloorPort)
	if !ok {
		return
	}
	roleName, err := floor.ReadFloorRole(mainSessionID)
	if err != nil {
		view.DesignNotice = append(view.DesignNotice, fmt.Sprintf("floor 读取失败：%v", err))
		return
	}
	if name := strings.TrimSpace(roleName); name != "" {
		view.FloorRole = name
	}
}

// builtinKinds 是 user/main 的内置角色名：它们不注册角色配置、不建角色会话。
var builtinKinds = map[string]dto.RoleKind{
	string(dto.RoleKindUser): dto.RoleKindUser,
	string(dto.RoleKindMain): dto.RoleKindMain,
}

func buildMember(mainSessionID, teamID, name string, orderIndex int, inOrder bool, byName map[string]dto.RoleSpec) dto.TeamMember {
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
		member.SystemPrompt = role.SystemPrompt
		member.ModelPolicy = role.ModelPolicy
		member.PresencePolicy = role.PresencePolicy
		// 权限格子回读（前端「编辑员工」要回填）：这里是成员表的唯一构造点，
		// 少这一行就会出现"装配好的格子被一次编辑清空"的静默丢失。
		if len(role.PermissionGroups) > 0 {
			member.PermissionGroups = make(map[string]uint8, len(role.PermissionGroups))
			for group, bits := range role.PermissionGroups {
				member.PermissionGroups[group] = bits
			}
		}
		// 插件装配回读（同一个理由：成员表是回读的唯一构造点，少这一行就会出现
		// "装配好的插件被一次编辑清空"的静默丢失）。
		if len(role.Plugins) > 0 {
			member.Plugins = append([]string(nil), role.Plugins...)
		}
		if needsRoleSession(kind) {
			member.RoleSessionID = RoleSessionID(mainSessionID, teamID, name)
		}
	}
	return member
}

// viewNotices 只报事实，不自动修补：注册了但不在顺序里的角色、顺序里未注册的角色、
// 顺序里没有任何执行者的团队（装配得出来但没有回合）。
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
	if unexecuted := unexecutedRoles(orderRoles); len(unexecuted) > 0 {
		notices = append(notices, fmt.Sprintf(
			"本团队（%s）暂无可执行者：%s 目前只有注册配置与角色会话，装配后不会自动产生回合（需要宿主为它接执行者）",
			teamKindOf(registry), strings.Join(unexecuted, "、")))
	}
	if len(notices) == 0 {
		return nil
	}
	return notices
}

// teamKindOf 返回可展示的团队名（team_kind 是 team_id 的别名；空值不伪装成某个
// 形态——形态目录已删，这里只说"哪支团队"）。
func teamKindOf(registry dto.TeamRegistry) string {
	if kind := strings.TrimSpace(registry.TeamKind); kind != "" {
		return kind
	}
	if teamID := strings.TrimSpace(registry.TeamID); teamID != "" {
		return teamID
	}
	return "本团队"
}

// RolesWithExecutor 是当前有运行时执行者的逻辑角色名（事实表，不是配置事实）：
//
//   - user / main：由宿主驱动（用户输入、主会话 ChatStream），不是"没人执行"；
//   - tl：goal 治理的 ADVISOR 回合执行者（goal 域 TL 评估器真实跑一轮）。
//
// 其余注册角色（review-team 的 reviewer、research-team 的 researcher、自定义
// agent/timer 角色）目前都没有执行者：角色会话建得出来、成员表列得出来，但不会
// 自动产生回合。装配面必须把这个状态说出来（DesignNotice），否则 UI 会让人以为
// 装配完就有人干活。
var RolesWithExecutor = map[string]bool{
	string(dto.RoleKindUser): true,
	string(dto.RoleKindMain): true,
	RoleTechlead:             true,
}

// unexecutedRoles 返回工作顺序里没有执行者的角色（保序、去重）。
func unexecutedRoles(orderRoles []string) []string {
	out := make([]string, 0, len(orderRoles))
	for _, name := range orderRoles {
		if RolesWithExecutor[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}

// UnexecutedRoles 是 unexecutedRoles 的导出形态：发言调度运行态（runtime.go）
// 与成员表投影共用同一份「谁没有执行者」事实，避免两处判定打架。
func UnexecutedRoles(orderRoles []string) []string {
	return unexecutedRoles(orderRoles)
}
