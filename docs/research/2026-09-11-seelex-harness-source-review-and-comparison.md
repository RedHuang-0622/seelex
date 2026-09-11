# Seelex Agent Harness 源码解剖与横向对比

> 调研日期：2026-09-11
> 对象：本仓库（Seelex，`github.com/RedHuang-0622/seelex`，Seele v0.1.3 运行时边界）
> 方法：① **静态源码阅读**（本仓库 + 同机 `../Seele` 上游源码）；② **一手文档/源码取证**（Claude Code 官方 md、OpenAI Codex 开源仓库、Gemini CLI 开源源码）
> 范围：harness 机制（Agent 循环、上下文、权限/沙箱、计划、子代理、持久化、扩展、前端协议、测试基座），不含前端视觉与模型效果
> 关联文档：[`coding-agent-harness-comparison.md`](coding-agent-harness-comparison.md)（2026-08-02 产品侧对比）、[`agent-harness-research-report.md`](agent-harness-research-report.md)（框架侧）、[`codex-claude-goal-harness-2026-09.md`](codex-claude-goal-harness-2026-09.md)（goal 机制）

## 证据分级

| 标记 | 含义 |
|---|---|
| 【源码】 | 本人直接读到代码行，给出 `文件:行` |
| 【一手文档】 | 官方文档原文或多源一致，给出链接 |
| 【二手】 | 第三方拆解/社区文章，或由子代理汇总、本人未逐行复核 |
| 【推断】 | 按机制必要性推断，非实证 |

本次**未执行**任何构建、测试或运行时验证；所有 Seelex 结论均为静态阅读结论。未覆盖：`gui/frontend` 前端 JS、`seelebridge` 全量文件、Seele 上游全量、真实模型链路。

---

## 0. 结论速览

1. **Seelex 是一个"产品语义层 + 运行时内核"二分的 harness**：Seele 提供 ReAct 循环、Session、工具分发、WorkPlan、账号租约；Seelex 提供任务/计划语义、上下文策略、权限策略、扩展与持久化。两者之间的 `seelebridge/` 是真正的防腐层，而不只是转接层。【源码】
2. **它的两个"别人少做"的设计**：(a) **压缩可逆**——窗口外轮次压成 CompactFrame 后，原始轮次仍可经 `read_compressed_turn` / `search_history` 按需读回，超大工具结果归档为 `result_ref` 后经 `read_tool_result` 分页读回；(b) **反虚假完成**——任务终态工具受 Plan 投影门禁（`nodesNotCovered`），目标完成由 TechLeader/Advisor 评审裁决（`completed` / `not_done` / `escalate_human`），拒绝让模型自称完成。【源码】
3. **它最大的结构性缺口是隔离深度**：安全边界只有"路径 containment + 路径规则门禁 + 人工审批"，没有任何 OS 级或容器级进程隔离；Claude Code（Seatbelt/bubblewrap）、Codex（Seatbelt/Landlock + execpolicy）、Gemini CLI（容器沙箱）、OpenHands（Docker runtime）都有。【源码 + 一手文档】
4. **权限档位过粗**：`manual` / `full_access` 两档（`main.go:54`）。行业已收敛到 allow/ask/deny + 自动度旋钮（Claude Code 六档：`default`/`acceptEdits`/`plan`/`auto`/`dontAsk`/`bypassPermissions`），Seelex 缺中间档，长任务上只能在"频繁打断"与"全放开"之间二选一。【源码 + 一手文档】
5. **扩展模型是同类里的异类**：Plugin 一次原子切换"工具可见性 + 系统提示词 + Skill 可见集 + MCP 连接"，失败逆序回滚（`plugin/apply.go:21`）。公开 harness 中少见同构实现。【源码】
6. **前端协议是同类里较重的一档**：Application Snapshot 为权威态 + Event 增量（全局 `Seq` 与按订阅 `DeliverySeq` 分离，缺口即 `resync.required`），支撑多会话/后台会话。多数开源 harness 是单进程 TUI 直接持有内存态。【源码】
7. **Plan 是"可选加载的一等 DAG"**，且区分 tasklist 模式（主代理串行执行）与 plan 模式（子代理并行）；节点 = 独立 Session + 独立 worktree + 账号 binding + token budget。这是 Seelex 相对同类最完整的一块能力，也是最难维护的一块。【源码】
8. **同层级可比性证据缺失**：仓库自述尚未发布 SWE-bench / Terminal-Bench 等标准基准，TUI 测试覆盖率 26.1%，因此"能力更强"只能在机制层面论证，不能在效果层面论证。【源码（README 自述）】

