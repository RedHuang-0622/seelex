# application/execution/toolrecord

## 生态位

`application/execution/toolrecord` 拥有工具记录的应用侧台账：成功/失败累计、失败原因、
执行单元暂存、会话批次落盘与看板投影。主要调用方是 `application/assembly` 装配后的
ToolRecord 组件；具体工具执行与持久化仍分别归工具层和 Session Store 的
`tool-record.jsonl` 端口。

## 职责

- 维护每会话一份本包 `Accumulator`；
- 维护 `(sessionID, unitID)` 的本包 `Stage`、`lastSeq` 幂等序号和 dropped 诊断；
- 唯一拥有工具成败与失败原因的判据；
- 先持久化开关设置、再修改内存；
- 在提交点 drain 叶子暂存并批量落盘整份会话快照；
- 每个会话最多懒加载一次持久快照；
- 把累计读数投影成 `dto.ToolRecordBoard`。
- 解析 teammate/subagent 终态帧、稳定单元键和递归终态收口。

## 非职责

- 不执行工具、不判断工具权限、不生成工具消息；
- 不直接读写存储文件；持久化只通过 `contract.ToolRecordSnapshotStore`；
- 不决定会话是否已物化；该宿主判据由 `Deps.WritableSession` 注入；
- 不持有完整 `Service`，也不 import `application/assembly`。

## 核心契约

```go
type Deps struct {
    SaveEnabled     func(bool) error
    WritableSession func(string) bool
    ViewSessionID   func() string
}
```

`SaveEnabled` 失败时开关保持旧值。`WritableSession` 只在 `Flush` 的落盘门前调用；
`ViewSessionID` 只用于 `Board` 选择当前会话。

## 数据流与生命周期

1. 主代理/命令结果进入 `RecordOutcome`，直接累计到会话；
2. 子代理/teammate 结果先进入 `StageUnitOutcome` 的执行单元暂存；
3. 单元收口 `CloseUnit` 或 flush 前 drain 通过 `CommitUnit` 合并到会话累计；
4. `Flush` 先 drain、再检查可写会话、最后在 dirty 时写整份快照；
5. `Board` lazily load 一次持久快照，再读取当前会话累计与最近 flush 错误；
6. 会话释放通过 `ReleaseSession` 回收累计、暂存、加载标记和幂等序号。
7. `StageStatus` / `StageSubagentEvent` / `CloseTerminalSubagents` 只做稳定帧分类、
   单元键折算与终态收口，不再在 core 保留第二套 adapter。

文件结构：

- `application.go`：累计、暂存、flush、看板与生命周期接口；
- `accumulator.go`：会话累计与失败原因唯一判据；
- `stage.go`：执行单元暂存、序号幂等和 dropped 诊断；
- `host.go`：teammate/subagent 状态帧与终态单元收口；
- `*_test.go`：内存台账、持久化 parity 与 host adapter 回归。

## 并发与错误语义

- 顶层 mutex 只保护开关、端口、表索引和错误槽；
- `Accumulator` / `Stage` 使用各自的锁，热路径不在顶层锁下累加；
- 持久化 I/O 不持顶层锁；
- 同一 `(sessionID, unitID)` 的 `seq <= lastSeq` 幂等忽略，`seq == 0` 保留旧兼容语义；
- flush 失败不回抛，错误进入看板 `LedgerError`；
- 无 owner/unit 的读数丢弃并增加诊断计数，不回落到当前视图。

## 扩展与 Review

- 新增读面优先扩展现有 `SessionView` / `Diagnostics`，避免创建无人使用的未来接口；
- 原因词表只能改 `Reason` 一处，并与 `dto.ToolReason*` 同步；
- Review 时检查 `Flush` 是否先 drain、是否在锁外 I/O、是否错误地回落视图会话，
  以及 `ReleaseSession` 是否清理 `lastSeq`。

## 测试与验证

