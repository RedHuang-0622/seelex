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

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### account_window_boundary_test.go

- `func TestCoreDoesNotImportAccountWindowImplementation(t *testing.T)`
- `func adminRepositoryRoot(t *testing.T) string`

### application.go

- `func NewAccountApplication(deps AccountDeps) *AccountApplication` — NewAccountApplication constructs an account application from consumer-owned
- `func (application *AccountApplication) AddAccount(_ context.Context, draft dto.AccountDraft) (dto.AccountAddResult, error)` — AddAccount forwards a validated account draft to the account admin host.
- `func (application *AccountApplication) AccountDetails(name string) (dto.AccountDetails, error)` — AccountDetails reads non-secret fields for one account.
- `func (application *AccountApplication) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error)` — UpdateAccount updates one account in place.
- `func (application *AccountApplication) DeleteAccount(name string) (dto.AccountDetails, error)` — DeleteAccount deletes one account and returns its non-secret receipt.
- `func (application *AccountApplication) AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error)` — AccountBalance probes an account through the optional balance host port.
- `func (application *AccountApplication) SelectAccount(_ context.Context, name string) error` — SelectAccount switches the runtime account, then republishes the projected
- `func (application *AccountApplication) SetAccountWindow(_ context.Context, name string, window int) (int, error)` — SetAccountWindow applies a per-account context window through the host's
- `func (application *AccountApplication) accountAdmin(action string) (contract.AccountAdminPort, error)`

### application_test.go

- `func (fake *fakeAccountAdmin) AddAccount(draft dto.AccountDraft) (dto.AccountAddResult, error)`
- `func (fake *fakeAccountAdmin) AccountDetails(name string) (dto.AccountDetails, error)`
- `func (fake *fakeAccountAdmin) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error)`
- `func (fake *fakeAccountAdmin) DeleteAccount(name string) (dto.AccountDetails, error)`
- `func (fake *fakeBalance) AccountBalance(context.Context, string) (dto.AccountBalance, error)`
- `func (fake *projectionFake) Collect(context.Context) view_state.RuntimeStateProjection`
- `func (fake *projectionFake) LockView()`
- `func (fake *projectionFake) SetRuntimeAccount(name string)`
- `func (fake *projectionFake) ApplyRuntimeProjectionLocked(view_state.RuntimeStateProjection)`
- `func (fake *projectionFake) BumpLocked() uint64`
- `func (fake *projectionFake) UnlockView()`
- `func (fake *projectionFake) CurrentViewSessionID() string`
- `func (fake *projectionFake) RuntimeSnapshot() model.RuntimeState`
- `func (fake *projectionFake) PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState)`
- `func (fake *projectionFake) AddNotice(message string)`
- `func TestAccountApplicationDelegatesAccountOperations(t *testing.T)`
- `func TestAccountApplicationPreservesUnavailableErrors(t *testing.T)`
- `func TestAccountApplicationPropagatesHostErrors(t *testing.T)`
- `func TestAccountApplicationSelectAccountOrder(t *testing.T)`
- `func TestAccountApplicationSelectAccountStopsWhenUnavailable(t *testing.T)`
- `func TestAccountApplicationSetAccountWindowDelegates(t *testing.T)`
- `func TestAccountApplicationSetAccountWindowPreservesUnavailableErrors(t *testing.T)`

### projection.go

- `func RuntimeVisibility(goalSkillActive bool, governance *dto.GoalGovernanceView) dto.RuntimeVisibilityProjection` — RuntimeVisibility 构造「可见性投影」（goal 技能门控 + 当前会话的 goal 治理
- `func ParentEvidence(sessionID string, messages []model.Message, conversationCount int) dto.ParentEvidenceProjection` — ParentEvidence 构造「父证据投影」：会话 ID、最近一条可见用户目标正文、
- `func LatestVisibleUserGoal(messages []model.Message) string` — LatestVisibleUserGoal 返回最后一条**可见**用户输入正文作为"父会话在做什么"的
- `func truncateRuntimeProjectionGoal(content string) string`

### projection_test.go

- `func TestLatestVisibleUserGoal(t *testing.T)`
- `func TestRuntimeProjectionConstructors(t *testing.T)`

### runtime.go

- `func NewRuntime(deps RuntimeDeps) *RuntimeApplication` — NewRuntime constructs the runtime projection application.
- `func (application *RuntimeApplication) Refresh()` — Refresh collects outside the lock, applies inside it, then publishes after

### runtime_test.go

- `func (fake *runtimeProjectionFake) CollectProjection(context.Context) view_state.RuntimeStateProjection`
- `func (fake *runtimeProjectionFake) LockView()`
- `func (fake *runtimeProjectionFake) ApplyRuntimeProjectionLocked(view_state.RuntimeStateProjection)`
- `func (fake *runtimeProjectionFake) BumpLocked() uint64`
- `func (fake *runtimeProjectionFake) UnlockView()`
- `func (fake *runtimeProjectionFake) CurrentViewSessionID() string`
- `func (fake *runtimeProjectionFake) RuntimeSnapshot() model.RuntimeState`
- `func (fake *runtimeProjectionFake) PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState)`
- `func (fake *runtimeProjectionFake) PublishTaskDeltas()`
- `func TestRuntimeApplicationRefreshOrder(t *testing.T)`
