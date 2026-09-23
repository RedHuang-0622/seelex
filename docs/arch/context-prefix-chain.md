# 上下文前缀链路（Context Prefix Chain）——已采用，已实现

> 状态：**已实现（任务 A/B/C/D 落地，2026-08-30）**。本文描述当前代码事实；
> 装配顺序、checkpoint 正常路径移出、达峰才压缩、plan/task 后置与 fork
> todolist 过滤均已按「实现点」落地；若后续调整，先改本文再改代码。
>
> 2026-09-24 复核修订：① 的 system 范围收窄到**插件级 skill 目录**为止
> （此前写的「task 级 skill 内容段」「plan 级 plan 政策段」取消：skill 正文与
> tool / command 同生态位、落在 context 区；plan 政策与 plan 同区放到尾部）；
> 新增《压缩相关名词与取值》，压缩讨论一律使用该表名词。skill / plan 的装配
> 已跑探针：**结论是不改**；该探测发现的一处缺口已于 2026-09-24 决定「接受现状」
> （证据、处理决定与重议触发条件见《已落地与已决定不改》第 5 条）。
>
> 2026-09-24 落地：原《待落地（目标设计）》的 1–4 项**已按设计实现**——保护区下限
> `context_retain_floor_percent`（含 `floor > retain_tokens` 启动期报错）、帧摘要传递上限
> `context_frame_carry_tokens`（超限退化为锚点）、replay 分片链（协议单元切片 + 摘要前向
> 传递）、四区显式化（`ContextLayout`，判据与报表同一份口径）。四者各自的**打点落点**与
> 验证手段见该节；三门口禁（真 API 冒烟 / `-race` / pprof 锁竞争）见《实现点》末尾。

## 目标

提升 LLM 请求的**前缀缓存命中率**与**执行注意力**：

1. 把几乎不变的段（system / project / memory / compact）放最前，形成稳定前缀；
2. 把已定稿的轮次作为 **append-only 累积 context**，成为可缓存前缀的主体；
3. 把频繁变化且**不参与压缩**的 plan / task 后置到贴近当前输入的位置（注意力焦点，减少幻觉）；
4. checkpoint **不进入 LLM 上下文**（正常路径；异常恢复路径保留）；
5. fork = 深拷贝新会话，**不继承父 todolist**。

## 装配顺序（新链路）

提交给 LLM 的最终顺序（按不易变程度排列）：

```text
system → project → memory → compact → context(累积已定稿轮次) → plan → task → 当前输入
```

实现细节：compact 帧作为稳定前缀块渲染在 memory 之后、紧邻 context；tools 走
请求级 API 通道，不在文本前缀。

- **system**：`prompt_layer` 的稳定层 identity → plugin → effort →
  instructions，**再追加插件级 skill 目录（`## Available Skills`）就到此为止**。
  system 里**没有** task 级 skill 内容段，也**没有** plan 政策段——这两段原先追加
  在 system 尾部，已移出（`prompt_layer/coordinator.go` 的
  `SystemPromptForActiveTaskLockedFor` 注释把这条纪律写在函数头上：system 只放
  「与任务/会话无关」的稳定字节）。
  - **skill 的「使用」与 tool / command 是同一个生态位**：它落在 **context 下面**
    ——激活时写一条 `ActiveSkillMarker` internal 事件进 transcript（append-only、
    与工具调用/命令输入同属对话内容），随 `TranscriptTailHistory` 携带、按协议单元
    切分、**参与压缩**。既不该占 system 字节，也不需要「在 system 里专门说明它是
    什么、怎么用」。
  - **plan 政策** 并入**尾部的 plan 上下文消息**（`## Active Plan Execution
    Policy`，`context_runtime.planContextMessageLocked`）：与 plan 坐一桌、放在
    后面，plan 推进只改尾部字节，system 开头永不动。
- **project**：`RenderProjectBlock`（项目模块语义，会话前预读）。除非毁灭性重构不变。
- **memory**：`relatedMemoryBlocks`（按当前查询从 CompactStack top-K）。项目粒度、跨会话；除非艰难纠错不变。
- **compact**：compact 栈帧（已压缩历史摘要 + evidence）。除非手动压缩/达峰不变。
- **context（累积）**：已定稿完整轮次，append-only；每次请求只追加新轮，旧轮字节不变 → 缓存前缀主体。
- **plan / task**：频繁变化、**不参与压缩**、后置（贴近输入）。
- checkpoint：不入 LLM 上下文（正常路径）；checkpoint 消息只保留恢复路径
  （provider 504 / history-safety，`RetainedSystemOnly` + 恢复信封）。plan
  尾部消息（`plan_ref` + 当前节点 slice）在正常路径注入，贴近当前输入。

### skill 与 tools 在当前实现中的装配位置

**位置线轴**（一次 LLM 请求 = 装配源 + 文本轨道 + API 轨道；C=可缓存稳定，
S=每轮重建；编号是装配点，明细见下）：

