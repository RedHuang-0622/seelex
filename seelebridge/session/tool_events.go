package session

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RedHuang-0622/Seele/tools"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
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

// ToolEventState 是子代理工具事件的分发器（回调 + 序号），由根包注入
// 工具注册表 middleware。
type ToolEventState struct {
	mu       sync.RWMutex
	callback func(SubagentToolEvent)
	seq      atomic.Uint64
}

// NewToolEventState 构造子代理工具事件分发器。
func NewToolEventState() *ToolEventState {
	return &ToolEventState{}
}

// SetCallback 注入子代理工具活动观察者。主代理工具仍由 ToolHookBridge
// 投影，避免同一次调用重复上报。
func (s *ToolEventState) SetCallback(callback func(SubagentToolEvent)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.callback = callback
	s.mu.Unlock()
}

// Publish 派发一条子代理工具事件（无观察者则丢弃）。
func (s *ToolEventState) Publish(event SubagentToolEvent) {
	if s == nil {
		return
	}
	s.mu.RLock()
	callback := s.callback
	s.mu.RUnlock()
	if callback != nil {
		callback(event)
	}
}

// Middleware 从 NodeScope 识别子代理调用并投影 started/completed。
// 中间件包在权限门外层，因此权限拒绝也会以 completed/error 返回前端。
func (s *ToolEventState) Middleware() tools.Middleware {
	return func(name string, next tools.ToolHandler) tools.ToolHandler {
		return tools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			scope, ok := model.NodeScopeFromContext(ctx)
			if !ok || scope.NodeID == "" || scope.Role != model.RoleSubAgent {
				return next.Execute(ctx, argsJSON)
			}
			startedAt := time.Now()
			id := fmt.Sprintf("subtool-%d", s.seq.Add(1))
			s.Publish(SubagentToolEvent{
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
			s.Publish(completed)
			return result, err
		})
	}
}
