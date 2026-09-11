# 缓存命中率不及 Codex：根因探究

> 调研日期：2026-09-12 | 对象：本仓库（Seelex / Seele v0.1.3） vs OpenAI Codex CLI
> 方法：① 主会话装配链路静态审计（含上游 `../Seele` 源码，逐条 `file:line`）；② **本地可复现探针实测**
> （`application/core/context_cache_divergence_probe_test.go`，走生产装配路径，不发网络请求）；
> ③ Codex 侧一手证据（`openai/codex` main 分支源码 + 官方文档）。
> 性质：只读审计 + 新增 1 个测试文件；**未改任何生产代码**。

---

## 0. 结论速览（按对命中率的期望影响排序）

| # | 根因 | 证据 | 量级 |
|---|---|---|---|
| 1 | **跨轮不是「上一轮请求 + 尾部追加」，而是每轮从 durable transcript **全量重投影**，且重投影字节 ≠ 上一轮真正发出去的字节**（assistant 工具轮正文在 wire 被丢弃、在 transcript 被补回） | `../Seele/session/loop.go` callLLM（tool_calls 时 `Content: nil`）、`application/core/chat.go:792-812`（`mergeStreamedToolNarration`）、`context_runtime/history.go`（空正文修复）；探针 §4 | 跨轮首请求 **raw 46.3% / @1024 36.4%**（失效 53.7%），对齐后控制组 **80.2%** |
| 2 | **每轮收尾清空工作历史**，主动放弃「增量延长上一请求」的形状，使 1 成为必然路径 | `main.go:182 EnableWorkingHistoryRelease` → `internal/adapters/engine_port.go:632-651 ReleaseWorkingHistoryFor → engine.ClearHistory()` | 结构性（Codex 侧有测试断言「上一请求是下一请求的字节前缀」，我们结构上不可能） |
| 3 | **记忆块按当前 query 选择、却渲染在累积 context 之前**（system 之前） | `seelexctx/assembler.go:85-88`（`Memories(ctx, LastUserQuery(...))` 插在 project 后、prefixStacks 前）、`seelebridge/runtime_context.go:127-153`、`seelexctx/memory/memory.go:52-84` | CompactStack 非空后任一次选择变化 → 其后 **95.3%** 内容失效（探针实测） |
| 4 | **provider 缓存键与命中观测双缺**：DeepSeek 无 `prompt_cache_key` 等价物、OpenAI 兼容路径也未发送；**streaming 路径整体丢弃 usage** → 我们看不到真实命中，只能本地估算 | `../Seele/agent/core/api/client.go:243-290`（`sseState` 无 usage 字段、`applySSEEvents` 无 `SSEEventUsage` 分支）、`../Seele/agent/core/api/strategy_openai.go:32-36`（只解析 3 个 usage 字段）、`task_context_state.go:315` | `TokenAudit.ActualPromptTokens` 在生产路径恒 0；仓库 97.6% 来自 DeepSeek 控制台账单，非 harness 自测 |
| 5 | **工具 schema 顺序不确定**（Go map 迭代）＋工具集可变（MCP 懒加载 / 插件切换 / plan 门） | `../Seele/tools/tools.go:210-229 rebuildLocked`（`for _, provider := range r.providers`） | 工具块在消息之前；一次 registry 重建即整条前缀失效（低频但全额） |
| 6 | 多会话交错争用同一前缀缓存单元；子代理各自独立前缀（Codex 子代理**共用父** `prompt_cache_key`） | 本仓库 `docs/research/2026-09-06-dash-context-cache-hit-comparison.md` §4.2 + Codex `core/src/session/multi_agents.rs` | K=3/L=1 推演 49.6%；K=6/L=1 → 19.9%（既有推演模型） |
| 7 | （潜在）Anthropic 账号：**从不发送 `cache_control`**；且 `system` 字段被最后一条 system 消息覆盖（plan 尾部消息会顶掉主 system） | `../Seele/agent/core/api/strategy_anthropic.go:287-289`（`case "system": sys = *m.Content`）、全仓库 `cache_control` 0 命中 | 当前账号为 DeepSeek（`config/accounts.yaml:2,21-22`），未触发；一旦挂 Anthropic 账号即命中率归零 + 丢指令 |

