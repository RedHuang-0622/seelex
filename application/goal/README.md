# application/goal

## 生态位

`application/goal` 承载 Goal 用例路由、会话级 Coordinator 缓存与会话治理视图。真实调用方是
`application/core.Service` 的 Goal 兼容入口；Goal 状态机仍在
`backend/multiagent/goal`。

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
- 不导入 `application/core`、`store` 或具体存储包。

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

允许依赖 `backend/multiagent/goal` 的 Goal 领域接口与 `contract/dto`。禁止依赖
`store`、`application/core`、GUI/TUI；Store 适配只在 `wiring/adapters` 装配。

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
- 是否 import `application/core`。

## 测试与验证

```text
go test ./application/goal -count=1
go test ./application/core -run 'Goal' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

### `application.go`

- `func New(deps Deps) *Application` — 构造 Goal 应用。
- `func (application *Application) GovernanceViewFor(sessionID string) *dto.GoalGovernanceView` — 读取 Goal 治理视图。
- `func (application *Application) BeginFor(...)` — 开始 Goal。
- `func (application *Application) UpdateFor(...)` — 更新 Goal。
- `func (application *Application) DoneFor(...)` — 完成或中止 Goal。
- `func (application *Application) StatusFor(sessionID string) (goaldomain.StatusView, error)` — 读取 Goal 状态。

### `application_test.go`

测试文件按仓库 README 规范不展开函数索引。

### `coordinator.go`

- `func NewCoordinator(deps CoordinatorDeps) *Coordinator` — 构造按会话缓存 Goal Controller 的协调器。
- `func (g *Coordinator) ForgetSession(sessionID string)` — 释放一个会话的 Goal bundle。
- `func (g *Coordinator) Begin/Update/FinishDirect/StatusFor(...)` — 按会话路由 Goal 状态机。
- `func (g *Coordinator) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView` — 组装活体或存档恢复的治理视图。
- `func StackFrames(stack []*goaldomain.GoalRecord) []dto.GoalFrameView` — 把活动 Goal 栈投影为逐帧只读视图。
