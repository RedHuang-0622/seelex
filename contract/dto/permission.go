package dto

import "strings"

// permission.go 是「员工权限装配」的跨层词汇：**路由组名 + 位值**。
//
// 生态位：权责模型（主体 × 路由组 × 位 × 动作）的可执行形态在
// backend/tool/registry（PermissionGate / EmployeePermission）；这里只放**前端与后端
// 共用的那一份字面量**——前端要按组渲染"逐格装配"的权限面板，后端要在写入侧
// 校验这些格子，两边必须是同一个集合，否则一个拼写错误会静默变成"没分配"。
//
// 唯一事实：组名与位值的字面量定义在这里，backend/tool/registry 的 Group* / bit*
// 常量是它们的别名（不是第二份定义）。任何一侧新增组名，另一侧跟着改别名即可。

// 路由组名（与 seele.yaml permission.groups 的默认组分封一致）。
const (
	PermissionGroupRO        = "ro"         // 读簇：不改任何共享状态
	PermissionGroupRW        = "rw"         // 写簇：项目文件 + 自有工作台
	PermissionGroupRWSession = "rw_session" // 写簇：本会话可变 transcript
	PermissionGroupRWDesktop = "rw_desktop" // 写簇：共享外设（一块桌面）
	PermissionGroupCTL       = "ctl"        // 叫停 loop 簇：结束 / 挂起 / 派生 / 装载执行结构
	PermissionGroupADM       = "adm"        // 属主簇：改变能力面本身
	// PermissionGroupSSH 是**通道能力**：允许把一次工具调用打到远端执行面。
	//
	// 它与操作类型正交（ro/rw 说的是"改不改共享状态"，这一格说的是"在哪台机器上改"），
	// 位取 x（执行：有这条路）。判据按**已定执行面**判，不按工具名路由——同一支
	// read_file 既能跑本地也能跑远端，名字路由分不出这两件事。
	PermissionGroupSSH = "ssh"
	// PermissionGroupSSHUserspace 是**远端用户空间访问**：允许在远端项目根之外活动
	// （/etc/nginx/nginx.conf、~/.ssh/config 这类对端绝对路径）。
	//
	// 位取 r|w|x 全位（与 adm 同宽）：越界一旦放行，读、写、执行三种后果都在同一格
	// 能力里——远端路径的"只读访问"不是一个可以单独立起来的档。`rwes` 里的 s 是
	// 提权（sudo）维度，框架没有这一位，它由组的 Default（ask）与 missing_bit 表达。
	PermissionGroupSSHUserspace = "ssh.userspace"
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
		// 远端两格（2026-10-2x）：ssh 是通道能力，ssh.userspace 是越界访问。
		// 追加在末尾而不是插在中间：规范组序同时是装配面板的格子顺序，动既有格子的
		// 位置会让已经落盘/已被引用的向量与面板排布跟着漂移。
		PermissionGroupSSH, PermissionGroupSSHUserspace,
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
// 档位不是新的权限机制，而是一份**权限向量预设**（见 backend/limit/policy 的
// TierVector）：向量里有的路由组 = 该族工具免审批；没有的组 = 断位（执行面按
// 主体类落成"提权 / 不可路由"）。升序 = 自动度递增；`full` 授予全部组，但
// **危险命令的 deny 规则在任何档位下都硬拦**（含 full）。
//
// 边界（产品决定，不是可选配置）：
//   - 档位只作用在 root（主 agent）主体上；员工/子代理的授权面来自它们自己的
//     权限向量（(会话, 角色) 的登记或四级链路），档位一字不改。
//   - 档位是**会话粒度**（跟着 SessionUnit 的槽走，切会话即换档位）。
//   - 共享桌面（rw_desktop）与能力面（adm）只有 full 档才在向量里（安全底线）。
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
			Description: "只读、本会话工作台与控制流免审；项目写、命令、桌面与能力面都提权（默认）"},
		{ID: PermissionTierEdit, Label: "自动改文件", Short: "改文件",
			Description: "再授予项目写（含命令）：文件写与任意命令免审批；桌面与能力面仍提权"},
		{ID: PermissionTierAuto, Label: "自动执行", Short: "自动",
			Description: "当前与「自动改文件」同一份向量预设（命令与项目写同属 rw 组）；危险命令仍硬拦"},
		{ID: PermissionTierFull, Label: "全权", Short: "全权",
			Description: "授予全部能力面（含共享桌面与 adm）；仅危险命令被硬拦，只作用于主 agent"},
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
