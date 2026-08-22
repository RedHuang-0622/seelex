# 关键数据流图

> 对应工作包：[README.md](README.md)。本文用字符画给出九条关键数据流：
> 主对话、Snapshot/Event 协议、Plan/子代理、上下文工程、持久化、插件/技能/
> MCP、账号路由、GUI 桥接、定时任务。每条流先给图，再给关键节点说明。

## 1. 主对话数据流（Submit → ReAct → 事件 → 前端）

```text
用户输入（TUI / GUI / backend console）
  │  app.Submit(ctx, input)
  ▼
application.Service.Submit
  │  input_router：/command │ #skill │ @plugin │ 对话
  ▼
startChat（service.Mu 锁内）
  ├─ 拒绝：closed / draining / Chat.Running
  ├─ 生成 requestID（chat-<unixnano>）
  ├─ BeginTask + ActivateTaskSkills + AppendTranscriptEvent(user)
  ├─ appendMessageLocked("user") + appendMessageLocked("assistant")
  └─ bump revision
  │
  ▼ 锁外
  ├─ EventMessageAdded(user, rev) + EventMessageAdded(assistant, rev)
  └─ go runChat(ctx, requestID, request)
            │
            ▼
runChat
  ├─ PrepareExecutionContext（seelexctx 装配 + 子代理 merge-back 注入）
  ├─ EnginePort.ChatStream(modelInput, onChunk)
  │     └─ frameworkSession.Session（ReAct Loop）
  │           ├─ LLM 调用（accountpool P2C 租约）
  │           ├─ 工具调用链：RegistryState
  │           │     → tools.Router（scoped）
  │           │     → ProjectScope.Resolve{Read,Write,Workdir}
  │           │     → PathGate（zone 策略）
  │           │     → PermissionGate（manual/full_access）
  │           │     → ApprovalBroker（human-in-the-loop）
  │           └─ telemetry Hook 链（Diagnostic → Stage → Summary）
  │
  ├─ onChunk → StreamBatcher → EventMessageDelta
  ├─ ToolHookBridge：OnToolStart/OnToolComplete
  │     → 消息 upsert + EventToolStarted / EventToolCompleted
  ├─ 终态：FinalizeTask / recordUnhandledTaskError
  ├─ PersistCurrentSession（SaveCommit → sessionstore）
  └─ 队列：inputQueue 合并 → 下一轮 runChat
  │
  ▼
EventSnapshotChanged（成功）/ EventError（失败）
  │
  ▼
EventHub.Publish → 订阅者（TUI/GUI Bridge）
  └─ 前端：增量 reducer 或 resync
```

关键点：

- 权威状态只在 `application.Service`（`Core.Snapshot`）内修改；前端拿到
  的是不可变 DTO 副本。
- `ChatStream` 全程在 runChat goroutine 内，事件回调（onChunk、工具钩子）
  由框架在回调线程同步触发，回调内部只发布增量、不重入引擎历史写操作。
- 队列续跑是“同一轮结束后立即起下一轮”，不引入中间提升队列
  （[core/chat.go](../../application/core/chat.go)）。

## 2. Snapshot / Event 增量协议数据流

```text
启动
  TUI: NewModel → app.Snapshot()（全量基线）
  GUI: Bridge.Start → seelex:ready（全量基线）
  ├─ 记录 snapshot revision floor
  └─ Subscribe(256) 订阅事件流

运行期
  application 状态迁移（锁内）→ bump revision
  EventHub.Publish(kind, revision, requestID, payload)
    ├─ publishMu 串行化 → seq++（进程内全局单调）
    └─ 复制订阅者列表 → 逐个 deliver
          ├─ 缓冲有空位 → 投递 Event
          └─ 缓冲满（慢消费者）→ drain 旧事件
                └─ 注入 EventResyncRequired

前端消费
  applyEvent(snapshot, event, lastSeq, snapshotRevisionFloor)
    ├─ protocol_version 不匹配 → 拒绝（需升级）
    ├─ seq 跳号（> lastSeq+1）→ 标记 needsRefresh → 重拉 Snapshot
    ├─ seq <= lastSeq → 丢弃（已应用）
    ├─ kind 不在增量白名单 → resync
    └─ 增量应用：
          message.added / tool.* → upsert conversation 消息
          message.delta → 追加 content
          runtime.changed → 替换 runtime 块
          worktable.changed / task.changed → 更新工作表格
          interaction.opened/closed → 设置/清空审批弹层
```

