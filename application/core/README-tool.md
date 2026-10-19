# core/tool（根包分卷）

## 生态位

工具事件钩子与诊断

覆盖：`tool*.go`；未归属文件由覆盖自检拦下。

## 实现要点

- 工具完成投影只有一条生产路径：`handleToolCompleteObserved`（`handleToolComplete`
  是 `observe=nil` 的薄包装）。可选诊断观察者（`ToolHookBridge.SetDiagnosticObserver`）
  只能**包围**阻塞边界（`toolhook.complete.lock.start/done` 等阶段打点），不改投影
  顺序、不带工具入参/输出；诊断事件是 metadata-only（`ToolHookDiagnosticEvent`）。
- 工具启动/完成按 `(turn, name, arguments)` 键在 `ToolHookBridge.pending` 里 FIFO
  配对：完成侧匹配不到在途 ID 时就地分配新 ID，事件不丢、不串会话。
  边界（未修，待独立改动）：该键**不含会话**，两个会话在同 turn 调同名同参工具时，
  完成侧可能取到另一会话的在途 ID，导致两侧视图的工具块都停在 `running`（不产生
  跨会话正文，但状态不落地）。见研究文档 §7.9「剩余边界」。
- 工具启动边界（`handleToolStart`）在 flush 流式缓冲、宣告事件就位后，把本轮**说明
  正文按迭代归位**到该工具调用的 assistant(tool_calls) 事件
  （`task_context.AttributeToolNarrationLocked`）：wire 上工具轮正文恒为空
  （Seele `session/loop.go:564`），说明文本只经 `onChunk` 进视图，不回填则
  record / 重启恢复只剩工具痕迹；归位必须按迭代增量做——回合收尾的事后填充会把
  叙述写到别的轮次（2026-09-12 修复）。目标调用 ID 必须按 `(name, arguments)` 在
  `pendingProviderCalls` 里解析成**框架真实 ID**：本卷传入的 `tool-N` 是合成 ID，
  直接用它定位 transcript 事件会**静默失效**（不报错，只是永不归位）。守卫用例：
  `TestToolNarrationStaysWithOwningIteration`、`TestConcurrentSessionsKeepOwnContent`。
- 空工具结果（`result == ""`）在 provider 投影里**保持为空**：wire 上本来就是空正文，
  补 `MissingHistoryContent` 会让下一轮重投影与已发出字节分叉（2026-09-12 修复；
  见 `backend/engine/context/history.go` 的 `RepairEmptyHistoryContent` 与
  `TestContextPrefixInvariant_EmptyToolResult`）。
- 快照截断链两端都在本卷：实时路径 `boundToolResultForSnapshot`（超
  `limits.snapshot_tool_output_chars`，默认 8000 字符、按 rune 边界安全切）只把预览
  放进可见会话，全文归档为 result_ref（前端「加载完整输出」再分页读回）；恢复路径
  `appendHistoryLocked` 复用同一预算。provider 侧历史不受该值影响，仍由
  `max_tool_result_chars` 约束（`tool_hooks_truncation_test.go` 两个用例各钉一端）。
- plan 工具入参在钩子边界规范化：`normalizePlanToolCallInfo` 让应用快照与 Seele
  执行使用同一 DAG 表示；非法入参原样保留，使工具错误对用户可见。
- 工具完成投影（`handleToolCompleteObserved`）在 `tool_result` 之后统一补一条空
  assistant 占位，不区分会话是否活跃：后台会话若缺占位，后续流式正文会被
  `appendVisibleDeltaBackground` 并入工具之前的旧 assistant 空消息，热切回该
  会话时工具会“后插入”到最终回复之后（2026-09-08 修复；回归用例
  `repro_hot_attach_background_tool_order_test.go` 钉住热/冷两种恢复顺序）。
