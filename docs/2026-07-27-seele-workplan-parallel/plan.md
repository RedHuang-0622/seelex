# Seele WorkPlan Fork 并行设计方案

> 本文对应 `requirements.md`。核心是 `sync.WaitGroup` 负责分支收敛，ContextManager 负责上下文隔离，semaphore/limiter 负责并发上限。

## 1. 设计目标

把当前的“多出边直接 goroutine + 共享 WorkflowContext”改为：

```text
Scheduler / ForkNode
  → ForkCoordinator 创建 ParentSnapshot
  → ContextManager 为每个分支 clone BranchContext
  → goroutine 执行分支
       ├─ WaitGroup：等待全部结束
       ├─ semaphore：限制并发
       └─ child context：取消传播
  → ResultCollector 收集稳定 BranchResult[]
  → JoinPolicy 合并后写回父上下文
```

### 设计模式选择

| 模式 | Go 实现 | 应用位置 | 理由 |
|------|---------|----------|------|
| Strategy | `JoinPolicy` / `FailurePolicy` interface | fork 成功与失败语义 | 避免把策略写死在 Scheduler |
| Factory Method | `NewBranchContext(snapshot, meta)` | 分支上下文 | 保证深拷贝和元数据完整 |
| Adapter | `ForkCoordinator` 适配 Scheduler/ForkNode | 两条现有 fork 路径 | 避免两套并行语义继续漂移 |
| Decorator | provider/account limiter | Agent 执行前 | 限流不污染 graph 与 context |

## 2. 当前源码事实与改造入口

### 2.1 显式 ForkNode

`WorkPlan.Fork` 创建 `ForkNode`：

- `Seele/workplan/workplan.go:146-152`
- `Seele/workplan/sugar/fork/fork.go:16-113`

现状：

```text
for branch
  → wg.Add(1)
  → goroutine
  → sem 获取 permit
  → RenderTemplate(branch.Input, ec)
  → Agent.Chat
  → results[i] = result
  → wg.Done()
wg.Wait()
```

它已有 WaitGroup 与局部 semaphore，但没有 BranchContext；部分失败只要不是全失败仍会返回合并 JSON。

### 2.2 Scheduler 自动 fork

- `Seele/workplan/runtime/graph/graph.go:123-142`
- `Seele/workplan/runtime/scheduler/scheduler.go:112-209`

现状：

```text
多条无条件出边
  → wg.Add(1)
  → goroutine
  → executor.RunNode(ctx, node, 同一个 wc)
  → results[i] = result
  → wg.Done()
wg.Wait()
```

它没有 semaphore，且 auto node 会写共享 `wc.PrevOutput`：`Seele/workplan/sugar/auto/auto.go:27-35`。

### 2.3 Seelex `plan_load`

`plan_load` 只将 JSON 转成 auto/manual nodes 和普通 edges：

- `Seele/agent/core/tool/builtin/workplan_handler.go:40-43`
- `Seele/agent/core/tool/builtin/workplan_handler.go:77-105`

所以 Seelex 的多出边 Plan 一定走 Scheduler 自动 fork，而不是 `ForkNode`。改造必须优先覆盖 Scheduler 路径。

## 3. 方案比较

| 维度 | 方案 A：BranchContext + WaitGroup + Join（推荐） | 方案 B：共享 WorkflowContext + Mutex |
|------|------|------|
| data race | 无共享可变上下文 | 可消除 race，但语义仍不确定 |
| 输入语义 | 每个分支固定读取 ParentSnapshot | 谁最后写 PrevOutput 会影响后续 |
| 并发 | WaitGroup 收敛，semaphore 限流 | 锁竞争可能把并行重新串行化 |
| 错误处理 | fail-fast/best-effort 可显式实现 | 错误与共享状态难回滚 |
| nested fork | 可递归建立 snapshot | 锁与状态所有权复杂 |
| 测试性 | 可单测 clone、join、coordinator | 时序耦合强 |

选择方案 A。WaitGroup 是这个方案的同步原语，但不是 ContextManager 的替代品。

## 4. 核心数据模型

### 4.1 ParentSnapshot

```go
type ParentSnapshot struct {
    PlanID       string
    ForkID       string
    ParentNodeID string
    SessionID    string
    WorkspaceID  string
    PrevOutput   string
    PrevResults  map[string]string
    Vars         map[string]string
    Metadata     map[string]any
    Runtime      BranchRuntime
}
```

