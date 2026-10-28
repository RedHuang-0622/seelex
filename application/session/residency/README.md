# application/session/residency

## 生态位

`application/session/residency` 拥有两层进程内驻留状态：引擎 bundle 的驻留顺序
和可见正文的加载顺序。主要调用方是 `application/assembly` 的 Session 生命周期、
冷恢复和查询装配；纯窗口/驱逐判据仍来自 `middle/residency`。

## 职责

- 维护 resident engine 与 loaded content 两个 LRU 顺序；
- 在 `Core.ViewMu` 内取一致的占用/受保护快照，锁外交给 Middle 判据；
- 执行内容 flush、LRU 驱逐、正文卸载与目录刷新；
- 维护唯一受保护集判据：当前视图、待审批、忙会话和 restoring；
- 在正文自动卸载后触发 cold reload。

## 非职责

- 不拥有 `SessionUnit`、Snapshot 或 View；
- 不实现 Middle 的窗口、排序和预算算法；
- 不打开 Store、Engine 或历史文件句柄；
- 不 import `application/assembly`，不持有完整 `Service`。

## 文件结构

- `runtime.go`：两层 LRU 状态、快照、驱逐、正文回读入口与受保护集；
- `snapshot_test.go`：快照不别名底层顺序的并发回归；

## 核心实现

`RuntimeDeps` 只接收共享状态内核、Session/View owner、Middle residency port、
上限读数和 flush/release/reload/catalog 回调。`Runtime` 自己拥有两个顺序切片；
所有读写与 core 的共享视图锁约定一致，Middle 判据在锁外运行。

## 数据流

Session 生命周期 → `TouchResident/TouchContent` → 锁内 LRU 更新 → 锁外
`ResidencyPort.Evict` → flush/卸载/回读回调 → 目录刷新或继续使用。

## 依赖方向

允许依赖 `application/session` 纯值、`session/state`、`session/viewstate`、
`contract`、`kernel/state` 与 `middle/residency`。禁止依赖 `application/assembly`、
`store`、具体 Backend 实现或 GUI/TUI。

## 并发与错误

`residentOrder` / `loadedContentOrder` 只由 `Core.ViewMu` 保护；驱逐判据拿到的
顺序必须复制。flush 失败不卸载内容；释放失败保持原状并等待下一轮收敛。

## 扩展与 Review

新增驻留层必须先证明它是独立资源，不得把 Session 状态或 Middle 判据复制进来。
Review 重点：锁外 I/O、顺序快照别名、受保护集漂移和回读失败语义。

## 测试与验证

