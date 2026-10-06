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
     查五种形态（比较/赋值/字段/switch 分支/常量声明）。

## 1. 已统一（状态面 → 契约枚举）

| 格 | 取值面 | 类型（家） | 备注 |
|---|---|---|---|
| 后台作业状态 | running\|done\|failed\|killed | `dto.AsyncState` | 登记表在内存（不落盘）；teammate 作业表复用同一格 |
| plan_run 批次结果 | completed\|failed\|aborted | `dto.PlanRunStatus` | 跨 plan 写方 / core 读方的 JSON |
| **记录状态**（子代理节点生命周期） | queued\|running\|done\|failed\|interrupted | `dto.SubAgentNodeStatus` | **落盘**：`sessionstore.NodeSessionRecord.Status` 仍是字符串（store 在契约之下），边界 `seelebridge` 的 `subagentNodeStatusDone/Failed`、`session.SubAgent*` 转调对外词；未知词 → `SubAgentUnknown`（**不是终态**） |
| **工具事件状态** | running\|success\|error | `dto.ToolEventStatus` | `dto.SubagentTool.Status` 与 `dto.SubagentToolEvent.Status` 两处字段同格；transcript/快照 wire（`model.ToolCall.Status`）保持字符串，边界 `.String()` |
| **task 状态** | pending\|queued\|running\|doing\|completed\|failed\|retry\|interrupted | `dto.TaskStatus` | `TaskRecord.Status`、`TaskTracePoint.Status`；打点状态与条目状态同格 |

## 2. 刻意**不枚举**（边界格：词不由我们定义，或字段本身是混合值）

| 面 | 为什么不枚举 |
|---|---|
| 回执状态 `asyncPayload.Status`（accepted\|observed\|killed\|already_finished\|retired） | 同一个字段里还流着 Seele `jobs.Manager` 的词（progress\|finished）。把框架的词收进我们的契约 = 框架加词我们就报错。工具侧只写我们自己那五个词，门禁白名单逐条登记 |
| 框架节点状态 `NodeBase.Status`（completed\|failed\|skipped\|…） | 框架 workplan 的词 |
| 统一事件摘要 `SummaryEvent.Status`（failed\|completed） | 框架 Seele 的词 |
| 视图/混合字段：`WorkItem.Status`（task 行 + 子代理行沿用 done 的显示映射）、`WorkTracePoint.Status`、`dto/teamwork_board.go:190` 的"开放取值" | 一个字段承载两格（或明确开放取值），是**展示投影**不是判定面；边界处 `.String()` 显式转换 |
| 落盘 wire：`sessionstore` 的各状态字段、`model.ToolCall.Status` | store/model 在契约之下，存的是**词**；契约类型在上面，转换点逐格命名（如 `nodeStateOfRecord`） |
| git 类字段（porcelain XY、git 名状态）、`TaskPhase*`（plan\|tasklist\|task\|subagent） | 不是状态机：一个是外部工具的原始输出，一个是**类别**不是状态 |

## 3. 还没统一（下一波候选，逐条写清为什么它是一格）

| 格 | 取值面 / 位置 | 备注 |
|---|---|---|
| `workunit.Status*` | queued\|running\|…（`seelebridge/workunit`） | 与**记录状态**同词面（`stage_preview_judgment_test.go` 的守卫逐词断言两者相等）——先判定"是不是同一格"，是则转调契约 |
| `application/model` 平行词表 | `model.TaskStatus`（progressing\|completed\|blocked\|interrupted\|failed…）、`model.SessionStatus*`、`model.NodeStatus`（pending\|queued\|running\|completed\|failed） | **同名不同机器 + 与 `dto` 平行**：这正是"状态机没有枚举统一"的核心症状。要逐格定：合并、改名、还是登记为独立格 |
| `application/core/task_context` 的 `StatusRunning/StatusFailed` | 任务执行状态 | 与 `model.TaskStatus` 的关系要先点清 |
| `seelebridge/scheduler` 的 `scheduledStatusPending/Running` | 定时任务状态 | 独立一格 |
| `application/core/goal` 的 `Status` / `PeerState` / 看板 `state(open\|closed)` | goal 生命周期与看板 | 落盘（goal 存档） |
| `sessionstore` 的其余状态字段 | `fork_store`(running\|archived\|merged)、`pending_tail`、`attempt_cache`、`board`、`compact_frames`(complete\|open)、`conversation`、`project_record` | 逐个判定词表归属（落盘 wire 还是我们的状态机） |

## 4. 怎么继续（免得又变成"每格各写一套"）

1. 新加一格状态：先在 `application/contract/dto/` 定枚举（类型 + words 表 + 4 个方法转调 codec），
   再把字段类型换掉——**编译器会把所有"拿字符串比状态"的点列出来**。
2. 落盘格：字段留在 store 侧当 wire 字符串，转换点写成**一个具名函数**（`nodeStateOfRecord` 这种），
   未知词取舍写在那个函数上（"不炸、也不静默当终态"）。
3. 加门禁范围：`statusVocabularyScopes` 加一格（写方/读方清单 + 取值面），
   顺手确认 `e2e/state_enum_cast_gate_test.go` 仍然绿（不许 `string(枚举值)`）。
