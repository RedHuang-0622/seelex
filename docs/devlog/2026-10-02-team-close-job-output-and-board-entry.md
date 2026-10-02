# 团队收口面（team_close / goal_done）+ 作业正文活到 close + 看板成员入口（阶段四 S4–S7）

- 日期：2026-10-02
- 范围：`seelebridge/{runtime_teamwork,runtime_teamwork_*,teamwork/*}.go`、`sessionstore/teamwork.go`、
  `application/{contract/dto,core}`、`gui/{team_board_wiring_test.go,frontend/dist/{app.js,team-board-view.js}}`、
  `main.go`、`plugins/default/{teamwork,goal}/SKILL.md`、`register_goal_tools.go`、
  `docs/arch/teamwork-leader-worker-architecture.md`
- 上游权威：`docs/arch/teamwork-leader-worker-architecture.md`（§4.4/§4.5/§4.6/§6）、
  [`team-board-gui-tui-contract.md`](../arch/team-board-gui-tui-contract.md)（§5/§8 第 7 条）

## 1. 口径（改完之后一条流水线怎么收口）

```
goal_begin → 看板 → team_plan
   → 阶段循环：team_dispatch（不等待）→ team_context（**非消费**读，正文进上下文）
              → team_milestone（leader 撰写）→ team_retire（放工作区 + 清上下文，**不动作业**）
   → 全绿 → leader 亲自复核 → team_close（**唯一回收点**：逐在编成员回收 + 封板看板）
   → goal_done（主代理即 TL：真收口，不过 gate）→ 团队离场
```

三条口径变化，都是"把两个动作拆开"：

| 之前 | 之后 |
|---|---|
| `team_retire` = 回收作业 + 放工作区 + 清上下文 | `team_retire` **不动作业**；回收统一到 `team_close`（实现仍只有一份：`retireSteps(..., reclaim bool)`） |
| 收尾取回产出 = `jobs_manage(op=fetch)`（取尽即销项） | 收尾读证据 = `team_context`（**非消费**：不推进游标、不销项）；`fetch` 留给"确实要拿走这条作业" |
| 目标收口 = `goal_propose_finish`（送 gate 提议） | 主代理用 `goal_done` **直接收口**（它在本团队里就是 TL）；`goal_propose_finish` 保留为"要 TL 评估器裁决"的那条路 |

## 2. 改动要点

### S4a `team_close`（整队收口唯一入口）

- `Coordinator.Close`：逐在编成员走同一套四步（`reclaim=true`）→ 封板团队看板存档（`closed` /
  `team.close`）→ 计划标 `closed`（域内权威：`plan.State`）→ 落一条 `close` 审计。
- **幂等**：第二次调用返回 `already_closed=true`，不重复封板、不重复审计——收口事实只有一个。
- **失败语义**：成员四步与"标 closed + 审计"是硬事实；封板失败同样上抛（域内尚未标 closed ⇒ 重试是
  一次干净的收口）。存档若不封板，重启恢复时会把"已收口"冒充成"在册"（读侧只认 `state=active`）。
- 落点：`seelebridge/teamwork/coordinator.go`、`seelebridge/runtime_teamwork.go`（`teamCloseHandler`）、
  `seelebridge/runtime_teamwork_board_close.go`、`sessionstore/teamwork.go`（`TeamworkStateClosed` /
  `TeamworkEventClose`）。

### S4b `goal_done`（main agent 的真收口）

- `goalCoordinator.FinishDirect` 直连 `Controller.Finish/Abort`，**不过终态 gate**；收口后
  `dismissTeamWhenGoalClosed` 让"干完就走人"成立（栈里没有 active goal ⇒ 在编团队离场）。
- 收口副作用只有一份实现（与提议路径汇到同一个 `Controller`）：弹栈 + History 留审计 + 看板据审计收口。
- 可见性：`goal_*` 仍在 ctl 组、`policy.go` 的 `isGoalTool` 对子代理整族不可见——员工连工具都看不到。

### S5 作业正文活到 close（`Spec.OutputPath` + 防 prune）

两条独立的路，缺一条就时真时假：

