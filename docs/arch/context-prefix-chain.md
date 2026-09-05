# 上下文前缀链路（Context Prefix Chain）——已采用，已实现

> 状态：**已实现（任务 A/B/C/D 落地，2026-08-30）**。本文描述当前代码事实；
> 装配顺序、checkpoint 正常路径移出、达峰才压缩、plan/task 后置与 fork
> todolist 过滤均已按「实现点」落地；若后续调整，先改本文再改代码。

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

实现细节：compact 帧作为稳定前缀块渲染在 memory 之后、紧邻 context；
激活 skill 的内容不进 seelexctx 文本栈帧，而是由 prompt_layer 按活跃任务
追加到 system 文本尾部（Trusted Active Skill 段）。seelexctx 稳定前缀栈
虽有 SkillStack 顶层帧渲染器，但当前主会话无生产压栈。tools 走请求级 API
通道，不在文本前缀（skill/tools 的精确落点见下节）。

- **system**：`prompt_layer`（identity/plugin/effort/instructions 基础四层
  稳定；活跃任务另在 system 文本尾部追加激活 skill 段与 plan 执行策略段——
  task 级、同一任务内字节稳定，任务/技能切换才失效一次）。
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

文本轨道（消息序列，前缀缓存友好；system 内部顺序 identity → plugin →
effort → instructions → skill 目录(被动,插件级 C) → 激活 skill 内容 → plan 政策）：
┌system──────────────────────┬project┬memory┬compact┬─context(已定稿累积)─┬plan/task 尾部┬当前输入┐
│ identity        (C)        │  (C)   │ (≈C)  │  (C)   │  append-only 达峰  │ 每轮重建 (S)  │  (S)   │
│ plugin          (C)        │        │       │        │  才压缩一次 (C)     │ 贴近当前输入   │        │
│ effort          (C)        │        │       │        │                    │ 不参与压缩     │        │
│ instructions    (C)        │        │       │        │                    │               │        │
│ ◆ skill 目录  (插件级 C)   │        │       │        │                    │               │        │
│ ◆ skill 内容  (task 级 C)  │        │       │        │                    │               │        │
│ ◆ plan 政策    (plan 级 C) │        │       │        │                    │               │        │
└──▲─────────────────────────┴────────┴───────┴────────┴────────────────────┴───────────────┴────────┘
   └ 装配点 S1b（被动目录，零调用）：prompt_layer.SystemPromptForActiveTaskLockedFor 在
     base 后插入 "## Available Skills" 段（skill_catalog.go RenderSkillCatalog）；
     数据源 contract.Skills.All()（skill.Registry 按 activePlugin 隔离 → adapters.SkillPort），
     随插件 Load/Activate 自动可见——模型无需先调 skills_list（降低插件自主性要求）。
     只含 name/description，永不携带指令正文。
   └ 装配点 S2（内容注入）：#<skill> → PromptStack(kind=skill) → chatRequest.skills →
     task_context.TrustedSkillLayers → prompt_layer "## Trusted Active Skill:"（system 尾部）
     模型侧激活：skill_activate 工具（main.go）→ application.Service.ActivateSkill
     （同一 promptStack 压栈）→ 本轮工具结果即时回传技能正文、下一轮提交自动进 S2。
     装配点 S3（休眠）：seelexctx SkillStack 顶层帧渲染器（已接线 PrefixStacks，
                    主会话无生产 PushSkill 压栈 → 实际不出现）

API 轨道（请求级 schema 通道；每轮经可见性过滤后全量下发，不进消息文本、不参与压缩）：
┌ tools: [read_file, grep_search, bash, …, plan_load, task_check_node, …, fork_subagents] ┐
└── 装配点 T2：registry → Policy.Filter（子代理排除 + goal skill 门 + 插件 include/exclude）
            → agent bridge RegistryRuntime.VisibleTools → Seele session 每轮
            AssemblyRequest.Tools → seelexctx.Assemble 透传 → provider API ─────────────┘
```

图例说明：**可见 skill 列表（哪些可 # 激活）与 tools 列表一样由 plugins 装配决定**
——来源 `plugins/<plugin>/<skill>/SKILL.md`，经 plugin.Manager 发布到
skill.Registry 并按 `activePlugin` 隔离（S0/S1，装配位置在 plugins 系统，见下表）；
**激活后注入文本的 skill 内容**才是 system 尾部 task 级段（S2）；另有一段
**被动目录（S1b，## Available Skills）**随插件装配自动进 system（base 后、
激活内容前），只含 name/description、零调用即发现，正文仍仅在激活后注入。
tools 列表走请求级 API schema（T0→T2），不在文本前缀；可见性集合变化 →
请求级变更。

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
- 应用侧预算面（非注入）：`application/core/context_runtime/coordinator.go:139`
  `VisibleTools(ctx)` → `CountRequestTokens`（估算 prompt tokens）。

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
  → `prompt_layer.SystemPromptForActiveTaskLockedFor` 渲染为 system 尾部
  `## Trusted Active Skill: <name>` 段，内容不落 durable history。
