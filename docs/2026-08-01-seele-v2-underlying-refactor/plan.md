# Seelex 底层重构详细设计 — Seele v0.0.8 新边界

> 状态：已实施（2026-08-01 子代理流水线完成，`go build/vet/test ./...` 与 GUI 构建全绿）
> 实施记录：确认点决策见 §8；遗留说明见 §9
> 关联架构设计：[`docs/arch/seele-v2-runtime-architecture.md`](../arch/seele-v2-runtime-architecture.md)
> 前置文档：`docs/2026-07-29-planact-context-control/plan.md`（上下文/终态协议，语义保留）、`docs/2026-07-27-seele-workplan-parallel/requirements.md`（并行约束，语义保留）、`plan.md`（seelebridge/seelexctx 边界，继续有效）
> 新 Seele 示例：`example_Implement/08_composable_agent`（装配）、`09_context_pipeline`（上下文）、`10_workplan_codec`（Plan codec）

## 1. 目标与范围

把 seelex 底层从旧 Seele API（`engine`、`agent/core/*`、`workplan/runtime/serialize`）迁移到新装配模型，重点解决四件事：

1. **Plan→subagent**：子代理获得项目作用域工具、有界上下文与父证据，DAG 可真并行（`plan_run` 不再隐藏）。
2. **Task 与 Plan 分离**：Task 状态机只消费事件投影，不再直接调用 workplan 执行器内部状态。
3. **上下文控制**：迁移到 `seelectx` 契约（DurableHistory/Assembler/ToolResultProcessor/Compressor/ContextController），`engine.ReplaceHistory` 路径退役。
4. **存储模型**：落实项目级模块语义（跨会话共享）+ 会话级上下文存储（plan/task/skill/compact 使用栈 + 聊天队列滑动窗口），压缩只发生在窗口外，N 由策略决定而非魔法数字（§3.7，2026-08-01 用户澄清）。

不在范围：修改 Seele 仓库；重写 TUI/GUI 前端；改变 `application/model` 对外 DTO 字段；重做权限产品策略（仅迁移接线）。

## 2. 旧 → 新 API 映射表

### 2.1 装配与账号

| 旧（seelex 引用） | 新 |
|---|---|
| `agent.New(agent.Options{LLMConfig,...})` | `agent.NewWithComponents(agent.Components{Completer, StreamCompleter, Tools})` |
| `agt.LLM().(*api.ChatClient)` + `client.WithAccountPool(pool)` | `accountpool.New[agent.Completer]()` + `bridge.NewAccountCompleter(pool, WithAccountRequestSelector)` |
| `client.SetProvider(pf)` / `ProviderFilter()` | selector 闭包读 seelex 当前 provider 过滤（Runtime 持有，无强转） |
| `storage.NewStore` / `seelectx/storage.FileStore` | seelex `DurableHistory` 适配器（sessionstore.Router 后端） |
| `agt.Shutdown()` | `runtime.Close()`：pool 释放、registry/适配器关闭、事件 sink flush |

### 2.2 会话与工具钩子

| 旧 | 新 |
|---|---|
| `engine.New(agt, WithStore/WithSystemPrompt/WithTracer/WithHooks)` | `session.NewSession(SessionComponents{Agent, History, Context, Telemetry, Hooks, SessionID, ModelName})` |
| `eng.ChatStream(ctx, input, onChunk)` | `session.ChatStream(ctx, query, callback)`（主会话包装见 §4.2） |
| `eng.History()` / `ClearHistory()` / `ReplaceHistory(m)` | working history 在 Session 内；`Controller.Handle → ContextDecision.ReplaceHistory`；投影历史由 DurableHistory/EventStore 提供 |
| `ToolHookBridge.Hooks()`（OnToolStart/OnToolComplete/OnIterationComplete） | `SessionComponents.Hooks *session.LoopHooks`（字段同名）；观测/投影逻辑迁入 handler |
| `eng.SetSystemPrompt(p)` / `SetMaxLoops(n)` | `Context.SystemPrompt`（会话级）+ `PromptBlocks`（请求级）；迭代预算在 `react_budget.go` 中由 LoopHooks.OnIterationComplete 返回 `false` 或 Controller 决策 |
| `eng.SessionID()` | `SessionComponents.SessionID`（seelex 生成） |

### 2.3 工具 / 插件 / MCP / 权限

| 旧 | 新 |
|---|---|
| `builtin.RegisterAll(agt.Tools())` | `registry.Register(provider)`；无产品语义内置用 `tools/builtin.New()` |
| `holder.NewPluginManager()` / `WithPluginManager` / `ActivatePlugin` / `ActivePlugin` | `bridge.WithVisibilityPolicy(seelexVisibilityPolicy)`；插件 Manager 改为维护 include/exclude 快照（§4.4） |
| `agt.Tools().ToolCallTimeout` | `tools.NewRegistry(tools.WithCallTimeout(cfg.ToolCallTimeout))` |
| `agt.RegisterTool(name, desc, schema, fn)` | 自定义 `tools.ToolProvider` / `tools.ToolHandler` 注册 |
| `agent/core/tool/mcp`（Attach/Detach/Refresh） | seelex MCP `ToolProvider`（`tools/adapter`），连接/刷新/关闭由 seelex 生命周期管理 |
| `agent/core/tool/permission`（构建阻断） | `tools.WithMiddleware` 门控 + 审批 Provider；先移除不可构建接线，恢复审批 UI 语义 |
| `builtin.NewChatAgentFactory(agt.LLM())` / `WorkPlanTool` | `bridge.NewAgentFactory` + seelex `SeelexAgentNode`（§4.5） |

### 2.4 WorkPlan / Plan