```text
装配源（进程启动 / switch_plugin 时装配；不随每轮请求重建）：
plugins 装配 ── plugins/<plugin>/<skill>/SKILL.md
  ├─ main.go:141 RegisterBuiltins ───────► seelebridge 工具注册表（API 轨道全量源）【装配点 T0】
  ├─ initSkillSystem/initPluginSystem ──► plugin.Manager.Load
  │    ├─ DefinePlugin        ──────────► 工具 include/exclude 可见性快照【T1】
  │    └─ PublishPluginSkills ──────────► skill.Registry.pluginSkills（每插件技能表）【S0】
  └─ activateDefaultPlugin / switch_plugin ─► plugin.Manager.Activate
       ├─ ActivatePlugin       ─────────► 工具可见性 active【T1b】
       └─ ActivatePluginSkills ─────────► skill.Registry.activePlugin ← 可见 skill 列表跟随插件【S1】

文本轨道（消息序列，前缀缓存友好）：
┌─ system ★前缀到此为止 ───────────┬─ 稳定/半稳定段 ────────────────┬─ 尾部（每轮重建）──┐
│ identity → plugin → effort →     │ project → memory → compact →   │ plan 上下文消息    │
│ instructions → skill 目录(插件级C)│ context（已定稿累积，         │  （含 plan 政策段）│
│                                  │  append-only，达峰才压缩一次）  │ → task → 当前输入  │
└──────────────────────────────────┴────────────────────────────────┴────────────────────┘
   ↑ 其后不再追加任何 system 段        ↑ 激活 skill 正文落在这里
                                        （与 tool/command 同生态位）
   └ 装配点 S1b（system 内的被动目录，零调用）：prompt_layer.SystemPromptForActiveTaskLockedFor 在
     base 后插入 "## Available Skills" 段（skill_catalog.go RenderSkillCatalog）；
     数据源 contract.Skills.All()（skill.Registry 按 activePlugin 隔离 → adapters.SkillPort），
     随插件 Load/Activate 自动可见——模型无需先调 skills_list（降低插件自主性要求）。
     只含 name/description，永不携带指令正文；**system 的前缀到此为止**。
   └ 装配点 S2（内容注入，落 context 区）：#<skill> → PromptStack(kind=skill) → chatRequest.skills →
     task_context 写 ActiveSkillMarker internal 事件（task_context_state.go:77-93）
     → 随 TranscriptTailHistory 进 context 区、按协议单元切分、参与压缩；不渲染进 system。
     模型侧激活：skill_activate 工具（main.go）→ application.Service.ActivateSkill
     （同一 promptStack 压栈）→ 本轮工具结果即时回传技能正文、下一轮提交自动进 S2。
     装配点 S3（休眠）：seelexctx SkillStack 顶层帧渲染器（已接线 PrefixStacks，
                    主会话无生产 PushSkill 压栈 → 实际不出现）
   └ 装配点 S4（plan 政策，落尾部区）：context_runtime.planContextMessageLocked 把 plan_ref/status/
     节点投影 + "## Active Plan Execution Policy" 拼成同一条尾部 plan 上下文消息——与 plan
     同区、每轮重建，不进 system。

API 轨道（请求级 schema 通道；每轮经可见性过滤后全量下发，不进消息文本、不参与压缩）：
┌ tools: [read_file, grep_search, bash, …, plan_load, task_check_node, …, fork_subagents] ┐
└── 装配点 T2：registry → Policy.Filter（子代理排除 + goal skill 门 + 插件 include/exclude）
            → agent bridge RegistryRuntime.VisibleTools → Seele session 每轮
            AssemblyRequest.Tools → seelexctx.Assemble 透传 → provider API ─────────────┘
```

图例说明：**可见 skill 列表（哪些可 # 激活）与 tools 列表一样由 plugins 装配决定**
——来源 `plugins/<plugin>/<skill>/SKILL.md`，经 plugin.Manager 发布到
skill.Registry 并按 `activePlugin` 隔离（S0/S1，装配位置在 plugins 系统，见下表）。
**system 里只有被动目录**（S1b，`## Available Skills`：base 之后、只含
name/description、零调用即发现），**system 的字节到此为止**；**skill 的「使用」与
tool / command 是同一个生态位**——激活后的正文是 transcript 里的一条 internal
事件（S2，落 context 区、与对话一起 append-only、参与压缩），不占 system 字节，
也不需要「在 system 里说明 skill 是什么、怎么用」。plan 政策同样不在 system：
它与 plan 一起拼成尾部消息（S4）。tools 列表走请求级 API schema（T0→T2），
不在文本前缀；可见性集合变化 → 请求级变更。

### tools（API 轨道）的精确装配位置

**① 注册期（进程启动，产物 = 工具注册表全量源）**

| 装配点 | 位置 | 内容 |
|---|---|---|
| T0 | `main.go:141` `runtime.RegisterBuiltins()` → `seelebridge/runtime_tools.go:140` `RegisterBuiltins` | project-scoped 文件/shell 工具、fork、todo/task 工具族注入框架注册表 |
| T0b | `main.go:185` `registerProductTools` | `get_time`、`websearch`、MCP 懒加载/`mcp_load`、`switch_plugin`/`switch_mode`、`plugins_reload`、`ask_approve` 等经 `runtime.RegisterTool` 注册 |
| T0c | `seelebridge/runtime.go:305` `seeltools.NewRegistryState(...)` | 注册表本体（tool 事件 middleware/权限门/bash 诊断）；plan/task 终态工具在 `registerTaskTerminalTools`/`registerContextReadTools` 追加 |

**② 可见性过滤（装配点 T1，快照在切换/激活时更新；T2 在每轮求值）**

| 装配点 | 位置 | 内容 |
|---|---|---|
| T1 | `plugin/manager.go` `Load()`/`Activate()` → `runtime.DefinePlugin/ActivatePlugin` → `seelebridge/plugin/plugin.go` `Manager.Filter` | 激活插件的 include/exclude 通配过滤（可见性快照缓存） |
| T1b | `seelebridge/runtime.go:311-325` `seeltools.NewPolicy` + `bridge.NewRegistryRuntime(…, WithVisibilityPolicy(policy.Filter))` | 策略装配进 agent 工具运行时：子代理 NodeScope 排除全局状态工具 + plan 工具族仅在 goal skill 激活时可见（`seelebridge/tools/policy.go` `Policy.Filter`） |
| T2 | `Seele v0.1.2 session/loop.go` `ReActLoop.visibleTools()` → agent bridge `RegistryRuntime.VisibleTools`（publicTools → policy） | **每轮**请求取可见工具集，进入 `AssemblyRequest.Tools` |

**③ 请求注入（装配点 T2 下游）**

- `seelexctx/assembler.go` `Assemble`：把 `AssemblyRequest.Tools` 原样透传 →
  `AssembledRequest.Tools`（不渲染为消息文本）。
- Seele `session/loop.go` `callLLM`：`assembledTools` 交给
  `llm.Complete/CompleteStream` → provider API schema。
- 应用侧预算面（非注入）：`application/core/context_runtime/coordinator.go:350`
  `Runtime.VisibleTools(ctx)` 取面 → `:391/:406` `CountRequestTokens`（估算 prompt tokens）。

**④ tools 到底排在上下文的哪个位置（三层定位）**

```text
组装层（seelexctx/seele）：消息数组内【无 tools 槽位】
  AssembledRequest{ WorkingHistory: [system, project, …, context, plan/task, 输入],   ← 文本轨道
                    Tools:        [可见工具 schema……] }                                ← 并列字段，不渲染进任何一条消息

请求体字节序（顶层字段，非消息项；Go struct 字段序即 JSON 字节序）：
  OpenAI    : model → messages → 【tools】 → max_tokens → temperature → stream
  Anthropic : model → messages → max_tokens → system → stream → temperature → 【tools】 → tool_choice

模型实际看到的输入序（provider 侧，黑盒/公开语义，本仓库不可验证）：
  [system 文本] → 【工具定义(系统级区段)】 → [对话历史] → [plan/task/当前输入]
  —— 不是最开头（最开头是 system identity/instructions），也不是最末尾
     （最末尾是 plan/task + 当前输入），而是夹在系统文本与对话历史之间。
```

