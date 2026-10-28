# store/misc（根包分卷）

## 生态位

跨卷用例与基础回归（会话状态/锁、head 发布与自愈、并发与读放大探针）

覆盖：测试文件兜底卷（前缀无归属时落这里）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### absent_state_reads_test.go

- `func TestAbsentHeadReadsReturnStateNotIOError(t *testing.T)`

### archive_status_test.go

- `func TestArchiveStatusRewriteKeepsFivePieces(t *testing.T)` — TestArchiveStatusRewriteKeepsFivePieces 钉住 C2 的存储契约：归档只重写

### blob_unify_test.go

- `func TestToolResultLivesInBigToolResultChannel(t *testing.T)` — TestToolResultLivesInBigToolResultChannel 对应 S13（§2.0 规则 6、§2.6、D9）：

### c1_publish_probe_test.go

- `func TestProbeCommitCostBreakdown(t *testing.T)` — TestProbeCommitCostBreakdown 是 C1/H3 的成本拆解探针（**不是判据**，只打印数字）。

### channel_semantics_test.go

- `func TestStackChannelReplayUsesDeterministicCommitID(t *testing.T)` — TestStackChannelReplayUsesDeterministicCommitID 对应 T-STK-10（§2.0 规则 4、
- `func TestStackChannelColdReloadDeduplicatesByItemID(t *testing.T)` — TestStackChannelColdReloadDeduplicatesByItemID 对应 T-STK-11（§2.0 规则 4
- `func TestStackChannelActiveWholeReplacement(t *testing.T)` — TestStackChannelActiveWholeReplacement 对应 T-STK-13（§2.0 通道类型表 +
- `func TestQueueFileWholeReplacement(t *testing.T)` — TestQueueFileWholeReplacement 对应 T-STK-14（§2.0 通道类型表）：队列

### cold_read_fixture_test.go

- `func coldReadKey() Key`
- `func coldReadProjectDirName() string` — coldReadProjectDirName 复刻项目目录的派生规则，供测试定位 fixture。
- `func TestColdReadFixture(t *testing.T)` — TestColdReadFixture 是 M0 的主判据：已提交的磁盘数据必须能被当前代码读回。
- `func TestColdReadFixtureRegenerate(t *testing.T)` — TestColdReadFixtureRegenerate 重建 testdata 里的冷读 fixture。
- `func seedColdReadSession(t *testing.T, root string)` — seedColdReadSession 用当前写入口生成一份覆盖全部通道的代表性会话。
- `func coldReadCopyTree(t *testing.T, src, dst string)` — coldReadCopyTree 把 fixture 复制到目标目录（保留相对路径，不保留时间戳）。
- `func coldReadCompactJSON(t *testing.T, raw []byte) string` — coldReadCompactJSON 归一化 JSON 文本，用于比较存储层原样保留的 payload。

### commit_write_order_test.go

- `func openWriteOrderRepository(t *testing.T) (*jsonRepository, string)` — openWriteOrderRepository 打开一个临时数据根的仓储（含数据根锁）。
- `func seedSessionRoot(t *testing.T, repository *jsonRepository, key Key) string` — seedSessionRoot 预建会话目录，用来在会话目录里放“毒化”路径。
- `func writeOrderVisibleRows(t *testing.T, repository *jsonRepository, key Key) int` — writeOrderVisibleRows 返回消息 head 当前发布的可见行数。
- `func TestCommitWritesArtifactBeforeMessage(t *testing.T)` — TestCommitWritesArtifactBeforeMessage 钉写序第一段：artifact → refs → message。
- `func TestCommitPublishesMessageBeforeCompactionFailure(t *testing.T)` — TestCommitPublishesMessageBeforeCompactionFailure 钉写序第二段与半失败语义：
- `func TestCommitRetryWithExplicitSeqIsIdempotent(t *testing.T)` — TestCommitRetryWithExplicitSeqIsIdempotent 钉“同一次提交重试”的幂等边界。

### complete_units_recovery_test.go

