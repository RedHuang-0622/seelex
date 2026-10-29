# 主流 coding agent 的前缀缓存机制对照与取舍

> 调研日期：2026-10-29
> 目的：为 [`../arch/context-prefix-stability-l1.md`](../arch/context-prefix-stability-l1.md)（L1 / draft / actor + memory 纪元表）提供外部对照，回答两个问题：**① 别人怎么保住前缀；② 我们的选择差在哪、换到了什么。**
> 范围：只谈**缓存机制轴**（前缀身份、请求形状、动态状态位置、工具集、压缩、观测）。产品能力面（权限、编排、UI）不在本文，见 [`2026-10-28-seelex-vs-peer-harnesses.md`](2026-10-28-seelex-vs-peer-harnesses.md) 与 [`coding-agent-harness-comparison.md`](coding-agent-harness-comparison.md)。
> 证据等级：**[源码]** = 一手代码/测试；**[官文]** = 厂商官方文档或工程博客；**[复述]** = 第三方审计/网关实现/社区工具（用于佐证现象，不作为唯一依据）。
> 性质：只读调研，未改任何代码。

---

## 0. 结论速览

1. **所有跑得久的 harness 都在做同一件事**：把请求变成"上一请求 + 尾部"，并且**只追加、不改写**。区别只在"谁来保证这件事"——Codex 靠 `prompt_cache_key` + 字节冻结的 instructions/tools，Claude Code 靠固定四层排序 + `<system-reminder>`，DSH 靠 append-only 消息 + 请求头快照，OpenHands 靠 event log + Condensation 事件。
2. **没有一家做"每轮从落盘记录重新拼一遍请求字节"**。Seelex 目前是这一族里的特例，这也是命中率差距的结构性来源（见 §4）。
3. **动态状态的位置是分水岭**：Claude Code / Codex / DSH 都把它放进**消息**（尾部），Cline 把它放进**每轮的 environment_details**（导致前缀永不稳定），Aider 放进**固定位置的伪对话**（稳定但会累积）。Seelex 目前把 plan 帧插在**历史中间**——这是三族里最坏的一档。
4. **观测是分水岭之二**：Claude Code 对缓存命中率配了告警、低了就开 SEV；Seelex 连 `cached_tokens` 都没解析（流式 usage 整帧丢弃），优化无法验收。
5. **我们的相对优势**：压缩时复用热前缀（`PrefixReplaySummarizer`）、system 已常量前缀化、skill 正文走 append-only 事件、单一事实源纪律已有先例。这些让我们做 L1 的**下限高于多数同行**，问题只是没做。

---

## 1. OpenAI Codex CLI

| 机制 | 事实 | 等级 |
|---|---|---|
| 缓存身份 | `prompt_cache_key` = 会话 `thread_id`，一次会话内不变；**压缩后仍复用同一个 key**（测试钉住）；子代理通过 `X-Codex-Parent-Thread-Id` **共享父 key** | [源码] `codex-rs/core/src/client.rs`、`tests/suite/prompt_caching.rs`、`tests/suite/compact_remote.rs` |
| HTTP 请求形状 | `store: false`、**不用** `previous_response_id`、每轮**全量重发** `input`。但请求体本身是 append-only：`instructions` 与 `tools` 固定，`input` 逐轮严格延长 | [源码] + [官文] OpenAI "Unrolling the Codex agent loop" |
| WS 请求形状 | 传 `previous_response_id` + **增量 delta**，仅在"非 input 字段全等 且 input 是严格扩展"时启用，否则整体回退全量重发 | [源码] `ResponseCreateWsRequest`；[复述] llxprt-code#3134 引用了该结构 |
| 动态状态位置 | `environment_context` 是**一条 user 消息**（cwd / shell / 日期 / 时区 / workspace roots），不写进 `instructions` | [复述] Codex CLI 0.154.0 抓包分析（2026-09-17） |
| 工具集 | 有序容器序列化，顺序确定 | [源码]（仓库内审计见 `2026-09-12` 篇） |
| 压缩 | token 预算式"装一个新窗口"，摘要指令只追加在尾部；压缩后沿用同一 cache key | [源码] `compact_token_budget.rs` |
| 附带优化 | 会话启动即**预热 WS 连接**，用户第一条消息到达时路由已就绪 | [源码] `session_startup_prewarm.rs` |

