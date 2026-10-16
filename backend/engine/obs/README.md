# obs（观测：请求画像 + 会话遥测 + 统一事件库）

## 生态位

`backend/engine/obs` 是**引擎侧的观测面**：把"这一轮发出去的请求长什么样"与"这个会话用了多少
token"变成可查询的读数，并保管**统一事件库**（A 类事实与 B 类脱敏摘要同库的查询面）。主要调用方
是装配器（`backend/engine/assemble` 的 Runtime 在 `agent.NewWithComponents` 之前包一层）与
`wiring/adapters`（会话入口注入 telemetry 归属键）。

物理位置：2026-10-16 按
[plan.md](../../../docs/2026-10-15-architecture-reshape/plan.md) §2.5 从
`seelebridge/{request_log.go,session_telemetry.go}`（该包已整体归位 `backend/engine/assemble`）
再下沉到这里；同域的 `events_unified.go`（统一事件库）随后按 **P21-6「域方法随类型走」**
从 `backend/engine/assemble` 归位到本包，装配层只剩薄转发与同身份别名。

## 职责与非职责

- **做**：① 请求画像记录——`SEELEX_REQUEST_LOG=<jsonl>` 时，把每轮请求的消息摘要（角色、长度、
  内容指纹）与工具名清单 append 到 JSONL，供事后核对"发出去的是不是我以为的那份"；
  ② 会话遥测——把会话号放进 ctx（`WithTelemetrySessionID`）并按会话读出 token 计数
  （`SessionTokenCount`）；
  ③ 统一事件库——B 类（llm/tool 意图-效果）脱敏摘要的 append-only 内存日志 + 可选持久化钩子
  （`SummaryLog`），以及按 sessionID/nodeID 关联两轨的统一查询（`UnifiedEvents` 的两个读法与
  `UnifiedEventTopic` 的话题映射判据）；
  ④ 事件关联面——双轨事件（事实轨 `event.Sink` / 遥测轨 `telemetry.Hook`）的关联说明与
  `CorrelateMainSessionID`（持久化前为主会话事件补 `session_id`）。
- **不做**：不做任何业务判断、不改变请求内容（记录点只读传入的 messages/tools）、不落盘会话正文；
  画像里只留摘要与指纹，不留原文。统一事件库不写第二份存储：B 类摘要经宿主注入的持久化钩子
  追加进**同一** store 事件库（A 类事实的库），不做双写；也不决定"持久化到哪里"——那是宿主的装配读数。

## 数据流

```text
Runtime 装配（assemble）
  └─ 若 SEELEX_REQUEST_LOG 非空：OpenRequestLogger(path)
       ├─ NewRequestLogCompleter(inner)       ← 同步调用：先 record 再转调原 Completer
       └─ NewRequestLogStreamCompleter(inner)  ← 流式调用：同上
会话入口（wiring/adapters）
  └─ WithTelemetrySessionID(ctx, sessionID) → 引擎侧按会话记账
  └─ SessionTokenCount(tracer, sessionID)   → 读该会话的 token 合计
A 类事实（plan/节点执行，Source=workplan.runner / seelex.subagent）─┐
B 类摘要（SummaryLog.Append → 持久化钩子，Source=seelex.telemetry.summary）─┤→ store 事件库（唯一事实源）
内存实时面（tracer）──────────────────────────────────────────────────────────┘
统一查询 UnifiedEvents.Query/QueryRange ◄── Router（store 事件库）+ Live（宿主传入的实时查询闭包）
```

## 文件结构

| 文件 | 职责 |
|---|---|
| `request_log.go` | `RequestLogger` / `OpenRequestLogger` / `RequestLogEnv` / 两个包装器（同步与流式）+ 消息摘要与 wire 文本归一 |
| `session_telemetry.go` | `WithTelemetrySessionID` / `SessionTokenCount` |
| `unified_events.go` | 统一事件库：`SummaryLog`（`NewSummaryLog` / `RecordSummary` / `Append` / `Summaries` / `SetPersister`）+ 摘要→框架事件转换 + `UnifiedEventView` / `UnifiedEventReader` / `UnifiedEvents`（`Query` / `QueryRange`）+ `UnifiedEventTopic` 话题映射 + `filterUnifiedEvents` 过滤 + `SummaryEventSource` / `ErrUnifiedEventsRouterMissing` |
| `event_correlation.go` | 双轨事件关联面：`CorrelateMainSessionID`（宿主闭包给主会话读数，缺失 `session_id` 的事实轨事件在持久化前补上 `agent.runtime` 定位）+ 两条轨的关联字段说明 |
| `request_log_test.go` | 画像落盘、开关语义、包装器透传 |
| `unified_events_test.go` | 摘要落库语义（Source/Seq 稳定、节点作用域）、两个读法（Router 缺失时报错、区间裁剪与 limit 上限）、话题映射与过滤判据 |
| `event_correlation_test.go` | 关联补全（缺失才补、已有不改、主会话未就绪原样透传）+ **跨实现一致性**用例（`store.SessionIDFromLocations` / `store.SessionIDFromEvent` / `UnifiedEventTopic` 三处对同一输入同解） |

