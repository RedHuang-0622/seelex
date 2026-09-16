# 2026-09-16 ADVISOR 裁决可见性：把「下一次用户回合才回放」改成「产出它的那一回合就回放」

> 日期: 2026-09-16 | 范围: `application/core/{chat.go,goal_service.go,goal_coordinator.go,
> goal_team_recorder.go,goal/a2a.go,goal/techleader.go,view_state/coordinator.go}`、
> `application/model/state.go`、探针 `gui/team_work_computer_use_live_probe_test.go`、
> 用例；本 devlog。**不改**受信注入（引擎历史）的时机与语义。

## 一、被指出来的事（P1-3 的实测缺口）

`docs/2026-09-16-next-session-prompt/README.md` P1-3 的自述是"角色回合 / computer
use 的真实 API 冒烟仍是下一件事"。上一会话把它跑起来了，并且拿到了**失败**：

```text
$env:SMOKE_TEAM_WORK_COMPUTER_LIVE=1
go test ./gui -run TestRealAPITeamWorkComputerUseLiveProbe -v -count=1 -timeout 20m
→ FAIL (911.47s)
[tl] 回合数=1；输入里 screen 证据=true media 引用=true 标题命中=true
等待 ADVISOR 裁决超时（15m0s）
```

即：EXEC 截图 → 画面证据进 ADVISOR 输入这条链**全绿**，但探针要的"ADVISOR 裁决行"
在可见聊天里**永远等不到**——15 分钟轮询（`_tmp/teamwork-diag.log` 里 90+ 次
`[poll]`，`conv.tl_directive` 恒为空）都没出现过。这不是模型慢，是结构上不可能出现。

## 二、真因（已核实，读码即可复现）

b→a 指令有两条出口，**触发点错开了一整个用户回合**：

| 出口 | 代码 | 触发点 |
|---|---|---|
| 受信注入（引擎历史里的 `〔[TL 指令 corr-N] …〕` user 行） | `injectGoalDirectivesForStart` → `DrainDirectives` | **下一次** `Submit` 时（`application/core/chat.go:130`） |
| 可见回放（`assistant` + `role_name=tl` 行） | `injectGoalDirectivesFor` → `TakeInjected` | **下一次**回合尾（`application/core/chat.go:236`） |

而治理回合（ADVISOR）跑在**本回合尾**（`goalAdvanceAfterChat` →
`goalCoordinator.AdvanceAfterChat`）。它产出的裁决只进 `TechLeaderMailbox` 待领取队列：
本回合尾的 `injectGoalDirectivesFor` 只回放"本回合**已注入**的指令"，看不见它；
`TakeInjected` 对它是空的。于是：

```text
回合 N 尾：ADVISOR 产出裁决 → 留在队列（无人回放）
回合 N+1 开始（用户再提交）：DrainDirectives 注入引擎
回合 N+1 尾：TakeInjected 才把它回放进可见聊天     ← 探针在这里才可能看到
```

用户提交完盯着面板，裁决不会来；探针等的就是这一行，所以它 15 分钟也等不到。

## 三、修法：把"回放"前移到产出它的那一回合（注入不动）

`application/core/goal_service.go` 新增两步，`chat.go` 收尾按序调用：

```go
service.goalAdvanceAfterChat(ctx)          // ADVISOR 回合（产出裁决）
service.injectGoalDirectivesFor(sessionID) // 回合内已注入受信区的指令 → 可见行（原有）
service.publishPendingGoalDirectivesFor(sessionID) // 刚产出的裁决 → 立刻可见（新增）
```

- `publishPendingGoalDirectivesFor` 用**非消费**的读面（新增
  `TechLeaderMailbox.PeekDirectives` → `goalCoordinator.PeekDirectives`）：
  裁决进可见聊天的同时**仍留在队列里**，`DrainDirectives` 依旧是受信注入的唯一
  消费者——注入时机（下一次 ChatStream 前）与语义**都不变**（选 B 的理由：把注入
  也提前会让它落在回合尾 `ReleaseWorkingHistoryFor` 要清掉的工作历史上，静默失效）。