**要点**：Codex 的命中率不来自"少发"，来自"**重发的字节永远与上次一致**"。它甚至容忍"整轮重发 8k token"，因为那 8k 是命中价。

---

## 2. Claude Code

官方工程博客《Lessons from building Claude Code: Prompt caching is everything》把纪律写成了条款，几乎是本设计的规范来源。

| 机制 | 事实 | 等级 |
|---|---|---|
| 固定四层排序 | **静态 system + tools（全局缓存）→ CLAUDE.md（项目级）→ 会话上下文（会话级）→ 对话消息**；越静态越靠前，以最大化跨会话共享 | [官文] |
| 曾踩过的坑 | ① 把**带时分秒的时间戳**写进静态 system；② **非确定性打乱工具定义顺序**；③ 改工具参数（如 Agent 工具能调哪些代理） | [官文] |
| 动态更新用消息 | 信息过期时**不改提示词**，而是在下一条 user 消息或工具结果里塞 `<system-reminder>` 标签，保住前缀 | [官文] |
| 工具集纪律 | **会话中途绝不增删工具**——工具属于缓存前缀，增删一次整条会话缓存作废。Plan Mode 因此做成 `EnterPlanMode`/`ExitPlanMode` **工具**，而不是切换工具集；MCP 工具用 `defer_loading` 只发轻量 stub 而不移除 | [官文] |
| 模型/effort | 每个模型、多数模型的每个 effort 档各自一份缓存；**中途换模型比换小模型回答更贵**（要重建缓存）。要换就走子代理 | [官文] |
| 压缩 | `/compact` 后缓存按层失效：对话层变化不影响 system 与项目上下文；**system 变化则全部失效** | [官文] 文档 |
| 观测 | 每个响应回报 `cache_read_input_tokens` / `cache_creation_input_tokens`；**对命中率配告警，命中率过低会开 SEV** | [官文] |
| TTL | 断点默认 5 分钟，可配 1 小时（`promptCacheTtl` / `CLAUDE_CODE_PROMPT_CACHE_TTL`）；缓存按"机器 + 目录"限域 | [官文] 文档 |

**要点**：Claude Code 把"缓存友好"当成**产品设计约束**（Plan Mode 的设计是被缓存纪律反向决定的），而不是事后优化。这是治理层面的差距。

---

## 3. Aider

| 机制 | 事实 | 等级 |
|---|---|---|
| 固定装配顺序 | `ChatChunks.all_messages()` 恒定：`system → examples → readonly_files → repo → done → chat_files → cur → reminder` | [源码] `aider/coders/chat_chunks.py` |
| 伪对话承载非对话内容 | repo map、只读文件、可编辑文件各自被包成**一对 user/assistant 消息**（assistant 侧是固定应答如 "Ok, I won't try and edit those files without asking first."），位置固定、字节稳定 | [源码] `base_coder.py:750-761` |
| 显式缓存断点 | 模型配置里 `cache_control: true`；`--cache-prompts` 用于缓存 system prompt、repo map 与只读文件 | [源码] `resources/model-settings.yml`；[官文] HISTORY |
| 曾经的同类缺陷 | repo-map 的**内存缓存 key 里混进了"本轮提到的文件名/标识符"**，导致每次请求都全量重扫（Linux 内核级仓库约 20 分钟/次）。修法是把 mentions 从 key 里移除，只留"文件集合 + token 预算" | [源码] PR #5556 |