- `func interruptedEventTurn(withSuffixText, atTail bool) []Event` — interruptedEventTurn 构造残缺工具链事件流（同 task_context 探针语义）：
- `func TestCompleteEventUnitsKeepsSuffixTextOfInterruptedChain(t *testing.T)` — TestCompleteEventUnitsKeepsSuffixTextOfInterruptedChain：残缺工具链之后的
- `func TestCompleteEventUnitsKeepsTailInterruptedChainAsOpenUnit(t *testing.T)` — TestCompleteEventUnitsKeepsTailInterruptedChainAsOpenUnit：以残缺工具链

### concurrency_race_test.go

- `func TestDurableHistoryConcurrentSessionsStaySeparate(t *testing.T)` — TestDurableHistoryConcurrentSessionsStaySeparate（阶段 D6b，-race）：多会话
- `func firstContentOr(messages []types.Message, fallback string) string`
- `func lastContentOr(messages []types.Message) string`
- `func TestRouterConcurrentProjectIsolation(t *testing.T)` — TestRouterConcurrentProjectIsolation（阶段 D6b，-race）：两个项目并发提交
- `func TestProjectRecordConcurrentReadDoesNotTear(t *testing.T)` — TestProjectRecordConcurrentReadDoesNotTear（阶段 D6b，-race）：单写多读项目语义
- `func TestForkCopiesDoNotShareParentGeneration(t *testing.T)` — TestForkCopiesDoNotShareParentGeneration（阶段 D6b，-race）：父会话持续提交

### data_root_lock_multiprocess_test.go

- `func TestJSONDataRootLockMultiProcessTwoArms(t *testing.T)` — TestJSONDataRootLockMultiProcessTwoArms 钉住 limits.runtime.allow_multi_process
- `func TestJSONDataRootLockMultiProcessStaleBypass(t *testing.T)` — TestJSONDataRootLockMultiProcessStaleBypass：allow_multi_process=true 时陈旧锁

### data_root_lock_test.go

- `func boolPtr(value bool) *bool`
- `func TestJSONDataRootLockAcquiredAndReleased(t *testing.T)` — TestJSONDataRootLockAcquiredAndReleased 验证 §9/D11 数据根独占锁随
- `func TestJSONDataRootLockForeignProcessRejected(t *testing.T)` — TestJSONDataRootLockForeignProcessRejected 验证存活的其他进程持有锁时
- `func TestJSONDataRootStaleLockRespectsAutoRecover(t *testing.T)` — TestJSONDataRootStaleLockRespectsAutoRecover 验证陈旧锁默认只报错
- `func TestJSONDataRootCrashedGUIResidualLockRecovered(t *testing.T)` — TestJSONDataRootCrashedGUIResidualLockRecovered 钉住 2026-09-17 的实际事故形状：

### data_root_lock_windows_test.go

- `func startTerminatedProcess(t *testing.T) (int, func())` — startTerminatedProcess 返回一个「已经退出、但 PID 仍被占用」的进程 pid。
- `func TestJSONDataRootTerminatedProcessLockIsStale(t *testing.T)` — TestJSONDataRootTerminatedProcessLockIsStale 钉住 2026-09-18 事故：dev GUI 被强杀

### draft_append_test.go

- `func TestAppendMessageDraftWorkspaceKeepsPublishedPoint(t *testing.T)` — TestAppendMessageDraftWorkspaceKeepsPublishedPoint 覆盖运行期周期快照的
- `func TestDraftSnapshotReusesTailPointer(t *testing.T)` — TestDraftSnapshotReusesTailPointer 钉住周期快照的双指针写侧：
- `func draftRowsOf(t *testing.T, store *storeEngine, key Key) []Event`
- `func physicalRows(t *testing.T, store *storeEngine, key Key, shard string) []Event`
- `func TestDraftAppendInvisibleUntilPublished(t *testing.T)` — TestDraftAppendInvisibleUntilPublished 草稿 append 不进读者可见范围。
- `func TestDraftAppendContinuesFromPhysicalTail(t *testing.T)` — TestDraftAppendContinuesFromPhysicalTail 续号基准是物理末行（A2 核心改动）：
- `func TestPublishDraftTailPromotesAllRows(t *testing.T)` — TestPublishDraftTailPromotesAllRows 发布 = 显式入口：一次性把草稿尾提升为
- `func TestDiscardDraftTailKeepsPublishPointAndRows(t *testing.T)` — TestDiscardDraftTailKeepsPublishPointAndRows 丢弃 = 显式入口：发布点不动、
- `func TestDraftAppendRollsShardBeyondPublishedIndex(t *testing.T)` — TestDraftAppendRollsShardBeyondPublishedIndex 草稿 append 触发的分片滚动
- `func TestNormalCommitReapsDraftTail(t *testing.T)` — TestNormalCommitReapsDraftTail 普通提交会 reap 掉草稿尾（崩溃残尾语义）。

