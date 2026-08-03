# Seelex 与 Seele v0.1.1 Runtime 边界

> 当前状态：迁移已经完成。Seelex 通过 `go.mod` 固定远程模块 `github.com/RedHuang-0622/Seele v0.1.1`；仓库不依赖本地 `go.work`、`replace` 或 `../Seele` 才能构建。本文保留迁移决策，同时以当前模块边界为事实来源。

> 状态：已实施（2026-08-01）
> 适用范围：seelex 主会话装配、Plan→subagent 执行、Task 生命周期、上下文控制、存储模型（项目级知识 + 会话级堆栈/队列/滑动窗口）、插件/可见性与遥测
> 配套详细设计：[`docs/2026-08-01-seele-v2-underlying-refactor/plan.md`](../2026-08-01-seele-v2-underlying-refactor/plan.md)
> 本文描述目标设计与当前实现；旧引擎时代设计见 [`DESIGN.md`](../../DESIGN.md)（已标记迁移状态）。

---

## 1. 背景

Seele 的新边界把旧单体拆为平行根能力 `agent`、`session`、`tools`、`workplan`、`seelectx`、`accountpool`、`event`、`errors` 与 `telemetry`。Seelex 当前通过 `agent.NewWithComponents`、`session.NewSession`、`tools.Registry`、`agent/bridge` 和 `workplan/codec` 完成装配，并由 `seelebridge/` 隔离上游 API 演进。上游示例与源码应按 `go.mod` 固定版本在 Go module cache 或 Seele 仓库对应 tag 中查阅。

seelex 仍引用已删除包，**当前 `go build ./...` 处于构建阻断状态**：

| 引用点 | 已删除的 Seele 包 |
|---|---|
| `main.go`、`seelebridge/*`、`tui/*` | `Seele/engine` |
| `seelebridge/branch.go` | `Seele/agent/core/tool/builtin`（ChatAgentFactory/WorkPlanTool） |
| `seelebridge/plugins.go` | `Seele/agent/core/tool/holder` |
| `seelebridge/mcp.go` | `Seele/agent/core/tool/mcp` |
| `seelebridge/plan_tool_provider.go` | `Seele/agent/core/tool/interfaces` |
| `application_adapters.go` | `Seele/agent/core/tool/permission` |
| `seelebridge/plan.go`、`branch.go` 等 | `Seele/workplan/runtime/serialize`、`workplan/core/*`（部分仍在但非新入口） |
| `seelebridge/runtime.go` | `Seele/agent/core/api`（ChatClient/AccountPool） |

## 2. 结构性缺陷（迁移必须同时解决的问题）

### 2.1 Plan→subagent：子代理无作用域，只能串行

现状（`seelebridge/plan_tool_provider.go`、`seelebridge/branch.go`）：plan 的 DAG 执行整体委托给旧 `builtin.WorkPlanTool`。`plan_run` 触发框架内部 scheduler/fork，框架按 `resolvePlanBranchRuntime()` 请求 seelex 提供 `forkexec.BranchRuntime`（账号解析 + `ChatAgentFactory` 私有 client）。但：

- 框架创建的 branch 子聊天不继承 seelex 的项目作用域工具集（`ProjectScope`、`scoped_tools.go` 的过滤逻辑）与上游证据 envelope；
- 因此 `withoutAuthoritativePlanMutationTools()` 在 authoritative 模式下把 `plan_run` 隐藏，主代理只能拿 plan 当清单**串行**执行，fork/并行能力被框架持有而 seelex 不可控；
- 节点上下文只有旧 `WorkflowContext` 字符串镜像，无法注入子代理自己的 goal/evidence/budget。

### 2.2 Task 与 Plan 强耦合

现状（`application/core/task_execution.go`、`chat.go`）：`taskExecutionState` 是请求级瞬态，`task_complete` 必须列出全部 plan 节点（`completeAuthoritativePlanLocked`），plan 状态直接驱动 task 自动终态（`finalizeTaskExecution`）。任务、计划、执行器三者在 chat.go 的单函数内互相调用，无法独立测试与复用。

### 2.3 上下文控制绑定旧 Engine 协议