**要点**：Aider 这条 PR 与本仓库"memory 块按当前 query 选择"是**同一个错误的另一种形态**——把"本轮的东西"放进了本应只由"世界状态"决定的缓存键。这是我们 I4 的外部佐证。

---

## 4. Cline（反面教材，但数据很有用）

| 机制 | 事实 | 等级 |
|---|---|---|
| 每轮动态元数据 | `environment_details` 每轮附带**当前时间（到秒）、打开的编辑器标签、可见文件、上下文用量计数**；这些字段不跨请求重复 | [复述] 多方分析一致；生态里出现专门的"把易变行搬到 system 末尾"的网关（cache aligner）与"钉住时间戳"的 skill，正是为了绕开这个设计 |
| 实测口径 | Cline 自家负载测试的平均请求：**fresh ≈13K / cached ≈71K / output ≈1K ⇒ 缓存占比 ≈83.5%** | [官文] Cline 自托管博客 |
| 推论 | 把"每轮必变"的字段放进前缀，等价于**主动放弃跨请求缓存**；而它同时也是"两个工程师、同一个仓库、同样 git 状态也拼不出相同前缀"的原因 | [复述] |

**要点**：Cline 证明了一件事——**只要前缀里有一个逐轮变化的字段，前面再多的静态内容都白搭**。这条正好命中我们 `composeRequestTail` 的现状：动态块本身没错，错在它没被当作"已发出字节"落进记录。

---

## 5. Gemini（API 层）

| 机制 | 事实 | 等级 |
|---|---|---|
| 两种缓存 | **隐式**（自动，最小 1024 token / Pro 4096）与**显式**（`cachedContents.create()` 返回命名缓存对象，需在请求里引用） | [官文] |
| 计费 | 缓存输入约为标准输入价的 0.25x；显式缓存另有**按小时的存储费**（缓存不用也在烧钱）；2.5+ 模型显式缓存折扣 90% | [官文] |
| 观测 | `usage_metadata.cachedContentTokenCount` | [官文] |
| 取舍 | 显式缓存适合"大静态上下文 + >5 分钟间隔 + 要可预测命中率"；隐式适合"连续对话" | [官文] |

**要点**：Gemini 把"缓存"做成了**显式资源**（有 TTL、有存储费、要管理生命周期）。这提示我们 L1 的运维面必须有一等公民的处置：上限、LRU、逐出打点，而不是"能缓存就缓存"。

---

## 6. OpenHands

| 机制 | 事实 | 等级 |
|---|---|---|
| 事实源 | 不可变 **event log**（append-only），同时是"记忆"与集成点；送给 LLM 的 `View` 每次由 `View.from_events()` **派生** | [官文] SDK Events 文档 |
| 压缩 = 一条事件 | `Condensation` 事件带 `forgotten_event_ids` + `summary` + `summary_offset`；`View` 过滤被遗忘事件并在指定位置插入摘要 | [官文] |
| 触发与窗口 | `RollingCondenser` 保首 `keep_first`（默认 4）+ 尾若干，中间交给 `LLMSummarizingCondenser` 摘要；阈值默认 120 事件 | [官文] |
| 关键性质 | **压缩不修改历史，只追加一条"遗忘声明"**；因此视图可随时重算且结果确定 | [官文] |

**要点**：OpenHands 的"遗忘是一条事件"与我们 §5 的 `memory_update` 表、以及 `ContextRetainedFrom` 指针是同一族思路——**把"改写"降维成"追加一条元数据"**。这是本设计最值得借鉴的一条工程范式。

---

## 7. DeepSeek Harness / DASH（仓库内既有调研）

摘要自 [`2026-09-06-dash-context-cache-hit-comparison.md`](2026-09-06-dash-context-cache-hit-comparison.md)（避免重复）：

