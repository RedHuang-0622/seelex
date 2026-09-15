// team_global.go：AgentTeam 的**全局母本**（团队库 / 员工库 / 默认顺序）。
//
// 作用域（用户口径）：三份事实都是**全局粒度**，落在数据根 `<root>/team/`，
// 不随项目/会话分目录：
//
//	team/library.json    团队库：可复用团队模板（角色配置集 + 顺序策略 + gate/compact）
//	team/employees.json  员工库：全局员工名册（可复用 RoleSpec 池）
//	team/order.json      默认顺序：会话/团队未显式编排时的默认发言次序
//
// 并发口径（用户口径）：**写落全局母本；读在会话侧深拷贝成私有副本**。会话内的
// 改动（入职/改序）只写会话副本（`session/team/roles.json` + lifecycle head），
// 不碰全局；只有显式「确认普及搭配到全局」才把会话 {员工, 顺序} 回写母本。
//
// 三份文件同形态：整份替换型、按路径写锁、schema 版本、重复键显式报错；
// 不写 message、不碰 sequencer（与 team_registry.go / team_library.go 同边界）。
//
// 兼容（旧项目级团队库）：老版本团队库落在 `project-<hash>/teams/library.json`。
// 全局库不存在时读侧做**只读回退**到锚定项目的旧库（不搬数据、不删旧文件）；
// 回退读到的条目会在下一次整份写入里自然并入全局，因此"改一次库"即完成迁移，
// 不需要单独的迁移脚本，也不会在启动/读路径上偷偷写盘。
package sessionstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TeamGlobalSchemaVersion 是全局母本文件（员工库 / 默认顺序）的 schema 版本。
const TeamGlobalSchemaVersion = 1

// EmployeeLibrary 是全局「员工库」：可复用员工的全局名册（RoleSpec 池）。
// Configured 由读侧推导，不落盘：文件不存在 = 还没建员工库。
type EmployeeLibrary struct {
	SchemaVersion int            `json:"schema_version"`
	Employees     []TeamRoleSpec `json:"employees"`
	UpdatedAt     time.Time      `json:"updated_at,omitempty"`
	Configured    bool           `json:"-"`
}

