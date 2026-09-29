# 子代理行的归属会话：行属于"发起 fork 的会话"，不是"谁在看"

日期：2026-09-29
范围：`seelebridge/session/subagent_tree.go`、`seelebridge/fork/tool.go`、`seelebridge/task/tools.go`、
`seelebridge/ports.go`、`seelebridge/runtime_tools.go`、`seelebridge/runtime_plan.go`、
`application/core/work_table.go`、`application/contract/dto/subagent.go`

## 0. 一句话

工作表格的行是**会话粒度**的（前端「仅本会话」与「实发」都按 `row.session_id` 取值），而子代理树
是**进程级**的一张（多会话的 fork 共处一树，节点上没有归属标记）。于是"这一行属于谁"在三个写入口
上都被答成了"谁在看/谁在写"：正在被同步的会话、当前视图会话的实时注册表。用户看到的正是这句
描述的反面——**本该会话粒度的子代理，出现在别的会话的工作表格里**，而它自己那个会话反而筛选不到。

## 1. 现场（三条写入口，同一个错答案）

| # | 写入口 | 今天归属成谁 |
| --- | --- | --- |
| 1 | `application/core/work_table.go` 的 `syncTasksFromSourcesFor(sessionID)`：`walkTree` 把**整棵**树投影进被同步会话的 task scope | 被同步的那个会话（视图切走后为后台会话同步，就把视图会话的子代理写进后台会话的分区） |
| 2 | `seelebridge/fork/tool.go` 的 `bindSubagentTask`：`TaskResolveByKey` / `TaskAdd` / `TaskSetStatus` 都是**无会话**入口 | 实时注册表 = 当前视图会话（后台会话里 fork 的子代理，台账行落进别的会话） |
| 3 | `seelebridge/task/tools.go` 的 `task_add` / `todo_init` / `todo_add` / `todo_done` / `todo_status`：handler 忽略 `ctx` | 同上（子代理看得见这些工具，它的 `todo_init` 会把**视图会话的清单整表顶掉**） |

第 1 条的树是 `Engine.SubAgentTree()` 的投影：`session/subagent_tree.go` 的 `subagentNodeRecord` 里
只有 `sessionID`（子代理**自己**的节点会话号），没有任何字段记"发起 fork 的主会话"。恢复路径
（`RestoreSubagentAnchors(sessionID)`）按会话 List 记录、却把节点并进同一张全局树，于是"谁的树"
在内存里再也分不出来。

前端的兜底把后果放大了一档：`gui/frontend/dist/work-table.js` 的 `rowBelongsToViewSession` 把
**空归属**按"本会话"处理（它的理由是对的——旧版无键增量不该让行消失）。因此一旦行的会话号错成
另一个会话号，它就在**每个**会话的「仅本会话」里出现；而正确归属的那个会话里，它反倒是"别的会话的
行"。请求尾部的打点块（`workTableTraceBlockFor(sessionID)`，按会话取 `TaskSnapshotFor`）拿到的是
被污染的 scope，于是别的会话的子代理行会真的进到**模型上下文**里。

## 2. 改法

1. **节点带归属**（`application/contract/dto/subagent.go` + `seelebridge/session/subagent_tree.go`）：
   `SubAgentTreeNode.MainSessionID` / `subagentNodeRecord.mainSessionID`。fork 注册时按执行 ctx 的
   会话键标注（`RegisterFork(mainSessionID, parentID, specs)`；嵌套 fork **从父节点继承**——嵌套链上
   的 ctx 会话键不是归属的权威来源），`Restore(records, mainSessionID, belongsToCurrent)` 按"这些记录
   属于哪个会话"标注。空 = 未标注（旧记录/无 ctx 会话的注册路径）。
2. **按归属投影行**（`application/core/work_table.go`）：`syncTasksFromSourcesFor` 只投影
   `MainSessionID == 本会话` 的节点，**未标注的不猜**（宁可少一行，也不把别人的行摊给每个会话）。
3. **写入口按会话键**：`fork.Deps` 的三个 task 回调换成 `TaskResolveByKeyFor` / `TaskAddFor` /
   `TaskSetStatusFor`（编译期钉住"谁忘了传会话"），`task.Deps` 换成 `SessionFromContext` +
   `TaskAddFor` + 清单族的四个 `*For`（`ReplaceTodoFor` / `AppendTodoFor` / `SetTodoStatusFor` /
   `TodoSnapshotFor`，清单项在注册表里就是 `kind=todo` 的行，会话级读面与 task 分区同构）。
   空会话键 = 旧调用面 → 实时注册表，行为不变。

## 3. 有牙证明（先红后绿）

- `application/core/work_table_subagent_session_scope_test.go`（新）：
  `TestSubagentRowsStayOutOfActiveRegistryOfOtherSessions`（视图路径）与
  `TestBackgroundSyncKeepsForeignSubagentRowsOut`（后台路径）在**过滤之前**红，报文分别是
  "B 的子代理行进进了 A 的实时注册表"/"B 的子代理行被同步进了 A 的分区"；第三条
  `TestSubagentWorkItemCarriesOwningSession` 钉台账行的会话号。
- `seelebridge/subagent_tree_test.go`：`TestRegisterForkTagsOwningMainSession`（嵌套继承）与
  `TestRestoreSkipsRecordsOfAnotherMainSession` 补钉恢复节点的归属标注。
- `seelebridge/task_add_session_scope_test.go`（新）：把 `taskToolsDeps` 的 `SessionFromContext` 换成
  恒空（= 修复前口径）立刻红在"后台会话的 taskadd 行落进了视图会话注册表"，`todo` 用例红在
  "视图会话的清单被别的会话顶掉"。
- `seelebridge/fork/bind_subagent_task_test.go`：`TestBindSubagentTaskRoutesByOwningSession` 用记录型
  回调钉住归属会话被一路带到三个写入口。
- 既有钉子按新契约修正：`TestRefreshWorkTableSnapshotPublishesSubagentRows` 的夹具补上
  `MainSessionID`（无归属的节点按设计不投影——正是它红了才把这个契约显式说出来）。

## 4. 验收

`go build ./...`、`go vet ./seelebridge/... ./application/... ./internal/... .`、
`gofmt -l`（改动文件为空）、`go test ./... -count=1`（63 个包全 ok）实跑通过。

## 5. 边界（本轮未做，需要时另开）

- **GUI 子代理树面板**仍读 `snapshot.runtime.subagent_tree`（进程级整树），本轮只把**工作表格行**
  收窄到归属会话；`main_session_id` 已经随 DTO 下发，面板要做会话轴时不必再改协议。
- **角色会话（A2A/员工）里发起的 fork** 归属是角色会话号本身；生产里"角色会话 → 宿主主会话"的折算
  只在权限门（`tools.SetRoleSessionOwnerResolver`）有实现。要不要把同一折算接到树归属上，取决于
  产品上角色会话是否算独立会话——不在本轮猜。
