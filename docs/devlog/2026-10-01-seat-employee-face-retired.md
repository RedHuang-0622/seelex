# 座位制的员工执行面退场（2026-10-01 · M4 的 #1/#6 两条）

> **口径**：本文记 **M4 清场**里两条候选的退场——`docs/arch/teamwork-leader-worker-architecture.md`
> §9 的 **#1**（`goal_coordinator.go` 的 `roleTurnSeat` / `newRoleTurnSeat`）与 **#6**
> （`application/core/role_turn.go` + `contract.RoleTurnPort` 适配层）。两条都带**引用事实**、
> **退场条件**与**回归证据**。

---

## 1. 为什么现在可以退场

§9 的清单把这两条标成 `blocked`，条件是「worker 执行体完全接管『员工干活』那条路之后」。
条件现已成立：

- **M2**（`edcc552`）把 goal 座位循环降级成 `jobs.KindSeat` 执行体——治理环不再是"与 jobs
  并列的第二套驱动"；
- **M1/M2** 让员工干活走 leader 派发的 worker 作业（`team_dispatch` → `jobs.KindWorker` →
  `Runtime.RunWorker` → `runRoleRound`）。座位表里剩下的只有 `main`（EXEC 让位座）与
  `techlead`（ADVISOR 评审座）。

也就是说：**座位不再承载"员工回合"**，而 `roleTurnSeat` 的全部语义就是"一次员工回合"。

## 2. 退场清单（逐条）

| # | 退场对象 | 原引用事实 | 退场后 |
|---|---|---|---|
| 1 | `application/core/goal_coordinator.go` 的 `roleTurnSeat` / `newRoleTurnSeat` / `roleTurnNote` / `withRoleTurnInput` / `roleTurnInputFromContext` / `RoleTurnRequest` / `RoleTurnOutcome` / `RoleTurnRunner` / `goalCoordinatorDeps.RoleTurnFor` / `seatPlan.Runner` | `seatPlan.seats()` 的 `dto.RoleKindAgent` 分支 | 该分支删除：`agent`/其余 kind **不占治理座位**（`seatPlan.seats` 只派生 EXEC/ADVISOR） |
| 2 | `RoleSeat.RoleSessionID` / `ToolsPolicy` / `PermissionGroups` | `agentteam_runtime.go:roleSeatOf` 逐字段拷贝给座位请求 | `RoleSeat` 只留 `RoleName` / `RoleKind`（座位只回答"谁在链上、什么 kind"） |
| 3 | `application/core/role_turn.go`（`contractRoleTurnRunner` / `roleTurnRunnerFor`） | `service_assembler.go` 注入 `RoleTurnFor` | 整文件删除 |
| 4 | `contract.RoleTurnPort` + `Dependencies.RoleTurn` | `main.go` 传 `RoleTurn: runtime` | 契约与装配点删除 |
| 5 | `dto.RoleTurnRequest` / `dto.RoleTurnOutcome` | 上面两者的跨层 DTO | 删除 |
| 6 | `seelebridge.Runtime.RunRoleTurn` | 唯一的实现方 | 删除；`runRoleRound` **保留**——worker 作业（`RunWorker`）与 ADVISOR 评审（`goalLLMEvaluator`）仍共用它 |

`seatPlan.SessionID` 也随之删除（只服务座位请求的角色会话）。

## 3. 边界（不假装的地方）

- **`runRoleRound` 不动**：它是"一轮角色回合"的执行原语（开角色会话即分配权限 → 绑项目根 →
  按构造带主体 → 跑一轮有界回合），两个消费者都还在。退场的是**它的跨层端口与座位适配层**，
  不是执行面本身。
- **`roleTurnInput` 随之退场**：它只被 `RunRoleTurn` 用来包 `<round_input>`。worker 有自己的
  `workerRoundInput`。员工回合同口径的正文装配留在用例里（`runtime_role_turn_test.go` 的
  `employeeSpec` / `employeeRoundInput`），写成测试助手而不是生产代码。
- **`RolesWithExecutor` 不动**：它是**发言环**（`application/core/agentteam` 的 ring）的事实
  表，回答"环里谁有执行者"。环本身仍 live（逃生记账），退场条件（team plan 成为唯一顺序事实）
  尚未成立。

## 4. 回归证据

```
go build ./...                       # exit 0
go vet ./application/... ./seelebridge/... .   # exit 0（含测试编译）
go test ./application/core/... -count=1
go test ./seelebridge/ -run "Fork|Subagent|RoleTurn|RoleRound" -count=1
```

用例侧的**规格变更**（不是删覆盖）：

- `goal_seats_test.go`：`TestSeatPlanGivesExecutionSeatToAgentRoles` → 改为
  `TestSeatPlanGivesNoSeatToAgentRoles`（agent 角色**一座都不占**）；
  `TestTeamRoleSeatsFromRegistryView` 不再断言"座位来源必须带角色会话号"；
- `goal_role_turn_test.go` / `role_turn_test.go`（跨层适配层）删除——被测对象已不存在；
- `runtime_role_turn_test.go` 直接钉 `runRoleRound`（生产路径本身，不是替身）：会话复用 /
  权限落地 / 工具面收窄 / 回合闸门不可重入 / 释放重建 / 会话粒度，判据一条没少。
