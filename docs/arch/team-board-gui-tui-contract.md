# 团队看板接 GUI + TUI 同源同步 —— 契约

状态：draft（leader 撰写，contract_review 只读评审后定稿）
日期：2026-10-02
目标看板：`g-2`

---

## 1. 目标与范围

把已经存在但**没接线**的团队看板渲染件（`gui/frontend/dist/team-board-view.js`）接进活体 GUI，
并让 TUI 用**同一份后端投影**在团队面板里呈现同一块看板；同时把「评审过程」从 GUI 目标面板里去掉。

**非目标**（本轮不做）：

- 不做第二套渲染口径（不复制色值、不重写一份拓扑排序 / 阶段状态折算）；
- 不改 sessionstore 的落盘格式、不改 jobs 契约、不改 Seele 的 `jobs.Manager`；
- 前端不回写后端（看板是**单向只读投影**，渲染结果不上传）；
- 不新建任何「收口待确认」界面，不把评审过程搬到交互确认弹窗（用户裁决：只去掉，不迁移）。

---

## 2. 数据形状

新增文件 `application/contract/dto/teamwork_board.go`（只放数据形状，不带行为、不 import 存储包）。

```go
type TeamworkBoardView struct {
    TeamID     string                  `json:"team_id,omitempty"`
    Version    int                     `json:"version,omitempty"`
    MaxMembers int                     `json:"max_members,omitempty"` // TeamworkBackend.MaxTeammates；0 = 不限制
    Stale      bool                    `json:"stale,omitempty"`
    Stages     []TeamworkStageView     `json:"stages,omitempty"`
    Members    []TeamworkMemberView    `json:"members,omitempty"`
    Milestones []TeamworkMilestoneView `json:"milestones,omitempty"`
    Jobs       []TeamworkJobView       `json:"jobs,omitempty"`
    Events     []TeamworkEventView     `json:"events,omitempty"`
}

type TeamworkStageView struct {
    ID        string   `json:"id"`
    Roles     []string `json:"roles,omitempty"`
    DependsOn []string `json:"depends_on,omitempty"`
}

type TeamworkMemberView struct {
    Role          string `json:"role"`
    RoleSessionID string `json:"role_session_id,omitempty"`
    Worktree      string `json:"worktree,omitempty"`
    ToolsPolicy   string `json:"tools_policy,omitempty"`
}

type TeamworkMilestoneView struct {
    ID      string   `json:"id"`
    After   []string `json:"after,omitempty"`
    Status  string   `json:"status,omitempty"`   // 空 = pending（与 sessionstore 同口径）
    Content string   `json:"content,omitempty"`
}

type TeamworkJobView struct {
    Handle   string               `json:"handle"`
    State    string               `json:"state,omitempty"`      // running|done|failed|killed
    ExitCode int                  `json:"exit_code,omitempty"`
    Bytes    int64                `json:"bytes,omitempty"`
    Stage    string               `json:"stage,omitempty"`      // 权威归属（桥给）
    Node     string               `json:"node,omitempty"`
    Role     string               `json:"role,omitempty"`       // 权威归属（桥给）
    Scope    TeamworkJobScopeView `json:"scope,omitempty"`
}

type TeamworkJobScopeView struct {
    Subject string `json:"subject,omitempty"` // emp_<role>
}

type TeamworkEventView struct {
    At        int64  `json:"at,omitempty"` // **unix 秒**（GUI 侧转 ISO，见 §5）
    Kind      string `json:"kind,omitempty"`
    Stage     string `json:"stage,omitempty"`
    Role      string `json:"role,omitempty"`
    Handle    string `json:"handle,omitempty"`
    Milestone string `json:"milestone,omitempty"`
    Detail    string `json:"detail,omitempty"`
}
```

字段口径（不许省）：

- `Stages[].DependsOn` 是**顺序的唯一事实**（`sessionstore.TeamworkPlan.Stages[].DependsOn`
  同名搬运）；渲染件的拓扑+层号只从它算，不从派发姿势猜。
- `State` / `ClosedAt` / `ClosedReason` 是整队收口的三字段。**收口之后看板整块退场**
  （2026-10-03 口径修正）：`TeamworkBoardSnapshot` 对 `state=closed` 的计划返回 `nil`，
  与"没有计划"同解——看板是在册编排的只读投影，收口之后没有在册编排可看。三字段因此
  只服务**存档载荷**（封板那一版与下发同形）；活体路径的消费方不会看到非空 `State`。
  存档读侧本来就是同一口径（只恢复 `state=active`），两端不再自相矛盾。
