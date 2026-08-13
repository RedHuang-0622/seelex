package seelebridge

import "github.com/RedHuang-0622/seelex/seelebridge/session"

// SubagentToolEvent 是子代理工具调用事件的兼容别名（实现下沉 session/ 域）。
type SubagentToolEvent = session.SubagentToolEvent

// SetSubagentToolCallback 注入子代理工具活动观察者（委托 session.ToolEventState）。
func (r *Runtime) SetSubagentToolCallback(callback func(SubagentToolEvent)) {
	if r == nil || r.toolEvents == nil {
		return
	}
	r.toolEvents.SetCallback(callback)
}
