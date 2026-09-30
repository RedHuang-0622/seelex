# goal-a2a 现状流程 × 市面对照 × 更好的结合方式 × 更优 TechLeader 流程（2026-09-30）

> 提问：**① 现在的 goal-a2a 流程长什么样？② 市面主流 a2a/get-goal 怎么做？③ 有没有更好的结合方式？④ 有没有更优秀的 TechLeader 流程？**
>
> 取证口径：
> - **仓库事实**（`实测`）逐条带 `文件`/`文件:符号`；
> - **外部一手**（`一手`）用本机浏览器（Playwright）直接抓发布方域名原文，见 §6 来源表；本轮 `web_search` 工具不可用（403 配额），未做穷举检索；
> - 未取到一手原文的条目进 §5「未核实」，不据此推断。
> - 本轮**不改代码**（用户口径）：本文是「掌握 full picture + 外面信息」的产物，供后续改动做输入。

---

## 0. 一页摘要

| 问题 | 结论 |
|---|---|
| 现状是什么 | 不是「多智能体团队」，而是**「单执行体（EXEC）+ 一个上下文被刻意隔离的后台评审者（ADVISOR/TL）+ 一条确定性治理循环（govern）+ 确定性逃生天花板」**。EXEC 是唯一 writer，b 只出结构化裁决（`advisory.send`/`gate.verdict`），a 永不因 b 缺席卡死（B4）。 |
| 市面对照 | **进程内协作**主流用「共享 task list + 轻量引用 + description 匹配」（Claude Teams / Codex / LangGraph subagents），**不用 A2A**；A2A 协议（v1.0，Linux Foundation）解决的是**跨进程/跨厂商 opaque agent 互操作**，不是「同进程让几个角色轮流发言」。**能力增益的两个确证来源**是「并行广度」与「可执行验证接地」，不是「角色分工」。 |
| 更好的结合 | **三层各归其位**：MCP=深度（工具/资源）、A2A=广度（跨边界委派/产物交换）、goal=意图与完成判定锚点。进程内继续用「唯一帧账本 + 裁决入账」，**只把 A2A 价值落在可观测/可审计**；把 goal 状态机与 A2A Task 状态机对齐以获得未来跨进程委派面；TL 触发放到「事件命中 + 任务类型开关」。 |
| 更优 TL 流程 | 关键洞见是「**评审者要有独立信息源或可执行验证，否则只是另一个观点**」。仓库已在 2026-09-16 把 ADVISOR 从「无工具 completer」升级为「**带只读工具的角色回合**」——方向正确。下一步按性价比：给受限**执行位（能跑测试）** → **按任务类型开关 TL** → **度量 A/B** → 高风险终态用**多评审投票/结构化异议**。参考：Anthropic `evaluator-optimizer`、MAST 失败分类、LLM-as-a-judge 偏差。 |

**一句话**：现在这套值钱的地方是**看护（逃生/审计/权责边界/上下文隔离）**，不值钱的是「宣称的团队产出能力」；把它从「有仪式感的角色扮演」变成「有实际作用的对抗审查」，靠的是**可执行验证 + 度量 + 只在值得的任务上开**。

---

## 1. 现状全链路（仓库事实）

### 1.1 角色与生态位

| 角色 | 定位 | 证据 |
|---|---|---|
| **EXEC (a)** | 执行会话：用户可见视图、全量转录、唯一 writer、plan/task/goal 主状态 | `docs/2026-09-07-seele-a2a-framework-req/ds-a2a-protocol.md` §1 |
| **ADVISOR (b)** | 后台评审会话：独立上下文、只出结构化裁决、永不写 a | 同上；`application/core/goal/advisor.go` |
| **CONTROLLER** | 创建/绑定/回收 b 的运行时主体（goal 域 owner），驱动同步与生命周期 | `application/core/goal/techleader.go` `Supervisor` |
| **govern** | 回合次序/循环推进/断环的**通用编排原语** | `application/core/govern/governance.go` |
| **AgentTeam** | 角色团队运行时实例；goal 的 TL 是**第一个实例** | `docs/arch/a2a-agent-team-factory.md` |

DS-A2A 铁律 **B1–B6**：B1 会话间零共享可变态；B2 镜像单调幂等（`ref_seq` 严格递增）；B3 指令幂等（`corr` 去重）；**B4 a 永不等待 b（缺席按风险矩阵判负/升人工）**；B5 全链路有界；B6 收敛前不发新回合、reap 有护栏。（协议 §7/§8/§9）

### 1.2 上下文与缓存模型（C1–C5）

```
C_a(t) = P_a ⊕ H_a(t)                    # EXEC：全量转录，每轮尾部增长
C_b(t) = P_b ⊕ F_b(0..k) ⊕ G_b(0..j)     # ADVISOR：帧历史 + 自身回合产物，只尾部 append
```
C1 前缀稳定；C2 只尾部追加；C3 事务式评估（`on_eval`：一次性补帧 + 一次 LLM 调用）；C4 零增重估（无新帧 ⇒ 请求体全等 ⇒ 全命中）；C5 跳帧自由。（协议 §2；`advisor.go` 的 `CacheStats`/`commonPrefix`）