关键点：

- EventHub 是内存投递；跨进程（HTTP adapter）尚未实现，`seq` 单调性
  只在单进程内成立（v2 规划按 scope 独立 seq，见
  [docs/gui/architecture.md](../gui/architecture.md) §6）。
- 慢消费者只影响自己：drain + resync，不阻塞发布者与其他订阅者。

## 3. Plan / 子代理数据流

```text
主代理 ReAct 回合
  │ 模型调用 plan_load（goal skill 激活时可见）
  ▼
plan.Executor.PreparePlan / plan_load handler
  ├─ PlanPolicy 校验（effort：节点数/串并行/并发上限）
  ├─ codec.Import 归一化（nodes[]/edges[] → canonical DAG）
  └─ 拓扑校验（引用/环/连通性）
  ▼
plan_run（planToolProvider → workplan.NewFromPlan）
  │ 每个 kind:agent 节点：
  ▼
AgentNode（node.Coordinator）
  ├─ 独立 frameworkSession（NodeScope + PromptBlocks + 预算 + 账号路由）
  ├─ 父证据注入（Goal/Decisions/Findings + 继承块：project/stack/memory）
  ├─ 执行事实 → event.Sink → PlanNodeEvent（CSP channel）
  │     └─ application 消费者 → PlanState 投影 → Snapshot.Runtime.Plan
  ├─ 子代理工具活动 → SubagentToolEvent
  │     └─ application.HandleSubagentToolEvent → PlanNode.ToolEvents（有界）
  └─ merge-back：SubagentContextActor mailbox（固定容量，满则丢弃计数）
        └─ 父会话下一次 ChatStream 前 injectPendingSubagentContexts
  ▼
Plan 终态 → summary 节点 → final_output

fork_subagents（轻量入口）
  ├─ task 幂等登记（bindSubagentTask / retry 语义）
  ├─ 结果复用：全部命中已完成 task + 树快照 → 直接读回（省 token）
  └─ 构造 start → N agent → summary DAG → runPlan 同步等待
        └─ 外层工具在 DAG 结束前不返回（“Waiting for output…” 是预期）
```

关键点：

- 子代理是“独立 Session、不共享主会话状态”；并行分支确定性选账号
  （role + branchID hash，显式 binding pin）。
- `fork_subagents` 同步等待是设计决策而非死锁；大结果必须经
  `read_tool_result` / 节点详情读回，不能把外层 `final_output` 当
  无界传输通道（README 已声明）。

## 4. 上下文工程数据流

```text
冷启动/恢复
  DurableHistory.Load（tail 窗口）→ Provider → ContextSnapshot
    └─ GapCoverer：滑动窗口与压缩内容之间的真空区 → CoverHistoryGap
          └─ CompactStack 合并帧

请求期
  Assembler（system prompt + PromptBlocks + working history）
    └─ 查询 memory.Select（压缩帧 top-K）→ 「相关记忆」块
  → ChatStream
    └─ ContextAfterTool / ContextAfterAssistant 事件
          └─ ContextController.Handle
                ├─ 软阈值（预算 75%）：compressWindowOutside
                │     └─ Compactor → 帧 Evidence（含读回句柄）
                │           ├─ PushCompact（CompactStackStore 持久化）
                │           └─ ReplaceHistory（history_safety 配对修复）
                ├─ 硬阈值（预算 90%）：超大工具结果
                │     ├─ ToolResultProcessor → result_ref 归档
                │     └─ 仍超限 → WindowPolicy 收缩窗口（>= MinRounds）
                └─ 决策 → ContextDecision{ReplaceHistory, 投影历史}

读回/检索
  read_tool_result(result_ref)      → 事件库读回原始工具结果（分页）
  read_compressed_turn(segment_id)  → 压缩轮次原文（分页）
  search_history(query)             → memory.Select 相关帧
        └─ 帧 [From..To] 单元范围 → 事件库读回真实记录 → token 预算内内联
```

