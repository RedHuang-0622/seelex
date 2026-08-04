package seelebridge

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RedHuang-0622/Seele/tools"
)

// SubagentToolEvent 是 Runtime 对子代理工具调用的稳定投影，不暴露 Seele
// 内部 hook 类型。Application 负责截断并写入 Snapshot/EventHub。
type SubagentToolEvent struct {
	ID        string
	NodeID    string
	Name      string
	Arguments string
	Result    string
	Error     string
	Status    string
	StartedAt time.Time
	Duration  time.Duration
}

type subagentToolEventState struct {
	mu       sync.RWMutex
	callback func(SubagentToolEvent)
	seq      atomic.Uint64
}

func newSubagentToolEventState() *subagentToolEventState {
	return &subagentToolEventState{}
}

// SetSubagentToolCallback 注入子代理工具活动订阅者。主代理工具仍由
// ToolHookBridge 投影，避免同一次调用重复上报。
func (r *Runtime) SetSubagentToolCallback(callback func(SubagentToolEvent)) {
	if r == nil || r.toolEvents == nil {
		return
	}
	r.toolEvents.mu.Lock()
	r.toolEvents.callback = callback
	r.toolEvents.mu.Unlock()
}

func (r *Runtime) publishSubagentToolEvent(event SubagentToolEvent) {
	if r == nil || r.toolEvents == nil {
		return
	}
	r.toolEvents.mu.RLock()
	callback := r.toolEvents.callback
	r.toolEvents.mu.RUnlock()
	if callback != nil {
		callback(event)
	}
}

// subagentToolMiddleware 从 NodeScope 识别子代理调用并投影 started/completed。
// 中间件包在权限门外层，因此权限拒绝也会以 completed/error 返回前端。
func (r *Runtime) subagentToolMiddleware() tools.Middleware {
	return func(name string, next tools.ToolHandler) tools.ToolHandler {
		return tools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			scope, ok := NodeScopeFromContext(ctx)
			if !ok || scope.NodeID == "" || scope.Role != RoleSubAgent {
				return next.Execute(ctx, argsJSON)
			}
			startedAt := time.Now()
			id := fmt.Sprintf("subtool-%d", r.toolEvents.seq.Add(1))
			r.publishSubagentToolEvent(SubagentToolEvent{
				ID: id, NodeID: scope.NodeID, Name: name, Arguments: argsJSON,
				Status: "running", StartedAt: startedAt,
			})
			result, err := next.Execute(ctx, argsJSON)
			completed := SubagentToolEvent{
				ID: id, NodeID: scope.NodeID, Name: name, Arguments: argsJSON,
				Result: result, Status: "success", StartedAt: startedAt,
				Duration: time.Since(startedAt),
			}
			if err != nil {
				completed.Status = "error"
				completed.Error = err.Error()
			}
			r.publishSubagentToolEvent(completed)
			return result, err
		})
	}
}