| 旧 | 新 |
|---|---|
| `builtin.WorkPlanTool`（框架内 DAG 执行） | seelex 产品工具 `plan_load`/`plan_run`/`plan_clear`，内部 `codec.Import` + `workplan.NewFromPlan` + `runner.Run` |
| `workplan/runtime/serialize.PlanEdgeSpec` | `codec.NodeSpec[T]` / `EdgeSpec` / `codec.Document[T]` |
| `workplan/core/node.AgentFactory`（保留，`workplan/core` 仍兼容） | 首选 `bridge.NewAgentFactory`；seelex 自定义节点实现 `node.Node{ID, Run}` |
| `forkexec.BranchRuntime` + `resolvePlanBranchRuntime()` | 节点会话组件 + 事件投影；账号解析逻辑保留为节点级 selector |
| `SetPlanNodeCallback` / `HandlePlanNodeComplete` | `event.Sink` 投影（节点 queued/running/终态事件 → PlanState/Task 投影） |
| `workplan/sugar/approve` | 兼容保留或 seelex 自实现审批节点（`kind=approve`） |
| `PlanPolicy.RequireSerial/MaxForkConcurrency` | DAG 拓扑 + 节点类型决定并行；串行=线性边；并发上限在 seelex 节点执行层施加 |

### 2.5 上下文

| 旧 | 新 |
|---|---|
| `engine.ReplaceHistory` + `OnIterationComplete` 内压缩 | `ContextController.Handle(ContextEvent) → ContextDecision{ReplaceHistory, History}` |
| `seelectx.EstimateTokens/NeedCompression/TrimHistory/CompressHistory`（re-export） | 兼容周期内保留（`seelexctx/seele.go`）；新代码走 `token_counter` + seelectx 契约 |
| `seelexctx/provider.EngineProvider`（读 engine） | 读 `DurableHistory`/`SessionRecord`；`TraceProvider` 读 telemetry |
| `seelexctx/compactor.Compactor`（主循环未用） | 实现 `seelectx.Compressor`（`CompressionRequest → CompressionResult`），供 Controller 显式压缩 |
| checkpoint 消息 + `history_safety.go` 配对规则 | 保留实现，挂到 Controller 的 ReplaceHistory 路径 |
| `ToolHookBridge.OnIterationComplete` 触发 `compactTaskContext` | `ContextController` 在 `after_tool`/`after_assistant` 事件触发 |
| `engine.History()` 全量读 | ChatQueue（append-only，sessionstore ProviderHistory）+ `SessionContextRecord`（5 栈，state blob）+ 窗口按轮读（`ReadRange`/`ReadEventTail`） |
| 压缩范围（旧：整段历史） | 只压缩滑动窗口外的轮次；产物 push CompactStack（`now using compact context`，§3.7.4） |
| 跨会话承袭（seelexctx 四子包） | 项目级 `ProjectKnowledge`（跨会话共享）+ 会话记录（会话私有，§3.7.1/3.7.2） |
| 窗口 N（保留的最新轮数） | `WindowPolicy` 构造注入（配置 + provider 推导，非魔法数字；§3.7.3、确认点 5） |

### 2.6 遥测 / 事件 / 错误

| 旧 | 新 |
|---|---|
| `seelectx/tracer.Tracer` / `eng.ExportTrace()` | `telemetry.NewMemoryTracer()` + `NewLifecycleHook`；`tracer.Query` 供 GUI/TUI |
| `application/event.go` EventHub | 保留（前端事件） + 新 `event.Sink`（执行事实）双轨：Sink→sessionstore 事件库，EventHub→UI 快照 |
| `errors`（字符串匹配） | `seeleerrors.From(err)`（Struct/Function/Step/Path） |

## 3. 核心接口设计（目标代码）

### 3.1 seelebridge.Runtime（composition root）

```go
// seelebridge/runtime.go — 目标形态（对外 facade 保持，内部装配重建）
type Runtime struct {
    registry   *tools.Registry
    pool       *accountpool.Pool[agent.Completer] // P2C 租约
    completer  agent.Completer                    // 同步（非流式路径）
    streamer   agent.StreamCompleter              // 流式，lease-until-EOF
    agt        *agent.Agent
    session    *session.Session                   // 主会话
    sink       event.Sink                         // 执行事实 → sessionstore 事件库
    tracer     *telemetry.MemoryTracer
    hook       telemetry.Hook
    planMgr    *PlanManager                       // codec 导入 + workplan 装配 + 投影
    taskSvc    *TaskService                       // Task 状态机（§3.4）
    ...
}

func NewRuntime(cfg RuntimeConfig) (*Runtime, error)
// cfg: AccountsPath/StorePath/ToolCallTimeout 语义不变；新增 PlanEventSinkPath 等
```

装配顺序（`NewRuntime` 内，与集成指南一致）：