- corr 幂等：新增 `goalCoordinator.DirectivePublished/MarkDirectivePublished`
  （每会话一张 `corr` 台账），`publishAdvisorDirectiveRows` 统一去重——否则下一次
  回合的常规回放会把同一裁决**再写一行**。
- 行形状与原有回放完全一致（`assistant` + `role_name=tl` + 正文
  `[TL 指令 corr-N] …`），额外补上机器可读类别 `Kind=tl_directive`
  （`MessageOrigin.Kind` → `model.Message.Kind`；字符串常量
  `goaldomain.DirectiveRowKind` 与 role draft 行同源，避免两处字面量漂移）。
  轨迹区/探针因此按**类别**取行，不靠正文措辞猜。

## 四、验证（红 → 绿 + 全量）

新增回归 `application/core/goal_directive_visible_immediately_test.go`：
两轮真实 `Submit`（第二轮的回合尾跑 ADVISOR），**不做第三次提交**就断言裁决行已可见，
并再断言"下一次回合的常规回放不重复写行"。

去掉 `chat.go` 里的新调用后复现红：

```text
--- FAIL: TestAdvisorVerdictVisibleInProducingTurn
    裁决应在产出它的那一回合就可见（且只有一行）：得到 0 行 = []
```

恢复后：

```text
--- PASS: TestAdvisorVerdictVisibleInProducingTurn (0.08s)
```

另加邮箱单测 `goal/techleader_test.go:TestMailboxPeekDoesNotConsume`（Peek 不消费、
返回副本）。全量：

```text
go build ./... / go vet ./... / gofmt -l .           → exit 0（无输出）
go test ./application/... -count=1 -timeout=180s     → 全 ok（core 36.6s）
go test ./gui/ -count=1 -timeout=600s                → ok 4.1s
go test ./sessionstore/... ./internal/... ./session/... ./seelebridge/... → 全 ok
node --test gui/frontend/dist/*.test.mjs             → pass 308 / fail 0
```

## 五、探针同步（gui/team_work_computer_use_live_probe_test.go）

证据三改了**取数来源**，不再有 15 分钟等待（那是结构上等不到，等于假阴性兜底）：

- 可见裁决行：主会话可见聊天里 `kind=tl_directive` **且** `role_name=tl`
  （`teamWorkAdvisorVerdict`）——`WaitIdle(#2)` 返回时它必须已经在（回放发生在
  `runChat` 收尾、`Chat.Running=false` 之前）；
- 裁决 kind：从 ADVISOR 回合**原文**解析（`role.snapshot(tl)` 的 tl 输出 = 裁决
  JSON，`teamWorkDirectiveKind`）——可见行是 `[TL 指令 corr-N] 正文` 形式，kind
  不在正文里，这样取不依赖措辞；
- 与 `goal.gov_snapshot` 的一致性断言（终态→`active=false`；非终态→在线且
  `LastDirective` 非空）保持不变；
- 删除不再需要的 `teamWorkWaitAdvisorDirective` / `teamWorkTLDirective`。

## 六、边界（本轮**没有**动的事）

- **受信注入的时机/语义**：仍在"下一次 ChatStream 前"注入；未把引擎历史注入
  提前到回合尾（见 §三）。
- **可见行的正文内容**：仍是 `[TL 指令 corr-N] <ADVISOR 正文>`（人读形式，与注入
  EXEC 的文本同源）。ADVISOR 的**原始 JSON**（role draft 的 `tl_directive` 行）
  照旧由 sequencer 写进主文档，本轮未改其可见投影路径。
- **`application/core` 之外**：sessionstore/角色 draft/sequencer 一行未动。
- 真机真实 API 探针需要 provider 与桌面（本机 opt-in 跑），其跑动结论见本目录同批
  记录；未跑时不得据此声称 P1-3 已验收。