- **工具记录**（`tool_record.go`，2026-10-15）：三条工具完成路径（主代理/后台会话的
  `tool.completed`、子代理、teammate）都经**唯一两处判据**（`toolStatusFailed` 判成败、
  `toolRecordReason` 归原因）后，累加进**按会话**的内存读数（`toolRecordAccumulators`，
  落盘在 `store/record` 的 `Accumulator`）。口径：**只有结果帧**（success/error）
  才记；**成功与失败都记**（没有分母的失败计数会把优化带偏）；开关
  （`SetToolRecordEnabled`，设置里那一格，落 runtime-limits.json）关着时一个字节都不记。
  命令面另有一条判据：`RecordToolCommandOutcome`（进程退出码非零 = 一次失败，原因归
  `command-failed`），上报口是工具层既有的 `bash.process.exited` 诊断事件。
  **落盘是批次**：`FlushToolRecord` 在真相源提交时刻（草稿物化）与停机兜底各写一次整份
  读数（会话粒度，见 store 的 `tool-record.jsonl`）——不做逐次工具写盘，避免磁盘写忙。
  原因归并吃的是**原始错误**而不是给模型看的包装文案（后者刻意不含内部细节）。

## 阶梯提交与会话隔离（2026-10-19 方向 C）

链路（用户口径原文）：`subagent/teammate -> mainagent -> 工具记录存储`。三条纪律：

- **归属键显式**：一笔读数带 `(ownerSessionID, unitID)`。owner 只从**执行上下文**取
  （子代理 = 节点注册时随 ctx 绑定的主会话，由 `backend/engine/assemble` 解析后经
  `SubagentToolObservation` 交进来；teammate = 事件自带的 `MainSessionID`）。
  `Snapshot.Session.ID` / `currentViewSessionID()` **永不作兜底**——切会话串写就是这么
  来的（#8/#13）。取不到 ⇒ 丢弃 + 计数（`ToolRecordDroppedReadings`，键
  `no_owner_session` / `no_unit_identity`），诊断面可见、不进快照。
- **阶梯提交**：执行单元（子代理节点 / 角色会话）在做工期间只往**自己的**暂存里累加
  （`store/record.Stage`，`StageUnitToolOutcome`：只碰台账锁，不碰 ViewMu、不碰磁盘）；
  单元收口时 `CloseUnitToolRecord` 把它**同步**一次性交给会话累计（返回即已入账，
  不丢 goroutine）。单元键口径只在本卷一份：`subagent:<nodeID>`（无 NodeID 时
  `subagent:<event.ID>` 兜底，不再整条丢）、`role:<roleSessionID>`。
  收口点三处：子代理节点进入终态（`RefreshWorkTableSnapshot` 扫树）、员工回合收口
  （`Runtime.runRoleRound` 的返回点 → `CloseRoleToolRecordUnit`）、以及落盘前抽干。
- **唯一入口**：读数进会话累计只有 `CommitUnitToolRecord(sessionID, unitID, seq, stats)`
  一条路——`sessionID` 空则丢弃；`(sessionID, unitID)` 记 `lastSeq`，`seq <= lastSeq`
  幂等忽略；合并是**相加**语义（`record.Accumulator.Merge`，与冷启动读回的 `Load`
  取较大者不同）。主代理自己的读数不经暂存（它本身就是主侧），仍走 `recordToolOutcome`，
  与暂存链共用同一份判据（`toolStatusFailed` / `toolRecordReason`）。

**落盘门**：`FlushToolRecord` 只在**已物化的真实会话**上写盘（判据复用会话域既有的
草稿归属 `switching.DraftTarget`，不新造一格）——草稿一次都不写；落盘前先
`drainStagesFor(sessionID)` 抽干该会话的全部叶子暂存（叶子没来得及收口也不丢）。
保留四处调用：`chat.go` 回合末尾（过门）、`materializeDraftSession`（物化即入账）、
`Shutdown` 全量、会话释放（releaseFull 之前）。

**回收**：会话释放/删除（`releaseSessionMemory` 的 releaseFull）连带回收该会话的
暂存、提交序号、累计与"读过盘"标记——会话没了，读数的归属也就没了；留着只会让复用
同一 ID 的新会话读到旧账（旧 `lastSeq` 还会把新会话的第一批读数误判成重复提交）。
视图切换**什么都不做**（读数不跟视图走）。

守卫用例：`tool_record_isolation_test.go`（12 条会话隔离边界矩阵）、
`store/record/stage_test.go`（暂存本身）、`middle/view/subagent/record_attribution_test.go`
（归属与"记账先发生"）。

### tool_record.go

