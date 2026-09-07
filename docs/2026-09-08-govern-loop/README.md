# 多代理治理循环：桌游式回合抽象（govern）与 goal 域适配

> 日期：2026-09-08 · 状态：设计 + 已落地代码（`application/core/govern`、
> `application/core/goal/adapter.go` + 测试）
> 系列：承接 `docs/2026-09-07-goal-domain-techleader/` 与
> `docs/2026-09-07-seele-a2a-framework-req/`；本文回答"多代理怎么按回合
> 共同治理一个会话，且能被显式打断"。

## 1. 为什么需要这个抽象

DS-A2A 已有完整双会话协议与状态机（EXEC 事件账本 / ADVISOR 帧上下文 /
corr 信封 / gate），但它把"谁在何时行动、何时该停"散落在 Supervisor 的
信号触发策略里。若每次接入一个新治理角色（评审、审计、审批代理）都要
复制一份"轮到自己→行动→让位→检查是否该停"的样板，治理面会越接越散。

结论：把**回合次序、循环推进、断环**沉淀为独立编排原语，goal 域作为
第一个适配实现。

## 2. 桌游隐喻 → 代码映射

| 桌游概念 | 治理语义 | govern 代码 |
|---|---|---|
| 玩家/角色 | 参与治理的代理（EXEC/TL/未来评审者） | `Seat`（Name/Kind/Act） |
| 一个回合 | 某座位行动一次（产出最小结果） | `TurnAction`（BreakLoop/Note） |
| 座次 | 每轮按注册顺序行动 | `turnGovernor.seats` |
| 主持人/发牌 | 决定轮到谁、逐座推进 | `Governor` |
| 一轮 | 所有座位各行动一次后回到首位 | `Round()` |
| 喊停/认输 | 打破治理循环（收口/升级/外部中断） | `TurnAction.BreakLoop` / `Governor.Break` |
| 轮次上限 | 防死循环护栏 | `maxRounds` |
| 牌面快照 | 审计/前端可观测读面 | `Snapshot`/`SnapshotOf` |

## 3. 接口要点

```go
type Seat interface {
    Name() string
    Kind() AgentKind
    Act(ctx) (TurnAction, error)
}

type Governor interface {
    AddSeat(seat) error
    Next(ctx) (bool, error) // false = 循环已收束
    Current() string
    Seats() []string
    Round() int
    Break(reason string)
    Broken() (bool, string)
}
```

约束：

- 抽象不携带协议语义（不 import goal/session/seelebridge）；
- 座位错误原样透传（B4 缺席矩阵由装配方决策）；
- 断环只表达"停"，不表达"为什么停"（Note 只做审计摘要）。

## 4. goal 域适配

`application/core/goal/adapter.go`：

- `DirectiveBreaksLoop(directive)`：verdict_done / escalate_human /
  approve / deny → 断环；correct / checkpoint_ok / verdict_not_done → 不断；
- `NewAdvisorSeat(supervisor)`：advisor 座位每次 Act = 一次真实 TL 回合
  （Supervisor.RunEval），并按裁决决定是否断环；
- `NewTurnGovernorForDSA2A(execName, execAct, supervisor, maxRounds)`：
  EXEC 闭包 + ADVISOR 座位组成双座治理循环。

```text
exec-a.Act（登记目标/推进进度/经 gate 提议收口）
   → advisor-b.Act（真实 TL 回合：评审 → TLDirective）
   → verdict_done/escalate → 断环
   → verdict_not_done/correct → 下一轮继续
```

## 5. 落地代码与测试

| 文件 | 内容 |
|---|---|
| `application/core/govern/governance.go` | Seat/Governor/TurnAction/Snapshot + 默认实现 |
| `application/core/govern/governance_test.go` | 座次交替、断环、护栏、外部中断、错误透传 |
| `application/core/goal/adapter.go` | DS-A2A 语义 → govern.Seat 适配 |
| `application/core/goal/adapter_test.go` | 治理循环驱动 TL 回合、verdict_done 收口断环、TL 缺席透传 |
| `tmp/goal-tl-live-smoke/smoke_test.go` | 真实 API：治理循环驱动 EXEC+TL 回合（SEELEX_LIVE_SMOKE） |

验证命令：

```text
go test ./application/core/govern/ ./application/core/goal/ -count=1
$env:SEELEX_LIVE_SMOKE='1'; go test ./tmp/goal-tl-live-smoke -v -count=1
```

## 6. 用例安排（当前覆盖）

| 用例 | 断言 |
|---|---|
| `TestTurnGovernorAlternatesSeatsByOrder` | 座次按注册顺序、一轮后 Round+1、回到首位 |
| `TestTurnGovernorBreaksWhenSeatRequests` | 任一座位 BreakLoop → 收束 + 原因记录 |
| `TestTurnGovernorMaxRoundsStopsLoop` | maxRounds 到达后 Next=false |
| `TestTurnGovernorExternalBreak` | 外部 Break 后不再推进 |
| `TestTurnGovernorSeatErrorStops` | 座位错误透传、不标主动断环 |
| `TestGovernorDrivesAdvisorRound` | 治理循环驱动真实 TL 回合（round≥2） |
| `TestGovernorBreaksOnVerdictDone` | EXEC 提议 → TL verdict_done → 收口出栈 + 断环 |
| `TestAdvisorSeatDisabledReportsTLDisabled` | 无评估器 → ErrTLDisabled 透传（B4） |
| `TestTurnGovernorRealLLM`（live） | 真实 API 下 EXEC+TL 轮替、快照可观测 |

## 7. 后续（未做，标注规划）

- 把 govern 循环接入 seelebridge/application 真实会话工具面（P0-wiring）；
- sessionstore 持久化治理快照（轮次/断环原因审计留档）；
- 前端投影治理循环状态（seat/round/broken）。
