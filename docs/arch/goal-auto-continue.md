# goal 未收口 + 被中断时的自动续跑

- 状态：§1 是现状核对；§2.1–§2.7 已施工（续跑 gate + 上限/退避 + 幂等 + 打点），
  §2.8 是**暂停 / 继续**（对标产品：goal 顶部一条 stalled 横条，右侧 ⏸ 与 ⟳）的设计，
  也已施工并带用例。仍未接线的是 GUI 按钮与装配注入（见 §2.8.6）。
- 结论一句话：**不新建调度器，把"终态 → 会话空闲 → 起一个回合 → 每个键只触发一次"这条既有链路
  （`application/execution/completion.go` 的 `CompletionApplication`）扩一个入口，判据换成
  "goal 栈还有 active 帧"，输入形态沿用"合成一条用户输入"**。
- 相关：`kernel/goal/README.md`、`kernel/sessionpolicy/README.md`、`docs/arch/session-board-metadata-lifecycle.md`、
  `docs/2026-09-24-async-tool-deferred-ack/README.md` §10.5（同族缺口的先例）。

---

## 1. 现状核对（每条都能在仓库里点开）

### 1.1 goal 栈语义与 `goal_done`

| 事实 | 证据 |
|---|---|
| 栈状态机唯一入口是 `Controller.Begin/Update/Finish/Abort/Reload/Status` | `kernel/goal/controller.go` |
| `Finish` 把栈顶标 `completed` 并**弹栈**；`Abort` 标 `aborted` 弹栈；两者都追加一条审计 | `kernel/goal/controller.go` `finishOrAbort`、`kernel/goal/audit.go` |
| `goal_done` 工具 → `Application.DoneHandler` → `DoneFor` → `Coordinator.FinishDirect` → `Controller.Finish/Abort`（无中间评审环） | `application/goal/application.go`、`application/goal/coordinator.go`、`application/goal/done_test.go` |
| **"未收口"的现成判据** = `StatusView.Active != nil` | `Type GoalStatusView struct{Stack []*GoalRecord; Active *GoalRecord; History int}` 在 `contract/dto/goal.go`；`Controller.Status()` 组装，`Application.StatusFor(sessionID)` 对外 |
| 终态判定已集中 | `kernel/goal/record.go` `IsTerminal(status Status)` = `status.Terminal()` |
| 栈落会话第五栈，重启可恢复 | `kernel/goal/sessionstore_store.go`（`Load`/`Save`）；`application/goal/coordinator.go` `bundleFor` 里 `controller.Reload(...)`，失败退化为空控制器 |
| TL/ADVISOR 评审环已删除，goal 不驱动 LLM | `kernel/goal/README.md` 首段（2026-10-17 用户裁决） |

结论：**goal 侧已经有"是否收口"的权威读面，续跑不需要自己判 goal。**

### 1.2 `SessionExecutionRecord.Continuation`

- 形状：`contract/protocol/state.go`
  ```go
  type SessionExecutionRecord struct {
      Task         *TaskState
      ReadFiles    []ReadFileRef
      Continuation string
  }
  ```
- **写侧唯一来源**：`kernel/session/archive.go` 组装 `SessionRecord` 时
  `if continuation := c.tasks.ContinuationSummaryFor(sessionID, requestID); continuation != ""`；
  摘要本体来自 `kernel/task/task_context_state.go` 的 `ContinuationSummaryFor` → `TaskExecutionState.ContextSummary()`。
  （`kernel/session/fork.go` 在分叉时把它清空；`internal/host/session_workspace_ports.go` 的 `migrateSessionArchive` 从旧 v1 存档 1:1 搬过来。）
- **读侧唯一消费者**：`kernel/session/archive.go` `recordResumeHistory(record)` —— 把
  `SessionArchiveResumePrefix + "\n" + continuation` 作为**一条 user 消息塞进 engine history**，
  仅在冷加载兜底时调用（`application/session/cold/history.go` `result.EngineHistory = deps.RecordResumeHistory(loaded.Record)`）。

结论：**`Continuation` 是"冷加载时告诉模型历史还在"的历史补丁，它没有任何"启动一个回合"的能力。**

### 1.3 `task_context.IsContinuableStatus` 与 `TaskCheckpoint`

- `IsContinuableStatus(status model.TurnStatus) bool` 在 `kernel/task/task_execution.go`，
  认 `TurnProgressing / TurnInterrupted / TurnBlocked / TurnNeedsUserDecision`。
- 唯一消费点：`application/conversation/turn_process.go` `BeginLocked`
  （`previousTask != nil && task_context.IsContinuableStatus(previousTask.Status)` → `BuildTaskCheckpointLocked`
  → `BeginTaskFor(..., previousTask, previousCheckpoint)`）；
  继承逻辑在 `kernel/task/task_execution.go` `continuationTaskExecutionState`
  （可续才继承 objective/checkpoints/toolOutcomes/ProgressEpoch；**会话**事实如 `ContextVersion`、
  `ContextRetainedFrom`、`ContextCompactions` 无论可续与否都继承）。
