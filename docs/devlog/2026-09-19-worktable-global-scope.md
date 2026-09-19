# 工作表格作用域：项目/全局台账，不是会话粒度（2026-09-19）

> 修复类工作：先红灯复现 → 只改根因 → 红转绿 → 沉淀回归。

## 1. 症状（红灯复现）

工作表格（worktable）把 plan / todo / task / subagent 四类条目投影成一张表。
症状是这张表被做成了**会话粒度**：在一个会话里产生的条目，切到另一个会话
就从表里消失（`/new` 也会把表清空）。

复现（`seelebridge/worktable_global_scope_test.go`）：

```text
--- FAIL: TestWorkTableGlobalScopeRepro (0.00s)
    worktable_global_scope_test.go:51: 工作表格是会话粒度：切到 session-b 后 "plan" 条目从全局表消失（table=[]）
```

复现步骤：会话 A 里各建一条 plan / todo / task / subagent → `SwitchSessionTasks("session-b", nil)`
→ `TaskSnapshot()`（worktable 投影数据源）= **空**。

## 2. 根因

`Runtime.TaskSnapshot()` 直接返回实时注册表（`r.tasks.Snapshot()`），而
`SwitchSessionTasks` 在换会话时把离开会话的注册表整表搬进 scope 分区、把目标
会话的分区装回实时注册表（`ReplaceAll`）——于是"换会话即换表"。

`TaskSnapshot` 是三条投影路径的唯一数据源：
`view_state.CollectRuntimeProjectionFor` → `refreshWorkTableLocked`、
`publishTaskDeltas`、以及会话快照 `snapshotOfResident` / 冷读 `snapshotOfCold`。

## 3. 修复

工作表格改为**项目/全局读面**：会话只是条目的产生地，不是表格的作用域。

- `seelebridge/ports.go`：`TaskSnapshot()` = 实时注册表 + 各会话 scope 分区
  合并，按**跨会话身份**去重（幂等键优先，否则行 ID——`task:<n>` 是会话内
  序号，跨会话会重号，只按 ID 去重会丢行），稳定排序。
- `TaskSnapshotFor(sessionID)` **保持会话粒度**，只服务两件事：落盘
  （`SessionRecord.Tasks` 是会话自己的条目）与请求尾部打点块（避免把别的
  会话的活动任务注入本会话上下文——S1 的既有约束继续成立）。
- `application/core/work_table.go`：打点块改走会话级读面
  （视图/活跃会话归一为 `""` 分区）；新增 `mergeTaskRecords` 供冷读面把
  全局表与会话持久化条目拼成同一张表。
- `view_state/coordinator.go`：删掉"后台会话读自身分区"的分支，统一取全局。
- `session_scope.go` / `session_cold_read.go`：会话快照与冷读的工作表格
  同样取全局台账。
- fake（`service_fakes_test.go`）镜像生产的全局语义。

## 4. 需求变更（显式记录）

- `/new`（`BeginNewSession`）不再清空工作表格：它只切换当前会话指针。
  旧行为"新建会话清空 task 注册表与工作表格"（会话级工作台隔离）与"工作
  表格 = 项目/全局台账"冲突，按新口径移除。
  回归用例 `TestBeginNewSessionKeepsGlobalWorkTable`（原名
  `TestBeginNewSessionClearsWorkTable`）。

## 5. 回归

```text
go test ./seelebridge/ -run "TestWorkTableGlobal" -count=1     # 红 → 绿
go test ./seelebridge/ -run "TestWorkTableLedger" -count=1     # 残留风险①红 → 绿
go test ./seelebridge/task/ -run "TestRegistryAutoIDs" -count=1
go test ./... -count=1 -timeout 1200s                          # 全绿
```

- 新增：`seelebridge/worktable_global_scope_test.go`
  （`TestWorkTableGlobalScopeRepro`、`TestWorkTableGlobalReadKeepsSessionReadScoped`）。
- 改写：`seelebridge/task_partition_test.go`（全局可见 + 会话读面仍隔离）、
  `application/core/session_runtime_slot_integration_test.go`（`SnapshotOf(B)`
  的工作表格为全局台账；槽位隔离仍由 tokens/replan 断言）。
- 残留风险①的回归（见 §7）：`seelebridge/worktable_ledger_identity_test.go`
  （`TestWorkTableLedgerRowIDsUniqueAcrossSessions`、
  `TestWorkTableLedgerNeverReusesRestoredRowID`）与
  `seelebridge/task/auto_id_test.go`（`TestRegistryAutoIDsNeverReuseRestoredIDs`）。

