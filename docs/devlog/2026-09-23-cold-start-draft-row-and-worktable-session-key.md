# 初始会话切换后从会话树消失 + 工作表格增量丢归属键 + 表格刷新缓冲（2026-09-23 第 3 轮）

> 触发（用户报告）：
> 1. 「初始会话如果切换会消失」需要复现并进一步修复；
> 2. 「子代理在工作表格中出现了独立出主会话的情况——用当前会话筛选结果筛选不到正在运行的
>    子代理内容；并且刷新过程不自然，总是在跳（需要设置一个缓冲机制）」；
> 3. 追问 goal 治理面板出现的 `active · Round 0 · 座次 advisor-b · peer advisory_pending ·
>    governance stalled` 是什么意思。
>
> 本轮把 1、2 做成带牙齿的修复，3 给出定性结论并把「要不要改」留给用户决策（见 §5）。

## 1. 根因（只读调查 + 复现，全部可验证）

### 1.1 冷启动草稿只分配 ID、不落槽位 → 切换后「初始会话」无来源

链路（Confirmed）：

- 冷启动 `EnginePort.SessionID() == ""`（`main.go` 传入的 engine 为 nil 时端口
  `sessionID` 保持空，`internal/adapters/engine_port.go`），装配期走草稿分支；
- `application/core/service_assembler.go`：

  ```go
  initialSessionID := service.Deps.Engine.SessionID()
  initialDraft := initialSessionID == ""
  if initialDraft {
      initialSessionID = service.newDraftSessionIDLocked()   // 只分配 ID
  }
  ```

  `newDraftSessionIDLocked`（`session_draft.go`）只做 `draftSeq++` + 拼字符串，**不建
  `draftSlot`**；全仓 `service.draft` 的写入点只有 `BeginNewSession`、`resetViewToDraftAfterRestoreFailure`
  与 `session_history.go` 的降级路径。
- `application/core/service_snapshot.go` 的草稿行注入被 `if service.draft != nil` 把门：

  ```go
  if service.draft != nil {
      // 保留的草稿槽位在会话树中始终可见（切换后不再"消失"）。
      ... snapshot.Sessions = append([]SessionInfo{{ID: slot.ID, ..., Status: SessionStatusDraft}}, snapshot.Sessions...)
  }
  ```

- 目录本体（`session_runtime/coordinator.go` 的 catalog worker）只枚举**持久化**会话，
  冷启动草稿没有落盘记录，因此不在目录里。

⇒ 冷启动那一行**只**由前端「当前会话兜底行」显示（`app.js renderSessions`：
`if (currentID && !items.some(...)) items.unshift({id: currentID, ...})`）。一旦切到别的会话，
`currentID` 变了，兜底行随之移动——初始会话在列表里「消失」。

### 1.2 `task.changed` 增量丢归属键 → 「仅本会话」筛不到正在跑的行

- 整表路径带键：`seelebridge/ports.go taskSnapshotAll()` 给实时注册表记录标
  `currentTaskSessionID`、给分区记录标分区键；
- 增量路径不带键：`application/core/work_table.go publishTaskChanged` 直接把
  `dto.TaskRecord`（实时注册表记录，`SessionID` 为空）经 `taskRecordToWorkItem` 下发；
- 前端按 `task_id` **整行替换**（`gui/frontend/dist/protocol.js`），已带键的行被无键副本
  覆盖；`work-table.js sessionRows` 再按 `(row.session_id||"") === viewSessionID` 过滤 → 行消失。
- 子代理最明显：`syncSubagentTask` 每次状态迁移都走 `TaskAddFor`/`TaskSetStatusFor` → 注册表
  发增量 → 运行中的行被反复替换（「独立出主会话」的观感就是这么来的）。

### 1.3 表格刷新「总在跳」

`runtime.changed` / `worktable.changed` / `task.changed` 三类事件各自**同步**触发一次整块
`renderWorkTable`（批次条重排、变更行 `replaceWith`、新行插入带动滚动位置变化），事件突发时
逐条重绘即视觉抖动。

## 2. 改法

### 2.1 冷启动落草稿槽位（F1）

`service_assembler.go` 的草稿分支同步建 `draftSlot`（与 `BeginNewSession` /
`resetViewToDraftAfterRestoreFailure` 同构）。收益不止「行不消失」：槽位是
`BeginNewSession` 幂等复用、显式提交物化（`isUnmaterializedDraftTarget` /
`materializeDraftForSubmit`）的唯一责任源，冷启动此前在这两条路径上也都缺身份。

`resident_lru_test.go` 的目录行计数断言随之排除草稿槽位行——草稿行与目录 `sess-*` 是两层数据。

### 2.2 增量补齐归属会话（F2）

