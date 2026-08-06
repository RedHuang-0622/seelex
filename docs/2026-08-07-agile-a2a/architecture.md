# Seelex 敏捷开发 A2A 架构设计（Tech Leader × Programmer）

> 状态：Draft（设计提案，非实现事实；代码与测试仍是"已实现"能力的唯一事实来源）
> 日期：2026-08-07
> 承接：W4 试点方案 [`../2026-08-06-agent-landscape-research/plan-iterative-product-pilot.md`](../2026-08-06-agent-landscape-research/plan-iterative-product-pilot.md)（M0–M4、DAG 模板、exit code 适应度、独立评审、人工生产门）与竞品调研 [`../2026-08-06-agent-landscape-research/research-agent-landscape-2026-08.md`](../2026-08-06-agent-landscape-research/research-agent-landscape-2026-08.md)。
> 配套：消息 schema、决策规则、执行契约、MVP 步骤见同目录 `design.md`。

---

## 0. 一句话

在 seelex 现有的 **Plan DAG + 子代理 + merge-back + 打点 + 审批**骨架上，把「敏捷开发」固化为两个角色之间的 **A2A 协议**：**Tech Leader（TL）**由父会话/主代理承载——拆需求、写死验收标准、分任务、把关门禁、失败时重规划；**Programmer** 由子代理节点（`kind:agent` / `fork_subagents`）承载——领任务、实现、写测试、自检、本地提交。**机器验证当裁判、独立评审把关、人工门收口**，全程在 GUI 可观测、状态落盘可恢复。

---

## 1. 目标与边界

### 1.1 目标

1. **双角色分工明确**：TL 只做决策（切分、分配、验收、重规划），Programmer 只做实现（编码、测试、自检、提交）。二者知识面刻意不同（见 §2.3），这是评审独立性与 context rot 防护的结构性基础。
2. **契约先于实现**：任务下发、结果回传、阻塞上报、评审、重规划全部走结构化 A2A 消息（`design.md` §2），落到 seelex 现有工具与打点机制。
3. **验证闭环可信**：验收标准以「真实命令 + exit code」判定，不用 LLM 当裁判；失败回环有界；交付物必须携带测试证据。
4. **零破坏演进**：MVP 先串行手写驱动验证语义（M1），再固化为 Plan DAG 双角色形态（M2+），缺口逐个补齐（§5）。

### 1.2 边界（Out）

- 不做自动 push / 自动 merge / 自动发布（人工生产门保留，对齐试点 §4.1）。
- 不做 TL 与 Programmer 的**通用对话**（A2A 只承载工作流消息，不引入新聊天通道）。
- 不引入 OS 级沙箱（沿用 PathGate + 独立 workspace / worktree）。
- 不做跨会话记忆（调研 N6/N8 为长期项，M4 后承接）。
- 不做跨仓库 / 外部 ticket 系统接入。

---

## 2. 角色模型

### 2.1 职责边界

| 维度 | Tech Leader（TL） | Programmer |
|---|---|---|
| 承载 | 父会话 / 主代理（含 tl skill） | 子代理节点（`kind:agent`，独立 Session + 独立账号） |
| 需求 | 消化需求原文，产出 `REQUIREMENTS.md`（编号、可测试验收标准 REQ-01…N） | 不接触需求全文，只看任务卡中本任务覆盖的 REQ |
| 拆解 | 把设计切成任务卡 T-01…T-N（粒度规则见 `design.md` §3.1） | 不参与切分，只执行单张任务卡 |
| 分配 | 写任务卡 + `fork_subagents` / `plan_load` 下发（TaskAssignment） | 领取任务，读任务卡 |
| 计划 | 产出/维护 `DESIGN.md`（模块划分、文件归属、测试计划） | 产出任务内实现计划（简短，随 WorkResult 回传） |
| 实现 | 不写业务代码 | 编码 + 补测试（覆盖 REQ 通过条件） |
| 验证 | 不亲自判定成败；调 verify 节点跑真实命令读 exit code | 自检：跑 Baseline + 本任务测试，逐条对照 Acceptance |
| 门禁 | 维护门禁序（验证门→评审门→生产门）；汇总验收 | 不得跳过任何门；自评不算评审 |
| 失败处理 | 失败分类 → 修复回环路由（有界）→ 超限重规划/升级人工 | verify 失败先重跑 1 次分类；真 bug 修复并报根因 |
| 评审 | 构造 ReviewRequest；采纳 ReviewVerdict（blocking 必修） | 不得自评；被评审 |
| 提交 | 交付物汇总（变更摘要/测试证据/PR 描述） | 本地 `git commit`（不 push、不 merge——框架的事） |
| 决策终点 | 可升级人工（approve_req / approve_deliver / needs_user_decision） | 升级终点是 TL（BlockerReport），TL 决策不了才到用户 |