创建规则：`PrevResults`、`Vars`、`Metadata` 都做深拷贝；之后只读。

### 4.2 BranchContext

```go
type BranchContext struct {
    Snapshot    ParentSnapshot
    BranchID    string
    NodeID      string
    PrevOutput  string
    PrevResults map[string]string
    Vars        map[string]string
    Metadata    map[string]any
    Result      *types.WorkPlanResult
}
```

每个分支都持有独立的 map 和 Result。它可以在 branch 内自由修改，但绝不能把指针写回 parent。

### 4.3 BranchResult

```go
type BranchResult struct {
    Index     int
    BranchID  string
    NodeID    string
    Status    string // completed / failed / canceled / panicked
    Output    string
    Context   *BranchContext
    Err       error
    StartedAt time.Time
    EndedAt   time.Time
}
```

`Index` 来自 fork 输入顺序，保证结果顺序稳定；不能以 goroutine 完成顺序或 map 遍历顺序作为 join 顺序。

## 5. ForkCoordinator 接口

接口由 Scheduler/ForkNode 调用方定义，建议位于 `workplan/runtime/forkexec`：

```go
type Coordinator interface {
    Run(ctx context.Context, parent *types.WorkflowContext, spec Spec) ([]BranchResult, error)
}

type Spec struct {
    ForkID        string
    Branches      []BranchSpec
    MaxConcurrent int
    FailureMode   FailureMode
}

type BranchSpec struct {
    BranchID string
    NodeID   string
    Execute  func(context.Context, *BranchContext) (string, error)
}
```

`ForkNode` 把 `ForkBranch` 适配为 BranchSpec；Scheduler 把每个 next node 适配为 BranchSpec。两者最终调用相同 Coordinator。

## 6. WaitGroup 驱动的算法

```go
func (c *forkCoordinator) Run(ctx context.Context, parent *types.WorkflowContext, spec Spec) ([]BranchResult, error) {
    snapshot := c.contexts.Snapshot(parent, spec.ForkID)
    forkCtx, cancel := context.WithCancelCause(ctx)
    defer cancel(nil)

    results := make([]BranchResult, len(spec.Branches))
    sem := make(chan struct{}, normalizeLimit(spec.MaxConcurrent))
    var wg sync.WaitGroup
    var firstErr error
    var failOnce sync.Once

    for i, branch := range spec.Branches {
        branchCtx := c.contexts.NewBranch(snapshot, branch)
        wg.Add(1)
        go func(i int, branch BranchSpec, branchCtx *BranchContext) {
            defer wg.Done()
            defer c.recoverPanic(&results[i], branch)

            select {
            case sem <- struct{}{}:
                defer func() { <-sem }()
            case <-forkCtx.Done():
                results[i] = canceledResult(i, branch, context.Cause(forkCtx))
                return
            }

            out, err := branch.Execute(forkCtx, branchCtx)
            results[i] = finishResult(i, branch, branchCtx, out, err)
            if err != nil && spec.FailureMode == FailFast {
                failOnce.Do(func() {
                    firstErr = err
                    cancel(err)
                })
            }
        }(i, branch, branchCtx)
    }

    wg.Wait()
    return c.join(parent, results, spec, firstErr)
}
```

关键点：

- `Add` 永远在 goroutine 前。
- `Done` 用第一层 defer 保证。
- semaphore 和 WaitGroup 职责分离。
- 取消通过 `forkCtx` 传播，不通过写共享 bool。
- 只有 `join` 能改 parent。

## 7. ContextManager

建议位于 `workplan/runtime/context`：

```go
type Manager interface {
    Snapshot(parent *types.WorkflowContext, forkID string) ParentSnapshot
    NewBranch(snapshot ParentSnapshot, branch BranchSpec) *BranchContext
    Join(parent *types.WorkflowContext, results []BranchResult, policy JoinPolicy) error
}
```

### 7.1 Snapshot

复制 fork 前的 `PrevOutput`、`PrevResults`、`Vars`、`Metadata`。不复制父 `WorkPlanResult` 指针。

### 7.2 NewBranch

从 snapshot 建立独占 map 和 result。auto node 在 branch 内写 `PrevOutput` 不会影响兄弟分支。

### 7.3 Join

Join 先验证 policy，再构造稳定输出：

```json
{
  "backend": {"status":"completed","output":"..."},
  "tests": {"status":"completed","output":"..."}
}
```

然后一次性写回 parent：