```go
// 1. 账号池
pool := accountpool.New[agent.Completer]()
for _, acc := range accounts { pool.Register(accountpool.Account[agent.Completer]{
    ID: acc.Name, Value: clientFor(acc), MaxConcurrency: acc.MaxConcurrency,
    Metadata: map[string]string{"provider": acc.Provider, "model": acc.Model},
}) }
selector := seelexAccountSelector(pool, &r.state) // 闭包读 provider 过滤/选中账号

// 2. 工具注册表
registry := tools.NewRegistry(tools.WithCallTimeout(cfg.ToolCallTimeout),
    tools.WithMiddleware(permissionGate, auditMiddleware))
registry.Register(seelexWorkspaceProvider{scope: projectScope})   // 文件/Git/工作区（带作用域校验）
registry.Register(planProvider{planMgr: r.planMgr})               // plan_load/plan_run/plan_clear
registry.Register(taskTerminalProvider{taskSvc: r.taskSvc})       // task_complete/failed/needs_user_decision
registry.Register(mcpProvider{stack: r.MCPStack})                 // MCP adapter
registry.Register(skillProvider{skills: skillManager})            // Skill 工具
registry.Register(builtin.New())                                  // 无产品语义工具
registry.Refresh(ctx)

// 3. Agent 装配
runtime, _ := bridge.NewRegistryRuntime(registry,
    bridge.WithVisibilityPolicy(seelexVisibilityPolicy{manager: pluginMgr}))
agt, _ := agent.NewWithComponents(agent.Components{
    Completer: r.completer, StreamCompleter: r.streamer, Tools: runtime,
})

// 4. 主会话
hist := sessionstore.NewDurableHistory(storeRouter, sessionID)     // §3.2
sess, _ := session.NewSession(session.SessionComponents{
    Agent: agt, History: hist,
    Context: session.ContextComponents{
        SystemPrompt:        "", // 会话级由 effort/skill 动态生成时经 Assembler
        Assembler:           seelexAssembler{...},   // §3.5
        PromptBlocks:        nil,                    // 请求级由 TaskService 注入
        ToolResultProcessor: seelexToolResultProcessor{...}, // §3.5
        Compressor:          seelexCompressor{...},  // §3.5
        Controller:          seelexContextController{...},  // §3.5
    },
    Hooks:      &session.LoopHooks{OnToolStart: ..., OnToolComplete: ..., OnIterationComplete: ...},
    Telemetry:  r.hook,
    SessionID:  sessionID, ModelName: r.model,
})

// 5. WorkPlan 工厂（供 plan_run 使用，§3.3）
agentFactory, _ := bridge.NewAgentFactory(agt,
    bridge.WithSessionComponents(nodeSessionComponents),
    bridge.WithSessionID(func(systemPrompt string) string { return derivedNodeSessionID(systemPrompt) }),
)
```

### 3.2 sessionstore → seelectx.DurableHistory 适配

```go
// sessionstore/durable_history.go
type DurableHistory struct {
    router    *Router       // 现有 JSON/SQLite/PG/Redis 后端
    sessionID string
    projection ProjectionIO // TaskContextProjection 读写（现有模型）
}
func (d *DurableHistory) Load(ctx context.Context) ([]types.Message, error)  // 现有 LoadHistory 语义
func (d *DurableHistory) Save(ctx context.Context, msgs []types.Message) error // SaveState + ProviderHistory
func (d *DurableHistory) Clear(ctx context.Context) error                     // Reset 语义（显式、可失败）
```

契约：Session 每次 Chat 前 `Load`、后 `Save`；`Reset(ctx)` 显式清空（对应旧 `ClearHistory`+store 清理）。`SessionRecord`/`TranscriptEvent`/`ToolResults` 的持久化继续由 Router 负责，DurableHistory 只编排 provider 消息。会话级 `SessionContextRecord`（SystemPrompt + 五栈）走 Router state blob（`WriteState/ReadState`），ChatQueue 即 ProviderHistory；滑动窗口按轮读取（§3.7.2）。

### 3.3 Plan：NodeFactory 与节点语义

```go
// seelebridge/plan_factory.go — 产品 DSL → node.Node
type SeelexNodeInput struct {
    ID    string `json:"id"`
    Input string `json:"input"`
    Kind  string `json:"kind"` // agent | auto | function | approve | verify | deliver
}

func buildNode(spec codec.NodeSpec[SeelexNodeInput]) (node.Node, error) {
    switch spec.Data.Kind {
    case "agent":
        return newSeelexAgentNode(spec, agentFactory, nodeScopeFactory), nil // §3.3.1
    case "approve":
        return approvalGateNode{...}, nil
    default: // auto/function/verify/deliver：确定性执行
        return node.NewTypedFunctionNode[SeelexNodeInput, string](spec.Data.ID, runProductNode), nil
    }
}

// plan_load 工具内部：
plan, err := codec.Import[SeelexNodeInput](payload,
    codec.NodeFactoryFunc[SeelexNodeInput](buildNode)) // 校验 + Seal
// plan_run 工具内部：
wp := workplan.NewFromPlan(plan, agentFactory,
    workplan.WithEventSink(sink, planID),
    workplan.WithEventRunID(runID),
    workplan.WithEventHeartbeatPolicy(event.HeartbeatPolicy{Interval: 15 * time.Second}),
    workplan.WithEventLocators(agent.EventLocator{AgentID: mainAgentID, SessionID: sessionID},
        workplan.EventLocator{PlanID: planID, RunID: runID}),
)
result, err := runner.New(wp.Plan()).Run(ctx) // 或 wp.Run(ctx)（按 workplan 实际入口）
```

#### 3.3.1 SeelexAgentNode — 子代理作用域包装

```go
// seelebridge/agent_node.go
type SeelexAgentNode struct {
    id       string
    inner    node.Node            // bridge.NewAgentFactory 产物
    scope    *nodeScope           // 节点级作用域：允许工具集、证据块、预算
    parent   seelexctx.ContextSnapshot  // 父证据（snapshot 承袭）
}
func (n *SeelexAgentNode) ID() string { return n.id }
func (n *SeelexAgentNode) Run(ctx context.Context, wc *types.WorkflowContext) (string, error) {
    // 1) 注入节点身份（VisibilityPolicy/middleware 读取）
    ctx = nodeScopeContext(ctx, NodeScope{NodeID: n.id, Role: n.scope.role,
        BranchID: n.scope.branchID, WorkspaceID: n.scope.workspaceID})
    // 2) 节点上下文（PromptBlocks）：目标 + 父证据 + 预算
    ctx = nodeContextComponents(ctx, n.scope.promptBlocks())
    return n.inner.Run(ctx, wc)
}
```

`WithSessionComponents` 提供的组件里 `PromptBlocks` 由节点 scope 动态提供（goal、evidence、剩余工作），`Assembler` 使用 seelex 装配器。工具作用域通过两处生效：

- `VisibilityPolicy(ctx, tools)`：读取 ctx 中 `NodeScope`，按节点角色过滤（主代理=全部，subagent=项目作用域+deliver 工具）；
- Registry `WithMiddleware`：`Dispatch` 前再次校验（防猜测调用隐藏工具，`ErrToolNotVisible` 语义）。

