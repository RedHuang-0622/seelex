# 事件体系双轨 → 统一事件库：两事件类取舍方案

日期：2026-08-15
性质：设计定稿（推荐形态；**已实施**，默认值已定，见 §6）
关联：`docs/2026-08-14-decoupling` §02.3 / §04.7；`seelebridge/events.go` /
`seelebridge/events_unified.go`

## 0. 结论先行

统一事件库的推荐形态是**单一追加日志源 + 分层投影**，不做全量双写：

- **A 类（plan/节点执行事实）**：全量持久化，是唯一事实源（现状已具备）；
- **B 类（llm/tool 意图-效果遥测）**：**不全量落盘**——保留内存实时面，
  另以**有界、脱敏的摘要事件**追加进统一日志；
- 统一查询接口按 sessionID/nodeID 跨两源关联（A 持久 + B 实时 + B 摘要），
  用"引用/索引"而不是复制 payload。

一句话：**A 类落盘、B 类留内存 + 摘要落盘，查询统一、存储不双写。**

## 1. 两事件类定义与现状

| 维度 | A 类：plan/节点执行事实（事实轨） | B 类：llm/tool 意图-效果（遥测轨） |
|---|---|---|
| 内容 | 节点生命周期（queued/running/completed/failed…）、plan 状态、worktree 阶段、fork 子代理阶段 | 每次 LLM 调用（before/after、model、tokens）、每次工具分发（name/args/result/duration） |
| 生产者 | `frameworkevent.Sink`（[plan/events.go](../../seelebridge/plan/events.go) `EventSink`） | `telemetry.Hook` 链（LifecycleHook → DiagnosticHook → StageHook，[internal/telemetry](../../seelebridge/internal/telemetry)） |
| 当前落点 | 内存事件库 + 投影订阅 + 持久化到 [sessionstore 事件库](../../sessionstore/event_store.go)（按 session_id 分会话） | 仅内存 [MemoryTracer](../../seelebridge/internal/telemetry/telemetry.go)，进程重启即失 |
| 量级 | 低-中（每节点/每次运行数十~数百条） | 高（一次会话可能数千条） |
| 敏感度 | 节点输出为有界证据，可截断 | 工具参数/结果、LLM 正文可能含用户数据与 secret；现 DiagnosticHook 刻意剥离 payload（"不含命令文本/参数/输出/账号数据"） |
| 消费方 | plan 状态投影（前端快照）、worktable 打点、审计、恢复 | GUI/TUI trace 视图（TraceText/TokenCount）、bash 停滞诊断、子代理第一视角 stage 日志 |
| 关联字段 | Scope{PlanID/RunID/NodeID}；session_id 已由短期桥补全（[events.go](../../seelebridge/events.go)） | sessionID 存在；nodeID 经 NodeScope 投影到 stage 日志 |

## 2. 取舍维度与选择原因

### 2.1 持久化必要性

- **A 类：必须持久化**。它是 plan 运行的审计证据与恢复依据（会话恢复/plan
  重建依赖），且已有落盘通道，不增加新存储。
- **B 类：不必须**。消费方（trace 视图、停滞诊断、实时 stage）都是
  **实时/调试导向**，进程重启后 B 类历史价值低；全量落盘只为"事后可查"，
  而事后可查的真正需求点是"哪个工具失败/超时/慢"，可以用摘要覆盖。

### 2.2 一致性（双写风险）

若 B 类全量双写进统一日志，会引入经典双写一致性问题：写一半崩溃、
两库对不上号，需要 Outbox/事务补偿，复杂度显著上升。**单一源 + 引用**
（Event Sourcing + CQRS 读模型）规避了这个问题——现状 A 类已是
"append-only 日志 + Subscribe 投影"（`EventSink`），B 类沿用内存订阅即可。

### 2.3 量级与成本

B 类全量落盘 = 每次 LLM 调用、每次工具调用都写盘，且其中大部分是
"成功且正常"的噪音；对存储与保留策略都是持续成本。摘要落盘只保留
**有审计价值的子集**（失败/超时/慢调用、终态），量级与 A 类相当。

### 2.4 敏感度与脱敏

工具参数/结果、LLM 正文属于高敏感数据。全量落盘会**成倍扩大风险面**
（磁盘副本、备份、日志泄漏路径）。摘要事件只含
`kind/name/status/duration/at/nodeID`，不含参数、结果、正文——与现有
DiagnosticHook 的脱敏原则一致。