### 1.3 一次 goal 治理的时序

```
goal_begin(压栈 + begin 审计)
   │  GoalBeginFor 落栈成功后自动装配 goal-a2a 团队（幂等，宿主未装配团队存储只记日志）
   ▼
EXEC 推进：goal_update / 打点 / 工具调用  ──Notify──►  Supervisor.Notify（execSeq 登记）
   │  触发策略：turn_completed 跳帧(不评，带 detail 则抽 work.progress 帧)；
   │            step_checkpoint 受 EvalWindow(默认 3 轮)抑制；
   │            context_compacted / budget_warning / approval_asked / terminal_proposal 立即评
   ▼
ChatStream 返回后 AdvanceAfterChat：登记 turn_completed → 若 TL 启用则推进一轮 Governor
   │  EXEC 座位让位 → ADVISOR 座位 Act = 一次真实 TL 回合(Supervisor.RunEval)
   ▼
b 回合三段式：准入(beginRoundLocked, s.mu 内, 判定+补帧+渲染 b 输入)
            → 执行(evaluateRound, 不持 s.mu: 模型/工具回合)
            → 提交(commitRoundLocked, s.mu 内: 复核 goal + 落回合段 + 发 corr 信封)
   ▼
TLDirective 回注 a 受信注入区（不回写 a 转录主历史）
   ├─ verdict_done         → CloseTopGoalOnTerminal / gate → completed 弹栈 + 审计
   ├─ verdict_not_done     → goal 保持 active，指令待 a 领取（纠偏）
   ├─ escalate_human       → 转人工（task_needs_user_decision）
   └─ 429/超时/回合在飞     → B4 缺席矩阵（保持 active 转人工 / 直连 / 良性跳过）
```

证据：`docs/arch/a2a-agent-team-factory.md` §8；`application/core/goal/techleader.go`（`Notify`/`runRound`/三段式）；`application/core/goal/gate.go`；`application/core/goal_coordinator.go`（`AdvanceAfterChat`/`newGovernor`）。

### 1.4 帧类型（a→b）与 b→a 载荷

| a→b | 默认进 b？ | b→a | 语义 |
|---|---|---|---|
| `peer.bind` / `peer.unbind` | 生命周期 | `gate.verdict` | done/not_done/rework/escalate（响应终态提议） |
| `goal.start` | **锚点必进** | `advisory.send` | directive/evidence/escalate（纠偏/证据/升级） |
| `goal.update` | 按策略 | `advisory.ack` | a 采纳回执（审计） |
| `transcript.inc` | 默认否（跳帧控量） | `error` | 429/超时/bad_seq 等 |
| `tool.checkpoint` | 按策略（推荐 yes） | | |
| `context.compacted` | **必进**（b 需知道 a 折叠了什么） | | |
| `approval.requested` | 按策略 | | |
| `terminal.proposed` | **必进**（触发 gate） | | |

（协议 §4/§5；`application/core/goal/a2a.go` 的 `SignalKind`/`DirectiveKind`）

`TLDirective.Kind ∈ {correct, normative_prompt, checkpoint_ok, verdict_done, verdict_not_done, escalate_human, approve, deny}`；`DirectiveBreaksLoop`：done/escalate/approve/deny 断环；correct/checkpoint_ok/not_done 不断环（`application/core/goal/adapter.go`）。

### 1.5 治理循环（govern）与座位派生

```
exec-a.Act(让位，不代跑) → advisor-b.Act(真实 TL 回合) → verdict_done/escalate → 断环
                                                       → not_done/correct → 下一轮
```

座位**存在性由团队工作顺序（链表）决定**：`main`→EXEC 座位；`techlead`（kind）→ADVISOR 座位；`agent`→**员工执行面座位**（装配了 `RoleTurnRunner` 才有座位，每轮真跑一次带工具回合；未装配则**不给座位**）；`timer/user`→无治理座位。座位名历史口径 `exec-a`/`advisor-b`。（`application/core/goal_coordinator.go` 的 `seatPlan.seats`）

逃生天花板：轮次上限（默认 `defaultGoalLoopMaxRounds = 24`）或无进展 → `Break` + **`AbortOnEscape`（收口 goal=aborted + 归档 b 历史 + reap peer）**（`application/core/goal/escape.go`）。**逃逸 = 这一轮 goal 结束**（不是挂起）。

### 1.6 ADVISOR 回合的执行面（2026-09-16 升级，关键）

