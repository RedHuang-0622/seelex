# Agent Harness 调研报告：主流上下文策略 / A2A / Subagent 派发，与 Seelex 现状分析

> 日期：2026-08-02
> Seelex 实现核对：2026-08-03。原调研中的“merger 未接线”和本地 `go.work` 描述已经过时，以下现状已按当前远程 Seele 依赖与 production merge-back 链路修订。
> 范围：① 主流 agent harness（LangGraph / OpenAI Agents SDK / Claude Code·Claude Agent SDK / AutoGen / CrewAI / smolagents 系 / A2A 协议）的上下文策略、A2A、subagent 派发机制调研；
> ② Seelex 项目（`seelebridge` + `seelexctx` + `session/sessionstore` + Seele `workplan`/`seelectx`）agent harness 的实现剖析与可优化空间。
> 结论速览见文末「四、Seelex 优化空间（分级）」与「五、建议路线图」。

---

## 一、主流 Agent Harness 调研

### 1.1 一览表

| 框架/产品 | 上下文策略 | A2A/多智能体 | Subagent 派发 |
|---|---|---|---|
| LangGraph (LangChain) | checkpoint 持久化 + trim / delete / summarize / 自定义策略；Store 做跨会话记忆 | Supervisor / Swarm / 分层多 agent 模式；subgraph 隔离；Send API 扇出 | 节点即 agent，共享 state 或子图隔离；工具调用式 supervisor 路由 |
| OpenAI Agents SDK | 服务端 Session 记忆（每次 run 前取历史、run 后回存），无需手动拼 input | Handoff（移交控制权）；多 agent 协作 | `Agent.as_tool` 把子 agent 作为工具派出；子 agent 独立运行 |
| Claude Code / Claude Agent SDK | 自动压缩（auto-compact 提前到 ~64-75% 触发）+ completion buffer；CLAUDE.md 持久上下文；context editing + memory tool | 主 agent 派生子 agent；subagents 独立上下文窗口 | Task 工具/子 agent：每个子代理有独立上下文，主代理发 prompt + 必要上下文，结果摘要回传 |
| AutoGen | 会话式消息传递；可教 agent 外部知识存储 | GroupChat + GroupChatManager（LLM 选发言者），协商式涌现协调 | ConversableAgent 之间消息驱动协作，无正式任务委派 |
| CrewAI | 内建 short-term / long-term / entity memory | Crew 角色编排，sequential / hierarchical（manager 委派+评审） | 任务显式指派给特定 agent；任务依赖与输出流转 |
| Google A2A 协议 | —（协议层：任务状态、消息、artifact、技能发现） | **跨组织 agent 通信标准**（Linux Foundation，150+ 组织） | Agent Card 暴露能力；Task 生命周期 + Message 流式交互 |
| MCP（对照） | — | agent↔工具/数据的连接协议，与 A2A 互补 | — |

（LangGraph/OpenAI/AutoGen/CrewAI 行为基于 2026 年公开文档与对比文章；A2A 基于 Linux Foundation 官方发布与安全分析。）

### 1.2 上下文管理策略（主流共识）

1. **窗口内不动、窗口外压缩**：只对滑出窗口的轮次做摘要，窗口内轮次原样保留（Claude Code auto-compact、LangGraph summarize、Seelex 同款语义）。
2. **压缩触发提前 + 完成缓冲**：Claude Code 从「90%+ 才压缩」改为 ~64-75% 触发，并预留 completion buffer 让当前任务能收尾——研究显示上下文接近占满是导致推理质量下降的主因；压缩本身要防「压缩循环/上下文损坏」等故障（Claude Code 曾出现 8-12% 剩余空间频繁打断、压缩后永久卡 102% 的 bug）。
3. **持久上下文外置**：把稳定信息（CLAUDE.md / CLAUDE.local.md、项目知识）从对话窗口挪到注入块，避免与易变对话争抢空间。
4. **服务端会话记忆**：OpenAI Agents SDK 的 Session 机制——历史由框架自动管理（run 前注入、run 后回存、可自定义 merge 顺序），agent 本身无状态；LangGraph 用 checkpoint 落盘实现断点恢复与多会话。
5. **工具结果外化**：超大工具输出不进对话（截断/归档 + 引用），模型只看到省略标记与按需读取句柄（OpenAI artifact、LangGraph trim、Seelex result_ref 同族）。
6. **记忆分层**：短期（窗口/checkpoint）与长期（跨会话 semantic memory：CrewAI 三态记忆、Mem0/Letta 式记忆服务、Claude memory tool）分离。

