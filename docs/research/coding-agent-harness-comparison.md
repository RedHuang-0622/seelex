# 主流 Coding Agent Harness 对比与 Seelex 差距分析

> 调研日期：2026-08-02
> 范围：以**产品形态的 coding agent**（Claude Code / OpenAI Codex / Gemini CLI / Cursor / Aider / OpenHands / Cline / GitHub Copilot coding agent / Windsurf / Devin）为主，聚焦 **harness**（LLM 之外的运行时基础设施：agent 循环、上下文、权限、沙箱、计划、子代理、持久化、记忆、MCP、遥测），并与 Seelex 的 harness 逐项对照。
> 关联文档：`docs/research/agent-harness-research-report.md`（已覆盖 LangGraph / OpenAI Agents SDK / AutoGen / CrewAI / smolagents / A2A 等**框架/协议**侧；本文档补充**产品侧**）。
> 结论速览见「六、差距矩阵」「七、差距分级与路线图」。

---

## 一、Harness 的定义（本文视角）

Harness = 模型之外、支撑「模型能够自主完成编码任务」的运行时骨架，至少包括：

1. **Agent 循环**：ReAct / plan-execute / event-stream 等主循环与循环预算；
2. **上下文管理**：窗口、压缩、摘要、工具结果外化、token 估算；
3. **记忆**：项目/会话/跨会话长期记忆（CLAUDE.md、repo map、embedding 检索等）；
4. **权限与审批**：allow/ask/deny 策略、审批流、hooks；
5. **沙箱与执行隔离**：OS 级 sandbox、容器、VM、路径门禁；
6. **计划与任务编排**：plan mode、DAG、任务生命周期；
7. **子代理 / 多智能体**：独立上下文、派发、证据回传、并发；
8. **会话持久化与检查点**：resume/fork/rewind/undo；
9. **生态互操作**：MCP、插件、skill；
10. **遥测 / 可观测性**：事件、trace、token/费用账本；
11. **前端形态**：CLI / IDE / GUI / Web；
12. **模型/账号层**：provider 抽象、多账号、路由。

---

## 二、主流 Coding Agent 一览（2026 年中公开信息）

| 产品 | 厂商 | 形态 | Harness 特征关键词 |
|---|---|---|---|
| Claude Code | Anthropic | CLI + IDE 扩展 + SDK | ReAct、auto-compact（~64-75% 触发 + completion buffer）、CLAUDE.md、hooks、subagents、checkpoint//rewind、sandbox（bubblewrap/seatbelt）、MCP、plan mode |
| OpenAI Codex | OpenAI | CLI（Rust）+ 云端 | REPL 式会话、容器 sandbox（Docker read-only/write）、`--ask-for-approval`/`--full-auto`、plan mode、MCP、session resume、worktree/后台并行、exec_commands |
| Gemini CLI | Google | CLI（Node） | 流式主循环、自动压缩、GEMINI.md/CLAUDE.md、subagents + 工具隔离（/agents）、sandbox 三态、MCP、skills |
| Cursor | Anysphere | IDE（VS Code fork） | 代码库索引 + embedding 检索、agent mode / background agents、plan-first、git worktree 隔离、云端 Ubuntu VM 后台代理、rules/memories、MCP |
| Aider | 开源 | CLI（Python） | repo map（tree-sitter 层级）、edit formats、git auto-commit//undo、architect 双代理模式、watch 模式、无 sandbox |
| OpenHands | All Hands AI | CLI + GUI + Cloud | **EventStream 架构**（action/observation）、Docker 沙箱 runtime（REST action server）、LLMSummarizingCondenser、agent delegation/microagents、MCP、会话管理 |
| Cline | 开源 | VS Code 扩展 | Plan/Act 双模式、git checkpoints + restore、MCP、hooks、AGENTS.md/.clinerules、auto-approve |
| GitHub Copilot coding agent | GitHub | IDE + CLI | IDE agent mode、plan mode、多文件编辑、MCP（beta）、custom instructions |
| Windsurf | Codeium | IDE | Cascade 代理、memories（project/personal）、flows、MCP |
| Devin | Cognition | 云端 IDE + VM | 云端沙箱 VM、knowledge、workflow（可编程代理）、并行长任务 |