- 会话日志是唯一事实源，模型历史由 `deriveMessages()` 派生；每次请求的 `system + tools + 前缀 + 配置` 快照记入 `request/header`（reason 分 `initial|resume|change|series`），**前缀可精确重建、可审计**。
- 动态事实（时间、AGENTS.md、会话引用、技能目录、激活技能正文）**一律 append-only 的 user 消息**；技能目录整表变化也是"追加一条完整替换目录"，不回头改头部。
- 压缩：`thresholdRatio=0.8`、`retainRatio=0.16`，替换最老区间；**摘要请求用与上次路由请求逐字节相同的 system/tools/消息重放**，只额外付尾部指令与摘要输出。
- 压缩前先跑确定性 `tool-result-pruner`（头/中/尾裁剪），不需要摘要就不发模型调用。
- 每个包 README 都声明 `KV Cache 影响`——纪律写在组织结构里。

---

## 8. 机制轴对照表

| 轴 | Codex CLI | Claude Code | Aider | Cline | OpenHands | DSH | **Seelex 现状** | **本设计目标** |
|---|---|---|---|---|---|---|---|---|
| 前缀身份 | `prompt_cache_key`=thread_id，压缩/子代理沿用 | 4 层 + 每层 `cache_control` 断点 | 模型配置 `cache_control` + `--cache-prompts` | 无键（每轮字段变） | 事件日志派生 | `request/header` 快照 | **无键**（DeepSeek 自动） | `prefix_key` 六格（§4） |
| 请求形状 | HTTP 全量重发但严格 append-only；WS 增量 | 全量重发，前缀严格延长 | 全量重发，块序固定 | 全量重发，前缀不稳定 | 由 log 派生 | 由 log 派生 | **每轮从记录重建** | 复用 L1 + 尾部拼接 |
| 动态状态 | `environment_context` **消息** | `<system-reminder>` **消息** | 固定位置伪对话 | **每轮 environment_details** | 事件 | append-only **消息** | **plan 帧插在历史中间**；env/worktable 拼在 user 但**不落记录** | draft 追加 + 逐字节提升 |
| 工具集 | 固定、有序 | **绝不中途增删**；Plan Mode 做成工具；`defer_loading` | 固定 | 可变 | 工具注册 | 固定顺序 | **切插件/懒加载/goal 门都会变；顺序随 map 迭代** | 排序 + `tools_digest` + 显式失效 |
| 压缩 | 预算窗口；沿用 key | 分层失效；`/compact` | 历史摘要 | 截断/摘要 | `Condensation` **事件** | 0.8/0.16 + **摘要复用热前缀** | 指针 + 帧 + **已复用热前缀** | 保持；补 memory 纪元 |
| 观测 | usage | `cache_read/creation` + **告警/SEV** | usage | usage + 自有负载统计 | usage | 账单 | **不解析 cached tokens** | M0 补齐 |
| 归因能力 | 测试断言"前一条是后一条前缀" | 分层表 + 失效原因清单 | PR 级修复可追溯 | 靠网关旁路 | Condensation 可读 | header 快照可审计 | **无** | 五条不变量 + 红灯用例 |

---

## 9. 差距、优势与取舍

### 9.1 我们落后在哪（三条结构性）

1. **"每轮重建"是我们独有的**。同行的做法是"事件流 → 派生视图"（每轮在内存里过滤一遍），或"字节冻结 + 只追加"。我们把"从落盘记录重建"当成**正确性手段**，于是每轮都要为"重建结果必须等于上次 wire"付代价——一旦不等就是全额失效。这不是缓存层的问题，是**模型的问题**。
2. **动态状态插在历史中间**。plan 上下文消息落在"累积 context 与当前输入之间"，代码注释自陈"本来就在缓存未命中区"——该判断在同一条请求内成立，**跨轮不成立**（下一轮那个位置会被上一轮的真实对话占住）。同行一致把动态状态放在**尾部消息**里，因为它永远在增长方向上。
3. **没有键也没有观测**。没有 `prefix_key` ⇒ 失效无法归因；不解析 `cached_tokens` ⇒ 改进无法验收。Claude Code 把命中率做成了 SLO，我们连仪表都没有。