- `Jobs[].Stage` / `Jobs[].Role` 是**权威归属**，由桥给出；渲染件里那条
  `job.stage → job.node → job.scope.subject` 的回落链只是过渡口径，接线后 stage 一定命中。
- `Jobs[].State` 的字面量与 `jobs.State` 同源；**开放取值**——只有 `done` 与
  `failed|killed` 是终态事实，认不出的状态不许读成「还没派发」。
- `Events[].At` 用 unix 秒（`time.Time.Unix()`）。它**不进模型上下文**，只上面板。
- `nil` 与「无阶段」等价：**没有计划 = 整块退场**，不留空壳（口径同目标看板）。

---

## 3. 投影路径

```
sessionstore（moduleTeamwork）
  TeamworkPlan   ← plan.json（head，整份替换）
  TeamworkEvent  ← teamwork/events.jsonl（只追加）
        │
        │  ① 桥侧组装：seelebridge.Runtime.TeamworkBoardSnapshot(sessionID)
        │          application/contract/dto/teamwork_board.go 的 DTO
        ▼
application/contract/ports.go   TeamworkBoardProjection（**窄可选**能力面）
        │
        │  ② 会话投影收集：view_state.Coordinator.CollectRuntimeProjectionFor
        ▼
application/model/state.go      SessionRuntime.TeamworkBoard  json:"teamwork_board"
        │
        ├──► GUI   app.js renderTeam(snapshot)  ← snapshot.runtime.teamwork_board
        └──► TUI   goalteam.go teamPanelLines() ← snapshot.Runtime.TeamworkBoard
```

逐段锚点：

| 段 | 文件:符号 |
|---|---|
| 计划 / 审计读面 | `sessionstore/teamwork.go` `TeamworkRepository.ReadTeamworkPlan/ReadTeamworkEvents` |
| 桥的存储端口 | `seelebridge/teamwork/teamwork.go` `PlanStore`；`seelebridge/runtime_teamwork.go` `TeamworkBackend{Store,KeyFor}` |
| 作业行 | `jobs.Manager.Snapshot(jobs.Scope{Session: sessionID})`（内存态，`seelebridge/runtime_teamwork.go` 的 `r.teamworkJobs`） |
| 组装 | `seelebridge/runtime_teamwork_board.go` `(*Runtime).TeamworkBoardSnapshot`（**已落地**） |
| 端口 | `application/contract/ports.go` `TeamworkBoardProjection`（**已落地**；窄可选接口，位置与 `RuntimePort` 同文件） |
| 收集 | `application/core/view_state/coordinator.go` `Deps.Teamwork` + `CollectRuntimeProjectionFor`（**已落地**） |
| 转发 | `application/core/teamwork_service.go` `(*Service).TeamworkBoardViewFor`（**已落地**：Deps.Teamwork 的接口名是「会话 → 视图」，由这一层类型断言到 Runtime 的 `TeamworkBoardSnapshot`） |
| 装配 | `application/core/service_assembler.go` 里 `view_state.NewCoordinator(Deps{...})` 的 `Teamwork: service`（**已落地**） |
| 会话槽 | `application/model/state.go` `SessionRuntime` / `RuntimeState` 的 `TeamworkBoard` + `CloneRuntimeState` → `CloneTeamworkBoardView` 深拷贝（**已落地**） |
| GUI | `gui/frontend/dist/app.js` `renderTeam`（消费件）；`team-board-view.js` `renderTeamBoard`（渲染件，**不改**） |
| TUI | `tui/goalteam.go` `teamPanelLines`（新增一节） |

**端口为什么是「窄可选接口 + 类型断言」**：`contract.RuntimePort` 是「每个后端都必须给出」的能力面，
给主接口加一个成员会逼所有 fake/harness 长出空方法。团队看板只在装配了 teamwork 的宿主上有意义，
所以按仓库既有口径（`context_runtime.CompactionIndexPort`）用可选接口探测：**有就有、没有就是没装配**。

---

## 4. 采集性能口径（硬要求）

会话快照是**高频**采集（回合边界 + 前端轮询）。因此：

- **禁止**每次采集都做 O(events) 的文件读；
- 采纳方案：**按会话作用域缓存「计划 + 审计」这两份文件读**，
  在 `team_*` 里**会改计划或审计**的四件（plan / dispatch / milestone / retire）**成功返回后**失效缓存
  （`team_join` 是只读汇合，不改这两份文件，因此不失效）；