> 结论：主流 coding agent 的 harness 已收敛出共识骨架（ReAct + 窗口压缩 + 权限策略 + 沙箱 + 子代理 + MCP + 会话持久化），差异主要体现在**执行隔离强度、记忆/检索、检查点/回滚、IDE 深度集成**四个方向。

---

## 三、Harness 环节逐项对比

### 3.1 Agent 循环

| 产品 | 主循环 | 循环预算/中断 |
|---|---|---|
| Claude Code | 工具调用式 ReAct（Anthropic tool use） | 隐式步数限制、auto-compact 中断后可继续 |
| Codex | REPL 交互循环（每条消息可多步工具调用） | 显式 approval gate 中断 |
| Gemini CLI | 流式 ReAct | 压缩触发 |
| Cursor | 计划→执行的 agent 循环（IDE 内） | 后台/前台双轨 |
| Aider | 单轮工具序列（git diff 循环） | 无长循环，靠 watch 重入 |
| OpenHands | **EventStream 事件循环**（action→observation 事件总线） | 事件驱动、可回放 |
| Cline | Plan→Act 双阶段循环 | 每步审批/auto-approve |
| **Seelex** | Seele ReAct 循环（agent/session/seelectx/react） | **Effort 分档 MaxLoops**：lite=15 / medium=48 / high=384 / max=768（`application/prompt/effort.go`）；PlanPolicy 硬约束 |

**差距**：Seelex 的循环本身对齐主流（ReAct + 预算）；OpenHands 的 EventStream 是更工程化的架构（可回放、可审计），Seelex 的 EventStore 事件投影接近但未把「执行循环」本身事件化。

### 3.2 上下文管理（核心）

| 产品 | 窗口策略 | 压缩 | 工具结果外化 | Token 估算 |
|---|---|---|---|---|
| Claude Code | 滑动窗口 + auto-compact **提前到 ~64-75%** + completion buffer | 窗口外摘要（不可逆） | 截断 + 提示 | provider 计数 |
| Gemini CLI | 自动压缩 | 摘要 | 截断 | provider 计数 |
| OpenHands | Condenser：超 `max_context_length` 触发，**保留 `keep_first`** 初始事件 | LLMSummarizingCondenser（滚动摘要） | 截断 | provider 计数 |
| Cursor | **代码库索引 + embedding 检索**（主动召回，非纯窗口） | — | — | provider 计数 |
| Aider | **repo map**（tree-sitter 层级符号图，压缩后固定注入） | 无窗口压缩 | — | 估算 |
| **Seelex** | 滑动窗口 `N=clamp((ctx×0.7−reserved)/avg, 4, 40)`（`seelexctx/window.go`）；**软阈值 75% / 硬 90% / 目标 60%** | 三级压缩：短历史直通 → 跨会话快照按 token 预算压缩 → QuickChat 递归；**压缩帧可逆**（`read_compressed_turn` + TurnArchiver） | **result_ref 归档**（>4000 字符，模型只见省略标记 + 按需读取） | **len/3 保守估算**（`ConservativeTokenCounter`） |

**差距**：
- **压缩可逆性是 Seelex 的差异化强项**（主流基本不可逆）；阈值 75%/90% 已对齐 Claude Code 的「提前触发」，但**缺 completion buffer**（压缩时无「当前任务收尾」预留）。
- **检索式上下文是主要短板**：Cursor 用 embedding 索引、Aider 用 repo map 主动注入；Seelex 只有 ProjectKnowledge（预读模块语义）与人工 `read_compressed_turn`，**无代码库索引/语义检索**。
- **Token 估算**：len/3 在中英混合与工具参数下误差大，主流普遍用 provider 计数或模型感知 tokenizer。

