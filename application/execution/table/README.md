# application/execution/table

## 生态位

`application/execution/table` 拥有 WorkTable runtime：Plan/子代理源同步、task
注册表投影、`worktable.changed` latest-wins 发布、生命周期消费者、Todo 刷新和请求
尾部 TraceBlock。主要调用方是 `application/assembly` 的装配与回合链路；行投影判据仍由
`kernel/worktable` 唯一拥有。

## 职责

- 包装 `kernel/worktable.WorkTablePublisher`；
- 维护上一次成功下发子代理树的签名；
- 在共享视图锁内重建 WorkTable，并在锁外发布不可变行/批次；
- 同步 Plan/子代理生命周期到各会话 task scope；
- 启动、停止并守护 subagent/task/async/teamwork 消费者；
- 将表格行、批次头和变化后的子代理树组装成 `model.WorkTableEvent`；
- 在关闭时排空并发布尾态；
- 在关闭后保持 `Send` 安全返回。
- 从不可变任务/后台执行/teammate 输入生成有界请求尾部 TraceBlock。
- 消费 async/teamwork 信号并执行 refresh、completion trigger 与 pending 补扫。
- 统一启动/停止 subagent、task 与 async/teamwork 三类 consumer。
- 对 WorkItem 行、批次与单 task 依赖补齐做统一委托。
- 以 identity alias/一行转发暴露 core 仍需的 Limits、WorkTableUpdate、clone、
  merge、build、status 与 evidence 截断入口。

## 非职责

- 不构建 WorkItem 行、排序、批次或证据截断；这些归 `kernel/worktable`；
- 不拥有 Agent 执行、持久化或 SessionUnit；Runtime、Engine、Task 与视图锁由窄
  `RuntimeDeps` 提供；
- 不拥有事件 Hub、revision 或 session ID，只经注入回调发布；
- 不 import `application/assembly`，不持完整 `Service`。

## 核心契约

```go
type Deps struct {
    SubAgentTree func() []dto.SubAgentTreeNode
    Publish      func(worktable.WorkTableUpdate, model.WorkTableEvent)
}

type RuntimeDeps struct {
    Core         *kernelstate.Core
    Tasks        *taskcontext.Coordinator
    View         *viewstate.Coordinator
    Limits       func() Limits
    // session/task 读面、发布、故障守护与 async trigger 回调
}

type TraceInputs struct {
    SessionID, ViewSessionID string
    Tasks                    []dto.TaskRecord
    AsyncRuns                []dto.AsyncRunRecord
    TeamworkRuns             []dto.TeamworkJobCompletionRecord
    Limits                   worktable.Limits
}

type AsyncConsumerDeps struct {
    AsyncEvents, TeamworkEvents, Stop <-chan struct{}
    Refresh       func()
    TriggerAsync  func(map[string]struct{}) int
    TriggerTeamwork func(map[string]struct{}) int
    SafeCall      func(func())
    RetryDelay    time.Duration
}
```

`SubAgentTree` 与 `Publish` 在 publisher goroutine 中调用。宿主负责在回调内获取
`ViewMu` 读锁、克隆树并取得当前 session ID；该包不直接触碰 core 锁。

文件结构：

- `application.go`：publisher、latest-wins 与 payload 去重；
- `runtime.go`：WorkTable 源同步、投影、发布、生命周期与 Todo 刷新；
- `trace.go`：TraceBlock 输入投影；
- `consumer.go`：async/teamwork consumer 与补扫；
- `lifecycle.go`：三路 consumer 的启动、停止与 start-once；
- `projection.go`：行/批次输入与单 task 依赖补齐；
- `facade.go`：Middle worktable 类型 alias 与纯函数一行转发；
- `*_test.go`：各切片的包内 parity。

## 数据流

1. `Runtime.RefreshLocked` 在 core 的视图锁内重建 WorkTable；
2. publisher 汇聚突发更新，只保留最新一份；
3. publisher 读取当前子代理树签名；
4. 签名变化时附带树快照；签名不变时省略树字段；
5. 宿主发布 `worktable.changed`；
6. core 关闭时调用 `Runtime.Close` / `StopLifecycleConsumers`，发布尾态并停止 goroutine。
7. `BuildTraceBlock` 只接收宿主已取好的不可变输入，按会话过滤终态任务并限制整块行数。
8. `AsyncConsumer` 在单个 goroutine 中等待 async/teamwork 信号，处理 pending 补扫；
   `Stop` 关闭时停止 timer 并返回。

## 并发与错误语义

publisher 内部是单 goroutine；`lastTreeSig` 只在该 goroutine 读写，无需额外锁。
`Send` 保留 middle 层的阻塞/关闭语义；`Close` 幂等由底层 publisher 保证。

## 扩展与 Review

- 行投影或异步打点规则必须扩展 `kernel/worktable`，不得复制到本包；
- 新增 payload 字段时所有真实前端 reducer 必须同步验证；
- Review 时检查宿主回调是否在锁外做慢 I/O、树签名是否只比较内容、
  关闭后 `Send` 是否仍安全。

## 测试与验证

