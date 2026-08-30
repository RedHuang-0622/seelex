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

实现细节：激活 skill 的栈帧作为稳定前缀栈之一渲染在 memory 之后、compact
之前（system prompt 内另有 TrustedSkillLayers）；compact 保持紧邻 context。

- **system**：`prompt_layer`（identity/plugin/effort/instructions + 激活 skill + plan 执行策略）。永久不变。
- **project**：`RenderProjectBlock`（项目模块语义，会话前预读）。除非毁灭性重构不变。
- **memory**：`relatedMemoryBlocks`（按当前查询从 CompactStack top-K）。项目粒度、跨会话；除非艰难纠错不变。
- **compact**：compact 栈帧（已压缩历史摘要 + evidence）。除非手动压缩/达峰不变。
- **context（累积）**：已定稿完整轮次，append-only；每次请求只追加新轮，旧轮字节不变 → 缓存前缀主体。
- **plan / task**：频繁变化、**不参与压缩**、后置（贴近输入）。
- checkpoint：不入 LLM 上下文（正常路径）；checkpoint 消息只保留恢复路径
  （provider 504 / history-safety，`RetainedSystemOnly` + 恢复信封）。plan
  尾部消息（`plan_ref` + 当前节点 slice）在正常路径注入，贴近当前输入。

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
