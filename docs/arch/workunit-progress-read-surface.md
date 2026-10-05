# workunit 进度读面（后端接口形状）—— 只读勘定

> 口径：**只读**。本次除本文件外未新增/修改/移动任何代码、测试或文档；未动桌面。
> 相关：`seelebridge/workunit/README.md`（契约不变式）、
> [`workunit-duplication-inventory.md`](workunit-duplication-inventory.md)（同一件事几份实现的盘点）、
> [`subagent-visibility-design.md`](subagent-visibility-design.md)、
> [`team-board-gui-tui-contract.md`](team-board-gui-tui-contract.md)。
> 纪律：只报事实 + 归类（**本质重复 / 偶然重复 / 各层专有**），不把"长得像"当"重复"。

---

## 0. 一句话结论

两条链的**进度读数在数据形状上已经统一**（双方都落同一张 `sessionstore.NodeSessionRecord`、
都经同一个 `workunit.SessionLedger` 端口读写），但**"把记录折算成给人看的进度"这一步是两份手写**，
而且**"还在跑 / 跑到哪"的实时读数走了两条互不相干的事件线**。因此"看不懂"不是形状问题，
是**折算与投递各写一遍**的问题——公有读面应当落在"折算"这一层（纯函数 + 一个按 NodeID 定位的读法），
**不**落在"再造一条统一事件流/第二份作业模型"上。

---

## 1. 两条链读面现状对比

### 1.1 数据产生点（同一件事：跑没跑完 / 结论 / 到哪一步）

| 读数 | subagent 链（产生点） | teammate 链（产生点） |
|---|---|---|
| **阶段 / 打点**（"到哪一步"） | 引擎 telemetry `Before` 钩子按 `NodeScope` 逐次投影：`seelebridge/internal/telemetry/stage_hook.go:61` `record()` → `StageRecorder.RecordStage`；装配点 `seelebridge/node/agent_node.go:156`（spawn/注册）与 `:263`（收尾）；落点 `seelebridge/session/subagent_sessions.go:710` `RecordStage` → actor `:281` **append**（turn 递增，无上限） | 团队尾插三处**整条重写**：`seelebridge/workunit_team.go:181` `markTeamUnitRunning`（stage=`running`）、`:186` `recordTeamUnitRoundOutput`（stage=`round_output`）、`:193` `settleTeamUnitRecord`（stage=收尾 kind）；编码 `:176` `teamUnitStages` —— **恒为单元素数组**（每条 stage 覆盖前一条） |
| **是否在跑** | 会话在册 ⇒ `running`，否则 `queued`：`seelebridge/session/subagent_sessions.go:474` `buildRecordLocked`（兜底分支 `subagent_sessions.go:515` 一带写死字面量） | 写方显式给 `running`/`done`/`failed`：`seelebridge/workunit_team.go:41` 常量；判"在跑"已转调契约 `seelebridge/workunit_team.go:50` `teamUnitInFlight` → `workunit.InFlight` |
| **结论 / 错误** | `seelebridge/session/subagent_sessions.go:812` `NoteOutcome`（status/summary/errMsg）；由 `seelebridge/node/coordinator.go` `CompleteSubagentNode` 调用 | `settleTeamUnitRecord`（`workunit_team.go:193`）把 `workunit.Outcome` 折成记录终态 |
| **现场** | `NoteWorktree`（`seelebridge/session/subagent_sessions.go:803`），唯一生产调用点 `seelebridge/runtime_plan.go:151` | `teamUnitWorktreeRecord`（`workunit_team.go`，取 worktree 注册表现值；**不落"曾经在哪"**） |
| **时间戳** | 记录带 `StartedAt/EndedAt`（记录形状里有） | **刻意不写**：`workunit_team.go` 注释——"哪一刻开始/落定是计划 `item.StartedAt/FinishedAt` 的事实" |

### 1.2 中间层（持久化 + 契约端口）

