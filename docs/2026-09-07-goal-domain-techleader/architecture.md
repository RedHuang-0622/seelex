# Goal 域 + TechLeader A2A 架构速览（2026-09-07）

> 状态：Draft（设计提案，非实现事实）
> 日期：2026-09-07
> 思路来源：goal 调研（`docs/research/goal-context-governance-2026-09.md`、
> `docs/research/codex-claude-goal-harness-2026-09.md`）+ 用户对 Codex/Claude goal 机制的调研结论
> （getGoal/updateGoal/finishGoal，会话粒度/会话单例）+ 敏捷 A2A 既有设计（`docs/2026-08-07-agile-a2a/`）。
> 详细设计见同目录 `design.md`；本文是架构总览与关键决策。

---

## 0. 一句话

把 goal 从「提示词技能 + 布尔投影」升级为**会话内一等运行时对象**（与 plan/task 同桌、压入 goal 栈、
工作台展示栈顶+下层状态），并在 goal 条件下给同一会话装配第二个角色 **TechLeader**（与 mainagent
共享会话，system prompt 强制嵌入 goal 内容，经 CSP channel 收信号、收会话嵌入、回吐纠偏指令），
用显式**状态机**判定会话/目标何时结束——用 harness 承载目标，让 LLM 少做"记得停/记得目标"这类事。

分层（用户明确的两层）：

- **Part I = goal 发起与存储**（注册/更新/完成、栈、投影）——本文 §2-§3；
- **Part II = goal 实现**（goal 条件下怎么干活：TechLeader 纠偏、减少 human judge、结束判定）——本文 §4-§6。

---

## 1. 现状与差距（为何要升级）

| 现状（代码事实） | 差距 |
|---|---|
| goal = 激活 goal skill → `application/core/input.go:108-110` `SetMaxLoops(9999)`（无限循环） | 目标只活在提示词里：无对象、无状态、无"完成"判定 |
| 投影只有 `GoalSkillActive bool`（`application/contract/dto/projection.go`） | 工作台"目标"不可见、不可恢复 |
| `ParentEvidenceProjection.Goal = latestVisibleUserGoal`（最近一条用户消息，截断 200 runes，`application/core/runtime_projection.go`） | 工作台把"发起聊天的提示词"当目标，无栈、无状态 |
| plan 工具族仅 goal skill 激活时可见（`seelebridge/tools/policy.go:46` `isPlanTool`） | goal 自身没有工具面（get/update/finish） |
| plan/task 状态在 `task_context.Coordinator.sessionTaskRuntime`（`planStack`/`activePlanID`/`taskExecution`）+ sessionstore state blob（Plan/Task/Skill/Compact 四栈） | goal 未与 plan/task 同桌、未入栈、未持久化 |
| task 终态协议 `task_complete/failed/needs_user_decision`；子代理 merge-back 有 mailbox actor 范式（`seelebridge/session/subagent_context.go`，seelactor + 有界命令通道 + 超时） | 无第二角色（TechLeader）、无共享会话双视角、无 CSP 信号通道 |
| 审批 `ask_approve`/ApprovalBroker | human judge 全量人工；无"TL 预筛无关紧要审批"路径 |
| 恢复/续跑靠 plan checkpoint + `ContextSnapshot.PendingWork` | 无 goal 级续跑（finish 语义、预算耗尽暂停、栈下层恢复） |

**结论**：Codex/Claude 已把 goal 做成 harness（目标生命周期 + 预算 + 停机 + 视图，见调研报告）；
Seelex 追赶的差异化 = **单会话双角色 TechLeader**（敏捷 A2A 内联化）+ **goal 栈工作台**。

---

## 2. 目标与设计原则

1. **goal 一等对象、会话粒度、会话单例**：每个会话同一时刻至多一个 active goal（栈顶）；goal 与
   plan/task 同桌（同一 `sessionTaskRuntime` 数据面、同一持久化 state blob）。
2. **goal 后置入栈**：装配顺序 goal 帧插在 plan 之前、贴近当前输入、不参与压缩、每轮重建；
   `goal_finish` 出栈（删除工作台语义，审计归档可选）。
3. **工作台目标 = goal 栈顶 + 下层 goal 状态**：不再拿"最近一条用户消息"当目标；栈空时回退现有行为。
4. **TechLeader 与 mainagent 共享同一会话**：mainagent 照旧执行（不感知 TL）；TL 在 goal 条件下注册，
   只读评估 + 输出纠偏/规范指令，**不做执行写**。
