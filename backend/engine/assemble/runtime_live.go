package assemble

// runtime_live.go 是**子代理第一视角实时流**的宿主转发面。
//
// 落点沿革（P21「域方法随类型走」，plan §2.5 的 `runtime_live` → 多智能体侧）：状态
// （`live*` 那一组字段）、广播循环、事件源接线、窗口归一化判据与分页读法都搬进域包
// [`backend/multiagent/subagent/live_dispatch.go`](../multiagent/subagent/live_dispatch.go)
// 的 `LiveDispatcher`；这里只留**一行薄转发**与同值常量别名——`Runtime` 的公开方法名与
// 签名逐字不变，私有转发面（`startLiveDispatcher` / `stopLiveDispatcher` / `broadcastLive` /
// `recordNodeAssistant` / `subagentLiveWindowSize`）也保留原名，既有调用点与用例零改动。
//
// 两个事件源（阶段日志 `NodeStageEvents`、工具事件 `ToolEventState.Subscribe`）走函数值端口；
// **配置读数留在宿主**（`Runtime.subagentLiveWindow` 字段），归一化判据在域包
// （`subagentsession.NormalizeLiveWindow`）。

import (
	"fmt"

	subagentsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 同值常量别名：既有用例引用这些名字，语义与域包一一对应。
const (
	subagentLiveChanCap          = subagentsession.LiveChanCap
	subagentLiveHistoryCap       = subagentsession.LiveHistoryCap
	subagentLiveHistoryMinWindow = subagentsession.LiveHistoryMinWindow
	subagentLivePageDefaultLimit = subagentsession.LivePageDefaultLimit
)

// subagentLiveWindowSize 归一化实时回放窗口上限（判据在域包：0 → 默认 512；< 下限 → 下限）。
// 零值 Runtime（测试直接构造）也走这一份归一，不把窗口剪成 0。
func (r *Runtime) subagentLiveWindowSize() int {
	if r == nil {
		return subagentsession.LiveHistoryCap
	}
	return subagentsession.NormalizeLiveWindow(r.subagentLiveWindow)
}

// startLiveDispatcher 启动实时流分发器（幂等）。
func (r *Runtime) startLiveDispatcher() {
	if r == nil || r.live == nil {
		return
	}
	r.live.Start()
}

// stopLiveDispatcher 停止实时流分发（幂等；Shutdown 调用）。
func (r *Runtime) stopLiveDispatcher() {
	if r == nil || r.live == nil {
		return
	}
	r.live.Stop()
}

// SubscribeSubagentLive 订阅 node 第一视角实时流（域包 `LiveDispatcher.Subscribe`）。
func (r *Runtime) SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error) {
	if r == nil || r.live == nil {
		return nil, nil, nil, fmt.Errorf("live subscribe: node id required")
	}
	return r.live.Subscribe(nodeID)
}

// SubagentLiveHistoryPage 读回实时回放的一页（域包 `LiveDispatcher.HistoryPage`）。
func (r *Runtime) SubagentLiveHistoryPage(nodeID string, offset, limit int) dto.SubagentLiveHistoryPage {
	if r == nil || r.live == nil {
		return dto.SubagentLiveHistoryPage{ScopeID: nodeID}
	}
	return r.live.HistoryPage(nodeID, offset, limit)
}

// broadcastLive 把一条事件写进窗口并投递（域包 `LiveDispatcher.Broadcast`）。
func (r *Runtime) broadcastLive(event dto.SubagentLiveEvent) {
	if r == nil || r.live == nil {
		return
	}
	r.live.Broadcast(event)
}

// recordNodeAssistant 投递子代理 assistant 正文增量（域包 `LiveDispatcher.RecordAssistant`：
// 只在该节点已有订阅者时保留实时面，无人订阅不积累）。
func (r *Runtime) recordNodeAssistant(nodeID, delta string) {
	if r == nil || r.live == nil {
		return
	}
	r.live.RecordAssistant(nodeID, delta)
}
