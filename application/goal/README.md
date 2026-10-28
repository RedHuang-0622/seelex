# application/goal

## 生态位

`application/goal` 承载 Goal 用例路由、会话级 Coordinator 缓存与会话治理视图。真实调用方是
`application/assembly.Service` 的 Goal 兼容入口；Goal 状态机仍在
`kernel/goal`。

## 职责与非职责

职责：

- 校验 session ID 并定位会话 Goal 协调器；
- 路由 Begin/Update/Done/Status 与治理视图；
- 按会话缓存 Goal Controller，并通过第五栈存储重载/释放；
- 从看板存档恢复治理视图；
- 成功后刷新运行时投影。

非职责：

- 不拥有 Goal 记录、栈或状态迁移；
- 不实现 Goal 记录、栈或状态迁移，也不直接读取 Runtime 投影源；
- 不导入 `application/assembly`、`store` 或具体存储包。

## 文件结构

- `application.go`：`CoordinatorPort`、`Deps`、`Application` 与构造器。
- `coordinator.go`：按会话缓存 Goal Controller、第五栈适配、看板存档恢复与
  `StackFrames` 投影。
- `application_test.go`：成功刷新、错误短路和缺失协调器测试。
- `stack_view_test.go`、`done_test.go`、`board_archive_read_test.go`：随协调器迁移的
  栈投影、直接收口与存档恢复测试。

## 核心实现

`CoordinatorPort` 只暴露 Goal 应用实际调用的五个方法；`Coordinator`
通过结构满足 port，不需要复制状态机。刷新只发生在成功变更后。Controller 仍是
每会话 bundle 的唯一状态所有者；`StoreFor=nil` 保持内存态降级。
`StoreFor` 返回 Goal 域自己的 `Store` / `AuditAccount`，`ArchiveFor` 返回已投影的
`dto.GoalGovernanceView`。具体 Store 构造和看板存档解码由 Wiring 注入。

## 数据流

兼容 Service 入口 → `goal.Application` → Goal coordinator → Goal domain；
成功后刷新 Runtime projection，锁与具体实现由宿主提供。

## 依赖方向

允许依赖 `kernel/goal` 的 Goal 领域接口与 `contract/dto`。禁止依赖
`store`、`application/assembly`、GUI/TUI；Store 适配只在 `internal/host` 装配。

## 并发与错误

Coordinator 用独立 mutex 保护每会话 bundle 缓存，bundle 内 Controller 不与其它会话
共享。协调器缺失返回 `ErrCoordinatorUnavailable`，空 session ID 显式失败，变更失败
时不刷新投影。

## 扩展方式

新增 Goal 应用入口时扩展 `CoordinatorPort` 的真实能力；不得把 Goal 状态迁移复制进
本包。兼容 Service 入口只保留一行转发。

## Review 指南

- 协调器是否仍由 Goal domain 唯一拥有；
- 本包生产文件是否仍不 import `store`；
- 变更成功后才刷新 projection；
- session ID 是否 trim/required；
- 是否 import `application/assembly`。

## 测试与验证

```text
go test ./application/goal -count=1
go test ./application/assembly -run 'Goal' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### application.go

- `func New(deps Deps) *Application` — New constructs the Goal application.
- `func (application *Application) coordinatorFor(sessionID string) (CoordinatorPort, error)`
- `func (application *Application) GovernanceViewFor(sessionID string) *dto.GoalGovernanceView` — GovernanceViewFor returns the session's goal governance projection.
- `func (application *Application) BeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — BeginFor begins a session goal.
- `func (application *Application) UpdateFor(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)` — UpdateFor updates a session goal.
- `func (application *Application) DoneFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error)` — DoneFor finishes or aborts a session goal.
- `func (application *Application) StatusFor(sessionID string) (goaldomain.StatusView, error)` — StatusFor returns the session goal status.
- `func (application *Application) BeginHandler(ctx context.Context, argsJSON string) (string, error)` — BeginHandler adapts the goal_begin model tool.
- `func (application *Application) UpdateHandler(ctx context.Context, argsJSON string) (string, error)` — UpdateHandler adapts the goal_update model tool.
- `func (application *Application) DoneHandler(ctx context.Context, argsJSON string) (string, error)` — DoneHandler adapts the goal_done model tool.
- `func (application *Application) StatusHandler(ctx context.Context, _ string) (string, error)` — StatusHandler adapts the goal_status model tool.
- `func (application *Application) writeSessionID(ctx context.Context) string`
- `func (application *Application) readSessionID(ctx context.Context) string`
- `func marshalResult(value any) (string, error)`

