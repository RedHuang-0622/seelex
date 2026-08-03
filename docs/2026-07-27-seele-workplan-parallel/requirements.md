# Seele WorkPlan Fork 并行需求文档

> 状态：Draft，待确认后进入实现
> 范围：Seele v0.0.5 WorkPlan 的 fork/fan-out/fan-in 并行执行
> 前提：并行分支使用 Go `sync.WaitGroup` 收敛；WaitGroup 不承担上下文管理职责。

## 1. 已确认的 v0.0.5 事实

GOPATH 中当前依赖为：

```text
github.com/RedHuang-0622/Seele v0.0.5
C:\Users\Administrator\go\pkg\mod\github.com\!red!huang-0622\!seele@v0.0.5
```

当前有两条不同的并行路径：

| 路径 | 入口 | 并发控制 | 上下文 | Seelex `plan_load` 是否使用 |
|------|------|----------|--------|:---:|
| 显式 ForkNode | `WorkPlan.Fork(...)` | `WaitGroup` + 局部 semaphore；默认 3 | 读取同一个 `WorkflowContext` | 否 |
| Scheduler 自动 fork | 节点有多条无条件出边 | 只有 `WaitGroup`，无 semaphore | 把同一个 `*WorkflowContext` 传给各分支 | 是 |

源码位置：

- 显式 ForkNode：`Seele/workplan/sugar/fork/fork.go:39-113`。
- 自动 fork：`Seele/workplan/runtime/scheduler/scheduler.go:112-209`。
- 多出边识别：`Seele/workplan/runtime/graph/graph.go:123-142`。
- `plan_load` 只创建 auto/manual 普通节点：`Seele/agent/core/tool/builtin/workplan_handler.go:77-105`。

因此本需求的目标不是新增 Work Queue，而是把两条路径统一为：

```text
ParentSnapshot
  → 为每个分支 clone BranchContext
  → goroutine + WaitGroup 执行和收敛
  → semaphore 负责并发上限
  → BranchResult 显式汇合
  → Join 继续 DAG
```

## 2. 问题定义

WaitGroup 只保证“已启动的 goroutine 都执行完 `Done()` 后，`Wait()` 才返回”。它不会：

- 复制或隔离 `WorkflowContext`。
- 决定哪个分支的输出写回父上下文。
- 传播取消或错误。
- 限制并发数。
- 决定 join 是否允许继续。

当前 Scheduler 自动 fork 把同一个 `*WorkflowContext` 传给所有分支；auto 节点会写 `wc.PrevOutput`：

- `Seele/workplan/runtime/scheduler/scheduler.go:124-145`
- `Seele/workplan/sugar/auto/auto.go:27-35`

所以并行 auto 节点会竞争修改 `PrevOutput`。这既是 data race，也是上下文语义不确定。

## 3. 目标

- REQ-001：每个分支运行时拥有独立的可变 `BranchContext`。
- REQ-002：所有分支只读同一份 fork 时刻的 `ParentSnapshot`。
- REQ-003：WaitGroup 只用于等待分支收敛；上下文复制与汇合由 ContextManager 完成。
- REQ-004：每个 fork 支持有限并发；限流由 semaphore/limiter 完成，不由 WaitGroup 完成。
- REQ-005：分支结果按稳定 branch/node ID 收集，不依赖 map 遍历或完成顺序。
- REQ-006：默认 fail-fast；任一分支失败后不能把整体 Plan 标成 completed。
- REQ-007：支持显式 best-effort，但它必须是 ForkPolicy，不是隐式行为。
- REQ-008：节点开始、完成、失败、取消必须可观察。
- REQ-009：线性 WorkPlan 和现有 `plan_load` JSON 保持兼容。
- REQ-010：Seelex 可以注入 role/account/session 绑定，但 Seele 不解析 Seelex 配置文件。

## 4. 上下文边界

### 4.1 ParentSnapshot：可共享，只读

fork 时冻结一次：

- Plan ID、fork ID、父 node ID、session ID、workspace ID。
- fork 前的 `PrevOutput`。
- fork 前的 `PrevResults`。
- fork 前的 `Vars`、`Metadata`。
- 已经解析好的 role、account、provider、trace ID。

map 必须深拷贝或使用只读包装；不能让后续分支写回这份 snapshot。

### 4.2 BranchContext：必须独占

每个分支独立持有：

- 自己的 `PrevOutput`。
- 自己的 `PrevResults`、`Vars`、`Metadata` map。
- 自己的 `WorkPlanResult/NodeResult` 收集区。
- 自己的 retry counter、deadline、cancel context。
- 自己的 Agent conversation/tool state。

### 4.3 Join：唯一允许写回父执行流的地方

分支不能直接修改父 `WorkflowContext`。WaitGroup 返回后，Join 统一执行：

