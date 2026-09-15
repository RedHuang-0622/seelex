package agentteam

// global.go — 全局粒度的 AgentTeam 母本（员工库 / 默认顺序）读写面。
//
// 生态位（三份事实各有归属，别混）：
//
//   - registry.go（**会话级**）= 某个会话"当前在编的员工表"，是会话副本；
//   - library.go（**全局级**）= 团队模板库（`<root>/team/library.json`）；
//   - 本文件（**全局级**）= 员工库（母本员工名册）+ 默认顺序（母本发言次序）。
//
// 并发口径（用户口径）：写落全局母本；读在会话侧深拷贝成私有副本后操作。会话内的
// 入职/改序只写会话副本（registry + lifecycle head），**不碰全局**；只有显式的
// 「确认普及搭配到全局」（由 application 层编排 Global + Library）才把会话
// {员工, 顺序} 回写母本。
//
// 内置角色（user/main）不进员工库：它们由会话本身提供，装配时会拒绝实例化。

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// GlobalPort 是全局母本（员工库 + 默认顺序）的读写面；锚定会话在构造适配器时
// 绑定，本包不 import 存储实现、也不解释作用域解析。
type GlobalPort interface {
	ReadEmployeeLibrary() (dto.EmployeeLibrary, error)
	WriteEmployeeLibrary(library dto.EmployeeLibrary) error
	ReadDefaultOrder() (dto.DefaultOrder, error)
	WriteDefaultOrder(order dto.DefaultOrder) error
}

// Global 是全局母本（员工库 + 默认顺序）的读写面。
type Global struct {
	port GlobalPort
}

// NewGlobal 构造全局母本读写面；port 为 nil 时显式报错。
func NewGlobal(port GlobalPort) (*Global, error) {
	if port == nil {
		return nil, errors.New("agentteam: global config port is required")
	}
	return &Global{port: port}, nil
}

// Employees 返回全局员工库。读是深拷贝语义：返回的切片归消费方所有。
func (global *Global) Employees() (dto.EmployeeLibrary, error) {
	if global == nil || global.port == nil {
		return dto.EmployeeLibrary{}, errors.New("agentteam: global config is not assembled")
	}
	stored, err := global.port.ReadEmployeeLibrary()
	if err != nil {
		return dto.EmployeeLibrary{}, err
	}
	if stored.Employees == nil {
		stored.Employees = []dto.RoleSpec{}
	}
	return stored, nil
}

// SaveEmployee 新增/覆盖全局员工库里的一个员工（按 role_name 幂等）。
func (global *Global) SaveEmployee(role dto.RoleSpec) (dto.EmployeeLibrary, error) {
	current, err := global.Employees()
	if err != nil {
		return dto.EmployeeLibrary{}, err
	}
	normalized, err := normalizeEmployeeRole(role)
	if err != nil {
		return dto.EmployeeLibrary{}, err
	}
	employees := make([]dto.RoleSpec, 0, len(current.Employees)+1)
	replaced := false
	for _, existing := range current.Employees {
		if existing.RoleName == normalized.RoleName {
			employees = append(employees, normalized)
			replaced = true
			continue
		}
		employees = append(employees, existing)
	}
	if !replaced {
		employees = append(employees, normalized)
	}
	current.Employees = employees
	if err := global.port.WriteEmployeeLibrary(current); err != nil {
		return dto.EmployeeLibrary{}, err
	}
	return global.Employees()
}

// DeleteEmployee 删除全局员工库里的一个员工（幂等：不存在时原样返回，不报错）。
func (global *Global) DeleteEmployee(roleName string) (dto.EmployeeLibrary, error) {
	roleName = strings.TrimSpace(roleName)
	if roleName == "" {
		return dto.EmployeeLibrary{}, errors.New("agentteam: role_name is required")
	}
	current, err := global.Employees()
	if err != nil {
		return dto.EmployeeLibrary{}, err
	}
	employees := make([]dto.RoleSpec, 0, len(current.Employees))
	found := false
	for _, existing := range current.Employees {
		if existing.RoleName == roleName {
			found = true
			continue
		}
		employees = append(employees, existing)
	}
	if !found {
		return current, nil
	}
	current.Employees = employees
	if err := global.port.WriteEmployeeLibrary(current); err != nil {
		return dto.EmployeeLibrary{}, err
	}
	return global.Employees()
}