| 项 | 现状 | 证据 |
|---|---|---|
| 有工作区时 | **带只读工具的角色回合**（`read_file`/`grep_search`/`glob`/读工具结果），项目根绑定，权限**按构造** = `ToolPolicyReadonly` | `seelebridge/runtime_goal_tl.go` `toolRoundEnvelope`/`review` |
| 无工作区时 | 退回**无工具的一次 completer 评审**，系统提示如实声明「本回合没有工具」 | 同上 `goalAdvisorToolingWithoutTools` |
| 上下文 | `FreshContext`：按帧渲染（锚点+帧+自身回合记忆），回合间不共享引擎历史（防跨 goal 泄漏） | 同上 |
| 流式 | `OnDelta` → `TLDeltaSinkFrom(ctx)`，单向后端→观察面 | 同上；`application/core/goal/tl_stream.go` |
| 输出契约 | 固定追加 `<output_contract>`（TLDirective JSON），**员工提示词改不掉** | 同上 `goalAdvisorOutputContract` |
| 未接线 | **跑测试要 `bash`（rw 组），不在只读面内**——「能跑测试的评审者」是下一步 | 同上文件头注释（明示） |

### 1.7 现有内部评鉴（已归档，结论可直接复用）

`docs/2026-09-16-ring-escape-permission-bearing/A2A-VALUE-REVIEW.md`（**当前形态的结构判断**，`实测`+`判断`）：

- ✅ **有实际作用**：① 逃生天花板 + 收口；② 一次独立上下文的对抗评审；③ 角色权责隔离的骨架（安全资产）。
- ❌ **没有实际作用**：① 「员工/团队并行干活」（当时**没有第二个执行体**）；② 「多智能体比单 agent 更强」（等预算对照不支持同模型换角色）；③ A2A 协议互操作（进程内自用不需要）。
- ⚠️ 结构定位：**「带确定性逃生天花板的单执行体 + 一个上下文隔离的评审者」**；座位名/角色名容易被误读成「团队在做工」。

> 注：该评鉴写于 2026-09-16；此后 §1.6 的「只读工具回合」与 §1.5 的「员工执行面座位（`RoleTurnRunner`）」已落地（`README.md:726`：`RunRoleTurn` 已落地，但只为拿到治理座位的 `agent` 角色提供承重面；`review-team` 的 reviewer、`research-team` 的 researcher **目前仍只有角色会话没有执行者**）。**评鉴的②「没有第二个执行体」需要按此更新**：骨架已在，缺的是接线到具体团队。

---

## 2. 市面对照 · 协议层（A2A / MCP / ACP / ANP）

### 2.1 A2A 协议（当前一手基线）

- **身份**：Google 2025-04 发布，2025-06 捐给 Linux Foundation（`a2aproject/A2A`），**当前规范 v1.0.0**。（来源：Google Developers Blog 2025-04-09；Linux Foundation 新闻稿 2025-06-23；`a2a-protocol.org/latest/specification/`）
- **六个 Key Goals**（一手原文）：Interoperability / Collaboration / Discovery / Flexibility（同步 + SSE 流 + 异步 push）/ Security / **Asynchronicity（原生支持长任务 + human-in-the-loop）**。
- **Guiding Principles**：Simple（复用 HTTP/JSON-RPC 2.0/SSE）、Enterprise Ready、**Async First**、Modality Agnostic。
- **核心对象**：`Agent Card`（身份/能力/端点/技能/认证，用于**发现**）、`Task`（**有状态工作单元**，唯一 ID + 生命周期）、`Message`（一轮通信，role=user/agent）、`Part`（text/file/structured）、`Artifact`（产物）。
- **交互机制**：Request/Response（轮询）、**SSE 流式**、**Push Notification（webhook）**。绑定：JSON-RPC / gRPC / HTTP+JSON/REST。
- **关键语义**：**remote agent 对 client 是 opaque 黑盒**——协议层刻意不共享内部 state/memory/tools（与 DS-A2A 的「b 不读 a 全量转录」同构）。

### 2.2 A2A vs MCP（定性的关键区分，一手原文）

> **MCP 是纵向的**（deepens a single agent，连工具/资源，agent 越连越能干）；**A2A 是横向的**（connects agents across a boundary，另一个 agent 可能属于别的团队/部门/公司）。「MCP gives each agent depth, and A2A gives your system reach.」

这条对照直接回答「进程内要不要 A2A」：**两个角色在同一个进程、同一个任务内轮流发言，属于 MCP/工具域，不属于 A2A 域**。DS-A2A 是「在进程内实现 A2A 形状」——收益落在**可观测/可审计**，不落在能力。

### 2.3 协议生态全景（一手：arXiv 2505.02279 综述）

| 协议 | 定位 | 交互 | 发现 |
|---|---|---|---|
| **MCP** | JSON-RPC **client-server**，安全工具调用 + 类型化数据交换 | 同步 | 工具/资源清单 |
| **ACP** | RESTful HTTP 通用通信，MIME 多部分消息，同步+异步 | 会话式 | 轻量、runtime 无关 |
| **A2A** | **peer-to-peer 任务委派**，capability-based Agent Cards | P2P | Agent Card |
| **ANP** | **开放网络 agent 发现**，W3C DID + JSON-LD 图 | 去中心 | DID 图 |

