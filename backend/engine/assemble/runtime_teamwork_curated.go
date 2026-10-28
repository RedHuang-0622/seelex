package assemble

// runtime_teamwork_curated.go — 「精选目录进运行期」在装配入口那一跳的**宿主面**。
//
// 落点沿革（P21-27「域方法随类型走」）：**判据与文案**（只判名字 / 已装插件不问 / 语法先于语义）
// 搬进团队域 [`backend/multiagent/team`](../../multiagent/team/README.md) 的 `plugin_reject.go`
// （与 `ValidateMemberPlugins` 同域——一个说"未定义"，一个说"为什么未定义"）；本文件只剩
// 三格读数：`r.plugins` 有没有判决面（`HasUnassembledReason`）、`Defined`、`UnassembledReason`，
// 以及每会话插件上限（`maxPluginsPerTeammate`）。
//
// 接线的由来：产品侧启动期从已解析的插件根读一次精选目录（`main.go` 的 `resolveCuratedRead`），
// 投影成判决函数注入桥（`plugin.Manager.SetUnassembledReason`），team_plan 的校验入口在**已装插件
// 之外**多问一句——否则一个未落盘的名字只会撞上"插件 %q 未定义"，那句话里既没有 pending、也没有来源。

import (
	teamdomain "github.com/RedHuang-0622/seelex/backend/multiagent/team"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// rejectUnassembledMemberPlugins 逐成员判"未定义的插件名是什么"（判据在域包）。
//
// 未注入判决函数（桥上的 SetUnassembledReason 没被调用）时整个函数是空操作：那正是
// "不带产品启动面"的用法，未定义名的口径仍是 ValidateMemberPlugins 那一句。
func (r *Runtime) rejectUnassembledMemberPlugins(members []storeteamwork.Member) error {
	if r == nil || r.plugins == nil || !r.plugins.HasUnassembledReason() {
		return nil
	}
	return teamdomain.RejectUnassembledPlugins(members, teamdomain.UnassembledPluginDeps{
		Limit:             r.maxPluginsPerTeammate(),
		Defined:           r.plugins.Defined,
		UnassembledReason: r.plugins.UnassembledReason,
	})
}
