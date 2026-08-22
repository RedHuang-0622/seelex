# 分层解读与架构图

> 对应工作包：[README.md](README.md)。本文先给总体字符画架构，再逐层
> 解读职责、关键文件与边界。所有路径相对仓库根；相对链接指向既有文件。

## 1. 总体架构（字符画）

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│                            Frontends（前端层）                                │
│                                                                              │
│   TUI                    GUI                      backend console            │
│   tui/ (bubbletea)       gui/ (Wails/WebView2)    application/console/       │
│   Model/Update/View      bridge.go + dist/*.js    EventLogger + Start        │
└───────────┬────────────────────────┬──────────────────────────┬──────────────┘
            │ Snapshot/Event/Action  │                          │
            ▼                        ▼                          ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                        application/（应用层 = 业务核心）                       │
│                                                                              │
│  contract/ports.go（消费方定义端口）     model/state.go（权威 Snapshot DTO）    │
│  event/hub.go（增量事件）               approval/broker.go（异步审批）         │
│  prompt/（PromptStack/Effort）          core/Service（门面 + 装配）            │
│                                                                              │
│  application/core 域子包：                                                   │
│  chat · task_context · session_runtime · context_runtime · view_state ·      │
│  subagent_view · input_router · worktable · prompt_layer · internal/state    │
└───────────┬──────────────────────────────────────────────┬───────────────────┘
            │ 实现 contract 端口                             │ 消费上下文能力
            ▼                                              ▼
┌───────────────────────────────┐          ┌──────────────────────────────────┐
│ internal/adapters（适配层）     │          │ seelexctx/（上下文工程层）          │
│ EnginePort（ReactorEngine 工厂）│          │ RequestAssembler · ToolResult     │
│ RuntimePort · PluginPort ·    │          │ Processor · Compressor ·          │
│ SkillPort · SessionPort ·     │          │ ContextController · WindowPolicy  │
│ WorkspacePort                 │          │ Compactor/Merger/Memory/Search/   │
│                               │          │ Lifecycle · snapshot/provider     │
└───────────┬───────────────────┘          └───────────────┬──────────────────┘
            │                                              │
            ▼                                              ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                    seelebridge/（防腐层 = Runtime 装配域）                    │
│                                                                              │
│  runtime.go（NewRuntime 拓扑序装配 / Shutdown 逆序）  ports.go（端口委托）      │
│  tools/router.go（scoped 工具：read/grep/glob/write/edit/bash）               │
│  security/（ProjectScope · PathGate · CommandSandbox）                       │
│  plan/（Executor/ToolProvider/Preflight/ReplanGuard）  fork/（fork_subagents）│
│  node/（kind:agent 节点）  session/（子代理注册表/树/merge-back actor）         │
│  task/（worktable 注册表 actor）  scheduler/（定时任务 actor）                  │
│  account/（账号路由）  mcp/（MCP 生命周期）  plugin/（可见性过滤）              │
│  worktree/ · fs/ · internal/（config/model/actor/mapper/stream/telemetry）    │
└───────┬────────────────────────────────────────────────┬─────────────────────┘
        │                                                │
        ▼                                                ▼
┌───────────────────────────┐            ┌─────────────────────────────────────┐
│ sessionstore/ workspace/  │            │ plugin/ skill/ mcpstack/             │
│ session/（持久化域）        │            │ （声明式扩展域）                      │
│ Router/Repository ·        │            │ plugin.Manager（事务式切换）          │
│ JSON/SQLite/PG/Redis ·     │            │ skill.Registry（目录化 SKILL.md）     │
│ workspace_index.json ·     │            │ mcpstack（MCP 调用 trace 中间件）      │
│ SessionRecord/EventStore   │            │                                      │
└───────────┬───────────────┘            └──────────────────────────────────────┘
            │
┌───────────▼──────────────────────────────────────────────────────────────────┐
│                        Seele v0.1.2（上游 Agent Runtime）                    │
│  agent/session/tools/workplan/seelectx/accountpool/event/telemetry/types     │
└──────────────────────────────────────────────────────────────────────────────┘

组合根 main.go 把上述全部按拓扑序装配：Runtime → 工具 → 插件 → 存储 →
事件 → 审批 → EnginePort → Application → 前端。
```

依赖方向总结（只允许自上而下，禁止反向）：

```text
前端 (tui/gui/console) ──▶ application ──▶ internal/adapters ──▶ seelebridge
                                                        │
              seelexctx ◀──(适配依赖)────────────────────┘
                                                        │
              sessionstore/workspace/session ◀──────────┘
              plugin/skill/mcpstack ◀───────────────────┘
                                                        ▼
                                               Seele v0.1.2
```

## 2. 第 0 层：组合根 `main.go`

- 职责：纯装配。解析 flag（`-frontend`/`-store`/`-plugins`/
  `-permission`），按拓扑序构造 Runtime、Skill、Plugin、Store、EventHub、
  ApprovalBroker、EnginePort、SessionManager、Application，注册产品工具，
  最后启动 TUI/GUI/backend console。
- 关键子流程：
  - `initRuntime`：读 `config/seelex.yaml`（window/limits 段）→
    `seelebridge.NewRuntime`。
  - `initPluginSystem`/`initSkillSystem`：插件加载 + default 激活。
  - `initStore`/`initWorkspaceRepo`：`sessionstore.Router` +
    `workspace.Repo`（`session-storage.json` + `workspace_index.json`）。
  - `initEngine`：`runtime.NewMainSessionWithID` 懒创建主会话；
    启动期不建 Session，保持 cold draft（注释明确说明）。
  - `registerProductTools`：get_time / web_search / mcp_load /
    switch_plugin / 插件自迭代工具族 / ask_approve / 上下文读回工具 /
    project_refresh / task 终态工具。
  - `runtime.ApplyDeps` / `appEngine.ApplyDeps`：装配期一次性注入
    （EventPersister、SubagentToolCallback、SchedulerObserver 等）。
- 值得注意：
  - `resolveStorePath` 用 `.lock` + PID 探测实现多实例自动递增路径后缀，
    避免两个实例写同一会话目录。
  - `setupPermissionGate` 始终先装 manual 基线，再叠加 full_access 覆盖，
    保证 GUI 可切回。
  - 大文件（1143 行）且内联大量工具 schema/handler，见
    [04-tech-leader-qa.md](04-tech-leader-qa.md) Q2。

## 3. 第 1 层：前端层（tui / gui / console）

### 3.1 TUI（`tui/`，bubbletea）

- 职责：终端交互。Model 只持有 `app.Snapshot()` 副本 + 订阅 + 纯 UI 状态
  （viewport、textarea、粘贴折叠、历史输入）。
- 事件循环：`Init` → `waitApplicationEvent` → `Update`（按 Event 刷新
  snapshot）→ `View` 渲染。`Chat.Running` 时附加 3 秒 tick 心跳刷新。
- 命令处理：`/command`、`#skill`、`@plugin` 触发 Suggestions；Enter 提交
  到 `AppController.Submit`；审批交互在 `Interaction` 打开时拦截按键。
- 边界：不复制业务状态机；`AppController` 接口由 `application.Service`
  直接满足（[tui/tui.go](../../tui/tui.go)）。

### 3.2 GUI（`gui/`，Wails + WebView2）

- `Bridge` 是 headless application → 桌面安全的窄适配：
  - `Start`：订阅 EventHub，先发 `seelex:ready`（全量 Snapshot），随后
    逐条转发 `seelex:event`。
  - 方法映射：Submit/BeginNewSession/ResumeSession/CancelChat/
    ResolveInteraction/SelectAccount/SwitchEffort/SwitchPlugin/
    CreateWorkspace/BindWorkspace/UpdateWorkItemStatus/ScheduleTask/
    SearchHistory 等。
  - 子代理详情：`SubagentDetailStreamStart` 返回历史回放 + 订阅
    `seelex:subagent_live` 实时通道（[gui/bridge.go](../../gui/bridge.go)）。
- 前端（`gui/frontend/dist/*.js`）：`protocol.js` 实现协议校验与增量
  reducer（`applyEvent`）；`app.js` 实现分区渲染、工作表格、Plan 详情、
  定时任务面板、历史检索等。**dist 即源码，无 src/ 构建链**（见 Q3）。
- 优雅关闭：`closeCoordinator.BeforeClose` →
  `BeginGracefulShutdown` → `WaitForIdle(5s)` → 超时则 `CancelChat` →
  `quit` 恰好一次（[gui/shutdown.go](../../gui/shutdown.go)）。

### 3.3 backend console（`application/console/`）

- 诊断前端：`EventLogger` 记录 startup 阶段 + 工具钩子事件；
  `Start` 从参数或 stdin 逐行 Submit，等待 idle 后输出事件流。
- 用途：无 UI 环境下验证后端链路与工具行为，可绑定显式项目根
  （无隐式 cwd fallback，避免误授权）。

## 4. 第 2 层：application（应用层/业务核心）

### 4.1 端口与 DTO

- `application/contract/ports.go`：消费方定义的端口。
  `ChatEngine`（History/ReplaceHistory/StartSession/Node* 查询）、
  `RuntimePort`（模型/账号/可见工具/插件/todo/task/plan/调度器/历史检索）、
  `PluginPort`/`SkillPort`/`SessionPort`/`WorkspacePort`/`ApprovalBroker`。
- `application/model/state.go`：`Snapshot` 是权威 DTO（protocol v1），
  含 Session/Sessions/Conversation/Chat/Task/Runtime/Interaction/
  Capabilities/Workspaces；`SessionRecord` 是持久化记录；
  `RuntimeState` 携带 Plan/WorkTable/SubAgentTree/Todo/ScheduledTasks。
- `application/event/hub.go`：进程内 EventHub。`Publish` 在 publishMu
  串行化下分配全局单调 `seq`；慢订阅者被 drain 并收到
  `EventResyncRequired`（不阻塞发布者）。

### 4.2 core 装配与状态内核

- `core/Service` 是门面：`Submit`/`Snapshot`/`Subscribe`/`SwitchPlugin`/
  `ResolveInteraction`/`BeginNewSession` 等。
- `core/serviceAssembler` 把域子包接线成一个 `Service`：
  task_context（任务状态机/ReAct 预算/转录）、prompt_layer（system
  prompt 组装）、session_runtime（会话生命周期/持久化）、
  context_runtime（执行上下文准备/上下文控制失败恢复）、view_state
  （Snapshot 读写/投影）、subagent_view（子代理详情/树）、input_router
  （/命令、#skill、@plugin、对话分流）、worktable（工作表格汇聚发布）。
- 状态内核 `core/internal/state.Core`：`Mu`（RWMutex）+ 权威 Snapshot +
  注入的 Deps/Events/Approval。域子包以嵌入/注入共享它，避免循环依赖。
- 关键工作流（[core/chat.go](../../application/core/chat.go)）：
  `startChat`（锁内建 TaskState/消息/rev bump，锁外发布事件并
  `go runChat`）→ `runChat`（PrepareExecutionContext → ChatStream →
  工具钩子增量 → 终态/错误 → PersistCurrentSession → 队列续跑）。

### 4.3 事件双轨

- 快照轨：`EventHub` → 前端增量（内存、可丢、resync 兜底）。
- 事实轨：`sessionstore.NewEventStore(store).Append` ←
  `seelebridge` 的 plan/节点执行事实（A 类）+ 脱敏遥测摘要（B 类），
  落盘供恢复/审计。装配点在 main.go 中显式注释（“双轨事件”）。

## 5. 第 3 层：适配层（`internal/adapters/`）

- `EnginePort`：把 `frameworkSession.Session`（ReactorEngine）适配成
  `contract.ChatEngine`；维护 sessionID、activeCalls、pendingHistory
  （运行中替换历史 → 下一轮 install）、prepareHistory handoff。
  关键设计：新逻辑会话 = 新 ReAct 循环（factory 重建），而不是旧 loop
  上叠 ID。
- `RuntimePort`：把 `seelebridge.Runtime` 适配成 `contract.RuntimePort`。
- `PluginPort`/`SkillPort`/`SessionPort`/`WorkspacePort`：薄映射到
  plugin.Manager / skill.Registry / session.Manager / workspace.Repo，
  并做 DTO 转换（adaptPlugin/adaptWorkspace 等）。
- `messages.go`：`types.Message`（框架）↔ `EngineMessage`（应用 DTO）
  转换，隔离上游类型。

## 6. 第 4 层：seelebridge（防腐层/Runtime）

### 6.1 Runtime 装配

- `NewRuntime`（[runtime.go](../../seelebridge/runtime.go)）按拓扑序：
  config.Load(accounts) → P2CPool + account.Manager → RegistryState →
  Completer/StreamCompleter → visibilityPolicy → agent.NewWithComponents →
  plan.Executor → node.Coordinator → worktree → fork.Tool →
  telemetry Hook 链（Diagnostic → Stage → Summary）→ mcp.Manager →
  planAgentFactory。生命周期按逆序关停。
- 设计要点：
  - 账号池：`accountpool.P2CPool[agent.Completer]`（P2C 租约），
    流式用独立 streamingAccountCompleter，租约覆盖整条流。
  - 可见性：`tools.Policy`（节点作用域排除全局工具 + goal skill 控制
    plan 工具族 + 插件 include/exclude 过滤）作为
    `bridge.WithVisibilityPolicy` 输入，隐藏工具调用返回
    `ErrToolNotVisible`。
  - 子代理：`subagentSessions`（会话注册表/工具结果归档）、
    `subagentTree`（fork 树投影）、`subagentContext`（父证据/merge-back
    mailbox actor）、live 流分发器。

### 6.2 scoped 工具与安全

- `tools.Router` 覆写框架内置文件/shell 工具：read_file/grep_search/glob/
  write_file/edit_file/bash，全部先过 ProjectScope 解析
  （worktree 节点走 NodeScope 根），再走权限门。
- `security.ProjectScope`：canonical absolute + EvalSymlinks containment；
  **无绑定 root 时拒绝访问 cwd**（无隐式 fallback）。
- `security.PathGate`：前缀 zone allow/deny + workspace zone 优先。
- `security.CommandSandbox`：Windows 显式系统 PowerShell 绝对路径 +
  `-NoProfile -NonInteractive`，cmd.Dir 固定为 ProjectScope 解析目录；
  GUI 构建用 HideWindow 避免闪窗。

### 6.3 Plan / fork / node / task / scheduler

- `plan.Executor`：PlanPolicy 约束、codec.Import 归一化、拓扑校验、
  ReplanGuard（并发/窗口/请求三重护栏）、PlanNodeEvent CSP channel、
  approve gate、节点事件 → 事件库 + 投影。
- `fork.Tool`：B6 task 幂等登记 → 结果复用（省 token）→ 构造
  start→N agent→summary DAG → runPlan 同步等待。
- `node.Coordinator`：节点会话注册、fork 树终态、task 打点、PromptBlocks
  与预算、skill 匹配；经接口注入 Sessions/Tree/Tasks/Plan，域内不依赖
  根包。
- `task.TaskRegistry`：worktable 条目 actor；todolist 内化为
  kind=todo 的 task；retry 计数；CSP channel 变更投递。
- `scheduler.State`：200ms tick 单循环；command 白名单（auto_get_jobs）
  与 prompt 任务两类；状态经 SchedulerObserver → application 刷新。

## 7. 第 5 层：上下文工程（`seelexctx/`）

- 适配 Seele v2 `seelectx` 的原子策略：
  - `RequestAssembler`：system prompt（effort/skill）+ PromptBlocks +
    working history。
  - `ToolResultProcessor`：超大工具结果 → result_ref/省略警告。
  - `Compressor`：短历史免压缩 + QuickChat 隔离摘要。
  - `ContextController`：软阈值 75%（窗口外压缩）/ 硬阈值 90%
    （先归档超大结果，仍超限收缩窗口，不低于 MinRounds）；
    ReplaceHistory 前做 history_safety 配对修复。
- 子包：snapshot/provider/compactor/merger/memory/search/tokens/lifecycle。
- 数据流见 [02-data-flows.md](02-data-flows.md) §5。

## 8. 第 6 层：持久化与扩展域

### 8.1 sessionstore

- `Repository` 契约（WriteCommit/WriteAtomic/Read/ReadRange/
  ReadEventTail/ReadEventRange/ReadConversationRange/ReadToolResult/
  State/ContextState/ProjectRecord/AppendFrameworkEvent/List/Delete）。
- 四后端统一逻辑语义：JSON generation 目录 + manifest 原子切换；
  SQLite/PG 用 `seelex_session_manifest` + `seelex_session_shard` 事务；
  Redis 用 project hash tag + MULTI/EXEC。
- `Router` 用 RWMutex 原子切换 active backend，配置切换等待旧操作完成。
- **注意**：单文件 2447 行（[sessionstore.go](../../sessionstore/sessionstore.go)），
  见 Q1。

### 8.2 workspace / session

- `workspace.Repo`：workspace CRUD + session→workspace binding +
  workspace_index.json 原子写。
- `session.Manager`：薄包装 Router，注入 saveFn/loadFn；提供
  SaveCommit/LoadEventTail/LoadToolResult/上下文状态等委托。

### 8.3 plugin / skill / mcpstack

- `plugin.Manager`：Loader（plugins/*/plugin.md 的 YAML front matter）+
  事务式 Load/Activate/Deactivate/Reload（`plugin.Transaction` 逆序回滚）；
  MCP 按 `plugin__server` 运行时名 attach，目标准备失败不触碰旧插件。
