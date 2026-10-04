# 2026-10-05 teamwork 与外部机制对照：Agent team 生态 × 长程任务工具

> goal：`g-4`｜team_id：`teamwork-vs-landscape`｜里程碑：`m-survey`（两路并行）→ `m-contrast` → `m-audit`
> 4 个 teammate 全部 `readonly`。
>
> **证据边界（先说，否则后面全是空话）**
> - 本仓**没有**任何外部项目的源码或 vendor；本环境 `web_search` **不可用**（403 额度）。所以外部侧条目
>   **全部**来自本仓既有调研文档（**二手**，逐条标日期），厂商自报一律原样标注「未证实」。
> - **模型先验单列**，不得进入结论。
> - 本文所有 `file:line` 都经过对抗复核**逐条去读代码/读文档核对**；复核纠正过的 4 处已按纠正后的行号引用（见 §4）。
> - 沿用上一份文档（`2026-10-05-teamwork-vs-loopx.md`）的纪律：**「文档未覆盖」≠「对方弱」**，一律写「不可比」。

---

## 1. 先定位：这三样不是同一层东西

| | 是什么 | 面向 | 谁在驱动 |
|---|---|---|---|
| **teamwork**（本仓） | 同进程内的**多代理编排工具面** | 一次交付里的多件事 | leader 会话同步调用 |
| **外部 Agent team 生态** | 编排**库 / 产品特性 / 协议**（LangGraph、AutoGen、CrewAI、MetaGPT、Codex multi-agent、Claude subagent、A2A…） | 通用编排能力 | 应用代码 / 父模型 |
| **长程任务工具** | **长时控制面**（LoopX、Codex `/goal`、Claude `/goal`、Magentic-One、云端异步 agent） | 跑数天到数百小时的无人值守任务 | 常驻循环 / 定时器 |

**所以只比「同一问题上的机制」，不比 API 与部署形状。**

---

## 2. 外部 Agent team / 多代理编排：7 个代表对象

来源：`docs/research/agent-harness-research-report.md`(08-02)、`coding-agent-harness-comparison.md`(08-02)、
`2026-09-12-external-harness-comparison.md`、`2026-09-17-fork-subagent-ownership-and-multiagent-orchestration.md`、
`agent-market-research-2026-08.md`、`docs/2026-08-06-agent-landscape-research/`(08-06，厂商自报)、
`docs/devlog/2026-10-01-claude-teamwork-run-mode-research.md`(10-01，Playwright 实测)、`docs/arch/a2a-agent-team-*.md`。

| 对象 | 编排形态（谁派谁 / 顺序来源） | 隔离单元 | 验证与证据 | 没做 / 做不了 |
|---|---|---|---|---|
| Claude Code subagent / agent teams | 主模型按 `description` 委派；**顺序＝调用姿势** | 独立 context window + 独立 tools/权限；`isolation: worktree` 可选 | 只回 summary；**无内建评审角色** | 顺序不进计划；嵌套派生无明文（未核实） |
| Codex multi-agent v2 / `/goal` | 父会话 spawn/send/wait/interrupt；**children may spawn children** | `fork_turns none\|n\|all` 控制继承 | 无独立评审（文档未覆盖）；产出进「评审队列」交人 | 无计划级顺序；checkpoint 缺位 |
| **ClaudeTeamwork 社区插件** | lead → Orchestrator → 角色；独立里程碑 fan-out；顺序＝分解 + **共享 TaskList** + `next_action` | 每里程碑一 worktree，**开局即在其中** | Reviewer/Critic/Auditor；**`Stop` hook 阻断至 `audit-PASS`**（插件自述） | **没有「切到某 teammate 会话」的能力** |
| Anthropic 多代理 research | lead 写计划进 Memory → 派生并行 subagents | 子代理独立上下文；官方明说共享上下文/强依赖不适合 | CitationAgent 定位引用；无独立评审 | 官方自述多数编码任务不适合；≈15× token |
| 框架系 LangGraph/AutoGen/CrewAI/MetaGPT/Magentic-One | 图 / 管理者 LLM / 固定 SOP 决定顺序；CrewAI **任务不预分配**、`allow_delegation` 默认 False | LangGraph 子代理每次全新；**AutoGen Swarm 共享消息上下文**；其余未覆盖 | CrewAI manager 评审；MetaGPT 校验中间产物；其余未覆盖 | LangGraph 官方建议改用工具式 supervisor |
| 云端异步 agent（Devin / Cursor Cloud / OpenHands / Codex Workspace / Claude Routines） | 协调者分解 → 多 worker 各自独立 VM；cron/webhook 触发 | 独立 VM / 每次运行独立沙箱 | Devin Review（厂商自报 70–90%，**未证实**）；评审队列 / 截图 | 质量仍需人验；内部调度未覆盖 |
| Google A2A 协议 | **不做编排**：client 送 Task，不规定自动拉起 | Task 状态机 + context id | 仅 task 生命周期 | 无顺序 / 无屏障 / 无角色生命周期 |
| 市场式派活（Contract Net / 拍卖） | **文档未覆盖**（仅书目：Smith 1980；Gerkey & Matarić 2004） | — | — | 机制层**拒绝补写** |