一句话：**Codex 的命中率来自「一条会话一条缓存桶 + 字节冻结的头部 + 世界状态以差分只追加尾部」；我们的跨轮形态是「每轮重建整条历史」，重建字节还与 wire 不一致，于是每进入新用户轮就把上一轮的工具轮整段重新计费。**

---

## 1. 先对齐口径与观测面（重要）

- **我们能观测到什么**：生产走流式（`application/core/chat.go:150-165` → `chatStream` → Seele `completeStreamInternal`）。
  `../Seele/agent/core/api/client.go:240-300` 的 `sseState` 只有 `tcMap/sb/reasoningSB/isToolMode`，`applySSEEvents` 只处理
  `SSEEventToolCall / SSEEventText / SSEEventReasoning` —— **`SSEEventUsage` 帧被静默丢弃**（strategy 侧确实产出了它，
  见 `strategy_openai.go:171-181`）。
- `../Seele/session/loop.go` 的 `callLLM`：带 tool_calls 的回复**不带 Usage**；纯文本回复补一个估算值
  `msg.Usage = &types.Usage{PromptTokens: 0, CompletionTokens: len(content)/4}`。
- 于是 `application/core/task_context/task_context_state.go:315`
  （`state.TokenAudit.ActualPromptTokens = info.Usage.PromptTokens`）在生产路径上拿到的是 0。
- **`../Seele/agent/core/api/strategy_openai.go:32-36`** 的 `openaiUsage` 只解析 `prompt_tokens / completion_tokens / total_tokens`：
  **没有** `prompt_tokens_details.cached_tokens`（OpenAI）也没有 `prompt_cache_hit_tokens / prompt_cache_miss_tokens`（DeepSeek）；
  `strategy_anthropic.go:37-40` 同样只有 `input_tokens / output_tokens`，没有 `cache_read_input_tokens`。
- 结论：**harness 自己从来没有 provider 上报的缓存命中数**。仓库里 97.6% 出自
  `docs/test/REPORT-perf-latest.md` 的 DeepSeek 控制台账单（37,689,781 输入 token 中 36,776,832 命中），
  是**账单聚合口径**；`application/core/goal/techleader.go:257-263` 的 cache 统计是**相邻回合输入文本的本地 LCP 估算**，
  两者都不是同一把尺子。
- 对比 Codex：解析 `usage.input_tokens_details.cached_tokens`（`codex-api/src/sse/responses.rs:127-150`），
  且测试里直接断言「上一请求是下一请求的字节前缀」（`core/tests/suite/prompt_caching.rs`）。
  **我们连"是否命中"都测不到，也就无从优化** —— 这是第一个必须先补的洞。

---

## 2. Codex 基线（一手证据）

| 机制 | 证据 | 为什么省命中率 |
|---|---|---|
| 每次 `/responses` 都带 `prompt_cache_key`，默认 = 根会话 id；`--resume` 与**子代理共用父 key** | `core/src/client.rs`（`prompt_cache_key`）、`core/src/session/multi_agents.rs` | 一条会话一条缓存桶，不与其他会话抢；子代理复用父前缀 |
| 静态 `instructions` 常量字段 + 工具顺序确定（插入序 `IndexMap`） | `core/src/client.rs:827-832,873`；工具用有序容器序列化 | 头部字节冻结 → 整条前缀可命中 |
| 动态事实（cwd / current_date / AGENTS.md / 权限 / 模式 / 工具命名空间）一律渲染为**尾部新 item**，只在变化时追加，从不回头改写头部 | `core/src/context_manager/updates.rs:32-60`（`merge_contextual_fragments`）、`prompt_caching.rs:555-598` | 世界状态变化 = 尾部追加，零前缀失效 |
| HTTP 传输 `store:false` **全量重发 input**；WS 传 `previous_response_id` 仅在"严格扩展且各字段相等"时启用 | `core/src/client.rs`；`prompt_caching.rs` | 请求形状 = 上一请求 + 尾部，前缀天然命中 |
| 压缩：token 预算式「装一个新窗口」，摘要指令只追加在尾部；溢出裁掉**最旧**项 | `core/src/compact_token_budget.rs:19-43`；`auto_compact_window.rs:38-43` | 压缩后旧前缀仍尽量保留 |

