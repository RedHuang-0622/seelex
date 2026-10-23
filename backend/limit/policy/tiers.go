package policy

// tiers.go —— 主会话权限档位（tier）在 seelex 侧的**向量预设**。
//
// 2026-10-16 从 seelebridge/tools/permission_tiers.go 归位（M2-C4）；
// 2026-10-21 起档位不再是"删规则的表覆盖"，而是**一份权限向量**（预设）：
//
// 旧实现（已删除）：每个档位复制一份 PermissionConfig、按工具名剪掉若干 ask 规则、
// 逼出一个按档位预构建的 checker。它有两个结构性问题：
//   - 同一件事有两处判据：规则表（base）与剪过的规则表（tier）要永远保持"只差那几条"，
//     任何一侧新增 ask 规则都会让两边悄悄分叉；
//   - 档位与"位"脱钩：剪规则只改"问不问"，不改"有没有"，于是新能力面（组）进来时
//     档位不会被自动纳入审查。
//
// 新实现（本文件）：档位就是**向量**——一组能力标签（= 路由组名，词表在
// contract/dto）。判定面把向量展开成"组 → 位"（见 vector.go 的 CellsForVector）：
//
//	向量里有的组   → 该组位给足 → 该族工具免审批（赋权即免审批）
//	向量里没有的组 → 断位 → 执行面按主体类落成"提权 / 不可路由"
//	`!tag`         → 显式 deny → 直接拒绝（不给提权机会）
//
// 因此档位之间只差"给了哪几族能力"，不再有第二份规则表。危险命令（bash 的 deny
// 规则）与规则层的 ask 判定**不受档位影响**：deny 在任何档位下都硬拦（含 full，
// 这是旧实现 full=BypassAll 的一处收紧）。
//
// 分级（升序 = 自动度递增；id 是持久化键，不可改，语义由下面的预设表定义）：
//
//	manual  读 + 本会话工作台 + 控制流            项目写与命令仍提权
//	edit    再加项目写（rw）                      命令与项目写免审批
//	auto    同 edit（保留独立 id 以便持久化兼容）   —— 见下方 D-tier 说明
//	full    全部六组                              含共享桌面与能力面；仅危险命令硬拦
//
// D-tier（待产品裁决，明写在这里免得当下一个人是 bug）：旧实现里 edit 与 auto 的
// 差别是"剪不剪 bash 的 ask 规则"，而 bash 与 write_file 同属 rw 组 ⇒ 在**组级**
// 向量下这两档给出同一族能力（rw），无法再分开。恢复区分需要新增独立的命令组
// （例如 exec）或工具级词表——那是词表变更，不在本次改动内。

import (
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TierPreset 是一个档位的向量预设：授予的路由组（缺组 = 断位）。
type TierPreset struct {
	// Groups 是授予的路由组名（= 权限向量标签，唯一词表在 dto）。
	Groups []string
}

// DefaultPermissionTiers 返回档位目录（跨层词表的唯一事实在 dto）。
func DefaultPermissionTiers() []dto.PermissionTierInfo {
	return dto.PermissionTiers()
}

// tierPresets 是档位 → 向量的**唯一**预设表。
//
// 用有序切片而不是 map：档位目录的展示顺序由 dto 定，这里的顺序只影响"更宽的档
// 在更后面"这一可读性，不参与判定。
var tierPresets = map[string]TierPreset{
	dto.PermissionTierManual: {Groups: []string{GroupRO, GroupRWSession, GroupCTL}},
	dto.PermissionTierEdit:   {Groups: []string{GroupRO, GroupRWSession, GroupCTL, GroupRW}},
	dto.PermissionTierAuto:   {Groups: []string{GroupRO, GroupRWSession, GroupCTL, GroupRW}},
	dto.PermissionTierFull: {
		Groups: []string{GroupRO, GroupRWSession, GroupCTL, GroupRW, GroupRWDesktop, GroupADM},
	},
}

// TierVector 返回档位的权限向量预设（= 该档授予的组）。
//
// 未识别 / 空档位按 manual 处理（写入侧 NormalizePermissionTier 已挡，这里只是
// 防御）：拿不到"用户选过的档"时回落到最窄的一档，绝不放宽。
//
// 返回新切片：调用方（执行面）可以随自己的需要拼接/加 `!tag`，不会改到预设本身。
func TierVector(tier string) []string {
	normalized, err := dto.NormalizePermissionTier(tier)
	if err != nil {
		normalized = dto.PermissionTierManual
	}
	preset, ok := tierPresets[normalized]
	if !ok {
		preset = tierPresets[dto.PermissionTierManual]
	}
	return append([]string(nil), preset.Groups...)
}

// TierCells 把档位展开成"组 → 位"格位表（执行面直接读它做判定）。
//
// groups 是**本次配置**的路由组表：档位预设里提到的组如果在本次配置里不存在，
// 展开时它自然不出现（与 CellsForVector 同口径，不另判一次）。
func TierCells(tier string, groups []toolspermission.PermissionGroup) PermissionVectorCells {
	return CellsForVector(dto.ResolvePermissionVectorDecision(TierVector(tier)), groups)
}