综述给的**分阶段采用路线**：MCP（工具接入）→ ACP（多模态会话消息）→ A2A（协作任务执行）→ ANP（去中心 agent 市场）。

---

## 3. 市面对照 · Goal / 多代理编排框架（一手）

### 3.1 结论先行（三条硬证据）

1. **等预算下，同质多 agent ≈ 单 agent，但更贵**：MAST/行业对照与 Anthropic 自述均指向「多 agent 的增益主要来自 token 预算与并行广度」。Anthropic 原文：多 agent 系统比 chat **多耗 ~15× token**，agent 本身 **~4×**；在 BrowseComp 上**token 用量单独解释 80% 的方差**。
2. **能力增益的两个确证来源**：① **并行广度**（信息超出单窗口 / 多条独立方向）；② **可执行验证接地**（跑测试/编译/复现）。负收益来源：**顺序化任务、同文件编辑、强耦合编码、仪式性角色扮演**。
3. **最危险的失败模式是「互相背书」**：多 agent 讨论会因 sycophancy/conformity **先于正确结论收敛**；**一名结构化异议者**能显著降低被带偏概率——但前提是异议者有**独立信息源或可执行验证**。

### 3.2 编排模式对照表

| 框架/产品 | 模式 | 谁决定下一步 | 上下文 | 一手要点 |
|---|---|---|---|---|
| **Anthropic Building Effective Agents** | workflow vs agent；五种 workflow：prompt chaining / routing / **parallelization（sectioning+voting）** / **orchestrator-workers** / **evaluator-optimizer** | 代码路径（workflow）或模型（agent） | 各自定义 | 「成功的实现用**简单可组合的模式**，而非复杂框架」；「能找到最简方案就用最简方案」 |
| **Anthropic 多 agent research** | **orchestrator-worker**，lead 写 Memory→派生并行 subagents→CitationAgent | lead agent | subagent 各自独立窗口，只回传摘要 | 90.2% 提升；**委派契约四要素**：objective / output format / tools & sources / **task boundaries**；「多数编码任务可并行子任务更少，模型实时协调委派还不行」 |
| **Anthropic Agent SDK** | 循环 = **gather context → take action → verify work → repeat** | 模型 | subagent 隔离；compaction | subagents 两大用途：**并行** + **上下文管理**；先用 agentic search，再考虑 embedding |
| **Cognition《Don't Build Multi-Agents》** | 单线程线性 agent | 单一轨迹 | **共享完整上下文与完整 trace** | 两原则：**① Share context, and share full agent traces, not just individual messages；② Actions carry implicit decisions, conflicting decisions carry bad results**；默认排除不满足的架构 |
| **LangGraph multi-agent** | Subagents（agent as tools）/ Handoffs（state 变量+tool 调用切换）/ Skills（按需加载）/ Custom | 主 agent 或状态 | **subagent 设计上 stateless**（每次全新） | 官方已建议「**用 tool 直接做 supervisor 而非库**」以获更多 context engineering 控制；Subagents 多一次模型调用（结果经主 agent 回流） |
| **OpenAI Agents SDK** | **Handoffs**（handoff 表示为 tool，`transfer_to_<agent>`） | 模型（tool call） | 可 `input_filter` 过滤 | handoff = tool；意图分流/升级范式 |
| **AutoGen AgentChat** | `SelectorGroupChat`（LLM 选下一个发言者，默认**不连续同一人**）/ `Swarm`（handoff tool，**所有 agent 共享 message context**） | LLM 选择 或 handoff tool | **共享**广播上下文 | 两条路线：中央选择 vs 平权 handoff |
| **CrewAI** | **Sequential** / **Hierarchical**（`manager_llm`/`manager_agent`：planning + delegation + validation，「Task 不预分配」，manager 按能力分派、**评审输出、评估完成度**） | manager（hierarchical） | 任务 context 显式传递 | hierarchical = **最接近「TechLead」的现成开源形态** |
| **Magentic-One** | **Orchestrator**（plan / track progress / **re-plan to recover from errors**）+ 专业 agent（browser/file/python） | Orchestrator | worker 只看自己任务所需 | **progress ledger** + 子任务卡；**ReAct 式内外双循环：外循环管 plan/进度，内循环每步自检**；「卡住就 replan，不是多启一个评审」 |
| **MetaGPT** | **SOP 编码进 prompt 序列**，装配线分工 | 消息类型路由（`cause_by`） | 角色各自 memory/action | 核心是「**让 agent 验证中间产物**」来压制 **naive chaining 引发的级联幻觉** |

### 3.3 关于 goal / 长任务 harness（harness 承载「目标」的位置）

`docs/research/codex-claude-goal-harness-2026-09.md` 已论证：Codex `/goal`（CLI v0.128.0）与 Claude Code `/goal`（v2.1.139 + Agent View）都是**harness 子系统**（命令入口 + 目标状态机 + **完成谓词/自评停机** + 预算门禁 + 上下文延续/压缩 + UI 投影），提示词只负责「goal 负载怎么写」。

