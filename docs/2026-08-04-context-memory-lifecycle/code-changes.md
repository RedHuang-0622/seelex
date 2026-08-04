# 代码变更摘要

## 结论

本工作包已把 DurableHistory、有界 Snapshot、流式批处理、子代理事件链和前端窗口接入实际应用。Durable storage 是真相源，Snapshot 是有界投影，Event 是连续增量；主代理工具和子代理工具保持独立投影路径。

## 新增/修改文件

| 模块 | 类型 | 说明 | 设计方式 |
|---|---|---|---|
| `seelexctx/lifecycle` | 修改 + README | 修复 Actor/Pipeline 并发关闭、flush 基线和关闭后 Snapshot；补 race/边界测试。 | Actor、生命周期门、有界管道 |
| `seelebridge` | 新增/修改 | DurableHistory Router、指定 Session ID、子代理工具 middleware、worktree phase 事实和回调。 | Adapter、Middleware、Event projection |
| `application/core` / `model` / `event` | 新增/修改 | 有界 Conversation、完整 SessionRecord 合并、批次 delta、子代理节点/工具 upsert 与事件 DTO。 | Durable owner、Snapshot projection、EventHub |
| `main.go` / `application_adapters.go` | 修改 | 在首个 Session 前装配 Router，持久化成功后释放 working history，注册子代理工具回调。 | Composition root、DI |
| `gui` / `gui/frontend` | 新增/修改 | Bridge relay、递归 Plan reducer、worktree 状态、工具详情、变高 DOM + history sentinel；Wails runtime 事件监听改为就绪后幂等绑定，避免静默进入无事件模式。 | Thin bridge、copy-on-write reducer、keyed DOM、Binder |
| `tool_full_chain*_test.go` / frontend event tests | 新增/修改 | mock 与真实账号覆盖 `full_access bash → Application event → provider follow-up`；前端覆盖 `seelex:event → reducer → tool card` 和 runtime 延迟就绪。 | Production composition harness、event-chain mock |
| `docs` / module README | 修改/新增 | 更新两份设计工作包、模块边界、验证命令和事件链事实。 | 文档即当前实现事实 |

## API 与协议变更

| API/字段 | 变更 | 兼容性 |
|---|---|---|
| `Runtime.AttachHistoryRouter` | 独立注入 provider durable history Router。 | 新增 optional 装配；未注入保持内存 history。 |
| `Runtime.NewMainSessionWithID` | 恢复时显式指定框架 Session ID。 | `NewMainSession` 保留。 |
| `Runtime.SetSubagentToolCallback` | 订阅子代理工具 started/completed。 | 新增 optional callback。 |
| `Snapshot` history metadata | 新增/使用 `conversation_window`、`history_offset`、`total_messages`、`has_more_history`。 | optional JSON 字段；旧客户端可忽略。 |
| `PlanNode.tool_events` | 有界子代理工具活动。 | optional JSON 字段。 |
| Event kinds | `subagent.changed`、`subagent.tool.started`、`subagent.tool.completed`。 | 协议版本保持 v1，前端已同步 reducer。 |
| Node status | `worktree_creating`、`rebasing`、`merging`。 | 前端 label/symbol/style 已同步。 |
| `createRuntimeEventBinder` | 新增 renderer 内部 binder；runtime 未就绪返回 false，绑定成功后保持幂等。 | 内部模块，无 Bridge/API 协议变化。 |

## 接口抽象

| 接口/边界 | 实现方 | 使用方 |
|---|---|---|
| `lifecycle.Storage[T]` | memory/discard/stream adapter | `ContextActor`、`BatchPipeline` |
| `sessionstore.Router` → `DurableHistory` | `sessionstore` | `seelebridge.Runtime` |
| `SetSubagentToolCallback` | Runtime callback state | Application Service |
| Application Event/DTO | `application/event`、`application/model` | GUI Bridge、frontend reducer |

## 循环依赖检查

- [x] `go build ./...` 与 GUI production-tag build 通过，无新增循环依赖。
- [x] frontend 仍只消费 Application DTO/Event；Seele 类型集中在 bridge/adapter。

## Commit 记录

本工作包经用户明确授权提交，实际 commit 标识以 Git 历史为准。

`feat(runtime): wire durable context and subagent events`

## GUI permission/event 链补强（2026-08-04）

- `RuntimePort`/`RuntimeState` 新增权威 `FullAccess/full_access`，GUI 不再维护本地 FA 镜像。
- `ApprovalBroker.ResolveAll` 在开启 Full Access 时释放所有已经等待的 permission request；Application 发布完整 `runtime.changed`。
- 默认 manual 白名单加入 `todolist_*` 和 task 终态控制工具，避免正常 Agent 编排被无意义审批阻塞。
- 启动配置始终先安装 manual 基线，再按 CLI/GUI 开启 Full Access 覆盖层，确保能够切回 manual。
- `gui/tool_full_chain_test.go` 以 GUI `Bridge` 为入口和出口，覆盖工具完成事件、FA 解锁审批、Snapshot 与 `seelex:event` relay。
