# Agent Team 第二阶段反思：市面对照、现状诊断，与 goal / V 模型的关系

> **用途**：第三阶段（「V 模型 agent team work loop」）动工前的讨论输入。本文回答三个问题：
> ① 市面主流的 agent team 协作怎么做？② 我们现在的 agent team 协作为什么仍是「玩具」、要改什么？
> ③ goal 会从「敏捷开发」走到 V 模型吗？——先把 ①②③ 摊开，再收敛到需要拍板的几个 fork。
>
> **口径声明（先读）**：
> - **本文 = 本轮仓库静读**（逐条带 `文件` / `文件:符号`）+ **引用仓库既有研究/评鉴**（标注来源与日期）
>   + **显式标注的判断**。凡判断一律写明「判断」，不冒充实现事实。
> - 本轮 `web_search` 工具返回 **403（配额不足）**，外部对照**全部转引**自
>   [`docs/research/2026-09-30-goal-a2a-flow-and-techleader.md`](../research/2026-09-30-goal-a2a-flow-and-techleader.md)（该文含一手浏览器取证），
>   **本文未在线复核**。引用外部数字前须回原出处，见该文 §5/§6。
> - **本文不改任何代码**，是「掌握 full picture + 商榷口径」的产物，供第三阶段动工做输入。

---

## 0. 一页结论

| 问题 | 结论 |
|---|---|
| 市面主流 | 进程内协作用「**共享 task list + 轻量引用 + description 匹配**」（Claude Teams / Codex / LangGraph subagents），**不用 A2A**；A2A（LF v1.0）解决的是**跨进程/跨厂商 opaque agent 互操作**。能力增益只来自**① 并行广度 ② 可执行验证接地**，**不来自角色分工**。 |
| 现状是什么 | 不是「多智能体团队」，而是「**单执行体（EXEC）+ 一个上下文隔离的评审者（ADVISOR/TL）+ 一条确定性治理循环 + 确定性逃生天花板**」。默认流程里**没有第二个真正干活的执行体**。 |
| 要改什么 | 按性价比：① 接第二个执行体（V 模型 preset + 员工回合接线）② 评审者「牙齿」（受限执行位 + gate 校验 refs）③ 补 orchestrator/manager ④ 按任务类型开关 TL ⑤ A/B 度量 ⑥ 进程内不再膨胀 A2A ⑦ 座位/认领分层叙事。 |
| goal 走 V 模型吗 | **不是「goal 走 V 模型」，而是「团队顺序拓扑换成 V 模型；goal 域只做两处适配」**。goal 与流程正交（它是「意图 + 完成判定 + 逃生」的宿主）；所谓「敏捷」只是**已归档原型**的标题，不是 goal 的实质。真正要动 goal 域的只有一件事：**把平铺的 `acceptance` 升级为「阶段↔验证」配对**。座位映射 `seatPlan.seats` 已按 kind 派生并预留了 V 模型口径。 |

---

## 1. 口径拆分：`goal` / `agent team` / 编排内核 是三件事

这是整场讨论最容易打结的地方，先钉死。三层的职责与落点互不重叠：

| 层 | 回答的问题 | 代码落点 |
|---|---|---|
| **goal** | 「要什么 + 算不算做完 + 卡住怎么办」 | `application/core/goal/`：Controller 活动栈 + 终态 gate（`gate.go`）+ 逃生（`escape.go`）；契约面 `goal_coordinator.go` |
| **agent team** | 「谁在编、谁先谁后、谁有座位」 | `application/core/agentteam/`：`presets.go` / `spec.go` / `registry.go` / `runtime.go`（`order_policy` / `order_roles`） |
| **编排内核** | 「这一轮谁说话」 | `application/core/govern/` 循环 + `goal_coordinator.go` 的座位派生（`newGovernor` / `seatPlan.seats`） |

当前的真实驱动链（**本轮静读**）：

