# core/work-table（根包分卷）

## 生态位

后台作业面与工作表格投影：执行登记表的只读投影、请求尾部打点块（在途行 + 待取回的完成行）、作业终态为**空闲**会话触发对话、项目看板的派生投影（按会话落盘 + 按需读取面）

覆盖：`work_table*.go`、`async*.go`、`project_board*.go`；未归属文件由覆盖自检拦下。

`worktable.changed` 既是表格增量，也是子代理树的送达通道：树内容变化时
随包附带 `subagent_tree`（清空时显式空数组），前端详情入口据此把工作表格行
解析成弹窗节点，避免"行先到、树未到"时详情打不开。

## 后台命令行（第四路输入，只读投影）

`buildWorkTable` 有四个输入：plan、task 注册表、子代理树，以及 **后台执行登记表投影**
（`Runtime.AsyncRunsSnapshot()`）。第四路刻意**不写进 task 注册表**：注册表随会话
`record.Tasks` 落盘并在恢复时回灌，而后台句柄活在内存——真进去就会在重启后留下一条永远
running 的假行。因此后台行的生命完全跟着登记表：派发出现、探针更新、终态被驱逐即消失
（不变量 I-21：registry 是 async 行的唯一事实源，投影只读、不落盘）。

- 行身份：`ID = SourceID = async:<handle>`、`kind = task`（owner 裁定）；行标题取模型派发时
  写的 `description`，描述列是命令行原文，探针读数（state/exit/已产出字节/末行/耗时/是否
  降级）落在可展开的打点行，日志路径落在附件列——**这四项前端零改动**，列位本来就存在。
- 三处建表入口（实时重投影、会话快照、冷读面）统一走 `asyncRunsForTable()`，避免"切到哪个
  会话才看到哪些行"。行数上限 `asyncWorkMaxRows`（在途优先、终态按最新补齐），否则登记表
  的 256 个记录槽会挤掉真实任务行。
- 请求尾部打点块并**在途行 + 待取回的完成行**（打点 K-5 的完成回填），且字段比界面窄：
  句柄、`kind`、state、已产出字节，在途行带标题、完成行带**有界摘要**（state + exit +
  行数 + 字节数 + 有界末行，≤512B）。**不含**日志路径、末行原文（摘要里的末行是压成一行、
  限长的采样）、时间戳——那三者是探针给 GUI 的，进上下文等于每轮重付一遍，还会把日志正文
  变成注入面。整块行数（含开/闭标记、标题与读法说明）≤ `workTableTraceMaxLines`；被截断
  的完成行补一行汇总，不静默消失。模型 `bg_manage(op=fetch)` 取回（或 `op=done` 销项）
  之后那一行才从投影里消失——history 里的结果成为唯一事实。
- 生命周期消费者由三个变四个：`consumeAsyncRuns()` 收登记表的变化信号（派发/终态/驱逐/
  去抖后的新字节），走 `refreshWorkTableFromSources()` 复用同一条发布路径，所以表格与打点块
  不会分叉。信号是容量 1 的汇聚口，中间态被合并掉无所谓：每次重投影读的都是当下全量。
- 同一个信号还驱动**后台作业终态触发对话**（`async_completion.go`）：落到 done / failed 的
  作业若所属会话**空闲**，就为它起一个回合（`submitConversationFor`），正文给出 handle 与
  取回指令；忙会话不唤醒（铁律 §6.1），它走上面那条"完成行回填"。它刻意**并进**
  `consumeAsyncRuns` 而不是再起一个消费者——信号口是容量 1 的**单接收者**通道，两个消费者
  抢同一次发送时只有一个收得到。开关 `limits.async_exec.trigger_conversation`（默认关，
  出厂打开）；幂等键是句柄，账本随登记表裁剪、被登记表规模封顶。
- 锁纪律：登记表锁是**叶子锁**（tools 侧从不回调进 application，只发 channel），所以持
  `ViewMu` 时读它是安全的；必须锁外取的是 `Engine.SubAgentTree()` 那类会拿会话锁的读面。

## 项目看板投影（project board，M2/M3；落点 M7 搬迁、M9 按会话分目录）

把**已经存在**的两份投影按会话落成一组追加式 JSONL **派生件**，供按需读取；物理落点是**项目目录下按会话分的目录**（2026-10-15：先从数据根 `project-board/project-<hash>/…` 搬进项目根，再**按会话分目录**——一个会话一份文件，防串写、水位按会话各自推进、回收即摘目录）：