```text
go test ./application/execution/toolrecord -count=1
go test ./application/assembly -run 'ToolRecord|ToolCommand|RoleTool|SubagentTool' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### accumulator.go

- `func NewAccumulator() *Accumulator` — NewAccumulator constructs an empty in-memory tool ledger.
- `func (accumulator *Accumulator) Record(tool string, ok bool, reason string)` — Record adds one tool call. Successful calls count too so the board has a
- `func (accumulator *Accumulator) Stats() []dto.ToolRecordStat` — Stats returns a deterministic snapshot ordered by total calls descending,
- `func (accumulator *Accumulator) Stat(tool string) (dto.ToolRecordStat, bool)` — Stat returns one tool's current totals.
- `func (accumulator *Accumulator) Totals() (int, int, int)` — Totals returns total, success and failure counts.
- `func (accumulator *Accumulator) Snapshot() (dto.ToolRecordSnapshot, bool)` — Snapshot returns a full persisted-shape snapshot and clears dirty.
- `func (accumulator *Accumulator) Load(snapshot dto.ToolRecordSnapshot)` — Load merges a persisted snapshot monotonically. It is used when a cold
- `func (accumulator *Accumulator) Merge(stats []dto.ToolRecordStat)` — Merge adds one leaf-stage delta. It deliberately differs from Load: Merge
- `func normalizeReason(reason string) string`
- `func bumpReason(reasons []dto.ToolRecordReason, reason string) []dto.ToolRecordReason`
- `func addReason(reasons []dto.ToolRecordReason, reason string, count int) []dto.ToolRecordReason`
- `func mergeReasons(current, incoming []dto.ToolRecordReason) []dto.ToolRecordReason`
- `func cloneStat(stat dto.ToolRecordStat) dto.ToolRecordStat`

### accumulator_test.go

- `func TestAccumulatorCountsBothOutcomes(t *testing.T)`
- `func TestAccumulatorSnapshotHonoursDirtyGate(t *testing.T)`
- `func TestAccumulatorLoadOnlyGrows(t *testing.T)`
- `func TestAccumulatorNormalizesReasonVocabulary(t *testing.T)`

### application.go

- `func New(deps Deps) *Application` — New constructs an empty ledger.
- `func (application *Application) SetEnabled(enabled bool) (bool, error)` — SetEnabled persists the user setting before changing the live value.
- `func (application *Application) ApplyEnabled(enabled bool)` — ApplyEnabled changes only the in-memory assembly value.
- `func (application *Application) Enabled() bool` — Enabled reports the current live switch.
- `func (application *Application) SetSnapshotStore(store contract.ToolRecordSnapshotStore)` — SetSnapshotStore injects or replaces the session snapshot port.
- `func (application *Application) Flush(sessionID string)` — Flush drains leaf stages, applies the writable-session gate and writes one
- `func (application *Application) FlushAll()` — FlushAll flushes every session already present in the in-memory ledger.
- `func (application *Application) Board() dto.ToolRecordBoard` — Board returns the current-view session projection.
- `func (application *Application) RecordCommandOutcome(sessionID, tool string, exitCode int)` — RecordCommandOutcome records the command-exit-code success criterion.
- `func (application *Application) RecordOutcome(sessionID, tool string, failed bool, errorText string)` — RecordOutcome records one completed main-agent or command outcome in memory.
- `func (application *Application) StageUnitOutcome(sessionID, unitID, tool string, failed bool, errorText string)` — StageUnitOutcome accumulates a leaf result in the execution unit's local stage.
- `func (application *Application) CommitUnit(sessionID, unitID string, seq uint64, stats []dto.ToolRecordStat)` — CommitUnit is the single idempotent leaf-to-session commit entry.
- `func (application *Application) CloseUnit(sessionID, unitID string)` — CloseUnit synchronously commits one unit's pending stage.
- `func (application *Application) CloseRoleUnit(mainSessionID, roleSessionID string)` — CloseRoleUnit 收口一个角色回合：主会话是记账归属，角色会话决定单元键。
- `func (application *Application) ReleaseSession(sessionID string)` — ReleaseSession reclaims all in-memory ledger state for a released session.
- `func (application *Application) SessionView(sessionID string) SessionView` — SessionView snapshots one session without mutating lazy-load state.
- `func (application *Application) Diagnostics() Diagnostics` — Diagnostics returns dropped-reading and stage counts.
- `func (application *Application) NoteDropped(reason string)` — NoteDropped records a visibly-dropped reading without fabricating an owner.
- `func SubagentUnitID(nodeID, eventID string) string` — SubagentUnitID derives the stable execution-unit key.
- `func RoleUnitID(roleSessionID, eventID string) string` — RoleUnitID derives the stable role execution-unit key.
- `func CountsStatus(status dto.ToolEventStatus) bool` — CountsStatus reports whether a tool frame is a terminal result frame.
- `func Failed(status dto.ToolEventStatus) bool` — Failed is the single typed-status failure predicate.
- `func Reason(text string) string` — Reason maps a raw failure text to the shared reason vocabulary.
- `func ErrorText(err error) string` — ErrorText returns the original error text, never the presentation wrapper.
- `func TerminalSubagentStatus(status dto.SubAgentNodeStatus) bool` — TerminalSubagentStatus reports whether a subagent node must close its stage.
- `func (application *Application) accumulator(sessionID string) *Accumulator`
- `func (application *Application) findAccumulator(sessionID string) *Accumulator`
- `func (application *Application) markLoaded(sessionID string) bool`
- `func (application *Application) load(sessionID string)`
- `func (application *Application) noteFlushError(err error)`
- `func (application *Application) stage(key unitKey, tool string, ok bool, reason string)`
- `func (application *Application) take(key unitKey) (uint64, []dto.ToolRecordStat)`
- `func (application *Application) acceptSeq(key unitKey, seq uint64) bool`
- `func (application *Application) drainSession(sessionID string)`
- `func (application *Application) noteDropped(reason string)`
- `func containsAny(haystack string, needles ...string) bool`

### application_test.go

- `func (store *fakeStore) SaveToolRecordSnapshot(sessionID string, snapshot dto.ToolRecordSnapshot) error`
- `func (store *fakeStore) LoadToolRecordSnapshot(string) (dto.ToolRecordSnapshot, bool, error)`
- `func TestSetEnabledPersistsBeforeChangingMemory(t *testing.T)`
- `func TestRecordOutcomeCountsSuccessFailureAndReasons(t *testing.T)`
- `func TestFlushDrainsStagesAndIsDirtyGated(t *testing.T)`
- `func TestFlushFailureAndBoardLazyLoad(t *testing.T)`
- `func TestOwnerDropsSeqIdempotencyAndRelease(t *testing.T)`
- `func TestDisabledRecordsAndStagesNothing(t *testing.T)`
- `func TestUnitIDAndStatusHelpers(t *testing.T)`

### host.go

- `func (application *Application) StageStatus(ownerSessionID, unitID, name, status, errorText string)` — StageStatus parses and records one teammate tool result frame.
- `func (application *Application) StageSubagentEvent(ownerSessionID string, event dto.SubagentToolEvent)` — StageSubagentEvent records one terminal subagent tool result frame.
- `func (application *Application) CloseSubagentUnit(ownerSessionID, nodeID string)` — CloseSubagentUnit commits one stable subagent unit key.
- `func (application *Application) CloseTerminalSubagents(nodes []dto.SubAgentTreeNode)` — CloseTerminalSubagents commits every terminal subagent node's staged reading.

### stage.go

- `func NewStage(unitID, sessionID string) *Stage` — NewStage constructs an execution-unit staging area.
- `func (stage *Stage) UnitID() string` — UnitID returns the stable execution-unit key.
- `func (stage *Stage) SessionID() string` — SessionID returns the owner session captured by the execution context.
- `func (stage *Stage) Record(tool string, ok bool, reason string)` — Record accumulates one completed tool call in the local stage.
- `func (stage *Stage) TakeForCommit() (uint64, []dto.ToolRecordStat)` — TakeForCommit synchronously returns this window's delta and clears the

### stage_test.go

- `func TestStageAccumulatesLocallyAndTakesSynchronously(t *testing.T)`
- `func TestStageMergesIntoAccumulatorByAddition(t *testing.T)`
- `func TestStageBoundsToolNamesWithoutLosingCounts(t *testing.T)`
- `func TestStageIgnoresEmptyIdentity(t *testing.T)`
