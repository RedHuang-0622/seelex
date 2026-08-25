# 会话资源与锁的粒度盘点：单例现状与多会话解除路径

日期：2026-08-24
状态：调研结论（部分落地，2026-08-24）。本文件是现状盘点与迁移路径研究；M1 保持已实现，M2（会话并行）仍未实现。本次 fork 恢复落地（见 [2026-08-24-fork-subagent-recovery.md](2026-08-24-fork-subagent-recovery.md) 实现记录）已覆盖本文件 §4.2 的键空间补 sessionID 与子代理树/记录按主会话索引持久化两个衔接项；seelebridge Runtime 会话槽化与应用组件栈会话化仍按 M2 规划。
范围：`application/core`（应用服务层）与 `seelebridge`（运行时桥接层）中锁、actor、注册表、上下文绑定等资源的持有粒度；多会话并行（M2/M3）时如何把进程级单例拆成会话级。

决策已定稿（2026-08-24，见「决策契约」）：runtime 生成与生命周期、锁、actor 保护层级一律**会话粒度且不传播**；fork 会话走深拷贝（= 快照数据拷贝 + 子会话全新 actor/runtime/mu）；参考 forksubagent 的调研结论为「确认走新建道路」；执行模型采用**每会话一个协程**。2026-08-25 落地状态：seelebridge 会话槽化（`sessionBundle` 注册表）已实现并测试；fork 数据面深拷贝原语与血缘 meta 由并行实现推进；per-session actor 实例化与 `max_running` 放开仍待 M2。

## TL;DR

1. 现状是“两级单例 + 少量会话级”：`application.Service` 全局只有一份 `Core.Mu`/Snapshot/组件栈（task/prompt/view/session/worktable），聊天运行态（`sessionChat`）和引擎（`EnginePort.engines`）已在 M1 收窄为会话级；`seelebridge.Runtime` 只有一个会话槽（`r.session`），tasks/子代理树/子代理会话/plan 绑定/项目作用域/窗口策略等全是进程级单例，多数靠“切换会话时替换内容”而不是“每会话一份”。
2. 真正的单飞边界在 M1：`ErrSessionBusy`/`anyChatRunningLocked` 仍是全局判定——同一时刻只允许一个会话执行；`Core.Mu` 是全局快照锁。真并行 = M2，代码注释和既有设计都明确指路。
3. 解除单例的核心不是换锁，而是“会话工厂化”：把可复制资源（组件栈、子代理注册表、plan 绑定、project scope、bindings）改成 `map[sessionID]*X` 由工厂创建；把天然共享资源（账号池、文件系统 actor、sandbox、skills、插件、权限）保留共享但按会话/节点路由；把键空间补上 `sessionID` 维度（nodeID、task key、live 事件、事件轨）。
4. 已登记的多会话 P0 与风险：多会话页签设计在 multi-session-pages.md（方案 B：SessionActor + WorkbenchCoordinator）；会话并行前需解决单 Service 操作串行、审批/取消/Workspace 作用域、MCP 连接等 P0（见 2026-07-24 GUI 架构审查）。

## 决策契约（2026-08-24 定稿）

> 决策事实源。后续实现与 agent 以本节为准；§4/§5 中的候选方案仅作调研记录，已被本节覆盖的方向不再作为可选实现。

1. **粒度契约：会话级，不传播**
   - runtime 的生成与生命周期管理、锁、actor 保护层级一律**会话粒度**。
   - fork 会话的粒度**不做传播**：子会话的 actor、runtime、mu 全部**新建**到子会话，不继承父会话的实例句柄。
2. **fork 深拷贝语义 = 快照数据拷贝 + 全新实例**
   - 深拷贝只适用于**数据面**：ProviderHistory/事件流/SessionRecord/context 四栈/tool-results 按 conversation-fork 一期决策整体复制到子会话 key，两端此后完全独立。
   - 运行期对象**禁止复制**：复制在用的 `sync.Mutex/RWMutex` 是 Go 未定义行为（go vet 可检）；复制 `actor.Actor` 会共享 mailbox/done 通道；复制框架 `session.Session` 会共享 `mu`/`history`/`loop`/`tracer` 与 agent/completer/cache/store 引用。**因此运行时一律走「新建道路」**：子会话新建 Session、actor、锁，只把快照数据灌入。
