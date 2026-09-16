package tools

import (
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// permission_tiers.go — 主会话权限档位（tier）在 seelex 侧的**覆盖实现**。
//
// 档位是"对 root 的声明式覆盖"，不是新的判定机制：它只把**要问人的 ask 规则**
// 按档位逐族剪掉，从不新增 allow、从不触碰 deny（`DefaultPermissionRules` 的顺序
// 即语义——危险 deny 放最后，永不被剪）。
//
// 单一事实的分工（与"组名的唯一事实在 dto、组的分封在 seelebridge"同构）：
//   - 档位目录（id/标签/说明）在 application/contract/dto（前端按它渲染列表）；
//   - 覆盖实现（剪哪条规则）在本文件（它拥有 DefaultPermissionRules 的语义）。
//
// 档位 → 覆盖（升序 = 自动度递增）：
//
//	manual  无（完全按权责表）
//	edit    剪 write_file / edit_file 的 ask（项目文件写不再打断）
//	auto    再剪 bash 的全部 ask（任意命令直跑；危险 deny 仍在）
//	full    BypassAll（执行门短路放行，见 registry_state.go 的 Enforce）
//
// 刻意**不放进中档**的两族：rw_desktop（一块共享外设）与 adm（改变能力面本身），
// 只有 full 档才放开——逐次确认共享外设与"先扩权再干活"是安全底线。

// DefaultPermissionTiers 返回档位目录（跨层词表的唯一事实在 dto）。
func DefaultPermissionTiers() []dto.PermissionTierInfo {
	return dto.PermissionTiers()
}

// ApplyTier 把档位声明式覆盖到 base 权责配置上，返回应用后的配置。
//
// 覆盖只做一件事：**删除**该档位要放开的 `ask` 规则（按工具名逐条剪）。因此
//   - group 默认动作不变（rw 组默认 allow、rw_desktop/adm 默认 ask）；
//   - 未列进覆盖表的 ask（如 plugin_create / skill_create）仍问人；
//   - deny（危险命令）逐条保留，任何档位都硬拦。
//
// manual 与 full 返回原配置：manual 不覆盖任何东西；full 由执行门短路，规则面
// 保持 base（万一短路未生效，规则仍是安全兜底）。未识别的档位按 manual 处理
// （写入侧 NormalizePermissionTier 已挡，这里只是防御）。
func ApplyTier(base toolspermission.PermissionConfig, tier string) toolspermission.PermissionConfig {
	normalized, err := dto.NormalizePermissionTier(tier)
	if err != nil {
		normalized = dto.PermissionTierManual
	}
	stopAsking := tierStopAskingTools(normalized)
	if len(stopAsking) == 0 {
		return base
	}
	overlay := base
	overlay.Rules = make([]toolspermission.PermissionRule, 0, len(base.Rules))
	for _, rule := range base.Rules {
		if rule.Action == toolspermission.ActionAsk && stopAsking[rule.ToolName] {
			continue
		}
		overlay.Rules = append(overlay.Rules, rule)
	}
	return overlay
}

// tierStopAskingTools 返回该档位下"不再问人"的工具名集合（空 = 不覆盖）。
//
//   - edit：写盘工具（write_file / edit_file）——项目文件写不再打断；
//   - auto：写盘工具 + bash（含危险命令白名单之外的"问人"段）——任意命令直跑，
//     但 bash 的 deny 规则（rm -rf /、dd if=* of=*、mkfs* …）逐条保留。
//
// 危险命令的"问人"段（npm install * / make * / docker * / rm * / mv * /
// chmod * / chown *）也随 bash 一起放开——这正是 auto 与 edit 的分界；真危险
// （deny 段）不受影响。
func tierStopAskingTools(tier string) map[string]bool {
	switch tier {
	case dto.PermissionTierEdit:
		return map[string]bool{"write_file": true, "edit_file": true}
	case dto.PermissionTierAuto:
		return map[string]bool{"write_file": true, "edit_file": true, "bash": true}
	default:
		return nil
	}
}
