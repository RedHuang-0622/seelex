# 市面 Agent 产品如何"启动"评审/技术负责人角色（A2A 语境，2026-09）

> 调研日期：2026-09-07。方法：厂商官方文档与公开论文直接抓取（A2A 官方、Claude Code 官方、Codex
> 官方、MetaGPT 源码分析站、Magentic-One 论文摘要），辅以仓库内既有设计基线
> （`docs/2026-09-07-seele-a2a-framework-req/`、`docs/2026-09-07-goal-domain-techleader/`）。
>
> 提问语境：Seelex 的 `#goal` 当前只激活提示词级 SKILL（压入 goal SKILL、放开 MaxLoops、点亮 GOAL badge），
> 并不创建 goal 状态、也不启动 ADVISOR/techleader。用户问：市面上的 A2A/Agent 产品到底是怎么把
> **评审/技术负责人（tech leader / reviewer / advisor）这类角色拉起来的**？
>
> 结论先行：**没有一家是把"评审角色"作为 goal/SKILL 激活的自动副作用带起来的**。启动一个
> "角色"（尤其评审/顾问）在成熟产品里是**一次显式或声明式的事件**：要么用户直接点名
> （"spawn 一个 reviewer"），要么任务与角色 description 匹配后由主代理按需分派，要么按
> 消息类型/里程碑事件让常驻协调者触发。goal 类机制提供的不是角色，而是**目标/任务状态对象**
> （task ledger / goal 栈），评审什么时候介入由状态机与事件决定，而不是由"激活一个技能"决定。

---

## 0. 证据分级与方法边界

本文事实按可靠度分级：

- **【一手官方】**：抓到的官方文档原文或论文原文（A2A key-concepts、Claude Code sub-agents / agent-teams、
  Codex 官方 subagents 文档、arXiv Magentic-One 摘要）。
- **【源码分析站】**：DeepWiki 对 MetaGPT 源码的角色机制解读（未逐一核对仓库行号）。
- **【仓库实证】**：Seelex 本地代码/文档事实（`#goal` 只做 SKILL 激活等）。

未能访问的页面（如 OpenHands microagents 文档两次 FETCH_TIMEOUT）不作为事实引用；相关厂商只列
已读到的证据。代理环境网络受限，本报告不把"搜索摘要转述"当官方事实。

---

## 1. 一个关键区分：goal 是"目标状态"，不是"角色启动器"

调研对象里，凡是提供 goal / 长任务的产品，goal 的产物都是一个**可枚举、有状态的任务对象**，
而不是"召唤一个 tech leader"：

| 产品 | goal / 任务的实体 | 与"评审/负责人角色"的关系 |
|---|---|---|
| Codex（OpenAI） | 目标生命周期对象（预算、续轮、完成判定） | 子代理由显式请求或 AGENTS.md/skill 指令触发，description 匹配决定用哪个角色 |
| Claude Code | Agent View 中的目标/任务条目；agent team 的共享 task list | 内置 Explore/Plan 按 description 自动匹配；teammate 由 lead 用 Agent tool 点名 spawn |
| Google A2A 生态 | Task（stateful 生命周期对象） | 对方 agent 常驻在 Agent Card 后；client 读卡后显式送 task，无自动拉起 |
| MetaGPT | 角色实例 + 消息（cause_by 路由） | 角色常驻订阅消息类型；评审是"收到触发消息才行动"的演员 |
| Magentic-One | Orchestrator 维护的 task/progress 状态 | 专业 agent 常驻或按需被 Orchestrator 分派；无"评审自动出现"语义 |

推论：如果 Seelex 想"`#goal` 之后真出 A2A/techleader"，缺口不在 prompt 层，而在 **goal 实体化 +
角色生命周期挂钩**——即 `goal_begin → spawn ADVISOR(b)`（详设 §8 主线接线的目标状态），
而不是让 SKILL 激活自动拉人。

