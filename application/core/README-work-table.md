# core/work-table（根包分卷）

## 生态位

后台作业面与工作表格投影：执行登记表的只读投影、请求尾部打点块（在途行 + 待取回的完成行）、作业终态为**空闲**会话触发对话

覆盖：`work_table*.go`、`async*.go`；未归属文件由覆盖自检拦下。

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
  的完成行补一行汇总，不静默消失。模型 `job_manage(op=fetch)` 取回（或 `op=done` 销项）
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

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### async_completion.go

- `func (service *Service) triggerAsyncCompletions(triggered map[string]struct{})` — triggerAsyncCompletions 对登记表做一次全量扫描：把"终态 + 会话空闲 + 还没触发过"
- `func asyncCompletionTriggers(state string) bool` — asyncCompletionTriggers 报告某个状态是否该触发对话。
- `func (service *Service) sessionIdleForAsyncTrigger(sessionID string) bool` — sessionIdleForAsyncTrigger 报告目标会话此刻是否**空闲到可以起一个新回合**。
- `func asyncCompletionPrompt(record dto.AsyncRunRecord) string` — asyncCompletionPrompt 组装触发回合的正文。
- `func asyncPromptKind(kind string) string` — asyncPromptKind 是进正文前的类别兜底：类别缺失时按 process 读，与打点块同口径。
- `func (service *Service) triggerTeamworkJobCompletions(triggered map[string]struct{})` — triggerTeamworkJobCompletions 对 teammate 作业表做一次全量扫描：把"终态 + 会话空闲 +
- `func teamworkJobCompletions(runtime contract.RuntimePort) []dto.TeamworkJobCompletionRecord` — teamworkJobCompletions 读窄可选端口（未装配 teamwork 的宿主 = nil）。
- `func teamworkJobEvents(runtime contract.RuntimePort) <-chan struct` — teamworkJobEvents 取 teammate 作业表的变化信号口（未装配 = nil；nil 通道在 select 里
- `func teamworkCompletionPrompt(record dto.TeamworkJobCompletionRecord) string` — teamworkCompletionPrompt 组装 teammate 完成回执的正文。

### async_completion_trigger_test.go

- `func asyncCompletionHarness(t *testing.T, trigger bool, engine ChatEngine) (*Service, *fakeRuntime)` — asyncCompletionHarness 造一个装配好该触发路径的 Service。
- `func waitForTriggeredTurn(t *testing.T, service *Service, needle string) string` — waitForTriggeredTurn 轮询可见会话，直到出现一条含 needle 的用户行（= 触发回合开
- `func assertNoTurn(t *testing.T, service *Service, needle string)` — assertNoTurn 报告可见会话里有没有含 needle 的用户行——"不该触发"的用例用它。
- `func completedRecord(handle, state string) dto.AsyncRunRecord` — completedRecord 造一条已落到终态的作业投影记录。
- `func TestAsyncCompletionTriggersIdleSessionTurn(t *testing.T)`
- `func TestAsyncCompletionTriggersOnFailureToo(t *testing.T)`
- `func TestAsyncCompletionIgnoresRunningAndKilled(t *testing.T)`
- `func TestAsyncCompletionDoesNotWakeBusySession(t *testing.T)` — 铁律 §6.1「绝不唤醒忙会话」：正在跑的会话不被打断、也不被塞队列——条目刻意不记账，
- `func TestAsyncCompletionTriggersOncePerHandle(t *testing.T)`
- `func TestAsyncCompletionStaysOffWhenDisabled(t *testing.T)` — 开关默认关：不置 trigger_conversation 时信号只驱动工作表格重投影，终态不会起任何回合。
- `func TestAsyncCompletionIgnoresJobWithoutSession(t *testing.T)` — 没有会话归属的作业（别的进程的作业归属、或会话已被删除）不该触发：没有可起的回合

### work_table.go

