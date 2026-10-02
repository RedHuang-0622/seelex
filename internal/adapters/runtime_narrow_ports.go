package adapters

// runtime_narrow_ports.go — 窄可选端口的**生产转发面**。
//
// 为什么需要这个文件（2026-10-02 现场）：
//
// application/core 对"不是每个后端都有的能力"一律用**类型断言**探测
// （见 contract.TeamworkBoardProjection / context_runtime.CompactionIndexPort 的
// 注释："有就有、没有就是没装配"）。这些断言的接收者是 `Deps.Runtime`，而组合根
// （main.go）注入的**不是** *seelebridge.Runtime，而是本包的 RuntimePort：
//
//	Engine: eng, Runtime: adapters.RuntimePort{Runtime: runtime},
//
// RuntimePort 是**显式手写转发**的包装（无内嵌、无方法提升）。于是"bridge 上有这个
// 方法"与"core 断言得到这个方法"之间隔着一份人肉清单——漏一行，断言就是静默
// false，功能**整块消失**，而且没有任何编译或测试信号。
//
// 这不是假设。下面两行就漏了，后果在 GUI 上直接可见：
//
//	① TeamworkBoardSnapshot 漏 → contract.TeamworkBoardProjection 断言恒 false
//	   → runtime.teamwork_board 恒 nil → 「团队看板」面板与 TUI Alt+T 那一节
//	   永远整块退场（用户报"团队看板没有接线到我们"）。
//	② SessionContextStoreFor 漏 → service_assembler 的 goalStoreFor 恒 nil
//	   → goal 域的 Controller 没有 Store：第五栈不落盘、看板存档面从不绑定
//	   → metadata/board_goal.json 从不产生、重启后 goal 看板无从恢复
//	   （用户报"goal 看板没有重启恢复"）。
//
// 两条都在真实 store 上取证过：同一次会话的 metadata/ 里有 teamwork.json 与
// board_team.json（团队侧经 main.go 的 SetTeamworkBackend 直接走 Runtime，不经过
// 本包装，所以那条链路是通的），却**没有** board_goal.json。
//
// 纪律（把清单变成编译期事实）：application/core 里新增一个窄可选端口断言时，
// 必须在本文件补一行转发 + 一行 `var _` 断言。TestRuntimePortForwardsEveryNarrowPort
// 是一次性闸门：application/core 里出现新的 `Deps.Runtime.(` 断言而本文件没同步时
// 它会红，并指名道姓要求补转发。

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TeamworkBoardSnapshot 实现 contract.TeamworkBoardProjection：把某会话的团队看板
// 只读投影（计划 + 作业行 + 审计流水）交给核心。nil = 未装配 teamwork / 解析不出
// 作用域 / 该会话尚无计划——前端据此整块退场（不留空壳），本层不改变这个语义。
func (port RuntimePort) TeamworkBoardSnapshot(sessionID string) *dto.TeamworkBoardView {
	if port.Runtime == nil {
		return nil
	}
	return port.Runtime.TeamworkBoardSnapshot(sessionID)
}

// SessionContextStoreFor 实现 application/core 的会话上下文存储取用面：goal 第五栈
// （以及绑定它的两块看板存档面）经它按会话注入。未实例化/未绑定 → nil，goal 域退回
// 内存态（不报错、不阻断）——那是能力缺失的降级，不是错误。
func (port RuntimePort) SessionContextStoreFor(sessionID string) *sessionstore.SessionContextStore {
	if port.Runtime == nil {
		return nil
	}
	return port.Runtime.SessionContextStoreFor(sessionID)
}

// ReplanMetricsFor 实现 view_state 的 per-session replan 指标面。漏了它不会报错：
// 调用方会静默回退到**进程级合计**，于是"这个会话的 replan 花了多少"在多会话下
// 变成"所有会话加起来"，读数看着正常但答的不是被问的那个问题。
func (port RuntimePort) ReplanMetricsFor(sessionID string) dto.ReplanMetrics {
	if port.Runtime == nil {
		return dto.ReplanMetrics{}
	}
	return port.Runtime.ReplanMetricsFor(sessionID)
}

// 编译期断言：application/core / view_state 用类型断言探测的每一个窄可选端口，
// 生产包装都必须满足。少一个方法，这里就编译不过——不留"运行时静默 false"。
var (
	_ contract.TeamworkBoardProjection    = RuntimePort{}
	_ context_runtime.CompactionIndexPort = RuntimePort{}
	_ interface {
		SessionContextStoreFor(string) *sessionstore.SessionContextStore
	} = RuntimePort{}
	_ interface {
		ReplanMetricsFor(string) dto.ReplanMetrics
	} = RuntimePort{}
	_ interface{ ForkInFlight(string) bool } = RuntimePort{}
	_ interface{ PerSessionExecution() bool } = RuntimePort{}
	_ interface {
		TaskSnapshotFor(string) []dto.TaskRecord
	} = RuntimePort{}
	// persistedPlanRestorer（application/core/session_history.go）：resume 时按
	// plan 参数恢复可执行 Plan；断言失败只是保留可见投影，症状同样无声。
	_ interface {
		RestorePlan(context.Context, string) error
	} = RuntimePort{}
)