### 1.3 A2A（Agent-to-Agent）

- **协议本体**：A2A Protocol 由 Google 提出、2025 年移交 Linux Foundation（与 MCP 同基金会），2025.1 版本新增流式支持、任务状态管理增强、安全机制改进；已获 150+ 组织采用并进入主流云平台。
- **核心对象**：`Agent Card`（name/url/skills/authentication 等能力声明）、`Task`（服务端执行单元，状态机 input-required/working/completed/failed 等）、`Message`（原子通信单元，携带 context id）、`Artifact`（任务产物的终端状态）。
- **A2A vs MCP**：A2A 定义「agent 与 agent 之间」跨组织通信协调；MCP 定义「agent 与内部工具/数据」连接；两者互补，共同构成互操作底座。
- **框架侧 A2A 形态**：OpenAI 用 Handoff（运行内移交）；AutoGen 用 GroupChat 协商；CrewAI 用层级 Crew；LangGraph 用 Supervisor/Swarm 图模式。这些多是**进程内/单运行时**的「A2A-like 编排」，真正实现**跨进程、跨组织、协议化** A2A 的仍以 A2A Protocol 为主。

### 1.4 Subagent 派发的主流设计

1. **独立上下文窗口**：子代理拥有自己的会话/上下文，父代理不继承子代理内部过程（Anthropic subagents、OpenAI sub-agents、Seelex node session 同款）。
2. **父证据注入**：派发时把父上下文压缩为结构化/摘要块传给子代理（goal、decisions、findings、constraints、pending work 等），保证子代理「知道要干什么、父已知什么」。
3. **显式预算**：子代理配 MaxLoops / MaxOutputTokens / token budget / 工具过滤（tool filter），防止子代理失控与资源耗尽。
4. **结果回传与合并**：子代理产出（文本 + 结构化字段）经 merge-back 合并回父上下文（append findings/decisions、去重 constraints、替换 progress）。
5. **并发与可见性**：并行分支受并发上限约束（LangGraph Send、forkexec semaphore）；生命周期事件（started/completed/failed/canceled）上抛到 UI（Claude Code Agent View / workflows 树、Seelex 规划的 SubAgentTree）。

---

## 二、Seelex 项目 Agent Harness 现状

### 2.1 架构总览

```
┌─ Seelex（产品）──────────────────────────────────────────────┐
│ application/   用例层：service、effort、task、窗口/预算策略   │
│ seelebridge/   Anti-Corruption Layer + 组合根 Runtime：       │
│                主会话/节点会话、agent factory、账号池、MCP、   │
│                plan 内核接线（agent_node/plan_*/branch/       │
│                node_scope/context_components）                │
│ seelexctx/     上下文领域：assembler/processor/compressor/    │
│                controller/window/history_safety +            │
│                snapshot/provider/compactor/merger             │
│ session+sessionstore  会话持久化：durable history / event     │
│                store / 4 栈状态 blob（plan/task/skill/compact）│
│ skill/ plugin/ mcpstack/ tui/ gui/                           │
└───────────────────────────────────────────────────────────────┘
┌─ Seele（框架，Go module 远程版本依赖）────────────────────────┐
│ agent/ session/ seelectx(react/ctx_manager/compressor/       │
│   storage/tracer)  workplan(codec/core/runtime{scheduler,    │
│   executor, runner, forkexec, checkpoint}/sugar)             │
└───────────────────────────────────────────────────────────────┘
```

### 2.2 上下文管理（实现要点）