---

## 1. 分层与边界【源码】

| 层 | 目录 | 职责（源码事实） |
|---|---|---|
| 组合根 | `main.go`（~1300 行） | 17 处 `runtime.RegisterTool(...)`；flag：`-store .seelex/sessions`、`-plugins`、`-permission manual\|full_access`、`-frontend tui\|gui\|headless\|backend`、`-backend-*`、`-version`（`main.go:52-60,90,97`） |
| 应用层 | `application/` | `contract/`（消费方定义端口）、`core/`（chat/task/plan/session/goal/context_runtime）、`model/`（Snapshot DTO）、`event/`（Hub）、`prompt/`（effort + prompt stack） |
| 运行时边界 | `seelebridge/` | `Runtime` 持有 `*tools.RegistryState`、`*accountpool.P2CPool[agent.Completer]`、`completer`/`streamer`、`*agent.Agent`、`bundles map[string]*sessionBundle`、`activeSessionID`、`historyRouter`（`runtime.go:62-81`） |
| 上下文领域 | `seelexctx/` | assembler / processor / compressor / controller / window / frame / gap / replay / DAG |
| 持久化 | `sessionstore/`（87 文件）、`session/`（领域与 actor） | 项目/会话分区、分片、module head、fork、检查点 |
| 扩展 | `plugin/`、`skill/`、`mcpstack/` | Plugin 事务、Skill 加载与资源逃逸检查、MCP 调用轨迹 |
| 前端 | `tui/`、`gui/` | 只消费 Application DTO / Snapshot / Event |
| 测试基座 | `e2e/scenario/`、`*_test.go` | scripted engine + fixture + event recorder，无需真实 LLM |

上游 `Seele v0.1.3`（外部 Go module）持有：`agent`、`session`（`session/loop.go` 的 `ReActLoop`）、`tools.Registry`、`workplan`、`seelectx`（`RequestAssembler`/`ToolResultProcessor`/`ContextController`/`DurableHistory`）、`accountpool`、`event`、`telemetry`。【源码】

---

## 2. 主循环与预算

**循环本体在上游**：`../Seele/session/loop.go`（607 行）的 `ReActLoop`，可选注入 `WithMaxLoops` / `WithRequestAssembler` / `WithToolResultProcessor` / `WithCompressor` / `WithContextController`（`:82-131`）；每轮结束回调 `hooks.OnIterationComplete`，返回 false 即停机（`:417-420`）；工具经 `agent.Dispatch`（`:358`）。**Seelex 只决定"给多少预算、注入什么组件"，不重写循环**——这正是"边界原则：Seele 提供无产品语义的执行能力，Seelex 决定何时调用、上下文放什么"的落地形态。【源码】

**Effort 是预算而不是提示词**（`application/prompt/effort.go:44-80`）：

| 档 | maxLoops | ReActBudget（工具轮 / 工具调用 / 无进展轮） | PlanPolicy |
|---|---|---|---|
| lite | 15 | 15 / 30 / 6 | `{Effort:"lite"}` |
| medium | 48 | 48 / 96 / 10 | `MaxNodes:4, RequireSerial:true` |
| high | 384 | 384 / 768 / 24 | `MaxNodeLoops:48` |
| max | 768 | 768 / 1536 / 48 | `MaxNodeLoops:96` |

`MaxForkConcurrency` 在 effort 中**有意保持 0**：子代理并发不再由档位限制，运行时执行"当前所有可运行节点同时跑"（`effort.go:52-79` 注释）。ReAct 预算的实际执行点在应用侧：`application/core/task_context/coordinator.go:544-561`（工具调用上限、工具轮上限、无进展轮上限），由 `chat.go:68,292` 起停。【源码】