- `TaskCheckpoint` 在 `contract/protocol/context.go`：有界事实与引用
  （`CompletedWork/PendingWork/Decisions/Failures/ChangedFiles/Artifacts/ToolResultRefs` + `CoversEventRange`），
  **明确排除原始工具输出与推理文本**。

结论：这两个符号管的是"**新回合能否继承上一回合的现场**"，不是"是否该自动起一个新回合"。
`TurnBlocked` / `TurnNeedsUserDecision` 恰好在可续表里，而它们恰恰**不该**被自动续跑 —— 见 §2.1。

### 1.4 `kernel/sessionpolicy` 与 `kernel/rolesessions` 的 idle / 自动续跑策略

**实测结论：这两个包里都没有 idle 策略，也没有任何自动续跑策略。**

- `kernel/sessionpolicy`（包名 `switching`）是**纯判据包**，现有成员：
  `release.go` `Busy(BusyState)` / `Protected(ProtectedInput)`（忙 = 待审批 / 运行 / 排队 / 在飞）、
  `queue.go` `QueueEditGate` / `QueueTargetSession` / `QueueTargetError`、
  `unload.go` `Unload`、`fork.go` `ForkGate`、`pendingtail.go` `RecoverPendingTail`、
  `async.go` `AsyncCompletionTriggers(dto.AsyncState)` + `TeamworkTriggeredPrefix` + `TeamworkJobEvents`、
  `draft.go` / `meta.go` / `interaction.go` / `releasewindow.go` / `bundle_release.go`。
- `kernel/rolesessions/roles.go` 是**纯转发门面**：`RolePort()` / `SchedulePort()` 取端口后逐方法转发
  （`CreateRoleSession`、`ScheduleRegister/Cancel/Fire` …），README 明说「不做：不持久化、不创建会话、不调度任务」。
- idle 的真实 owner 在 application 侧两处：
  1. **可见状态唯一判据** `application/session/state/visible_status.go` `ProjectVisibleStatus(VisibleStatusFacts)`
     （`Restoring → !HasUnit → PendingApproval>0 → Running → PendingRequests>0 → Idle`）；
  2. **"能不能起一个回合"的判据** `application/assembly/async_completion.go` `sessionIdleForAsyncTrigger`
     （`!closed && !draining && unit != nil && !unit.ChatState().Running`）。

结论：续跑**不往 `kernel/sessionpolicy` / `kernel/rolesessions` 里加东西**（否则就是第二份 idle 判据）。

### 1.5 四个中断入口，分别走哪段代码

一律终止在同一个可判定状态上：`TaskState.Status` 变成终态，落 `SessionRecord.Execution.Task`
（`kernel/session/archive.go`），冷读用 `kernel/task/turn_status_record.go` `TurnStatusOfRecord` 读回。

| 入口 | 调用链（真实符号） | 落点状态 |
|---|---|---|
| **用户停止** | `gui/bridge.go` `Bridge.CancelChat` / `tui/tui.go` → `application/lifecycle/application.go` `CancelChat` → `application/assembly/service_assembler_scheduler_lifecycle.go` 里的 `CancelChat:` 闭包 → `unit.CancelFunc()()`；随后 `application/conversation/turn_error.go` `Record` 命中 `errors.Is(err, context.Canceled)` → `Interrupt(requestID, "The task was canceled before completion.")` | `TurnInterrupted`（摘要文本是唯一区分信息） |
| **流式中断 / provider 失败** | 同一 `TurnErrorApplication.Record`：`ClassifyProviderFailure(err) != ProviderFailureNone` → `Interrupt(requestID, "The provider interrupted the task before completion.")`；provider 上下文耗尽另有 `application/conversation/context/recovery.go`（`"Recoverable Provider Interruption"`） | `TurnInterrupted` |
| **额度耗尽** | `backend/engine/account/failure.go` `FailureQuota`（`quotaMarkers`）+ `internal/stream` 换账号 failover；换不出可用账号即按上一行的 provider 失败收尾 | `TurnInterrupted` |
| **进程重启** | 运行时无收尾：靠落盘 `record.Session.Status` 读回判定，`application/conversation/unified.go` `MainRecovery`：`Status == SessionStatusRunning` → `dto.RecoveryInterrupted`；随后 `application/session/cold` 冷加载 + `RecordResumeHistory` | 冷加载后 `Execution.Task.Status = TurnInterrupted` |

**顺带一条必读语义**：用户停止**不会丢队列** —— `application/assembly/service_input.go` 的 `CancelChat`
注释写死"清空消息队列并全部发出去：排队输入在回合收尾整批提升为下一轮"，
实现是 `application/conversation/turn_process.go` `PromoteLocked`。

### 1.6 已有的"终态 → 起一个回合"机制（本设计的模板）

`application/execution/completion.go`：