| 段 | 锚点 | 说明 |
|---|---|---|
| 记录形状（唯一一份） | `sessionstore/node_session_store.go:43` `NodeSessionRecord`（`Goal/Status/Summary/Error/History/ContextJSON/StagesJSON/ResultJSON/Worktree/StartedAt/EndedAt/UpdatedAt`） | 两条链共用同一个 `subagents/` 目录、同一张表 |
| 契约持久化端口 | `seelebridge/workunit/session.go:52` `SessionLedger`（`Save/List`）+ 编译期断言 `var _ SessionLedger = (*sessionstore.NodeSessionStore)(nil)` | **不写适配器、不另立记录类型**——这是"复用"的落点 |
| subagent 写 | `seelebridge/session/subagent_sessions.go:474` `buildRecordLocked`（经 actor，`persistLocked`） | 读侧折算（`Status` 兜底）也在这一个函数里 |
| teammate 写 | `seelebridge/workunit_team.go:134` `saveTeamUnitRecord`（**直写** `NodeSessionStore`） | 与 subagent 同一张表，但写入口是第二条（见"归类"①） |
| subagent 读回 | `seelebridge/session/subagent_sessions.go:394` `restoreLocked`（解 `StagesJSON`/`ResultJSON`/`Worktree`/终态） | 读侧折算的第二份 |
| teammate 读回 | `seelebridge/workunit_team.go:412` `teamUnitRecords`（`List` + 按现场名单过滤）+ `:331` `RecoverTeamworkUnits` | 读侧折算的第三份 |
| 共用判据（已收口） | `seelebridge/workunit/session.go:60` `StatusQueued/StatusRunning` + `InFlight` | 子代理侧仍有两处字面量未转调（`runtime_subagent_resume.go:329`、`subagent_sessions.go:515`） |
| 共用恢复说明 | `seelebridge/workunit/session.go` `RecoveryNote`（生产接线：`runtime_subagent_resume.go:386`、`workunit_team.go:383`） | 已是"同一族"，不是自造说明 |
| 共用收尾分类 | `seelebridge/workunit/classify.go` `ClassifyFinish`（生产接线：`seelebridge/node/agent_node.go:191`、`seelebridge/teamwork/items.go:478`） | 三层唯一一份判据（已落地） |

### 1.3 最终读面：谁读去画「面板 / 打点表 / 会话事件」

| 读面 | subagent 链 | teammate 链 |
|---|---|---|
| **打点表**（注入上下文的请求尾部块） | `application/core/work_table_async.go:158` `asyncTraceLines`（句柄/kind/state/字节/有界摘要） | `application/core/work_table_async.go:232` `teamworkTraceLines`（**同一块**、另一行生成器：句柄/状态/归属），拼接点 `application/core/work_table.go:743`（`workTableTraceBlockFor`，`work_table.go:699`） |
| **工作表格行**（GUI 台账） | `application/contract/dto/async_run.go:20` `AsyncRunRecord`（`Kind=subagent`）→ `asyncWorkItems`/`asyncRunToWorkItem`（`work_table_async.go`）+ 子代理树行 `syncSubagentTask`（`work_table.go`） | **没有**：teammate 作业不进工作表格（只在看板 `jobs[]` 与打点块） |
| **执行树** | `application/contract/dto/subagent.go:24` `SubAgentTreeNode`（`ID/ParentID/Children/Status/Summary/Context`）；投影 `Engine.SubAgentTree()` → `view_state/coordinator.go` 的 `SubAgentTree` | 无对应物（teammate 不是树，是甘特） |
| **编排看板** | 无（子代理不进 AgentTeam 名册） | `application/contract/dto/teamwork_board.go:23` `TeamworkBoardView`（`members/milestones/work_items/jobs/events`）← `seelebridge/runtime_teamwork_board.go:102` `TeamworkBoardSnapshot` ← `TeamworkBoardProjection`（`application/contract/ports.go`） ← `view_state/coordinator.go:163` `CollectRuntimeProjectionFor` |
| **会话事件 / 实时** | `Runtime.SubscribeSubagentLive(nodeID)`（`application/contract/ports.go:77`，实现 `seelebridge/runtime_live.go:87`）→ `dto.SubagentLiveEvent`（`subagent_live.go:9`：`stage`/`tool`/`assistant` 三类增量）+ 阶段实时通道 `node.Coordinator.StageEvents()`（`seelebridge/node/coordinator.go:208`） | 会话级事件 `teammate.tool.started/completed`：`dto.RoleToolActivity`（`application/contract/dto/role_tool_activity.go`）→ `Service.HandleRoleToolActivity`（`application/core/service.go:111`）；另 `TeammateSessionProjection.TeammateSessionLive`（`seelebridge/runtime_teammate_session_live.go:35`）→ `dto.TeammateSessionLiveView` |
| **作业终态读数** | `AsyncRunRecord`（`State/ExitCode/LogBytes/Summary/Tail`） | `dto.TeamworkJobCompletionRecord`（`application/contract/dto/teamwork_jobs.go:13`）← `Runtime.TeamworkJobCompletions`（`seelebridge/runtime_teamwork_jobs.go:21`） |
| **恢复态** | `dto.SubagentRecoveryView`（`subagent_recovery.go:11`：`Active/ConclusionFound/Resumable`）← `ListSubagentRecovery` | `workunit.Resume`（`Scenes/Sessions/Interrupted`）← `RecoverTeamworkUnits` |

