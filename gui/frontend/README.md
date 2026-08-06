# GUI Frontend

## 模块定位

本目录包含被 Go `embed.FS` 打包进 Wails 的前端源码。当前没有 bundler；`dist/` 就是可维护源文件和生产资产，而不是可随意删除的生成目录。

## 结构

| 文件 | 职责 |
|---|---|
| `dist/app.js` | DOM 绑定、Bridge 调用、工作区/session/runtime/settings 编排。 |
| `dist/client-state.js` | Snapshot/Event reducer、seq gap 和 resync。 |
| `dist/runtime-events.js` | Wails `EventsOn` 就绪探测、幂等绑定与 ready/event 转发。 |
| `dist/conversation-view.js` / `chat-view.js` | 变高 keyed conversation、顶部 history sentinel 与 chat activity 渲染。 |
| `dist/components.js` | message/tool/queue 等纯渲染组件。 |
| `dist/plan-dsl.js` | Plan JSON DSL 归一化、DAG → 树状布局渲染、子代理树视图、节点详情弹窗。 |
| `dist/todo-view.js` | todolist 待办面板渲染（数据源 `runtime.todo_items` 权威投影）。 |
| `dist/scheduled-tasks-view.js` | 定时周期任务面板渲染（数据源 `runtime.scheduled_tasks` / `runtime.scheduled_commands` 权威投影）。 |
| `dist/read-sources.js` | 从会话工具事件中收集成功完成的 `read_file` 路径，供右侧栏显示。 |
| `dist/markdown.js` | 安全 Markdown、think block 和 URL 过滤。 |
| `dist/effort-control.js` | Effort selector 状态与 rollback。 |
| `dist/protocol.js` | protocol version 校验、conversation window 和递归 Plan 增量 reducer。 |
| `dist/*.test.mjs` | Node 内置 test runner 契约测试。 |

## 状态流

1. 初始化先等待并幂等绑定 Wails `EventsOn`，再通过 Bridge `Snapshot` 获取权威状态；runtime 尚未就绪时整个初始化按既有重试机制继续，不能静默进入无事件模式。
2. `client-state` 应用连续 `seelex:event` 增量。
3. seq gap、协议不兼容或未知状态触发完整 Snapshot resync。
4. render functions 根据 state 投影 DOM；所有 mutation 通过 Bridge 返回 Application。

Full Access 按钮不维护本地布尔状态：显示与下一次 toggle 都读取 `snapshot.runtime.full_access`，调用 `Bridge.SetFullAccess` 后重新拉取 Snapshot；后端同时发布完整 `runtime.changed` 供连续事件链更新。

事件是状态更新的快速路径；在 `chat.running=true` 期间，`active-chat-sync.js` 每秒从 Bridge 拉取一次权威 Snapshot 作为有限对账。它只用于纠正桌面 WebView 丢失某个 terminal event 后遗留的 `RUN`/`Waiting for output…`，Snapshot 显示 idle 后立即停止。

Plan DSL 常驻右侧项目栏；没有 Plan 时隐藏整个 section，加载、运行和完成状态都由 `runtime.plan` 驱动。Runtime 弹窗只保留运行时诊断信息。

todolist 待办面板同样常驻右侧栏：数据来自 `snapshot.runtime.todo_items`（application 经 `runtime.changed` 增量携带），清单为空时隐藏整个 section；条目勾选状态只读权威 JSON 的 `done` 标志，渲染不做本地猜测。

## 定时周期任务

右侧栏「定时任务」section 常驻（含「新建周期任务」按钮）：数据来自 `snapshot.runtime.scheduled_tasks`（seelebridge 调度器状态变化经 observer → `RefreshRuntimeSnapshot` → `runtime.changed` 增量投影，见 `seelebridge/scheduler.go` 与 `application/core/service_scheduler.go`）。任务渲染只读展示：名称/类型/启用状态/下次运行/上次结果/日志尾部，取消按钮以 `data-sched-cancel` 携带任务 ID 并调用 `Bridge.CancelScheduledTask`。

新建弹窗的字段由 `Bridge.ScheduleTask` 提交（`scheduled-tasks-view` 不直接持有 Bridge）：类型分「命令」与「提示词」两种。

- **命令任务（主路径）**：下拉选项来自 `snapshot.runtime.scheduled_commands`（后端编译期白名单，`main.go` 登记 `auto_get_jobs`，指向 `local/tools/auto_get_jobs/main.py`）。白名单命令的 argv 固定、不经 shell 展开，前端无法注入任意命令。脚本依赖（`.env`、`user_requirements.txt`、`city_list.json`、chromedriver）均按其自身目录解析，调度器只提供固定工作目录与超时。
- **提示词任务（扩展点）**：提交后由后端注入的 executor 触发一次 agent 会话（main 装配为 application Submit，排队语义：会话忙时任务排队，不与进行中的对话冲突）。任务绑定当前 main session（`session_id` 留空 = 执行时当前会话；显式绑定会在会话切换后跳过而非误投）。结果回传为「已提交」状态字；异步会话的完整输出请从会话记录/事件库查询，这是当前实现的有意取舍。

周期以分钟为单位（前端下限 1 分钟，后端另设最小周期校验）。任务状态、白名单命令均为公开元数据，不含 secret；渲染文本全部 escape。