3. **forksubagent 调研结论（2026-08-24）**
   - forksubagent 本身就是「每节点全新 `session.NewSession` + 独立 SessionComponents + 独立 worktree」，**不是深拷贝**；可参考的是它的「新装配」模式与单消费者 actor 免锁模式，不能作为深拷贝活对象的蓝本。
   - 它的进程级单例注册表（task registry、subagent 三件套、plan executor、live 分发器、sessionBindings）在多会话并行下是跨会话**语义竞态/串写**来源：nodeID 键碰撞（同 goal 同 `node-<hash>`）、`ReplaceAll` 互清、LoadedPlan/runID/binding 互踩、事件归因闭包串会话、`Core.Mu`/`ErrSessionBusy` 全局单飞。
   - 结论：actor 内部无内存级数据竞争（单 goroutine 串行），但上述跨会话竞态成立 → **确认走新建道路**，与决策 1/2 一致；actor 免锁模式保留，但实例范围改为每会话一份。
4. **执行模型：每会话一个协程（actor）**
   - 每会话一个专用 goroutine（沿用 `internal/actor` 底座：有界命令通道 + 单消费者），会话内所有变更串行，per-session 锁降为少数；全局只保留共享资源锁（账号池 lease、fs actor 写分片、worktree git 全局串行、sessionstore、EventHub）。
   - 约束：**不强制异步队列**——跨会话调用发生在会话执行 goroutine（不在 actor handler 内）时，沿用现有「同步投递 + 有界超时 reply」模式（`subagentSessionCmdTimeout` 10s）即可；唯一硬约束是 **actor handler 内部不得同步阻塞等待另一会话的 actor**（否则 A/B handler 互等死锁，且阻塞期间本会话取消/审批命令进不来）。跨会话请求如需排队，才走 WorkbenchCoordinator；空闲会话收敛 goroutine（Close/Wait 幂等）；`max_running > 1` 前执行仍受全局单飞门控，协程化可先行落地。
5. **共享保留**：账号池（P2C lease）、filesystem actor、sandbox、skills/plugins/permission、EventHub、sessionstore 保持共享，按 sessionID 路由（见 §4.1 表格）。

### 落地记录（2026-08-25）

已落地：

- seelebridge 会话槽化：`sessionMu/session/sessionBindings` 单槽 →
  `map[sessionID]*sessionBundle`（[runtime_bundle.go](../../seelebridge/runtime_bundle.go)）；
  每 bundle 独立 `mu`/`session`/`binding`（ctxStore/mainHistory/mainSessionID）；
  跨会话共享装配（historyRouter/turnArchiver/project）上移 Runtime 全局；
  `Session()`/`MainSessionID()`/`PrepareMainSessionHistory`/
  `AttachSessionContextStore` 按当前激活会话路由，公共 API 签名不变，
  单飞门控期间语义与 M1 一致；测试 `TestRuntimeSessionBundlesArePerSession`
  覆盖驻留与不串写。

待办（M2，未实现）：

- task 注册表 / subagent 三件套 / plan executor / worktree manager / live
  分发器从进程级单例改为每会话实例（actor 保护层级会话化）；
- application 组件栈会话化与 `Core.Mu` 拆 per-session shard；
- 每会话一协程（actor）执行模型的接线（单飞门控保留至 `max_running > 1`）；
- fork 数据面深拷贝原语与血缘 meta：并行实现推进中（sessionstore
  `ListToolResults`/`CurrentGeneration`/段落索引/`SessionRecord.ForkedFrom`）。

## 1. 两级现状盘点

### 1.1 application/core 层（应用服务）

进程级单例（每进程一份）：