### 3.3 记忆（跨会话）

| 产品 | 项目记忆 | 用户记忆 | 长期记忆服务 |
|---|---|---|---|
| Claude Code | CLAUDE.md / CLAUDE.local.md | ~/.claude/CLAUDE.md | memory tool（可选） |
| Gemini CLI | GEMINI.md（兼容 CLAUDE.md） | 全局记忆文件 | — |
| Cursor | .cursor/rules + memories | memories | 云端 |
| Windsurf | memories（project/personal） | memories | 云端 |
| Cline | AGENTS.md / .clinerules | 全局规则 | — |
| **Seelex** | **ProjectKnowledge**（`seelex.project.md` + 模块扫描，hash 版本化，会话前预读，`sessionstore/project_record.go`） | — | — |

**差距**：Seelex 的 ProjectKnowledge 是「自动构建、版本化」的项目知识块（优于静态 CLAUDE.md 的自动性），但**不消费 AGENTS.md/CLAUDE.md 惯例文件**、**无用户级记忆**、**无跨会话事实/决策/教训持久化**（现有 `ContextSnapshot` 字段可复用，但未落库）。

### 3.4 权限与审批

| 产品 | 策略 | 审批形式 | 扩展点 |
|---|---|---|---|
| Claude Code | settings.json allow/deny/ask；plan mode 降权 | 模态审批（once/always） | hooks（PreToolUse/PostToolUse/Stop） |
| Codex | `--ask-for-approval` / `--full-auto` / sandbox 分级 | 逐工具审批 | exec_commands 白名单 |
| Gemini CLI | sandbox 三态（unsandboxed/allowlist/denylist） | 文件/命令确认 | 工具隔离 |
| Cline | auto-approve 配置 | 逐操作审批 | hooks |
| **Seelex** | `seele.yaml` 声明式规则：**tool + patterns 匹配、LMRW 后匹配胜出**，allow/ask/deny；路径分区（plugin 只读 / workspace 读写 / config 拒绝 / 默认拒绝，`docs/arch/permission-path-gating.md`） | **异步 ApprovalBroker**（`application/approval/broker.go`）+ `ask_approve` 工具 + 终态 `task_needs_user_decision` | 插件可见性过滤（VisibilityPolicy）；无 hooks 体系 |

**差距**：Seelex 的声明式规则 + 路径分区 + 异步审批对齐甚至细于多数 CLI 产品（Codex/Gemini 的规则表达能力弱于 LMRW+patterns）；**缺 hooks 体系**（Pre/PostToolUse 拦截、命令钩子），也**缺 plan mode 的整体降权语义**（approve 节点是 DAG 节点而非全局权限档位）。

### 3.5 沙箱与执行隔离（最大差距区）

| 产品 | 隔离机制 | 说明 |
|---|---|---|
| Claude Code | OS 级 sandbox：**Linux bubblewrap / macOS seatbelt**（/sandbox），限制 FS/网络/命令，权限提示减少 84% | 无容器开销 |
| Codex | **容器 sandbox**（Docker，read-only/write 模式） | 隔离强、依赖 Docker |
| OpenHands | **Docker 沙箱 runtime**（action execution server，REST 通信，可自带镜像） | 云端/本地双形态 |
| Cursor | **git worktree 隔离**（并行） + **云端 Ubuntu VM**（后台代理） | 进程/文件隔离而非系统隔离 |
| Gemini CLI | sandbox 三态 + 工具隔离 | 网络/文件策略 |
| Aider | 无 sandbox（本地直接执行） | — |
| **Seelex** | **仅项目路径门禁 + 固定 cwd**（`workspace/`、`seelebridge/pathgate.go`）；`docs/2026-07-28-project-session-scope/sandbox-research.md` 自评「非 OS 级隔离」，已调研 isobox/agentbox，计划 CommandSandbox 端口 | 零依赖但执行隔离弱 |