### draft_snapshot_bench_test.go

- `func BenchmarkDraftSnapshotAppend(b *testing.B)` — BenchmarkDraftSnapshotAppend 对照周期快照复用末分片指针前后的成本。
- `func benchmarkDraftSnapshotAppend(b *testing.B, resetCache bool)`

### ensure_indexed_test.go

- `func TestEnsureIndexedMakesRecordOnlySessionEnumerable(t *testing.T)` — TestEnsureIndexedMakesRecordOnlySessionEnumerable G：record-only 草稿会话

### event_kind_test.go

- `func TestEventKindRoundTrip(t *testing.T)`
- `func TestEventKindOfFallback(t *testing.T)`

### event_log_append_only_test.go

- `func eventLogFixture(t *testing.T) (*jsonRepository, Key)` — eventLogFixture 打开一个 JSON v8 后端并返回具体实现（用例要取会话目录路径）。
- `func eventLogPathFor(repository *jsonRepository, key Key) string`
- `func readEventLogBytes(t *testing.T, path string) []byte`
- `func eventLogTail(data []byte) string`
- `func assertEventLogIsLineLog(t *testing.T, path string) int` — assertEventLogIsLineLog 断言事件库是逐行日志，并返回行数。
- `func assertEventSeqs(t *testing.T, entries []EventLogEntry, want ...uint64)`
- `func TestEventLogAppendsLinesWithoutRewriting(t *testing.T)` — TestEventLogAppendsLinesWithoutRewriting 是 §2.9 的主判据：追加不得重写整份
- `func TestEventLogMigratesLegacyArrayFileOnFirstAppend(t *testing.T)` — TestEventLogMigratesLegacyArrayFileOnFirstAppend 覆盖线上存量形态（整份 JSON
- `func TestEventLogMigratesLegacyGenerationFilesOnFirstAppend(t *testing.T)` — TestEventLogMigratesLegacyGenerationFilesOnFirstAppend 覆盖 v1 布局遗留
- `func TestEventLogDropsTornTailAndHealsOnNextAppend(t *testing.T)` — TestEventLogDropsTornTailAndHealsOnNextAppend 覆盖「追加写被崩溃截断」：
- `func TestEventLogKeepsLastEntryForRepeatedSeq(t *testing.T)` — TestEventLogKeepsLastEntryForRepeatedSeq 钉住重试幂等口径：同 Seq 重复追加
- `func TestEventLogConcurrentAppendsKeepEveryEntry(t *testing.T)` — TestEventLogConcurrentAppendsKeepEveryEntry 覆盖并发追加：每一条都要完整落盘
- `func writeLegacyEventLogArray(t *testing.T, path string, entries []EventLogEntry)` — writeLegacyEventLogArray 手写旧形态（整份 JSON 数组）到盘上。

### first_user_input_test.go

- `func TestFirstUserInputsSkipsInjectedUserRows(t *testing.T)` — TestFirstUserInputsSkipsInjectedUserRows：会话最前面的若干条用户输入要么是
- `func TestFirstUserInputsStopsEarlyInFirstShard(t *testing.T)` — TestFirstUserInputsStopsEarlyInFirstShard：标题回填是**有界读**——只打开首个
- `func TestFirstUserInputsReportsMissingLayout(t *testing.T)` — TestFirstUserInputsReportsMissingLayout：会话还没有已发布行（未落盘的新会话/
- `func TestFirstUserInputsRefusesTrimmedPrefix(t *testing.T)` — TestFirstUserInputsRefusesTrimmedPrefix：首问所在前缀已被 LRU 淘汰（首个分片

### fork_test.go