## 核心实现

- `SummaryLog`：`sync.Mutex` + `[]seeletelemetry.SummaryEvent` 内存日志；`Append` 先入内存、再调
  宿主注入的 `persister`（未注入即空操作）。`RecordSummary` 是 `seeletelemetry.SummaryRecorder` 的
  best-effort 实现（void，丢弃落库错误）。
- `summaryEventToFrameworkEvent`：`Sequence` 取 `At.UnixNano()`（与 `plan.AppendPhase` 同策略），
  使同库按 Seq 幂等合并时不会把多次摘要折叠成一条；`Status` 只在 `"failed"` 时映射为失败态。
- `UnifiedEvents`：装配期注入两格读数——`Router`（`*store.Router`，可能后装配）与 `Live`
  （`func(ctx, limit) (frameworktelemetry.ViewModel, error)`，宿主闭包，域包因此不认识 tracer 的具体类型）。
  两格都在**调用时**读取，保持"Router 可能后装配 / tracer 可能未接线"的既有语义。
- `UnifiedEventTopic` / `filterUnifiedEvents`：两轨关联的**唯一判据处**（哪些通道算会话级、按
  sessionID/nodeID 归属与 limit 截断），查询与区间查询共用。
- `CorrelateMainSessionID`：包装持久化钩子的**唯一一处**（两格入参都是函数值：主会话读数由宿主
  闭包给，下游钩子原样转调；任一为空即原样返回下游）。它**不自己解析**会话归属——那句判据在
  `store.SessionIDFromEvent`（本包 `UnifiedEventTopic` 与 `backend/tool/plan` 的事件投影
  也问同一句）。

## 并发、安全与错误语义

`RequestLogger` 自持锁，`record` 串行追加；`OpenRequestLogger("")` 与未设环境变量都是空操作。
打开文件失败要如实返回（调用方据此决定"要不要带着诊断继续起"）。画像内容做了**摘要化**：
`digestRequestMessage` 只留角色、长度与内容指纹，避免把提示词原文写进日志。
`SummaryLog` 同样自持锁；`Summaries` 返回拷贝，调用方拿到的切片不会反向改写内部状态。
`ErrUnifiedEventsRouterMissing` 是"Router 未装配"的**唯一文案处**（两个读法共用一句），
错误值本身由装配层的同值变量转发，不另写文案。

## 依赖方向

允许：标准库、Seele（`agent`/`types`/`event`/`telemetry`）、`internal/telemetry`、`store`
（**只读**用途：统一事件库的 `*store.Router` 与 `store.EventStore`，本包不写业务数据）。
禁止：`application/*`（上层）、`backend/engine/assemble`（装配器反过来 import 本包）、
`backend/{engine,tool,multiagent,limit}` 的其它领域包（观测面不被业务域反向依赖）。

## 扩展方式

新增观测点＝在本包加一个"只读传入值"的记录器，并在 `assemble` 的装配处包装；
不要在记录器里做判断或改写入参。开关一律走环境变量/构造参数，不读全局状态。
新增统一事件读法＝在本包加方法并在 `assemble/events_unified.go` 加一行转发（宿主读数仍只有
Router 与 Live 两格）；不要为读法在装配层复制判据。

## Review 指南

- 记录器是否只读入参、绝不改 messages/tools？
- 画像是否仍然只留摘要与指纹（不落原文）？
- 包装顺序是否仍在最靠近 provider 的一层（早于它的包装会被漏记）？
- 统一事件库是否仍然"同库不双写"？宿主读数是否仍只有 Router 与 Live 两格（没有偷偷接第三个）？
- `SummaryEventSource` 是否仍是**唯一**的 Source 字面量处（查询侧与用例都按它认事件）？

## 测试与验证

```text
go test ./backend/engine/obs -count=1
```