### 1.4 为什么 subagent 那条"好懂"（具体到字段与粒度）

1. **实时读数有专属结构、且逐帧**：`SubagentLiveEvent`（`subagent_live.go:9`）一帧一个事件，
   `kind` 三分（`stage`/`tool`/`assistant`），字段带 `Stage.Turn` / `Stage.TokenEstimate` /
   `Stage.Preview` / `Tool.Status` / `Tool.DurationMS`（`subagent_live.go:26-56`）。
   → 前端能画出"第几轮、在调哪个工具、成功没、耗时多少、模型正在写什么"。
2. **历史可回放**：`SubscribeSubagentLive` 返回 `[]SubagentLiveEvent`（历史）+ `<-chan`（增量）
   ——详情**中途打开也能补全**（`application/core/subagent_view/README.md` 的时序图：
   "低频权威刷新 + live 即时刷新双通道"）。
3. **阶段是累积的**：`subagent_sessions.go:281` `append` + turn 自增 → `StagesJSON` 是一条**时间线**
   （`SubagentDetail.Stages` 直接用），不是一条被覆盖的状态。
4. **进展有"真信号"**：`AsyncRunRecord.LogBytes`（"真实的进展信号"，`dto/async_run.go` 注释）+
   `Tail` + `State`，界面与打点块据它判断"在不在动"，不靠墙钟猜。
5. **行/树/详情三层各就各位**：行（工作表格 `async:<handle>`）→ 树（`subagent_tree` 快照键，
   `gui/frontend/dist/snapshot-shape.js:21`）→ 详情（`SubagentDetail` 的 7 个 tab：
   会话记录/第一视角阶段/上下文/功能打点/事件时间线/工具活动/输出，`subagent_view/README.md`）。

### 1.5 teammate 缺了什么才有"看不懂"

| 缺口 | 事实 | 造成的读感 |
|---|---|---|
| **记录侧进度粒度只有一帧** | `teamUnitStages`（`workunit_team.go:176`）恒为单元素；每次 `saveTeamUnitRecord` 整条覆盖 → `StagesJSON` 没有时间线 | 回灌/详情只能说出"中断前**一步**"，说不出轨迹 |
| **实时读数落在第三个载体** | 记录（`NodeSessionRecord`）/ 看板（`TeamworkWorkItemView`）/ 实时（`teammate.tool.*`）三处各自回答"在干什么"，**互不引用** | 同一件事三处口径，读的人要自己拼 |
| **看板行没有进度字段** | `TeamworkWorkItemView`（`teamwork_board.go`）只有 `Status/SessionID/Worktree/Handle/Note/Live/Interrupted`——**没有 stage / 预览 / 字节** | "这件活在跑"能看到，"跑到哪"看不到 |
| **实时那一格是后补的对称面** | `role_tool_activity.go` 明确写它是 `subagent.tool.*` 的"员工侧对称面"（2026-10-03 才补），粒度够（`Turn`/`Duration`/`Status`）但**只画在"正在做（实时）"一节** | 有实时，但与记录/看板不是同一条读法 |
| **作业行走不进工作表格** | `teamworkTraceLines` 只进打点块；工作表格只有 `asyncWorkItems`（tools 表） | leader 熟悉的"台账"里看不到 teammate 的行 |

### 1.6 归类（纪律：不把"长得像"当"重复"）

**① 本质重复（同一件事，两份实现 → 该并）**

| # | 同一件事 | 现有份数 | 锚点 |
|---|---|---|---|
| R1 | **会话记录 → 进度读数的折算**（跑没跑完 / 结论 / stage / 现场） | 3 份手写 | `subagent_sessions.go:474`（写侧折算）、`:394`（读侧折算）、`workunit_team.go:134`+`:412`（teammate 折算） |
| R2 | **StagesJSON 的编/解码**（`[{"stage","preview"}]`） | 2 份载体 | `subagent`：`[]model.NodeStageLog`（`subagent_sessions.go:502` 编码 / `:408` 解码）；`teammate`：`teamUnitStages`（`workunit_team.go:176`） |
| R3 | **"是否在跑"词表** | 契约 1 份 + 2 处字面量 | 契约 `workunit/session.go:60`；未收口：`runtime_subagent_resume.go:329`、`subagent_sessions.go:515` |
| R4 | **恢复说明状态容器**（map，键 → 说明，读完即消） | 2 份同形状 | `runtime_subagent_resume.go:49` `subagentResumeState` vs `workunit_team.go:265` `teamResumeState`（源码注释自称"同一形状、同一语义"） |

