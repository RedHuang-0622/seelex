# goal 席位轮转整条退场（阶段三 W3）

- 日期：2026-10-03
- 范围：`application/core/goal_coordinator.go`、`application/core/goal_service.go`、
  `application/core/goal/{adapter,headless}.go`、`application/core/govern/*`（删除）、
  `application/core/{service_assembler,agentteam_runtime}.go`、
  `application/contract/dto/{projection,seat_job}.go`、
  `seelebridge/{runtime,runtime_teamwork}.go`、`seelebridge/teamwork/{teamwork,executor}.go`、
  `gui/headless.go`、`gui/frontend/dist/app.js`、`tui/goalteam.go`、`main.go`
- 上游权威：`docs/devlog/2026-10-01-goal-board-vmodel-brief.md`（W3）、
  `docs/arch/teamwork-leader-worker-architecture.md`（§1/§9/M4）

## 1. 口径：goal 的驱动从"座位环"换成"提示词驱动"

阶段三 §0.1 / W1 已把 `$goal` / `$teamwork` 改写成"**没有席位轮转**"的口径：goal =
leader 自总结的看板 + V 模型阶段派活，teamwork 走 `team_plan` 的 `stages[].depends_on`。
但当时**代码仍在跑座位循环**——回合尾 `AdvanceAfterChat` 会同步推一轮 Governor
（`exec-a` 让位 → `advisor-b` 真实 TL 回合），这就是用户说的"**串行到飞起**"的东西。

本次把这条驱动整条退场：goal 从此**没有任何座位概念**。

## 2. 保留 vs 退场（边界）

保留（goal 的"意图 + 完成判定 + 逃生"宿主不变）：

- `goal.Controller`（LIFO 栈 + 状态机 + 审计 + 第五栈持久化）；
- `goal.Supervisor`（EXEC 账本、ADVISOR 独立上下文、corr 信封、B4 缺席矩阵）；
- **终态 gate**：`goal_propose_finish` → `Supervisor.ProposeFinish`（TL verdict_done 收口 /
  not_done 纠偏 / escalate 转人工 / TL 缺席直连）；
- **审批预筛**：`Supervisor.PreScreenApproval`；
- **逃生**：团队环 `NoteTurn` 记账 + `AbortOnEscape` 收口（环到达轮次上限/连续无进展）；
- b→a 指令的受信注入与可见回放（`injectGoalDirectives*` / `publishAdvisorDirectiveRows`）；
- 评审过程投影（`RoundSteps` / `InFlight` / `PeerState`）——只在 gate / 预筛跑真实 TL
  回合时短暂有值。

退场（删除，非"停用"）：

| 退场项 | 原落点 |
|---|---|
| 治理循环抽象（`Governor` / `Seat` / `TurnAction` / `SnapshotOf`） | `application/core/govern/*`（整包删除） |
| goal→govern 适配（`NewAdvisorSeat` / `NewTurnGovernorForDSA2A` / `DirectiveBreaksLoop` / `funcSeat`） | `application/core/goal/adapter.go`（删除） |
| 座位派生 / 座位绑定 / 座位作业（`newGovernor` / `seatsFor` / `seatPlan.seats` / `seatsFromOrder` / `orderSeats` / `teamRoleSeat` / `RoleSeat` / `runSeatRound` / `advanceSeatViaJobs` / `SeatJobs` / `SeatJobsAssembled` / `SeatJobOutcome`） | `application/core/goal_coordinator.go` |
| goal 协调器循环入口（`Next` / `Break` / `noteRoundError` / `deps.MaxRounds` / `deps.RoleSeatsFor` / `deps.SeatJobs`） | 同上 |
| Service 循环控制（`GoalNextFor` / `GoalNext` / `GoalBreakFor` / `RunSeatRound`） | `application/core/goal_service.go` |
| 座位作业面（`KindSeat` / `SeatRequest` / `SeatRunner` / `SeatRoundRunner` / `SeatExecutor` / `SetSeatRoundRunner` / `DispatchSeat` / `JoinSeat` / `RunSeat` …） | `seelebridge/teamwork/*`、`seelebridge/runtime_teamwork.go`、`seelebridge/runtime.go`、`main.go` |
| headless 治理循环 RPC（`WithGovernor` / `goal_gov_next` / `goal_gov_snapshot` / `goal_gov_break`） | `application/core/goal/headless.go`；GUI 侧 `goal.gov_next` / `goal.gov_break` |
| 只读视图的循环字段（`Round` / `RoundLimit` / `CurrentSeat` / `Broken` / `BreakReason` / `RoundError`） | `application/contract/dto/projection.go` |
| 座位作业 DTO | `application/contract/dto/seat_job.go`（删除） |
| 前端/终端的轮次·座次·断环渲染 | `gui/frontend/dist/app.js`、`tui/goalteam.go` |

## 3. 改动要点

- `AdvanceAfterChat` 保留为**回合边界收尾**：登记 `turn_completed`（exec 账本水位 +
  本轮工作正文摘要）+ 团队环逃生记账；**不再**驱动任何座位。
- 团队环的轮次上限不再与 goal 治理同源：`agentteam_runtime.go` 用独立的
  `defaultTeamRoundLimit = 24`（`goalLoopRoundLimitForTeam` 删除）。
- 只读治理视图 `GoalGovernanceView` 退化为**看板投影**：`Stack`（活动栈逐帧）+
  `Status` + `LastDirective` + `PeerState`/`InFlight`/`RoundSteps`（gate 期间）。
- headless 的 `goal.gov_snapshot` 保留（读治理视图）；`goal.gov_next` / `goal.gov_break`
  退场——没有循环可推、可中断（现在与未知方法一样被拒）。

## 4. 测试的规格变更（连带改动，不是删覆盖）

**删除**（其"被钉住的行为"已不存在）：

- `application/core/govern/governance_test.go`（治理循环抽象本身退场）；
- `application/core/goal/adapter_test.go`（座位适配退场）；
- `application/core/goal/headless_gov_test.go`（`goal_gov_*` 退场）；
- `application/core/goal_seats_test.go`、`goal_seat_jobs_test.go`（座位派生 / 座位作业退场）；
- `application/core/goal_loop_limit_test.go`、`goal_loop_turn_order_test.go`（治理循环轮次退场）；
- `seelebridge/runtime_seat_test.go`（座位执行体退场）。

**改写**（保留回归点，换触发源）：

- `goal_coordinator_test.go`：只留会话隔离 + 视图进 SessionRuntime；座位推进相关用例删除；
- `goal_governance_steps_test.go`：改由 `sup.RunEval` 显式驱动一轮 TL 回合（gate 路径）；
- `goal_work_summary_test.go`：`AdvanceAfterChat` 只登记信号，改由 `sup.RunEval` 驱动回合；
- `goal_directive_session_lock_test.go`：指令改由直接投递队列复刻现场（回归点是锁纪律）；
- `goal_directive_visible_immediately_test.go`：来源改为显式投递 + 同一段回合尾回放；
- `agentteam_ring_revive_test.go`：保留"环逃生不传染下一轮 goal"，去掉 ADVISOR 驱动断言；
- `tui/goalteam_test.go`、`gui/headless_goal_test.go`：去掉轮次/座次/断环断言与 `gov_break` 可达断言。

## 5. 回归证据

- `go build ./...`：通过；
- `go vet ./...`：通过；
- `go test ./application/core/... ./seelebridge/... ./tui/... ./gui/ -count=1`：通过（见提交信息）。