- `func TestListToolResultsAndCurrentGenerationAcrossBackends(t *testing.T)`
- `func TestForkCommitDeepCopiesToolResultsAfterParentDelete(t *testing.T)` — TestForkCommitDeepCopiesToolResultsAfterParentDelete 验证 fork 落盘数据面
- `func TestForkToolResultsConcurrentDivergenceStayIsolated(t *testing.T)` — TestForkToolResultsConcurrentDivergenceStayIsolated（D6b 发散式 -race）：

### gap_probe_test.go

- `func TestProbeForkEventConstantCommitID(t *testing.T)`

### god_type_ratchet_test.go

- `func TestGodTypeMethodSurfaceOnlyShrinks(t *testing.T)` — TestGodTypeMethodSurfaceOnlyShrinks 是 ratchet 主判据。
- `func TestGodTypeMethodScannerSeesAdditionsAndSkipsTests(t *testing.T)` — TestGodTypeMethodScannerSeesAdditionsAndSkipsTests 证明门禁确实会“咬”。
- `func godTypeMethodDrift(want, got []string) (added, removed []string)` — godTypeMethodDrift 返回基线有而实际没有（removed）、实际有而基线没有（added）的方法。
- `func readGodTypeMethodBaseline(t *testing.T) map[string][]string` — readGodTypeMethodBaseline 读取基线并要求每个列表有序去重（手工编辑最容易留下的脏数据）。
- `func scanGodTypeMethods(t *testing.T, dir string) map[string][]string` — scanGodTypeMethods 返回四个 God 类型在本包生产文件里声明的方法名（各自有序）。
- `func scanGodTypeDeclarations(t *testing.T, dir string) map[string]bool` — scanGodTypeDeclarations 返回本包生产文件里声明过的类型名集合。
- `func godTypeReceiverName(expression ast.Expr) string` — godTypeReceiverName 从接收者类型表达式里取出类型名（兼容 `T` 与 `*T`）。
- `func godTypeSourceFiles(t *testing.T, dir string) []string` — godTypeSourceFiles 返回目录下的生产 .go 文件（非递归、排除测试文件）。
- `func containsGodType(name string) bool`
- `func storePackageDir(t *testing.T) string` — storePackageDir 返回本测试文件所在目录（= store 包根）。

### head_publish_probe_test.go

- `func probeExclusiveOpen(path string) error` — probeExclusiveOpen 以 share mode 0 打开文件；返回 nil 表示此刻无人持柄。
- `func TestProbeStackHeadPublishHandle(t *testing.T)`

### head_rebuild_test.go

- `func TestHeadRepairRebuildsMessageFromData(t *testing.T)` — TestHeadRepairRebuildsMessageFromData 对应 T-M1-09（§2.0 规则 3 / S15）：
- `func TestHeadRepairRebuildsEventFromData(t *testing.T)` — TestHeadRepairRebuildsEventFromData 验证 EVENT head 同样按分片重建。
- `func TestHeadRepairRebuildsCompactFromData(t *testing.T)` — TestHeadRepairRebuildsCompactFromData 验证 compact head 从 compact.jsonl

### head_self_heal_test.go

- `func corruptHeadChecksum(t *testing.T, store *storeEngine, key Key, module storageModule)` — corruptHeadChecksum 把 head 文件的 checksum 改坏（构造"校验失败两次"的自愈
- `func TestMessageHeadSelfHealKeepsHeadOnlyFields(t *testing.T)` — TestMessageHeadSelfHealKeepsHeadOnlyFields 钉住 D1：自愈重建只按数据文件
- `func TestHeadSelfHealDoesNotPublishWhileModuleLockHeld(t *testing.T)` — TestHeadSelfHealDoesNotPublishWhileModuleLockHeld 钉住 D2：head 的重建发布
- `func TestHeadSelfHealUnderLockHealsInPlace(t *testing.T)` — TestHeadSelfHealUnderLockHealsInPlace 钉住持锁入口的原地自愈：提交路径

### history_wire_resume_test.go

