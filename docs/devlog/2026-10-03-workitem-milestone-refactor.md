# Work Item 里程碑重构（2026-10-03）：Milestone 是屏障，Work Item 是调度单位

## 0. 一句话

把 teamwork 从「阶段（stages）派活」推进到「**里程碑甘特 + Work Item**」：
**Milestone 之间串行**（屏障），**里程碑内部按工作项依赖 DAG 并行**；
**一个 Work Item 一个 Session + 一个 git worktree**，生命周期到 `team_close` 为止。

## 1. 为什么（问题不是"少了个字段"）

改前的编排只有"阶段"：`stages[].roles` 回答**哪个角色先上**，回答不了：

- 这一步**具体做什么**（名称 / 描述 / 达成目标）；
- 做完的**判据**是什么（谁验收、验收什么）；
- 同一个角色在同一个阶段里的**多件事**怎么各干各的（上下文与改动会互相污染）；
- V 模型右腿要的"exec 做完 test 立刻跟上"这条**里程碑内依赖**。

再加上 subagent 那三条老问题（自由度过高、前缀无法复用、对用户是黑盒、长程上下文焦虑），
结论是：**工作必须是一个有名字、有目标、有归属、有依赖、有隔离的实体**——那就是 Work Item。

## 2. 数据形状

```jsonc
{
  "team_id": "v-model", "version": 1,
  "members": [ { "role": "exec", "role_session_id": "s-v-model-exec", "tools_policy": "readwrite" } ],
  "milestones": [
    { "id": "m-build", "name": "构建", "status": "active", "depends_on": [] },
    { "id": "m-ship",  "name": "发布", "status": "pending", "depends_on": ["m-build"] }
  ],
  "work_items": [                       // 扁平下发；每条自带 milestone
    { "id": "wi-impl", "milestone": "m-build", "role": "exec", "name": "实现",
      "description": "…", "goal": "跑通 + 有回归",
      "depends_on": ["wi-req"], "status": "running",
      "session_id": "s-v-model-exec-wi-wi-impl", "worktree": "seelex/exec-wi-impl",
      "handle": "a7", "note": "跑完待验收", "live": true }
  ]
}
```

- **顺序的唯一事实**分两层：`milestones[].depends_on`（屏障）与 `work_items[].depends_on`（里程碑内 DAG）。
  **跨里程碑的 item 依赖被显式拒绝**——那会绕过"里程碑是屏障"这条口径。
- **`stages` 退居历史口径**：计划里仍可带（旧计划照读、旧看板照画），新计划不必写。
  两块数据面并存是**迁移期**的事实，不是两套事实（渲染件"有计划给哪块就画哪块"）。
- 工作项状态：`pending | running | review | done | failed`（空 = pending）。
  `review` 是"跑完待验收"——它把"做完"与"验收通过"分开，正是尾插与 leader 评估之间的那一段。

## 3. 一 Work Item 一个 Session + 一个 worktree

- 会话号 `WorkItemSessionID(derive, main, team, role, itemID)` = `<role_session>-wi-<itemID>`；
- worktree 指派名 `WorkItemWorktreeName(role, itemID)` = `seelex/<role>-<itemID>`，
  与 worktree 管理器的节点 id **同一套命名**（去掉 `seelex/` 前缀就是 nodeID），
  两处各算一次命名迟早算成两个目录。
- 绑定关系落**追加型 JSONL** `session/<sid>/teamwork/worktrees.jsonl`（KV 语义：按 work_item 取最后一行）。
  释放是**再记一行**（`released:true`）而不是删行——"发生过什么"比"现在是什么"更值得留。

生命周期：