- `seelexctx.RenderStablePrefixBlocks` 的 SkillStack 顶层帧渲染器已接线
  `PrefixStacks`，但主会话无生产 `PushSkill` 压栈（仅测试/恢复拷贝）→ 该帧
  实际不出现；节点子代理另经 `seelebridge/node` 注入 skill 目录块 + 匹配
  激活块（kind:agent 范围），并继承主代理稳定块。

> 结论：**skill 列表的装配位置在 plugins 系统**（S0 loader 扫描插件技能目录
> → S1 plugin.Manager 发布/激活 + registry activePlugin 隔离 → S2 用户
> `#<skill>` 激活后内容才注入 system 尾部）；tools 列表的装配位置是注册表 +
> 插件 include/exclude + goal skill 门（T0→T2，API schema 通道）。

### 压缩（折叠 compact 栈顶 + context 窗口）

```text
同一任务连续轮次（context 逐轮追加）：
├system(C)┼project(C)┼memory(C)┼compact(C)┼─context(轮1..N-1 已定稿,C)─┼plan(S)┼task(S)┼输入(S)┤
   ↑ 稳定段 + 已定稿 context 全命中；每轮只重发 plan/task/新输入

context 达峰 → 压缩（折叠 compact 栈顶 + context 窗口，保留新鲜 compact 帧 + 窗口剩余）：
├system(C)┼project(C)┼memory(C)┼compact(+新帧,S)┼context(重置,新起点,C)┼plan(S)┼task(S)┼输入(S)┤
   ↑ 压缩即整条前缀失效一次；之后 context 重新累积，恢复长命中
```

实现落点：应用侧软阈值（75% 预算）触发压缩——发布
`Snapshot.Task.ContextCompactions` 并把 context 切换为有界新鲜窗口
（≤ `limits.context_max_units` 轮，默认 4）；框架侧 `ContextController` 把
窗口外轮次折叠进 CompactStack（保留窗口剩余 + compact 帧标记）。plan/task
尾部消息是控制标记（`<!-- seelex:active-plan:v1 -->`），不参与轮次单元切分，
即不参与压缩。

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

## 数据结构与涉及文件

| 层 | 文件 | 数据结构/函数 |
|---|---|---|
| 应用装配 | `application/core/context_runtime/coordinator.go` | `prepareExecutionContext`、`fitExecutionHistory`、`RetainedSystemHistory`、`planContextMessageLocked`、`checkpointContextMessage`、`TranscriptTailHistory` |
| 框架装配 | `seelexctx/assembler.go` | `AssemblerOptions{SystemPrompt, ProjectBlock, StackBlocks, Memories}`、`Assemble` 投影顺序、`RenderStackBlocks`（plan/task/skill/compact） |
| 运行时接线 | `seelebridge/runtime_context.go` | `seelexAssembler`、`relatedMemoryBlocks`、`stackBlocks`、`projectBlock` |
| 系统提示 | `application/core/prompt_layer/coordinator.go` | `SystemPromptForActiveTaskLockedFor` |
| 会话上下文 | `sessionstore.SessionContextRecord` | `PlanStack` / `TaskStack` / `SkillStack` / `CompactStack` |
| Fork | `application/core/session_runtime/fork.go` | `truncateForkRecord`（过滤 `kind=todo` 的 Tasks） |
| 模型 | `application/model` | `TokenAudit`（可加缓存命中观测）、`SessionRecord` |

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
2. 已实现：`PrepareExecutionContextFor` 正常路径不再组装 `checkpointMessage`；
   `history_safety.go` 恢复路径保留 checkpoint（改用 `RetainedSystemOnly`）。
3. 已实现：`TranscriptTailHistory` `maxUnits<=0` = 全量累积（达峰前 append-only
   字节稳定）；`RetainedSystemHistory` 保留稳定前缀 + 已定稿轮次，下一轮只
   追加保留段之后的新事件；达峰（软阈值）才压缩并发布 ContextCompactions。
4. 已实现：`RenderStablePrefixBlocks`（skill/compact）与 `RenderTailBlocks`
   （plan/task）拆分；plan 尾部只带 plan_ref/title/status（整计划 nodes 不
   入尾部，节点详情由 plan 尾部消息与 `read_plan` 提供）；plan 尾部消息是
   控制标记，不参与压缩单元切分。
5. 已实现：`forkTaskRecordsByTime` 过滤 `kind=todo`。

## 风险与约束

- 压缩 = 整条前缀失效一次（可接受，低频）。
- 无 provider 前缀缓存时累积 context 每轮全量重发（本方案默认 All-in；若实测成本异常再回退）。
- 累积 context 稀释中段注意力（可接受；plan/task 在尾部保焦点）。
- 恢复路径（504 / history-safety）必须保留 checkpoint 消息。
- plan/task 尾部内容克制：plan_ref + 当前节点 slice。