---

## 3. 工具面、权限与沙箱

**分发链**（`seelebridge/tools/registry_state.go:22-25`）：`WithCallTimeout(timeout)` + `WithMiddleware(event, permission.Middleware(approvalTimeout), diagnostic)`；审批配置经 `PermissionGate.Set(cfg, handler)`（`:98-111`）在请求期原子替换。

**两层安全边界**：

1. **ProjectScope**（`seelebridge/security/project_scope.go:18-45`）：`Bind` 同时保存词法 `root` 与 `EvalSymlinks` 后的 `realRoot`，解析目标为 canonical absolute path 并校验仍在 root 内（`ResolveRead:63`）——处理了 symlink/junction 绕过。
2. **PathGate**（`seelebridge/security/pathgate.go:29-30,52,85,99`）：按 zone 声明 `read/write: allow|deny` 规则，绑定 workspace 后给出 `AllowRead/AllowWrite`；合法范围内的进一步策略判定。
3. **人工审批**：默认 `manual`（`main.go:54`），`full_access` 全放行。Windows Shell 使用显式 PowerShell + `-NoProfile -NonInteractive`（`security/command_windows.go`，README 自述为降低 profile 注入）。

**没有的东西（Confirmed by absence）**：进程级/容器级隔离、网络域名 allowlist、命令级策略引擎（对比 Codex 的 `execpolicy`）。`limits.go` 中的 `disable_docker_auto_start` / `docker_start_timeout` 说明 Docker 只用于"模型命令里出现 docker 时自动拉起守护进程"，不是沙箱。**这是与成熟 harness 差距最大的一处**（见 §6 观察 4）。【源码】

---

## 4. 上下文工程（本仓库最厚的一块）

### 4.1 预算与阈值（`seelexctx/controller.go:56-73`）

```
Budget          = Window − OutputReserve − SafetyReserve
SafetyReserve   = Window / 8          (NewContextWindowPolicy)
SoftThreshold   = Budget × 75%
HardThreshold   = Budget × 90%
```

窗口轮数 `N = clamp((ContextTokens × Ratio − Reserved) / AvgRoundTokens, MinRounds, MaxRounds)`，默认 `{Ratio:0.7, MinRounds:4, MaxRounds:40}`（`seelexctx/window.go:38,43`）。

### 4.2 触发与压缩范围（`Handle`，`controller.go:225-262`）

- `after_tool` + 超大结果 → `hardThresholdPath`（先归档、后压缩，`:349-352`）
- `after_tool` / `after_assistant` + 命中软阈值 → `compressWindowOutside`（**只压缩滑动窗口之外的轮次**）
- 硬阈值路径可收缩窗口 N，但 `WindowPolicy` 的 clamp 保证不低于 `MinRounds`（`controller.go` 中 `shrinkWindowRounds`）

### 4.3 工具结果外化与**可逆读回**

`seelexctx/processor.go:126-133`：超限结果 → `archive.Store(...)` 得到 `result_ref` → 模型看到的是 `OversizedToolResultWarning(name, ref)` 省略块；`read_tool_result`（`main.go:329`）支持 bounded pagination / line filtering 读回。压缩帧同理：`read_compressed_turn`（`main.go:341`）按 `segment_id` 读回原始消息，`search_history`（`main.go:353`）在压缩段索引上做检索并在 token 预算内读回真实记录。**这三个工具把"压缩"从有损操作变成可逆操作**，是 Seelex 与多数 harness 的实质差别。【源码】

### 4.4 栈模型与项目级知识

`SessionContextRecord{SystemPrompt, PlanStack, TaskStack, SkillStack, CompactStack, GoalStack, GoalAudit}`（`sessionstore/session_context.go`）；"now using X" = 栈顶；`CompactFrame` 携带 `From/To/Summary/Evidence/SegmentID`。项目级 `ProjectKnowledge` 经 `project_refresh`（`main.go:392`）按内容 hash 重建，会话前预读。【源码】

### 4.5 两处执行点（已确认，需要正视）

