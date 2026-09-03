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
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
	workplancheckpoint "github.com/RedHuang-0622/Seele/workplan/runtime/checkpoint"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	seetelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
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

	// slotMu/slots 是按会话建立的 plan 额度槽（G1-C/M6）：policy/binding/
	// runID 都以 sessionID 为键隔离；"" 是无 sid legacy/默认槽（单飞执行
	// 期间行为与改造前一致，run 路径的 For 化读取随 G4 并行执行接线）。
	slotMu sync.RWMutex
	slots  map[string]*planSlot

	provider   *ToolProvider
	events     *EventSink         // plan 执行事实 → 事件库 + 投影订阅
	nodeEvents chan PlanNodeEvent // plan 节点事件 channel（CSP：application 消费者处理）
	replans    *ReplanGuards

	agentFactoryMu sync.RWMutex
	agentFactory   node.AgentFactory // bridge.NewAgentFactory 产物（plan 子代理工厂）

	nodeFactoryMu sync.RWMutex
	nodeFactory   func() codec.NodeFactory[SeelexNodeInput] // 后置注入（plan→node 构造环）

	approvalMu   sync.RWMutex
	approvalGate approve.ApprovalGate

	eventErrorMu sync.RWMutex
	eventError   frameworkevent.ErrorHandler

	checkpointMu    sync.RWMutex
	checkpointStore workplancheckpoint.Store
}

// planSlot 是一次会话的 plan 额度槽（策略 / 分支绑定 / run ID）。
type planSlot struct {
	policy  PlanPolicy
	binding PlanBranchBinding
	runID   string
}

// slotLocked 返回指定会话槽（写锁内调用；不存在则创建）。
func (executor *Executor) slotLocked(sessionID string) *planSlot {
	if executor.slots == nil {
		executor.slots = make(map[string]*planSlot)
	}
	slot := executor.slots[sessionID]
	if slot == nil {
		slot = &planSlot{}
		executor.slots[sessionID] = slot
	}
	return slot
}