- `type toolRecordAccumulators struct{…}` — toolRecordAccumulators 是**每会话**一份的内存累计（键 = 会话 ID）。
- `func newToolRecordAccumulators() *toolRecordAccumulators` — 构造按会话的累计表。
- `func (table *toolRecordAccumulators) accumulator(sessionID string) *toolrecord.Accumulator` — 取（必要时建）某会话的累计。
- `func (table *toolRecordAccumulators) markLoaded(sessionID string) bool` — 报告"这个会话还是第一次从存储读回"。
- `func (service *Service) SetToolRecordEnabled(enabled bool) (bool, error)` — 开关计数（设置里那一个开关的落点）。
- `func (service *Service) ApplyToolRecordEnabled(enabled bool)` — 只改内存（装配期用：启动时把配置/覆盖层里的值装进来）。
- `func (service *Service) ToolRecordEnabled() bool` — 返回当前开关状态（设置面板与看板都读它）。
- `func (service *Service) FlushToolRecord(sessionID string)` — 把一个会话的读数**批量**写进会话自己的存储（真相源提交时刻调一次）。
- `func (service *Service) loadToolRecord(sessionID string)` — 把该会话最后一次落盘的读数读回内存（每个会话只读一次）。
- `func (service *Service) flushAllToolRecords()` — 把所有会话的读数批量落一次（停机兜底用）。
- `func (service *Service) SetToolRecordSnapshotStore(store ToolRecordSnapshotStore)` — 注入（或收回：传 nil）读数落盘口。
- `func (service *Service) toolRecordSnapshotStore() ToolRecordSnapshotStore` — 取一份当前落盘口快照（读锁）。
- `func (service *Service) ToolRecordBoard() dto.ToolRecordBoard` — 返回**当前视图会话**的工具记录看板（TUI / GUI 同源读法）。
- `func (service *Service) RecordToolCommandOutcome(sessionID, tool string, exitCode int)` — 记一次**命令面**的结果（进程退出码）。
- `func toolStatusFailed(status dto.ToolEventStatus) bool` — "这次工具调用算不算失败"的**唯一判据**。
- `func (service *Service) toolRecordOutcomeForStatus(sessionID, name, status, errorText string)` — 字符串状态那两条链（teammate / 子代理）的唯一入口。
- `func toolRecordReason(text string) string` — 把失败文本归到**唯一一份原因词表**（dto.ToolReason*）。
- `func containsAny(haystack string, needles ...string) bool` — 子串命中（原因词表匹配用）。
- `func toolErrorText(err error) string` — 取错误的**原始**文本（没有错误 = 空串）。
- `func (service *Service) recordToolOutcome(sessionID, tool string, failed bool, errorText string)` — 三族工具完成路径上的**唯一**调用口（成功与失败都记）。

### tool_record_test.go

- `type recordingSnapshotStore struct{…}` — 落盘口替身：记下每次批量写，并能吐回一份"旧读数"。
- `func (s *recordingSnapshotStore) SaveToolRecordSnapshot(sessionID string, snapshot dto.ToolRecordSnapshot) error` — 记下一次批量写（替身实现）。
- `func (s *recordingSnapshotStore) LoadToolRecordSnapshot(string) (dto.ToolRecordSnapshot, bool, error)` — 数一数读回了几次（替身实现）。
- `func (s *recordingSnapshotStore) savesSnapshot() []dto.ToolRecordSnapshot` — 取回已落盘的快照。
- `func (s *recordingSnapshotStore) loadCount() int` — 取回读回次数。
- `func enableToolRecord(t *testing.T, service *Service, store *recordingSnapshotStore)` — 打开开关并装上落盘口（测试装配动作）。
- `func TestToolRecordCountsBothOutcomesAcrossFamilies(t *testing.T)` — 三条完成路径都记：成功与失败都记、running 帧不记、原因各归其格。
- `func TestToolRecordFlushIsBatchAndIdempotent(t *testing.T)` — 批量落盘：只在提交点写、没有新读数不重写、写的是整份快照。
- `func TestToolRecordFlushFailureSurfacesOnBoard(t *testing.T)` — 落盘失败不回抛，错误留给看板显示。
- `func TestToolRecordLoadsPersistedReadingOnce(t *testing.T)` — 冷启动读回：看板读之前懒加载一次，且每个会话只读一次。
- `func TestToolRecordDisabledRecordsNothing(t *testing.T)` — 关就是关：开关关着时一个字节都不记、也不落盘。
- `func TestToolCommandOutcomeCountsNonZeroExitAsFailure(t *testing.T)` — 命令面（退出码）是另一条成败判据：非零 = 一次失败（归 command-failed）。
- `func TestToolRecordIgnoresUnknownStatusWords(t *testing.T)` — 未知/空状态词不记。
- `func TestToolRecordIsPerSession(t *testing.T)` — 每会话一份：两个会话的读数互不串。
- `func TestToolRecordWithoutSessionIsDropped(t *testing.T)` — 每条记录都要有会话归属（无归属的调用记不进任何会话）。
- `func TestToolRecordFlushWithoutStoreIsNoop(t *testing.T)` — 没装落盘口时批量落盘是空操作（读数仍在内存里）。
- `func TestToolRecordDisabledRecordsNothing(t *testing.T)` — 关就是关：开关关着时一个字节都不记。
- `func TestToolRecordIgnoresUnknownStatusWords(t *testing.T)` — 未知/空状态词不记（认不得的词不该被当成成功）。
- `func TestToolRecordBoardReportsSwitchAndTotals(t *testing.T)` — 看点板：开关状态与合计从同一份台账折出来。
### tool_hook_diagnostic_test.go