| 执行点 | 位置 | 行为 |
|---|---|---|
| Session 侧 `ContextController` | `seelexctx/controller.go`，注入上游 `ReActLoop` | 软/硬阈值、窗口外压缩、push `CompactFrame` |
| Application 侧 `context_runtime.Coordinator` | `application/core/context_runtime/coordinator.go`，装配于 `service_assembler.go:113,148` | `PrepareExecutionContext`、`RejectToolResults`、checkpoint 标记、`ErrProviderContextBudgetExceeded`、ReActBudget 计数 |

两者阈值同源（`task_context.ContextBudgetFor(runtime)`，`coordinator.go:150`），但**替换历史有两条路径**：`ReplaceHistoryFor(sessionID, ...)` 与 `Engine.ReplaceHistory(sessionID, ...)`（`coordinator.go:645-650`）。这是本项目上下文一致性最值得加测试的地方。【源码】

---

## 5. Plan / Subagent 编排与治理

### 5.1 Plan 是可选的一等 DAG

- 校验：`seelebridge/plan/policy.go:30-53`（`MaxNodes`、`RequireSerial`（必须是从 entry 出发的单链）、节点 `MaxNodeLoops`、`MaxNodeOutputTokens`，超出即报错）；`PlanPolicy` 字段见 `application/contract/dto/plan.go`。
- 节点 → 独立 Session：`seelebridge/node/agent_node.go:114-129`，`RoleSubAgent` 先 `BeginNodeWorktree`（每子代理一个 worktree），再 `WithNodeScope(ctx, scope)`。
- 子代理提示词资产：`internal/promptassets/assets/subagent/charter.md`（明确"不触碰主工作区，合并是框架的事"、结构化 findings 供 merge-back、`git rebase` 收尾纪律、预算可见）。
- 编排语义写进系统提示词：`internal/promptassets/assets/system/instructions.md` 区分 **tasklist 模式**（主代理串行执行 DAG，逐节点 `task_check_node`）与 **plan 模式**（`plan_run`，agent 节点并行，事件投影驱动勾选），并规定终态工具只能延后一次调用。

### 5.2 `fork_subagents` 是一个"运行时构造的 DAG"

`seelebridge/fork/tool.go`：校验子代理 id 唯一、`len > policy.MaxNodes` 直接拒绝（`:52-64`），构造 `start → s1..sN → summary`；summary 节点对各子代理输出做**有界拼接**，完整结果走 read-back。README 自述：外层工具结果可能超单条 provider context 预算，此时不得转述摘要，须以节点详情为准——**这是当前已知的交付契约缺口**。【源码 + README 自述】

### 5.3 Task 与 Goal 的"反虚假完成"

- Task 状态：`running / completed / needs_user_decision / blocked / interrupted / failed`（`task_execution.go:16-21`）；终态工具 `task_complete / task_check_node / task_needs_user_decision / task_failed`（`:23-26`）。
- 门禁：`TaskService.VerifyAndApply`（`task_service.go:286`）→ `applyCompleteLocked`（`:361-362`）→ `verifyCompletionLocked` 的 `nodesNotCovered`（`:402,450`）：**已加载 Plan 的每个节点都必须出现在 `completed_nodes`**，否则不许完成。
- Goal：LIFO 栈（`goal/controller.go:143 Begin`，同标题幂等、超深度拒绝），完成提议经 gate 裁决，输出 `no_goal / no_tl / completed / not_done / escalate_human`（`goal/gate.go:24-28`）；TL 缺席或评审失败一律安全侧处理，`escalate_human` 映射为 `task_needs_user_decision`。【源码】

### 5.4 状态权威与前端协议

`model.Snapshot{ProtocolVersion:1, Revision, ...}`（`model/state.go:10-15`）是权威态；`EventHub` 事件带全局单调 `Seq` 与按订阅 `DeliverySeq`（`event/hub.go:85-90`），订阅溢出或缺口投递 `resync.required`（`:33,47`），`replay(sinceSeq)` 支持补发。TUI 只做 `app.Snapshot()` + `app.Subscribe(256)` / `SubscribeSession(...)`（`tui/tui.go:18-22,74-78,104`）。【源码】

---

## 6. 横向对比

### 6.1 取证来源

