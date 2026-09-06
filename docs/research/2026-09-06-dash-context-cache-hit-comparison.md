# 为什么缓存命中率停在 97% 而不是 99%：与 DeepSeek Harness / DASH 的上下文策略对比

> 调研日期：2026-09-06
> 对象确认：本调研把「dh / dash」解读为 DeepSeek Harness（DSH，`deepseek-ai/deepseek-harness`，官方开源 Agent Harness）及其 TUI 前端 DASH（`Deepseek Agentic Service Harness`，DSH 插件生态中的终端前门）。若指别的产品，本报告的机制对比结论不变——因为差异点都在“每轮请求的前缀由谁、按什么顺序、以多大粒度拼出来”，而这类事实对任何 DeepSeek 系 agent harness 都是公开的。
> 性质：只读调研 + 仓库内证据对照，未改代码。

---

## 0. 结论摘要

**97% 与 99% 不是同一个量纲下的两个分数，先对齐口径，再看结构。**

1. **口径**：DSH/DASH 展示的“缓存命中率”通常是**会话末轮稳态**或“最近一次请求的 `cacheRead/(cacheRead+in)`”，而 Seelex 目前报告的 97.6% 是**整个会话/任务所有请求的 token 加权聚合**（账单口径）。聚合率天然低于末轮稳态：早期短请求分母小、命中占比低，会永久压低总和。
2. **几何**：若会话每轮新增 b 个 token、共 N 轮，聚合命中率 ≈ (N−1)/(N+1)。N=16 时约 88%（与本地冒烟聚合 90.3% 同量级），N=100 时约 98%，N≈200 才到 99%。所以“97% vs 99%”首先差在**会话长度 × 每轮新增 token 的相对比例**，不是单纯前缀稳不稳。
3. **结构**：DSH 有一整套把“每轮新增体积”压小、把“可变事实”全部放到可复用前缀之后的工程纪律（每个包 README 都声明 `KV Cache 影响`）；Seelex 主会话主体已同构（system 常量前缀化、skill/plan/打点移尾），但仍有三个结构性漏点：框架装配层按**当前查询**重算记忆块并放在累积上下文之前（`seelexctx/assembler.go`）、工具结果默认上限偏高且只在硬阈值时归档、以及 GUI 多会话热切换造成的 provider 前缀单元争用。这三个漏点把聚合率钉在 97% 附近，而不是 99%。

---

## 1. 先把两个数字对齐

### 1.1 本地可复现的“前缀模型”数据

仓库已有 [`context_cache_smoke_test.go`](../../application/core/context_cache_smoke_test.go)，按生产装配路径逐轮固化请求，再按 LCP 估算命中：

- 单会话 16 轮长会话（每轮追加约 5.3k token）：末轮 raw=96.2%、1024 块=95.2%；turn≥4 的 raw 平均 89.7%；**聚合（含前几轮）只有 90.3%**。
- 命中率收敛依赖“前缀体量 / 每轮新增”持续变大：要到 99% 末轮稳态，需要约 500k+ token 前缀或把每轮新增压到 1–2k 以下。

### 1.2 真实账单数据

[`docs/test/REPORT-perf-latest.md`](../../docs/test/REPORT-perf-latest.md) 的 37,689,781 token 实测：

| 项目 | token | 占比 |
|---|---|---:|
| 输入命中缓存 | 36,776,832 | 97.6% |
| 输入未命中缓存 | 692,746 | 1.8% |
| 输出 | 220,203 | 0.6% |

这是“130+ 次工具调用、每次把 system + 完整历史 + 请求参数整体重发”的**token 加权聚合**。未命中 69.3 万 ≈ 每请求平均新增约 5.3k（新工具结果、新文件内容）。

### 1.3 DASH 前端实际展示的口径

DASH 插件代码里的两种口径并存：

- `songqikong/dash`：`cacheReadTotal/(cacheReadTotal+usage.in)`，**会话累计**；
- `realchenwenqiao/dash`：`lastCacheRead/lastBilledInput`，**最近一次请求**。

同一场会话，“最近一次请求”会明显高于“会话累计”——市场产品如果展示末轮值，99% 并不稀奇。

---

## 2. DSH / DASH 的上下文策略（官方开源仓库事实）

