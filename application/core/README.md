# Application Core

## Service 装配结构

`Service` 是稳定的应用门面与跨组件编排层，本身只保存 `serviceState` 和 `serviceComponents`。共享状态按基础设施、会话、计划、任务上下文、提示词、生命周期和前端快照分组，避免继续扩张成平铺字段集合。

`service_assembler.go` 是唯一组合根，负责装配 prompt、context、task-context、session、history-safety、view 和 input 组件。组件共享同一份受锁保护的状态，但行为通过窄组件端口协作；组件不得持有完整 `*Service`。聊天执行、会话切换、workspace 切换等跨域事务仍由 `Service` 编排，单域规则由对应组件实现。

`service_components_test.go` 固化两条结构约束：`Service` 只能包含状态与组件图，聚焦组件不能反向持有门面。

## 模块定位

`core` 是 Seelex 的应用用例层和权威状态机。它不直接创建数据库、Wails 窗口或 Seele Agent，而是通过 `contract.Dependencies` 编排这些能力。

主要调用方是 `application` facade；主要消费者是 TUI、GUI Bridge 和 E2E harness。

> Seele v2 装配模型：`contract.ChatEngine` 端口由 seelebridge 创建的
> `session.Session`（`session.NewSession` 主会话）经 `enginePort` 适配满足；
> `RuntimePort` 转发 seelebridge.Runtime（账号池/Completer/可见性/Plan
> preflight/策略）。本模块只消费窄端口，不直接接触 Seele 会话实现。

## 文件结构

| 文件 | 职责 |
|---|---|
| `service.go` | `Service` 共享状态、锁、依赖与构造；不承载具体用例。 |
| `service_assembler.go` | 装配件：补齐基础设施默认值，按既有顺序组合 `Service`、输入路由、Prompt、Runtime 与初始 workspace。 |
| `service_input.go` | 输入路由、conversation 排队、取消、空闲等待与关闭。 |
| `input_router.go` | 组合式输入路由器；按 command → skill → plugin → conversation 的策略顺序分派已规范化输入。 |
| `service_prompt.go` | system prompt 层组装与引擎同步。 |
| `service_interaction.go` | approval/session/account/plan retry 交互，以及 effort 和 plugin 切换。 |
| `service_snapshot.go` | Snapshot 读取、runtime 刷新、消息追加、revision 与事件发布。 |
| `workspace_usecase.go` | session 删除和 workspace 的创建、绑定、解绑与 snapshot 同步。 |
| `session_history.go` | session 恢复、历史分页加载和 EngineMessage 到 UI Message 的转换。 |
| `chat.go` | 流式聊天、输入队列、工具事件、Plan 状态打点和 idle/draining。 |
| `task_execution.go` | 请求私有的 PlanAct checkpoint、终态 payload 校验和 `task_complete` / `task_needs_user_decision` / `task_failed` handler。 |
| `history_safety.go` | Provider 空 content、上下文耗尽与 504 可恢复中断的历史安全处理。 |
| `visible_output.go` | 前端可见输出过滤；剥离模型 `<think>` 块，不暴露内部推理。 |
| `context_controller.go` | 基于 token 与 checkpoint 的上下文控制；超长工具结果以重取警告替代，内部控制消息在持久化前清除。 |
| `command.go` | 内置命令注册与执行。 |
| `session_scope.go` | 跨项目 session catalog、真实存储位置定位、标题恢复和 scoped read。 |
| `session_draft.go` | GUI 新会话草稿、首次请求物化和项目 binding。 |
| `session_storage.go` | JSON/SQLite/PostgreSQL 存储设置用例。 |
| `skill_context.go` | Skill 指令与用户可见输入的 envelope 编解码。 |
| `completion.go` | `/`、`#`、`@` 输入建议。 |
| `diagnostics.go` | Snapshot 诊断文本。 |
| `aliases.go` | 对 model/event/approval/prompt/contract 的兼容别名。 |

## 权威状态与生命周期

`Service.snapshot` 是前端状态事实源，受 `Service.mu` 保护。一次普通对话：

1. `Submit` 识别 command、Skill、Plugin 或 conversation。
2. `startChat` 写入 user/assistant placeholder、设置 ChatState 并发布事件。
3. Engine `ChatStream` 的 chunk 和 tool hooks 增量更新 conversation/runtime/plan。
4. 完成后保存当前 session；若队列非空，把排队输入冻结并合并为下一 turn。
5. `WaitForIdle` 在 active turn 和已接受队列全部完成后返回。