| 对象 | 来源 | 可信度 |
|---|---|---|
| Claude Code | `code.claude.com/docs/en/{sub-agents,permission-modes,model-config,checkpointing,sandboxing,costs,agent-view}.md` 原文 | 高（一手文档） |
| OpenAI Codex CLI | `github.com/openai/codex`（开源，Rust）：`codex-rs/core/src/sandboxing/mod.rs`、`rollout_budget.rs`、`context/compaction_summary.rs`；`docs/*.md` 已改为指向 `developers.openai.com` 的短跳转 | 中高（源码 + 短文档） |
| Gemini CLI | `github.com/google-gemini/gemini-cli`：`packages/core/src/context/chatCompressionService.ts` 等 | 高（一手源码） |
| Aider | `aider.chat/docs/repomap.html` | 中（文档） |
| OpenHands / Cline / Cursor / SWE-agent / LangGraph / Agents SDK / A2A | 本仓库既有调研文档 + 公开常识，本次未重新取证 | 中低（标【二手】） |

### 6.2 机制矩阵

| | 主循环 | 上下文/压缩 | 权限与隔离 | 子代理/编排 | 持久化/恢复 | 扩展 | 差异点 |
|---|---|---|---|---|---|---|---|
| **Seelex** | ReAct（上游 `ReActLoop`），effort 决定 loop/tool/no-progress 预算 | 预算=窗口−输出预留−窗口/8；软 75%/硬 90%；只压窗口外轮次；N=0.7 clamp 4..40；**压缩可逆** | `manual`/`full_access`；ProjectScope + PathGate + 审批；**无 OS/容器隔离** | 可选 JSON DAG；节点=独立 Session+worktree+账号+预算；tasklist/plan 双模式 | 项目/会话分区 + 分片 + module head 原子发布；fork（对话）+ 压缩帧 | Plugin 事务切换（工具+提示词+Skill+MCP）；Skill；MCP | 压缩可逆 + 反虚假完成门禁 |
| **Claude Code** | 单会话 agentic loop + hooks | auto-compact window 可配（`/autocompact`、`--autocompact`、`autoCompactWindow`），1M Sonnet 5 默认约 967K 触发，窗口受模型上下文上限约束【一手文档】 | 六档模式 `default(manual)/acceptEdits/plan/auto(分类器)/dontAsk/bypassPermissions`【一手文档】；Bash 沙箱 = macOS Seatbelt / Linux bubblewrap，文件+网络隔离，**不支持原生 Windows**【一手文档】 | subagent 独立 context window + 独立权限 + 自定义系统提示词；内置子代理继承父权限；Plan 子代理【一手文档】 | 每轮用户输入前文件快照，保留最近 100 个检查点，`/rewind`，随会话持久化【一手文档】 | MCP、skills、hooks、`.claude/agents/` | 权限自动度 + 检查点回滚 |
| **Codex CLI** | turn/step loop，协作模式（default/plan） | 有 `context/compaction_summary.rs` + `rollout_budget.rs` 的 token 阈值提醒与压缩 | 沙箱类型含 `MacosSeatbelt`；docs 指向 `developers.openai.com/codex/security`；config 支持 managed hooks（`allow_managed_hooks_only`）【源码】 | 无独立文档取证（【二手】多 Agent/并行 worktree） | rollout JSONL + 截断/迁移/重建模块（`rollout.rs`、`thread_rollout_truncation.rs`）【源码】 | MCP、skills（`docs/skills.md`） | 会话 rollout 可重建 |
| **Gemini CLI** | 事件驱动循环 | `DEFAULT_COMPRESSION_TOKEN_THRESHOLD = 0.5`、`COMPRESSION_PRESERVE_THRESHOLD = 0.3`、函数响应预算 50k，按模型选压缩模型【源码】 | 容器沙箱（【二手】） | subagents + 工具隔离（【二手】） | 会话摘要服务（`sessionSummaryService`） | MCP、skills | 阈值硬编码，简单可预测 |
| **OpenHands** | EventStream（action/observation） | Condenser（软/硬触发，替换前半历史）【二手】 | Docker/远程 runtime（【二手】） | agent delegation / microagents（【二手】） | 追加式事件流 + 压缩墓碑（【二手】） | MCP | 事件流即事实源 |
| **Aider** | 逐消息循环，`code/architect/ask` 模式 | tree-sitter repo map（图排序算法选文件）【文档】 | 无沙箱（【二手】） | architect/editor 双模型（【二手】） | git 自动提交/撤销 | 无 MCP 依赖 | repo map 上下文替代压缩 |
| **SWE-agent** | ACI 约束的 thought/action 循环 | 窗口化查看（约 100 行/次） | 容器（【二手】） | 无（单代理） | 轨迹日志 | YAML 工具/提示词包 | ACI = 接口即能力上限 |

