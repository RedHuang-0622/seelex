package host

// role_permission_port.go 是「角色设置」在装配侧的落地：把**会话端口**补上
// application/assembly 用类型断言发现的两个可选能力——
//
//	rolePermissionPort      —— 会话级角色权限登记（键 (sessionID, role)，落盘在 store）
//	rolePermissionLayerPort —— 五层向量来源（user_allow > agent_dispatch >
//	                           session_default > role_default > global_default）
//
// 生态位（为什么是适配层、为什么带一份按 Router 的实例表）：
//   - 登记的**事实源**只有一处：store.RolePermissionStore（每 (项目, 会话, 角色) 一格、
//     各自持锁、按项目一把文件锁）。本文件只做"会话 → 项目 → 登记格"的转发，
//     不缓存第二份向量（应用层也不缓存，判据在 contract/dto）。
//   - store 的登记格**在进程内缓存已加载的条目**，因此同一个数据根必须共用**同一个**
//     RolePermissionStore 实例：装配期由本文件按 Router 建一次并复用。若每次调用新建
//     一个实例，两个实例各持一份缓存，A 写入后 B 读到的还是它加载时的旧副本——
//     判定链前后不一致（正是 store 注释里点名的失败形态）。
//   - 五层来源的**判据**不在这里（在 dto.ResolvePermissionVector / ResolveRoleSettings）；
//     这里只负责"每一层今天从哪读、没有就留空层"。空层 = 这一层没表态，不是"允许"。
//
// 与权限门的边界：这里产出的是**组级**向量标签（ro / rw / rw_session / rw_desktop /
// ctl / adm），用于装配与展示；逐位（r/w/x）与资源限定（项目 / 本会话 / 共享外设）
// 的判定仍在 backend/tool/registry 的 PermissionGate。本文件不重新实现位判定，
// 只把"该组有没有位"投影成标签——位为 0 的组投影成 `!tag`（显式不给），否则低层
// 会把"这一层说了 0 位"读成"没提"，进而按更低层放行（权限放大的经典形态）。

import (
	"errors"
	"strings"
	"sync"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store"
)

// rolePermissionStores 是「数据根（Router）→ 登记存储」的实例表。
//
// 只增不删：数据根在进程生命周期内是固定的少数几个（真机一个；测试各自一个
// t.TempDir）。键是指针身份——同一个数据根必然来自同一个 Router 实例。
var rolePermissionStores sync.Map // *store.Router -> *store.RolePermissionStore

// rolePermissionStoreFor 取该数据根的登记存储（首次调用建实例，其后复用）。
func rolePermissionStoreFor(router *store.Router) *store.RolePermissionStore {
	if router == nil {
		return nil
	}
	if cached, ok := rolePermissionStores.Load(router); ok {
		return cached.(*store.RolePermissionStore)
	}
	created, _ := rolePermissionStores.LoadOrStore(router, store.NewRolePermissionStore(router))
	return created.(*store.RolePermissionStore)
}

// rolePermissionStore 返回本端口的登记存储（存储未装配时 nil）。
func (port SessionPort) rolePermissionStore() *store.RolePermissionStore {
	if port.Manager == nil {
		return nil
	}
	return rolePermissionStoreFor(port.Manager.Router())
}

// rolePermissionProject 解析会话归属的项目（登记的键是 (项目, 会话, 角色)）。
//
// 与 SetSessionMeta / SetSessionPermissionTier 同一条解析（granular 的
// ResolveProjectForSession）：归属按"数据实际所在"定位，不按当前活跃写作用域猜。
func (port SessionPort) rolePermissionProject(sessionID string) string {
	if port.Manager == nil {
		return ""
	}
	return port.granular().ResolveProjectForSession(sessionID)
}

