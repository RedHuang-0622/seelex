# assembly/context（根包分卷）

## 生态位

上下文控制相关集成测试

覆盖：`context*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## Plan 与工具结果预算归属（A5.4）

历史 Plan 的当前/后台选择与 Snapshot 基线由
`application/execution/taskcontext.PlanProjectionApplication` 持有；工具结果
visible/ref/truncated/total 元组由
`application/session/viewstate.ToolResultBudgetApplication` 持有。
`assembly/plan_tools.go` 已删除，snapshot budget 文件只保留依赖适配。

## 跨轮前缀与模型输出边界（2026-10-21）

**不变量**：每条 provider 请求的字节都以本会话更早发出的某条请求为前缀；工具轮
还要求下一请求完整复现上一请求之后实际产生的 assistant output。后一条对应
DeepSeek 的模型输出缓存单元：provider 会在 user input 结束和 model output 结束
分别落 cache prefix unit，二者都必须完整匹配。

- **携带工具调用的 assistant 消息保留模型实际生成的正文**。Seele 的流式工具轮
  同时可能产生 `content`、`reasoning_content` 与 `tool_calls`；三者都属于模型
  output，写入引擎历史并由 `RepairEmptyHistoryContent`、`DurableHistory.Load`
  原样投影。旧实现把 `content` 置 nil，只能命中 user-input 边界，无法命中更大的
  model-output 单元。
- **空正文仍保持空**。provider 没有生成文本时不得补占位，否则重投影会写入从未
  存在的字节；旧恢复占位只用于识别历史记录，不进入新请求。
- 同族规则（同一条"不写 wire 上从未存在的字节"）：**`tool` 角色空结果保持为空**。
  工具返回空串时 wire 上就是空正文，补 `MissingHistoryContent` 会让下一轮重投影从
  `""` 变成占位（同族分叉）；中断工具链的协议占位由 `RepairInterruptedToolChains`
  生成，不受影响。守卫用例：
  `context_prefix_invariant_test.go::TestContextPrefixInvariant_EmptyToolResult`。
- 同族第二处（2026-09-12 第二轮）：**工具结果的 provider 投影取 wire 原文，应用呈现
  文本只属于视图**。工具失败时 wire 上是框架合成的 `{"error": %q}`；超限时 wire 上是
  处理器归档引用（`result_ref=result:<callID>`），而视图/轨迹读的是应用呈现文本
  （`presentToolError` / `tr-<digest>` 归档引用）。记录侧为此增 provider-only 字段
  `provider_content`（`contract/protocol/context.go` 的 `TranscriptEvent`、
  `store/sessionstore.go` 的 `Event`），三个 provider 投影出口统一取它：
  `store/durable_history.go` 的 `eventsToMessages`、`store/wire_assembler.go`
  的会话 wire 装配、`kernel/task/plan_transcript.go` 的
  `transcriptEventMessage`。守卫用例：
  `prefix_invariant_fullchain_test.go::TestFullChainPrefixInvariantToolErrorAcrossTurns`
  与 `...OversizedToolResultAcrossTurns`（比对生产路径录到的真实 wire 字节）、
  `store/provider_content_test.go`（落盘 + 冷载 + 重启恢复）。
- 工具轮说明正文**在工具钩子边界按迭代归位**到本次迭代的 assistant(tool_calls) 事件
  （`task_context.AttributeToolNarrationLocked`；回合收尾不再做事后填充——旧做法会
  把叙述写到别的轮次上）。引擎历史、durable 记录、视图与重启恢复使用同一份正文；
  `store.DurableHistory.Load` 不再对工具轮 assistant 做二次归零。守卫用例：
  `TestToolNarrationStaysWithOwningIteration`、
  `TestConcurrentSessionsKeepOwnContent`、`TestFullChainPrefixInvariantAcrossTurns`。
- 红灯用例：`context_prefix_invariant_test.go`（沿生产装配路径断言「每条请求是
  上一条的前缀」，并让工具轮携带非空模型正文）；Seele 侧另有
  `TestEngine_ChatStream_ToolCallReplaysModelOutputContent` 断言下一请求完整复现
  `content + reasoning_content + tool_calls`。

**历史说明**：2026-09-12 的归零修复解决的是当时“durable 事后补写、wire 从未收到”
造成的重投影分叉。2026-10-21 起，Seele 改为在生成工具轮时直接保留模型输出正文，
因此 Seelex 同步取消归零和 `ProviderWireMessages` 二次投影。旧研究文档中的归零
数据是历史基线，不再描述当前实现。

## 冷加载会话的 `/compact`：当场压缩，不再只登记（2026-09-24）

**触发**：用户在刚冷加载的会话里执行 `/compact`，拿到的是「当前会话没有进行中的
执行纪元（例如刚冷加载或刚清空），现在没有可压缩的请求上下文；已登记：下一条消息
组装上下文前立即压缩」。他接着想问「我需要你的摘要内容」——而命令早已结束，登记的
兑现要等下一条消息，用户看到的是「按了没反应」。

**根因**：显式压缩的入口判据是「该会话有匹配当前 request 的执行纪元」
（`CompactContextNow` 在 `state == nil || state.RequestID == ""` 时只登记）。冷加载
会话的 transcript 与引擎历史**都已装载**，缺的只是"一个在飞回合的 RequestID"——
把"没有在飞回合"读成了"没有可压缩的上下文"。2026-09-23 那轮拒绝"伪造纪元"是对的
（伪造会把"有人在跑这个会话"写进可见面），但落成了"只登记"，等于把用户明确要求的
压缩推迟到他自己再发一条消息。

**改法**：新增**会话级维护身份**（`kernel/task/session_context_maintenance.go`）：
`BeginSessionContextMaintenanceLocked` 给这类会话开一个带前缀的维护 `RequestID`
（`session-maintenance:<sessionID>`），没有任务状态时按会话自己的事实（最后一条真实
用户输入作 objective）建一份上下文状态，状态标记 `idle`；`CompactContextNow` 用它跑
同一条显式压缩路径（判据量照算、记录照落、帧正文照出、引擎历史照换、按会话落盘），
结束后 `EndSessionContextMaintenanceLocked` 撤销身份并保留压缩产生的
`ContextVersion`/`ContextCompactions`/checkpoint。三条诚实约束：不写
`ChatState.Running`、不设快照 `Chat.RequestID`、不建任务注册表条目；已有在飞回合时
**拒绝**发放身份（退回纪元路径），压缩失败也**必须**撤销身份。空会话（transcript 与
引擎历史都没有对话消息）维持登记语义——压缩空上下文只会产出一条区间为空的记录。

**回执**：`explicit_after_turn` 来源不变，但结果面新增 `NoEpoch`（工具 JSON 字段
`no_epoch`，omitempty），命令与工具共用的 `compactionRecordNote` 因此能说出
「会话没有在飞回合（冷加载或刚清空），已按会话级显式压缩立即执行（压缩已装载的
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

- 宿主不再需要判断"我在不在环内"，也不再需要把 ctx 透传进压缩：`context_runtime` 的
  `loopHistoryChannel` 与 `contract.InLoopEngine` 整条删除，压缩统一走 `sessionHistory` /
  `replaceSessionHistory`（按会话路由到 `HistoryFor` / `ReplaceHistoryFor`）。
- 回合内压缩**仍然当场生效**，落点从"锁内直写"变成"下一个检查点"：`compact_context` 在工具
  handler 里提交替换，循环紧接着（tool 结果 append **之前**）排空它，因此同回合的下一次模型请求
  读到的已是压缩后的历史——对压缩正是期望语义（它本就要在下一次请求才生效）。
- 忙会话的**锁外**压缩也不再"登记待安装 + 等回合收尾换引擎"：`EnginePort.replaceRawHistoryFor`
  把替换直接交给引擎排队（`queueSessionHistory`），登记表只在引擎不具备该能力时兜底。压缩因此对
  下一次请求生效，而不是等下一次装载。
- 下界不变：压缩产物必须保留「assistant 已带 tool_calls、其结果尚未 append」的在飞尾部
  （`withInFlightTail`，现在对**所有**路径都做），否则引擎以 `ErrInFlightToolCallDropped` 拒收，
  紧随其后 append 的结果行成孤儿。多保留的是本轮自己的消息，**不参与压缩判据**。
- 观测面读历史（`/history`、工作区切换、子代理落账）不再需要"循环发布的检查点快照"：
  `History()` 任何时刻都返回当前工作历史，且不会因长流式阻塞。
- 同会话压缩门（`application/conversation/context/compaction_gate.go`）仍在：并发压缩会各自读同一段历史、各自替换，后写的
  那份把前一轮整个丢掉。区别是现在**所有**调用方都走阻塞领轮——没有谁持着"对方要用的锁"，等待只
  会排队。

**有牙证明**（`internal/host/engine_port_history_test.go`，真实 Session）：
`TestReplaceHistoryInsideTurnTakesEffectInSameTurn`（同回合下一次请求已带压缩帧；终态历史 =
帧 + 在飞 assistant + tool 结果 + 收尾 assistant）、`TestReplaceHistoryDropsInFlightTailIsRefused`
（丢在飞尾部被拒且不动历史）、`TestReplaceHistoryOutsideTurnAppliesImmediately`（锁外立即落地）；
`engine_port_reentrance_test.go` 的 `TestEngineHistoryFromToolHandlerReturnsPromptly` 是旧自锁
断言的反转报警器；`application/assembly/context_compact_gate_test.go` 钉同会话压缩门的等待与收口。

**边界**：① 忙会话上的写回是"下一个检查点生效"而不是"立即"，期间的其它写入按提交顺序一起排空；
② `/compact` 与自动压缩共用同一条装配路径，因此两者的历史出入口完全一致（不再分环内环外）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### context_compact.go

- `func compactionRangeLabel(result ContextCompactionResult) string`
- `func compactionRecordNote(result ContextCompactionResult) string`