账号：节点级 selector 复用 `ResolveAccountForBranch` 的确定性 hash 逻辑，但作为 `WithAccountRequestSelector` 的输入（completer 层），不再构造旧 `forkexec.BranchRuntime`。

### 3.4 TaskService（与 Plan 分离）

```go
// application/core/task_service.go（由 task_execution.go 演进）
type TaskService struct {
    state     *taskExecutionState          // 功能打点快照（feature-instrumentation 打点表）；终态即亡，不持久化
    projection PlanProjectionReader        // 事件投影（NodeStatus/PlanStatus），注入，非直连 workplan
    terminals terminalToolHandlers         // task_complete/failed/needs_user_decision
}

// 输入面（全部为观察/事件，不访问执行器内部）：
func (s *TaskService) ObserveTool(ctx, ToolObservation) error            // LoopHooks.OnToolStart/Complete
func (s *TaskService) ObservePlanEvent(ctx, PlanEvent) error             // event.Sink 订阅（节点 queued/running/终态）
func (s *TaskService) ObserveModelOutput(ctx, ModelOutput) error         // 会话回复（自然终态判定）
func (s *TaskService) OnChatEnd(ctx, ChatEndSummary) (TaskState, error)  // finalizeTaskExecution 演进

// 终态校验（不再读执行器）：
func (s *TaskService) verifyCompletion(ctx, t taskTerminal) error {
    nodes := s.projection.AllNodes()        // ← 事件投影累积
    missing := nodesNotIn(nodes, t.CompletedNodes)
    if len(missing) > 0 { return incompletePlanError(missing) }
    ...
}
```

要点：
- `task_complete`/`task_failed`/`task_needs_user_decision` 注册为 Registry 产品工具（`taskTerminalProvider`），handler 内 `TaskService.VerifyAndApply`；
- 事件投影在终态工具执行前**同步 flush**（Sink 追加返回后再判定），保证投影不滞后（架构文档风险 5 的落地）；
- 快照不持久化：`TaskCheckpoint`/`continuationTaskExecutionState` 的生命周期在任务终态（或会话结束）结束；排队输入续接的最小记录（objective + 排队输入引用）随 `TaskFrame` 与事件投影保存，恢复不依赖快照；
- `react_budget.go`（无进展/轮数预算）保留：决策输入来自 TaskService 的语义进展计数（epoch），不再从工具输出 hash 推断。

### 3.5 上下文适配层

```go
// seelexctx/assembler.go
type seelexAssembler struct {
    effort     EffortPromptProvider // system 提示（effort/skill/任务摘要）
    resolver   seelectx.PlaceholderResolver
}
func (a seelexAssembler) Assemble(ctx, req seelectx.AssemblyRequest) (seelectx.AssembledRequest, error) {
    blocks := a.dynamicBlocks(ctx)      // plan authority / task checkpoint / evidence
    return seelectx.PlaceholderRequestAssembler{Resolver: a.resolver}.
        Assemble(ctx, seelectx.AssemblyRequest{Blocks: append(req.Blocks, blocks...),
            WorkingHistory: req.WorkingHistory})
}

// seelexctx/processor.go
type seelexToolResultProcessor struct{ limit int }
func (p seelexToolResultProcessor) Process(ctx context.Context, r seelectx.ToolResult) (seelectx.ToolResultView, error) {
    if len(r.Raw) > p.limit { return seelectx.ToolResultView{Content: toolResultOmittedPrefix + ref(r)}, nil }
    return seelectx.ToolResultView{Content: r.Raw}, nil // 结果引用交给 read_tool_result
}

// seelexctx/compressor.go — seelexctx/compactor 适配
type seelexCompressor struct {
    compactor *compactor.Compactor // 三级预算压缩（跨会话承袭场景）
    quickChat seelectx.QuickChat   // 无工具隔离压缩
    shortThreshold int             // 短对话免 LLM（RecursiveCompressor 语义）
}
func (c seelexCompressor) Compress(ctx, req seelectx.CompressionRequest) (seelectx.CompressionResult, error) {
    if len(req.History) < c.shortThreshold { return seelectx.CompressionResult{Messages: req.History}, nil }
    return c.quickChat.Complete(...) // 结构化 checkpoint → CompressionResult
}

// seelexctx/controller.go — 原 context_controller.go 决策迁移
type seelexContextController struct {
    policy  ContextWindowPolicy  // 软 50% / 硬 75% 阈值（2026-07-29 文档决策，构造注入）
    window  WindowPolicy         // 滑动窗口轮数 N（§3.7.3，非魔法数字）
    stacks  *SessionContextStore // 5 栈读写（§3.7.2）
    budget  BudgetProvider       // Runtime.ContextWindow / MaxOutputTokens
    tokens  TokenCounter         // seelex token_counter
    archive TaskEvidenceStore    // 片段证据归档（会话内）
}
func (c *seelexContextController) Handle(ctx context.Context, ev seelectx.ContextEvent) (seelectx.ContextDecision, error) {
    switch ev.Kind {
    case seelectx.ContextAfterTool:
        if oversizedTool(ev.Tool) { return c.archiveAndCompact(ctx, ev), nil } // 硬阈值路径
    case seelectx.ContextAfterAssistant:
        if c.segmentClosed(ev) { ... } // 片段闭合软压缩（plan 节点完成/阶段 checkpoint）
    }
    return seelectx.ContextDecision{}, nil
}
```

迁移边界：`context_controller.go` 中 `prepareExecutionContext/rejectOversizedToolResults/protectOversizedCurrentInputLocked/fitExecutionHistory` 的**决策逻辑**迁入 Controller 与 Assembler；`history_safety.go` 的合法配对规则与 checkpoint 标记清理保留，挂到 `ReplaceHistory` 前。`transcriptTailHistory`（task_context_state.go）作为 Assembler 的 WorkingHistory 源。