| 时刻 | 会话 | worktree |
|---|---|---|
| 派发 | 建（首次）/**复用**（中断恢复） | 建（`BindWorkspace`） |
| 跑完（尾插） | 留着 | **合并**（`MergeWorkspace`），失败保留现场 |
| 验收通过 | **清内容**（下一件事重新开） | **释放** |
| 整队 `team_close` | 清 | 释放 |

**中断恢复不清会话**（额度中断 / 进程重启之后要接着干，记忆必须建在）：重派同一个工作项
时 `DispatchItem` 复用原会话号——角色会话是进程内执行面，会话号是它取历史的钥匙。

## 4. 尾插（自动）

`workerExecutor.Start` 在 `RunWorker` 返回后调用 `ItemSettler.SettleWorkItem`（执行体 → 协调器）：

1. **先合并**这件事的 worktree（顺序反了就会出现"leader 已经看到结论、而主干上还没有这份改动"）；
2. 把**有界一行**插进 teammate 的**消息队列**：
   - 正常：`[wi-impl] 实现：跑完，改动已合并回主工作区，等待 leader 评估`；
   - 合并失败：`… 改动**插入失败**（worktree 合并失败：…）；请 leader 亲自执行合并，现场保留在 …`；
   - 回合失败：bug 原文直接进这一行。
3. 状态 → `review`（正常）/ `failed`（有错误或合并失败）。

消息队列 = **审计流里 kind=message 的行**（按 `role_session_id` 读），不另开一份存储：
两份存储只会让"看板看到的"与"审计记的"漂移。它落在 teammate 自己的队列上，
**不 push 进忙会话**（铁律不变）。

## 5. 分里程碑排活（不是一次把全程铺好）

- `team_work(milestone, items)` 只给**依赖已 done** 的里程碑排活；
- `team_dispatch(item=…)` 同样受屏障与 item 依赖两道闸门约束；
- 里程碑状态是**算出来的**：依赖 done + 它下面全部工作项 done → `done`；
  依赖 done 但还没干完 → `active`；否则 `pending`。
  （没有工作项、也没有 `depends_on` 的旧式里程碑状态归 `team_milestone` 管，不被覆盖。）

于是"上一个里程碑还有人在干、下一个里程碑的工作已经在跑"这件事在**编排面上不可能发生**
（不是靠 leader 自觉）。

## 6. 调整 / 验收 / 收口

- `team_item`：只改**未开始**（`pending`）的工作项。已开始（running/review）与已结束（done/failed）
  是**既定事实**，改它们等于改历史。
- `team_accept`：验收通过 → `done` + 结束这件事的执行隔离 + **打开下游依赖闸门**。
- `team_fail`：判失败 → `failed`，现场与记忆都留着，可重派。
- `team_recover`：中断恢复的读面（哪些可重派、哪些绑定还活着）。
- `team_close`：唯一收口点——逐在编成员四步 + **所有活绑定一并结束** + 封板 + 标 closed。
  leader 可以**中途提前** `team_close`；收口不改写已产生的工作项结论。

## 7. 落地清单

| 层 | 文件 | 内容 |
|---|---|---|
| 存储 | `sessionstore/teamwork_items.go`（新） | Work Item 类型 + 里程碑图/item 图校验（含成环、跨里程碑依赖拒绝）+ 绑定账本 JSONL |
| 存储 | `sessionstore/teamwork.go` | `TeamworkMilestone` 加 `name/depends_on/items`；审计事件加 `work_item`/`role_session_id`；`message`/`item`/`accept`/`fail`/`settle`/`recover` 六类事件 |
| 编排 | `seelebridge/teamwork/items.go`（新） | `PlanMilestone`/`AdjustItem`/`DispatchItem`/`SettleWorkItem`/`AcceptItem`/`FailItem`/`Items`/`Recover` + 屏障与依赖闸门 |
| 编排 | `seelebridge/teamwork/teamwork.go` | 端口 `Workspaces`（建/并/释放）与 `TeammateQueue`（尾插落点）；`WorkerRequest` 加 `work_item_id`/`milestone` |
| 编排 | `seelebridge/teamwork/coordinator.go` | `SetPlan` 保留同名里程碑下已排好的工作项；`Retire` 拒绝"名下还有没落定的活"；`Close` 一并结束活绑定 |
| 编排 | `seelebridge/teamwork/executor.go` | `WorkerExecutor(runner, settler, maxTurns)`：回合结束**自动尾插** |
| 接线 | `seelebridge/runtime_teamwork_items.go`（新） | Runtime 实现 `Workspaces`/`TeammateQueue`/`ItemSettler`（git 与项目根只在这里接） |
| 接线 | `seelebridge/worktree/worktree_manager.go` | `BeginNamed`/`WorktreeForNode`（非子代理节点也能建/取现场） |
| 工具 | `seelebridge/runtime_teamwork.go` | `team_work`/`team_item`/`team_accept`/`team_fail`/`team_recover`/`team_items`；`team_dispatch` 支持 `item` |
| 工具 | `seelebridge/tools/permission_policy.go` | 六个新工具进 ctl 组（员工/子代理一律断位） |
| 投影 | `application/contract/dto/teamwork_board.go` + `seelebridge/runtime_teamwork_board*.go` | `work_items` 扁平投影 + 成员 `status`/`queue`/`messages` + 里程碑 `depends_on`；判据从"有阶段"扩成"有阶段或里程碑" |
| 前端 | `gui/frontend/dist/team-board-view.js`（+ test） | 里程碑甘特（含里程碑内依赖）+ teammate 队列 + **执行进度子页面**（只保留上面的条目，不要下面的表格） |
| TUI | `tui/goalteam.go` | 看板同一份投影的终端读法（工作项、队列） |
| 提示词 | `plugins/default/teamwork/SKILL.md` | leader 提示词改到 Work Item 口径 |

## 8. 验证

```text
go build ./...
go test ./sessionstore/... -count=1
go test ./seelebridge/teamwork/... -count=1
go test ./seelebridge/ ./seelebridge/tools/ ./tui/... -count=1
go test ./... -count=1
node --test gui/frontend/dist/team-board-view.test.mjs
```

新增回归（按用户提的九类）：

| # | 要求 | 用例 |
|---|---|---|
| 1 | teammate 越权有测试 | `seelebridge/tools/permission_teammate_test.go`：`TestTeammateInGrantRunsDirectlyAndOutOfGrantGoesToTheEscalationPage`（位内直通 / 位外提权 / 格子优先于档位 / 能力面断位）、`…StaysClosedUnderFullTier`、`…EscalationDeniedKeepsTheCallDenied` |
| 2 | worktree 与 Session 生命周期 | `TestWorktreeAndSessionSurviveUntilTheItemSettles`、`TestAcceptReleasesOnlyThatItemIsolation`（并入多 Session 用例）、`TestTeamCloseEndsEveryLiveBinding`、`TestFailedWorkItemKeepsItsWorkspace`（并入合并失败用例） |
| 3 | 后台执行 | `TestDispatchItemReturnsBeforeTheRoundFinishes` |
| 4 | 分里程碑排活 | `TestPlanMilestoneRefusesMilestoneWhoseDependencyIsNotDone`、`TestNextMilestoneOpensOnlyAfterEveryItemIsDone`、`TestDispatchRefusesItemInMilestoneBehindTheBarrier` |
| 5 | 里程碑内依赖 | `TestDispatchRefusesItemWhoseDependencyIsNotDone`、`TestDependencyChainReleasesStepByStep` |
| 6 | 一人一事多 Session | `TestTeammateRunsMultipleItemsEachWithItsOwnSessionAndWorktree` |
| 7 | 每 Session 进度详情子页面 | `gui/frontend/dist/team-board-view.test.mjs`：`renderWorkItemSessionPanel …只有上面的条目，没有下面的表格` |
| 8 | 未开始的工作可调整 | `TestAdjustItemRefusesStartedAndFinishedWork` |
| 9 | 中断恢复（额度 / 重启）+ 记忆 | `TestRecoverReportsInterruptedItemsAndKeepsTheirMemory`、`TestSettlePrintsTheRunErrorIntoTheTeammateMessage` |

## 9. 未决 / 残差

- **`stages` 尚未撤场**（M4 纪律：先建新面、后撤旧面）。新计划不必写；撤它要连带改
  看板渲染件、e2e 文档门禁与旧用例，属于独立一步。
- **合并门**沿用 `worktree_manager.Finish` 的语义：**有未提交改动且没有任何提交**时显式报错，
  现场保留 → 尾插正文写"请 leader 亲自执行"。这是有意的（产出是人的资产，框架不替人决定丢还是留），
  但意味着 teammate 必须**自己提交**其改动，否则每轮都会落到这条残边上。
- **per-item 会话号**进角色会话投影需要 `AgentTeamRoleSnapshot(role, roleSessionID)` 支持
  `-wi-<itemID>` 后缀的会话；前端"子页面"先复用同一个弹窗打开（入口已接），
  真正把 per-item 会话的**实时工具活动**也接进来是下一步。
- 前端只有渲染件与委托入口改造；`index.html` / `styles.css` 的静态节未动
  （看板样式走 `TEAM_BOARD_CSS` 注入，与既有口径一致）。