- `parent.PrevResults[branchID] = output`
- `parent.PrevOutput = aggregate JSON`
- `parent.Result.NodeResults` 以稳定顺序追加。

## 8. 错误和取消

### 默认：fail-fast

一个分支失败时：

```text
first error
  → cancel(forkCtx)
  → 其它未拿到 semaphore 的分支直接 canceled
  → 已运行分支收到 ctx.Done()
  → wg.Wait()
  → Join 返回 error
  → Scheduler 不执行后继节点
```

### best-effort

只有显式 `FailureMode=BestEffort` 时：

```text
全部 branch Done
  → wg.Wait()
  → JoinPolicy 判断是否允许缺失 branch
  → 返回 partial result 或 error
```

`WorkPlanTool` 必须依据 Join/Scheduler 返回 error 输出 failed JSON，不能只因为 `Run` 返回了 NodeResults 就输出 completed。目标文件：`Seele/agent/core/tool/builtin/workplan_handler.go:125-161`。

## 9. DAG Join

当前 Scheduler 只要求直接共同后继。目标改为基于依赖计数：

```text
join 需要 backend、tests 两个前置
backend 完成 → completedDeps=1
tests 完成   → completedDeps=2
completedDeps == requiredDeps → 可执行 join
```

这样可支持：

```text
start → backend → backend-check ─┐
start → tests   → tests-check   ─┴→ review
```

不会因为两个分支的直接 next 不同就提前结束。

## 10. Seelex 接入

Seelex 在 fork 前提供只读运行元数据：

```go
type BranchRuntime struct {
    SessionID   string
    WorkspaceID string
    Role        string
    AccountID   string // 日志脱敏
    Provider    string
    TraceID     string
}
```

Seele 不解析账号池。Seelex 根据 roles/accounts 配置创建 branch-bound AgentFactory/provider，并把它注入框架。

事件至少含：queued、started、completed、failed、canceled、panicked。Seelex 将事件映射到 `PlanState` 与 TUI/GUI。现有入口：

- `application/chat.go:378-399`
- `application/state.go:71-109`

## 11. 实施步骤

| 阶段 | 改动 | 文件/模块 |
|------|------|-----------|
| M1 | ParentSnapshot、BranchContext、BranchResult、深拷贝测试 | `workplan/core/types` / `runtime/context` |
| M2 | ForkCoordinator（WaitGroup + semaphore + cancel） | 新 `workplan/runtime/forkexec` |
| M3 | Scheduler 自动 fork 改接 ForkCoordinator | `runtime/scheduler` |
| M4 | 显式 ForkNode 改接同一 Coordinator | `sugar/fork` |
| M5 | dependency counter / JoinPolicy | `runtime/scheduler` / graph runtime |
| M6 | Node lifecycle hook、handler status/lock 边界 | scheduler / builtin handler |
| M7 | Seele race、失败、取消和多级 DAG 测试 | `workplan/**/_test.go` |
| M8 | Seelex role/account、PlanState、TUI/GUI、E2E | seelebridge/application/tui/gui/e2e |

## 12. 测试策略

| 测试 | 证明 |
|------|------|
| `TestBranchContextIsolation` | 分支 PrevOutput/Vars 完全隔离 |
| `TestWaitGroupPanicSafety` | panic 后 Done 仍执行，Wait 不死锁 |
| `TestForkSemaphoreLimit` | 并发不超过 MaxConcurrent |
| `TestForkFailFast` | 首个错误取消兄弟并阻止 join |
| `TestForkBestEffort` | 显式策略下才允许部分结果 |
| `TestStableJoinResult` | 随机完成顺序不改变输出 |
| `TestNestedForkDependencyJoin` | 多级分支正确汇合 |
| `go test -race` | fork 路径无共享上下文 data race |
| Seelex `parallel-plan` E2E | role/account/PlanState/界面事件完整 |

## 13. 回滚

在 ForkCoordinator 稳定前，保留：

```text
parallel_mode = serial
```

开启时，多出边以稳定拓扑顺序串行执行。它牺牲并行度，但不会共享可变上下文。

## 14. 推荐结论

以 `sync.WaitGroup` 作为 fork 的“收敛屏障”，以 `context.WithCancelCause` 作为取消传播，以 semaphore/limiter 作为并发控制，以 ContextManager 作为上下文隔离，以 JoinPolicy 作为唯一写回父上下文的入口。

WaitGroup 解决的是“什么时候可以 Join”；它不解决“什么上下文可以共享”。