**差距（结论性）**：Seelex 的 `bash` 是**确定性路径门禁**而非沙箱，属于当前主流 coding agent 中隔离最弱的一档（与 Aider 同级，弱于 Claude/Codex/Gemini/OpenHands/Cursor）。这是 harness 上**优先级最高的补齐项之一**。

### 3.6 计划与任务编排

| 产品 | 计划机制 | 任务生命周期 |
|---|---|---|
| Claude Code | plan mode（降权、先规划后执行） | 会话内任务 |
| Codex | plan mode（2026 演进中，整体审批替代逐文件审批） | 会话 |
| Cursor | 计划先行（plan-first）→ 执行 | 后台任务 |
| Cline | **Plan/Act 双模式**（规划与执行分离） | 任务 + 检查点 |
| OpenHands | planner agent（可选） | 会话/任务事件 |
| **Seelex** | **WorkPlan DAG**：`plan_load` 规范 JSON（nodes+edges）、拓扑排序 + 环检测、节点 kind（agent/approve/manual/auto/function/verify/deliver）、**隔离规划会话**（独立 Completer + 强制 `tool_choice=plan_load`，`seelebridge/plan_preflight.go`）、`plan_run` 执行、事件投影驱动前端 PlanState | **Task 生命周期独立于 Plan**：终态工具 `task_complete`/`task_failed`/`task_needs_user_decision`，投影收敛校验后接受（`seelebridge/task_terminal.go`） |

**差距**：Seelex 的 Plan DAG + 任务终态协议是**产品化深度高于多数 CLI 竞品**的（Claude/Codex 的 plan mode 只是权限档位，无 DAG 与节点类型）；但**「计划质量」依赖 preflight 单轮规划**，无 multi-step 规划评审/重规划闭环（replan 已存在但为隔离会话）。

### 3.7 子代理 / 多智能体

| 产品 | 子代理机制 | 证据/结果回传 |
|---|---|---|
| Claude Code | 内置 subagents（独立上下文、CLAUDE.md 作用域）；SDK 支持 | 结果摘要回传 |
| Gemini CLI | subagents + **工具隔离**（独立工具集/MCP/上下文），@agent 显式委派，/agents 查看 | 单响应合并 |
| Codex | 后台并行 agent（worktree/云端） | PR/结果合并 |
| Cursor | background agents（worktree + VM） | PR 合并 |
| OpenHands | agent delegation + microagents（注册表） | 事件回传 |
| **Seelex** | **Plan DAG agent 节点**：`SeelexAgentNode` 独立 Session + NodeScope 注入 + 父 `ContextSnapshot`（goal/decisions/findings/constraints/progress/pending）经 compactor 压缩注入 + 节点预算；`forkexec` 并发（信号量默认 3，fail-fast/best-effort、ParentSnapshot 克隆隔离、生命周期事件）；账号按 role+branch 确定性 FNV hash 路由 | **merger 存在但未接入生产**：`seelexctx/merger` 语义完整（findings/decisions append、progress 替换、constraints 去重），仅测试使用；生产回传只有 `BranchResult.Output` 字符串（`docs/research/agent-harness-research-report.md` P0-1） |

**差距**：子代理隔离/预算/并发对齐主流；**「父证据注入 → 子执行 → 结果 merge-back」闭环未接通**（主流 Claude/Gemini 均做结果合并回父），以及**子代理树可见性未上线**（SubAgentTree 规划未完成，`docs/arch/subagent-visibility-design.md`）。

### 3.8 会话持久化与检查点