```go
type CompletionDeps struct {
    AsyncRuns          func() []dto.AsyncRunRecord
    TeamworkRuns       func() []dto.TeamworkJobCompletionRecord
    SessionIdle        func(string) bool
    SubmitConversation func(context.Context, string, string) error
    EvidenceChars      func() int
}
func (a *CompletionApplication) TriggerAsyncCompletions(triggered map[string]struct{}) int
func (a *CompletionApplication) TriggerTeamworkJobCompletions(triggered map[string]struct{}) int
```

- 幂等账：进程内 `triggered map[string]struct{}`，键 = 作业句柄（句柄单调、永不复用）。
- 空闲门：`SessionIdle`（装配成 `sessionIdleForAsyncTrigger`）。
- 起回合：`SubmitConversation`（装配成 `conversationTurn.submission.SubmitFor`，
  见 `application/assembly/service_assembler_capabilities.go`）。
- 驱动：`application/execution/table/consumer.go` `AsyncConsumer.Run` —— 事件通道 + `RetryDelay` 重试，
  `triggered` 账在 consumer 里持有；装配在 `application/execution/table/runtime.go`。
- 开关：`kernel/limit/limits.go` `AsyncExecLimits.TriggerConversation`（零值 = 关，出厂档
  `config/seelex.yaml` 显式 `trigger_conversation: true`）。
- 语义边界（同一处注释写死）：done/failed 触发、killed 不触发、只对空闲会话触发、
  每个句柄只触发一次。

**这就是"自动续跑"要复用的骨架，一个零件都不用新造。**

---

## 2. 设计

### 2.1 ① 触发条件：什么叫"goal 未收口且真的被中断"

四个**与**条件同时成立才续，缺一不续：

```
C1 goal 未收口      goal.Application.StatusFor(sessionID).Active != nil && !IsTerminal(Active.Status)
C2 真被中断        上一回合终态 == TurnInterrupted（且不是用户停止，见 C3）
C3 用户没喊停       !record.Execution.StoppedByUser（跨重启有效）
C4 现在空闲且不忙   sessionIdleForAsyncTrigger(sessionID) && !switching.Busy(BusyState{...})
```

三类收尾的判定依据（都在代码里查得到）：

| 类别 | 判定 | 依据 |
|---|---|---|
| **正常收尾**（不续） | `TurnCompleted` / `TurnFailed` | 不在 `IsContinuableStatus` 表内；`TurnFailed` 是"已验证的失败"，重试属于模型自己的责任，不是宿主的 |
| | `TurnNeedsUserDecision` / `TurnBlocked`（**也归此类**） | 虽在 `IsContinuableStatus` 表内（那是给 checkpoint 继承用的），语义是"等用户/等外部"，`kernel/task/task_service.go` 在 plan 待执行等处写入；自动续跑会变成自问自答 |
| **用户主动停止**（不续） | `record.Execution.StoppedByUser == true` | **现在区分不出来**：`CancelChat` 与 provider 失败的落点都是 `TurnInterrupted`，只能比 `TaskState.Summary` 文案（"The task was canceled before completion."），字符串比对不可靠且不跨重启。因此本设计**新增一个显式位**（见 §2.3 防呆 1） |
| **硬中断**（续） | `TurnInterrupted` 且 `StoppedByUser == false` | 三个来源统一收口到 `TurnInterrupted`：`turn_error.go`（provider/canceled）、`conversation/context/recovery.go`（上下文/额度耗尽）、`MainRecovery` 判 `RecoveryInterrupted`（进程重启） |

补充两条排除项（都是踩过的坑）：

- **排队输入在的时候不续**：`PromoteLocked` 已经会把用户排队输入整批提升为下一轮，
  此时再插一条"继续"就是往用户队列里塞脏输入。判据落在 C4 的 `BusyState.QueuedCount > 0` / `PendingRequests > 0`。
- **待审批在的时候不续**：`BusyState.PendingApprovals > 0`（人正在等你点头）。

### 2.2 ② 续跑输入形态：合成"继续"用户输入（复用 `SubmitConversation` 通道）

**选合成输入，不挪用 `Continuation` 字段。** 理由四条，全部是代码事实：

1. `Continuation` 没有启动能力。它唯一的读点是 `recordResumeHistory`，只往 **engine history** 里塞一条
   *带前缀的 user 消息*，供冷加载兜底（§1.2）。要让"读 Continuation"变成"跑一轮"，得新造一条
   回合启动路径 —— 那才是另起一套。
2. 合成输入走的是**唯一既有的回合启动链**：`SubmitConversation → SubmitFor → startChat → TurnRunner.Run
   → TurnProcessApplication.BeginLocked`。这条链免费带来：checkpoint/skills/ProgressEpoch 继承
   （`continuationTaskExecutionState`）、transcript 落一条可审计的 user 行、排队语义、
   stale request 保护（`TurnOutcomeApplication.ApplyLocked` 的 `ChatState().RequestID != requestID` 分支）。
