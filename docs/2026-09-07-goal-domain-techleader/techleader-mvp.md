# TechLeader A2A MVP（Part II 可运行切片，2026-09-07）

> 配套 `prototype.md`（Part I：goal 域 + headless 调教接口）与 `design.md`（P0-P2 全量设计）。
> 本文记录 **goal MVP 的 Part II**：单会话双角色 TechLeader + A2A 契约 + 上下文共享机制——
> 全部以 MVP 为导向实现于 `application/core/goal`，可在 headless 上驱动闭环，并有测试收束。

## 0. MVP 定位（与 design.md §8 P1/P2 的对应）

| design 阶段 | MVP 交付 | 落点 |
|---|---|---|
| P1 TechLeader A2A（CSP + 回合） | 有界信箱 + 监督器 + TL 回合 + 触发策略 | `techleader.go` |
| §5.2 信号分类 | 全 SignalKind 类型 + 关键信号立即 / eval_window 抑制 / turn 不触发 | `a2a.go` + `techleader.go` |
| §5.3 上下文共享（有界嵌入） | 每次回合强制嵌入 goal 帧 + 会话尾窗 + 待处理信号 + TL 指令环记忆 | `a2a.go goalFrameOf` / `techleader.go buildEmbed` |
| §5.4 TLDirective | 契约类型 + 校验 + 有界摘要写 goal 指令环 + 待 mainagent 排空 | `a2a.go` / `directive.go` |
| §5.6 终态 gate（P2 子集） | `ProposeFinish`：verdict_done 收口 / not_done 拦截纠偏 / escalate 转人工 / 无 TL 回退直连 | `gate.go` |
| §5.5 审批预筛（P2 子集） | `PreScreenApproval`：low 可 TL 代答 approve/deny；high 与无 TL 一律 escalate（默认人工） | `gate.go` |
| §5.1 CSP actor | MVP 为同步有界队列（确定性可测）；升级 actor 时队列语义不变 | `techleader.go TechLeaderMailbox` |

**刻意不做（Out，留给 P0-wiring / P1 生产接线）**：真实 LLM provider 评估器（注入 `TLEvaluator`）、
`seelebridge/session` ChatStream 信号接线与指令注入、sessionstore 五栈、gui 工作台 goal 视图、
async CSP actor goroutine（注释中给出升级路径）。

## 1. 架构（MVP 层）

```text
                   ┌────────────── application/core/goal ──────────────┐
headless 驱动 ───► │ Controller（goal 栈/状态机/事件，Part I）           │
 (RPC/Client)      │      ▲  ActiveGoal / AppendDirective               │
                   │      │                                             │
                   │  ┌───┴────────────────────────────────┐           │
                   │  │ Supervisor（TechLeader 监督器）       │           │
                   │  │  Notify(信号) / RunEval / Snapshot  │           │
                   │  │  ProposeFinish / PreScreenApproval  │           │
                   │  └───┬───────────┬─────────────────────┘           │
                   │      │ 有界队列   │ TLSessionEmbed                  │
                   │  TechLeaderMailbox  ▲    │                          │
                   │   signals/directives│    ▼                          │
                   │   overflow 计数      │  TLEvaluator（注入：stub/LLM）│
                   └─────────────────────┴──────────────────────────────┘
```

上下文共享（同一 goal 栈，双视角）：
- **TL 不忘目标**：每次 TL 回合 `TLSessionEmbed.Goal` 由 Controller 实况重建（statement/acceptance/
  out_of_scope/status/progress≤3/指令环）；`Validate` 强制 `Goal.ID` 非空（防遗忘编译级护栏）。
- **指令写回共享状态**：`TLDirective.Summary()` 入 goal 指令环（`goal.directive` 事件 + 持久化），
  下一次嵌入作为 `TLMemory` 回读——TL 记得自己说过什么，mainagent 经 Goal 帧/headless 排空可见。
- **有界性**：信号/指令队列封顶（满丢最旧+计数，状态可重读追平）、tail ≤8、content ≤1200 runes。

## 2. A2A 契约（三类消息）

```go
// 执行事件 → TL（design §5.2）
type TLEvalSignal struct { Kind SignalKind; At; Source; Detail; Ref }
//   turn_completed(不评估) | step_checkpoint(window 内抑制) |
//   context_compacted / budget_warning / approval_asked / terminal_proposal(立即) |
//   goal_updated(不评估)
// TL ← 有界输入（每次回合强制带 goal 帧）
type TLSessionEmbed struct { Goal GoalFrame; SessionTail []TurnBrief; Pending []TLEvalSignal;
                             Trigger string; TLMemory []string }
// TL → mainagent（design §5.4 / §5.5 / §5.6）
type TLDirective struct { GoalID; Kind DirectiveKind; Content; Refs; Severity; At }
//   correct | normative_prompt | checkpoint_ok | approve | deny |
//   verdict_done | verdict_not_done | escalate_human
```

