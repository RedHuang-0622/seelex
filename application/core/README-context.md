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

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### context_budget_last_resort_test.go

- `func TestContextBudgetOvershootKeepsNewestSettledRound(t *testing.T)` — TestContextBudgetOvershootKeepsNewestSettledRound：达峰装配时单个已定稿
- `func TestContextBudgetOvershootRefusesWhenNewestExceedsFullBudget(t *testing.T)` — TestContextBudgetOvershootRefusesWhenNewestExceedsFullBudget：最新轮本身

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

### context_controller_test.go

- `func (runtime runtimeWithContextLimits) ContextWindow() int`
- `func (runtime runtimeWithContextLimits) MaxOutputTokens() int`
- `func TestRejectToolResultsPreservesPairingWithoutPreview(t *testing.T)`
- `func TestPrepareExecutionContextCountsActiveSystemPrompt(t *testing.T)`
- `func TestPrepareExecutionContextUsesRuntimeContextLimits(t *testing.T)`
- `func TestPreparedRequestNeverExceedsSafeBudget(t *testing.T)`
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