### application_test.go

- `func (coordinator *coordinatorFake) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView`
- `func (coordinator *coordinatorFake) Begin(context.Context, string, goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)`
- `func (coordinator *coordinatorFake) Update(context.Context, string, goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)`
- `func (coordinator *coordinatorFake) FinishDirect(context.Context, string, goaldomain.FinishRequest, bool) (*goaldomain.GoalRecord, error)`
- `func (coordinator *coordinatorFake) StatusFor(string) goaldomain.StatusView`
- `func TestGoalApplicationRoutesAndRefreshesOnSuccess(t *testing.T)`
- `func TestGoalHandlersRejectMalformedInput(t *testing.T)`
- `func TestGoalApplicationDoesNotRefreshOnErrors(t *testing.T)`
- `func TestGoalApplicationRequiresCoordinatorAndSession(t *testing.T)`

### board_archive_read_test.go

- `func boardRouterForArchive(t *testing.T) (*store.Router, *storeboard.SessionStore)` — boardRouterForArchive 建一个临时会话存储（看板存档落在其 metadata 目录）。
- `func boardDepsFor(router *store.Router, boardStore *storeboard.SessionStore) CoordinatorDeps` — boardDepsFor mirrors the Wiring adapter without making the production Goal
- `func writeBoardArchiveFor(t *testing.T, boardStore *storeboard.SessionStore, sessionID string, meta store.GoalBoardMeta)` — writeBoardArchiveFor 直接发布一份看板存档（构造"只有存档、没有活体栈"的现场）。
- `func TestGoalGovernanceViewRecoversBoardFromArchive(t *testing.T)` — TestGoalGovernanceViewRecoversBoardFromArchive 验证兜底读：本次进程还没碰过该
- `func TestGoalGovernanceViewRetiresBoardOnClosedArchive(t *testing.T)` — TestGoalGovernanceViewRetiresBoardOnClosedArchive 验证"看板退场"：存档 state=closed
- `func TestGoalGovernanceViewCarriesHistoryLedgerWhileLive(t *testing.T)` — TestGoalGovernanceViewCarriesHistoryLedgerWhileLive 验证账本与活动栈**正交**：
- `func TestGoalGovernanceViewWithoutArchiveStaysHidden(t *testing.T)` — TestGoalGovernanceViewWithoutArchiveStaysHidden 验证未装配/无存档时的降级：

### coordinator.go

- `func NewCoordinator(deps CoordinatorDeps) *Coordinator` — NewCoordinator constructs the session goal coordinator.
- `func (g *Coordinator) ForgetSession(sessionID string)` — ForgetSession 释放一个会话的 goal 运行时（会话释放面调用）：丢掉按会话缓存
- `func (g *Coordinator) bundleFor(sessionID string) *sessionRuntime` — bundleFor 返回（需要时创建）指定会话的 goal bundle。
- `func (g *Coordinator) Begin(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)` — Begin 注册并压栈（会话路由）。
- `func (g *Coordinator) Update(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)` — Update 更新栈顶（会话路由）。
- `func (g *Coordinator) FinishDirect(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error)` — FinishDirect 直接落终态收口（goal_done 的领域动作）：把栈顶 goal 标
- `func (g *Coordinator) StatusFor(sessionID string) goaldomain.StatusView` — StatusFor 返回会话 goal 栈全量视图（无 bundle 时返回空视图）。
- `func StackFrames(stack []*goaldomain.GoalRecord) []dto.GoalFrameView` — StackFrames 把 Controller 的活动栈投影成逐帧只读视图（栈底→栈顶，末元素
- `func (g *Coordinator) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView` — GoalGovernanceViewFor 组装只读治理视图（无 bundle/无 goal/无存档 → nil）。
- `func (g *Coordinator) readGoalBoardArchive(sessionID string) (dto.GoalGovernanceView, bool)` — readGoalBoardArchive 读取该会话的看板存档（缺失/损坏 → false）。

### done_test.go

- `func TestGoalDoneFinishesDirectly(t *testing.T)`

### stack_view_test.go

- `func TestGoalStackFramesProjectsEveryFrame(t *testing.T)`
- `func TestGoalStackFramesCarriesDetailFields(t *testing.T)` — TestGoalStackFramesCarriesDetailFields 钉住"点开看详情"需要的字段不缺：
- `func TestGoalStackFramesEmptyAndNilRecords(t *testing.T)`
