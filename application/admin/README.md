# application/admin

## 生态位

`application/admin` 是 Seelex Application 层中的账号管理与 Runtime 投影应用包；
定时任务应用位于子包 [`scheduler/`](scheduler/README.md)。当前真实调用方是
`application/assembly.Service` 的兼容入口；GUI/TUI 仍通过原 Application API 调用。

## 职责与非职责

职责：

- 规整账号名称；
- 把账号 CRUD、余额查询和窗口修改转发给窄宿主端口；
- 保留宿主能力缺失时的明确错误；
- 固定账号切换时 projection 采集、视图锁内提交、一次 revision bump 和事件发布顺序。
- 固定 runtime projection 的锁外采集、锁内应用与锁外发布顺序。
- 构造 RuntimeVisibility、ParentEvidence 与父目标证据的三个纯投影值。

非职责：

- 账号 YAML 校验、写入和热注册；
- 供应商余额探测；
- 账号窗口优先级与落盘；
- Scheduler、Runtime、Session 状态机和快照 wire。

## 文件结构

- `application.go`：窄端口 `AccountDeps`、`ProjectionPort` 和 `AccountApplication`。
- `account_window.go`：`AccountWindowPolicy` 消费方端口。
- `runtime.go`：`RuntimeProjectionPort`、`RuntimeDeps` 与 `RuntimeApplication`。
- `projection.go`：RuntimeVisibility、ParentEvidence、LatestVisibleUserGoal 与
  200-rune 截断口径（EP-018 从 `middle/view` 归位）。
- `scheduler/`：定时任务 CRUD、运行装配与后台会话。
- `application_test.go`：端口转发、错误语义、账号切换顺序和缺失能力测试。
- `runtime_test.go`：runtime 刷新锁与发布顺序测试。
- `projection_test.go`：父目标选择、子代理标记跳过与投影构造测试。

## 核心实现

`AccountDeps` 只暴露账号管理、余额、账号选择、窗口设置和 projection 五类能力。函数型
端口保持“调用时解析”语义，宿主替换端口后下一次调用立即生效。

`ProjectionPort` 由 Application 消费方定义，具体锁和快照实现留在装配宿主。本包不知道
`Service`、Runtime 或存储实现。

`AccountWindowPolicy` 固定账号窗口“校验 → 持久化 → 运行面生效”的消费契约；
Middle Setter 仍唯一实现判据，Wiring adapter 只转发。

`RuntimeApplication` 固定 Runtime projection 的“锁外采集 → 锁内应用/bump →
锁外发布”顺序。纯投影构造由 `projection.go` 唯一拥有，宿主只提供锁内输入。

## 数据流

账号 CRUD 和余额请求由兼容 API 进入，经本包规整后交给 `contract.AccountAdminPort` 或
`contract.AccountBalancePort`。账号切换先确认目标可用，再锁外采集 projection、锁内写入
账号并应用投影、单次 bump revision，解锁后发布 `EventRuntimeChanged` 并添加通知。

## 依赖方向

允许依赖 `contract`、`contract/dto`、`contract/protocol` 与
`application/session/viewstate.SubagentContextMarker` 这一只读标记。禁止导入
`application/assembly`、持有完整 `Service`、调用 GUI/TUI，或反向依赖
`backend/**`、`middle/view` 的具体实现。

## 并发与错误语义

`Collect` 必须在共享写锁外执行。锁内只允许账号写入、projection 应用和 revision bump；
发布和通知在解锁后执行。端口未装配时返回稳定的能力缺失错误，不静默成功。

## 扩展方式

新增账号管理用例时先判断是否属于同一领域和同一状态所有者，再扩充最小端口。不得把
Scheduler、Runtime 或 Session 生命周期塞入本包。兼容入口只能保留一行转发。

## Review 指南

- 账号切换是否只 bump 一次 revision；
- projection 慢采集是否仍在锁外；
- 解锁前是否没有调用事件发布或用户通知；
- 父目标是否跳过空正文与子代理产出标记，且截断仍为 200 rune；
- 是否出现第二份账号校验、余额探测或窗口落盘判据；
- 新包是否 import `application/assembly` 或持有完整 `Service`。

## 测试与验证

```text
go test ./application/admin -count=1
go vet ./application/admin
```

关键测试文件：`application_test.go`。

## 文件与函数索引

### `application.go`

- `func NewAccountApplication(deps AccountDeps) *AccountApplication` — 构造账号管理应用。
- `func (application *AccountApplication) AddAccount(_ context.Context, draft dto.AccountDraft) (dto.AccountAddResult, error)` — 转发账号新增。
- `func (application *AccountApplication) AccountDetails(name string) (dto.AccountDetails, error)` — 读取账号非秘密字段。
- `func (application *AccountApplication) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error)` — 原位更新账号。
- `func (application *AccountApplication) DeleteAccount(name string) (dto.AccountDetails, error)` — 删除账号。
- `func (application *AccountApplication) AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error)` — 查询账号余额。
- `func (application *AccountApplication) SelectAccount(_ context.Context, name string) error` — 切换运行账号并发布投影。
- `func (application *AccountApplication) SetAccountWindow(_ context.Context, name string, window int) (int, error)` — 修改逐账号上下文窗口。

### `application_test.go`

测试文件按仓库 README 规范不展开函数索引。

### `runtime.go`

- `func NewRuntime(deps RuntimeDeps) *RuntimeApplication` — 构造 runtime 投影应用。
- `func (application *RuntimeApplication) Refresh()` — 刷新 runtime 投影并发布增量。
