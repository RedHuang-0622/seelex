package policy

// vector.go —— 权限向量在**权责表坐标系**上的展开（判定的单一事实源）。
//
// 生态位：`contract/dto` 拥有"什么算标签、四级优先级怎么解析"（ResolvePermissionVector
// / ResolvePermissionVectorDecision），本文件拥有"标签落到哪一组、给多少位"。
// 执行面（backend/tool/registry 的 PermissionGate）只读本文件给出的格位，不再自己
// 读共享主体表、也不再自己写一条派生条目——那是"双判据"的来源（见 CHANGELOG 与
// 本文件的 Covers/ExplicitlyDenied 两侧注释）。
//
// 语义三态（缺一不可，缺哪一侧都会把"没提到"读成"允许"）：
//
//	允许（标签在 Allowed 里）    → 该组的位 = 组的 Mode（整族可用）
//	没提到（组不在两边的任何一个） → **断位**：这一族能力不在授权面内
//	显式 deny（标签在 Denied 里） → 该组位为 0 且**带拒绝语义**：不给提权机会
//
// 位值不另起一套：授予一个组就把该组的 Mode 给足（组 Mode 的唯一事实在
// dto/DefaultPermissionGroupList），因此"向量说给 rw"与"主体条目给 rw 的
// read|write"是同一个结论，不需要第二张映射表。

import (
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// PermissionVectorCells 是权限向量展开后的"组 → 位"格位表。
//
// 缺组 = 断位（不在 Bits 里）；Denied 里的组是**显式拒绝**（与断位不同档）。
type PermissionVectorCells struct {
	Bits   map[string]uint8
	Denied map[string]bool
}

// CellsForVector 把一次解析结论按本次配置的路由组表展开成格位。
//
// 只认当前配置里存在的组：标签指向一个本次配置没有的组时，它既不能授予也不能
// 拒绝（配置里没有这一族，判定的路由也不会落到它上面）。这不是"静默忽略"，
// 而是与 sanitizeMeta 同一条口径——声明的簇必须在本次配置里才生效。
func CellsForVector(decision dto.PermissionVectorDecision, groups []toolspermission.PermissionGroup) PermissionVectorCells {
	modes := make(map[string]uint8, len(groups))
	for _, group := range groups {
		modes[group.Name] = group.Mode
	}
	cells := PermissionVectorCells{
		Bits:   make(map[string]uint8, len(decision.Allowed)),
		Denied: make(map[string]bool, len(decision.Denied)),
	}
	for _, tag := range decision.Allowed {
		if mode, ok := modes[tag]; ok {
			cells.Bits[tag] = mode
		}
	}
	for _, tag := range decision.Denied {
		if _, ok := modes[tag]; ok {
			cells.Denied[tag] = true
			delete(cells.Bits, tag)
		}
	}
	return cells
}

// Covers 报告该组是否被授予（且位足够）。required 为 0 时按"组在表里且位非 0"判。
func (cells PermissionVectorCells) Covers(group string, required uint8) bool {
	bits, ok := cells.Bits[group]
	if !ok {
		return false
	}
	if required == 0 {
		return bits != 0
	}
	return bits&required == required
}

// ExplicitlyDenied 报告该组是否被**显式**否掉（`!tag`）：执行面据此直接拒绝，
// 不给"提权"机会（用户说了不给，就不是"没说过")。
func (cells PermissionVectorCells) ExplicitlyDenied(group string) bool {
	if cells.Denied == nil {
		return false
	}
	return cells.Denied[group]
}

// TagsForGroups 把"组 → 位"投影成权限向量标签（规范组序）。
//
// 位为 0（或该组没出现）→ `!tag`：`dto.NormalizePermissionGroups` 的口径是
// "未列出的组 = 0 位，即这一族能力不在面上"，所以投影时必须显式说出"不给"。
// 否则四层解析会把沉默读成"让低层决定"，一层 `{"rw": 0}` 就会被低层的 rw
// 反过来放行（权限放大的经典形态）。
//
// 这是"逐格位 → 向量"的**唯一实现**：装配侧（wiring/adapters 的四层来源）与
// 执行面（员工身份兜底）都用它，不允许各写一份。
func TagsForGroups(bits map[string]uint8) []string {
	tags := make([]string, 0, len(dto.PermissionGroupNames()))
	for _, group := range dto.PermissionGroupNames() {
		if bits[group] != 0 {
			tags = append(tags, group)
			continue
		}
		tags = append(tags, dto.PermissionTagDenyPrefix+group)
	}
	return tags
}

// VectorForGroups 是 TagsForGroups 的语义别名（读起来更像"给这份格位一条向量"）。
func VectorForGroups(bits map[string]uint8) []string {
	return TagsForGroups(bits)
}

// NormalizeVectorTag 把单个标签规整成规范形态（小写、去空白、保留单个 `!`）。
// 无法识别时返回 ok=false（与 dto 的写入侧校验同一口径，不另造判据）。
func NormalizeVectorTag(raw string) (string, bool) {
	normalized, err := dto.NormalizePermissionVector([]string{raw})
	if err != nil || len(normalized) != 1 {
		return "", false
	}
	return normalized[0], true
}

// EmployeeGroupsForClass 把员工主体类（emp_ro / emp_rw）折成默认逐组位。
//
// 用途：调用面只有"主体类"、没有"角色身份"时（老调用 WithEmployeeSubjectClass），
// 执行面仍要有一条向量可判。口径与 backend/tool/registry 的 EmployeeGroupsForPolicy
// 完全一致（只读、readonly → ro；readwrite → ro + rw），因此两处不会给出两个结论。
func EmployeeGroupsForClass(class SubjectClass) map[string]uint8 {
	switch class {
	case SubjectClassEmployeeRO:
		return map[string]uint8{
			GroupRO: BitRead, GroupRW: 0, GroupRWSession: 0, GroupCTL: 0, GroupADM: 0,
			// 只读员工能远端读：通道位默认给（与主体表同口径）。
			GroupSSH: BitExecute,
		}
	case SubjectClassEmployeeRW:
		return map[string]uint8{
			GroupRO: BitRead, GroupRW: BitRead | BitWrite, GroupRWSession: 0, GroupCTL: 0, GroupADM: 0,
			GroupSSH: BitExecute,
		}
	default:
		return map[string]uint8{}
	}
}