3. `CompletionApplication` 已经在用这条通道起回合，且被 `application/assembly/integration/async_completion_trigger_test.go` 覆盖。
   同一条路，零新增机制。
4. `Continuation` 的既有职责必须保住：它同时是**冷加载**的兜底材料，被挪用后冷加载会退化成无摘要。

续跑输入正文的要求（写成模板，不散落在多处）：

```
goal 未收口，上一轮被中断（request=…，原因=<interrupted|provider|restarted>）。
- goal: <Active.Title>（id=…）
- 上次进度: <TaskCheckpoint.CompletedWork 摘要>
- 待办: <TaskCheckpoint.PendingWork>
请接着推进；先把已完成部分复核一遍再动手。若目标其实已经达成，用 goal_done 收口并只报告结论。
```

只有 `PendingWork` 空时才退回"读回现场再继续"的通用措辞 —— 不做整段上下文重放（那是重放，不是继续）。

### 2.3 ③ 防呆四条

**1) 用户显式停止优先（不覆盖人工意图）**

- 落盘位（跨重启）：`contract/protocol/state.go` `SessionExecutionRecord` 增
  `StoppedByUser bool` / `StoppedAt time.Time`。写点唯一：`CancelChat` 闭包
  （`application/assembly/service_assembler_scheduler_lifecycle.go`）。
- 清除点唯一：`application/conversation/turn_process.go` `BeginLocked`（开新回合 = 用户已经自己继续了）。
- 判据：C3。**只要用户点过停止，哪怕进程重启也不再续**，直到用户自己发下一条。
- 额外一票否决：续跑前重判 `unit.ChatState().Running`（用户已经自己发了话，正在跑，不要插队）。
- 不动的既有语义：`CancelChat` 的"清空队列并整批提升"（§1.5）保持不变 —— 那是"用户的输入一条不丢"，
  与"系统替我发一句"是两件事。

**2) 最大次数 + 退避（避免无限烧钱）**

- 新配置块 `kernel/limit/limits.go` `ContinuationLimits{Enabled, MaxAttemptsPerGoal, BackoffSec, MaxBackoffSec}`，
  形状与纪律照抄 `AsyncExecLimits`：**零值 = 关**，出厂档显式打开，`WithDefaults` / 校验同步。
- 计数口径：**同一 goalID 的连续 attempt**；成功续跑（`ChatState().RequestID` 变化且新回合终态 `TurnCompleted`）后归零。
- 退避：`BackoffSec × 2^(attempt-1)`，封顶 `MaxBackoffSec`；退避期间不扫（复用 `AsyncConsumer` 的
  `RetryDelay` 定时器语义，不新增定时器）。
- 触顶：记一条 `truncated = true` 的审计（§2.4）后**停**，不静默重试，不降级为"再试一次"。

**3) 幂等（重启 / 重复触发不重复续）**

- 幂等键 `(goalID, interruptedRequestID, attemptSeq)`。
  - 进程内：`triggered map[string]struct{}`，与 `TriggerAsyncCompletions` 同形。
  - 跨重启：`SessionExecutionRecord.Continuations []ContinuationAttempt`（§2.4）。触发前比对
    `record.Execution.Task.RequestID` 与最近一次 attempt 的 `RequestID`：**同一个中断 requestID 不续第二次**。
- "真的起了一个回合"才递增计数：以 `unit.ChatState().RequestID` 是否变化为证据（`SubmitConversation` 返回 nil 不等于起了回合）。
- 重启后再判一次 C1（`Active != nil`）—— 重启前后 goal 可能已被别处收口。

**4) `goal_done` 之后不再续**

- 判据就是 C1，每次触发前现读 `StatusFor(sessionID).Active`，为 nil 直接短路。
- 续跑输入里那句"若已达成用 goal_done 收口"只是**给模型的提示**，不作为判据（判据只有 `Active`）。
- 收口动作本身已有落点：`Controller.Finish` 弹栈 + 审计（§1.1），无需为续跑加任何"收口钩子"。

### 2.4 ④ 可观测：每次续跑记什么、落在哪

记 5 个字段：`{At, GoalID, RequestID(被中断的那一轮), Reason(interrupted|provider|restarted), Attempt, Truncated}`。

两个落点，职责分开：

1. **现场日志**：`log.Printf("[continuation] …")`，与 `application/execution/completion.go` 里既有的
   `log.Printf("[async] 后台作业 %s 终态触发对话失败…")` 同格式同位置（失败也要有日志）。
2. **权威账（append-only，给人读）**：goal 审计账本 `kernel/goal/audit.go` `AuditEntry`，
   新增一个**非终态** kind `EventContinue EventKind = "goal.continue"`
   （词表在 `kernel/goal/controller.go`，现有 `goal.begin/update/finish/abort/restore` 五个）。
   选它的理由：已 append-only、已按会话隔离、文本已有 rune 上限（`MaxAuditTextRunes`）、
   已随看板存档可读（`kernel/goal/board_archive.go`）；且 `isTerminalAuditKind` 只认 `finish`/`abort`，
   非终态条目**不会污染收口账本与看板 history**（`mergeBoardHistory` 只看终态）。