**② 偶然重复（长得像，其实是两件事 → 不并）**

| # | 长得像的两份 | 为什么不并（事实） |
|---|---|---|
| A1 | `AsyncRunRecord` ↔ `TeamworkJobCompletionRecord` | 文档明确"**两张不同的表**"：来源（tools 登记表 vs `jobs.Manager`）、取回工具（`job_manage` vs `jobs_manage`）、scope 模型都不同；合并会产出"正文指着一个取不到这条作业的工具"（`dto/teamwork_jobs.go` 注释） |
| A2 | `SubagentToolEvent` ↔ `RoleToolActivity` | DTO 注释明确"同一件事的两面，**不合并成一个结构**（合并会让每个消费方都要先判断'这条到底是哪个身份'，而字段名会说谎）"——身份轴不同：`NodeID` vs `(主会话, 角色名, 角色会话)` |
| A3 | `SubAgentTreeNode` ↔ `TeamworkWorkItemView` | 一个是**执行树节点**（parent/children/depth），一个是**编排计划节点**（milestone/depends_on/role）——不同的图 |
| A4 | `asyncTraceLine` ↔ `teamworkTraceLine` | 同一块里两个**行生成器**（列集不同：字节/末行 vs 归属/工作项）；这是"一张表两种行"，不是同一件事两份 |

**③ 各层专有（只属于一层 → 不为统一强塞）**

- **subagent 专有**：执行树（`SubAgentTreeNode.Children/ParentID`）、`SubagentLiveEvent` 的
  `assistant` 正文增量、`SubagentDetail` 的 7-tab 分类、`NodeStageLogs` 第一视角历史、
  `SubagentRecoveryView.Resumable/ConclusionFound`、现场临时（`Immediate` 收尾即回收）。
- **teammate 专有**：编排计划（`milestones/work_items/depends_on`）、成员状态只有
  `running|free`（`teamwork_board.go` 注释：不存在 `done`）、`PluginAssemblyView` 装配读数、
  teammate 消息队列回执（`TeamworkTeammateMessageView`）、`TeamworkEventView` 审计流水、
  `AtTeamClose` 回收策略、`jobs.Manager` 作业行。

---

## 2. 建议的公有读面接口形状（Go 签名）

**落点**：`seelebridge/workunit/` 新增一个只读文件（暂名 `progress.go`）。
它**只装"折算"与"按 NodeID 读法"**——不装事件流、不装作业模型、不装归属。

### 2.1 进 workunit 的读数（= 两层交集）

判据：**这份读数在两条链都出现、语义一致、且本来就住在同一张 `NodeSessionRecord` 里**。

```go
package workunit

// Stage 是一条打点（阶段 + 有界预览）：与 NodeSessionRecord.StagesJSON 的载荷同形状。
//
// 它是 subagent 的 model.NodeStageLog 与 teammate 的 teamUnitStages 的共同抽象面——
// 两边**本来就在编/解同一串** [{"stage":…,"preview":…}]，只是各自的 Go 类型不同（R2）。
type Stage struct {
	Stage   string `json:"stage"`
	Preview string `json:"preview,omitempty"`
}

// EncodeStages / DecodeStages 是这串载荷**唯一**一份编解码（含 rune 上限）。
// 收编：subagent_sessions.go:502/:408、workunit_team.go:176 的编解码各自转调这里。
func EncodeStages(stages []Stage) []byte
func DecodeStages(payload []byte) []Stage

// Progress 是一个工作单元"此刻跑到哪、结论是什么、还在不在跑"的读数——**两层交集**。
//
// 它只是 NodeSessionRecord 的**只读折算**：不新增存储、不新增事件流、不新增归属。
type Progress struct {
	Kind      Kind      `json:"kind"`       // 描述性（日志/审计/恢复说明前缀族），不是分支判据
	NodeID    string    `json:"node_id"`    // subagent：节点 id；teammate：<role>-<itemID> / <role>
	SessionID string    `json:"session_id"` // 这件活自己的会话
	Goal      string    `json:"goal,omitempty"`
	Status    string    `json:"status"`     // 记录原词（终态由写方定名，契约不替它写死）
	InFlight  bool      `json:"in_flight"`  // = InFlight(Status)，判"还在不在跑"的唯一答案
	Summary   string    `json:"summary,omitempty"`
	Error     string    `json:"error,omitempty"`
	Stages    []Stage   `json:"stages,omitempty"`
	Worktree  sessionstore.NodeWorktreeRecord `json:"worktree,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProgressOf 把一条会话记录折成进度读数（**唯一一份折算**；不编造记录里没有的事实）。
