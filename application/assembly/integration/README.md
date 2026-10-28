# application/assembly/integration

## 生态位

`application/assembly/integration` 承载必须经过真实 assembly 组合图、但不需要访问
父包私有字段的跨 owner 集成测试。生产代码不依赖本包。

## 职责与非职责

- 做：在测试包内声明 fake ports/依赖变量，构造真实 Service，验证公开
  `ComponentsFor` owner 的协作、事件和错误语义；
- 不做：访问 `service.components`、复制生产状态机或把测试专用接口导出到生产包。

## 目录结构

- `fakes_test.go`、`helpers_test.go`：本测试包私有的 fake ports 与 Service harness；
- `runtime_extra_test.go`：fake Runtime 的会话级项目根能力；
- 其余 `*_test.go`：已迁出的 account、history search、message sequence、notice、
  agent-team fixture、close/shutdown、fault guard、history safety、hot attach、
  context/compaction、plugin source projection、review files、role tool activity、
  role attribution、service/session lifecycle、session history pagination、
  snapshot budget、tool hook、task skill、turn status、session snapshot、
  workspace use cases 等集成用例；
- `legacy_service_test.go` / `context_legacy_helpers_test.go`：仅测试包私有的
  owner 映射与上下文夹具，不作为生产契约。

## 核心实现

测试通过公开的 `assembly.New` 构造 Service，通过 `ComponentsFor(service)` 取得稳定
owner 指针。依赖缺失或类型变化在测试包内修复，不改生产 struct 形状。

## 数据流

本地 fake ports → assembly composition root → `ComponentsFor` owner → 断言/事件。

## 依赖方向

允许依赖 `application/assembly` 的公开面和 contract；禁止导入父包未导出符号，
禁止生产代码反向依赖本测试包。

## 并发与错误

fake Runtime/Engine 按其 public 端口并发语义加锁；测试 harness 在 cleanup 中通过
公开 Lifecycle owner 关闭 catalog/worker。

## 扩展方式

新增依赖优先声明为测试包私有 fake 或通道/recorder 变量，再注入 `Dependencies`。
只有当测试确实不可表达时才扩展 `ComponentsFor` 的真实 owner 指针。

## Review 指南

- 是否只通过公开 API 和 owner 指针访问？
- fake 是否在测试包内，而不是生产包？
- 是否复制了生产判据或重新引入 harness 结构耦合？

## 测试与验证

