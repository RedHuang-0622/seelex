# application/execution

## 生态位

`application/execution` 承载执行域、项目看板、WorkItem/teammate、Subagent 与
Role Tool Activity 的 Application 用例。真实调用方是
GUI/headless 的消费方窄端口、assembly 组件快照与看板装配入口。

## 职责与非职责

职责：

- 按会话组合 WorkTable 活体输入并调用唯一投影器；
- 读取会话绑定项目根；
- 通过窄端口读取 Teamwork Plan 并提取运行态；
- 委托 WorkItem execution、teammate inspection 与实时会话读取；
- 编排子代理恢复、fork、详情和树清理。
- 校验 teammate 工具活动路由、截断证据并按 started/completed 发布会话事件。
- 保持项目看板查询不缓存第二份事实。

非职责：

- 不执行任务、不写 Task/Goal/Teamwork 状态；
- 不扫描或写入项目看板文件；
- 不导入 `application/assembly`，不持完整 `Service`。

## 文件结构

- `project_board.go`：`ProjectBoardDeps`、`ProjectBoardApplication` 与构造器。
- `work_item.go`：WorkItem/teammate 读取与 todo 状态变更。
- `board_reader.go`：`BoardReader`，WorkItem 消费的团队看板与 teammate live 只读端口。
- `subagentview/`：子代理详情、live 事件与 Plan 节点投影 owner。
- `kernel/board`：ProjectBoardSink、TeamworkPlanSource 与看板读写的唯一 owner。
- `subagent.go`：子代理恢复、fork、详情与树清理。
- `completion.go`：异步作业与 teammate 作业终态触发。
- `role_activity.go`：`RoleActivityApplication`，负责任务归属、状态分类、
  ToolRecord stage 与实时事件发布，不拥有 ToolRecord 状态或 EventHub。
- `toolhooks/`：Seele Tool Hook 适配、稳定 ID 配对与诊断。
- `toolrecord/`：工具记录累计、执行单元 stage、失败原因与批量落盘。
- `table/`：WorkTable publisher、latest-wins 汇聚与 payload 去重。
- `project_board_test.go`：输入裁剪、WorkTable 构建、项目根和 Team state 测试。
- `work_item_test.go`：Runtime 执行读取、todo 状态与错误语义测试。
- `subagent_test.go`：子代理恢复、fork、详情与刷新测试。
- `completion_test.go`：终态扫描、忙会话、幂等账与完成正文测试。

## 核心实现

`ProjectBoardDeps` 只接收 WorkTable 输入、限制、项目根和 Team Plan 回调。
`WorkItemDeps` 与 `SubagentDeps` 只接收真实 Runtime/投影能力。
`CompletionDeps` 只接收两张作业表与空闲会话提交回调。
`WorkTableRowsFor` 让 `kernel/worktable.BuildWorkTable` 拥有唯一行投影判据。

## 数据流

兼容 Service 入口 → `ProjectBoardApplication` → 宿主输入的 Plan/Task/SubAgent/Async
投影 → `worktable.BuildWorkTable`；WorkItem/teammate 与 Subagent 入口委托既有
Runtime 和 subagent view coordinator。

Runtime teammate tool activity → `RoleActivityApplication` → EvidenceChars
截断/ToolRecord stage/session event 宿主回调。

## 依赖方向

允许依赖 `contract/dto`、`contract/protocol` 与 `kernel/worktable`。
TeamworkPlan 直接使用 `contract/dto`，本包不再保留类型 alias，也不依赖 `store` 类型。
禁止依赖 `application/assembly`、backend 实现、GUI/TUI 或写入路径。

## 并发与错误

本包不持有锁，也不执行 I/O；WorkTable 输入在宿主提供的锁边界内采集，投影计算使用
输入快照。缺失 Team Plan 时返回 `nil`，不伪造空计划。

## 扩展方式

WorkItem/teammate 等执行查询优先扩展本包的真实端口；不得把 Runtime 或存储实现搬入包，
也不得在 core 复制 WorkTable 构建判据。

## Review 指南

- WorkTable 构建是否仍只有 kernel/worktable 一份；
- project root 是否只来自绑定事实；
- Team state 是否只从 Plan.State 提取且不复用指针；
- 查询是否不获取共享写锁；
- 是否 import `application/assembly`。

## 测试与验证