- **消息数组内**：`seelexctx/assembler.go` `Assemble` 把 `request.Tools`
  透传为 `AssembledRequest.Tools`，与 `WorkingHistory` 并列——tools 既不在
  消息最前也不在消息最后，文本轨道里没有任何槽位（上页线轴的"API 轨道"
  就是这一事实）。
- **HTTP 请求体**：tools 是顶层字段，字节序在 `messages` 之后（OpenAI 第 3
  字段；Anthropic 在 `messages`/`system` 之后、接近体尾）。此序不影响消息
  数组语义，但决定请求字节前缀的内容。
- **provider 输入序**：OpenAI/Anthropic 把工具定义作为**系统级输入**处理，
  位于 system 内容之后、最早一条对话之前；二者均将该区段计入输入 tokens
  并支持（自动/显式）前缀缓存——tools 集合稳定时它随 system 一起构成可
  缓存前缀。该点是 provider 公开语义，仓库代码只能证到请求体字段序。

### skill 列表的精确装配位置（装配在 plugins 系统，S0/S1）

**① 来源装配 S0：插件目录 = 技能目录（磁盘→插件定义）**

- `plugin/loader.go:120` `loadPlugin` → `skill.LoadPluginDir(pluginRoot)`
  （`skill/loader.go:202`）扫描 `plugins/<plugin>/<skill>/SKILL.md`；插件定义
  `plugin.Plugin{Skills: []skill.Skill}` 自带技能表。
- `main.go:473` `initSkillSystem` = `skill.NewRegistry()`（无全局技能 loader；
  注释明示 registry 由 plugin Load/Activate 的 `PublishPluginSkills` 填充）。

**② 发布/激活装配 S1：plugin.Manager 事务驱动**

- `main.go:145` `initPluginSystem`：`plugin.NewManager(loader, runtime, runtime, skillRegistry)`
  （ToolBackend/MCPBackend=runtime，SkillBackend=registry）→ `manager.Load()`：
  每插件 `skills.PublishPluginSkills(p.Name, p.Skills)`
  （`skill/skill.go:188` → `pluginSkills[name]`）。
- `main.go:186` `activateDefaultPlugin` → `manager.Activate(default)` →
  `skills.ActivatePluginSkills("default")`（`skill/skill.go:172` →
  `registry.activePlugin = "default"`）。
- 运行期切换：`switch_plugin`/`switch_mode`（`main.go` `registerPluginSwitchTools`）
  → `adapters.PluginPort.Activate` → `plugin.Manager.Activate`（同一事务：工具
  可见性 + `ActivatePluginSkills` + MCP attach/detach）。

**③ 可见性隔离（activePlugin ≠ "" 才生效）**

- `skill/skill.go` `Get`(76)/`All`(93)：只返回 `pluginSkills[activePlugin]`
  → `internal/adapters/session_workspace_ports.go:121` `SkillPort.Get/All` →
  `application/core/completion.go:33`（`#` 补全）与 `input.go:53`（`#<skill>`
  路由）只能看到当前插件技能（default→plan/code/review/…，freecad→cad-*）。
- tools 侧的 goal skill 门同源：`seelebridge/runtime.go:314/361` 闭包读
  `node.GoalSkillActive()`（应用发布的不可变可见性投影）。

**④ 激活内容注入 S2/S3（下游，不是列表装配）**

- `#<skill>` 命中后（`input.go` `activateSkillAndSubmit`）→ `PromptStack` 压
  `kind="skill"` 层 → `newChatRequest` 固化到 `chatRequest.skills` → 任务启动
  `ActivateTaskSkillsLocked`（`application/core/chat.go` →
  `task_context`）写 `TrustedSkillLayers` + 投影 `ActiveSkills`（ContentHash）
  → `task_context.ensureActiveSkillEventsLocked` 落一条 `ActiveSkillMarker`
  internal user 事件（`task_context_state.go:77-93`：`<!-- seelex:active-skill:v1 -->`
  + `## Trusted Active Skill: <name>` + 正文），随 `TranscriptTailHistory` 进
  context 区、按协议单元切分并**参与压缩**（同处注释明示：压缩/历史安全路径不特殊
  处理，可被压掉，由帧摘要与检索承载）。该事件同时以 **wire material 落盘**
  （`sessionstore/durable_history.go:205-218` 保持 `user` 角色回放、
  `wire_assembler.go:266-280` 以 `user` 回放且 `Internal: true`）→ resume 可回放、
  `search_history` 可回读。**system 不再渲染该段**：
  `prompt_layer/coordinator.go` 的 `SystemPromptForActiveTaskLockedFor` 注释明确
  记载「激活技能正文已移出 system，改为 internal 事件 append 进 transcript」。
- `seelexctx.RenderStablePrefixBlocks` 的 SkillStack 顶层帧渲染器已接线
  `PrefixStacks`，但主会话无生产 `PushSkill` 压栈（仅测试/恢复拷贝）→ 该帧
  实际不出现；节点子代理另经 `seelebridge/node` 注入 skill 目录块 + 匹配
  激活块（kind:agent 范围），并继承主代理稳定块。

> 结论：**skill 列表的装配位置在 plugins 系统**（S0 loader 扫描插件技能目录
> → S1 plugin.Manager 发布/激活 + registry activePlugin 隔离 → S2 用户
> `#<skill>` 激活后正文以 transcript internal 事件进 context 区，**system 里
> 只留插件级目录**）；tools 列表的装配位置是注册表 + 插件 include/exclude +
> goal skill 门（T0→T2，API schema 通道）。

### 压缩相关名词与取值（先定名词，再讲行为）

后面所有关于压缩的讨论只用下表的**名词**，不再用「软的 / 硬的」这类含糊说法。