// readSlot 返回指定会话槽（读锁内调用）。显式会话只读自己的槽：槽未建
// 时返回零值，绝不回退全局默认（G1-C/M6：额度按 sid 建槽，activeSessionID
// 与默认槽不得成为后台会话的事实源）；"" 是 legacy 无 sid 槽。
func (executor *Executor) readSlot(sessionID string) *planSlot {
	if executor.slots == nil {
		return &planSlot{}
	}
	if slot := executor.slots[sessionID]; slot != nil {
		return slot
	}
	return &planSlot{}
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
		replans:    NewReplanGuards(maxConcurrentReplans, maxReplansPerWindow, maxReplanProviderRequests, replanWindow),
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
		// 事件归属会话由 sink 写入（AppendPhase/AppendNodeResult 携带执行
		// 绑定；runner 事件从 Locations 投影）。订阅者不再读全局单例补号：
		// 并发会话的 plan 事件各自携带自身 sid，应用侧按会话路由投影。
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

// SetPolicy 更新默认（无 sid）槽的约束策略（legacy 入口：单飞执行期间
// 等价于全局策略；per-session 策略经 SetPolicyFor 建槽）。
func (executor *Executor) SetPolicy(policy PlanPolicy) {
	executor.SetPolicyFor("", policy)
}

// Policy 返回默认（无 sid）槽的 Plan 策略（legacy 读取）。
func (executor *Executor) Policy() PlanPolicy {
	return executor.PolicyFor("")
}

// SetPolicyFor 按会话建立策略槽（G1-C/M6：per-session effort 落位后，
// plan_load 按会话读取自己的约束；未建槽时回退默认槽）。
func (executor *Executor) SetPolicyFor(sessionID string, policy PlanPolicy) {
	if executor == nil {
		return
	}
	executor.slotMu.Lock()
	executor.slotLocked(sessionID).policy = policy
	executor.slotMu.Unlock()
}

// PolicyFor 返回指定会话的策略（未建槽时回退默认槽值）。
func (executor *Executor) PolicyFor(sessionID string) PlanPolicy {
	if executor == nil {
		return PlanPolicy{}
	}
	executor.slotMu.RLock()
	slot := executor.readSlot(sessionID)
	executor.slotMu.RUnlock()
	return slot.policy
}

// SetBinding 冻结下一次 plan_run 的请求级绑定（默认值填充由 Runtime 委托
// 完成）。绑定写入其 SessionID 槽；单飞执行期间默认（无 sid）槽保持为
// legacy 别名，两种读取语义一致。
func (executor *Executor) SetBinding(binding PlanBranchBinding) {
	if executor == nil {
		return
	}
	executor.SetBindingFor(binding.SessionID, binding)
	if binding.SessionID != "" {
		executor.SetBindingFor("", binding)
	}
}

// Binding 返回默认（无 sid）槽的分支绑定（legacy 读取；单飞执行别名）。
func (executor *Executor) Binding() PlanBranchBinding {
	return executor.BindingFor("")
}

// SetBindingFor 按会话建立分支绑定槽（G1-C/M6：后台会话 plan_run 绑定
// 互不覆盖；未建槽回退默认槽）。
func (executor *Executor) SetBindingFor(sessionID string, binding PlanBranchBinding) {
	if executor == nil {
		return
	}
	executor.slotMu.Lock()
	executor.slotLocked(sessionID).binding = binding
	executor.slotMu.Unlock()
}

// BindingFor 返回指定会话的分支绑定（未建槽回退默认槽值）。
func (executor *Executor) BindingFor(sessionID string) PlanBranchBinding {
	if executor == nil {
		return PlanBranchBinding{}
	}
	executor.slotMu.RLock()
	slot := executor.readSlot(sessionID)
	executor.slotMu.RUnlock()
	return slot.binding
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

// SetNodeFactory 后置注入节点工厂（plan→node 构造环解耦：node.Coordinator
// 需要 plan.Executor，而 nodeFactory 闭包需要 node；先建 plan 再建 node 后
// 回填工厂，与 SetAgentFactory 同模式）。
func (executor *Executor) SetNodeFactory(factory func() codec.NodeFactory[SeelexNodeInput]) {
	if executor == nil {
		return
	}
	executor.nodeFactoryMu.Lock()
	executor.nodeFactory = factory
	executor.nodeFactoryMu.Unlock()
}

// currentNodeFactory 返回当前节点工厂（后置注入优先，回退构造期 deps）。
func (executor *Executor) currentNodeFactory() func() codec.NodeFactory[SeelexNodeInput] {
	if executor == nil {
		return nil
	}
	executor.nodeFactoryMu.RLock()
	defer executor.nodeFactoryMu.RUnlock()
	if executor.nodeFactory != nil {
		return executor.nodeFactory
	}
	return executor.deps.NodeFactory
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

// SetCheckpointStore 装配 workplan checkpoint 持久化（sessionstore.CheckpointStore
// 或其他实现）。未装配时 plan_run 保持原有行为（无快照、无 Resume 入口）。
func (executor *Executor) SetCheckpointStore(store workplancheckpoint.Store) {
	if executor == nil {
		return
	}
	executor.checkpointMu.Lock()
	executor.checkpointStore = store
	executor.checkpointMu.Unlock()
}

// CurrentCheckpointStore 返回当前 checkpoint 存储（nil = 未装配）。
func (executor *Executor) CurrentCheckpointStore() workplancheckpoint.Store {
	if executor == nil {
		return nil
	}
	executor.checkpointMu.RLock()
	defer executor.checkpointMu.RUnlock()
	return executor.checkpointStore
}

// planCheckpointSnapshot 把一次 plan_run 的完整结果沉淀为 workplan 快照，
// 供后续 Resume 恢复（vars/已执行节点输出保留；NodeResult.Err 不跨存储）。
func planCheckpointSnapshot(entryNodeID string, result *workplanTypes.WorkPlanResult, runErr error) *workplanTypes.Snapshot {
	context := workplanTypes.NewWorkflowContext()
	status := workplanTypes.StatusFailed
	if result != nil {
		context.Result = result
		for _, nr := range result.NodeResults {
			if nr == nil {
				continue
			}
			context.SetResultRaw(nr.NodeID, nr.Output)
			if nr.Output != "" {
				context.PrevOutput = nr.Output
			}
		}
		switch {
		case result.Aborted:
			status = workplanTypes.StatusAborted
		case runErr != nil:
			status = workplanTypes.StatusFailed
		default:
			status = workplanTypes.StatusCompleted
		}
	} else if runErr != nil {
		status = workplanTypes.StatusFailed
	}
	return &workplanTypes.Snapshot{
		NodeID:    entryNodeID,
		Context:   context,
		Timestamp: time.Now().UTC(),
		Status:    status,
	}
}

// persistCheckpoint 把最近一次 plan_run 的快照写入 checkpoint 存储
// （best-effort：写入失败只记日志，不改变 plan_run 的返回语义）。
// 快照键使用入口节点 ID，与 runner.Resume(snapshotID) 的“从快照节点续跑”
// 契约一致。
func (executor *Executor) persistCheckpoint(entryNodeID string, result *workplanTypes.WorkPlanResult, runErr error) {
	store := executor.CurrentCheckpointStore()
	if store == nil || entryNodeID == "" {
		return
	}
	snapshot := planCheckpointSnapshot(entryNodeID, result, runErr)
	// Store.Save 直接落最终快照（含 status）；Resume 由 runner 内部的
	// checkpoint.Manager 经同一 Store.Load 读取（runner.WithCheckpoint 注入）。
	if err := store.Save(entryNodeID, snapshot); err != nil {
		log.Printf("seelebridge: persist plan checkpoint %q: %v", entryNodeID, err)
	}
}

// beginRun 登记默认（无 sid）槽的执行 run ID（legacy 单飞入口）。
func (executor *Executor) beginRun() string {
	return executor.beginRunFor("")
}

// beginRunFor 按会话登记执行 run ID（G1-C：不同会话的 run 分槽登记，
// 不互相覆盖；后台会话 plan 事件按自己槽的 run ID 关联）。
func (executor *Executor) beginRunFor(sessionID string) string {
	runID := newPlanRunID()
	executor.slotMu.Lock()
	executor.slotLocked(sessionID).runID = runID
	executor.slotMu.Unlock()
	return runID
}

// endRun 清除默认（无 sid）槽的 run ID（legacy 单飞入口）。
func (executor *Executor) endRun(runID string) {
	executor.endRunFor("", runID)
}

// endRunFor 清除指定会话槽的 run ID（只清理仍属于本次 run 的 ID）。
func (executor *Executor) endRunFor(sessionID, runID string) {
	if executor == nil {
		return
	}
	executor.slotMu.Lock()
	if executor.slots != nil {
		if slot := executor.slots[sessionID]; slot != nil && slot.runID == runID {
			slot.runID = ""
		}
	}
	executor.slotMu.Unlock()
}

// AppendPhase 记录 Seelex 侧子代理阶段事件：内部读取当前分支绑定与 run ID，
// 保持与框架 runner 事件相同的 plan/run/session 关联契约。
func (executor *Executor) AppendPhase(ctx context.Context, nodeID, status string) {
	if executor == nil || executor.events == nil || nodeID == "" || status == "" {
		return
	}
	sessionID := runSessionID(ctx)
	executor.slotMu.RLock()
	slot := executor.readSlot(sessionID)
	runID := slot.runID
	executor.slotMu.RUnlock()
	executor.events.AppendPhase(ctx, executor.BindingFor(sessionID), runID, nodeID, status)
}

// runSessionID 从执行 ctx 读取当前会话 ID（seelebridge 会话入口注入：
// ChatStreamFor/节点会话把 runChat 的 sid 转写为 telemetry 路由键，工具
// handler 原样收到）。空 = legacy 无 sid 路径（回退默认槽）。
func runSessionID(ctx context.Context) string {
	return seetelemetry.SessionIDFromContext(ctx)
}

// ReplanMetrics 返回 replan 成本与拒绝统计（legacy 无 sid 口径 = 默认槽）。
func (executor *Executor) ReplanMetrics() ReplanMetrics {
	if executor == nil || executor.replans == nil {
		return ReplanMetrics{}
	}
	return executor.replans.MetricsFor("")
}

// ReplanMetricsFor 返回指定会话的 replan 成本与拒绝统计（G1/M5：按会话
// 额度槽查询，视图只展示自己的重规划预算）。
func (executor *Executor) ReplanMetricsFor(sessionID string) ReplanMetrics {
	if executor == nil || executor.replans == nil {
		return ReplanMetrics{}
	}
	return executor.replans.MetricsFor(sessionID)
}

// CurrentRunID 返回默认（无 sid）槽的执行 run ID（legacy 读取）。
func (executor *Executor) CurrentRunID() string {
	return executor.CurrentRunIDFor("")
}

// CurrentRunIDFor 返回指定会话槽的执行 run ID（诊断/测试读取）。
func (executor *Executor) CurrentRunIDFor(sessionID string) string {
	if executor == nil {
		return ""
	}
	executor.slotMu.RLock()
	slot := executor.readSlot(sessionID)
	executor.slotMu.RUnlock()
	return slot.runID
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
