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

## 回合内即时压缩：已持锁的环内通道（2026-09-26）

**触发事实**：`Session.ChatStream` 从进函数持 `Session.mu` 到出函数，工具派发与全部循环回调都在
同一 goroutine、同一把锁内（`Seele session/chat.go:273-278`、`loop.go:358`）。`compact_context`
交给模型在回合内调用，其落点 `CompactContextNow → prepareExecutionContextFor` 第一件事就是读
引擎历史，走 `EnginePort.HistoryFor → RawHistoryFor`（`port.mu.RLock`）`→ engine.History()`
（`Session.mu`）——同 goroutine 抢自己已持有的非重入锁 = 永久自锁，而且此时 `port.mu` 的读锁还
攥在手里，之后任何要 `port.mu.Lock` 的动作（含别的会话开新回合）全部排队。反向探针
`TestEngineHistoryFromToolHandlerSelfBlocks` 实测过这条。

**改法**：把折叠的**历史出入口**从锁外挪到唯一的合法持锁点——引擎在 `Chat`/`ChatStream` 取锁后
注入本轮 ctx 的 `session.InLoop` 把手（Seele `session/inloop.go`）。三段锁征用对照：

```
回合内·改前   EnginePort,ChatStreamFor <-get- EnginePort.mu … -return-（182→189）
             SeeleSession,ChatStream   <-get- SeeleSession.mu（全程）
             Coordinator,engineHistory  <-get- EnginePort.mu.RLock → <-get- SeeleSession.mu ⛔自锁
回合内·改后   SeeleSession,ChatStream   <-get- SeeleSession.mu（全程，唯一一次）
             Coordinator,foldHistory    读把手缓存，不取任何锁
             Coordinator,replaceFold…  <-get- Controller/Store 细粒度锁（微秒级）；不碰 EnginePort.mu
锁外（回合之间/冷加载） 与改前完全一致：`inLoop == nil` → 原路 `engineHistory/replaceEngineHistory`
```

- 通道是**按调用存在的值**（`context_runtime/inloop_history.go` 的 `loopHistoryChannel`，挂在
  `prepareOptions.inLoop` 与显式入参上），不进任何长期对象；`InLoopChannelFrom(engine, ctx)`
  拿不到把手就返回 nil，调用方回落——不用调用计数/时间戳去猜"引擎在不在跑"。
- 端口语义：`(false, nil)`＝不在环内，必须回落；`(true, err)`＝在环内被引擎拒绝，**不得**回落
  （回落等于再去拿一次本回合已持有的锁）。见 `contract.InLoopEngine`。
- 环内替换刻意**不 arm** `PrepareMainSessionHistory`（"交给下一次装载"那条），本轮收尾由循环自己
  `saveToCache → DurableHistory.Save` 落盘；所以折叠帧进的是**这一次**的 provider 历史，不需要
  等下一条消息，也不需要下一次装配兑现。
- 下界：环内调用点必然落在「assistant 已带 tool_calls、其结果尚未 append」的时刻，所以折叠产物
  必须保留这段在飞尾部（`withInFlightTail`），否则紧随其后 append 的结果行成孤儿；引擎侧也会以
  `ErrInLoopInFlightDropped` 拒收丢尾部的替换。多保留的是本轮自己的消息，**不参与压缩判据**。

**策略零改动**：阈值、软硬线、窗口 N、帧构造、记录门槛、门禁顺序一行未动——通道只换"历史字节从
哪来、回到哪去"。对拍：`TestCompactContextInLoopAndOutOfLoopAgree` 用同一引擎类型 + 两种 ctx，
断言判据量（compared/assembled/soft/hard）、被压区间（事件号与消息号两端）、`reason`/`origin` 与
写回的 provider 历史逐字段相等，并要求通道计数一边非零、一边为零（否则"一致"只是两次走了同一条
路）。

**有牙证明**：`internal/adapters/engine_port_inloop_test.go` 三条——即时生效（同回合下一次请求
已带折叠帧、终态历史是 帧+在飞 assistant+tool 结果+收尾 assistant）、丢在飞尾部被拒且不动历史、
环外一律回报"未处理"；与反向探针同批 PASS。Seele 侧 `session/inloop_test.go` 钉把手可见性、
世代守卫（回合结束后旧 ctx 取不到）与"被拒不改动历史"。