5. **TL 不忘目标**：TL 的 system prompt 每次评估时强制嵌入 goal 内容（栈顶 statement/acceptance/进度）。
6. **结束判定是状态机**：mainagent 提议终态 → TL 校验 → verdict → 状态迁移；不靠模型自觉"做完就停"。
7. **角色信号走 CSP channel**：mainagent 执行事件（信号）+ 有界会话嵌入 → TL；TL 指令 → 注入
   mainagent 下一轮。复用 `seelactor.Actor` 有界命令通道范式。
8. **减少 human judge**：低风险/规范可判的审批与过程问题先由 TL 代判；高风险与越权仍人工（默认拒绝）。

---

## 3. 分层视图

```text
┌─────────────── 会话（Session，持久化 state blob + DurableHistory）───────────────┐
│                                                                                   │
│  RuntimeVisibilityProjection  ◄── Goal 栈（栈顶=当前目标 + 下层状态）  ──► 工作台  │
│                                                                                   │
│  sessionTaskRuntime（task_context.Coordinator 分片）                              │
│    ├─ taskExecution / taskService       （任务终态，已有）                         │
│    ├─ planStack / activePlanID          （Plan DAG，已有）                        │
│    └─ goalStack / activeGoal            （Goal 对象，新增 Part I）                │
│                                                                                   │
│  装配顺序: system → project → memory → compact → context →【goal】→ plan → task→输入 │
│                                                                                   │
│  mainagent 循环（执行）：ReAct + 工具（plan/task/goal 工具族）                     │
│        │ 事件信号 ──────────────► TechLeaderMailbox（CSP actor）                  │
│        │ 会话嵌入（有界 embed）──►    │   │                                        │
│        ◄── TLDirective（注入边界）    │   │                                        │
│                                      ▼   ▼                                        │
│  TechLeader 循环（评估，goal 条件下注册）：                                        │
│     system = TL 角色模板 + 【goal 内容嵌入】+ 最近 TL 指引 + 本次 embed             │
│     输出 = TLDirective / TLCompletionVerdict / 审批预筛                            │
│                                                                                   │
│  会话/目标结束判定状态机（显式）：mainagent 终态提议 → TL verdict → 状态迁移        │
└───────────────────────────────────────────────────────────────────────────────────┘
```

---

## 4. 关键决策（D1–D8，待用户拍板）

| # | 决策 | 内容 | 待确认 |
|---|---|---|---|
| D1 | goal 栈语义 | 会话内 goal 栈 LIFO；栈顶 = 当前 active goal；压栈挂起下层；`goal_finish` 弹栈 | ✅ 需确认 |
| D2 | 会话单例 | v0 默认 `goal_stack_depth=1`（严格单例，压栈即报错或替换）；配置放开嵌套 | ✅ 需确认 |
| D3 | finish 语义 | 工作台/运行时删除（用户表述）；审计归档到 state blob goal-history 可选，默认开 | ✅ 需确认 |
| D4 | fork 继承 | 默认不继承父 goal 栈（对齐"fork 不继承父 todolist"）；可带父 goal 文本作草稿 | ✅ 需确认 |
| D5 | TL 触发 | 事件驱动 + 最小间隔；TL 不每轮跑 | ✅ 需确认 |
| D6 | TL 无写工具 | TL 只读评估 + 输出指令，不直接改文件/不执行写工具 | ✅ 需确认 |
| D7 | 审批预筛 | 仅低风险/白名单审批由 TL 代答；高写风险与越权默认人工 | ✅ 需确认 |
| D8 | goal 帧落点 | 装配顺序 goal 插在 plan 前；每轮重建、不参与压缩（后置桌面） | ✅ 需确认 |

---

## 5. 里程碑（详见 design.md §8）

- **P0 — Goal 域落地**：GoalRecord/GoalRuntime + 状态机 + goal 工具族（goal_begin/update/finish/status，
  入 `isGoalTool` 门控）+ Goal 帧注入 + state blob 第五栈（schema bump）+ 工作台投影 + fork/恢复语义。
- **P1 — TechLeader A2A**：`TechLeaderMailbox`（CSP actor）+ 信号接线 + 有界会话嵌入 + TL 评估循环
  （system 嵌 goal）+ TLDirective 注入 + TL 触发策略与预算。
- **P2 — 收敛与治理**：会话结束状态机（终态提议 → TL verdict → 迁移）+ 审批预筛（human judge 前置）+
  goal 预算护栏（耗尽 → waiting/needs_decision）+ 度量与 GUI goal 视图。
