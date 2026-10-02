# core/context（根包分卷）

## 生态位

上下文控制相关集成测试

覆盖：`context*.go`；未归属文件由覆盖自检拦下。

## 跨轮前缀不变量与 provider 投影归零（2026-09-12）

**不变量**：每条 provider 请求的字节都以本会话更早发出的某条请求为前缀（相邻请求
即上一条）。回合内本就是纯追加（0 违反），唯一失效点在回合边界——修复前跨轮首请求
命中 65.9%、失效计费 8,837 tok，修复后 98.5% / 371 tok，与 Codex 臂持平。

- 规则：**携带工具调用的 assistant 消息，其 provider 投影正文恒为空**
  （`RepairEmptyHistoryContent`）。依据是 wire 事实——框架构造该消息时直接置 nil
  （Seele `session/loop.go:564`），provider 从未收到这段正文；事后补占位或补流式
  叙述都会让重投影字节与已发出字节分叉，使该点之后的 prefix cache 全部失效。
- 归零只作用于 **provider 投影**：durable 转写仍保留叙述，视图、轨迹与重启恢复
  不变。
- 同族规则（同一条"不写 wire 上从未存在的字节"）：**`tool` 角色空结果保持为空**。
  工具返回空串时 wire 上就是空正文，补 `MissingHistoryContent` 会让下一轮重投影从
  `""` 变成占位（同族分叉）；中断工具链的协议占位由 `RepairInterruptedToolChains`
  生成，不受影响。守卫用例：
  `context_prefix_invariant_test.go::TestContextPrefixInvariant_EmptyToolResult`。
- 同族第二处（2026-09-12 第二轮）：**工具结果的 provider 投影取 wire 原文，应用呈现
  文本只属于视图**。工具失败时 wire 上是框架合成的 `{"error": %q}`；超限时 wire 上是
  处理器归档引用（`result_ref=result:<callID>`），而视图/轨迹读的是应用呈现文本
  （`presentToolError` / `tr-<digest>` 归档引用）。记录侧为此增 provider-only 字段
  `provider_content`（`application/model/context.go` 的 `TranscriptEvent`、
  `sessionstore/sessionstore.go` 的 `Event`），三个 provider 投影出口统一取它：
  `sessionstore/durable_history.go` 的 `eventsToMessages`、`sessionstore/wire_assembler.go`
  的会话 wire 装配、`application/core/task_context/plan_transcript.go` 的
  `transcriptEventMessage`。守卫用例：
  `prefix_invariant_fullchain_test.go::TestFullChainPrefixInvariantToolErrorAcrossTurns`
  与 `...OversizedToolResultAcrossTurns`（比对生产路径录到的真实 wire 字节）、
  `sessionstore/provider_content_test.go`（落盘 + 冷载 + 重启恢复）。
- 工具轮说明正文（wire 上被丢弃、只经 `onChunk` 进视图的那段）**在工具钩子边界按
  迭代归位**到本次迭代的 assistant(tool_calls) 事件
  （`task_context.AttributeToolNarrationLocked`；回合收尾不再做事后填充——旧做法会
  把叙述写到别的轮次上）。归位只影响 record / 视图，投影侧仍归零，二者共同保证
"重投影 == 已发出"。归零有两处实现，对应两条投影出口：引擎历史路径
（`RepairEmptyHistoryContent`）与**生产实际出口**——框架 WorkingHistory 来自
`sessionstore.DurableHistory.Load`（尾窗重投影），归零在那里由
`sessionstore.ProviderWireMessages` 实施（2026-09-12 真实 API 冒烟发现前者到不了
wire）。守卫用例：`TestToolNarrationStaysWithOwningIteration`、
`TestConcurrentSessionsKeepOwnContent`、`TestFullChainPrefixInvariantAcrossTurns`。
- 红灯用例：`context_prefix_invariant_test.go`（沿生产装配路径断言「每条请求是
  上一条的前缀」，修复前在 `t1.iter3 → t2.iter1` 处 BREAK）；三臂对照组与逐边界
  数据见 `docs/research/2026-09-11-seelex-vs-codex-context-strategy-control-group.md`。

**修复引入的耦合（报警器，不是退化）**：归零依赖上游「wire 不保留工具轮正文」这一
行为。对照组 S-fix / Codex 两臂假设相反，修复后由 98.6% / 98.3% 掉到 74.4% / 46.8%，
因此成为**耦合报警器**——若上游改为在 wire 上保留工具轮正文，必须同步取消归零，
届时这两臂重新变好、生产臂变差。另有一处本轮实测踩中的耦合：
`BackfillAssistantReasoning` 的候选匹配对**工具轮事件**必须以「引擎侧正文」为准
——事件侧正文现在是归位来的说明文本，若仍要求内容相等，推理草稿会静默失配、
跨轮字节在 msg#1 分叉（研究文档 §7.8）。同一「事后改写」家族的其他来源与收口进度以
`context_runtime/history.go` 的 doc 注释和研究文档 §7/§8 为准（本 README 不复制易变
状态）。

## 冷加载会话的 `/compact`：当场折叠，不再只登记（2026-09-24）

**触发**：用户在刚冷加载的会话里执行 `/compact`，拿到的是「当前会话没有进行中的
执行纪元（例如刚冷加载或刚清空），现在没有可折叠的请求上下文；已登记：下一条消息
组装上下文前立即压缩」。他接着想问「我需要你的摘要内容」——而命令早已结束，登记的
兑现要等下一条消息，用户看到的是「按了没反应」。