### 2.2 角色知识面（各看什么上下文）

知识面划分原则：**状态放文件不放窗口**（试点 R1 护栏）——权威事实全部落盘，上下文只放「指针 + 有界摘要」；两个角色的上下文刻意不对称。

**TL 知识面（父会话上下文，大而"浅"）**：

| 内容 | 形态 |
|---|---|
| 需求原文 | 用户输入（会话内） |
| `REQUIREMENTS.md` | 落盘，编号可测试验收标准（权威事实，人工锁定） |
| `DESIGN.md` | 落盘：模块划分、文件归属、任务切分依据 |
| 任务卡集合与状态 | `.seelex/a2a/tasks/*.json`（指针 + 摘要 + 状态） |
| 汇总结果 | fork `summary` 节点输出 + merge-back findings |
| ReviewVerdict | 评审结论（approved / blocking_issues / suggestions） |
| 人工门反馈 | approve 拒绝原因（驱动修订） |
| 失败统计 | 哪类 REQ/任务失败率高（切分与验收标准调优输入） |

TL **不看**：各任务源码细节、programmer 的工具轨迹、其他任务的实现过程。

**Programmer 知识面（子代理上下文，小而"全"——任务内全量）**：

| 内容 | 注入机制 |
|---|---|
| 任务卡（Goal / Acceptance / Scope / OutOfScope / Baseline / Budget） | TaskAssignment（`design.md` §2） |
| 父证据（需求/设计相关摘录） | `nodeParentEvidence` → `parent-evidence` PromptBlock（`seelebridge/agent_node.go`） |
| Scope 内源文件与基线测试 | 工作区 + PathGate 约束 |
| verify 失败输出（fix 轮：失败用例列表） | verify 节点结构化回传（缺口 V1，见 §5） |
| 激活 skill（programmer skill / 领域 skill） | `node-skill-catalog` / `node-skill-active` 块 |

Programmer **不看**：其他任务卡细节（无依赖时）、需求全文（只给本任务 REQ）、人工门对话、TL 的全局统计。

**评审角色知识面（独立第三人）**：ReviewRequest 内容（diff 范围 + 测试文件 + 验收标准 + Baseline）——独立 Session/账号，既不继承 TL 也不继承 programmer 的完整轨迹，这是"写者不给自己打分"的机制保证（`research-agent-landscape-2026-08.md` §3.4 共识）。

### 2.3 双角色与现有机制的对应

| 角色 | seelex 机制 | 说明 |
|---|---|---|
| TL | 父会话 + `tl` skill 模板 | skill 模板约束 TL 的决策纪律（切分规则、门禁序），试点资产，M0 产出 |
| Programmer | `fork_subagents` 子代理 / Plan `kind:agent` 节点 | `SeelexAgentNode.Run`：NodeScope + worktree + 预算 + 收尾契约全套现成 |
| 独立评审 | 子代理（独立 Session + 独立账号） | 新定义 review 节点契约（缺口 R1） |
| 人工 | `kind:approve` 节点 + ApprovalBroker + `ask_approve` | 现成，异步审批对话框 |

---

## 3. 交互协议（A2A 消息与工具契约）

### 3.1 消息一览（schema 见 `design.md` §2）

| 消息 | 方向 | 语义 | 落到 seelex 的通道 |
|---|---|---|---|
| TaskAssignment | TL → Programmer | 任务下发：任务卡（REQ 覆盖、Scope、验收、预算） | 落盘任务卡 + `fork_subagents {id, goal}` 或 `plan_load` 节点 input（goal 摘要） |
| WorkResult | Programmer → TL | 结果回传：改动清单 + 测试记录 + 自检逐条对照 + findings | 子代理最终回复（结构化 findings）→ merge-back（`merger.MergeBack` 上卷 TL 上下文）+ `task_check_node` 打点（ChangedFiles/Evidence） |
| BlockerReport | Programmer → TL | 阻塞上报：缺规格 / 依赖 / 环境 / 权限 / 预算耗尽 | 子代理内输出 blocker JSON（skill 契约）→ TL 决策；TL 决策不了 → `task_needs_user_decision`（DecisionQuestion/DecisionOptions） |
| ReviewRequest | TL → 评审 | 评审请求：diff 范围 + 测试文件 + 验收标准 | review 节点 input（新契约，缺口 R1） |
| ReviewVerdict | 评审 → TL | 评审结论：approved / blocking_issues / suggestions + 测试质量判定 | review 节点输出 JSON（新契约，缺口 R1） |
| ReplanRequest | TL（内部） | 重规划：rescope / retry / abandon / escalate | TL 在父会话决策；`taskTerminal.ReplanRecommended` 预留信号；超限升级人工 |