```text
BranchResult[]
  → 按稳定 branch ID 排序
  → 验证 ForkPolicy
  → 构造 PrevResults[branchID]
  → 构造 aggregate PrevOutput
  → 写入父 WorkflowContext
```

后继节点读取：

```text
{{.PrevResults.backend}}
{{.PrevResults.tests}}
```

而不是读取“最后一个 goroutine 写进去的 PrevOutput”。

## 5. WaitGroup 使用要求

### 5.1 生命周期

```text
创建 ParentSnapshot 和 branch contexts
  → 对每个分支先 wg.Add(1)
  → 启动 goroutine
  → goroutine defer wg.Done()
  → 父协程 wg.Wait()
  → 所有 BranchResult 收敛后再 Join
```

### 5.2 强制规则

- `Add(1)` 必须发生在启动 goroutine 前。
- 每个 goroutine 的第一层 defer 必须调用 `Done()`。
- panic recovery 必须在 goroutine 内，并在 recovery 后仍保证 `Done()`。
- `Wait()` 期间父上下文不得被分支直接修改。
- 一个 ForkExecution 完成前，不复用同一个 WaitGroup 开始下一批分支。
- WaitGroup 不作为锁，不用于保护 map 或输出写入。

### 5.3 并发上限

WaitGroup 不限流。每个 fork 仍需：

```go
sem := make(chan struct{}, maxConcurrent)
```

分支 goroutine 通过 `select { case sem <- struct{}{}: ...; case <-ctx.Done(): ... }` 获取 permit；退出时释放 permit。

## 6. ForkPolicy

```go
type ForkPolicy struct {
    MaxConcurrent int
    FailureMode   FailureMode // fail_fast | best_effort
    JoinMode      JoinMode    // all | quorum（后续）
}
```

默认：

```text
MaxConcurrent = 有限框架默认值
FailureMode   = fail_fast
JoinMode      = all
```

### 6.1 fail-fast

```text
任一分支失败
  → sync.Once 记录首个错误
  → cancel fork child context
  → 已启动分支尽快退出
  → WaitGroup 等待全部 Done
  → 不执行 Join
  → WorkPlan failed
```

### 6.2 best-effort

```text
所有分支完成
  → WaitGroup 返回
  → Join 检查哪些结果存在
  → 明确决定继续或 failed
```

不允许当前“部分分支失败，但不标记 policy 就默认返回成功”的行为。

## 7. DAG 与 Join 需求

- Join 必须等待它的全部依赖分支，而不是只比较各分支的直接 next node。
- 支持 `A → A2 → join` 与 `B → B2 → join` 这类多级分支。
- 未满足前置依赖时，join node 不能入场。
- 并行分支的完成顺序不能改变 join 输入顺序。
- 当前 `scheduler.fork` 的“共同直接后继，否则提前结束”只可作为过渡行为，不能成为目标语义。

## 8. role/account/session 要求

- Seelex 在 fork 前绑定 role/account/provider，形成 branch runtime metadata。
- Seele 仅接收已绑定的 AgentFactory/runtime，不读取 roles/accounts YAML。
- branch ID 必须进入节点事件、审批 ID、trace 和测试断言。
- account/provider 并发额度由注入的 limiter 处理；WaitGroup 不承担资源分配职责。
- 若一个分支可写 workspace，必须使用独立 workspace revision 或显式锁策略。

## 9. 验收标准

| 验收项 | 标准 |
|--------|------|
| 上下文隔离 | 两个分支同时写本地 PrevOutput/Vars，彼此输入不串扰 |
| WaitGroup 收敛 | 任一分支 panic/error/cancel 后 `Wait()` 仍返回，无泄漏 |
| 并发上限 | `maxConcurrent=2` 时第三分支不能进入 Agent.Chat |
| fail-fast | 一分支失败，join 不执行，整体 failed |
| best-effort | 仅显式策略下可产生部分结果 |
| 稳定合并 | 分支完成顺序变化时 JoinOutput 不变化 |
| 多级 DAG | 两条多级分支在全部依赖完成后才 join |
| race | `go test -race` 覆盖真实 fork，无 data race |
| Seelex | PlanState/TUI/GUI 可显示 branch 的 queued/running/failed/completed |

## 10. 开放决策

1. 默认 `MaxConcurrent` 是按 fork、按 WorkPlan，还是同时两层？
2. best-effort 是否第一期支持，还是先只交付 fail-fast？
3. 多级 join 用 dependency counter，还是引入显式 JoinNode？
4. branch retry 是只重跑失败分支，还是重跑整个 fork？
5. workspace 写操作是否允许进入并行分支？
6. role/account 在 fork 创建时固定，还是在分支开始前重新租约？