对齐到 seelex：goal 现在已是**一等对象**（Controller 栈 + 状态机 + 审计 + 第五栈持久化 + 前端投影 + 预算字段 `budget.tl_tokens_share`），**比「提示词技能」强**；仍缺的是 §4.4 的「完成谓词 harness 化 + 按任务类型开关」。

### 3.4 关于评审者 / TechLead 的市面启动方式

`docs/research/2026-09-07-a2a-techleader-startup-research.md`（一手摘要）结论：

- **没有一家把「评审角色」作为 goal/SKILL 激活的自动副作用带起来**；启动是显式/声明式事件。
- 五种模式：A 用户点名/显式分派（Codex/Claude）；B description 声明 + 模型按任务匹配（Claude 内置 subagent）；C Agent Card 发现 + client 送 task（A2A）；D 常驻演员 + 消息类型订阅（MetaGPT）；**E 常驻 Orchestrator + 按需分派专业 agent（Magentic-One）**。
- 「评审的资源生命周期和 goal 绑定，goal 收口就回收，不留常驻幽灵」——与 DS-A2A 的 `unbind+reap` 一致。

### 3.5 学术侧（评审/验证的机制证据）

| 机制 | 一手要点 | 对 TL 流程的含义 |
|---|---|---|
| **Reflexion**（arXiv 2303.11366） | 用**语言反馈**（非权重更新）强化 agent：verbal reflect + **episodic memory buffer** 引导后续 trial；HumanEval 91% pass@1 | 「把失败原因写进可复用的反思记忆」比多角色更像能力增益 |
| **Multi-agent debate**（arXiv 2305.14325） | 多个模型实例多轮**提议+辩论**再收敛共同答案，提升数学/策略推理与**事实性**（减少幻觉） | 高风险终态可用「多评审投票/辩论」；但成本 ×N |
| **LLM-as-a-judge**（arXiv 2306.05685） | 强 LLM judge 与人类偏好 >80% 一致；但有 **position / verbosity / self-enhancement bias** | 评审者输出要**结构化+可核对 refs**，防 verbosity/自偏好；不宜裸信 |
| **MAST 失败分类**（arXiv 2503.13657） | 1600+ traces / 7 框架；**14 失败模式归 3 类**：(i) system design issues (ii) inter-agent misalignment (iii) **task verification** | 「任务验证」是独立的失败大类 → **验证接地是刚需**；也说明 MAS「benchmark 增益常常很小」 |
| **AI 代码评审产品**（CodeRabbit / Greptile，一手） | CodeRabbit：path instructions + 复用 `AGENTS.md`/`.cursorrules` + custom checks；Greptile：**整仓代码图**、~3min 出评、**从 👍/👎 学习**、一键把 issue 发回执行 agent | 「TL」在产品化时 = **给执行者的结构化、可定位（file:line）、可被反馈纠正的评审**，且**默认只读/不改代码** |

---

## 4. 更好的结合方式（建议）

### 4.1 三层各归其位（最重要的一条）

```
MCP   = 深度：工具/资源/技能（单个 agent 能力面）        → seelex 工具面（已大量使用）
A2A   = 广度：跨进程/跨租户/跨厂商 opaque agent 委派与产物交换 → 未来跨边界；进程内是「形状借用」
goal  = 意图：用户目标 + 完成判定 + 预算 + 停机           → Controller 栈（已是一等对象）
TL    = 看护：独立上下文的对抗评审 + 终态 gate + 逃生       → ADVISOR（已落地，见 §4.3）
```

**推论**：进程内不要为「A2A 形状」继续膨胀（帧/信封/状态机）除非换来可观测/可审计；**真实收益场景是跨边界**。这条与 Cognition/Claude Teams/Codex 的做法一致（同宿主协作**不用** A2A，用共享 task list + 轻量引用）。

### 4.2 把 goal 状态机与 A2A Task 状态机对齐

A2A `Task` 状态机（submitted/working/input-required/completed/failed/cancelled）与 goal 状态机（`active→reviewing→completed/aborted/failed/waiting_human`）高度同构。建议**显式声明一张映射**，好处：

- goal 天然获得「**跨进程委派**」面（未来把某个 goal 委派给远程 A2A agent 时，协议对象已经在位）；
- `input-required` ↔ `waiting_human` 直接复用（HITL 语义对齐）；
- `Artifact` ↔ goal 收口的 `result/证据` 对齐（产物交换有规范位）。

代价：引入协议对象映射层；**不建议现在做**（无跨边界需求时是纯开销）——列为**预留**而非「待办」。

### 4.3 「证据接地」是 TL 唯一能兑现能力的地方（继续加码）

市场/学术两侧都指向同一结论：**增益来自可执行验证，不是角色名**。仓库已在 2026-09-16 给 ADVISOR 上**只读工具**；下一步按性价比：