| 资源 | 位置 | 说明 |
|---|---|---|
| `Core.Mu` 全局 RWMutex + 唯一 `Snapshot` | [internal/state/state.go](../../application/core/internal/state/state.go) | 权威快照锁，所有共享组件在锁内发布一致快照 |
| `task_context.Coordinator` | [task_context/coordinator.go](../../application/core/task_context/coordinator.go) | 任务执行状态、ReAct 预算、skill 激活，全局单例 |
| `prompt_layer.Coordinator` + `PromptStack` + `EffortManager` | [service_assembler.go](../../application/core/service_assembler.go) | prompt 栈与 effort 全局单例 |
| `session_runtime.Coordinator` | [session_runtime/coordinator.go](../../application/core/session_runtime/coordinator.go) | 会话目录/workspace 绑定/标题，内部 `sessionNameMu`/`sessionTransitionMu` |
| `view_state.Coordinator`、`workTablePublisher` | [view_state/coordinator.go](../../application/core/view_state/coordinator.go) | 投影与工作台发布器全局单例 |
| `lifecycleRuntimeState` | [service_state.go](../../application/core/service_state.go) | `cancelChat`/`idle`/`draining`/`closed` 全局 |
| `conversationRuntimeState` | 同上 | 全局 `streamOutput`/`streamBatcher`（同时存在会话级副本，M3 待删） |
| `CommandRegistry`、`inputRouter` | [service_assembler.go](../../application/core/service_assembler.go) | 命令与输入路由全局 |

已会话级（M1 完成）：

| 资源 | 位置 | 说明 |
|---|---|---|
| `sessionChat map[sessionID]*sessionChatRuntime` | [session_scope.go](../../application/core/session_scope.go) | 每会话 ChatState/cancel/inputQueue/streamOutput/streamBatcher |
| `EnginePort.engines map[sessionID]ReactorEngine` | [engine_port.go](../../internal/adapters/engine_port.go) | 每会话独立 framework engine + 调用计数；切换不销毁其它引擎 |
| Event 会话路由 | [event/hub.go](../../application/event/hub.go) | `PublishSession`/`SubscribeSession`，事件带 `session_id` |
| SessionStore 持久化 | [sessionstore/](../../sessionstore/session_context.go) | 事件库/会话记录已按 `sessionID` 分片 |

### 1.2 seelebridge 层（运行时桥接）

进程级单例（NewRuntime 装配一次）：

| 资源 | 位置 | 说明 |
|---|---|---|
| `session *session.Session` + `sessionMu` | [runtime.go](../../seelebridge/runtime.go) | 单会话槽，切换会话时重建/替换 |
| `bindings sessionBindings` | [runtime_session.go](../../seelebridge/runtime_session.go) | ctxStore/historyRouter/mainHistory/project/turnArchiver/mainSessionID 单套 |
| `tasks *task.TaskRegistry` | [task/task.go](../../seelebridge/task/task.go) | 单 actor；会话切换用 `ReplaceAll` 换内容 |
| `subagentSessions/subagentTree/subagentContext` | [session/](../../seelebridge/session/subagent_sessions.go) | 单 actor/单树/单合并队列；切会话 `ClearSubagentTree` |
| `planExecutor` | [plan/executor.go](../../seelebridge/plan/executor.go) | 单策略/单 binding/单 runID/单 loaded plan（内部多把锁） |
| `projectScope` | [security/project_scope.go](../../seelebridge/security/project_scope.go) | 单项目根绑定，`Bind/Unbind` 切换 |
| `worktreeMgr` | [worktree/worktree_manager.go](../../seelebridge/worktree/worktree_manager.go) | 单注册表 + 单锁（git 子进程全局串行） |
| `window` 窗口策略 | [runtime_context.go](../../seelebridge/runtime_context.go) | 单 WindowPolicy（A4 遗留：未按账号/节点实例化） |
| live 分发器 | [runtime_live.go](../../seelebridge/runtime_live.go) | 单通道 + `map[nodeID]` 订阅/历史 |
| `scheduler/toolEvents/bashObserver/lazyMCPServers` | [runtime.go](../../seelebridge/runtime.go) | 各单例 |