```
用户 / 主会话轮  ──►  goal 治理循环（唯一驱动力）
                         │  seatsFor(sessionID) 读团队注册表
                         │  → seatPlan.seats 按 RoleKind 派生座位
                         ▼
            exec-a(让位) → advisor-b(真实 TL 回合) → 员工座位(roleTurnSeat.Act)
                         │
                         └─► advanceAfterChat：本轮结束推进一轮；断环/逃生 → AbortOnEscape 收口
```

关键事实：**goal 治理循环是唯一驱动力，team 环只决定「谁有座位」**。`application/core/agentteam/runtime.go` 头注释已自陈：`TurnScheduler` 只提供链表轮转原语，真正驱动轮次的是 goal 治理；`docs/devlog/2026-09-17-summon-starts-goal.md` §6 亦确认 `TurnScheduler` **至今未接线**。

> 把这三层分开后，问题 ③（goal 要不要走 V 模型）会自动变形——见 §4。

---

## 2. 市面主流做法

> 以下为**转引**（来源见文首口径声明），本轮未在线复核；机制描述不针对任何产品的具体版本。

### 2.1 两个正交的轴：进程内 vs 跨边界

- **进程内协作**：主流是「**共享 task list + 轻量引用（`result_ref` 式）+ description 匹配**」，**不用 A2A**。
  代表：Claude Teams（lead + teammates、共享任务清单、队友可互发）、Codex（并行 subagent、主线程收集）、
  LangGraph subagents（subagent 设计上 stateless）。
- **A2A 协议**：Google 2025-04 发布、Linux Foundation 维护，**当前规范 v1.0**。它解决的是
  **跨进程/跨厂商 opaque agent 的互操作与产物交换**；核心对象 `Agent Card` / `Task` / `Message` /
  `Part` / `Artifact`。协议层刻意**不共享内部 state/memory/tools**。
- **MCP vs A2A**（一手原文对照）：**MCP 纵向**（加深单个 agent，连工具/资源），**A2A 横向**
  （连接边界外的 agent）。→ 推论：**同进程让几个角色轮流发言，属于工具域，不属于 A2A 域**；
  进程内用 A2A 是「形状借用」，收益落在**可观测/可审计**，不落在能力。

### 2.2 编排模式谱系（谁决定下一步）

| 模式 | 代表 | 谁决定下一步 | 关键要点 |
|---|---|---|---|
| orchestrator-workers | Anthropic 多 agent research | lead 写 Memory → 并行 subagents | 委派四要素：objective / output format / tools & sources / **task boundaries**；子代理写文件系统、只回传轻量引用 |
| workflow 五模式 | Anthropic《Building effective agents》 | 代码路径 | prompt chaining / routing / parallelization / orchestrator-workers / **evaluator-optimizer** |
| handoff = tool | OpenAI Agents SDK | 模型 tool call（`transfer_to_<agent>`） | 意图分流/升级范式 |
| selector group | AutoGen `SelectorGroupChat` | LLM 选下一个发言者 | 默认不连续同一人；`Swarm` 共享 message context |
| **hierarchical / manager** | CrewAI（`manager_llm`：planning + delegation + **validation**）、Magentic-One（Orchestrator：plan / track / **replan**） | manager / orchestrator | 最接近「TechLead」的现成开源形态 |
| SOP 流水线 | MetaGPT | 消息类型路由 | 让 agent 验证中间产物，压级联幻觉 |
| 单线程共享上下文 | Cognition《Don't Build Multi-Agents》 | 单一轨迹 | 共享完整上下文与完整 trace |

### 2.3 三条会直接决定我们改法的硬结论

1. **等预算下同构多 agent ≈ 单 agent 且更贵**：Anthropic 自述多 agent 比 chat 多耗 ~15× token、
   agent 本身 ~4×，token 用量单独解释 80% 方差。
2. **能力增益只有两个确证来源**：**① 并行广度**（信息超出单窗口）**② 可执行验证接地**（跑测试/编译/复现）。
   负收益来源：**顺序化任务、同文件编辑、强耦合编码、仪式性角色扮演**。
3. **最危险的失败模式是「互相背书」（sycophancy/conformity）**：多 agent 会**先于正确结论收敛**；
   解法是一名**有独立信息源或可执行验证的结构化异议者**（对应 MAST 的 `task verification` 失败大类）。