- `func TestToolHookDiagnosticObserverMarksCompletionProjection(t *testing.T)`

### tool_hooks.go

- `func (service *Service) handleToolStart(ctx context.Context, name, id, arguments string)`
- `func (service *Service) planBranchBindingLocked() dto.PlanBranchBinding`
- `func (service *Service) handleToolComplete(name, id, result string, toolErr error, duration time.Duration)`
- `func (service *Service) handleToolCompleteObserved(ctx context.Context, name, id, arguments, result string, toolErr error, duration time.Duration, observe func(string))` — handleToolCompleteObserved 把生产完成投影保持在一处，同时允许 ToolHookBridge
- `func NewToolHookBridge() *ToolHookBridge`
- `func (bridge *ToolHookBridge) Bind(service *Service)`
- `func (bridge *ToolHookBridge) SetDiagnosticObserver(observer ToolHookDiagnosticObserver)` — SetDiagnosticObserver 安装可选、尽力而为的生命周期诊断。传入 nil 关闭。
- `func (bridge *ToolHookBridge) observeDiagnostic(event ToolHookDiagnosticEvent)`
- `func (bridge *ToolHookBridge) Hooks() *session.LoopHooks`
- `func normalizePlanToolCallInfo(info session.ToolCallInfo) session.ToolCallInfo` — normalizePlanToolCallInfo 保持应用快照与 Seele 执行的同一规范 DAG 表示。
- `func (bridge *ToolHookBridge) beginTool(info session.ToolCallInfo) (*Service, string)`
- `func (bridge *ToolHookBridge) completeTool(info session.ToolCallInfo) (*Service, string)`
- `func (bridge *ToolHookBridge) nextToolIDLocked() string`
- `func toolHookKey(info session.ToolCallInfo) string`

### tool_hooks_truncation_test.go

- `func TestToolCompleteTruncatesSnapshotOutput(t *testing.T)` — TestToolCompleteTruncatesSnapshotOutput 验证实时截断链端到端：大工具
- `func TestAppendHistoryLockedTruncatesRestoredOutput(t *testing.T)` — TestAppendHistoryLockedTruncatesRestoredOutput 验证会话恢复路径：恢复的

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### tool_hook_diagnostic_test.go

- `func TestToolHookDiagnosticObserverMarksCompletionProjection(t *testing.T)`

### tool_hooks.go

- `func (service *Service) handleToolStart(ctx context.Context, name, id, arguments string)`
- `func (service *Service) handleToolComplete(name, id, result string, toolErr error, duration time.Duration)`
- `func (service *Service) handleToolCompleteObserved(ctx context.Context, name, id, arguments, result string, toolErr error, duration time.Duration, observe func(string))` — handleToolCompleteObserved 把生产完成投影保持在一处，同时允许 ToolHookBridge
- `func NewToolHookBridge() *ToolHookBridge`
- `func (bridge *ToolHookBridge) Bind(service *Service)`
- `func (bridge *ToolHookBridge) SetDiagnosticObserver(observer ToolHookDiagnosticObserver)` — SetDiagnosticObserver 安装可选、尽力而为的生命周期诊断。传入 nil 关闭。
- `func (bridge *ToolHookBridge) observeDiagnostic(event ToolHookDiagnosticEvent)`
- `func (bridge *ToolHookBridge) Hooks() *session.LoopHooks`
- `func (bridge *ToolHookBridge) beginTool(info session.ToolCallInfo) (*Service, string)`
- `func (bridge *ToolHookBridge) completeTool(info session.ToolCallInfo) (*Service, string)`
- `func (bridge *ToolHookBridge) nextToolIDLocked() string`
- `func toolHookKey(info session.ToolCallInfo) string`