---

## 2. 市面五种"启动评审/协作角色"的模式

### 模式 A：用户直接点名 / 显式分派（Codex、Claude Code）

【一手官方】当前 Codex 本地客户端"spawn agents after a direct request or applicable project or skill
instruction"；官方示例就是"spawn one agent per point"式的显式文本。Claude Code 中用户可以直接
说 "Use the code-improver agent to ..." 触发自定义 subagent；agent team 场景下 lead 调用 Agent tool
并给 teammate 命名来 spawn，默认关闭（`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` 才启用）。

启动时机：**对话中出现明确的委派请求**。谁启动：主代理（模型侧决策）或 harness（解析指令）。
生命周期：一次性任务，产出 summary 回主线程后关闭。

对 Seelex 的启示：评审角色若要"人工可控"，最低成本的杠杆是 **LLM 可调用的 spawn/分派工具**
（如 `goal_begin` 之外单独的评审请求），而不是技能激活。评审属于 review 任务，用户通常愿意
显式说一次。

### 模式 B：description 声明 + 模型按任务匹配（Claude Code 内置/自定义 subagent）

【一手官方】Claude Code 的自定义 subagent 是 `.claude/agents/*.md`，front matter 里 `description`
写"何时用我"；Claude 遇到匹配任务就 delegate。内置 Explore / Plan 也靠 description 被自动选用。
Codex 的自定义 agent（TOML）同样要求 `description` 字段，作为模型选择 agent 的依据。

启动时机：**任务特征命中 description**（自动），或用户指名（显式）。谁启动：主模型决定。
生命周期：按次 spawn，独立上下文窗口，受限工具面（评审常用只读）。

对 Seelex 的启示：若 ADVISOR 有清晰的触发面（"里程碑评审/终态 gate/审批预筛"），可以把触发面
写进 ADVISOR 角色定义与事件分类（`SignalKind`），由**事件**命中而不是由模型自觉想起。

### 模式 C：Agent Card 发现 + client 送 task（Google A2A 协议）

【一手官方】A2A 中 Agent Card（identity / capabilities / endpoint / skills / auth）是发现与交互的
入口；Task 是有唯一 ID 和生命周期（submitted/working/input-required/completed/failed）的工作单元，
由 **client 发起**。服务端（远程 agent）对 client 是不透明黑盒。

启动时机：client 决定需要对方能力时（读卡 → 建 task）。谁启动：请求方 client。生命周期：
task 级有终态；agent 本身是否常驻由部署方决定，**协议不规定自动拉起**。

对 Seelex 的启示：DS-A2A 的 EXEC(a) 就是 A2A client 的角色，ADVISOR(b) 是 server 侧角色；
协议层 b 的启动=CONTROLLER 在 `goal.start` 锚点后执行 spawn（协议时序 t0），与 A2A "client
发起 task"同构。可见性靠帧与水位，不靠共享转录——b 黑盒语义天然符合 A2A。

### 模式 D：常驻演员 + 消息类型订阅（MetaGPT）

【源码分析站】MetaGPT 的 Role 是 observe-think-act 循环的常驻对象：每个角色维护自己的
memory/action，通过 `watch` 集合订阅特定 `cause_by` 的消息类型，只有匹配消息到达才进入
`_observe → _think → _act`；消息路由按 role name / profile / broadcast。

启动时机：**初始化即常驻，行动按消息类型触发**。谁启动：团队编排（environment/team）在开局
创建角色表。生命周期：随团队会话常驻，直到任务结束；无独立"评审启动"动作。

对 Seelex 的启示：这解释了"角色靠订阅而非拉起的另一条路线"，但其代价是**所有角色共享同一套
消息总线与内存语义**，与 DS-A2A 的 b 独立会话 + 帧镜像路线相反。Seelex 选择 b 独立会话，
等价于把"订阅"换成"CONTROLLER 显式转发帧"，缓存特性更可控。