| 产品 | 持久化 | 检查点/回滚 |
|---|---|---|
| Claude Code | 会话文件、--resume/--continue/--fork | **checkpoint + /rewind**（可回滚到检查点） |
| Codex | 会话文件、--resume/--last | fork 会话 |
| Cline | 会话 + **git checkpoints**（项目快照 restore） | 一键回滚 |
| Aider | git 全程自动 commit | **/undo**（git revert） |
| OpenHands | 会话管理（云端持久化） | 事件回放 |
| **Seelex** | `sessionstore` **四后端**（json/sqlite/postgres/redis）：DurableHistory + EventStore（追加式执行事实）+ SessionContextRecord（5 栈）+ ProjectRecord + workspace binding；`--resume`/session archive | **无 git 级检查点/回滚**：仅「上下文 checkpoint」（任务状态快照，`application/core/context_controller.go`）与会话归档 |

**差距**：持久化能力强（四后端 + 事件库 + 5 栈快照，主流 CLI 基本是单文件 JSON）；**检查点/回滚是明确短板**——Cline/Claude/Aider 都有「文件系统级快照 + 撤销」，Seelex 只有会话级状态，无法回滚代码改动（git 工具本身可做，但 harness 未封装 undo 语义）。

### 3.9 MCP 与生态

| 产品 | MCP |
|---|---|
| Claude Code / Codex / Gemini CLI / Cursor / Cline / OpenHands / Copilot | 全部支持（attach/config、工具动态注册）；Gemini 增加 MCP server 支持 |
| **Seelex** | `mcpstack`（transport-neutral provider、breaker、interceptor、persist、snapshot、prompt）+ `seelebridge/mcp.go`：**Attach/Detach/Refresh/Status** + 健康状态（alive/tool count/error）+ 插件级 MCP 作用域 |

**差距**：MCP 能力对齐主流且带 breaker 与插件作用域（略强）；无 MCP server 侧能力（主流亦然，Gemini 除外）。

### 3.10 遥测 / 可观测性

| 产品 | 遥测 |
|---|---|
| Claude Code / Codex / Gemini CLI | 无公开结构化遥测（社区靠 hooks 自建） |
| OpenHands | 事件流 + SDK 可观测性（较完整） |
| **Seelex** | `telemetry.NewMemoryTracer` + `NewLifecycleHook`（llm/tool intent-effect 事件）+ EventStore 执行事实 + `PlanNodeEvent` 投影 |

**差距**：Seelex 的遥测原语多于多数 CLI 产品；差距在**对外导出**（无 OTLP/结构化日志导出、无 token/费用账本的查询面）。

### 3.11 前端形态

| 产品 | 前端 |
|---|---|
| Claude Code | CLI + IDE 扩展 + Agent View + SDK/headless |
| Codex | CLI + 云端 web（Codex cloud） |
| Cursor / Cline / Copilot / Windsurf | IDE 深度集成（diff、内联、@引用） |
| OpenHands | GUI + CLI + Cloud |
| Aider | CLI |
| **Seelex** | **TUI（Bubble Tea，默认）+ GUI（Wails，Alpha）**，事件面板/PlanState/命令系统 |

**差距**：TUI/GUI 双前端自研但**无 IDE 集成**（主流 coding agent 大多以 IDE 为主战场）；GUI 为 Alpha，代码 diff 预览、@ 文件引用等 IDE 体验缺失。

### 3.12 模型 / 账号层

| 产品 | 模型层 |
|---|---|
| Claude Code | Anthropic（+ Bedrock/Vertex），单一厂商路由 |
| Codex | OpenAI（gpt-5-codex 等），云端/本地 |
| Gemini CLI | Gemini 系列 |
| Aider | **多 provider 灵活**（OpenAI/Anthropic/本地等） |
| **Seelex** | **P2C 账号池 + 租约**：多 provider（OpenAI/Anthropic/base_url）、角色（agent/subagent/goalplan）、防超售、流式 lease-until-EOF、分支确定性 hash 路由、运行时 /account 切换 |

**差距**：账号池/租约/多角色路由是 Seelex 的**差异化强项**（主流均为单账号单 provider）；主流补齐点在模型上下文长度适配（provider 推导窗口，Seelex 已有）与云端模型网关。

---

## 四、Seelex harness 现状速览（代码证据）

