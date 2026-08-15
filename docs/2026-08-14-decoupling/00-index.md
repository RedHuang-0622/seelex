# 减少代码耦合与重复：盘点与提炼总览

日期：2026-08-14
性质：只读调研 + 设计建议（未改任何代码）

## 目标

解决两个已确认的耦合/冗余问题：

1. **重复类型**：同一概念在 dto / internal/model / application/model 等层出现 2~3 份
   定义，改一处要同步多处；
2. **双轨兼容**：同一能力存在新旧两条路径（双输入队列、双上下文通道、双事件体系、
   双插件管理器、双存储），历史包袱与现状并存。

## 文档导航

| 文档 | 内容 | 结论 |
|---|---|---|
| [01-duplicate-contracts.md](01-duplicate-contracts.md) | 重复契约盘点（类型拷贝、接口分层、测试桩） | 类型单源 + mapper；接口按需收敛 |
| [02-duplicate-architecture.md](02-duplicate-architecture.md) | 重复架构/双轨兼容盘点（队列/通道/事件/插件/存储） | 每轨收敛到单一来源 |
| [03-duplicate-patterns.md](03-duplicate-patterns.md) | 重复模式盘点（actor、provider 注入、hook 链、回滚链、fake） | 通用件提炼 |
| [04-reuse-extraction.md](04-reuse-extraction.md) | 提炼清单 + 放置位置 + 优先级/风险 | 分批执行建议 |

## 方法

- 只盘点"同一语义的重复"，不把"刻意分层"（契约/适配/门面）当冗余——分层本身是
  安全边界，砍的时候要保留边界只砍拷贝。
- 每个条目给出**证据位置**（文件/行号）与**收敛目标**，便于逐条验收。
- 与 `docs/2026-08-14-runtime-refactor`（R1–R5）衔接：前一轮削了根包门面，
  本轮针对类型与双轨。

## 总优先级（详见 04）

- **P0（低风险高收益）**：类型单源 + 显式 mapper；输入队列双轨收敛；测试桩共享。
- **P1（中风险，行为不变重构）**：通用 actor 助手；provider 注入面收敛。
- **P2（依赖上游或产品决策）**：事件桥（Seele G5）、插件双轨合并。

## 实施状态（2026-08-15）

P0 批次已实施并全绿（`go build ./...`、`go vet ./...`、
`go test ./... -count=1`、`node --test gui/frontend/dist/*.test.mjs`）：

- 类型单源 + mapper：`NodeStageLog` 以 `application/contract/dto` 为单源
  （`seelebridge/internal/model` 改 alias）；`SubagentToolEvent` 下沉到 dto
  单源，`seelebridge/session` 与 `application/model` 改 alias；新增
  `seelebridge/internal/mapper` 集中 `ToolEventToDTO` 转换，`runtime_live.go`
  的内联拷贝消除。
- 输入队列双轨收敛：删除 `deferredInputQueue` 与
  `drainQueuedInputsAfterLoop` 中间提升路径，单一 `inputQueue` + `runChat`
  结尾单点消费；`OnIterationComplete` 只保留 Session-backed 中断判定与
  非 Session-backed 的 history 修复，队列不再在 hook 内注入。
- 子代理上下文双通道收敛：删除 `pendingSubagentContexts` 与
  `AppendSubagentContext`，单一入口 = `Runtime.DrainSubagentContexts`。
- 测试桩共享：新增 `internal/testutil.EmbeddedChatEngine`（全方法 panic
  底座），`guiChainEngine`/`fakeEngine`/`ScriptedEngine` 内嵌复用。

P1（通用 actor、注入面收敛）已完成，见下节「实施状态（2026-08-15 P1 批次）」；
P2（事件桥、插件双轨）仍待产品决策/上游评估。

## 实施状态（2026-08-15 P1 批次）

P1 批次已实施并全绿（`go build ./...`、`go vet ./...`、
`go test ./... -count=1`）：

- 通用 actor 助手：新增 `seelebridge/internal/actor`（有界命令通道 + 单
  消费者 goroutine + 幂等 Close + 带超时投递），并迁移 3 个 mailbox actor：
  `task.TaskRegistry`、`session.SubagentSessions`、
  `session.SubagentContextActor`；公共方法面零改动。`subagent_tree`、
  `scheduler.State`、`fs`、`mcp` 实际为锁/ticker 实现而非 mailbox actor，
  保持现状不强行改造（见 `internal/actor/README.md`）。