现状（`application/core/context_controller.go`）：压缩路径依赖 `engine.ReplaceHistory()` + `ToolHookBridge.OnIterationComplete`；`seelexctx` 四子包（snapshot/provider/compactor/merger）为跨会话承袭而建，但**主 ReAct 压缩并未走 Compactor**，两套机制并存且不与新 seelectx 契约（DurableHistory/RequestAssembler/ToolResultProcessor/ContextController）对齐。

## 3. 目标架构

### 3.1 边界原则（与 Seele 一致）

> Seele 提供无产品语义的执行能力；Seelex 决定何时调用、调用什么、上下文放什么、费用如何归集。

| 能力 | 所有者 |
|---|---|
| LLM 装配、会话 ReAct 循环、DAG 内核、账号租约、工具分发契约、事件/遥测原语 | Seele（新根模块） |
| Task 生命周期、Plan 产品 DSL（`plan_load` JSON 语义）、节点 kind 解释、工具实现（文件/Git/工作区/审批）、插件可见性策略、上下文压缩策略、Token 账本、EventStore | Seelex |

### 3.2 装配拓扑（目标）

```text
seelex config (accounts.yaml / plugin.md / skills/)
   │
   ▼
accountpool.Pool[agent.Completer] ── bridge.NewAccountCompleter ──┐
tools.Registry (产品 provider + MCP + skill + plan/task 工具)      │
   │  └─ bridge.NewRegistryRuntime(WithVisibilityPolicy=插件策略)  │
   ▼                                                              ▼
              agent.NewWithComponents(Completer, Tools, ...)
                          │
seelex ProjectKnowledge（项目级模块语义）─────┐
seelex SessionContextRecord（5 栈 + 窗口）────┤
seelex DurableHistory (sessionstore 适配) ───┤
seelex Assembler/Compressor/Controller ─────┤
seelex ToolResultProcessor ─────────────────┼── session.NewSession(SessionComponents{...})
seelex LoopHooks（进度回调）────────────────┘
                          │
        ┌─────────────────┴──────────────────┐
        ▼                                    ▼
 主会话 Chat/ChatStream               workplan 装配
   （用户对话/终态工具）        codec.NodeFactory[SeelexNode]
                                   → Plan.Build/Seal
                                   → workplan.New(bridge.NewAgentFactory(...),
                                          WithEventSink(seelex sink, planID),
                                          WithEventLocators(...))
                                   → runner.Run(ctx)
                                   → 每节点 = 独立 Session（subagent）
                                          │
                                   event.Sink → sessionstore 事件库 → Task/PlanState 投影
                                          │
                                   telemetry Hook/Tracer → GUI/TUI 视图
```

### 3.3 依赖方向

```text
tui / gui ──> application（服务层，接口稳定）
application ──> seelebridge（窄接口）/ seelexctx / sessionstore / plugin / skill
seelebridge ──> Seele 新根模块（agent/tools/session/workplan/seelectx/accountpool/event/errors/telemetry）
禁止：seelexctx -> application；workplan 产品节点 -> seelex 业务包（反向依赖由调用方接口解耦）
```

核心解耦手段：**WorkPlan 节点不直接调用 Task 状态机**；节点状态经 `event.Sink` 投影到 Task/PlanState。Task 终态工具（`task_complete` 等）是注册在 `tools.Registry` 的普通产品工具，通过投影校验完成度。

## 4. 分模块设计

### 4.1 装配与账号（seelebridge.Runtime）

- `seelebridge.Runtime` 从"持有旧 agent+client+pool"改为 **composition root**：持有 `*session.Session`（主会话）、`*tools.Registry`、`*accountpool.P2CPool[agent.Completer]`、`*workplan.WorkPlan`（工厂+事件）、`telemetry`、`event.Sink` 与产品配置。
- 账号：`accountpool.New[agent.Completer]()` 注册账号（key/baseURL/model 属 seelex），`bridge.NewAccountCompleter(pool, WithAccountRequestSelector(provider 过滤 + 账号选择))` 生成同步 `Completer`。
- **流式**：主会话与子代理使用流式输出。seelex 需要自实现 `StreamCompleter`/`StreamEventCompleter`，**lease 必须覆盖整条流直到 EOF/Close**（见 `agent/bridge/README.md` 与集成指南第 3 章约束），不能复用同步 `AccountCompleter`。账号切换（`/account`）通过 selector 闭包读当前选中账号/Provider 过滤实现，不再强转 `*api.ChatClient`。
- provider/模型信息视图（`Account.Name/Provider/Model`）继续由 seelex 从 pool 快照提供，TUI/GUI 契约不变。