- `skill.Registry`：目录化 SKILL.md，ResourcePath 防逃逸；
  plugin skills 发布/激活/清理。
- `mcpstack`：MCP 调用 trace 双栈（append-only MCPCallLog + Interceptor），
  熔断事件记录、原子持久化、Provider 链集成。

## 9. 第 7 层：上游 Seele v0.1.2

- 本仓库只消费其 agent/session/tools/workplan/seelectx/accountpool/
  event/telemetry/types 的公开面；所有上游类型集中在 seelebridge 与
  internal/adapters 内，Application/frontend 只见自己的 DTO。
- Plan 内核（workplan codec + runner）与 ReAct Loop 由上游提供，
  seelex 做编排、投影与产品语义。

## 10. 模块边界速查（谁不能依赖谁）

| 组件 | 允许依赖 | 禁止 |
|---|---|---|
| tui/gui/console | application（窄接口 + DTO） | Seele、sessionstore、plugin.Manager |
| application/core | contract、model、event、approval、prompt、seelexctx/search、snapshot | workspace、sessionstore 具体后端、seelebridge 根包（仅 contract/dto） |
| seelebridge 根包 | 其子包、Seele、application/contract/dto、sessionstore、skill、mcpstack | 被子包反向依赖 |
| seelebridge 子包 | 自身域 + internal/* | seelebridge 根包（协作走 Deps 闭包/接口） |
| sessionstore | Seele seelectx/storage、types、lifecycle | application |
| plugin/skill/mcpstack | 自身域 + 上游 | application、seelebridge 根包 |

## 11. 分层审阅小结

- 优点：依赖方向在绝大多数路径上单向且可验证；状态归属明确
  （Application 权威、Runtime 只读投影、前端只读 DTO）；并发以
  actor/CSP 为主，锁内不做 IO。
- 风险集中点：组合根与 sessionstore 单文件过大；前端无构建链；
  跨平台路径语义存在一处偏差（PathGate 小写化）；事件双轨的长期
  统一仍在过渡中。逐项见 [04-tech-leader-qa.md](04-tech-leader-qa.md)。