### 2.4 主流「呼出 / 召唤」形态

没有一家把「评审角色」作为 goal 激活的自动副作用带起来；启动是**显式/声明式事件**（`docs/research/2026-09-07-a2a-techleader-startup-research.md`）：

- **A** 用户点名/显式分派（Codex / Claude `/goal`）；
- **B** description 声明 + 模型按任务匹配（Claude 内置 subagent）；
- **C** Agent Card 发现 + client 送 Task（A2A）；
- **D** 常驻演员 + 消息类型订阅（MetaGPT）；
- **E** 常驻 Orchestrator + 按需分派专业 agent（Magentic-One）。

---

## 3. 现状诊断：为什么协作仍是「玩具」

### 3.1 真实形态（本轮静读）

**「单执行体（EXEC = 主会话，唯一 writer）+ 一个上下文被刻意隔离的评审者（ADVISOR/TL，只出结构化裁决）+ 一条确定性治理循环（govern）+ 确定性逃生天花板」**——不是「多智能体团队」。
（此结构判断沿用 `docs/2026-09-16-ring-escape-permission-bearing/A2A-VALUE-REVIEW.md`；该评鉴的「没有第二个执行体」一条须按 09 月下旬改动更新，见 §3.2 第 1 项。）

### 3.2 根因（逐条带锚点）

| # | 根因 | 证据（本轮静读） |
|---|---|---|
| 1 | **默认流程没有第二个真正干活的执行体** | `application/core/agentteam/presets.go`：`goalA2APreset` 的 `OrderRoles = [user, main, tl]`，角色**没有任何 `RoleKindAgent`**。而 `goal-a2a` 既是「goal 上线自动装配」的团队（`goal_service.go:ensureGoalAgentTeam`），也是 `@` 召唤的默认目标。 |
| 2 | **员工执行面已接线，但缺「带角色的预设」** | `application/core/service_assembler.go:122`：`RoleTurnFor: service.roleTurnRunnerFor`；`application/core/role_turn.go`：`roleTurnRunnerFor` **只按端口装没装判断**（装了 `contract.RoleTurnPort` 即非 nil）；`seelebridge/runtime_role_turn.go` 实现该端口，头注释已写「**V 模型团队循环里 pm/exec/test_case 各自做工**」。→ 机制在，只差预设里有这些角色。 |
| 3 | **座位只有在 goal 活跃时才被驱动** | `goal_coordinator.go` 的 `AdvanceAfterChat`：`ctl.Status().Active == nil` 直接返回；`seatPlan.seats` 里 `RoleKindAgent` **必须装配 `RoleTurnRunner` 才给座位**（否则「宁可少一座，不要假一座」）。`docs/devlog/2026-09-17-summon-starts-goal.md`：`@` 曾「只装配、不落 goal」，于是 tl 永远不上场——09-17 才补成「带附言的召唤才落 goal」。 |
| 4 | **评审者没有「牙齿」** | ADVISOR 已是「带**只读**工具的角色回合」（`seelebridge/runtime_goal_tl.go`），但**跑测试的 `bash` 属 `rw` 组，不在只读面内**（见 `2026-09-30` 研究 §1.6 明示的「下一步」）。→ 裁决仍是「观点」而非「证据」，正是 MAST `task verification` 类失败。 |
| 5 | **环未接线 + 产物证据未校验** | `TurnScheduler` 未接线（§1）；`output_contract` 已要求 `refs` 为仓库内相对路径，但 **gate 未校验 `refs` 存在性**（`2026-09-30` 研究 §5.4）→ 防不住编造证据。 |

**一句话**：现在值钱的是**看护**（逃生/审计/权责边界/上下文隔离），不值钱的是**宣称的团队产出能力**；「玩具感」正来自把「评审仪式」误读成「团队在做工」（座位名 `exec-a` / `advisor-b` 也在放大这种误读）。

---

## 4. goal 从「敏捷开发」走 V 模型吗？

**不是。这里有一个范畴错误要先拨正。**

### 4.1 「敏捷」是历史标题，不是 goal 的实质

