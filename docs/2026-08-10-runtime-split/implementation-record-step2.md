# Runtime 拆分 Step 2 实施记录：planExecutor 组件

> 日期：2026-08-11；分支：`refactor/plan-executor`；目标设计：
> `docs/arch/plan-executor.md`。

## 目标

把散落在 `Runtime` 上的 plan 执行域状态与生命周期收进独立组件
`planExecutor`（`seelebridge/plan_executor.go`），Runtime 只保留公开方法
委托；`application/contract/ports.go` 保持不变；测试全绿后做真实 API 冒烟
并构建 dev 产物。

## 波及文件

### 新增

- `seelebridge/plan_executor.go`：组件状态（policy/binding/runID/events/
  nodeEvents/replan/agentFactory/approvalGate/eventError）+ deps 注入 + 编排
  方法（SetPolicy/Policy、SetBinding/Binding、SetApprovalGate、SetAgentFactory、
  AppendPhase、runPlan、PrepareReplan/preparePlan/completePlanPreflight、
  planLoadDefinition、planPreflightClient、resolvePreflightAccountSpec、
  buildNode/nodeFactory 除外——见下）。
- `seelebridge/plan_executor_test.go`：组件级单测（policy/binding 切换、run
  生命周期与 runID 置位/复位、事件投影并发、persister/error handler 注入）。

### 迁移

- `runtime.go`：删除 14 个 plan 字段（planPolicyMu/planPolicy、planRunMu/
  currentPlanRunID、planProvider、planEvents、planNodeEvents、
  eventErrorHandler、replanGuard、agentFactoryMu/agentFactory、
  approvalGateMu/approvalGate、branchBinding），新增 `planExecutor` 字段；
  `NewRuntime` 在字面量后构造组件（deps 闭包引用 r/cfg），worktreeManager
  的审批门改为 `r.planExecutor.currentApprovalGate`；公开方法全部改为委托。
- `plan_tool_provider.go`：`planToolProvider.runtime *Runtime` →
  `executor *planExecutor`；`runPlan` receiver 改为 `*planExecutor`（内部
  引用全部改为 executor 字段/deps）。
- `plan_preflight.go`：`PrepareReplan`/`preparePlan`/`completePlanPreflight`/
  `planLoadDefinition`/`planPreflightClient`/`resolvePreflightAccountSpec`
  receiver 改为 `*planExecutor`；`Runtime.PrepareReplan` 保留公开委托。
- `plan_authority.go`：`authorizePlanMutation` receiver 改为 `*planExecutor`。
- `branch.go`：删除 `setPlanBranchBinding`，binding 状态归组件；
  `currentPlanBranchBinding` 改为组件委托（默认值填充留在
  `Runtime.SetPlanBranchBinding`）。
- `agent_node.go`：`appendNodePhase` 改为 `r.planExecutor.AppendPhase(...)`。
- `fork_tool.go`：`r.runPlan(...)` → `r.planExecutor.runPlan(...)`。
- `plan_kernel_test.go` / `runtime_test.go`：`runtime.planProvider.*` →
  `runtime.planExecutor.provider.*`。

### 保留在 Runtime（有意为之）

- `buildNode`/`nodeFactory`（`plan_factory.go`）：`SeelexAgentNode` 依赖
  Runtime 的节点作用域、子代理上下文、worktree 等服务，迁入组件会引入反向
  依赖；节点工厂经 `planExecutorDeps.nodeFactory` 闭包提供给 provider。
- `branch.go` 的节点会话装配 helper（`nodeSessionComponents`/
  `nodeSessionID`）：仅在 `NewRuntime` 装配 agent factory 时使用，无运行期
  状态。
- 账号选择状态（`selectedAccountID`/`providerFilter` + `branchMu`）：属于
  账号池面，不属于 plan 域。

## 并发模型

policy/binding/run/agentFactory/approval/eventError 各自 RW 锁，锁域按职责
拆分；事件面保持 CSP（`planEventSink` 内部锁 + `nodeEvents` channel，
application 消费者串行）；`runPlan` 期间 `currentRunID` 由 `runMu` 保护。
组件不持有 `*Runtime`，deps 闭包注入账号列表、plan_load 定义、工具分发与
节点工厂。

## 验收

```text
go build ./...
go vet ./seelebridge/ ./application/...
go test ./seelebridge/ -skip '^Test(Worktree|ResolveNodePath|GlobSkipsHeavyDirs|ProjectScopeResolvesOnlyInsideBoundRoot|RuntimeProjectScopedToolsUseBoundProject|ScopedBashPublishesDiagnosticStages|BashDiagnosticObserverPanicDoesNotBreakTool)' -count=1
go test -race ./seelebridge/ -run 'TestPlan|TestReplan|TestForkSubagents|TestMergeBack|TestPlanExecutor' -count=1
go test ./application/... -count=1
```

真实 API 冒烟（`TestForkSubagentsLiveSmoke`）与 dev 构建见仓库工作区执行
记录，不在本文重复。