```text
go test ./application/assembly/integration -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### agentteam_fixture_test.go

- `func newLibrarySessions() *librarySessions`
- `func (s *librarySessions) ReadTeamLibrary(string) (dto.TeamLibrary, error)`
- `func (s *librarySessions) WriteTeamLibrary(_ string, library dto.TeamLibrary) error`
- `func (s *librarySessions) ReadEmployeeLibrary(string) (dto.EmployeeLibrary, error)`
- `func (s *librarySessions) WriteEmployeeLibrary(_ string, library dto.EmployeeLibrary) error`
- `func (s *librarySessions) ReadDefaultOrder(string) (dto.DefaultOrder, error)`
- `func (s *librarySessions) WriteDefaultOrder(_ string, order dto.DefaultOrder) error`
- `func (s *librarySessions) globalWriteCount() int` — globalWriteCount 读当前全局母本写次数（断言"读不写盘"用）。
- `func (s *librarySessions) writeCount() int` — writeCount 读团队库写次数（加锁：直接读 sessions.writes 与写侧没有 happens-before
- `func (s *librarySessions) setLibrary(library dto.TeamLibrary)` — setLibrary 覆盖夹具的团队库（加锁）。
- `func summonService(t *testing.T, sessions contract.SessionPort) *assembly.Service` — summonService 造一个带会话端口的服务，并把视图会话钉成固定 ID

### approval_session_ownership_test.go

- `func waitApprovalCount(t *testing.T, service *legacyTestService, sessionID string, want int)`
- `func TestApprovalSessionOwnershipStatusAndSnapshot(t *testing.T)` — TestApprovalSessionOwnershipStatusAndSnapshot 波 4 approval 会话级归属：
- `func TestBackgroundApprovalDoesNotClobberViewSlotAndResolvesById(t *testing.T)` — TestBackgroundApprovalDoesNotClobberViewSlotAndResolvesById 波 4 归属：
- `func TestApprovalConcurrentSessionsStayAttributed(t *testing.T)` — TestApprovalConcurrentSessionsStayAttributed -race 靶场：两会话并发开
- `func startsWithApprovalID(id, prefix string) bool`

### archive_reasoning_persist_test.go

- `func TestPersistKeepsToolNarrationAndReasoningInRecord(t *testing.T)`

### archive_session_test.go

- `func newArchiveCommandSessions() *archiveCommandSessions`
- `func (sessions *archiveCommandSessions) add(projectID, sessionID string)`
- `func (sessions *archiveCommandSessions) recordFor(projectID, sessionID string) (model.SessionRecord, bool)`
- `func (sessions *archiveCommandSessions) SaveSessionRecordWorkspace(projectID, sessionID string, record model.SessionRecord) error`
- `func (sessions *archiveCommandSessions) SaveSessionRecord(sessionID string, record model.SessionRecord) error`
- `func (sessions *archiveCommandSessions) LoadSessionRecordWorkspace(projectID, sessionID string) (model.SessionRecord, error)`
- `func (sessions *archiveCommandSessions) LoadSessionRecord(sessionID string) (model.SessionRecord, error)`
- `func (sessions *archiveCommandSessions) SessionsOf(projectID string) []model.SessionInfo` — SessionsOf 以 record 状态覆盖目录行（与生产 adapter 一致）。
- `func archiveTestService(t *testing.T, sessions *archiveCommandSessions) *legacyTestService`
- `func TestArchiveSessionHidesSessionFromItsProjectGrid(t *testing.T)` — TestArchiveSessionHidesSessionFromItsProjectGrid C2 契约：归档后 record
- `func TestArchiveSessionRejectsBusySessions(t *testing.T)` — TestArchiveSessionRejectsBusySessions C2 门控：running/queued/awaiting
- `func TestArchiveSessionMissingRecord(t *testing.T)` — TestArchiveSessionMissingRecord 钉住失败路径：会话不存在/record 缺失时

### async_completion_trigger_test.go

- `func TestAsyncCompletionFacadeIsRetiredFromService(t *testing.T)`
- `func asyncCompletionHarness(t *testing.T, trigger bool, engine contract.ChatEngine) (*legacyTestService, *fakeRuntime)` — asyncCompletionHarness 造一个装配好该触发路径的 Service。
- `func waitForTriggeredTurn(t *testing.T, service *legacyTestService, needle string) string` — waitForTriggeredTurn 轮询可见会话，直到出现一条含 needle 的用户行（= 触发回合开
- `func assertNoTurn(t *testing.T, service *legacyTestService, needle string)` — assertNoTurn 报告可见会话里有没有含 needle 的用户行——"不该触发"的用例用它。
- `func completedRecord(handle string, state dto.AsyncState) dto.AsyncRunRecord` — completedRecord 造一条已落到终态的作业投影记录。
- `func TestAsyncCompletionTriggersIdleSessionTurn(t *testing.T)`
- `func TestAsyncCompletionTriggersOnFailureToo(t *testing.T)`
- `func TestAsyncCompletionIgnoresRunningAndKilled(t *testing.T)`
- `func TestAsyncCompletionDoesNotWakeBusySession(t *testing.T)` — 铁律 §6.1「绝不唤醒忙会话」：正在跑的会话不被打断、也不被塞队列——条目刻意不记账，
- `func TestAsyncCompletionTriggersOncePerHandle(t *testing.T)`
- `func TestAsyncCompletionStaysOffWhenDisabled(t *testing.T)` — 开关默认关：不置 trigger_conversation 时信号只驱动工作表格重投影，终态不会起任何回合。
- `func TestAsyncCompletionIgnoresJobWithoutSession(t *testing.T)` — 没有会话归属的作业（别的进程的作业归属、或会话已被删除）不该触发：没有可起的回合

### chat_cold_load_narration_repro_test.go

- `func (s *coldNarrationStore) SaveSessionRecordWorkspace(_, _ string, record model.SessionRecord) error`
- `func (s *coldNarrationStore) LoadSessionRecordWorkspace(workspaceID, sessionID string) (model.SessionRecord, error)`
- `func TestColdLoadKeepsToolWheelNarration(t *testing.T)`

### chat_delegation_test.go

- `func (engine *singleLoopEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *singleLoopEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 显式转发到自身 ChatStream（会话路由面下仍单次提交）。
- `func TestLoopDelegatedToSeele(t *testing.T)` — TestLoopDelegatedToSeele（UC6）：core 无自编 ReAct 循环——一次提交只

### chat_error_surface_test.go

- `func (failingSnapshotSessions) SaveSessionSnapshot(string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult) error`
- `func (failingSnapshotSessions) SaveSessionSnapshotWorkspace(string, string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult) error`
- `func TestRunChatUsesRawErrorWhileToolProjectionUsesPresentedError(t *testing.T)`
- `func TestRunChatLogsRawUnclassifiedError(t *testing.T)`
- `func TestPersistenceFailureDoesNotClaimProgressWasSaved(t *testing.T)`
- `func conversationContains(conversation []model.Message, role, fragment string) bool`

### chat_hot_attach_reasoning_repro_test.go

- `func TestHotMountKeepsStepReasoningInVisibleWindow(t *testing.T)`

### chat_state_event_test.go

- `func TestChatStateIsPublishedNotInferred(t *testing.T)`

### close_semantics_test.go

- `func TestAnyChatRunningReflectsBackgroundSessions(t *testing.T)` — TestAnyChatRunningReflectsBackgroundSessions G0c 判定面：视图快照只镜像
- `func TestCancelAllChatsDrainsEveryRunningSession(t *testing.T)` — TestCancelAllChatsDrainsEveryRunningSession G0c 超时路径：取消必须覆盖

### compressed_turn_test.go

- `func testStringPtr(value string) *string`
- `func (f *fakeCommitSession) SaveCommitWorkspace(projectID, sessionID string, commit store.Commit) error`
- `func TestCompressedTurnArchiverScopesCommitToSessionProject(t *testing.T)` — TestCompressedTurnArchiverScopesCommitToSessionProject 写侧项目作用域：先按
- `func (f *fakeTranscriptSession) LoadTranscriptTailWorkspace(_, _ string, _, _ int) ([]model.TranscriptEvent, error)`
- `func (f *fakeTranscriptSession) LoadToolResultWorkspace(_, _, ref string) (model.StoredToolResult, error)`
- `func TestCompressedTurnArchiverPersistsOriginal(t *testing.T)` — TestCompressedTurnArchiverPersistsOriginal 写侧：溢出轮次原文序列化后
- `func TestCompressedTurnArchiverRoutesByContextSessionID(t *testing.T)` — TestCompressedTurnArchiverRoutesByContextSessionID 写侧归属（G0a 回归）：
- `func TestCompressedTurnArchiverFallsBackToProviderWithoutContext(t *testing.T)` — TestCompressedTurnArchiverFallsBackToProviderWithoutContext 无 ctx 注入时
- `func TestCompressedTurnArchiverRejectsWithoutSessionID(t *testing.T)` — TestCompressedTurnArchiverRejectsWithoutSessionID ctx 与 provider 都拿不到
- `func TestReadCompressedTurnHandlerReadsOriginal(t *testing.T)` — TestReadCompressedTurnHandlerReadsOriginal 读侧：read_compressed_turn

### content_lru_test.go

- `func withContentLimit(limit int) func()` — withContentLimit 临时把进程级 loaded_content_limit 改成 limit，返回还原
- `func newMultiPagedStore(counts map[string]int) *multiPagedStore`
- `func (store *multiPagedStore) SessionsOf(projectID string) []model.SessionInfo` — SessionInfo 行只带标题（零正文读）：目录刷新不会把正文拉回内存。
- `func (store *multiPagedStore) LoadHistory(sessionID string) ([]contract.EngineMessage, error)`
- `func (store *multiPagedStore) LoadHistoryRange(sessionID string, offset, limit int) ([]contract.EngineMessage, int, error)`
- `func (store *multiPagedStore) LoadSessionRecordWorkspace(workspaceID, sessionID string) (model.SessionRecord, error)`
- `func (store *multiPagedStore) SaveSessionRecord(sessionID string, record model.SessionRecord) error` — SaveSessionRecord* 是 no-op：本夹具的 durable 消息由 messages 槽给出，卸载
- `func (store *multiPagedStore) SaveSessionRecordWorkspace(workspaceID, sessionID string, record model.SessionRecord) error`
- `func (store *multiPagedStore) LoadSessionRecord(sessionID string) (model.SessionRecord, error)`
- `func (store *multiPagedStore) LoadConversationRangeWorkspace(workspaceID, sessionID string, offset, limit int) ([]model.Message, int, error)`
- `func (store *multiPagedStore) messageCount(sessionID string) int`
- `func readContentState(t *testing.T, service *legacyTestService, sessionID string) contentState`
- `func assertContentLoaded(t *testing.T, service *legacyTestService, sessionID string, want bool) contentState`
- `func assertContentWindow(t *testing.T, service *legacyTestService, sessionID string, offset, count int)` — assertContentWindow 断言会话当前持有「尾部窗口 offset 起 count 条」的正文。
- `func waitForContentLoaded(t *testing.T, service *legacyTestService, sessionID string) contentState` — waitForContentLoaded 轮询等待会话正文装载完成：运行中会话存在时冷加载走
- `func waitForContentUnloaded(t *testing.T, service *legacyTestService, sessionID string) contentState` — waitForContentUnloaded 轮询等待内容 LRU 真正把正文卸载（区别于「本来就
- `func snapshotSessions(t *testing.T, service *legacyTestService) map[string]model.SessionInfo` — snapshotSessions 等一轮目录收敛后返回左侧列表行（按 ID 索引）。
- `func TestLoadedContentLayerLimitDefaults(t *testing.T)` — TestLoadedContentLayerLimitDefaults 内容层的限额口径（2026-10-14）：
- `func TestContentLimitEvictsLeastRecentlyUsedIdleContent(t *testing.T)` — TestContentLimitEvictsLeastRecentlyUsedIdleContent 上限 1：冷加载第二个会话
- `func TestContentLimitKeepsBusyAndActiveSessions(t *testing.T)` — TestContentLimitKeepsBusyAndActiveSessions 上限 1：运行中（busy）会话与当前
- `func TestContentEvictionColdReloadsSameWindow(t *testing.T)` — TestContentEvictionColdReloadsSameWindow 会话正文被卸载后再激活（冷回读）
- `func TestEnsureSessionContentRebuildsEvictedWindow(t *testing.T)` — TestEnsureSessionContentRebuildsEvictedWindow 热挂载回读面

### context_budget_frequency_test.go

- `func TestContextBudgetDoesNotRecompactWithoutNewContent(t *testing.T)`

### context_budget_last_resort_test.go

- `func pinMechanismCompactionRatios(t *testing.T)`
- `func TestContextBudgetOvershootKeepsNewestSettledRound(t *testing.T)` — TestContextBudgetOvershootKeepsNewestSettledRound：达峰装配时单个已定稿
- `func TestContextBudgetProactivelyCompactsAtHardThreshold(t *testing.T)` — TestContextBudgetProactivelyCompactsAtHardThreshold：装配结果落在硬阈值
- `func TestContextBudgetOvershootCompactsWhenNewestExceedsFullBudget(t *testing.T)` — TestContextBudgetOvershootCompactsWhenNewestExceedsFullBudget：最新轮自身

### context_budget_margin_idempotency_test.go

- `func pinIneffectiveCompactRatios(t *testing.T)`
- `func TestContextBudgetSkipsCompactionWithoutMargin(t *testing.T)` — TestContextBudgetSkipsCompactionWithoutMargin 钉住软线压缩的**幂等/有效性校验**：

### context_cache_divergence_probe_test.go

- `func TestContextCacheDivergenceProbe_TwoTurnToolCliff(t *testing.T)` — TestContextCacheDivergenceProbe_TwoTurnToolCliff 驱动 1 个含工具调用的回合
- `func runPrefixProbeTwoTurns(t *testing.T, cfg prefixProbeConfig)`
- `func TestContextCacheDivergenceProbe_MemoryBlockPosition(t *testing.T)` — TestContextCacheDivergenceProbe_MemoryBlockPosition 用生产装配器
- `func probeAccumulatedHistory(messages, charsPerMessage int) []types.Message`
- `func probeAssembleRequest(t *testing.T, assembler seelectx.RequestAssembler, query string, accumulated []types.Message) seelectx.AssembledRequest`
- `func probeSerializeAssembly(request seelectx.AssembledRequest) string`
- `func probeMessageIndex(request seelectx.AssembledRequest, marker string) int`
- `func probeRole(request seelectx.AssembledRequest, marker string) string`
- `func probeMessageIndexByBytes(request seelectx.AssembledRequest, offset int) int`
- `func probeRoleByBytes(request seelectx.AssembledRequest, offset int) string`
- `func probeBlockTokens(block *seelectx.PromptBlock) int`
- `func probeBlockText(block *seelectx.PromptBlock) string`
- `func probeSegmentIDs(selected []memory.Candidate) []string`
- `func probeSameSegments(left, right []memory.Candidate) bool`

### context_cache_smoke_test.go

- `func smokeService(t *testing.T, window int) (*legacyTestService, *fakeEngine)` — smokeService 构建一个可驱动生产装配路径的服务。
- `func appendSettledRound(service *legacyTestService, round int, toolResultToken int)` — appendSettledRound 追加一个"已定稿轮次"的 transcript 事件（user → assistant
- `func systemPromptFor(service *legacyTestService) string` — systemPromptFor 取当前 system（与 Prepare 内部使用的同一组装函数）。
- `func serText(label, text string) string` — serText 生成 JSON 前缀友好串行化片段（固定键序、无长度前缀；内容前缀相等
- `func serializeRequest(system string, history []contract.EngineMessage, input string, tools []model.Tool) string` — serializeRequest 把一次请求（system + history + current input + tools）固化为
- `func lcpBytes(a, b string) int` — lcpBytes 返回两个字节串的最长公共前缀长度。
- `func blockFloor(shared, block int) int` — blockFloor 按缓存块粒度对齐（provider 只命中整块）。
- `func measure(prev, current string, totalTok int) (shared int, ratioRaw, ratio64, ratio1k float64)` — measure 计算本轮与上一轮请求的命中指标（block 粒度按 provider 整块缓存语义
- `func logSmokeRow(t *testing.T, row smokeTraceRow, input string)` — smokeCacheHitRow 汇总输出一行。
- `func TestContextCacheSmoke_SingleSessionLongRun(t *testing.T)` — TestContextCacheSmoke_SingleSessionLongRun 复现"单会话多轮长会话"的最优可达
- `func TestContextCacheSmoke_SkillActivationAppendOnly(t *testing.T)` — TestContextCacheSmoke_SkillActivationAppendOnly 对照场景：会话中段激活一个
- `func TestContextCacheSmoke_ProviderCacheContentionModel(t *testing.T)` — TestContextCacheSmoke_ProviderCacheContentionModel 是一个显式标注的推演模型：

### context_compact_across_rounds_repro_test.go

- `func TestReproCompactionRecordSurvivesNextRoundAfterColdMaintenance(t *testing.T)` — TestReproCompactionRecordSurvivesNextRoundAfterColdMaintenance 冷加载维护
- `func TestReproContextFactsSurviveCompletedTurnBoundary(t *testing.T)` — TestReproContextFactsSurviveCompletedTurnBoundary 回合**正常收尾**

### context_compact_failure_trace_repro_test.go

- `func compactionFailureRuntime() *compactionReadbackRuntime` — compactionFailureRuntime 是"摘要器装好了、但这次真的拿不到模型读后感"的宿主：
- `func appendSizedRounds(t *testing.T, service *legacyTestService, taskID string, rounds int, asciiRunes int)` — appendSizedRounds 追加 rounds 个已定稿轮次，每轮的 assistant 正文按 asciiRunes 个
- `func startCompactionFailureSession(t *testing.T, taskID string) (*legacyTestService, *fakeEngine, string)` — startCompactionFailureSession 起一个任务执行中的会话（/compact 的显式路径）。
- `func startCompactionFailureSessionWith(t *testing.T, runtime contract.RuntimePort, taskID string) (*legacyTestService, *fakeEngine, string)` — startCompactionFailureSessionWith 同上，但宿主由调用方给——两种失败形态（回执里
- `func historyHasContent(history []contract.EngineMessage, needle string) bool` — historyHasContent 报告引擎工作历史里是否还留着某段正文。
- `func TestCompactionFailureKeepsWholeContextAndLeavesTrace(t *testing.T)` — TestCompactionFailureKeepsWholeContextAndLeavesTrace（红灯复现①）
- `func TestCompactionFailureDoesNotInterruptOverBudgetSession(t *testing.T)` — TestCompactionFailureDoesNotInterruptOverBudgetSession（红灯复现②）
- `func TestCompactionFailureNoteCarriesReadbackEvidence(t *testing.T)` — TestCompactionFailureNoteCarriesReadbackEvidence（② 的留痕补丁，先红后绿）
- `func TestCompactionFailureNoteCarriesReadbackProbeError(t *testing.T)` — TestCompactionFailureNoteCarriesReadbackProbeError：读数闸**调用本身报错**（不是
- `func TestFrontendCompactionFailureErrorMarkerMatchesBackend(t *testing.T)` — TestFrontendCompactionFailureErrorMarkerMatchesBackend：失败痕 note 的报错段标记是

### context_compact_gate_test.go

- `func seedLongRounds(t *testing.T, service *legacyTestService, requestID string)` — seedLongRounds 与 TestCompactContextHandlerCompactsTranscript 同一份量：4 轮、每轮
- `func TestCompactContextWaitsForHeldCompactionGate(t *testing.T)` — TestCompactContextWaitsForHeldCompactionGate 钉住等待方向：门已被占用时，第二次

### context_compact_index_test.go

- `func (recorder *compactionIndexRecorder) push( _ context.Context, sessionID string, request context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)`
- `func (recorder *compactionIndexRecorder) snapshot() ([]context_runtime.CompactionIndexRequest, []string)`
- `func (runtime *compactionIndexRuntime) PushCompactionFrame( ctx context.Context, sessionID string, request context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)`
- `func indexGateDetail(t *testing.T, frames []compactionProgressFrame) string` — indexGateDetail 取出本轮进度里门禁 index 的事实行（找不到直接失败：一轮真压缩
- `func appendIndexRounds(t *testing.T, service *legacyTestService, taskID string)` — appendIndexRounds 追加 4 个已定稿轮（每轮约 4 万 tokens），足以越过软阈值触发
- `func TestCompactionPushesFrameIntoIndex(t *testing.T)` — TestCompactionPushesFrameIntoIndex：装配层压缩必须把这次折出的区间推进会话
- `func TestCompactionWithoutIndexFaceReportsDegradedGate(t *testing.T)` — TestCompactionWithoutIndexFaceReportsDegradedGate：索引面未装配（Runtime 不实现
- `func TestCompactionPushFailureIsReportedNotFatal(t *testing.T)` — TestCompactionPushFailureIsReportedNotFatal：索引面在、推帧报错时，压缩与本次请求
- `func TestCompactionWithoutOverflowReportsSkippedNotUnavailable(t *testing.T)` — TestCompactionWithoutOverflowReportsSkippedNotUnavailable：索引面在，但这次压缩**没有

### context_compact_local_fallback_repro_test.go

- `func (runtime *compactionReadbackRuntime) ReadbackCompactionSummary( _ context.Context, _ string, request context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)`
- `func TestCompactionFailureWithFailedReadbackLeavesContextUntouched(t *testing.T)` — TestCompactionFailureWithFailedReadbackLeavesContextUntouched（现场复现）：
- `func TestCompactionWithReadbackStillCompacts(t *testing.T)` — TestCompactionWithReadbackStillCompacts：对照组——读数**拿到了**模型读后感

### context_compact_no_summary_test.go

- `func (runtime *compactionIndexNoSummaryRuntime) CompactionSummaryAvailable() bool`
- `func TestCompactionFailureLeavesContextAndStackUntouched(t *testing.T)` — TestCompactionFailureLeavesContextAndStackUntouched 钉住用户口径

### context_compact_progress_test.go

- `func drainCompactionProgress(t *testing.T, subscription event.Subscription) []compactionProgressFrame` — drainCompactionProgress 取出订阅里已排队的 compaction.progress。发布与装配
- `func gateSequence(frames []compactionProgressFrame) []string` — gateSequence 抽出运行中门禁的 id 序列（终局事件不带 gate；起手帧不是"某一关
- `func assertProgressShape(t *testing.T, frames []compactionProgressFrame, wantSession string)` — assertProgressShape 校验所有来路都要守的公共形状：路由键齐、序号单调、总数
- `func assertBeginFrame(t *testing.T, frames []compactionProgressFrame)` — assertBeginFrame 钉起手帧的事实性：显式压缩在动第一个重活之前就把"这一轮开始
- `func TestExplicitCompactEmitsOrderedProgressGates(t *testing.T)` — TestExplicitCompactEmitsOrderedProgressGates：/compact 显式压缩时逐关报告，
- `func TestAutoCompactionEmitsProgressGates(t *testing.T)` — TestAutoCompactionEmitsProgressGates：自动路径（软阈值）在回合执行中压缩时
- `func TestExplicitCompactGateTimeline(t *testing.T)` — TestExplicitCompactGateTimeline：逐关计时是这一轮压缩**串行工作**的唯一证据。
- `func TestCompactProgressTerminatesOnAssemblyError(t *testing.T)` — TestCompactProgressTerminatesOnAssemblyError：装配失败也必须收口。结构性超限
- `func TestNoProgressEventsWithoutFold(t *testing.T)` — TestNoProgressEventsWithoutFold：没压缩就没有进度。「登记为下一条消息兑现」
- `func TestMaintenanceCompactEmitsOrderedProgressGates(t *testing.T)` — TestMaintenanceCompactEmitsOrderedProgressGates：会话级维护身份路径（冷加载、
- `func TestCompactReceiptCarriesGateChecklist(t *testing.T)` — TestCompactReceiptCarriesGateChecklist：回执自带门禁清单（逐关 id + 毫秒）。
- `func TestFrontendGateLabelsMatchBackendOrder(t *testing.T)` — TestFrontendGateLabelsMatchBackendOrder：门禁 id 是跨语言协议字面量——后端

### context_compact_selfdeadlock_repro_test.go

- `func (stub *archiverSessionsStub) SaveCommitWorkspace(_, _ string, _ store.Commit) error`
- `func (stub *archiverSessionsStub) commitCount() int`
- `func (runtime *productionArchiveIndexRuntime) PushCompactionFrame( ctx context.Context, _ string, request context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)`
- `func TestExplicitCompactFramePushSelfDeadlocksRepro(t *testing.T)` — TestExplicitCompactFramePushSelfDeadlocksRepro：在真实归档接线（boot 的 CompressedTurnArchiver 接线 同形）

### context_compact_test.go

- `func compactTestService(t *testing.T, requestID string) (*legacyTestService, *fakeEngine, string)` — compactTestService 构造带活跃任务执行的会话：主动压缩绑定请求纪元
- `func TestCompactContextHandlerCompactsTranscript(t *testing.T)` — TestCompactContextHandlerCompactsTranscript：compact_context 工具（= /compact
- `func TestCompactManualCompactsBelowThreshold(t *testing.T)` — TestCompactManualCompactsBelowThreshold：显式压缩（/compact、compact_context）
- `func TestCompactManualAfterTurnRecordsExplicitOrigin(t *testing.T)` — TestCompactManualAfterTurnRecordsExplicitOrigin：回合已收尾（任务状态不再是
- `func TestCompactAfterTurnSurfacesRecordWithoutTaskFace(t *testing.T)` — TestCompactAfterTurnSurfacesRecordWithoutTaskFace：快照里还没有任务面时
- `func TestCompactionFrameBodyIsReadableByRef(t *testing.T)` — TestCompactionFrameBodyIsReadableByRef：记录里的 frame_ref 真能读回帧正文——
- `func TestAutoCompactionAfterTurnKeepsRecordGate(t *testing.T)` — TestAutoCompactionAfterTurnKeepsRecordGate：自动路径（软/硬阈值）在回合已
- `func TestCompactContextWithoutTaskExecutionCompactsImmediately(t *testing.T)` — TestCompactContextWithoutTaskExecutionCompactsImmediately：会话没有任务执行
- `func TestCompactWithoutEpochKeepsExecutionFacesClean(t *testing.T)` — TestCompactWithoutEpochKeepsExecutionFacesClean：会话级维护身份不得在可见面
- `func TestCompactEmptySessionRegistersAndRedeemsOnNextMessage(t *testing.T)` — TestCompactEmptySessionRegistersAndRedeemsOnNextMessage：会话真的没有可压缩
- `func TestCompactCommandRegisteredAndSharesPath(t *testing.T)` — TestCompactCommandRegisteredAndSharesPath：/compact 命令注册成功，且与工具
- `func TestCompactCommandWithoutEpochCompactsImmediately(t *testing.T)` — TestCompactCommandWithoutEpochCompactsImmediately：命令入口（用户真的按回车的
- `func TestCompactCommandNoticeReportsCompactedRange(t *testing.T)` — TestCompactCommandNoticeReportsCompactedRange：记录分支的提示只说**记录里已有的
- `func TestCompactionRangeLabel(t *testing.T)` — TestCompactionRangeLabel：区间渲染只在**有边界**时成段——空区间返回空串
- `func TestCompactCommandNeverReportsCompactionWithoutRecord(t *testing.T)` — TestCompactCommandNeverReportsCompactionWithoutRecord：/compact 是显式路径，只要

### context_compact_trigger_liveness_test.go

- `func (runtime *reentrantGateIndexRuntime) PushCompactionFrame( _ context.Context, sessionID string, _ context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)`
- `func triggerLivenessFixture(t *testing.T, requestID string) (*legacyTestService, *reentrantGateIndexRuntime, string)` — triggerLivenessFixture 是一个"已越过硬阈值、必折出非空溢出区"的会话：
- `func releaseGate(runtime *reentrantGateIndexRuntime) func()` — releaseGate 造一个**幂等**放行器：返回的函数可以反复调用（含 defer + 显式调用），
- `func awaitPushEntered[T any](t *testing.T, runtime *reentrantGateIndexRuntime, inFlight <-chan T, what string)` — awaitPushEntered 等到夹具真的走到推帧（否则判据是空集上的真命题）。
- `func assertInteractionFaceLive(t *testing.T, service *legacyTestService, what string)` — assertInteractionFaceLive 断言交互面四个入口在推帧进行中照常返回。任一被冻 =
- `func TestAutoCompactionPushKeepsInteractionFaceLive(t *testing.T)` — TestAutoCompactionPushKeepsInteractionFaceLive：**自动**入口（rawTokens ≥ 硬阈值，
- `func TestMaintenanceCompactionPushKeepsInteractionFaceLive(t *testing.T)` — TestMaintenanceCompactionPushKeepsInteractionFaceLive：**维护入口**
- `func TestSessionLevelCompactPushKeepsInteractionFaceLive(t *testing.T)` — TestSessionLevelCompactPushKeepsInteractionFaceLive：**无在飞回合的会话级**
- `func TestExplicitCompactionWhileAutoCompactionInFlightDoesNotInterlock(t *testing.T)` — TestExplicitCompactionWhileAutoCompactionInFlightDoesNotInterlock：**显式与自动并发**。
- `func TestCompactionChurnOnOneSessionDoesNotHang(t *testing.T)` — TestCompactionChurnOnOneSessionDoesNotHang：把触发入口混在一起反复跑（显式压缩

### context_compact_viewmu_hold_repro_test.go

- `func (runtime *blockingIndexRuntime) PushCompactionFrame( ctx context.Context, _ string, _ context_runtime.CompactionIndexRequest, ) (context_runtime.CompactionIndexReceipt, error)`
- `func returnsWithin(d time.Duration, fn func()) bool` — returnsWithin 报告 fn 是否在 d 之内返回（返回 false = 阻塞住了）。
- `func TestExplicitCompactReproViewMuHoldAcrossFramePush(t *testing.T)` — TestExplicitCompactReproViewMuHoldAcrossFramePush：推帧进行中，交互面是否仍然可用。

### context_compaction_body_missing_repro_test.go

- `func TestCompactionMaterialMustCarryConversationBody(t *testing.T)`
- `func TestCompactionMaterialContrastEmptyEngineHistory(t *testing.T)` — ── 对照组（断言当前行为，不是红灯）────────────────────────────────────
- `func dialogueRows(messages []contract.EngineMessage) []string` — dialogueRows 返回引擎历史里的**对话正文行**（role=user/assistant 且内容非空）。
- `func describeRoles(messages []contract.EngineMessage) string` — describeRoles 渲染素材的角色构成（红线里要能一眼看出「全是 system」）。
- `func firstLine(content string) string`

### context_compaction_facts_restore_repro_test.go

- `func (sessions *retiredRecordChannelSessions) SaveSessionRecord(_ string, record model.SessionRecord) error`
- `func (sessions *retiredRecordChannelSessions) SaveSessionRecordWorkspace(_, _ string, record model.SessionRecord) error`
- `func (sessions *retiredRecordChannelSessions) persist(record model.SessionRecord) error` — persist 模拟 v8 的落盘：record 通道收下 status/title/对话，`execution` 子树丢弃
- `func (sessions *retiredRecordChannelSessions) LoadCompactionRecordsWorkspace(_, _ string) ([]model.ContextCompaction, error)`
- `func restartCompactionRuntime(recorder *compactionIndexRecorder, source string) *compactionReadbackRuntime` — restartCompactionRuntime 造一个 200k 窗口的宿主；readback 决定这次压缩能不能
- `func startRestartSession(t *testing.T, store *retiredRecordChannelSessions, runtime contract.RuntimePort, requestID string, rounds int) (*legacyTestService, string)` — startRestartSession 起一个任务执行中的会话并追加 rounds 个已定稿轮次
- `func persistRestartSession(t *testing.T, service *legacyTestService, store *retiredRecordChannelSessions, sessionID string)` — persistRestartSession 按会话落盘（事件流是会话自己的 durable 事实，重启后按同一
- `func TestReproCompactionFactsSurviveProcessRestart(t *testing.T)` — TestReproCompactionFactsSurviveProcessRestart 重启后压缩栈必须从存储读回来：
- `func TestReproCompactionFailureTraceSurvivesProcessRestart(t *testing.T)` — TestReproCompactionFailureTraceSurvivesProcessRestart 压缩失败痕同样要跨重启：

### context_compaction_real_session_probe_test.go

- `func realProbeEnv(key, fallback string) string`
- `func realProbeEnvInt(key string, fallback int) int`
- `func realProbeRepoRoot(t *testing.T) string` — realProbeRepoRoot 从测试的工作目录（go test 把 CWD 设在包目录）向上找到仓库根。
- `func realProbeResolve(t *testing.T, path string) string` — realProbeResolve 把相对路径解析到仓库根下（绝对路径原样保留）。
- `func realProbeCopyTree(t *testing.T, src, dst string)`
- `func realProbeTranscriptEvent(row store.Event) model.TranscriptEvent`
- `func realProbeSessionID(t *testing.T, sessionDir string) string` — realProbeSessionID 从会话自己的 message head 读回 Router 的会话键：磁盘目录名是
- `func realProbeProjectID(t *testing.T, root, sessionID string) string` — realProbeProjectID 从 workspace_index.json 的 bindings（会话 → 项目）读回项目键。
- `func realProbeLoadFacts(t *testing.T) *realProbeFacts` — realProbeLoadFacts 只读拷贝真实会话目录到临时根，再按生产读面把事实读回来。
- `func realProbeWindowConfig(t *testing.T) cc.WindowConfig` — realProbeWindowConfig 读真实 seelex.yaml 的 window 段（缺省档见探针文件头）。
- `func realProbeWindow(t *testing.T) int`
- `func realProbeSelectEvents(facts *realProbeFacts, fromSeq, toSeq uint64) []model.TranscriptEvent`
- `func realProbeRunArm(t *testing.T, facts *realProbeFacts, arm realProbeArm) realProbeOutcome` — realProbeRunArm 在**被动**路径上跑一轮：自动装配（判据命中才折）。
- `func realProbeDescribeRecords(records []model.ContextCompaction) string`
- `func realProbeDumpReport(t *testing.T, text string)`
- `func TestRealSessionCompactionChainProbe(t *testing.T)` — TestRealSessionCompactionChainProbe 真实会话记录上的「第一次压缩触发点 → 被动压缩

### context_compaction_restart_refill_repro_test.go

- `func startRestartRound(t *testing.T, service *legacyTestService, sessionID, requestID string)` — startRestartRound 在重启恢复后的会话上开新回合（自动路径）：**把上一份状态传下去**
- `func TestProbeCompactionFailureAfterRestartEncounter(t *testing.T)` — TestProbeCompactionFailureAfterRestartEncounter（**红灯复现，等待口径裁决**）
- `func TestRestartRefillCompactionFailureKeepsTraceWithReadbackError(t *testing.T)` — TestRestartRefillCompactionFailureKeepsTraceWithReadbackError——「重启 → 越线 →

### context_compactions_restore_repro_test.go

- `func compactedSessionStore(t *testing.T) (*archiveSessions, string, model.ContextCompaction, int)` — compactedSessionStore 造一份「重启前」的持久面并结束进程：第一份 Service 在
- `func coldResume(t *testing.T, store *archiveSessions, sessionID string) *legacyTestService` — coldResume 在 store 上新建 Service（进程重启）并冷加载会话。
- `func TestReproCompactionStackSurvivesProcessRestart(t *testing.T)` — TestReproCompactionStackSurvivesProcessRestart 应用重启后，旧会话的压缩栈
- `func TestReproCompactionStackVisibleWithoutProjection(t *testing.T)` — TestReproCompactionStackVisibleWithoutProjection projection 缺失（老记录、
- `func TestReproCompactionRetainedFromRestoredFromRecord(t *testing.T)` — TestReproCompactionRetainedFromRestoredFromRecord 冷恢复时保留窗口起点

### context_controller_test.go

- `func (runtime runtimeWithContextLimits) ContextWindow() int`
- `func (runtime runtimeWithContextLimits) MaxOutputTokens() int`
- `func TestRejectToolResultsPreservesPairingWithoutPreview(t *testing.T)`
- `func TestPrepareExecutionContextCountsActiveSystemPrompt(t *testing.T)` — TestPrepareExecutionContextCountsActiveSystemPrompt：system 提示自身就超出
- `func TestPrepareExecutionContextUsesRuntimeContextLimits(t *testing.T)`
- `func TestPreparedRequestAutonomouslyCompactsOversizedRounds(t *testing.T)`
- `func TestPrepareExecutionContextOrderAndNoCheckpoint(t *testing.T)`
- `func TestTranscriptTailKeepsInterruptedRoundAndDropsOrphanToolProtocols(t *testing.T)`
- `func TestTranscriptTailKeepsTrailingUnansweredUserInput(t *testing.T)`
- `func TestTranscriptTailAccumulatesAllSettledRoundsWhenUnlimited(t *testing.T)`
- `func TestPrepareExecutionContextAccumulatesAllSettledRoundsAndByteStable(t *testing.T)`
- `func historyContents(history []contract.EngineMessage) []string`
- `func TestRejectToolResultsRecognizesFrameworkTruncationMarker(t *testing.T)`

### context_hook_test.go

- `func TestIterationHookDoesNotTriggerContextControl(t *testing.T)`

### context_legacy_helpers_test.go

- `func (sessions *contextAwareSessions) AttachSessionContext(workspaceID, sessionID string) error`
- `func (sessions *contextAwareSessions) DetachSessionContext()`
- `func compactionRecordsOf(t *testing.T, service *legacyTestService) []model.ContextCompaction`
- `func (sessions *archiveSessions) SessionsOf(projectID string) []model.SessionInfo`
- `func (sessions *archiveSessions) setSessionID(sessionID string)`
- `func (sessions *archiveSessions) SaveCurrent(string) error`
- `func (sessions *archiveSessions) LoadHistory(string) ([]contract.EngineMessage, error)`
- `func (sessions *archiveSessions) SaveSessionRecord(_ string, record model.SessionRecord) error`
- `func (sessions *archiveSessions) SaveSessionRecordWorkspace(_ string, _ string, record model.SessionRecord) error`
- `func (sessions *archiveSessions) LoadSessionRecord(string) (model.SessionRecord, error)`
- `func (sessions *archiveSessions) LoadSessionRecordWorkspace(string, string) (model.SessionRecord, error)`
- `func (sessions *archiveSessions) LoadTranscriptTailWorkspace(_, _ string, tokenBudget, maxUnits int) ([]model.TranscriptEvent, error)`
- `func (sessions *archiveSessions) LoadToolResultWorkspace(string, string, string) (model.StoredToolResult, error)`

### context_narration_attribution_test.go

- `func TestToolNarrationStaysWithOwningIteration(t *testing.T)`

### context_prefix_invariant_test.go

- `func TestContextPrefixInvariant_CrossTurn(t *testing.T)`
- `func TestContextPrefixInvariant_EmptyToolResult(t *testing.T)` — TestContextPrefixInvariant_EmptyToolResult 覆盖第 3 处事后改写：工具返回空
- `func probeAssertPrefixInvariant(t *testing.T, harness *prefixProbeHarness)` — probeAssertPrefixInvariant 断言相邻请求保持字节前缀；首条违反即失败并归因。
- `func probeMessageAt(messages []contract.EngineMessage, index int) contract.EngineMessage` — probeMessageAt 返回序列化消息快照中的第 index 条（越界返回空消息）。
- `func probeMessageContentAt(messages []contract.EngineMessage, index int) string`
- `func probeFirstDiffKind(previous, current []contract.EngineMessage, index int) string` — probeFirstDiffKind 归因首个差异消息的成因——只在两侧 role / tool_calls ID

### context_probe_harness_test.go

- `func newPrefixProbeHarness(t *testing.T, window int) *prefixProbeHarness`
- `func (h *prefixProbeHarness) startStream()` — startStream 绑定本会话的流式输出（生产 runChat 的 newBatchedDeltaSink 边界）。
- `func (h *prefixProbeHarness) beginTurn(input string)` — beginTurn 复刻 runChat 开头 + Seele 循环开头的生产顺序：
- `func (h *prefixProbeHarness) streamText(text string)` — streamText 走生产流式链路写可见正文（跨 ReAct 迭代追加到同一条 assistant
- `func (h *prefixProbeHarness) llmIteration(engineContent, reasoning string, calls []types.ToolCall)` — llmIteration 复刻一次 LLM 调用之后的引擎/transcript 状态：
- `func (h *prefixProbeHarness) toolRound(name, callID, arguments, result string)` — toolRound 复刻 ToolHookBridge 的 OnToolStart/OnToolComplete 生产投影。
- `func (h *prefixProbeHarness) finishTurn(reply string)` — finishTurn 复刻 runChat 的回合收尾（chat.go:175/180）：补终态 assistant 事件 +
- `func (h *prefixProbeHarness) capture(turn, iter int, label string)` — capture 固化当前工作历史会产生的 provider 请求（serializeRequest 与冒烟
- `func (h *prefixProbeHarness) toolCallEventContents() []string` — toolCallEventContents 返回 transcript 中 assistant 工具调用事件的正文。
- `func (h *prefixProbeHarness) visibleAssistantText() string` — visibleAssistantText 返回可见投影中最后一条 assistant 正文（生产视图里
- `func prefixProbeCompare(prev, cur prefixProbeCall) prefixProbeDivergence` — prefixProbeCompare 计算 prev → cur 的字节 LCP 与失效后缀（block 粒度按
- `func prefixProbeFirstDiff(prev, cur []contract.EngineMessage) (int, string)`
- `func probeMessagesEqual(left, right contract.EngineMessage) bool`
- `func probeEngineCalls(calls []types.ToolCall) []contract.EngineToolCall`
- `func probeCallIDs(calls []contract.EngineToolCall) string`
- `func probeStreamedContent(cfg prefixProbeConfig) string` — probeStreamedContent 返回对照臂下引擎 assistant 工具调用消息的正文。
- `func splitProbeChunks(text string, size int) []string`
- `func summarizeProbeText(text string) string`
- `func probeWindow(text string, offset int) string` — probeWindow 返回 text 中 offset 附近的可读窗口（定位 system 漂移点）。
- `func logProbeDivergence(t *testing.T, kind string, divergence prefixProbeDivergence)`

### context_restore_prefix_order_test.go

- `func TestRunningSessionAccumulatesAllRoundsInOrderControl(t *testing.T)`
- `func TestColdRestoreTailFirstRequestKeepsTranscriptOrder(t *testing.T)`
- `func appendOrderedRoundsLocked(service *legacyTestService, rounds int) []model.TranscriptEvent`
- `func orderRoundLabels(history []contract.EngineMessage) []string`
- `func expectedOrderRoundLabels(rounds int) []string`

### context_retain_floor_test.go

- `func ValidateRetainWindow(config appwindow.WindowConfig, limits limit.Limits, windowTokens int) error`
- `func applyTestLimits(t *testing.T, mutate func(*limit.Limits))` — applyTestLimits 在测试内改一项 limits 配置并在结束恢复（进程内生效配置）。
- `func TestValidateRetainWindowStartupCheck(t *testing.T)` — TestValidateRetainWindowStartupCheck：《压缩四区模型》的配置校验——保护区下限
- `func TestCompactRetainFloorRaisesProtectedWindow(t *testing.T)` — TestCompactRetainFloorRaisesProtectedWindow：保护区下限生效——同样的 transcript
- `func TestCompactFrameBodyCarriesZoneLayout(t *testing.T)` — TestCompactFrameBodyCarriesZoneLayout：四区显式化的**报表落点**——压缩记录指向的

### context_strategy_ab_probe_test.go

- `func abBody(line string, repeat int) string`
- `func abDemoScript() []abTurnScript` — abDemoScript 是一条 4 轮脚本会话，每轮都发 2 次工具调用（与生产 ReAct 形状一致）。
- `func abSerialize(system string, items []contract.EngineMessage, tools []model.Tool) string` — abSerialize 把一次"provider 可见请求"固化为确定性字节流：
- `func abToolsSignature(tools []model.Tool) string`
- `func abSortedTools(tools []model.Tool) []model.Tool`
- `func abCapture(arm string, turn, iter int, system string, items []contract.EngineMessage, tools []model.Tool) abCall`
- `func abCompare(prev, cur abCall) abDivergence`
- `func abLog(t *testing.T, kind string, d abDivergence)`
- `func runABProductionArm(t *testing.T, label string, script []abTurnScript, cfg prefixProbeConfig) *abRun`
- `func runABCodexArm(t *testing.T, label string, script []abTurnScript, system string, tools []model.Tool) *abRun` — runABCodexArm 按 Codex 的发布策略构造同一脚本的请求序列：
- `func TestContextStrategyAB_CrossTurnScript(t *testing.T)`
- `func abCallAt(run *abRun, turn, iter int) *abCall`
- `func abPreview(text string, limit int) string`
- `func TestContextStrategyAB_WorkedExample(t *testing.T)` — TestContextStrategyAB_WorkedExample 把同一个回合边界（第 1 轮最后一次请求 →
- `func TestStrategyAB_NarrationAttributionAcrossTurns(t *testing.T)` — TestStrategyAB_NarrationAttributionAcrossTurns 沿生产归位路径
- `func lastIter(run *abRun, turn int) int`
- `func TestContextStrategyAB_DynamicFactsPlacement(t *testing.T)` — TestContextStrategyAB_DynamicFactsPlacement 用**同一份动态内容**、**同一次
- `func abCodexPlacementMessages(accumulated []types.Message, fragment, query string) []types.Message` — abCodexPlacementMessages 是 arm C 的投影：常量指令 + 已发布累积 context +
- `func abSerializeAssembled(messages []types.Message) string`
- `func abRoleSeq(messages []types.Message) string`

### context_window_rule_test.go

- `func RetainedReadTailBudget(runtime any) int`
- `func applyWindowConfig(t *testing.T, config cc.WindowConfig)` — applyWindowConfig 应用 window 段并在测试结束恢复（进程内生效配置）。
- `func appendWindowRounds(t *testing.T, service *legacyTestService, requestID string, rounds, answerChars int) []string` — appendWindowRounds 追加 rounds 个已定稿轮次（短问 + 长答），返回每轮标记。
- `func retainedRounds(history []contract.EngineMessage, marks []string) int` — retainedRounds 统计仍留在 provider 历史里的轮次数（按轮标记匹配）。
- `func compactNow(t *testing.T, service *legacyTestService, sessionID string) model.ContextCompaction` — compactNow 走显式压缩落点（/compact、compact_context 同一条路径），返回
- `func TestCompactRetainedPrefixIsMinOfTwoWindows(t *testing.T)` — TestCompactRetainedPrefixIsMinOfTwoWindows：保留前缀 = min(token1, token2)。
- `func TestCompactHardThresholdForcesCompression(t *testing.T)` — TestCompactHardThresholdForcesCompression：window.force_compact_tokens 是硬
- `func TestCompactHardThresholdBypassesEpochThrottle(t *testing.T)` — TestCompactHardThresholdBypassesEpochThrottle：硬压缩不被"同一批进展只压
- `func TestReadTailBudgetFollowsRetainedWindowRule(t *testing.T)` — TestReadTailBudgetFollowsRetainedWindowRule：读尾（冷恢复装载 transcript /
- `func compactionRecords(service *legacyTestService, sessionID string) []model.ContextCompaction`
- `func lastCompactionRecord(t *testing.T, service *legacyTestService, sessionID string) model.ContextCompaction`
- `func TestCompactContextHandlerReportsRecordedRange(t *testing.T)` — TestCompactContextHandlerReportsRecordedRange：compact_context 工具/命令的

### durable_queue_wire_test.go

- `func (s *queueRecordingSessions) QueueEnqueueWorkspace(_, _, _, content string) error`
- `func (s *queueRecordingSessions) QueueMarkConsumedWorkspace(_, _, turnID string) error`
- `func (s *queueRecordingSessions) QueueConfirmConsumedWorkspace(_, _, turnID string) error`
- `func (s *queueRecordingSessions) QueueFailConsumedWorkspace(_, _, turnID string) error`
- `func (s *queueRecordingSessions) QueueRecoverItemsWorkspace(_, _ string) (store.QueueRecoveryReport, bool, error)`
- `func (s *queueRecordingSessions) queueCalls() (enqueued, marked, confirmed, failed []string)`
- `func (*snapshotFailingSessions) SaveSessionSnapshot( string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult, ) error`
- `func (*snapshotFailingSessions) SaveSessionSnapshotWorkspace( string, string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult, ) error`
- `func waitQueueCalls(t *testing.T, sessions interface { queueCalls() (enqueued, marked, confirmed, failed []string) }, ready func(enqueued, marked, confirmed, failed []string) bool)`
- `func newBlockingEngine() *sessionBackedBlockingEngine`
- `func TestDurableQueueMirrorsQueuedInputAndConfirmsTurn(t *testing.T)` — TestDurableQueueMirrorsQueuedInputAndConfirmsTurn 运行中提交 → 镜像落盘；
- `func TestDurableQueueReturnsConsumedContentToDraftOnPersistFailure(t *testing.T)` — TestDurableQueueReturnsConsumedContentToDraftOnPersistFailure 快照落盘失败
- `func TestDurableQueueBackfillVisibleAndDrainedOnNextSubmit(t *testing.T)` — TestDurableQueueBackfillVisibleAndDrainedOnNextSubmit 冷加载恢复的输入

### environment_context_test.go

- `func renderEnvironmentTraceBlock(projectRoot, defaultEnv, currentEnv string, environments []model.WorkspaceEnvironmentInfo) string`
- `func environmentTraceBlockFor(service *assembly.Service, sessionID string) string`
- `func TestRenderEnvironmentTraceBlockListsModeAddressAndCurrent(t *testing.T)`
- `func TestRenderEnvironmentTraceBlockSkipsEmptyTable(t *testing.T)`
- `func TestRenderEnvironmentTraceBlockWithoutBindingStaysLocal(t *testing.T)`
- `func TestEnvironmentTraceBlockForUsesProjectAndSessionState(t *testing.T)`

### environment_helpers_test.go

- `func (workspace *environmentTraceWorkspace) WorkspaceEnvironments(string) ([]model.WorkspaceEnvironmentInfo, string, error)`
- `func (*environmentTraceWorkspace) SetWorkspaceEnvironment(string, string, model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error)`
- `func (*environmentTraceWorkspace) DeleteWorkspaceEnvironment(string, string) error`
- `func (*environmentTraceWorkspace) SetDefaultWorkspaceEnvironment(string, string) error`
- `func (sessions *environmentTraceSessions) SessionSSHState(sessionID string) (model.SessionSSHInfo, error)`
- `func (sessions *environmentTraceSessions) SetSessionSSHState(_ string, state model.SessionSSHInfo) error`

### fakes_test.go

- `func (engine *sessionBackedBlockingEngine) SessionBacked() bool`
- `func (engine *sessionBackedBlockingEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *sessionBackedBlockingEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 显式转发到自身 ChatStream（覆盖内嵌 fakeEngine 的提升方法，
- `func (sessions *blockingSaveSessions) SaveCurrent(string) error`
- `func (engine *fakeEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *fakeEngine) History() []contract.EngineMessage`
- `func (engine *fakeEngine) ClearHistory()`
- `func (engine *fakeEngine) SessionID() string`
- `func (engine *fakeEngine) StartSession() string`
- `func (engine *fakeEngine) ActivateSession(sessionID string) error` — ActivateSession 以显式会话 ID 创建并激活引擎实例（G4 早分配 SID：
- `func (engine *fakeEngine) ReplaceHistory(sessionID string, history []contract.EngineMessage) error`
- `func (engine *fakeEngine) SetSystemPrompt(prompt string)`
- `func (engine *fakeEngine) SetMaxLoops(maxLoops int)`
- `func (engine *fakeEngine) SetSessionMaxLoopsFor(sessionID string, loops int)` — SetSessionMaxLoopsFor 实现 contract.SessionLoopLimitPort：定时任务声明的循环上限
- `func (engine *fakeEngine) sessionLoopsFor(sessionID string) int` — sessionLoopsFor 读指定会话被钉住的循环上限（0 = 没钉，按进程默认值跑）。
- `func (engine *fakeEngine) AppendHistory(msg contract.EngineMessage)`
- `func (*fakeEngine) TraceText() string`
- `func (*fakeEngine) TokenCount() string`
- `func (*fakeEngine) NodeSessionConversation(string) ([]contract.EngineMessage, bool)`
- `func (engine *fakeEngine) NodeContextSnapshot(string) (*snapshot.ContextSnapshot, bool)`
- `func (engine *fakeEngine) NodeToolResult(nodeID, ref string) (string, bool)`
- `func (engine *fakeEngine) NodeWorktreeInfoFor(nodeID string) (assemble.NodeWorktreeInfo, bool)`
- `func (engine *fakeEngine) SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error)`
- `func (engine *fakeEngine) SubagentLiveHistoryPage(nodeID string, offset, limit int) dto.SubagentLiveHistoryPage`
- `func (engine *fakeEngine) SubAgentTree() []dto.SubAgentTreeNode`
- `func (engine *fakeEngine) ReleaseWorkingHistory()`
- `func (engine *fakeEngine) ReleaseWorkingHistoryFor(sessionID string)`
- `func (engine *fakeEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *fakeEngine) HistoryFor(sessionID string) []contract.EngineMessage`
- `func (engine *fakeEngine) AppendHistoryFor(sessionID string, msg contract.EngineMessage)`
- `func (engine *fakeEngine) ClearHistoryFor(sessionID string)`
- `func (engine *fakeEngine) SetSystemPromptFor(sessionID, prompt string)`
- `func (engine *fakeEngine) ReplaceHistoryFor(sessionID string, history []contract.EngineMessage) error`
- `func (engine *fakeEngine) HasSession(sessionID string) bool`
- `func (*fakeRuntime) Model() string`
- `func (*fakeRuntime) Provider() string`
- `func (*fakeRuntime) Accounts() []model.AccountInfo`
- `func (runtime *fakeRuntime) SelectAccount(name string) bool`
- `func (runtime *fakeRuntime) VisibleTools(context.Context) []model.Tool`
- `func (*fakeRuntime) ActivePlugin() string`
- `func (runtime *fakeRuntime) FullAccess() bool`
- `func (runtime *fakeRuntime) SetFullAccess(on bool)`
- `func (runtime *fakeRuntime) SetFullAccessFor(sessionID string, on bool)` — SetFullAccessFor 镜像生产权限门的会话级解析（空会话 ID = 进程级默认）：
- `func (runtime *fakeRuntime) FullAccessFor(sessionID string) bool` — FullAccessFor 返回指定会话生效的全权模式（会话级选择优先，未选择回退
- `func (runtime *fakeRuntime) PermissionTier() string` — PermissionTier 返回进程级默认权限档位（fake 只有二元口径：fullAccess → full）。
- `func (runtime *fakeRuntime) SetPermissionTierFor(sessionID, tier string) error` — SetPermissionTierFor 把档位落到 fake 的二元全权口径（full → true，其余 → false），
- `func (runtime *fakeRuntime) SetRuntimeVisibilityProjection(projection assemble.RuntimeVisibilityProjection)` — SetRuntimeVisibilityProjection / SetParentEvidenceProjection 会被并行会话的
- `func (runtime *fakeRuntime) SetParentEvidenceProjection(projection assemble.ParentEvidenceProjection)`
- `func (runtime *fakeRuntime) DrainSubagentContexts() []string` — DrainSubagentContexts 排空 merge-back 邮箱。M2 多会话并行下多个
- `func (runtime *fakeRuntime) SetSubagentPolicy(policy dto.SubagentPolicy)`
- `func (runtime *fakeRuntime) SetSubagentPolicyFor(sessionID string, policy dto.SubagentPolicy)`
- `func (runtime *fakeRuntime) subagentPolicyFor(sessionID string) (dto.SubagentPolicy, bool)`
- `func (runtime *fakeRuntime) TodoSnapshot() []dto.TodoItem`
- `func (runtime *fakeRuntime) SetTodoStatus(index int, status dto.TodoItemStatus) error`
- `func (runtime *fakeRuntime) TaskSnapshot() []dto.TaskRecord` — TaskSnapshot 返回**项目/全局** task 表（镜像生产 Runtime：实时注册表 +
- `func (runtime *fakeRuntime) ReleaseSessionAsync(sessionID string) int` — ReleaseSessionAsync 记下被释放的会话，供"会话销毁即杀后台命令"的用例断言。
- `func (runtime *fakeRuntime) AsyncPendingFor(string) int` — AsyncPendingFor 回答测试显式设置的在途后台命令数。
- `func (runtime *fakeRuntime) AsyncRunsSnapshot() []dto.AsyncRunRecord` — AsyncRunsSnapshot 回答测试显式设置的后台执行投影（后台行投影用例的输入源）。
- `func (runtime *fakeRuntime) AsyncRunEvents() <-chan struct` — AsyncRunEvents 返回测试自己持有的信号口（默认 nil = 消费者不启动）。
- `func (runtime *fakeRuntime) TeamworkJobCompletions() []dto.TeamworkJobCompletionRecord` — TeamworkJobCompletions / TeamworkJobEvents 回答 teammate 作业表（jobs.Manager）的
- `func (runtime *fakeRuntime) TeamworkJobEvents() <-chan struct`
- `func (runtime *fakeRuntime) TaskSnapshotFor(sessionID string) []dto.TaskRecord` — TaskSnapshotFor 保持会话粒度（持久化落盘/请求尾部打点块用）。
- `func (runtime *fakeRuntime) globalSnapshotLocked() []dto.TaskRecord` — globalSnapshotLocked 合并实时注册表与所有会话分区（跨会话身份去重：
- `func (runtime *fakeRuntime) snapshotLocked() []dto.TaskRecord`
- `func (runtime *fakeRuntime) TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error)`
- `func (runtime *fakeRuntime) addTaskLocked(spec dto.TaskSpec) (dto.TaskRecord, bool, error)`
- `func (runtime *fakeRuntime) TaskAddFor(sessionID string, spec dto.TaskSpec) (dto.TaskRecord, bool, error)`
- `func (runtime *fakeRuntime) ResolveTaskByKey(key string) (dto.TaskRecord, bool, error)`
- `func (runtime *fakeRuntime) ResolveTaskByKeyFor(sessionID, key string) (dto.TaskRecord, bool, error)`
- `func (runtime *fakeRuntime) TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error)`
- `func (runtime *fakeRuntime) TaskSetStatusFor(sessionID, id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error)`
- `func (runtime *fakeRuntime) TaskAttachParticipant(id, participant string) (dto.TaskRecord, error)`
- `func (*fakeRuntime) TaskChangedChannel() <-chan dto.TaskRecord`
- `func (*fakeRuntime) SubagentTreeEvents() <-chan struct`
- `func (*fakeRuntime) PlanNodeEventChannel() <-chan dto.PlanNodeEvent`
- `func (runtime *fakeRuntime) TakeServedRoute(sessionID string) (dto.ServedRoute, bool)` — TakeServedRoute 按 fake 预置回答路由事实（读即清：与生产 Runtime 同语义，
- `func (runtime *fakeRuntime) SwitchSessionTasks(sessionID string, records []dto.TaskRecord)`
- `func (runtime *fakeRuntime) SetSessionWorkspace(sessionID, workspaceID string)`
- `func (runtime *fakeRuntime) WithPluginAssembly(ctx context.Context, plugins []string) context.Context` — WithPluginAssembly 是生产 Runtime 同名方法的镜像：把插件装配带进这一轮执行
- `func (runtime *fakeRuntime) PluginAssembly() []string` — PluginAssembly 读最近一次装配（同包用例直读，加锁）。
- `func (runtime *fakeRuntime) ScheduledCommands() []assemble.ScheduledCommandInfo`
- `func (runtime *fakeRuntime) ScheduledTasksSnapshot() []assemble.ScheduledTaskStatus`
- `func (runtime *fakeRuntime) ScheduleTask(_ context.Context, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error)`
- `func (runtime *fakeRuntime) CancelScheduledTask(id string) error`
- `func (runtime *fakeRuntime) UpdateScheduledTask(_ context.Context, id string, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error)` — UpdateScheduledTask 是编辑入口的桩：记录"改哪一条、改成什么"，并把快照里
- `func (runtime *fakeRuntime) ClearSubagentTree() error`
- `func (runtime *fakeRuntime) RestoreSubagentAnchors(string) error`
- `func (runtime *fakeRuntime) ListSubagentRecovery(string) ([]dto.SubagentRecoveryView, error)`
- `func (runtime *fakeRuntime) ResumeInterruptedSubagents(context.Context, string) (dto.SubagentResumeReport, error)`
- `func (runtime *fakeRuntime) ResumeSubagent(context.Context, string, string) (dto.SubagentResumeResult, error)`
- `func (runtime *fakeRuntime) ForkSubagents(context.Context, string, []dto.SubagentForkSpec) (string, error)`
- `func (runtime *fakeRuntime) SetSubagentParentRepairer(func(string) error)`
- `func (runtime *fakeRuntime) SearchHistory(_ context.Context, _ string, _ int) (seelexctxsearch.Result, error)`
- `func (runtime *fakeRuntime) BindProjectRoot(rootPath string) error`
- `func (runtime *fakeRuntime) UnbindProjectRoot()`
- `func (runtime *fakeRuntime) ProjectRoot() string` — ProjectRoot 读当前绑定的项目根（加锁）：写侧可能来自后台 chat goroutine，
- `func (runtime *fakeRuntime) SetCurrentTaskBatch(sessionID, batchID string)` — SetCurrentTaskBatch 会被并行会话的多个 runChat 并发调用（M2：每个会话
- `func (runtime *goalVisibilityRuntime) VisibleTools(context.Context) []model.Tool`
- `func (*fakePlugins) All() []model.PluginInfo`
- `func (plugins *fakePlugins) Activate(_ context.Context, name string) error`
- `func (plugins *fakePlugins) Deactivate(context.Context) error`
- `func (plugins *fakePlugins) Current() (model.PluginInfo, bool)`
- `func (fakeSkills) All() []model.SkillInfo`
- `func (fakeSkills) Get(name string) (model.SkillInfo, bool)`
- `func (fakeSessions) SaveCurrent(string) error`
- `func (fakeSessions) Resume(string) error`
- `func (fakeSessions) List() []model.SessionInfo`
- `func (fakeSessions) LoadHistory(string) ([]contract.EngineMessage, error)`
- `func (fakeSessions) LoadHistoryRange(string, int, int) ([]contract.EngineMessage, int, error)`
- `func (fakeSessions) Delete(string) error`
- `func (fakeSessions) MessageCount(string) (int, error)`
- `func (fakeSessions) SetWorkspace(string)`
- `func (fakeSessions) Workspace() string`
- `func (sessions *blockingCatalogSessions) List() []model.SessionInfo`
- `func (persistenceFailingSessions) SaveCurrent(string) error`
- `func (sessions *trackingSessions) SaveCurrent(sessionID string) error`
- `func (sessions *scopedSessions) SetWorkspace(workspaceID string)`
- `func (sessions *scopedSessions) Workspace() string`
- `func (sessions *scopedSessions) SaveCurrent(sessionID string) error`
- `func (sessions *scopedSessions) SavedIDs() []string`
- `func (sessions *scopedSessions) SessionsOf(projectID string) []model.SessionInfo` — SessionsOf 实现 session_runtime.SessionGranularPort：按项目索引枚举会话。
- `func (sessions *scopedSessions) LoadedWorkspace() string`
- `func (sessions *scopedSessions) resolveWorkspaceFor(sessionID string) (string, []contract.EngineMessage, bool)` — resolveWorkspaceFor 返回会话历史所在 workspace（扫描 histories；未找到
- `func (sessions *scopedSessions) LoadHistory(sessionID string) ([]contract.EngineMessage, error)` — LoadHistory 实现 SessionGranularPort：读取会话历史（会话粒度键；workspace
- `func (sessions *scopedSessions) LoadHistoryRange(sessionID string, offset, limit int) ([]contract.EngineMessage, int, error)` — LoadHistoryRange 实现 SessionGranularPort：按窗口读取会话历史。
- `func (sessions *scopedSessions) Delete(sessionID string) error` — Delete 实现 SessionGranularPort：从全部项目索引删除会话。
- `func newFakeWorkspace() *fakeWorkspace`
- `func (repo *fakeWorkspace) Create(name, rootPath, gitRemote string) (model.WorkspaceInfo, error)`
- `func (repo *fakeWorkspace) Get(id string) (model.WorkspaceInfo, error)`
- `func (repo *fakeWorkspace) List() []model.WorkspaceInfo`
- `func (repo *fakeWorkspace) Delete(id string) error`
- `func (repo *fakeWorkspace) BindSession(sessionID, workspaceID string)`
- `func (repo *fakeWorkspace) UnbindSession(sessionID string)`
- `func (repo *fakeWorkspace) SessionWorkspace(sessionID string) (model.WorkspaceInfo, bool)`
- `func (repo *fakeWorkspace) AllBindings() map[string]string`
- `func (*fakeWorkspace) DetectGitRemote(string) string`

### fault_guard_test.go

- `func TestFaultGuardFacadeOnlyDelegatesToApplication(t *testing.T)`
- `func TestDegradeAndRequestExitNotifiesHost(t *testing.T)` — TestDegradeAndRequestExitNotifiesHost：降级退出必须幂等、标记 closed/
- `func TestLifecycleFaultPanicsAfterDegrade(t *testing.T)` — TestLifecycleFaultPanicsAfterDegrade：生命周期消费者中的故障不再被静默

### fixture_concurrency_test.go

- `func TestTeamRecordingSessionsAccessorsAreRaceFree(t *testing.T)` — TestTeamRecordingSessionsAccessorsAreRaceFree 并发读写夹具：-race 下证明读写两侧
- `func TestFixturesDoNotBypassLockedAccessors(t *testing.T)` — TestFixturesDoNotBypassLockedAccessors 是防复发的机械防线：AST 扫描本包测试源码，

### goal_team_wiring_test.go

- `func (s *teamRecordingSessions) EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error)`
- `func (s *teamRecordingSessions) ReadLifecycleOrder(string) (string, []string, error)`
- `func (s *teamRecordingSessions) SetLifecycleOrder(_ string, policy string, roles []string) error`
- `func (s *teamRecordingSessions) ReadTeamRegistry(string) (dto.TeamRegistry, error)`
- `func (s *teamRecordingSessions) WriteTeamRegistry(_ string, registry dto.TeamRegistry) error`
- `func (s *teamRecordingSessions) RemoveTeamRegistry(string) error` — RemoveTeamRegistry 是 agentteam.DismissPort 的桩：团队离场 = 注册表清空 + 顺序复位
- `func (s *teamRecordingSessions) dismissCount() int` — dismissCount 返回团队离场次数。
- `func (s *teamRecordingSessions) CreateRoleSession(string, string, string, uint64) (dto.RoleSessionInfo, error)`
- `func (s *teamRecordingSessions) AppendRoleDraft(string, string, string, []dto.RoleDraftRow) error`
- `func (s *teamRecordingSessions) ReadRoleDraft(string, string, string) ([]dto.RoleDraftRow, error)`
- `func (s *teamRecordingSessions) SyncRoleDraft(string, string, string, []string) (dto.RoleDraftSyncResult, error)`
- `func (s *teamRecordingSessions) AppendRoleSessionRows(string, string, string, []dto.RoleRow) error`
- `func (s *teamRecordingSessions) ReadRoleSessionRows(string, string, string) ([]dto.RoleRow, error)`
- `func (s *teamRecordingSessions) RoleSnapshot(string, string, string) (dto.RoleSnapshot, error)`
- `func (s *teamRecordingSessions) AssembleRoleWire(string, string, string, int, int) (dto.RoleWireSnapshot, error)`
- `func (s *teamRecordingSessions) SetRoleLifecycle(string, string, string, uint64, *dto.CompactFrameRef) error`
- `func (s *teamRecordingSessions) setLifecycle(policy string, order []string)` — 以下是夹具的**加锁存取入口**：夹具状态只允许通过它们读写。
- `func (s *teamRecordingSessions) setOrder(order []string)` — setOrder 只覆盖顺序（顺序策略不动）：装配前的"清现场"用它。
- `func (s *teamRecordingSessions) setRegistry(registry dto.TeamRegistry)` — setRegistry 覆盖夹具的团队注册表（加锁）。
- `func (s *teamRecordingSessions) lifecycleSnapshot() (string, []string)` — lifecycleSnapshot 读当前顺序策略与顺序（加锁 + 深拷贝，读到的是快照）。
- `func (s *teamRecordingSessions) registrySnapshot() dto.TeamRegistry` — registrySnapshot 读当前注册表（加锁 + 拷贝角色切片）。
- `func (s *teamRecordingSessions) orderSnapshot() []string` — orderSnapshot 只读顺序（加锁 + 拷贝）。
- `func (s *teamRecordingSessions) ensuredRoles() []string` — ensuredRoles 读已装配的角色会话名（加锁 + 拷贝）。
- `func (s *teamRecordingSessions) joinSeqSnapshot() []string` — joinSeqSnapshot 读角色会话装配时的 join 切点（加锁 + 拷贝）：
- `func (s *teamRecordingSessions) ListRoleSessions(string) ([]string, error)`
- `func TestGoalBeginLeavesSessionTeamAlone(t *testing.T)` — TestGoalBeginLeavesSessionTeamAlone 钉住 2026-10-01 的口径：创建 goal **不**自动装配
- `func TestGoalBeginWithoutTeamStorageIsBestEffort(t *testing.T)` — TestGoalBeginWithoutTeamStorageIsBestEffort 钉住降级语义：宿主没有团队存储
- `func (s *teamRecordingSessions) joinSeqsSnapshot() []string` — joinSeqsSnapshot 只读角色装配的 join_seq_id 记录（加锁 + 拷贝）。

### helpers_test.go

- `func (locker *recordingLocker) Lock()`
- `func (locker *recordingLocker) Unlock()`
- `func withSessionID(ctx context.Context, sessionID string) context.Context`
- `func sessionIDFromContext(ctx context.Context) string`
- `func ApplyLimits(limits applicationpolicy.Limits)`
- `func Limits() applicationpolicy.Limits`
- `func workTableBounds() worktable.Limits`
- `func withTestSessions(sessions contract.SessionPort) testServiceOption`
- `func withTestRuntime(runtime contract.RuntimePort) testServiceOption`
- `func testResidencyPort() applicationsession.ResidencyPort`
- `func withTestReleaseWindowStore(store contract.ReleaseWindowStore) testServiceOption` — withTestReleaseWindowStore 注入「会话驻留窗口」的落盘口（不注入 = 界面上改的值
- `func withTestAccountWindowStore(store contract.AccountWindowStore) testServiceOption` — withTestAccountWindowStore 注入「每账号上下文窗口」的落盘口（不注入 = 界面上改的值
- `func withTestPlugins(plugins contract.PluginPort) testServiceOption` — withTestPlugins 注入自定义插件端口（默认桩只给一个不含来源的 default）。来源面
- `func newTestService(t testing.TB, engine contract.ChatEngine, options ...testServiceOption) *assembly.Service` — newTestService 构造一个带默认 fakes 的 Service（测试夹具），并在测试
- `func debugLog(format string, args ...any)`
- `func testExternalPorts() assembly.ExternalPorts` — testExternalPorts 是集成夹具自己的端口表：组合根的表（boot.ServicePorts）
- `func mustNew(t testing.TB, deps contract.Dependencies) *assembly.Service`
- `func componentsOf(service any) assembly.Components`
- `func waitForSnapshot(t *testing.T, service any, ready func(model.Snapshot) bool) model.Snapshot` — waitForSnapshot 轮询 Snapshot 直到 ready 条件满足，返回满足条件的快照。
- `func (*sessionBackedEngine) SessionBacked() bool`
- `func newGracefulShutdownEngine() *gracefulShutdownEngine`
- `func (engine *gracefulShutdownEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *gracefulShutdownEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 显式转发到自身 ChatStream（覆盖内嵌 fakeEngine 的提升方法，
- `func waitForChatCompletion(t *testing.T, service any)` — waitForChatCompletion 轮询直到当前 chat 结束。

### history_safety_test.go

- `func newHistorySafetyService(t *testing.T, engine contract.ChatEngine) *historySafetyService`
- `func nonEmptyProviderInput(input string) string`
- `func (service *historySafetyService) recoverProviderContext(err error, originalRequest string) error`
- `func (service *historySafetyService) recoverProviderFailure(err error, originalRequest string) (bool, error)`
- `func (service *historySafetyService) recoverProviderFailureFor(ctx context.Context, err error, originalRequest string) (bool, error)`
- `func retryableAfterRecovery(err error) bool`
- `func (service *historySafetyService) removeProviderContextRecovery() error`
- `func TestNonEmptyProviderInputExplainsEmptySubmission(t *testing.T)`
- `func TestPrepareProviderHistoryRepairsBeforeChat(t *testing.T)`
- `func TestRecoverProviderContextReplacesRejectedTranscriptWithPrivateCheckpoint(t *testing.T)`
- `func TestRecoverProviderContextIgnoresOtherProviderErrors(t *testing.T)`
- `func TestRecoverProviderTimeoutCreatesPrivateResumeCheckpoint(t *testing.T)`
- `func TestContextExhaustionPersistsInterruptedProjectionAfterBoundedRetryFails(t *testing.T)`
- `func containsRecoveryHistory(history []contract.EngineMessage, prefix string) bool`
- `func TestContextExhaustionReturnsBoundedRecoveryInstructionToAgent(t *testing.T)`
- `func TestEmptyProviderContentRejectionResumesFromBoundedCheckpoint(t *testing.T)` — TestEmptyProviderContentRejectionResumesFromBoundedCheckpoint 覆盖“记录不合法”
- `func TestNonRecoverableProviderFailureMarksTaskFailed(t *testing.T)`
- `func TestIterationRepairsNewlyAddedEmptyToolHistory(t *testing.T)`
- `func TestServerFailuresAreRecoverableWithoutAutomaticReplay(t *testing.T)`
- `func TestToolProtocolRejectionsAreHistoryFailures(t *testing.T)` — TestToolProtocolRejectionsAreHistoryFailures 覆盖 2026-09-20 现场那条 400：
- `func TestInsufficientToolMessagesIsAHistoryFailure(t *testing.T)` — TestInsufficientToolMessagesIsAHistoryFailure 覆盖 tool 配对协议的另一半措辞：
- `func TestRetryableAfterRecoveryOnlyReplaysPreExecutionRejections(t *testing.T)` — TestRetryableAfterRecoveryOnlyReplaysPreExecutionRejections 钉住"什么时候可以
- `func liveSessionLoopToolPairingFailure() error` — liveSessionLoopToolPairingFailure 是 2026-09-28 现场原文：会话循环第一次请求
- `func TestHistoryProtocolFailureResumesInsteadOfKillingTheSession(t *testing.T)` — TestHistoryProtocolFailureResumesInsteadOfKillingTheSession 覆盖现场后果：恢复

### history_search_test.go

- `func (runtime *searchRuntime) SearchHistory(_ context.Context, query string, limit int) (seelexctxsearch.Result, error)`
- `func TestSearchHistoryHandlerReturnsStructuredHits(t *testing.T)`
- `func TestSearchHistoryHandlerRejectsEmptyQuery(t *testing.T)`
- `func TestSearchHistoryHandlerPropagatesRuntimeError(t *testing.T)`
- `func TestSearchHistoryRejectsEmptyQueryAtServiceBoundary(t *testing.T)`

### hot_attach_running_test.go

- `func newFrameworkLockEngine() *frameworkLockEngine`
- `func (engine *frameworkLockEngine) lockFor(sessionID string) *sync.Mutex`
- `func (engine *frameworkLockEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 持锁到 release（模拟 framework Session.ChatStream 全程持锁）。
- `func (engine *frameworkLockEngine) SetSystemPromptFor(sessionID, prompt string)` — SetSystemPromptFor 需要会话锁：运行中会话调用会阻塞（生产死锁源）。
- `func (engine *frameworkLockEngine) promptCallCount(sessionID string) int`
- `func TestHotAttachRunningSessionDoesNotBlockOnEngineLock(t *testing.T)` — TestHotAttachRunningSessionDoesNotBlockOnEngineLock（生产死锁回归）：

### input_unknown_command_notice_test.go

- `func TestNewAssemblesInputRouter(t *testing.T)`
- `func TestUnknownCommandNoticeSeparatesToolsFromCommands(t *testing.T)`
- `func TestUnknownCommandSubmitKeepsNoticeNotError(t *testing.T)` — TestUnknownCommandSubmitKeepsNoticeNotError：未知命令仍然不是错误（照常回
- `func TestHelpStatesPanelListsOnlyExecutableEntries(t *testing.T)` — TestHelpStatesPanelListsOnlyExecutableEntries：`/help` 的口径必须与建议面板一致——

### interrupted_continue_test.go

- `func TestColdResumeContinueAfterInterruptedToolChain(t *testing.T)`

### legacy_prompt_engine_test.go

- `func completeToolForTest(service *legacyTestService, name, id, result string, toolErr error, duration time.Duration)` — completeToolForTest 走工具投影的完成段（真实完成路径），用于断言"完成即同步"。
- `func newRecordingPromptEngine() *recordingPromptEngine`
- `func (engine *recordingPromptEngine) SetSystemPromptFor(sessionID, prompt string)`
- `func (engine *recordingPromptEngine) SetSystemPrompt(prompt string)`
- `func (engine *recordingPromptEngine) SetMaxLoops(loops int)`
- `func (engine *recordingPromptEngine) ClearHistory()`
- `func (engine *recordingPromptEngine) promptSnapshot() (promptFor map[string]string, globalCount, loops, clears int)` — promptSnapshot 返回测试断言的引擎侧快照（加锁拷贝）。

### legacy_service_test.go

- `func newLegacyTestService(t testing.TB, engine contract.ChatEngine, options ...testServiceOption) *legacyTestService`
- `func mustNewLegacy(t testing.TB, deps contract.Dependencies) *legacyTestService`
- `func legacyServiceFor(service *assembly.Service) *legacyTestService`
- `func (service *legacyTestService) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error`
- `func (service *legacyTestService) reapplyEffortAfterPluginSwitch()`
- `func (service *legacyTestService) submitCommand(ctx context.Context, input string) error`
- `func (service *legacyTestService) prepareCompletedTaskBoundary()`
- `func selectedSkillLayers(layers []applicationprompt.PromptLayer) []applicationprompt.PromptLayer`
- `func (service *legacyTestService) appendHistoryLocked(history []contract.EngineMessage)`
- `func (service *legacyTestService) appendHistoryLockedFor(sessionID string, history []contract.EngineMessage)`
- `func (service *legacyTestService) startDraftSnapshotLoop(ctx context.Context, sessionID, requestID string) func()`
- `func (service *legacyTestService) draftSnapshotBaseSeq(sessionID, requestID string) (uint64, bool)`
- `func (service *legacyTestService) persistDraftSnapshotSuffix(sessionID string, left uint64) (uint64, bool, error)`
- `func (service *legacyTestService) transcriptSnapshot(sessionID string) []protocol.TranscriptEvent`
- `func (service *legacyTestService) currentViewSessionID() string`
- `func (service *legacyTestService) effortForSession(sessionID string) string`
- `func (service *legacyTestService) syncSubagentPolicyFor(sessionID string)`
- `func (service *legacyTestService) permissionTierForSession(sessionID string) string`
- `func (service *legacyTestService) fullAccessForSession(sessionID string) bool`
- `func (service *legacyTestService) syncFullAccessFor(sessionID string)`
- `func (service *legacyTestService) readStoredPermissionTier(sessionID string) string`
- `func (service *legacyTestService) applyStoredPermissionTier(sessionID, tier string)`
- `func (service *legacyTestService) perSessionExecution() bool`
- `func (service *legacyTestService) bindGlobalProjectRoot(rootPath string) error`
- `func (service *legacyTestService) setWorkspaceWriteScope(workspaceID string)`
- `func (service *legacyTestService) newGeneratedSessionID(prefix string) string`
- `func (service *legacyTestService) bindProjectRootIfSafe(_ string, rootPath string) bool`
- `func (service *legacyTestService) recoverPendingMessageTailAtLoad(sessionID string) string`
- `func (service *legacyTestService) publishChatStateFor(sessionID string)`
- `func (service *legacyTestService) finalizeTaskExecution(requestID string) error`
- `func (service *legacyTestService) resetViewToDraftAfterRestoreFailure()`
- `func (service *legacyTestService) switchViewToFreshDraftLocked()`
- `func (service *legacyTestService) enterFreshDraftShellLocked(keepWorkspace, keepProcessInteraction bool)`
- `func (service *legacyTestService) sessionUnitLocked(sessionID string) *sessionstate.SessionUnit`
- `func LegacyCompactionRangeLabel(result LegacyContextCompactionResult) string`

### message_seq_scope_test.go

- `func TestVisibleMessageSeqIsScopedPerSession(t *testing.T)` — TestVisibleMessageSeqIsScopedPerSession 钉住可见消息派号的会话作用域：

### multi_session_helpers_test.go

- `func newMultiSessionEngine() *multiSessionEngine`
- `func (engine *multiSessionEngine) register(sessionID string)`
- `func (engine *multiSessionEngine) StartSession() string`
- `func (engine *multiSessionEngine) ActivateSession(sessionID string) error`
- `func (engine *multiSessionEngine) HasSession(sessionID string) bool`
- `func (engine *multiSessionEngine) UnloadSession(sessionID string) error`
- `func (engine *multiSessionEngine) SessionID() string`
- `func (engine *multiSessionEngine) History() []contract.EngineMessage`
- `func (engine *multiSessionEngine) HistoryFor(sessionID string) []contract.EngineMessage`
- `func (engine *multiSessionEngine) ReplaceHistory(sessionID string, history []contract.EngineMessage) error`
- `func (engine *multiSessionEngine) ReplaceHistoryFor(sessionID string, history []contract.EngineMessage) error`
- `func (engine *multiSessionEngine) ResumeSession(sessionID string, history []contract.EngineMessage) error`
- `func (engine *multiSessionEngine) AppendHistoryFor(sessionID string, message contract.EngineMessage)`
- `func (engine *multiSessionEngine) ClearHistoryFor(sessionID string)`
- `func (engine *multiSessionEngine) ClearHistory()`
- `func (engine *multiSessionEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *multiSessionEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *multiSessionEngine) SetSystemPrompt(string)`
- `func (engine *multiSessionEngine) SetSystemPromptFor(string, string)`
- `func waitChatStarted(t *testing.T, started <-chan struct{})`
- `func waitUnitIdle(t *testing.T, service any, sessionID string)` — waitUnitIdle 等到会话真正空闲：视图状态不是运行态，且会话单元的 chat 也不再 running。

### permission_command_test.go

- `func permissionCommandFor(t *testing.T, service *legacyTestService) interface`
- `func TestPermissionCommandRegisteredInHelp(t *testing.T)` — TestPermissionCommandRegisteredInHelp：命令在册，且 /help 能列出来。
- `func TestPermissionCommandWithoutArgsShowsCurrentAndCatalog(t *testing.T)` — TestPermissionCommandWithoutArgsShowsCurrentAndCatalog：无参回显当前档位 + 档位目录
- `func TestPermissionCommandSwitchesTier(t *testing.T)` — TestPermissionCommandSwitchesTier：带参切档走 SetPermissionTier（会话槽 + 快照），

### plugin_source_projection_test.go

- `func TestCollectRuntimeProjectionCarriesPluginSource(t *testing.T)`
- `func sourceCatalogYAML(localRoot string) string` — sourceCatalogYAML 生成一份**合法**的精选目录：三个随包条目（builtin/vendored）+
- `func writeSourcePlugin(t *testing.T, root, name string)`
- `func writeSourceCatalog(t *testing.T, root, body string)`
- `func loadSourceManager(t *testing.T, roots ...string) *plugin.Manager` — loadSourceManager 走生产同一条路（多根 loader + manager 事务加载）。
- `func (sourceTestTools) DefinePlugin(string, string, []string, []string) error`
- `func (sourceTestTools) UndefinePlugin(string)`
- `func (sourceTestTools) ActivatePlugin(string) error`
- `func (sourceTestTools) DeactivatePlugin()`
- `func (sourceTestTools) ActivePlugin() string`
- `func (sourceTestMCP) AttachMCPServer(_ context.Context, _, _, _ string, _, _ []string, _ string) error`
- `func (sourceTestMCP) DetachMCP(string) error`
- `func (sourceTestSkills) PublishPluginSkills(string, []skill.Skill) error`
- `func (sourceTestSkills) ClearPluginSkills(string)`
- `func (sourceTestSkills) ActivatePluginSkills(string) error`
- `func (sourceTestSkills) DeactivatePluginSkills() error`

### project_board_read_test.go

- `func TestProjectBoardReadFacadeUsesPorts(t *testing.T)`
- `func newBoardTestService(t *testing.T, options ...testServiceOption) (*legacyTestService, *fakeRuntime, string)`
- `func TestBuildProjectBoardRowsMatchesLiveProjection(t *testing.T)`
- `func TestProjectBoardIsNotAnInput(t *testing.T)`
- `func marshalWorkItems(t *testing.T, items []model.WorkItem) string`
- `func TestProjectBoardReadersForAssembly(t *testing.T)`
- `func (runtime *boardPlanRuntime) TeamworkPlanFor(sessionID string) (store.TeamworkPlan, bool)`
- `func (sink *recordingBoardSink) OnToolComplete(name string, err error, sessionID string)`
- `func (sink *recordingBoardSink) snapshot() []boardSinkCall`
- `func TestProjectBoardHooksOnExistingSyncPoint(t *testing.T)`

### race_test.go

- `func TestEventHub_RacePublishSubscribe(t *testing.T)` — TestEventHub_RacePublishSubscribe 验证并发 Publish 与 Subscribe 不产生 data race。
- `func TestEventHub_RaceSubscribeClosePublish(t *testing.T)` — TestEventHub_RaceSubscribeClosePublish 验证 Subscribe/Close 与 Publish 并发安全。
- `func TestEventHub_RaceMultipleSubscribers(t *testing.T)` — TestEventHub_RaceMultipleSubscribers 验证多订阅者并发订阅/关闭。
- `func TestApprovalBroker_RaceConcurrentRequests(t *testing.T)` — TestApprovalBroker_RaceConcurrentRequests 验证并发 Request 安全。
- `func TestApprovalBroker_RaceRequestShutdown(t *testing.T)` — TestApprovalBroker_RaceRequestShutdown 验证 Shutdown 与 Request 并发安全。
- `func TestApprovalBroker_RaceTimeoutResolve(t *testing.T)` — TestApprovalBroker_RaceTimeoutResolve 验证 Timeout 与 Resolve 竞态。
- `func TestApprovalBroker_RaceDuplicateResolve(t *testing.T)` — TestApprovalBroker_RaceDuplicateResolve 验证重复 Resolve 返回错误（并发安全）。
- `func TestChat_RaceConcurrentSubmitChatRunning(t *testing.T)` — TestChat_RaceConcurrentSubmitChatRunning 验证并发 Submit 时 ErrChatRunning 和 InputQueue 竞态安全。
- `func TestChat_RaceSnapshotDuringChat(t *testing.T)` — TestChat_RaceSnapshotDuringChat 验证 Chat 运行期间并发读取 Snapshot 无 data race。
- `func TestChat_RaceToolHandling(t *testing.T)` — TestChat_RaceToolHandling 验证工具投影与 Snapshot 并发安全。
- `func TestService_RaceShutdownSubmit(t *testing.T)` — TestService_RaceShutdownSubmit 验证 Shutdown 与 Submit 并发安全。
- `func TestService_RaceShutdownChat(t *testing.T)` — TestService_RaceShutdownChat 验证 Shutdown 与 Chat 并发安全。
- `func TestService_ShutdownThenSubmit(t *testing.T)` — TestService_ShutdownThenSubmit 验证 Shutdown 后 Submit 返回错误。
- `func TestService_ShutdownThenStartChat(t *testing.T)` — TestService_ShutdownThenStartChat 验证 Shutdown 后 startChat 返回错误。
- `func TestEffortManager_RaceConcurrentApply(t *testing.T)` — TestEffortManager_RaceConcurrentApply 验证并发 Apply 安全。
- `func TestEffortManager_RaceCycleAndApply(t *testing.T)` — TestEffortManager_RaceCycleAndApply 验证 Cycle 与 Apply 并发安全。
- `func TestPromptStack_RacePushRender(t *testing.T)` — TestPromptStack_RacePushRender 验证并发 Push + Render 安全。
- `func TestPromptStack_RacePopPush(t *testing.T)` — TestPromptStack_RacePopPush 验证并发 Pop/PopKind + Push 安全。
- `func TestPromptStack_RaceClearKind(t *testing.T)` — TestPromptStack_RaceClearKind 验证并发 ClearKind + Push 安全。
- `func TestInputQueue_RaceEnqueueDuringChat(t *testing.T)` — TestInputQueue_RaceEnqueueDuringChat 验证 Chat 运行中并发入队安全。
- `func TestSnapshot_RaceReadWrite(t *testing.T)` — TestSnapshot_RaceReadWrite 验证 Snapshot 读写并发安全。
- `func TestCancelChat_Race(t *testing.T)` — TestCancelChat_Race 验证并发 CancelChat 安全。
- `func TestObserveInteraction_Race(t *testing.T)` — TestObserveInteraction_Race 验证审批观察并发安全。

### reasoning_visible_test.go

- `func TestAttachLatestReasoningExposesThinkingToVisibleMessage(t *testing.T)` — TestAttachLatestReasoningExposesThinkingToVisibleMessage 验证回合结束后，

### reference_tools_test.go

- `func TestReadToolResultResolvesNodeRef(t *testing.T)` — TestReadToolResultResolvesNodeRef 验证 P1 桥：read_tool_result 对
- `func TestReadToolResultResolvesCallAlias(t *testing.T)` — TestReadToolResultResolvesCallAlias 验证 result:call_<callID> 别名映射：

### resident_lru_test.go

- `func withResidentLimit(limit int) func()`
- `func residentOf(t *testing.T, service *legacyTestService, sessionID string) bool`
- `func TestResidentLimitEvictsLeastRecentlyUsedIdle(t *testing.T)` — TestResidentLimitEvictsLeastRecentlyUsedIdle 波 4 G6 INV-G8：驻留上限 2，
- `func TestResidentLimitKeepsBusySessions(t *testing.T)` — TestResidentLimitKeepsBusySessions 波 4 G6 INV-G8：运行中会话不可驱逐
- `func TestSessionReleaseDefaults(t *testing.T)` — TestSessionReleaseDefaults 释放面默认口径（2026-10-14）：统一窗口默认 1

### review_files_test.go

- `func reviewFixture(t *testing.T) (*assembly.Service, string)` — reviewFixture 在一个真实临时工作区里摆出各类文件（正常 / 敏感 / 二进制 /
- `func TestRequestFileReviewClassifiesByWorkspaceBoundary(t *testing.T)` — TestRequestFileReviewClassifiesByWorkspaceBoundary：一次批处理召唤的裁决口径——
- `func TestRequestFileReviewProjectsIntoSnapshotAndClears(t *testing.T)` — TestRequestFileReviewProjectsIntoSnapshotAndClears：落点是**会话作用域的可重放
- `func TestRequestFileReviewClearedOnWorkspaceSwitch(t *testing.T)` — TestRequestFileReviewClearedOnWorkspaceSwitch：失效语义对齐既有口径——工作区
- `func TestRequestFileReviewRejectsOverCapAndEmpty(t *testing.T)` — TestRequestFileReviewRejectsOverCapAndEmpty：超限显式报错（不静默截断），空清单
- `func TestRequestFileReviewWithoutWorkspaceFailsExplicitly(t *testing.T)` — TestRequestFileReviewWithoutWorkspaceFailsExplicitly：没有绑定工作区时明确失败
- `func TestRequestFileReviewAllSkippedKeepsSnapshotUntouched(t *testing.T)` — TestRequestFileReviewAllSkippedKeepsSnapshotUntouched：全部被跳过时不写快照——

### role_permissions_test.go

- `func newRolePermissionFakeSessions() *rolePermissionFakeSessions`
- `func (fake *rolePermissionFakeSessions) RolePermissionVectors(sessionID string) (map[string][]string, error)`
- `func (fake *rolePermissionFakeSessions) RolePermissionVector(sessionID, role string) ([]string, bool, error)`
- `func (fake *rolePermissionFakeSessions) SetRolePermissionVector(sessionID, role string, vector []string) error`
- `func (fake *rolePermissionFakeSessions) RolePermissionLayers(sessionID, role string) dto.RoleSettingsLayers`
- `func (fake *rolePermissionFakeSessions) RoleDefaultVectors(sessionID string) (map[string][]string, error)`
- `func (fake *rolePermissionFakeSessions) RoleDefault(sessionID, role string) ([]string, bool, error)`
- `func (fake *rolePermissionFakeSessions) SetRoleDefault(sessionID, role string, vector []string) error`
- `func (fake *rolePermissionFakeSessions) RoleAccount(sessionID, role string) (string, bool, error)`
- `func (fake *rolePermissionFakeSessions) SetRoleAccount(sessionID, role, account string) error`
- `func (fake *rolePermissionFakeSessions) RoleInheritance(sessionID, role string) (bool, bool, error)`
- `func (fake *rolePermissionFakeSessions) SetRoleInheritance(sessionID, role string, inherit bool) error`
- `func (fake *rolePermissionFakeSessions) RoleSSHUserSpaces(sessionID, role string) ([]string, error)`
- `func (fake *rolePermissionFakeSessions) RoleDefaultSSHUserSpaces(_ string, role string) ([]string, error)`
- `func (fake *rolePermissionFakeSessions) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error`
- `func (fake *rolePermissionFakeSessions) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error`
- `func fourLayerFixture() dto.RoleSettingsLayers` — fourLayerFixture 是五层中的四层配了一遍、且**互相冲突**的一组来源：
- `func TestRestoreRolePermissionsPrefersStoredVector(t *testing.T)`
- `func TestRestoreRolePermissionsExplicitEmptyVectorWins(t *testing.T)` — TestRestoreRolePermissionsExplicitEmptyVectorWins 是三态纪律的核心用例：
- `func TestRestoreRolePermissionsWithoutRegistryFailsLoudly(t *testing.T)`
- `func TestRolePermissionVectorSingleRole(t *testing.T)`
- `func TestRoleDefaultVectorIsExplicitNotEffective(t *testing.T)`
- `func TestRoleAccountRoundTrip(t *testing.T)`
- `func (spy *roleAccountRuntimeSpy) SetRoleAccount(role, account string) error`
- `func TestRoleAccountPushesToRuntimeAndRestores(t *testing.T)`
- `func TestRoleInheritanceDefaultsAndUserSpaceNormalization(t *testing.T)`
- `func TestInheritedRoleUsesMainAgentSSHUserSpaces(t *testing.T)`
- `func TestSessionSSHUserSpacesOverrideDefaultAndFollowInheritance(t *testing.T)`
- `func TestSetRolePermissionVectorRejectsMalformedEntries(t *testing.T)`
- `func TestRestoreRolePermissionsConcurrentRoles(t *testing.T)` — TestRestoreRolePermissionsConcurrentRoles 让多个角色同时恢复：判据无共享可变
- `func TestRoleAccountApplicationSetAccountRollsBackRuntime(t *testing.T)`
- `func TestRoleAccountApplicationRestoreSkipsExplicitlyMissingAccount(t *testing.T)`
- `func (store *roleAccountApplicationFakeStore) RoleAccount(sessionID, role string) (string, bool, error)`
- `func (store *roleAccountApplicationFakeStore) SetRoleAccount(sessionID, role, account string) error`
- `func (runtime *roleAccountApplicationFakeRuntime) SetRoleAccount(role, account string) error`

### role_tool_activity_test.go

- `func waitRoleToolEvent(t *testing.T, subscription event.Subscription, wantKind event.EventKind) event.Event` — waitRoleToolEvent 等一条 teammate.tool.* 事件（超时即失败）。
- `func TestHandleRoleToolActivityPublishesSessionEvent(t *testing.T)`
- `func TestHandleRoleToolActivityTruncatesAndDropsUnroutable(t *testing.T)`

### runtime_extra_test.go

- `func (runtime *fakeRuntime) BindProjectRootFor(sessionID, rootPath string) error`
- `func (runtime *fakeRuntime) UnbindProjectRootFor(sessionID string)`

### served_route_test.go

- `func TestStampServedRouteFillsAssistantMessageAndPublishes(t *testing.T)`
- `func TestStampServedRouteWithoutFactIsNoop(t *testing.T)`
- `func TestStampServedRouteSkipsAlreadyStampedTail(t *testing.T)`
- `func TestStampAssistantRouteForScopeAndIdempotence(t *testing.T)` — StampAssistantRouteFor 的行级口径：只写本轮（TaskID 匹配）的 assistant 行，

### service_account_test.go

- `func withTestAccountAdmin(admin contract.AccountAdminPort) testServiceOption` — withTestAccountAdmin 注入「手动添加账号」的宿主能力面（不注入 = 该宿主不支持
- `func (admin *fakeAccountAdmin) AddAccount(draft dto.AccountDraft) (dto.AccountAddResult, error)`
- `func (admin *fakeAccountAdmin) AccountDetails(name string) (dto.AccountDetails, error)`
- `func (admin *fakeAccountAdmin) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error)`
- `func (admin *fakeAccountAdmin) DeleteAccount(name string) (dto.AccountDetails, error)`
- `func TestAddAccountNeedsHostPort(t *testing.T)` — TestAddAccountNeedsHostPort：宿主没装 AccountAdmin 时要给一句明确错误，而不是
- `func TestAddAccountForwardsDraftVerbatim(t *testing.T)` — TestAddAccountForwardsDraftVerbatim：草稿（含 api_key）必须原样递到宿主——本层
- `func TestAddAccountPropagatesHostError(t *testing.T)` — TestAddAccountPropagatesHostError：宿主的错误要原样冒上来（界面靠这句话判断
- `func TestUpdateAndDeleteAccountForwardIdentities(t *testing.T)`
- `func (runtime *balanceFakeRuntime) AccountBalance(_ context.Context, name string) (dto.AccountBalance, error)`
- `func TestAccountBalanceNeedsHostPort(t *testing.T)` — TestAccountBalanceNeedsHostPort：宿主没装余额能力时明确报错（demo/headless 宿主）。
- `func TestAccountBalanceForwardsName(t *testing.T)` — TestAccountBalanceForwardsName：按名字查（不是"当前选中账号"），读数原样返回。
- `func TestAccountBalancePropagatesHostError(t *testing.T)` — TestAccountBalancePropagatesHostError：探测失败（含"该供应商没有余额接口"）

### service_account_window_forward_test.go

- `func TestSetAccountWindowNeedsRuntimePort(t *testing.T)`
- `func (store *recordingWindowStoreForForward) SaveAccountWindow(string, int) error`

### service_batch_helpers_test.go

- `func (sessions *liveCatalogSessions) SessionsOf(string) []model.SessionInfo`
- `func (sessions *liveCatalogSessions) setInfos(infos []model.SessionInfo)`
- `func runtimeKnobs(service *legacyTestService) (string, int)`
- `func enqueueQueuedInputs(t *testing.T, service *legacyTestService, inputs ...string)`
- `func sessionIDsOf(sessions []model.SessionInfo) []string`
- `func containsSessionID(ids []string, target string) bool`
- `func lastMessageOf(messages []model.Message) *model.Message`
- `func dumpParallelState(_ *legacyTestService, engine *multiSessionEngine, ids ...string) string`
- `func (engine *blockingEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *blockingEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *multiSessionEngine) debugSnapshot(sessionIDs ...string) string`

### service_chat_test.go

- `func TestReActBudgetStopsOnlyAfterItsToolBudget(t *testing.T)`
- `func TestReActBudgetUsesReservedFinalDeliveryTurn(t *testing.T)` — TestReActBudgetUsesReservedFinalDeliveryTurn 预算耗尽后的收尾回合必须走
- `func TestRuntimeMailboxDrainedAndDiscarded(t *testing.T)` — TestRuntimeMailboxDrainedAndDiscarded：子代理 merge-back 邮箱的内容不得进入
- `func TestSessionBackedIterationInterruptsOnQueuedInput(t *testing.T)`
- `func TestChatPublishesSnapshotWithoutUI(t *testing.T)`
- `func TestGracefulShutdownWaitsForQueuedChat(t *testing.T)`
- `func TestSessionBackedQueueIsConsumedAtRunChatEnd(t *testing.T)`
- `func TestCancelChatInterruptsContextAwareEngine(t *testing.T)`
- `func TestCancelChatWithStaleRequestID(t *testing.T)` — TestCancelChatWithStaleRequestID 验证取消语义归属：request_id 只是参考，动作

### service_components_test.go

- `func TestNewAssemblesFocusedServiceComponents(t *testing.T)`
- `func TestServiceFacadeContainsOnlyAssembly(t *testing.T)` — TestServiceFacadeContainsOnlyAssembly 钉住 Service 只由「装配状态 + 组件图」
- `func TestFocusedComponentsDoNotHoldServiceFacade(t *testing.T)`
- `func TestComponentsForReturnsCompleteOwnerGraph(t *testing.T)`
- `func assertNonNilOwnerGraph(t *testing.T, value reflect.Value, path string)`

### service_effort_wire_test.go

- `func (runtime *effortWireRuntime) SetSessionReasoningEffort(effort string) int`
- `func (runtime *effortWireRuntime) pushedEfforts() []string`
- `func newEffortWireService(t *testing.T) (*legacyTestService, *effortWireRuntime)`
- `func TestSwitchEffortPushesWireReasoningEffort(t *testing.T)` — TestSwitchEffortPushesWireReasoningEffort：档位变化必须把该档对应的 wire 思考
- `func TestSwitchEffortCyclePushesNextLevel(t *testing.T)` — TestSwitchEffortCyclePushesNextLevel：cycle 入口（GUI 控件/TUI 快捷键的循环切档）
- `func TestSwitchEffortUnknownLevelDoesNotPush(t *testing.T)` — TestSwitchEffortUnknownLevelDoesNotPush：不认识的档位在 Apply 阶段就被拒，账号池

### service_input_fork_queue_test.go

- `func (runtime *forkQueueRuntime) ForkInFlight(string) bool`
- `func TestSubmitQueuedWhileForkRunning(t *testing.T)` — TestSubmitQueuedWhileForkRunning（2026-10-05 退役 fork 门控）钉住新链路：fork/subagent

### service_input_test.go

- `func TestLifecycleApplicationWaitForIdle(t *testing.T)`
- `func TestLifecycleApplicationShutdownRunsCleanupOnce(t *testing.T)`
- `func trustedSkillInHistory(t *testing.T, history []contract.EngineMessage, name, text string) bool` — trustedSkillInHistory 断言引擎历史中存在激活技能 internal 事件
- `func TestSystemPromptStableAcrossPlanNodeChanges(t *testing.T)`
- `func TestWorkTableTraceBlock(t *testing.T)`
- `func TestPrepareExecutionContextCarriesWorkTableTraceBlock(t *testing.T)`
- `func TestSuggestionsAndSkillRouting(t *testing.T)`
- `func TestApprovalBrokerResolve(t *testing.T)`

### service_interaction_guard_test.go

- `func TestEffortAndPluginGuardsAroundRunningSessions(t *testing.T)` — TestEffortAndPluginGuardsAroundRunningSessions 覆盖 G0b 守卫：
- `func TestEffortCommandUsesGuardedServicePath(t *testing.T)` — TestEffortCommandUsesGuardedServicePath /effort 命令必须走 SwitchEffort：
- `func TestPluginSwitchPreservesEffort(t *testing.T)` — TestPluginSwitchPreservesEffort：插件切换只换 prompt 前缀，不得顺手把用户的

### service_knobs_smoke_test.go

- `func (engine *knobSmokeEngine) SetMaxLoops(loops int)`
- `func (engine *knobSmokeEngine) loops() int`
- `func projectRuntime(t *testing.T, service *legacyTestService)` — projectRuntime 模拟一次 runtime 投影重建（回合尾 / 工具边界会做的事）。
- `func TestKnobChainSmoke(t *testing.T)`

### service_knobs_test.go

- `func knobService(t *testing.T) (*legacyTestService, *fakeEngine)`
- `func TestSetLoopLimitOnlyTouchesLoopLimit(t *testing.T)` — TestSetLoopLimitOnlyTouchesLoopLimit：改循环次数不改思考强度，且引擎与快照一致。
- `func TestSetLoopLimitRejectsOutOfRange(t *testing.T)` — TestSetLoopLimitRejectsOutOfRange：上界 = limits.input_loop_limit（9999 是 goal
- `func TestSetReasoningEffortOnlyTouchesReasoningEffort(t *testing.T)` — TestSetReasoningEffortOnlyTouchesReasoningEffort：改思考强度只推一次 wire 值，
- `func TestSetReasoningEffortRejectsUnknownVocabulary(t *testing.T)` — TestSetReasoningEffortRejectsUnknownVocabulary：非法值（含 "session"——那是"没有
- `func TestSwitchEffortSeedsBothKnobReadings(t *testing.T)` — TestSwitchEffortSeedsBothKnobReadings：档位是两条拉条的**预设**——切一次档位，
- `func TestKnobsAreGuardedWhileViewSessionRuns(t *testing.T)` — TestKnobsAreGuardedWhileViewSessionRuns：视图会话 running 时两条拉条都拒绝，且
- `func serviceRuntimePushes(t *testing.T, service *legacyTestService) []string` — serviceRuntimePushes 读记录桩里的下发序列（等价于 effortWireRuntime.pushedEfforts，

### service_notice_test.go

- `func TestServiceAddNoticeAppendsSystemMessage(t *testing.T)`

### service_queue_component_test.go

- `func TestConversationQueuePreservesPresentationEventOrder(t *testing.T)`
- `func TestConversationQueueDoesNotOwnServiceOrDuplicateQueueState(t *testing.T)`

### service_queue_test.go

- `func enqueueRawQueuedInput(t *testing.T, service *legacyTestService, text string)`
- `func TestQueueEditIndexSpaceMatchesProjection(t *testing.T)` — TestQueueEditIndexSpaceMatchesProjection（节点 156 红线）：ChatState.InputQueue
- `func TestReorderQueuedInputFollowsProjectionIndex(t *testing.T)` — TestReorderQueuedInputFollowsProjectionIndex：调换的下标是投影下标——旧实现
- `func startBlockingChat(t *testing.T) (*legacyTestService, *blockingEngine)` — startBlockingChat 启动一个阻塞中的回合（返回可在测试里显式放开的引擎）。
- `func TestReorderQueuedInputReordersVisibleQueue(t *testing.T)` — TestReorderQueuedInputReordersVisibleQueue 调换排队顺序：可见投影
- `func TestReorderQueuedInputDrivesPromotedBatchOrder(t *testing.T)` — TestReorderQueuedInputDrivesPromotedBatchOrder 调换后的顺序决定下一轮
- `func TestRecallQueuedInputPopsEntryAndReturnsText(t *testing.T)` — TestRecallQueuedInputPopsEntryAndReturnsText 撤回 = 出队 + 交还展示原文：
- `func TestQueueEditErrorSemantics(t *testing.T)` — TestQueueEditErrorSemantics 越界 / 非运行态 / 会话不存在都有明确错误。
- `func TestQueueEditErrorsBeforeAnySession(t *testing.T)` — TestQueueEditErrorsBeforeAnySession 进程还没有任何会话单元时，视图会话按
- `func TestConcurrentRecallQueuedInput(t *testing.T)` — TestConcurrentRecallQueuedInput（-race）并发撤回同一会话：撤回与 Enqueue /

### service_release_window_test.go

- `func (store *recordingReleaseWindowStore) SaveSessionReleaseWindow(window int) error`
- `func (store *recordingReleaseWindowStore) calls() []int`
- `func snapshotWindowLimits(t *testing.T)` — snapshotWindowLimits 保存并恢复**进程级** limits 快照：窗口住在包级 atomic 上，
- `func TestSetSessionReleaseWindowAppliesImmediately(t *testing.T)` — TestSetSessionReleaseWindowAppliesImmediately：改一个数，释放面的现读路径立刻看见。
- `func TestSetSessionReleaseWindowAcceptsBoundaries(t *testing.T)` — TestSetSessionReleaseWindowAcceptsBoundaries：区间两端（1 与 16）都被接受。
- `func TestSetSessionReleaseWindowRejectsOutOfRange(t *testing.T)` — TestSetSessionReleaseWindowRejectsOutOfRange：越界一个字节都不改——不落盘、不动内存。
- `func TestSetSessionReleaseWindowLeavesMemoryUntouchedWhenPersistFails(t *testing.T)` — TestSetSessionReleaseWindowLeavesMemoryUntouchedWhenPersistFails：落盘失败 ⇒ 整步
- `func TestSetSessionReleaseWindowWithoutStoreAppliesToMemoryOnly(t *testing.T)` — TestSetSessionReleaseWindowWithoutStoreAppliesToMemoryOnly：宿主没装配落盘口 ⇒

### service_scheduler_test.go

- `func TestStartScheduledSessionCreatesNewSessionInWorkspace(t *testing.T)` — TestStartScheduledSessionCreatesNewSessionInWorkspace 钉住定时触发的会话落点：
- `func TestStartScheduledSessionWithoutWorkspaceKeepsSessionUnbound(t *testing.T)` — TestStartScheduledSessionWithoutWorkspaceKeepsSessionUnbound 钉住"工作区是
- `func (runtime *fakeRuntime) sessionProjectRoot(sessionID string) string` — sessionProjectRoot 读 fakeRuntime 的按会话工具根（同包测试直读，加锁）。
- `func conversationContents(messages []model.Message) string` — conversationContents 把可见会话拼成一段文本（判据只看"出现过没有"）。
- `func TestUpdateScheduledTaskForwardsAndValidatesWorkspace(t *testing.T)` — TestUpdateScheduledTaskForwardsAndValidatesWorkspace 钉住编辑这条链的应用层口径：
- `func TestAssembleScheduledRunAppliesTierAndPlugins(t *testing.T)` — TestAssembleScheduledRunAppliesTierAndPlugins 钉住任务的**装配**落到哪儿：
- `func TestScheduledRunParamsDefaultsAndValidation(t *testing.T)` — TestScheduledRunParamsDefaultsAndValidation 钉住定时任务那两条执行参数（思考强度 /
- `func TestScheduledPluginsValidatedAgainstCatalog(t *testing.T)` — TestScheduledPluginsValidatedAgainstCatalog 钉住应用层的插件装配校验：未知插件名

### service_snapshot_test.go

- `func TestSnapshotNeverSerializesSystemPrompt(t *testing.T)`
- `func TestEventHubOrdersAndResyncs(t *testing.T)`
- `func TestMessageDeltaIncludesStableMessageID(t *testing.T)`
- `func TestToolEventsUpdateSnapshot(t *testing.T)`
- `func TestToolCompletionDoesNotReenterServiceLockForGoalSkillVisibility(t *testing.T)`

### service_stop_flush_test.go

- `func (engine *stopQueueEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *stopQueueEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 显式转发到自身 ChatStream（覆盖内嵌 fakeEngine 的提升方法，
- `func (engine *stopQueueEngine) recordedInputs() []string`
- `func TestStopFlushesQueuedInputsIntoTheNextTurn(t *testing.T)` — TestStopFlushesQueuedInputsIntoTheNextTurn 钉住停止按钮的第三条语义：点停止后清空

### service_test.go

- `func TestNewTestServiceCleansUpCatalogWorker(t *testing.T)`
- `func TestSessionCatalogAllowsDuplicateNamesWithDistinctIDs(t *testing.T)`
- `func TestSessionTitleUsesFirstUserQuestion(t *testing.T)`
- `func TestCurrentSessionNameUsesFirstQuestion(t *testing.T)`
- `func TestWorkingHistoryReleasesOnlyAfterSuccessfulPersistence(t *testing.T)`
- `func TestBeginNewSessionIsLazyAndFirstQuestionMaterializesIt(t *testing.T)`
- `func TestLazySessionInheritsProjectOnlyWhenMaterialized(t *testing.T)`
- `func TestCreateWorkspacesPublishesCatalogWithoutBindingCurrentSession(t *testing.T)`
- `func TestInitialLazySessionIsDraftAndFirstSubmitMaterializes(t *testing.T)`
- `func TestResumeSessionLeavesLazyDraft(t *testing.T)`
- `func TestNewTaskSessionIsTrulyUnbound(t *testing.T)` — TestNewTaskSessionIsTrulyUnbound 未关联工作区的会话必须真正未关联：
- `func TestWorkspaceSessionBindsDraftBeforeMaterialization(t *testing.T)` — TestWorkspaceSessionBindsDraftBeforeMaterialization 覆盖 GUI「工作区会话」
- `func TestResumeRestoresProjectScope(t *testing.T)`
- `func TestNewHydratesPersistedWorkspaceSessions(t *testing.T)`
- `func TestResumeReadsSessionFromItsPersistedWorkspace(t *testing.T)`
- `func TestResumeRepairsBindingWhenHistoryLivesInAnotherWorkspace(t *testing.T)`
- `func TestLoadMoreHistoryUsesResumedSessionWorkspace(t *testing.T)`
- `func TestSwitchProjectStartsIndependentSessionWhenHistoryExists(t *testing.T)`
- `func TestSnapshotIncludesPersistedSessions(t *testing.T)`
- `func TestSnapshotDoesNotReadBlockedSessionCatalog(t *testing.T)`
- `func TestShutdownDoesNotWaitForBlockedSessionCatalog(t *testing.T)`
- `func TestBeginNewSessionKeepsGlobalWorkTable(t *testing.T)` — TestBeginNewSessionKeepsGlobalWorkTable 工作表格是项目/全局台账：/new
- `func TestResumedChatPersistsToSelectedSession(t *testing.T)`
- `func TestLoadMoreHistoryAssignsStableMessageIDs(t *testing.T)`
- `func TestResumeCommandOpensSessionInteraction(t *testing.T)`

### session_archive_test.go

- `func TestResumeSessionAttachesSessionContext(t *testing.T)` — TestEnrichTranscriptMessageIDsPairsEventsToMessages 验证 event-to-message
- `func TestResumeSessionFailsWhenContextCorrupt(t *testing.T)` — TestResumeSessionFailsWhenContextCorrupt 验证损坏/不可用的 context 模块
- `func TestBeginNewSessionDetachesSessionContext(t *testing.T)` — TestBeginNewSessionDetachesSessionContext 验证进入 draft/新建会话时解绑
- `func TestSessionArchivePreservesVisibleHistoryPlanAndReadCache(t *testing.T)`
- `func TestResumeSessionDropsMetadataOnlyCheckpointAndUsesDurableConversation(t *testing.T)`
- `func TestResumeLongContextReasksOpeningQuestionFromCheckpoint(t *testing.T)`
- `func historyContainsAssistant(history []contract.EngineMessage, content string) bool`
- `func TestBoundConversationTailKeepsOnlyConfiguredVariableHeightWindow(t *testing.T)`
- `func TestPersistSessionRecordRebuildsConversationFromTranscript(t *testing.T)`
- `func TestSessionRecordStoresLargeContentByReference(t *testing.T)`
- `func TestCompletedTaskClearsTaskScopedSkillsBeforeNextRequest(t *testing.T)`
- `func TestResumeSessionUsesRecordWhenProviderHistoryIsUnavailable(t *testing.T)`
- `func TestResumeSessionContinuationKeepsTranscriptHistory(t *testing.T)`
- `func TestResumeSessionContinuationKeepsToolStepsWithoutEmptyAssistantEvents(t *testing.T)` — TestResumeSessionContinuationKeepsToolStepsWithoutEmptyAssistantEvents 是
- `func TestResumeSessionContinuationKeepsTrailingUnansweredUserInput(t *testing.T)`
- `func TestProviderRepairNoteNeverBecomesVisibleAssistantText(t *testing.T)`

### session_binding_regression_test.go

- `func TestBindWorkspaceSetsProjectScope(t *testing.T)`

### session_catalog_grid_test.go

- `func TestCatalogRefreshScopedByProjectKeepsOtherProjectGrids(t *testing.T)`
- `func TestCatalogFullRefreshReplacesProjectGrid(t *testing.T)` — TestCatalogFullRefreshReplacesProjectGrid 钉住全量刷新按项目整格替换：

### session_catalog_test.go

- `func addCatalogSession(sessions *scopedSessions, projectID, sessionID string, updatedAt time.Time)`
- `func newCatalogTestService(t *testing.T, sessions *scopedSessions) *legacyTestService`
- `func TestWaitCatalogRefreshSettlesFreshCatalog(t *testing.T)` — TestWaitCatalogRefreshSettlesFreshCatalog 是 C3 回执的核心契约：一轮
- `func TestWaitCatalogRefreshServesCoalescedRequests(t *testing.T)` — TestWaitCatalogRefreshServesCoalescedRequests 覆盖唤醒槽位被丢弃时的回执
- `func TestWaitCatalogRefreshConvergesAfterShutdown(t *testing.T)` — TestWaitCatalogRefreshConvergesAfterShutdown 钉住关闭路径：worker 退出时
- `func TestCatalogCacheMirrorsWorkerRound(t *testing.T)` — TestCatalogCacheMirrorsWorkerRound G5 CatalogMu：目录 worker 的枚举结果先落
- `func TestCatalogCacheObservesProjectDiscoveredBindings(t *testing.T)` — TestCatalogCacheObservesProjectDiscoveredBindings 钉住缓存里的 discovered
- `func TestCatalogRefreshConcurrentWithTitleWritesAndSnapshotReads(t *testing.T)` — TestCatalogRefreshConcurrentWithTitleWritesAndSnapshotReads 钉住 G5 锁拆分

### session_cold_read_test.go

- `func newColdReadSessions() *coldReadSessions`
- `func (sessions *coldReadSessions) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]store.Event, error)`
- `func (sessions *coldReadSessions) setEvents(projectID, sessionID string, events []store.Event)`
- `func newColdReadService(t *testing.T, sessions *coldReadSessions) *legacyTestService`
- `func TestSnapshotOfColdSessionAssemblesReadOnlyBaseline(t *testing.T)` — TestSnapshotOfColdSessionAssemblesReadOnlyBaseline C1：未驻留（无 unit）
- `func TestSnapshotOfColdUnknownSessionStillUnavailable(t *testing.T)` — TestSnapshotOfColdUnknownSessionStillUnavailable 钉住未知会话的失败语义
- `func TestGetSessionTranscriptRange(t *testing.T)` — TestGetSessionTranscriptRange 钉住冷读区间语义：含端点、倒置显式报错、
- `func TestListSessionsReturnsAuthoritativeDirectory(t *testing.T)` — TestListSessionsReturnsAuthoritativeDirectory C1：目录枚举不要求视图快照

### session_cold_start_draft_test.go

- `func TestColdStartDraftSlotIsRetainedAcrossSwitch(t *testing.T)`

### session_concurrent_content_isolation_test.go

- `func TestConcurrentSessionsKeepOwnContent(t *testing.T)`
- `func concurrentTranscriptText(events []model.TranscriptEvent) string` — concurrentTranscriptText 把 transcript 拼成可搜索文本（角色 + 正文 + 调用 ID）。
- `func concurrentViewText(service *legacyTestService, sessionID string) string` — concurrentViewText 返回指定会话可见视图的全部消息文本（用户 + assistant +

### session_conversation_history_query_test.go

- `func (store *blockingConversationRangeStore) LoadConversationRangeWorkspace( workspaceID, sessionID string, offset, limit int, ) ([]model.Message, int, error)`
- `func TestLoadMoreHistoryReadsOutsideSharedWriteLock(t *testing.T)`

### session_cross_session_cold_load_repro_test.go

- `func (sessions *gatedColdSessions) releaseNow()`
- `func (sessions *gatedColdSessions) LoadHistory(sessionID string) ([]contract.EngineMessage, error)`
- `func (sessions *gatedColdSessions) LoadHistoryRange(sessionID string, offset, limit int) ([]contract.EngineMessage, int, error)`
- `func (sessions *gatedColdSessions) gate(sessionID string)`
- `func newGatedColdSessions() *gatedColdSessions`
- `func (runtime *scopeSpyRuntime) SwitchSessionTasks(sessionID string, records []dto.TaskRecord)`
- `func (runtime *scopeSpyRuntime) ClearSubagentTree() error`
- `func (runtime *scopeSpyRuntime) snapshot() (string, []string, int)`
- `func callerStacks() string` — callerStacks 抓取全部 goroutine 栈（阻塞现场证据）。
- `func transitionLockFrames(stacks string) string` — transitionLockFrames 从栈里挑出与过渡锁/冷加载相关的帧（截断输出用）。
- `func waitRestoringCleared(t *testing.T, service *legacyTestService, sessionID string)` — waitRestoringCleared 等目标会话的后台冷加载走完（restoring 清除）。冷加载
- `func releaseEngineSession(engine *multiSessionEngine, sessionID string)` — releaseEngineSession 释放测试引擎的会话回合门闩（幂等；会话从未运行过时
- `func drainColdLoadFixture(t *testing.T, service *legacyTestService, engine *multiSessionEngine, sessions *gatedColdSessions)` — drainColdLoadFixture 释放冷加载门闩与引擎门闸，把现场收干净后在调用方报告
- `func TestReproColdLoadBlocksAnotherSessionSubmit(t *testing.T)` — TestReproColdLoadBlocksAnotherSessionSubmit：A 冷加载（历史装载中）时，
- `func TestReproColdLoadStealsSharedRuntimeScopeFromRunningSession(t *testing.T)` — TestReproColdLoadStealsSharedRuntimeScopeFromRunningSession：B 在飞（引擎

### session_directory_visibility_test.go

- `func TestWorkspaceDraftShowsItsWorkspaceInDirectory(t *testing.T)` — TestWorkspaceDraftShowsItsWorkspaceInDirectory 钉住第 1 条：草稿的工作区绑定
- `func TestTaskDraftStaysUnboundInDirectory(t *testing.T)` — TestTaskDraftStaysUnboundInDirectory 钉住反向判据：没有工作区的「任务会话」
- `func TestRunningSessionVisibleInDirectory(t *testing.T)` — TestRunningSessionVisibleInDirectory 钉住第 2 条：首轮运行中（尚未落盘）的
- `func TestScheduledSessionVisibleBeforePersist(t *testing.T)` — TestScheduledSessionVisibleBeforePersist 钉住第 3 条：定时任务新建的后台会话
- `func findSessionRow(rows []model.SessionInfo, sessionID string) (model.SessionInfo, bool)`

### session_draft_context_attach_test.go

- `func TestMaterializeDraftSessionBindsContextStoreForNewSession(t *testing.T)`
- `func TestBeginNewSessionKeepsRunningSessionContextStore(t *testing.T)` — TestBeginNewSessionKeepsRunningSessionContextStore 钉住"离开会话"这条路上的
- `func (sessions *contextAwareScopedSessions) AttachSessionContext(workspaceID, sessionID string) error`
- `func (sessions *contextAwareScopedSessions) DetachSessionContext()`
- `func TestWorkspaceSwitchFreshSessionBindsContextStore(t *testing.T)` — TestWorkspaceSwitchFreshSessionBindsContextStore 钉住第三条"让某个会话成为当前

### session_draft_context_scope_test.go

- `func TestBeginNewSessionClearsPreviousSessionContextFacts(t *testing.T)`

### session_draft_shell_test.go

- `func seedPreviousSessionFacts(service *legacyTestService)` — seedPreviousSessionFacts 在快照里铺满"上一个会话留下的会话事实"，供三条路径消费。
- `func TestBeginNewSessionDraftShellClearsEverySessionFact(t *testing.T)`
- `func TestColdRestoreFailureDraftShellKeepsWindowOnly(t *testing.T)`
- `func TestUnloadViewDraftShellKeepsProcessInteractionAndWritesNoSlot(t *testing.T)`
- `func TestUnloadViewDraftShellDropsSessionScopedInteraction(t *testing.T)` — 会话级交互挂在被卸下的会话上：换单元后必须清掉，否则用户会对着一个已不存在的

### session_draft_snapshot_test.go

- `func (sessions *draftSnapshotSessions) AppendDraftSnapshotWorkspace( _ string, _ string, events []model.TranscriptEvent, ) (int, bool, error)`
- `func (sessions *draftSnapshotSessions) snapshotCalls() [][]model.TranscriptEvent`
- `func TestDraftSnapshotAdvancesLeftPointerByMemorySuffix(t *testing.T)` — TestDraftSnapshotAdvancesLeftPointerByMemorySuffix 钉住双指针语义：
- `func TestDraftSnapshotLoopIsPeriodicAndStops(t *testing.T)` — TestDraftSnapshotLoopIsPeriodicAndStops 覆盖长任务定时器：回合运行中按周期

### session_effort_test.go

- `func TestEffortOwnershipPerSession(t *testing.T)` — TestEffortOwnershipPerSession（G4）：effort 选择归属进 SessionUnit——
- `func TestSubagentPolicySlotSyncPerSession(t *testing.T)` — TestSubagentPolicySlotSyncPerSession（G1-C）：chat 起点按会话 effort 把 plan

### session_fork_store_regression_test.go

- `func newRouterForkSessions(t *testing.T) *routerForkSessions`
- `func (s *routerForkSessions) saveParentFixture(t *testing.T, record model.SessionRecord, events []store.Event, contextPayload []byte)`
- `func (s *routerForkSessions) SaveCurrent(string) error`
- `func (s *routerForkSessions) SetWorkspace(projectID string)`
- `func (s *routerForkSessions) Workspace() string`
- `func (s *routerForkSessions) List() []model.SessionInfo`
- `func (s *routerForkSessions) LoadHistory(string) ([]contract.EngineMessage, error)`
- `func (s *routerForkSessions) LoadHistoryRange(string, int, int) ([]contract.EngineMessage, int, error)`
- `func (s *routerForkSessions) Delete(sessionID string) error`
- `func (s *routerForkSessions) SessionsOf(projectID string) []model.SessionInfo`
- `func (s *routerForkSessions) SaveSessionRecord(sessionID string, record model.SessionRecord) error`
- `func (s *routerForkSessions) SaveSessionRecordWorkspace(projectID, sessionID string, record model.SessionRecord) error`
- `func (s *routerForkSessions) LoadSessionRecord(sessionID string) (model.SessionRecord, error)`
- `func (s *routerForkSessions) LoadSessionRecordWorkspace(projectID, sessionID string) (model.SessionRecord, error)`
- `func (s *routerForkSessions) SaveSessionSnapshot( sessionID string, history []contract.EngineMessage, record model.SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult, ) error`
- `func (s *routerForkSessions) SaveSessionSnapshotWorkspace( projectID, sessionID string, _ []contract.EngineMessage, record model.SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult, ) error`
- `func (s *routerForkSessions) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]store.Event, error)`
- `func (s *routerForkSessions) LoadToolResultsWorkspace(projectID, sessionID string) ([]store.ToolResult, error)`
- `func (s *routerForkSessions) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error)`
- `func (s *routerForkSessions) SaveContextStateWorkspace(projectID, sessionID string, payload []byte) error`
- `func (s *routerForkSessions) CurrentGenerationWorkspace(projectID, sessionID string) (string, error)`
- `func (s *routerForkSessions) LoadTranscriptTailWorkspace(projectID, sessionID string, _, _ int) ([]model.TranscriptEvent, error)`
- `func (s *routerForkSessions) LoadToolResultWorkspace(projectID, sessionID, ref string) (model.StoredToolResult, error)`
- `func transcriptEventsToStore(events []model.TranscriptEvent) []store.Event`
- `func storeEventsToTranscript(events []store.Event) []model.TranscriptEvent`
- `func storedToolResultsToStore(results []model.StoredToolResult) []store.ToolResult`
- `func TestForkChildVisibleWithRealStore(t *testing.T)`

### session_fork_test.go

- `func (s *forkServiceSessions) LoadSessionRecord(string) (model.SessionRecord, error)`
- `func (s *forkServiceSessions) Delete(sessionID string) error`
- `func (s *forkServiceSessions) LoadSessionRecordWorkspace(_, sessionID string) (model.SessionRecord, error)`
- `func (s *forkServiceSessions) SaveSessionRecord(string, model.SessionRecord) error`
- `func (s *forkServiceSessions) SaveSessionRecordWorkspace(string, string, model.SessionRecord) error`
- `func (s *forkServiceSessions) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]store.Event, error)`
- `func (s *forkServiceSessions) LoadTranscriptTailWorkspace(projectID, sessionID string, tokenBudget, maxUnits int) ([]model.TranscriptEvent, error)`
- `func (s *forkServiceSessions) LoadToolResultsWorkspace(projectID, sessionID string) ([]store.ToolResult, error)`
- `func (s *forkServiceSessions) SaveSessionSnapshotWorkspace(projectID, sessionID string, history []contract.EngineMessage, record model.SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult) error`
- `func (s *forkServiceSessions) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error)`
- `func (s *forkServiceSessions) SaveContextStateWorkspace(projectID, sessionID string, payload []byte) error`
- `func (s *forkServiceSessions) CurrentGenerationWorkspace(projectID, sessionID string) (string, error)`
- `func TestForkSessionCreatesAndSwitchesToChild(t *testing.T)`
- `func TestForkSessionChildContentVisibleAfterResume(t *testing.T)` — TestForkSessionChildContentVisibleAfterResume 钉住 fork 回归：ForkSession
- `func lastNonSystemMessageOf(messages []model.Message) *model.Message`
- `func TestForkSessionLatestResolvesNewestParagraph(t *testing.T)`
- `func TestForkCommandForksCurrentSession(t *testing.T)`
- `func TestForkSessionDeepCopyIsolation(t *testing.T)` — TestForkSessionDeepCopyIsolation（T2.7）：fork 子会话 record 与父数据面
- `func TestForkSessionRejectsRunningParent(t *testing.T)` — TestForkSessionRejectsRunningParent（UC5）：父会话运行中拒绝 fork。

### session_fullaccess_isolation_test.go

- `func waitPendingApprovalsFor(t *testing.T, service *legacyTestService, sessionID string, want int)` — waitPendingApprovalsFor 等待指定会话的待批审批数达到 want。
- `func TestProbeFullAccessToggleStaysWithinViewSession(t *testing.T)` — TestProbeFullAccessToggleStaysWithinViewSession：视图会话点全权只影响自己。
- `func TestProbeFullAccessSurvivesBackgroundSessionStartSync(t *testing.T)` — TestProbeFullAccessSurvivesBackgroundSessionStartSync：别的会话起跑

### session_fullaccess_test.go

- `func TestFullAccessOwnershipPerSession(t *testing.T)` — TestFullAccessOwnershipPerSession（G4）：fullAccess 选择归属进 SessionUnit
- `func TestFullAccessProjectionPerSession(t *testing.T)` — TestFullAccessProjectionPerSession（G4）：运行时投影按会话读取生效的

### session_g5_lock_test.go

- `func (e *streamEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 覆盖内嵌引擎：先发流式块，再委托底层阻塞执行。
- `func TestConcurrentStreamingViewSwitchNoPollution(t *testing.T)` — TestConcurrentStreamingViewSwitchNoPollution 钉住两个会话在流式输出与热

### session_history_browsing_submit_repro_test.go

- `func TestReproSubmitWhileBrowsingKeepsUserRowInWindow(t *testing.T)` — TestReproSubmitWhileBrowsingKeepsUserRowInWindow 回看历史时提交一轮对话，
- `func TestReproSubmitWhileBrowsingEndsBrowsingState(t *testing.T)` — TestReproSubmitWhileBrowsingEndsBrowsingState 提交之后会话不得再被判成
- `func containsContent(contents []string, want string) bool`

### session_history_coldload_latest_repro_test.go

- `func internalCheckpointRow(index int) model.Message` — internalCheckpointRow 造一条**已落盘**的内部行（上下文 checkpoint 标记）。
- `func seedDurableRows(store *pagedSessionStore, visible, trailingInternal int)` — seedDurableRows 铺一整份**已发布**行：visible 条 durable-N + trailingInternal
- `func coldLoadLongSession(t *testing.T, trailingInternal int) (*legacyTestService, *pagedSessionStore)` — coldLoadLongSession 冷加载一个「durable 20 条 + 末尾内部行」的长会话（窗口 6），
- `func assertWindowAtTail(t *testing.T, service *legacyTestService, label string)` — assertWindowAtTail 断言「回到最新」之后的三件事：窗口贴尾（前端
- `func TestReproColdLoadThenReturnToLatestReachesTail(t *testing.T)` — TestReproColdLoadThenReturnToLatestReachesTail 红灯 1：冷加载后翻一页再点
- `func TestReproColdLoadReturnToLatestWhenTailWindowIsInternalRows(t *testing.T)` — TestReproColdLoadReturnToLatestWhenTailWindowIsInternalRows 红灯 2：尾窗那一段

### session_history_hot_tail_test.go

- `func beginInFlightTurn(t *testing.T, service *legacyTestService, store *pagedSessionStore, count int) []string` — beginInFlightTurn 造「本轮已进内存窗口、尚未落盘」的在飞尾部：store 只物理
- `func assertVisibleWindow(t *testing.T, label string, snapshot model.Snapshot, wantOffset int, wantContents []string, wantTotal int)` — assertVisibleWindow 断言快照里的「窗口起点 + 窗口内容 + 已可见总数」。
- `func TestLoadLatestHistoryKeepsInFlightTail(t *testing.T)` — TestLoadLatestHistoryKeepsInFlightTail 红灯 1（缺陷 1）：回合进行中，内存窗口
- `func TestColdPageReadDoesNotRegressVisibleTotal(t *testing.T)` — TestColdPageReadDoesNotRegressVisibleTotal 红灯 2（缺陷 2）：「加载更早」滑走
- `func TestToolResultAfterPagingDoesNotPunchHole(t *testing.T)` — TestToolResultAfterPagingDoesNotPunchHole 红灯 3（缺陷 1+2 的后果）：内存窗口
- `func TestLoadLatestHistorySplicesStraddlingHotTail(t *testing.T)` — TestLoadLatestHistorySplicesStraddlingHotTail 红灯 4（缺陷 1+3）：内存窗口正好
- `func TestInFlightTailReturnsAfterCommit(t *testing.T)` — TestInFlightTailReturnsAfterCommit 收口断言：在飞行落盘（发布点推进）之后，

### session_history_pagination_test.go

- `func withHistoryWindow(window int) func()` — withHistoryWindow 临时把进程级 history_window 调小，让分页行为在少量消息
- `func newPagedSessionStore(sessionID string, count int) *pagedSessionStore`
- `func (store *pagedSessionStore) countLocked() int`
- `func (store *pagedSessionStore) SessionsOf(projectID string) []model.SessionInfo`
- `func (store *pagedSessionStore) LoadHistory(sessionID string) ([]contract.EngineMessage, error)`
- `func (store *pagedSessionStore) LoadHistoryRange(sessionID string, offset, limit int) ([]contract.EngineMessage, int, error)`
- `func (store *pagedSessionStore) LoadSessionRecordWorkspace(workspaceID, sessionID string) (model.SessionRecord, error)`
- `func (store *pagedSessionStore) SaveSessionRecord(sessionID string, record model.SessionRecord) error` — 下面三个方法补齐 SessionRecordPort（生产 internal/host.SessionPort 同时
- `func (store *pagedSessionStore) SaveSessionRecordWorkspace(workspaceID, sessionID string, record model.SessionRecord) error`
- `func (store *pagedSessionStore) LoadSessionRecord(sessionID string) (model.SessionRecord, error)`
- `func (store *pagedSessionStore) LoadConversationRangeWorkspace(workspaceID, sessionID string, offset, limit int) ([]model.Message, int, error)` — LoadConversationRangeWorkspace 是生产分页读回面（SessionConversationRangePort）：
- `func (store *pagedSessionStore) appendDurable(role, content string)` — appendDurable 追加一条**已提交**行（物理落盘 + 推进发布点）。
- `func (store *pagedSessionStore) appendDraft(role, content string)` — appendDraft 只将行物理落盘，不推进发布点：对冷读面不可见（等价 message
- `func (store *pagedSessionStore) publishDraftTail()` — publishDraftTail 推进发布点（回合收尾 PersistCurrentSession 同一效果）。
- `func (store *pagedSessionStore) appendRowLocked(role, content string)`
- `func (store *pagedSessionStore) setPublished(messages ...model.Message)` — setPublished 直接铺一整份**已发布**的磁盘消息：不关心草稿尾的用例用它，
- `func (store *pagedSessionStore) rangeReads() int`
- `func (store *pagedSessionStore) historyReads() int`
- `func sliceWindow[T any](items *[]T, offset, limit int) []T`
- `func visibleContents(snapshot model.Snapshot) []string` — visibleContents 取快照对话里的非 system 消息内容（system 是「已恢复会话」
- `func wantRange(start, count int) []string`
- `func assertRange(t *testing.T, label string, snapshot model.Snapshot, start, count int)`
- `func TestLegacySessionWithoutRecordExposesEarlierHistory(t *testing.T)` — TestLegacySessionWithoutRecordExposesEarlierHistory 旧格式会话（端口只提供
- `func openLongSession(t *testing.T, window, total int) (*legacyTestService, *pagedSessionStore)` — openLongSession 打开一个长会话（window 条尾窗 + offset），返回服务与存储。
- `func mirrorOnce(t *testing.T, service *legacyTestService, store *pagedSessionStore)` — mirrorOnce 触发一次会话可见投影镜像（真实链路上任何新消息/工具事件都会
- `func TestLoadMoreHistoryPrependsPageAndSurvivesMirror(t *testing.T)` — TestLoadMoreHistoryPrependsPageAndSurvivesMirror 红灯 1：
- `func TestLoadMoreHistoryPagesToBeginning(t *testing.T)` — TestLoadMoreHistoryPagesToBeginning 红灯 2：连续翻页必须单调向更早推进，
- `func TestLoadLatestHistoryReturnsToTail(t *testing.T)` — TestLoadLatestHistoryReturnsToTail 红灯 3：翻到更早以后必须能一键回到最新
- `func TestAppendWhileBrowsingHistoryKeepsWindow(t *testing.T)` — TestAppendWhileBrowsingHistoryKeepsWindow 红灯 4：正在翻更早历史时新到达的
- `func TestColdLoadRecordDoesNotReadProviderHistory(t *testing.T)` — TestColdLoadRecordDoesNotReadProviderHistory 钉住 v8 冷加载的重复读边界：

### session_input_index_test.go

- `func inputIndexService(t *testing.T, store *pagedSessionStore) *legacyTestService`
- `func setLoadedWindow(t *testing.T, service *legacyTestService, sessionID string, start, end int, messages []model.Message)` — setLoadedWindow 模拟"前端只加载了尾部一窗"：把可见窗口锚定在 [start,end)。
- `func TestSessionInputIndexCoversUnloadedEarlyRounds(t *testing.T)` — TestSessionInputIndexCoversUnloadedEarlyRounds 全量索引必须包含尚未加载到
- `func TestSessionInputIndexEmptySession(t *testing.T)` — TestSessionInputIndexEmptySession 空会话（无消息、无窗口）返回空索引且不报错。
- `func TestSessionInputIndexRequiresSessionID(t *testing.T)` — TestSessionInputIndexRequiresSessionID 空 sessionID 显式失败（不静默成空索引）。
- `func TestSessionInputIndexTruncatesSummary(t *testing.T)` — TestSessionInputIndexTruncatesSummary 摘要长度有界（rune 截断 + 原文长度留痕）。
- `func TestSessionInputIndexSkipsNonUserAndInternalRows(t *testing.T)` — TestSessionInputIndexSkipsNonUserAndInternalRows 只索引真正的用户输入：
- `func TestSessionInputIndexFallsBackToRecord(t *testing.T)` — TestSessionInputIndexFallsBackToRecord 会话存储没有 message 行（旧布局）时

### session_lifecycle_test.go

- `func TestResumeRunningSessionAllowsHotAttach(t *testing.T)` — TestResumeRunningSessionAllowsHotAttach（TC-A3-01 新语义）：运行中会话
- `func TestHotAttachDoesNotTouchRunningSession(t *testing.T)` — TestHotAttachDoesNotTouchRunningSession（TC-A3-02）：B 活跃、A 后台运行中
- `func TestHotAttachNoReplay(t *testing.T)` — TestHotAttachNoReplay（TC-LC-02）：空闲驻留会话 resume 只换指针，不重建
- `func TestUnloadReleasesScope(t *testing.T)` — TestUnloadReleasesScope（TC-LC-03）：unload 释放会话内存态（chat 运行态、
- `func TestUnloadRejectsRunningSession(t *testing.T)` — TestUnloadRejectsRunningSession（阶段 D · 卸载/提交互斥）：unload 与 submit
- `func containsMessageContent(messages []model.Message, needle string) bool`
- `func conversationTextsForTest(conversation []model.Message) []string`

### session_live_content_probe_test.go

- `func newStagedChatEngine() *stagedChatEngine`
- `func (engine *stagedChatEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func TestSwitchBackShowsLatestBackgroundContent(t *testing.T)` — TestSwitchBackShowsLatestBackgroundContent 复现“切回会话时前端拿到的内容

### session_message_edit_test.go

- `func editForkFixture(t *testing.T) (*legacyTestService, *forkServiceSessions, *multiSessionEngine)`
- `func editForkRetainedWindow(t *testing.T, service *legacyTestService, retainedFrom int)`
- `func userTexts(messages []model.Message) []string`
- `func TestEditUserMessageAndForkUsesPreviousParagraphAndSubmitsReplacement(t *testing.T)`
- `func TestEditUserMessageAndForkCanReplaceLatestUserMessage(t *testing.T)`
- `func TestEditUserMessageAndForkRejectsCompressedAndRunningParents(t *testing.T)`
- `func TestEditUserMessageAndForkRejectsStaleParentRevision(t *testing.T)`
- `func TestEditUserMessageAndForkSameOperationIsIdempotent(t *testing.T)`
- `func TestEditUserMessageAndForkSamePointWithNewOperationCreatesNewChild(t *testing.T)`
- `func TestEditUserMessageAndForkKeepsChildIsolatedFromParentLaterTurns(t *testing.T)`
- `func TestEditForkRollbackDeletesChildAndRestoresParentView(t *testing.T)`
- `func TestEditUserMessageAndForkExplainsMissingEventSeq(t *testing.T)`
- `func TestAnnotateConversationUsesRetainedWindowAndRunningState(t *testing.T)`

### session_parallel_test.go

- `func TestParallelSessionsExecuteConcurrently(t *testing.T)`
- `func TestParallelSessionsQueuedPerSession(t *testing.T)` — TestParallelSessionsQueuedPerSession 验证每个会话维护自己的输入队列：A 运行

### session_pending_tail_test.go

- `func (s *pendingTailSessions) record(call string)`
- `func (s *pendingTailSessions) callOrder() []string`
- `func (s *pendingTailSessions) PendingMessageTailWorkspace(_, _ string) (dto.PendingMessageTailReport, bool, error)`
- `func (s *pendingTailSessions) RecoverPendingMessageTailWorkspace(_, _ string) (dto.PendingMessageTailReport, bool, error)`
- `func (s *pendingTailSessions) DiscardPendingMessageTailWorkspace(_, _ string) (dto.PendingMessageTailReport, bool, error)`
- `func pendingTailVisibleTail(t *testing.T, service *legacyTestService) string` — pendingTailVisibleTail 返回可见会话里最后一条 system 行的正文（没有则 ""）。
- `func TestPendingTailRecoverableIsRecoveredAndVisible(t *testing.T)` — TestPendingTailRecoverableIsRecoveredAndVisible：基座一致的草稿尾在冷加载时被
- `func TestPendingTailGapIsReportedOnly(t *testing.T)` — TestPendingTailGapIsReportedOnly：基座断裂只报告——不得调用恢复，也不得丢弃，
- `func TestPendingTailCleanIsSilent(t *testing.T)` — TestPendingTailCleanIsSilent：没有草稿尾时不做任何写、不打扰用户。
- `func TestPendingTailRunningSessionNotTouched(t *testing.T)` — TestPendingTailRunningSessionNotTouched：回合正在跑的会话，加载路径不得抢发布点
- `func TestPendingTailCapabilityAbsentIsNoop(t *testing.T)` — TestPendingTailCapabilityAbsentIsNoop：存储未装配该能力时静默退回（纯增强语义）。

### session_permission_tier_persist_test.go

- `func newTierMemSessions() *tierMemSessions`
- `func (sessions *tierMemSessions) SessionPermissionTier(sessionID string) (string, error)` — SessionPermissionTier 读回会话级档位设置（空 = 该会话从未选择，回退进程默认）。
- `func (sessions *tierMemSessions) SetSessionPermissionTier(sessionID, tier string) error` — SetSessionPermissionTier 写入会话级档位设置。
- `func (sessions *tierMemSessions) tierFor(sessionID string) string`
- `func TestPermissionTierSurvivesSessionReload(t *testing.T)` — TestPermissionTierSurvivesSessionReload：选 full → 重新装配（= 重启）→ 仍是
- `func TestPermissionTierReloadKeepsManualChoice(t *testing.T)` — TestPermissionTierReloadKeepsManualChoice：显式选了 manual 也必须记住——它不同于
- `func TestPermissionTierWithoutSettingPortStaysInMemory(t *testing.T)` — TestPermissionTierWithoutSettingPortStaysInMemory：未装配会话级设置端口的最小宿主
- `func TestPermissionTierSettingPortErrorSurfaces(t *testing.T)` — TestPermissionTierSettingPortErrorSurfaces：会话级设置写失败必须显式报错且**不改**
- `func (sessions *failingTierSessions) SetSessionPermissionTier(string, string) error`
- `func TestBeginNewSessionMirrorsDraftPermissionTier(t *testing.T)` — TestBeginNewSessionMirrorsDraftPermissionTier：**新建会话**必须把新会话的生效
- `func TestBeginNewSessionRestoresPersistedDraftTier(t *testing.T)` — TestBeginNewSessionRestoresPersistedDraftTier：草稿槽位是**可复用**的（切走再
- `func TestPermissionTierSwitchMirrorsViewSnapshot(t *testing.T)` — TestPermissionTierSwitchMirrorsViewSnapshot：切到某会话时，**目标会话**的生效

### session_permission_tier_test.go

- `func projectedTier(t *testing.T, service *legacyTestService, sessionID string) string` — projectedTier 返回指定会话的运行时投影里生效的权限档位。
- `func projectedFullAccess(t *testing.T, service *legacyTestService, sessionID string) bool` — projectedFullAccess 返回指定会话的运行时投影里生效的全权派生位。
- `func TestPermissionTierOwnershipPerSession(t *testing.T)` — TestPermissionTierOwnershipPerSession：档位选择归属进 SessionUnit——每个会话保存
- `func TestPermissionTierProjectionPerSession(t *testing.T)` — TestPermissionTierProjectionPerSession：运行时投影按会话读取生效档位（view 协调器
- `func TestSetPermissionTierRejectsUnknown(t *testing.T)` — TestSetPermissionTierRejectsUnknown：非法档位 id 报错且**不改变**当前档位。
- `func TestSetFullAccessCompatMapsToTier(t *testing.T)` — TestSetFullAccessCompatMapsToTier：旧全权开关是档位的兼容壳（true ⇔ full，

### session_pollution_s0_test.go

- `func TestS0BackgroundSessionTaskWriteMustNotPolluteActiveRegistry(t *testing.T)`

### session_project_root_test.go

- `func (runtime *fakeRuntime) toolRootForSession(sessionID string) string` — toolRootForSession 模拟工具面的路径根解析：会话根优先，未绑定时回退进程默认
- `func newMultiProjectWorkspace() *multiProjectWorkspace`
- `func (repo *multiProjectWorkspace) Create(name, rootPath, gitRemote string) (model.WorkspaceInfo, error)`
- `func TestBackgroundSessionKeepsOwnProjectRoot(t *testing.T)` — TestBackgroundSessionKeepsOwnProjectRoot 复现工作区污染：会话 A 绑定项目 A，
- `func waitSessionIdle(t *testing.T, service *legacyTestService)` — waitSessionIdle 等待全部会话回合结束（多会话并行时视图 Chat 状态不代表进程空闲）。

### session_race_test.go

- `func TestSnapshotBumpConcurrentWithRunChatTail(t *testing.T)` — TestSnapshotBumpConcurrentWithRunChatTail（TC-R-02）：并发 Submit（触发
- `func TestReleaseWorkingHistoryConcurrentWithChatStream(t *testing.T)` — TestReleaseWorkingHistoryConcurrentWithChatStream（TC-R-03）：收尾清工作

### session_recovery_bench_test.go

- `func BenchmarkSessionRecoveryPath(b *testing.B)` — BenchmarkSessionRecoveryPath 对照应用层两条恢复路径：

### session_release_async_test.go

- `func TestDeleteSessionReleasesBackgroundRuns(t *testing.T)`

### session_release_test.go

- `func TestSessionReleaseFacadeOnlyDelegatesToApplication(t *testing.T)`
- `func withSessionReleaseWindow(window int) func()` — withSessionReleaseWindow 把统一窗口改成 window，并把两个分层覆盖键归零
- `func newReleaseAwareEngine() *releaseAwareEngine`
- `func (engine *releaseAwareEngine) ReleaseSessionBundle(sessionID string) bool` — ReleaseSessionBundle 模拟桥侧释放面：记录被释放的会话并回报"真的释放了"。
- `func (engine *releaseAwareEngine) wasReleased(sessionID string) bool`
- `func (engine *contentReleaseEngine) ReleaseSessionBundle(sessionID string) bool`
- `func waitEngineReleased(t *testing.T, engine *releaseAwareEngine, sessionID string)` — waitEngineReleased 轮询等待引擎侧会话离场（驱逐在 touch 内同步完成；-race/高负载
- `func goalBundleActive(service *legacyTestService, sessionID string) bool` — goalBundleActive 通过公开状态读面判断 goal 协调器是否仍持有该会话运行时。
- `func TestPointViewAtRestoringShellKeepsTargetUnitContent(t *testing.T)` — TestPointViewAtRestoringShellKeepsTargetUnitContent：切回"装载在途"的会话时，视图
- `func TestPointViewAtRestoringShellIgnoresSettledTarget(t *testing.T)` — TestPointViewAtRestoringShellIgnoresSettledTarget：装载已收口（restoring 已清）时
- `func TestReleaseSessionMemoryForgetsGoalStoreHandle(t *testing.T)` — TestReleaseSessionMemoryForgetsGoalStoreHandle：释放面必须通知"按会话缓存了
- `func TestSessionReleaseWindowKeepsOnlyPreviousSession(t *testing.T)` — TestSessionReleaseWindowKeepsOnlyPreviousSession：窗口 1 的口径——三个会话依次
- `func TestSessionReleaseWindowProtectsBusySession(t *testing.T)` — TestSessionReleaseWindowProtectsBusySession：运行中会话是受保护集，即使它是最旧
- `func TestSessionReleaseWindowDropsPreviousContent(t *testing.T)` — TestSessionReleaseWindowDropsPreviousContent：窗口 1 下最旧会话的**可见正文**
- `func TestDeleteSessionReleasesMemory(t *testing.T)` — TestDeleteSessionReleasesMemory：删除**非视图**会话必须把内存一起放掉（引擎 +
- `func TestDeleteViewSessionSwitchesViewToFreshDraft(t *testing.T)` — TestDeleteViewSessionSwitchesViewToFreshDraft：删除**当前视图**会话时，重的三面
- `func TestReleaseSessionMemorySkipsProtectedSession(t *testing.T)` — TestReleaseSessionMemorySkipsProtectedSession：释放面自身带二次确认——直接调用

### session_resource_isolation_test.go

- `func TestSessionDomainsDisjoint(t *testing.T)` — TestSessionDomainsDisjoint（TC-INV-01）：A 与 B 的 M/X/R 状态域无共享，
- `func TestViewSwitchDoesNotMutateExecution(t *testing.T)` — TestViewSwitchDoesNotMutateExecution（TC-INV-02）：切到 B 只换视图指针，
- `func TestPersistReadsOnlyOwnDomain(t *testing.T)` — TestPersistReadsOnlyOwnDomain（TC-INV-03）：快照/活跃槽全是 B 时，

### session_running_not_rerooted_test.go

- `func TestRunningSessionNotRerootedByAttach(t *testing.T)`

### session_runtime_slot_integration_test.go

- `func (engine *sessionTokenEngine) TokenCountFor(sessionID string) string`
- `func TestBackgroundRuntimeProjectionLandsInOwnSlot(t *testing.T)` — TestBackgroundRuntimeProjectionLandsInOwnSlot（G1-A/B 验收）：后台会话 B

### session_s0_events_test.go

- `func (engine *s0ChunkEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func viewContains(unit *session.SessionUnit, text string) bool` — viewContains 报告指定会话可见对话中是否出现目标文本。
- `func TestS0BackgroundEventsDoNotPolluteActiveSnapshot(t *testing.T)` — TestS0BackgroundEventsDoNotPolluteActiveSnapshot（波 1 验收锚）：
- `func TestS0SwitchResyncsBaseline(t *testing.T)` — TestS0SwitchResyncsBaseline（波 1 验收锚）：视图切到会话 B 后按显式 sid
- `func conversationTexts(messages []model.Message) []string`

### session_scope_test.go

- `func (perSessionFakeRuntime) PerSessionExecution() bool`
- `func TestPerSessionHostDoesNotSkipGlobalScopeSideEffects(t *testing.T)` — TestPerSessionHostDoesNotSkipGlobalScopeSideEffects 回归：即使宿主声明
- `func TestCrossSessionSubmitWhileRunningNoLongerBusy(t *testing.T)` — TestCrossSessionSubmitWhileRunningNoLongerBusy 验证 M2 多会话并行语义：
- `func TestSubmitToSessionDelegatesForActiveSession(t *testing.T)` — TestSubmitToSessionDelegatesForActiveSession 验证同会话提交走既有
- `func TestSubmitToSessionRejectsEmptyID(t *testing.T)` — TestSubmitToSessionRejectsEmptyID 验证会话级 API 拒绝空会话 ID。
- `func TestActivateSessionAllowedForIdleTargetWhileOtherRunning(t *testing.T)` — TestActivateSessionAllowedForIdleTargetWhileOtherRunning 验证 M2 会话级
- `func TestSnapshotOfOnlyActiveSessionAvailable(t *testing.T)` — TestSnapshotOfOnlyActiveSessionAvailable 验证 M1 快照粒度：仅活跃会话
- `func TestChatEventsCarrySessionID(t *testing.T)` — TestChatEventsCarrySessionID 验证 chat 生命周期事件携带会话路由键。
- `func TestSubscribeSessionFiltersBySessionID(t *testing.T)` — TestSubscribeSessionFiltersBySessionID 验证会话级订阅只投递目标会话

### session_snapshot_test.go

- `func TestSessionSnapshotTransportShape(t *testing.T)`
- `func TestSessionSnapshotCarriesResidentFlag(t *testing.T)` — TestSessionSnapshotCarriesResidentFlag C1：SessionSnapshot 顶层携带
- `func TestProcessSnapshotTransportShape(t *testing.T)` — TestProcessSnapshotTransportShape（G3）：ProcessSnapshot 承载进程级目录与

### session_ssh_test.go

- `func TestSessionEnvironmentCanBeFixedOnceAndNeverChanged(t *testing.T)`

### session_state_display_test.go

- `func TestBackgroundSessionStateChangeNotifiesDisplaySurfaces(t *testing.T)`

### session_status_test.go

- `func catalogStatusOf(t *testing.T, service *legacyTestService, sessionID string) model.SessionStatus` — catalogStatusOf 返回会话在 Snapshot 目录中的可见状态（等待目录刷新）。
- `func TestSessionStatusReflectsRunningWhileChatActive(t *testing.T)` — TestSessionStatusReflectsRunningWhileChatActive（状态机回归）：chat 启动
- `func TestSwitchToRunningSessionPreservesState(t *testing.T)` — TestSwitchToRunningSessionPreservesState（T4.5/运行中切换）：A 运行中切到

### session_stress_test.go

- `func TestStressConcurrentSessionsDoNotPollute(t *testing.T)`

### session_submit_restoring_test.go

- `func (engine *multiSessionEngine) streamCallsFor(sessionID string) int`
- `func (engine *multiSessionEngine) releaseSession(sessionID string)` — releaseSession 放行某会话的引擎回合（加锁取通道；幂等关闭）。
- `func coldRestoreFixture(t *testing.T) (*multiSessionEngine, *gatedHistorySessions, *legacyTestService)` — coldRestoreFixture 复现「A 运行中、目标冷会话 B 的存储读被门闩限速」的
- `func TestSubmitDuringColdRestoreDefersAndKeepsHistoryOrder(t *testing.T)` — TestSubmitDuringColdRestoreDefersAndKeepsHistoryOrder 锁定 2026-09-17 根因
- `func TestSubmitToSessionDefersUntilRestoreCompletes(t *testing.T)` — TestSubmitToSessionDefersUntilRestoreCompletes 锁定「延后」语义的两条边界：

### session_switch_deadlock_test.go

- `func dumpAllGoroutines() string` — dumpAllGoroutines 返回全量 goroutine 栈（死锁断点现场）。
- `func waitOrDump(t *testing.T, done <-chan error, what string) error` — waitOrDump 等待 done；超时则 dump 全量 goroutine 并 Fatal。
- `func TestSwitchToOtherSessionWhileChattingRepro(t *testing.T)` — TestSwitchToOtherSessionWhileChattingRepro 场景 1：会话 A 运行中切换到会话 B。
- `func TestBeginNewSessionWhileChattingRepro(t *testing.T)` — TestBeginNewSessionWhileChattingRepro 场景 2：会话 A 运行中尝试新建会话。
- `func TestQueueSendDuringSessionSwitchRepro(t *testing.T)` — TestQueueSendDuringSessionSwitchRepro 场景 3：运行中切换与入队并发（-race 检测）。
- `func TestDraftSlotRetainedAcrossSwitchRepro(t *testing.T)` — TestDraftSlotRetainedAcrossSwitchRepro 验证草稿槽位：切换会话后草稿仍在
- `func TestBeginNewSessionAllowedWhileChattingRepro(t *testing.T)` — TestBeginNewSessionAllowedWhileChattingRepro 验证运行中允许进入草稿：

### session_switch_dynamics_test.go

- `func newChunkCaptureEngine() *chunkCaptureEngine`
- `func (engine *chunkCaptureEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *chunkCaptureEngine) emit(sessionID, chunk string)` — emit 向指定会话进行中的 ChatStream 注入一个文本块（会话未启动时静默
- `func viewRoles(service *legacyTestService, sessionID string) []string` — viewRoles 返回指定会话可见对话的角色序列（含文本内容），用于断言顺序。
- `func indexOf(roles []string, prefix string) int` — indexOf 返回序列中首个以 prefix 开头的下标；不存在返回 -1。
- `func TestToolOrderingStableAfterSwitchToRunningSession(t *testing.T)` — TestToolOrderingStableAfterSwitchToRunningSession 复现「切换运行中会话后
- `func TestBackgroundToolEventsCarryOwnRequestID(t *testing.T)` — TestBackgroundToolEventsCarryOwnRequestID 复现后台会话工具事件携带活跃
- `func TestSubmitPromptWhileOtherSessionRuns(t *testing.T)` — TestSubmitPromptWhileOtherSessionRuns 复现输入阻塞：A 运行中（引擎阻塞），
- `func TestDeltasKeepFlowingAfterSwitchToRunningSession(t *testing.T)` — TestDeltasKeepFlowingAfterSwitchToRunningSession 回归守卫：切到运行中

### session_switch_failed_resume_test.go

- `func (s *attachFailingSessions) AttachSessionContext(_ string, sessionID string) error`
- `func (s *attachFailingSessions) DetachSessionContext()`
- `func TestFailedResumeKeepsViewOnPreviousSessionAndSubmitContinuesIt(t *testing.T)` — TestFailedResumeKeepsViewOnPreviousSessionAndSubmitContinuesIt 复现“切换失败

### session_switch_helpers_test.go

- `func (sessions *gatedHistorySessions) releaseNow()`
- `func (sessions *gatedHistorySessions) LoadHistory(sessionID string) ([]contract.EngineMessage, error)`
- `func (sessions *gatedHistorySessions) LoadHistoryRange(sessionID string, offset, limit int) ([]contract.EngineMessage, int, error)`
- `func (sessions *gatedHistorySessions) gateEnabled(sessionID string) bool`

### session_switch_probe_test.go

- `func switchDuringInFlightChat(t *testing.T, label string)` — switchDuringInFlightChat 在引擎回合仍在执行（未 release）时切换视图到另
- `func TestSwitchDuringToolInvocation(t *testing.T)` — TestSwitchDuringToolInvocation 复现：工具调用（引擎忙）期间切换会话。
- `func TestSwitchDuringLLMStreaming(t *testing.T)` — TestSwitchDuringLLMStreaming 复现：LLM 输出（引擎忙）期间切换会话。
- `func TestSwitchDuringColdLoadSerializes(t *testing.T)` — TestSwitchDuringColdLoadSerializes 度量「冷加载占不占视图过渡 key」这条契约。
- `func TestBeginNewSessionSingleDraftOwner(t *testing.T)` — TestBeginNewSessionSingleDraftOwner 复现/钉住“新建会话”的幂等与责任链：

### session_switch_running_cold_test.go

- `func TestColdResumeWhileRunningIsAsync(t *testing.T)`

### shutdown_concurrent_test.go

- `func (engine *cancelAwareEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *cancelAwareEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *cancelAwareEngine) count() int`
- `func TestShutdownConcurrentRunningSessions(t *testing.T)` — TestShutdownConcurrentRunningSessions（R5）：运行中多会话 + 并发 Shutdown

### skill_activate_test.go

- `func TestActivateSkillPushesLayerAndNextSubmissionTrustsIt(t *testing.T)` — TestActivateSkillPushesLayerAndNextSubmissionTrustsIt：模型侧激活（ActivateSkill）
- `func TestActivateSkillIdempotent(t *testing.T)` — TestActivateSkillIdempotent：同名重复激活不产生重复层（Push 同名覆盖）。
- `func TestActivateSkillRejectsBadNames(t *testing.T)` — TestActivateSkillRejectsBadNames：空名/未知技能/goal 均拒绝且不改状态。
- `func TestActivateSkillCoexistsWithUserSlashSkill(t *testing.T)` — TestActivateSkillCoexistsWithUserSlashSkill：模型激活与用户 Skill 前缀路径

### skill_context_test.go

- `func TestDisplayUserInputRejectsInvalidEnvelope(t *testing.T)`
- `func TestAdaptEngineMessageRestoresOriginalUserInput(t *testing.T)` — TestAdaptEngineMessageRestoresOriginalUserInput 走会话历史的唯一判据
- `func TestDisplayUserInputHidesPrivateRecoveryEnvelopes(t *testing.T)` — TestDisplayUserInputHidesPrivateRecoveryEnvelopes 钉住「显示归一化」与「历史可见性」

### snapshot_budget_test.go

- `func snapshotToolOutputLimit() int`
- `func TestBoundToolResultForSnapshotArchives(t *testing.T)` — TestBoundToolResultForSnapshotArchives 验证截断链入口：超限输出归档为
- `func TestToolResultContentPagination(t *testing.T)` — TestToolResultContentPagination 验证分页读回：offset/limit/UTF-8 边界
- `func TestSnapshotBudgetUsesConfiguredLimit(t *testing.T)` — TestSnapshotBudgetUsesConfiguredLimit 验证 limits 注入生效：设置较小

### subagent_detail_lock_regression_test.go

- `func (engine *lockOrderEngine) SubAgentTree() []dto.SubAgentTreeNode`
- `func TestSubagentDetailNoViewMuSessionLockInversion(t *testing.T)` — TestSubagentDetailNoViewMuSessionLockInversion 复现 2026-09-08 死锁链路：

### subagent_detail_test.go

- `func TestSubagentSessionDetailMissingNode(t *testing.T)` — TestAdaptSubagentConversation 验证会话记录适配：截断（evidence_chars）、
- `func TestSubagentSessionDetailCarriesContext(t *testing.T)` — TestAdaptSubagentContext 验证上下文快照适配：截断（evidence_chars）、
- `func TestSubagentSessionDetailCarriesWorktree(t *testing.T)` — TestSubagentSessionDetailCarriesWorktree 验证失败现场恢复入口：节点
- `func TestScheduledTasksProjectIntoRuntimeSnapshot(t *testing.T)` — TestScheduledTasksProjectIntoRuntimeSnapshot 验证周期任务与白名单命令经
- `func TestTodoItemsProjectIntoRuntimeSnapshot(t *testing.T)` — TestTodoItemsProjectIntoRuntimeSnapshot 验证 todolist 清单经运行时投影
- `func strPtr(value string) *string`

### subagent_envelope_store_test.go

- `func TestInheritedContextEnvelopeNotEnteringContextStores(t *testing.T)` — TestInheritedContextEnvelopeNotEnteringContextStores 复现：子代理 merge-back

### subagent_evidence_test.go

- `func TestSubagentMailboxContentDoesNotReachFrontendOrStorage(t *testing.T)`

### subagent_tree_test.go

- `func TestCollectRuntimeProjectionCarriesSubAgentTree(t *testing.T)` — TestCollectRuntimeProjectionCarriesSubAgentTree 验证权威 Snapshot 投影

### subscribe_session_test.go

- `func TestSubscribeSessionFollowsDraftMaterialization(t *testing.T)` — TestSubscribeSessionFollowsDraftMaterialization 验证早分配 SID 后，草稿
- `func TestSubscribeSessionFollowsViewPointer(t *testing.T)` — TestSubscribeSessionFollowsViewPointer 验证空 sid 订阅（过渡口径）仍按

### task_execution_test.go

- `func TestTaskTerminalHandlerRecordsBoundedCompletion(t *testing.T)`
- `func TestTaskFailedRequiresFailureType(t *testing.T)`
- `func TestTaskNeedsUserDecisionRecordsDistinctTerminalState(t *testing.T)`
- `func TestTaskCompleteRequiresAllAuthoritativePlanNodes(t *testing.T)`
- `func TestNaturalStopWithPendingAuthoritativePlanNeedsUserDecision(t *testing.T)`
- `func TestContextControllerCompactsAndCleansInternalCheckpoint(t *testing.T)`
- `func TestContextControllerRepeatedCompactionDoesNotAccumulateCheckpoints(t *testing.T)`
- `func TestTaskContextRecoveryHistoryKeepsOnlyProductSystemInstruction(t *testing.T)`
- `func TestContextControllerRejectsLargeToolOutputBeforeGlobalCompaction(t *testing.T)`
- `func TestTaskContextSummaryRetainsCompletedToolEvidence(t *testing.T)`
- `func TestTaskContextSummaryIgnoresMetadataOnlyCheckpoint(t *testing.T)`
- `func TestInterruptedTaskContinuationCarriesCheckpointAndSkills(t *testing.T)`
- `func TestTaskContextSummaryStaysWithinProviderToolBudget(t *testing.T)`
- `func TestNoProgressBudgetStopsRepeatedToolRounds(t *testing.T)`

### task_service_test.go

- `func TestTaskCompleteRejectedWhenProjectionNotConverged(t *testing.T)`
- `func TestTaskCompleteFlushConvergesProjectionBeforeVerdict(t *testing.T)`
- `func TestTaskCompleteRejectedWhenProjectionFlushFails(t *testing.T)`
- `func TestTerminalResumeRecordKeepsObjectiveAndQueuedInputs(t *testing.T)`
- `func TestOnChatEndKeepsResumeRecord(t *testing.T)`
- `func TestCheckNodeMarksNodeCompletedInTasklist(t *testing.T)`
- `func TestCheckNodeRejectsUnknownNode(t *testing.T)`
- `func TestCheckNodeRequiresLoadedPlanAndNodeID(t *testing.T)`
- `func TestCheckNodeIdempotentAndDoesNotReplayEpoch(t *testing.T)`
- `func TestTaskCompleteCoversAlreadyCheckedNodes(t *testing.T)` — TestTaskCompleteCoversAlreadyCheckedNodes 验证歧义消除：在途打点已完成的节点
- `func TestTaskCompleteStillRejectsUncheckedNodes(t *testing.T)` — TestTaskCompleteStillRejectsUncheckedNodes 打点流下缺节点仍拒绝：
- `func TestNoProgressBudgetReadsTaskServiceSemanticProgress(t *testing.T)`

### task_skills_for_test.go

- `func TestActiveSkillsProjectionForSession(t *testing.T)`

### teamwork_board_projection_test.go

- `func (r teamBoardRuntime) TeamworkBoardSnapshot(string) *dto.TeamworkBoardView`
- `func teamBoardFixture() *dto.TeamworkBoardView`
- `func TestCollectRuntimeProjectionCarriesTeamworkBoard(t *testing.T)`
- `func TestCollectRuntimeProjectionOmitsTeamworkBoardWithoutPort(t *testing.T)`
- `func TestTeamworkBoardViewForWithoutPort(t *testing.T)`

### teamwork_board_session_switch_test.go

- `func (r *sessionSwitchBoardRuntime) TeamworkBoardSnapshot(sessionID string) *dto.TeamworkBoardView`
- `func TestSessionReactivationRebuildsTeamBoardProjection(t *testing.T)`

### teamwork_completion_trigger_test.go

- `func teamworkCompletionHarness(t *testing.T, trigger bool, engine contract.ChatEngine) (*legacyTestService, *fakeRuntime)` — teamworkCompletionHarness 造一个装配好 teammate 触发路径的 Service。
- `func completedTeammateRecord(handle string, state dto.AsyncState) dto.TeamworkJobCompletionRecord` — completedTeammateRecord 造一条已落到终态的 teammate 作业记录。
- `func TestTeamworkCompletionTriggersIdleSessionTurn(t *testing.T)`
- `func TestTeamworkCompletionTriggersOnFailureToo(t *testing.T)`
- `func TestTeamworkCompletionIgnoresRunningAndKilled(t *testing.T)`
- `func TestTeamworkCompletionDoesNotWakeBusySession(t *testing.T)` — 铁律 §6.1「绝不唤醒忙会话」：忙的时候不起回合，跳过的条目**不记账**——会话回到空闲
- `func TestTeamworkCompletionTriggersOncePerHandle(t *testing.T)` — 幂等：句柄在册期间只触发一次（句柄单调不复用，进程内一个集合就够）。
- `func TestTeamworkCompletionDoesNotCollideWithToolHandles(t *testing.T)` — 两张表的终态链**各触发一次**：幂等账按表（前缀）分开，不靠句柄字面量。
- `func TestTeamworkCompletionStaysOffWhenDisabled(t *testing.T)` — 开关关闭时连扫描都不做（与 subagent 那条同一个开关）。
- `func TestTeamworkTraceLinesCarryCompletionReceipt(t *testing.T)` — 忙会话的读法：**回合边界打点块**里的 teammate 完成行（这是"回执被看见"的另一半——

### tool_hook_diagnostic_test.go

- `func TestToolHookDiagnosticObserverMarksCompletionProjection(t *testing.T)`

### tool_hooks_test.go

- `func TestHandleSubagentToolEventProjectsBoundedIncrementals(t *testing.T)` — TestHandleSubagentToolEventProjectsBoundedIncrementals 钉住子代理工具事件的
- `func TestToolHookBridgeAssignsUniqueStableIDs(t *testing.T)`

### tool_hooks_truncation_test.go

- `func TestToolCompleteTruncatesSnapshotOutput(t *testing.T)`
- `func TestAppendHistoryLockedTruncatesRestoredOutput(t *testing.T)` — TestAppendHistoryLockedTruncatesRestoredOutput 验证会话恢复路径：恢复的

### tool_record_isolation_test.go

- `func toolRecordStat(tool string, success, failure int, reason string) []dto.ToolRecordStat` — toolRecordStat 造一格增量读数（失败笔数挂一个原因）。
- `func subagentReading(tool string, status dto.ToolEventStatus, errorText string) seelsession.SubagentToolEvent` — subagentReading 造一条子代理工具读数（执行单元 = subagent:<nodeID>）。
- `func totals(service *legacyTestService, sessionID string) (int, int, int)` — totals 读某会话的累计三数（不存在 ⇒ 全 0）。
- `func newToolRecordHarness(t *testing.T) *toolRecordHarness`
- `func (harness *toolRecordHarness) switchToB()` — switchToB 走**生产切换路径**（ResumeSession），不是直接改快照字段。
- `func (harness *toolRecordHarness) switchToA()`
- `func TestToolRecordSubagentReadingStaysWithOwnerAcrossViewSwitch(t *testing.T)`
- `func TestToolRecordConcurrentSubagentsStayInOwnSession(t *testing.T)`
- `func TestToolRecordTeammateWithoutMainSessionIsDropped(t *testing.T)`
- `func TestToolRecordCommitIsIdempotentPerSeq(t *testing.T)`
- `func TestToolRecordCommitAccumulatesAcrossSeqs(t *testing.T)`
- `func draftService(t *testing.T) (*legacyTestService, *recordingSnapshotStore)` — draftService 造一个**仍是草稿**的 Service（引擎还没建 bundle：HasSession=false）。
- `func TestToolRecordDraftSessionNeverWrites(t *testing.T)`
- `func TestToolRecordDraftMaterializationWritesOnceWithLeafReadings(t *testing.T)`
- `func TestToolRecordFlushDrainsUnclosedLeafStages(t *testing.T)`
- `func TestToolRecordBoardFollowsViewWithoutCrossing(t *testing.T)`
- `func TestToolRecordReleaseRecyclesStageAndSeq(t *testing.T)`
- `func TestToolRecordDisabledStagesNothing(t *testing.T)`
- `func TestToolRecordThreeFamiliesMergeInOneSession(t *testing.T)`

### tool_record_test.go

- `func (s *recordingSnapshotStore) SaveToolRecordSnapshot(sessionID string, snapshot dto.ToolRecordSnapshot) error`
- `func (s *recordingSnapshotStore) LoadToolRecordSnapshot(string) (dto.ToolRecordSnapshot, bool, error)`
- `func (s *recordingSnapshotStore) savesSnapshot() []dto.ToolRecordSnapshot`
- `func (s *recordingSnapshotStore) loadCount() int`
- `func enableToolRecord(t *testing.T, service *legacyTestService, store *recordingSnapshotStore)` — enableToolRecord 打开开关并把落盘口装上（测试里两条装配动作各走一次）。
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

### turn_status_single_word_test.go

- `func TestTurnStatusSpeaksOneWordOnSnapshotAndArchive(t *testing.T)`
- `func TestTurnStatusOfRecordKeepsLegacyRunningAndNeverFoldsToTerminal(t *testing.T)` — 落盘面读回：合并前的存档里"进行中"写的是 "running"，合并后写 "progressing"。

### view_singleton_test.go

- `func TestViewSingletonMirrorConsistent(t *testing.T)` — TestViewSingletonMirrorConsistent（M4/INV-M4）：V 的唯一镜像
- `func TestDraftMaterializeStreamsMirrorToSnapshot(t *testing.T)` — TestDraftMaterializeStreamsMirrorToSnapshot：草稿物化后流式增量走活跃路径，

### view_switch_isolation_test.go

- `func TestLongRunningSessionSurvivesViewSwitch(t *testing.T)`
- `func readSessionView(t *testing.T, service *legacyTestService, sessionID string) []model.Message`
- `func viewText(messages []model.Message) string`
- `func joinHistoryText(history []contract.EngineMessage) string`
- `func joinTranscript(t *testing.T, service *legacyTestService, sessionID string) string`

### visible_output_test.go

- `func TestAppendDeltaDoesNotExposeThoughtContent(t *testing.T)`

### visible_role_attribution_test.go

- `func TestVisibleConversationCarriesRoleAttribution(t *testing.T)` — TestVisibleConversationCarriesRoleAttribution 复现实时聊天载荷缺归属：
- `func visibleMessage(t *testing.T, messages []model.Message, match func(model.Message) bool) model.Message` — visibleMessage 取第一条满足条件的可见消息（缺失即用例失败）。

### work_table_async_test.go

- `func runningAsyncRecord(handle string) dto.AsyncRunRecord`
- `func workItemByID(rows []model.WorkItem, id string) (model.WorkItem, bool)`
- `func TestAsyncRunProjectsEveryVisibleColumn(t *testing.T)` — 列位分配：描述→行标题、指令→描述列、探针读数→打点行、日志路径→附件列。
- `func TestAsyncTerminalStatesMapToWorkStatus(t *testing.T)` — 终态映射：done→completed，failed/killed→failed（表格状态机没有"被杀"这一档）。
- `func TestAsyncTraceLinesCarryNoPathsOrLogContent(t *testing.T)` — 打点块列**在途行 + 待取回的完成行**（打点 K-5 的回填规范），且进上下文的字段必须
- `func TestAsyncRunAloneMaterializesTraceBlock(t *testing.T)` — 整块语义：没有活动任务、只有一条在跑的后台命令时，打点块必须出现（这是
- `func TestAsyncRowDisappearsWhenRegistryDropsIt(t *testing.T)` — 登记表是唯一事实源：它不再报这条记录（终态被驱逐），投影里就没有这行。
- `func TestAsyncChangeSignalReprojectsWorkTable(t *testing.T)` — 第 4 个生命周期消费者：执行域一发声，表格就重投影——不靠模型再调一次工具，
- `func TestAsyncRowsAreCappedAndNeverEvictTasks(t *testing.T)` — 后台行按上限封顶，且不挤掉真实任务行（在途优先，终态按最新补齐）。
- `func TestAsyncBackfillStaysBounded(t *testing.T)` — TC-K5-1（打点 K-5 的"有界"判据）：注入 100 个已完成作业 →
- `func serviceWorkTableRows(service *Service) []model.WorkItem`

### work_table_plan_deps_test.go

- `func TestWorkTablePlanRowsCarryDAGDependencies(t *testing.T)` — TestWorkTablePlanRowsCarryDAGDependencies 投影面：平铺节点 + 边集 → 行依赖。
- `func TestServicePlanSyncExposesDAGDependencies(t *testing.T)` — TestServicePlanSyncExposesDAGDependencies 走 Service 路径：plan 投影 → task
- `func TestTaskChangedIncrementCarriesPlanDependencies(t *testing.T)` — TestTaskChangedIncrementCarriesPlanDependencies 增量面：task.changed 单行

### work_table_project_scope_test.go

- `func newProjectSwitchHarness(t *testing.T) *projectSwitchHarness`
- `func (harness *projectSwitchHarness) enterProjectA(t *testing.T)` — enterProjectA 在项目 A 里跑一轮（会话 A 成为已加载会话，引擎带历史），并把一条
- `func (harness *projectSwitchHarness) switchToProjectB(t *testing.T)` — switchToProjectB 触发项目切换（startFreshSession：新建独立会话）。
- `func appendProjectRoundsFor(t *testing.T, service *legacyTestService, sessionID string, rounds, chars int)` — appendProjectRoundsFor 给**指定会话**追加 rounds 个已定稿轮次（每轮约
- `func TestProjectSwitchDoesNotInjectForeignWorkTableRows(t *testing.T)` — TestProjectSwitchDoesNotInjectForeignWorkTableRows —— 时机①：切换项目后
- `func TestProjectSwitchWorkTableBlockStaysCleanAcrossCompaction(t *testing.T)` — TestProjectSwitchWorkTableBlockStaysCleanAcrossCompaction —— 时机②：切换项目
- `func TestProjectSwitchRebindsTaskRegistryToNewSession(t *testing.T)` — TestProjectSwitchRebindsTaskRegistryToNewSession —— 同一根因的数据面：项目切换
- `func TestProjectSwitchKeepsForeignRowsInLedger(t *testing.T)` — TestProjectSwitchKeepsForeignRowsInLedger —— 边界：工作表格是**项目/全局台账**，
- `func TestProjectSwitchWithoutFreshSessionKeepsOwnRows(t *testing.T)` — TestProjectSwitchWithoutFreshSessionKeepsOwnRows —— 对照：**没有**新建独立会话

### work_table_race_test.go

- `func TestWorkTableRaceConcurrentMutations(t *testing.T)`

### work_table_session_axis_test.go

- `func TestWorkItemCarriesOwningSession(t *testing.T)`
- `func TestGlobalWorkTableCarriesOwningSession(t *testing.T)` — 全局台账行带归属会话：实时注册表 = 当前会话，后台 scope 分区 = 分区键。
- `func TestMergeTaskRecordsStampsSecondarySession(t *testing.T)` — 冷读合并：磁盘记录不带会话标记（归属由 SessionRecord 容器表达），合并时按
- `func TestTaskChangedIncrementCarriesOwningSession(t *testing.T)` — 增量面：task.changed 单行必须带归属会话（与整表路径 taskSnapshotAll 同源

### work_table_session_scope_test.go

- `func TestS1BackgroundSessionContextMustNotCarryActiveSessionWorkTable(t *testing.T)`
- `func TestWorkTableTraceBlockForScopesBySession(t *testing.T)`

### work_table_subagent_session_scope_test.go

- `func subagentSessionScopeFixture(t *testing.T) (*fakeRuntime, *Service)` — subagentSessionScopeFixture 造一个视图会话在 session-a 的服务，并把"进程级
- `func TestSubagentRowsStayOutOfActiveRegistryOfOtherSessions(t *testing.T)` — 视图路径：活跃会话同步只收自己的子代理行，别的会话的行不得进实时注册表。
- `func TestBackgroundSyncKeepsForeignSubagentRowsOut(t *testing.T)` — 后台路径：为 session-a 后台同步时，树里**别的会话**的子代理不得写进 A 的分区。
- `func TestSubagentWorkItemCarriesOwningSession(t *testing.T)` — 归位后的行必须带上归属会话键（前端会话轴的唯一数据源）；缺键的行会被前端
- `func hasTaskKey(records []dto.TaskRecord, key string) bool`

### work_table_test.go

- `func TestBuildWorkTableMapsPlanNodes(t *testing.T)`
- `func TestBuildWorkTableTasklistModeMarksCheckNode(t *testing.T)`
- `func TestBuildWorkTableMapsTodoItems(t *testing.T)`
- `func TestBuildWorkTableBatches(t *testing.T)` — TestBuildWorkTableBatches 验证批次分片：按 BatchID 分组、按 CreatedAt
- `func TestTaskRecordToWorkItemCarriesBatch(t *testing.T)` — TestTaskRecordToWorkItemCarriesBatch 验证批次字段透传（task.changed 单行
- `func TestBuildWorkTableMapsSubagentTasks(t *testing.T)`
- `func TestBuildWorkTableBoundsRowsAndTruncatesEvidence(t *testing.T)`
- `func TestUpdateWorkItemStatusTodoThreeStates(t *testing.T)` — TestUpdateWorkItemStatusTodoThreeStates 走完整 Service 路径：
- `func TestUpdateWorkItemStatusRejectsInvalid(t *testing.T)`
- `func TestRefreshWorkTableSnapshotPublishesSubagentRows(t *testing.T)` — TestRefreshWorkTableSnapshotPublishesSubagentRows 验证被动触发：
- `func waitForWorkTableEvent(t testing.TB, subscription protocol_event.Subscription) model.WorkTableEvent`
- `func TestWorkTableEventCarriesSubagentTreeOnChange(t *testing.T)` — TestWorkTableEventCarriesSubagentTreeOnChange 验证详情入口的数据面：子代理

### workspace_file_usecase_test.go

- `func (fake *fileFakeWorkspace) ReadFile(root, relPath string, limit int64) (dto.FileContent, error)`
- `func (fake *fileFakeWorkspace) GitCommitFileContent(root, hash, relPath string, limit int64) (dto.FileContent, error)`
- `func (fake *fileFakeWorkspace) WriteFile(root, relPath string, content []byte) (dto.FileWriteResult, error)`
- `func (fake *readOnlyFileFakeWorkspace) ReadFile(root, relPath string, limit int64) (dto.FileContent, error)`
- `func (fake *readOnlyFileFakeWorkspace) GitCommitFileContent(root, hash, relPath string, limit int64) (dto.FileContent, error)`
- `func TestWorkspaceFileContentForwardsCurrentWorkspaceRoot(t *testing.T)`
- `func TestWorkspaceGitCommitFileContentForwardsHashPathAndLimit(t *testing.T)`
- `func TestWorkspaceGitCommitFileContentRejectsWithoutBoundWorkspace(t *testing.T)`
- `func TestWorkspaceGitCommitFileContentFallsBackWhenBackendLacksFilePort(t *testing.T)`
- `func TestWorkspaceFileContentRejectsWithoutBoundWorkspace(t *testing.T)`
- `func TestWorkspaceFileContentFallsBackWhenBackendLacksFilePort(t *testing.T)`
- `func TestWorkspaceFileContentUseCaseReadsRealFilesystem(t *testing.T)` — TestWorkspaceFileContentUseCaseReadsRealFilesystem 走真实 workspace.Repo
- `func TestWorkspaceWriteFileForwardsCurrentWorkspaceRoot(t *testing.T)` — TestWorkspaceWriteFileForwardsCurrentWorkspaceRoot：编辑保存走的是与预览读取
- `func TestWorkspaceWriteFileRejectsWithoutBoundWorkspace(t *testing.T)`
- `func TestWorkspaceWriteFileRejectsReadOnlyBackend(t *testing.T)`
- `func TestWorkspaceWriteFileUseCaseWritesRealFilesystem(t *testing.T)` — TestWorkspaceWriteFileUseCaseWritesRealFilesystem：真实 workspace.Repo 上的

### workspace_gitrepos_usecase_test.go

- `func (fake *gitReposFakeWorkspace) GitRepos(root string, limit int) (dto.GitReposResult, error)`
- `func makeDir(t *testing.T, root, rel string) string` — makeDir 在工作区根下造目录（仓库路径解析要求目录真实存在）。
- `func TestWorkspaceGitQueriesForwardResolvedRepoRoot(t *testing.T)` — TestWorkspaceGitQueriesForwardResolvedRepoRoot 钉住四条查询：repo 先解析成
- `func TestWorkspaceGitCommitFileContentForwardsResolvedRepoRoot(t *testing.T)` — TestWorkspaceGitCommitFileContentForwardsResolvedRepoRoot 钉住提交内文件内容
- `func TestWorkspaceGitReposForwardsWorkspaceRoot(t *testing.T)` — TestWorkspaceGitReposForwardsWorkspaceRoot 钉住仓库发现：root 只来自后端
- `func TestWorkspaceGitReposDegradesWithoutPort(t *testing.T)` — TestWorkspaceGitReposDegradesWithoutPort 钉住两种"不可用"：后端没有仓库发现

### workspace_tree_usecase_test.go

- `func (fake *treeFakeWorkspace) ListTree(root, relPath string, depth int) (dto.TreeListing, error)`
- `func (fake *treeFakeWorkspace) CountFiles(root string) (dto.TreeCount, error)`
- `func (fake *treeFakeWorkspace) GitLog(root string, limit int) (dto.GitLogResult, error)`
- `func (fake *treeFakeWorkspace) GitChanges(root string, limit int) (dto.WorkspaceChangesResult, error)`
- `func (fake *treeFakeWorkspace) GitCommitDetail(root, hash string, limit int) (dto.GitCommitDetail, error)`
- `func TestWorkspaceTreeForwardsCurrentWorkspaceRoot(t *testing.T)`
- `func TestWorkspaceTreeRejectsWithoutBoundWorkspace(t *testing.T)`
- `func TestWorkspaceGitLogForwardsCurrentWorkspaceRoot(t *testing.T)`
- `func TestWorkspaceGitCommitDetailForwardsCurrentWorkspaceRoot(t *testing.T)`
- `func TestWorkspaceTreeFallsBackWhenBackendLacksTreePort(t *testing.T)`
- `func TestWorkspaceTreeUseCaseReadsRealFilesystem(t *testing.T)` — TestWorkspaceTreeUseCaseReadsRealFilesystem 走真实 workspace.Repo（实现
- `func TestWorkspaceChangesForwardsCurrentWorkspaceRoot(t *testing.T)`
- `func TestWorkspaceChangesUseCaseReadsRealGitRepo(t *testing.T)` — TestWorkspaceChangesUseCaseReadsRealGitRepo 走真实 workspace.Repo（实现

### workspace_use_cases_test.go

- `func workspaceUseCase(service *assembly.Service) *switchworkspace.UseCase`
- `func workspaceApplication(service *assembly.Service) *applicationworkspace.Application`