3. **计数与 truncated 的持久面**：`SessionExecutionRecord.Continuations`（跨重启需要它；审计账本是文本账，不适合当计数权威）。

**刻意不做**：不新增 GUI 面板 / 不新增事件 kind。`GoalGovernanceView` 已能显示 `History`，
里程碑要的是"可核对"，不是新增一条投影。

### 2.5 ⑤ 与现有 sessionpolicy / continuation 机制的关系

| 对象 | 关系 | 说明 |
|---|---|---|
| `CompletionApplication`（`application/execution/completion.go`） | **复用骨架** | 新增同形方法 `TriggerGoalContinuations(triggered map[string]struct{}) int`，用同一个 `SessionIdle` 门、同一个 `SubmitConversation` 通道、同一种 `triggered` 幂等账。不新建包、不新建循环 |
| `AsyncConsumer.Run`（`application/execution/table/consumer.go`） | **扩展一处** | `AsyncConsumerDeps` 加 `ContinuationEvents <-chan struct{}`（第三个信号源）+ 开关透传；重试/退避沿用其 `RetryDelay` 定时器 |
| 回合结束漏斗（`application/conversation/turn_commit.go` `Commit` → `application/conversation/turn_runner.go` `Run`） | **扩展一处** | 在整个仓库唯一的"回合收尾"调用点 `Run` 里、`Commit` 返回之后（锁已释放）非阻塞地 emit 一次续跑信号。`SubmitFor` 会重取 `ViewMu`，不能在 `Commit` 的锁内同步发 |
| `sessionIdleForAsyncTrigger`（`application/assembly/async_completion.go`） | **复用** | 空闲口径只此一份 |
| `kernel/limit/limits.go` | **扩展一块** | 新增 `ContinuationLimits`，纪律照抄 `AsyncExecLimits`（零值 = 关，显式开） |
| `SessionRecord.Execution`（`contract/protocol/state.go`） | **扩展三字段** | `StoppedByUser` / `StoppedAt` / `Continuations`；`migrateSessionArchive`（`internal/host/session_workspace_ports.go`）同步 1:1 映射 |
| `Continuation` 字段（`recordResumeHistory`） | **不动** | 职责仍是冷加载兜底的历史补丁（§2.2） |
| `IsContinuableStatus` / `continuationTaskExecutionState` | **不动** | 它们管 checkpoint 继承，不管"要不要起回合" |
| `kernel/sessionpolicy` / `kernel/rolesessions` | **不动** | 前者是纯判据包（加进去就是第二份 idle 判据），后者是纯转发门面 |
| goal 审计（`kernel/goal/audit.go`） | **扩展一个 kind** | `EventContinue`，非终态，不进收口账本 |
| `PromoteLocked`（队列提升） | **不动** | 语义是"用户已发的消息一条不丢"，与"系统替我发一句"严格区分 |

### 2.6 施工清单（文件级）

1. `contract/protocol/state.go`：`SessionExecutionRecord` 加 `StoppedByUser` / `StoppedAt` / `Continuations []ContinuationAttempt`
   （新类型 `ContinuationAttempt{At, GoalID, RequestID, Reason string; Attempt int; Truncated bool}`）。
2. `internal/host/session_workspace_ports.go`：`migrateSessionArchive` 同步映射新字段。
3. `kernel/limit/limits.go`：`ContinuationLimits` + `WithDefaults` + 校验 + 注释写清"零值 = 关"。
4. `kernel/goal/controller.go`：`EventContinue`；`kernel/goal/audit.go` 无需改形状（`Kind` 已是 `EventKind`）。
5. `application/goal/`：给 `Application` 加一个薄读面 `HasActiveGoal(sessionID) bool`（包 `StatusFor` + `IsTerminal`），
   供 application 侧装配注入。
6. `application/execution/completion.go`：`TriggerGoalContinuations` + `CompletionDeps` 扩
   `{HasActiveGoal, ContinuationDue, ContinuationPrompt, RecordAttempt}` 四个窄函数。
7. `application/execution/table/consumer.go` / `runtime.go`：`ContinuationEvents` 信号源与开关透传。
8. `application/conversation/turn_runner.go`（+ `TurnRunnerDeps`）：`Commit` 之后 emit 续跑信号。
9. `application/conversation/turn_process.go` `BeginLocked`：清 `StoppedByUser`。
10. `application/assembly/service_assembler_scheduler_lifecycle.go` `CancelChat:`：写 `StoppedByUser`。
11. `application/assembly/service_assembler_capabilities.go`：装配上面四个窄函数。
12. `docs/arch/README.md`：索引加一行。

### 2.7 验收（红→绿）

