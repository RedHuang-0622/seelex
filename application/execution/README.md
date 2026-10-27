# application/execution

## 生态位

`application/execution` 承载执行域、项目看板、WorkItem/teammate、Subagent 与
Role Tool Activity 的 Application 用例。真实调用方是
`application/compat.Frontend` 与看板装配入口。

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

### `board_reader.go`

- `type BoardReader interface` — WorkItem 消费的团队看板与 teammate live 只读投影。

### `project_board.go`

- `func NewProjectBoard(deps ProjectBoardDeps) *ProjectBoardApplication` — 构造项目看板读应用。
- `func (application *ProjectBoardApplication) WorkTableRowsFor(sessionID string) []model.WorkItem` — 返回会话活体 WorkTable。
- `func (application *ProjectBoardApplication) ProjectRootFor(sessionID string) string` — 读取会话绑定项目根。
- `func (application *ProjectBoardApplication) ProjectBoardTeamPlanFor(sessionID string) *dto.TeamworkPlan` — 读取可选团队计划。
- `func (application *ProjectBoardApplication) ProjectBoardTeamStateFor(sessionID string) *dto.TeamworkState` — 从计划提取运行态。

### `work_item.go`

- `func NewWorkItem(deps WorkItemDeps) *WorkItemApplication` — 构造 WorkItem/teammate 应用。
- `func (application *WorkItemApplication) WorkItemExecution(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error)` — 读取 WorkItem 执行视图。
- `func (application *WorkItemApplication) TeammateInspection(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error)` — 读取 teammate 安全详情。
- `func (application *WorkItemApplication) BoardView(sessionID string) *dto.TeamworkBoardView` — 读取团队看板。
- `func (application *WorkItemApplication) SessionLive(sessionID string) dto.TeammateSessionLiveView` — 读取 teammate 实时页。
- `func (application *WorkItemApplication) SessionPage(sessionID string, offset, limit int) dto.TeammateSessionLiveView` — 读取分页实时页。
- `func (application *WorkItemApplication) UpdateWorkItemStatus(id, status string) error` — 仅修改 todo 状态并刷新投影。

### `subagent.go`

- `func NewSubagent(deps SubagentDeps) *SubagentApplication` — 构造子代理应用。
- `func (application *SubagentApplication) ListRecovery(sessionID string) ([]dto.SubagentRecoveryView, error)` — 列出可恢复子代理。
- `func (application *SubagentApplication) ResumeInterrupted(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error)` — 恢复全部中断单元。
- `func (application *SubagentApplication) Resume(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error)` — 恢复单个子代理。
- `func (application *SubagentApplication) Fork(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error)` — 派发子代理。
- `func (application *SubagentApplication) Detail(nodeID string) (*model.SubagentDetail, error)` — 读取子代理详情。
- `func (application *SubagentApplication) ClearTree() error` — 清空并刷新子代理树。

### `completion.go`

- `func NewCompletion(deps CompletionDeps) *CompletionApplication` — 构造后台完成触发应用。
- `func (application *CompletionApplication) TriggerAsyncCompletions(triggered map[string]struct{}) int` — 扫描异步作业终态并唤醒空闲会话。
- `func (application *CompletionApplication) TriggerTeamworkJobCompletions(triggered map[string]struct{}) int` — 扫描 teammate 作业终态并唤醒空闲会话。

### `project_board_test.go`

测试文件按仓库 README 规范不展开函数索引。