**根因**：显式压缩的入口判据是「该会话有匹配当前 request 的执行纪元」
（`CompactContextNow` 在 `state == nil || state.RequestID == ""` 时只登记）。冷加载
会话的 transcript 与引擎历史**都已装载**，缺的只是"一个在飞回合的 RequestID"——
把"没有在飞回合"读成了"没有可折叠的上下文"。2026-09-23 那轮拒绝"伪造纪元"是对的
（伪造会把"有人在跑这个会话"写进可见面），但落成了"只登记"，等于把用户明确要求的
压缩推迟到他自己再发一条消息。

**改法**：新增**会话级维护身份**（`task_context/session_context_maintenance.go`）：
`BeginSessionContextMaintenanceLocked` 给这类会话开一个带前缀的维护 `RequestID`
（`session-maintenance:<sessionID>`），没有任务状态时按会话自己的事实（最后一条真实
用户输入作 objective）建一份上下文状态，状态标记 `idle`；`CompactContextNow` 用它跑
同一条显式折叠路径（判据量照算、记录照落、帧正文照出、引擎历史照换、按会话落盘），
结束后 `EndSessionContextMaintenanceLocked` 撤销身份并保留压缩产生的
`ContextVersion`/`ContextCompactions`/checkpoint。三条诚实约束：不写
`ChatState.Running`、不设快照 `Chat.RequestID`、不建任务注册表条目；已有在飞回合时
**拒绝**发放身份（退回纪元路径），压缩失败也**必须**撤销身份。空会话（transcript 与
引擎历史都没有对话消息）维持登记语义——折叠空上下文只会产出一条区间为空的记录。

**回执**：`explicit_after_turn` 来源不变，但结果面新增 `NoEpoch`（工具 JSON 字段
`no_epoch`，omitempty），命令与工具共用的 `compactionRecordNote` 因此能说出
「会话没有在飞回合（冷加载或刚清空），已按会话级显式压缩立即执行（折叠已装载的
上下文并落记录，无需下一条消息）：已压缩上下文：v…」，并照旧回带帧正文 `frame_ref`
（用户问「摘要内容」时读到的就是它，状态页「上下文压缩」条目可展开）。空区间时只有
登记语义仍保留旧措辞，且把原因改成「没有在飞回合、也没有已装载的对话材料（空会话）」。

**有牙证明**：把 `compactSessionContextWithoutEpoch` 开头改回 `return
CompactResult{}, false, nil`（旧行为）后，
`TestCompactContextWithoutTaskExecutionCompactsImmediately` 红在「冷加载会话必须
当场压缩并留记录（不是登记）」、`TestCompactWithoutEpochKeepsExecutionFacesClean` 红在
「压缩后会话应保留上下文状态」；撤销探针后两条全绿。
`TestCompactEmptySessionRegistersAndRedeemsOnNextMessage` 钉住空会话的登记与"下一条
消息兑现"仍在。

**已知边界**（本轮不扩大范围）：① 压缩记录在**下一条消息的回合收尾持久化**后可能从
`record.Execution.Task.ContextCompactions` 消失（回合状态按 `IsContinuableStatus`
重建，不继承记录）——与本轮之前的"回合之间压缩"同一口径，帧正文仍可从内容存储按
`frame_ref` 回读；② 冷加载且没有任务证据时，帧正文的证据区只有 objective/plan，
区间与四区 token 事实照旧完整。

## 回合内即时压缩：同一套方法 + 检查点（2026-09-27，替换环内把手）

**旧模型的问题**：`Session.ChatStream` 从进函数持 `Session.mu` 到出函数，工具派发与全部循环回调
都在同一 goroutine、同一把锁内。`compact_context` 交给模型在回合内调用，其落点
`CompactContextNow → prepareExecutionContextFor` 第一件事就是读引擎历史（`EnginePort.HistoryFor →
RawHistoryFor → engine.History()`）——同 goroutine 抢自己已持有的非重入锁 = 永久自锁，而且此时
`port.mu` 的读锁还攥在手里，之后任何要 `port.mu.Lock` 的动作（含别的会话开新回合）全部排队。反向
探针 `TestEngineHistoryFromToolHandlerSelfBlocks` 实测过这条。当时的绕法是"环内把手"：引擎取锁后往
本轮 ctx 注入 `session.InLoop`，宿主凭它读写历史。

**Seele 换了模型，宿主跟着简化**：回合准入改成**闸门**（不持锁跑整轮），工作历史改由**短临界区**
保护。于是环内与环外走的是同一套方法、同一份数据：

```
回合内 / 回合外（同一套方法）   History()            只取一次微秒级临界区，**永不阻塞**
                            ReplaceHistory(h)   忙时排队到循环的下一个检查点，空闲时立即应用
                            （检查点：模型调用前 / assistant 落历史后 / tool 结果 append 前后）
```

- 宿主不再需要判断"我在不在环内"，也不再需要把 ctx 透传进折叠：`context_runtime` 的
  `loopHistoryChannel` 与 `contract.InLoopEngine` 整条删除，折叠统一走 `foldHistory` /
  `replaceFoldHistory`（按会话路由到 `HistoryFor` / `ReplaceHistoryFor`）。
- 回合内折叠**仍然当场生效**，落点从"锁内直写"变成"下一个检查点"：`compact_context` 在工具
  handler 里提交替换，循环紧接着（tool 结果 append **之前**）排空它，因此同回合的下一次模型请求
  读到的已是折叠后的历史——对压缩正是期望语义（它本就要在下一次请求才生效）。
