package seelebridge

import (
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/session"
)

// 子代理树兼容别名与 Runtime 门面：类型/常量本体下沉 session/ 域，
// 根包保留公开 API 面（application 与 TUI 消费）。
type (
	SubAgentNodeStatus  = session.SubAgentNodeStatus
	SubAgentTreeNode    = session.SubAgentTreeNode
	SubAgentNodeContext = session.SubAgentNodeContext
)

const (
	SubAgentQueued  = session.SubAgentQueued
	SubAgentRunning = session.SubAgentRunning
	SubAgentDone    = session.SubAgentDone
	SubAgentFailed  = session.SubAgentFailed
)

// mainAgentNodeID 是子代理树的合成根节点 ID（常量本体下沉 internal/model）。
const mainAgentNodeID = model.MainAgentNodeID

// ClearSubagentTree 清空子代理树（GUI"清空"按钮入口）。失败节点（树里
// 唯一会长期驻留的节点）由用户显式清走；详情数据面不受影响。
func (r *Runtime) ClearSubagentTree() error {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.Clear()
}

// SubagentTreeEvents 返回子代理树生命周期信号 channel（CSP：fork 注册/节点
// 完成时投递信号，application 消费者刷新工作表格，无需模型手动打点）。
func (r *Runtime) SubagentTreeEvents() <-chan struct{} {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.Events()
}

// SubAgentTree 返回子代理树的只读投影（根 = 主代理；含全部层级子节点）。
func (r *Runtime) SubAgentTree() []SubAgentTreeNode {
	if r == nil || r.subagentTree == nil {
		return nil
	}
	return r.subagentTree.Projection()
}