### 6.3 横向观察

1. **追加式日志 + 压缩已收敛**；差异在"压缩后是否可逆"。Codex 用 rollout 前缀重放恢复会话，Claude Code 保留会话但模型侧不读回原轮次；**Seelex 把"读回原轮次"做成了模型可调用的工具**（`read_compressed_turn` / `search_history` / `read_tool_result`）——机制上更彻底，代价是三个额外工具 + 段索引维护成本。【源码 + 一手文档】
2. **压缩阈值哲学不同**：Gemini 硬编码 0.5/0.3（可预测、不可调）；Claude Code 暴露可调窗口 + 期望重建感知（prompt cache 计入"expected rebuild"）；Seelex 用"输出预留 + 12.5% 安全余量 + 软 75/硬 90"三段式，并额外约束"只压窗口外"。【源码 + 一手文档】
3. **"子代理独立上下文窗"已是共识，差异在继承策略**：Claude Code 默认全新上下文 + 独立权限（内置子代理继承父权限）；Seelex 显式注入父证据（PromptBlock）+ 独立 worktree + 独立 Session + 账号 binding + 预算。Seelex 在**工作区隔离**上比 Claude Code 默认共享工作区更保守，但在**进程/文件系统隔离**上更弱。【源码 + 一手文档】
4. **隔离深度是最大差距（Confirmed）**：Claude Code Seatbelt/bubblewrap、Codex Seatbelt/Landlock + execpolicy、Gemini 容器、OpenHands Docker；Seelex 只有路径 containment + PathGate + 人工审批，命令仍在宿主进程内以用户权限执行。【源码 + 一手文档】
5. **权限 UX 已收敛到 allow/ask/deny + 自动度旋钮**；Seelex 只有两档，缺 `acceptEdits`/`auto`(分类器)/`dontAsk` 这类中间档——长任务上"审批疲劳"与"全放开"不可兼得。【源码 + 一手文档】
6. **Plan 的形态各家不同**：Claude Code 的 plan mode 本质是只读权限模式 + 计划子代理；Codex 是协作模式；Seelex 把 DAG 做成**模型可加载的一等对象**并配事件投影与终态门禁，还区分 tasklist（串行）与 plan（并行）两种使用方式。这个设计在同类里少见且自洽。【源码 + 一手文档】
7. **扩展底座在收敛**（MCP + skills/rules 文件 + hooks）；Seelex 的**事务式 Plugin 切换**（原子替换工具可见性/提示词/Skill/MCP，失败逆序回滚）在公开 harness 中少见。【源码】
8. **用户级回滚是行业事实标准，Seelex 只有半边**：Claude Code 有 `/rewind`（文件快照，100 个检查点）、Cline 有 shadow git、Codex 有 rollout resume；Seelex 有会话 fork（对话深拷贝 + 血缘）与 append-only 会话恢复，但**没有"回滚文件改动到某个用户输入之前"的能力**（17 处注册工具中无 rewind/undo 类）。【源码 + 一手文档】

---

## 7. 评估与建议

### 7.1 强项（有源码支撑）