### 3.6 流式 Completer（lease-until-EOF）

```go
// seelebridge/stream_completer.go
type streamingAccountCompleter struct {
    pool     *accountpool.Pool[agent.Completer]
    selector *accountSelector // provider 过滤 + 账号选择（共享闭包）
}
func (c *streamingAccountCompleter) CompleteStream(ctx, msgs, tools, onChunk) (string, string, []types.ToolCall, error) {
    lease, err := c.pool.Acquire(ctx, accountpool.AcquireRequest{Selector: c.selector.Select})
    if err != nil { return "", "", nil, err }
    defer lease.Release()   // ← 覆盖整个流生命周期（EOF/错误/Close 均幂等释放）
    return c.streamClient(ctx, lease.Value, msgs, tools, onChunk)
}
```

同步路径 `bridge.NewAccountCompleter`（release 每次 Complete 后）；流式路径必须独立适配（集成指南第 3 章）。`tool_choice=plan_load` 的规划会话使用**独立 Completer 实例**（无账号选择副作用）。

### 3.7 存储模型设计（模块 A–D）

> 背景：2026-08-01 用户澄清的存储内核（架构文档 §4.8）。模块拆分：A=项目级知识、B=会话级 5 栈 + 队列、C=窗口策略、D=压缩范围。

#### 3.7.1 模块 A：ProjectKnowledge（项目级模块语义，跨会话共享）

```go
// sessionstore/project_record.go（新增；JSON/SQLite/PG/Redis 四后端实现）
type ProjectRecord struct {
    Version      string            `json:"version"`       // 内容 hash（由 SourceHashes 推导）
    Modules      []ModuleSemantics `json:"modules"`
    SourceHashes []string          `json:"source_hashes"` // 来源文件 hash → 增量重建判定
    BuiltAt      time.Time         `json:"built_at"`
}
type ModuleSemantics struct {
    Name    string   `json:"name"`
    Summary string   `json:"summary"`        // 语义说明（职责/边界）
    Path    string   `json:"path"`           // 模块路径
    Docs    []string `json:"docs,omitempty"` // 文档/接口索引
}

// Repository 新增两个方法（四后端各自实现）：
//   WriteProjectRecord(ctx, projectID string, record ProjectRecord) error
//   ReadProjectRecord(ctx, projectID string) (ProjectRecord, error)
```

- 来源可插拔：模块文档目录 + 模块元数据（如 `gui/module_dotting.json` 的职责字段）+ 可选手工 `seelex.project.md`；由 seelex 工具 `project_refresh` 扫描重建。
- 会话开始：Assembler 首次请求前 `ReadProjectRecord`，内容 hash 未变则复用内存缓存；渲染为 `project` 块。
- 只读契约：会话不得写入；重建失败保留上一版本（可回退），首建失败显式跳过并提示。

#### 3.7.2 模块 B：SessionContextStore（5 栈 + 队列）

```go
// sessionstore/session_context.go（新增）
type SessionContextRecord struct {
    SchemaVersion int            `json:"schema_version"`
    SystemPrompt  string         `json:"system_prompt"`  // 会话级基础提示（永不压缩，始终完整进入请求）
    PlanStack     []PlanFrame    `json:"plan_stack"`     // now using plan = 栈顶
    TaskStack     []TaskFrame    `json:"task_stack"`     // now using task
    SkillStack    []SkillFrame   `json:"skill_stack"`    // now using skill
    CompactStack  []CompactFrame `json:"compact_stack"`  // now using compact context
}
type PlanFrame struct {
    PlanID    string        `json:"plan_id"`
    Title     string        `json:"title"`
    Status    string        `json:"status"` // active | closed
    Nodes     []NodeSummary `json:"nodes"`
    EnteredAt time.Time     `json:"entered_at"`
    ClosedAt  *time.Time    `json:"closed_at,omitempty"`
}
type TaskFrame struct {
    TaskID    string        `json:"task_id"`
    Objective string        `json:"objective"`
    Status    string        `json:"status"` // active | completed | failed | needs_user_decision
    Evidence  []EvidenceRef `json:"evidence,omitempty"`
}
type SkillFrame struct {
    SkillID string `json:"skill_id"`
    Name    string `json:"name"`
}
type CompactFrame struct {
    SegmentID    string       `json:"segment_id"`
    From, To     int          `json:"from,to"`          // 被压缩的轮次范围（ChatQueue 单元索引）
    Summary      string       `json:"summary"`          // 结构化摘要 / checkpoint
    Evidence     []EvidenceRef `json:"evidence,omitempty"`
    CompressedAt time.Time    `json:"compressed_at"`
}

type SessionContextStore struct{ ... } // 读/写 state blob（Router.WriteState/ReadState）+ 内存缓存
func (s *SessionContextStore) PushPlan(f PlanFrame) error       // plan_load/plan_run 进入时
func (s *SessionContextStore) CloseTopPlan(planID string) error // 终态/被取代时
func (s *SessionContextStore) PushTask(f TaskFrame) error
func (s *SessionContextStore) CloseTopTask(taskID string) error // 终态工具接受后
func (s *SessionContextStore) PushSkill(f SkillFrame) error
func (s *SessionContextStore) PopSkill(skillID string) error
func (s *SessionContextStore) PushCompact(f CompactFrame) error
func (s *SessionContextStore) Snapshot() SessionContextRecord  // 供 Assembler 渲染（拷贝）
```