每个 conversation request 在提交时冻结其 effort 对应的 ReAct budget（工具轮数、工具调用数）。budget 耗尽时保留一次无工具的最终交付回合；它不把报告导出等交付工具一概禁止。长编码任务不设 wall-clock 超时，用户仍可随时主动取消。

`BeginGracefulShutdown` 拒绝新输入但允许已接受工作完成；`Shutdown` 才取消 active chat 并关闭 broker/events。

每个请求另有私有 `TaskExecutionState`：工具结果与 Plan 节点状态写入有界 `NodeCheckpoint`。超长工具结果在下一次 provider 调用前被替换为与原 tool-call 配对的短警告，原文不作为模型上下文；Agent 必须以文件路径、行范围、过滤条件、分页或摘要命令重新读取。历史超过标准 context budget 时，`ContextController` 保留 system prompt，并以一个私有 checkpoint 替换整段可变的 user/assistant/tool transcript；后续轮次需通过定向工具重新获取被省略的细节。连续无新事实、变更、产物或节点状态的工具轮次会触发预算兜底，但不是上下文管理主路径。模型应以 `task_complete`、`task_needs_user_decision` 或 `task_failed` 结束工具型任务；它们分别表示可交付完成、必须由用户选择的有效分歧，以及有界失败事实。若已加载的 authority Plan 尚未执行而模型自然收尾，运行时将其表示为 `needs_user_decision`，而不伪造完成或暴露内部错误。`Snapshot.Task` 公开 `progressing/completed/needs_user_decision/blocked/interrupted/failed`，而 checkpoint、装配的 system prompt 和 `<think>` 内容都不进入 frontend snapshot。Provider 明确返回 context overflow 时，Service 保存私有恢复 checkpoint 并给同一 Agent 一次受控的恢复回合；504 从不自动重放可能已有副作用的工具轮。

## Session 与 Project 语义

- project 只定义会话的文件读写范围，不共享 conversation history。
- session ID 是唯一键；标题是按 `(workspaceID, sessionID)` 保存的稳定 KV 元数据。首次请求只初始化一次标题；除显式重命名外，恢复、压缩、历史分页和首条历史消息都不能改写它。
- `BeginNewSession` 保存旧的非空历史并清空 Engine history，然后只进入幂等 draft：不生成 ID、不写入空 Session、不建立 workspace binding；第一次进入 `submitConversation` 时才调用 `StartSession`，并立即用首问设置显示名。
- workspace ID 是 binding 与 storage shard 的键；显示名来自 root basename。
- 恢复 session 时先定位真实 `workspaceID + sessionID`，再读取历史和绑定 Runtime。
- 有历史的 session 切换 project 时先保存旧 scope，然后创建新 session，禁止把同一 ID 重新绑定后继续写。
- `sessionCatalog` 跨所有项目聚合 metadata；标题按 updatedAt 缓存，存储切换和删除时失效。

## Snapshot/Event 协议

每次状态变化先在锁内 bump revision，再在锁外 Publish。Snapshot 可独立重建全部 UI；Event 只负责低延迟增量。Message ID 由 Service 生成并在 history prepend 时保持稳定。

## Plan 集成

Tool hooks 把用户或 Agent 自主调用的 `plan_load` JSON 转为 Plan DAG。Plan 是可选的
可视化任务结构，不是聊天入口门禁；普通 ReAct 直接使用 project-scoped tools 工作。系统提示
区分 tasklist 与 plan：`plan_run` 可执行含 `kind:agent` 节点的 DAG，子代理继承项目作用域
与父证据、可真并行；tasklist 模式由主代理串行执行并在节点完成后 defer 单个
`task_complete`。`task_complete` 在存在已加载
Plan 时必须枚举完成节点才会把 Plan 标记完成。`replan` 只基于失败原因、旧 Plan 和已完成
节点证据加载一个原子替换的恢复 Plan；它不自动调用 `plan_run`，保留用户复核副作用的边界。

Effort 只为一次可选 `plan_load` 提供节点数、串行和并发约束；它不再创建 isolated preflight
subagent、request-scoped authority lease 或向普通用户输入注入 hidden Plan envelope。这样聊天、
问候和小任务始终直接进入 ReAct，同时复杂任务仍可在用户或 Agent 明确选择时使用 Plan。