```text
<项目根>/.seelex/board/<会话 ID>/worktable.jsonl   # 工作：todo / task 行（会话私有）
<项目根>/.seelex/board/<会话 ID>/teamwork.jsonl    # 编排：milestone / milestone_item 行
<项目根>/.seelex/board/BOARD.md                    # 人读件（全项目汇总，看板根）
```

- **落点解析只有一处**：`Service.ProjectRootFor(sessionID)`（= 会话所属工作区的 `RootPath`；
  未绑定工作区返回 `""` ⇒ **不落盘**，契约 §3）。存储面用
  `Router.SetProjectBoardRootFor(func(sessionID) string)` 注入同一解析器——存储面不认识工作区，
  因此会话删除时的回收挂在 `Router.DeleteWorkspace` 上（不在 `jsonRepository.Delete` 里）；
  落点目录由 `store.ProjectBoardDir(projectRoot)` / `ProjectBoardSessionDir(projectRoot,
  sessionID)` 一处派生（固定名 + 字面会话 ID，不再派生 hash 目录）。
- **事实层不动**：工作表格行来自 `buildWorkTable`（`work_table.go`），里程碑/工作项来自
  teamwork 计划与运行态（`store/teamwork.go`）。本模块只做**字段映射**，不判定、
  不重算状态（`AGENTS.md §8.3` 的唯一判据纪律）。
- **纯函数**：`BuildProjectBoardRows(scope, seq, at, workItems, teamPlan, teamState)`。
  只搬 `kind ∈ {todo, task}` 的行——plan 节点与 subagent 行各有自己的读面（`read_plan` /
  子代理树），搬进来就是第二份判据。里程碑状态**运行态优先、声明态兜底**；整队是否收口
  读 `TeamworkState.State`（**不是** `TeamworkPlan.State`，那是 `TeamworkState` 结构体本身）。
- **单写者同步器**：`ProjectBoardSyncer`。`OnToolComplete` **不做 IO**（判触发面 → 标脏 →
  非阻塞唤醒；信号口容量 1、latest-wins），唯一 flush goroutine 跑全部落盘：取活体 →
  `BuildProjectBoardRows` → 内容指纹去重（去掉 seq/at）→ 差集补 `payload{"removed":true}`
  （tombstone）→ 追加。写失败只记日志（投影可重建），不 panic、不阻塞回合。
- **I1 自愈**：指纹相同但水位退到记账值之下（文件被删/被截）⇒ 即使内容没变也重建，
  否则一次误删会被去重固化成"投影永远缺失"。
- **触发装配表**：`TriggerFor(hook)` = `off | todo_list（缺省）| task_complete |
  tool_complete/all`；词表唯一来源是 `ProjectBoardHookModes()`（装配期用它校验配置里拼错的
  档名，**显式启动失败**，不静默降级成"从不触发"）。
- **注入缝**：`Service.SetProjectBoardSink`（`components.projectBoard` 槽，装配期交接一次）；
  完成段在**既有** `refreshWorkTableFromSources()` 之后交接——`ToolHookBridge.Hooks()` 是固定
  闭包，不新造回调入口。
- **不进上下文（I7）**：投影不进 system/project 前缀，也不进请求尾部；能力开/关的装配前缀
  逐字节相同（根包 `project_board_i7_test.go`）。
- **关就是关**：`limits.project_board.enabled=false` ⇒ 不注入订阅口、不注册读取面、一个文件
  都不写。**出厂档 `config/seelex.yaml` 显式打开**（2026-10-15 起：代码落地 ≠ 盘上有东西，
  缺省必须是"真的会留下投影"那一侧；回滚臂就是这一行改回 false）。
- **写侧端到端**：根包 `project_board_write_test.go` 走**生产口径**的全链路——建工作区 →
  `BeginNewSession`（早分配 SID）→ 绑定工作区 → 模型真的调 `todo_add` → 工具完成段 →
  同步器取活体工作表格 → `<项目根>/.seelex/board/{worktable.jsonl,BOARD.md}` 真的出现，
  且行（落在该会话自己的目录里）归属该会话。它同时钉住 R-2 那条前提：**视图会话 = 运行时当前 task 会话**时
  `WorkTableRowsFor` 才取得到行（不是该口径的会话取到空集 ⇒ I6 退场、什么都不落）。