`publishTaskChanged` 在发布前补齐：

```go
if record.SessionID == "" {
    record.SessionID = sessionID // 实时注册表恒属当前任务会话（与 taskSnapshotAll 同源）
}
```

前端同口径收敛：新增唯一判定 `work-table.js rowBelongsToViewSession`，**空归属按本会话处理**
（该文件注释早已声明这一意图，代码未实现），由会话筛选、会话计数、「实发」轴共用——避免
「计数说 1 条、列表 0 条」。

### 2.3 表格三轴事件尾随合并（F3）

`app.js` 新增 `renderIncrementalBuffered`：三类事件进 ~120ms 尾随窗口，flush 时取并集里最强的
一类（`runtime.changed ⊇ task.changed ⊇ worktable.changed`）只重绘一次，用**最新快照**
（latest-wins）。消息/工具/交互/团队事件仍即时；快照应用与回执水位（`client-state.js`）不变。

## 3. 验证

- `go build ./...` 通过；`go vet ./application/core/...` 通过；
- `go test ./application/core/ -count=1`（561+ 用例）通过；`go test ./application/...`、
  `go test ./seelebridge/...` 全通过；
- 新增牙齿：
  - `TestColdStartDraftSlotIsRetainedAcrossSwitch`：**改前红**（`cold-start draft row "draft_…"
    missing from Snapshot().Sessions = []`）→ 改后绿；
  - `TestTaskChangedIncrementCarriesOwningSession`：**改前红**（`task.changed 行归属会话 = ""`）
    → 改后绿；
  - 前端 `session filter treats rows without an owning key as the view session`：**改前红** → 绿；
  - 前端 `node --test dist/*.test.mjs`：**433 passed / 0 failed**。
- GUI 冒烟：**未执行**（运行中的 GUI 是旧构建，需重开进程才加载新的 `dist/*.js`；DOM 交互靠人工）。

## 4. 遗留与已知边界

1. `publishTaskChanged` 用「事件登记时的视图会话」补齐归属；`SwitchSessionTasks` 与
   `Snapshot.Session.ID` 之间仍有极短窗口（`session_draft.go` 先写快照、锁外再切任务会话），
   窗口内到达的增量可能标成新会话。彻底关闭需要 `RuntimePort` 暴露 `CurrentTaskSessionID()`
   （接口加方法波及全部实现，本轮未做）。
2. 子代理**持久化记录**的键空间漂移（RC-2）：`sessionstore/node_session_store.go` 以「出生时的
   mainSessionID」为键，而草稿早分配 ID（`draft_*`）与引擎/恢复态 ID（`sess_*`）是两个空间，
   冷恢复时 `List(sessionID)` 可能取空（`seelebridge/runtime_subagent_recovery.go`）。本轮未改，
   属独立议题。
3. 前端「刷新总在跳」只做了事件侧合并；`work-table.js reconcileSheets` 计数变化即整段
   `innerHTML` 重写带来的横向滚动归零仍存在（未动，避免扩大改动面）。
4. 前端测试仍以纯函数/源码口径断言为主，DOM 侧（合并窗口、滚动保持）未自动化。

## 5. `governance stalled` 的定性结论（说明，不是修复）

- 三个后端词是**准确**的：`active` = `GoalGovernanceView.Status`；`Round N` = 已完成整轮座位轮转数
  （`govern/SnapshotOf`，`goal_coordinator.go`）；`座次 advisor-b` = `turnGovernor.Current()`；
  `peer advisory_pending` = `Supervisor.Snapshot().Peer`（协议生命周期
  `bound → evaluating → advisory_pending → …`，见 `docs/2026-09-07-seele-a2a-framework-req/ds-a2a-*.md`）。
- `governance stalled` **不是后端字段**，是前端纯只读的墙钟启发式
  （`app.js startGoalStallMonitor`）：`floor(now) > heartbeat_at + 10`。心跳由
  `goalCoordinator.bumpHeartbeat` 在治理推进/状态迁移时单调自增（`Begin` 也会打点）。
- 因此：**目标空闲等用户输入时也会亮**（Round 0、无 ADVISOR 在飞，10 秒后即显示 stalled）。
  该组合同时是「一次轮转被中止」的指纹（`runRoundLocked` 置 `PeerAdvisoryPending` 后因求值失败
  返回错误），前端这条纯墙钟判据**无法区分**「合法空闲」与「真卡住」。
- 结论：这不是后端错误，是**前端过度断言**。两种改法各有代价——(a) 仅在「有在飞工作时」才算
  stalled：会同时把「轮转中止」的症状藏起来；(b) 由后端显式给出「本回合是否应有进展」再判定：
  需要动契约。**留作决策项**，本轮不改前端语义。