关键点：

- 预算公式：`Budget = Window - OutputReserve - SafetyReserve(12.5%)`；
  压缩目标 60%。System Prompt 与工作栈不参与历史压缩。
- 压缩丢失可逆：原文经 `Turns` 归档器持久化，帧携带 `segment_id`。

## 5. 持久化数据流

```text
runChat 收尾 → session_runtime.PersistCurrentSession
  ├─ ProviderHistory（框架历史，有界/可压缩）
  ├─ TranscriptEvent（append-only 用户可见事实）
  ├─ SessionRecord（state blob：title/PlanStack/Tasks/Execution/…）
  └─ ToolResults（immutable result_ref 对象）
  │
  ▼
Router.SaveCommit(Key{ProjectID, SessionID}, Commit)
  └─ 选中 backend：
        JSON     → generation-* 目录（100 条/shard）→ manifest.json 原子替换
        SQLite   → seelex_session_manifest + seelex_session_shard 事务
        PostgreSQL → 同上（pgx stdlib）
        Redis    → 同一 project hash tag 下 MULTI/EXEC 原子切换

恢复
  ResumeSession(sessionID)
    ├─ DurableHistory.Load → framework Session.ReplaceRawHistory
    ├─ SessionRecord → Conversation / Plan / Tasks 恢复
    └─ 成功提交完整 SessionRecord 后才释放 provider working history

双轨事件
  plan/节点执行事实（A 类）→ event.Sink → sessionstore 事件库（事实轨）
  llm/tool 意图-效果摘要（B 类）→ SummaryHook → 同库（脱敏）
  前端增量（快照轨）→ EventHub
```

关键点：

- 读者只看到“旧完整 generation”或“新完整 generation”，中途失败不会被发布。
- `framework-events.json` 是独立 append-only 文件，不随 generation
  rollover 失效（v1 `events.json` 首次写入时迁移合并）。

## 6. 插件 / Skill / MCP 数据流

```text
启动
  initPluginSystem
    ├─ plugin.Loader.LoadAll（plugins/*/plugin.md YAML front matter）
    └─ plugin.Manager.Load
          └─ Transaction（define → publish skills；失败逆序回滚）
  activateDefaultPlugin("default")
    └─ Manager.Activate：
          ├─ attachLocked（按 plugin__server 名 attach MCP；失败不动旧插件）
          ├─ Transaction（activate tools → activate skills → detach 旧）
          └─ 任一步失败 → 逆序回滚 + restoreToolPluginLocked(previous)

运行期自迭代
  switch_plugin / switch_mode（兼容别名）
    └─ Manager.Activate/Deactivate → applyPluginPrompt（system prompt）
  plugins_reload
    ├─ Loader.LoadAll → DiffState（added/removed/updated）
    ├─ 事务式应用（删除/新增/修改；active 插件先停后启）
    └─ 失败 → 快照回滚（plugins/current/attached 全量恢复）
  plugin_create / skill_create
    └─ 只写磁盘 → 必须 plugins_reload 才生效

MCP
  accounts.yaml mcp_servers 段 → RegisterLazyMCP（冷启动零进程）
    └─ mcp_load(server_name)
          ├─ spawn + initialize + tools/list（30s 超时）
          ├─ 工具注册进 RegistryState
          └─ mcpstack 中间件记录每次调用 trace（append-only + 熔断事件）
  plugin 内 mcp_servers → AttachMCPServer（plugin__server 运行时名）
```

关键点：

- 插件可见性过滤发生在每次请求的工具快照上（`bridge.WithVisibilityPolicy`
  + `plugin.Manager.Filter`），避免正在执行的请求看到半新半旧能力。