- **按需读取面（M4）**：插件技能条目 `plugins/default/project-board/SKILL.md`（只有 name +
  description 进技能目录）+ 只读工具 `project_board_read`（`registry.RegisterProjectBoardReadTool`，
  **只在 enabled 时注册**；取数在 `middle/view/board/read.go`（P21-11 归位），有界、越界显式报错）。

契约：`docs/arch/seelex-project-board.md`。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### async_completion.go

- `func (service *Service) triggerAsyncCompletions(triggered map[string]struct{})` — triggerAsyncCompletions 对登记表做一次全量扫描：把"终态 + 会话空闲 + 还没触发过"
- `func (service *Service) sessionIdleForAsyncTrigger(sessionID string) bool` — asyncCompletionTriggers 报告某个状态是否该触发对话。
- `func asyncCompletionPrompt(record dto.AsyncRunRecord) string` — asyncCompletionPrompt 组装触发回合的正文。
- `func asyncPromptKind(kind string) string` — asyncPromptKind 是进正文前的类别兜底：类别缺失时按 process 读，与打点块同口径。
- `func (service *Service) triggerTeamworkJobCompletions(triggered map[string]struct{})` — triggerTeamworkJobCompletions 对 teammate 作业表做一次全量扫描：把"终态 + 会话空闲 +
- `func teamworkJobCompletions(runtime contract.RuntimePort) []dto.TeamworkJobCompletionRecord` — teamworkJobCompletions / teamworkJobEvents 是 teammate 作业表的**宿主转发**：
- `func teamworkJobEvents(runtime contract.RuntimePort) <-chan struct`
- `func teamworkCompletionPrompt(record dto.TeamworkJobCompletionRecord) string` — teamworkCompletionPrompt 组装 teammate 完成回执的正文。

### async_completion_trigger_test.go

- `func asyncCompletionHarness(t *testing.T, trigger bool, engine contract.ChatEngine) (*Service, *fakeRuntime)` — asyncCompletionHarness 造一个装配好该触发路径的 Service。
- `func waitForTriggeredTurn(t *testing.T, service *Service, needle string) string` — waitForTriggeredTurn 轮询可见会话，直到出现一条含 needle 的用户行（= 触发回合开
- `func assertNoTurn(t *testing.T, service *Service, needle string)` — assertNoTurn 报告可见会话里有没有含 needle 的用户行——"不该触发"的用例用它。
- `func completedRecord(handle string, state dto.AsyncState) dto.AsyncRunRecord` — completedRecord 造一条已落到终态的作业投影记录。
- `func TestAsyncCompletionTriggersIdleSessionTurn(t *testing.T)`
- `func TestAsyncCompletionTriggersOnFailureToo(t *testing.T)`
- `func TestAsyncCompletionIgnoresRunningAndKilled(t *testing.T)`
- `func TestAsyncCompletionDoesNotWakeBusySession(t *testing.T)` — 铁律 §6.1「绝不唤醒忙会话」：正在跑的会话不被打断、也不被塞队列——条目刻意不记账，
- `func TestAsyncCompletionTriggersOncePerHandle(t *testing.T)`
- `func TestAsyncCompletionStaysOffWhenDisabled(t *testing.T)` — 开关默认关：不置 trigger_conversation 时信号只驱动工作表格重投影，终态不会起任何回合。
- `func TestAsyncCompletionIgnoresJobWithoutSession(t *testing.T)` — 没有会话归属的作业（别的进程的作业归属、或会话已被删除）不该触发：没有可起的回合

### project_board_read.go

- `func (service *Service) WorkTableRowsFor(sessionID string) []model.WorkItem` — WorkTableRowsFor 返回指定会话的**活体工作表格投影**（只读；不重算任何状态）。
- `func (service *Service) ProjectRootFor(sessionID string) string` — ProjectRootFor 返回会话所属工作区的**项目根**（绝对路径；"" = 未绑定/无落点）。
- `func (service *Service) ProjectBoardTeamPlanFor(sessionID string) *store.TeamworkPlan` — ProjectBoardTeamPlanFor 按**窄可选端口**取 teamwork 计划（未装配 / 该会话没有计划 ⇒ nil）。
- `func (service *Service) ProjectBoardTeamStateFor(sessionID string) *store.TeamworkState` — ProjectBoardTeamStateFor 取该会话的 teamwork **运行态**（里程碑结论 / 整队收口）。
- `func (state *serviceState) taskRecordsForSession(sessionID, viewSessionID string) []dto.TaskRecord` — taskRecordsForSession 按会话取 task 注册表快照：视图/活跃会话 = 实时注册表（"" 分区），