- 插件事务助手：新增 `plugin/apply.go` 的 `Transaction`（顺序执行 + 失败
  逆序回滚 + 错误聚合）与 `DiffState`（新增/删除/修改 diff）；
  `Load`/`Activate`/`Deactivate` 统一改走 `Transaction`，`Reload` 热更新
  diff 走 `DiffState`。
- provider 注入面收敛：`EnginePort` 的 6 个节点数据源 setter +
  `SetHistoryPreparer` 收敛为 `ApplyDeps(EnginePortDeps)` 一次注入
  （main.go 与 adapters_test 同步）；运行期可变 setter（权限/投影/policy/
  binding）按方案保留。Runtime 的 9 个一次性 setter（诊断观察者/审批门/
  事件持久化/项目知识/子代理工具回调/skill 目录/压缩归档/调度器执行器与
  观察者）收敛为 `Runtime.ApplyDeps(RuntimeDeps)` 一次注入；单字段 setter
  保留供测试与细粒度注入，生产装配点走 Deps 结构。

待办：
- P2 可选项 telemetry `Chain` 与统一事件库均已实施，见下节
  「实施状态（2026-08-15 统一事件库批次）」。

## 实施状态（2026-08-15 P2 短期/收尾批次）

全绿（`go build ./...`、`go vet ./...`、`go test ./... -count=1`）：

- §01.2 接口分层重复：抽取 `contract.EngineBase` 公共基础面
  （ChatStream/ClearHistory/SessionID/SetSystemPrompt/SetMaxLoops），
  `ChatEngine` 与 `adapters.ReactorEngine` 都内嵌它，表达"引擎 = 基础面 +
  扩展面"；History/AppendHistory 因消息类型边界（types.Message ↔
  EngineMessage）保留在两侧。
- P2 事件桥（短期收敛，seelex 侧自补）：新增 `seelebridge/events.go`
  收拢双轨说明与关联字段，并在 `Runtime.SetEventPersister` 持久化前为
  缺失 session_id 的事实轨事件补主会话关联（框架 runner 事件不携带
  session_id 时 EventStore 原本跳过落库）。长期统一事件库仍待产品决策。
- §02.5 存储双轨：`runtime.go` 内存 history 兜底标注"仅测试可用"
  （生产装配点始终 AttachHistoryRouter）；legacy 缓存兜底标注"只读迁移"。
- §02.6 兼容别名移除窗口：`stream_batcher.BatchSize`、`dto.TodoItem`/
  `TodoItemStatus`、`plan/input_adapter` 顶层节点旧形状均标注移除窗口
  （建议 v0.2，2026-12-31）。
- §02.4/§04.7 插件双轨：新建
  `docs/2026-08-14-decoupling/05-plugin-dual-track-decision.md`，产品已确认
  **单选 + 方案 A 轻量版**（root 唯一事实源，`seelebridge/plugin` 为可见性
  投影缓存，写路径单入口 + Transaction 原子）；包注释与 README 已标注边界。

## 实施状态（2026-08-15 统一事件库批次）

全绿（`gofmt -l .` 空、`go build ./...`、
`go build -tags "gui,desktop,production" ./...`、`go vet ./...`、
`go test ./... -count=1`）：

- P2 可选项 telemetry `Chain` 组合器已落地：`seelebridge/internal/telemetry`
  新增 `Chain`（`Wrapper` 装饰器形态，透传/nil 兜底/`ErrorHook` 传播集中
  处理），`DiagnosticHook`/`StageHook` 改为 Wrapper 签名，runtime.go 的
  hook 链改由 `Chain` 一次组装。行为变化（修复）：`OnError` 此前被
  StageHook 透传链丢弃，现在能到达 `LifecycleHook`（tracer 记录 error
  事件），chain_test 已固化。
- P2 事件桥长期项（统一事件库）按推荐形态实施：
  `seelebridge/events_unified.go` 新增 `SummaryLog`（B 类脱敏摘要统一日志，
  与 A 类事实同一 sessionstore 事件库持久化，不双写）与
  `UnifiedEventReader`/`Runtime.UnifiedEvents`（按 sessionID/nodeID 关联
  A 持久 + B 摘要 + B 实时）。`SummaryHook` 只记失败/超时/慢调用（默认
  阈值 30s），字段仅 kind/name/status/duration_ms/at/nodeID（无参数/结果/
  正文）。采纳默认值：摘要与 A 类同库、慢阈值 30s、字段集按
  [06-unified-event-store-decision.md](06-unified-event-store-decision.md)。

待办：
- 统一事件库产品细化项（非阻塞）：摘要保留期限/预算、慢调用阈值是否
  需要可配置化（当前为包常量）、GUI/TUI 是否消费统一查询接口（后端门面
  已就绪，前端消费未做）。