// RoleDefaultVectors 实现 application/assembly 的 roleDefaultPort：该数据根下所有显式
// 定过的角色默认向量（权限面板一次读回全部角色）。
//
// 归属项目的解析与会话级登记同一条（ResolveProjectForSession）：默认按项目存一份，
// 换项目就是另一份——不把 A 项目的默认套到 B 项目上。
func (port SessionPort) RoleDefaultVectors(sessionID string) (map[string][]string, error) {
	storage := port.rolePermissionStore()
	if storage == nil {
		return nil, errors.New("role default: session storage is not assembled")
	}
	return storage.Defaults(port.rolePermissionProject(sessionID))
}

// RoleDefault 实现 roleDefaultPort：读单个角色的默认；第二个返回值报告"是否定过"。
func (port SessionPort) RoleDefault(sessionID, role string) ([]string, bool, error) {
	storage := port.rolePermissionStore()
	if storage == nil {
		return nil, false, errors.New("role default: session storage is not assembled")
	}
	return storage.Default(port.rolePermissionProject(sessionID), strings.TrimSpace(role))
}

// SetRoleDefault 实现 roleDefaultPort：写一个角色的默认向量。
func (port SessionPort) SetRoleDefault(sessionID, role string, vector []string) error {
	storage := port.rolePermissionStore()
	if storage == nil {
		return errors.New("role default: session storage is not assembled")
	}
	_, err := storage.PutDefault(port.rolePermissionProject(sessionID), strings.TrimSpace(role), vector)
	return err
}

func (port SessionPort) RoleAccount(sessionID, role string) (string, bool, error) {
	storage := port.rolePermissionStore()
	if storage == nil {
		return "", false, errors.New("role account: session storage is not assembled")
	}
	return storage.RoleAccount(port.rolePermissionProject(sessionID), strings.TrimSpace(role))
}

func (port SessionPort) SetRoleAccount(sessionID, role, account string) error {
	storage := port.rolePermissionStore()
	if storage == nil {
		return errors.New("role account: session storage is not assembled")
	}
	return storage.SetRoleAccount(port.rolePermissionProject(sessionID), strings.TrimSpace(role), strings.TrimSpace(account))
}

func (port SessionPort) RoleInheritance(sessionID, role string) (bool, bool, error) {
	storage := port.rolePermissionStore()
	if storage == nil {
		return false, false, errors.New("role inheritance: session storage is not assembled")
	}
	return storage.RoleInheritance(port.rolePermissionProject(sessionID), strings.TrimSpace(role))
}

func (port SessionPort) SetRoleInheritance(sessionID, role string, inherit bool) error {
	storage := port.rolePermissionStore()
	if storage == nil {
		return errors.New("role inheritance: session storage is not assembled")
	}
	return storage.SetRoleInheritance(port.rolePermissionProject(sessionID), strings.TrimSpace(role), inherit)
}

func (port SessionPort) RoleSSHUserSpaces(sessionID, role string) ([]string, error) {
	storage := port.rolePermissionStore()
	if storage == nil {
		return nil, errors.New("role user spaces: session storage is not assembled")
	}
	return storage.RoleEffectiveSSHUserSpaces(
		port.rolePermissionProject(sessionID), strings.TrimSpace(sessionID), strings.TrimSpace(role),
	)
}

func (port SessionPort) RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error) {
	storage := port.rolePermissionStore()
	if storage == nil {
		return nil, errors.New("role user spaces: session storage is not assembled")
	}
	return storage.RoleSSHUserSpaces(port.rolePermissionProject(sessionID), strings.TrimSpace(role))
}

func (port SessionPort) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error {
	storage := port.rolePermissionStore()
	if storage == nil {
		return errors.New("role user spaces: session storage is not assembled")
	}
	return storage.SetRoleSSHUserSpaces(port.rolePermissionProject(sessionID), strings.TrimSpace(role), paths)
}

func (port SessionPort) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error {
	storage := port.rolePermissionStore()
	if storage == nil {
		return errors.New("role user spaces: session storage is not assembled")
	}
	return storage.SetRoleSessionSSHUserSpaces(
		port.rolePermissionProject(sessionID), strings.TrimSpace(sessionID), strings.TrimSpace(role), paths,
	)
}