```text
go test ./application/execution/table -count=1
go test ./application/assembly -run 'WorkTable|RefreshWorkTable|Todo|Shutdown|Fault' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### application.go

- `func New(deps Deps) *Application` — New constructs the WorkTable publishing application.
- `func (application *Application) Send(update worktable.WorkTableUpdate)` — Send submits one table update to the latest-wins coalescer.
- `func (application *Application) Close()` — Close publishes the latest tail and stops the publisher goroutine.
- `func (application *Application) publish(update worktable.WorkTableUpdate)`

### application_test.go

- `func TestApplicationCoalescesAndOnlyResendsChangedTree(t *testing.T)`
- `func TestApplicationCloseDrainsTailAndSendAfterCloseIsSafe(t *testing.T)`
- `func receivePayload(t *testing.T, published <-chan model.WorkTableEvent) model.WorkTableEvent`

### consumer.go

- `func NewAsyncConsumer(deps AsyncConsumerDeps) *AsyncConsumer` — NewAsyncConsumer constructs one lifecycle consumer.
- `func (consumer *AsyncConsumer) Run()` — Run blocks until both signal sources are absent or Stop closes.

### consumer_test.go

- `func TestAsyncConsumerDispatchesSignalsAndStops(t *testing.T)`
- `func TestAsyncConsumerRetriesPendingCompletions(t *testing.T)`
- `func TestAsyncConsumerWithoutSignalsReturns(t *testing.T)`

### lifecycle.go

- `func NewLifecycle(deps LifecycleDeps) *Lifecycle` — NewLifecycle constructs the WorkTable lifecycle consumers.
- `func (lifecycle *Lifecycle) Start()` — Start launches each configured consumer once.
- `func (lifecycle *Lifecycle) consumeSubagentLifecycle()`
- `func (lifecycle *Lifecycle) consumeTaskChanges()`
- `func (lifecycle *Lifecycle) safeCall(call func())`

### lifecycle_test.go

- `func TestLifecycleDispatchesSubagentAndTaskSignals(t *testing.T)`
- `func TestLifecycleWithoutSignalsDoesNotStartWorkers(t *testing.T)`

### projection.go

- `func BuildRows(input RowInputs) ([]model.WorkItem, []model.WorkTableBatch)` — BuildRows delegates to the unique row/batch projection implementation.
- `func WorkItemForRecord(record dto.TaskRecord, plan *model.PlanState, limits worktable.Limits) model.WorkItem` — WorkItemForRecord adds Plan adjacency dependencies to one task-change row.

### projection_test.go

- `func TestBuildRowsDelegatesRowAndBatchProjection(t *testing.T)`
- `func TestWorkItemForRecordWithoutPlanLeavesDependencies(t *testing.T)`

### runtime.go

- `func NewRuntime(deps RuntimeDeps) *Runtime` — NewRuntime constructs the WorkTable runtime.
- `func (runtime *Runtime) Close()` — Close stops the latest-wins publisher after flushing its final update.
- `func (runtime *Runtime) RefreshLocked(tasks []dto.TaskRecord, asyncRuns []dto.AsyncRunRecord)` — RefreshLocked rebuilds the WorkTable projection while the caller holds the
- `func (runtime *Runtime) Publish(revision uint64, requestID string, items []protocol.WorkItem, batches []protocol.WorkTableBatch)` — Publish sends one immutable table snapshot to the latest-wins publisher.
- `func (runtime *Runtime) PublishTaskChanged(record dto.TaskRecord, revision uint64, requestID, sessionID string)` — PublishTaskChanged publishes one task.changed increment.
- `func (runtime *Runtime) PublishTaskDeltas()` — PublishTaskDeltas samples runtime facts outside the lock, rebuilds the
- `func (runtime *Runtime) SyncFromSources()` — SyncFromSources synchronizes the active session's Plan and subagent tree.
- `func (runtime *Runtime) SyncFromSourcesFor(sessionID string)` — SyncFromSourcesFor projects one session's Plan and owned subagent nodes into
- `func (runtime *Runtime) SessionActivePlanLocked(sessionID string) *protocol.PlanState` — SessionActivePlanLocked returns the active or background session's Plan
- `func (runtime *Runtime) syncPlanNodeTask(sessionID string, node protocol.PlanNode, parentID string)`
- `func (runtime *Runtime) syncSubagentTask(sessionID string, node dto.SubAgentTreeNode, parentID string)`
- `func (runtime *Runtime) WorkItemForRecord(record dto.TaskRecord, sessionID string) protocol.WorkItem` — WorkItemForRecord maps one task record and fills Plan adjacency dependencies.
- `func (runtime *Runtime) TraceBlock() string` — TraceBlock returns the active session's provider-only WorkTable trace block.
- `func (runtime *Runtime) TraceBlockFor(sessionID string) string` — TraceBlockFor returns one session's unfinished task and async execution
- `func (runtime *Runtime) RefreshSubagents()` — RefreshSubagents updates the tree snapshot and synchronizes terminal tool
- `func (runtime *Runtime) RefreshFromSources()` — RefreshFromSources synchronizes Plan/subagent sources and publishes the
- `func (runtime *Runtime) StartLifecycleConsumers()` — StartLifecycleConsumers starts the WorkTable signal consumers once.
- `func (runtime *Runtime) StopLifecycleConsumers()` — StopLifecycleConsumers closes the lifecycle signals once.
- `func (runtime *Runtime) SafeLifecycleCall(call func())` — SafeLifecycleCall routes consumer panics to the lifecycle fault guard.
- `func (runtime *Runtime) RefreshAfterTodoChange()` — RefreshAfterTodoChange reruns the runtime projection and publishes runtime,
- `func (runtime *Runtime) AsyncRuns() []dto.AsyncRunRecord` — AsyncRuns returns the host async execution projection.
- `func (runtime *Runtime) limits() worktable.Limits`
- `func (runtime *Runtime) publishSessionEvent(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) event.Event`

### trace.go

- `func BuildTraceBlock(input TraceInputs) string` — BuildTraceBlock renders the session-scoped request-tail work table.

### trace_test.go

- `func TestBuildTraceBlockFiltersAndBounds(t *testing.T)`
- `func TestBuildTraceBlockEmpty(t *testing.T)`
