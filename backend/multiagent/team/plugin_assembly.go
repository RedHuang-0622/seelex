package team

// plugin_assembly.go 是「teammate 按指定 plugin 装配」的**判据面**（P21-18「域方法随类型走」：
// 从 `backend/engine/assemble/runtime_role_plugins.go` 把解析集合、写入侧校验与装配面裁决搬进
// 本域；plan §2.7 把 `runtime_role_plugins.go`(288) 列为 `backend/multiagent/team/` 的目标）。
//
// 本包因此有三件事：**角色身份**（`RoleSessionID`）、**写入侧规整**（`Normalize` / `NormalizeRole`，
// 见 spec.go）与**运行时插件装配判据**（本文件）。装配面的宿主读数（插件定义目录、可见性管理器、
// 技能注册表、limits）仍由装配层注入——本文件不认识 `Runtime`。
//
// 三条写死的边界（与运行时一致）：
//   - **权限不随插件走**：装配只回答"能用哪些能力包"；权限永远来自 ToolsPolicy / PermissionGroups，
//     插件面与权限面相交后只收窄**权限面**（相对宿主装配它可以是放宽，见 runtime_role_plugins.go
//     里那条口径的长注）；
//   - **未知插件名显式拒绝**，不是静默忽略（见 `ValidateMemberPlugins`）；
//   - **重复声明显式拒绝**，不是静默合并（`dto.NormalizePlugins` 与存储层两处拒绝；本文件的
//     `NormalizePluginNames` 是**读侧/传输侧**的规范化，不是声明侧口径）。

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/backend/engine/plugin/visibility"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store"
)

// ResolvePluginAssembly 解析"这一轮装配哪些插件"：显式声明替换集合 → 角色自带 → 空集（不覆盖）。
// 三层顺序即契约 6 的"集合"口径。
func ResolvePluginAssembly(explicit, roleOwn []string) []string {
	if names := NormalizePluginNames(explicit); len(names) > 0 {
		return names
	}
	return NormalizePluginNames(roleOwn)
}

// NormalizePluginNames 去首尾空白、丢弃空项、保序去重；清完为空 = nil（"没装配"
// 与"装配了零个"必须是同一个形态，否则空集语义会在某一条分支上变成"装配了空集"）。
//
// **这不是声明侧的口径**：声明侧的重复在入口就被显式拒绝了（dto.NormalizePlugins /
// store.ValidateTeamworkPlan），走到这里的都是已经过校验的事实（计划成员条目、
// 角色自带读面）。这一层的去重因此是**传输/读侧的规范化**：它不会把 leader 写的重复
// 名单吞掉——那种输入根本到不了这里（2026-10-05 对抗复核 D 的口径统一）。
func NormalizePluginNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	cleaned := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		cleaned = append(cleaned, name)
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

// ValidateMemberPlugins 是编排入口的两道**显式拒绝**（外加语法规整）：
//
//  1. 语法：去首尾空白、丢弃空项、保序去重（`dto.NormalizePlugins`，与写入侧同一份口径），
//     并把规整结果写回成员条目——写回去，计划里存的才是"运行时真会用的那一份"；
//  2. 语义：名字必须在插件目录里（`defined`）。**未知名显式拒绝，不是静默忽略**：静默忽略会让
//     leader 以为装上了，而员工那头一个能力也没有（要到员工跑完才发现），而且"拒绝"才是可回执的事实；
//  3. 数量：超过 `limit`（limits.plugins.per_teammate）显式拒绝，不静默截断；
//  4. 重复：显式拒绝，不静默合并——与"未知名"同口径。
//
// 抽成函数是为了可测：它只依赖"上限"与"名字是否存在"两个事实，不需要整个 coordinator / 存储面在场。
func ValidateMemberPlugins(members []store.TeamworkMember, limit int, defined func(string) bool) error {
	for index := range members {
		plugins, err := dto.NormalizePlugins(members[index].Plugins, limit)
		if err != nil {
			return fmt.Errorf("成员 %q 的插件装配非法: %w", members[index].Role, err)
		}
		for _, name := range plugins {
			if defined != nil && !defined(name) {
				return fmt.Errorf("插件 %q 未定义（成员 %q；显式拒绝，不静默忽略）", name, members[index].Role)
			}
		}
		members[index].Plugins = plugins
	}
	return nil
}

// PluginFaceJudgement 是一次**装配面裁决**：读数用它，运行面也用它。
//
// 为什么要有这个类型：回执里的"插件面有几个工具"如果另写一套判据，回执就会与运行事实
// **相反**——最刺眼的一种是"插件被 root 撤销之后，回执报满面、运行面为空"。
//
// 三种形态（覆盖全部输入，且与运行事实一一对应）：
//
//	Defs 有值        → 按这份定义集合收窄（include 取并集、exclude 并集硬拆）；
//	Defs 为空        → 一律可见（"没装配"或"宿主没激活插件"的等价形态）；
//	Faulted = true   → **失灵**：声明还在，定义没了（Missing 是那些名字）⇒ 一个都不给。
//
// 最后一条是刻意的：visibility.VisibleName 对空 defs 返回 true（"没装配就不改变工具面"），
// 那条规则是给 Defs 为空的情形用的，**不能**被"装配了但定义没了"复用——复用就等于
// 静默放宽（丢掉某份收窄）。
type PluginFaceJudgement struct {
	Defs    []visibility.Def
	Missing []string
	Faulted bool
}

// Visible 是"这个工具名在本装配面下可见吗"——**读数**用的那一半。
func (j PluginFaceJudgement) Visible(name string) bool {
	if j.Faulted {
		return false
	}
	return visibility.VisibleName(j.Defs, name)
}

// Face 是"这份工具面收窄之后是什么"——**运行面**用的那一半。
func (j PluginFaceJudgement) Face(tools []types.Tool) []types.Tool {
	if j.Faulted {
		return nil
	}
	return visibility.Face(j.Defs, tools)
}

// FaultNote 把失灵写成一句可回执的话（回执/日志"说清"这件事，而不是让 leader 从一个
// 0 里猜它是"没装配"还是"坏了"）。
func (j PluginFaceJudgement) FaultNote(declared []string) string {
	if !j.Faulted {
		return ""
	}
	return fmt.Sprintf("声明 %s，现已失灵（本进程未定义：%s），工具面为空（不静默放宽为宿主面）",
		strings.Join(declared, ", "), strings.Join(j.Missing, ", "))
}