→ 三条最关键的：**session 级缓存键**、**字节冻结的头部**、**世界状态差分只追加尾部**。

---

## 3. 我们的装配事实（生产路径）

单次请求（主会话）在 wire 上的顺序：

```text
[blocks] project → memory(按本轮 query 选) → skill 栈帧 → compact 栈帧 → 调用方 PromptBlocks
[WorkingHistory] system → 已定稿累积 context（append-only）→ plan 尾部消息(system) → 当前输入(含工作打点表)
[tail] plan/task 栈帧
tools：[API 轨道，独立字段；provider 侧渲染在 system 之后、首条消息之前]
```

证据链：`seelexctx/assembler.go:73-118`（`Assemble` 投影顺序）→ `../Seele/seelectx/assembly.go`（`DefaultRequestAssembler`）→
`../Seele/session/loop.go` `callLLM`（`WorkingHistory + Blocks + Tools`）→
`../Seele/agent/core/api/strategy_openai.go:14-30`（请求体 `{model, messages, tools, ...}`）。

跨轮时：

1. 回合收尾：`EnsureFinalAssistantTranscript` → `BackfillAssistantReasoning` → `mergeStreamedToolNarration`
   （`application/core/chat.go:175-183`，函数体 `:792-812`）→ `ReleaseWorkingHistoryFor` **清空引擎工作历史**
   （`internal/adapters/engine_port.go:641-651`）。
2. 下一轮 `PrepareExecutionContextFor`（`context_runtime/coordinator.go:136-238`）：
   `systems = RetainedSystemHistory(engineHistory)` → **空** → 从 transcript 全量重建
   （`task_context/plan_transcript.go:100-140 TranscriptTailHistory(events, budget, 0)`）。
3. `PrepareProviderHistoryFor`（`context_runtime/history.go:49-64`）再做「空正文修复 + 中断工具链配对修复」。

**关键不变量缺失**：设计文档（`docs/arch/context-prefix-chain.md`）要求「已定稿轮次 append-only、旧轮字节不变」，
这只在**同一轮内**成立（引擎循环是纯追加）；**跨用户轮**是「清空 + 重投影」，重投影与 wire 是否逐字节一致**从未被断言过**。

---

## 4. 实验：跨轮前缀发散探针（本次新增，可复现）

```bash
go test ./application/core -run ContextCacheDivergenceProbe -v -count=1
```

三组配置同一条装配路径（唯一差别是「引擎工具轮消息是否保留流式正文」与「收尾是否合并视图正文」）：

| 场景 | raw | @64 | @1024 | 首个差异位置 | 失效 token |
|---|---|---|---|---|---|
| 回合内 iter1→iter2（append-only） | 58.9% | 58.2% | 46.6% | msg#1（尾部追加） | 2709（41.1%） |
| 回合内 iter2→iter3（append-only） | 81.5% | 81.0% | 74.8% | msg#3（尾部追加） | 1518（18.5%） |
| **跨轮 A 生产实测（wire 丢正文 + 收尾合并正文）** | **46.3%** | 46.2% | **36.4%** | **msg#1（assistant 工具轮）** | **4537（53.7%）** |
| 跨轮 B 只丢不合并（空正文 → 恢复注记） | 46.4% | 46.4% | 36.5% | msg#1（assistant 工具轮） | 4514（53.6%） |
| 跨轮 C 控制组（wire 与 transcript 正文一致） | **80.2%** | 79.9% | 73.0% | msg#3 | 1668（19.8%） |