- MCP 冷启动策略把启动路径的进程开销降到零，需要时再按名加载。

## 7. 账号路由数据流

```text
config/accounts.yaml
  └─ seelebridge/internal/config.Load → AccountSpecs + AccountLimits
        ├─ accountpool.P2CPool[agent.Completer] 注册（P2C 租约）
        └─ account.Manager（选中账号/provider/限额/选择器）
              ├─ 同步请求：ClientFor（round-robin / 确定性选择）
              ├─ 流式请求：streamingAccountCompleter
              │     └─ 租约保持到 EOF / 错误 / 显式 Close
              └─ Plan 分支：ResolveAccountForBranch
                    ├─ role + branchID 确定性 hash
                    └─ 显式 AccountID binding → pin
  │
  ▼
application.RuntimePort.Accounts/SelectAccount
  └─ GUI/TUI 账号面板 → Snapshot.Runtime.Accounts
```

关键点：

- 流式租约防止响应中途被其他请求抢占或切换账号；分支 hash 让相同 DAG
  的账号路由可复现（[seelebridge/account](../../seelebridge/account/README.md)）。

## 8. GUI 桥接数据流

```text
启动
  gui.Run(app, Options)
    └─ Bridge.Start(ctx, emit)
          ├─ seelex:ready（全量 Snapshot）
          └─ 循环转发 seelex:event（订阅 EventHub）

用户操作
  前端调用 Wails 绑定方法 → Bridge 方法映射
    ├─ Submit → app.Submit
    ├─ BeginNewSession / ResumeSession → session 生命周期
    ├─ CancelChat（requestID 失效时重试 ""）
    ├─ ResolveInteraction → ApprovalBroker.Resolve
    ├─ CreateWorkspace/BindWorkspace → workspace.Repo + ProjectScope.Bind
    ├─ UpdateWorkItemStatus → RuntimePort.SetTodoStatus
    ├─ ScheduleTask/CancelScheduledTask → scheduler actor
    └─ SearchHistory → seelexctx/search

子代理详情
  SubagentDetailStreamStart(nodeID)
    ├─ 返回历史回放（start 以来有界缓冲）
    └─ 转发 seelex:subagent_live（阶段/工具事件即时输出）
          └─ 重复启动同 node → 先停旧流（幂等）

关闭
  BeforeClose → BeginGracefulShutdown
    ├─ 无运行中 Chat → 直接 quit
    ├─ 运行中 → WaitForIdle(5s)
    │     └─ 超时 → CancelChat("") → quit
    └─ quit 恰好一次（closeCoordinator）
```

关键点：

- Bridge 不维护业务状态：所有查询走 `app.Snapshot()`，所有变更走
  application 端口；前端 reducer 在 `protocol.js` 内实现增量合并。
- 关闭路径有界（5s），避免工具/审批/provider 挂起导致窗口无法关闭。

## 9. 定时/周期任务数据流

```text
GUI 新建弹窗（白名单命令 / prompt 任务）
  └─ Bridge.ScheduleTask → application.Service.ScheduleTask
        └─ RuntimePort.ScheduleTask → scheduler.State（ticker 200ms 单循环）
              ├─ command 任务：WorkingDir + Argv（如 auto_get_jobs main.py）
              │     └─ 超时默认 10min / 配置 30min
              ├─ prompt 任务：ScheduledPromptExecutor
              │     └─ 会话绑定校验（sessionID 必须匹配当前主会话）
              │           └─ app.Submit(prompt) → 主会话异步执行
              └─ 状态机：pending → running → ok/failed/skipped
                    └─ 状态快照 → SchedulerObserver
                          └─ app.RefreshRuntimeSnapshot
                                ├─ runtime.changed 事件 → GUI 面板
                                └─ publishTaskDeltas（工作表格增量）
```

关键点：

- 调度器是单消费者 actor；命令任务经 `security.CommandSandbox` 路径约束
  cwd，prompt 任务复用主会话（显式绑定会话避免切换后误投递）。

