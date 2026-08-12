# Seelex 上下文控制 / 数据流架构评审

> **状态：历史评审（2026-07 前后），待与当前代码同步刷新。** 本文件由根目录迁入 `docs/arch/` 存档；
> 其中的 H1-H28 假设清单与结论可能与当前实现不一致，引用时以代码与模块 README 为准。

> 范围：仓库根目录的 Go 源码（`application/`, `seelexctx/`, `sessionstore/`, `session/`, `seelebridge/`, `mcpstack/`, `plugins/`, `internal/`, `main.go`）。侧重**上下文控制**与**数据流**的端到端形态，不对每个工具逐个复核。
>
> 证据等级：Confirmed = 引用了具体文件/符号；Hypothesis = 推断但需进一步验证。

---

## 0. 摘要

整体架构在 "内存 / 持久化 / 投影 / 事件" 四个面上都做了**显式的有界化**，并且把"父↔子"、"应用↔运行时"这两类最容易死锁的边界都拆成"消息进出 + 不可变快照"。`docs/2026-08-04-context-memory-lifecycle/finish-review.md` 也明确把这套设计标为已通过窗口/race/内存三项主要压力测试。

主要问题集中在 **横切一致性** 而非单点：
1. `context.TODO` 在 5 处仍然作为 "无 ctx 入口" 的兜底。
2. `Repository` 内部对 `context.Background()` 的大量硬编码，与外部 `ctx` 传递形成双轨。
3. `mcpstack` 的"快照 + 拦截器"双栈与项目其它 actor 模型**不同源**——这是技术债，但被 Read 路径刻意隔离。
4. `seelexctx.Compressor` 跨会话承袭快照的 Compressor 路径在错误返回时**静默回退**到 QuickChat，下游不感知。
5. 验收测试 `go test -race` 仍有路径未覆盖到（Windows race 工具链缺失），但有真实账号 smoke 兜底（见 finish-review）。

下面按"模块地图 → 数据流图 → 设计原则 → 已验证的不变量 → 按层评估 → 风险与建议"组织。

---

## 1. 模块地图（事实陈列）

| 层 | 关键包 | 关键类型 | 行数（Go） |
|---|---|---|---|
| 应用门面 | `application/core` | `Service`/`serviceState`/`serviceComponents` | ~26 358（包含测试） |
| 应用事件/审批 | `application/event`, `application/approval` | `EventHub`, `ApprovalBroker` | ~300 |
| 应用契约 | `application/contract` | `ChatEngine`/`RuntimePort`/`Dependencies` | ~150 |
| 上下文控制 | `seelexctx` + 子包 | `Controller`, `Assembler`, `Processor`, `Compressor`, `ContextWindowPolicy`, `DefaultWindowPolicy`, `ContextSnapshot` 等 | ~11 455 |
| 上下文生命周期基建 | `seelexctx/lifecycle` | `ContextActor[T]`, `BatchPipeline[T]`, `Storage[T]` | ~1 246 |
| 持久化 | `sessionstore` | `Router`, `Repository` (4 backend), `DurableHistory`, `EventStore`, `SessionContextStore`, `ProjectRecord` | ~13 098 |
| 会话 | `session` | `Manager`（薄封装 Seele `storage.Store`） | ~615 |
| 桥接 | `seelebridge` | `Runtime`, `SeelexAgentNode`, `fork_subagents`, `plan_*`, `account_*`, `MCPStack` 装配 | ~11 716 |
| MCP 可观测 | `mcpstack` | `MCPStack` + 双栈（Interceptor/Snapshot/Provider/Prompt/Breaker） | ~1 569 |
| 主入口 | `main.go`, `application_adapters.go` | `enginePort` (Reactor 适配) | ~2 000 |

每个核心包都有 README 描述其"模块定位 / 职责边界 / Review 指南"。这是项目的一个突出优点：评审门槛被刻意降低。

---

## 2. 数据流主图（Confirmed）

下面这张图把"一次 ChatStream 回合"涉及的所有**有界通道**画出来。绿色 = 有显式上限；橙色 = 已显式声明了上限但缺少监控；红色 = 已知风险点。