```text
go test ./application/execution -count=1
go test ./application/assembly -run 'ProjectBoard|WorkTableRowsFor|ProjectRootFor' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### completion.go

- `func NewCompletion(deps CompletionDeps) *CompletionApplication` — NewCompletion constructs the background-completion application.
- `func (application *CompletionApplication) TriggerAsyncCompletions(triggered map[string]struct{}) int` — TriggerAsyncCompletions scans asynchronous jobs once and returns the number
- `func (application *CompletionApplication) TriggerTeamworkJobCompletions(triggered map[string]struct{}) int` — TriggerTeamworkJobCompletions scans teammate jobs once and returns the number
- `func (application *CompletionApplication) asyncCompletionPrompt(record dto.AsyncRunRecord) string`
- `func (application *CompletionApplication) teamworkCompletionPrompt(record dto.TeamworkJobCompletionRecord) string`
- `func (application *CompletionApplication) evidenceChars() int`
- `func asyncPromptKind(kind string) string`

### completion_test.go

- `func TestCompletionTriggersIdleAsyncJobOnce(t *testing.T)`
- `func TestCompletionKeepsBusyAsyncJobPending(t *testing.T)`
- `func TestCompletionPrunesRemovedAsyncHandles(t *testing.T)`
- `func TestCompletionTriggersTeamworkJobWithNamespacedKey(t *testing.T)`
- `func TestCompletionIgnoresNonTerminalAndOrphanJobs(t *testing.T)`

### project_board.go

- `func NewProjectBoard(deps ProjectBoardDeps) *ProjectBoardApplication` — NewProjectBoard constructs the project-board read application.
- `func (application *ProjectBoardApplication) WorkTableRowsFor(sessionID string) []model.WorkItem` — WorkTableRowsFor returns one session's live worktable projection.
- `func (application *ProjectBoardApplication) ProjectRootFor(sessionID string) string` — ProjectRootFor returns the bound workspace root for the session.
- `func (application *ProjectBoardApplication) ProjectBoardTeamPlanFor(sessionID string) *dto.TeamworkPlan` — ProjectBoardTeamPlanFor returns the optional teamwork plan for the session.
- `func (application *ProjectBoardApplication) ProjectBoardTeamStateFor(sessionID string) *dto.TeamworkState` — ProjectBoardTeamStateFor extracts the run state from the authoritative plan.

### project_board_test.go

- `func TestProjectBoardApplicationTrimsInputsAndBuildsLiveRows(t *testing.T)`
- `func TestProjectBoardTeamStateUsesPlanOnly(t *testing.T)`

### role_activity.go

- `func NewRoleActivity(deps RoleActivityDeps) *RoleActivityApplication`
- `func (application *RoleActivityApplication) Handle(activity dto.RoleToolActivity)`

### role_activity_test.go

- `func TestRoleActivityApplicationRoutesStagesAndPublishes(t *testing.T)`

### subagent.go

- `func NewSubagent(deps SubagentDeps) *SubagentApplication` — NewSubagent constructs the subagent application.
- `func (application *SubagentApplication) ListRecovery(sessionID string) ([]dto.SubagentRecoveryView, error)` — ListRecovery lists interrupted subagent recovery entries.
- `func (application *SubagentApplication) ResumeInterrupted(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error)` — ResumeInterrupted resumes all interrupted subagents for one session.
- `func (application *SubagentApplication) Resume(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error)` — Resume resumes one subagent.
- `func (application *SubagentApplication) Fork(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error)` — Fork creates child subagents from fork specifications.
- `func (application *SubagentApplication) Detail(nodeID string) (*model.SubagentDetail, error)` — Detail reads one subagent detail projection.
- `func (application *SubagentApplication) ClearTree() error` — ClearTree clears the Runtime subagent tree and refreshes the projection.

### subagent_test.go

- `func (runtime *subagentRuntimeStub) ListSubagentRecovery(string) ([]dto.SubagentRecoveryView, error)`
- `func (runtime *subagentRuntimeStub) ResumeInterruptedSubagents(context.Context, string) (dto.SubagentResumeReport, error)`
- `func (runtime *subagentRuntimeStub) ResumeSubagent(context.Context, string, string) (dto.SubagentResumeResult, error)`
- `func (runtime *subagentRuntimeStub) ForkSubagents(context.Context, string, []dto.SubagentForkSpec) (string, error)`
- `func (runtime *subagentRuntimeStub) ClearSubagentTree() error`
- `func TestSubagentApplicationDelegatesRuntimeUseCases(t *testing.T)`
- `func TestSubagentApplicationPreservesRuntimeUnavailable(t *testing.T)`

### work_item.go

- `func NewWorkItem(deps WorkItemDeps) *WorkItemApplication` — NewWorkItem constructs the execution read application.
- `func (application *WorkItemApplication) WorkItemExecution(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error)` — WorkItemExecution reads one WorkItem execution view.
- `func (application *WorkItemApplication) TeammateInspection(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error)` — TeammateInspection reads one teammate inspection view.
- `func (application *WorkItemApplication) BoardView(sessionID string) *dto.TeamworkBoardView` — BoardView returns the team board projection.
- `func (application *WorkItemApplication) SessionLive(sessionID string) dto.TeammateSessionLiveView` — SessionLive returns a teammate's live session view.
- `func (application *WorkItemApplication) SessionPage(sessionID string, offset, limit int) dto.TeammateSessionLiveView` — SessionPage returns a bounded teammate live-session page.
- `func (application *WorkItemApplication) UpdateWorkItemStatus(id, status string) error` — UpdateWorkItemStatus updates the only manually mutable WorkItem kind: todo.
- `func parseWorkItemID(id string) (kind string, index int, err error)`

### work_item_test.go

- `func (runtime *workItemRuntimeStub) WorkItemExecution(context.Context, string, string, dto.ConversationViewRequest) (dto.WorkItemExecutionView, error)`
- `func (runtime *workItemRuntimeStub) TeammateInspection(context.Context, string, string) (dto.TeammateInspectionView, error)`
- `func (runtime *workItemRuntimeStub) SetTodoStatus(index int, status dto.TodoItemStatus) error`
- `func TestWorkItemApplicationDelegatesExecutionReads(t *testing.T)`
- `func TestWorkItemApplicationOnlyMutatesTodos(t *testing.T)`
- `func TestWorkItemApplicationPreservesMissingCapabilities(t *testing.T)`
- `func TestParseWorkItemID(t *testing.T)`
