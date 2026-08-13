// Plan 执行域组件：把散落在 Runtime 上的 plan 状态与生命周期收进单一组件。
// Runtime 只保留公开方法委托；组件经 deps 闭包注入 Runtime 能力，不反向依赖。
package plan

import (
	"context"
	"log"
	"sync"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// ExecutorDeps 是 Executor 的能力注入点（闭包均指向 Runtime 装配面，
// 组件自身不持有 *Runtime，保持依赖方向 Runtime → Executor）。
type ExecutorDeps struct {
	Model               string
	Heartbeat           time.Duration
	Limits              seelexctx.Limits
	PlanDecisionTimeout time.Duration
	Accounts            func() []model.AccountSpec
	LoadPlanDefinition  func() (types.Tool, bool)
	Dispatch            func(context.Context, string, string) (string, error)
	NodeFactory         func() codec.NodeFactory[SeelexNodeInput]
	EventError          frameworkevent.ErrorHandler
}

// Executor 管理 Plan 策略、分支绑定、执行 run ID、事件投影通道、
// 重规划护拦与 plan 子代理工厂。DAG 执行仍委托 Seele workplan 内核
// （workplan.NewFromPlan），组件只负责 Seelex 侧的编排与并发边界。
type Executor struct {
	deps ExecutorDeps

	policyMu sync.RWMutex
	policy   PlanPolicy

	bindingMu sync.RWMutex
	binding   PlanBranchBinding

	runMu        sync.RWMutex
	currentRunID string

	provider   *ToolProvider
	events     *EventSink         // plan 执行事实 → 事件库 + 投影订阅
	nodeEvents chan PlanNodeEvent // plan 节点事件 channel（CSP：application 消费者处理）
	replan     *ReplanGuard

	agentFactoryMu sync.RWMutex
	agentFactory   node.AgentFactory // bridge.NewAgentFactory 产物（plan 子代理工厂）

	approvalMu   sync.RWMutex
	approvalGate approve.ApprovalGate

	eventErrorMu sync.RWMutex
	eventError   frameworkevent.ErrorHandler
}

// newPlanExecutor 装配 plan 执行域组件：事件通道与订阅在构造时建立，
// provider 由组件持有（newPlanToolProvider 接收 executor 而非 Runtime）。
func NewExecutor(
	deps ExecutorDeps,
	maxConcurrentReplans, maxReplansPerWindow, maxReplanProviderRequests int,
	replanWindow time.Duration,
) *Executor {
	executor := &Executor{
		deps:       deps,
		events:     NewEventSink(),
		nodeEvents: make(chan PlanNodeEvent, 256),
		replan:     NewReplanGuard(maxConcurrentReplans, maxReplansPerWindow, maxReplanProviderRequests, replanWindow),
		eventError: deps.EventError,
	}
	if executor.eventError == nil {
		executor.eventError = func(_ context.Context, err error) {
			log.Printf("seelebridge: event sink: %v", err)
		}
	}
	executor.provider = NewToolProvider(executor)
	// plan 节点事件走 CSP channel（非阻塞投递；消费者慢时丢事件——前端经
	// Snapshot resync 兜底），application 侧不同步回调嵌套。
	executor.events.Subscribe(func(event PlanNodeEvent) {
		select {
		case executor.nodeEvents <- event:
		default:
		}
	})
	return executor
}

// Provider 返回 plan 工具 provider，供 Runtime 注册进工具注册表。
func (executor *Executor) Provider() *ToolProvider {
	if executor == nil {
		return nil
	}
	return executor.provider
}

// SetPolicy 更新后续 plan_load 应用的约束策略。
func (executor *Executor) SetPolicy(policy PlanPolicy) {
	if executor == nil {
		return
	}
	executor.policyMu.Lock()
	executor.policy = policy
	executor.policyMu.Unlock()
}

// Policy 返回当前 Plan 策略。
func (executor *Executor) Policy() PlanPolicy {
	if executor == nil {
		return PlanPolicy{}
	}
	executor.policyMu.RLock()
	defer executor.policyMu.RUnlock()
	return executor.policy
}

// SetBinding 冻结下一次 plan_run 的请求级绑定（默认值填充由 Runtime 委托完成）。
func (executor *Executor) SetBinding(binding PlanBranchBinding) {
	if executor == nil {
		return
	}
	executor.bindingMu.Lock()
	executor.binding = binding
	executor.bindingMu.Unlock()
}

// Binding 返回当前分支绑定。
func (executor *Executor) Binding() PlanBranchBinding {
	if executor == nil {
		return PlanBranchBinding{}
	}
	executor.bindingMu.RLock()
	defer executor.bindingMu.RUnlock()
	return executor.binding
}

// SetApprovalGate 设置 plan kind:approve/manual 节点的审批门控。
func (executor *Executor) SetApprovalGate(gate approve.ApprovalGate) {
	if executor == nil {
		return
	}
	executor.approvalMu.Lock()
	executor.approvalGate = gate
	executor.approvalMu.Unlock()
}

// currentApprovalGate 返回当前审批门（approvalGateNode / worktreeManager 的读取器）。
func (executor *Executor) CurrentApprovalGate() approve.ApprovalGate {
	if executor == nil {
		return nil
	}
	executor.approvalMu.RLock()
	defer executor.approvalMu.RUnlock()
	return executor.approvalGate
}

// SetAgentFactory 装配 plan 子代理工厂（bridge.NewAgentFactory 产物）。
func (executor *Executor) SetAgentFactory(factory node.AgentFactory) {
	if executor == nil {
		return
	}
	executor.agentFactoryMu.Lock()
	executor.agentFactory = factory
	executor.agentFactoryMu.Unlock()
}

// currentAgentFactory 返回当前 plan 子代理工厂（SeelexAgentNode 的读取器）。
func (executor *Executor) CurrentAgentFactory() node.AgentFactory {
	if executor == nil {
		return nil
	}
	executor.agentFactoryMu.RLock()
	defer executor.agentFactoryMu.RUnlock()
	return executor.agentFactory
}

// PlanNodeEventChannel 返回 plan 节点事件 channel（CSP：application 消费者串行处理）。
func (executor *Executor) PlanNodeEventChannel() <-chan PlanNodeEvent {
	if executor == nil || executor.nodeEvents == nil {
		return nil
	}
	return executor.nodeEvents
}

// SetPlanNodeCallback 注册节点/计划状态投影订阅（EventSink）。
func (executor *Executor) SetPlanNodeCallback(cb func(PlanNodeEvent)) {
	if executor == nil {
		return
	}
	executor.events.Subscribe(cb)
}

// SetEventPersister 安装执行事实持久化钩子（sessionstore 事件库）。
func (executor *Executor) SetEventPersister(fn func(context.Context, frameworkevent.Event) error) {
	if executor == nil {
		return
	}
	executor.events.SetPersister(fn)
}

// SetEventErrorHandler 覆盖 Sink 失败处理（默认 log.Printf 兜底）。
func (executor *Executor) SetEventErrorHandler(handler frameworkevent.ErrorHandler) {
	if executor == nil || handler == nil {
		return
	}
	executor.eventErrorMu.Lock()
	executor.eventError = handler
	executor.eventErrorMu.Unlock()
}

// currentEventError 返回当前 Sink 失败处理（runPlan 读取）。
func (executor *Executor) CurrentEventError() frameworkevent.ErrorHandler {
	if executor == nil {
		return nil
	}
	executor.eventErrorMu.RLock()
	defer executor.eventErrorMu.RUnlock()
	return executor.eventError
}

// AppendPhase 记录 Seelex 侧子代理阶段事件：内部读取当前分支绑定与 run ID，
// 保持与框架 runner 事件相同的 plan/run/session 关联契约。
func (executor *Executor) AppendPhase(ctx context.Context, nodeID, status string) {
	if executor == nil || executor.events == nil || nodeID == "" || status == "" {
		return
	}
	executor.runMu.RLock()
	runID := executor.currentRunID
	executor.runMu.RUnlock()
	executor.events.AppendPhase(ctx, executor.Binding(), runID, nodeID, status)
}

// ReplanMetrics 返回进程级 replan 成本与拒绝统计。
func (executor *Executor) ReplanMetrics() ReplanMetrics {
	if executor == nil || executor.replan == nil {
		return ReplanMetrics{}
	}
	return executor.replan.snapshot()
}

// CurrentRunID 返回当前执行 run ID（执行中非空，结束后清空；诊断/测试读取）。
func (executor *Executor) CurrentRunID() string {
	if executor == nil {
		return ""
	}
	executor.runMu.RLock()
	defer executor.runMu.RUnlock()
	return executor.currentRunID
}

// EventSink 返回执行事实投影 sink（事件库 + 订阅；诊断/测试读取）。
func (executor *Executor) EventSink() *EventSink {
	if executor == nil {
		return nil
	}
	return executor.events
}

// MaxForkConcurrency 返回当前加载 Plan 的并发上限（诊断/测试读取）。
func (executor *Executor) MaxForkConcurrency() int {
	if executor == nil || executor.provider == nil {
		return 0
	}
	executor.provider.mu.Lock()
	defer executor.provider.mu.Unlock()
	return executor.provider.maxForkConcurrency
}

// LoadedPlan 返回当前加载的权威 Plan（无 → false；只读，供诊断/测试读取）。
func (executor *Executor) LoadedPlan() (*LoadedPlanDoc, bool) {
	if executor == nil || executor.provider == nil {
		return nil, false
	}
	executor.provider.mu.Lock()
	defer executor.provider.mu.Unlock()
	if executor.provider.loaded == nil {
		return nil, false
	}
	return executor.provider.loaded, true
}