- 忙会话的**锁外**折叠也不再"登记待安装 + 等回合收尾换引擎"：`EnginePort.replaceRawHistoryFor`
  把替换直接交给引擎排队（`queueSessionHistory`），登记表只在引擎不具备该能力时兜底。折叠因此对
  下一次请求生效，而不是等下一次装载。
- 下界不变：折叠产物必须保留「assistant 已带 tool_calls、其结果尚未 append」的在飞尾部
  （`withInFlightTail`，现在对**所有**路径都做），否则引擎以 `ErrInFlightToolCallDropped` 拒收，
  紧随其后 append 的结果行成孤儿。多保留的是本轮自己的消息，**不参与压缩判据**。
- 观测面读历史（`/history`、工作区切换、子代理落账）不再需要"循环发布的检查点快照"：
  `History()` 任何时刻都返回当前工作历史，且不会因长流式阻塞。
- 同会话压缩门（`context_compact_gate.go`）仍在：并发折叠会各自读同一段历史、各自替换，后写的
  那份把前一轮整个丢掉。区别是现在**所有**调用方都走阻塞领轮——没有谁持着"对方要用的锁"，等待只
  会排队。

**有牙证明**（`internal/adapters/engine_port_history_test.go`，真实 Session）：
`TestReplaceHistoryInsideTurnTakesEffectInSameTurn`（同回合下一次请求已带折叠帧；终态历史 =
帧 + 在飞 assistant + tool 结果 + 收尾 assistant）、`TestReplaceHistoryDropsInFlightTailIsRefused`
（丢在飞尾部被拒且不动历史）、`TestReplaceHistoryOutsideTurnAppliesImmediately`（锁外立即落地）；
`engine_port_reentrance_test.go` 的 `TestEngineHistoryFromToolHandlerReturnsPromptly` 是旧自锁
断言的反转报警器；`application/core/context_compact_gate_test.go` 钉同会话压缩门的等待与收口。

**边界**：① 忙会话上的写回是"下一个检查点生效"而不是"立即"，期间的其它写入按提交顺序一起排空；
② `/compact` 与自动压缩共用同一条装配路径，因此两者的历史出入口完全一致（不再分环内环外）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### context_budget_frequency_test.go

- `func TestContextBudgetDoesNotRecompactWithoutNewContent(t *testing.T)` — TestContextBudgetDoesNotRecompactWithoutNewContent 钉住回合边界达峰判据的

### context_budget_last_resort_test.go

- `func pinMechanismCompactionRatios(t *testing.T)` — pinMechanismCompactionRatios 把压缩阈值钉回 75/90/60：这组用例验证的是
- `func TestContextBudgetOvershootKeepsNewestSettledRound(t *testing.T)` — TestContextBudgetOvershootKeepsNewestSettledRound：达峰装配时单个已定稿
- `func TestContextBudgetProactivelyCompactsAtHardThreshold(t *testing.T)` — TestContextBudgetProactivelyCompactsAtHardThreshold：装配结果落在硬阈值
- `func TestContextBudgetOvershootCompactsWhenNewestExceedsFullBudget(t *testing.T)` — TestContextBudgetOvershootCompactsWhenNewestExceedsFullBudget：最新轮自身

### context_budget_margin_idempotency_test.go

- `func pinIneffectiveFoldRatios(t *testing.T)` — pinIneffectiveFoldRatios 把压缩比例钉成「折叠落点够不到软线」的**合法**档：
- `func TestContextBudgetSkipsFoldWithoutMargin(t *testing.T)` — TestContextBudgetSkipsFoldWithoutMargin 钉住软线折叠的**幂等/有效性校验**：

### context_cache_divergence_probe_test.go