### tool_hooks_test.go

- `func TestHandleSubagentToolEventProjectsBoundedIncrementals(t *testing.T)` — TestHandleSubagentToolEventProjectsBoundedIncrementals 钉住子代理工具事件的
- `func TestToolHookBridgeAssignsUniqueStableIDs(t *testing.T)`

### tool_hooks_truncation_test.go

- `func TestToolCompleteTruncatesSnapshotOutput(t *testing.T)` — TestToolCompleteTruncatesSnapshotOutput 验证实时截断链端到端：大工具
- `func TestAppendHistoryLockedTruncatesRestoredOutput(t *testing.T)` — TestAppendHistoryLockedTruncatesRestoredOutput 验证会话恢复路径：恢复的

### tool_record.go

- `func newToolRecordAccumulators() *toolRecordAccumulators`
- `func (table *toolRecordAccumulators) accumulator(sessionID string) *record.Accumulator` — accumulator 取（必要时建）某会话的累计。
- `func (table *toolRecordAccumulators) markLoaded(sessionID string) bool` — markLoaded 报告"这个会话还是第一次从存储读回"（每个会话只读一次，免得每次记录/看板都读盘）。
- `func (service *Service) SetToolRecordEnabled(enabled bool) (bool, error)` — SetToolRecordEnabled 开关计数（设置里那一个开关的落点）。
- `func (service *Service) ApplyToolRecordEnabled(enabled bool)` — ApplyToolRecordEnabled 只改内存（**装配期**用：启动时把配置/覆盖层里的值装进来）。
- `func (service *Service) ToolRecordEnabled() bool` — ToolRecordEnabled 返回当前开关状态（设置面板与看板都读它）。
- `func (service *Service) FlushToolRecord(sessionID string)` — FlushToolRecord 把一个会话的读数**批量**写进会话自己的存储（真相源提交时刻调一次）。
- `func (service *Service) toolRecordWritableSession(sessionID string) bool` — toolRecordWritableSession 报告该会话是不是**已物化的真实会话**（落盘门的唯一判据）。
- `func (service *Service) loadToolRecord(sessionID string)` — loadToolRecord 把该会话最后一次落盘的读数读回内存（每个会话只读一次）。
- `func (service *Service) SetToolRecordSnapshotStore(store contract.ToolRecordSnapshotStore)` — SetToolRecordSnapshotStore 注入（或收回：传 nil）读数落盘口。
- `func (service *Service) toolRecordSnapshotStore() contract.ToolRecordSnapshotStore`
- `func (service *Service) flushAllToolRecords()` — flushAllToolRecords 把**所有**会话的读数批量落一次（停机兜底用）。
- `func (service *Service) ToolRecordBoard() dto.ToolRecordBoard` — ToolRecordBoard 返回**当前视图会话**的工具记录看板（TUI / GUI 同源读法）。
- `func (service *Service) RecordToolCommandOutcome(sessionID, tool string, exitCode int)` — RecordToolCommandOutcome 记一次**命令面**的结果（进程退出码）。
- `func toolStatusFailed(status dto.ToolEventStatus) bool` — toolStatusFailed 是"这次工具调用算不算失败"的**唯一判据**（吃类型化状态那一格）。
- `func toolRecordReason(text string) string` — toolRecordReason 把失败文本归到**唯一一份原因词表**（dto.ToolReason*）。
- `func containsAny(haystack string, needles ...string) bool`
- `func toolErrorText(err error) string` — toolErrorText 取错误的**原始**文本（没有错误 = 空串）。
- `func (service *Service) recordToolOutcome(sessionID, tool string, failed bool, errorText string)` — recordToolOutcome 在三族工具完成路径上按同一判据记一笔（成功与失败都记）。

### tool_record_isolation_test.go