### 4.2 工具、插件与可见性（tools.Registry）

- 全部产品工具迁移为 `tools.ToolProvider`/`tools.ToolHandler`，注册进 `tools.Registry`（`WithCallTimeout` 保留，工具超时语义不变）。
- **插件切换（holder.PluginManager）→ `bridge.WithVisibilityPolicy`**：插件 Manager 不再控制 holder，而是维护"当前插件 include/exclude"配置；`VisibilityPolicy(ctx, tools) → tools` 对每次请求过滤可见集。`Dispatch` 侧由 Registry 的同一策略/中间件再次校验，隐藏工具不可被猜测调用（`ErrToolNotVisible`）。
- 审批/权限：新 Seele `tools` 提供 middleware 与 permission 目录；seelex 的 `application_adapters.go` 权限接线改为 `tools.WithMiddleware` 门控 + 审批 Provider，不再依赖已删除的 `agent/core/tool/permission`。
- MCP：`seelebridge/mcp.go` 改为 `tools.ToolProvider` 适配器（`tools/adapter` Catalog/Invoker），`mcpstack` trace 逻辑保留并接 telemetry。
- 内置无产品语义工具（计算/时间等）可用 `tools/builtin`（新包），产品工具（文件/Git/工作区/plan/task 终态）由 seelex 实现。

### 4.3 会话与历史（session.NewSession）

- `engine.Engine` 拆除为：
  - **主会话**：`session.NewSession(SessionComponents{Agent, History, Context, Hooks, Telemetry, SessionID, ModelName})`。`ChatStream(ctx, query, onChunk)` 对应旧 `eng.ChatStream`。
  - **durable history**：`sessionstore` 适配 `seelectx.DurableHistory`（`Load/Save/Clear`）。Session 每次 Chat 前 Load、结束后 Save；`Reset(ctx)` 显式清空。transcript 事件与 TaskContextProjection 的持久化仍走 sessionstore.Router，但由 DurableHistory 实现统一编排。会话级上下文记录 `SessionContextRecord`（SystemPrompt + plan/task/skill/compact 五栈）由 Router state blob 承载，与 ChatQueue（ProviderHistory）分离；滑动窗口按轮读取（`ReadRange`/`ReadEventTail`），详见 §4.8。
- **工具钩子映射**（原 `ToolHookBridge`）：`SessionComponents.Hooks *LoopHooks`（`OnLLMStart/OnToolStart/OnToolComplete/OnIterationComplete`）同步进度回调；`OnIterationComplete` 不再直接调压缩——压缩决策移交 `ContextController`（见 4.6）。原 `handleToolStart/handleToolComplete/handlePlanNodeComplete` 的**观测与投影**逻辑移到 LoopHooks + 事件投影中。
- `engine.History()/ReplaceHistory()/ClearHistory()` → working history 由 Session 持有；`History()` 语义由 DurableHistory/投影提供；`ReplaceHistory` 改由 `ContextController.Handle → ContextDecision{ReplaceHistory, History}` 驱动。

### 4.4 Plan→subagent（workplan/codec + agent/bridge）

- **产品 DSL 解释归 seelex**：`plan_load`/`plan_run`/`plan_clear` 由 seelex 作为普通产品工具实现（注册进 Registry），内部：
  1. `NormalizePlanLoadArguments`/`DetectCycle`/`TopoSort`/`PlanPolicy` 校验逻辑保留（`seelebridge/plan_input_adapter.go`、`plan.go`、`plan_policy.go`）；
  2. 规范 JSON 经 `codec.Import`/`codec.ImportEdgeList` 导入为 `*coreplan.Plan`（`Document[SeelexNodeInput]` 或 `Document[string]`），由 seelex 的 `codec.NodeFactory[SeelexNodeInput]` 将 `kind`/`input` 解释为具体 Node；
  3. `workplan.NewFromPlan(plan, factory, WithEventSink(sink, planID), WithEventRunID(runID), WithEventLocators(agent.EventLocator, workplan.EventLocator))`，`runner.New(plan).Run(ctx)` 执行，支持真并行 fork、取消、checkpoint、Resume。