### 3.2 主交互时序

```
TL(父会话)             Programmer(子代理)        verify(机器)         Review(独立)        人工
  │ 1. 写任务卡 T-01       │                       │                  │                  │
  │ 2. fork/plan 下发 ───→ │                       │                  │                  │
  │                        │ 3. 读任务卡+实现+测试 │                  │                  │
  │                        │ 4. 自检(跑Baseline)  │                  │                  │
  │                        │ 5. WorkResult+commit─→│                  │                  │
  │ 6. 收 summary+merge-back│                      │                  │                  │
  │ 7. verify(真实命令) ─────────────────────────→ exit code          │                  │
  │ 8a. 失败→fix回环(≤5) ───────────────────────→ (重跑确认)          │                  │
  │ 8b. 通过→ReviewRequest───────────────────────────────────────────→│                  │
  │ 9. blocking→fix；approved→deliver           │                      │                  │
  │ 10. approve_deliver──────────────────────────────────────────────────────────────→   │
```

### 3.3 失败与重规划的交互

- **任务级失败**（verify 失败）：先重跑 1 次确认（flaky 分流），真 bug → fix 回环（有界 ≤5 轮或预算内），修复后回到 verify。回环实现三选一与推荐见 `design.md` §3.3。
- **任务级阻塞**（缺规格/依赖/环境）：Programmer 发 BlockerReport → TL 决策（rescope / 补规格 / 调整依赖顺序 / 升级人工）。
- **战略级重规划**（需求变更 / 方案分歧 / 多任务连锁失败）：TL 触发 replan（隔离规划会话），产出新任务卡集或调整 DESIGN.md；`taskTerminal` 的 `ReplanRecommended` / `PartialProgress` 字段为此提供终态承载。
- **越权护栏**：Programmer 不直接向用户升级（先 TL）；TL 也不自动交付（人工门）。

---

## 4. 状态机与 DAG 模板

### 4.1 任务生命周期状态机

```
assigned ──→ coding ──→ testing ──→ verifying ──┬─ 通过 ──→ review_pending ──→ reviewing
                                                 │                                     │
         ┌───────── fixing ←──── verify 失败(重跑确认非 flaky)                          │
         │     ↑                                      ┌───────────────────────────────┘
         └─────┘  (有界回环 ≤5 轮)                    │ verdict = approved
                                                      ▼
                                             merged ──→ delivered ──→ approved(人工 approve_deliver) ──→ completed
                                                      │
                             verdict = blocking_issues ─┴──→ fixing（回到测试链）
                    预算耗尽 / 无法修复 / 阻塞升级：failed ──→ replanning ──→ assigned(新任务卡) 或 needs_user_decision(人工)
```

状态转移事件映射：

| 状态 | 进入条件 | 执行者 | 触发工具/动作 | 打点 |
|---|---|---|---|---|
| assigned | 任务卡落盘 + 下发 | TL | `fork_subagents` / `plan_run` | `task_check_node`(T-01, assigned) |
| coding | Programmer 开始实现 | Programmer | 读任务卡 → 实现 | 子代理会话详情（GUI 可观测） |
| testing | 实现完成开始补测试 | Programmer | 写测试（真实断言） | 同上 |
| verifying | 测试完成提交自检 | Programmer + verify | 自检命令 + verify 节点 | `task_check_node`(verifying, evidence=exit code) |
| fixing | verify 失败且重跑确认非 flaky | Programmer | 修复 → 重测 → 回到 verifying | `task_check_node`(fixing, failure=失败用例) |
| review_pending | verify 通过 | TL | 构造 ReviewRequest | `task_check_node`(review_pending) |
| reviewing | 评审节点启动 | 独立评审 | 独立 Session/账号执行 | 评审节点会话 |
| merged | verdict=approved | 框架 | worktree 变基 + 合并（`finishNodeWorktree`） | 框架事件 |
| delivered | 交付物产出 | TL / deliver 节点 | 交付物文件（摘要/测试证据/PR 描述） | `task_check_node`(delivered) |
| approved | approve_deliver = approve | 人工 | `task_complete` | 终态 |
| failed | 预算耗尽 / 无法修复 | 自动 | `task_failed`（FailedNode/PartialProgress） | 终态 |
| replanning | TL 决策重规划 | TL | 新任务卡 / replan | `ReplanRecommended` |
| blocked | BlockerReport 升级到用户 | TL → 人工 | `task_needs_user_decision` | 终态 |

