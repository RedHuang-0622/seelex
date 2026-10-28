package team

// plugin_reject.go 是「精选目录进运行期」在**声明校验**那一跳的判据（P21-27「域方法随类型走」：
// 从 `backend/engine/assemble/runtime_teamwork_curated.go` 搬进本域；plan §2.7 把
// `runtime_role_plugins.go` 一族指向 `backend/multiagent/team/`）。
//
// 背景（2026-10-05 复核实读）：`plugin.LoadCuratedFromRoot` / `ActivateFromCatalog` 当时**只出现在
// 测试里**——真走 team_plan，一个未落盘的名字会落到 `ValidateMemberPlugins` 的"插件 %q 未定义"
// 那一句上，而那句话里既没有 pending，也没有来源。库级为真、运行期不成立。本函数把它接上：
// 产品侧启动期读一次精选目录（路线图），投影成判决函数注入宿主，这里在**已装插件之外**多问一句。
//
// 三条写死的边界：
//   - **只判名字**：不解析 preset、不激活插件、不碰插件集合的透传与收窄（那条链在
//     `ResolvePluginAssembly` 与权限面，各有其人）。
//   - **已装插件不问**：`Defined` 为真就是原路径，逐字不变（判定只发生在"确实未定义"时）。
//   - **语法先于语义**：规整报错（超限 / 重复）的成员在这里**跳过**，把话留给
//     `ValidateMemberPlugins` 的语法/上限那一条——语义错误不许盖住语法错误。

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// UnassembledPluginDeps 是这一跳需要的三格宿主读数。
type UnassembledPluginDeps struct {
	// Limit 是每会话插件上限：只用于**规整**（超限的那条语法错误在这里跳过）。
	Limit int
	// Defined 报告名字是否已装（nil = 都不认识）。
	Defined func(name string) bool
	// UnassembledReason 报告"为什么这个名字没被装配"（路线图 / 两边都不认识 / 精选目录没读到）；
	// nil = 宿主没接判决面 ⇒ **整个函数空操作**（那正是"不带产品启动面"的用法）。
	UnassembledReason func(name string) error
}

// RejectUnassembledPlugins 逐成员判"未定义的插件名是什么"：pending 候选（路线图）必须点名上游来源，
// 谁都不认识必须说清两边都不在，精选目录没读到必须出声（不得静默当空目录）。
//
// **在写入任何计划之前**调用，因此被拒的计划不会落盘。
func RejectUnassembledPlugins(members []dto.TeamworkMember, deps UnassembledPluginDeps) error {
	if deps.UnassembledReason == nil {
		return nil
	}
	for _, member := range members {
		// 上限用**配置里那个真值**：规整（去空白/去空项/重复拒绝）照做，但任何语法错误
		// （超限、重复）都在这里跳过——那是语法层的话，由 ValidateMemberPlugins 带着
		// 配置值说（语义错误不许盖住语法错误）。
		names, err := dto.NormalizePlugins(member.Plugins, deps.Limit)
		if err != nil {
			continue
		}
		for _, name := range names {
			if deps.Defined != nil && deps.Defined(name) {
				continue
			}
			if reason := deps.UnassembledReason(name); reason != nil {
				return fmt.Errorf("成员 %q 的插件装配被拒绝: %w", member.Role, reason)
			}
		}
	}
	return nil
}