| 名词（代码字段 / 配置键） | 含义 | 取值 / 公式 | 当前值 |
|---|---|---|---|
| 窗口 `Window` | provider/model 声明的输入上限 | `context_window` | 200000 |
| 输出预留 `OutputReserve` | 留给模型回复的额度，先从窗口里扣 | `limits.output_reserve_tokens` | 512 |
| 安全预留 `SafetyReserve` | 兜底额度，抵估算偏差 | `Window / limits.context_safety_reserve_divisor` | 25000 |
| **预算 `Budget`** | 本项目所有 token 判据的基数 | `Window − OutputReserve − SafetyReserve` | 174488 |
| **达峰线 `SoftThreshold`** | 请求估算到这条线就触发压缩 | `Budget × context_soft_percent` | 130866（75%） |
| **装配上限 `HardThreshold`** | 装配完之后估算到这条线，再压一次 | `Budget × context_hard_percent` | 157039（90%） |
| 压缩目标 `TargetAfterCompaction` | 压完希望落回的规模（报表口径） | `Budget × context_target_percent` | 104692（60%） |
| **单条外置线 `SingleItemInputLimit`** | 单条 message / 当轮输入超过它就外置为 `result_ref` | `Budget × context_single_item_percent` | 87244（50%） |
| **全量上下文 `all_context`** | system prompt + 全部历史事件 + plan 尾块的计数（**不含** tools、不含当轮输入） | `CountRequestTokens("", fullContext, "", nil)` | 随会话增长 |
| **请求估算** | 形状与真实请求一致：system + 全量历史 + 当轮输入 + tools（与缓存形状取大） | `CountRequestTokens(systemPrompt, fullContext, currentInput, tools)` | 随会话增长 |
| **装配后估算 `estimated`** | 真装配完再数一遍的数——判装配上限与报错用它 | `fitExecutionHistory` 返回值 | 每次装配算一次 |
| **保留区 `retained`** | 压缩时保留多少上下文（③ 的候选值） | `min(token1, token2)`，再 clamp 到 `[1, all_context]` | 见下节 |
| 保留比例 `window.ratio` | 保留区占全量上下文的比例 = `token2` | `token2 = ratio × all_context` | 0.7 |
| 保留上限（写死）`window.retain_tokens` | 保留区的绝对上限 = `token1`；0 = 未配置 | 未配置时 `token1` 取 `Budget.Window` 兜底 | 0（→ 默认实际由 ratio 决定） |
| **保护区下限 `context_retain_floor_percent`** | 保留区的比例下限（《已落地》1） | `floor = max(最近 1 个完整协议单元, 比例 × 预算)`；判定 `clamp(min(token1,token2), floor, token1)` | 0（未配置 → 只剩「至少 1 单元」兜底） |
| **帧摘要传递上限 `context_frame_carry_tokens`** | 本地折叠把上一帧 Chapter 2 正文并入新帧的并入量上限（《已落地》2） | 超限 → 退化为锚点（`segment_id` + request 首尾 + 一句话） | 1024 |
| 独立触发线 `window.force_compact_tokens` | 不看比例的另一条压缩触发线 | `all_context ≥ 该值` 即触发；0 = 未配置 | 0（不触发） |
| 轮数窗口 `window.rounds/min_rounds/max_rounds` | 框架侧按**轮**折叠的窗口（另一条独立路径） | `clamp((Window × ratio − Reserved) / AvgRoundTokens, min, max)` | 0 / 4 / 40 |
| 窗口保留轮数 `limits.context_max_units` | 压缩后 context 最多留几轮 | — | 4 |
| 消息分片 `limits.message_shard_size` | 原文分片粒度（事实源） | — | 100 条/片 |
| 项目摘要上限 `limits.summary_chars` | 只作用于**项目记录摘要**，**不作用于压缩帧** | — | 800 |
| 重放摘要上限 `PrefixReplayMaxTokens` | 前缀重放通道生成 Chapter 2 的输出上限 | — | 2048 |

> 口径提醒：`window.ratio` 在实现里驱动**两个分母不同**的公式——`retained` 的分母是
> **全量上下文**（`seelexctx/window.go` `RetainedContextTokens`），轮数窗口的分母是
> **窗口减预留**（同文件 `WindowRounds`）。本文凡说「占比」都指前者。
>
> 固定开销实测（`seelexctx/tokens.Count`，default 插件／典型 plan）：skill 目录段
> **1227 字符 / 388 token**（10 个技能，插件级稳定）；plan 政策段 **287 字符 / 85 token**；
> 一条典型 plan payload（含 `current_slice`）**347 字符 / 119 token**。

### 压缩（折叠 compact 栈顶 + context 窗口）

```text
同一任务连续轮次（context 逐轮追加）：
├system(C)┼project(C)┼memory(C)┼compact(C)┼─context(轮1..N-1 已定稿,C)─┼plan+政策(S)┼task(S)┼输入(S)┤
   ↑ 稳定段 + 已定稿 context 全命中；每轮只重发 plan/政策/task/新输入

context 达峰 → 压缩（折叠 compact 栈顶 + context 窗口，保留新鲜 compact 帧 + 窗口剩余）：
├system(C)┼project(C)┼memory(C)┼compact(+新帧,S)┼context(重置,新起点,C)┼plan+政策(S)┼task(S)┼输入(S)┤
   ↑ 压缩即整条前缀失效一次；之后 context 重新累积，恢复长命中
```

实现落点：应用侧按**达峰线**触发压缩——请求估算 ≥ `SoftThreshold`（当前 130866
≈ 75% 预算）即发布 `Snapshot.Task.ContextCompactions`，并把 context 切换为有界
新鲜窗口（≤ `limits.context_max_units`，当前 4 轮）；框架侧 `ContextController` 把
窗口外轮次折叠进 CompactStack（保留窗口剩余 + compact 帧标记）。装配完成后再按
`estimated` 判一次**装配上限**（当前 157039 ≈ 90% 预算）；`estimated > Budget` 直接
报错（`ErrProviderContextBudgetExceeded`），不静默超限。plan/task 尾部消息是控制
标记（`<!-- seelex:active-plan:v1 -->`），不参与轮次单元切分，即不参与压缩。
（四区划分与该边界的判据见下节《压缩四区模型》。）

### Fork（深拷贝新会话）

```text
父：├system(C)┼project(C)┼memory(C)┼compact(C)┼context(C)┼plan(S)┼task(S)┼输入(S)┤
子：├system(C:复用)┼project(C:同workspace)┼memory(C:项目级跨会话)┼compact(C:深拷贝复制)┼context(C:深拷贝复制)┼plan(S:子任务)┼task(S)┼输入(S)┤
   ↑ 几乎整条前缀命中父热缓存；只重发 plan/task/输入
```

子会话通过深拷贝独立（`PrepareFork` → `SaveCommitWorkspace`），前端不复用父视图；**后端不继承父 todolist**（见实现点 5）。

已实现：`truncateForkRecord` → `forkTaskRecordsByTime` 过滤 `kind=todo`，
子会话 todolist 全新；ForkedFrom 血缘与 plan/task/checkpoint/tool-results
拷贝不受影响。

## 压缩四区模型（边界与判据）

> 状态：**模型已共识冻结（2026-09-24）**。「压缩不归档 / 只压一次 / message 不截断 / 后缀成员归属」
> 是**当前实现**；「保护区下限 floor / 帧摘要传递上限 / replay 分片链 / 四区显式化」**已按设计落地**
> （实现位置、打点落点与验证见《已落地与已决定不改》1–4）；「skill 与 plan 装配是否再改代码」
> 已跑探针，结论为 **不改**（同节第 5 条）。