1. **输出归属**：`Coordinator.Dispatch` 经 `teamwork.JobOutputs` 端口分配**产品自有**路径
   （会话目录内 `teamwork/jobs/<role>-<n>.log`），同时写进 `jobs.Spec.OutputPath` 与
   `WorkerRequest.OutputPath`。框架侧因此**不建写句柄、只按偏移读**，且 `externalOutput.remove()`
   是空操作——销项 / 驱逐 / `Close` 都不删正文。代价是执行体必须自己写（`sink.Note` 在
   `externalOutput` 形态下是空操作）：`Runtime.RunWorker` → `JobOutputs.WriteJobOutput`（经
   **产品面**写，与收口清目录串行），失败正文也写进去。
   生命周期归产品：`team_close` 时 `ClearJobOutputs` 清掉整目录（收口 = 产品决定"不再需要"的那一刻）。
2. **防 prune**：`jobs.WithLimits(Limits{Records: teamworkJobRecordCeiling(maxTeammates)})`。
   框架缺省 256 是**进程级兜底**，与"一支团队从开工到收口能派多少次活"无关；prune 判据是"表长 >
   上限"、只逐最老的**终态**行，撞上它丢掉的恰好是"团队还开着、某一轮已经跑完"的行。产品按
   `maxTeammates × 64`（只抬不降）给上限。

**残留边界**（本轮已收口，见 §4.2/§4.3）：记录槽上限仍是上限——极端情况下某条终态行会被逐出，
此时读面（`team_context`）按"在册作业行"找不到它（显示空闲）；但**正文文件仍在产品目录里**，
读面据此按**角色名**回读（不依赖句柄），并用 `Evicted` 标出这一来源。

### S6 `team_context`（成员上下文只读面）

- `Snapshot` + `jobs.Manager.Peek`（**非消费**）；正文默认关，`include_body=true` 时逐成员默认
  4KB、硬顶 32KB，按 UTF-8 边界裁剪（切半个中文字 = 损坏，不是截断）。
- `Idle` 显式化：分得清"没派过活"与"派过、已终态"；`Closed` 让调用方分得清"收口造成的空"。
- 读面**不建 Coordinator**（那会装配执行体）：它只要 backend + 作业表 + 作用域键。

### S7 GUI：看板在编行 → 成员会话入口

- 渲染件（`gui/frontend/dist/team-board-view.js` 的 `renderTeamRoster`）：角色名成为按钮，
  带 `data-team-role-open` / `data-team-role-session`（与团队面板成员行同一对钩子）；**没有
  `role_session_id` 就不渲染按钮**——点不动的入口比没有入口更坏。
- 接线（`app.js` 的 `bindTeamBoardActions`）：委托必须绑在 **`#team-board-view`** 上。Agent Team
  面板那条挂在 `#team-view`，是另一块 section，收不到看板子树里的事件——渲染出按钮却不绑监听，
  就是一个点不动的入口。动作复用同一个 `openRoleSessionDetail`（不另造"员工会话"概念）。
- 只读不变：开的是一张读视图（`AgentTeamRoleSnapshot` + `team.changed` 重取），看板仍是单向投影。

### 文案对齐（提示词与 schema 回到与实现同口径）

改之前有四处会**让提示词驱动走偏**的旧文案（实现已改、说法没跟上）：

| 位置 | 旧 | 新 |
|---|---|---|
| `$teamwork` §2 工具表 | 只有六件套，`team_retire` 写"回收作业" | 补 `team_close` / `team_context`；`team_retire` 明写"不回收作业，正文活到 `team_close`" |
| `$teamwork` §5 收尾第 1 步 | `jobs_manage(op=fetch)` 取回产出 | `team_context`（非消费）；`fetch` 的销项语义单列 |
| `$goal` §2/§6/§7 | "末尾**必有** `review` 阶段"、工具表用 `goal_propose_finish` 收口、节奏无 `team_close` | 去掉 `review`/`tl`；补 `goal_done`；节奏 = `team_close → goal_done` |
| schema / 工具描述 | `teamworkRetireDescription` 与 `team_retire` 回执称"回收作业"；`goal_begin` 称"由 ADVISOR 回合制评审" | 同步为新口径 |

## 3. 验证

