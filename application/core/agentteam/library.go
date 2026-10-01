package agentteam

// library.go — 「团队库」的读写面（可复用团队模板）。
//
// 生态位（与 registry.go 的分工，别混两份事实）：
//
//   - registry.go（`session/team/roles.json`）= **某个会话**当前在编的员工表；
//   - 本文件（`<root>/team/library.json`）= **全局**团队模板库：一支团队 =
//     角色配置集 + 顺序策略，可装配到任意会话。
//
// 装配路径：库条目 → TeamSpec（SpecOfEntry）→ Factory.Materialize（建角色会话 +
// 写会话 registry + 写 lifecycle 顺序）。因此"新建团队"与"入职员工"最终落在同一
// 套既有落盘通道上，本包不新增第二份顺序或角色事实。
//
// 并发口径（用户口径）：库的**写落全局**；读在会话侧深拷贝成私有副本后操作；
// 会话内的改动只落会话副本，只有显式「确认普及搭配到全局」才回写库。
//
// **没有内置团队模板**（2026-10-01）：库条目就是唯一的团队来源（用户数据）。
// 旧的"内置形态目录"（`presets.go`：goal-a2a / review-team / research-team）已删除，
// 所以"新建团队"不再有"从模板起手"这条路——起手方式只剩"空白"与"从当前会话填充"。

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ErrUnknownTeam 表示请求的团队库条目不存在。
var ErrUnknownTeam = errors.New("agentteam: unknown team in library")

// LibraryPort 是团队库的读写面；作用域由实现方（application 适配器）解析，
// 本包不 import 存储实现。
type LibraryPort interface {
	ReadTeamLibrary() (dto.TeamLibrary, error)
	// WriteTeamLibrary 整份替换式写入（与角色注册表同形态）。
	WriteTeamLibrary(library dto.TeamLibrary) error
}

// Library 是团队库的读写面。
type Library struct {
	port LibraryPort
}

// NewLibrary 构造团队库读写面；port 为 nil 时显式报错。
func NewLibrary(port LibraryPort) (*Library, error) {
	if port == nil {
		return nil, errors.New("agentteam: team library port is required")
	}
	return &Library{port: port}, nil
}

// View 返回团队库全文（按 name 排序由存储层保证；这里只做防御性规整）。
func (library *Library) View() (dto.TeamLibrary, error) {
	if library == nil || library.port == nil {
		return dto.TeamLibrary{}, errors.New("agentteam: team library is not assembled")
	}
	stored, err := library.port.ReadTeamLibrary()
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	if stored.Teams == nil {
		stored.Teams = []dto.TeamLibraryEntry{}
	}
	return stored, nil
}

// Entry 读单条团队库条目（装配入口用；不存在时返回 ErrUnknownTeam）。
func (library *Library) Entry(teamID string) (dto.TeamLibraryEntry, error) {
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return dto.TeamLibraryEntry{}, errors.New("agentteam: team_id is required")
	}
	current, err := library.View()
	if err != nil {
		return dto.TeamLibraryEntry{}, err
	}
	for _, entry := range current.Teams {
		if entry.TeamID == teamID {
			return entry, nil
		}
	}
	return dto.TeamLibraryEntry{}, fmt.Errorf("%w: %s", ErrUnknownTeam, teamID)
}

// SaveTeam 新增或覆盖一条团队库条目（按 team_id 幂等），返回整份库。
func (library *Library) SaveTeam(entry dto.TeamLibraryEntry) (dto.TeamLibrary, error) {
	if library == nil || library.port == nil {
		return dto.TeamLibrary{}, errors.New("agentteam: team library is not assembled")
	}
	normalized, err := NormalizeLibraryEntry(entry)
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	current, err := library.View()
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	teams := make([]dto.TeamLibraryEntry, 0, len(current.Teams)+1)
	replaced := false
	for _, existing := range current.Teams {
		if existing.TeamID == normalized.TeamID {
			teams = append(teams, normalized)
			replaced = true
			continue
		}
		teams = append(teams, existing)
	}
	if !replaced {
		teams = append(teams, normalized)
	}
	current.Teams = teams
	current.Configured = true
	if err := library.port.WriteTeamLibrary(current); err != nil {
		return dto.TeamLibrary{}, err
	}
	return library.View()
}