- `func toolRecordStat(tool string, success, failure int, reason string) []dto.ToolRecordStat` — toolRecordStat 造一格增量读数（失败笔数挂一个原因）。
- `func subagentReading(tool string, status dto.ToolEventStatus, errorText string) seelsession.SubagentToolEvent` — subagentReading 造一条子代理工具读数（执行单元 = subagent:<nodeID>）。
- `func totals(service *Service, sessionID string) (int, int, int)` — totals 读某会话的累计三数（不存在 ⇒ 全 0）。
- `func newToolRecordHarness(t *testing.T) *toolRecordHarness`
- `func (harness *toolRecordHarness) switchToB()` — switchToB 走**生产切换路径**（ResumeSession），不是直接改快照字段。
- `func (harness *toolRecordHarness) switchToA()`
- `func TestToolRecordSubagentReadingStaysWithOwnerAcrossViewSwitch(t *testing.T)`
- `func TestToolRecordConcurrentSubagentsStayInOwnSession(t *testing.T)`
- `func TestToolRecordTeammateWithoutMainSessionIsDropped(t *testing.T)`
- `func TestToolRecordCommitIsIdempotentPerSeq(t *testing.T)`
- `func TestToolRecordCommitAccumulatesAcrossSeqs(t *testing.T)`
- `func draftService(t *testing.T) (*Service, *recordingSnapshotStore)` — draftService 造一个**仍是草稿**的 Service（引擎还没建 bundle：HasSession=false）。
- `func TestToolRecordDraftSessionNeverWrites(t *testing.T)`
- `func TestToolRecordDraftMaterializationWritesOnceWithLeafReadings(t *testing.T)`
- `func TestToolRecordFlushDrainsUnclosedLeafStages(t *testing.T)`
- `func TestToolRecordBoardFollowsViewWithoutCrossing(t *testing.T)`
- `func TestToolRecordReleaseRecyclesStageAndSeq(t *testing.T)`
- `func TestToolRecordDisabledStagesNothing(t *testing.T)`
- `func TestToolRecordThreeFamiliesMergeInOneSession(t *testing.T)`

### tool_record_stage.go

- `func subagentToolRecordUnitID(nodeID, eventID string) string` — subagentToolRecordUnitID 折算子代理执行单元的键：有 NodeID 用节点，
- `func roleToolRecordUnitID(roleSessionID, eventID string) string` — roleToolRecordUnitID 折算 teammate 执行单元的键：角色会话是它的身份；
- `func newToolRecordStages() *toolRecordStages`
- `func (table *toolRecordStages) record(key toolRecordUnitKey, tool string, ok bool, reason string)` — record 把一笔读数记进单元暂存（调用方已完成归属/单元键的判空与取整）。
- `func (table *toolRecordStages) take(key toolRecordUnitKey) (uint64, []dto.ToolRecordStat)` — take 交出某单元的增量（没有暂存 = 空）。
- `func (table *toolRecordStages) acceptSeq(key toolRecordUnitKey, seq uint64) bool` — acceptSeq 报告该 seq 是否可以入账（`seq <= lastSeq` = 重复提交）。
- `func (table *toolRecordStages) drainSession(sessionID string) []toolRecordCommit` — drainSession 交出某会话名下**全部**单元的增量（落盘前抽干：叶子没来得及收口也不丢）。
- `func (table *toolRecordStages) releaseSession(sessionID string)` — releaseSession 回收某会话的全部暂存与提交序号（会话释放/删除）。
- `func (table *toolRecordStages) noteDropped(reason string)` — noteDropped 记一次"读数被丢弃"（诊断面：丢弃必须是可见的，不能静默）。
- `func (table *toolRecordStages) droppedCounts() map[string]int` — droppedCounts 返回丢弃计数快照。
- `func (table *toolRecordStages) unitCount() int` — unitCount 返回当前登记的暂存个数（测试/诊断读面）。
- `func (service *Service) StageUnitToolOutcome(sessionID, unitID, tool string, failed bool, errorText string)` — StageUnitToolOutcome 把一笔读数记进**执行单元自己的暂存**（叶子本地累加）。
- `func (service *Service) CommitUnitToolRecord(sessionID, unitID string, seq uint64, stats []dto.ToolRecordStat)` — CommitUnitToolRecord 是**叶子 → 主侧**的唯一入口：同步、幂等、只碰台账锁。
- `func (service *Service) CloseUnitToolRecord(sessionID, unitID string)` — CloseUnitToolRecord 是一次**单元收口**：把它的暂存一次性同步提交给主会话累计并清空。
- `func (service *Service) CloseSubagentToolRecordUnit(ownerSessionID, nodeID string)` — CloseSubagentToolRecordUnit 子代理节点的收口（单元键折算只有一份：`subagent:<nodeID>`）。
- `func (service *Service) CloseRoleToolRecordUnit(mainSessionID, roleSessionID string)` — CloseRoleToolRecordUnit 角色会话的收口（单元键折算只有一份：`role:<roleSessionID>`）。
- `func (service *Service) drainStagesFor(sessionID string)` — drainStagesFor 抽干某会话名下的全部叶子暂存（落盘前的兜底：叶子未收口也不丢）。
- `func (service *Service) releaseToolRecordSession(sessionID string)` — releaseToolRecordSession 回收某会话的工具记录内存面（会话释放/删除时调用）。
- `func (service *Service) noteDroppedToolRecordReading(reason string)` — noteDroppedToolRecordReading 记一次"读数被丢弃"（丢弃必须可见，不能静默）。
- `func (service *Service) ToolRecordDroppedReadings() map[string]int` — ToolRecordDroppedReadings 返回"读数被丢弃"的计数（诊断读面，不进快照）。
- `func toolRecordCountsStatus(status dto.ToolEventStatus) bool` — toolRecordCountsStatus 报告这一帧状态是不是"一次调用的结束"（成功/失败两格）。
- `func (service *Service) stageUnitToolOutcomeForStatus(ownerSessionID, unitID, name, status, errorText string)` — stageUnitToolOutcomeForStatus 是"字符串状态"那条链（teammate）进暂存的入口。
- `func (service *Service) stageSubagentToolOutcome(ownerSessionID string, event dto.SubagentToolEvent)` — stageSubagentToolOutcome 是子代理链进暂存的入口（事件自带身份：节点/事件 ID）。
- `func (service *Service) closeTerminalSubagentToolRecords(nodes []dto.SubAgentTreeNode)` — closeTerminalSubagentToolRecords 把**进入终态**的子代理节点的暂存收口（收口点之一）。
- `func terminalSubagentStatus(status dto.SubAgentNodeStatus) bool` — terminalSubagentStatus 报告子代理节点是否已经收口（终态三格：完成/失败/中断）。