- 失效采用**整表丢弃**：缓存只省文件读，重建成本是"一次计划读 + 一次审计读"；按 key 精确失效
  要在 4 个 handler 里各传一遍作用域，漏一处就是**静默陈旧**（面板永远显示旧计划）。宁可多读一次。
- **作业行不进缓存**：`jobs.Manager.Snapshot` 是内存读，随取随新，缓存它只会制造陈旧；
- 审计投影**有界**：只取最近 `teamworkBoardEventLimit = 32` 条。

可测验收量级（实测，非估计）：

- `TestTeamworkBoardSnapshotCachesFileReads`：用一个计数版 `PlanStore` 打桩，采集 100 次快照 →
  **计划读 = 1、审计读 = 1**；`team_*` 之后下一次采集恰好 +1。
- `BenchmarkTeamworkBoardSnapshot`（32 条审计 + 2 阶段 + 1 里程碑）：
  **2879 ns/op · 4056 B/op · 10 allocs/op**；benchmark 自身在收尾断言稳态文件读 = 1
  （i5-1155G7 / go1.25.8 / windows-amd64，数据为一次性测量，仅作量级）。

---

## 5. GUI 挂点

- **落点**：`gui/frontend/dist/index.html` 的 `#goal-section` **之后**新增
  `<section id="team-board-section" class="hidden">`，内含 `#team-board-badge` 与 `#team-board-view`。
  注意：`team-section` / `team-view` / `team-count` 已被「状态」页的 Agent Team 块占用，
  **不得复用**这几个 id。
- **渲染**：`app.js` 新增 `renderTeam(snapshot)`，在快照刷新路径里与 `renderGoal(snapshot)` 相邻调用；
  内部只做 **DTO → 渲染件入参**的搬运：

  | DTO | 渲染件入参 |
  |---|---|
  | `teamwork_board.stages/members/milestones` | `plan.{stages,members,milestones}`（字段名同名，直接透传） |
  | `teamwork_board.jobs[]` | `jobs[]`（`{handle,state,exit_code,bytes,stage,node,role,scope}`） |
  | `teamwork_board.events[]` | `events[]`，`at` 由 unix 秒转 **本地时间**的 `YYYY-MM-DDTHH:MM` |
  | `teamwork_board.max_members` | `maxMembers`（缺失/0 = 不限制，渲染件只写「在编 n」） |
  | `teamwork_board.stale` | `stale`（句柄投影过期标记） |

  **不转 `toISOString()`**：那是 UTC，面板上会显示成差 8 小时的时间。渲染件 `formatEventTime`
  的正则 `^\d{4}-\d{2}-\d{2}T(\d{2}:\d{2})` 对本地时间戳同样命中，取到的就是"用户看到几点"。
  `at` 缺失（`omitempty` + 0）时转出 `""`，渲染件写 `—`——**不得**让 `new Date(NaN).toISOString()`
  抛错污染整块面板。
- **id 必须同时登记进 app.js 的 `elements` 白名单**（`app.js` 的 `elements` 表）：只写
  `index.html` 不登记，`document.getElementById` 拿到 null、`renderTeam` 静默返回，
  且 `element-registry.test.mjs` 会红。徽标口径：`#team-board-badge` 写**阶段数**
  （与同栏工作表格 / 定时任务的计数徽标同口径），不写 "TEAM"——渲染件自己的看板头里
  已经有一个 TEAM 标记。
- **样式**：`team-board-view.js` 导出 `TEAM_BOARD_CSS`。app.js 在首次渲染时把它注入一个
  `<style id="team-board-styles">`（**唯一来源**，不往 `styles.css` 里抄第二份——抄一份就是两处色值漂移）。
- **退场语义**：`teamwork_board` 缺失 / 无 `stages` / **计划已收口**（`state=closed`）/
  `renderTeamBoard` 返回 `""` → `#team-board-section` 加 `hidden`、`#team-board-view` 清空。
  **不留空壳**（"结束就是没有了"，与目标看板同口径）。

---

## 6. TUI 挂点

`tui/goalteam.go` `teamPanelLines()` 末尾追加「团队看板」一节，数据源 = `model.snapshot.Runtime.TeamworkBoard`
（**同源投影**，不另走 `TeamConfigFor` 那类异步取值）：

- 头部一行：`team_id · v<version> · 阶段 n · 在编 n[/max] · 作业 跑/完/败`；
- 阶段行：按计划声明顺序，逐行 `id 角色 deps:…`，紧随其下是该阶段的作业行（`job.stage` 归属）；
- 里程碑行：`id status · 内容摘要`；
- 退场语义：`nil` / 无阶段 / **计划已收口** → **不追加任何行**（不留空壳，口径同 GUI）；
- 仍受 `clampLines(lines, panelLineLimit)` 约束（超长折叠成一行提示）。