- `func wireFixture(t *testing.T) (*storeEngine, Key)`
- `func roundRows(count int) []Event` — roundRows 生成 seq 连续的轮次行（user + assistant 交替）。
- `func commitRoundRows(t *testing.T, store *storeEngine, key Key, rows []Event)`
- `func TestHistoryPagePagingStableAndMarksInternal(t *testing.T)` — TestHistoryPagePagingStableAndMarksInternal 对应 T-R1-01。
- `func TestHistoryPageCompactKeepsOriginalRows(t *testing.T)` — TestHistoryPageCompactKeepsOriginalRows 对应 T-R1-02：压缩不删正文，R1 仍能读到
- `func TestHistoryPageLRUPlaceholderNoHoles(t *testing.T)` — TestHistoryPageLRUPlaceholderNoHoles 对应 T-R1-03：LRU 删除 watermark 前行后，
- `func TestHistoryPageAssistantToolCallsSingleRow(t *testing.T)` — TestHistoryPageAssistantToolCallsSingleRow 对应 T-R1-04：assistant 带多个
- `func runWireAssembly(t *testing.T, store *storeEngine, key Key, cache *attemptCache, budget, k int) wireResult`
- `func wireRoles(result wireResult) []string`
- `func TestWireAssemblyStablePrefix(t *testing.T)` — TestWireAssemblyStablePrefix 对应 T-R2-01：无 frame、无新消息两次 R2 前缀一致。
- `func TestWireAssemblyTailStartsAfterFrame(t *testing.T)` — TestWireAssemblyTailStartsAfterFrame 对应 T-R2-02：frame=[msg3,msg7] → 摘要在前；
- `func TestWireAssemblyFilterInternalRows(t *testing.T)` — TestWireAssemblyFilterInternalRows 对应 T-R2-03：internal/context 行仅
- `func TestWireAssemblyCacheKeepsRecentK(t *testing.T)` — TestWireAssemblyCacheKeepsRecentK 对应 T-R2-04：同操作重试 > K，wire 只含最近
- `func TestWireAssemblyCacheClearedPurePersistent(t *testing.T)` — TestWireAssemblyCacheClearedPurePersistent 对应 T-R2-05：缓存清空后输出与纯持久
- `func TestWireAssemblyOrphanToolSkipped(t *testing.T)` — TestWireAssemblyOrphanToolSkipped 对应 T-R2-06：孤儿 tool 行不进入 wire。
- `func TestWireAssemblyOpenUnitRepairNoSuffixDrop(t *testing.T)` — TestWireAssemblyOpenUnitRepairNoSuffixDrop 对应 T-R2-07：流尾残缺工具轮保留 open
- `func TestWireAssemblyOpenRepairThenNextUser(t *testing.T)` — TestWireAssemblyOpenRepairThenNextUser 对应 T-R2-08：缺失结果后有后续 user →
- `func TestWireAssemblyBudgetStopsAtUnitBoundary(t *testing.T)` — TestWireAssemblyBudgetStopsAtUnitBoundary 对应 T-R2-09：预算极小 → 停在完整单元
- `func TestResumeRecordUsesCompactPointerAndKeepsContextDigest(t *testing.T)` — TestResumeRecordUsesCompactPointerAndKeepsContextDigest 覆盖冷加载的压缩
- `func TestWireAssemblyEventMissingNoEffect(t *testing.T)` — TestWireAssemblyEventMissingNoEffect 对应 T-R2-10：删除 event.json 条目（短窗口
- `func TestWireAssemblyLRUDeletedRegionOnlyFrameAndTail(t *testing.T)` — TestWireAssemblyLRUDeletedRegionOnlyFrameAndTail 对应 T-R2-11：LRU 已删旧区后 R2
- `func TestResumeTailInterruptedAnchorTail(t *testing.T)` — TestResumeTailInterruptedAnchorTail 对应 T-R3-01：interrupted 锚 msg95，后续
- `func TestResumeTailCompleteNoFakeInterrupted(t *testing.T)` — TestResumeTailCompleteNoFakeInterrupted 对应 T-R3-02：消息完整但 interrupted
- `func TestResumeTailIncompleteSynthesizesInterrupted(t *testing.T)` — TestResumeTailIncompleteSynthesizesInterrupted 对应 T-R3-03：消息残缺（缺 tool
- `func TestResumeTailRestartCacheEmptyR2OK(t *testing.T)` — TestResumeTailRestartCacheEmptyR2OK 对应 T-R3-04：重启后尝试缓存为空，恢复后
- `func rawJSON(value string) []byte`
- `func wirePrefixStrip(messages []wireMessage) []wireMessage`

### layout_golden_test.go