- `func newPrefixProbeHarness(t *testing.T, window int) *prefixProbeHarness`
- `func (h *prefixProbeHarness) startStream()` — startStream 绑定本会话的流式输出（生产 runChat 的 newBatchedDeltaSink 边界）。
- `func (h *prefixProbeHarness) beginTurn(input string)` — beginTurn 复刻 runChat 开头 + Seele 循环开头的生产顺序：
- `func (h *prefixProbeHarness) streamText(text string)` — streamText 走生产流式链路写可见正文（跨 ReAct 迭代追加到同一条 assistant
- `func (h *prefixProbeHarness) llmIteration(engineContent, reasoning string, calls []types.ToolCall)` — llmIteration 复刻一次 LLM 调用之后的引擎/transcript 状态：
- `func (h *prefixProbeHarness) toolRound(name, callID, arguments, result string)` — toolRound 复刻 ToolHookBridge 的 OnToolStart/OnToolComplete 生产投影。
- `func (h *prefixProbeHarness) finishTurn(reply string)` — finishTurn 复刻 runChat 的回合收尾（chat.go:175/180）：补终态 assistant 事件 +
- `func (h *prefixProbeHarness) capture(turn, iter int, label string)` — capture 固化当前工作历史会产生的 provider 请求（serializeRequest 与冒烟
- `func (h *prefixProbeHarness) toolCallEventContents() []string` — toolCallEventContents 返回 transcript 中 assistant 工具调用事件的正文。
- `func (h *prefixProbeHarness) visibleAssistantText() string` — visibleAssistantText 返回可见投影中最后一条 assistant 正文（生产视图里
- `func prefixProbeCompare(prev, cur prefixProbeCall) prefixProbeDivergence` — prefixProbeCompare 计算 prev → cur 的字节 LCP 与失效后缀（block 粒度按
- `func prefixProbeFirstDiff(prev, cur []EngineMessage) (int, string)`
- `func probeMessagesEqual(left, right EngineMessage) bool`
- `func probeEngineCalls(calls []types.ToolCall) []EngineToolCall`
- `func probeCallIDs(calls []EngineToolCall) string`
- `func probeStreamedContent(cfg prefixProbeConfig) string` — probeStreamedContent 返回生产路径下引擎 assistant 工具调用消息的流式正文
- `func splitProbeChunks(text string, size int) []string`
- `func summarizeProbeText(text string) string`
- `func probeWindow(text string, offset int) string` — probeWindow 返回 text 中 offset 附近的可读窗口（定位 system 漂移点）。
- `func logProbeDivergence(t *testing.T, kind string, divergence prefixProbeDivergence)`
- `func TestContextCacheDivergenceProbe_TwoTurnToolCliff(t *testing.T)` — TestContextCacheDivergenceProbe_TwoTurnToolCliff 驱动 1 个含工具调用的回合
- `func runPrefixProbeTwoTurns(t *testing.T, cfg prefixProbeConfig)`
- `func TestContextCacheDivergenceProbe_MemoryBlockPosition(t *testing.T)` — TestContextCacheDivergenceProbe_MemoryBlockPosition 用生产装配器
- `func probeAccumulatedHistory(messages, charsPerMessage int) []types.Message`
- `func probeAssembleRequest(t *testing.T, assembler seelectx.RequestAssembler, query string, accumulated []types.Message) seelectx.AssembledRequest`
- `func probeSerializeAssembly(request seelectx.AssembledRequest) string`
- `func probeMessageIndex(request seelectx.AssembledRequest, marker string) int`
- `func probeRole(request seelectx.AssembledRequest, marker string) string`
- `func probeMessageIndexByBytes(request seelectx.AssembledRequest, offset int) int`
- `func probeRoleByBytes(request seelectx.AssembledRequest, offset int) string`
- `func probeBlockTokens(block *seelectx.PromptBlock) int`
- `func probeBlockText(block *seelectx.PromptBlock) string`
- `func probeSegmentIDs(selected []memory.Candidate) []string`
- `func probeSameSegments(left, right []memory.Candidate) bool`

### context_cache_smoke_test.go

- `func smokeService(t *testing.T, window int) (*Service, *fakeEngine)` — smokeService 构建一个可驱动生产装配路径的服务。
- `func appendSettledRound(service *Service, round int, toolResultToken int)` — appendSettledRound 追加一个"已定稿轮次"的 transcript 事件（user → assistant
- `func systemPromptFor(service *Service) string` — systemPromptFor 取当前 system（与 Prepare 内部使用的同一组装函数）。
- `func serText(label, text string) string` — serText 生成 JSON 前缀友好串行化片段（固定键序、无长度前缀；内容前缀相等
- `func serializeRequest(system string, history []EngineMessage, input string, tools []Tool) string` — serializeRequest 把一次请求（system + history + current input + tools）固化为
- `func lcpBytes(a, b string) int` — lcpBytes 返回两个字节串的最长公共前缀长度。
- `func blockFloor(shared, block int) int` — blockFloor 按缓存块粒度对齐（provider 只命中整块）。
- `func measure(prev, current string, totalTok int) (shared int, ratioRaw, ratio64, ratio1k float64)` — measure 计算本轮与上一轮请求的命中指标（block 粒度按 provider 整块缓存语义
- `func logSmokeRow(t *testing.T, row smokeTraceRow, input string)` — smokeCacheHitRow 汇总输出一行。
- `func TestContextCacheSmoke_SingleSessionLongRun(t *testing.T)` — TestContextCacheSmoke_SingleSessionLongRun 复现"单会话多轮长会话"的最优可达
- `func TestContextCacheSmoke_SkillActivationAppendOnly(t *testing.T)` — TestContextCacheSmoke_SkillActivationAppendOnly 对照场景：会话中段激活一个
- `func TestContextCacheSmoke_ProviderCacheContentionModel(t *testing.T)` — TestContextCacheSmoke_ProviderCacheContentionModel 是一个显式标注的推演模型：

### context_compact.go

- `func compactionReasonLabel(reason string) string` — compactionReasonLabel 渲染压缩原因（用户可读）。未知原因原样返回，不编造。
- `func compactionRecordNote(result ContextCompactionResult) string` — compactionRecordNote 渲染「压缩已落记录」的回执：版本 + 原因 + 被压区间 +
- `func compactionGateChecklist(gates []CompactionGateTiming) string` — compactionGateChecklist 把逐关耗时渲染成一行事实清单，例如
- `func compactionGateDuration(milliseconds int) string` — compactionGateDuration 渲染单关耗时。毫秒整数里 0 的含义是"不到一毫秒"，
- `func compactionRangeLabel(result ContextCompactionResult) string` — compactionRangeLabel 把压缩结果面里**已有的**区间字段（MessageFrom/To、
- `func (service *Service) CompactContextNow(ctx context.Context) (ContextCompactionResult, error)` — CompactContextNow 压缩当前执行会话（命令/工具共用）：会话从 ctx 解析，
- `func (service *Service) CompactContextHandler(ctx context.Context, argsJSON string) (string, error)` — CompactContextHandler 实现 compact_context 工具：模型在上下文逼近上限、
- `func contextCompactionGates(gates []context_runtime.CompactionGateTiming) []CompactionGateTiming` — contextCompactionGates 把 context_runtime 的门禁耗时映射为回执面类型（JSON
- `func newContextCompactionResult(outcome context_runtime.CompactResult) ContextCompactionResult`

