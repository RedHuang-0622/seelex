# backend/tool/jobs

## 生态位

`backend/tool/jobs` 是 Seelex 的**异步作业域**的只读面：把"在册作业出现了新状态"投影成会话级
事实事件（`EventStream`），并把后台执行表的探针读数投影给 Application 侧（`ProjectAsyncRuns`）。

作业的**状态不在本包**：teammate 作业活在 Seele 的 `jobs.Manager`（装配层
`backend/engine/assemble` 持有），后台命令执行表活在
[`backend/tool/registry`](../registry/README.md)（`Router.AsyncRuns()`）。本包只读、只搬运。

主要调用方：`backend/engine/assemble` 的 `SetTeamworkBackend`（装事件流并登记停机顺序）与
`Runtime.AsyncRunsSnapshot` / `Runtime.AsyncRunEvents`（薄转发）；经 `wiring/adapters` 与
`application/core` 到工作表格与请求尾部打点块。

物理位置：2026-10-17 按 [plan.md](../../../docs/2026-10-15-architecture-reshape/plan.md) §2.6
（`seelebridge` 的 runtime_async / jobs_events 两个文件 → `backend/tool/jobs/`）从
`backend/engine/assemble` 归位（P21-10「域方法随类型走」；迁移前后的对照见该文档的
[m5-record.md](../../../docs/2026-10-15-architecture-reshape/m5-record.md) §36）。

## 职责与非职责

**做**：

1. `EventStream`：订阅作业变化信号（**扇出后**的一条订阅）→ 每次被唤醒对在册作业做一次
   `Snapshot` → 只把**新出现的状态**投影成 `frameworkevent.Event`（补 `agent.runtime` 会话定位、
   序号取时间基）→ 经装配期注入的 persister 追加到该会话的事件库；
2. `ProjectAsyncRuns`：把 `registry.AsyncRunInfo` 的**每一列**搬进 `dto.AsyncRunRecord`（按派发顺序）。

**不做**：不派发、不取回、不终止作业（写侧全在 `backend/tool/registry` 与 Seele `jobs`）；
不推进任何游标、不销项（取回依旧是消费式的）；不决定"事件落到哪个会话的哪份文件"
（那是 `store` 的落盘口径）；不把作业结果投递进忙会话（事件是**观察**，不是唤醒）。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| `event_stream.go` | `EventStream` + `SnapshotReader` 窄端口 + `NewEventStream` / `Start` / `Close` / `loop` / `drain` / `append` / `eventStatus`；常量 `EventSource` / `MainAgentID` |
| `projection.go` | `ProjectAsyncRuns`（空表归一为 nil）+ `recordFrom`（逐列搬运） |
| `event_stream_test.go` | 缺装配语义（manager 为 nil ⇒ nil 流）、状态变化才落库（同状态不重复、无会话归属不落库）、状态映射表、起停幂等（含"从未 Start 也能 Close"） |
| `projection_test.go` | 逐列搬运（漏搬一列即红）、空表归一与顺序保持 |

## 核心实现

- `EventStream.drain`：**投影的是状态变化，不是逐帧回放**。句柄 → 上次状态的记忆表在
  `drain` 里更新；销项（快照里消失）的句柄从记忆里移除。句柄序号单调、永不复用
  （命名空间见 `dto.JobNamespace`），所以"重新出现同一个句柄"不会发生，移除是安全的。
- `EventStream.append`：**没有会话归属的作业一条都不落**（`record.Scope.Session` 为空即返回）——
  宁可丢弃，也不回填到别的会话。序号取 `uint64(at.UnixNano())`：本库按会话排序，框架 recorder 的
  进程级序号在这里不成立。
- `SnapshotReader`：端口只取投影真正需要的那一格（`Snapshot(Scope) []Record`），
  `seelejobs.Manager` 结构上满足它（编译期断言 `var _ SnapshotReader = (seelejobs.Manager)(nil)`），
  因此**不需要适配层**，测试也不需要伪造整个 `Manager`。
- `EventStream.Close`：没起过协程就直接返回（停机链上可能与"从未 Start"相遇，
  等一个永不关闭的 `done` 会把停机钉死）。

## 数据流