// RolePermissionVectors 实现 application/assembly 的 rolePermissionPort：返回该会话下
// **所有显式登记过**的角色 → 权限向量（会话加载点一次读回全部角色）。
func (port SessionPort) RolePermissionVectors(sessionID string) (map[string][]string, error) {
	storage := port.rolePermissionStore()
	if storage == nil {
		return nil, errors.New("role permission: session storage is not assembled")
	}
	return storage.Vectors(port.rolePermissionProject(sessionID), strings.TrimSpace(sessionID))
}

// RolePermissionVector 实现 rolePermissionPort：读单个角色；第二个返回值报告
// "是否登记过"（"显式空集"与"从未定过"必须分得开）。
func (port SessionPort) RolePermissionVector(sessionID, role string) ([]string, bool, error) {
	storage := port.rolePermissionStore()
	if storage == nil {
		return nil, false, errors.New("role permission: session storage is not assembled")
	}
	return storage.Vector(port.rolePermissionProject(sessionID), strings.TrimSpace(sessionID), strings.TrimSpace(role))
}

// SetRolePermissionVector 实现 rolePermissionPort：写入一个角色的权限向量
// （空集合法 = 显式不给任何能力；"删除登记"是另一件事，由 store 的 Drop 负责）。
//
// 规整（大小写/`!` 前缀/非法项报错）在调用方（application/assembly 走 dto.NormalizePermissionVector）
// 做完才下来；本端口不再做第二遍判据。
func (port SessionPort) SetRolePermissionVector(sessionID, role string, vector []string) error {
	storage := port.rolePermissionStore()
	if storage == nil {
		return errors.New("role permission: session storage is not assembled")
	}
	_, err := storage.Put(port.rolePermissionProject(sessionID), strings.TrimSpace(sessionID), strings.TrimSpace(role), vector)
	return err
}