- `func buildWorkTable(plan *PlanState, tasks []dto.TaskRecord, subagentTree []dto.SubAgentTreeNode, asyncRuns []dto.AsyncRunRecord) []WorkItem` — buildWorkTable 组装工作表格行：注册表 task → WorkItem；plan 行额外合并
- `func taskRecordToWorkItem(record dto.TaskRecord) WorkItem` — taskRecordToWorkItem 把注册表 task 快照映射为 WorkItem（含 retry 计数）。
- `func batchLabel(id string, createdAt time.Time) string` — batchLabel 由批次 ID 与创建时间派生展示标签：真实批次用本地时间
- `func buildWorkTableBatches(rows []WorkItem) []WorkTableBatch` — buildWorkTableBatches 从工作表格行派生批次分片头：按 BatchID 分组，
- `func planNodeTrace(node PlanNode, tasklistMode bool) []WorkTracePoint` — planNodeTrace 由节点事件 + 子代理工具活动合成打点（按时间倒序、有界；
- `func boundWorkTrace(points []WorkTracePoint) []WorkTracePoint` — boundWorkTrace 按时间倒序排序并截断。
- `func truncateWorkEvidence(value string, limit int) string`
- `func formatWorkDuration(duration time.Duration) string`
- `func (state *serviceState) refreshWorkTableLocked(tasks []dto.TaskRecord, asyncRuns []dto.AsyncRunRecord)` — refreshWorkTableLocked 在 service.ViewMu 持锁时重建工作表格投影。
- `func (state *serviceState) publishWorkTable(revision uint64, requestID string, items []WorkItem, batches []WorkTableBatch)` — publishWorkTable 在锁外发布整表（CSP 汇聚发布器，latest-wins；items 必须
- `func (service *Service) workTableEventPayload(update worktable.WorkTableUpdate) WorkTableEvent` — workTableEventPayload 组装 worktable.changed 的 payload：表格 + 批次头 +
- `func subagentTreePayloadSignature(nodes []dto.SubAgentTreeNode) string` — subagentTreePayloadSignature 生成子代理树投影的内容签名，用于判断
- `func (service *Service) publishTaskChanged(record dto.TaskRecord, revision uint64, requestID, sessionID string)` — publishTaskChanged 发布单 task 增量（task.changed；直发 hub，不汇聚——
- `func (service *Service) publishTaskDeltas()` — publishTaskDeltas 拉取注册表快照，锁内重建 worktable，发布
- `func (service *Service) syncTasksFromSources()` — syncTasksFromSources 同步当前活跃会话的 plan/子代理树到其自身 task scope。
- `func (service *Service) syncTasksFromSourcesFor(sessionID string)` — syncTasksFromSourcesFor 把指定会话的 plan 节点与子代理树生命周期投影进该
- `func (service *Service) sessionActivePlanLocked(sessionID string) *PlanState` — sessionActivePlanLocked 返回指定会话当前 plan 投影：活跃会话读 Snapshot 镜像，
- `func (service *Service) syncPlanNodeTask(sessionID string, node PlanNode, parentID string)`
- `func (service *Service) syncSubagentTask(sessionID string, node dto.SubAgentTreeNode, parentID string)`
- `func taskStatusForNode(status NodeStatus) dto.TaskStatus`
- `func taskStatusForSubagent(status dto.SubAgentNodeStatus) dto.TaskStatus`
- `func planNodeIDFor(record dto.TaskRecord) string` — planNodeIDFor 返回 plan 行对应的 plan 节点 ID：SourceID 优先，缺失时
- `func planDependencies(plan *PlanState, nodeID string) []string` — planDependencies 由 plan 邻接面取节点的前置任务 ID（plan:<前置节点>）。
- `func mergeWorkDependencies(recorded []string, derived []string) []string` — mergeWorkDependencies 合并依赖来源（注册表记录 + plan 邻接面）：去重、
- `func mergeTaskRecords(primary, secondary []dto.TaskRecord, secondarySessionID string) []dto.TaskRecord` — mergeTaskRecords 合并两组 task 记录（按跨会话身份去重，前者优先，稳定
- `func taskRecordLedgerIdentity(record dto.TaskRecord) string` — taskRecordLedgerIdentity 返回记录在跨会话台账里的去重身份（幂等键优先、
- `func (service *Service) workItemForRecord(record dto.TaskRecord, sessionID string) WorkItem` — workItemForRecord 把注册表记录映射为工作表格行，并按 plan 邻接面补齐
- `func (state *serviceState) workTableTraceBlock() string` — workTableTraceBlock 返回当前活跃会话的打点表标记块（活跃会话即
- `func (state *serviceState) workTableTraceBlockFor(sessionID string) string` — workTableTraceBlockFor 返回指定会话的打点表标记块：只含该会话 scope 中
- `func clonePlanForSync(plan *PlanState) *PlanState`
- `func cloneSubAgentTreeForSync(nodes []dto.SubAgentTreeNode) []dto.SubAgentTreeNode`
- `func (service *Service) RefreshWorkTableSnapshot()` — RefreshWorkTableSnapshot 是子代理树生命周期变更的被动投影入口（由 CSP
- `func (service *Service) refreshWorkTableFromSources()` — refreshWorkTableFromSources 是被动触发的统一入口：同步 plan/子代理树 →
- `func (service *Service) startLifecycleConsumers()` — startLifecycleConsumers 启动四个消费者 goroutine：子代理树信号 → 刷新
- `func (service *Service) stopLifecycleConsumers()`
- `func (service *Service) consumeSubagentLifecycle()`
- `func (service *Service) consumePlanNodeEvents()`
- `func (service *Service) consumeTaskChanges()`
- `func (service *Service) consumeAsyncRuns()` — consumeAsyncRuns 是第四个生命周期消费者：后台执行表一有可见变化（派发、终态、
- `func (service *Service) safeLifecycleCall(call func())` — safeLifecycleCall 处理消费者中的 panic：数据竞争/逻辑故障不得静默吞掉
- `func (service *Service) UpdateWorkItemStatus(id, status string) error` — UpdateWorkItemStatus 是工作表格的人工状态更新入口（v1：todo 三态
- `func parseWorkItemID(id string) (kind string, index int, err error)`
- `func (service *Service) refreshRuntimeAfterTodoChange()` — refreshRuntimeAfterTodoChange 在 todo 状态变更后重投影并发布三类增量：