- **H1 成立**：回合内是 append-only，前缀完好（只有尾部新内容计费）。
- **H2 成立（这是主因）**：跨轮重投影在「上一轮**第一个**工具调用 assistant 消息」处就与 wire 分叉
  → 上一轮整段工具轮（工具调用 + 工具结果 + 收尾答复）在新轮首个请求里全部按 miss 计费。
- **H3 成立**：工作打点表块在最后一条 user 消息内（261 字符 / 86 tok ≈ 1.02%），只影响尾部。
- **H4 成立**：CompactStack 非空后，记忆块随 query 重选 → 从 memory 块（约 prompt 的 4.7% 处）起
  **失效 79,651/83,554 token（95.3%）**；且 memory 块在 system 之前，等于把头部一起废掉。

（探针把 system 与 tools 作为跨轮常量排除在 LCP 之外，与既有 `context_cache_smoke_test.go` 口径一致。）

---

## 5. 量化：这些失效怎么把一个长会话压在 97% 而不是 99%

既有口径模型（`2026-09-06-dash-context-cache-hit-comparison.md` §4.1）：每轮新增 b、共 N 请求时聚合率 ≈ (N−1)/(N+1)。
本报告在它之上补一条**跨轮悬崖**项：

```text
miss_total ≈ Σ_用户轮 ( 首请求失效份额 ~0.5 × 该轮 P )   ← 根因 1/2
           + Σ_请求 ( 本轮新增工具结果 + 新输入 )        ← 单轮内正常增量（根因 5/6 影响）
           + [CompactStack 非空时] 记忆块重选 → 0.95 × P  ← 根因 3（低频但致命）
```

用 `docs/test/REPORT-perf-latest.md` 的形状粗算：130+ 次工具调用、37.7M 输入、0.69M 未命中（1.8%）。
若 5 个用户轮、平均 P≈200k，则跨轮悬崖贡献 ≈ 5×0.5×200k = 500k，与轮内增量 0.65M 同量级 ——
**即：把跨轮悬崖消掉，未命中大致减半；再把每轮新增压小，才有 99% 的空间。**
（此段为量级估算，标注为 HYPOTHESIS；探针给的是确定性的相对结论。）

---

## 6. 修复建议（按性价比）

1. **P0 · 先让命中可观测**：解析 `prompt_tokens_details.cached_tokens`（OpenAI 兼容）与
   `cache_read_input_tokens / cache_creation_input_tokens`（Anthropic），补 `sseState` 的 `SSEEventUsage` 分支
   （`../Seele/agent/core/api/client.go:255`）、落进 `TokenAudit.ActualPromptTokens`；流式请求补 `stream_options.include_usage`。
   没有这一步，后续任何优化都无法验收。
2. **P0 · 让 wire 与 durable 投影逐字节一致**（消掉根因 1）：在 `../Seele/session/loop.go` 的 tool_calls 分支保留流式正文
   （`Content: &content`），或让 transcript 的 provider 投影对工具轮 assistant 使用「wire 当时的正文」而不是事后合并的视图正文；
   然后加一条**不变量测试**：`Turn(n+1).Request[0].Bytes` 必须以 `Turn(n).Request[last].Bytes` 为前缀（探针 C 组即验收基线：
   46.3% → 80.2%）。
3. **P0 · 跨轮不重建**（消掉根因 2）：取消 `ReleaseWorkingHistory`（或改成「保留段复用 + 只追加新事件」，
   即 `context_runtime/coordinator.go:196-210` 的 `covered` 快路径真正生效），把内存压力交给会话 LRU 而不是每轮清空。
4. **P1 · 记忆块移出前缀**（消掉根因 3）：`relatedMemoryBlocks` 要么会话级装载一次，要么渲染到累积 context **之后**
   （对照 DSH 的 append-only 动态快照）；同时按 `SegmentID` 稳定排序，避免同集合不同序造成字节变化。