- **Node 语义（kind 由 seelex 解释）**：
  - `agent`（LLM 子代理）：`bridge.NewAgentFactory(agt, WithSessionComponents(nodeSessionComponents))`——每个节点获得**独立 Session**，天然并行隔离；
  - `auto`/`function`（只读/验证/交付等确定性节点）：`node.NewTypedFunctionNode[I,O]` 或自定义 Node，可注入产品工具 handler；
  - `approve`：seelex 的 ApprovalGate 节点（保留 `workplan/sugar/approve` 兼容或自实现）。
- **子代理作用域（解决 2.1）**：seelex 用自己的 `SeelexAgentNode` 包装 factory 产物——`Run(ctx)` 内先 `context.WithValue` 注入 `nodeID/role/branchID/projectScope`，再委托内部节点执行；`WithVisibilityPolicy` 与 Registry middleware 按这些值过滤工具，`WithSessionComponents` 注入节点目标、继承证据（seelexctx snapshot/merger 产物）与预算作为 `PromptBlocks`。**子代理由此获得项目作用域工具、有界上下文与父证据，`plan_run` 不再需要隐藏**，DAG 可真并行。
- 规划（自愿 + 显式 replan）：规划是模型的自愿决策，**不设聊天入口强制门槛**（RequirePlan/PlanActScope 强制 preflight 已于 2026-08-01 作为失败设计移除，`PlanPreflight` 仅由显式 `PrepareReplan` 触发，经隔离规划会话执行）；`ReplanRequest` 证据经 checkpoint/事件投影提取，`replanGuard` 不变。
- 进度观测：不再需要 `SetPlanNodeCallback`；workplan 的 `event.Recorder` 在节点 queued/running/终态时经 `event.Sink` 推送到 seelex EventStore，`HandlePlanNodeComplete` 逻辑改为投影订阅（GUI/TUI 的 `PlanState`/`SubAgentTree` 由投影驱动）。

### 4.5 Task 机制（与 Plan 分离）

Task 有**快照**与**存储**两个角色，职责明确分工：

- **`TaskExecutionState`（快照，请求内瞬态）**：唯一用途是**功能打点**（`docs/feature-instrumentation.md` 打点表与北极星指标）——记录任务从开始到终态的进展、checkpoint 与 evidence。生命周期：task 开始创建，**task 终态（`task_complete`/`task_failed`/`task_needs_user_decision` 被接受）即结束**；不持久化、不承担会话恢复。
- **`TaskStack`（存储，会话级持久化）**：Task 使用史以 `TaskFrame` 入栈（§4.8，`now using task` = 栈顶），由事件投影降级生成；会话恢复只依赖 TaskFrame（含 Evidence 引用）与事件投影，**不依赖已消亡的快照**。

Task 状态机（`taskExecutionState` → `TaskService`）只消费：会话结果（模型回复/错误）、工具事件（LoopHooks 观测）、**workplan 事件投影**（节点状态）与用户输入；终态工具 `task_complete`/`task_failed`/`task_needs_user_decision` 是 Registry 中的普通产品工具，`task_complete` 的"全部节点完成"校验改为**对 PlanState 投影**（由事件累积而来）的检查，不再在 chat.go 内直接读执行器状态；写入时机由"chat 生命周期"改为"事件 + 终态 + 会话边界"。好处不变：任务状态机、plan 事件投影、会话持久化可独立单测；终态校验与 DAG 执行器完全解耦。

### 4.6 上下文控制（seelectx 契约）