### context_compact_across_rounds_repro_test.go

- `func TestReproCompactionRecordSurvivesNextRoundAfterColdMaintenance(t *testing.T)` — TestReproCompactionRecordSurvivesNextRoundAfterColdMaintenance 冷加载维护
- `func TestReproContextFactsSurviveCompletedTurnBoundary(t *testing.T)` — TestReproContextFactsSurviveCompletedTurnBoundary 回合**正常收尾**

### context_compact_gate.go

- `func (service *Service) isCompactingLocked(sessionID string) bool` — isCompactingLocked 报告该会话是否有一轮上下文压缩正在进行（调用方持有
- `func (service *Service) signalCompactionLocked()` — signalCompactionLocked 广播一次「compacting 集合已变化」（调用方持有
- `func (service *Service) compactionSignalLocked() <-chan struct` — compactionSignalLocked 返回当前收口信号（调用方持有 Core.ViewMu）。惰性
- `func (service *Service) acquireCompactionRound(ctx context.Context, sessionID string) error` — acquireCompactionRound 领取该会话的压缩轮：已有轮在跑时先等它收口（同会话
- `func (service *Service) releaseCompactionRound(sessionID string)` — releaseCompactionRound 收口该会话的压缩轮并唤醒等待方。幂等：没领过轮的
- `func (service *Service) awaitCompactionRound(ctx context.Context, sessionID string) error` — awaitCompactionRound 等到该会话没有压缩轮在跑（ctx 取消即返回错误）。
- `func (service *Service) deferSubmitUntilCompacted(ctx context.Context, sessionID, text string)` — deferSubmitUntilCompacted 把一次对话提交挂到该会话压缩轮的收口点：门开着时

### context_compact_gate_test.go

- `func seedLongRounds(t *testing.T, service *Service, requestID string)` — seedLongRounds 与 TestCompactContextHandlerFoldsTranscript 同一份量：4 轮、每轮
- `func TestCompactContextWaitsForHeldCompactionGate(t *testing.T)` — TestCompactContextWaitsForHeldCompactionGate 钉住等待方向：门已被占用时，第二次

### context_compact_index_test.go

- `func (recorder *compactionIndexRecorder) push( _ context.Context, sessionID string, request context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)`
- `func (recorder *compactionIndexRecorder) snapshot() ([]context_runtime.CompactionIndexRequest, []string)`
- `func (runtime *compactionIndexRuntime) PushCompactionFrame( ctx context.Context, sessionID string, request context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)`
- `func indexGateDetail(t *testing.T, frames []compactionProgressFrame) string` — indexGateDetail 取出本轮进度里门禁 index 的事实行（找不到直接失败：一轮真折叠
- `func appendIndexRounds(t *testing.T, service *Service, taskID string)` — appendIndexRounds 追加 4 个已定稿轮（每轮约 4 万 tokens），足以越过软阈值触发
- `func TestFoldPushesCompactionFrameIntoIndex(t *testing.T)` — TestFoldPushesCompactionFrameIntoIndex：装配层折叠必须把这次折出的区间推进会话
- `func TestFoldWithoutIndexFaceReportsDegradedGate(t *testing.T)` — TestFoldWithoutIndexFaceReportsDegradedGate：索引面未装配（Runtime 不实现
- `func TestFoldPushFailureIsReportedNotFatal(t *testing.T)` — TestFoldPushFailureIsReportedNotFatal：索引面在、推帧报错时，折叠与本次请求
- `func TestFoldWithoutOverflowReportsSkippedNotUnavailable(t *testing.T)` — TestFoldWithoutOverflowReportsSkippedNotUnavailable：索引面在，但这次折叠**没有

### context_compact_local_fallback_repro_test.go

- `func (runtime *compactionReadbackRuntime) ReadbackCompactionSummary( _ context.Context, _ string, _ context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)`
- `func TestFoldWithFailedModelReadbackLeavesContextUntouched(t *testing.T)` — TestFoldWithFailedModelReadbackLeavesContextUntouched（现场复现）：
- `func TestFoldWithModelReadbackStillFolds(t *testing.T)` — TestFoldWithModelReadbackStillFolds：对照组——读数**拿到了**模型读后感

### context_compact_no_summary_test.go

- `func (runtime *compactionIndexNoSummaryRuntime) CompactionSummaryAvailable() bool`
- `func TestFoldWithoutModelSummaryLeavesContextAndStackUntouched(t *testing.T)` — TestFoldWithoutModelSummaryLeavesContextAndStackUntouched 钉住用户口径

### context_compact_progress_test.go

