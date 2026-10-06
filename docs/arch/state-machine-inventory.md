# 状态机总表（"状态机没有枚举统一"这件事的地图）

- 口径来源：用户指令 **"状态用枚举和 iota 来规范，不要用零散的字符串做硬编码比较"**，以及
  "记录状态 / 工具事件词 / 工具调用词 / 回执词**都是一件事**：状态机没有枚举统一"。
- 本文是这件事的**唯一地图**：仓库里每一格状态面 → 它的取值面 → 统一后的类型与家 → 现状。
  落地记录与证据在 `docs/2026-10-06-workunit-jobs-port/`（按波次）。
- 统一后的形状（`application/contract/dto/state_codec.go` 是**唯一一份编码口径**）：

  ```go
  type XState uint8                    // iota，0 约定"未知"
  var xStateWords = [...]string{...}   // 枚举 ↔ 对外词，一格一张表
  func (s XState) String() string                    { return xStateWords.word(uint8(s)) }
  func ParseX(text string) (XState, bool)            { o, ok := xStateWords.ordinal(text); ... }
  func (s XState) MarshalJSON() ([]byte, error)      { return xStateWords.marshal(uint8(s)) }
  func (s *XState) UnmarshalJSON(data []byte) error  { return xStateWords.unmarshal(data, (*uint8)(s)) }
  ```

  **各格的差别只有那张表**；`String/Parse/Marshal/Unmarshal` 的实现收在一处（曾经是每格各写一遍，
  连错误消息都各写一遍）。进程内 wire 的未知词**报错**（读不懂就说读不懂）；**落盘**格的未知词
  另有明确取舍（见下 `nodeStateOfRecord`）。

