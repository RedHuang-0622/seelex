# application/session/cold

## 生态位

`application/session/cold` 承载冷会话恢复的前半链：持久化三读、history 装配、
Engine/queue 恢复、workspace 绑定与锁内激活。主要调用方是 core 的冷恢复主链。

## 职责与非职责

职责：

- 并行读取 record 与 transcript，按需回退 legacy history；
- 重建 transcript 与 provider history；
- 恢复 Engine 历史、durable queue 与 system prompt；
- 应用 workspace 绑定、epoch、active、tier、title 顺序。

非职责：

- 不拥有 SessionStore、Engine、Runtime 或 View；
- 不实现冷恢复顶层切换与失败回退；
- 不导入 `application/core`。

## 文件结构

- `read.go`：冷加载持久化读取。
- `history.go`：transcript / provider history 装配。
- `restore.go`：Engine 恢复与 queue 回填。
- `workspace.go`：项目根和 session binding 规则。
- `activation.go`：锁内激活准备。
- `task.go`：Task/Plan/checkpoint/compaction 恢复。
- `view.go`：record/legacy 可见窗口构造与安装。
- `commit.go`：锁内 pending-tail、ChatState、Plan、workspace 与 revision。
- `publish.go`：锁外 context、Runtime、Snapshot 与 LRU 发布。
- `cold_test.go`：读取短路与 workspace 绑定测试。

## 依赖方向

允许依赖 backend engine 的 Session/Task 类型、`contract`、`contract/protocol`、
`application/session/state`。禁止依赖 `application/core`、GUI/TUI 或存储实现。

## 并发与错误

`ReadApplication` 并行读取 record 与 transcript；调用方保证后续 View 写面在正确
锁内。错误包装、legacy fallback、Engine replace 与 queue 恢复顺序保持迁移前语义。

## 测试与验证

```text
go test ./application/session/cold -count=1
go test ./application/core -run 'SessionCold(Read|History|Restore|Workspace|Activation)' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

### `read.go`

- `func NewRead(deps ReadDeps) *ReadApplication` — 构造冷读取应用。
- `func (application *ReadApplication) Read(sessionID string) (ReadResult, error)` — 并行读取 record/transcript 并回退 history。

### `history.go`

- `func NewHistory(deps HistoryDeps) *HistoryApplication` — 构造 history 装配应用。
- `func (application *HistoryApplication) Assemble(sessionID string, loaded ReadResult) (HistoryResult, error)` — 重建 transcript 与 provider history。

### `restore.go`

- `func NewRestore(deps RestoreDeps) *RestoreApplication` — 构造 Engine 恢复应用。
- `func (application *RestoreApplication) Restore(...)` — 恢复 Engine、queue 和 system prompt。

### `workspace.go`

- `func NewWorkspace(deps WorkspaceDeps) *WorkspaceApplication` — 构造冷 workspace 应用。
- `func (application *WorkspaceApplication) Bind(...)` — 应用项目根、binding 与解绑规则。

### `activation.go`

- `func NewActivation(deps ActivationDeps) *ActivationApplication` — 构造锁内激活应用。
- `func (application *ActivationApplication) PrepareLocked(...) ActivationResult` — 应用 epoch、active、tier 与 title。

### `task.go`

- `func NewTask(deps TaskDeps) *TaskApplication` — 构造冷 Task 恢复应用。
- `func (application *TaskApplication) RestoreLocked(...)` — 恢复 plan/task/checkpoint/compaction。

### `view.go`

- `func NewView(deps ViewDeps) *ViewApplication` — 构造冷 View 安装应用。
- `func (application *ViewApplication) InstallLocked(...)` — 安装 record 或 legacy 可见窗口。

### `commit.go`

- `func NewCommit(deps CommitDeps) *CommitApplication` — 构造锁内 commit 应用。
- `func (application *CommitApplication) CommitLocked(...) CommitResult` — 提交 pending-tail、ChatState、Plan、workspace 与 revision。

### `publish.go`

- `func NewPublish(deps PublishDeps) *PublishApplication` — 构造锁外发布应用。
- `func (application *PublishApplication) Publish(...)` — 挂接 context、发布 runtime/snapshot 并收敛 LRU。
