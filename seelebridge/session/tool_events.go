package session

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RedHuang-0622/Seele/tools"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// SubagentToolEvent 是 Runtime 对子代理工具调用的稳定投影，不暴露 Seele
// 内部 hook 类型。Application 负责截断并写入 Snapshot/EventHub。
// 结构体本体以 application/contract/dto 为单源（与 application 快照投影同一
// 形状），本包用别名保持运行态引用面兼容。
type SubagentToolEvent = dto.SubagentToolEvent

// ToolEventState 是子代理工具事件的分发器（回调 + 序号），由根包注入
// 工具注册表 middleware。
type ToolEventState struct {
	mu          sync.RWMutex
	callback    func(SubagentToolEvent)
	seq         atomic.Uint64
	observerSeq int64
	observers   map[int64]func(SubagentToolEvent)
}

// NewToolEventState 构造子代理工具事件分发器。
func NewToolEventState() *ToolEventState {
	return &ToolEventState{observers: make(map[int64]func(SubagentToolEvent))}
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

// Subscribe 注册额外的工具事件观察者（多消费者：main 的 SetCallback 之外，
// Runtime 实时流等可并行订阅）。返回取消函数（幂等）。
func (s *ToolEventState) Subscribe(fn func(SubagentToolEvent)) func() {
	if s == nil || fn == nil {
		return func() {}
	}
	s.mu.Lock()
	s.observerSeq++
	id := s.observerSeq
	s.observers[id] = fn
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.observers, id)
			s.mu.Unlock()
		})
	}
}

// Publish 派发一条子代理工具事件（无观察者则丢弃）。
func (s *ToolEventState) Publish(event SubagentToolEvent) {
	if s == nil {
		return
	}
	s.mu.RLock()
	callback := s.callback
	observers := make([]func(SubagentToolEvent), 0, len(s.observers))
	for _, observer := range s.observers {
		observers = append(observers, observer)
	}
	s.mu.RUnlock()
	if callback != nil {
		callback(event)
	}
	for _, observer := range observers {
		observer(event)
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