// DeleteTeam 删除一条团队库条目（幂等：不存在时原样返回，不报错）。
func (library *Library) DeleteTeam(teamID string) (dto.TeamLibrary, error) {
	if library == nil || library.port == nil {
		return dto.TeamLibrary{}, errors.New("agentteam: team library is not assembled")
	}
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return dto.TeamLibrary{}, errors.New("agentteam: team_id is required")
	}
	current, err := library.View()
	if err != nil {
		return dto.TeamLibrary{}, err
	}
	teams := make([]dto.TeamLibraryEntry, 0, len(current.Teams))
	found := false
	for _, entry := range current.Teams {
		if entry.TeamID == teamID {
			found = true
			continue
		}
		teams = append(teams, entry)
	}
	if !found {
		return current, nil
	}
	current.Teams = teams
	if err := library.port.WriteTeamLibrary(current); err != nil {
		return dto.TeamLibrary{}, err
	}
	return library.View()
}

// NormalizeLibraryEntry 规整一条团队库条目：team_id 必填（缺省由 team_kind 兜底）、
// 名字缺省取 team_id、角色清单按 role_name 去重（同名后者覆盖前者，与入职路径
// 同口径）、顺序表去空去重**保序**、user/main 自动补齐进顺序表。
func NormalizeLibraryEntry(entry dto.TeamLibraryEntry) (dto.TeamLibraryEntry, error) {
	entry.TeamID = strings.TrimSpace(entry.TeamID)
	entry.TeamKind = strings.TrimSpace(entry.TeamKind)
	if entry.TeamID == "" {
		entry.TeamID = entry.TeamKind
	}
	if entry.TeamID == "" {
		return dto.TeamLibraryEntry{}, errors.New("agentteam: team_id is required")
	}
	if entry.Name = strings.TrimSpace(entry.Name); entry.Name == "" {
		entry.Name = entry.TeamID
	}
	entry.TeamKind = firstNonEmpty(entry.TeamKind, entry.TeamID)
	entry.OrderPolicy = firstNonEmpty(strings.TrimSpace(entry.OrderPolicy), dto.DefaultOrderPolicy)
	switch entry.OrderPolicy {
	case dto.OrderPolicyGoalLoop, dto.OrderPolicyUserMainDecided, dto.OrderPolicyScheduledOnly:
	default:
		return dto.TeamLibraryEntry{}, fmt.Errorf("agentteam: unsupported order policy %q", entry.OrderPolicy)
	}
	entry.Origin = firstNonEmpty(strings.TrimSpace(entry.Origin), "custom")

	seen := make(map[string]struct{}, len(entry.Roles))
	roles := make([]dto.RoleSpec, 0, len(entry.Roles))
	for _, role := range entry.Roles {
		normalized, err := NormalizeRole(role)
		if err != nil {
			return dto.TeamLibraryEntry{}, err
		}
		if _, ok := seen[normalized.RoleName]; ok {
			// 同名后者覆盖前者（与 InstantiateRole 的就地覆盖同口径），不报错：
			// 库条目是用户数据，重名在 UI 上已经不可能同时存在。
			for index := range roles {
				if roles[index].RoleName == normalized.RoleName {
					roles[index] = normalized
					break
				}
			}
			continue
		}
		seen[normalized.RoleName] = struct{}{}
		roles = append(roles, normalized)
	}
	entry.Roles = roles

	order := make([]string, 0, len(entry.OrderRoles)+2)
	for _, name := range entry.OrderRoles {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok || isBuiltinRoleName(name) {
			order = append(order, name)
		}
	}
	order = dedupePreserveOrder(order)
	// user/main 是群聊的起手与收口：库条目的顺序表必须含它们（否则装配时
	// Normalize 会拒绝），缺就补到规范位置。
	if !containsName(order, string(dto.RoleKindUser)) {
		order = append([]string{string(dto.RoleKindUser)}, order...)
	}
	if !containsName(order, string(dto.RoleKindMain)) {
		at := indexOfName(order, string(dto.RoleKindUser)) + 1
		order = append(order, "")
		copy(order[at+1:], order[at:])
		order[at] = string(dto.RoleKindMain)
	}
	entry.OrderRoles = order
	return entry, nil
}

func containsName(values []string, name string) bool {
	return indexOfName(values, name) >= 0
}

func indexOfName(values []string, name string) int {
	for index, value := range values {
		if value == name {
			return index
		}
	}
	return -1
}

func isBuiltinRoleName(name string) bool {
	_, ok := builtinKinds[name]
	return ok
}

