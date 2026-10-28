# application/conversation/context

## 生态位

`application/conversation/context` 承载显式上下文压缩（`/compact` 与
`compact_context`）的应用编排和回执。压缩算法、持久化与门禁判据仍在
`kernel/context`。

## 职责与非职责

职责：

- 从 ctx/视图解析当前 Session；
- acquire/release 同会话压缩 gate；
- 调用 Runtime 唯一压缩入口；
- 映射 Done/Unrecorded/Failed/Scheduled/NotNeeded 回执；
- 组装用户可读 note、门禁耗时、区间和失败原因。

非职责：

- 不实现摘要、预算、替换历史或持久化；
- 不拥有 Task、Engine、View 或 EventHub；
- 不导入 `application/assembly`。

## 文件结构

- `application.go`：`Deps`、`Application`、`Result`、`GateTiming` 与用例。
- `compaction_gate.go`：per-session compaction 集合、收口信号、领轮/等待与
  deferred submit 重放。
- `recovery.go`：provider 拒绝后的有界 checkpoint、恢复/重放、可重放判据和
  恢复信封清理。
- `engine_facade.go`：Engine Context Coordinator/History/Deps alias 与 predicate/warning
  一行转发，供 core 装配。

## 依赖方向

允许依赖 backend engine context 的 identity alias/转发面、`contract/protocol` 与
标准库。禁止依赖 `application/assembly`、GUI/TUI 或存储实现。

## 并发与错误

压缩 gate 覆盖压缩、落盘和回执构造；`CompactionGate` 直接拥有 per-session
compacting 集合与广播信号，所有读写都由共享 `Core.ViewMu` 保护。模型调用在本地
回合内也不会持有 Session 写锁。错误与五种 outcome 的 note 文案保持迁移前语义。

## 测试与验证

```text
go test ./application/conversation/context -count=1
go test ./application/assembly -run 'CompactContext' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### application.go

- `func New(deps Deps) *Application` — New constructs the context-compaction application.
- `func CompactionReasonLabel(reason string) string` — CompactionReasonLabel renders a user-readable compaction reason.
- `func FailureReasonLabel(failure string) string` — FailureReasonLabel renders a known failure literal while preserving numeric
- `func CompactionRecordNote(result Result) string` — CompactionRecordNote renders a recorded compaction receipt.
- `func CompactionGateChecklist(gates []GateTiming) string` — CompactionGateChecklist renders one line of gate timings.
- `func CompactionGateDuration(milliseconds int) string` — CompactionGateDuration renders zero as "<1ms".
- `func CompactionRangeLabel(result Result) string` — CompactionRangeLabel renders recorded message/event boundaries.
- `func (application *Application) CompactContextNow(ctx context.Context) (Result, error)` — CompactContextNow compacts the current execution session.
- `func (application *Application) CompactContextHandler(ctx context.Context, argsJSON string) (string, error)` — CompactContextHandler handles the compact_context model tool.
- `func gates(values []context_runtime.CompactionGateTiming) []GateTiming`
- `func newResult(outcome context_runtime.CompactResult) Result`

### compaction_gate.go

- `func NewCompactionGate(deps CompactionGateDeps) *CompactionGate` — NewCompactionGate constructs the per-session compaction gate.
- `func (gate *CompactionGate) IsCompactingLocked(sessionID string) bool` — IsCompactingLocked reports whether a session has a compaction round.
- `func (gate *CompactionGate) signalLocked()`
- `func (gate *CompactionGate) SignalLocked() <-chan struct` — SignalLocked returns the current compaction-change signal.
- `func (gate *CompactionGate) Acquire(ctx context.Context, sessionID string) error` — Acquire blocks until this session owns its single compaction round.
- `func (gate *CompactionGate) Release(sessionID string)` — Release closes one compaction round and wakes waiters.
- `func (gate *CompactionGate) Await(ctx context.Context, sessionID string) error` — Await waits until the session has no compaction round.
- `func (gate *CompactionGate) DeferSubmitAfterCompaction(ctx context.Context, sessionID, text string)` — DeferSubmitAfterCompaction replays one input after the session's current

### compaction_gate_test.go

- `func TestCompactionGateSerializesOneSession(t *testing.T)`
- `func TestCompactionGateAcquireHonorsContext(t *testing.T)`

### engine_facade.go

- `func NewEngineCoordinator(deps EngineDeps) *EngineCoordinator`
- `func NewEngineHistoryCoordinator(core *state.Core) *EngineHistoryCoordinator`
- `func IsProviderOnlyHistoryContent(content string) bool`
- `func IsTaskContextCheckpoint(content string) bool`
- `func IsActiveSkillContent(content string) bool`
- `func IsOversizedToolResult(content string, limit int) bool`
- `func OversizedToolResultWarning(name, resultRef string) string`
- `func ContentReferenceWarning(resultRef string) string`
- `func RetainedSystemOnly(history []contract.EngineMessage) []contract.EngineMessage`

### engine_facade_test.go

- `func TestEngineFacadeTypesAreBackendIdentity(t *testing.T)`

### recovery.go

- `func NewRecovery(deps RecoveryDeps) *RecoveryApplication` — NewRecovery constructs the provider recovery application.
- `func (application *RecoveryApplication) sessionID(ctx context.Context) string`
- `func (application *RecoveryApplication) RecoverProviderContext(err error, originalRequest string) error` — RecoverProviderContext recovers only provider context-exhaustion failures.
- `func (application *RecoveryApplication) RecoverProviderFailure(err error, originalRequest string) (bool, error)` — RecoverProviderFailure is the active-session compatibility wrapper.
- `func (application *RecoveryApplication) RecoverProviderFailureFor(ctx context.Context, err error, originalRequest string) (bool, error)` — RecoverProviderFailureFor replaces an unusable transcript with a bounded,
- `func (application *RecoveryApplication) RetryContextRecovery(ctx context.Context, requestID string, onChunk func(string)) error` — RetryContextRecovery gives one rejected request a bounded recovery turn.
- `func RetryableAfterRecovery(err error) bool` — RetryableAfterRecovery reports whether a failed request is safe to replay.
- `func NonEmptyProviderInput(input string) string` — NonEmptyProviderInput supplies the provider-safe fallback for an empty user
- `func providerRecoveryDetails(kind contract.ProviderFailureKind) (prefix, heading, summary string)`
- `func (application *RecoveryApplication) RemoveProviderContextRecovery() error` — RemoveProviderContextRecovery removes recovery/control envelopes from the
- `func (application *RecoveryApplication) RemoveProviderContextRecoveryFor(sessionID string) error` — RemoveProviderContextRecoveryFor removes recovery/control envelopes from one

### recovery_test.go

- `func TestRetryableAfterRecoveryOnlyAcceptsPreExecutionRejections(t *testing.T)`
- `func TestNonEmptyProviderInputKeepsRequestOrSuppliesRecoveryNote(t *testing.T)`