- 单测 `application/execution`：同一中断 requestID 只触发一次；触顶后 `Truncated`；退避期内不扫；开关关 = 一次都不扫。
- 单测 `application/conversation`：`TurnRunner.Run` 每次收尾只 emit 一个信号；`BeginLocked` 清 stop 标记。
- 单测 `kernel/limit`：`ContinuationLimits` 零值 = 关（照 `TestAsyncExecZeroValueStaysOff` 的写法）。
- 集成 `application/assembly/integration`（新增文件 `goal_continuation_trigger_test.go`）：
  1. 用户 `CancelChat` 后**不**出现自动续跑，且重启后仍不续；
  2. provider 失败 → 会话空闲 → 续一轮，transcript 里能看到那条 user 行；
  3. 第 `MaxAttemptsPerGoal+1` 次被截断，审计里有 `goal.continue` 且 `truncated`；
  4. `goal_done` 之后一次都不续；
  5. 重启后同一 `requestID` 不重复续（`Execution.Continuations` 读回）。
- 命令：
  ```
  go test ./application/execution ./application/conversation ./kernel/limit ./kernel/goal -count=1
  go test ./application/assembly/... -run 'Continuation|Goal' -count=1
  ```

---

### 2.8 暂停 / 继续（goal 横条右侧的 ⏸ 与 ⟳）

> 本节补的是 §2 没覆盖的一条产品语义：**自动续跑要能被用户按停、也要能被用户按醒**。
> 对标形态：goal 还没收口、上一轮被硬中断时，goal 区顶部出现一条 stalled 横条，
> 右侧两个按钮 —— ⏸（暂停自动续跑）、⟳（清暂停并立即续一次）。

#### 2.8.1 读面与落盘位

- 读面 `execution.ContinuationStatus`（`application/execution/continuation.go`）：
  `{GoalID, GoalTitle, RequestID, Stalled, Paused, Truncated, Attempts, MaxAttempts, NextAttemptAt}`。
  `Stalled = 未收口 && 被硬中断 && 没被停止 && 没被暂停`；`Truncated = 已达上限或该中断点已有止步痕`。
  横条文案只读这一份，不自己算。
- 落盘位（跨重启）：`contract/protocol/state.go` `SessionExecutionRecord` 新增
  `Paused` / `PausedAt`（与 §2.3 防呆 1 的 `StoppedByUser` / `StoppedAt` 并列、互不复用）。
  **向后兼容**：旧档缺这四个字段 = 零值 = "没停过、没暂停过"，语义正确，不需要升
  `SessionRecord.Version`；`internal/host/session_workspace_ports.go` 的 `migrateSessionArchive`
  因此无需改形状（旧 v1 档映射过来就是零值）。

#### 2.8.2 暂停语义（⏸）——必须写死的三条

1. **只停"轮结束之后的自动续跑"，当前这一轮照常跑完。** 暂停路径（`ContinuationApplication.Pause`）
   只写一个 bool + 一条 `goal.pause` 打点，**它手里没有任何取消句柄**，也没有通向
   `CancelFunc` 的代码路径。用例 `TestPauseKeepsRunningTurnAndStopsNextContinuation`
   同时证两半：暂停期间回合 ctx 未被取消、回合以 `completed` 收尾（第一半）；回合结束后
   被中断、扫描一次都不发（第二半）。只证明第二半等于把"中断"当"暂停"。
2. **暂停 ≠ 用户停止。** 暂停不写 `StoppedByUser`：前者是"别替我继续"，后者是"这一轮别跑了"
   （`CancelChat`，终态 `TurnInterrupted` + C3 一票否决，§1.5/§2.3）。两者落点不同、恢复方式
   不同（⟳ vs 用户自己发下一条），合并会把"用户的两种意图"压成一种。
3. **暂停期间不挂欠账。** `TriggerGoalContinuations` 在暂停时返回 `pending=0`：不占上限、
   不写续跑记录、不排定时器。理由：暂停是持续状态，重复扫描没有意义（不是"这次没发出去"）。

**为什么不做成"暂停 = 中断当前轮"**：那会造出第二套中断实现，并且绕开 `CancelChat` 已经
写死的"清空队列并整批提升"语义（§1.5）。暂停只删掉一个自动行为，不动用户已经发出去的东西。

#### 2.8.3 继续语义（⟳）——清位、留痕、只发一次

`ContinuationApplication.Resume(sessionID)` 依次做三件事，缺一不可：

1. **清暂停位**（`SetPaused(false)`）。
2. **计数清零必须留痕**：写一条 `ContinuationAttempt{Manual:true, Attempt:0}` 记录 + 一条
   `goal.resume`（`manual=true`）打点。清零不另设字段——读面按"最后一条 Manual 且 Attempt=0
   的记录之后重新数"，所以"重置"这条动作有且只有一种表示，且必然可追溯。