压缩视角下，一次请求切四段，**只需要判定一条边界**：

```text
┌─ ① 绝不压的前缀 ──┬── ② 被压掉的上下文 ─┬── ③ 窗口保护的上下文 ─┬── ④ 绝不压的后缀 ──┐
│ 稳定 system 指令   │ 折出窗口的完整协议   │ 最新的一段；整条      │ plan 尾部消息      │
│ + 插件级 skill 目录│ 单元 → 有界         │ message 不许被切      │（plan 政策同一条） │
│ + 稳定栈块         │ checkpoint 帧        │                      │ worktable          │
│ （compact 栈顶帧） │                      │                      │ goal（栈不入上下文）│
└───────────────────┴────────────────────┴──────────────────────┴────────────────────┘
                                    ▲
                        ★ 要判定的就这一条边界：③ 从哪里开始 ★
```

- **① 绝不压前缀**：`prompt_layer` 的 system 稳定层（identity/plugin/effort/instructions）+ **插件级 skill 目录（`## Available Skills`，system 到此为止）** + 稳定栈块（compact 栈顶帧摘要紧邻 context）。
  **skill 不进 ①（也不在 system 里）**：激活 skill 的正文是 transcript 里的一条 `ActiveSkillMarker` internal 事件——与 tool / command 的调用**同一个生态位**，落在 context 区、按协议单元切分、**参与压缩**；`seelexctx` 的 SkillStack 前缀帧渲染器虽已接线（`PrefixStacks`），主会话无生产 `PushSkill` 压栈 → 实际不出现。
  **plan 政策不进 ①**：它与 plan 拼成 ④ 的尾部消息（见下）。
- **② 被压掉的上下文**：边界之外的完整协议单元 → 折出窗口，写成有界 checkpoint 帧。
- **③ 窗口保护的上下文**：最新的一段，**整条 message 不切**。
- **④ 绝不压后缀**：`worktable`（拼在 currentInput 上、不落历史）、**plan 尾部消息**（控制标记 `<!-- seelex:active-plan:v1 -->` + plan 投影 + `## Active Plan Execution Policy` 政策段，同一条消息；不参与轮次单元切分）、goal（栈本身不进上下文，只经 goal 工具读写；裁决指令在 ChatStream 前注入）。三者都「跟着用户输入一起更新」，**不参与压缩**。

### 边界判定（③ 从哪里开始）

| 候选 | 语义 | 量 |
|---|---|---|
| A 占比窗口 | 按会话真实规模等比留 | `占比 × 全量上下文`（占比 = `window.ratio`） |
| B 绝对上限 | 写死的绝对上限 | `retain_tokens`（`token1`） |

**判定：取少（min）**，并包一层下限：

```text
保留区 = clamp(占比 × 全量, floor, retain_tokens)
floor  = max(最近 1 个完整协议单元, context_retain_floor_percent × 预算)   ← 已实现（《已落地》1）
```

理由：

1. `retain_tokens` 在直觉上是**上限**（不许超过这么多）而不是下限，只有 min 表达得上；取 max 等于把用户意图读反。
2. 失效方向单调：保留区越小 → 压缩区越大 → 越有余量、越压得动；取小的代价只是「少看一点原文」，而原文可检索回读；取大一顶到窗口上限就直接发不出去（不可挽回）。
3. 只有保留区会收紧时，**装配上限**（当前 90% 预算）才有腾挪空间；取大会让保护区先吃掉余量，装配上限一响只剩「整段折叠 = 失忆」。

下限（floor）用于挡住 min 的坏方向：用户把 `retain_tokens` 写小 → 保护区被压到失忆。现状只有「至少保最新 1 个完整协议单元」这条兜底，没有比例下限。
配置校验：`floor > retain_tokens` 必须**报错**，不许静默取小（否则「用户设了个大数」这类问题会被吞掉）。
已实现落点：`seelexctx.WindowConfig.ValidateRetainWindow` + `application/core.ValidateRetainWindow`
（后者由 `main.initRuntime` 在 Runtime 建成后调用，预算口径与压缩判据同源）；比例类旋钮超界
（不在 [0,100]）在 `seelexctx.LoadLimits` 报错，不再静默回退默认值。

### 粒度：不截断 + 判定顺序

- 边界若切进某条 message → 该 message **整体划入 ③**（向内取整）；因此实测保留区会略大于候选值。
- 判定口径两分：**达峰线**（该不该压）算的是**请求估算**——形状与真实请求一致（system + 全量历史 + 当轮输入 + tools）；**装配上限**（压完还超没超）必须放在**对齐之后、用装配后估算 `estimated`**——不能用候选值判（否则会出现「说超线了却没压」）。
- 极端：单条 message 自身超过 ③ 的候选值（例如一次超长工具结果）→ ③ **至少保 1 个完整协议单元**（宁可暂时超预算，也不切 message），再靠「单条输入 > 预算 50% → 外置 `result_ref`」收场。**「不截断」与「外置」必须同时存在**，缺一个就会出现「满足不了约束又没出口」的死角。

### 不归档、只压一次

- **事实源唯一**：原文只存在消息分片（`message_<from>_<to>.jsonl`，`limits.message_shard_size` 默认 100 条/片）与事件库。压缩移动的是**指针**，不复制、不搬走原文。
- **帧只存索引 + 摘要**：`CompactFrame{segment_id, from..to, request/round/event/message 区间, summary, evidence, prev_*}`；链锚点只指向前驱（segment_id/request/一句话），**不复制前驱全文**。
- **回读走检索**：`search_history` 把 CompactStack 帧当语义索引，命中后按 `[From..To]` 从事件库读回真实记录内联返回。压缩侧不另造事实源副本。
- **只压一次**：分片 i 的内容 → 摘要_i → 给下一个分片/帧；同一内容不再被压第二遍，摘要不再被二次加工。

### 分片压缩链（压缩区自身超过模型输入范围时）

```text
溢出区 O = [u1 … un]
  片1 = system + O[0:k1]         + 指令 → 摘要₁
  片2 = system + O[k1:k2] + 摘要₁ + 指令 → 摘要₂      ← 片界落在协议单元边界，不切 message
  …
  Chapter2 = 摘要_k → 写入一帧（原文完全不动）
```

