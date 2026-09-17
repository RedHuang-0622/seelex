# 2026-09-17 `@` 召唤即干活：带附言的召唤落 goal，目标收口后团队离场

> 范围：`application/core/{input_team.go,goal_service.go,agentteam_service.go}`、
> `application/core/agentteam/{factory.go,dismiss_test.go}`、
> `sessionstore/team_registry.go`、`internal/adapters/agentteam_ports.go`、
> 用例 `application/core/{input_team_work_test.go,goal_team_wiring_test.go}`。
> 结论口径同步：`docs/gui/decisions.md` ADR-GUI-021、`docs/gui/CHANGELOG.md`、
> `application/core/agentteam/README.md` 接线现状表。

## 一、症状（用户实测）

在会话里输入

```text
@goal-a2a 你看看怎么做一个演示的视频出来
```

回执正常（"已召唤团队 goal-a2a：3 个席位在编 / 成员 user · main · tl / 发言顺序
user→main→tl"），但 **teammate（tl）从头到尾没有开始工作**。

## 二、根因（落盘证据，不是猜测）

同一条会话的落盘事实：

| 事实 | 证据 |
|---|---|
| 团队**装配成功** | `<session>/team/roles.json`：`team_id=goal-a2a`、`order_policy=goal_loop`、roles=`main/user/tl` |
| tl 角色会话**零消息** | `goal_a384d51d11c6689e/.../session-a384d51d11c6689e/metadata/message.json` = `last_seq: 0, total_rows: 0` |
| tl 角色会话只有入伙事件 | 同目录 `event/event_1_1.jsonl`：唯一一行 `kind:"role_session"`（`join_seq_id=0`） |
| 会话里**没有 goal** | 会话树内没有任何 goal 记录 |

代码口径对得上：

- `application/core/input_team.go` 的 `submitTeam` 过去只做两件事：装配 + 把附言当
  一条输入下发；
- `application/core/goal_coordinator.go` 的 `AdvanceAfterChat` 在
  `runtime.ctl.Status().Active == nil` 时**直接返回**——治理循环（Governor 的
  `exec-a` + `advisor-b` 座位）根本不推进；
- `application/core/agentteam/README.md`「接线现状」表：运行时轮次驱动**已接线（仅
  goal-a2a）**，`tl` 的 ADVISOR 回合由 goal 治理执行，而 `goal-a2a` 的 `tl` 还带
  `join_policy=on_goal_create` + `presence_policy=online_when_goal_active`。

**结论：`@` 只装配，不落 goal；而 teammate 的座位由 goal 治理驱动 —— 没有 active
goal，tl 永远不会产生回合。**"召唤完就有人干活"是用户的合理预期，但被装配面默认了
（`unexecutedRoles` 只覆盖"根本没有执行者"的角色，`tl` 有执行者，所以连
`DesignNotice` 都不会报）。

## 三、变更

### ① 召唤即干活：`@<团队> <附言>` 落一个 goal

`application/core/input_team.go`：

- `submitTeam` 在装配成功且 `tail != ""` 时，先 `beginGoalForSummon`（附言 =
  goal 陈述，标题由附言派生：取首行、压空白、按 rune 截断 40），再按旧口径把附言
  作为一条输入下发。
- **顺序不能颠倒**：goal 必须在主会话这一轮跑起来之前就在栈上，否则回合尾的
  `AdvanceAfterChat` 看不到 active goal，teammate 依旧不上场。运行时序因此是
  「装配 → goal 落栈 → 主会话这一轮（= EXEC 座位）→ 回合尾 Governor 让 tl 上场」。
- `beginGoalForSummon` 走 `goalCoordinator.Begin`，**不**调 `ensureGoalAgentTeam`：
  召唤已经装配了用户点名的那支团队，再补一支 `goal-a2a` 等于替用户改团队（召唤
  `review-team` 却长出一个 `tl`）。`goal_begin` 工具路径的自动装配口径不变。
- 落 goal 失败不吞掉这次召唤：附言仍按旧口径下发，回执明说失败原因。
- 附言派生标题复用 `goal_begin` 的校验口径（`title` 必填、`statement` 限长）。

### ② 干完就走人：目标收口后团队离场

新能力（此前**没有任何**团队解装配路径）：