### 2.1 这一类的核心发现：外部只有三种「顺序来源」

1. **调用姿势**（Claude Code subagent、Codex multi-agent）——顺序只活在父模型的连续决策里，没有可校验的载体。
2. **计算 / 分配**（CrewAI hierarchical、Magentic-One、Anthropic research、云端 agent）——顺序由 manager LLM 或 orchestrator **运行时算出**。
3. **声明式结构**（LangGraph 图、MetaGPT SOP、ClaudeTeamwork 共享 TaskList）——顺序有可读载体。

**只有第 3 种里「结构可被机器校验」的那一支，与 teamwork 同族；而在本仓可核的全部出处里，
没有任何外部对象把「里程碑屏障」与「里程碑内 DAG」两种依赖同时写进计划头。**

---

## 3. 长程任务工具：4 家 × 7 问

来源：`docs/2026-08-07-loopx-context-research/research-loopx-context-management.md`(08-07)、
`docs/research/codex-claude-goal-harness-2026-09.md`、`goal-context-governance-2026-09.md`、
`2026-09-11-seelex-vs-codex-context-strategy-control-group.md`、`2026-09-30-goal-a2a-flow-and-techleader.md`。

**共同形状**：控制状态**外置成会话之外的追加式一等对象**（LoopX 的 `events.jsonl`、Codex 的 rollout JSONL + state db、
OpenHands EventStream、Magentic-One 的 progress ledger），聊天/转录只作执行上下文，**停机权从模型收回到 harness**。

| 问题 | LoopX | Codex `/goal` | Claude `/goal` + Agent View | Magentic-One |
|---|---|---|---|---|
| 下一步该干嘛 | **frontier 9 规则归约 + 8 类漂移触发器，完成＝机器判定** | 文档未覆盖（自判完成续轮） | 文档未覆盖 | 依赖 ledger，摘要级 |
| 何时停 / 何时静默 | **安静是一等公民**：`monitor_quiet_skip` / DONT_NOTIFY / 不记账；3 次 unchanged 才允许停 | 完成自评 + 预算护栏 | 完成自评停机；停机的已知限制官方承认 | ledger 判 done/terminate |
| 跨轮上下文 | 分级投影**决策载荷**，模型不读事件流 | token 预算换新窗 + pre/post-compact hooks | auto-compact（≈967K）+ 分层 CLAUDE.md/MEMORY.md | 文档未覆盖 |
| 有界性 | TurnEnvelope ≤8KB；**quota 只对「已验证的 accountable delivery run」记账** | goal 携带预算；compaction token 基线 | Task Budgets / effort 档位 | 文档未覆盖 |
| 人机接口 | **gate 是一等对象**（问题 + 阻塞路线 + safe default + 可推进旁路） | 权限面（SandboxPolicy × AskForApproval） | 权限模式 + allow/ask/deny | 文档未覆盖 |
| 失败与恢复 | 失败绑相位 + `turn_key` 幂等定点重试 | resume/fork + 截断标记 | `/rewind`、checkpoint 回滚 | "replan to recover" |

**分档说死**：**只有 LoopX 把「下一步 / 何时停 / 何时静默」三问都做成机器可计算归约**。
Codex / Claude 只到「完成自评 + 预算护栏」。最集中的空白是**人机接口（gate 一等对象）与有界性记账**。