- `go build ./...`：通过。
- `go test -count=1 ./sessionstore/... ./seelebridge/... ./gui/`（`-run "Teamwork|Output|EmbeddedTeamBoardWiring"`）：
  全绿，含本轮新增用例：
  - `sessionstore`：`TestTeamworkJobOutputDirIsSessionScopedAndIdempotent`（目录落在会话内、
    幂等、空键拒绝）；
  - `seelebridge/teamwork`：`TestDispatchAssignsProductOwnedOutput`（`Record.OutputRef` 与载荷
    `OutputPath` 同值）、`TestDispatchWithoutOutputsKeepsFrameworkOwnedFile`（未装配 = 交回框架）、
    `TestRetireKeepsJobOutputsUntilClose`（退场不清、不销项；收口清一次且在退场之后）；
  - `seelebridge`：`TestTeamworkJobOutputsAllocatesDistinctPathsPerDispatch` /
    `...SanitizesRoleName` / `...ClearRemovesDirAndRetiresPaths`（清目录 + 作废落点 + 进程内不复用）/
    `...WriteRefusesForeignPath` / `...LatestJobOutputPathPicksNewest` / `...RefusesWithoutSession`；
    `TestWriteWorkerOutputRoutesThroughProductFaceAndDropsAfterClear`（写经产品面；收口后迟到写被丢弃、
    不留残文件）；`TestTeamworkContextReadsEvictedBodyFromProductFile`（行被 prune 逐出时按角色名回读
    产品自有正文并标 `Evicted`；缺省不碰文件系统）；
  - `gui`：`TestEmbeddedTeamBoardWiring` 第 ⑦ 条（看板在编行必须带成员入口钩子，且 app.js 把委托
    绑在 `#team-board-view` 上、复用到同一个 `openRoleSessionDetail`）；
  - 前端：`node --test gui/frontend/dist/team-board-view.test.mjs`（成员入口渲染 + 降级 + 转义）。

## 4. 未做 / 下一步

1. **GUI 端到端冒烟未做**：本轮改动只到"单元 + 接线守卫"一级，没有启动 GUI 点一遍成员入口
   （`$goal` §5 的 computer-use 冒烟是收口条件之一——收目标前必须补）。
2. **prune 的残留（本轮已收口）**：行被 prune 逐出后按句柄读不到，但**正文文件归产品、
   活到收口**——读面 `team_context` 因此按**角色名**回读最近一份
   （`teamwork.JobOutputs.LatestJobOutputPath` + `Runtime.readOutputHead`），并用
   `dto.TeamworkMemberContextView.Evicted` 显式标出"正文来自产品文件（行已被逐出）"，与按
   句柄的非消费读法（`Peek`）分得清来源。行本身仍归框架（框架没有 pin 概念，产品给的记录槽
   上限只是把概率压小）——但"行被逐出 ⇒ 正文丢了"这条推论已经不成立。缺省（`include_body=false`）
   不碰文件系统。落点：`seelebridge/runtime_teamwork_context.go` 的 `teamworkAttachEvictedBody`、
   `runtime_teamwork_output.go` 的 `LatestJobOutputPath`/`readOutputHead`。
3. **收口清目录 vs 被取消执行体的最后一次写（本轮已收口）**：执行体的写改经**产品面**
   （`teamwork.JobOutputs.WriteJobOutput`，唯一入口），与 `ClearJobOutputs` 落在**同一把锁**上
   串行；清目录同时**作废**这一批落点（`TeamworkJobOutputs.live` 表），且序号在收口时**不复位**
   （进程内落点不复用，堵住"旧回合的迟到写命中新团队同名文件"这条更坏的路）。于是迟到写只有两种
   归宿：要么先落盘随后被清掉，要么查不到落点被丢弃——任何一种交错都不留残文件。
   `Runtime.writeWorkerOutput` 只保留"带落点却没装配产品面"这一组装矛盾的直接落盘兜底。
   落点：`runtime_teamwork_output.go`（`WriteJobOutput`/`ClearJobOutputs`/`live`）、
   `runtime_teamwork.go`（`writeWorkerOutput` 方法 + `teamworkJobOutputs`）。
4. **`team_context` 的可达面未接 GUI**：目前是 leader（agent 侧）的读面 + 看板行的会话入口；
   "点开成员看**正文**"的 GUI 面板还没做（本轮只做入口）。
5. 旧异步面迁移（M0）与 M4 余项仍按 `docs/arch/teamwork-leader-worker-architecture.md` §11/§12 的口径
   留在原状态（本轮未动）。