| 强项 | 证据 |
|---|---|
| 边界清晰、可替换、可离线验证 | `seelebridge/runtime.go:62-81` 组合根；`e2e/scenario/harness.go` scripted engine + ports + recorder，无需真实 LLM |
| 预算治理闭环 | `effort.go:44-80` → `task_context/coordinator.go:544-561`（轮/调用/无进展）→ `plan/policy.go:30-53`（节点数与节点预算） |
| 反虚假完成 | `task_service.go:402,450` 的 `nodesNotCovered`；`goal/gate.go:24-28` 的 `not_done`/`escalate_human` |
| 上下文可逆 | `processor.go:126-133` + `read_tool_result`/`read_compressed_turn`/`search_history`（`main.go:329,341,353`） |
| 子代理隔离与证据传递 | `node/agent_node.go:114-129`（worktree + node scope）；`assets/subagent/charter.md`（结构化 findings 契约） |
| 扩展原子性 | `plugin/apply.go:21` Transaction（逆序 Undo + `errors.Join`）；`plugin/manager.go:52-166` |
| 多前端单一权威态 | `model/state.go:10-15` + `event/hub.go:85-90`（Seq/DeliverySeq/resync） |

### 7.2 弱项与风险

| 级别 | 问题 | 证据 | 影响 |
|---|---|---|---|
| P0 | 无 OS/容器级沙箱；`bash` 在宿主进程内以用户权限运行 | `security/` 仅 `project_scope.go`/`pathgate.go`/`command_windows.go`/`sandbox.go`；`limits.go` 中 docker 仅自动拉起守护进程 | 模型失误/被注入提示的影响面超出项目边界 |
| P0 | 权限只有两档，缺中间自动度 | `main.go:54` | 长任务审批疲劳，或被迫 `full_access` |
| P0 | `fork_subagents` 结果交付契约不完整（大结果无法完整读取） | README 自述 + `fork/summary.go` 有界拼接 | 审查类任务可能被要求"不得转述摘要"而陷入无结论 |
| P1 | 上下文策略两个执行点、两条替换历史路径 | `service_assembler.go:113,148`；`context_runtime/coordinator.go:645-650` | 历史一致性与协议配对风险（assistant/tool 配对） |
| P1 | 子代理节点的压缩栈是内存态，不跨恢复持久化 | `context_runtime` 中 `NewMemoryCompactStack`（【二手】，未逐行复核） | 长任务恢复后子代理上下文丢失 |
| P1 | 常驻会话 LRU 上限（默认 6）与多会话/后台会话并存 | `seelexctx/limits.go`（`resident_limit`） | 并发会话数的硬约束，需明确产品预期 |
| P2 | 缺用户级文件回滚（rewind/undo） | 注册工具清单中无此类 | 与行业基线相比体验缺口 |
| P2 | 缺标准基准结果；TUI 覆盖率 26.1% | README 自述 | 无法在效果层面与同类比较 |

### 7.3 建议（按优先级）

1. **P0 · 加一层可选执行隔离**：把 PathGate 声明升级为"可执行的沙箱策略"（Linux: bubblewrap/landlock；macOS: Seatbelt；Windows: Job Object/AppContainer 或显式 WSL2 委派），并补网络域名 allowlist；无沙箱时在 UI 与工具结果里显式标注"未隔离"。
2. **P0 · 权限档位补中间层**：至少新增 `auto-approve-edits`（写文件自动放行、shell 仍问）与规则化 `auto`（按 PathGate allow 规则 + 工具白名单自动放行），把"频繁打断"从长任务的默认路径上移除。
3. **P0 · 修 `fork_subagents` 交付契约**：summary 节点固定输出"有界摘要 + 每子代理 `result_ref`"，并在工具描述里明确"大结果请读 `result_ref`"，消除 README 记录的"不得转述"死结。
4. **P1 · 收敛上下文执行点**：明确 session 侧 `ContextController` 为主、应用侧 `Coordinator` 为"预算守卫 + 投影"，删除 `Engine.ReplaceHistory` 备用路径或为其加显式不变量测试（压缩帧 `From/To` 坐标系 vs reader 坐标系）。
5. **P1 · 子代理压缩栈持久化**：节点 CompactStack 走 sessionstore，与主会话同构，避免恢复后上下文断层。
6. **P1 · 会话容量与产品预期对齐**：把 `resident_limit` 与后台会话、Agent View 类能力一起定义 SLA，并在 GUI 暴露驱逐状态。
7. **P2 · 补可比性证据**：跑一个可复现的 SWE-bench 子集/Terminal-Bench 子集并公开口径；同时把 TUI 覆盖率抬到与核心层同量级。