- `func drainCompactionProgress(t *testing.T, subscription event.Subscription) []compactionProgressFrame` — drainCompactionProgress 取出订阅里已排队的 compaction.progress。发布与装配
- `func gateSequence(frames []compactionProgressFrame) []string` — gateSequence 抽出运行中门禁的 id 序列（终局事件不带 gate；起手帧不是"某一关
- `func assertProgressShape(t *testing.T, frames []compactionProgressFrame, wantSession string)` — assertProgressShape 校验所有来路都要守的公共形状：路由键齐、序号单调、总数
- `func assertBeginFrame(t *testing.T, frames []compactionProgressFrame)` — assertBeginFrame 钉起手帧的事实性：显式压缩在动第一个重活之前就把"这一轮开始
- `func TestExplicitCompactEmitsOrderedProgressGates(t *testing.T)` — TestExplicitCompactEmitsOrderedProgressGates：/compact 显式折叠时逐关报告，
- `func TestAutoCompactionEmitsProgressGates(t *testing.T)` — TestAutoCompactionEmitsProgressGates：自动路径（软阈值）在回合执行中折叠时
- `func TestExplicitCompactGateTimeline(t *testing.T)` — TestExplicitCompactGateTimeline：逐关计时是这一轮压缩**串行工作**的唯一证据。
- `func TestCompactProgressTerminatesOnAssemblyError(t *testing.T)` — TestCompactProgressTerminatesOnAssemblyError：装配失败也必须收口。结构性超限
- `func TestNoProgressEventsWithoutFold(t *testing.T)` — TestNoProgressEventsWithoutFold：没折叠就没有进度。「登记为下一条消息兑现」
- `func TestMaintenanceCompactEmitsOrderedProgressGates(t *testing.T)` — TestMaintenanceCompactEmitsOrderedProgressGates：会话级维护身份路径（冷加载、
- `func TestCompactReceiptCarriesGateChecklist(t *testing.T)` — TestCompactReceiptCarriesGateChecklist：回执自带门禁清单（逐关 id + 毫秒）。
- `func TestFrontendGateLabelsMatchBackendOrder(t *testing.T)` — TestFrontendGateLabelsMatchBackendOrder：门禁 id 是跨语言协议字面量——后端

### context_compact_selfdeadlock_repro_test.go

- `func (stub *archiverSessionsStub) SaveCommitWorkspace(_, _ string, _ sessionstore.Commit) error`
- `func (stub *archiverSessionsStub) commitCount() int`
- `func (runtime *productionArchiveIndexRuntime) PushCompactionFrame( ctx context.Context, _ string, request context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)`
- `func TestExplicitCompactFramePushSelfDeadlocksRepro(t *testing.T)` — TestExplicitCompactFramePushSelfDeadlocksRepro：在真实归档接线（main.go:310 同形）

### context_compact_test.go

- `func compactTestService(t *testing.T, requestID string) (*Service, *fakeEngine, string)` — compactTestService 构造带活跃任务执行的会话：主动压缩绑定请求纪元
- `func TestCompactContextHandlerFoldsTranscript(t *testing.T)` — TestCompactContextHandlerFoldsTranscript：compact_context 工具（= /compact
- `func TestCompactManualFoldsBelowThreshold(t *testing.T)` — TestCompactManualFoldsBelowThreshold：显式压缩（/compact、compact_context）
- `func TestCompactManualAfterTurnRecordsExplicitOrigin(t *testing.T)` — TestCompactManualAfterTurnRecordsExplicitOrigin：回合已收尾（任务状态不再是
- `func TestCompactAfterTurnSurfacesRecordWithoutTaskFace(t *testing.T)` — TestCompactAfterTurnSurfacesRecordWithoutTaskFace：快照里还没有任务面时
- `func TestCompactionFrameBodyIsReadableByRef(t *testing.T)` — TestCompactionFrameBodyIsReadableByRef：记录里的 frame_ref 真能读回帧正文——
- `func TestAutoCompactionAfterTurnKeepsRecordGate(t *testing.T)` — TestAutoCompactionAfterTurnKeepsRecordGate：自动路径（软/硬阈值）在回合已
- `func TestCompactContextWithoutTaskExecutionCompactsImmediately(t *testing.T)` — TestCompactContextWithoutTaskExecutionCompactsImmediately：会话没有任务执行
- `func TestCompactWithoutEpochKeepsExecutionFacesClean(t *testing.T)` — TestCompactWithoutEpochKeepsExecutionFacesClean：会话级维护身份不得在可见面
- `func TestCompactEmptySessionRegistersAndRedeemsOnNextMessage(t *testing.T)` — TestCompactEmptySessionRegistersAndRedeemsOnNextMessage：会话真的没有可折叠
- `func TestCompactCommandRegisteredAndSharesPath(t *testing.T)` — TestCompactCommandRegisteredAndSharesPath：/compact 命令注册成功，且与工具
- `func TestCompactCommandWithoutEpochFoldsImmediately(t *testing.T)` — TestCompactCommandWithoutEpochFoldsImmediately：命令入口（用户真的按回车的
- `func TestCompactCommandNoticeReportsFoldedRange(t *testing.T)` — TestCompactCommandNoticeReportsFoldedRange：记录分支的提示只说**记录里已有的
- `func TestCompactionRangeLabel(t *testing.T)` — TestCompactionRangeLabel：区间渲染只在**有边界**时成段——空区间返回空串
- `func TestCompactCommandNeverReportsFoldWithoutRecord(t *testing.T)` — TestCompactCommandNeverReportsFoldWithoutRecord：/compact 是显式路径，只要

### context_compact_trigger_liveness_test.go

