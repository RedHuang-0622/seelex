# M4 §9 死代码清单：逐条核实（2026-10-01）

> 口径：**本文件只做「逐条确认」，不删任何东西**（对齐 `docs/arch/teamwork-leader-worker-architecture.md`
> §9 的纪律：「M4，逐条确认，不批量删」）。
> 每条给**当前引用事实**（`git grep`，只算生产调用点，排除 `_test.go`）与**结论**：
> `live`（有生产消费者，不能删）/ `test-only`（只有用例在调，删就同时改规格）/
> `blocked`（有消费者，但要等新面接管后才能退场）。
>
> 本文档是 M4 的**执行前检查单**：M0（旧异步面迁 jobs.Manager）与 M2（goal 座位循环降级为
> `KindSeat`）落地之后再逐条动刀；在此之前删任何一条都会把仓库留在「顺序事实/驱动各半」的状态。

## 逐条

| # | §9 候选 | 当前引用事实（生产调用点） | 结论 |
|---|---|---|---|
| 1 | goal 治理座位循环：`goal_coordinator.go` 的 `newGovernor` / `seatPlan.seats` / `roleTurnSeat` / `newRoleTurnSeat` | `newGovernor` ← `Next`（`goal_coordinator.go`）与 `advanceAfterChat`；`seatPlan.seats` ← `seatsFor`；`roleTurnSeat` ← `seatPlan.seats`。整条链路由 `Service.goalAdvanceAfterChat`（`goal_service.go`）在每次 chat 收尾驱动 | **blocked**：这是**当前唯一**的治理驱动。降级为 `jobs.KindSeat` 执行体（M2）之前不能动；降级后本条应逐符号收敛 |
| 2 | ADVISOR 座位：`application/core/goal/adapter.go` 的 `NewAdvisorSeat` | `adapter.go:94`（`NewTurnGovernorForDSA2A`）、`goal_coordinator.go:406`（`seatPlan.seats` 的 techlead 分支）、`goal_coordinator.go:542`（`seatsFromOrder` 退化路径） | **blocked**：同 #1，随 `seatPlan` 一起转 review worker / `KindSeat` |
| 3 | team 环与逃生：`agentteam/scheduler.go`（`TurnScheduler`）、`runtime.go`（`Runtime`） | `Runtime` 被 `teamRuntimeBySession` 持有（`agentteam_runtime.go:169`）并被 `advanceAfterChat` 用于逃生记账（`NoteTurn`/`Reset`）；但 `TurnScheduler` 的 `Next()` / `Request()` / `Remove()` / `Restore()` 在 `application/core/agentteam/*.go` 里**只有测试调用**（`runtime_test.go` / `prefix_test.go` / `scheduler_test.go`），生产只消费 `Order()` / `SetPrefix()` / `Advance()`（经 `Runtime.Next`）与 `setOrderLocked` | **已退场（2026-10-01）**：本行点名的四件（`Next` / `Request` / `Remove` / `Restore`），连同同证据的 `Move` / `Prefix` / `requests` 通道 / `RuntimeOptions.Buffer` / `TurnRequest.RoundID` / `orderLocked` / `indexOfRole` 一并删除，见 [`2026-10-01-turn-rotation-retired.md`](2026-10-01-turn-rotation-retired.md)；`scheduler_wiring_test.go` 的两条断言按事实改写，并**新增一条**「退场必须被记下来」（README 与 `scheduler.go` 都要出现"已退场"）。**本行另有一处更正**：原写生产消费 `Advance()`（经 `Runtime.Next`）——按当轮核实不成立（`Runtime.Next` 也只有用例在调），`Advance` / `Runtime.Next` 仍是「有代码、无生产消费者」，但**本笔未删**（它们是逃生路径 ③`no_executor` / ④`empty_ring` 的唯一计算点，删=删行为），留待 team plan 成为唯一顺序事实那一笔 |
| 4 | 座位派生读面：`agentteam_runtime.go` (`teamRoleSeatsFor`) | `service_assembler.go:119` 注入 `goalCoordinatorDeps.RoleSeatsFor` | **live**：goal 座位派生按 role kind 的唯一来源。退场条件 = team plan（`moduleTeamwork` 的 `stages/members`）成为**唯一**顺序事实（M4 与其后的迁移） |
| 5 | 旧顺序字段 `lifecycle.order_policy/order_roles` | 写入面：`agentteam/factory.go`（`SetLifecycleOrder`：装配 / dismiss / 默认顺序同步）、`agentteam/global.go`、`agentteam/library.go`、`agentteam/presets.go`；读面：`sessionstore/lifecycle.go:85`、`sessionstore/role_session.go:76`、`dto/agentteam.go`（多处）、`gui`（`team.set_order` 拖拽调序）、`tui/goalteam.go`、`e2e/dto_boundary_test.go` | **blocked**：既被席位环消费，也是前端拖拽调序的唯一事实。只读化 = 退掉写入面 + 读面切到计划，属**产品面决定**（会让「员工栏拖拽调序」退场），必须在 team plan 成为读面唯一来源之后 |
| 6 | `RoleTurnRunner` 适配：`application/core/role_turn.go`、`contract.RoleTurnPort` | `role_turn.go` 把 `contract.RoleTurnPort` 适配成 `RoleTurnRunner`；生产实现 = `seelebridge/runtime_role_turn.go` 的 `RunRoleTurn`；注入点 `service_assembler.go:122` | **blocked**：worker Executor（`Runtime.RunWorker`）与 goal 座位**共用**这条角色回合执行面。等 worker 执行体完全接管「员工干活」那条路之后，本适配层才可归并/改名 |

## 判据（M4 退出条件，照抄 §8）

- 仓库只剩**一套顺序事实**（team plan 的 `stages[].depends_on`）与**一套驱动**（`jobs`）；
- 无引用、无回归、`go vet` 静默、`e2e` 文档门禁绿。

## 现在能安全动的部分

- **#3 已于 2026-10-01 退场**（`TurnScheduler` 的无消费者接口，见 [`2026-10-01-turn-rotation-retired.md`](2026-10-01-turn-rotation-retired.md)）。当时的执行口径是：
  改写 `scheduler_wiring_test.go` 的三条声明与 `agentteam/README.md` 的接线现状段——
  「删代码」在这里是一次**规格变更**，不是清理。
- 其余五条都在等 M0/M2 的新面接管，属「先建新面、后撤旧面」的**后撤**一侧（进度：#1/#6 已于 2026-10-01 整条退场，#3 同日退场；#2/#4/#5 仍 `blocked`，退场条件见上表）。