- `TaskStack` 是 Task 的**存储方式**：帧由事件投影降级生成，TaskService 不直接写存储；`TaskExecutionState` 是任务期间的**功能打点快照**（`docs/feature-instrumentation.md` 打点表与北极星指标），终态即亡、不持久化、不承担恢复（§3.4）。
- 持久化：`SchemaVersion` 校验；损坏/不兼容时拒绝加载并显式失败（不静默重建），走会话恢复错误路径。
- ChatQueue = ProviderHistory（append-only；轮 = `completeEventUnits` 单元）。窗口不单独存储，按轮读取：`window := ReadEventTail(sessionID, windowTokens, N)`（token 预算 + 单元数双限；短读用 `ReadRange` 语义）。
- SystemPrompt 永不压缩：始终完整进入 provider 请求，不写入 ProviderHistory 消息、不被压缩或截断（会话不变量）。

#### 3.7.3 模块 C：WindowPolicy（N 的确定机制，非魔法数字）

```go
// application/core/window_policy.go（新增）
type ProviderContextInfo struct {
    ContextTokens  int // provider/model 上下文窗口
    AvgRoundTokens int // token_counter 按最近完整单元估算
    ReservedTokens int // system prompt + 栈块固定预留
    ConfigRounds   int // 用户显式配置 window.rounds（0 = 未配置）
}
type WindowPolicy interface {
    WindowRounds(ctx context.Context, info ProviderContextInfo) (int, error)
}
type DefaultWindowPolicy struct {
    Ratio, MinRounds, MaxRounds int // 来自配置（window 配置段），代码不硬编码常量
}
// N = clamp((ContextTokens×Ratio − Reserved) ÷ AvgRoundTokens, MinRounds, MaxRounds)
// ConfigRounds > 0 时直接覆盖
```

- 注入点：`seelexContextController` 与 Assembler（窗口读参数同源）。
- 决策顺序：配置显式覆盖 > provider 推导（clamp）> 出错时保守回退 MinRounds。

#### 3.7.4 模块 D：压缩范围与 CompactStack（只压窗口外）

```go
// seelexctx/controller.go — 窗口外压缩（帧 = 合并上一栈顶帧 + 新溢出轮次，栈顶自足）
func (c *seelexContextController) Handle(ctx context.Context, ev seelectx.ContextEvent) (seelectx.ContextDecision, error) {
    n, err := c.window.WindowRounds(ctx, c.info())           // §3.7.3
    units := c.chatUnits(ev)                                 // 完整协议单元
    if len(units) <= n { return seelectx.ContextDecision{}, nil }
    overflow := units[:len(units)-n]                         // 窗口外轮次（相对上次压缩的新溢出部分）
    if len(overflow) == c.lastCompactedLen { return seelectx.ContextDecision{}, nil }
    frame := c.compressOutside(ctx, overflow)                // 合并上一栈顶帧 → 综合摘要（栈顶自足）
    _ = c.stacks.PushCompact(frame)                          // 只存；活跃会话只渲染新栈顶
    projected := c.project(ctx)                              // 栈块（compact=栈顶）+ 窗口（Assembler 渲染）
    return seelectx.ContextDecision{ReplaceHistory: true, History: projected}, nil
}
```

- 触发点：`after_assistant`/`after_tool` 片段闭合时压缩窗口外（软阈值）；硬阈值超限时先归档超大工具输出为 `result_ref`（ToolResultProcessor 路径），仍超限才收缩窗口（不得低于 MinRounds），新移出窗口的轮次进入压缩。
- 读取规则：活跃会话只渲染**栈顶一帧**（帧 Summary 为"该时刻窗口外全部轮次的综合摘要"：合并上一栈顶帧生成，栈顶自足，无需截断渲染）；更早帧仅存储，用于 (a) fork 新会话时恢复压缩上下文、(b) 修改会话内容重新会话时重建、(c) 重新打开会话时取出栈顶。(a)(b)(c) 为**扩展预留点**，本阶段不实现（目标形态参考代码图谱式上下文重建；否决向量模型方案，见架构文档 §4.8.1）——帧的结构化字段即接缝，届时只消费帧结构、不改存储层。
- 审计：`CompactFrame.From/To` 与 ChatQueue 单元索引对应，可断言"窗口外才被压缩"不变量。

## 4. 垂直切片实施步骤

> 每片保持可构建、可测试；编号即实施顺序。`go build ./...` 恢复从切片 1 开始。

### 切片 1 — 装配层重建（恢复构建）

| 项 | 内容 |
|---|---|
| 文件 | `seelebridge/runtime.go`、`seelebridge/config.go`、新增 `seelebridge/accounts.go`、`seelebridge/stream_completer.go` |
| 动作 | 删除 `agent.New`/`api.ChatClient`/`api.AccountPool` 引用；改为 accountpool+completer+registry+agent.NewWithComponents；Runtime 暂不持有 Session（主会话仍走旧路径则双轨） |
| 验证 | `go build ./...` 恢复；`seelebridge` 单测（账号空池、selector、lease 释放） |
| 阻断依赖 | 本片内其它文件对 `engine` 的引用先以 compile-tag 隔离或顺序排后 |

> 注：切片 1 与切片 2 必须成对推进——`engine` 引用遍布主链路，先迁移装配、再迁移会话才能让 `go build` 真正转绿。若仓库要求每 commit 可构建，将切片 1+2 合并为"装配+会话"一个切片。

### 切片 2 — 会话与历史

| 项 | 内容 |
|---|---|
| 文件 | `seelebridge/runtime.go`（Session 持有）、`sessionstore/durable_history.go`、`sessionstore/session_context.go`（新增）、`application/core/chat.go`（入口改 `Runtime.Session`）、`seelebridge/storage.go`（改 DurableHistory 编排） |
| 动作 | `engine.Engine` → `session.Session`；LoopHooks 接管 ToolHookBridge 观测；`History()/ClearHistory/ReplaceHistory` 调用点迁移（`session_history.go`、`history_safety.go`）；`SessionContextRecord`（SystemPrompt + 5 栈）读写 state blob；窗口按轮读取（`ReadEventTail`/`ReadRange`） |
| 验证 | 会话持久化往返测试（JSON/SQLite）；`session` 串行约束（并发 Chat 拒绝）；会话恢复 = 记录 + 窗口重建；窗口内/外轮次读取断言（§3.7.2） |

