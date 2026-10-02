# 团队收口后看板退场 + teammate 工作详情的实时钩子 + 上下文记录竖排（2026-10-03）

- 范围：`seelebridge/{runtime_teamwork_board,runtime_role_turn,runtime_teamwork,runtime_deps,runtime}.go`、
  `application/{contract/dto/role_tool_activity.go,event/hub.go,application.go,core/{service,aliases}.go}`、
  `main.go`、`gui/frontend/dist/{protocol.js,app.js,agent-team-view.js,styles.css,team-board-view.js,text-button-chrome.test.mjs}`、
  `tui/`（无需改动，见 §5）、`docs/*`
- 上游权威：[`docs/arch/team-board-gui-tui-contract.md`](../arch/team-board-gui-tui-contract.md)、
  [`docs/arch/teamwork-leader-worker-architecture.md`](../arch/teamwork-leader-worker-architecture.md)
- 现场来源：用户 2026-10-03 陈述三条（团队收口后看板不退场 / teammate 工作详情的钩子放错 /
  上下文是横向表格）。**结论口径**：修前事实都标 Confirmed（有代码锚点），改法都带红→绿用例。

## 1. 三条口径（改完之后是什么样）

| # | 现场 | 修前（Confirmed） | 修后 |
|---|---|---|---|
| ① | `team_close` 之后「团队看板」没退场，在编名册（含 `tl` 这类成员行）残留 | 看板投影对**已收口计划**照常出图：计划里 `stages` 还在，只有 `plan.State.State=closed`（`runtime_teamwork_board.go` 的活体分支只看 `planMissing`/`len(stages)==0`） | `TeamworkBoardSnapshot` 对 `state=closed` 返回 `nil`：**结束就是没有了**（口径同目标看板，也与存档读侧"只恢复 active"一致） |
| ② | teammate 工作详情没有实时钩子，形如"里程碑做完才拿到完整上下文" | 员工做工回合的工具步骤**没有出口**：ReAct 钩子只送 goal 域 TLStep sink（只有 ADVISOR 挂得上，`runtime_role_turn.go` 注释原样写着"员工回合没有 sink"）；子代理那条有 `subagent.tool.*` 逐帧推送 | 员工回合的每次工具调用 → `RoleToolActivity` → 装配根 → 会话级事件 `teammate.tool.started/completed` → 前端把它画进详情里的「正在做（实时）」一节 |
| ③ | 上下文记录是横向表格 | `renderRoleRecordTable` 输出 `excel-grid`：车道当行、回合号当列 | **一条回合一行**（行号 = `seq`，时间自上而下；末尾「草稿N」行），两栏 = main / 自身车道 |

三条的共同点：把"还在跑"与"已收口"、"此刻在做什么"与"记录里有什么"分开陈述，读侧不许
用一份投影冒充另一份。

## 2. 改动要点

### 2.1 ① 收口 ⇒ 看板退场（`seelebridge/runtime_teamwork_board.go`）

- 活体分支加一条闸：`cached.plan.State.State == sessionstore.TeamworkStateClosed` → `return nil`。
  它与"没有计划"同解，GUI（`renderTeam` 对空串加 `hidden`）与 TUI（`teamBoardLines` 对 nil
  不追加行）**本来就有**这条退场路径，因此两端不需要改动。
- 为什么不"照常出图 + 前端写已关闭"：看板是**在册编排**的只读投影。收口之后继续画一块看板，
  收口后的在编名册就会一直挂在面板上，读侧从此分不清"这支队还在跑"与"早就收口了"
  （用户现场看到的正是这个）。域内 `closed` 事实仍归计划（`plan.State`）——它管的是
  "还在不在册"，投影这一侧只回答"有没有在册编排可看"。
- 存档载荷（`buildTeamworkBoardView` → `sealClosedTeamBoard`）不变：三字段（`state` /
  `closed_at` / `closed_reason`）仍随封板那一版落盘，只是活体路径不再下发。

### 2.2 ② teammate 的实时钩子（`seelebridge/runtime_role_turn.go` 等）

一条流水线，四段：

```
角色回合 ReAct 钩子（OnToolStart/OnToolComplete）
  → reportRoleToolStart/Complete（ctx 上有 roleWorkScope 才出声）
  → Runtime.publishRoleToolActivity → roleToolObserver
  → main.go: RuntimeDeps.RoleToolCallback = app.HandleRoleToolActivity
  → 会话级事件 teammate.tool.started / teammate.tool.completed（revision=0）
  → 前端 applyTeammateToolActivity → 「正在做（实时）」
```

- **身份按轮给**：`roleRoundSpec.WorkScope` 由 `workerRoleRoundSpec`（`team_dispatch` 的
  worker 作业回合）填；`runRoleRound` 把它挂到本轮 ctx。钩子挂在**引擎**上（一个角色会话
  开一次、活得比一轮长），所以"这一轮是谁的活"只能按轮从 ctx 读——ADVISOR 评审回合不带它，
  因此评审不会被误播成员工活动（用例钉住）。
- **两帧同 ID**：`ToolCallInfo` 没有调用 ID 这一栏，ID 取 `工具名#轮次#入参指纹(crc32)`，
  started/completed 因此能对上（前端 upsert）。诚实后果写在函数注释里：同一轮里**同名同参**
  的两次调用会合并成一条。