// DefaultOrder 是全局「默认顺序」：没有任何会话/团队显式编排顺序时的默认发言次序。
// order_policy 与 order_roles 的运行时权威仍是各会话的 lifecycle head；本文件只做
// "默认值"这条登记事实。
type DefaultOrder struct {
	SchemaVersion int       `json:"schema_version"`
	OrderPolicy   string    `json:"order_policy,omitempty"`
	OrderRoles    []string  `json:"order_roles,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
	Configured    bool      `json:"-"`
}

// teamConfigLocks 是全局母本文件的按路径写锁（整份替换型文件只需串行化同路径的
// 读改写；三份文件各自一把锁，互不阻塞）。
var teamConfigLocks sync.Map

func teamConfigLock(path string) *sync.Mutex {
	value, _ := teamConfigLocks.LoadOrStore(path, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// teamGlobalDir 返回全局母本目录（数据根下的 team/，不随项目分目录）。
func (store *storeEngine) teamGlobalDir() string {
	return filepath.Join(store.root, "team")
}

func (store *storeEngine) teamGlobalLibraryPath() string {
	return filepath.Join(store.teamGlobalDir(), "library.json")
}

func (store *storeEngine) teamGlobalEmployeesPath() string {
	return filepath.Join(store.teamGlobalDir(), "employees.json")
}

func (store *storeEngine) teamGlobalOrderPath() string {
	return filepath.Join(store.teamGlobalDir(), "order.json")
}

// normalizeEmployeeLibrary 规整整份员工库：版本校验、role_name 必填、重复角色显式
// 报错（不静默丢弃），排序用与注册表同口径的 OrderPriority → role_name（保证同一份
// 库写出字节稳定，便于幂等比对）。
func normalizeEmployeeLibrary(library EmployeeLibrary) (EmployeeLibrary, error) {
	if library.SchemaVersion == 0 {
		library.SchemaVersion = TeamGlobalSchemaVersion
	}
	if library.SchemaVersion != TeamGlobalSchemaVersion {
		return EmployeeLibrary{}, fmt.Errorf("session storage: unsupported employee library schema %d", library.SchemaVersion)
	}
	if len(library.Employees) == 0 {
		library.Employees = nil
		return library, nil
	}
	seen := make(map[string]struct{}, len(library.Employees))
	employees := make([]TeamRoleSpec, 0, len(library.Employees))
	for _, role := range library.Employees {
		role = normalizeTeamRoleSpec(role)
		if role.RoleName == "" {
			return EmployeeLibrary{}, errors.New("session storage: employee library role_name is required")
		}
		if _, ok := seen[role.RoleName]; ok {
			return EmployeeLibrary{}, fmt.Errorf("session storage: duplicate employee library role %q", role.RoleName)
		}
		seen[role.RoleName] = struct{}{}
		employees = append(employees, role)
	}
	sort.SliceStable(employees, func(i, j int) bool {
		if employees[i].OrderPriority != employees[j].OrderPriority {
			return employees[i].OrderPriority < employees[j].OrderPriority
		}
		return employees[i].RoleName < employees[j].RoleName
	})
	library.Employees = employees
	return library, nil
}

// normalizeDefaultOrder 规整整份默认顺序：版本校验、policy 去空白、顺序表去空去重
// 保序（顺序是发言次序，不能排序）。是否含 user/main、是否引用了员工库里的角色由
// 应用层校验（存储层只做形态规整）。
func normalizeDefaultOrder(order DefaultOrder) (DefaultOrder, error) {
	if order.SchemaVersion == 0 {
		order.SchemaVersion = TeamGlobalSchemaVersion
	}
	if order.SchemaVersion != TeamGlobalSchemaVersion {
		return DefaultOrder{}, fmt.Errorf("session storage: unsupported default order schema %d", order.SchemaVersion)
	}
	order.OrderPolicy = strings.TrimSpace(order.OrderPolicy)
	order.OrderRoles = normalizeNameList(order.OrderRoles)
	return order, nil
}

// readTeamLibraryGlobal 读全局团队库。文件不存在返回 Configured=false 的空库
// （未建库不是错误）；文件损坏显式报错，不返回半份配置。
func (store *storeEngine) readTeamLibraryGlobal() (TeamLibrary, error) {
	data, err := os.ReadFile(store.teamGlobalLibraryPath())
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return TeamLibrary{SchemaVersion: TeamLibrarySchemaVersion}, nil
	case err != nil:
		return TeamLibrary{}, err
	}
	var library TeamLibrary
	if err := json.Unmarshal(data, &library); err != nil {
		return TeamLibrary{}, errors.New("session storage: decode team library")
	}
	if library.SchemaVersion == 0 {
		library.SchemaVersion = TeamLibrarySchemaVersion
	}
	library.Configured = true
	return library, nil
}

// writeTeamLibraryGlobal 整份原子替换全局团队库；替换成功即发布。
func (store *storeEngine) writeTeamLibraryGlobal(library TeamLibrary) error {
	library, err := normalizeTeamLibrary(library)
	if err != nil {
		return err
	}
	library.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(library, "", "  ")
	if err != nil {
		return err
	}
	path := store.teamGlobalLibraryPath()
	lock := teamConfigLock(path)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600)
}

// readTeamLibraryForScope 读团队库：全局母本优先；全局库不存在时只读回退到锚定
// 项目的旧项目级库。回退读到的条目会在下一次整份写入里自然并入全局（见文件头）。
func (store *storeEngine) readTeamLibraryForScope(projectID string) (TeamLibrary, error) {
	global, err := store.readTeamLibraryGlobal()
	if err != nil {
		return TeamLibrary{}, err
	}
	if global.Configured {
		return global, nil
	}
	return store.readLegacyTeamLibrary(projectID)
}

// readEmployeeLibrary 读全局员工库。文件不存在返回 Configured=false 的空库。
func (store *storeEngine) readEmployeeLibrary() (EmployeeLibrary, error) {
	data, err := os.ReadFile(store.teamGlobalEmployeesPath())
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return EmployeeLibrary{SchemaVersion: TeamGlobalSchemaVersion}, nil
	case err != nil:
		return EmployeeLibrary{}, err
	}
	var library EmployeeLibrary
	if err := json.Unmarshal(data, &library); err != nil {
		return EmployeeLibrary{}, errors.New("session storage: decode employee library")
	}
	if library.SchemaVersion == 0 {
		library.SchemaVersion = TeamGlobalSchemaVersion
	}
	library.Configured = true
	return library, nil
}

// writeEmployeeLibrary 整份原子替换全局员工库；替换成功即发布。
func (store *storeEngine) writeEmployeeLibrary(library EmployeeLibrary) error {
	library, err := normalizeEmployeeLibrary(library)
	if err != nil {
		return err
	}
	library.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(library, "", "  ")
	if err != nil {
		return err
	}
	path := store.teamGlobalEmployeesPath()
	lock := teamConfigLock(path)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600)
}

// readDefaultOrder 读全局默认顺序。文件不存在返回 Configured=false 的空顺序。
func (store *storeEngine) readDefaultOrder() (DefaultOrder, error) {
	data, err := os.ReadFile(store.teamGlobalOrderPath())
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return DefaultOrder{SchemaVersion: TeamGlobalSchemaVersion}, nil
	case err != nil:
		return DefaultOrder{}, err
	}
	var order DefaultOrder
	if err := json.Unmarshal(data, &order); err != nil {
		return DefaultOrder{}, errors.New("session storage: decode default order")
	}
	if order.SchemaVersion == 0 {
		order.SchemaVersion = TeamGlobalSchemaVersion
	}
	order.Configured = true
	return order, nil
}

// writeDefaultOrder 整份原子替换全局默认顺序；替换成功即发布。
func (store *storeEngine) writeDefaultOrder(order DefaultOrder) error {
	order, err := normalizeDefaultOrder(order)
	if err != nil {
		return err
	}
	order.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(order, "", "  ")
	if err != nil {
		return err
	}
	path := store.teamGlobalOrderPath()
	lock := teamConfigLock(path)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600)
}

// ReadTeamLibraryGlobal 读**全局**团队库（全局母本优先，缺失时只读回退到锚定
// 项目的旧项目级库）。projectID 只用于回退定位，不改变全局读语义。
func (router *Router) ReadTeamLibraryGlobal(projectID string) (TeamLibrary, error) {
	var library TeamLibrary
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: team library requires session layout")
		}
		var err error
		library, err = layout.layout.readTeamLibraryForScope(projectID)
		return err
	})
	return library, err
}

// WriteTeamLibraryGlobal 整份写入**全局**团队库。
func (router *Router) WriteTeamLibraryGlobal(library TeamLibrary) error {
	return router.withRepositoryAt("", func(repository Repository, _ string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: team library requires session layout")
		}
		return layout.layout.writeTeamLibraryGlobal(library)
	})
}

// ReadEmployeeLibraryGlobal 读全局员工库（未建库返回空库，不是错误）。
func (router *Router) ReadEmployeeLibraryGlobal() (EmployeeLibrary, error) {
	var library EmployeeLibrary
	err := router.withRepositoryAt("", func(repository Repository, _ string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: employee library requires session layout")
		}
		var err error
		library, err = layout.layout.readEmployeeLibrary()
		return err
	})
	return library, err
}

// WriteEmployeeLibraryGlobal 整份写入全局员工库。
func (router *Router) WriteEmployeeLibraryGlobal(library EmployeeLibrary) error {
	return router.withRepositoryAt("", func(repository Repository, _ string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: employee library requires session layout")
		}
		return layout.layout.writeEmployeeLibrary(library)
	})
}

// ReadDefaultOrderGlobal 读全局默认顺序（未建返回空顺序，不是错误）。
func (router *Router) ReadDefaultOrderGlobal() (DefaultOrder, error) {
	var order DefaultOrder
	err := router.withRepositoryAt("", func(repository Repository, _ string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: default order requires session layout")
		}
		var err error
		order, err = layout.layout.readDefaultOrder()
		return err
	})
	return order, err
}

// WriteDefaultOrderGlobal 整份写入全局默认顺序。
func (router *Router) WriteDefaultOrderGlobal(order DefaultOrder) error {
	return router.withRepositoryAt("", func(repository Repository, _ string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: default order requires session layout")
		}
		return layout.layout.writeDefaultOrder(order)
	})
}
