# Agent Team 发言环不含 user：删除「user 席位口径」

> 日期: 2026-09-22 | 范围: `application/core/agentteam/{runtime.go,runtime_test.go,README.md}`、
> `application/contract/dto/agentteam.go`、`application/core/{agentteam_runtime.go,chat.go,service_input.go,service_queue.go}`
> + 用例 `application/core/{agentteam_ring_user_projection_test.go,goal_loop_turn_order_test.go}`、
> `tui/goalteam{,_test}.go`、`gui/frontend/dist/agent-team-view.js`、
> `gui/README.md`、`gui/frontend/README.md`、`application/core/README-agentteam.md`
> 承接: [2026-09-15 agentteam 实例化与发言调度](2026-09-15-agentteam-instantiation-and-speak-schedule.md)、
> [2026-09-15-09-16 发言环复活](2026-09-16-agentteam-ring-revive.md)、
> [2026-09-17 召唤即干活](2026-09-17-summon-starts-goal.md)

## 1. 症状与用户口径

用户要的运行时次序是

```text
user（起手）→ exec → tl → exec → tl → … → 收口(over)
```

即：用户把话说出去之后，其余时间都是 agent teammate 在互动（exec 干活、tl 评审交替），
直到裁决收口。实际状态里 teammate 的「下一个」**会指向 user**：`order_roles` 是
`user → main → tl`，而发言环直接照抄了这份顺序，于是面板/巡检面把「下一个发言」标到人身上。
「下一个谁发言 = 人」这件事本身与产品口径矛盾，用户实测也确认了这一点。

### 根因（代码事实，不是猜测）

- `Runtime` 建环时把 `order_roles` 原样交给 `TurnScheduler`（`runtime.go` 旧
  `NewRuntime`/`SyncOrder` 调 `cleanOrder(order)`），user 因此是**环成员**；
- 为了掩盖"环里有人"造成的卡顿，另加了一套「user 席位口径」：`queued`（有排队输入才占位）
  / `member`（每轮固定占位）/ `absent`（不占位），由 `order_policy` 推导
  （`UserSeatPolicyFor`），并在 `skipLocked` 里按口径跳过 user；
- 队列侧因此需要反向通知环：`NoteTeamUserQueued` / `noteTeamUserSeat` 在
  `chat.go`（队列提升）、`service_input.go`（提交入队 ×3）、`service_queue.go`（队列编辑）
  四处被调用。

**结论：三态口径全都建立在同一个错误前提上——把 user 当成环里的一个排班位。**
只要这个前提成立，"下一个"就迟早会落到人身上；修口径只是把症状挪到配置层。

## 2. 变更

### ① 环成员 = `order_roles` − `user`（一次性投影，不是第二份事实）

`application/core/agentteam/runtime.go` 新增 `ringOrder(order)`：`cleanOrder` 之后去掉
`user`，`NewRuntime` / `SyncOrder` 只经它建环；`Order()` / `Snapshot().Order` 因此
天然不含 user。`order_roles` 仍然含 `user`（群聊的起手与收口，`resolveOrderRoles` 的
校验也要求它在场）——顺序事实仍只有 `lifecycle` 一份，环是它的**投影**。

用户的发言机会只剩一个：每次 react loop 收尾时**消息队列被整批提升为下一轮**
（`application/core/chat.go` 的队列提升点）。它是动作，不是座位。

### ② 删除「user 席位口径」整条链

| 删除项 | 位置 |
|---|---|
| `UserSeatPolicy` / `UserSeatQueued` / `UserSeatMember` / `UserSeatAbsent` / `UserSeatPolicyFor` / `RuntimeOptions.UserSeat` / `SetUserSeat` / `Runtime.userSeat` / `userSeatExplicit` | `application/core/agentteam/runtime.go` |
| `Runtime.NoteUserQueued` / `Runtime.userQueued` / `skipLocked` 里的 user 分支 / `Next()` 消费排队输入 | 同上 |
| `dto.TeamSchedule.UserSeat` 与 `dto.UserSeatQueued/Member/Absent` | `application/contract/dto/agentteam.go` |
| `Service.NoteTeamUserQueued` / `noteTeamUserSeat` 及其 5 处调用点 | `application/core/{agentteam_runtime.go,chat.go,service_input.go,service_queue.go}` |
| GUI「user 席位」chip、`USER_SEAT_LABEL`、`normalizeSchedule().userSeat` | `gui/frontend/dist/agent-team-view.js` |
| TUI 调度行的 `user 席位 <x>` | `tui/goalteam.go` |

`skipLocked` 现在只剩一条判据：`!RolesWithExecutor[roleName]`（无执行者的角色占位但不产生回合），
如实出现在 `Snapshot().Unexecuted` 里。

## 3. 证据

### 3.1 行为用例（新增/改写）

