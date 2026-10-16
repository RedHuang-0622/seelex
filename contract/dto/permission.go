package dto

import "strings"

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
		// "没显式装配"用**空 map**而不是裸 `nil, nil` 表达：语义与 nil 完全等价
		// （调用方一律看 len，见 docs 里那条"空 map / nil 同类"），但静态门禁
		// 禁止非测试代码出现 `return nil, nil`（它通常是吞掉错误的信号）。
		return map[string]uint8{}, nil
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

// 主会话权限档位（tier）的 id：**主 agent（root 主体）在本会话的自动度档位**。
//
// 档位不是新的权限机制，而是对既有权责表（分组表 + 规则表）的一层**声明式覆盖**：
// 每个档位只做一件事——把"要问人的 ask 规则"按档位逐族剪掉，从不新增 allow、
// 从不触碰 deny。升序 = 自动度递增；`full` 由执行门短路（等价旧的 full_access）。
//
// 边界（产品决定，不是可选配置）：
//   - 档位只作用在 root（主 agent）主体上；员工/子代理的判定链一字不改
//     （"主会话全权只管网主会话，员工越权照旧审批提权"）。
//   - 档位是**会话粒度**（跟着 SessionUnit 的槽走，切会话即换档位）。
//   - 共享桌面（rw_desktop）与能力面（adm）只有 full 档才放开（安全底线）。
const (
	PermissionTierManual = "manual" // 手动（默认）：完全按权责表问/放
	PermissionTierEdit   = "edit"   // 自动改文件：write_file/edit_file 不再问人
	PermissionTierAuto   = "auto"   // 自动执行：bash 不再问人（危险命令仍硬拦）
	PermissionTierFull   = "full"   // 全权（免审）：本会话短路放行
)

// PermissionTierInfo 是一个档位的展示目录项（前端按序渲染可选列表；id 是
// 唯一事实，标签/说明由后端下发，避免前后端各写一套档位名漂移）。
type PermissionTierInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Short       string `json:"short"`
	Description string `json:"description"`
}

// PermissionTiers 返回规范档位目录（升序 = 自动度递增；返回新切片，调用方
// 随自己的展示需要裁剪，不会改到词表本身）。
func PermissionTiers() []PermissionTierInfo {
	return []PermissionTierInfo{
		{ID: PermissionTierManual, Label: "手动", Short: "手动",
			Description: "写文件、命令、桌面与能力面都按权责表问人（默认）"},
		{ID: PermissionTierEdit, Label: "自动改文件", Short: "改文件",
			Description: "项目文件写不再打断；命令与桌面/能力面仍问人"},
		{ID: PermissionTierAuto, Label: "自动执行", Short: "自动",
			Description: "文件写与任意命令直跑（危险命令仍硬拦）；桌面/能力面仍问人"},
		{ID: PermissionTierFull, Label: "全权", Short: "全权",
			Description: "本会话全部放行（免审）；只作用于主 agent"},
	}
}

// PermissionTierIDs 返回规范档位 id 序列（装配/校验/测试用）。
func PermissionTierIDs() []string {
	infos := PermissionTiers()
	ids := make([]string, 0, len(infos))
	for _, info := range infos {
		ids = append(ids, info.ID)
	}
	return ids
}

// ValidPermissionTier 报告 tier 是否是已知档位 id。
func ValidPermissionTier(tier string) bool {
	for _, info := range PermissionTiers() {
		if info.ID == tier {
			return true
		}
	}
	return false
}

// NormalizePermissionTier 规整档位 id：空（未选择）→ manual（进程默认）；未识别
// → 报错。写入侧宁可显式失败，也不要静默落到一个用户没选的档位（fail-open 的
// "全权"尤其不可接受）。
func NormalizePermissionTier(tier string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(tier))
	if normalized == "" {
		return PermissionTierManual, nil
	}
	if ValidPermissionTier(normalized) {
		return normalized, nil
	}
	return "", &PermissionTierError{Tier: tier}
}

// PermissionTierError 是档位 id 非法（承载原值，便于装配方/前端直接报出用户
// 选的档位，而不是一句"参数非法"）。
type PermissionTierError struct{ Tier string }

func (e *PermissionTierError) Error() string {
	if e == nil {
		return "权限档位非法"
	}
	return "权限档位 " + e.Tier + " 非法（取值见 dto.PermissionTierIDs）"
}

// PermissionTierFromFullAccess 把旧的二元 full_access 口径映射成档位（兼容壳：
// SetFullAccess(true/false) ⇔ full/manual）。
func PermissionTierFromFullAccess(on bool) string {
	if on {
		return PermissionTierFull
	}
	return PermissionTierManual
}

// PermissionTierIsFullAccess 报告档位是否等价旧 full_access（诊断/兼容读面）。
func PermissionTierIsFullAccess(tier string) bool {
	normalized, err := NormalizePermissionTier(tier)
	return err == nil && normalized == PermissionTierFull
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