- `func layoutGoldenHash16(value string) string` — layoutGoldenHash16 复刻落盘目录名的派生规则：sha256 前 8 字节的小写 hex。
- `func layoutGoldenSHA256(data []byte) string` — layoutGoldenSHA256 是内容寻址段（media）的派生规则。
- `func layoutGoldenRelativePaths(t *testing.T, sessionRoot string) []string` — layoutGoldenRelativePaths 返回会话根下的全部相对路径（目录与文件），已排序。
- `func layoutGoldenHeadKeys(t *testing.T, sessionRoot, module string) []string` — layoutGoldenHeadKeys 读取一个模块 head，返回稳定的顶层 key 集合。
- `func TestSessionLayoutGolden(t *testing.T)` — TestSessionLayoutGolden 钉住一次"全模块都写过"的会话的物理布局。
- `func layoutGoldenMissing(a, b []string) []string` — layoutGoldenMissing 返回 a 中有、b 中没有的元素（用于失败信息）。

### lock_hotspot_probe_test.go

- `func TestProbeCommitVsFullRead(t *testing.T)` — TestProbeCommitVsFullRead 定量归因探针（**不是判据**，只打印数字）。

### lock_hotspot_test.go

- `func errorsIsSkipAll(err error) bool` — errorsIsSkipAll 兼容 filepath.SkipAll 被包成 error 返回的调用姿势。
- `func newLockProfileRouter(t *testing.T, root string) *Router` — newLockProfileRouter 打开一个指向 tempdir 的 router（锁热点测试专用）。
- `func signalFirstPublishedRead(t *testing.T) <-chan struct` — signalFirstPublishedRead 把"读者已走到解码边界"变成确定性前提：返回的
- `func TestProbeNoHistoryCacheWritten(t *testing.T)` — TestProbeNoHistoryCacheWritten 断言 v8 JSON 布局的完整提交不再产生
- `func TestCommitNotBlockedByFullHistoryRead(t *testing.T)` — TestCommitNotBlockedByFullHistoryRead 断言"提交不被一次全量历史读长时间挡住"。

### m10_test_compat_test.go

- `func Boards(repository Repository) (BoardRepository, bool)`
- `func Teamwork(repository Repository) (TeamworkRepository, bool)`
- `func MediaStoreOf(repository Repository) (MediaStore, bool)`
- `func (router *Router) BoardsFor() (BoardRepository, bool)`
- `func (repository *jsonRepository) ReadTeamworkPlan(_ context.Context, key Key) (TeamworkPlan, error)`
- `func (repository *jsonRepository) ReadTeamworkEvents(_ context.Context, key Key) ([]TeamworkEvent, error)`
- `func (repository *jsonRepository) ReadGoalBoard(ctx context.Context, key Key) (GoalBoardMeta, error)`
- `func (repository *jsonRepository) ReadTeamBoard(ctx context.Context, key Key) (TeamBoardMeta, error)`
- `func (repository *jsonRepository) ReadToolRecordSnapshot(ctx context.Context, key Key) (json.RawMessage, bool, error)`
- `func (repository *jsonRepository) WriteGoalBoard(ctx context.Context, key Key, meta GoalBoardMeta) error`
- `func (repository *jsonRepository) WriteTeamBoard(ctx context.Context, key Key, meta TeamBoardMeta) error`
- `func (repository *jsonRepository) WriteTeamworkPlan(ctx context.Context, key Key, plan TeamworkPlan, maxTeammates int) error`
- `func (repository *jsonRepository) AppendTeamworkEvent(_ context.Context, key Key, event TeamworkEvent) error`
- `func (repository *jsonRepository) AppendTeamworkBinding(_ context.Context, key Key, binding TeamworkBinding) error`
- `func (repository *jsonRepository) ReadTeamworkBindings(_ context.Context, key Key) ([]TeamworkBinding, error)`
- `func (repository *jsonRepository) TeamworkJobOutputDir(_ context.Context, key Key) (string, error)`
- `func (repository *jsonRepository) AppendToolRecordSnapshot(ctx context.Context, key Key, payload any) error`

### message_read_lock_test.go