跨会话共享安全（建议保留共享）：

| 资源 | 说明 |
|---|---|
| 账号池/completer/streamer/agt | P2C lease 机制，流式请求持有 lease 到 EOF；按 role+branch 路由 |
| `filesystem` actor | 写路径分片串行化，天然支持并发读 |
| `sandbox`/`projectScope`（改造后） | 安全边界建议保留全局策略对象，根路径按会话传入 |
| `skills`/`plugins`/`permission`/`accounts` | 只读或按会话/节点过滤的共享策略 |
| `mcpManager`/`MCPStack` | 连接共享，调用上下文按会话标记 |

## 2. 锁清单（两层的全部锁/串行化点）

application 层：

- `Core.Mu`（全局快照锁，主入口 `startChat` 全程持写锁）——多会话最大瓶颈；
- `sessionChat` 由 `Core.Mu` 保护（map 本身无独立锁）；
- `session_runtime.Coordinator.sessionNameMu/sessionTransitionMu`；
- `task_context` 内部 `token_counter.mu`、`goalSkillActive` 原子、`tool_hooks.mu`；
- `worktable` 发布器（CSP 汇聚，latest-wins）；
- `stream_batcher` 计数原子。

seelebridge 层：

- `sessionMu`（会话槽替换）、`liveMu`（实时流）、`bashObserverMu`、`windowMu`、`lazyMCPServerMu`；
- `sessionBindings.mu` + `mainSessionMu`；
- `plan.Executor`：`policyMu/bindingMu/runMu/provider.mu/agentFactoryMu/nodeFactoryMu/approvalMu/eventErrorMu`；
- actor 单消费者串行：`TaskRegistry`、`SubagentSessions`、`SubagentTree`、`SubagentContextActor`、`scheduler.State`、`ToolEventState`；
- `worktreeMgr.mu`（git 子进程全局串行）；
- `fs.FileSystem` per-path 写锁；
- `skill.Registry` 内部锁。

## 3. 为什么“换会话”不等于“多会话”

当前切换会话的路径：`ActivateSession` → `resumeSession` → 重建引擎/恢复历史 → `SwitchSessionTasks`（整体替换 task 注册表）→ `ClearSubagentTree`。所有“会话状态”都落在同一个内存单例里，切页只是把单例内容换成目标会话，因此：

- 任意时刻只有一个会话可执行（`ErrSessionBusy`/`anyChatRunningLocked` 全局判定）；
- 非活跃会话没有驻留快照（`SnapshotOf` 返回 `ErrSessionSnapshotUnavailable`）；
- 跨会话提交/切页是破坏式重建，运行中切换被拒绝。

这与 multi-session-pages.md 的四种状态模型（persisted/open/active/running，`running` 可多个）相差一个 M2。

## 4. 解除单例的路径（对已有方案 B 的细化）

### 4.1 三类资源的处置原则

| 类别 | 处置 | 例子 |
|---|---|---|
| 天然共享 | 保留单例，加会话/节点路由 | 账号池、fs actor、sandbox、skills、plugins、permission、EventHub、sessionstore |
| 会话状态 | 工厂化 `map[sessionID]*X`，每会话一份 | task 注册表、子代理树/会话/合并队列、plan 绑定/runID/loaded、projectScope、bindings、window、worktreeMgr、live 分发器 |
| 应用组件栈 | 整栈会话化（SessionActor） | task/prompt/view/session/context 各 coordinator 每会话实例；`Core.Mu` 降级为 per-session shard 锁 + 全局只读聚合 |

### 4.2 具体改造点（与 M2/M3 对照）

