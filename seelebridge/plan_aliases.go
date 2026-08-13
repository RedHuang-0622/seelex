package seelebridge

import (
	"context"
	"time"

	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
)

// ── plan 域 API 兼容别名 ───────────────────────────────────────────────
// plan 域已迁入 seelebridge/plan（executor/policy/preflight/events/replan/
// input/factory/branch 类型）；本文件在根包重导出全部公开符号，保证既有
// 调用面（application/contract、Runtime 装配、根包测试）不因拆包而破坏。

// PlanEdge 是 DAG 边的只读视图（graph.go）。
type PlanEdge = plan.PlanEdge

// AdjacencyToEdges converts an adjacency list to a PlanEdge slice.
func AdjacencyToEdges(adj map[string][]string) []PlanEdge {
	return plan.AdjacencyToEdges(adj)
}

// DetectCycle checks if the directed graph contains a cycle.
func DetectCycle(edges map[string][]string) error {
	return plan.DetectCycle(edges)
}

// TopoSort produces a stable topological order.
func TopoSort(entry string, edges map[string][]string, allNodes map[string]struct{}) []string {
	return plan.TopoSort(entry, edges, allNodes)
}

// PlanPolicy defines the runtime constraints applied to a plan_load request.
type PlanPolicy = plan.PlanPolicy

// PlanPreflight is the audited result of an isolated optional planning turn.
type PlanPreflight = plan.PlanPreflight

// ReplanRequest is the bounded, auditable context supplied to an isolated
// recovery-planning turn.
type ReplanRequest = plan.ReplanRequest

// PlanNodeEvent 是 seelex 侧节点/计划投影事件（不含 Seele 运行时类型）。
type PlanNodeEvent = plan.PlanNodeEvent

// ReplanMetrics is a non-secret, process-wide accounting snapshot.
type ReplanMetrics = plan.ReplanMetrics

// PlanBranchEvent is the Seelex-owned representation of a branch lifecycle event.
type PlanBranchEvent = plan.PlanBranchEvent

// PlanBranchBinding freezes the request-scoped values used to construct
// branch runtimes.
type PlanBranchBinding = plan.PlanBranchBinding

// SeelexNodeInput 是产品 DSL 的节点负载。
type SeelexNodeInput = plan.SeelexNodeInput

// NodeBudgetInput 是节点子代理的预算参数。
type NodeBudgetInput = plan.NodeBudgetInput

// LoadedPlanDoc 是当前加载的权威 Plan 的规范化存储。
type LoadedPlanDoc = plan.LoadedPlanDoc

// NormalizePlanLoadArguments converts the LLM-friendly list forms accepted by
// plan_load into Seele's canonical object-keyed WorkPlan JSON.
func NormalizePlanLoadArguments(argsJSON string) (string, error) {
	return plan.NormalizePlanLoadArguments(argsJSON)
}

// ExecutorDeps 是 Executor 的能力注入点（Runtime 装配沿用旧名）。
type planExecutorDeps = plan.ExecutorDeps

// replanPrompt 渲染可复制的恢复规划提示（根包薄壳）。
func replanPrompt(policy PlanPolicy) string { return plan.ReplanPrompt(policy) }

// canonicalPlanDocument 把规范化 plan_load JSON 转为 codec.Document（根包薄壳）。
func canonicalPlanDocument(canonical string) (codec.Document[SeelexNodeInput], error) {
	return plan.CanonicalPlanDocument(canonical)
}

// newPlanExecutor 构造 plan 执行域组件（plan.NewExecutor 的根包薄壳，
// 保持 Runtime 装配与既有测试调用面不变）。
func newPlanExecutor(deps plan.ExecutorDeps, maxConcurrentReplans, maxReplansPerWindow, maxReplanProviderRequests int, replanWindow time.Duration) *plan.Executor {
	return plan.NewExecutor(deps, maxConcurrentReplans, maxReplansPerWindow, maxReplanProviderRequests, replanWindow)
}

// newPlanEventSink 构造事件投影 sink（根包薄壳）。
func newPlanEventSink() *plan.EventSink { return plan.NewEventSink() }

// newReplanGuard 构造重规划护栏（根包薄壳）。
func newReplanGuard(maxConcurrent, maxWindowAttempts, maxProviderRequests int, window time.Duration) *plan.ReplanGuard {
	return plan.NewReplanGuard(maxConcurrent, maxWindowAttempts, maxProviderRequests, window)
}

// newProductNode 构造确定性执行节点（根包薄壳，buildNode 使用）。
func newProductNode(spec codec.NodeSpec[SeelexNodeInput], kind string) node.Node {
	return plan.NewProductNode(spec, kind)
}

// newApprovalGateNode 构造审批门控节点（根包薄壳，buildNode 使用）。
func newApprovalGateNode(spec codec.NodeSpec[SeelexNodeInput], gate func() approve.ApprovalGate) node.Node {
	return plan.NewApprovalGateNode(spec, gate)
}

// newPlanToolProvider 构造 plan 工具 provider（根包薄壳，plan.NewExecutor 内部使用）。
func newPlanToolProvider(executor *plan.Executor) *plan.ToolProvider {
	return plan.NewToolProvider(executor)
}

// authorizePlanMutation 是 plan 变更授权钩子（当前为放行钩子；归属 plan 域）。
func authorizePlanMutation(ctx context.Context, toolName string) error {
	return plan.AuthorizePlanMutation(ctx, toolName)
}

// planEventSink 类型别名（事件 sink；根包测试/装配沿用旧名）。
type planEventSink = plan.EventSink

// replanGuard 类型别名（重规划护栏；根包测试沿用旧名）。
type replanGuard = plan.ReplanGuard

// planExecutor 类型别名（plan 执行域组件；Runtime 装配沿用旧名）。
type planExecutor = plan.Executor

// planToolProvider 类型别名（plan 工具 provider）。
type planToolProvider = plan.ToolProvider

// loadedPlanDoc 类型别名（当前加载的权威 Plan 存储）。
type loadedPlanDoc = plan.LoadedPlanDoc

// planLoadSpec 是 plan_load 请求的规范化 JSON 骨架（plan 域内部类型）。
type planLoadSpec = plan.PlanLoadSpec