| 环节 | 实现 | 关键参数（证据） |
|---|---|---|
| Token 估算 | `seelectx.EstimateTokens`（保守 len/3），`ConservativeTokenCounter` | `controller.go` CountText = (len+2)/3 |
| 请求装配 | `seelexAssembler` 固定投影序：system prompt（effort/skill 动态、不落盘）→ project 块（ProjectKnowledge 预读）→ 栈块（plan/task/skill/compact 栈顶 = now using）→ 调用方静态块 → WorkingHistory（窗口轮次） | `seelexctx/assembler.go` |
| 滑动窗口 | `N = clamp((ContextTokens×ratio − Reserved) ÷ AvgRoundTokens, 4, 40)`；决策序：显式 rounds > provider 推导 > 保守回退 | `window.go`，默认 ratio 0.7（seele.yaml 注释 + DefaultWindowConfig） |
| 阈值/预算 | budget = context − outputReserve − safety(context/8)；软阈值 75%、硬阈值 90%、压缩目标 60%；**只压缩窗口外轮次**，窗口内永不压缩 | `controller.go` |
| 压缩 | 三级：① 短历史(<6 条)免 LLM 直通；② 跨会话快照经 compactor 按 token 预算压缩（全量 ≥500 / 摘要 200-499 / 极简 <200，保留最小安全快照，预算不足报错回退）；③ QuickChat 无工具隔离递归摘要；压缩帧 push CompactStack，原文经 TurnArchiver 归档 → `read_compressed_turn` 可逆读回 | `compressor.go` / `compactor/compactor.go` / `processor.go` |
| 工具结果外化 | 超大结果（> 工具结果字符预算，seelex 默认 20000 字符，可经 seelex.yaml `limits.max_tool_result_chars` 覆盖）→ 归档为 result_ref → 模型只见省略警告 + `read_tool_result`；错误结果原样透传 | `processor.go` / `limits.go`（`DefaultToolResultLimit`；框架 `ctx_manager` 默认约 4000 不再兜底） |
| 历史安全 | ReplaceHistory 前清理 checkpoint/压缩帧控制标记 + 空正文配对修复（assistant+tool_calls / 纯 reasoning / 中断恢复） | `history_safety.go` |
| 持久化 | `DurableHistory`（Chat 前 Load / 后 Save）；`EventStore` 追加式执行事实日志；`SessionContextStore` 状态 blob（SystemPrompt 永不压缩 + 4 栈） | `sessionstore/durable_history.go` / `event_store.go` / `session_context.go` |

**评价**：上下文管理是 Seelex 的强项——滑动窗口 + 阈值 + 可逆压缩 + 工具结果外化 + 栈式"now using"上下文，整体设计已对齐 Claude Code / LangGraph 的主流思路，且压缩可逆（比多数框架强）。

### 2.3 多智能体 / A2A-like（实现要点）

1. **Plan DAG 即派发图**：`plan_load` 规范 JSON `{entry, nodes:{id:{input,kind}}, edges}`；节点类型 `agent`（子代理）/ `approve|manual`（审批门）/ `auto|function|verify|deliver`（确定性，输出=input）。稳定拓扑排序 + 环检测（Kahn）。
   - `seelebridge/plan_factory.go`、`plan.go`
2. **子代理执行 = SeelexAgentNode**：包装 `bridge.NewAgentFactory` 产物；`Run()` 时把 **NodeScope**（NodeID/Role/BranchID/WorkspaceID）与**节点级 PromptBlocks**（node-goal / parent-evidence / node-budget）注入 ctx，再 `NewAgent` 起**独立 Session**（历史隔离）执行。
   - `seelebridge/agent_node.go`、`node_scope.go`
3. **父证据承袭与回传**：父上下文经 provider 导出为结构化 `ContextSnapshot`（goal/parent goal/decisions/findings/progress/constraints/pending/escape），经 compactor 按预算压缩后作为 evidence 块注入；子结果按 merger 语义（findings/decisions append、progress 替换、constraints 去重、token 累加）合并后，通过 `ContextExchanger` 投递到主会话 mailbox。
   - `seelexctx/provider/engine.go`、`compactor/compactor.go`、`merger/merger.go`
4. **并发/隔离**：Seele `forkexec.ForkCoordinator`——信号量限流（`SetMaxForkConcurrency` 默认 3，≤0 时兜底 3）、fail-fast/best-effort 策略、join require_all/successful、panic 恢复、确定性排序、冻结 ParentSnapshot 克隆隔离分支上下文、生命周期事件（queued/started/completed/failed/canceled/panicked）。
   - Seele v0.1.1 `workplan/runtime/forkexec/forkexec.go`、`scheduler/scheduler.go`
5. **角色/账号路由**：entry 节点 → PrimaryRole，`_` 前缀 → goal-plan 角色，其余 → sub-agent 角色；账号按 role+branch 确定性 FNV hash 选择（可显式 pin），不占用主链路租约；节点会话 ID 由 system prompt 稳定 hash 派生（跨 plan_run 可复现）。
   - `seelebridge/branch.go`