```text
┌─────────────────────── 用户输入 (TUI / GUI / CLI) ────────────────────────┐
│            [历史]会话  /new /resume / 命令行 / 文件事件                     │
└────────────────────────────┬────────────────────────────────────────────┘
                             │ inputRouter
                             ▼
┌─────────────────────── application/core.Service ──────────────────────────┐
│  startChat → runChat (goroutine, ctx 派生自 service.cancelChat)            │
│                                                                          │
│  ┌─ viewCoordinator ──────────────────────────────────────────────────┐  │
│  │ appendMessageLocked (system 跳过 bounded tail)                       │  │
│  │ boundConversationTailLocked → HistoryWindow (limits)                 │  │
│  │ appendPlanNodeEvent → PlanNodeEvents 截断 (limits)                   │  │
│  │ snapshot.Conversation ← slice (有界副本)                              │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│  ┌─ contextCoordinator (prepareExecutionContext) ──────────────────────┐  │
│  │ 1) rejectOversizedToolResults (defaultToolResultLimit)               │  │
│  │ 2) systemPrompt 注入 (已锁读)                                          │  │
│  │ 3) rawTokens = CountRequest → soft/hard threshold 比较                │  │
│  │ 4) buildTaskCheckpointLocked → 软阈值: 升级版本                         │  │
│  │ 5) fitExecutionHistory  → 实际限 budget.Budget                        │  │
│  │ 6) ReplaceHistory(assembled) → Engine.assemble() → Provider 调       │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│  ┌─ Service.mu (RWMutex) ──────────────────────────────────────────────┐  │
│  │ snapshot / Conversation / Plan / Runtime / Skill / Account 状态      │  │
│  │ publishRuntimeProjections (锁读 + 不可变投影拷贝)                     │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│  ┌─ EventHub ─ Publish ──→ subscribers (per-buffer channel) ─────────────┐  │
│  │   EventMessageAdded / Delta / ToolStarted / ToolCompleted /          │  │
│  │   SubagentChanged / RuntimeChanged / InteractionOpened/Closed        │  │
│  │   慢订阅: drain + resync.required 事件                                  │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│  ┌─ StreamBatcher (lifecycle.BatchPipeline) ─ 32 条 / 40ms / 128 buffer  │  │
│  │ → appendVisibleDelta (锁内, 追加到 snapshot.Conversation 尾条)         │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│  ┌─ ToolHookBridge (OnToolStart/Complete/Iteration/LLM) ────────────────┐  │
│  │ 不重入 Service.mu: 通过 goalSkillActive (atomic.Bool) 解耦             │  │
│  │ appendTranscriptEvent → service.transcript (锁内)                      │  │
│  │ HandleSubagentToolEvent → truncateSubagentEvidence (limits 截断)      │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│  ChatStream 结束后:                                                       │
│  • injectPendingSubagentContexts (DrainSubagentContexts, 锁外)              │
│  • persistCurrentSession → sessionCoordinator.sessionRecordLocked        │
│    → SaveSessionSnapshot (project-scoped) / SaveCurrent (legacy)         │
│  • on chat end: chat.idle                                                │
└────────────────────────────┬────────────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────── seelebridge.Runtime ────────────────────────────────┐
│                                                                            │
│  Engine.ChatStream (Seele session.Session)                                 │
│  ┌─ LoopHooks: OnLLMComplete / OnToolStart / OnToolComplete /             │
│  │             OnIterationComplete (锁内同步)                                │
│  │   iteration hook 不再触发 compactTaskContext (迁移到 ContextController)   │
│  └────────────────────────────────────────────────────────────────────────┘
│                                                                            │
│  ┌─ subagentMailbox (有界, SubagentMailboxSize) ──────────────────────────┐
│  │ enqueueSubagentContext (非阻塞 select+default)                            │
│  │ DrainSubagentContexts (主会话 ChatStream 开始前抽干)                       │
│  │ subagentDropped 计数可观测                                                  │
│  └────────────────────────────────────────────────────────────────────────┘
│                                                                            │
│  ┌─ planEventSink (in-memory + 投影) ───────────────────────────────────┐
│  │ AppendNodeResult 同步 → 投影 PlanNodeEvent → Service.HandlePlanNode   │
│  │ (持久化由 sessionstore EventStore 承担，按事件 Location 落库)            │
│  └────────────────────────────────────────────────────────────────────────┘
│                                                                            │
│  ┌─ accountpool.P2CPool[Completer] ──────────────────────────────────────┐
│  │ ResolveAccountForBranch(scope) → main / node 账号选择                  │
│  └────────────────────────────────────────────────────────────────────────┘
│                                                                            │
│  ┌─ MCPStack (mcpstack) ─────────────────────────────────────────────────┐
│  │ BeforeCall → 追加 pending MCPCall (autoSave atomic)                   │
│  │ CallRecorder.AfterCall → 升级状态                                       │
│  │ ListenBreaker (goroutine) → 写入 __breaker__* 特殊 MCPCall              │
│  └────────────────────────────────────────────────────────────────────────┘
│                                                                            │
│  ┌─ ProjectScope + NodeScope ───────────────────────────────────────────┐
│  │ ProjectScope.Bind (canonicalize, EvalSymlinks, IsDir 校验)            │
│  │ NodeScope (ctx 注入) → worktree 节点根或 ProjectScope 单根              │
│  └────────────────────────────────────────────────────────────────────────┘
│                                                                            │
│  Plan 装载 (plan_load/plan_run/plan_clear):                                │
│  ┌─ NormalizePlanLoadArguments (canonical object DAG) ─────────────────┐
│  │ → canonicalPlanDocument → codec.Import → nodeFactory()              │
│  │ → buildNode: agent/approve/auto/function/verify/deliver/summary      │
│  └────────────────────────────────────────────────────────────────────────┘
│  SeelexAgentNode.Run: 注入 NodeScope + PromptBlocks → 节点 Session 执行   │
│  MergeBack: childSnapshot → merger.Merger.MergeBack(parent, child)       │
└────────────────────────────┬────────────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────── seelexctx (seele 适配) ─────────────────────────────┐
│                                                                            │
│  Assembler:                                                                │
│    system (effort/skill 动态) → projectBlock → StackBlocks                │
│    → request.Blocks → Window(ctx) → WorkingHistory → Provider 调           │
│  Processor (工具结果):                                                       │
│    超大 → ToolResultArchiver.Store → result_ref + 省略警告                  │
│  Compressor:                                                               │
│    1) 短历史免 LLM                                                          │
│    2) 跨会话快照 (SnapshotFor + Compactor) → 三级预算 200/200-499/全量      │
│    3) 预算不足时静默回退 QuickChat                                             │
│  Controller (软/硬阈值):                                                     │
│    after_assistant / after_tool 钩子 (TODO: 在新装配下未接)                  │
│    WindowPolicy (显式 > provider 推导 > 保守)                                │
│  HistorySafety:                                                            │
│    PrepareReplaceHistory: 移除 checkpoint/compact 标记 + 修复空 content    │
└────────────────────────────┬────────────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────── sessionstore (持久化) ─────────────────────────────┐
│  Repository (JSON / SQLite / PostgreSQL / Redis)                            │
│   WriteCommit/WriteAtomic: generation directory → manifest.json 原子切换    │
│   Read: total 优先, range 走单 shard (windowed)                            │
│  Router:                                                                    │
│   mu.RLock 串行化 (切换后端时用 lock)                                          │
│   withRepository / withRepositoryAt                                         │
│  DurableHistory (seele.DurableHistory 适配):                                 │
│   Load: prepared ? : tail budget ? full                                     │
│   Save: prepared 一次性消费 (PrepareNextLoad)                                │
│  SessionContextStore: SaveState/LoadState  (state blob)                    │
│  EventStore: AppendFrameworkEvent / ReadFrameworkEvents                    │
│  ProjectRecord: WriteProjectRecord / ReadProjectRecord                     │
│  RouterStorage: lifecycle.Storage[T] 适配 (Append O(n) 读-合-写)             │
└────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. 设计原则（已在代码中显式表达）

1. **copy-on-write + 有界**：merger.MergeBack、compactor.Compact、history_safety.PrepareReplaceHistory、viewCoordinator.appendMessageLocked、mergeConversationMessages 全部明确写"不修改入参"。
2. **状态私有、消息进出**：lifecycle 包与 actor.go 的核心命题；`subagentMailbox`、`RuntimeVisibilityProjection`（值对象快照）、`ParentEvidenceProjection`（值对象快照）都是这一原则的体现。`docs/2026-08-04-context-memory-lifecycle/runtime-tool-completion-deadlock.md` 把"持锁者等待自己的工作"作为死锁教训记录在案。
3. **背压 = 显式错误**：`ErrMailboxFull`、`ErrPipelineFull`；StreamBatcher 在背压时 `runtime.Gosched()` 重试，避免退化为丢消息。
4. **持久化 = 真相源、内存 = 工作缓存**：`sessionstore.Repository` 写面是原子 manifest 切换 + shard；Engine 内存 history 只是 provider cache。
5. **预算/上限都从配置读**：`seele.yaml` 的 `limits` 段在 `application/core/limits.go` 收编所有运行时上限；零值回退 `DefaultLimits`。
6. **错误结构化**：`seeleerrors.Wrap` + `errorCodePersistenceFailed/PlanPreflight/ReActBudget/ContextExceeded`；`presentUserError` 把内部错误翻译为"模块|方法"形态，不暴露 provider/requestID。
7. **失败设计 = 显式 fail-fast**：`nativeProjectCWD`（沙箱）非 OS 级隔离，配置要求 OS 级能力时 `SandboxCapabilities` 错误返回，调用方拒绝（不悄悄降级）。
8. **ReAct 预算双层防御**：每个 ChatStream 周期有 `activeReActBudget`（tool calls、noProgress 轮数）；超过返回 `ErrReActBudgetExceeded`。

---

## 4. 已通过的不变量（自检可重现）

> 这些都是项目自身在 commit message / docs 中已声明并经测试的；本节只把它们汇总以便交叉对照代码。

| 不变量 | 代码证据 | 测试证据 |
|---|---|---|
| Window 区间只装载、不拉全量 | `sessionstore/durable_history.go:LoadEventTail` + `SetTailBudget` | `TestJSONRepositoryReadRangeWindowed` |
| 替换历史前清掉控制标记 | `seelexctx/history_safety.go:PrepareReplaceHistory` | `controller_test.go` / `history_safety_test.go` |
| 软阈值：窗口内不压缩 | `seelexctx/controller.go` 注释 (after_assistant/after_tool) | `controller_test.go` |
| Provider 拒绝空 content | `application/core/history_safety.go:prepareProviderHistory` | `error_presentation_test.go` |
| SubagentMailbox 满则计数 + 丢弃 | `seelebridge/actor.go:enqueueSubagentContext` (select+default) | `subagent_audit_test.go:TestSubagentMailboxIsBoundedAndNeverBlocksProducer` |
| ToolHook 不重入 Service.mu | `taskRuntimeState.goalSkillActive` atomic.Bool | `TestToolCompletionDoesNotReenterServiceLockForGoalSkillVisibility` |
| Plan 节点级 cycle 拒绝 | `seelebridge/plan.go:DetectCycle` (Kahn) | `plan_kernel_test.go` |
| Plan 节点 budget 上限 | `seelebridge/plan_policy.go:validateLoad` (MaxNodeLoops/MaxNodeOutputTokens) | `plan_input_fuzz_test.go` |
| Plan 节点事件时间线截断 | `application/core/chat.go:appendPlanNodeEvent` (limits.plan_node_events) | race_test / session_archive_test |
| 流式 chunk 按 32 条 / 40ms 批 | `application/core/chat.go:newBatchedDeltaSink` | — |
| 上下文恢复有界 | `application/core/context_controller.go:prepareExecutionContext` (TargetAfterCompaction) | `context_controller_test.go` |
| Repository 原子切换后端 | `sessionstore/sessionstore.go:Router.withRepository` | `TestRouterPersistsAndSwitchesConfiguredBackend` |
| Repository manifest 原子写 | `sessionstore/sessionstore.go:jsonRepository.WriteCommit` (writeAtomic) | `TestJSONRepositoryCommitsShardGenerationsAtomically` |
| 三路并行恢复 | `application/core/session_history.go:resumeSession` (sync.WaitGroup 3 goroutines) | session_history_test |
| 锁外 publishRuntimeProjections | `application/core/runtime_projection.go:publishRuntimeProjections` (RLock 拷值, 锁外 publish) | service_test |
| 锁外 collectRuntimeProjection | `application/core/service_snapshot.go:collectRuntimeProjection` (调外部 port 无锁) | service_test |
| Slow subscriber 触发 resync | `application/event/hub.go:eventSubscriber.deliver` (drain + resync) | race_test |
| 持久化用 ProjectID 隔离 | `sessionstore/sessionstore.go:Router.SetWorkspace` + 全部 `*Workspace` 变体 | `TestRouterReadsExplicitWorkspaceWithoutChangingActiveScope` |
| 父子合并 copy-on-write | `seelexctx/merger/merger.go:MergeBack` (mu.Lock + copyDecisions/copyStrings) | `merger_test.go` |

---

## 5. 跨层评估

### 5.1 上下文控制（seelexctx + application/core/context_controller）

**设计**
- 三层决策：Assembler（每次请求投影）→ Processor（超大工具结果）→ Compressor（结构化压缩）→ Controller（软/硬阈值 + 替换历史）。
- 软阈值 75% / 硬阈值 90% / 压缩目标 60% 三个常量在 `controller.go:ContextWindowPolicy` 集中定义。
- `seelexctx.Compressor` 的三级预算（200/499/全量）在 `compactor/compactor.go` 实现。
- `history_safety.go` 统一负责"control block 清理 + 配对修复"。

**Confirmed 优点**
- 默认值集中在 `seelexctx.DefaultLimits` 和 `DefaultWindowConfig`；决策代码不硬编码魔法数字（`window.go` 注释明确这一点）。
- `WindowPolicy.WindowRounds` 显式决策顺序：配置 > provider 推导 > 保守回退（`window.go:WindowRounds` 注释里把"死接线"教训 R4 都标出来了）。
- `PrepareReplaceHistory` 既是压缩产物的入检查，也是 chat 后端回放历史的入检查；语义统一。

**Hypothesis / 风险**
- H1：`seelexctx.Compressor.Compress` 在快照路径"预算不足以保留最小安全快照"时**静默回退**到 QuickChat（`compressor.go:60-67` 的注释里说"失败回退 QuickChat 路径"，但 `Compress` 并没有把这个回退以 warning 形式上报给 telemetry）。如果父会话的快照是模型继续推理的关键信号，回退 = 静默丢信息。建议把回退纳入 `presentedError` 或加 telemetry 计数。
- H2：`ContextController` 的"after_assistant / after_tool 钩子"在新装配下没有看到接线代码（`controller.go` 注释假设有这两个钩子，但 `chat.go:1288 OnIterationComplete` 的注释明确"压缩决策移交 ContextController" + "配对修复由 chat 边界承担"——意味着真正的钩子移交可能尚未完成或被拆到了 history_safety 路径上）。这是"软阈值：窗口内不压缩"的关键路径，需要在 `seelebridge/runtime.go` 的 Hooks 注册里查证。
- H3：`seelexctx/bridge.go` 的 `Export` / `ExportWithGoal` / `ExportSnapshot` 全部使用 `context.TODO()`（5 处），且把 `provider.Export` 的 err 吞掉（`snap, _ := ...`）。这是兼容层，可以接受但需要明确标注"必须只在 chat 外调用"。
- H4：`prepareExecutionContext` 里两次 `service.tokenCounter.CountRequest`（一次 rawTokens 决策，一次 fitted 估算），且最后仍可能触发 `errProviderContextBudgetExceeded`；这个错误码被 `error_codes.go` 定义为 `context.budget_exceeded`，但**没有证据**会触发 `presentUserError` 的特定分支。需要查 `error_presentation.go` 是否能命中。

### 5.2 数据流 / 持久化（sessionstore + session + DurableHistory）

**设计**
- 单一持久化契约 `Repository`，4 个 backend（JSON/SQLite/PostgreSQL/Redis），4 套 `WriteCommit`/`WriteAtomic` 实现。
- 原子切换通过 `Router.mu.RLock` + `withRepository` 保证；切换期间旧 repository 完成所有 in-flight 调用后，新 repository 接管。
- `Commit` 把 provider history、events、state、tool results 原子化进一次写（避免"半新半旧"被读者看到）。
- `DurableHistory` 一次性 `prepared` 窗口用于"应用侧已装配好的恢复上下文优先"（`PrepareNextLoad` 一次性消费）。

**Confirmed 优点**
- SessionContextSchemaVersion=1 强制版本，不匹配显式失败（`session_context.go`）。
- 8.4MB / 6 shard 会话下从全量解析降为单 shard 解析（`session_history.go:resumeSession` 注释；`TestJSONRepositoryReadRangeWindowed` 覆盖）。
- `commitConversation` 风格的去重合并（`session_archive.go:mergeConversationMessages` 用 ID 索引）。
- `RouterStorage` 把 Router 适配为 `lifecycle.Storage[T]`，ContextActor 以 Router 为冷存储接缝——但**当前 Append 路径是 O(n) 读-合-写**（`router_storage.go:Append` 注释明确写出），基建期可接受。

**Hypothesis / 风险**
- H5：`sessionstore.Router.Save/Load/SaveCommit/LoadRange` 等约 15 处 `Repository` 方法，**全部使用 `context.Background()`**（`sessionstore.go` 中 15 处）。这意味着 caller 的 ctx（cancel、超时）无法传播到持久化层。建议在 Router 与底层 Repository 之间加 ctx 透传边界（虽然这与"切换后端"语义有冲突，但能透传 cancel 信号至少不会让 HTTP client 死等）。
- H6：`mcpstack.TraceProvider` 通过 `provider.Compact` 路径（`mcpstack/provider.go:48`）直接构造 `ContextSnapshot` 副本，但绕过了 `seelexctx/provider.Provider` 接口——这是为了避免 import 循环而做的妥协。代价是 mcpstack 不在 seelexctx.Provider 命名空间下，可能在 `Merger.MergeBack` 时被遗漏。建议加 compile-time 接口断言 `var _ provider.Provider = (*mcpstack.TraceProvider)(nil)`（如果 import 关系允许重构）。
- H7：`SessionRecord` / `SessionContextStore` 的 state blob 与 `ProviderHistory` 是双写关系（`persistCurrentSession` 既写 framework history 也写 state blob）；但二者在落库失败时**只返回第一个错**，后续动作是否会回滚无显式事务。`sessionstore.WriteCommit` 显式接受一次 Commit（4 段），但 Commit 内部各段（ProviderHistory/Events/State/ToolResults）的失败一致性是 backend 决定的（JSON 后端是串行 `os.WriteFile` + `os.Rename`）。
- H8：`RouterStorage.Append` 注释明确"基建期 O(n)"，并把增量优化（事件库 SaveCommit + 投影）列为后续切片。**当前若用作冷加载主路径，每次 Append 都是 read+merge+write 全量**——长会话下这是 O(n²) 的写入成本。一旦切到活跃生产，需要先把增量写做掉。

### 5.3 应用门面 / 事件流（application/core + application/event + application/approval）

**设计**
- `Service` 用 `serviceState` + `serviceComponents` 把状态按"基础设施/对话/生命周期/提示/会话/计划/任务"分组；`Service.mu`（RWMutex）作为整体门。
- `viewCoordinator` 是用户可见 Snapshot 的唯一拥有者；`boundConversationTailLocked` 把可见对话限制为 `Limits().HistoryWindow`（默认 4000 上限未在代码中验证但类型定义存在）。
- 事件流：`EventHub` 单生产者多订阅者，慢订阅 drain + `EventResyncRequired`；前端从此可以"重启时拿全量 + 实时拿增量"二选一。
- 审批流：`ApprovalBroker` 持有 `pending[ID] → result chan`；`Request` 在 `result` 上阻塞等待（带 ctx timeout + autoApprove 旁路）。

**Confirmed 优点**
- 状态分组让 `cloneSnapshot` 一次 RLock 拷出不可变副本（`service_snapshot.go:snapshotView`）——这是 26k 行核心库还能保持单 RWMutex 的关键。
- 三类"应用→运行时"投影（`RuntimeVisibilityProjection` / `ParentEvidenceProjection` / `ReplanMetrics`）都是只读快照，反向闭包从 Runtime 回到 Application 是不可能的——`docs/2026-08-04-context-memory-lifecycle/runtime-tool-completion-deadlock.md` 明确把这点写成教训。
- 事件订阅者解耦：`Subscribe(buffer)` 返回 `Subscription{Events, close}`；frontend 自己的 buffer 满了不会阻塞 publisher。
- `ApprovalBroker.Resolve` 的 `select default: return ErrInteractionResolved` 防止双重决策；`ResolveAll` 用于 full access 切换时批量推进。

**Hypothesis / 风险**
- H9：`publishRuntimeProjections` 在 chat 开始/next/会话切换/账号切换等至少 6 处被调用（grep 结果），意味着每次调用都要做 RLock + 不可变投影拷贝。值得做 per-event diff（只在变更时 publish）。
- H10：`streamBatcher` 与 `streamOutput`（`visibleOutputStream`）是两段流式处理：`visibleOutputStream.Consume` 在 `consumeVisibleChunk` 里同步执行，**持 service.mu.Lock**。如果模型流式输出一个超长 `<think>...</think>` 块，`Consume` 可能在锁内做大量字符串拼接（`strings.Builder`），但锁是 application 锁而非 Engine 锁，所以不会阻塞 ChatStream；但会阻塞其他事件 publish。建议在测试里加锁持有时间断言。
- H11：`OnIterationComplete` 注释（`chat.go:1288`）明确指出"Session 锁内同步执行，回调不得重入 Session 历史操作"——这是 2026-08-04 死锁复盘后的关键不变量。但 `OnIterationComplete` 里仍然 `svc.deps.Engine.AppendHistory`（`chat.go:1333`）。Comment 明确说"新 Session 装配下，OnIterationComplete 在 Session 锁内同步执行"。这意味着 AppendHistory 走的是框架侧已经允许的"锁内更新历史"路径，**但需要确认 `enginePort.AppendHistory` 是否在底层走了 deferred install**（看 `application_adapters.go:AppendHistory` 的实现）。`application_adapters.go:120` 实现了 `AppendHistory` 但没有看到 deferred 路径——这与 ChatStream 路径上的 `pendingHistory` 逻辑不一致。
- H12：`ApprovalBroker.Request` 的 ctx 在 `Timeout > 0` 时派生 `waitContext`，但**Remove 操作的 ctx 与 Request 的 ctx 不同**：`remove(id)` 不接受 ctx，依赖 broker 内部 map 删除。意味着一旦 Request 走到 ctx timeout，`remove` 会同步执行；如果用户事后又 Resolve，可能出现"race（已 remove + 后来 push 决策）"——目前用 `default: return ErrInteractionResolved` 兜底。
- H13：`appendPlanNodeEvent` 有 30 条上限（`limits.plan_node_events`），但截断是"头部溢出"还是"尾部溢出"需要确认（`chat.go:835` 注释没明说）。如果是"砍头部"，会丢失"节点开始"信息。

### 5.4 桥接层（seelebridge）

**设计**
- `Runtime` 是 composition root：账号池 + Completer + StreamCompleter + Agent + 工具注册 + Seele 侧 session。
- `enginePort` 是 application → framework 的窄适配（`application_adapters.go:27`）。
- Plan 子代理：`SeelexAgentNode` 包装 `bridge.NewAgentFactory` 产物，Run 时把 NodeScope 注入 ctx；可见性策略 / 账号选择器 / 节点装配器都从 ctx 读取作用域与块——并行的子代理各得其域。
- `fork_subagents` 把"DAG 知识"封装在工具内部，模型只传 id + goal——降低模型侧使用门槛（弱模型可用）。

**Confirmed 优点**
- `RequirePlan/plan scope` 强制规划在 2026-08-01 作为失败设计移除（`plan_authority.go` 注释保留为教训），当前只保留自愿的 `plan_load`。
- `PlanPolicy` 把"effort 关联的并发/节点预算"集中管理；`MaxNodeLoops/MaxNodeOutputTokens` 防止节点级 budget 滥用。
- `mcpstack.ListenBreaker` 与 `mcpstack.MCPStack` 通过 `AutoSave` 原子写持久化（`stack.go:autoSave` 注释"write .tmp then rename"）。
- `scoped_tools.go:resolveNodePath` 区分 worktree 节点根 vs ProjectScope 单根；`withinRoot` 越界拒绝——安全边界清晰。
- `pathgate.go` 接受 `seele.yaml` 的 path zone allow/ask/deny 规则。

**Hypothesis / 风险**
- H14：`mcpstack.MCPStack` 与项目其它 actor 模型**不同源**：seelexctx/lifecycle 用的是 `mailbox + 唯一 actor goroutine + 闭包状态`；mcpstack 用的是 `mu.RWMutex + slice append`。两套并发范式并存增加了评审负担。考虑到 MCPStack 范围小且已加锁保护，可接受但应在 `mcpstack/README.md` 显式说明。
- H15：`mcpstack.ForPrompt` 的 `avgTokens` 来自 `s.averageTokenCount()`（每次调用扫描全部 Calls），token budget 评估成本 O(n)。长 MCP 会话下每次 LLM 请求都做一次 O(n) 扫描。值得做缓存。
- H16：`mcpstack.MCPStack.Record` 每次都 `autoSave()`（`stack.go:169`），相当于同步写盘；`Undo/Redo` 也都 autosa ve。高频 MCP 调用下磁盘 IO 是瓶颈。考虑 batch autosave（与 `lifecycle.BatchPipeline` 同构，但 mcpstack 没有引入）。
- H17：`SeelexAgentNode` 包装 `node.AgentFactory` 的 closure 解析 `scope/blocks` 是惰性的（`agent_node.go:newSeelexAgentNode`），但 `blocks` 是 `[]seelectx.PromptBlock`，在 `plan_run` 时被冻结——这里需要确认"在 `plan_run` 之前调用是否安全"。
- H18：`stream_completer.go:streamCompleter` 是 "lease-until-EOF"——意味着账号与一条流绑定。如果流中途因网络断流但 EOF 未来，账号会一直被这个流占用。需要有 lease timeout。
- H19：`subagent_audit_test.go:13` 演示"mailbox 容量=1 时 `enqueueSubagentContext` 阻塞不会发生"——但 `subagentDropped` 计数增加可观测。**生产代码（`actor.go:75`）没有看到 drop 计数被采样的位置**（如 telemetry、DiagnosticSnapshot）。需要确认"丢弃可观测"的承诺是否真的端到端实现。

### 5.5 Lifecycle 基建（seelexctx/lifecycle）

**设计**
- `ContextActor[T]` 串行处理 append / window load / snapshot；请求只通过有界 mailbox 进入。
- `BatchPipeline[T]` 聚合写，按 `FlushSize` / `Interval` 调用 `Storage.Append`。
- `Storage[T]` 是 cold storage 接口；包内只提供测试用 memory/discard 实现。
- 4 个策略：`FullRetain` / `ColdLoad` / `Windowed` / `Pipelined`。

**Confirmed 优点**
- actor 关闭协议：mailbox close 之前的提交必须被排空，`Close` 等待"已接受数据"处理完。`mock_bench_test.go:533` 行数证明这是经过压力测试的。
- 背压 = 显式错误：`ErrMailboxFull` / `ErrPipelineFull`。
- `discardStorage` 用来验证冷加载策略的驻留收益（对比 memoryStorage 的"假冷"）——测试方法学很扎实。

**Hypothesis / 风险**
- H20：`ContextActor.opTimeout`（5s）应用于 storage 操作，但 Run 内 handleAppend 的 storage 调用是同步的——慢 storage 会卡 actor goroutine，进而阻塞所有后续 append。考虑用 per-op goroutine 派生 + actor 自己的 mailbox 做背压。
- H21：`ContextActor` 的 `drop atomic.Int64` 计数可观测但**没有看到在生产中被采样**——`seelebridge.subagentDropped` 类似。需要确认"丢弃可观测"承诺。

### 5.6 MCP 可观测（mcpstack）

**设计**
- 双栈：`MCPCallLog`（append-only 数据层）+ `Interceptor`（middleware 钩子）。
- `BeforeCall` 写入 pending，`AfterCall` 升级状态。
- `ListenBreaker` 把 frameworkmcp 的熔断事件 channel 编码为特殊 MCPCall（StatusRolledBack）。
- `TraceProvider.BuildSnapshot` 把 MCP history 转为 `ContextSnapshot`（Findings + PendingWork）。
- `ForPrompt` 按 token budget 生成 LLM 可见摘要。
- `Snapshot` 通过 `json.Marshal+Unmarshal` 做深拷贝（无结构共享）。

**Confirmed 优点**
- `Save` 写 .tmp + rename 原子；失败清理 .tmp。
- `Snapshot` 深拷贝防止调用方修改内部状态——这是 `mcpstack/persist.go` 的核心承诺。
- 隐含的不变量："MCP stack 是不可变的；Record/Undo/Redo 都是栈操作"。

**Hypothesis / 风险**
- H22：mcpstack 的 `mu` 是单一 RWMutex，但 `Record` 内 `autoSave` 调用 `save` → `s.save(path)` 走 `mu.RLock` → 再调用内部 `save` 又拿 `mu.RLock`——Go 的 `sync.RWMutex` 在**同一 goroutine 重复 RLock 是允许但浪费**，且如果其它 goroutine 持有 Lock 会死锁。当前实现是 RLock 后再 RLock，没有升级路径，相对安全；但代码气味是"自动保存与 Record 在同一锁内"，磁盘慢会拖慢 Record。
- H23：`StackMetadata` 仅 `SessionGoal` + `Domain`——MCP 域（CAD/chem/medical）的元信息被简化到只有 Domain 字符串；`mcpstack.TraceProvider.BuildSnapshot` 的"Findings/Decisions"提取规则可能无法反映真实业务（`provider.go:BuildSnapshot` 注释里有"pending MCP"逻辑，但 Findings 来自 `FormatSummary`——后者是结构化文本）。
- H24：`MCPStack` 持有所有 Calls 的 `Args/Result` 全文（`MCPCall.Args` 是 `json.RawMessage`），长 MCP 会话下内存增长无界。这是设计取舍（用于回放），但应明确"上限"和"过期淘汰"策略。

### 5.7 Cross-cutting

**Confirmed 优点**
- 错误传播：`seeleerrors.Wrap/From` 贯穿全栈，`errorCodeXxx` 4 个稳定 code；`presentUserError` 把内部错误转用户可见。
- 安全边界：`ProjectScope` 强制 root 必须存在且是目录 + EvalSymlinks；`NodeScope` 走 worktree 时仍在 `resolveInside` 校验越界。SandboxCapabilities fail-fast。
- 取消：`startChat` 用 `service.cancelChat`（context.CancelFunc），`Application.Draining` 与 `Chat.Running` 双信号。
- 资源回收：`pendingHistory` 在 `enginePort.ChatStream` 退出且 `activeCalls==0` 时安装（`application_adapters.go:96`）；`ReleaseWorkingHistory()` 是显式释放 API。
- 测试覆盖：`TestJSONRepositoryCommitsShardGenerationsAtomically`、`TestSQLiteRepositoryRoundTrip`、`TestRouterPersistsAndSwitchesConfiguredBackend`、`TestSubagentMailboxIsBoundedAndNeverBlocksProducer`、`TestToolCompletionDoesNotReenterServiceLockForGoalSkillVisibility`、`TestCanonicalPlanDocumentConvertsDSL`、`FuzzNormalizePlanLoadArguments`——覆盖关键并发与安全断言。

**Hypothesis / 风险**
- H25：`context.Background()` 在 `sessionstore` 内 15 处硬编码、`seelexctx/bridge.go` 5 处 `context.TODO()`。这些是"ctx 不可用"的兜底点，但形成了一个**反模式**：业务层有 ctx，infra 层无 ctx。**建议**：把 `context.TODO()` 限制在"`export` 系列（明确标为 chat 外调用）"内，且在 `sessionstore` 引入 ctx 透传边界。
- H26：`go func() { ... }` 在 `application/core/chat.go:68` 启动 `runChat`，在 `session_history.go:50-58` 启动三路并行读。**这些 goroutine 没有 panic recovery**——单个 panic 会让进程崩。建议在 `startChat` 和 `resumeSession` 的入口加 `defer recover()`。
- H27：`Service.deps.Runtime` / `Service.deps.Engine` 是**注入的具体实现**（`application_adapters.go` 的 `enginePort` 适配 Seele `*Session`）。如果某个实现方法 panic 而没有 recover，application 会因为 goroutine leak 而逐渐 OOM。
- H28：`streamBatcher` 是在 `consumeVisibleChunk` 锁内构造（`consumeVisibleChunk` 持锁调 `service.streamOutput.Consume`，再调 `batcher.OnChunk`），锁内做了：
  1. 字符串解析（`<think>` tag 跟踪）
  2. pipeline 推送（`OnChunk`）
  3. pipeline 满时 `runtime.Gosched()` 重试
  `runtime.Gosched` 期间仍然持锁——会**饿死其它 goroutine 等待同一把锁**。建议在 `OnChunk` 调用前后做解锁/重锁。

---

## 6. 关键风险汇总（按优先级）

| 优先级 | ID | 风险 | 影响 | 建议 |
|---|---|---|---|---|
| P1 | H11 | `enginePort.AppendHistory` 与 `pendingHistory` 路径可能不一致 | OnIterationComplete 死锁回归 | 查 `application_adapters.go:120` 与 `enginePort.ChatStream:96` 的协同；补充 race test |
| P1 | H5 | `Repository` 全部走 `context.Background()` | 上游 ctx 取消无法传播 | 引入 ctx 透传边界；至少透传 cancel |
| P1 | H26/H27 | `startChat` / `resumeSession` 启动的 goroutine 无 panic recovery | 进程崩溃 / goroutine leak | 加 `defer recover()` + 故障日志 |
| P1 | H1 | Compressor 快照路径静默回退 QuickChat | 父会话关键信息可能丢失而无任何告警 | 在 `seelexctx.Compressor.Compress` 加 telemetry 计数；或把回退用 `presentedError` 上报 |
| P1 | H2 | ContextController 的"after_assistant/after_tool"钩子未在新装配下接线 | 软阈值"窗口内不压缩"可能失效 | 查 `seelebridge/runtime.go` 的 Hooks 注册并补测试 |
| P2 | H15 | `mcpstack.ForPrompt` 每次 O(n) 扫描 | 高频 MCP 调用下 LLM 上下文组装变慢 | 加 token 估算缓存或增量聚合 |
| P2 | H16 | `MCPStack` 每次 `Record`/`Undo`/`Redo` 都 autoSave | 高频 MCP 调用下 IO 成为瓶颈 | 引入 batch autosave |
| P2 | H8 | `RouterStorage.Append` 是 O(n) 读-合-写 | 长会话下 O(n²) 写成本 | 实现增量 SaveCommit（事件库已就绪） |
| P2 | H10 | `streamBatcher` 锁内 `runtime.Gosched` 持锁 | 锁等待时间被放大 | `OnChunk` 前后做解锁/重锁 |
| P2 | H9 | `publishRuntimeProjections` 在 6+ 处全量拷贝 | 大量不必要的全量 publish | per-event diff publish |
| P2 | H25 | `context.TODO`/`Background` 反模式 | ctx 取消信号丢失 | 限制在显式标注的兼容层；sessionstore 引入 ctx 透传 |
| P2 | H18 | StreamCompleter lease-until-EOF 无 timeout | 流断流时账号长期占用 | 加 lease timeout |
| P2 | H19/H21 | `subagentDropped` / `ContextActor.drop` 没有端到端观测 | 背压丢弃静默 | 在 `/diag` 中暴露，纳入 Snapshot.Runtime |
| P3 | H3 | `seelexctx/bridge.go` 5 处 `context.TODO` + 吞 err | 错误路径不清晰 | 注释明确"必须只在 chat 外调用" |
| P3 | H4 | `errorCodeContextExceeded` 是否走 `presentUserError` 特定分支未确认 | 用户看不到结构化错误 | 在 `error_presentation_test.go` 补 case |
| P3 | H6 | `mcpstack.TraceProvider` 绕过 seelexctx.Provider 命名空间 | 可能被 Merger 漏接 | 评估 import 循环，做 compile-time 断言 |
| P3 | H7 | `SessionRecord` 双写无显式事务 | 持久化失败可能只回滚一段 | 在 `sessionstore.WriteCommit` 加跨段失败一致性测试 |
| P3 | H12 | `ApprovalBroker` resolve timeout 后 late push 的 race | 双 Resolve race | 加 `tryResolve` 原子操作 |
| P3 | H13 | `appendPlanNodeEvent` 30 条截断方向未明 | 节点开始信息可能丢失 | 确认截断方向并补测试 |
| P3 | H14 | mcpstack 与 lifecycle 并发范式不同源 | 评审门槛 | 在 `mcpstack/README.md` 显式说明 |
| P3 | H17 | `SeelexAgentNode.blocks` 惰性解析，plan_run 之前调用不安全 | 提前解析可能拿到旧 plan 的 blocks | 加 test 覆盖"在 plan_run 之前调用" |
| P3 | H20 | ContextActor storage 同步调用 | 慢 storage 阻塞 actor | 派生 goroutine + 自邮箱背压 |
| P3 | H22 | MCPStack Record 持锁 + 持锁 autoSave | 磁盘 IO 拖慢 Record | 拆 RWMutex：state mu + save mu |
| P3 | H23 | `StackMetadata` 简化，业务语义缺失 | Findings 提取规则粗糙 | 评估增加 `Domain` 之外的轻量标签 |
| P3 | H24 | MCPStack Calls 内存无界 | 长会话内存增长 | 显式上限 + 淘汰策略 |

---

## 7. 与目标文档的一致性

| 文档 | 主张 | 代码一致性 | 备注 |
|---|---|---|---|
| `docs/2026-08-04-context-memory-lifecycle/plan.md` | "上下文三时刻生命周期"（冷加载/按需/无驻留） | ✅ 大体一致 | 但 `RouterStorage.Append` O(n) 与"冷加载"目标不完全契合（待增量实现） |
| `docs/2026-08-04-context-memory-lifecycle/runtime-tool-completion-deadlock.md` | "持锁者等待自己的工作"教训 | ✅ `goalSkillActive` 原子解耦 | 但需要回归测试持续守护 |
| `docs/2026-08-05-session-resume-memory/front-review.md` | 一次性 prepared history、legacy 尾窗、catalog 缓存 | ✅ `DurableHistory.PrepareNextLoad` + `loadHistoryTailWindow` | 注释里也承认"探测与窗口读非原子"——观察项 |
| `DESIGN.md` §3.7.4 | 软/硬阈值，窗口内不压缩 | ⚠️ 需查 ContextController 钩子接线 | H2 |
| `DESIGN.md` §3.5 | Compressor 三级预算 | ✅ 200/499/全量 | 但静默回退 H1 |
| `plan.md` §3.7.3 | WindowPolicy 决策顺序 | ✅ 显式 > provider > 保守 | R4 死接线教训已修 |
| `seelexctx/README.md` Review 指南 | "是否把完整 secrets/tool raw output 无界注入 child" | ✅ `Merger.MergeBack` 走结构化字段，raw output 不直接注入 | 工具结果通过 `ToolResultArchiver` 归档为 ref |

---

## 8. 给后续工作的建议

### 8.1 必须做（不阻塞当前 PR，但下一迭代）
1. **H1 / H2 验证**：跑一次真实账号 chat，注入"软阈值 75% 触发"场景，确认 `prepareExecutionContext` 真的命中 `ContextController` 的"after_assistant/after_tool"钩子。若没有，把它接进 `OnLLMComplete` / `OnToolComplete`。
2. **H11 验证**：在 `enginePort.AppendHistory` 与 `enginePort.ChatStream` 路径上做 race test，确认 OnIterationComplete 不会与 ChatStream 入口的 `pendingHistory` 形成顺序违反。
3. **H26/H27 治理**：所有 `go service.runChat` / 三路 `loadGroup` 起点加 `defer recover()` + `log.Printf` + snapshot bump。
4. **H25 治理**：把 `context.TODO()` / `context.Background()` 的使用点列入 lint 规则（`revive` / `staticcheck`），允许"显式标记的兼容层"豁免。

### 8.2 应该做
5. **H8 增量写**：把 `RouterStorage.Append` 切换到事件库增量写；这是"冷加载"承诺的最后一公里。
6. **H9 投影 diff**：在 `Service.mu` 内做"自上次 publish 以来"变更检测，仅在变更时 publish `EventRuntimeChanged`。
7. **H15/H16 mcpstack 批处理**：把 mcpstack 的 `autoSave` 改造为 batch 模式（与 `lifecycle.BatchPipeline` 同构），并在 `ForPrompt` 加 token 估算缓存。
8. **H18 lease timeout**：给 StreamCompleter 加 lease timeout（与 `replanGuard` 思路一致）。
9. **H10 锁拆分**：`streamBatcher` 推送前后做解锁/重锁，避免持锁 `Gosched` 饿死。
10. **H19/H21 观测**：`subagentDropped` 与 `ContextActor.drop` 纳入 `/diag` 输出。

### 8.3 可以做
11. **H3/H4 显式注释**：把 `seelexctx/bridge.go` 5 处 `context.TODO` 标 "compat-only"；给 `errorCodeContextExceeded` 补 `presentUserError` case。
12. **H6 接口断言**：评估把 `mcpstack.TraceProvider` 适配成 `seelexctx/provider.Provider` 命名空间下。
13. **H7 事务测试**：`SessionRecord` 双写一致性测试。
14. **H13 截断方向**：补 `appendPlanNodeEvent` 的"头/尾"截断测试。
15. **H17 提前解析测试**：`SeelexAgentNode.blocks` 惰性解析在 plan_run 之前调用的安全性。
16. **H22/H24 mcpstack 健壮性**：拆 RWMutex + Calls 上限。

### 8.4 文档
17. 在每个 README 显式列出"本模块的事件/错误/取消契约"——目前是分散在 `runtime-tool-completion-deadlock.md` 这类散落文档中，门槛偏高。
18. 在 `DESIGN.md` 补一个"上下文 / 数据流主图"对应本章第 2 节——把评审门槛前置到设计文档。

---

## 9. 总体评分

| 维度 | 评分 | 理由 |
|---|:---:|---|
| 正确性 | A | 死锁教训明确记录；race 与并发断言覆盖关键路径；Window 区间装载、原子切换、parent/child 隔离、ProjectScope 越界都有显式测试。剩 P1 项需要进一步验证。 |
| 可读性 | A | 模块 README 完整、命名一致、状态分组清晰；27k+ 行核心库仍能保持单 RWMutex 门禁。 |
| 架构 | A | Application / frontend 不依赖 Seele 内部类型；Router / callback / Bridge 均通过窄接口装配；actor / mailbox / bounded pipeline 的 actor 模型贯穿一致（mcpstack 例外）。 |
| 安全性 | A | 工具证据后端截断、前端 escape；ProjectScope + NodeScope 越界拒绝；SandboxCapabilities fail-fast；无新增秘密、DSN、原始系统提示词暴露。 |
| 性能 | A | Conversation / tool events / DOM / pipeline buffer 均有上限；流式从逐 chunk 降为批次事件；窗口尾读把全量解析降为单 shard 解析。 |
| 可维护性 | A- | 大量 hook/桥接/适配，但**横切一致性**（ctx 透传、错误结构化、cancellation 边界、observable 丢失）尚有改进空间（H1-H25）。 |
| 测试 | A- | 关键包 race×3、JS syntax/57 Node tests 通过；本机缺 GCC 导致部分 race 未跑（已记录）；coverage 报告（`docs/2026-08-04-context-memory-lifecycle/finish-review.md`）74.6%，建议补 diff coverage 基线。 |

---

## 10. 致评审者

- 本评审基于静态阅读 `application/`, `seelexctx/`, `sessionstore/`, `seelebridge/`, `mcpstack/`, `session/`, `main.go`, `application_adapters.go` 与相关 README/文档。
- 任何标记为 **Hypothesis** 的项都需要进一步验证（建议从 `H1, H2, H5, H11, H25, H26/H27` 入手）。
- 文档已声明通过的真实账号 smoke（`docs/2026-08-04-context-memory-lifecycle/finish-review.md`）部分抵消了本机无 GCC 的 race 覆盖盲区；建议在 CI 上保留 Linux `-race -covermode=atomic` job。
- 本评审未触碰：plugin/ 各插件实现、gui/frontend、TUI/、build/release 脚本。这些是独立子系统。