1. **受限执行位（能跑测试/lint/编译）**：给评审者一把**比 EXEC 更细的执行权限位**（不是整包 `bash`），把裁决从「似真」变「证据」。这是 A2A-VALUE-REVIEW §3.3 的第 1 条，也是本轮 §1.6 明示的「下一步」。
2. **产物引用而非全文**：Anthropic 建议子代理**写文件系统、只回传轻量引用**（避免「传话游戏」与 token 复制）；与 seelex 的 `result_ref`（>20000 字符归档）天然合流。
3. **refs 必须是可核对的仓库路径**：已由 `output_contract` 约束（`refs` ≤16，仓库内相对路径）——保持并**在 gate 里校验 refs 存在性**（防「编造证据」）。

### 4.4 goal/stop 条件 harness 化（对照 Codex/Claude /goal）

- **完成谓词 + 自评停机**：不靠「轮次用尽」停机，而是「每轮收敛检查 + 汇报」。见 §3.3。
- **预算门禁**：`budget.tl_tokens_share` 已在 goal 记录里；把它接到「TL 回合计费」与「超预算 → 受管暂停/降级」（对齐 Codex「目标携带预算」）。
- **按任务类型开关 TL**：Anthropic/Codex/Claude 都明说「多数编码任务不适合多代理」；**默认常开 ring 是负期望**。建议 TL 装配条件从「有 goal」升级为「goal 类型 ∈ {review, audit, 宽面检索, 高风险终态}」。
- **提示词层收敛**：goal SKILL 保留方法论，但把「完成条件/成功标准/停机条件/预算」**结构化字段化**（goal 对象已支持 `acceptance`/`out_of_scope`/`budget`，可再补 `stop_conditions`）。

### 4.5 收敛语义对齐（避免「另一个观点」）

- **结构化异议**：`severity`(P0/P1/P2) + `refs` + 明确 `kind` 已是「结构化异议者」的骨架（§2.5 强证据方向）；继续**禁止自由文本裁决**。
- **反背书**：ADVISOR 与 EXEC **不共享完整上下文**是强项（避开 Cognition 的「半成品被并行拼接」与 sycophancy 通道）——**保持**。
- **高风险终态多评审**：`verdict_done` 属高风险不可逆动作，可引入 **voting（Anthropic parallelization）或 debate（Du et al.）**：N 个独立上下文 ADVISOR 投「done」，或对「done/not_done」做一轮辩论。成本 ×N，仅对终态。
- **收敛判据**：用「**新证据出现**才允许推翻已裁事项」（现 `constraints` 已写「不重复裁决已裁过的事，除非有新证据」）——保持并显式化。

---

## 5. 更优秀的 TechLeader 流程（具体改进项）

> 目标：把 ADVISOR 从「带逃生天花板的评审者」升级为「**有证据、可度量、只在值得时开、能纠偏也能被纠偏**的 TL」。

### 5.1 触发面（何时介入）

| 现状（已实现） | 建议 |
|---|---|
| 关键信号立即评（compacted/budget/approval/terminal）；`step_checkpoint` 受 `EvalWindow` 抑制；`turn_completed` 跳帧 | 增加**任务类型开关**：goal 创建时标注类型，只有 `review/audit/研究/高风险` 才装配 ADVISOR 回合；其余走「只收审计、不发起回合」的**观察模式**（省 token，对齐「默认常开是负期望」） |
| 事件驱动（模式 B） | 增加**里程碑策略**：`tool.checkpoint` 默认进 b（协议已推荐 yes），但按「工具是否产生可验证产物（测试/lint/构建）」分级 |

### 5.2 输入契约（给 TL 什么）

- 现状：锚点 goal 帧 + 追加帧 + 自身回合记忆（`TLSessionEmbed`）+ 只读工具。
- 建议补 **Anthropic 委派四要素的显式段**：`objective`（已由 goal 帧承载）、**`output format`（已由 output_contract 承载）**、`tools & sources`（`<tooling>` 已承载）、**`task boundaries`（缺）**——显式写「**别做什么、别重复评什么、范围边界**」，直接对应 Anthropic「没有清晰任务边界 → 重复劳动/留空白」。
- 建议补**「已知证据句柄」**：把 EXEC 侧可核对产物（diff/测试输出/`result_ref`）**以引用形式**放入 embed，而非让 TL 自己找。

### 5.3 独立性与可执行验证（TL 的「牙齿」）

- 已做：只读工具 + 项目根绑定 + `FreshContext`（隔离）。
- 补做：**受限执行位**（测试/lint）——A2A-VALUE-REVIEW §3.3 与 MAST「task verification」都指向这里。
- 边界：**不改代码**（评审者的写权限保持为 0），要动手交回 EXEC——这正是「角色权责隔离的骨架」（安全资产）。

### 5.4 裁决与收口（gate 的形态）