3. **立即续一次**：不等退避、不受剩余额度限制（刚清零），但**同一中断点只发一次**：
   发出去就落一条 `Attempt ≥ 1` 的记录，紧接着的定时扫描（以及重启后的新实例）读到
   `Continued[key]` 直接短路。用例 `TestResumeFiresExactlyOnceAndDoesNotStack` 用
   "重新观测同一中断点 + 定时扫描 + 新实例扫描"三路验证**不双发**。

人工恢复**不绕过事实**：goal 已收口 / 上一轮不是硬中断 / 被用户停止 / 会话忙 → ⟳ 只清位 + 打点，
不发续跑（`ContinuationApplication.evaluate(..., manual=true)` 只跳过"上限 + 退避"两问）。

触顶（§2.3 防呆 2）之后 ⟳ 仍然有用：**截断记录不参与幂等判据**（它没发出任何续跑），
所以人工恢复能把同一个中断点再试一次；而止步痕对同一中断点只记一条，不刷屏
（`TestResumeResetsAttemptCounterAfterTruncation`）。

#### 2.8.4 审计词表（扩三个非终态 kind）

`kernel/goal/controller.go` 新增 `EventContinue = "goal.continue"`、`EventPause = "goal.pause"`、
`EventResume = "goal.resume"`。三者都是**非终态**：`isTerminalAuditKind` 只认
`finish`/`abort`（`kernel/goal/board_archive.go`），所以它们不会污染收口账本与看板 history，
只作为 append-only 的可核对痕迹（用例 `TestContinuationAuditKindsAreNotTerminal`）。
打点内容用结构化 `execution.ContinuationAudit{Kind,GoalID,RequestID,Reason,Attempt,Truncated,Paused,Manual,Detail}`，
"被截断"与"人工恢复"分别由 `Truncated` / `Manual` 位标注——不靠文案猜。

#### 2.8.5 施工清单（本节，已落地）

| 文件 | 改动 |
|---|---|
| `contract/protocol/state.go` | `SessionExecutionRecord` 加 `Paused`/`PausedAt`/`Continuations`；新类型 `ContinuationAttempt`（含三种记录的派生口径注释） |
| `kernel/limit/continuation.go`（新） | `ContinuationLimits{Enabled,MaxAttemptsPerGoal,BackoffSec,MaxBackoffSec}` + `Attempts()/Backoff()/MaxBackoff()/Validate()`；`kernel/limit/limits.go` 挂到 `Limits.Continuation`（**零值 = 关**） |
| `kernel/goal/controller.go` | 三个非终态 kind |
| `application/execution/continuation.go`（新） | 续跑 gate（`TriggerGoalContinuations` / `Pause` / `Resume` / `Status`）；判据 C1–C4 与防呆四条集中在一处 |
| `application/execution/table/consumer.go` | `AsyncConsumerDeps` 加第三个信号源 `ContinuationEvents <-chan string` + `TriggerContinuation`；欠账复用既有 `RetryDelay` 定时器（**不新增定时器**） |

#### 2.8.6 本节验收（用例与运行输出见 `application/execution/continuation_test.go`）

| 判据 | 用例 |
|---|---|
| ① 未收口 + 中断 → 续一次 | `TestContinuationFiresOnceForInterruptedOpenGoal` |
| ① 已 goal_done → 不续 | `TestContinuationSkipsClosedGoal` |
| ① 用户显式停止 → 不续（重启后仍不续） | `TestContinuationSkipsUserStoppedTurn` |
| ① 防呆：`TurnBlocked`/`TurnNeedsUserDecision` 不续 | `TestContinuationSkipsWaitingForUserOrExternal` |
| ② 暂停：本轮跑完 + 下一轮不续 | `TestPauseKeepsRunningTurnAndStopsNextContinuation` |
| ③ 继续：清位 + 立即一次 + 不叠加 | `TestResumeFiresExactlyOnceAndDoesNotStack` |
| ④ 上限截断 + 打点 | `TestContinuationTruncatesAtAttemptLimit` |
| ④ 退避 | `TestContinuationBackoffDelaysNextAttempt` |
| ④ resume 重置计数 + 人工恢复打点 | `TestResumeResetsAttemptCounterAfterTruncation` |
| ⑤ 幂等（含重启） | `TestContinuationFiresOnceForInterruptedOpenGoal`（后半）+ 三路不双发用例 |
| 开关零值 = 关 | `TestContinuationOffMeansOff`（`kernel/limit` 侧另有零值用例） |
| 忙会话挂欠账不插队 | `TestContinuationStaysPendingWhileSessionBusy` |
| "提交成功 ≠ 起了回合"不计数 | `TestContinuationDoesNotCountTurnThatNeverStarted` |

#### 2.8.7 仍未接线（下一手的活，不是本节的判据）

- **装配注入**：`ContinuationDeps` 的九个窄函数（`Facts` 从 `SessionRecord.Execution` + goal 栈读、
  `RecordAttempt`/`SetPaused` 写回存档、`Audit` 映射到 `goal.AuditAccount`、`SessionIdle` 复用
  `sessionIdleForAsyncTrigger`）尚未在 `application/assembly` 里装配；接上之前 `Limits()` 为
  `ContinuationLimits{}`（零值 = 关），**行为与今天完全一致**。