### 切片 3 — 插件/可见性/MCP/权限

| 项 | 内容 |
|---|---|
| 文件 | `seelebridge/plugins.go`、`seelebridge/scoped_tools.go`、`seelebridge/mcp.go`、`application_adapters.go`、`plugin/manager.go` |
| 动作 | `holder` 删除：Plugin Manager 输出不可变 include/exclude 快照；`seelexVisibilityPolicy` 实现；MCP 改 ToolProvider；权限改 middleware（先解阻断，审批语义等价） |
| 验证 | 插件切换后 VisibleTools 过滤断言；隐藏工具 Dispatch 返回 `ErrToolNotVisible`；MCP fake provider 状态测试 |

### 切片 4 — Plan 内核迁移（codec + 事件）

| 项 | 内容 |
|---|---|
| 文件 | `seelebridge/plan.go`、`plan_input_adapter.go`、`plan_tool_provider.go`、`plan_policy.go`、新增 `seelebridge/plan_factory.go`、`seelebridge/plan_events.go` |
| 动作 | `plan_load` 改走 `codec.Import[SeelexNodeInput]`；`plan_run` 改走 `workplan.NewFromPlan` + runner；`serialize.PlanEdgeSpec` 引用删除；NodeResult → 事件投影（`event.Sink` → sessionstore 事件库） |
| 验证 | DSL 导入/环检测/topo/策略测试保留；`HandlePlanNodeComplete` 改投影后状态一致（`plan_gate_test.go` 语义不变） |

### 切片 5 — Plan→subagent（作用域与并行）

| 项 | 内容 |
|---|---|
| 文件 | 新增 `seelebridge/agent_node.go`、`seelebridge/node_scope.go`；`seelebridge/branch.go`（重构）；`application/core/chat.go`（`plan_run` 恢复可见） |
| 动作 | `bridge.NewAgentFactory` 接入；`SeelexAgentNode` 注入 NodeScope；节点级 PromptBlocks/证据/预算；账号 hash 解析保留为 selector；`withoutAuthoritativePlanMutationTools` 删除，`plan_run` 在 authoritative 模式可见 |
| 验证 | DAG 两分支并行（确定性 completer）；子代理只看到作用域工具；隐藏工具被拒；父证据块进入节点请求；节点事件流入投影 |

### 切片 6 — Task 分离

| 项 | 内容 |
|---|---|
| 文件 | `application/core/task_execution.go` → `task_service.go`（新增接口，原文件瘦身）、`application/core/chat.go`（终态处理改 TaskService）、`application/model/state.go`（DTO 不变） |
| 动作 | 终态工具注册进 Registry；`completeAuthoritativePlanLocked` 改为投影校验；`finalizeTaskExecution` → `OnChatEnd`；投影 flush 契约（同步写后再判定） |
| 验证 | 投影未收敛时 `task_complete` 拒绝；自然回复自动终态不变；续接任务（queued inputs）语义保持 |

### 切片 7 — 上下文控制迁移

| 项 | 内容 |
|---|---|
| 文件 | 新增 `seelexctx/assembler.go`、`seelexctx/processor.go`、`seelexctx/compressor.go`、`seelexctx/controller.go`；`sessionstore/project_record.go`（新增）、`application/core/window_policy.go`（新增）、`application/core/context_controller.go`（决策迁出后瘦身为装配/工具函数）、`seelexctx/provider/engine.go`、`seelexctx/compactor`（适配 Compressor） |
| 动作 | `ContextComponents` 五件套注入主会话与节点会话；`OnIterationComplete` 压缩调用删除（改 Controller）；`engine.ReplaceHistory` 路径退役；Assembler 渲染 project/plan/task/skill/compact 栈块（now using = 栈顶，§3.7.2）；Controller 只压缩窗口外轮次 → CompactStack（§3.7.4）；`WindowPolicy` 注入（§3.7.3）；`project_refresh` 构建 ProjectKnowledge（§3.7.1） |
| 验证 | 软/硬阈值触发（token 注入 fake）；片段闭合压缩后保留 goal/plan/evidence；checkpoint 标记不入持久化/前端；`history_safety` 配对规则测试；窗口外被压、窗口内原样（`CompactFrame.From/To` 单元索引断言）；ProjectKnowledge 会话前可读、内容 hash 未变则复用 |

### 切片 8 — 遥测与事件

| 项 | 内容 |
|---|---|
| 文件 | `seelebridge/trace.go`、`seelexctx/provider/trace.go`、`application/event.go`（双轨）、`tui/*`、`gui/*`（只读适配） |
| 动作 | telemetry Hook/Tracer 接入；trace 视图 Query 化；执行事实走 Sink→sessionstore 事件库；`seeleerrors.From` 替换字符串错误匹配 |
| 验证 | 生命周期事件（llm/tool intent-effect）；GUI 构建（`-tags gui` + `-ldflags "-X main.DefaultFrontend=gui"`） |

### 切片 9 — 账号池与规划会话收尾

| 项 | 内容 |
|---|---|
| 文件 | `seelebridge/plan_preflight.go`（隔离规划会话）、`seelebridge/branch.go`（account selector 收尾）、`seelebridge/replan_guard.go`（复用，无改动） |
| 动作 | 规划/重规划走独立 Completer+无工具会话；`forkexec` 引用删除；流式 lease 覆盖 EOF 的测试 |
| 验证 | 规划回合不消耗主账号 lease；流式中断释放 lease；`go test -race` |

### 切片 10 — 清理与文档

| 项 | 内容 |
|---|---|
| 动作 | 删除全部旧引用与过渡 shim（`seelexctx/seele.go` 旧 re-export 视需要保留）；更新 `DESIGN.md`/模块 README/`docs/README.md`；CI 全绿 |
| 验证 | `go build/vet/test ./...`；`git diff --check`；覆盖率报告 |