// Order 返回全局默认顺序。
func (global *Global) Order() (dto.DefaultOrder, error) {
	if global == nil || global.port == nil {
		return dto.DefaultOrder{}, errors.New("agentteam: global config is not assembled")
	}
	stored, err := global.port.ReadDefaultOrder()
	if err != nil {
		return dto.DefaultOrder{}, err
	}
	if stored.OrderRoles == nil {
		stored.OrderRoles = []string{}
	}
	return stored, nil
}

// SetOrder 写全局默认顺序：引用了员工库不存在的角色会被剔除，user/main 自动补齐
// （与库条目顺序表的规整同口径）。
func (global *Global) SetOrder(policy string, orderRoles []string) (dto.DefaultOrder, error) {
	employees, err := global.Employees()
	if err != nil {
		return dto.DefaultOrder{}, err
	}
	normalized, err := NormalizeDefaultOrder(dto.DefaultOrder{OrderPolicy: policy, OrderRoles: orderRoles}, employees.Employees)
	if err != nil {
		return dto.DefaultOrder{}, err
	}
	if err := global.port.WriteDefaultOrder(normalized); err != nil {
		return dto.DefaultOrder{}, err
	}
	return global.Order()
}

// NormalizeEmployeeLibrary 规整整份员工库：role_name 必填、内置角色剔除、同名后者
// 覆盖前者（与 InstantiateRole 的就地覆盖同口径）。
func NormalizeEmployeeLibrary(library dto.EmployeeLibrary) (dto.EmployeeLibrary, error) {
	index := make(map[string]int, len(library.Employees))
	employees := make([]dto.RoleSpec, 0, len(library.Employees))
	for _, role := range library.Employees {
		normalized, err := NormalizeRole(role)
		if err != nil {
			return dto.EmployeeLibrary{}, err
		}
		if isBuiltinRoleName(normalized.RoleName) {
			continue
		}
		if at, ok := index[normalized.RoleName]; ok {
			employees[at] = normalized
			continue
		}
		index[normalized.RoleName] = len(employees)
		employees = append(employees, normalized)
	}
	library.Employees = employees
	return library, nil
}

// NormalizeDefaultOrder 规整默认顺序：策略校验、顺序表去空去重**保序**、只保留
// user/main 与员工库里存在的角色、user/main 自动补齐到规范位置。
func NormalizeDefaultOrder(order dto.DefaultOrder, employees []dto.RoleSpec) (dto.DefaultOrder, error) {
	policy := strings.TrimSpace(order.OrderPolicy)
	if policy == "" {
		policy = dto.DefaultOrderPolicy
	}
	switch policy {
	case dto.OrderPolicyGoalLoop, dto.OrderPolicyUserMainDecided, dto.OrderPolicyScheduledOnly:
	default:
		return dto.DefaultOrder{}, fmt.Errorf("agentteam: unsupported order policy %q", policy)
	}

	allowed := map[string]struct{}{
		string(dto.RoleKindUser): {},
		string(dto.RoleKindMain): {},
	}
	for _, role := range employees {
		if role.RoleKind == dto.RoleKindTimer {
			// 定时 agent 单独分区，不参与工作顺序（prompt §7.1）。
			continue
		}
		allowed[role.RoleName] = struct{}{}
	}
	names := make([]string, 0, len(order.OrderRoles)+2)
	for _, name := range order.OrderRoles {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := allowed[name]; !ok {
			continue
		}
		names = append(names, name)
	}
	names = dedupePreserveOrder(names)
	if !containsName(names, string(dto.RoleKindUser)) {
		names = append([]string{string(dto.RoleKindUser)}, names...)
	}
	if !containsName(names, string(dto.RoleKindMain)) {
		at := indexOfName(names, string(dto.RoleKindUser)) + 1
		names = append(names, "")
		copy(names[at+1:], names[at:])
		names[at] = string(dto.RoleKindMain)
	}
	order.OrderPolicy = policy
	order.OrderRoles = names
	return order, nil
}

// normalizeEmployeeRole 规整单个员工（复用 NormalizeRole 的默认值口径）并拒绝内置
// 角色：user/main 由会话本身提供，不是"可入职的员工"。
func normalizeEmployeeRole(role dto.RoleSpec) (dto.RoleSpec, error) {
	normalized, err := NormalizeRole(role)
	if err != nil {
		return dto.RoleSpec{}, err
	}
	if isBuiltinRoleName(normalized.RoleName) {
		return dto.RoleSpec{}, fmt.Errorf("agentteam: role %q is builtin and cannot join the employee library", normalized.RoleName)
	}
	return normalized, nil
}