```text
Seele jobs.Manager ──Events()──► 装配层扇出（teamwork_job_signals.go，单接收者上游一次读取）
                                      ├─ 订阅 → EventStream.loop → drain → append → persister
                                      │                                        └─► store 事件库（按会话）
                                      └─ 订阅 → application 终态触发回合

backend/tool/registry.Router.AsyncRuns() ──► ProjectAsyncRuns ──► dto.AsyncRunRecord
                                                                  （工作表格 / 请求尾部打点块）
```

## 依赖方向

允许：标准库、Seele（`event` / `jobs`）、`backend/tool/registry`（只读探针读数类型）、`contract/dto`。
禁止：`backend/engine/assemble`（装配层反过来 import 本包）、`application/*`、`store`
（本包不落盘：落盘钩子由装配点以函数值注入）。

## 并发、存储与错误语义

- `EventStream` 只有一条投影协程（`Start` 起、`Close` 停）；`seen` 记忆表自持锁。
  `Close` 幂等（`sync.Once` 关停通道），等待投影协程退出后再返回。
- 不落盘：本包没有文件写入口，落盘经注入的 persister（可能为 nil = 宿主没接事件库，静默跳过）。
- 错误语义：追加事件失败**不改变控制流**（观察失败只记一行日志，不返回错误）；
  persister 取用器与 persister 本身为 nil 时静默跳过。
- 生命周期顺序（装配层登记）：停机时先停投影、再取消在途作业——不为停机合成一批 killed 事件。

## 扩展方式

- 新增投影列：先在 `registry.AsyncRunInfo` 与 `dto.AsyncRunRecord` 加字段，再在 `recordFrom` 加一行，
  并在 `projection_test.go` 断言里带上它（漏搬一列编译不报错，只有用例能拦）。
- 新增事件字段：改 `append` 一处；不要另写一条"补事件"的路径（本包是唯一的投影器）。
- 新增读侧消费者：从装配层的扇出器**再订一份**（上游 `jobs.Manager.Events()` 是容量 1 的单接收者通道，
  两个读者直接抢它会让后起者永远收不到）。

## Review 指南

- 投影是否仍然**只读**：不推进游标、不销项、不改作业表？
- 无会话归属的作业是否仍然**不落库**（不得回填到别的会话）？
- 事件序号是否仍取时间基（按会话可排序）？
- 端点是否仍只有一处：作业表状态只在 Seele `jobs` / `registry` 里，本包只有投影？
- `Close` 是否仍在"从未 Start"时不阻塞？

## 测试与验证

```text
go test ./backend/tool/jobs/ -count=1
go test ./backend/engine/assemble/ -run 'JobsEvent|Async' -count=1   # 宿主接线（真扇出 + 真 jobs.Manager）
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### event_stream.go

- `const EventSource string` — 作业生命周期事件在统一事件库里的稳定 Source 标识（`seelex.jobs`）。
- `const MainAgentID string` — 主会话在事件定位里的稳定标识（`seelex-main`）。
- `type EventStream struct` — 把作业生命周期投影成会话事件流的读侧投影器。
- `type SnapshotReader interface` — 事件投影需要的唯一一格框架读数（`seelejobs.Manager` 的读面）。
- `func NewEventStream(reader SnapshotReader, persister func() func(context.Context, frameworkevent.Event) error, signals <-chan struct{}) *EventStream` — 构造事件流（reader 为 nil 时返回 nil）。
- `func (stream *EventStream) Start()` — 起投影协程（幂等；nil 接收者安全）。
- `func (stream *EventStream) Close()` — 停止投影协程（幂等；没起过协程就直接返回）。
- `func (stream *EventStream) loop()` — 订阅变更信号口，每次被唤醒投影一次增量。
- `func (stream *EventStream) drain()` — 把"自上次投影以来新出现的状态"追加到各会话的事件库。
- `func (stream *EventStream) append(record seelejobs.Record)` — 把一条作业读数的当下状态投影为一个会话级事实事件。
- `func eventStatus(state seelejobs.State) frameworkevent.Status` — 把作业状态映射为框架事件状态。

### projection.go

- `func ProjectAsyncRuns(infos []registry.AsyncRunInfo) []dto.AsyncRunRecord` — 把后台执行表的探针读数投影为 DTO，按派发顺序。
- `func recordFrom(info registry.AsyncRunInfo) dto.AsyncRunRecord` — 搬运探针读数到 DTO（漏搬一列由用例钉住）。