| 组件 | 证据 |
|---|---|
| ReAct + Effort MaxLoops | `application/prompt/effort.go`（15/48/384/768）、`application/core/input.go` |
| 滑动窗口/阈值 | `seelexctx/window.go`（ratio=0.7, min=4, max=40）、`controller.go`（75%/90%/60%） |
| 可逆压缩 | `seelexctx/compressor.go`、`compactor/compactor.go`、`processor.go`、`read_compressed_turn` |
| 工具结果外化 | `seelexctx/processor.go`（>4000 字符 → result_ref） |
| 权限规则 | `seele.yaml`（LMRW + tool/patterns）、`seelebridge/pathgate.go`、`docs/arch/permission-path-gating.md` |
| 审批 | `application/approval/broker.go`（异步 Interaction） |
| Plan DAG | `seelebridge/plan_factory.go`、`plan.go`、`plan_preflight.go`、`plan_tool_provider.go` |
| 子代理 | `seelebridge/agent_node.go`、`node_scope.go`、`context_components.go`、Seele `forkexec` |
| 快照/压缩/合并 | `seelexctx/snapshot/`、`provider/`、`compactor/`、`merger/` |
| 持久化 | `sessionstore/`（json/sqlite/postgres/redis）、`durable_history.go`、`event_store.go`、`session_context.go`、`project_record.go` |
| MCP | `mcpstack/`、`seelebridge/mcp.go` |
| 遥测 | `seelebridge/trace.go`（MemoryTracer + LifecycleHook） |
| 前端 | `tui/`（Bubble Tea）、`gui/`（Wails Alpha） |
| 沙箱缺口 | `docs/2026-07-28-project-session-scope/sandbox-research.md`（自评非 OS 级，isobox/agentbox 候选） |
| 既有优化清单 | `docs/research/agent-harness-research-report.md`（P0/P1/P2 + 路线图） |

---

## 五、与既有框架调研的衔接

`docs/research/agent-harness-research-report.md` 已从**框架/协议**侧给出差距（merger 未接线、无 A2A、无跨会话长期记忆、压缩预算闭环、子代理可见性等）。本文档从**产品**侧补充后，交叉结论一致，并新增两个框架侧未强调的维度：

1. **执行隔离是产品侧最大共性差距**（Codex/OpenHands/Claude/Cursor 全部强隔离，Seelex 仅路径门禁）；
2. **检查点/回滚是产品侧普遍标配**（Claude /rewind、Cline git checkpoint、Aider /undo），Seelex 无文件系统级 undo。

---

## 六、差距矩阵（总表）

| # | 维度 | Seelex | 主流基线 | 差距 |
|---|---|---|---|---|
| 1 | Agent 循环 | ReAct + Effort MaxLoops | ReAct / plan-execute / EventStream | 小（EventStream 事件化可选） |
| 2 | 窗口 + 阈值压缩 | ✅ 75%/90%，可逆 | 64-75% + completion buffer | 小（补 completion buffer） |
| 3 | 代码库检索/repo map | ❌ 仅 ProjectKnowledge + 手动 ref | Cursor 索引、Aider repo map | **大** |
| 4 | 跨会话长期记忆 | ❌ 仅 ProjectKnowledge | CLAUDE.md/GEMINI.md/memories | **中-大** |
| 5 | 权限规则 | ✅ LMRW + patterns + 路径分区 | allow/deny/ask + hooks | 小（补 hooks） |
| 6 | 执行沙箱 | ❌ 仅路径门禁 | 容器/OS sandbox/VM | **大（最高优先）** |
| 7 | 计划编排 | ✅ DAG + 节点类型 + 隔离规划 | plan mode（权限档位） | 小（Seelex 更产品化） |
| 8 | 子代理 + merge-back | 部分（merger 未接线） | 结果合并回父 | **中-大（P0）** |
| 9 | 检查点/回滚 | ❌ 无文件系统级 undo | /rewind、git checkpoint、/undo | **中-大** |
| 10 | 会话持久化 | ✅ 四后端 + 事件库 | 单文件 JSON | —（Seelex 更强） |
| 11 | MCP | ✅ 带 breaker/插件作用域 | 全部支持 | — |
| 12 | 遥测 | ✅ 内存 trace + 事件库 | 多数无结构化 | —（补导出面） |
| 13 | 前端 | TUI + GUI(Alpha) | IDE 深度集成为主 | 中（缺 IDE） |
| 14 | 账号/模型层 | ✅ P2C 池 + 租约 + 角色路由 | 单账号单 provider | —（Seelex 更强） |
| 15 | A2A/跨进程 | ❌ 未实现 | 少数（协议级） | 大（长期） |