### tool_record_test.go

- `func (s *recordingSnapshotStore) SaveToolRecordSnapshot(sessionID string, snapshot dto.ToolRecordSnapshot) error`
- `func (s *recordingSnapshotStore) LoadToolRecordSnapshot(string) (dto.ToolRecordSnapshot, bool, error)`
- `func (s *recordingSnapshotStore) savesSnapshot() []dto.ToolRecordSnapshot`
- `func (s *recordingSnapshotStore) loadCount() int`
- `func enableToolRecord(t *testing.T, service *Service, store *recordingSnapshotStore)` — enableToolRecord 打开开关并把落盘口装上（测试里两条装配动作各走一次）。
- `func TestToolRecordCountsBothOutcomesAcrossFamilies(t *testing.T)`
- `func TestToolRecordFlushIsBatchAndIdempotent(t *testing.T)` — 批量落盘：只在提交点写一次；没有再记过就不重写（dirty 门）；写的是整份快照。
- `func TestToolRecordFlushFailureSurfacesOnBoard(t *testing.T)` — 落盘失败不回抛（会话提交不该因为读数写不进去而失败），错误留给看板显示。
- `func TestToolRecordLoadsPersistedReadingOnce(t *testing.T)` — 冷启动读回：看板读之前懒加载一次，且**每个会话只读一次**（不每次读盘）。
- `func TestToolRecordDisabledRecordsNothing(t *testing.T)` — 关就是关：开关关着时一个字节都不记、也不落盘。
- `func TestToolCommandOutcomeCountsNonZeroExitAsFailure(t *testing.T)` — 命令面（退出码）是**另一条**成败判据：工具没报错，但命令退非零 = 一次失败。
- `func TestToolRecordIgnoresUnknownStatusWords(t *testing.T)` — 未知/空状态词不记（认不得的词不该被当成成功）。
- `func TestToolRecordIsPerSession(t *testing.T)` — 每会话一份：两个会话的读数互不串（切会话看到的是自己的）。
- `func TestToolRecordWithoutSessionIsDropped(t *testing.T)` — 每条记录都要有会话归属：没有归属的调用记不进任何会话（别造出"没有主人的读数"）。
- `func TestToolRecordFlushWithoutStoreIsNoop(t *testing.T)`