//
// 收编 R1：subagent_sessions.go:474 的写侧兜底（running/queued）与 :394 的读侧还原、
// workunit_team.go:134/:412 的折算，全部转调这里——写侧只负责把"这一轮的事实"塞进
// record，折算口径只有一处。
func ProgressOf(kind Kind, record sessionstore.NodeSessionRecord) Progress

// UnitReader 是"**按 nodeID 读一件事此刻的进度**"的读面：把契约已经钉住的
// SessionLedger（Save/List）包成按 NodeID 定位的读法。
//
// 为什么需要它：两条链现在都在手写"List → 线性查找 → 折算"（subagent 的
// nodeWorkUnit.ledgerRecord、teammate 的 teamUnitRecords），读法已经是同一件事的第二、
// 第三份；包成一份后，**前端要读的"这件事跑到哪"只有一个后端入口**。
type UnitReader struct {
	ledger        SessionLedger
	projectID     string
	mainSessionID string
}

func NewUnitReader(ledger SessionLedger, projectID, mainSessionID string) *UnitReader

// Read 返回一件事的进度读数；无存储/无记录 → found=false（正常读数，不是错误）。
func (r *UnitReader) Read(nodeID string) (Progress, bool, error)

// List 返回本会话账本里全部单元的进度读数（按 NodeID 稳定排序；跨层混排时调用方按
// Kind 过滤——过滤是调用方的事，不是读面替它猜）。
func (r *UnitReader) List() ([]Progress, error)
```

**为什么"这些"进 workunit**（逐条给理由，不靠"看起来像"）：

1. `Stage`/`EncodeStages`/`DecodeStages`：两条链**已经在用同一个 JSON 载荷**（R2，锚点见上），
   形状是既成事实，不是新造抽象。
2. `Progress`/`ProgressOf`：字段**逐一对应 `NodeSessionRecord` 已有字段**（`Kind` 是描述性标注，
   `InFlight` 是已有判据 `InFlight()` 的显式化）。没有任何新事实源。
3. `UnitReader`：它包的是**契约已声明的 `SessionLedger`**（编译期断言钉着同一实现），
   把"两份手写的 List+查找+折算"收成一份。**不引新依赖、不新增存储**。

**刻意不进 workunit 的**（列出来才是"没有强塞"）：

| 不进的东西 | 理由（事实） |
|---|---|
| `Unit` 的**写侧**（`Begin/Finish/Reclaim`） | 已在契约里；本次只补**读侧**，不重开生命周期 |
| **归属/身份轴**（team_id / work_item / role / plan node） | `Progress` 只带 `NodeID/SessionID`；归属由各层补。依据 `role_tool_activity.go` 的明确裁决：两个身份轴合并会让"字段名说谎" |
| **实时事件流**（stage/tool/assistant 增量） | 载体与粒度两层不同（subagent = 按 nodeID 的 RPC 订阅 + 历史回放；teammate = 会话级事件）。统一它要动的是**事件发布点**；把事件流塞进契约包正是 `contract.go` 明确列为"刻意不装"的"统一事件流 = 第二份进度真相" |
| `AsyncRunRecord` / `TeamworkJobCompletionRecord` | 偶然重复（A1）：两张不同的表，取回工具不同 |
| 编排计划（milestones/work_items/members） | 各层专有（teammate） |
| 执行树 / 详情 7-tab / `Resumable` / `ConclusionFound` | 各层专有（subagent） |
| `StartedAt/EndedAt` 的 teammate 侧补齐 | teammate **刻意不写**记录时间戳（事实归计划 `item.StartedAt/FinishedAt`）。`Progress` 里这几个字段对 teammate 留零值 = "这件事没有这一格事实"，不是"读数没算" |

---

## 3. 前端改造面（若 workunit 当后端读面）

### 3.1 现在怎么消费（两套，各自独立）

| 链 | GUI 组件/函数 | 数据来源 | TUI |
|---|---|---|---|
| subagent（树+行） | `gui/frontend/dist/work-table.js`（行/详情入口）、`tree-fork.js`；快照键 `subagent_tree` | `snapshot.runtime.subagent_tree`（`snapshot-shape.js:21`）+ 事件 `worktable.changed`（随包附 `subagent_tree`） | `tui/plan.go`（plan 面板，按节点状态） |
| subagent（实时详情） | app.js 的详情弹窗绑 `SubscribeSubagentLive(nodeID)` | `dto.SubagentLiveEvent` 三类增量（`stage`/`tool`/`assistant`） | —（TUI 无子代理详情弹窗） |
| subagent（台账行/打点） | `work-table.js` | `snapshot.runtime.work_table`（`snapshot-shape.js:25`）+ 打点块 | —（TUI 不渲染工作表格） |
| teammate（看板） | `app.js` `renderTeam(snapshot)` → 渲染件 `team-board-view.js` `renderTeamBoard`（接线守卫 `gui/team_board_wiring_test.go`） | `snapshot.runtime.teamwork_board`（`snapshot-shape.js:27`） | `tui/goalteam.go:231` `teamPanelLines` → `:295` `teamBoardLines` |
| teammate（实时） | `app.js` `applyTeammateToolActivity`（按 `role_session_id` 分组、40 条有界缓存） | 会话级事件 `teammate.tool.started/completed` | —（TUI 无"正在做"区） |
| teammate（会话详情） | `team-board-view.js` `renderTeammateLiveSession` | `TeammateSessionLive`（RPC 拉取） | — |

**关键事实**：前端的"实时进度"是**两个不同的 hook**——子代理是**按 nodeID 的 RPC 订阅 + 历史回放**，
teammate 是**会话级事件 + 按角色会话分组的有界缓存**。两套 hook 的入参、键、刷新策略都不同。

### 3.2 若 workunit 当后端：前端要改成什么形状

目标形状 = **一条会话级事件 + 一个 hook**，事件载荷就是 `workunit.Progress` 的超集：

```go
// 建议：一条统一事件（载荷 = Progress + 可选的一帧实时活动）
//   kind              = KindSubagent | KindTeammate（描述性）
//   node_id           = 定位键（subagent 节点 id / teammate <role>-<itemID>）
//   session_id        = 这件事自己的会话
//   in_flight / status / stages / summary / error / worktree
//   activity{kind, name, status, duration_ms, text}   ← 实时那一帧（沿用现有有界口径）
```

前端收敛为一套 hook：

```js
// gui/frontend/dist/app.js（新）
function applyUnitProgress(event) {
  // 键 = (kind, node_id)：两层的进度区共享同一份 reducer
  // 子代理详情：「第一视角阶段 / 工具活动 / 正在做」三个 tab 都从这里长
  // teammate 详情：「正在做（实时）」+ 「进度（stage 时间线）」也从这里长
}
```

改动面与量级：

| 改动 | 文件 | 量级 |
|---|---|---|
| 后端：新增 `workunit/progress.go`（`Stage`/编解码/`ProgressOf`/`UnitReader`） | 新文件 | 小（~80 行纯函数 + 端口，无 I/O 新增） |
| 后端：subagent 读侧折算转调 `ProgressOf`（写侧仍各层） | `seelebridge/session/subagent_sessions.go`（`buildRecordLocked`/`restoreLocked`）、`seelebridge/workunit_node.go`（`ledgerRecord`） | 小（局部替换折算体） |
| 后端：teammate 折算转调 `ProgressOf` + `UnitReader` | `seelebridge/workunit_team.go`（`saveTeamUnitRecord`/`teamUnitRecords`/`RecoverTeamworkUnits`） | 小-中 |
| 后端：把两处实时投递点接到**同一条事件**（`subagent.tool.*` 与 `teammate.tool.*` 的发布点） | `seelebridge/runtime_live.go`、`seelebridge/runtime_role_turn.go`（`RoleToolCallback`）、`application/core/service.go:111`、`main.go:612` | **中-大**（跨 seelebridge→application→装配根三段；这是本次**不建议**与读面折算同批做的一步） |
| 后端：让 `TeamworkWorkItemView` 增加进度格（`stage`/`preview`/`in_flight`），从 `ProgressOf` 取 | `application/contract/dto/teamwork_board.go`、`seelebridge/runtime_teamwork_board.go` | 小-中（**可选**，见下） |
| 前端：新增 `applyUnitProgress`，替换 `applyTeammateToolActivity` 的分组缓存 + 子代理 live 的追加逻辑 | `gui/frontend/dist/app.js`、`team-board-view.js`（`renderTeammateLiveSession`）、`work-table.js`/`tree-fork.js`（子代理详情） | 中 |
| 前端守卫 | `gui/frontend/dist/*.test.mjs`、`gui/team_board_wiring_test.go`、`e2e/dto_boundary_test.go`（wire 键名） | 小 |

**分两批做的建议（重要）**：
- **第一批（低风险、纯收口）**：只做 `workunit/progress.go` + 后端两份折算转调 + `UnitReader`。
  前端零改动，两链行为不变（可用现有用例当回归网）。这一步就消灭 R1/R2。
- **第二批（中-大、动前端）**：把两处实时投递点接到同一条事件 + 前端收敛为一个 hook。
  这一步才真正产生"同一套 hook"；先做第一批不会挡它，也不会与它抢同一批文件。

**不许为了统一而做的三件事**（各层专有，见 §1.6③）：
1. 不把 teammate 的身份（`role_session_id`）塞进 `Progress`，也不把 `NodeID` 冒充成角色身份：
   两处详情入口仍**各按自己的身份**打开（`openRoleSessionDetail` vs 子代理详情弹窗）。
2. 不把 teammate 的编排计划（milestones/work_items）折进 `Progress`：那是 `TeamworkBoardView` 的事。
3. 不把子代理执行树折进 `Progress`：树是 `subagent_tree` 投影，进度是另一格。

---

## 4. 不确定项与验证方法

- **U1（本次顺带回答，见 §5）**：`workunit.Unit` 有没有生产调用者。
- **U2｜`Progress` 的 `StartedAt/EndedAt` 对 teammate 恒零值，会不会让"读面看起来缺了一格"？**
  - 验证：在 `workunit/progress_test.go` 加一条断言——teammate 记录（不带时间戳）经 `ProgressOf`
    得到零值 `StartedAt/EndedAt`，且 `InFlight`/`Stages`/`Worktree` 与非零；并由看板消费方
    （`runtime_teamwork_board.go`）从计划 `item.StartedAt/FinishedAt` 取时间，**不**从 `Progress` 取。
- **U3｜把 `subagent_sessions.go:474` 的写侧兜底（`running`/`queued`）改成转调 `ProgressOf`
  会不会改变落盘语义？** 现在写侧在"无 outcome 且有会话 ⇒ running，否则 queued"，读侧 `restoreLocked`
  原样还原。需要确认 `ProgressOf` 不把"写侧兜底"与"读侧还原"混成一个分支（记录里 `Status` 为空时
  的兜底只属于写侧）。
  - 验证：`go test ./seelebridge/session/ -run TestSubagentPersist -count=1`（现有
    `seelebridge/session/subagent_persist_test.go` 断言 `Status=="done" && len(StagesJSON)>0`）
    与 `go test ./seelebridge/ -run TestWorkUnit -count=1` 全绿；再补一条"空 Status 的旧记录读回
    不额外改写 Status"的用例。
- **U4｜`teamUnitStages` 单元素 → 时间线，会改回灌文案吗？** `workunit.RecoveryNote` 把 `StagesJSON`
  原样列出（`session_test.go:29` 用单元素夹具）。若第一批只做"编解码收口"（不扩成多元素），
  文案不变；若顺手让它累积，`RecoveryNote` 的"已到阶段（打点）"一行会变长 → 需回归。
  - 验证：`go test ./seelebridge/workunit/ -count=1` + `workunit_team_test.go:425`
    （断言 `len(record.StagesJSON) > 0`）作为护栏；扩展单一元素前先改这条断言并跑
    `TestTeamUnitRecoverMarksInterruptedAndInjectsTheNoteOnce`。
- **U5｜`UnitReader` 的 `List()` 该不该按 `Kind` 过滤？** teammate 记录与 subagent 记录**共用同一个
  `subagents/` 目录**（`workunit_team.go:412` 注释），teammate 侧现在**必须按现场名单过滤**
  （计划 + 账本）才不误读子代理记录。若 `List()` 不加区分，teammate 消费方会多读到子代理记录。
  - 验证：`List()` 返回 `Kind` 让调用方过滤（默认不过滤，避免把"猜归属"塞进读面）；
    加一条用例：同一目录写入一条 subagent 记录 + 一条 teammate 记录 → `List()` 返回 2 条、
    各带正确 `Kind`。
- **U6｜统一实时事件（第二批）的粒度上限**：`subagent` 侧有 `assistant` 正文增量、
  `teammate` 侧没有（`role_tool_activity.go` 只做工具）。统一事件是否带 `assistant` 增量需要裁决——
  带了则 teammate 侧恒空，不带则子代理正文增量要另走一条。
  - 验证：先写"事件载荷字段表"（每字段两层是否都有值），再定 schema；用
    `e2e/dto_boundary_test.go` 的 wire 键名断言口径守住 snake_case。

---

## 5. 顺带回答盘点 U1：`workunit.Unit` / `Lifecycle` 的生产调用者

**结论：`Unit` 接口本身**（以及它的两个适配器）**目前没有生产调用者，只在测试里**；
但契约的**函数与端口已在生产链上**。仓库里**不存在** `lifecycleHost`（全仓 grep 无命中）。

### 5.1 只有测试调用（生产无装配）

| 符号 | 定义 | 全部调用点 |
|---|---|---|
| `newNodeWorkUnit` | `seelebridge/workunit_node.go:45` | **唯一一处**：`seelebridge/workunit_node_test.go:235` |
| `newTeamUnit` | `seelebridge/workunit_team.go:493` | **唯一一处**：`seelebridge/workunit_team_test.go:591` |
| `workunit.Unit`（形状断言） | `workunit_node.go:42`、`workunit_team.go:603`（`var _ ... = (*xUnit)(nil)`） | 编译期断言；另有 `seelebridge/workunit_chain_test.go:213` 的类型表 |
| `FinishPolicy()` | `workunit_node.go:53`、`workunit_team.go:528` | 仅测试调用（`workunit_node_test.go:241/288`、`workunit_team_test.go:579`、`workunit_chain_test.go:62/181`） |

即：真实装配里，两条链**仍然直接调既有函数**，**不**把 `Unit` 当门面用——
subagent 走 `beginNodeWorktree`/`finishNodeWorktree`/`releaseNodeWorktree`
（`seelebridge/runtime_plan.go`），teammate 走 `BindWorkspace`/`SettleWorkItemOutcome`/
`Coordinator.Reclaim`/`RecoverTeamworkUnits`。适配器是"已写好、已用测试钉住、但还没接进生产装配"的中间态。

### 5.2 契约的哪些部分**已经**在生产链上

| 符号 | 生产装配点 |
|---|---|
| `workunit.ClassifyFinish` | `seelebridge/node/agent_node.go:191`（subagent 节点收尾）、`seelebridge/teamwork/items.go:478/482`（teammate 尾插收尾） |
| `workunit.RecoveryNote` | `seelebridge/runtime_subagent_resume.go:386`（KindSubagent）、`seelebridge/workunit_team.go:383`（KindTeammate） |
| `workunit.RecoveryNoteRole` | `seelebridge/runtime_subagent_resume.go:37`（`SubagentRecoveryNoteRole` 直接绑契约） |
| `workunit.InFlight` | `seelebridge/workunit_team.go:50`（`teamUnitInFlight` 转调）——**子代理侧两处字面量仍未转调**（R3） |
| `workunit.SessionLedger` | `seelebridge/workunit_node.go`（`ledgerRecord` 用 `workunit.SessionLedger(store).List`）、`seelebridge/workunit_team.go`（`teamUnitLedger` 返回 `*NodeSessionStore`） |
| `workunit.Jobs` | 编译期断言 `var _ Jobs = (jobs.Manager)(nil)`（`contract.go`）；消费方是 `teamwork.Coordinator.Reclaim` |

### 5.3 对本次读面建议的含义

- 因为 `Unit` 适配器**还没有生产调用者**，本次提议的读面**不要**挂在 `Unit` 的方法面上
  （那会变成"给没人调的接口再加四个方法"）。
  读面应当独立成 `progress.go` 的**纯函数 + `UnitReader`**，可被两条链**现成的**函数直接调用
  ——这正是"改一处、两条链都受益"而不逼迫立刻接手 `Unit` 门面的最小路径。
- 若将来把 `Unit` 接进生产装配，`UnitReader` 就是 `Unit` 之外**自然的读侧补充**（写侧 `Unit`、
  读侧 `UnitReader`），两者不互相替代。

---

## 6. 本次未做 / 边界（如实标注）

1. **未改任何代码**：本文件是唯一新增产物；所有结论来自只读阅读与 grep，未跑构建。
2. **未在真机验证前端**：GUI/TUI 的 hook 收敛只到"锚点 + 建议形状"一级；第二批（统一实时事件）
   需要另开实现任务，并按 `gui/team_board_wiring_test.go` 的接线守卫口径补用例。
3. **`node_session_store.go` 的时间戳字段**：subagent 写 `StartedAt`/`EndedAt`（记录形状里有），
   teammate 不写；本文件按"事实归计划"的口径记录，未把该差异判成缺陷。
4. **`workunit-single-lifecycle-one-implementation.md` 引用**：本 worktree 的 `docs/arch/` 下
   **不存在**该文件（只有 `workunit-duplication-inventory.md`）。§1.6 的归类纪律按任务正文的
   §7.2 口径执行（本质重复 / 偶然重复 / 各层专有），并在本文件内自洽。