- `func buildMessageRows(t *testing.T, store *storeEngine, key Key, rows, step int)` — buildMessageRows 提交 rows 行（seq 从 1 起，按 step 分批），返回 message key。
- `func pinFirstReader(t *testing.T) (entered chan struct{}, release func())` — pinFirstReader 把**第一个**进入"锁外解码"的读者钉住：返回的 release 关闭后
- `func TestMessageReadDecodeOutsideWriterLock(t *testing.T)` — TestMessageReadDecodeOutsideWriterLock 证明写者不排在读者后面：
- `func TestMessageReadStaleSnapshotConvergesAfterEviction(t *testing.T)` — TestMessageReadStaleSnapshotConvergesAfterEviction 钉住无锁快照的**可见性契约**：
- `func TestWithPublishedHeadRetriesOnceAfterDrift(t *testing.T)` — TestWithPublishedHeadRetriesOnceAfterDrift 钉住重试契约：解码撞上并发清理
- `func TestWithPublishedHeadReportsErrorWithoutDrift(t *testing.T)` — TestWithPublishedHeadReportsErrorWithoutDrift 反向护栏：坐标没漂移时错误

### module_head_guide_test.go

- `func TestGuideNotRewrittenAfterModuleHeadWrites(t *testing.T)` — TestGuideNotRewrittenAfterModuleHeadWrites 对应 T-DP-04（§2.0 规则 3、

### module_lock_identity_test.go

- `func allStorageModules() []storageModule` — allStorageModules 是 storageModule 的全集。新增枚举必须同步这里——漏掉的模块会在
- `func TestModuleLocksAreDistinct(t *testing.T)` — TestModuleLocksAreDistinct 钉住「模块 ↔ 锁」一对一。
- `func TestUnmappedModuleLockPanicsInsteadOfAliasing(t *testing.T)` — TestUnmappedModuleLockPanicsInsteadOfAliasing 是上一条的护栏：未映射的模块必须
- `func TestMediaReadNotBlockedByHeldMessageLock(t *testing.T)` — TestMediaReadNotBlockedByHeldMessageLock 钉住行为面：他人持 messageMu（= 正在

### project_refresh_key_test.go

- `func writeModuleDotting(t *testing.T, root string, relative ...string)` — writeModuleDotting 在 root 下按相对路径写出模块元数据（docs/gui/module_dotting.json
- `func TestProjectRefreshWriteReadSameKey(t *testing.T)` — TestProjectRefreshWriteReadSameKey 钉住「写入键 = 读取键 = 调用方给的工作区 ID」。
- `func TestProjectRefreshSourcesConfigurable(t *testing.T)` — TestProjectRefreshSourcesConfigurable 钉住来源口径：默认 MetadataPath 对齐真实路径
- `func TestProjectRefreshRealRepositorySources(t *testing.T)` — TestProjectRefreshRealRepositorySources 把默认来源钉在**本仓库真实布局**上：

### provider_content_test.go

- `func providerContentToolRows() []Event`
- `func TestProviderContentPersistsAcrossReload(t *testing.T)` — TestProviderContentPersistsAcrossReload 覆盖「重启恢复」：ProviderContent 必须
- `func TestProviderContentDrivesProviderProjections(t *testing.T)` — TestProviderContentDrivesProviderProjections 覆盖两个 provider 投影出口都取
- `func TestProviderContentFallsBackToContent(t *testing.T)` — TestProviderContentFallsBackToContent 钉住兜底语义：未设置 ProviderContent 的
- `func TestDurableHistoryLoadUsesProviderContent(t *testing.T)` — TestDurableHistoryLoadUsesProviderContent 覆盖 DurableHistory.Load 的滑动窗口
- `func providerContentToolRow(t *testing.T, rows []Event) Event`
- `func providerContentToolWireContent(t *testing.T, assembled wireResult, toolCallID string) string`

### retirement_gate_test.go

- `func TestRetiredChannelsAbsentAfterFullCommit(t *testing.T)` — TestRetiredChannelsAbsentAfterFullCommit 对应 T-DP-01/T-DP-02/T-DP-03
- `func dirNames(t *testing.T, root string) []string`

### role_account_test.go

- `func TestRoleAccountPersistsWithPermissionDefault(t *testing.T)`
- `func TestRoleInheritanceAndUserSpacesShareRecordWithoutErasingVector(t *testing.T)`
- `func TestPermissionVectorUpdatePreservesRoleRecordFields(t *testing.T)`
- `func TestSessionSSHPathsOverrideDefaultAndSurviveVectorUpdate(t *testing.T)`