### 4.2 DAG 模板（M2 目标形态，双角色标注）

在试点 §5.2 模板基础上标注角色与回环：

```
entry: req
req            [TL]          分析需求 → REQUIREMENTS.md（REQ-01..N，通过条件=命令+exit code）
approve_req    [人工]        门0：锁定验收标准（可修改后重确认；确认后 agent 不得单方改）
design         [TL]          DESIGN.md：模块/文件划分 → 任务切分（任务卡 T-01..T-N 落盘）
split          [auto]        按任务卡生成并行分支（每任务一个 programmer 节点；组内无依赖才并行）
  ├─ T-01      [Programmer]  code → test（覆盖 REQ 通过条件）→ 自检 → 本地 commit
  ├─ T-02      [Programmer]  code → test → 自检 → 本地 commit
  └─ ...
verify_01      [verify]      真实命令：go test ./... && go vet；exit code 即结果（失败输出结构化回传）
  └─ 失败(重跑确认) → fix_01 [Programmer] → 回 verify_01   （有界回环 ≤5 轮，缺口 L1）
merge          [auto]        worktree 变基 + 合并（框架 finishNodeWorktree，合并审批现成）
review         [独立评审]    独立 Session/账号：diff + 测试质量 → verdict（缺口 R1）
  └─ blocking_issues → 回 verify/fix
deliver        [deliver]     交付物：变更摘要 / 测试结果证据 / PR 描述；本地 commit 不 push（缺口 D1）
approve_deliver [人工]       门3：生产门 → task_complete
```

### 4.3 两种执行形态（TL 侧选择规则）

| 形态 | 机制 | 适用 | 理由 |
|---|---|---|---|
| A 轻量委派 | TL 调 `fork_subagents`（程序化 all-parallel DAG） | 任务组内无依赖 | 模型无需 DAG 知识，弱模型可用（`fork_tool.go` 设计初衷） |
| B 完整 DAG | TL 调 `plan_load`（声明式 DAG + 门禁节点） | 依赖链长 / 需要精确门禁定位 / 回环 | 节点级预算、审批、打点全量可用 |

选择规则：任务组内无相互依赖且并行安全 → A；存在依赖链或需要 verify/review 门禁精确落位 → B。两种形态共用同一执行内核（fork 是 DAG 的编程生成特例）。

---

## 5. 与现有架构的映射表（A2A 概念 × seelex 组件）

> 状态口径：**现成** = 代码已实现；**半现成** = 机制在，但 A2A 契约语义缺一层薄壳；**缺口** = 需要新增或强化。