以下全部来自 `deepseek-ai/deepseek-harness` 的文档（浅克隆验证），每条是“它怎么做 + 它对缓存的承诺”。

### 2.1 请求前缀身份与派生

- 会话日志是唯一事实源，模型历史由 `deriveMessages()` 派生，不另存一份历史（[`packages/core/session/README.md`](https://github.com/deepseek-ai/deepseek-harness/blob/main/packages/core/session/README.md)）。
- 每轮把「system prompt + 工具 schema + 会话前缀 + 调用配置」的完整规范快照记入 `request/header`（reason=`initial|resume|change|series`），同序列步骤继承最新快照——**请求前缀可精确重建、可审计**。
- 文档明言：append 保持可复用前缀；`replace`（压缩）只从第一个被遮蔽消息起失效；日志本身零前缀影响。

### 2.2 前缀里只放“永不重挂”的内容

- 身份/persona/插件文本在 agent **发布前挂载一次**，生命周期内不再改（[`packages/preset/persona`](https://github.com/deepseek-ai/deepseek-harness/blob/main/packages/preset/persona/README.zh.md)、[`packages/core/system-prompt`](https://github.com/deepseek-ai/deepseek-harness/blob/main/packages/core/system-prompt/README.zh.md)）。
- 工具 schema 在限制/组装后**按配置或字典序排列**，可见集合与顺序不变则前缀稳定。
- 动态/可变事实（时间上下文、AGENTS.md 发现、会话引用快照、技能目录、激活技能正文）**全部渲染成 append-only 的 user-role 持久消息**，追加在可复用前缀之后；技能目录即使整表变化，也是“追加一条完整替换目录”，不回头改写头部（[`packages/context/time-context`](https://github.com/deepseek-ai/deepseek-harness/blob/main/packages/context/time-context/README.zh.md)、[`packages/context/agent-instructions`](https://github.com/deepseek-ai/deepseek-harness/blob/main/packages/context/agent-instructions/README.zh.md)、[`packages/context/session-reference`](https://github.com/deepseek-ai/deepseek-harness/blob/main/packages/context/session-reference/README.zh.md)、[`packages/skill/tool-skill`](https://github.com/deepseek-ai/deepseek-harness/blob/main/packages/skill/tool-skill/README.zh.md)）。
- 计划模式切换会改写 system 固定顺序区段，文档**显式标注**“进入/离开模式会使该段之后失效”作为已知代价，而不是把它藏起来（[`packages/plan/plan-mode`](https://github.com/deepseek-ai/deepseek-harness/blob/main/packages/plan/plan-mode/README.zh.md)）。

### 2.3 压缩与工具结果：控制“每轮新增”和“整段失效”

[`packages/compaction/compaction-basic/README.md`](https://github.com/deepseek-ai/deepseek-harness/blob/main/packages/compaction/compaction-basic/README.md)：

- 默认 `thresholdRatio=0.8`、`retainRatio=0.16`：到 80% 才压缩，**保留最近 16% 原文**，把最老的可平衡区间替换成一条 checkpoint；压缩只是让“被替换区起点”之后的复用失效，前缀前段仍可复用。
- **摘要请求本身复用热前缀**：用与最后一次路由请求逐字节相同的 system/tools/消息重放，只在末尾追加固定压缩指令——所以压缩这笔额外调用也基本是缓存命中（“only the trailing instruction and the summary output are uncached”）。
- 先跑确定性 `tool-result-pruner`（头部/中段/尾部裁剪超预算工具结果），**不需要摘要时就不发模型调用**；裁剪让工具结果不再整块留在后续请求里。
- 溢出恢复（`CONTEXT_WINDOW_EXCEEDED`）同样走“裁剪 → 最大平衡头部缩减 → 换代后重试”。

### 2.4 子代理/worker 的请求隔离

workflow/tool-workflow 明确：

- 子 agent 在自身 provider/model/prompt/schema 下只复用**逐字节相同**的前缀；父级只拿到**被上限约束的最终结果**追加在父前缀之后；
- 独立 worker thread 有自己的请求缓存，父级上下文不膨胀。

这套组合拳的效果：**主会话每一步新增 token 被刻意压小**，长任务才能稳定出现在 99% 区间。

---

## 3. Seelex 当前请求拼装事实（代码证据）

### 3.1 主会话链路（application/core/context_runtime）

[`coordinator.go`](../../application/core/context_runtime/coordinator.go)（`PrepareExecutionContextFor` → `fitExecutionHistory`）：

```text
system（常量前缀，跨会话字节稳定）
  → 保留段（RetainedSystemHistory：已定稿轮次 append-only）
  → 累积 context（达峰前全量；压缩后 ContextMaxUnits 有界窗口）
  → plan 尾部消息（每轮重建，天然在未命中区，零额外失效成本）
  → 当前输入 + 尾部打点表
```

- system 只含 identity→plugin→effort→instructions + 被动技能目录（[`prompt_layer/coordinator.go`](../../application/core/prompt_layer/coordinator.go) 注释原话：system 变成“跨会话、跨任务共享的常量前缀”）。
- 激活技能正文走 append-only internal 事件；plan 执行策略并入尾部 plan 消息；工作打点表追加当前输入前——三处动态内容全部移出头部。
- 压缩触发：`rawTokens >= SoftThreshold`（应用侧 75% 预算、目标 60%，默认窗口 `ContextMaxUnits=4`，见 [`seelexctx/limits.go`](../../seelexctx/limits.go) 与 [`context-prefix-chain.md`](../arch/context-prefix-chain.md)）。
- 超大工具结果在硬阈值路径归档为 `result_ref`（[`seelexctx/controller.go`](../../seelexctx/controller.go)），默认 `MaxToolResultChars=60000`。

### 3.2 框架/子代理装配链路（seelexctx/assembler）

[`seelexctx/assembler.go`](../../seelexctx/assembler.go) 的装配顺序：

```text
system → project → 记忆块（按当前查询 top-K，每轮求值）→ 稳定前缀栈块
（skill/compact）→ 调用方静态块 → WorkingHistory → 动态尾部栈块（plan/task）
```

注意 `Memories(ctx, LastUserQuery(request.WorkingHistory))`：**记忆选择按当前请求动态求值，却渲染在 compact/累积 context 之前**。只要两个连续请求的查询不同导致记忆块内容不同，从记忆块起的整条前缀（compact 帧 + 全部已定稿轮次）都会失效。这是文档级已识别的旧风险（[`context-management-review.md`](./context-management-review.md) 记录“动态 memory 块单独放最后”），在 prefix-chain 新链路里需要重新验证。

---

## 4. 为什么停在 97% 而不是 99%

### 4.1 数量层：聚合口径有收敛上限

设每轮新增固定为 b，N 轮会话：

```text
request i 输入 ≈ i×b；命中 ≈ (i−1)×b
聚合命中率 = Σ(i−1) / Σi = (N−1)/(N+1)
```

| N（请求数） | 聚合命中率 |
|---:|---:|
| 16 | ~88% |
| 40 | ~95% |
| 100 | ~98% |
| 200 | ~99.0% |

97.6% 的真实账单 ≈ 130+ 请求且每请求平均新增约 5.3k token 的几何结果；末轮稳态可以上 99%，聚合要到 99% 需要把会话做到 200+ 轮、或把每轮新增压到远小于前序前缀。

### 4.2 结构层：DSH 与 Seelex 的可对照差异

| 维度 | DSH/DASH | Seelex 当前 | 对命中率的影响 |
|---|---|---|---|
| 可变事实的位置 | 一律 append-only user 消息，前缀只挂一次 | system 前缀已稳定；但框架层记忆块仍按查询重算且放在前缀中 | 记忆块变化会整段打断 compact+累积 context |
| 工具结果体积 | `maxResultChars` 上限 + 触发压缩前确定性剪枝 | 默认 60k 字符才归档 `result_ref`，普通输出全量进 transcript | 每轮新增大 → 聚合率被钉在 97% |
| 压缩 | 0.8 触发、保留最近 16% 原文、替换最老区间；摘要调用复用热前缀 | 75% 软触发、60% 目标、窗口 4；压缩即整段前缀悬崖一次（已标注为可接受代价） | DSH 压缩后恢复期更短、且压缩本身几乎不花未命中 token |
| 会话交错 | 单 agent 流顺序执行，一次只打一条热前缀 | GUI 多会话并行/热切换，多条长前缀交替 | provider 前缀单元争用/逐出（仓库自建 LRU 推演模型：K=8、保留 L=3 时聚合只有 42.6%） |
| 指标口径 | 常见末轮/最近请求展示 | 实测为账单聚合 | 95%↔99% 之间约 2–4 个点是口径差 |
| 机制承诺 | 每个包 README 有 `KV Cache 影响` 章节 | 集中写在 arch/context-prefix-chain + coordinator 注释 | 组织纪律差异，决定“漏点是否被系统性地防住” |

### 4.3 DeepSeek 侧约束（为什么字节稳定是硬条件）

DeepSeek 官方文档（[Context Caching](https://api-docs.deepseek.com/guides/kv_cache)）：

- 每次请求都会在**用户输入结束位置、模型输出结束位置、公共前缀检测、固定 token 间隔**落盘；
- 每条缓存前缀是**独立完整单元**，后续请求必须**完整匹配**缓存单元才能命中；
- 因此“前缀中间某处插入/改写动态内容”不止丢那一段，而是从改写点起整条单元链失效——这解释了为什么 DSH 把所有可变内容推到尾部、为什么把“每轮只追加”当成硬契约。

---

## 5. 收敛到 99% 的工程路径（按收益排序）

1. **口径先行**：在 `TokenAudit` 增加“每请求 hit/miss”与“会话累计 hit%”两个字段，真实 usage 落盘归因；之后比较 dash 时明确口径（末轮稳态 vs 聚合）。
2. **压小每轮新增**（这是 97→99 的最大杠杆）：
   - 下调超大工具结果归档阈值，或对中等偏大结果先确定性裁剪（对齐 DSH `tool-result-pruner`），原始内容进 `result_ref` 按需读回；
   - read/grep 类结果默认窄化（分页/过滤），避免整文件输出进 transcript。
3. **记忆块移出前缀或改为会话级装载一次**：框架装配层 `Memories` 不再按每轮查询求值后放在 compact 之前；要么会话开始时定稿，要么渲染到累积 context 之后（对照 DSH 的 append-only 动态快照）。
4. **压缩后热前缀延续**：压缩尽量保留原文尾部（而不是整段窗口重置），摘要/压缩帧本身视为可复用前缀的一部分；如需模型摘要，摘要请求复用最后一次请求的字节前缀（DSH 做法）。
5. **会话调度**：GUI/后台尽量按会话聚合连续请求，避免多条长前缀高频交替；同项目同 system 的会话天然共享公共前缀，可在 DeepSeek “公共前缀落盘”语义下被复用（对应最近会话切换修复工作的价值）。

---

## 6. 资料来源

### 本地

- [context_cache_smoke_test.go](../../application/core/context_cache_smoke_test.go)：前缀命中冒烟（LCP、64/1024 块、LRU 争用推演）
- [REPORT-perf-latest.md](../test/REPORT-perf-latest.md)：37,689,781 token 真实账单（97.6% 命中）
- [context-prefix-chain.md](../arch/context-prefix-chain.md)、[context-management-review.md](./context-management-review.md)：Seelex 前缀链路与既有 risk 记录
- [coordinator.go](../../application/core/context_runtime/coordinator.go)、[prompt_layer/coordinator.go](../../application/core/prompt_layer/coordinator.go)、[seelexctx/assembler.go](../../seelexctx/assembler.go)、[seelexctx/limits.go](../../seelexctx/limits.go)
- [devlog/2026-08-11-subagent-interview-answers.md](../devlog/2026-08-11-subagent-interview-answers.md)：DeepSeek 缓存落盘/命中前提（interview 结论）

### 外部（开源/官方）

- DeepSeek Harness 官方仓库与文档：<https://github.com/deepseek-ai/deepseek-harness>（核心：`packages/core/session`、`packages/core/system-prompt`、`packages/compaction/compaction-basic`、`packages/context/*`、`packages/skill/tool-skill`，各 README 均含 `KV Cache effect` 声明）
- DASH TUI 前端：<https://github.com/songqikong/dash>、<https://github.com/realchenwenqiao/dash>
- DeepSeek API 官方缓存文档：<https://api-docs.deepseek.com/guides/kv_cache>