- `func (runtime *reentrantGateIndexRuntime) PushCompactionFrame( _ context.Context, sessionID string, _ context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)`
- `func triggerLivenessFixture(t *testing.T, requestID string) (*Service, *reentrantGateIndexRuntime, string)` — triggerLivenessFixture 是一个"已越过硬阈值、必折出非空溢出区"的会话：
- `func releaseGate(runtime *reentrantGateIndexRuntime) func()` — releaseGate 造一个**幂等**放行器：返回的函数可以反复调用（含 defer + 显式调用），
- `func awaitPushEntered[T any](t *testing.T, runtime *reentrantGateIndexRuntime, inFlight <-chan T, what string)` — awaitPushEntered 等到夹具真的走到推帧（否则判据是空集上的真命题）。
- `func assertInteractionFaceLive(t *testing.T, service *Service, what string)` — assertInteractionFaceLive 断言交互面四个入口在推帧进行中照常返回。任一被冻 =
- `func TestAutoFoldPushKeepsInteractionFaceLive(t *testing.T)` — TestAutoFoldPushKeepsInteractionFaceLive：**自动**入口（rawTokens ≥ 硬阈值，
- `func TestMaintenanceFoldPushKeepsInteractionFaceLive(t *testing.T)` — TestMaintenanceFoldPushKeepsInteractionFaceLive：**维护入口**
- `func TestSessionLevelCompactPushKeepsInteractionFaceLive(t *testing.T)` — TestSessionLevelCompactPushKeepsInteractionFaceLive：**无在飞回合的会话级**
- `func TestExplicitFoldWhileAutoFoldInFlightDoesNotInterlock(t *testing.T)` — TestExplicitFoldWhileAutoFoldInFlightDoesNotInterlock：**显式与自动并发**。
- `func TestCompactionChurnOnOneSessionDoesNotHang(t *testing.T)` — TestCompactionChurnOnOneSessionDoesNotHang：把触发入口混在一起反复跑（显式压缩

### context_compact_viewmu_hold_repro_test.go

- `func (runtime *blockingIndexRuntime) PushCompactionFrame( ctx context.Context, _ string, _ context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)`
- `func returnsWithin(d time.Duration, fn func()) bool` — returnsWithin 报告 fn 是否在 d 之内返回（返回 false = 阻塞住了）。
- `func TestExplicitCompactReproViewMuHoldAcrossFramePush(t *testing.T)` — TestExplicitCompactReproViewMuHoldAcrossFramePush：推帧进行中，交互面是否仍然可用。

### context_compactions_restore_repro_test.go

- `func compactedSessionStore(t *testing.T) (*archiveSessions, string, ContextCompaction, int)` — compactedSessionStore 造一份「重启前」的持久面并结束进程：第一份 Service 在
- `func coldResume(t *testing.T, store *archiveSessions, sessionID string) *Service` — coldResume 在 store 上新建 Service（进程重启）并冷加载会话。
- `func TestReproCompactionStackSurvivesProcessRestart(t *testing.T)` — TestReproCompactionStackSurvivesProcessRestart 应用重启后，旧会话的压缩栈
- `func TestReproCompactionStackVisibleWithoutProjection(t *testing.T)` — TestReproCompactionStackVisibleWithoutProjection projection 缺失（老记录、
- `func TestReproCompactionRetainedFromRestoredFromRecord(t *testing.T)` — TestReproCompactionRetainedFromRestoredFromRecord 冷恢复时保留窗口起点

### context_controller_test.go

- `func (runtime runtimeWithContextLimits) ContextWindow() int`
- `func (runtime runtimeWithContextLimits) MaxOutputTokens() int`
- `func TestRejectToolResultsPreservesPairingWithoutPreview(t *testing.T)`
- `func TestPrepareExecutionContextCountsActiveSystemPrompt(t *testing.T)` — TestPrepareExecutionContextCountsActiveSystemPrompt：system 提示自身就超出
- `func TestPrepareExecutionContextUsesRuntimeContextLimits(t *testing.T)`
- `func TestPreparedRequestAutonomouslyCompactsOversizedRounds(t *testing.T)`
- `func TestPrepareExecutionContextOrderAndNoCheckpoint(t *testing.T)`
- `func TestTranscriptTailKeepsInterruptedRoundAndDropsOrphanToolProtocols(t *testing.T)`
- `func TestTranscriptTailKeepsTrailingUnansweredUserInput(t *testing.T)`
- `func TestTranscriptTailAccumulatesAllSettledRoundsWhenUnlimited(t *testing.T)`
- `func TestPrepareExecutionContextAccumulatesAllSettledRoundsAndByteStable(t *testing.T)`
- `func historyContents(history []EngineMessage) []string`
- `func TestRejectToolResultsRecognizesFrameworkTruncationMarker(t *testing.T)`

### context_hook_test.go

- `func TestIterationHookDoesNotTriggerContextControl(t *testing.T)`

### context_narration_attribution_test.go

- `func TestToolNarrationStaysWithOwningIteration(t *testing.T)`

### context_prefix_invariant_test.go

- `func TestContextPrefixInvariant_CrossTurn(t *testing.T)`
- `func TestContextPrefixInvariant_EmptyToolResult(t *testing.T)` — TestContextPrefixInvariant_EmptyToolResult 覆盖第 3 处事后改写：工具返回空
- `func probeAssertPrefixInvariant(t *testing.T, harness *prefixProbeHarness)` — probeAssertPrefixInvariant 断言相邻请求保持字节前缀；首条违反即失败并归因。
- `func probeMessageAt(messages []EngineMessage, index int) EngineMessage` — probeMessageAt 返回序列化消息快照中的第 index 条（越界返回空消息）。
- `func probeMessageContentAt(messages []EngineMessage, index int) string`
- `func probeFirstDiffKind(previous, current []EngineMessage, index int) string` — probeFirstDiffKind 归因首个差异消息的成因——只在两侧 role / tool_calls ID