`TLEvaluator` 接口即 TL 角色边界（design D6）：评估器只可返回指令，无任何写工具——TL 无执行权。

## 3. Headless 调教接口（Part I 之上新增）

| method | 参数 → 返回 | 语义 |
|---|---|---|
| `goal_tl_snapshot` | – → TLState | TL 读面（回合数/待处理/溢出/窗口） |
| `goal_tl_notify` | TLEvalSignal → ok | 投递执行信号（按策略自动回合） |
| `goal_tl_eval` | {trigger} → TLDirective | 强制一次回合 |
| `goal_tl_tail` | []TurnBrief → ok | 喂有界会话尾窗 |
| `goal_tl_directives` | – → []TLDirective | 排空指令（mainagent 下一轮领取） |
| `goal_propose_finish` | FinishRequest → FinishProposalResult | 终态 gate（TL 裁决后收口/拦截/转人工） |
| `goal_prescreen` | ApprovalScreenRequest → ApprovalVerdict | 审批预筛（low 代答；high 转人工） |

装配：`NewServer(ctl).WithTechLeader(supervisor)`；未装配时 `goal_tl_*` 返回可读错误。

## 4. 上下文共享机制要点（防遗忘 / 防漂移 / 防风暴）

1. **防遗忘**：goal 帧从 Controller 实况重建 + 嵌入校验（缺 goal 帧即错）→ 压缩（`context_compacted`）
   触发强制回合时 TL 仍拿到完整目标（单测 `TestEmbedAlwaysCarriesGoalFrame`）。
2. **防漂移**：指令 `GoalID` 必须等于回合 active goal id，否则 `ErrBadDirective` 拒绝（单测覆盖）。
3. **防风暴**：`turn_completed` 只计数不评估；`step_checkpoint` 受 `EvalWindow`（默认 3 轮）抑制；
   关键信号（压缩/预算/审批/终态）立即评估（单测 `TestEvalWindowSkipsAndFires`/`TestCriticalSignalImmediateEval`）。
4. **防注入污染**：指令经 `goal.directive` 事件与独立 `DrainDirectives` 通道交付，不混入用户消息
   （生产接线时对齐 `view_state.SubagentContextMarker` 前缀惯例）。
5. **无双重写**：TL 只出指令；mainagent 是唯一执行者（design §6/D6）。

## 5. 测试收束（全部 -race 通过，36 用例）

```text
go test ./application/core/goal/... -count=1 -race
```

关键负向/正向用例：
- `TestProposeFinishVerdictNotDoneBlocks`：提前 task_complete → TL 拦截、纠偏写回、goal 保持 active（不弹栈）。
- `TestProposeFinishVerdictDonePops`：裁决 done → completed 弹栈 + 投影回退 + History 审计。
- `TestPreScreenApproval`：low approve/deny 代答；high 与无 TL → escalate（默认人工兜底，不放行高危）。
- `TestEmbedAlwaysCarriesGoalFrame`：压缩后 TL 请求仍含完整 statement/acceptance/progress。
- `TestMailboxBoundedQueuesAndOverflow`：队列封顶、溢出计数、排空幂等。
- `TestHeadlessTLE2E`：headless 全链 begin→tail→notify→drain→propose_finish 拦截→收口。
- `TestConcurrentNotifyRace` / `TestConcurrentBeginFinishRace`：-race 无竞态。

## 6. 下一步接线位（生产化，均在本 MVP 之外）

1. `seelebridge/session/techleader.go`：把 Supervisor 挂到会话运行时，ChatStream 边界投信号/排空指令注入
   （`injectPendingSubagentContexts` 同类位置；TL 回合经 provider LLM 调用，带 10s 超时）。
2. `seelebridge/tools`/executor：goal 条件下把 plan/task 打点、compaction、approval、终态工具调用
   翻译为 `TLEvalSignal`；`task_complete`/`goal_finish` 改走 `ProposeFinish` gate。
3. `sessionstore` 五栈：goal 栈 + 指令环持久化到 state blob（schema bump）。
4. `gui`/工作台：消费 `RuntimeVisibilityProjection.Goals`；审批链插入 `PreScreenApproval`（low 白名单由装配层圈定）。