### 模式 E：常驻 Orchestrator + 按需分派专业 agent（Magentic-One）

【一手官方】Magentic-One 中 Orchestrator 是 lead agent："plans, tracks progress, and re-plans
to recover from errors"，执行中把任务分派给 browser / file / python 等专业 agent。

启动时机：任务一开始就有 Orchestrator，专业 agent 在执行中被指派。谁启动：Orchestrator。
生命周期：随任务起止；失败/卡住触发 replan，不是"多启一个评审"。

对 Seelex 的启示：Orchestrator 只做 plan/track/replan，**不执行**；专业 agent 是执行者。
映射到 DS-A2A：EXEC 同时承担执行与轻量编排，ADVISOR 承担评审——两者的分界是
"谁持有目标状态与完成判定"（CONTROLLER），不是谁先被用户点名。

---

## 3. 触发时机矩阵（评审/负责人什么时候被"拉起"）

| 产品/方案 | 角色怎么来 | 谁启动 | 评审何时介入 | 生命周期 |
|---|---|---|---|---|
| Codex subagents | 用户点名或指令/技能声明 | 主 agent / harness | 委派请求出现时 | 单任务，summary 后回收 |
| Claude Code subagents | description 自动匹配或用户指名 | 主 agent | 任务匹配 description | 单任务，独立上下文 |
| Claude Code agent teams | lead 调 Agent tool 命名 spawn（实验默认关） | lead | lead 决定或 teammate 自领 task | 团队会话，显式 shutdown |
| Google A2A | Agent Card 常驻端点 | 请求方 client | client 建 task | task 有终态；agent 常驻与否与协议无关 |
| MetaGPT | 开局注册角色表 | environment/team | watch 命中的消息类型到达 | 团队会话常驻 |
| Magentic-One | Orchestrator 分派 | Orchestrator | 执行需要某专业能力 | 随任务；卡住 replan |

共同点（也是给 Seelex 的最重要结论）：

1. **没有"SKILL 激活 → 自动 spawn 评审"的组合**；评审/顾问要么被显式点名，要么被事件/匹配触发，
   且触发后是**有界的一次性或阶段性子任务**。
2. **评审有独立的角色定义（description/系统提示/工具面）**，通常只读；这与 Seelex
   ADVISOR"无工具、只读工具面待定"的设计一致（Q4 建议先无工具）。
3. **评审的资源生命周期和 goal 绑定**：goal 收口/abort 就回收（unbind+reap），不留常驻幽灵。

---

## 4. 与 Seelex `#goal` 现状的差距分析

### 现状（仓库实证）

- `#goal` 激活的是 `plugins/default/goal/SKILL.md`：prompt 压入、MaxLoops 放开、GOAL badge 点亮、
  plan 工具可见；**不创建 goal 栈、不启动 Supervisor/ADVISOR**。
- goal 工具族（`goal_begin` / `goal_tl_*` / `goal_propose_finish` 等）与 Supervisor 只存在于
  `application/core/goal` 包及 headless 契约，生产工具面（seelebridge/main/gui）未注册；
  因此对话里即使提到 goal，也没有可调用的 `goal_begin` 钩子把 b 拉起来。
- 基线与详设已把目标状态写明：`goal_begin → spawn b（只读工具面）`（协议时序 t0 /
  详设 §8 主线接线）；b 生命周期由 CONTROLLER 管理，`peer.unbind` 收口即 reap。

### 差距：缺的不是"激活语义"，是"实体化 + 工具化"

对照模式 A/C：市场产品的 goal 实体（Codex 目标状态机、A2A Task）都是**可创建的状态对象**，
创建动作是受管入口（slash command / goal_begin / task 请求）。Seelex 的 `#goal` 只是技能提示，
`goal_begin` 又未进生产工具面——两头悬空：