6. **预算**：节点子代理 MaxLoops 15（主会话 25）、MaxOutputTokens 取账号限额；渲染为 node-budget 块并写入节点 SessionConfig。
   - `agent_node.go`

### 2.4 与主流的对照（差距一览）

| 能力 | 主流 | Seelex | 差距 |
|---|---|---|---|
| 滑动窗口 + 摘要压缩 | ✅ | ✅ 同款且可逆读回 | — |
| 压缩触发策略 | 提前触发 + completion buffer | 75%/90% 阈值，无 completion buffer 概念 | 小 |
| 跨会话长期记忆 | OpenAI sessions / LangGraph Store / CLAUDE.md | 仅 ProjectKnowledge 块 | **中-大** |
| 语义检索旧上下文 | 部分框架 embedding/RAG | 仅 ref 手工 `read_compressed_turn` | 中 |
| 工具结果外化 | ✅ | ✅ result_ref | — |
| 子代理独立上下文 | ✅ | ✅ 节点独立 Session | — |
| 父证据注入 | ✅ | ✅ 结构化 snapshot + 预算压缩 | — |
| 子结果 merge-back | ✅ 已接线 | ✅ 结构化合并后经 mailbox 回传 | 小（补展示与时序验证） |
| 并发派发 | ✅ | ✅ forkexec（上限默认 3） | 上限硬编码在框架 |
| 可见性/生命周期事件 | Claude Code Agent View / workflows | forkexec 事件已有，TUI/GUI 子代理树未接通 | 中 |
| 跨进程协议化 A2A | A2A Protocol / MCP 生态 | **未实现**（项目自评"主 Agent 编排多个模型节点"） | **大** |

---

## 三、Seelex 优化空间（分级）

### P0 —— 正确性 / 可靠性

1. **验证 merge-back 的展示一致性与注入时序**
   现状：`SeelexAgentNode` 已在生产路径导出子快照、执行 `merger.MergeBack`，并通过 `ContextExchanger` mailbox 回传，避免 `plan_run` 锁内反向写 Engine。下一步应验证：合并块在同轮/下一轮的模型可见性、TUI/GUI 可追溯性，以及多个并行子代理的去重与来源标识。

2. **并行分支的上下文隔离做一次并发评审**
   2026-07-27 审查指出"共享上下文存在并发写风险、分支失败可能被吞掉"；forkexec 已用 ParentSnapshot 克隆 + NodeScope ctx 注入缓解，但需验证：并行子代理对同一 `SessionContextStore`（4 栈）/ProjectKnowledge/账号池租约的读写是否真正互不串台；建议补并发 plan_run 的 race 测试（已有 `race_test.go`/`subagent_audit_test.go` 可扩展）。

3. **子代理执行可见性接通**
   `docs/arch/subagent-visibility-design.md` 规划的 SubAgentTree + `subagent.started/completed/tool_call` 事件未完成；forkexec 已发出生命周期事件，建议在 application 层建 SubAgentTree、TUI/GUI 树形渲染，并把节点 token 消耗/耗时/工具调用列表展示出来（对齐 Claude Code Agent View）。

### P1 —— 对齐主流

4. **真正的 A2A 协议适配（或先标准化内部 A2A API）**
   项目自评"完整 A2A 尚未实现；当前更接近主 Agent 编排多个模型节点"。建议二选一或分两步：① 先把内部"派发-执行-回传"抽象为 A2A 形状的接口（AgentCard/Task/Message/Artifact 语义映射），让 plan 节点、未来的远程 agent、MCP 工具在统一调用模型下工作；② 再按 A2A Protocol 2025.1 加 HTTP/SSE 传输层与 agent card 发布，实现跨进程/跨组织互操作（与 MCP 互补，不冲突）。

5. **跨会话长期记忆**
   目前唯一"跨会话"信息是 ProjectKnowledge 模块语义块（会话前预读）。建议引入会话级事实/决策/教训的持久化记忆（近似 CrewAI 长记忆 / Mem0 / Claude memory tool）：结构化快照（decisions/findings/constraints）按 session 落库，新会话按相关性注入，天然复用现有 `ContextSnapshot` 字段。