// RolePermissionLayers 实现 application/assembly 的 rolePermissionLayerPort：给出该角色
// 在五层来源上的取值（层序由 dto.RoleSettingsLayers.Ordered 定死，本文件不重排）。
//
// 五层今天的事实来源（缺一个来源就是那层留空，**不编默认值**）：
//
//	user_allow      用户的显式允许。会话内"用户点过头"的台账今天不存在（审批面板的
//	                放行是即时决定，不落会话台账），故这一层恒为空层。
//	agent_dispatch  派活时携带的显式权限格子（dto.RoleSpec.PermissionGroups）：装配期
//	                由 assemble.Runtime.AssignEmployeePermissions 落成主体 emp_<角色>，
//	                这里按角色名读回。**非空即封闭**——未列出的组投影成 `!tag`。
//	session_default 该角色在本会话的 ToolsPolicy 档位派生（readonly / readwrite，与
//	                emp_ro / emp_rw 同口径，唯一实现在 EmployeeGroupsForPolicy）。
//	global_default  全局默认授权表（policy.DefaultPermissionSubjects）里对应主体类的
//	                默认位；档位认不出时按宿主默认（root）——与 ClassForToolsPolicy 的
//	                兜底一致，不另造一条兜底规则。
//
// 已知边界（写在这里，免得下一个人以为漏了）：员工权限按**角色名**落在进程级权责表，
// 同一角色名在不同会话共享同一条派发约束——这是权责表今天的形状，不是本端口的取舍。
func (port SessionPort) RolePermissionLayers(sessionID, role string) dto.RoleSettingsLayers {
	_ = sessionID // 五层来源今天都是进程级读数；签名保留会话维度给后续按会话的台账
	role = strings.TrimSpace(role)
	layers := dto.RoleSettingsLayers{
		// 空层 = "这一层没表态"，不是"允许"（判据在 dto，不在本文件）。
		UserAllow: dto.RoleSettings{},
	}
	permission, assigned := port.employeePermission(role)
	if assigned {
		// 显式格子 = 派发方说"这次就这些"：非空即封闭，逐组给位/不给位都要说出来。
		if len(permission.Groups) > 0 {
			layers.AgentDispatch = dto.RoleSettings{PermissionVector: permissionVectorTags(permission.Groups)}
		}
		// 档位派生 = 这个会话里该角色的默认能力面（readonly → 只读，readwrite → 读+项目写）。
		if groups := toolreg.EmployeeGroupsForPolicy(permission.Policy); groups != nil {
			layers.SessionDefault = dto.RoleSettings{PermissionVector: permissionVectorTags(groups)}
		}
	}
	class := policy.SubjectClassRoot
	if assigned {
		class = policy.ClassForToolsPolicy(permission.Policy)
	}
	if assigned && class == policy.SubjectClassRoot {
		// "继承宿主默认"的员工：全局默认层就是宿主（root）的默认位。这一层只在**明确
		// 继承**时才有话可说。
		grant := policy.DefaultPermissionSubjects()[policy.SubjectForClass(class)]
		layers.GlobalDefault = dto.RoleSettings{PermissionVector: permissionVectorTags(grantBits(grant))}
	}
	// 未装配 / 员工档：全局默认层留空（= 这一层没表态）。
	//
	// 为什么不能拿 root 的默认位兜底（2026-10-21 收紧）：执行面现在按"这几层叠出来的
	// 向量"判定员工的授权面，若这一层在员工身份不明时补一份 root 的全组位，那就等于
	// "谁都不知道你是谁 ⇒ 给你主代理的全部能力"——典型的 fail-open。留空后，
	// 未登记/未装配的角色由执行面按 ctx 身份（显式格子 → 档位）派生，方向上只会更窄。
	// 角色级默认（权限面板里给这个角色定的"默认拿什么"）是一个独立层：它压住
	// global_default，但仍低于会话、派发与用户显式允许。过去把它塞进 global_default
	// 会让 UI 写着“角色默认”而实际被 session_default 静默盖掉，属于两份语义共用一格。
	if storage := port.rolePermissionStore(); storage != nil {
		projectID := port.rolePermissionProject(sessionID)
		inheritMain := role == string(dto.RoleSubAgent) || role == string(dto.RoleTeammate)
		if value, explicit, err := storage.RoleInheritance(projectID, role); err == nil && explicit {
			inheritMain = value
		}
		defaultRole := role
		if inheritMain && role != string(dto.RoleAgent) {
			defaultRole = string(dto.RoleAgent)
		}
		if vector, ok, err := storage.Default(projectID, defaultRole); err == nil && ok {
			layers.RoleDefault = dto.RoleSettings{PermissionVector: vector}
		}
	}
	return layers
}

// employeePermission 取该角色在权责表上的装配期分配（未分配 → ok=false）。
//
// 角色名比较大小写不敏感、忽略首尾空白：装配方与派活方对角色名的书写习惯不同
// （"Review" vs "review"），而主体命名本身已经是归一化过的（EmployeeSubjectName）。
func (port SessionPort) employeePermission(role string) (toolreg.EmployeePermission, bool) {
	if port.Runtime == nil || role == "" {
		return toolreg.EmployeePermission{}, false
	}
	for _, permission := range port.Runtime.EmployeePermissions() {
		if strings.EqualFold(strings.TrimSpace(permission.RoleName), role) {
			return permission, true
		}
	}
	return toolreg.EmployeePermission{}, false
}

// grantBits 把框架授权条目投影成"路由组 → 位值"（只取位，资源限定留给权限门）。
func grantBits(grant toolspermission.SubjectGrant) map[string]uint8 {
	bits := make(map[string]uint8, len(grant.Bits))
	for group, entry := range grant.Bits {
		bits[group] = entry.Bits
	}
	return bits
}

// permissionVectorTags 把"路由组 → 位值"投影成权限向量标签（规范组序，位 0 → `!tag`）。
//
// 判据**不在这里**：唯一实现在 backend/limit/policy 的 TagsForGroups（执行面也要
// 用它把员工身份折成向量），本文件只做转发——两处各写一份口径必然漂移。
func permissionVectorTags(bits map[string]uint8) []string {
	return policy.TagsForGroups(bits)
}
