# Plan

`seelebridge/plan` 承载 Plan 执行域（依赖方向：根 facade → plan；plan 不反向
依赖 seelebridge 根包，共享账号类型经 `internal/model`，节点负载类型本包导出）：

- `graph.go`：`PlanEdge` / `AdjacencyToEdges` / `DetectCycle` / `TopoSort`。
- `policy.go`：`PlanPolicy`（effort 约束、节点预算上限校验、并发解析）。
- `executor.go`：`Executor` 执行域组件（策略/绑定/runID/事件通道/replan/
  审批门/子代理工厂；deps 闭包注入 Runtime 能力，含 `CurrentRunID`/
  `EventSink`/`LoadedPlan`/`MaxForkConcurrency` 读取面）。
- `preflight.go`：`PlanPreflight`/`ReplanRequest` + 隔离规划/重规划回合。
- `tool_provider.go`：`ToolProvider`（plan_load/plan_run/plan_clear/status/
  export/validate）、`LoadedPlanDoc`、`Executor.RunPlan`。
- `events.go`：`PlanNodeEvent` 投影 + `EventSink`（事件库 + 订阅 + 持久化钩子）。
- `replan_guard.go`：`ReplanGuard` 进程级重规划护栏与 `ReplanMetrics`。
- `input_adapter.go`：`NormalizePlanLoadArguments` 规范化/兼容归一化。
- `factory_types.go`：`SeelexNodeInput`/`NodeBudgetInput` 节点负载、
  `CanonicalPlanDocument`、product/approval 节点实现。
- `branch_types.go`：`PlanBranchBinding`/`PlanBranchEvent` 分支绑定类型。
- `authority.go`：`AuthorizePlanMutation` 变更授权钩子（当前放行）。

根包 `plan_aliases.go` 全量重导出保持公共 API 兼容；Runtime 保留 plan 门面
（`PrepareReplan`/`currentPlanPolicy`/`SetPlanPolicy`/...）+ `buildNode`/
`nodeFactory`（SeelexAgentNode 依赖 Runtime 节点作用域服务）。

## 验证

```text
go test ./seelebridge/plan -count=1
```