- 现状：`verdict_done` 收口 / `verdict_not_done` 纠偏 / `escalate_human` 转人工；`PreScreenApproval`（low 代答 / high 转人工）；B4 缺席矩阵；`ErrRoundInFlight` 不排队。
- 建议：
  1. **gate 校验 refs 存在性**（防编造证据）；
  2. **终态多评审**（voting/debate）——仅对 `verdict_done`；
  3. **`verdict_not_done` 必带可执行下一步**（`content` 里给出「补什么」），否则退化为「另一个观点」；
  4. **裁决与用户可见性**：保持「指令产出即回放进可见会话 + corr 幂等」（已实现，`DirectivePublished`/`MarkDirectivePublished`）。

### 5.5 逃生 / 看护（性价比最高，保持并强化）

- 现状：轮次上限（默认 24）+ 无进展 → `Break` + `AbortOnEscape`（收口+归档+reap）；3 次失败/预算 80% 降级（SKILL §4）。
- 建议：把逃生原因**分类进审计**（round_limit / no_progress / budget / external），供 §5.7 度量与「为什么停」的用户解释（对齐 Magentic-One 的「卡住就 replan/收束，不是多启一个评审」）。

### 5.6 团队形态（把 TL 放进多角色环）

- 现状：`goal_loop` = `user→main→tl→main→tl…`；座位按 **kind** 派生（改名不丢座位）；`agent` kind 需要 `RoleTurnRunner` 才算数。
- 建议：
  1. **V 模型团队循环**（`pm → exec → test case → 末尾评审`）落地时，用 `RoleTurnRunner` 给员工真实回合；但**评审（TL）与员工（agent）要分座位、分权限**（已由 seatPlan 区分）。
  2. **补 `order_policy = user_main_decided` 与「manager 分派」**（工厂文档已规划）：对齐 CrewAI hierarchical 的 `manager_llm`（planning/delegation/**validation**）与 Magentic-One 的 Orchestrator（plan/**track**/replan）。
  3. **岗位 vs 流程分离**：`docs/arch/agent-team-seat-vs-claim.md` 已区分「座位（座位=说话权）」与「认领（做工权）」；避免把「座位名」当「有人在干活」（A2A-VALUE-REVIEW 的误读风险）。

### 5.7 度量（没有数，任何「有用」都是叙事）

- 建议基于已有轮次/帧/回合统计做 **A/B**：同批任务「开 ADVISOR / 不开」，记录 **token 消耗** 与 **结局（达成/纠偏/逃生）**；对齐 Anthropic「token 解释 80% 方差」与 A2A-VALUE-REVIEW §3.3 第 3 条。
- 只读视图 `GoalGovernanceView`（`Round`/`CurrentSeat`/`Broken`/`BreakReason`/`RoundError`/`InFlight`）**已具备**度量素材，缺的是**汇总与对照**。

### 5.8 反模式清单（对齐 MAST 三类失败，作为 TL 设计的检查单）

| MAST 类别 | 反面案例 | seelex 对应防线 |
|---|---|---|
| **system design issues** | 职责重叠/无边界/循环 | 座位按 kind 派生；`DirectiveBreaksLoop`；轮次上限 |
| **inter-agent misalignment** | 互相背书 / 信息不一致 | ADVISOR 上下文隔离（反背书）；refs 可核对；corr 幂等 |
| **task verification** | 缺验证手段 / 验证走过场 | **只读工具（已做）→ 受限执行位（待做）**；gate 校验 refs |

---

## 6. 差距清单（按性价比排序，均为「下一步」而非「待办池」）

1. **给 ADVISOR 受限执行位（能跑测试/lint）** —— 把裁决从「观点」变「证据」（§1.6 明示的下一步；A2A-VALUE-REVIEW §3.3-1；MAST）。
2. **按任务类型开关 TL** —— 默认常开是负期望（Anthropic/Codex；§4.4）。
3. **度量 A/B** —— 开/关 ADVISOR 的 token 与结局（§5.7）。
4. **input 契约补 `task boundaries` + 证据句柄**（§5.2，Anthropic 四要素）。
5. **终态多评审（voting/debate）+ gate 校验 refs**（§5.4，Du et al. / Anthropic parallelization）。
6. **goal 完成谓词/停机条件 harness 化 + 预算门禁接线**（§4.4）。
7. **团队扩展**：`RoleTurnRunner` 接线到 review-team/research-team；补 `user_main_decided`/`manager` order_policy（§5.6）。
8. **（预留，非现做）** goal ↔ A2A Task 状态机映射，为跨边界委派留面（§4.2）。

---

## 附 A. 对外一手来源