## 5. 测试策略

- **单元**：新接口（DurableHistory、VisibilityPolicy、NodeFactory、TaskService、Controller、streaming completer）各 ≥85% 行覆盖；现有 `plan_test/replan_guard_test/context_controller_test/history_safety_test` 语义不降级。
- **集成（无真实 LLM）**：确定性 `scriptedCompleter`（example 08 模式）跑通 `plan_load → DAG 并行（2 分支）→ 节点证据 → task_complete`；压缩路径用 fake QuickChat（example 09 模式）。
- **并发**：`go test -race`；多会话共享 registry 时可见性策略并发；并行节点租约并发。
- **回归**：`application/core/race_test.go`、`sessionstore` 往返、TUI/GUI 快照契约（`layout_test.go`/`docs_contract_test.go` 语义保持）。

## 6. 回滚方案

1. 切片 1–2 合并为单一"装配+会话"切片（每 commit 可构建）；回滚只 revert 该切片。
2. 切片 4–5 的 Plan 迁移保留旧 `plan_load` 工具注册路径一个版本（feature flag 切回线性清单模式）。
3. 切片 7 的 Controller 以 `ContextWindowPolicy` flag 选择新旧路径；新 Store 写失败保留未压缩 history 并显式失败（不删证据，语义沿用 2026-07-29 文档）。
4. 切片 8 双轨事件：Sink 故障进 ErrorHandler，不影响 WorkPlan 控制流（`event/README.md` 语义）。
5. 前端 DTO 不变，回滚无需前端配合。

## 7. 明确不在本次范围

- 修改 Seele 仓库或升级 go.mod 版本（go.work 本地工作区维持）。
- 在 seelex 内重写 Agent/Session/WorkPlan 内核（全部复用新根模块）。
- 重做权限产品策略（本次只恢复构建与等价门控）。
- 改变 `application/model` 对外 DTO（GUI/TUI 无破坏性变更）。
- FreeCAD/MCP 业务栈、GUI 新功能。

## 8. 计划确认点

编码前需确认：

1. 切片 1/2 是否合并为单一切片（取决于"每个 commit 必须可构建"的仓库约束）。
2. `workplan` 运行入口选择：`workplan.NewFromPlan(...).Run(ctx)` 还是 `runner.New(plan).Run(ctx)`（以 workplan 模块当前实际导出为准，两条路径在切片 4 开始时核对）。
3. `plan_run` 恢复可见后，串行要求的 `PlanPolicy.RequireSerial` 语义改为"节点类型 + 边拓扑"约束，是否接受。
4. 旧 `seelexctx/seele.go` re-export（EstimateTokens 等）兼容窗口保留时长。
5. 滑动窗口 N 的默认值机制：`DefaultWindowPolicy` 的 ratio/MinRounds/MaxRounds 默认参数与配置段命名（provider 上下文窗口推导，代码不硬编码常量；§3.7.3）。

### 已决（2026-08-01 实施时拍板）

1. **切片 1+2 合并**为单一切片执行（仓库每状态可构建）。
2. **运行入口 = `workplan.NewFromPlan(plan, factory, opts...).Run(ctx)`**（API 映射实证：`runner.New(plan).Run(ctx)` 会新建空 EventConfig 的 runner，所有 WithEvent* 选项静默丢失——事件永不发出且无错误提示；`runner.New` 仅对无 sink 的纯函数节点计划等价）。
3. **RequireSerial 语义接受**"节点类型 + 边拓扑"约束：并行 = DAG 无依赖分支；串行 = 线性边；并发上限在 seelex 节点执行层施加。
4. **seele.go 兼容窗口**：切片 10 清理后仅保留仍被引用的 re-export（EstimateTokens/TrimHistory/CompressHistory 等经 seelectx ctx_manager 重导出），新代码走 seelectx 契约与 token 估算。
5. **WindowPolicy 默认参数**：ratio=0.7（float64，plan.md §3.7.3 类型草图 int 已修正）、MinRounds=4、MaxRounds=40；配置段 `window`（字段 `rounds`/`ratio`/`min_rounds`/`max_rounds`，0 值 = 未配置）；代码不硬编码常量，默认值经配置加载注入。

## 9. 实施遗留说明（2026-08-01）

- `seelebridge/accounts.go` 与 `plan_preflight.go` 的每账号 client 工厂仍经 `agent/core/api.NewChatClient`（包在本地 Seele 磁盘仍存在故可编译，无 `agt.LLM().(*api.ChatClient)` 类型断言）；API 映射确认为兼容窗口允许并存，随 Seele 该包删除时移除。
- `workplan/sugar/approve`（runtime.go、plan_factory.go、application_adapters.go、plan_kernel_test.go）与 `seelectx/storage`（storage.go、sessionstore.go）仍被引用（磁盘存在可编译）；approve 节点为 seelex ApprovalGate 的兼容层，storage 适配已收敛到 DurableHistory。
- `application/core/context_controller.go` 决策主体（软/硬阈值、prepareExecutionContext）暂保留原样运行（经 `enginePort.ReplaceHistory` 旧路径）；seelexctx 侧 `seelexContextController`（新契约）已接线可用——application 侧决策迁出为后续工作项。
- 主会话 `SessionContextStore` 绑定（AttachSessionContextStore seam）需 sessionID 就绪后接线，随会话恢复流程落地。
- **强制规划已移除（2026-08-01 同日决策）**：`PlanPolicy.RequirePlan`、`PlanActScope`、主链路 preflight 注入（`AcquirePlanActScope`/`runPlanPreflight`/authority envelope）整体删除——规划是模型的自愿决策，不设聊天入口门槛（旧文档"权威 preflight"为失败设计）；`PrepareReplan`（显式）保留隔离规划会话与强制 tool_choice。冒烟测试 high 阶段同步改为自愿 plan_load 验证。