- `agentteam.DismissPort`（**可选**面，与 `FloorPort` 同一取舍：不给既有装配桩加
  编译期义务）+ `agentteam.Factory.Dismiss`（删角色注册表 + 复位 `order_policy`/
  `order_roles`）；**角色会话子树不动**——装配幂等键是 `(team_id, role_name)`，
  再次召唤/上线复用同一棵子树，"离场"改变的是"本会话还有没有在编团队"。
- `sessionstore` 增 `removeTeamRegistry` / `RemoveTeamRegistryWorkspace`（删文件 =
  读面 `Configured=false`）。**刻意不写"空注册表"**：读面只按"文件存在与否"报
  `Configured`，写一份空表会让面板显示一支没有成员的团队。
- `internal/adapters.SessionPort.RemoveTeamRegistry`。
- `application/core.DismissAgentTeam`（收口唯一：删注册表 → 同步发言环 →
  发 `team.changed`）。
- 触发判据是**"栈里还有没有 active goal"**，不是"是谁召唤的"：召唤与 goal 自动装配
  写的是同一份团队事实，离场也只能有一条判据（否则 `@` 召唤的团队会走、
  `goal-a2a` 自动装配的不会，同一件事两种行为）。落点：`goalAdvanceAfterChat`、
  `GoalProposeFinishFor`、`GoalNextFor`、`GoalBreakFor`。

### ③ 回执与自述

`teamSummonNotice` 带附言时多一行「已落目标 `<id>`「`<title>`」：teammate 随本轮
开工，目标收口后离场。」；`teamSummonHelp` 自述同步。

## 四、口径决定（写下来免得下轮 review 重新推）

1. **不带附言仍是只装配**：`@<团队>` 没有要干的活，凭空落 goal 是替用户造目标。
2. **离场不删角色会话**：删了会破坏装配幂等（每次召唤都是新子树），也把 teammate 的
   记录（`session-f…/team/…`）当垃圾扔了；"离场"是会话内的在编关系，不是存储回收。
3. **离场判据只看 active goal**：见 ② 的理由。
4. **其它团队同理**：`@review-team <附言>`、`@research-team <附言>`、`@<库条目> <附言>`
   走的是同一条 `beginGoalForSummon`；它们的员工座位（`RoleKindAgent`）在装配了
   员工执行面时由 Governor 真跑一轮，"召唤即干活"对它们同样成立。

## 五、验证

```text
gofmt -l application sessionstore internal        → 空
go build ./...                                    → ok
go build -tags "gui,desktop,production" ./...     → ok
go vet ./application/core/... ./sessionstore/ ./internal/adapters/  → 空

go test ./application/core/agentteam/ -run Dismiss -count=1 -v
→ TestDismissClearsRegistryAndOrder / TestDismissWithoutPortFailsExplicitly /
  TestDismissRejectsEmptySession 全 PASS

go test ./application/core/ -run "SubmitTeamWithTrailingTextBeginsGoal|SubmitTeamWithoutTrailingText" -count=1 -v
→ TestSubmitTeamWithTrailingTextBeginsGoalAndTeamLeaves / TestSubmitTeamWithoutTrailingTextKeepsTeam 全 PASS

go test ./sessionstore/ ./internal/adapters/ ./application/core/... -count=1
→ 全部 ok（sessionstore 86.9s / adapters 3.7s / core 25.0s / agentteam 6.8s / goal 11.7s）
```

新增用例钉住的三件事：① 带附言 → 装配不变 + 附言仍作为一条输入下发 + 落 active goal
（陈述含附言）+ 回执写明已落目标；② 目标收口（`GoalProposeFinishFor`，TL 缺席按 B4
直连收口）→ 离场：注册表清空、顺序复位、`dismissCount=1`；③ 不带附言 → 不落 goal、
不离场。工厂侧钉住"离场清注册表 + 复位顺序且保留角色会话"，以及"宿主没有离场面时
显式报 `ErrDismissUnsupported`"（静默成功会让调用方以为人走了）。

## 六、未做 / 下一步

- **离场不归档角色会话历史**：`goal_*` 子树留在盘上（尺寸可控，下次召唤复用）。要不要
  随离场做归档/清理，属于存储回收策略，需要先定保留口径。
- **`TurnScheduler` 仍未接线**：本次没有动它——"召唤即干活"的驱动力仍是 goal 治理的
  座位循环（与 `agentteam/README.md` 接线现状表一致）。
- **ADVISOR 裁决仍可能以 `escalate_human` 收口**（缺上下文的老问题），与本次无关。
