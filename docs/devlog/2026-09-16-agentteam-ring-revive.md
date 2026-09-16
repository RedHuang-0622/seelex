# 2026-09-16 AgentTeam team work 整改：环逃生复活 + tools_policy 写入侧枚举化

> 日期: 2026-09-16 | 范围: `application/core/agentteam/{runtime,spec,README,scheduler_wiring_test,runtime_test,instantiate_role_test}.go`、
> `application/core/{agentteam_runtime,goal_coordinator,agentteam_ring_revive_test,archive_session,workspace_usecase}.go`
> | 前置：2026-09-16 全仓只读评价（AgentTeam teamwork 专项）

## 一、背景：两条被"接线现状"表如实记着、但没有被修的问题

评价复核时确认了两件事，本次各修一半的一半（见第四节边界）：

1. **P0 · 逃生路径会把 ADVISOR 永久噤声。**
   三步叠加成一个会话级静默：
   - `agentteam.Runtime.Stop` 是终态：`SyncOrder` 只改成员与顺序、不碰 `stopped`；
     `NewRuntime` 只在 `teamRuntimeStore` 槽为空时发生；`store.drop` **全仓无调用者**；
   - `goalCoordinator.AdvanceAfterChat` 每次 chat 结束先做逃生记账，见到
     `stopped=true` 就 `runtime.gov.Break(reason)` 并 return；
   - `goalCoordinator.Begin` 在新 goal 上线时只重置 `gov`，没有重置环。

   于是轮次上限（默认 24）或连续无进展（默认 3）一旦触发，该会话此后**每一个新
   goal** 都会在上线后的第一次 chat 结束被立刻断环——ADVISOR 再也不会被叫起，
   goal 停在 `active` 无人收口，治理面板恒 0 轮。

2. **P1 · `tools_policy` 是 fail-open 的登记。**
   运行时的映射（`seelebridge/tools.ClassForToolsPolicy`）只有
   `readonly → emp_ro`、`readwrite → emp_rw`，**其它一切取值（含拼写错误）→ root 全权**。
   写入侧没有任何校验，所以一个 `"read-only"` 的拼写错误在运行时等价于最高权限。
   本次整改前的既有用例本身就是这个缺口的证据：`instantiate_role_test.go` 用的正是
   `"read-only"` / `"read-write"`。

## 二、改动

### 1. 环的逃生结论只属于"当前这一轮 goal"

- `agentteam.Runtime.Reset()`（`runtime.go`）：清 `stopped/stopReason/round/noProgress`，
  **不动**顺序、成员与 user 席位口径；nil 接收者安全。语义边界写在方法注释里：
  顺序事实仍然只有 `lifecycle.order_policy/order_roles` 一份，Reset 不落盘、不写 message。
- `goalCoordinator.Begin`（`goal_coordinator.go`）：新 goal 上线（`before == nil ||
  before.ID != record.ID`）时，在原有 `runtime.gov = nil` 之后追加
  `g.teamRuntimeFor(sessionID).Reset()`。幂等 begin（同名返回既有 active）不重置——
  否则轮次上限形同虚设。

### 2. `drop` 接线到会话消失

- `Service.releaseTeamRuntime`（`agentteam_runtime.go`）：环是派生状态（顺序来自
  lifecycle、成员来自注册表），会话消失时释放，重开按落盘事实重建。
- 调用点：`DeleteSession`（会话删除后）、`ArchiveSession`（写 archived 之后）。
  此前 `drop` 无调用者，环与进程同寿——既按 sessionID 单调增长内存，又让已停止的环
  永久污染该会话后续的每一次治理循环。

### 3. `tools_policy` 写入侧枚举化

- `agentteam.ValidToolPolicy` + `NormalizeRole`（`spec.go`）：只接受
  `readonly` / `readwrite` / `full` / 空（= 继承），枚举外**显式报错**。
  `NormalizeRole` 是整队装配 / 一步入职 / 员工库共用的唯一规整入口，因此注水进不了注册表。
  改完之后 `ClassForToolsPolicy` 的 `default → root` 分支语义收窄为"继承 / 全权"，
  不再兜未知值。
- 既有用例的 fixture 随之从 `"read-only"`/`"read-write"` 换成 `dto.ToolPolicy*`
  （原值在运行时就是 root 全权，属缺口本身，不是有效取值）。