6. **压缩预算闭环 + 完成缓冲**
   压缩后没有对实际 provider token 的二次校验（compactor 用 len/3 估算，压缩目标 60% 是预算侧计算）。建议：压缩帧落盘时记录压缩前后估算与实际计数；参考 Claude Code 在软阈值（~75%）触发而非硬顶，并预留 completion buffer，降低"上下文爆顶打断"与"压缩循环"风险（压缩失败已有 QuickChat 回退，但缺少显式失败恢复路径审计）。

7. **压缩历史语义检索**
   `read_compressed_turn` 是"知道 ref 才能读"的拉取式。可加：压缩帧 Summary + Evidence 摘要的检索接口（简单全文/关键词 → 稳定 ID 定位），为未来 embedding 检索留好接口，减少模型幻觉与重复劳动。

### P2 —— 工程化 / 体验

8. **双轨上下文实现收敛**
   `application/core` 仍保留旧 context_controller/window_policy/token_counter/history_safety/skill_context 等，与新的 `seelexctx`/`seelebridge.context_components` 功能重叠（2026-08-01 重构进行中）。建议完成迁移后删除旧轨，或在过渡期加一致性测试护栏，避免两套阈值/窗口策略漂移。

9. **并发上限与窗口参数配置化**
   `SetMaxForkConcurrency` 默认 3 硬编码在 Seele scheduler，Seelex 侧未见接线；`window` 配置段在 seele.yaml 中仍是注释示例。建议在 `RuntimeConfig`/`seele.yaml` 暴露 `max_fork_concurrency`、窗口参数（rounds/ratio/min/max），按账号/effort 差异化。

10. **Token 估算升级**
    len/3 保守估算在中文/工具参数混合场景误差大。建议接入模型感知 tokenizer（tiktoken 或 provider 计数接口），至少对窗口决策与压缩预算使用同源计数，避免"估算说够、实际爆顶"。

11. **快照字段稳定 ID**
    merger 的 constraints 去重按文本、findings 无 ID，README 已标注"是否需要未来稳定 ID"。建议为 decisions/findings/constraints 加稳定 ID（如内容 hash 或生成 ID），使跨会话/跨分支合并可增量、可去重、可溯源。

12. **评审/过程文档沉淀**
    `docs/` 下大量 dated 审查与方案（2026-07-23 ~ 08-01），关键结论（如本报告 P0-2、A2A 自评）散落其中。建议把已验证的优化项收敛进 `docs/arch/` 或 DESIGN.md，形成单一事实源。

---

## 四、建议路线图

1. **短期（1-2 周）**：merge-back 展示/时序测试 + 并发评审与 race 测试 + 子代理事件/树形可见性。
2. **中期（1 月内）**：P1-4 内部 A2A 形状 API + P1-5 跨会话记忆 + P1-6 压缩预算闭环/完成缓冲。
3. **长期**：P1-4b A2A 协议传输层（跨进程互操作）、P1-7 语义检索、P2 工程收敛（双轨删除、参数配置化、token 估算、稳定 ID）。

---

## 附：主要证据索引

- 上下文：`seelexctx/assembler.go`、`window.go`、`controller.go`、`compressor.go`、`processor.go`、`history_safety.go`、`seele.go`
- 快照/压缩/合并：`seelexctx/snapshot/snapshot.go`、`provider/{provider,engine,trace}.go`、`compactor/compactor.go`、`merger/merger.go`
- Harness 接线：`seelebridge/{runtime,context_components,agent_node,node_scope,branch,plan,plan_factory}.go`
- 持久化：`sessionstore/{durable_history,event_store,session_context,sessionstore}.go`
- 框架：Seele v0.1.1 `workplan/runtime/{scheduler,scheduler.go,forkexec/forkexec.go}`、`workplan/core/node/agent_node.go`、`seelectx/{react/strategy.go,ctx_manager/config.go}`
- 项目自评：`docs/2026-07-27-workplan-e2e-a2a-review/finish-review.md`（并行 Plan 与完整 A2A 不通过生产审查）、`docs/arch/subagent-visibility-design.md`、`docs/arch/context-improvement-plan.md`
- 外部：A2A Protocol（Linux Foundation 2025.1，150+ 组织；Semgrep 安全指南）、LangGraph memory/supervisor 文档、OpenAI Agents SDK sessions 文档、Claude Code 上下文管理分析（auto-compact 提前、CLAUDE.md、子代理隔离）、CrewAI vs AutoGen 对比。