| 来源 | 用途 | 抓取 |
|---|---|---|
| `a2a-protocol.org/latest/specification/` | A2A v1.0 Key Goals / Guiding Principles / 对象模型 / 绑定 | ✅ 浏览器一手 |
| `a2a-protocol.org/latest/topics/key-concepts/` | Agent Card / Task / Message / Part / Artifact / 交互机制 | ✅ |
| `a2a-protocol.org/latest/topics/a2a-and-mcp/` | MCP 纵向 vs A2A 横向 | ✅ |
| Google Developers Blog《Announcing A2A》2025-04-09 | A2A 发布 | ✅（搜索摘要/结果页） |
| Linux Foundation 新闻稿 2025-06-23 | A2A 项目捐给 LF | ✅（搜索摘要/结果页） |
| arXiv 2505.02279 | MCP/ACP/A2A/ANP 综述 + 分阶段路线 | ✅ |
| Anthropic《Building effective agents》 | workflow 五模式 + evaluator-optimizer | ✅ |
| Anthropic《How we built our multi-agent research system》 | orchestrator-worker / 90.2% / 15× / 委派四要素 / 何时不用 | ✅ |
| Anthropic《Building agents with the Claude Agent SDK》 | gather→act→verify→repeat；subagent 两用途 | ✅ |
| Cognition《Don't Build Multi-Agents》 | 共享上下文/完整 trace 两原则 | ✅ |
| LangGraph multi-agent 文档 | 四种模式 + stateless subagents + 「用 tool 做 supervisor」 | ✅ |
| OpenAI Agents SDK `handoffs` | handoff = tool | ✅ |
| AutoGen `SelectorGroupChat` | LLM 选下一个发言者 + 共享广播 | ✅ |
| CrewAI `Processes` | Sequential / Hierarchical（manager planning+delegation+validation） | ✅ |
| Magentic-One（arXiv 2411.04468） | Orchestrator plan/track/**replan** + 专业 agent | ✅ |
| MetaGPT（arXiv 2308.00352） | SOP 装配线 + 验证中间产物 + 抗级联幻觉 | ✅ |
| MAST（arXiv 2503.13657） | 14 失败模式 / 3 类别（含 task verification） | ✅ |
| Reflexion（arXiv 2303.11366） | verbal feedback + episodic memory | ✅ |
| Multi-agent debate（arXiv 2305.14325） | 多实例辩论提事实性 | ✅ |
| LLM-as-a-judge（arXiv 2306.05685） | >80% 一致 + position/verbosity/self-enhancement bias | ✅ |
| Claude Code `agent-teams` / `sub-agents` 官方文档 | lead+teammates / shared task list / 直接互发 / hooks 质量门；description 匹配 + 只读 Explore | ✅ |
| Codex 官方 subagent 文档 | 并行 subagent、主线程收集、AGENTS.md/skill 触发、读写小心 | ✅ |
| CodeRabbit `review-instructions` / Greptile 文档 | path instructions / 复用 AGENTS.md / 整仓图 / 反馈学习 / 把 issue 发回执行 agent | ✅ |

## 附 B. 仓库内证据

- 协议/详设：`docs/2026-09-07-seele-a2a-framework-req/{README,ds-a2a-protocol,ds-a2a-detailed-design}.md`
- 治理循环：`docs/2026-09-08-govern-loop/{README,design}.md`、`application/core/govern/governance.go`
- 团队工厂：`docs/arch/a2a-agent-team-factory.md`、`docs/arch/agent-team-seat-vs-claim.md`、`docs/arch/agent-team-work-vs-market.md`
- goal 域：`application/core/goal/README.md`、`{a2a,controller,techleader,advisor,gate,adapter,escape,directive,tl_stream,audit}.go`
- 装配：`application/core/goal_coordinator.go`、`seelebridge/runtime_goal_tl.go`、`register_goal_tools.go`
- SKILL：`plugins/default/goal/SKILL.md`
- 既有评鉴/调研：`docs/2026-09-16-ring-escape-permission-bearing/A2A-VALUE-REVIEW.md`、`docs/research/2026-09-07-a2a-techleader-startup-research.md`、`docs/research/codex-claude-goal-harness-2026-09.md`、`docs/research/goal-context-governance-2026-09.md`、`docs/research/2026-09-17-fork-subagent-ownership-and-multiagent-orchestration.md`

## 附 C. 未核实 / 局限（不据此推断）

1. 本轮 `web_search` 工具 403（配额），外部证据靠浏览器抓发布方一手页面 + arXiv abs 页；**不是穷举检索**，未做任何原始实验。
2. LangGraph `multi_agent` 页抓到的内容以 Graph API 章节为主（multi-agent 模式表以仓库既有 `2026-09-17` Part B 的一手摘录为准）。
3. Claude Code `agent-teams`/`sub-agents` 为实验特性文档，行为随版本变化（`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` 默认关）。
4. A2A 规范版本以抓到的 `latest`（v1.0.0）为准；协议仍在演进。
5. `2026-09-16 A2A-VALUE-REVIEW` 的「没有第二个执行体」结论**已被 2026-09 下旬改动部分推翻**（`RunRoleTurn` 落地、ADVISOR 只读工具回合落地）——引用该评鉴时须按 §1.6/§1.7 注更新。