5. **P1 · 工具序确定化**（消掉根因 5）：`rebuildLocked` 后按 name 排序再发布 `Definitions`
   （`../Seele/tools/tools.go:229`）；并把工具集变化（MCP 懒加载 / 插件切换）视为「显式前缀失效事件」，在 UI/日志里标注。
6. **P2 · 会话调度**：同项目同 system 的会话共享公共前缀，尽量按会话聚合连续请求；子代理考虑沿用父会话的缓存键思路（DeepSeek 无 key，只能靠前缀共享）。
7. **P2 · Anthropic 账号前置修复**：补 `cache_control` 断点；并修 `strategy_anthropic.go:287-289` 的 `sys` 覆盖语义
   （plan 尾部是 role=system，会顶掉主 system）。

---

## 7. 未验证 / 未覆盖

- 未做真实 provider 抓包（本次全部为本地字节级复现），**provider 侧严格按字节前缀匹配与 TTL/逐出策略是外部公开语义**，
  仓库内不可验证；跨轮悬崖对账单的实际占比是量级估算（HYPOTHESIS）。
- 未覆盖子代理/节点会话的装配（`seelebridge/node`）与 `fork_subagents`；未覆盖 GUI 多会话热切换的真实交错时序。
- 未覆盖 `seelexctx/controller.go` 压缩路径的前缀失效幅度（压缩前后各一次全额失效，低频，既有文档已标注为可接受代价）。
- 探针以**镜像 Seele 循环**的方式复刻 wire 历史（真实装配函数 + 真实 transcript 投影），
  未跑真实 `ReActLoop`；H2 的机制另由源码独立确认（`loop.go` 丢弃 content、`chat.go` 事后合并），两者一致。

## 8. 证据索引

| 结论 | 位置 |
|---|---|
| 工具轮 wire 正文被丢弃 | `../Seele/session/loop.go` `callLLM`（streaming + tool_calls 分支 `Content: nil`） |
| 事后把视图正文合并进 transcript | `application/core/chat.go:175-183, 792-812` |
| 空正文 → provider 恢复注记 | `application/core/context_runtime/history.go:190-215`（`RepairEmptyHistoryContent`）；配对修复 `:107-160`（`RepairInterruptedToolChains`） |
| 每轮清空工作历史 | `main.go:182`；`internal/adapters/engine_port.go:632-651` |
| 跨轮全量重投影 | `application/core/context_runtime/coordinator.go:136-238`；`task_context/plan_transcript.go:100-140` |
| 记忆块位置与 query 依赖 | `seelexctx/assembler.go:85-88`；`seelebridge/runtime_context.go:127-153`；`seelexctx/memory/memory.go:52-84` |
| 装配总序 | `seelexctx/assembler.go:73-118`；`../Seele/seelectx/assembly.go` |
| streaming 丢弃 usage | `../Seele/agent/core/api/client.go:240-300`（`sseState`/`applySSEEvents`）；`strategy_openai.go:171-181` |
| usage 字段贫化 | `../Seele/agent/core/api/strategy_openai.go:32-36`；`strategy_anthropic.go:37-40` |
| 工具顺序非确定 | `../Seele/tools/tools.go:210-229` |
| Anthropic system 覆盖 | `../Seele/agent/core/api/strategy_anthropic.go:287-289` |
| Codex 基线 | `openai/codex@main`：`core/src/client.rs`、`core/src/context_manager/updates.rs:32-60`、`core/tests/suite/prompt_caching.rs`、`codex-api/src/sse/responses.rs:127-150` |
| 实测数据 | `go test ./application/core -run ContextCacheDivergenceProbe -v -count=1`（本文件 §4 表） |
| 账单口径旧数据 | `docs/test/REPORT-perf-latest.md`；`docs/research/2026-09-06-dash-context-cache-hit-comparison.md` |