### 2.5 消费方式（实时 vs 历史）

B 类消费方全部是实时订阅（channel/回调），没有"历史重放"诉求；
A 类消费方包含历史重放（审计、恢复、回放）。因此 B 类维持内存实时面，
A 类维持持久化，天然匹配。

### 2.6 回放/恢复需求

会话恢复、plan 重建、worktable 审计都依赖 A 类持久日志；B 类不存在恢复
依赖（trace 视图只是调试窗口）。明确接受"进程重启后 B 类实时历史不可
回放"。

## 3. 推荐方案（三层）

```text
统一查询接口（按 sessionID/nodeID 关联）
   ├── A 类：持久事件库（sessionstore，事实源；session 关联已补）
   ├── B 类实时：内存 tracer（现状保留；进程内索引 = 引用，不复制 payload）
   └── B 类摘要：有界脱敏摘要事件 → 追加进统一日志（新：telemetry 摘要 hook）
```

1. **层 1（A 类全量）**：维持 sessionstore 事件库为 plan 执行事实源；
   `seelebridge/events.go` 的 session 关联已落地（短期桥）。
2. **层 2（B 类实时）**：MemoryTracer 不变；按 sessionID/nodeID 建立
   **进程内索引**（浅拷贝/引用，不复制事件 payload），供统一查询在
   进程存活时关联两轨。
3. **层 3（B 类摘要）**：新增 telemetry 摘要观察面，把
   `name/status/duration/at/nodeID`（脱敏）追加为统一事件；失败/超时/慢
   调用才有审计价值，正常成功调用可只留终态或丢弃。

## 4. 为什么不是"全量双写"（选择原因汇总）

- 成本：B 类量级大，全量落盘是持续存储/保留成本；
- 风险：参数/结果/正文落盘扩大敏感数据暴露面，与现有脱敏原则冲突；
- 一致性：双写引入 Outbox/补偿复杂度，单一源 + 投影（Event Sourcing +
  CQRS）是既有形态的自然延伸；
- 收益：B 类消费方全部实时，历史价值低；审计诉求用脱敏摘要即可覆盖。

## 5. 代价与风险（明确接受的取舍）

- 进程重启后 B 类实时历史不可回放（调试视图，接受）；
- 摘要事件丢失细节——如需追原文，沿用 `tool result ref` 的"摘要 + 引用"
  模式（摘要只存 ref，不存内容）；
- 两源查询比单库复杂——用统一查询接口封装，不扩散到调用方；
- 摘要 hook 需要新观察面：落地前置是 telemetry `Chain` 组合器
  （P2 可选），避免再手写一段透传样板。

## 6. 实施状态与采纳默认值（2026-08-15）

推荐形态已实施（`seelebridge/events_unified.go` +
`seelebridge/internal/telemetry/summary.go`），默认值如下：

- **B 类摘要落盘**：做。摘要与 A 类事实**同库**（同一 sessionstore 事件库，
  Source=`seelex.telemetry.summary`），存储不双写；
- **摘要触发策略**：失败/错误必记；正常成功但耗时 ≥ 阈值记 `completed`；
  其余正常成功丢弃。慢阈值默认 `30s`（包常量 `summarySlowThreshold`，
  `WithSlowThreshold` 已支持测试覆盖，生产可配置化留作产品细化）；
- **摘要字段集**：`kind/name/status/duration_ms/at/nodeID`，无参数、结果、
  正文（与 DiagnosticHook 脱敏原则一致）；
- **统一查询**：`Runtime.UnifiedEvents(sessionID, nodeID, limit)` 关联
  A 持久 + B 摘要（同库读取）+ B 实时（内存 tracer）；GUI/TUI 消费未做，
  后端门面已就绪。

剩余产品细化项（非阻塞）：摘要保留期限/预算、慢阈值生产可配置化、
是否在 GUI/TUI 暴露统一查询。

## 7. 模式参考

- 同步推送：观察者（Observer）/ 发布-订阅（Pub-Sub）——事件发生方持订阅者
  列表，Go 形态为回调（`EventSink.Subscribe`）与 channel（
  `PlanNodeEventChannel`/`TaskChangedChannel`）；
- 单一源 + 投影：事件溯源（Event Sourcing）+ CQRS 读模型——日志是唯一
  事实源，状态/视图是投影，消费方持引用按需回源；
- 防双写不一致：Outbox——业务状态与事件同事务，本方案用"不双写"直接
  规避，而非引入事务补偿。