---

## 4. 三方对照

### 4.1 可比 / 不可比

- **不可比**：API 与部署形状；**工作区隔离**（外部只在 ClaudeTeamwork 与云端 agent 有明确记载，其余「文档未覆盖」）；
  **独立评审角色**（同上）；**多代理广度**（外部有 `children may spawn children` 等无穷嵌套，teamwork 结构性封顶，
  两者不在同一坐标系）。
- 下面只在**同一问题上比机制**。

### 4.2 对照表

| 维度 | teamwork | 外部 Agent team | 长程工具 | 判决 / 代价 |
|---|---|---|---|---|
| 顺序表达 | 计划头**双写**屏障（`sessionstore/teamwork.go:113`）+ 里程碑内 DAG（`teamwork_items.go:48`） | 仅单轴：调用姿势 / manager LLM 分配 / 单层声明图 | typed todo + 前沿归约 | **只有 teamwork 是两层且可拒**；代价＝计划必须先写全 |
| 拒绝式闸门 | 派发边界同步拒（`items.go:297/318/326/331`）；超员拒（`coordinator.go:519-522`）；收口闸（`coordinator.go:377/426`） | 多为人工门；**仅 ClaudeTeamwork 的 `Stop` hook**（阻断至 `audit-PASS`，插件自述）最接近 | LoopX 的 gate 一等对象，但顺序对 host 是**建议**、事后校验 | teamwork 占优；**缺口＝收口后无 `closed` 闸** |
| 隔离与广度 | 一事一会话 + 一事一 worktree（`items.go:57,62`），**且可降级**（`runtime_teamwork_items.go:45-63`）；广度硬绑 6 人（`config/seelex.yaml:167`） | worktree 仅 ClaudeTeamwork；VM 仅云端；Anthropic 可 >10 subagents | 文档未覆盖 → **不可比** | 外部广度更大，代价＝token 与失控（Anthropic 自述 ≈15× token） |
| 验证与证据 | `AcceptItem` 链尾闸（`items.go:534/547`）+ 尾插 review/failed；**无机器 validator** | 人工门为主 | LoopX argv-only validator（不用 LLM 当裁判） | 长程占优 |
| 无人值守 / 恢复 | leader 不叫就没有下一步；`Recover` 靠**内存** `handleAlive`（实测高危） | 心跳 + SessionStart rehydrate | heartbeat + quiet；`turn_key` 幂等 | 长程**结构性占优** |
| 成本 / 可移植 / 审计 | **护 leader 的上下文**（实测 4.1× / 0.96×，出处 `2026-10-05-teamwork-vs-loopx.md:30-31`）；绑 seelex；**拒收不落审计** | 子代理多一次回传；库可嵌 | **护用户的注意力**；零依赖；不可篡改 | 各有所护，不是同一个「省」 |

### 4.3 三条结论

**① teamwork 相对这两类各自的优势**

- **对外部 Agent team**：它是本仓可核范围内**唯一**把「里程碑屏障 + 里程碑内 DAG」两层依赖同时写进计划头、
  并在**派发调用边界硬拒**（屏障 / 依赖 / 不在编 / 超员）的实现。外部的顺序要么活在父模型的决策里、
  要么由 manager LLM 运行时算出、要么只有单层声明图；**「能不能现在派」在外部不是可拒的结构事实**。
- **对长程工具**：它把顺序做成**可拒的结构事实**，而 LoopX 的顺序对 host 是**建议**（事后校验）。
  一个在「拦」，一个在「算」——这是两种不同的东西，不是强弱。

**② teamwork 相对这两类各自的短板**

- **对外部**：广度被 `max_teammates=6` 结构性封顶，且 teammate 面**硬移除** `fork_subagents`（没有孙级 worktree），
  要并行只能线性加人；隔离是「有条件的」（建不出来就降级回主工作区，看板与账本看不出来）。
- **对长程**：没有无人值守（不叫就没有下一步）；「下一步该干嘛」的归约在本仓搜索为空；没有机器 validator；
  恢复靠内存 `handleAlive`（已实测把「已 done」误报成「在跑」）。