`Snapshot.Conversation` 是后端提供的有界窗口；增量 reducer 继续按 `conversation_window` 截断。消息 DOM 使用真实内容高度的 keyed reconciliation，顶部 sentinel 接近视口时调用 `LoadMoreHistory` 并用 anchor 恢复滚动位置，不使用 `virtual-list.js` 的固定行高模型。

子代理增量递归更新 `runtime.plan.nodes`：`subagent.changed` 替换完整节点，工具 started/completed 按 ID upsert `node.tool_events`。Plan 支持 `worktree_creating`、`rebasing`、`merging`；节点整卡可打开详情，详情弹窗显示会话、节点时间线和工具输入/结果/错误。Plan 面板与详情页都渲染功能打点表：tasklist 的 `task_check_node` 检查点和子代理工具活动均由同一事件投影驱动。所有更新都先深拷贝 Plan 树，避免修改旧 Snapshot。

详情弹窗另有「上下文」标签：展示 `SubagentSessionDetail` 返回的子代理结构化上下文快照（Goal/Progress/Findings/Decisions/Constraints/PendingWork/MessageCount/TokenEstimate，运行中实时导出、结束后快照），与会话记录、工具活动共同构成运行过程的可核验证据面；快照只含公开证据，不含 prompt 原文或秘密。

Plan 渲染是树状布局（不是扁平 DAG）：`plan-dsl.js` 的 `layoutPlanTree` 从无入边根节点做 Kahn 拓扑分层（level = 到根最长路径），节点按层级缩进 + 引导字符连线（`├─`/`└─`/`│`）呈现父子关系；多入边节点（菱形 join）采用「主路径树 + 旁路标记」策略——树父节点取入边源中层最深者，其余入边渲染为「旁路」chip 引用，节点只出现一次、不死循环。无 edges 的 Plan（纯 children 嵌套）保留旧缩进契约；Kahn 未访问的环内节点按根扁平处理（环防御，深度有界）。节点卡片能力（打点表、详情弹窗、工具活动）原样保留。

右侧栏 Plan section 下另有「子代理树」视图：`fork_subagents` 创建的子代理以 parent/child 链组织（数据源 `snapshot.runtime.subagent_tree`，权威 Snapshot 增量携带；后端内存态，不落盘）。树节点行显示状态着色（running/done/failed）、goal、紧凑上下文（消息数/token 估算/发现，有界截断）、会话摘要与子会话 ID，整行可点开详情弹窗；fork 子代理不在活跃 Plan 里时（计划已清除）详情弹窗回退到子代理树投影数据，会话记录/上下文仍由 `SubagentSessionDetail` 承载。

`fork_subagents` 的外层工具在 summary 完成前保持运行态；这时应点击 Plan 节点查看真实进度，不能仅以 `Waiting for output…` 判定卡死。若外层工具报告子代理结果过大，详情中的会话、功能打点和工具活动才是可核验的证据面；renderer 不把过大的 `final_output` 当作完整审查结果的替代品。

点击新建会话只调用 `BeginNewSession` 进入编辑草稿：允许选择项目和编辑输入框，但左侧列表不新增任何条目，也不生成临时 ID。第一次提交真实对话后，Application 返回真实 ID，左侧才新增正式 Session，并以首个问题作为列表标题。

## 安全和身份规则

- 所有模型/工具/用户文本在进入 HTML 前 escape 或经过受控 Markdown renderer。
- 禁止执行 raw HTML、危险 URL 或任意脚本。
- session/project 名称只显示；按钮 `data-session`、`data-ws` 必须保存 ID。
- draft session 没有 ID、没有左侧列表行，不允许触发 resume/delete/binding；物化后列表行为仍只使用真实 ID。
- DSN、API key 等秘密不能进入 renderer state。
- system prompt、其装配结果和层摘要不能进入 renderer state；Runtime 面板只显示模型、Provider、Plugin、Effort、工具和 Plan 等可公开诊断信息。

## Review 指南

- Event delta 是否可能重复应用或越过 revision floor。
- DOM key 是否稳定，streaming 时是否无意义重建大列表。
- `innerHTML` 数据是否全部 escape。
- Plan 点状态更新是否来自权威 JSON，而非仅靠 CSS 本地猜测。
- 新 Bridge 调用是否处理 rejection 并恢复 optimistic UI。

## 测试

```text
Get-ChildItem gui/frontend/dist -Filter *.js | ForEach-Object { node --check $_.FullName }
node --test gui/frontend/dist/*.test.mjs
go test ./gui -count=1
```

`runtime-events.test.mjs` 验证 Wails runtime 延迟就绪时不会漏绑或重复绑定。`event-chain.test.mjs` mock Wails `seelex:event` 并验证主代理/子代理工具完成状态和权威 `runtime.full_access` 通过 `createGUIClient`/`protocol.js` 后可见，且连续事件不会退化为 Snapshot reload；主代理工具卡明确断言完成后不再显示 `Waiting for output…`。

## Context compression summary

The project overview renders `task.context_compactions` as a small timeline of successful context compressions. The frontend receives only public metadata (version, reason, counts, and time); it does not receive private checkpoint content, prompt text, tool payloads, or raw conversation history.

The “Agent read files” panel also merges the live successful `read_file` calls with the persisted `read_files` cache. It therefore remains useful after context compression or session restoration without retaining file content in renderer state.