```text
go test ./application/core/agentteam/ -run 'Ring|Seat|Reset|Escape' -count=1 -v
→ TestRuntimeRingExcludesUser（新）：三种 order_policy 下 Order() 都是 main,tl、
  环绕次序 main,tl,main、快照 NextRole ≠ user
→ TestRuntimeMaintainsRingFromRegistryOrder / TestRuntimeRingsThroughExecutorsOnly /
  TestRuntimeResetRevivesEscapeState：期望值从 user,main,tl 收紧为 main,tl
→ TestRuntimeEscapeNoExecutor：新增「只有 user 的会话 → 空环 empty_ring」

go test ./application/core/ -run 'DropsUserFromOrderRoles|TurnsAlternateExecAdvisor' -count=1 -v
→ TestTeamScheduleOrderDropsUserFromOrderRoles（新，端到端）：真实装配路径下
  view.OrderRoles = user,main,tl 而 schedule.Order = main,tl（= 顺序 − user），NextRole ≠ user
→ TestGoalLoopTurnsAlternateExecAdvisorUntilVerdictCloses（新，端到端回合次序）
```

旧用例钉的正是相反的事实（`TestRuntimeUserSeatPolicy` 断言 `Order() == "user,main,tl"`、
`member` 口径下第一个发言者是 user），所以这次是"把断言反过来了"，不是补一条新用例。

### 3.2 冒烟：user → exec → tl → exec → tl → over

`TestGoalLoopTurnsAlternateExecAdvisorUntilVerdictCloses` 用真实 `Submit`
（队列提升）+ 真实回合尾治理推进 + 脚本化 TLEvaluator（第一轮 continue、第二轮
verdict_done）跑完整链，实测次序与环投影：

```text
起手   user     Submit(@goal-a2a 附言) —— 队列提升
第1轮  exec     主会话回合（ChatStream，goal-a2a 的 exec-a 座位）
第1轮  tl       回合尾治理推进 → ADVISOR 回合（continue）
环     members  main,tl        ← order_roles 是 user,main,tl，环去掉了 user
环     next     main           ← 永不指向 user
第2轮  exec     主会话回合（第二条 user 输入驱动；TL 指令在回合边界注入）
第2轮  tl       ADVISOR 回合（verdict_done）
over   goal     收口：active 清空、history=1
```

同时实测到一个与本次改动无关、但值得写下来的事实：**核心层没有「裁决 continue 后
自动续跑下一轮 exec」的机制**（用例里显式探测过：第一轮 tl 之后 400ms 内第二轮不会
自己起来）。每一轮 exec 都由一条 user/队列输入驱动，TL 指令挂在**下一次 ChatStream
回合边界**注入（`goal_service.go` 的 `Peek/DrainDirectives`）。所以真实会话里
`user → exec → tl` 会重复出现，直到某一轮 tl 给出收口裁决为止。

### 3.3 全量验证

```text
gofmt -l .                          → 空
go build ./...                      → ok
go vet ./...                        → 仅剩既有告警：application/core/durable_queue_wire_test.go
                                      （另一条工作流的未提交文件：SaveSessionSnapshot passes lock by value）
go test ./application/... ./tui/... ./gui/... -count=1  → 全部 ok
node --test gui/frontend/dist/*.test.mjs                → tests 425 / pass 425 / fail 0
node --check gui/frontend/dist/agent-team-view.js       → ok
```

## 4. 影响与不变量

- **顺序事实仍只有一份**：`lifecycle.order_policy/order_roles`（含 user）。环是
  `order_roles − user` 的投影，前端「工作顺序」编辑照旧既改持久事实又即时同步环。
- **逃生路径不动**：`round_limit` / `no_progress` / `no_executor` / `empty_ring` /
  `external_break` 五条判据与语义不变；"只有 user 的会话"现在会明确落到 `empty_ring`。
- **`Next()`/`Advance()` 仍无生产消费者**：`Snapshot().NextRole` 仍是环头扫描的静态
  投影（`peekNext` 的注释这次写明了这一点与"环里没有 user"），真正驱动轮次的仍是 goal
  治理的座位循环（`goal_coordinator.go`）。
- **契约收敛**：`team.schedule` 不再有 `user_seat` 字段（前端/巡检面/测试中已无引用）；
  `user` 仍会出现在 `TeamView.members` 与 `TeamView.order_roles` 里（成员栏与顺序事实）。

## 5. 未做 / 下一步

- **环的"下一个"仍是静态投影**：`Next()`/`Advance()` 没有生产消费者，"下一个发言"
  不随轮转变化（本次只保证它**永远不会是 user**）。让它真随轮转变化需要给环接上生产
  推进者（或明确废弃这条投影），属于独立决定。
- **`scripts/gen_core_readme_index.py` 的分卷自检当前会提前退出**：报
  `未归属任何分卷：durable_queue_wire_test.go`（另一条工作流的未提交文件），因此
  `application/core/README-agentteam.md` 的索引这次是**手工**删掉两个已下线函数；
  该文件归属确定后重跑生成脚本即可。