```text
市面（Codex/Claude/A2A）：#goal(入口) ─► goal 对象/任务实体 ─► 需要时 spawn 角色
Seelex 现状：           #goal(入口) ─► SKILL 激活（提示词）    ✗ 无 goal 实体
                                                              ✗ 无 spawn 钩子
DS-A2A 目标：           #goal(入口) ─► goal_begin(工具) ─► 栈 + spawn ADVISOR(b)
```

### 建议落点（与 DS-A2A 详设 §8 一致）

1. **让 goal 有可创建的运行时实体**：把 `goal_begin` 挂进生产工具面（`isGoalTool` 方向），
   `#goal` 的 SKILL 提示负责引导模型调用它；b 的 spawn 绑定 `goal.start` 锚点，而不是绑定
   `#goal` 文本本身。这正对应 A2A"client 发起 task"和 Codex"goal 入口→状态对象"的共性。
2. **评审触发面独立于激活面**：ADVISOR 回合由事件（step_checkpoint / context_compacted /
   budget_warning / approval_asked / terminal_proposal）与 gate（ProposeFinish / PreScreen）
   驱动（已实现于 `application/core/goal`），与"评审角色 description 声明 + 事件匹配"的模式 B 对齐；
   无需让用户为每次评审另起炉灶。
3. **保持 B4（a 永不等待 b）作为与市场产品的差异优势**：Codex 的 subagent workflow 语义是主线程
   收集齐各 agent 结果后才给 consolidated response；Claude Code 的 agent teams 默认关闭、官方文档
   承认 coordination 与停机有已知限制。Seelex 的缺席矩阵（429/超时→判负/人工，不阻塞 a）反而更稳。
4. **评审角色工具面先无工具**：与 Q4 建议及 Magentic-One/Claude reviewer 的只读/受限面一致。

---

## 5. 参考来源

| 来源 | 链接 | 用途 | 抓取状态 |
|---|---|---|---|
| A2A Protocol Key Concepts（Google） | https://a2a-protocol.org/latest/topics/key-concepts/ | Agent Card / Task / Message 语义 | 已抓原文 |
| Codex Subagents（OpenAI 官方） | https://www.codex-docs.com/en/docs/agent-configuration/subagents | 显式/指令触发、description、模型选择 | 已抓原文 |
| Claude Code Sub-agents（Anthropic 官方） | https://code.claude.com/docs/en/sub-agents | description 匹配、自定义 subagent | 已抓原文 |
| Claude Code Agent Teams（Anthropic 官方） | https://code.claude.com/docs/en/agent-teams | lead spawn teammate、task list、默认关闭 | 已抓原文 |
| MetaGPT Role Architecture（DeepWiki） | https://deepwiki.com/FoundationAgents/MetaGPT/2.1-role-architecture | watch/cause_by 触发、observe-think-act | 已抓原文 |
| Magentic-One（arXiv 2411.04468） | https://arxiv.org/abs/2411.04468 | Orchestrator plan/track/replan + 分派 | 已抓摘要 |

仓库对照基线：

- [ds-a2a-protocol.md](../2026-09-07-seele-a2a-framework-req/ds-a2a-protocol.md)（§时序/生命周期：goal_begin→spawn b→unbind+reap）
- [ds-a2a-detailed-design.md](../2026-09-07-seele-a2a-framework-req/ds-a2a-detailed-design.md)（§8 主线接线：goal_begin→spawn b）
- [techleader-mvp.md](../2026-09-07-goal-domain-techleader/techleader-mvp.md)（已实现切片与 P0-wiring 缺口）

## 6. 未能核实项（不当作结论）

- MetaGPT 的 TeamLeader/Mike/MGXEnv 中央协调细节（DeepWiki 对应页面抓取超时）。
- Magentic-One 的双 ledger 与五问内循环（论文全文 HTML 抓取超时，仅采用摘要内容）。
- OpenHands microagents 的 always/keyword/manual 触发（GitHub 文档两次抓取超时）。