## 6. 残留风险（首轮结论，§7 收口①）

- 跨会话同号：`task:<n>` 是会话内序号，若两个会话各自产生无显式 ID 的条目，
  台账里会出现同 ID 两行（前端按 ID 做 keyed reconciliation）。现状生产路径
  的后台条目都带显式 ID（`plan:<node>` / `subagent:<id>`）或幂等键，触发面
  很小；彻底消除需把自增 ID 提到全局分配（另行设计）。
- "全局"是**进程内**：未驻留会话的条目不在内存分区里，冷读面用
  `全局表 ∪ 该会话 record.Tasks` 兜底；跨进程/跨项目的精确归档未做。

## 7. 残留风险①收口：自动条目 ID 提到进程级分配

### 7.1 症状（红灯复现）

台账的行身份是「幂等键优先，否则行 ID」，而自动行 ID 是**每会话各自从 1 数**：

```text
A id="task:1"   B id="task:1"        ← 两个会话的无键条目同号
global table rows=1                  ← 合并去重把 B 整行丢掉
  id="task:1" task="A 的无键任务"
```

第二处更硬：会话切换/磁盘恢复把外部的号带进注册表，而分配器水位从 1 数，
于是**新条目直接覆盖旧行**（`state.tasks[id]` 以 ID 为键）：

```text
Restore("task:1", 磁盘旧任务) → Add(无键新任务) = "task:1"
snapshot: 只剩 id="task:1" task="new"   ← 旧任务凭空消失
```

待办同理：`todo:<n>` 从注册表计数器取号，恢复的 `todo:2` 会被新待办重发，
`TodoSnapshot` 出现同一条两行。

### 7.2 根因

`addTaskLocked` 用注册表私有 `state.nextID`，`addTaskPartition` 用
`task:<len(records)+1>`——两处都是"每个注册表/每个分区各自从 1 数"；而注册
表的行键就是 ID，台账的合并去重也按行 ID，所以重号既是覆盖（丢数据）也是
合并丢行（跨会话不可见）。

### 7.3 修复

- `seelebridge/task/task.go`：自增源提到**包级**（`autoIDSeq`，跨注册表/跨
  会话分区共享），`NextAutoID(prefix)` 出号；`ObserveAutoID(id)` 在
  **装载/恢复**时抬高水位（外部数据的号不重发）；`nextFreeIDLocked` 再跳过
  注册表里已占用的号（防御跨版本/外部分配）；删除 `TaskRegistryState.nextID`。
- `seelebridge/ports.go`：`addTaskPartition` 改用 `task.NextAutoID`（不再用
  分区长度），显式 ID 也经 `ObserveAutoID` 抬高水位，并在分区内跳过已占用号。
- 分配器的语义边界写进注释与模块 README：**进程内唯一**；跨进程/跨项目
  的精确归档仍属 §6 第二条。

### 7.4 回归

```text
go test ./seelebridge/ -run "TestWorkTableLedger" -count=1      # 红 → 绿
go test ./seelebridge/task/ -run "TestRegistryAutoIDs" -count=1 # 红 → 绿
```

### 7.5 顺带发现（**未修**，另行排期）

`todo:<n>` 的号**不是**待办清单索引，而 GUI 三态回写按 ID 里的数字当索引：

- 生产：`todo:<n>` 的号取自**进程级共享计数器**（与 `task:<n>` 同一序列），
  与清单位置无关。修前 `todo_init` 两条 → ID `todo:2` / `todo:4`（todo 分支
  在旧代码里多消耗一次号）；修后双消耗没了，但共享计数器下仍不对齐——先发生
  一次普通 task 分配再 `todo_init` 两条 → ID `todo:2` / `todo:3`（实测）；
- `UpdateWorkItemStatus("todo:2", "doing")`（第一行）→ `parseWorkItemID`
  取 index=2 → `SetTodoStatus(2)` → 命中第三条（或越界报错：两条清单时
  `todo: index 2 out of range (0..1)`）。

即 `todo:` 行的 ID↔清单索引契约在真实注册表下本就不成立（`application/core`
的用例走 `fakeRuntime`，其 `todo:<index>` 恰好对齐，所以没照出来）。彻底修需
把 GUI 回写改成**按行 ID 寻址**（记录 ID 现在是进程内唯一的，天然可寻址），
并让回写落到条目所属会话的 scope，而不是 `SetTodoStatus(index)` 打当前会话。
本轮不动它：它与本次残留风险①不是同一个面，改动会牵动 GUI 协议与 fake。