**③ 最短的那块板——两票，我并列采纳**

| 提案 | 内容 | 落点 |
|---|---|---|
| 对照者 | **计划前沿的单次只读归约**，派发闸门只走它 | `seelebridge/teamwork/`，纯读、不动 sessionstore 写接口 |
| 复核者（更锋利） | **核心卖点「结构性拒绝」在生产不可观测**：拒绝不落审计，且唯一被实证触发过的闸门只有 `AcceptItem`，其余本轮「未触发即未实证」 | 「在跑 / 拦过」＝**持久事实** + 拒绝入账 |

两条其实是同一件事的两半：**前沿归约负责「可计算」，落盘裁决负责「可观测」**。
没有前者，"能不能派"没有答案；没有后者，"拦过没有"永远只能反推。

---

## 5. 对抗复核：纠正了什么

**数字口径（3 条）**

1. Anthropic 的 `>10 subagents` 与 `90.2%` 命中原文（`2026-09-17-…md:85-86`），但 **90.2% 是相对单 Opus 4 的「提升」，不是成功率**——本仓另三处引用也都写「提升」，无挪用。
2. `handoff 省 40–50%` 命中但**口径窄**：原文是 "save 40-50% of **calls on repeat requests**"（LangChain 官方文档＝厂商自报），单位是**调用数**且限「重复请求」，**不是成本或 token**。
3. 本仓团队的 **4.1× / 0.96× 出自 `2026-10-05-teamwork-vs-loopx.md:30-31`**，不在 `teamwork-mechanism-evaluation.md`（此前引用有误，本文已更正）。

**file:line 修正（4 处）**：依赖判定在 `coordinator.go:234-238`（＋`milestoneDone:531`），不是 `:229-232`；
`Interrupted` 在 `items.go:42-49`，不是 `:33-38`；超员拒绝文案在 `coordinator.go:519-522`，不是 `:507`；
README 漂移的真址是 **`seelebridge/teamwork/README.md:60`**，不是仓库根 README。
（另确认：`docs/arch/team-board-gui-tui-contract.md:100` 仍以**已整条退场**的 `Stages[]` 为顺序事实。）

**不同意（已按此修正）**：上一份文档在结论处把「**文档未覆盖**」的三条（工作区隔离 / 并行上限 / 独立评审）
改写成 teamwork 的「**独有优势**」，与同文 §3.1 的「不可比」自相矛盾。本文一律按「不可比」处理；
凡涉及「外部结构上做不到」的表述，一律标 **Hypothesis**（结构论证，非已证事实）。

**无法判断（留白，不凑）**：LoopX 等外部机制的**真实现状**（仓内无源码）、`jobs.Reclaim` 的 5s 边界、
各家自报数字（90.2% / 15× / 70–90%）。

---

## 6. 一句话交付

**teamwork 的生态位是「在调用边界做拒绝式裁决的多代理编排」**——
相对外部 Agent team 生态，它的优势不在「能起多少个 agent」，而在**顺序是写进计划、可被机器校验、并在派发那一刻能拒**；
相对长程任务工具，它的优势是**顺序是硬闸而不是建议**，短板则是**它没有「没人问也能自己往下走」的那套东西**。
外部两类共同做到了而 teamwork 没有的，正好是 LoopX 那一套：**停机/静默的机器判据 + 让每个 agent 都看得见的持久状态前沿**。

## 7. 证据索引

| 段 | 来源 | 落点 |
|---|---|---|
| Agent team 画像（7 对象 × 六问） | team_surveyor · `wi-agentteams` | 见 §2 各条出处文档 |
| 长程工具画像（4 家 × 七问） | horizon_surveyor · `wi-horizon` | 见 §3 出处 |
| 三方对照表与三条结论 | comparator · `wi-triad` | 本仓当前 HEAD 代码 + 上列文档 |
| 对抗复核与纠正 | reviewer · `wi-audit-triad` | `items.go:42-49/297/534/547`、`coordinator.go:234-238/377/426/519-522`、`seelebridge/teamwork/README.md:60` |
| teamwork 整体评价与实测 | 上一轮 | `docs/devlog/2026-10-05-teamwork-vs-loopx.md`、`2026-10-05-teamwork-mechanism-evaluation.md` |