- 只有**前缀重放通道**需要分片：它是唯一把原文重新送进 LLM 的路径（`seelexctx/replay.go`：system 同字节 + 最近真实请求 History 原样 + 固定指令）。
- 每片只压自己的区间一次；摘要_i 仅作为片 i+1 的输入随行，不再被压。
- 取舍（已定）：片 1 命中前缀缓存，**片 2..k 不复用**（前缀不同）。走到分片说明压缩区已大到一次性发不出，正确性与「不重复送原文」优先于缓存。

| 规则 | 当前落点 |
|---|---|
| 保护区不截断 + 至少 1 单元 | `application/core/task_context/plan_transcript.go`（`TranscriptTailWindow`、`transcriptProtocolUnitList`） |
| 保护区候选取小 | `seelexctx/window.go`（retained tokens 取 min） |
| 帧只存指针 + 摘要 | `sessionstore.CompactFrame`、`seelexctx/controller.go`（`buildCompactFrame`） |
| 回读检索 | `application/core/history_search.go` → `seelexctx/search` |
| 分片事实源 | `sessionstore/message_rows.go`（`message_<from>_<to>.jsonl`） |
| 重放（分片落点） | `seelexctx/replay.go`（`PrefixReplaySummarizer`） |

## 数据结构与涉及文件

| 层 | 文件 | 数据结构/函数 |
|---|---|---|
| 应用装配 | `application/core/context_runtime/coordinator.go` | `prepareExecutionContext`、`fitExecutionHistory`、`RetainedSystemHistory`、`planContextMessageLocked`、`checkpointContextMessage`、`TranscriptTailHistory` |
| 框架装配 | `seelexctx/assembler.go` | `AssemblerOptions{SystemPrompt, ProjectBlock, StackBlocks, Memories}`、`Assemble` 投影顺序、`RenderStackBlocks`（plan/task/skill/compact） |
| 运行时接线 | `seelebridge/runtime_context.go` | `seelexAssembler`、`relatedMemoryBlocks`、`stackBlocks`、`projectBlock` |
| 系统提示 | `application/core/prompt_layer/coordinator.go` | `SystemPromptForActiveTaskLockedFor` |
| 会话上下文 | `sessionstore.SessionContextRecord` | `PlanStack` / `TaskStack` / `SkillStack` / `CompactStack` |
| Fork | `application/core/session_runtime/fork.go` | `truncateForkRecord`（过滤 `kind=todo` 的 Tasks） |
| 压缩帧 | `seelexctx/frame.go`、`seelexctx/controller.go`、`seelexctx/replay.go` | Chapter 2 八小节骨架、`buildCompactFrame`、前缀重放厚摘要（`PrefixReplaySummarizer`） |
| 保护区窗口 | `seelexctx/window.go`、`application/core/task_context/plan_transcript.go` | 保留区候选取小、`TranscriptTailWindow`（按协议单元不截断、至少保 1 单元） |
| 事实源与回读 | `sessionstore/message_rows.go`、`seelexctx/search`、`application/core/history_search.go` | 消息分片 `message_<from>_<to>.jsonl`、帧当语义索引 + `[From..To]` 读回真实记录 |
| 模型 | `application/model` | `TokenAudit`（可加缓存命中观测）、`SessionRecord` |
| 四区显式化/打点（《已落地》1、4） | `application/core/context_runtime/layout.go` | `ContextLayout`、`ContextZone`、`RetainDecision`、`ContextZones`、`retainWindowDecision` |
| 保护区下限（《已落地》1） | `seelexctx/window.go`、`application/core/window.go` | `RetainedContextTokensWithFloor`、`RetainFloorTokens`、`ValidateRetainWindow` |
| 帧摘要传递上限（《已落地》2） | `seelexctx/frame.go` | `CarryPreviousChapter2`、`CarryDiagnostics`、`CarryEvidence`、`AnchorSourceWithCarry`、`LocalChapter2WithCarry` |
| 分片重放（《已落地》3） | `seelexctx/replay.go`、`seelexctx/dag.go` | `ChunkReplayMessages`、`SummarizeChunkPlan`、`ReplayChunkPlan`、`ReplayEvidence`、`CompactionDAGOptions.ReplayInputTokens` |

## 实现点

1. **装配顺序对齐**：主会话最终请求 = seelexctx assembler 的 project/stacks/memory/blocks + context_runtime 的 system/plan/checkpoint/tail 两层拼接。需调整为：system → project → memory → compact → 累积 context → plan → task → 输入。
2. **checkpoint 移出**：正常路径不再注入 `checkpointMessage`；`history_safety.go` 的恢复路径保留。
3. **累积 context**：`RetainedSystemHistory` 语义从“仅首条 system”扩展为“稳定前缀 + 已定稿轮次”；`TranscriptTailHistory` 的有界窗口改为达峰才压缩。
4. **plan/task 后置且不被压缩**：`RenderStackBlocks` 拆分——compact 前移，plan/task 移到尾部；压缩只折叠 compact 栈顶 + context 窗口。
5. **fork todolist 过滤**：`truncateForkRecord` 的 `Tasks` 过滤 `kind=todo`，子会话 todolist 全新。

**实现状态**：

1. 已实现：`seelexctx/assembler.go` 的 `Assemble` 投影（system → project →
   memory → 稳定前缀栈 skill/compact → WorkingHistory → 尾部栈 plan/task）；
   `context_runtime.fitExecutionHistory` 把 plan 尾部放在累积 context 之后。
   （其中 skill 前缀栈分支主会话无生产压栈 → 实际不出现，见《压缩四区模型》①。）
   补充（2026-09-24 复核）：system 侧**只**由 `prompt_layer` 追加插件级
   skill 目录段；激活 skill 正文走 transcript internal 事件（context 区），
   plan 政策走尾部 plan 上下文消息——即 system 的字节止于 skill 目录。
2. 已实现：`PrepareExecutionContextFor` 正常路径不再组装 `checkpointMessage`；
   `history_safety.go` 恢复路径保留 checkpoint（改用 `RetainedSystemOnly`）。
3. 已实现：`TranscriptTailHistory` `maxUnits<=0` = 全量累积（达峰前 append-only
   字节稳定）；`RetainedSystemHistory` 保留稳定前缀 + 已定稿轮次，下一轮只
   追加保留段之后的新事件；达峰（请求估算 ≥ `SoftThreshold`）才压缩并发布
   ContextCompactions。
4. 已实现：`RenderStablePrefixBlocks`（skill/compact）与 `RenderTailBlocks`
   （plan/task）拆分；框架侧 plan 尾部栈只带 plan_ref/title/status（整计划
   nodes 不入尾部栈）；应用侧另在上下文尾部注入**plan 上下文消息**
   （`context_runtime.planContextMessageLocked`：`plan_ref` + 节点投影
   `current/completed/failed/pending/current_slice` + `## Active Plan Execution
   Policy` 政策段，**同一条消息**），该消息是控制标记、不参与压缩单元切分，
   节点详情仍由 `read_plan` 提供。