// IsBuiltinRole 判定角色名是否是内置角色（user/main）。内置角色由会话本身提供，
// 不是"可入职/可入库的员工"：员工库与团队库条目都必须把它们排除在外。
func IsBuiltinRole(roleName string) bool {
	return isBuiltinRoleName(strings.TrimSpace(roleName))
}

// dedupePreserveOrder 去重但保序（顺序表是发言次序，不能排序）。
func dedupePreserveOrder(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SpecOfEntry 把团队库条目投影成装配输入（TeamSpec）。顺序与角色配置原样带入，
// 由 Normalize 再校验一次——库条目与 preset 走同一条装配路径。
func SpecOfEntry(entry dto.TeamLibraryEntry) dto.TeamSpec {
	return dto.TeamSpec{
		TeamID:        entry.TeamID,
		TeamKind:      firstNonEmpty(entry.TeamKind, entry.TeamID),
		OrderPolicy:   firstNonEmpty(entry.OrderPolicy, dto.DefaultOrderPolicy),
		OrderRoles:    append([]string(nil), entry.OrderRoles...),
		Roles:         append([]dto.RoleSpec(nil), entry.Roles...),
		GatePolicy:    entry.GatePolicy,
		CompactPolicy: entry.CompactPolicy,
	}
}

// EntryFromSpec 把一次性 TeamSpec 投影成团队库条目（"把这份配置存成一支可复用团队"）。
func EntryFromSpec(spec dto.TeamSpec, name, origin string) (dto.TeamLibraryEntry, error) {
	normalized, err := Normalize(spec)
	if err != nil {
		return dto.TeamLibraryEntry{}, err
	}
	entry := dto.TeamLibraryEntry{
		TeamID:        normalized.TeamID,
		TeamKind:      normalized.TeamKind,
		Name:          firstNonEmpty(strings.TrimSpace(name), normalized.TeamKind),
		OrderPolicy:   normalized.OrderPolicy,
		OrderRoles:    append([]string(nil), normalized.OrderRoles...),
		Roles:         append([]dto.RoleSpec(nil), normalized.Roles...),
		GatePolicy:    normalized.GatePolicy,
		CompactPolicy: normalized.CompactPolicy,
		Origin:        firstNonEmpty(strings.TrimSpace(origin), "custom"),
	}
	return NormalizeLibraryEntry(entry)
}

// EntryFromRegistry 把"某个会话当前在编的员工表"投影成一条团队库条目
// （UI 的"把当前团队存进团队库"）。orderRoles 是会话 lifecycle 里的实际顺序
// （空则按 OrderPriority 推导只读顺序）。
func EntryFromRegistry(registry dto.TeamRegistry, orderRoles []string, name, teamID string) (dto.TeamLibraryEntry, error) {
	roles := make([]dto.RoleSpec, 0, len(registry.Roles))
	for _, role := range registry.Roles {
		if isBuiltinRoleName(role.RoleName) {
			// user/main 由会话本身提供，不是"员工"，不进库条目（否则装配时会
			// 试图实例化内置角色而报错）。
			continue
		}
		roles = append(roles, role)
	}
	teamKind := firstNonEmpty(strings.TrimSpace(registry.TeamKind), strings.TrimSpace(registry.TeamID))
	if len(orderRoles) == 0 {
		orderRoles = OrderRolesOf(roles)
	}
	entry := dto.TeamLibraryEntry{
		TeamID:      firstNonEmpty(strings.TrimSpace(teamID), strings.TrimSpace(registry.TeamID), teamKind),
		TeamKind:    teamKind,
		Name:        firstNonEmpty(strings.TrimSpace(name), strings.TrimSpace(registry.TeamID), teamKind),
		OrderPolicy: firstNonEmpty(strings.TrimSpace(registry.OrderPolicy), dto.DefaultOrderPolicy),
		OrderRoles:  append([]string(nil), orderRoles...),
		Roles:       roles,
		Origin:      "current-session",
	}
	return NormalizeLibraryEntry(entry)
}

// OrderRolesOf 从角色配置推导工作顺序：user → main → 其余角色（OrderPriority
// 升序，同级按登记次序），与 registry.View 的只读推导同口径。
func OrderRolesOf(roles []dto.RoleSpec) []string {
	order := []string{string(dto.RoleKindUser), string(dto.RoleKindMain)}
	rest := make([]dto.RoleSpec, 0, len(roles))
	for _, role := range roles {
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
	return order
}
