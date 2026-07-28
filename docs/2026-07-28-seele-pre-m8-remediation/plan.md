# Seele M8 前置修复方案

## 目标

在启动 Seelex M8 前补齐 Seele 的三个阻断项：fork 的 `Prepare` panic
收敛、自动 fork 实际使用 branch-bound `AgentFactory`、以及
`WorkPlanTool` 对 Seelex 暴露 branch runtime/event 配置入口。

## 方案比较

| 维度 | A：执行资源覆盖（推荐） | B：写入 `WorkflowContext.Metadata` |
|---|---|---|
| factory 绑定 | Scheduler 显式传入 branch resource | Auto node 从通用 metadata 隐式取值 |
| 类型安全 | 强 | 弱，依赖 string key 和断言 |
| 并发隔离 | 每次执行独立、无共享节点写入 | 容易把运行时资源泄漏至模板/其他节点 |
| 可测性 | 可断言每个 branch 实际调用的 factory | 只能间接检查 metadata |
| 兼容性 | nil resource 时保留现有节点行为 | 会污染通用工作流上下文 |

选择 A。禁止用 goroutine-local 状态或修改共享 `StrategyNode.Factory`，两者都会重新引入并发串扰。

## 核心接口

在 `workplan/core/node` 定义执行方所需的窄接口与 DTO，避免该包依赖
`forkexec`：

```go
type ExecutionResources struct {
    AgentFactory AgentFactory
}

type ResourceAwareNode interface {
    RunWithResources(context.Context, *types.WorkflowContext, ExecutionResources) (string, error)
}
```

`executor.Executor` 新增 `RunNodeWithResources`：若节点实现
`ResourceAwareNode` 则调用它，否则回退到现有 `RunNode` 行为。Scheduler 的
`forkexec.Spec.Execute` 使用：

```go
resources := node.ExecutionResources{AgentFactory: branch.Runtime.AgentFactory}
return s.executor.RunNodeWithResources(ctx, n, branch.Workflow, resources)
```

`auto.StrategyNode` 和 `approve.ApproveNode` 实现该接口；只有 resource 中的
factory 非 nil 时才覆盖本次 `NewAgent` 调用。底层 graph node 保持不变，所以
兄弟分支不会互相改写 factory；没有 runtime 的旧计划完全兼容。

## panic 修复

在 `forkexec.Coordinator.Run` 的 goroutine 中，将 recover defer 置于
`Prepare` 前、紧随 `defer wg.Done()`。recover 负责写入 `panicked` Result、发送
事件并触发 fail-fast cancel；`Prepare`、限流和 Execute 均处于同一保护范围。

## WorkPlanTool 配置边界

为 `builtin.WorkPlanTool` 增加锁保护的公开 setter：

```go
SetBranchEventHook(func(forkexec.Event))
SetBranchRuntimeResolver(func(string) forkexec.BranchRuntime)
SetForkPolicy(forkexec.Policy)
SetMaxForkConcurrency(int)
```

`plan_load` 创建 `WorkPlan` 时把这些设置转换为 `workplan.Option`。Seele 不读
roles/accounts YAML；它只接收已经构造好的 runtime/factory/limiter。

## Seelex M8 接入

1. `seelebridge` 定义自己的 `PlanBranchEvent` DTO，并在 Runtime 中把 Seele
   `forkexec.Event` 适配为该 DTO；application 不直接 import Seele runtime 包。
2. Runtime 构造 branch-bound factory。它必须创建或租用分支私有 provider/client，
   不得调用共享 `r.client.SetProviderFilter` 来切换账号。账号租用失败返回一个
   会在 `Chat` 时给出明确错误的 factory/agent，交给 fork fail-fast 记录。
3. application 新增 `HandlePlanBranchEvent`，按 `NodeID` 更新 `PlanState`；扩展
   节点状态为 queued/running/completed/failed/canceled/panicked。
4. TUI/GUI 为 canceled、panicked 添加文案、图标和颜色；E2E 使用两条自动 fork
   分支验证不同 factory 标签、并行状态和失败状态。

## 实施顺序

| 阶段 | 文件/模块 | 验收 |
|---|---|---|
| S1 | Seele `forkexec` | `Prepare` panic 不崩溃，结果为 panicked，Wait 返回 |
| S2 | Seele `node` / `executor` / `auto` / `approve` / `scheduler` | 自动 fork 的每个分支只调用自己的 factory |
| S3 | Seele `builtin.WorkPlanTool` | `plan_load` 可接收 runtime/event/policy 配置 |
| S4 | Seelex `seelebridge` | role/account 解析生成 branch-bound factory，未改共享 client |
| S5 | Seelex `application` / `tui` / `gui` / `e2e` | 生命周期实时展示；两分支 E2E 通过 |

## 测试

- `TestPreparePanicIsRecovered`
- `TestAutomaticForkUsesInjectedFactoryPerBranch`
- `TestAutomaticForkFactoryDoesNotBleedToSibling`
- `TestPlanLoadForwardsBranchRuntimeAndEvents`
- Seelex `parallel-plan` E2E：两个分支账号标签不同，事件顺序包含 queued → started → terminal。
- 在 CGO 可用的 CI 执行 `go test -race ./workplan/...`。

## 回滚

`AgentFactory == nil` 时走原节点执行路径；M8 的 branch runtime resolver 未设置时，
行为与当前版本一致。出现账号隔离故障时，关闭 Seelex resolver 即可回退，不能回退到
共享 client 的动态 provider 切换。