- seelex 实现/适配 seelectx 四个原子策略，注入 `session.ContextComponents`：
  1. **`DurableHistory`**：sessionstore 适配（见 4.3）；
  2. **`RequestAssembler`**：`seelexAssembler.Assemble(AssemblyRequest{Blocks, WorkingHistory})` 组合 system prompt（effort/skill）、`PromptBlock`（plan authority、task checkpoint、evidence）与 working history；`PlaceholderRequestAssembler` 解析 `{{plan}}/{{skill}}` 占位符；系统提示与 PromptBlocks **只进模型请求，不写 durable history**（新不变量）；原 `preflightPlanAuthorityContext` 的 XML envelope 由 `PromptBlock` 承载；
  3. **`ToolResultProcessor`**：`rejectOversizedToolResults` + `result_ref`/`read_tool_result` 分页语义迁移到此（与 example 09 一致），工具结果进入 working history 前先筛选为 `ToolResultView`；
  4. **`Compressor`/`ContextController`**：`seelexContextController.Handle(ContextEvent{Kind: before_model|after_assistant|after_tool, Turn, Query, History, Tool}) → ContextDecision{ReplaceHistory, History}`。原 `compactTaskContext/prepareExecutionContext` 的决策逻辑（软/硬阈值、`ContextWindowPolicy`、片段闭合压缩、checkpoint 消息 `<-- seelex:context-checkpoint:v1 -->` 的生成与移除）整体迁移进 Controller；需要 LLM 摘要时经 `Compressor`（`QuickChat` 隔离调用，无工具、独立 history）。压缩范围新规则（§4.8）：**只压缩 ChatQueue 滑动窗口之外的轮次**，产物 push CompactStack；窗口 N 由 `WindowPolicy` 决定（配置 + provider 推导，非魔法数字）。
- **seelexctx 四子包重新定位**（跨 Agent 承袭，不再承担主循环压缩）：
  - `snapshot/`：DTO 保留，作为子代理上下文承袭的载荷；
  - `provider/`：`EngineProvider` 改为从 `DurableHistory`/`SessionRecord` 导出；`TraceProvider` 改从 `telemetry`（MemoryTracer Query）导出；
  - `compactor/`：实现 `seelectx.Compressor` 适配器（三级预算压缩 → `CompressionResult`），供 Controller 的显式压缩路径使用；
  - `merger/`：子代理 `ContextSnapshot` 合并回父上下文，产物作为主会话下一个 `PromptBlock` 的证据块（配合 4.4 的节点证据注入）。
  - `seele.go` 的旧函数 re-export（`EstimateTokens`/`NeedCompression` 等）在兼容周期内保留，新代码改走 seelectx 契约与 `token_counter`。
- token 账本：`TokenAudit`/`ContextCompaction` 记录仍由 seelex 负责（费用归集所有者），数据源为 Session/Completer 返回的 usage 与 telemetry 事件，不再从 trace 树字符串解析。

### 4.7 事件与遥测

- `seelectx/tracer` → `telemetry.NewMemoryTracer()` + `telemetry.NewLifecycleHook(tracer)`；GUI/TUI 的 trace 视图改用 `tracer.Query`。`engine.ExportTrace()` 的兼容面由 `SessionComponents.Tracer` 承担（新 Session 支持注入 compat tracer）。
- 执行事实：seelex 实现 `event.Sink`（`event.SinkFunc`），追加写入 sessionstore 事件库；KV/投影（PlanState/TaskState/SubAgentTree）只作为可重建投影。心跳（`WithEventHeartbeatPolicy`）用于长节点 liveness。
- `errors`：跨模块错误统一 `seeleerrors.From(err)` 读取 `Struct/Function/Step/Path`；`application/core/error_presentation.go` 的错误码映射改为结构化读取，不再依赖错误字符串。

### 4.8 存储模型：项目级知识 + 会话级上下文（5 栈 + 队列 + 滑动窗口）

存储内核（2026-08-01 用户澄清）：**项目粒度**下唯一跨会话共享的是"项目通用模块语义说明"；**会话粒度**下会话完全独立，每个会话的上下文存储由 system prompt、4 个使用栈、1 个压缩栈与 1 个聊天队列组成。

#### 4.8.1 两级作用域

