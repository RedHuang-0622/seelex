# Plan Executor（plan 执行域组件）架构

> 状态：已实现（2026-08-11，实施记录见
> `docs/2026-08-10-runtime-split/implementation-record-step2.md`）。
> 模块：`seelebridge`；调用方：`Runtime` 门面（组合根）与 workplan 内核。

## 定位

`planExecutor` 是 Runtime 拆分 Step 2 的组件：把原先散落在 `Runtime` 上的
plan 执行域状态与生命周期收进一个独立组件，Runtime 只保留委托门面。它管理：
Plan 策略（policy）、分支绑定（branch binding）、执行事件（event sink +
节点事件 channel）、运行 ID、已加载 Plan（经 `planToolProvider`）、重规划
护栏（replan guard）、审批门与子代理工厂（agent factory）的装配。

它不是新的执行引擎：DAG 的实际执行仍委托 Seele `workplan` 内核
（`workplan.NewFromPlan` + scheduler/fork），组件只负责 Seelex 侧的编排、
事件投影与并发边界。这与 Step 1 的 `subagentSessions` / `worktreeManager`
同构——组件持有可变状态与锁，channel/actor 收编并发，Runtime 收窄为组合根。

## 职责与非职责

职责：

- 持有 `PlanPolicy` 与 `PlanBranchBinding`，提供读写门面
  （`Policy()` / `SetPolicy()` / `Binding()` / `SetBinding()`）；
- 持有 `planEventSink` 与 `PlanNodeEvent` 输出 channel，向 application 投影
  计划/节点状态（CSP：`planNodeEvents` 消费者串行处理）；
- 持有 `loadedPlanDoc`（plan_load 的规范化存储）与 `maxForkConcurrency`；
- 持有 `replanGuard`（重规划频率/并发护栏）与 `agentFactory`
  （plan 子代理工厂）；
- 提供 `Load` / `Run` / `Clear` / `Export` / `Status` / `Validate` /
  `PrepareReplan` 编排入口，并维护 `currentPlanRunID`（run 生命周期）；
- 提供 `EventPersister` / `EventErrorHandler` / 审批门注入点。

非职责：

- 不实现 DAG 执行算法（委托 Seele `workplan`）；
- 不管理子代理会话注册表 / worktree / 子代理树（Step 1 组件与
  `subagentTree` 职责，planExecutor 只经事件与 NodeScope 协作）；
- 不直接触达主会话 History（死锁边界不变：任何子代理 goroutine 不得
  反向访问主会话，消息只能经 mailbox/channel 出）。

## 目录/文件结构

```text
seelebridge/
  plan_executor.go        # 组件：状态、命令门面、Load/Run/Replan 编排（已实现）
  plan_events.go          # planEventSink（归属组件）
  plan_policy.go          # PlanPolicy 定义与校验（归属组件）
  plan_preflight.go       # PrepareReplan 隔离回合（归属组件；Runtime 保留公开委托）
  plan_authority.go       # authorizePlanMutation（归属组件）
  branch.go               # PlanBranchBinding 类型与节点会话装配；binding 状态归组件
  plan_tool_provider.go   # plan_load/plan_run 工具 provider（归属组件，持有 executor 引用）
  replan_guard.go         # 重规划护栏（归属组件）
  plan_executor_test.go   # 组件级单测（policy/run 生命周期/事件投影并发/persister）
```

## 核心实现

```go
type planExecutor struct {
    deps       planExecutorDeps // model/heartbeat/limits/accounts/loadPlanDefinition/dispatch/nodeFactory
    policyMu   sync.RWMutex
    policy     PlanPolicy
    bindingMu  sync.RWMutex
    binding    PlanBranchBinding
    runMu      sync.RWMutex
    currentRunID string

    provider   *planToolProvider // 持有 loadedPlanDoc
    events     *planEventSink    // 事件库 + 投影订阅
    nodeEvents chan PlanNodeEvent // CSP 输出
    replan     *replanGuard
    agentFactoryMu sync.RWMutex
    agentFactory   node.AgentFactory
    approvalMu     sync.RWMutex
    approvalGate   approve.ApprovalGate
    eventErrorMu   sync.RWMutex
    eventError     frameworkevent.ErrorHandler
}
```

并发模型：状态锁按职责拆分（policy/binding/run/agentFactory/approval/eventError
各自 RW 锁）；事件面保持现有 CSP（sink 内部锁 + `nodeEvents` channel，消费者
串行）；`runPlan` 期间 `currentRunID` 由 `runMu` 保护，与 Step 1 组件一致。
组件不持有 `*Runtime`：`accounts`/`loadPlanDefinition`/`dispatch`/`nodeFactory`
均以 deps 闭包注入（与 `worktreeManagerDeps` 同模式），provider 只持有
`*planExecutor` 引用。

## 数据流/生命周期

```text
application → Runtime.PrepareReplan → planExecutor.PrepareReplan
                                └ replanGuard.acquire → 隔离 preflight 回合
application → plan_load → planExecutor.provider（policy 校验 + codec 导入 + 存储）
模型 → plan_run → planExecutor.runPlan → workplan.NewFromPlan(agentFactory, events)
                        ├ 事件 → planEventSink → PlanNodeEvent → nodeEvents → application
                        └ 节点 → NodeScope（SessionID/WorkspaceID/PlanID）
```

## 依赖方向

- `planExecutor → Seele workplan / codec / event / types`（执行内核）；
- `planExecutor → seelexctx（limits）、internal/promptassets（preflight 模板）`；
- `Runtime → planExecutor`（组合根装配，Runtime 实现 `RuntimePort` 时转发）；
- 禁止：`planExecutor → application`；`planExecutor` 反向访问主会话；
  `workplan` 产品节点 → seelex 业务包（沿 `seele-v2-runtime-architecture.md`）。

## 扩展方式

- 新增 plan 工具族：在 `planToolProvider` 注册并保持 policy 校验；
- 调整重规划护栏：`replanGuard` 参数（并发/窗口/provider 请求预算）；
- 调整事件持久化：`SetEventPersister` 注入 sessionstore 事件库接线。

## Review 指南

- policy/binding/runID/agentFactory 是否各自锁域清晰、无跨域死锁；
- `runPlan` 期间 `currentRunID` 与事件相关性是否一致；
- `nodeEvents` 消费者是否唯一且可终止（无 goroutine 泄漏）；
- 审批门注入是否保持「未注入 → 放行」的框架兜底；
- 与 Step 1 组件（subagentSessions/worktreeManager）的调用链是否单向。

## 测试与验证

沿用现有 `plan_kernel_test.go` / `plan_test.go` / `plan_input_fuzz_test.go`，
并新增组件级单测 `plan_executor_test.go`（policy/binding 切换、run 生命周期与
runID 置位/复位、事件投影并发、persister/error handler 注入）。

```text
go test ./seelebridge/ -run "TestPlan|TestReplan|TestForkSubagents|TestPlanExecutor" -count=1
go test -race ./seelebridge/ -run "TestPlan|TestReplan|TestForkSubagents|TestMergeBack|TestPlanExecutor" -count=1
```