5. 已实现：`forkTaskRecordsByTime` 过滤 `kind=todo`。

补充（2026-09-24 落地，原《待落地》1–4 项）：

6. 已实现：保护区下限 `context_retain_floor_percent`（`seelexctx/window.go` +
   `application/core/window.go`）；`floor > retain_tokens` 在 `main.initRuntime` 报错拒绝启动；
   比例类旋钮超界在 `seelexctx.LoadLimits` 报错。
7. 已实现：帧摘要传递上限 `context_frame_carry_tokens`
   （`seelexctx.CarryPreviousChapter2`，控制器 / DAG / 真空区三条路径共用）。
8. 已实现：replay 分片链（`seelexctx.ChunkReplayMessages` / `SummarizeChunkPlan`）；
   分片由 `CompactionDAGOptions.ReplayInputTokens` 触发，失败整条回退本地折叠。
9. 已实现：四区显式化（`application/core/context_runtime/layout.go` 的 `ContextLayout`）：
   判据量与报表（门禁 Detail、帧正文四区区块）读同一份 layout。

**打点落点汇总（"全部打点做完" 的清单）**：

| 事实 | 落点 | 形式 |
|---|---|---|
| 保留窗口决策（含下限） | 判据关 `compaction.progress` Detail + 帧正文四区区块 | `all= budget= cap= ratio= floor= retained= floor_applied=` |
| 四区（分区 + token 数 + 来源） | 装配关 Detail + 帧正文 `## Context zones (四区)` | `stable_prefix= folded= protected_window= tail= current_input=` 与逐区 `tokens/条数/来源` |
| 帧摘要传递 | `CompactFrame.Evidence` + `AnchorSource` + Chapter 2 正文 | `frame-carry:{kept\|anchor}:<carried>/<limit>`、`anchor_source=degraded`、降级说明 |
| 分片重放 | `CompactFrame.Evidence` + `SummarySource` | `replay-chunked:<n>`、`summary_source=replay`（未分片不留痕） |

**三门口禁（2026-09-24 全绿）**：

1. **真 API 冒烟**：`go test -tags compactlive . -run 'TestCompactLivePreflight|TestCompactLiveSmoke|TestPrefixChainRetainFloorLiveSmoke'`
   —— 前者证明既有压缩链路未被破坏，后者证明保护区下限端到端生效
   （baseline retained=1361/floor=0 → floored retained=16476/floor=16476，`floor_applied` 翻真）
   且四区与打点在真实 provider 下如实出现。
2. **数据竞争**：`go test -race ./seelexctx/... ./application/core/... ./application/event/... ./seelebridge/... .`
   —— 全绿。唯一例外 `TestWorkspaceSwitchConcurrentWithBackgroundPersist` 是**既有 flake**
   （Windows `TempDir` 清理与后台持久化抢跑；在改动前的基线上同样 2/8 复现），与本次改动无关，
   门禁运行时以 `-skip` 排除。
3. **pprof 锁竞争**：`seelexctx/contention_gate_test.go`
   （`TestPrefixChainLockContentionGate`）。A 臂（本次改动的纯函数）用户级锁竞争样本必须为
   零；B 臂（多会话共用一把压缩栈锁）profile 必须非空，且每个竞争样本的**阻塞点**不得落在
   本次改动的符号上（实测阻塞点只有既有的 `memoryCompactStack.Snapshot/PushCompact`）。
   profile 文本留档 `tmp/lock-contention-gate/` 供人工复核；不设竞争总量的绝对上界
   （mutex profile 取值依赖机器，钉绝对数字只会得到会抖动的假门禁）。

## 已落地与已决定不改（2026-09-24）

> 本节原名《待落地（目标设计）》。1–4 已按设计落地（先设计后编码，落地内容与设计一致、
> 未偏离）；5 是探测后的「不改」决定。两者都留在这里作为口径来源：代码是事实，本文是口径。
> 落地后的实现位置、**打点落点**与验证手段写在每条下面。

### 已实现（2026-09-24）

1. **保护区下限 floor**：新增旋钮 `limits.context_retain_floor_percent`（0 = 未配置），
   `floor = max(最近 1 个完整协议单元, 比例 × 预算)`；判定改为
   `retained = clamp(min(token1, token2), floor, token1)`，落在
   `seelexctx/window.go` 的 `RetainedContextTokensWithFloor` / `RetainFloorTokens`。
   非法组合 `floor > retain_tokens` 由 `WindowConfig.ValidateRetainWindow` **报错**，
   在 `main.initRuntime` 建起 Runtime 之后立刻校验（账号窗口已知，预算与判据同源）——
   拒绝启动，而不是静默取小；比例类旋钮超界（不在 [0,100]）也在 `seelexctx.LoadLimits` 报错
   （此前是 `percentOf` 静默回退默认值）。
   - **打点**：判据关 Detail 带 `all= budget= cap= ratio= floor= retained= floor_applied=`；
     `floor_applied` 只在**下限真的抬高结果**时为真（"配了却没生效"不报成生效）；
   - 验证：`seelexctx/window_floor_test.go`、`application/core/context_retain_floor_test.go`
     （base=1 轮 → floored=4 轮）、真 API 冒烟 `TestPrefixChainRetainFloorLiveSmoke`
     （baseline retained=1361 / floor=0 → floored retained=16476 / floor=16476，`floor_applied` 翻真）。

2. **帧摘要传递上限**：新增旋钮 `limits.context_frame_carry_tokens`（默认 1024），
   并入动作收敛到 `seelexctx.CarryPreviousChapter2`（控制器 / DAG / 真空区三条折叠路径共用）：
   并入量 ≤ 上限时原样并入；超限则退化为**锚点**（`segment_id` + request 首尾 + 一句话 +
   "正文为什么不见了"），细节靠 `search_history` / `read_compressed_turn` 回读。
   - **打点**：帧证据留一条 `frame-carry:{kept|anchor}:<carried>/<limit>`（`CarryEvidence`），
     降级时链锚标记写 `anchor_source=degraded`（`AnchorSourceWithCarry`）——读帧的人据此
     知道这一帧的正文不是上一帧原文，而是一段定位信息；
   - 验证：`seelexctx/frame_carry_test.go`（超限后大段正文不再并入、退化正文含定位信息与回读手段）。