---

## 七、差距分级与建议路线图

### P0 —— 与主流产品对齐的正确性/可靠性项
1. **执行沙箱落地**（`CommandSandbox` 端口 + isobox/agentbox 适配，至少 Linux/macOS；Windows AppContainer）：当前 `bash` 无 OS 隔离是主流产品中最弱的一环，也是安全评审最可能在意的点。
2. **merger 接入生产 plan_run 闭环**：父证据注入 → 子执行 → 结果 merge-back（既有 `docs/research/agent-harness-research-report.md` P0-1）。
3. **检查点/回滚**：封装 git 级 undo（Cline 式 checkpoint / Aider 式 /undo 语义），或至少在 plan 节点执行前后提供快照-回滚工具。

### P1 —— 对齐主流
4. **代码库索引 / 语义检索**：repo map（tree-sitter）或 embedding 索引，注入「相关文件」而非整窗口（对齐 Aider/Cursor）。
5. **跨会话长期记忆**：把 `ContextSnapshot`（decisions/findings/constraints）按 session 落库，新会话按相关性注入（对齐 CLAUDE.md/memories）。
6. **hooks 体系**：PreToolUse/PostToolUse/Stop 事件钩子（对齐 Claude Code/Cline），权限与审计从「规则」扩展为「可编程」。
7. **completion buffer + 压缩预算闭环**：压缩触发预留收尾空间，压缩帧记录实际 token 二次校验。
8. **IDE 集成**（可选）：WebView/extension 或 LSP 形态，补 diff 预览与 @ 引用。

### P2 —— 差异化/工程化
9. 遥测对外导出（OTLP/结构化日志）+ token/费用账本查询面。
10. Token 估算升级（模型感知 tokenizer 或 provider 计数）。
11. AGENTS.md/CLAUDE.md 惯例文件消费（可选，兼容生态）。
12. A2A 协议适配（长期，与既有框架调研结论一致）。

---

## 八、证据索引

- Seelex：`seelebridge/{runtime,plan,plan_factory,plan_preflight,agent_node,node_scope,mcp,pathgate,task_terminal,trace}.go`；`seelexctx/{assembler,window,controller,compressor,processor,history_safety}.go`；`application/{approval/broker.go,prompt/effort.go,core/context_controller.go}`；`sessionstore/*`；`mcpstack/*`；`seele.yaml`；`docs/arch/permission-path-gating.md`；`docs/2026-07-28-project-session-scope/sandbox-research.md`；`docs/research/agent-harness-research-report.md`
- 外部：Claude Code auto-compact 分析（hyperdev.matsuoka.com 2026）；Claude sandbox（bubblewrap/seatbelt，mintmcp.com）；OpenAI Codex plan mode issue（github.com/openai/codex#4897）；OpenHands Runtime Architecture（docs.openhands.dev）与 Condenser 文档；Gemini CLI subagents（developers.googleblog.com 2026-04）与 memory-management 文档；Cursor 2.0 agent-first 架构（digitalapplied.com）；Aider repo map / architect 模式；Cline Plan-Act / checkpoints（cfi-pub.gitlab.io）；git worktree 并行 agent（augmentcode.com）。
