package table

// continuation.go — 自动续跑的**信号面**（docs/arch/goal-auto-continue.md §2.5/§2.8.2）。
//
// 分工写死在这里：
//   - 信号源（本文件）：一条"某会话刚在回合休止点收尾，去看看要不要自动续"的提示。
//     `NotifyContinuation` 是非阻塞发送：满了就丢（丢的是**唤醒**，不是判据——
//     同一会话的下一条信号、或已排队的 RetryDelay 定时器仍会扫到）。
//   - 判据（application/execution 的续跑 gate）：本文件一行判据都不写。
//
// 为什么信号挂在 WorkTable 运行时上：它已经持有 AsyncConsumer 与既有 RetryDelay 定时器
// （续跑不新增定时器），也是装配层唯一已经握在手里的"消费者宿主"，因此不需要为这条
// 通道再加一个组件字段。

import (
	"github.com/RedHuang-0622/seelex/application/execution"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ContinuationGate 是续跑 gate 的两条窄能力：扫一次门、读一次横条状态。
type ContinuationGate interface {
	TriggerGoalContinuations(sessionID string, triggered map[string]struct{}) int
	Status(sessionID string) execution.ContinuationStatus
}

// NotifyContinuation 发一条续跑唤醒信号（payload = sessionID）。非阻塞：
// 队列满时丢弃本条，不把回合收尾线程拖住。
func (runtime *Runtime) NotifyContinuation(sessionID string) {
	if runtime == nil || runtime.continuationEvents == nil || sessionID == "" {
		return
	}
	select {
	case runtime.continuationEvents <- sessionID:
	default:
	}
}

// ContinuationStatus 是横条读面（goal 状态 / 子状态 / stalled 时长）的运行时入口。
func (runtime *Runtime) ContinuationStatus(sessionID string) execution.ContinuationStatus {
	if runtime == nil || runtime.deps.Continuation == nil {
		return execution.ContinuationStatus{}
	}
	return runtime.deps.Continuation.Status(sessionID)
}

// ContinuationView 把横条读面投影成快照里的两层状态视图（GUI / 工具只读这一份）。
// nil = 续跑能力未装配，调用方据此不显示横条。
func (runtime *Runtime) ContinuationView(sessionID string) *dto.GoalContinuationView {
	if runtime == nil || runtime.deps.Continuation == nil {
		return nil
	}
	return runtime.ContinuationStatus(sessionID).View()
}
