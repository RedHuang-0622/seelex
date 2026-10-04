package seelebridge

// runtime_teamwork_curated.go — 「精选目录进运行期」在装配入口的那一跳：**名字怎么被判定、
// 文案怎么写**。
//
// 背景（2026-10-05 复核实读）：`plugin.LoadCuratedFromRoot` / `ActivateFromCatalog` 当时
// **只出现在测试里**——真走 team_plan，一个未落盘的名字会落到 `validateMemberPlugins` 的
// "插件 %q 未定义"那一句上，而那句话里既没有 pending，也没有来源。库级为真、运行期不成立。
// 本文件把它接上：产品侧启动期从已解析的插件根读一次精选目录（见 main.go 的
// resolveCuratedRead），投影成判决函数注入桥（`seelebridge/plugin.Manager.
// SetUnassembledReason`），team_plan 的校验入口在**已装插件之外**多问一句。
//
// 三条写死的边界：
//   - **只判名字**：本文件不解析 preset、不激活插件、不碰插件集合的透传与收窄
//     （那条链在 runtime_role_plugins.go 与 tools/policy.go，各有其人）。
//   - **已装插件不问**：`Defined` 为真就是原路径，逐字不变（判定只发生在"确实未定义"时）。
//   - **语法先于语义**：超过每会话上限的成员在这里**跳过**，把话留给 validateMemberPlugins
//     的语法/上限那一条——语义错误不许盖住语法错误。

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// rejectUnassembledMemberPlugins 逐成员判"未定义的插件名是什么"：pending 候选（路线图）
// 必须点名上游来源，谁都不认识必须说清两边都不在，精选目录没读到必须出声（不得静默当空
// 目录）。**在写入任何计划之前**调用，因此被拒的计划不会落盘。
//
// 未注入判决函数（桥上的 SetUnassembledReason 没被调用）时整个函数是空操作：那正是
// "不带产品启动面"的用法，未定义名的口径仍是 validateMemberPlugins 那一句。
func (r *Runtime) rejectUnassembledMemberPlugins(members []sessionstore.TeamworkMember) error {
	if r == nil || r.plugins == nil || !r.plugins.HasUnassembledReason() {
		return nil
	}
	for _, member := range members {
		// 上限用**配置里那个真值**：规整（去空白/去空项/重复拒绝）照做，但任何语法错误
		// （超限、重复）都在这里跳过——那是语法层的话，由 validateMemberPlugins 带着
		// 配置值说（语义错误不许盖住语法错误）。
		names, err := dto.NormalizePlugins(member.Plugins, r.maxPluginsPerTeammate())
		if err != nil {
			continue
		}
		for _, name := range names {
			if r.plugins.Defined(name) {
				continue
			}
			if reason := r.plugins.UnassembledReason(name); reason != nil {
				return fmt.Errorf("成员 %q 的插件装配被拒绝: %w", member.Role, reason)
			}
		}
	}
	return nil
}
