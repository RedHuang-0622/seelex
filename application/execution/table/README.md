# application/execution/table

## 生态位

`application/execution/table` 拥有 WorkTable `worktable.changed` 的 publisher 应用状态：
latest-wins 汇聚、子代理树 payload 去重和关闭尾态。主要调用方是 `application/core`
的 WorkTable 投影入口；行投影判据仍由 `middle/view/worktable` 唯一拥有。

## 职责

- 包装 `middle/view/worktable.WorkTablePublisher`；
- 维护上一次成功下发子代理树的签名；
- 将表格行、批次头和变化后的子代理树组装成 `model.WorkTableEvent`；
- 在关闭时排空并发布尾态；
- 在关闭后保持 `Send` 安全返回。
- 从不可变任务/后台执行/teammate 输入生成有界请求尾部 TraceBlock。
- 消费 async/teamwork 信号并执行 refresh、completion trigger 与 pending 补扫。
- 统一启动/停止 subagent、task 与 async/teamwork 三类 consumer。
- 对 WorkItem 行、批次与单 task 依赖补齐做统一委托。

## 非职责

- 不构建 WorkItem 行、排序、批次或证据截断；这些归 `middle/view/worktable`；
- 不读取 Runtime、Task、Plan 或 Queue；树快照由宿主回调提供；
- 不拥有事件 Hub、revision 或 session ID；
- 不 import `application/core`，不持完整 `Service`。

## 核心契约

```go
type Deps struct {
    SubAgentTree func() []dto.SubAgentTreeNode
    Publish      func(worktable.WorkTableUpdate, model.WorkTableEvent)
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
- `trace.go`：TraceBlock 输入投影；
- `consumer.go`：async/teamwork consumer 与补扫；
- `lifecycle.go`：三路 consumer 的启动、停止与 start-once；
- `projection.go`：行/批次输入与单 task 依赖补齐；
- `*_test.go`：各切片的包内 parity。

## 数据流

1. core 锁内提交 `Send(WorkTableUpdate)`；
2. publisher 汇聚突发更新，只保留最新一份；
3. publisher 读取当前子代理树签名；
4. 签名变化时附带树快照；签名不变时省略树字段；
5. 宿主发布 `worktable.changed`；
6. core 关闭时调用 `Close`，发布尾态并停止 goroutine。
7. `BuildTraceBlock` 只接收宿主已取好的不可变输入，按会话过滤终态任务并限制整块行数。
8. `AsyncConsumer` 在单个 goroutine 中等待 async/teamwork 信号，处理 pending 补扫；
   `Stop` 关闭时停止 timer 并返回。

## 并发与错误语义

publisher 内部是单 goroutine；`lastTreeSig` 只在该 goroutine 读写，无需额外锁。
`Send` 保留 middle 层的阻塞/关闭语义；`Close` 幂等由底层 publisher 保证。

## 扩展与 Review

- 行投影或异步打点规则必须扩展 `middle/view/worktable`，不得复制到本包；
- 新增 payload 字段时所有真实前端 reducer 必须同步验证；
- Review 时检查宿主回调是否在锁外做慢 I/O、树签名是否只比较内容、
  关闭后 `Send` 是否仍安全。

## 测试与验证

```text
go test ./application/execution/table -count=1
go test ./application/core -run 'WorkTable|RefreshWorkTable|Todo|Shutdown|Fault' -count=1
```