### project_board_read_test.go

- `func newBoardTestService(t *testing.T, options ...testServiceOption) (*Service, *fakeRuntime, string)`
- `func TestBuildProjectBoardRowsMatchesLiveProjection(t *testing.T)`
- `func TestProjectBoardIsNotAnInput(t *testing.T)`
- `func marshalWorkItems(t *testing.T, items []model.WorkItem) string`
- `func TestProjectBoardReadersForAssembly(t *testing.T)`
- `func (runtime *boardPlanRuntime) TeamworkPlanFor(sessionID string) (store.TeamworkPlan, bool)`
- `func (sink *recordingBoardSink) OnToolComplete(name string, err error, sessionID string)`
- `func (sink *recordingBoardSink) snapshot() []boardSinkCall`
- `func TestProjectBoardHooksOnExistingSyncPoint(t *testing.T)`

### work_table.go

- `func (state *serviceState) refreshWorkTableLocked(tasks []dto.TaskRecord, asyncRuns []dto.AsyncRunRecord)` — refreshWorkTableLocked 在 service.ViewMu 持锁时重建工作表格投影。
- `func (state *serviceState) publishWorkTable(revision uint64, requestID string, items []model.WorkItem, batches []model.WorkTableBatch)` — publishWorkTable 在锁外发布整表（CSP 汇聚发布器，latest-wins；items 必须
- `func (service *Service) workTableEventPayload(update worktable.WorkTableUpdate) model.WorkTableEvent` — workTableEventPayload 组装 worktable.changed 的 payload：表格 + 批次头 +
- `func (service *Service) publishTaskChanged(record dto.TaskRecord, revision uint64, requestID, sessionID string)` — publishTaskChanged 发布单 task 增量（task.changed；直发 hub，不汇聚——
- `func (service *Service) publishTaskDeltas()` — publishTaskDeltas 拉取注册表快照，锁内重建 worktable，发布
- `func (service *Service) syncTasksFromSources()` — syncTasksFromSources 同步当前活跃会话的 plan/子代理树到其自身 task scope。
- `func (service *Service) syncTasksFromSourcesFor(sessionID string)` — syncTasksFromSourcesFor 把指定会话的 plan 节点与子代理树生命周期投影进该
- `func (service *Service) sessionActivePlanLocked(sessionID string) *model.PlanState` — sessionActivePlanLocked 返回指定会话当前 plan 投影：活跃会话读 Snapshot 镜像，
- `func (service *Service) syncPlanNodeTask(sessionID string, node model.PlanNode, parentID string)`
- `func (service *Service) syncSubagentTask(sessionID string, node dto.SubAgentTreeNode, parentID string)`
- `func (service *Service) workItemForRecord(record dto.TaskRecord, sessionID string) model.WorkItem` — workItemForRecord 把注册表记录映射为工作表格行，并按 plan 邻接面补齐
- `func (state *serviceState) workTableTraceBlock() string` — workTableTraceBlock 返回当前活跃会话的打点表标记块（活跃会话即
- `func (state *serviceState) workTableTraceBlockFor(sessionID string) string` — workTableTraceBlockFor 返回指定会话的打点表标记块：只含该会话 scope 中
- `func (service *Service) RefreshWorkTableSnapshot()` — RefreshWorkTableSnapshot 是子代理树生命周期变更的被动投影入口（由 CSP
- `func (service *Service) refreshWorkTableFromSources()` — refreshWorkTableFromSources 是被动触发的统一入口：同步 plan/子代理树 →
- `func (service *Service) startLifecycleConsumers()` — startLifecycleConsumers 启动四个消费者 goroutine：子代理树信号 → 刷新
- `func (service *Service) stopLifecycleConsumers()`
- `func (service *Service) consumeSubagentLifecycle(runtime contract.RuntimePort)`
- `func (service *Service) consumePlanNodeEvents(runtime contract.RuntimePort)`
- `func (service *Service) consumeTaskChanges(runtime contract.RuntimePort)`
- `func (service *Service) consumeAsyncRuns(runtime contract.RuntimePort)` — consumeAsyncRuns 是第四个生命周期消费者：后台执行表一有可见变化（派发、终态、
- `func (service *Service) safeLifecycleCall(call func())` — safeLifecycleCall 处理消费者中的 panic：数据竞争/逻辑故障不得静默吞掉
- `func (service *Service) UpdateWorkItemStatus(id, status string) error` — UpdateWorkItemStatus 是工作表格的人工状态更新入口（v1：todo 三态
- `func parseWorkItemID(id string) (kind string, index int, err error)`
- `func (service *Service) refreshRuntimeAfterTodoChange()` — refreshRuntimeAfterTodoChange 在 todo 状态变更后重投影并发布三类增量：
- `func (state *serviceState) asyncRunsForTable() []dto.AsyncRunRecord` — asyncRunsForTable 读后台执行投影。三处建表入口（实时重投影、会话快照、冷读面）