所谓「敏捷开发」只是 **2026-08-07 那个已归档原型**的标题
（[`docs/2026-08-07-agile-a2a/`](../2026-08-07-agile-a2a/design.md)：TL × Programmer、verify 失败回边循环）。
该原型**已被明确废弃**，被 DS-A2A 双会话治理取代（见 `docs/2026-09-07-seele-a2a-framework-req/`）。

真正活下来的 `goal` 域（Controller / Supervisor / gate）是**目标状态机 + 完成判定 + 逃生天花板**——
它**没有 sprint、没有 backlog、没有迭代增量**。`plugins/default/goal/SKILL.md` 的表述也是「**GOAL 方法论** + 直接顺序执行、默认不用 plan DAG」，与「敏捷迭代」无关。
→ **goal 从来就不是「敏捷」，只是它的祖辈文档标题里写了「敏捷」。**

### 4.2 正确拆解：goal 是宿主，V 模型是「顺序事实」

- **goal 与流程正交**：它不该「走」去任何流程，它是**宿主**（意图锚点 + 完成判定 + 逃生）。
- **V 模型是一种「团队顺序拓扑」**（一种 `order_policy` / preset 组成），属于 §1 的中间层，**不等于 goal 域**。
- 代码其实早按这个拆分预留了：`goal_coordinator.go` 的 `seatPlan.seats` 注释白纸黑字写着
  「**V 模型团队循环口径：pm → exec → test case，末尾评审**」，且**按 `RoleKind`（不是角色名字面量）派生座位**
  （改名不丢座位）。→ **goal 已经是 V 模型 loop 的宿主，只差预设里有这几个角色 + 执行面接线。**

### 4.3 goal 域真正要改的，只有两处

1. **验收结构化（唯一真要动 goal 语义的地方）**：
   V 模型的核心是「**左侧每个分解阶段 ↔ 右侧一个验证阶段**」的配对
   （需求↔验收测试、设计↔集成测试、模块↔单元测试）。goal 现在 `acceptance []string` 是**平铺**的验收项，
   没有阶段配对；要改成「阶段↔验证」结构，**gate 才能逐阶段判「证据齐不齐」**。
   这也正好把 §2.3 结论 2 的「可执行验证」制度化。
2. **座位映射（已预留）**：`seatPlan.seats` 已按 kind 派生、已写 V 模型口径 →
   只需**新增一支 V 模型 preset（pm/exec/test_case + 末尾评审）并把 `RoleTurnRunner` 接到这些 `agent` 座位**；
   `advanceAfterChat` 的 `attempts = len(Seats)+1` 多座轮转也已支持。

### 4.4 警示：V 模型要挑任务开，不要默认常开

V 模型本身偏顺序（左→右做完再做右），而 §2.3 结论 2 说「**顺序化任务/强耦合编码是多代理的负收益来源**」。
所以 V 模型对 agent team 的**真正价值是「验收可追溯」**（把每阶段配验证写进流程），
**不是并行加速**。它应当**按任务类型开关**（review/audit/高风险才开），**默认常开是负期望**。

> **一句话**：不是「goal 从敏捷走 V 模型」，而是「**团队顺序从 `goal_loop(user→main→tl)` 拓扑换成 V 模型拓扑；
> goal 域只把 `acceptance` 从平铺升级为阶段配对，座位映射它早就预留了**」。

---

## 5. 改动清单（按性价比排序，均属「下一步」而非「待办池」）

1. **接上第二个执行体**：新增 V 模型 preset（pm/exec/test_case/reviewer）+ 把 `RoleTurnRunner` 接到这些 `agent` 座位。
   这是「从玩具到团队」的**唯一必要动作**（其余皆为锦上添花）。
2. **给评审者「牙齿」**：受限执行位（能跑 test/lint/编译，而非整包 `bash`）+ **gate 校验 `refs` 存在性**。
   把裁决从「似真」变成「证据」。
3. **补 orchestrator/manager 角色与 `order_policy`**：对齐 CrewAI hierarchical / Magentic-One 的 plan·track·replan；
   现状只有 `goal_loop` 与 `user_main_decided`，缺 manager 分派/validation。
