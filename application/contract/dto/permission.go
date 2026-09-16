package dto

// permission.go 是「员工权限装配」的跨层词汇：**路由组名 + 位值**。
//
// 生态位：权责模型（主体 × 路由组 × 位 × 动作）的可执行形态在
// seelebridge/tools（PermissionGate / EmployeePermission）；这里只放**前端与后端
// 共用的那一份字面量**——前端要按组渲染"逐格装配"的权限面板，后端要在写入侧
// 校验这些格子，两边必须是同一个集合，否则一个拼写错误会静默变成"没分配"。
//
// 唯一事实：组名与位值的字面量定义在这里，seelebridge/tools 的 Group* / bit*
// 常量是它们的别名（不是第二份定义）。任何一侧新增组名，另一侧跟着改别名即可。

// 路由组名（与 seele.yaml permission.groups 的默认组分封一致）。
const (
	PermissionGroupRO        = "ro"         // 读簇：不改任何共享状态
	PermissionGroupRW        = "rw"         // 写簇：项目文件 + 自有工作台
	PermissionGroupRWSession = "rw_session" // 写簇：本会话可变 transcript
	PermissionGroupRWDesktop = "rw_desktop" // 写簇：共享外设（一块桌面）
	PermissionGroupCTL       = "ctl"        // 叫停 loop 簇：结束 / 挂起 / 派生 / 装载执行结构
	PermissionGroupADM       = "adm"        // 属主簇：改变能力面本身
)

// 位值（r=4 / w=2 / x=1，与 Seele 权限框架的位口径同源）。
const (
	PermissionBitRead    uint8 = 4
	PermissionBitWrite   uint8 = 2
	PermissionBitExecute uint8 = 1
	PermissionBitAll     uint8 = PermissionBitRead | PermissionBitWrite | PermissionBitExecute
)

// PermissionGroupNames 返回规范组序（装配面板的"格子顺序"）。
//
// 返回的是新切片：调用方随自己的展示需要排序/裁剪，不会改到这份词表本身。
func PermissionGroupNames() []string {
	return []string{
		PermissionGroupRO, PermissionGroupRW, PermissionGroupRWSession,
		PermissionGroupRWDesktop, PermissionGroupCTL, PermissionGroupADM,
	}
}

// ValidPermissionGroup 报告 group 是否是已知路由组。
//
// 为什么必须枚举（而不是"未知组名给 0 位"）：装配期的一个拼写错误
// （"readonly" 当成组名、"rw-project"）在运行时等价于"这一格没分配"——用户以为
// 给员工开了写项目，实际什么都没开，而且没有任何报错面。写入侧报错，装配方
// 立刻看得见。
func ValidPermissionGroup(group string) bool {
	for _, name := range PermissionGroupNames() {
		if group == name {
			return true
		}
	}
	return false
}

// NormalizePermissionGroups 规整一份"逐格装配"的权限：校验组名与位值，返回**独立
// 副本**（不共享调用方的 map）。
//
// 语义边界（就写在写入侧，免得运行时再猜）：
//   - 返回 nil 表示**没有显式装配**（空 map / nil）→ 装配期按 ToolsPolicy 档位派生；
//   - 非 nil（哪怕全是 0 位）表示**显式装配**→ 逐组以它为准（0 位 = 这一族能力
//     明确不开，而不是"继承默认"）。
//
// 这个区分是刻意的：`{"rw": 0}` 与"没写 rw"必须是两件事，否则"收回一格能力"
// 就没法表达——收回写权限会退化成继承默认（而默认可能恰好有写权限）。
func NormalizePermissionGroups(groups map[string]uint8) (map[string]uint8, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	normalized := make(map[string]uint8, len(groups))
	for group, bits := range groups {
		if !ValidPermissionGroup(group) {
			return nil, &PermissionGroupError{Group: group, Reason: "未知路由组（取值见 dto.PermissionGroupNames）"}
		}
		if bits&^PermissionBitAll != 0 {
			return nil, &PermissionGroupError{Group: group, Reason: "位值越界（r=4 / w=2 / x=1，最大 7）"}
		}
		normalized[group] = bits
	}
	return normalized, nil
}

// PermissionGroupError 是权限格子规整失败（承载坏格子，便于装配方直接报出用户
// 填错的那一格，而不是一句"参数非法"）。
type PermissionGroupError struct {
	Group  string
	Reason string
}

func (e *PermissionGroupError) Error() string {
	if e == nil {
		return "权限格子非法"
	}
	return "权限格子 " + e.Group + " 非法: " + e.Reason
}