### context_restore_prefix_order_test.go

- `func TestRunningSessionAccumulatesAllRoundsInOrderControl(t *testing.T)`
- `func TestColdRestoreTailFirstRequestKeepsTranscriptOrder(t *testing.T)`
- `func appendOrderedRoundsLocked(service *Service, rounds int) []TranscriptEvent`
- `func orderRoundLabels(history []EngineMessage) []string`
- `func expectedOrderRoundLabels(rounds int) []string`

### context_retain_floor_test.go

- `func applyTestLimits(t *testing.T, mutate func(*seelexctx.Limits))` — applyTestLimits 在测试内改一项 limits 配置并在结束恢复（进程内生效配置）。
- `func TestValidateRetainWindowStartupCheck(t *testing.T)` — TestValidateRetainWindowStartupCheck：《压缩四区模型》的配置校验——保护区下限
- `func TestCompactRetainFloorRaisesProtectedWindow(t *testing.T)` — TestCompactRetainFloorRaisesProtectedWindow：保护区下限生效——同样的 transcript
- `func TestCompactFrameBodyCarriesZoneLayout(t *testing.T)` — TestCompactFrameBodyCarriesZoneLayout：四区显式化的**报表落点**——压缩记录指向的

### context_strategy_ab_probe_test.go

- `func abBody(line string, repeat int) string`
- `func abDemoScript() []abTurnScript` — abDemoScript 是一条 4 轮脚本会话，每轮都发 2 次工具调用（与生产 ReAct 形状一致）。
- `func abSerialize(system string, items []EngineMessage, tools []Tool) string` — abSerialize 把一次"provider 可见请求"固化为确定性字节流：
- `func abToolsSignature(tools []Tool) string`
- `func abSortedTools(tools []Tool) []Tool`
- `func abCapture(arm string, turn, iter int, system string, items []EngineMessage, tools []Tool) abCall`
- `func abCompare(prev, cur abCall) abDivergence`
- `func abLog(t *testing.T, kind string, d abDivergence)`
- `func runABProductionArm(t *testing.T, label string, script []abTurnScript, cfg prefixProbeConfig) *abRun`
- `func runABCodexArm(t *testing.T, label string, script []abTurnScript, system string, tools []Tool) *abRun` — runABCodexArm 按 Codex 的发布策略构造同一脚本的请求序列：
- `func TestContextStrategyAB_CrossTurnScript(t *testing.T)`
- `func abCallAt(run *abRun, turn, iter int) *abCall`
- `func abPreview(text string, limit int) string`
- `func TestContextStrategyAB_WorkedExample(t *testing.T)` — TestContextStrategyAB_WorkedExample 把同一个回合边界（第 1 轮最后一次请求 →
- `func TestStrategyAB_NarrationAttributionAcrossTurns(t *testing.T)` — TestStrategyAB_NarrationAttributionAcrossTurns 沿生产归位路径
- `func lastIter(run *abRun, turn int) int`
- `func TestContextStrategyAB_DynamicFactsPlacement(t *testing.T)` — TestContextStrategyAB_DynamicFactsPlacement 用**同一份动态内容**、**同一次
- `func abCodexPlacementMessages(accumulated []types.Message, fragment, query string) []types.Message` — abCodexPlacementMessages 是 arm C 的投影：常量指令 + 已发布累积 context +
- `func abSerializeAssembled(messages []types.Message) string`
- `func abRoleSeq(messages []types.Message) string`

### context_window_rule_test.go

- `func applyWindowConfig(t *testing.T, config WindowConfig)` — applyWindowConfig 应用 window 段并在测试结束恢复（进程内生效配置）。
- `func appendWindowRounds(t *testing.T, service *Service, requestID string, rounds, answerChars int) []string` — appendWindowRounds 追加 rounds 个已定稿轮次（短问 + 长答），返回每轮标记。
- `func retainedRounds(history []EngineMessage, marks []string) int` — retainedRounds 统计仍留在 provider 历史里的轮次数（按轮标记匹配）。
- `func compactNow(t *testing.T, service *Service, sessionID string) ContextCompaction` — compactNow 走显式压缩落点（/compact、compact_context 同一条路径），返回
- `func TestCompactRetainedPrefixIsMinOfTwoWindows(t *testing.T)` — TestCompactRetainedPrefixIsMinOfTwoWindows：保留前缀 = min(token1, token2)。
- `func TestCompactHardThresholdForcesCompression(t *testing.T)` — TestCompactHardThresholdForcesCompression：window.force_compact_tokens 是硬
- `func TestCompactHardThresholdBypassesEpochThrottle(t *testing.T)` — TestCompactHardThresholdBypassesEpochThrottle：硬压缩不被"同一批进展只压
- `func TestReadTailBudgetFollowsRetainedWindowRule(t *testing.T)` — TestReadTailBudgetFollowsRetainedWindowRule：读尾（冷恢复装载 transcript /
- `func compactionRecords(service *Service, sessionID string) []ContextCompaction`
- `func lastCompactionRecord(t *testing.T, service *Service, sessionID string) ContextCompaction`
- `func TestCompactContextHandlerReportsRecordedRange(t *testing.T)` — TestCompactContextHandlerReportsRecordedRange：compact_context 工具/命令的