**边界**：① 把手**不校验 goroutine**（Go 无可靠 goroutine 身份）——世代守卫能挡住"上一回合泄漏
的 ctx"，挡不住"本轮进行中被别的 goroutine 拿走的那个 ctx"，因此只允许在同一调用栈内（工具
handler / 循环回调）使用；② 环内折叠的压缩帧仍要跑一次模型调用，这段时间 `Session.mu` 被多持有
数秒，等它的是同会话的 `ClearHistory/Reset/AppendHistory`（观测面走 `HistoryIfAvailable()` 的
`TryLock`，不阻塞），其他会话零影响；③ 锁外那条路径已关掉：`port.mu` 的任一次持有都不再跨越可能
阻塞的 `Session.mu` 等待——目标会话有回合在飞时折叠只登记待安装（按会话键控，那次回合收尾时兑现），
历史读也挪到 `port.mu` 之外；判据与不变量见 `internal/adapters/README.md`「并发与锁纪律
（EnginePort）」（台账 S3b）。锁外折叠的语义是"该会话这次回合收尾时装上"，不是"交给下一次装载"。

**发布收尾**（依赖，不在本仓库内）：Seele 侧改动需打 tag → `go.mod` 去掉临时 `replace` →
`GOWORK=off go mod vendor`。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### context_budget_last_resort_test.go

- `func TestContextBudgetOvershootKeepsNewestSettledRound(t *testing.T)` — TestContextBudgetOvershootKeepsNewestSettledRound：达峰装配时单个已定稿
- `func TestContextBudgetProactivelyCompactsAtHardThreshold(t *testing.T)` — TestContextBudgetProactivelyCompactsAtHardThreshold：装配结果落在硬阈值
- `func TestContextBudgetOvershootCompactsWhenNewestExceedsFullBudget(t *testing.T)` — TestContextBudgetOvershootCompactsWhenNewestExceedsFullBudget：最新轮自身

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

### context_compact_gate.go

- `func (service *Service) isCompactingLocked(sessionID string) bool` — isCompactingLocked 报告该会话是否有一轮上下文压缩正在进行（调用方持有
- `func (service *Service) signalCompactionLocked()` — signalCompactionLocked 广播一次「compacting 集合已变化」（调用方持有
- `func (service *Service) compactionSignalLocked() <-chan struct` — compactionSignalLocked 返回当前收口信号（调用方持有 Core.ViewMu）。惰性
- `func (service *Service) acquireCompactionRound(ctx context.Context, sessionID string) error` — acquireCompactionRound 领取该会话的压缩轮：已有轮在跑时先等它收口（同会话
- `func (service *Service) releaseCompactionRound(sessionID string)` — releaseCompactionRound 收口该会话的压缩轮并唤醒等待方。幂等：没领过轮的
- `func (service *Service) awaitCompactionRound(ctx context.Context, sessionID string) error` — awaitCompactionRound 等到该会话没有压缩轮在跑（ctx 取消即返回错误）。
- `func (service *Service) deferSubmitUntilCompacted(ctx context.Context, sessionID, text string)` — deferSubmitUntilCompacted 把一次对话提交挂到该会话压缩轮的收口点：门开着时

### context_compact_gate_test.go

- `func engineChatInputs(engine *fakeEngine) []string`
- `func waitForChatInputs(t *testing.T, engine *fakeEngine, want int) []string` — waitForChatInputs 轮询到引擎收到的请求数达到 want，返回这些请求。
- `func TestSubmitParksWhileCompactionRoundOpen(t *testing.T)` — TestSubmitParksWhileCompactionRoundOpen 是这条不变量的正面：压缩轮进行中，
- `func TestCompactionGateIsScopedToItsSession(t *testing.T)` — TestCompactionGateIsScopedToItsSession 钉住"同会话串行"里的另一半：门按会话
- `func TestCompactionRoundIsAcquiredExclusively(t *testing.T)` — TestCompactionRoundIsAcquiredExclusively 是"串行"那一半：上一轮没收口时，下一

### context_compact_inloop_test.go

- `func inLoopCompactContext() context.Context`
- `func (engine *inLoopFakeEngine) marked(ctx context.Context) bool`
- `func (engine *inLoopFakeEngine) HistoryInLoop(ctx context.Context) ([]EngineMessage, bool)`
- `func (engine *inLoopFakeEngine) ReplaceHistoryInLoop(ctx context.Context, history []EngineMessage) (bool, error)`
- `func (engine *inLoopFakeEngine) SetSystemPromptInLoop(ctx context.Context, prompt string) (bool, error)`
- `func (engine *inLoopFakeEngine) counters() (int, int, int)`
- `func newInLoopCompactService(t *testing.T, requestID string) (*Service, *inLoopFakeEngine, string)` — newInLoopCompactService 复刻 compactTestService 的纪元装配，但引擎换成能同时
- `func seedLongRounds(t *testing.T, service *Service, requestID string)` — seedLongRounds 与 TestCompactContextHandlerFoldsTranscript 同一份量：4 轮、每轮
- `func TestCompactContextInLoopAndOutOfLoopAgree(t *testing.T)`

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