### 9.2 我们领先/不落后的地方（四条，别丢）

1. **压缩复用热前缀**：装配层压缩握着上次真实请求的 system/history/tools（`MainCompactionDAG` + `PrefixReplaySummarizer`），这是 DSH 同级的能力，比"另起一次摘要调用"省一个数量级。多数 harness 没有这一步。
2. **system 已常量前缀化**：skill 正文移出 system 改成 append-only internal 事件、plan 政策移出 system、effort/plugin 进稳定层——这恰好是 Claude Code 说的"静态在前"。
3. **单一事实源 + 派生投影的纪律已有先例**：看板投影可再生、不是事实源。L1 按同一纪律落地阻力最小。
4. **会话级引擎与 seq/messageID 已参数化**：actor 所需的"每会话单写者 + 全序 seq"在 `sessionTaskRuntime` 里已有雏形。

### 9.3 取舍（写清代价，别只看收益）

| 选择 | 换到什么 | 付出什么 | 谁已经这么选 |
|---|---|---|---|
| 事件日志 + 派生视图（vs 直接维护消息数组） | 压缩/遗忘/回放都变成"追加一条元数据"；永远可重算 | 每次派生都要遍历事件；必须维护"事件 → 消息"的**稳定投影**；投影一旦不稳就是我今天查到的这类缺陷 | OpenHands、DSH、Codex（部分） |
| **本设计：派生 + L1 缓存派生结果** | 比同行少一次遍历（首 token 更快）；命中率上限不低于同行 | 多一份内存；多一套失效键；键写错就是"看起来命中其实错了" | 无（我们是这一族里第一个把派生结果缓存的） |
| memory 用**纪元表**（append-only + 逐字节 Bytes） | 相关性选择变成**确定性、可回放、可审计**的事件；冷恢复字节一致 | 多一张表；每次压缩多一次选择与写入；相关性不再逐轮自适应 | 无。同行分别是"整表替换追加"（DSH）、"system-reminder 追加"（CC）、"Condensation 事件"（OpenHands）——都**没有做**"把选择本身记成带 seq 的表" |
| 工具集**冻结**（Claude Code 式） | 前缀永不因工具变化失效 | 模型永远看得见全量工具（token 固定开销）；Plan Mode 等能力要改造成工具 | Claude Code |
| 我们的现状：工具集**可变**（插件/MCP/goal 门） | 能力面灵活、可装配 | 每次变化整条前缀失效 | Cline（同样是可变，且更糟） |

**注**：D3 的工具集哨兵（`⟦toolset⟧`）不是新发明——它是把 Claude Code 的 `<system-reminder>`、Codex 的 `environment_context` 这条"动态信息用消息承载"的范式，从"时间 / cwd"推广到"工具可用性"。区别在于它带显式 epoch 与定界符，因而**可被机器解析、可被探针断言**（同行的 `<system-reminder>` 是写给模型看的，不承诺可解析）。

**一句话取舍**：我们为了"能力面灵活 + 实现简单"选了"全动态注入 + 每轮重建"，代价是命中率；同行为了命中率接受"事件→视图投影"的复杂度。本设计不是推翻我们的选择，而是**在保留动态能力的前提下，把重建从热路径挪到冷路径，并把"选择"降维成可回放的事件**——这正是 OpenHands/DSH 证明可行的范式，加上 L1 之后我们比他们少一次遍历。

---

## 10. 对我们五条不变量的外部支撑