4. **按任务类型开关 TL**：从「有 goal 就开」升级为「goal 类型 ∈ {review/audit/宽面检索/高风险终态} 才开」。
5. **度量 A/B**：同批任务「开/不开 ADVISOR」的 **token 与结局**。`GoalGovernanceView` 已有
   `Round / CurrentSeat / Broken / BreakReason / InFlight` 素材，缺汇总与对照。**没有数，「有用」就只是叙事。**
6. **进程内不再膨胀 A2A 帧**：真实收益在跨边界；进程内继续用「唯一帧账本 + 裁决入账」。
   `goal ↔ A2A Task` 状态机映射**预留、不现做**（无跨边界需求时是纯开销）。
7. **叙事分层**：把「座位制（seat）= 治理面」与「认领制（claim）= 执行面」当一个「治理 + 执行」方案讲
   （[`agent-team-seat-vs-claim.md`](agent-team-seat-vs-claim.md) 已铺好），别让座位名继续被误读成「有人在干活」。

---

## 6. 待商榷的 fork（需产品或架构拍板）

1. **goal 的 `acceptance` 是否引入「阶段↔验证」结构？**
   —— 这是唯一真正动 goal 域语义的改动，会波及 gate 判据与面板投影。
2. **V 模型 loop 是新增一个 `order_policy`，还是复用 `user_main_decided` + 一个 manager 角色？**
3. **TL 默认开关策略**：默认关、按任务类型开（研究建议），还是先默认开一段做对照？
4. **是否现在做 `goal ↔ A2A Task` 状态机映射？** 建议预留不现做。
5. **「V 模型比敏捷更适合 agent team」如何度量证实？** 需一次同任务、同模型的 A/B（否则仍是判断）。

---

## 附：锚点索引

**代码（本轮静读）**

- 座位派生与治理循环：`application/core/goal_coordinator.go`（`seatPlan.seats` / `newGovernor` / `AdvanceAfterChat` / `roleTurnSeat` / `RoleTurnRunner`）
- 团队 preset 与顺序事实：`application/core/agentteam/{presets,spec,registry,runtime,scheduler}.go`
- 员工执行面接线：`application/core/role_turn.go`、`application/core/service_assembler.go:119-122`、`application/core/agentteam_runtime.go`（`teamRoleSeatsFor`）
- 员工回合执行体：`seelebridge/runtime_role_turn.go`（`RunRoleTurn`）；契约 `application/contract/ports.go`（`RoleTurnPort` / `Dependencies.RoleTurn`）
- 评审者回合与只读工具：`seelebridge/runtime_goal_tl.go`
- goal 方法论：`plugins/default/goal/SKILL.md`

**文档**

- 市面对照（一手取证）：[`docs/research/2026-09-30-goal-a2a-flow-and-techleader.md`](../research/2026-09-30-goal-a2a-flow-and-techleader.md)
- 机制差异姊妹篇：[`agent-team-work-vs-market.md`](agent-team-work-vs-market.md)、[`agent-team-seat-vs-claim.md`](agent-team-seat-vs-claim.md)
- 团队工厂（目标态）：[`a2a-agent-team-factory.md`](a2a-agent-team-factory.md)
- 结构评鉴（历史，须按 09 月下旬改动更新）：[`../2026-09-16-ring-escape-permission-bearing/A2A-VALUE-REVIEW.md`](../2026-09-16-ring-escape-permission-bearing/A2A-VALUE-REVIEW.md)
- 召唤链路：[`../devlog/2026-09-17-summon-starts-goal.md`](../devlog/2026-09-17-summon-starts-goal.md)、[`../devlog/2026-09-17-input-sigils-and-manual-team-summon.md`](../devlog/2026-09-17-input-sigils-and-manual-team-summon.md)
- 记录与数据流：[`../2026-09-16-team-work-record-dataflow/README.md`](../2026-09-16-team-work-record-dataflow/README.md)
- 已归档的「敏捷」原型：[`../2026-08-07-agile-a2a/design.md`](../2026-08-07-agile-a2a/design.md)
