# 重复契约盘点（类型拷贝 / 接口分层 / 测试桩）

日期：2026-08-14
性质：只读调研

## 1. 类型重复（同一概念 2~3 份定义）

| 概念 | 出现位置 | 份数 | 说明 |
|---|---|---|---|
| `NodeStageLog` | `application/contract/dto/subagent_live.go`、`seelebridge/internal/model/node_result.go` | 2 | 字段一致，改一处要同步另一处 |
| `NodeSemanticResult` | 同上两处 | 2 | 同上 |
| `SubagentToolEvent` | `seelebridge/session/tool_events.go`、`application/model/state.go`；另有 `dto.SubagentTool`（`dto/subagent_live.go`，形状略不同含 DurationMS） | 2~3 | 运行态/投影/对外契约三份 |
| `NodeWorktreeInfo` | `application/contract/dto/worktree.go`、`seelebridge/worktree` | 2 | runtime 与 dto 各一份 |
| `ScheduledTaskSpec/Status` | `dto/scheduler.go` 权威，`seelebridge/scheduler` 用 alias | 1 + alias | alias 属于健康做法，保留 |
| `TaskRecord/TodoItem` | `dto/task.go` 权威；`seelebridge/task` 做 `TaskToTodoItem` 转换 | 1 + 转换 | 转换是显式适配，可保留但收敛到 mapper |
| `ContextSnapshot` | `seelexctx/snapshot` 权威；`application/model.SubagentContext` 是适配投影 | 1 + 适配 | 适配保留，但转换散在 core 各文件 |

**问题**：同一结构多处手抄 → 字段演进不同步（如刚加的 `NodeStageLog.Turn` 要改两份）、
序列化标签可能漂移、测试要覆盖转换一致性。

**收敛方向**：每概念一份权威定义 + 一处显式转换。

- 对外契约/跨层结构 → 以 `application/contract/dto` 为**单源**；
- 运行态内部结构 → 以 `seelebridge/internal/model` 为单源；
- 两者之间 → 收敛到一个 `mapper` 包集中转换（见 04 §1），禁止散落内联拷贝。

## 2. 接口分层重复（同一引擎多份契约与多份测试桩）

- `ChatEngine`（`application/contract/ports.go`）：application 依赖的引擎契约，
  新方法（如 `SubscribeSubagentLive`）要同步改 3 份测试实现：
  - `gui/tool_full_chain_test.go` `guiChainEngine`
  - `application/core/service_test.go` `fakeEngine`
  - `e2e/scenario/scripted_engine.go` `ScriptedEngine`
- `ReactorEngine`（`application/adapters/adapters.go`）：适配层私有契约，与
  `ChatEngine` 部分重叠（History/ClearHistory/SessionID/SetSystemPrompt/…）。
- `SessionPort`/`TreePort`/`TaskPort`（`seelebridge/node/coordinator.go`）：node 域
  对 session/tree/task 的接口化，每加方法要同步 `Coordinator` 委托 + `SubagentSessions`
  实现。

**问题**：接口本身是刻意分层（安全边界），保留；但**同一接口的测试实现重复**
和**两个引擎契约字段重叠**是纯成本。

**收敛方向**：
- 测试桩 → 共享 `internal/testutil` 假实现（见 04 §4），不再每个测试文件手写全量；
- `ReactorEngine` 与 `ChatEngine` 的重叠字段 → 抽取最小公共接口，或让
  `ChatEngine` 内嵌 `ReactorEngine` 表达"引擎 = 基础面 + 扩展面"。

## 3. 兼容别名与重导出

- `application/application.go`：整包重导出（`Service = core.Service`、`ChatEngine =
  contract.ChatEngine`、全部 model 类型）——这是门面，健康；
- `seelebridge/internal/telemetry/telemetry.go`：重导出 `NewTracer/NewLifecycleHook`
  "保持公共 API"——若内部无外部消费者可删，有则保留并标注；
- `application/contract/dto/task.go`：`TodoItem`/`TodoItemStatus` 标注"兼容 TUI/旧契约"，
  权威在 `TaskRecord`——旧契约保留有期限，建议定一个移除窗口。

## 4. 验收标准（本类收敛后）

- 同一结构体名在 `go/types` 全仓扫描只出现一次定义（alias 除外）；
- 任一 `dto` 字段增删，`rg` 到的定义数 = 1，转换点 = 1；
- 给 `ChatEngine` 加方法时，需要改的测试文件 = 1（共享桩）。