## 依赖边界

允许依赖 Application 子包、`seelebridge` 的稳定桥接 DTO 和 `sessionstore.Config`。禁止依赖 `gui`、`tui`、根目录 main package 或具体数据库实现。

## Review 指南

- 不在持有 `Service.mu` 时调用 LLM、网络、数据库、文件系统或外部 callback。
- Snapshot 改动是否 bump revision 并发布正确事件。
- queued input 是否冻结提交时的 Skill 上下文，而非执行时重新读取。
- resume/load-more/delete 是否使用目标 session 的真实 workspace，而非当前 active scope。
- project 切换、storage reconfigure、shutdown 与 running chat 的竞争是否有明确结果。
- draft 期间切换项目是否只更新待继承 scope，是否避免空 Session ID binding；重复点击新建是否仍只保留一个 draft。
- Tool/Plan callback 是否只更新所属 request/session。

## 测试

```text
go test ./application/core -count=1
go test ./application/core -race -count=1
```

## Runtime projections and catalog cache

`Snapshot()` only clones the in-memory `Service.snapshot` under `Service.mu`.
It never reads Engine, Runtime, Workspace, or session storage. A background
worker refreshes the session catalog cache and stops without blocking GUI
shutdown on a legacy non-context-aware catalog call.

Application publishes immutable `RuntimeVisibilityProjection` and
`ParentEvidenceProjection` values to Runtime after relevant state changes.
Runtime never calls Application back. Subagent merge-back enters a bounded
Runtime mailbox and is drained outside `Service.mu` before the next main
`ChatStream` starts.

## Context compression visibility

`ContextController` rebuilds provider history from the active system policy, trusted task Skills, the active Plan slice, one structured checkpoint, and at most four recent complete protocol units. A unit is admitted only when every tool call has a matching result; orphan results and incomplete parallel calls remain in the durable transcript but never enter provider context.

The token audit counts the separately configured system prompt, message/tool-call overhead, visible tool metadata, the current input, and an output plus safety reserve. Requests that still exceed the safe budget after dropping complete old units and minimizing the checkpoint are rejected before `ChatStream`.

Oversized tool output and oversized current input are stored through immutable `result_ref` records. Provider history and `SessionRecord` contain only the reference warning; `read_tool_result` provides bounded, read-only pagination or filtering. `read_plan` retrieves omitted canonical Plan nodes without changing Plan state.

When compaction creates a new checkpoint, Application also publishes a separate `Snapshot.Task.ContextCompactions` record. The record contains only a version, public trigger reason, message count, estimated token count, and timestamp. It never contains checkpoint text, system prompts, tool arguments, tool results, or raw conversation history.

## Session record and recovery

Provider history is an execution cache, not the user-visible source of truth. `persistCurrentSession` atomically stores version 3 `SessionRecord`, bounded provider history, append-only transcript events, and new tool-result objects. The record owns stable title, visible conversation, Plan revisions, `TaskContextProjection`, checkpoint history, and tool-result metadata. Resume reads only a token-bounded tail of complete transcript units for the Engine, restores content-addressed task Skills and the canonical Plan from the projection/store, and keeps the full archive untouched. Interrupted or blocked tasks carry their checkpoint into the next ordinary input; `/new` clears task-scoped Skill, Plan, checkpoint, and result state. Legacy v1/v2 records remain readable.

`Snapshot.Conversation` 是 `limits.history_window` 控制的有界投影；`HistoryOffset`、`TotalMessages`、`HasMoreHistory` 和 `ConversationWindow` 描述当前窗口。持久化前按稳定 message ID 与已有完整 `SessionRecord` 合并，因此尾部窗口不会覆盖旧历史。流式 chunk 经 `StreamBatcher`/`BatchPipeline` 按条数或时间聚合，批次 flush 后才更新 Snapshot 并发布一个 `message.delta`；工具和 Interaction 事件前会先 flush，以保持事件顺序。

子代理事件由 `HandlePlanNodeComplete` 和 `HandleSubagentToolEvent` 投影到嵌套 `PlanNode`。节点生命周期发布 `subagent.changed`，内部工具活动按 ID upsert 到有界 `tool_events` 并发布 started/completed 增量；Snapshot 始终可以重建相同状态。

重点测试：`service_test.go` 覆盖 session/project/storage 用例，`command_test.go` 覆盖输入协议，`race_test.go` 覆盖并发与关闭。