### role_snapshot_teammate_repro_test.go

- `func TestRoleSnapshotForTeammateCarriesMainRowsWithNoOwnRows(t *testing.T)`

### session_cold_load_bench_test.go

- `func newSessionColdLoadBenchFixture(b *testing.B, rows, rowBytes, compactAt int) *sessionColdLoadBenchFixture` — newSessionColdLoadBenchFixture 在临时目录写入合成会话；构造成本不计入
- `func sessionColdLoadBenchRow(index, rowBytes int) Event` — sessionColdLoadBenchRow 生成可预测的 user/assistant 交替行。内容长度覆盖
- `func BenchmarkSessionColdLoad(b *testing.B)` — BenchmarkSessionColdLoad 测量冷恢复会穿过的主要存储读面：
- `func benchmarkSessionColdLoadOperation( b *testing.B, fixture *sessionColdLoadBenchFixture, operation func(*sessionColdLoadBenchFixture) error, )` — benchmarkSessionColdLoadOperation 执行单一读面并报告每次操作的整分片解码
- `func runSessionColdLoadReads(fixture *sessionColdLoadBenchFixture, includeHistory bool) error` — runSessionColdLoadReads 复刻应用冷加载的并行磁盘读，并在收口后执行 wire

### session_title_test.go

- `func TestSessionTitleDurableThroughMessageHead(t *testing.T)` — TestSessionTitleDurableThroughMessageHead：会话标题持久化在 message head 的
- `func TestSessionTitleSurvivesMessageHeadRebuild(t *testing.T)` — TestSessionTitleSurvivesMessageHeadRebuild：message head 自愈重建（分片

### shard_write_no_decode_test.go

- `func seedMessageRows(t *testing.T, store *storeEngine, key Key, count int)` — 写侧「免整片解码」优化（C1/H3）的验收面。
- `func TestCommitSkipsTailShardDecode(t *testing.T)` — TestCommitSkipsTailShardDecode 结构性证明：末分片索引可信时，一次提交不解码
- `func TestTrustedTailKeepsIndexExact(t *testing.T)` — TestTrustedTailKeepsIndexExact 快路径不改语义：行数/区间/摘要必须与真实文件
- `func TestTrustedTailFallsBackWhenDraftRowsPresent(t *testing.T)` — TestTrustedTailFallsBackWhenDraftRowsPresent 安全性：草稿行 append 到**已索引**

### stack_subagent_test.go

- `func TestStackSubagentBatchSemantics(t *testing.T)` — TestStackSubagentBatchSemantics 对应 T-STK-12（§2.4/§8.2、D6、S18）：
- `func mustActiveSubagents(t *testing.T, store *storeEngine, key Key) []StackItemRecord`
- `func mustHistorySubagents(t *testing.T, store *storeEngine, key Key) []StackItemRecord`

### state_test.go

- `func TestStateRoundTripAcrossLocalBackends(t *testing.T)`
- `func TestCommitRoundTripIsAtomicAcrossLocalBackends(t *testing.T)`
- `func TestWriteStateAfterCommitReturnsLatestState(t *testing.T)`
- `func TestEventTailKeepsCompleteUserAndParallelToolUnits(t *testing.T)`
- `func TestEventTailKeepsTrailingUnansweredUserInput(t *testing.T)`
- `func testMessages(count int, marker string) []types.Message`

### tail_read_amplification_test.go

- `func TestTailReadAmplificationMeasurement(t *testing.T)` — TestTailReadAmplificationMeasurement 量化“尾窗读取”相对“全量读文件”的
- `func maxDuration(left, right time.Duration) time.Duration`

### wire_soft_budget_test.go

- `func TestApplyWireBudgetDerivesSoftThreshold(t *testing.T)` — TestApplyWireBudgetDerivesSoftThreshold 钉住 §5.2 配置的装配侧消费：
- `func TestWireAssemblyStopsAtSoftThreshold(t *testing.T)` — TestWireAssemblyStopsAtSoftThreshold 钉住「软阈值触发压缩」：装配在软阈值处