```text
项目粒度（跨会话共享，只读）
└── ProjectKnowledge：项目通用模块语义说明
      · 会话开始前即可读，让模型先对项目形成大致判断
      · 内容 hash 版本化；变更才重建；不随会话写入

会话粒度（互相独立）
└── SessionContextRecord
      ├── SystemPrompt        —— 会话级基础提示（永不压缩，始终完整进入请求）
      ├── PlanStack           —— now using plan：plan 进入时 push，结束/被取代时关闭
      ├── TaskStack           —— now using task：task 开始 push，终态时关闭
      ├── SkillStack          —— now using skill：skill 激活 push，退出时关闭
      ├── CompactStack        —— now using compact context：每次窗口外压缩 push 一个摘要帧
      └── ChatQueue           —— 完整对话轮次（append-only 队列）
            ├── 滑动窗口：最新 N 轮原样保留（进入 provider 请求）
            └── 窗口外：唯一允许被压缩的部分
```

- **"now using X" = 栈顶**：模型与 UI 看到的是当前使用中的 plan/task/skill 与最近一次压缩摘要；栈保留完整使用史，会话恢复时按栈重建现场。
- **轮（round）**：完整协议单元 = 用户回合 → assistant 回复（无工具）或工具链闭环（assistant tool-call + 全部 tool result），即 sessionstore `completeEventUnits` 语义。
- **压缩规则（唯一压缩面）**：只有 ChatQueue 中滑动窗口保留之外的轮次可被压缩；压缩产物以 `CompactFrame`（段范围 + 摘要 + 证据引用）push 到 CompactStack。窗口内轮次永不压缩——硬阈值（token）超限时先归档超大工具输出为 `result_ref`（ToolResultProcessor 路径），仍超限才经策略**收缩窗口**（不得低于 MinRounds），新移出窗口的轮次才进入压缩。SystemPrompt 与栈帧**永不压缩**、不写入 ProviderHistory 消息，会话不变量保持。
- **CompactStack 读取规则**：活跃会话只读**栈顶一帧**（帧为"该时刻窗口外全部轮次的综合摘要"，栈顶自足）；更早帧仅作存储（压缩台账），用于 fork 新会话恢复、修改会话内容重开会话、或重新打开会话时取出栈顶。
- **扩展预留（2026-08-01 决策）**：三个读栈场景目前仅规划、不做细节设计——stack 能力是给未来可扩展功能（目标形态参考**代码图谱（code graph）**式上下文重建）准备的底子。曾评估独立向量模型/embedding 方案，成本与收益不划算而否决；当前采用结构化栈帧（Summary/Evidence/From/To/SegmentID）的简单实现，**帧字段即未来图谱功能的接缝**：扩展时只消费帧结构，不改存储层。
- **恢复**：会话恢复 = 读 `SessionContextRecord`（state blob）+ ChatQueue 尾窗口（按轮读取），不重放全量。

#### 4.8.2 滑动窗口 N 的确定机制（非魔法数字）

N 由 `WindowPolicy` 构造注入决定：

- 输入：provider/model 上下文窗口、会话配置 `window.rounds`（可选显式覆盖）、system prompt + 栈块的固定预留、每轮 token 估算；
- 输出：窗口轮数 N 与窗口 token 预算；
- 默认策略：`N = clamp((ContextTokens × ratio − Reserved) ÷ AvgRoundTokens, MinRounds, MaxRounds)`，ratio/Min/Max 来自配置，代码无硬编码常量；具体默认值列为详设确认点 5。

#### 4.8.3 与 sessionstore / seelectx 契约映射

| 存储概念 | seelex 落地 | Seele 契约 |
|---|---|---|
| ProjectKnowledge | `Repository.WriteProjectRecord/ReadProjectRecord`（新增，四后端） | 无（缺口 1，见 4.9） |
| SystemPrompt | `SessionContextRecord.SystemPrompt`（state blob）→ 会话开始注入 `ContextComponents.SystemPrompt` | `session.ContextComponents.SystemPrompt` |
| Plan/Task/Skill/Compact 栈 | `SessionContextRecord`（state blob），帧 DTO 见详设 §3.7.2 | `PromptBlock`（Assembler 渲染块） |
| ChatQueue | ProviderHistory（sessionstore 分片） | `DurableHistory`（Load/Save/Clear） |
| 滑动窗口 | `ReadRange`/`ReadEventTail` 按轮/预算读尾 | DurableHistory 无范围读（缺口 3） |
| 窗口外压缩 | Controller 段压缩 → CompactStack push | `Compressor`/`ContextController`（无段范围，缺口 4） |