### 4. 文档与守卫对齐（去掉"虚假保证"）

- `agentteam/README.md`：`TurnScheduler` 行由「**已接线** ……运行时由 `Next()` 决定
  下一个该发言的成员」改为「**部分接线** ……生产实际消费 `Order()`/`NoteTurn()`/
  `SyncOrder()`/`Snapshot()`，**`Next()`/`Advance()` 没有生产消费者**」；结论口径段落
  重写为可复核的事实陈述（含"下一个谁发言是表头扫描的静态投影"与
  "逃生记账只属于当前这一轮 goal"）。员工权限行补上写入侧枚举校验与
  "按角色拦截没有承载体"。
- `scheduler_wiring_test.go`：该用例 2026-09-14 钉"没有任何生产调用点"，2026-09-15 被
  反转成"必须有且仅有一处"，反转后只证明"有人 new 了一个 TurnScheduler"，不证明任何
  消费者，同时禁掉了"其实没人消费 Next()"的唯一书面提示。本次改成可证伪的文档断言：
  README 必须点名 `Order()` / `NoteTurn()` 并如实声明"没有生产消费者"，且不得再出现
  "尚未接线"（环已被 `Runtime` 持有，旧措辞同样与事实矛盾）。

## 三、证据（红 → 绿）

```text
# 回归用例：新 goal 上线必须复活已逃生的环，并且 ADVISOR 必须能再次拿到回合
go test ./application/core/ -run TestNewGoalRevivesStoppedTeamRing -count=1 -v
```

- **修复前（把 `Runtime.Reset()` 临时注释掉）**：
  `FAIL … agentteam_ring_revive_test.go:69: 新 goal 上线后团队环仍是停止态（reason="round_limit"）：上一轮 goal 的逃生结论传染到了新 goal`
- **修复后**：`--- PASS: TestNewGoalRevivesStoppedTeamRing`，且 `GoalGovernanceView.Broken == false`。

其余验证（本机 Windows，全部 exit 0）：

```text
gofmt -l application/core/agentteam application/core     # 无输出
go build ./...                                            # ok
go build -tags "gui,desktop,production" ./...             # ok
go vet ./application/core/... ./sessionstore/... ./gui/... ./seelebridge/tools/...   # 无输出
go test -race ./application/core/agentteam -count=1       # ok
go test ./... -count=1                                    # 73 包 ok，0 FAIL
```

新增用例：`agentteam_ring_revive_test.go`（新 goal 复活 + 幂等 begin 不重置记账）、
`runtime_test.go`（`TestRuntimeResetRevivesEscapeState` / `TestRuntimeResetOnNilIsSafe`）、
`instantiate_role_test.go`（`TestNormalizeRoleRejectsUnknownToolsPolicy`，含
`read-only`/`readOnly`/`read_only`/`root`/`admin`/`只读` 六个拒绝样例）。

## 四、边界与未做（比改动本身更重要）

- **环逃生后 goal 仍停在 active**：本次只修"跨 goal 传染"，没有改"逃生是否该自动收口
  当前 goal"。后者是产品决定（自动关掉用户的目标 vs 交还控制权），需要单独裁决。
- **`no_progress` 的判据没动**：`NoteTurn(progressed)` 的 progressed 仍等价于"本轮有正文
  或工具"。三次中断/报错回合即可触发逃生——现在它只影响当前这一轮 goal（这是本次能修的
  部分），门限值本身是否过低需要线上统计 `TeamSchedule.StopReason` 后定夺。
- **按角色权限拦截仍无承载体**：除 `tl` 外没有任何角色会发起工具调用（ADVISOR 是一次无
  工具的 LLM 调用），所以 `PermissionGate` 的角色级拦截即使接线也没有被拦截方。本次只把
  写入侧的 fail-open 关掉。
- **`role_session_id = team_id + "-" + role_name` 不含主会话身份**：不同主会话的同名角色
  共用同一个 ID。修它要动落盘形态与既有引用，需要迁移方案，本次未动。
- **`main.go` 的角色权限 resolver 仍以"当前视图会话"为锚**：后台会话的员工查不到 → 回落
  root。要修需要 `role_session_id → 主会话` 的反向索引，属新能力。
- **双 round 记账**（`TeamSchedule.Round` 每 chat 回合 +1 vs `Governor.Round` 每两座位 +1）
  未动，前端仍会看到两个不同分子的"轮次"。
