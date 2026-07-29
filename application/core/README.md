# Application Core

## 模块定位

`core` 是 Seelex 的应用用例层和权威状态机。它不直接创建数据库、Wails 窗口或 Seele Agent，而是通过 `contract.Dependencies` 编排这些能力。

主要调用方是 `application` facade；主要消费者是 TUI、GUI Bridge 和 E2E harness。

## 文件结构

| 文件 | 职责 |
|---|---|
| `service.go` | Service 生命周期、Submit 分派、Runtime/Workspace/Session 用例和 Snapshot。 |
| `chat.go` | 流式聊天、输入队列、工具事件、Plan 状态打点和 idle/draining。 |
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

`BeginGracefulShutdown` 拒绝新输入但允许已接受工作完成；`Shutdown` 才取消 active chat 并关闭 broker/events。

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

Tool hooks 把 `plan_load` JSON 转为 Plan DAG，把 `plan_run` node/branch 回调映射为 `PlanState`。失败节点可以打开 retry/replan/skip/abort Interaction。`replan` 只基于失败原因、旧 Plan 和已完成节点证据加载一个原子替换的恢复 Plan；它不自动调用 `plan_run`，保留用户复核副作用的边界。Plan branch binding 携带 session/workspace/account/trace/plan IDs，避免分支结果失去归属。

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