#### 4.8.4 每次请求的投影顺序（Assembler）

```text
system prompt → ProjectKnowledge 块（会话前预读） → 栈块（plan/task/skill/compact，栈顶=now using） → WorkingHistory = 窗口轮次
```

### 4.9 Seele 能力缺口与 seelex 自实现边界（供 Seele 演进参考）

seelex 侧以适配器隔离以下缺口；Seele 实现后仅移除适配层，不动产品逻辑。

| # | seelex 需要 | Seele 现状 | 建议（供 Seele 演进） |
|---|---|---|---|
| 1 | 项目级模块语义：跨会话共享、会话前可读 | `DurableHistory` 仅会话级，无 project scope 概念 | `seelectx.ProjectKnowledge`：项目级记录的 Load/Reload 契约 |
| 2 | "now using X" 栈语义（LIFO、可嵌套、可恢复） | `PromptBlock` 为扁平列表 | `seelectx.ContextStack` 原语（Push/Pop/Top/Index）与渲染契约 |
| 3 | 按轮保留的滑动窗口 | `DurableHistory` 只有全量 Load/Save/Clear | `DurableHistory.LoadTail(ctx, units)` 范围读 |
| 4 | 只压缩窗口外段 | `Compressor.Compress` 面向整段 History | `CompressionRequest{From, To}` 段范围压缩 |
| 5 | 轮 = 完整协议单元 | seelex sessionstore 已有 `completeEventUnits`（产品侧） | seelectx 提供单元化 History 视图 |

## 5. 迁移策略与兼容

| 策略 | 说明 |
|---|---|
| 垂直切片 | 按详细设计 10 个切片推进，每个切片保持 `go build ./...` 与测试通过 |
| 旧包过渡 | Seele 中 `workplan/core`、`workplan/runtime`、`workplan/sugar`、`seelectx/{storage,tracer,ctx_manager,react}` 仍存在；仅在切片过渡期使用，新代码不引用 |
| 双路径 | 会话/上下文以 feature flag 在旧 Engine 路径与新 Session 路径间切换（ContextWindowPolicy 类似机制），回滚只切 flag |
| 前端契约稳定 | `application/model` DTO（Snapshot/TaskState/PlanState）不因本次重构改变字段；GUI/TUI 无破坏性变更 |
| 删除确认 | 迁移完成后删除 seelex 内全部 `Seele/engine`、`agent/core/*` 引用；不修改 Seele 仓库 |

## 6. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| StreamCompleter 租约语义错配 | 账号并发超售 | 见 4.1：流式必须自实现 lease-until-EOF 适配器，集成指南检查项 6 纳入验收 |
| VisibilityPolicy 并发安全 | 多会话共享 runtime 时工具可见集串台 | 策略闭包持有不可变快照 + 原子替换（参考 `Registry.Snapshot` 语义） |
| 子代理上下文膨胀 | 并行节点复制大证据 | 每个节点 PromptBlock 走 seelexctx 预算压缩（compactor），上限由节点预算约束 |
| ContextController 误替换历史 | provider history 协议失效 | 迁移现有 `history_safety.go` 的合法 assistant/tool 配对规则与 checkpoint 标记清理 |
| 窗口/压缩范围误判 | 窗口内轮次被压缩或 provider 请求超预算 | N 由 `WindowPolicy` 推导 + 配置显式覆盖；压缩只作用窗口外且 `CompactFrame.From/To` 可审计；硬阈值先归档 tool result 再收缩窗口（保底 MinRounds） |
| 事件投影落后于终态校验 | task_complete 判定依赖投影未收敛 | 终态工具执行前强制投影 flush（事件同步写入），失败则按现有协议报 `task_failed` |

## 7. 验证

- `go build ./...`、`go vet ./...` 全绿（恢复构建阻断）。
- 切片级测试：装配/账号/可见性/会话/plan codec/节点作用域/task 投影/context controller/事件 sink。
- 集成验收（example 模式）：无 API Key 的确定性 completer 跑通"plan_load → DAG 并行 → 节点证据 → task_complete"全链路；GUI 构建（`-tags gui` + ldflags）通过。