### work_table_ab_test.go

- `func TestWorkTablePayloadSmallerThanFullRuntime(t *testing.T)`
- `func heavyTestPlan(nodes int) *model.PlanState`
- `func heavySubagentTree(rows int) []dto.SubAgentTreeNode`

### work_table_async_test.go

- `func runningAsyncRecord(handle string) dto.AsyncRunRecord`
- `func workItemByID(rows []model.WorkItem, id string) (model.WorkItem, bool)`
- `func TestAsyncRunProjectsEveryVisibleColumn(t *testing.T)` — 列位分配：描述→行标题、指令→描述列、探针读数→打点行、日志路径→附件列。
- `func TestAsyncTerminalStatesMapToWorkStatus(t *testing.T)` — 终态映射：done→completed，failed/killed→failed（表格状态机没有"被杀"这一档）。
- `func TestAsyncTraceLinesCarryNoPathsOrLogContent(t *testing.T)` — 打点块列**在途行 + 待取回的完成行**（打点 K-5 的回填规范），且进上下文的字段必须
- `func TestAsyncRunAloneMaterializesTraceBlock(t *testing.T)` — 整块语义：没有活动任务、只有一条在跑的后台命令时，打点块必须出现（这是
- `func TestAsyncRowDisappearsWhenRegistryDropsIt(t *testing.T)` — 登记表是唯一事实源：它不再报这条记录（终态被驱逐），投影里就没有这行。
- `func TestAsyncChangeSignalReprojectsWorkTable(t *testing.T)` — 第 4 个生命周期消费者：执行域一发声，表格就重投影——不靠模型再调一次工具，
- `func TestAsyncRowsAreCappedAndNeverEvictTasks(t *testing.T)` — 后台行按上限封顶，且不挤掉真实任务行（在途优先，终态按最新补齐）。
- `func TestAsyncBackfillStaysBounded(t *testing.T)` — TC-K5-1（打点 K-5 的"有界"判据）：注入 100 个已完成作业 →
- `func serviceWorkTableRows(service *Service) []model.WorkItem`

### work_table_fuzz_test.go

- `func FuzzBuildWorkTable(f *testing.F)` — FuzzBuildWorkTable 以畸形输入喂投影构建器：深层嵌套、错类型、超大文本

### work_table_plan_deps_test.go

- `func TestWorkTablePlanRowsCarryDAGDependencies(t *testing.T)` — TestWorkTablePlanRowsCarryDAGDependencies 投影面：平铺节点 + 边集 → 行依赖。
- `func TestServicePlanSyncExposesDAGDependencies(t *testing.T)` — TestServicePlanSyncExposesDAGDependencies 走 Service 路径：plan 投影 → task
- `func TestTaskChangedIncrementCarriesPlanDependencies(t *testing.T)` — TestTaskChangedIncrementCarriesPlanDependencies 增量面：task.changed 单行

### work_table_project_scope_test.go

- `func newProjectSwitchHarness(t *testing.T) *projectSwitchHarness`
- `func (harness *projectSwitchHarness) enterProjectA(t *testing.T)` — enterProjectA 在项目 A 里跑一轮（会话 A 成为已加载会话，引擎带历史），并把一条
- `func (harness *projectSwitchHarness) switchToProjectB(t *testing.T)` — switchToProjectB 触发项目切换（startFreshSession：新建独立会话）。
- `func appendProjectRoundsFor(t *testing.T, service *Service, sessionID string, rounds, chars int)` — appendProjectRoundsFor 给**指定会话**追加 rounds 个已定稿轮次（每轮约
- `func TestProjectSwitchDoesNotInjectForeignWorkTableRows(t *testing.T)` — TestProjectSwitchDoesNotInjectForeignWorkTableRows —— 时机①：切换项目后
- `func TestProjectSwitchWorkTableBlockStaysCleanAcrossCompaction(t *testing.T)` — TestProjectSwitchWorkTableBlockStaysCleanAcrossCompaction —— 时机②：切换项目
- `func TestProjectSwitchRebindsTaskRegistryToNewSession(t *testing.T)` — TestProjectSwitchRebindsTaskRegistryToNewSession —— 同一根因的数据面：项目切换
- `func TestProjectSwitchKeepsForeignRowsInLedger(t *testing.T)` — TestProjectSwitchKeepsForeignRowsInLedger —— 边界：工作表格是**项目/全局台账**，
- `func TestProjectSwitchWithoutFreshSessionKeepsOwnRows(t *testing.T)` — TestProjectSwitchWithoutFreshSessionKeepsOwnRows —— 对照：**没有**新建独立会话