1. application 组件栈会话化：`serviceComponents`（tasks/prompts/view/sessions/context/subagent/input）从单例改为 `SessionActor` 每会话实例（方案 B），`Core.Mu` 拆成每会话 state shard；全局只剩 `WorkbenchCoordinator`（open 注册表、activeID、scheduler、EventHub）与共享基础设施。
2. seelebridge Runtime 会话槽化：`sessionMu/session/bindings` → `map[sessionID]*sessionBundle`（每个 bundle 含独立 Session、ctxStore 绑定、DurableHistory、turnArchiver、projectScope、window、task registry、subagent 三件套、plan binding/runID/loaded、worktree manager、live 分发器）。
3. 键空间补 sessionID：`nodeID`（`node-<hash>` 是 goal 的稳定 hash）与 `task key`（`subagent:<nodeID>`）在全局键空间中会跨会话碰撞；内存 map（subagentSessions/subagentTree/liveHistory）与事件轨必须改成 `sessionID+nodeID` 复合键或按 session 分桶。
4. 锁粒度：`Core.Mu` → 会话级锁 + 全局只读快照发布；`worktreeMgr.mu` → 每会话 manager；plan executor 锁 → 每会话 executor 实例；actor 类资源随会话实例化即天然隔离。
5. 持久化与事件：sessionstore 已按 sessionID 分片；M2 把 worktable/子代理树/task 从内存单例改为按会话读写持久化（衔接《fork 子代理异常中断状态恢复可行性调研》）。
6. 审批/交互/Effort/Plugin/Skill/Plan：从“当前会话全局态”改为随 SessionActor 持有（M2/M3 清单中已登记）。
7. 前端：Bridge 显式 `sessionID` API（M1 已具备委托形态），页签路由与 per-session reducer 是 M3。

### 4.3 分期

- M1（已完成）：chat 保护会话级、EnginePort 多实例、事件带 session_id、显式 session API；
- M2（下一阶段）：持久化/审批/worktable/子代理树按会话隔离，会话并行 race + E2E；
- M3：删除全局 ChatState/单 Engine 语义，前端多页签并行渲染与通知；
- 并行开关：`max_running > 1` 前必须完成 per-session Engine、审批、Workspace 前置（agent-workbench 架构 §9.4 已给出 A/B 并行验收流）。

## 5. 风险与依赖

- 账号池并发：P2C lease 支持并发，但多会话同时长流式请求会放大对上游 API 的并发压力，需配额/公平调度（WorkbenchCoordinator.SessionScheduler）。
- worktree 并发：git 子进程当前全局串行；每会话 manager 后仍需跨会话合并到同一主仓库时的互斥与审批作用域。
- 同一项目多会话写冲突：并行会话写同一 project root 需要 worktree 隔离 + 合并审批兜底（既有 R10 风险登记）。
- nodeID 碰撞：跨会话同 goal 会生成相同 `node-<hash>`，事件/任务/实时流必须带 sessionID 维度。
- MCP/插件连接：共享连接需按会话标记调用上下文，避免审批/计费/权限串会话。
- 上下文治理：窗口/压缩栈需随会话实例化（衔接前一调研：子代理压缩帧当前还写主会话栈）。

## 6. 验证建议

- 保持 M1 现状测试绿色：`go test ./application/core ./seelebridge/... ./internal/adapters ./gui ./e2e -count=1 -timeout=300s`；
- 新增验收口径（规划）：
  1. 两个会话可同时 Running，互不返回 `ErrSessionBusy`；
  2. 会话 A/B 的同名 fork 节点（相同 goal）事件、task、实时流互不串写；
  3. 切页不销毁其它会话引擎与驻留状态；
  4. 审批/取消只作用于目标会话；
  5. `-race` 全绿（并行会话 + 子代理并行）。

## 参考

- [multi-session-pages.md](../gui/modules/multi-session-pages.md)（方案 B：SessionActor + WorkbenchCoordinator；四种会话状态模型）
- [session-dialog-sidebar §8](../2026-08-23-session-dialog-sidebar/README.md)（M1/M2/M3 实施阶段与文件归属）
- [agent-workbench-architecture.md](../arch/agent-workbench-architecture.md)（会话并行与 P0 前置）
- [session_scope.go](../../application/core/session_scope.go)（M1 会话级 chat runtime）
- [engine_port.go](../../internal/adapters/engine_port.go)（M1 会话级引擎注册表）
- [runtime.go](../../seelebridge/runtime.go)（Runtime 单例资源清单）