### work_table_ab_test.go

- `func TestWorkTablePayloadSmallerThanFullRuntime(t *testing.T)`
- `func heavyTestPlan(nodes int) *PlanState`
- `func heavySubagentTree(rows int) []dto.SubAgentTreeNode`

### work_table_async.go

- `func (state *serviceState) asyncRunsForTable() []dto.AsyncRunRecord` — asyncRunsForTable 读后台执行投影。三处建表入口（实时重投影、会话快照、冷读面）
- `func asyncWorkItems(records []dto.AsyncRunRecord) []WorkItem` — asyncWorkItems 把后台执行全量投影成工作表格行（跨会话：工作表格是全局台账，
- `func asyncRunToWorkItem(record dto.AsyncRunRecord) WorkItem` — asyncRunToWorkItem 映射一条后台执行到工作表格行。
- `func asyncProbePoint(record dto.AsyncRunRecord, elapsed time.Duration) WorkTracePoint` — asyncProbePoint 是一次探针采样的打点行：状态、字节数、末行、耗时。
- `func asyncWorkStatus(state string) string` — asyncWorkStatus 把执行域状态映射到工作表格的权威状态字面量。
- `func asyncTraceLines(records []dto.AsyncRunRecord, sessionID string) []string` — asyncTraceLines 生成打点块里的作业行，且只取本会话——打点块注入在组装请求的那个
- `func asyncTraceLine(record dto.AsyncRunRecord) string` — asyncTraceLine 渲染一条作业行：句柄、类别、状态、（完成行）摘要或（在途行）标题。
- `func teamworkTraceLines(records []dto.TeamworkJobCompletionRecord, sessionID string) []string` — teamworkTraceLines 生成打点块里的 **teammate 作业行**（与 asyncTraceLines 同一块、
- `func teamworkTraceLine(record dto.TeamworkJobCompletionRecord) string` — teamworkTraceLine 渲染一条 teammate 作业行：句柄、状态、归属（teammate/工作项）、
- `func teamworkOwnerText(record dto.TeamworkJobCompletionRecord) string` — teamworkOwnerText 是 teammate 行的归属文本（`<role>/<work item>`）。
- `func formatAsyncBytes(bytes int64) string` — formatAsyncBytes 把字节数写成便于扫读的量级（界面与打点块共用一个口径）。

### work_table_async_test.go

- `func runningAsyncRecord(handle string) dto.AsyncRunRecord`
- `func workItemByID(rows []WorkItem, id string) (WorkItem, bool)`
- `func TestAsyncRunProjectsEveryVisibleColumn(t *testing.T)` — 列位分配：描述→行标题、指令→描述列、探针读数→打点行、日志路径→附件列。
- `func TestAsyncTerminalStatesMapToWorkStatus(t *testing.T)` — 终态映射：done→completed，failed/killed→failed（表格状态机没有"被杀"这一档）。
- `func TestAsyncTraceLinesCarryNoPathsOrLogContent(t *testing.T)` — 打点块列**在途行 + 待取回的完成行**（打点 K-5 的回填规范），且进上下文的字段必须
- `func TestAsyncRunAloneMaterializesTraceBlock(t *testing.T)` — 整块语义：没有活动任务、只有一条在跑的后台命令时，打点块必须出现（这是
- `func TestAsyncRowDisappearsWhenRegistryDropsIt(t *testing.T)` — 登记表是唯一事实源：它不再报这条记录（终态被驱逐），投影里就没有这行。
- `func TestAsyncChangeSignalReprojectsWorkTable(t *testing.T)` — 第 4 个生命周期消费者：执行域一发声，表格就重投影——不靠模型再调一次工具，
- `func TestAsyncRowsAreCappedAndNeverEvictTasks(t *testing.T)` — 后台行按上限封顶，且不挤掉真实任务行（在途优先，终态按最新补齐）。
- `func TestAsyncBackfillStaysBounded(t *testing.T)` — TC-K5-1（打点 K-5 的"有界"判据）：注入 100 个已完成作业 →
- `func serviceWorkTableRows(service *Service) []WorkItem`

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
- `func waitForWorkTableEvent(t testing.TB, subscription Subscription) WorkTableEvent`
- `func TestWorkTableEventCarriesSubagentTreeOnChange(t *testing.T)` — TestWorkTableEventCarriesSubagentTreeOnChange 验证详情入口的数据面：子代理