| A2A 概念 | 期望语义 | seelex 现有组件（证据） | 状态 | 缺口处置 |
|---|---|---|---|---|
| Tech Leader 角色 | 需求拆解/分配/门禁/重规划 | 父会话 + skill 目录机制（`nodeSkillBlocks`） | 半现成 | tl skill 模板（试点资产，零产品改动） |
| Programmer 角色 | 实现/测试/自检/提交 | `fork_subagents`（`fork_tool.go`）、Plan `kind:agent` 节点（`agent_node.go`） | 现成 | — |
| 独立评审角色 | 写者不给自己打分 | 子代理机制（独立 Session + 独立账号路由） | 半现成 | review 节点契约（缺口 R1） |
| TaskAssignment 下发 | 结构化任务卡 + 指针 | fork 的 `{id, goal}` / 节点 input（自由文本） | 半现成 | 任务卡文件方案（缺口 A1，零 DSL 改动） |
| WorkResult 回传 | 结构化结果 + 测试证据 | merge-back（`merger.MergeBack`）+ 最终回复 findings + `NodeCheckpoint`（Evidence/ChangedFiles） | 半现成 | skill 契约约束输出（缺口 A2，轻） |
| BlockerReport | Programmer→TL 分级升级 | 子代理失败终态 + `task_needs_user_decision`（DecisionQuestion/Options） | 半现成 | skill 契约 + TL 分流（缺口 A3，轻） |
| 打点与状态 | 生命周期可观测 | `task_check_node` / `NodeCheckpoint` / `taskTerminal`（`task_execution.go`） | 现成 | — |
| 适应度 | exit code 优先，不用 LLM 当裁判 | bash 工具 exit code | 现成 | verify 强化后闭环（缺口 V1） |
| 验证门 verify | 真实命令绑定 + 失败输出回传 + 重跑确认 | verify 节点当前为确定性节点（`KindMethod`，`plan_factory.go`） | **缺口 V1（S）** | 命令模板绑定 + 结构化失败回传 + 重跑 1 次（试点已列） |
| 失败回环 | 有界自动修复循环 | 无（DAG 静态拓扑） | **缺口 L1（M）** | DAG 失败边（推荐，见 `design.md` §3.3）/ replan |
| 评审门 review | 输入 diff+测试 → 输出 verdict | 子代理机制现成 | **缺口 R1（S）** | review 输入输出契约（试点已列） |
| 交付物 | 变更摘要/测试证据/PR 描述 | deliver 节点存在（`KindMethod`） | **缺口 D1（S）** | deliver 输出契约 + 本地 commit 封装（试点已列） |
| 人工生产门 | 不自动发布 | `kind:approve` 节点 + ApprovalBroker + `ask_approve`（`application/approval/broker.go`） | 现成 | — |
| 并行度控制 | 受控并行 | `SetMaxForkConcurrency` / `PlanPolicy.concurrency` / worktree 隔离 + 变基兜底 | 现成 | — |
| 角色上下文裁剪 | 知识面不对称 | NodeScope + `nodePromptBlocks`（goal/parent-evidence/budget/skill/finish）+ worktree | 现成 | — |
| 任务生命周期 | assigned→…→approved | `taskExecutionState`（running/completed/blocked/interrupted/needs_user_decision/failed） | 半现成 | A2A 状态 ↔ 机器状态映射表落地（缺口 A4，轻） |
| 可观测 | 双角色树形视图 | Plan 面板 + 打点 + tasklist | 半现成 | SubAgentTree 树形视图（缺口 O1，M，P0 项未上线） |
| 角色知识库 | TL 跨会话教训 | 无跨会话记忆（ProjectKnowledge 为静态知识） | **缺口 M1（L）** | 调研 N6/N8，M4 后承接 |

### 缺口清单（汇总）

| # | 缺口 | 层级 | 工作量 | 备注 |
|---|---|---|---|---|
| V1 | verify 节点绑定真实命令 + 失败输出结构化回传 + 重跑确认 | 产品 | S | 试点 §5.3 已列；D2 方向 |
| R1 | review 节点输入输出契约 + 独立账号路由 | 产品 | S | 试点已列；D3 方向 |
| D1 | deliver 节点输出契约 + 本地 commit 封装 | 产品 | S | 试点已列；D4 方向 |
| L1 | DAG 失败边（有界回环） | 产品 | M | 试点已列；D1/D2 方向 |
| A1 | TaskAssignment 结构化（任务卡文件方案） | 试点资产 + 轻壳 | S–M | 本文新增；M0 以文件方案起步 |
| A2 | WorkResult 结构化（skill 契约 + 打点字段） | 试点资产 | S | 本文新增 |
| A3 | BlockerReport 分级升级（skill 契约） | 试点资产 | S | 本文新增 |
| A4 | A2A 状态 ↔ 机器状态映射表 | 产品 | S | 本文新增（§4.1 即映射草案） |
| O1 | SubAgentTree 树形视图 | 产品 | M | 既有 P0 项（B1 方向） |
| M1 | 跨会话记忆（TL 教训） | 产品 | L | 调研 N6/N8，M4 后 |

---

## 6. 架构要点（结论）

1. **角色即隔离**：TL / Programmer / 评审三者上下文刻意不对称，评审独立于前两者——这是"写者不给自己打分"的结构性保证，不是 prompt 约束的运气。
2. **协议即文件 + 工具**：任务卡与结果落盘（`.seelex/a2a/`）承载权威事实（防 context rot、可恢复），工具通道（fork/plan/merge-back/打点/审批）承载消息流动——消息是薄壳，状态是文件。
3. **裁判即机器**：验收标准 = 命令 + exit code；verify 节点是唯一"完成"权威；LLM 只负责做，不负责判。
4. **回环有界**：fix 回环深度 ≤5 或预算内，超限显式升级，不静默重试。
5. **门禁序列固定**：需求锁（人工）→ 验证门（机器）→ 评审门（独立）→ 生产门（人工），不可跳过、不可自批。
6. **演进零破坏**：M1 串行手写驱动先验证语义，M2 再固化为 DAG + 失败边；10 个缺口中 4 个 S 级小改动即可覆盖主要闭环。