- **有界**：入参/结果按 rune 截断到 `roleToolActivityLimit`（600），application 侧再按
  `Limits().EvidenceChars` 截一刀（与子代理同一把尺子）。
- **不进快照、`revision=0`**（同 `team.changed` 口径）：载荷不是快照事实（员工的权威记录在
  角色会话里，读面是 `AgentTeamRoleSnapshot`），带 revision 会被"快照比事件新"的陈旧判据吃掉，
  逐帧的进度就又退化成"跑完才看得到"。
- 前端 `applyTeammateToolActivity`：按 `role_session_id` 分组的**有界**缓存（40 条），
  开着的正是这一位时用"最近一次权威快照 + 新到的帧"重绘（**不跑 RPC**）；不是这一位就只记忆
  （切过去自然带出来）。

### 2.3 ③ 记录表竖排（`gui/frontend/dist/agent-team-view.js`）

- `renderRoleRecordTable`：`<table class="role-record-table">`，**行 = 回合**（`th.role-record-seq`
  是行号 `seq`），列 = main / 自身车道；草稿行接在末尾，行号写「草稿N」。
- 四类归属与判据不变（`is-main` / `is-own` / `is-shared` / `is-outside`），`data-record-*`
  统计字段一字未改——变的是**方向**，不是语义。
- 样式从 `.excel-grid` 迁到 `.role-record-*`（`styles.css`），不再吃横向网格的皮肤。

### 2.4 顺带修复：一条**前置红灯**（不是我引入的）

`gui/frontend/dist/text-button-chrome.test.mjs` 在本次改动前就是红的（上一批 S7「看板成员入口」
留下的）：它只扫 `styles.css` 找"抹掉组件库按钮皮"的规则，而团队看板成员入口的 `background`
按契约写在渲染件导出的 `TEAM_BOARD_CSS` 里（**不抄进** `styles.css`）。已用**原始**
`styles.css` 复现过（`git checkout -- styles.css` 后同一用例照样失败），因此确认是前置红灯。
修法：把扫描面扩到"渲染件导出的 `XXX_CSS` 模板串"（护栏的本意是"裸类名按钮必须有背景规则"，
不是"背景规则必须写在 styles.css 里"）。

## 3. 红 → 绿证据

```text
# ① 收口 ⇒ 退场
go test ./seelebridge/ -run TestTeamworkBoardSnapshotRetiresClosedPlan -count=1
  修前：FAIL（"已收口的计划必须让看板退场…得到 … State:closed … Members:[arch impl_ui]（在编 2 人仍残留）"）
  修后：PASS（同包 TestTeamworkBoardSnapshot* 共 11 条全绿，含存档兜底/负缓存/性能口径三条）

# ② 实时钩子（RED 用"把修好的两处探针打回修前形态"复现，见下）
go test ./seelebridge/ -run "TestRoleLoopHooks|TestWorkerRoundCarriesWorkScope|TestRoleWorkScopeIsPerRound" -count=1
  修前形态：FAIL（"一次工具调用应发 started + completed 两帧，得到 0 帧"、"
             员工做工回合必须把身份塞进本轮 ctx"）——4 条红
  修后：PASS（8 条全绿，另含"评审回合不播员工活动但 TLStep sink 照旧"）
go test ./application/core/ -run TestHandleRoleToolActivity -count=1   # PASS（2 条）

# ③ 竖排记录（用原始 agent-team-view.js 跑新用例复现）
node --test gui/frontend/dist/agent-team-view.test.mjs
  修前：FAIL（期望 role-record-seq 行号/竖排行，实际拿到 excel-grid 横向表）
  修后：PASS（42 条）

# 全量
go test ./seelebridge/... ./application/... ./tui/... ./gui/ -count=1     # 49 包全 ok
node --test gui/frontend/dist/*.test.mjs                                  # 618 pass / 0 fail
gofmt -l <本次改动的文件>                                                  # 空
go vet ./seelebridge/ ./application/...                                    # 无输出
```

## 4. 未做 / 边界（如实标注）

1. **GUI 端到端冒烟未做**：本机没有启动 GUI 点一遍（`$goal` §5 的三层证据里，第一层
   单元/集成与第三层全局冒烟是两回事）。因此"实时区在真机上逐帧长出来"只有单元 + 接线
   守卫一级的证据（`applyTeammateToolActivity` 的分组/边界/重绘条件 + 渲染件用例）。
2. **`ToolCallInfo` 缺调用 ID**：同一轮里同名同参的两次并行调用会合并成一条实时读数。
   修它要动框架的 `session.ToolCallInfo`（不在本仓），本轮不宣称。
3. **实时区是瞬态**：不落盘、不进快照、关掉面板即停止更新（重开时从事件流重新积累）。
   权威记录仍是角色会话投影（拉取面），两份不互相冒充。
4. **`team_close` 之后计划仍留在存储里**（只有投影退场）：收口原因与收口时间是可核对的事实，
   删掉它们等于把"为什么结束"也一起删了。存档仍按 `state=closed` 封板。
5. `docs/2026-09-16-team-work-record-dataflow/README.md` 等**dated 记录**保留原样（纸面轨迹），
   只有会误导"今天的实现"的那几篇加了口径修正注记。
