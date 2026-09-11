# core/work-table（根包分卷）

## 生态位

工作表格投影与测试

覆盖：`work_table*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### work_table.go

- `func buildWorkTable(plan *PlanState, tasks []dto.TaskRecord, subagentTree []dto.SubAgentTreeNode) []WorkItem` — buildWorkTable 组装工作表格行：注册表 task → WorkItem；plan 行额外合并
- `func taskRecordToWorkItem(record dto.TaskRecord) WorkItem` — taskRecordToWorkItem 把注册表 task 快照映射为 WorkItem（含 retry 计数）。
- `func batchLabel(id string, createdAt time.Time) string` — batchLabel 由批次 ID 与创建时间派生展示标签：真实批次用本地时间
- `func buildWorkTableBatches(rows []WorkItem) []WorkTableBatch` — buildWorkTableBatches 从工作表格行派生批次分片头：按 BatchID 分组，
- `func planNodeTrace(node PlanNode, tasklistMode bool) []WorkTracePoint` — planNodeTrace 由节点事件 + 子代理工具活动合成打点（按时间倒序、有界；
- `func boundWorkTrace(points []WorkTracePoint) []WorkTracePoint` — boundWorkTrace 按时间倒序排序并截断。
- `func truncateWorkEvidence(value string, limit int) string`
- `func formatWorkDuration(duration time.Duration) string`
- `func (state *serviceState) refreshWorkTableLocked(tasks []dto.TaskRecord)` — refreshWorkTableLocked 在 service.ViewMu 持锁时重建工作表格投影。
- `func (state *serviceState) publishWorkTable(revision uint64, requestID string, items []WorkItem, batches []WorkTableBatch)` — publishWorkTable 在锁外发布整表（CSP 汇聚发布器，latest-wins；items 必须
- `func (service *Service) publishTaskChanged(record dto.TaskRecord, revision uint64, requestID, sessionID string)` — publishTaskChanged 发布单 task 增量（task.changed；直发 hub，不汇聚——
- `func (service *Service) publishTaskDeltas()` — publishTaskDeltas 拉取注册表快照，锁内重建 worktable，发布
- `func (service *Service) syncTasksFromSources()` — syncTasksFromSources 同步当前活跃会话的 plan/子代理树到其自身 task scope。
- `func (service *Service) syncTasksFromSourcesFor(sessionID string)` — syncTasksFromSourcesFor 把指定会话的 plan 节点与子代理树生命周期投影进该
- `func (service *Service) sessionActivePlanLocked(sessionID string) *PlanState` — sessionActivePlanLocked 返回指定会话当前 plan 投影：活跃会话读 Snapshot 镜像，
- `func (service *Service) syncPlanNodeTask(sessionID string, node PlanNode, parentID string)`
- `func (service *Service) syncSubagentTask(sessionID string, node dto.SubAgentTreeNode, parentID string)`
- `func taskStatusForNode(status NodeStatus) dto.TaskStatus`
- `func taskStatusForSubagent(status dto.SubAgentNodeStatus) dto.TaskStatus`
- `func (state *serviceState) workTableTraceBlock() string` — workTableTraceBlock 返回当前活跃会话的打点表标记块（活跃会话即
- `func (state *serviceState) workTableTraceBlockFor(sessionID string) string` — workTableTraceBlockFor 返回指定会话的打点表标记块：只含该会话 scope 中
- `func clonePlanForSync(plan *PlanState) *PlanState`
- `func cloneSubAgentTreeForSync(nodes []dto.SubAgentTreeNode) []dto.SubAgentTreeNode`
- `func (service *Service) RefreshWorkTableSnapshot()` — RefreshWorkTableSnapshot 是子代理树生命周期变更的被动投影入口（由 CSP
- `func (service *Service) refreshWorkTableFromSources()` — refreshWorkTableFromSources 是被动触发的统一入口：同步 plan/子代理树 →
- `func (service *Service) startLifecycleConsumers()` — startLifecycleConsumers 启动三个消费者 goroutine：子代理树信号 → 刷新
- `func (service *Service) stopLifecycleConsumers()`
- `func (service *Service) consumeSubagentLifecycle()`
- `func (service *Service) consumePlanNodeEvents()`
- `func (service *Service) consumeTaskChanges()`
- `func (service *Service) safeLifecycleCall(call func())` — safeLifecycleCall 处理消费者中的 panic：数据竞争/逻辑故障不得静默吞掉
- `func (service *Service) UpdateWorkItemStatus(id, status string) error` — UpdateWorkItemStatus 是工作表格的人工状态更新入口（v1：todo 三态
- `func parseWorkItemID(id string) (kind string, index int, err error)`
- `func (service *Service) refreshRuntimeAfterTodoChange()` — refreshRuntimeAfterTodoChange 在 todo 状态变更后重投影并发布三类增量：

### work_table_ab_test.go

- `func TestWorkTablePayloadSmallerThanFullRuntime(t *testing.T)`
- `func heavyTestPlan(nodes int) *PlanState`
- `func heavySubagentTree(rows int) []dto.SubAgentTreeNode`

### work_table_fuzz_test.go

- `func FuzzBuildWorkTable(f *testing.F)` — FuzzBuildWorkTable 以畸形输入喂投影构建器：深层嵌套、错类型、超大文本

### work_table_race_test.go

- `func TestWorkTableRaceConcurrentMutations(t *testing.T)` — TestWorkTableRaceConcurrentMutations 并发执行工作表格三类变更路径：

### work_table_session_scope_test.go

- `func TestS1BackgroundSessionContextMustNotCarryActiveSessionWorkTable(t *testing.T)`
- `func TestWorkTableTraceBlockForScopesBySession(t *testing.T)`

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