3. **replay 分片链**：`seelexctx/replay.go` 新增 `ChunkReplayMessages`（按协议单元切片，
   **不切 message**；单单元自身超预算时独占一片）与 `SummarizeChunkPlan`（逐片重放、摘要
   前向传递：片 i>1 把上一片摘要拼进指令尾巴，不改摘要器契约）。片预算由
   `CompactionDAGOptions.ReplayInputTokens` 给出（≤0 = 不分片，走原有的单次重放以保住前缀
   缓存）；任何一片失败即整条退出，调用方回退本地确定性折叠，绝不中断请求。
   - **打点**：分片时帧证据留 `replay-chunked:<n>`（`ReplayEvidence`）+ `summary_source=replay`；
     未分片不留痕（避免"分片 1 片"这种无信息项）；
   - 验证：`seelexctx/replay_chunk_test.go`（片界不切工具链、片 2 携带片 1 摘要、
     片预算足够大时仍走单次重放、失败整条退出）；
   - 前提不变：生产未注入 `Summarizer`（字节级装配出口未固化，见《风险与约束》），
     因此分片链目前只在注入摘要器时可用——这是既有决定，不是本次改动引入的缺口。

4. **四区显式化**：`application/core/context_runtime/layout.go` 定义
   `ContextLayout{Zones, Retain, Compared/Estimated/Soft/Hard, Compacting}` 与
   `ContextZone{Kind, Tokens, Messages, Source}`（五格：① `stable_prefix` / ② `folded` /
   ③ `protected_window` / ④ `tail` / `current_input`）。**判据与报表读同一份**：判据量、
   保留窗口决策与分区在装配时**一次采样**写入 layout，门禁 Detail 与帧正文的四区区块都由
   它渲染——两处不可能各说各话。分区判据是消息自身的事实（role + 前缀标记），不做位置推算。
   - **打点**：装配关 Detail 带 `stable_prefix= folded= protected_window= tail= current_input=`
     的 token 数；帧正文新增 `## Context zones (四区)` 区块（分区 + 各区 token 数与来源 +
     判据 + 保留窗口）；
   - 口径边界（不粉饰）：本层只看得到引擎消息数组与引擎级 system prompt。框架装配器下游渲染的
     project / memory / compact 栈块会落在 ① 的位置，但它们的 token 不进本结构，因此 ① 是
     **下界**；要与真实请求估算对账请用 `EstimatedTokens`（同一计数器、含全量请求形状）；
   - 验证：`application/core/context_runtime/layout_test.go`、`application/core/context_retain_floor_test.go`
     （帧正文可回读四区块）、真 API 冒烟实测
     `assemble="assembled=18827 target=16476 … stable_prefix=8307 folded=3 protected_window=6810 tail=3 current_input=3"`。

### 已决定不改

5. **skill 内容与 plan 政策的装配位置：探针已跑，结论是「都不改」**（2026-09-24 探测）。
   本文这一版已按代码现状描述：system 止于插件级 skill 目录；激活 skill 正文是与
   tool / command 同生态位的 transcript internal 事件；plan 政策与 plan 同一条尾部消息。
   实测（用仓库自己的 `seelexctx/tokens.Count`；default 插件 / 典型 plan）：
   skill 目录段 **1227 字符 / 388 token**（10 个技能，插件级稳定）；plan 政策段
   **287 字符 / 85 token**；一条典型 plan payload（含 `current_slice`）**347 字符 / 119 token**。
   - **a) 激活 skill 正文不做 provider-only 前缀消息**：它本来就是 append-only 的
     **wire 材料**——`sessionstore/durable_history.go:205-218` `providerRoleForEvent` 对含
     `<!-- seelex:active-skill:` 的行保持 `user` 角色回放（不折叠成 system），
     `sessionstore/wire_assembler.go:266-280` 在 `WireMaterial` 为真时以 `user` 回放到
     wire（`Internal: true`）：激活后到被压缩前稳定命中缓存，resume 也能回放；
     做成「前缀消息」只能塞回 system 稳定区——正是刚从 system 移出的那件事。
   - **a-1) 注释措辞已修（2026-09-24）**：`ActiveSkillMarker` 的注释原写「事件**不落盘**」，
     与 sessionstore 侧 wire material 落盘/回放相矛盾。现按三问改写：
     「**落盘**（wire material，resume 回放、检索回读）／不进可见会话与「用户输入」视图／
     **会被压缩裁掉**」；`context_runtime/coordinator.go` 的 `ActiveSkillPrefix` 注释同步补上
     落盘与裁剪说明。`sessionstore/durable_history.go` 的 `providerRoleForEvent` 注释无歧义，未动。
   - **a-2) 缺口处理：定为 (iii) 接受现状（2026-09-24 决定）**。事实链：技能事件是独立协议
     单元（`plan_transcript.go` 的 `transcriptProtocolUnitList` 分支），压缩窗口裁剪后正文从 wire
     消失；同一任务内再次 `skill_activate` **不会**补写（去重基准 `state.ActiveSkills`，
     `task_context_state.go:55`），只有「运行态由投影重建」路径强制补写（同文件 `:948`，`logged=nil`）。
     接受理由：正文以 wire material 落盘，`search_history`／会话存档可回读原文，压缩帧摘要也带锚点；
     而备选 (i) 把活跃技能正文钉在**尾部区**要每轮重发正文 token、(ii) 压缩时强制补写会扰动压缩
     窗口的字节稳定性——两者都为长尾场景付常驻成本。**触发重议的条件（任一出现即重开）**：
     长任务里检索回读实际失败，或出现「技能已激活、正文已不在上下文、执行走样」的实测案例。
   - **b) plan 政策不拆块**：拆分只在「payload 变、政策不变」的轮次多命中 85 token
     （≈ 0.05% 预算），代价是多一条消息 + 顺序约束（政策必须排在 payload 之前）；
     同一条消息还保证「状态与纪律同生共死」，不会出现半截上下文。
   - **c) 插件级 skill 目录不再收窄**：388 token/请求是固定开销（占预算 0.22%），只在
     `switch_plugin`／`plugins_reload`／技能表变化时改字节，且位于 system 尾部、仍在前缀
     覆盖内；去掉 description 会牺牲「零调用发现」（`skill_catalog.go` 注释：发现不泄全文）。

## 风险与约束

- 压缩 = 整条前缀失效一次（可接受，低频）。
- 无 provider 前缀缓存时累积 context 每轮全量重发（本方案默认 All-in；若实测成本异常再回退）。
- 累积 context 稀释中段注意力（可接受；plan/task 在尾部保焦点）。
- 恢复路径（504 / history-safety）必须保留 checkpoint 消息。
- plan/task 尾部内容克制：plan_ref + 当前节点 slice。
