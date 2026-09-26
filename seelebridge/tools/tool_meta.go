package tools

import (
	frameworktools "github.com/RedHuang-0622/Seele/tools"
)

// 工具簇属（ToolMeta）的**声明路径**（打点 K-0）。
//
// 为什么必须有这一段：框架权限门吃的是**工具自带的簇属**（ToolMeta.Groups /
// Kind / Resource，见 Seele/tools/permission/checker.go 的 DecideForMeta）——工具
// 声明了簇属就走声明，没声明才退回按名字路由。而 K-0 之前，生产注册路径
// （Runtime.RegisterTool → RegistryState.AddInline）**从不填 Meta**，于是：
//   - 判定永远走名字路由，声明的能力面（Kind=control 的"仅 root 可路由"、
//     Resource=project 的资源限定）全部失效；
//   - 任何想按 Groups 派生的策略（并发分类、工具面清单）读不到东西——设计文档
//     §0 事实 2 记的就是这件事。
//
// **单一事实源 = 路由组表**（permission_policy.go 的 DefaultPermissionGroupList）。
// 这里刻意不新增第二张"工具 → 组"的表：两张表必然漂移，而漂移没有任何报错面
// （设计文档 §0 事实 1）。要给工具改簇属，改的就是路由组表——因为那张表同时是
// 权限路由的唯一事实，改一处两处都跟着变，这才是"声明"该有的样子。
//
// 派生不出来的名字（动态 MCP 工具、第三方插件工具、测试里的自定义工具）**不猜**：
// 返回零值簇属，由调用方记进"未分封"清单（RegistryState.UndeclaredTools）。
// 这不是沉默：未分封的执行口径是"按框架默认走审批"（员工侧不上面），零值簇属
// 恰好落回同一条名字路由，行为与 K-0 之前逐字一致。

// DeclaredToolMeta 返回某工具**声明的**簇属：按名字在默认路由组表里路由，
// 命中即把组翻译成簇属（Kind / Groups / Resource）。
//
// 未命中返回零值（Groups 为空 ⇒ 框架退回名字路由，与历史行为一致）。
func DeclaredToolMeta(name string) frameworktools.ToolMeta {
	group, routed := RoutePermissionGroup(DefaultPermissionGroupList(), name)
	if !routed {
		return frameworktools.ToolMeta{}
	}
	return frameworktools.ToolMeta{
		Kind:     ToolKindForGroup(group.Name),
		Groups:   []string{group.Name},
		Resource: group.Resource,
	}
}

// ToolKindForGroup 把路由组翻译成框架的簇属类别。
//
// 口径 = 组注释的语义（permission_policy.go 的表头注释）：
//   - ro 不改共享状态 → read；
//   - rw / rw_session / rw_desktop 都写（区别只在写的对象：项目 / 本会话 / 共享外设）
//     → write；
//   - ctl 改循环控制流 → control（框架据此让非 root 主体**默认不可路由**）；
//   - adm 改能力面本身 → admin。
//
// Bits 刻意留 0：checker 在 meta.Bits==0 时采用候选组的 Mode（DecideForMeta），
// 于是"声明所需位"与"组要求位"永远是同一个数——再填一份就是第二个事实源。
func ToolKindForGroup(name string) frameworktools.ToolKind {
	switch name {
	case GroupRO:
		return frameworktools.ToolKindRead
	case GroupRW, GroupRWSession, GroupRWDesktop:
		return frameworktools.ToolKindWrite
	case GroupCTL:
		return frameworktools.ToolKindControl
	case GroupADM:
		return frameworktools.ToolKindAdmin
	default:
		return ""
	}
}