| 不变量 | 外部佐证 |
|---|---|
| I1 字节冻结 | Codex：请求体 append-only，且测试断言"上一条是下一条前缀"；DSH：日志是唯一事实源、`deriveMessages()` 派生；CC：`<system-reminder>` 用消息而不是改提示词 |
| I2 只追加 | Codex `context_manager` 差分追加；DSH 技能目录"整表替换也是追加一条"；OpenHands `Condensation` 事件 |
| I3 指针而非平移 | OpenHands `forgotten_event_ids`；DSH 压缩"只从第一个被遮蔽消息起失效"；CC 分层失效表 |
| I4 前缀有键 | Aider PR #5556（把 mentions 从 cache key 移除）；CC "非确定性工具顺序 / 时间戳" 三类坑；Cline environment_details 反例 |
| I5 L1 派生 | Gemini 显式缓存是**受管资源**（TTL + 存储费 + 可删除），提示"缓存必须可丢弃"；OpenHands 视图每步可重算 |

---

## 11. 未验证 / 边界

- Claude Code、Codex 的**字节级行为**来自官方博客/源码阅读与第三方抓包，**未做本地复现**；本文把它们当作"机制存在性"证据，不作为数值依据。
- Cline 的 13K/71K 是厂商自家负载测试的**平均请求**口径，不等于稳态命中率。
- Gemini 的隐式缓存计费口径在 2026 年文档里从"免费"改为"按模型倍率"，数值随模型变化，引用需标注时间。
- 子代理/teammate 的前缀共享（Codex 共享父 key）本轮不展开：DeepSeek 无 key，"字节相同即共享"，属于本设计的**后续红利**而非前提。
- 未覆盖：Cursor（闭源）、Qwen Code、OpenCode、Crush；如需，按同一轴补测。

---

## 12. 来源

### 一手（源码/官方文档/官方博客）

- OpenAI Codex CLI：`codex-rs/core/src/client.rs`、`tests/suite/prompt_caching.rs`、`tests/suite/compact_remote.rs`、`session_startup_prewarm.rs`；OpenAI《Unrolling the Codex agent loop》。
- Anthropic：《Lessons from building Claude Code: Prompt caching is everything》—— <https://claude.com/blog/lessons-from-building-claude-code-prompt-caching-is-everything>；Claude Code 文档《How Claude Code uses prompt caching》—— <https://code.claude.com/docs/zh-CN/prompt-caching>。
- Aider：`aider/coders/chat_chunks.py`、`aider/coders/base_coder.py`、`aider/resources/model-settings.yml`、`HISTORY.md`；PR #5556（repo-map cache key 去掉 mentions）。
- Cline：自托管负载测试博客（fresh/cached 分布）。
- Google：Gemini API《Context caching》—— <https://ai.google.dev/gemini-api/docs/caching>；Gemini Enterprise《Context caching overview》。
- OpenHands：SDK《Events》《Condenser》《Context Condenser》—— <https://docs.openhands.dev/sdk/arch/events>、<https://docs.openhands.dev/sdk/arch/condenser>。

### 二手（用于佐证现象）

- Codex CLI 请求体抓包分析（system/tools/`environment_context` 的体积与位置）。
- llxprt-code issue #3134（WS `previous_response_id` 与增量 delta 的结构引用）。
- nodetool PR #6971、vllm semantic-router issue #4184（Codex 的 `prompt_cache_key` / `session_id` 行为对路由的影响）。
- Cline 生态的 "cache aligner" 网关与 "pin timestamp" skill（说明该缺陷需要外部补丁）。
- DeepSeek API《Context Caching》—— <https://api-docs.deepseek.com/guides/kv_cache>（前缀单元必须完整匹配）。

### 仓库内既有调研

- [`2026-09-12-cache-hit-vs-codex-root-cause.md`](2026-09-12-cache-hit-vs-codex-root-cause.md)：本仓库断点归因（含探针实测）。
- [`2026-09-06-dash-context-cache-hit-comparison.md`](2026-09-06-dash-context-cache-hit-comparison.md)：DSH/DASH 机制与口径对齐。
- [`2026-09-11-seelex-vs-codex-context-strategy-control-group.md`](2026-09-11-seelex-vs-codex-context-strategy-control-group.md)：控制组实验。
- [`../arch/context-prefix-chain.md`](../arch/context-prefix-chain.md)：现行装配顺序与压缩四区模型。