### work_table_race_test.go

- `func TestWorkTableRaceConcurrentMutations(t *testing.T)` — TestWorkTableRaceConcurrentMutations 并发执行工作表格三类变更路径：

### work_table_session_axis_test.go

- `func TestWorkItemCarriesOwningSession(t *testing.T)` — 归属会话必须从注册表记录透传到 WorkItem（GUI「会话」列与「仅本会话」筛选
- `func TestGlobalWorkTableCarriesOwningSession(t *testing.T)` — 全局台账行带归属会话：实时注册表 = 当前会话，后台 scope 分区 = 分区键。
- `func TestMergeTaskRecordsStampsSecondarySession(t *testing.T)` — 冷读合并：磁盘记录不带会话标记（归属由 SessionRecord 容器表达），合并时按
- `func TestTaskChangedIncrementCarriesOwningSession(t *testing.T)` — 增量面：task.changed 单行必须带归属会话（与整表路径 taskSnapshotAll 同源

### work_table_session_scope_test.go

- `func TestS1BackgroundSessionContextMustNotCarryActiveSessionWorkTable(t *testing.T)`
- `func TestWorkTableTraceBlockForScopesBySession(t *testing.T)`

### work_table_subagent_session_scope_test.go

- `func subagentSessionScopeFixture(t *testing.T) (*fakeRuntime, *Service)` — subagentSessionScopeFixture 造一个视图会话在 session-a 的服务，并把"进程级
- `func TestSubagentRowsStayOutOfActiveRegistryOfOtherSessions(t *testing.T)` — 视图路径：活跃会话同步只收自己的子代理行，别的会话的行不得进实时注册表。
- `func TestBackgroundSyncKeepsForeignSubagentRowsOut(t *testing.T)` — 后台路径：为 session-a 后台同步时，树里**别的会话**的子代理不得写进 A 的分区。
- `func TestSubagentWorkItemCarriesOwningSession(t *testing.T)` — 归位后的行必须带上归属会话键（前端会话轴的唯一数据源）；缺键的行会被前端
- `func hasTaskKey(records []dto.TaskRecord, key string) bool`

### work_table_subagent_status_test.go

- `func TestTaskStatusForSubagentInterrupted(t *testing.T)` — TestTaskStatusForSubagentInterrupted（G4 stale）：崩溃遗留节点经树恢复为

### work_table_test.go

- `func TestBuildWorkTableMapsPlanNodes(t *testing.T)`
- `func TestBuildWorkTableTasklistModeMarksCheckNode(t *testing.T)`
- `func TestBuildWorkTableMapsTodoItems(t *testing.T)`
- `func TestBuildWorkTableBatches(t *testing.T)` — TestBuildWorkTableBatches 验证批次分片：按 BatchID 分组、按 CreatedAt
- `func TestTaskRecordToWorkItemCarriesBatch(t *testing.T)` — TestTaskRecordToWorkItemCarriesBatch 验证批次字段透传（task.changed 单行
- `func TestBuildWorkTableMapsSubagentTasks(t *testing.T)`
- `func TestBuildWorkTableBoundsRowsAndTruncatesEvidence(t *testing.T)`
- `func TestUpdateWorkItemStatusTodoThreeStates(t *testing.T)` — TestUpdateWorkItemStatusTodoThreeStates 走完整 Service 路径：
- `func TestUpdateWorkItemStatusRejectsInvalid(t *testing.T)`
- `func TestRefreshWorkTableSnapshotPublishesSubagentRows(t *testing.T)` — TestRefreshWorkTableSnapshotPublishesSubagentRows 验证被动触发：
- `func waitForWorkTableEvent(t testing.TB, subscription protocol_event.Subscription) model.WorkTableEvent`
- `func TestWorkTableEventCarriesSubagentTreeOnChange(t *testing.T)` — TestWorkTableEventCarriesSubagentTreeOnChange 验证详情入口的数据面：子代理