```text
go test ./application/session/residency -count=1
go test ./application/assembly -run 'Resident|Content|Release|Cold|Session' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### runtime.go

- `func NewRuntime(deps RuntimeDeps) *Runtime` — NewRuntime constructs the Session residency runtime.
- `func (runtime *Runtime) TouchResident(sessionID string)` — TouchResident records resident use and reconciles the resident layer.
- `func (runtime *Runtime) MarkResidentRecentLocked(sessionID string, resident bool)` — MarkResidentRecentLocked moves a resident session to the MRU end.
- `func (runtime *Runtime) ReconcileResident()` — ReconcileResident evicts resident engines until the configured window is met.
- `func (runtime *Runtime) ResidentSnapshotLocked() applicationsession.EvictionSnapshot` — ResidentSnapshotLocked returns one coherent resident eviction reading.
- `func (runtime *Runtime) SessionBusy(unit *sessionstate.SessionUnit) bool` — SessionBusy reports whether a session must not be released.
- `func sessionBusyState(unit *sessionstate.SessionUnit) switching.BusyState`
- `func (runtime *Runtime) TouchContent(sessionID string)` — TouchContent records visible-content use and reconciles the content layer.
- `func (runtime *Runtime) MarkContentRecentLocked(sessionID string)` — MarkContentRecentLocked moves a session to the content MRU end.
- `func (runtime *Runtime) ForgetContentLocked(sessionID string)` — ForgetContentLocked removes a session from the content order.
- `func (runtime *Runtime) PruneContentOrderLocked() int` — PruneContentOrderLocked reconciles the content order with loaded units.
- `func (runtime *Runtime) ReconcileContent(protect string)` — ReconcileContent evicts loaded content until the configured window is met.
- `func (runtime *Runtime) ContentSnapshotLocked(protect string) applicationsession.EvictionSnapshot` — ContentSnapshotLocked returns one coherent loaded-content eviction reading.
- `func (runtime *Runtime) EvictLoadedContent(sessionID string) error` — EvictLoadedContent flushes and drops one idle session's visible content.
- `func (runtime *Runtime) ContentLoadedLocked(sessionID string) bool` — ContentLoadedLocked reports whether a session currently owns visible content.
- `func (runtime *Runtime) ContentUnloaded(sessionID string) bool` — ContentUnloaded reports whether content must be reloaded from durable state.
- `func (runtime *Runtime) EnsureContent(sessionID string) error` — EnsureContent reloads content after an automatic eviction.
- `func (runtime *Runtime) ProtectedSessionLocked(sessionID string) bool` — ProtectedSessionLocked reports the single release-protected-set predicate.
- `func (runtime *Runtime) ReleaseContentMemory(sessionID string)` — ReleaseContentMemory drops only visible content after a release decision.
- `func (runtime *Runtime) ForgetResidentLocked(sessionID string)` — ForgetResidentLocked removes a session from the resident order.
- `func (runtime *Runtime) ResidentOrderLen() int` — ResidentOrderLen returns the resident order length for tests and metrics.
- `func (runtime *Runtime) ContentOrderLen() int` — ContentOrderLen returns the content order length for tests and metrics.

### snapshot_test.go

- `func (engine snapshotEngine) HasSession(sessionID string) bool`
- `func (snapshotEngine) UnloadSession(string) error`
- `func (snapshotEngine) SessionID() string`
- `func (snapshotEngine) StartSession() string`
- `func (snapshotEngine) SetSystemPrompt(string)`
- `func (snapshotEngine) SetMaxLoops(int)`
- `func (snapshotEngine) ChatStream(context.Context, string, func(string)) (string, error)`
- `func (snapshotEngine) ClearHistory()`
- `func (snapshotEngine) History() []contract.EngineMessage`
- `func (snapshotEngine) ReplaceHistory(string, []contract.EngineMessage) error`
- `func (snapshotEngine) TraceText() string`
- `func (snapshotEngine) TokenCount() string`
- `func (snapshotEngine) AppendHistory(contract.EngineMessage)`
- `func (snapshotEngine) NodeSessionConversation(string) ([]contract.EngineMessage, bool)`
- `func (snapshotEngine) NodeContextSnapshot(string) (*dto.ContextSnapshot, bool)`
- `func (snapshotEngine) NodeToolResult(string, string) (string, bool)`
- `func (snapshotEngine) NodeWorktreeInfoFor(string) (dto.NodeWorktreeInfo, bool)`
- `func (snapshotEngine) SubscribeSubagentLive(string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error)`
- `func (snapshotEngine) SubagentLiveHistoryPage(string, int, int) dto.SubagentLiveHistoryPage`
- `func (snapshotEngine) SubAgentTree() []dto.SubAgentTreeNode`
- `func (snapshotPort) Preview(content string, limit int) (string, bool)`
- `func (snapshotPort) Touch(order []string, sessionID string) []string`
- `func (snapshotPort) Forget(order []string, sessionID string) []string`
- `func (snapshotPort) Prune(order []string, loaded func(string) bool, unitIDs []string) []string`
- `func (snapshotPort) Budget(legacyLimit, protectedCount, window int) int`
- `func (snapshotPort) InputIndexSummaryLimit() int`
- `func (snapshotPort) BuildInputIndex([]protocol.Message, []string, int, func(protocol.Message) string) []protocol.SessionInputIndexRow`
- `func (snapshotPort) WaitCatalogRefresh(context.Context, applicationsession.CatalogRefresher) error`
- `func (snapshotPort) Evict(applicationsession.EvictionDeps)`
- `func (snapshotPort) Archive(applicationsession.ArchiveUseCaseDeps, string) error`
- `func TestEvictionSnapshotsDoNotAliasOrderBackingArray(t *testing.T)`