- 判据分层（"枚举化"把判定面从运行期挪到编译期）：
  1. **字段类型** — 状态字段写自己那格的枚举：写错词、跨格混用 = **编译不过**。
  2. **`string(枚举值)` = 陷阱** — 枚举化之后 `string(dto.SubAgentDone)` 照样编译过、`go vet` 也不报，
     但得到的是控制字符（`string(3)` = `"\x03"`）。本波踩到过（用例外先红抓到），
     现有门禁：`e2e/state_enum_cast_gate_test.go`（全仓扫，无白名单）。
  3. **字面量门禁** — `e2e/subagent_status_vocabulary_gate_test.go` 按"格子"声明写方/读方与取值面，
     查**六**种形态（比较/赋值/字段/switch 分支/常量声明/**map 状态键**——`map[string]string{"state":
     "running"}` 这种只有 map 的载荷，前五种都看不见它）。

## 1. 已统一（状态面 → 契约枚举）

| 格 | 取值面 | 类型（家） | 备注 |
|---|---|---|---|
| 后台作业状态 | running\|done\|failed\|killed | `dto.AsyncState` | 登记表在内存（不落盘）；teammate 作业表复用同一格 |
| plan_run 批次结果 | completed\|failed\|aborted | `dto.PlanRunStatus` | 跨 plan 写方 / core 读方的 JSON |
| **记录状态**（子代理节点生命周期） | queued\|running\|done\|failed\|interrupted | `dto.SubAgentNodeStatus` | **落盘**：`sessionstore.NodeSessionRecord.Status` 仍是字符串（store 在契约之下），边界 `seelebridge` 的 `subagentNodeStatusDone/Failed`、`session.SubAgent*` 转调对外词；未知词 → `SubAgentUnknown`（**不是终态**） |
| **工具事件状态** | running\|success\|error | `dto.ToolEventStatus` | `dto.SubagentTool.Status` 与 `dto.SubagentToolEvent.Status` 两处字段同格 |
| **工具调用视图词**（快照 / 事件 wire / 存档里的工具行） | running\|success\|error | `dto.ToolEventStatus`（**与工具事件状态同格**） | `model.ToolCall.Status` 从字符串换成枚举；**落盘**（`model.SessionArchive` 存档 + 事件 payload 的 JSON）读回经具名转换点 `model.ToolCallStatusOfRecord`（认不得的词与空词 → `ToolEventUnknown`，**不折成成功**）；store 那一侧的名词 `sessionstore.ConversationToolCallStatusSuccess` 由 `internal/adapters` 的 `TestToolCallStatusOfRecordLocksTheStoreVocabulary` 互锁。原来的漂移写点（子代理详情投影写 `completed`）已修正——这个词从来不在本格取值面里 |
| **task 状态** | pending\|queued\|running\|doing\|completed\|failed\|retry\|interrupted | `dto.TaskStatus` | `TaskRecord.Status`、`TaskTracePoint.Status`；打点状态与条目状态同格 |
| **回合状态** | idle\|progressing\|completed\|needs_user_decision\|blocked\|interrupted\|failed | `dto.TurnStatus` | 同一格两个面：可见面 `Snapshot.Task.Status`、存档面 `TaskContextProjection.Status`；存档老词 `running` 由 `task_context.TurnStatusOfRecord` 读回（**不再**是 `model.TaskStatus` + `task_context.Status*` 两份词表） |
| **会话可见状态** | draft\|idle\|running\|queued\|awaiting_approval\|archived\|restoring | `dto.SessionStatus` | 持久子集在 `sessionstore.Status`（契约之下）；转换点 `adapters.sessionStatusOfRecord` |
| **计划状态** | pending\|running\|completed\|failed\|aborted | `dto.PlanStatus` | 与 **plan_run 批次结果**（`dto.PlanRunStatus`，只有终态词）分两格，边界写在两格文件头 |
| **节点状态** | pending\|queued\|running\|worktree_creating\|rebasing\|merging\|completed\|failed\|aborted\|skipped\|canceled\|panicked | `dto.NodeStatus` | 框架 workplan 词 + 我们的 worktree 词；折法 `dto.NodeStatusFromFramework`（**带 ok**）；`PlanNodeEvent.Status` 是混合面，不属于本格 |
| **定时任务上次运行结果** | pending\|running\|ok\|failed\|skipped | `dto.ScheduleRunStatus` | GUI 定时任务面板数据源 |
| **goal 状态** | active\|paused\|reviewing\|completed\|failed\|aborted\|waiting_human | `dto.GoalStatus` | 存档面（`sessionstore.GoalFrame.Status`）读回走 `goal.StatusOfRecord`；终态判定在枚举上（`Terminal()`） |
| **评审者状态** | detached\|bound\|evaluating\|advisory_pending\|reaped | `dto.PeerState` | 治理投影 `peer_state`（ADVISOR 那一侧的生命周期） |
| **恢复单元状态** | active\|done\|failed | `dto.UnitStatus` | 恢复模板的**粗分三桶**（由记录状态折一次）；`Unit.Active()` 转调枚举 |

## 2. 刻意**不枚举**（边界格：词不由我们定义，或字段本身是混合值）

| 面 | 为什么不枚举 |
|---|---|
| 回执状态 `asyncPayload.Status`（accepted\|observed\|killed\|already_finished\|retired） | 同一个字段里还流着 Seele `jobs.Manager` 的词（progress\|finished）。把框架的词收进我们的契约 = 框架加词我们就报错。工具侧只写我们自己那五个词，门禁白名单逐条登记 |
| 框架节点状态 `NodeBase.Status`（completed\|failed\|skipped\|…） | 框架 workplan 的词 |
| 统一事件摘要 `SummaryEvent.Status`（failed\|completed） | 框架 Seele 的词 |
| 视图/混合字段：`WorkItem.Status`（task 行 + 子代理行沿用 done 的显示映射）、`WorkTracePoint.Status`、`dto/teamwork_board.go:190` 的"开放取值" | 一个字段承载两格（或明确开放取值），是**展示投影**不是判定面；边界处 `.String()` 显式转换 |
| 落盘 wire：`sessionstore` 的各状态字段 | store 在契约之下，存的是**词**；契约类型在上面，转换点逐格命名（如 `nodeStateOfRecord`）。同理 `sessionstore.ConversationToolCall.Status`（工具行）——它的名词是与契约互锁的 `ConversationToolCallStatusSuccess` |
| git 类字段（porcelain XY、git 名状态）、`TaskPhase*`（plan\|tasklist\|task\|subagent） | 不是状态机：一个是外部工具的原始输出，一个是**类别**不是状态 |
| 事件字段 `dto.PlanNodeEvent.Status` / `model.PlanNodeEventInfo.Status` | **混合面**：本格节点状态词 + 框架 workplan 词 + 我们自己的 worktree 收尾阶段词（`worktree_unmerged` / `merge_blocked`）混在同一个字段里。它不是"节点状态那一格"：认不得的词**不许覆盖节点状态**（旧写法 `default → pending` 会让跑完的节点显示成"待开始"，有回归用例 `plan_node_phase_word_test.go`）。阶段词本身登记在 `seelebridge/node/node_phase_words.go` |
| 工具调用视图词（`model.ToolCall.Status`：running\|completed\|failed） | 与**工具事件状态**（running\|success\|error）同词不同格：详情页里"这条历史工具调用跑完了"是展示口径（快照/transcript wire），不是判定面；收口要连读方清单一起做，登记为下一批 |
| todo 三态（`dto.TodoItemStatus`：pending\|doing\|done） | **已在移除窗口内**的兼容面（权威状态是 `TaskRecord.Status`，文件头写了移除窗口）；唯一转换点是 `seelebridge/task.TodoToTaskStatus`（一处映射）。给它套枚举等于给一个待删面化妆 |
| 存储层的跨域"完成"判据（`sessionstore/stack_channel.go` 的 `terminalStackStatus`） | 计划帧 `closed` / 任务帧 `completed\|failed` / goal 帧 `completed\|failed\|aborted` / 子代理帧 `done` / 归档态 `archived`：这是"整批都完成才弹栈归档"的判据，而 store 不认识各域的类型（在契约之下，只存词）。逐词登记主人，不当成某一格 |
| `CompactBoundaryComplete/Open`（complete\|open）、`StackItem.Status`、`TeamworkState*`、`BoardState*`、`TaskFrame.Status`（active\|completed\|failed\|needs_user_decision）、`PlanFrame.Status`（active\|closed） | store 在契约之下：存的是词；同格类型在上层，转换点逐格具名 |

## 3. 还没统一（下一波候选，逐条写清为什么它是一格）

第二波（`docs/2026-10-06-workunit-jobs-port/step-3-state-enum-unification-wave2.md`）把 §3 原清单
逐条判完并收口；第三波（`step-3-state-enum-unification-wave3.md`）收掉了"工具调用视图词"。
**剩下的只剩一处刻意不动**（todo 三态，理由在 §2）：

| 格 | 结论 |
|---|---|
| ~~`workunit.Status*`~~ | **已收口**：与记录状态同格，现在直接引 `dto.SubAgentQueued/SubAgentRunning`（构造上同一份，不再靠用例比对上另一份） |
| ~~`application/model` 平行词表~~ | **已收口**：四格全部并成契约枚举（回合 / 会话可见 / 计划 / 节点），`model.*` 只剩别名 |
| ~~`core/task_context` 的 `Status*`~~ | **已收口**：与回合状态同格（"running" 统一成 `progressing`，存档老词由 `TurnStatusOfRecord` 读回） |
| ~~`seelebridge/scheduler` 的 `scheduledStatus*`~~ | **已收口**：`dto.ScheduleRunStatus` |
| ~~goal 的 `Status` / `PeerState`~~ | **已收口**：`dto.GoalStatus` / `dto.PeerState`；看板 `open\|closed` 判定为 store wire（契约之下） |
| ~~`sessionstore` 的其余状态字段~~ | **已收口**：逐个判定为"契约之下的 wire + 具名转换点"；唯一的跨域判据（`terminalStackStatus`）逐词登记主人 |
| `application/core/resume` 的 `UnitStatus` | **已收口**：`dto.UnitStatus`（由记录状态折一次） |
| 工具调用视图词（`model.ToolCall.Status`） | **已收口 · 第三波**：与**工具事件状态**本来就是同格（取值面 `running \| success \| error`，不是"同词不同格"）；字段换成 `dto.ToolEventStatus`，落盘读回走 `model.ToolCallStatusOfRecord`，门禁加了这一格。原先记的 `running\|completed\|failed` 与代码事实不符（唯一写 `completed` 的地方就是子代理详情投影的一处漂移） |
| todo 三态（`dto.TodoItemStatus`） | **刻意不动**（见 §2）：已在移除窗口内 |
| teammate 人状态（`dto.TeamworkMemberView.Status`：`running`\|`free`） | **只登记，未动**：取值面只有"这个人此刻在不在干活"两值，`dto` 里没有对应枚举；两个写点常量在 `seelebridge/runtime_teamwork_board.go:305–306`（`teamworkMemberRunning` / `teamworkMemberFree`）。**归哪一格待定**（自成一格 `dto.TeamworkMemberStatus`，还是折进"记录状态"的读侧投影），登记在 U6 残留点那一批（`step-3-u6-residual-points.md` §6） |

## 4. 怎么继续（免得又变成"每格各写一套"）

1. 新加一格状态：先在 `application/contract/dto/` 定枚举（类型 + words 表 + 4 个方法转调 codec），
   再把字段类型换掉——**编译器会把所有"拿字符串比状态"的点列出来**。
2. 落盘格：字段留在 store 侧当 wire 字符串，转换点写成**一个具名函数**（`nodeStateOfRecord` 这种），
   未知词取舍写在那个函数上（"不炸、也不静默当终态"）。
   若落盘格**同时是 wire 形状本身**（一个结构既要落盘又要上事件总线，例如 `model.ToolCall`）：
   字段可以就是枚举，但要在那个类型上挂一个**宽松的 `UnmarshalJSON`**（别名类型避递归），把
   词读进来经同一个具名转换点折一次——**别把契约枚举那套"认不得就报错"的严格读法直接顶上去**，
   否则老文件里一个没见过的词 = 整个会话读不回来（第三波的反证原文见落地记录 §3）。
3. 加门禁范围：`statusVocabularyScopes` 加一格（写方/读方清单 + 取值面），
   顺手确认 `e2e/state_enum_cast_gate_test.go` 仍然绿（不许 `string(枚举值)`）。
