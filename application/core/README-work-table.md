# core/work-table

## 生态位

工作表格投影与测试

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### work_table.go

- `func buildWorkTable(plan *PlanState, tasks []dto.TaskRecord, subagentTree []dto.SubAgentTreeNode) []WorkItem` — buildWorkTable 组装工作表格行：注册表 task → WorkItem；plan 行额外合并
- `func taskRecordToWorkItem(record dto.TaskRecord) WorkItem` — taskRecordToWorkItem 把注册表 task 快照映射为 WorkItem（含 retry 计数）。
- `func planNodeTrace(node PlanNode, tasklistMode bool) []WorkTracePoint` — planNodeTrace 由节点事件 + 子代理工具活动合成打点（按时间倒序、有界；
- `func boundWorkTrace(points []WorkTracePoint) []WorkTracePoint` — boundWorkTrace 按时间倒序排序并截断。
- `func truncateWorkEvidence(value string, limit int) string`
- `func formatWorkDuration(duration time.Duration) string`
- `func (state *serviceState) refreshWorkTableLocked(tasks []dto.TaskRecord)` — refreshWorkTableLocked 在 service.Mu 持锁时重建工作表格投影。
- `func (state *serviceState) publishWorkTable(revision uint64, requestID string, items []WorkItem)` — publishWorkTable 在锁外发布整表（CSP 汇聚发布器，latest-wins；items 必须
- `func (state *serviceState) publishTaskChanged(record dto.TaskRecord, revision uint64, requestID string)` — publishTaskChanged 发布单 task 增量（task.changed；直发 hub，不汇聚——
- `func (service *Service) publishTaskDeltas()` — publishTaskDeltas 拉取注册表快照，锁内重建 worktable，发布
- `func (service *Service) syncTasksFromSources()` — syncTasksFromSources 把 plan 节点与子代理树的生命周期投影进 task 注册表
- `func (service *Service) syncPlanNodeTask(node PlanNode, parentID string)`
- `func (service *Service) syncSubagentTask(node dto.SubAgentTreeNode, parentID string)`
- `func taskStatusForNode(status NodeStatus) dto.TaskStatus`
- `func taskStatusForSubagent(status dto.SubAgentNodeStatus) dto.TaskStatus`
- `func (state *serviceState) workTableTraceBlock() string` — workTableTraceBlock 构建打点表标记块：只含未终态任务
- `func clonePlanForSync(plan *PlanState) *PlanState`
- `func cloneSubAgentTreeForSync(nodes []dto.SubAgentTreeNode) []dto.SubAgentTreeNode`
- `func (service *Service) RefreshWorkTableSnapshot()` — RefreshWorkTableSnapshot 是子代理树生命周期变更的被动投影入口（由 CSP
- `func (service *Service) refreshWorkTableFromSources()` — refreshWorkTableFromSources 是被动触发的统一入口：同步 plan/子代理树 →
- `func (service *Service) startLifecycleConsumers()` — startLifecycleConsumers 启动三个消费者 goroutine：子代理树信号 → 刷新
- `func (service *Service) stopLifecycleConsumers()`
- `func (service *Service) consumeSubagentLifecycle()`
- `func (service *Service) consumePlanNodeEvents()`
- `func (service *Service) consumeTaskChanges()`
- `func (service *Service) safeLifecycleCall(call func())` — safeLifecycleCall 隔离消费者中的单次 panic（记录并继续，避免消费者
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

### work_table_test.go

- `func TestBuildWorkTableMapsPlanNodes(t *testing.T)`
- `func TestBuildWorkTableTasklistModeMarksCheckNode(t *testing.T)`
- `func TestBuildWorkTableMapsTodoItems(t *testing.T)`
- `func TestBuildWorkTableMapsSubagentTasks(t *testing.T)`
- `func TestBuildWorkTableBoundsRowsAndTruncatesEvidence(t *testing.T)`
- `func TestUpdateWorkItemStatusTodoThreeStates(t *testing.T)` — TestUpdateWorkItemStatusTodoThreeStates 走完整 Service 路径：
- `func TestUpdateWorkItemStatusRejectsInvalid(t *testing.T)`
- `func TestRefreshWorkTableSnapshotPublishesSubagentRows(t *testing.T)` — TestRefreshWorkTableSnapshotPublishesSubagentRows 验证被动触发：
- `func waitForWorkTableEvent(t testing.TB, subscription Subscription) WorkTableEvent`