**两处数据源的分工（同一面板里不许出现两份名册）**：面板上半段的「TEAM + 成员表」来自
`AgentTeamView` 的**异步**取值（发言调度面：AgentTeam 装配），看板一节来自 `TeamworkBoard`
（leader 的**硬编排**面：计划 + 作业 + 审计）。两者本来就是不同的集合，因此看板一节**只报
在编计数、不逐行列成员**——同屏列两遍"在编"，只会让人以为其中一份是错的。TUI 的看板节
不做拓扑排序与状态折算（那是渲染件纯函数的职责），只按声明顺序陈述投影里已有的事实。

---

## 7. 不变式

1. **单向只读**：整条链路上没有任何前端 → 后端的写入口；渲染结果不回写。
2. **无计划 = 退场**：`nil` / 无阶段 / 计划已收口 → GUI 与 TUI 两端都不渲染空壳。
3. **顺序唯一事实**：`stages[].depends_on`；看板的排序/层号是它的**派生**，不是第二份事实。
4. **句柄只是投影**（jobs I-4）：句柄活在内存，进程重启后计划里残留的 `state.jobs` 一律视为过期；
   `Stale=true` 就是这条的显式化（计划里有句柄、句柄表里查不到），渲染件据此打「句柄投影可能过期」chip。

---

## 8. 验收矩阵

| # | 命令 | 期望 |
|---|---|---|
| 1 | `go build ./...` | 通过 |
| 2 | `go vet ./application/... ./seelebridge/... ./tui/...` | 无告警 |
| 3 | `go test ./application/model/... ./application/core/view_state/... ./seelebridge/...` | 全绿 |
| 4 | `go test ./tui/...` | 全绿（含新增团队看板面板用例） |
| 5 | `go test -bench=TeamworkBoard -benchmem ./seelebridge/...` | 有量级输出；采集 100 次文件读 = 1 |
| 6 | `node --test gui/frontend/dist/*.test.mjs`（仓库规范写法；全仓无 package.json） | 全绿（含 `team-board-view` / `goal-*` / `snapshot-shape` / `element-registry`） |
| 7 | 接线守卫用例 | `go test -run TestEmbeddedTeamBoardWiring ./gui/`：`app.js` import 渲染件 + 调 `renderTeamBoard`；`index.html` 有 `#team-board-section`/`#team-board-view`；`snapshot-shape.js` 的 `SESSION_RUNTIME_KEYS` 含 `teamwork_board`；`styles.css` 不抄第二份看板样式；缺一即红 |

---

## 9. 追加需求：去掉「评审过程」

- 现象：评审过程由 `gui/frontend/dist/goal-stack-view.js` 的 `renderGoalSteps` 渲染，
  被 `goal-board-view.js` 的 `renderGoalGovernance` 放进「目标」面板治理块——
  只要 goal 栈上有 active 帧就一直显示，即「挂在 goal 是否存活下面」。
- 裁决（用户）：**去掉**。不迁移到交互确认弹窗、不新建收口待确认块。
- 口径：
  - `renderGoalGovernance` 不再渲染评审过程；
  - 「进行中正文」（`in_flight`）**保留**（它是另一件事，用户只点名了评审过程）；
  - 后端投影 `GoalGovernanceView.RoundSteps` **保留**（本轮不动后端契约），只是前端不再消费；
  - **孤儿处置（已定）**：`renderGoalSteps` / `renderGoalStep` 与 `STEP_TEXT_LIMIT` 一并删除
    （不保留空壳 export）；`goal-stack-view.test.mjs` 里对应的 3 条用例换成 1 条**反向守卫**
    （断言模块不再导出 `renderGoalSteps`），防止评审过程悄悄长回目标面板。
- 与团队看板同批实现、同批验收（都属于「看板」这一族）。

---

## 10. 风险与未决

- **只读权责跑不了命令**：本仓库的 `ToolPolicyReadonly` = 不写文件、**也不执行命令**。
  因此「契约评审」可以用只读角色（只读文档 + 读代码核对）；**验证阶段必须给 readwrite 权责**，
  否则 `go test` / `node --test` 根本起不来——验收分工必须按这条排。
- 未决：`Stale` 的判据只在「计划里有句柄、句柄表里查不到」时置真；进程重启后第一次采集会置真，
  一次新派发即恢复。这是**有意**的近似（跨进程无法分辨「已回收」与「本进程没派发过」）。