- **回合收尾发信号**：`application/conversation/turn_runner.go` 在 `Commit` 之后非阻塞 emit
  一次 `ContinuationEvents <- sessionID`（§2.5 同一处）未做。
- **GUI/桥**：goal 横条 + ⏸/⟳ 两个按钮（`gui/` 与 `application/lifecycle` 的暂停/继续入口）未做；
  `Paused` 的清除点（`turn_process.go` `BeginLocked`）也未接线。
- **出厂档**：`config/seelex.yaml` 的 `limits.continuation` 尚未显式打开（与 §2.3 防呆 2 的
  "零值 = 关、出厂档显式打开"纪律一致：接线完成后再开，避免"配置说开了、其实没有消费者"）。

## 3. 排除掉的已有机制（以及为什么不能直接用）

| 机制 | 位置 | 为什么不能直接用 |
|---|---|---|
| `SessionExecutionRecord.Continuation` | `contract/protocol/state.go`；读点 `kernel/session/archive.go` `recordResumeHistory` | 是**历史补丁**不是**启动信号**：只往 engine history 塞一条带前缀的 user 消息，没有任何起回合的能力；挪用后冷加载会退化成无摘要 |
| `IsContinuableStatus` | `kernel/task/task_execution.go` | 判的是"新回合能否继承上一回合现场"，且把 `TurnBlocked` / `TurnNeedsUserDecision` 也算可续 —— 这两个语义恰恰是"等人"，当续跑判据就是自问自答 |
| `AsyncCompletionTriggers` | `kernel/sessionpolicy/async.go` | 词表绑定 `dto.AsyncState`（作业句柄终态）；续跑没有句柄，硬套要伪造作业记录 |
| `PromoteLocked`（队列提升） | `application/conversation/turn_process.go` | 语义是"用户已发的消息一条不丢"（`service_input.go` `CancelChat` 注释写死）；把系统输入塞进用户队列会污染"用户说过什么"这个事实 |
| 定时任务面 `SchedulePort` | `kernel/rolesessions/roles.go`（纯转发）→ `application/admin/scheduler/` | 是"用户预定的时刻插话"，有独立 `ScheduledTaskSpec` 与全局表；把自动行为写进用户日程表无法解释，且完全不含 goal 语义 |
| `ResumeInterruptedSubagents` | `application/execution/subagent.go` → `backend/engine/assemble/runtime_subagent_resume.go` | 作用域是 fork 出的 subagent 单元，不是会话回合 |
| `MainRecovery` / `RecoveryInterrupted` | `application/conversation/unified.go` | 只做**读侧标注**（告诉前端"这份会话是被中断的"），不产生任何回合 |
| `RecoverPendingTail` | `kernel/sessionpolicy/pendingtail.go` | 修的是"草稿尾没发布"的写入点断裂，属于持久化完整性，与续跑无关 |
| `AsyncConsumer` 现有两个 trigger | `application/execution/table/consumer.go` | 只能挂作业表；直接复用它们等于给续跑伪造作业行（会污染 `bg_manage` 的作业视图） |
| goal TL 评审环 | 已删除（`kernel/goal/README.md`，2026-10-17 用户裁决） | 不存在了，不能靠"评审驱动再跑一轮" |
| `kernel/rolesessions` | `kernel/rolesessions/roles.go` | 纯端口转发门面（自己 README 写明"不调度任务"），没有可挂的策略点 |

---

## 4. 已知未解决（施工时须拍板）

1. **退避的首次值——已拍板**：首次 0（立即续一次），**第 2 次起**才按
   `BackoffSec × 2^(n-2)` 拉开间隔、封顶 `MaxBackoffSec`。落点是
   `kernel/limit/continuation.go` 的 `ContinuationLimits.Backoff(attemptsDone)`（attemptsDone<=0 → 0），
   用例 `TestContinuationAttemptsAndBackoffClamp`。理由：用户刚被中断，先给一次"接着干"，
   失败后才谈退避；一上来就等 30s 反而像卡住。
2. **后台会话的续跑**：`sessionIdleForAsyncTrigger` 对后台（非视图）会话同样返回 true，
   而 `SubmitConversation` 走 `SubmitFor` 明确支持后台会话。本设计**允许**后台续跑（`async` 触发就是这么做的），
   但这会让"我没看着的会话在花钱"。开关粒度是否要收到"仅视图会话"，须与产品确认。
3. **`SessionRecord.Version`**：`internal/host/session_workspace_ports.go` 的读侧只认 `Version == 2 || == 3`。
   新增 `Execution` 字段是**向后兼容的追加**（旧档缺字段读成零值），不需要升版本；但若把 `Continuations`
   当判据使用，必须接受"旧档 = 没续过"，这一点要写进实现注释。
