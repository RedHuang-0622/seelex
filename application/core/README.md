# Application Core

## 模块定位

`core` 是 Seelex 的应用用例层和权威状态机。它不直接创建数据库、Wails 窗口或 Seele Agent，而是通过 `contract.Dependencies` 编排这些能力。

主要调用方是 `application` facade；主要消费者是 TUI、GUI Bridge 和 E2E harness。

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
| `context_controller.go` | 基于 token 与 checkpoint 的工具结果裁剪；内部控制消息在持久化前清除。 |
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

每个请求另有私有 `TaskExecutionState`：工具结果与 Plan 节点状态写入有界 `NodeCheckpoint`，当历史 token 或工具输出过大时 `ContextController` 以 checkpoint 摘要替换冗长工具结果。连续无新事实、变更、产物或节点状态的工具轮次会触发预算兜底，但不是上下文管理主路径。模型应以 `task_complete`、`task_needs_user_decision` 或 `task_failed` 结束工具型任务；它们分别表示可交付完成、必须由用户选择的有效分歧，以及有界失败事实。`Snapshot.Task` 公开 `progressing/completed/needs_user_decision/blocked/interrupted/failed`，而 checkpoint、装配的 system prompt 和 `<think>` 内容都不进入 frontend snapshot。Provider 返回 context overflow 或 504 时，Service 保存私有恢复 checkpoint；504 从不自动重放可能已有副作用的工具轮。

## Session 与 Project 语义

- project 只定义会话的文件读写范围，不共享 conversation history。
- session ID 是唯一键；显示名来自首个用户问题，允许重复。
- `BeginNewSession` 保存旧的非空历史并清空 Engine history，然后只进入幂等 draft：不生成 ID、不写入空 Session、不建立 workspace binding；第一次进入 `submitConversation` 时才调用 `StartSession`，并立即用首问设置显示名。
- workspace ID 是 binding 与 storage shard 的键；显示名来自 root basename。
- 恢复 session 时先定位真实 `workspaceID + sessionID`，再读取历史和绑定 Runtime。
- 有历史的 session 切换 project 时先保存旧 scope，然后创建新 session，禁止把同一 ID 重新绑定后继续写。
- `sessionCatalog` 跨所有项目聚合 metadata；标题按 updatedAt 缓存，存储切换和删除时失效。

## Snapshot/Event 协议

每次状态变化先在锁内 bump revision，再在锁外 Publish。Snapshot 可独立重建全部 UI；Event 只负责低延迟增量。Message ID 由 Service 生成并在 history prepend 时保持稳定。

## Plan 集成

Tool hooks 把 `plan_load` JSON 转为 Plan DAG。当前 authority 阶段把 DAG 作为主 Agent 的权威检查表：主 Agent 使用正常的 project-scoped ReAct tools 执行，而不调用缺少工具和上游产物注入的 `plan_run` 子聊天。`task_complete` 必须枚举所有完成的 Plan 节点才会把 Plan 标记完成。`replan` 只基于失败原因、旧 Plan 和已完成节点证据加载一个原子替换的恢复 Plan；它不自动调用 `plan_run`，保留用户复核副作用的边界。

Medium/High/Max 的成功 preflight 会先加载 canonical DAG，再由当前 request ID 获取独占的 `PlanAuthorityLease`。该 lease 存在期间，普通 ReAct 只可执行或读取已加载 Plan，不能替换或清空；同一 Runtime 的第二个 authority 请求会 fail closed。ChatStream 返回时释放 lease，随后用户显式选择的 replan 才可加载恢复计划。

For Medium/High/Max, `chat.go` acquires an exclusive request-ID-bound
`PlanActScope` before preflight. Only its private context may load the Plan;
after promotion, normal ReAct and stale `plan_load`/`plan_clear` handlers are
both prevented from mutating it. The scope is released after ChatStream, before
an explicit replan may load a recovery DAG.

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

重点测试：`service_test.go` 覆盖 session/project/storage 用例，`command_test.go` 覆盖输入协议，`race_test.go` 覆盖并发与关闭。