---

## 8. 附录

### 8.1 关键证据索引

| 结论 | 位置 |
|---|---|
| 组合根与工具注册 | `main.go:52-60,90,97`；17 × `runtime.RegisterTool` |
| Runtime 组合根字段 | `seelebridge/runtime.go:62-81` |
| 中间件与审批 | `seelebridge/tools/registry_state.go:22-25,98-111` |
| 路径 containment | `seelebridge/security/project_scope.go:18-45,63` |
| 路径规则门禁 | `seelebridge/security/pathgate.go:29-30,52,85,99` |
| Effort 预算表 | `application/prompt/effort.go:44-80` |
| ReAct 预算执行 | `application/core/task_context/coordinator.go:544-561` |
| 上下文预算与阈值 | `seelexctx/controller.go:56-73`；`Handle` `:225-262`；`hardThresholdPath` `:349-352` |
| 窗口公式与默认值 | `seelexctx/window.go:38,43` |
| 工具结果外化 | `seelexctx/processor.go:126-133,138,144` |
| 压缩读回工具 | `main.go:329,341,353` |
| Plan 策略校验 | `seelebridge/plan/policy.go:30-53`；`application/contract/dto/plan.go` |
| 节点=独立 Session + worktree | `seelebridge/node/agent_node.go:114-129` |
| fork_subagents 构造 | `seelebridge/fork/tool.go:43-64`；`README.md`「子代理的进度与结果」 |
| Task 状态与终态门禁 | `application/core/task_context/task_execution.go:16-26`；`task_service.go:286,361-362,402,450` |
| Goal 裁决 | `application/core/goal/controller.go:143`；`goal/gate.go:24-28` |
| Snapshot/Event 协议 | `application/model/state.go:10-15`；`application/event/hub.go:33,47,85-90` |
| 前端消费 | `tui/tui.go:18-22,74-78,104` |
| Plugin 事务 | `plugin/apply.go:21,56`；`plugin/manager.go:52-166` |
| 存储分区与分片 | `sessionstore/sessionstore.go:39,1375-1383`；`sessionstore/module_heads.go` |
| 运行时上限集中治理 | `seelexctx/limits.go`（`Limits` / `SessionStorageLimits`） |
| 提示词资产 | `internal/promptassets/assets/{system/instructions.md, subagent/charter.md, effort/*.md}` |
| 离线 e2e | `e2e/scenario/harness.go:15-65`；`e2e/fixtures/*.json` |
| 外部：Claude Code 权限模式 | `code.claude.com/docs/en/permission-modes.md`（模式表） |
| 外部：Claude Code 压缩窗口 | 同上 `model-config.md`（`/autocompact`、`--autocompact`、默认约 967K 触发、受模型上限约束） |
| 外部：Claude Code 子代理 | `sub-agents.md`（独立 context window / 独立权限 / 内置子代理继承父权限） |
| 外部：Claude Code 沙箱 | `sandboxing.md`（Seatbelt / bubblewrap / 不支持原生 Windows） |
| 外部：Claude Code 检查点 | `checkpointing.md`（每轮前快照、最近 100 个、`/rewind`） |
| 外部：Gemini CLI 压缩阈值 | `packages/core/src/context/chatCompressionService.ts:41,47,52` |
| 外部：Codex 沙箱与预算 | `codex-rs/core/src/sandboxing/mod.rs:181-182`；`codex-rs/core/src/rollout_budget.rs:25,80` |

### 8.2 本次未覆盖 / 后续可做

- `gui/frontend` 前端实现（DSL 卡片、轨迹窗口）与桌面桥协议未审。
- `seelebridge` 未逐文件审（重点看过 `runtime.go`、`tools/`、`security/`、`plan/`、`node/`、`fork/`）。
- Seele 上游只看了 `session/loop.go` 与 `agent`/`seelectx` 接口面，`workplan` 内核调度未审。
- 未执行构建/测试/真实模型链路；未做性能与成本实测。
- 标准基准（SWE-bench / Terminal-Bench）与同类 harness 的实测对比未做。
