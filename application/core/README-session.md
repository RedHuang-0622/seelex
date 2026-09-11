# core/session（根包分卷）

## 生态位

会话草稿/恢复/存储用例与集成测试；运行中切到未驻留会话走异步冷加载（restoring 空壳 + 后台装载 + epoch 判定发布基线）

覆盖：`session*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 历史分页契约（2026-09-11）

`LoadMoreHistory` / `LoadLatestHistory` 是 GUI 顶部 sentinel 与历史栏的应用
边界，语义收口如下：

1. **一页 = 一整窗**（`limits.history_window`，入参只当上限建议）：半页会让
   「窗口」与「页」两个尺寸混在一起，用户翻一次只多出半屏又丢掉半屏。
2. **分页态写进会话可见投影**（事实源）后再镜像 Snapshot：`Snapshot` 是活跃
   会话的只读镜像，只写镜像会在下一次镜像（新消息/工具事件/切换）被整体抹掉，
   offset 退回尾部——表现为「点了加载更早，内容回卷，再点还是同一页」。
3. 窗口 = `[HistoryOffset, HistoryOffset + 窗口条数)` 的连续区间：窗口整体后退
   一页，可见列表不会无限加长（WebView 渲染内存有硬上限）。
4. 回看期间（窗口未贴尾）新消息不回卷窗口、也不进可见列表；`LoadLatestHistory`
   重新读尾部窗口贴尾，并带回回看期间的新消息。
5. 无 record 的旧格式会话冷加载同样写入 `TotalMessages/HistoryOffset`（历史
   总数来自 provider 历史），否则 `HasMoreHistory` 恒为 false，早期历史读不到。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### approval_session_ownership_test.go

- `func waitApprovalCount(t *testing.T, service *Service, sessionID string, want int)` — waitApprovalCount 轮询指定会话单元的待批数直到到达目标（approval 观察
- `func TestApprovalSessionOwnershipStatusAndSnapshot(t *testing.T)` — TestApprovalSessionOwnershipStatusAndSnapshot 波 4 approval 会话级归属：
- `func TestBackgroundApprovalDoesNotClobberViewSlotAndResolvesById(t *testing.T)` — TestBackgroundApprovalDoesNotClobberViewSlotAndResolvesById 波 4 归属：
- `func TestApprovalConcurrentSessionsStayAttributed(t *testing.T)` — TestApprovalConcurrentSessionsStayAttributed -race 靶场：两会话并发开
- `func startsWithApprovalID(id, prefix string) bool`

### fork_gate_test.go

- `func (runtime *forkGateRuntime) ForkInFlight(string) bool`
- `func TestSubmitRejectedWhileForkRunning(t *testing.T)` — TestSubmitRejectedWhileForkRunning 钉住“fork 运行时禁止同会话继续对话”

### hot_attach_running_test.go

- `func newFrameworkLockEngine() *frameworkLockEngine`
- `func (engine *frameworkLockEngine) lockFor(sessionID string) *sync.Mutex`
- `func (engine *frameworkLockEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 持锁到 release（模拟 framework Session.ChatStream 全程持锁）。
- `func (engine *frameworkLockEngine) SetSystemPromptFor(sessionID, prompt string)` — SetSystemPromptFor 需要会话锁：运行中会话调用会阻塞（生产死锁源）。
- `func (engine *frameworkLockEngine) promptCallCount(sessionID string) int`
- `func TestHotAttachRunningSessionDoesNotBlockOnEngineLock(t *testing.T)` — TestHotAttachRunningSessionDoesNotBlockOnEngineLock（生产死锁回归）：

### interrupted_continue_test.go

- `func TestColdResumeContinueAfterInterruptedToolChain(t *testing.T)` — TestColdResumeContinueAfterInterruptedToolChain：重启后 continue 场景端到端

### message_seq_scope_test.go

- `func TestVisibleMessageSeqIsScopedPerSession(t *testing.T)` — TestVisibleMessageSeqIsScopedPerSession 钉住可见消息派号的会话作用域：

### session_archive_test.go

- `func (sessions *archiveSessions) SaveCurrent(string) error`
- `func (sessions *archiveSessions) LoadHistory(string) ([]EngineMessage, error)`
- `func (sessions *archiveSessions) SaveSessionRecord(_ string, record SessionRecord) error`
- `func (sessions *archiveSessions) SaveSessionRecordWorkspace(_ string, _ string, record SessionRecord) error`
- `func (sessions *archiveSessions) LoadSessionRecord(string) (SessionRecord, error)`
- `func (sessions *archiveSessions) LoadSessionRecordWorkspace(string, string) (SessionRecord, error)`
- `func (sessions *archiveSessions) LoadTranscriptTailWorkspace(_, _ string, tokenBudget, maxUnits int) ([]TranscriptEvent, error)`
- `func (sessions *archiveSessions) LoadToolResultWorkspace(string, string, string) (StoredToolResult, error)`
- `func (sessions *contextAwareSessions) AttachSessionContext(workspaceID, sessionID string) error`
- `func (sessions *contextAwareSessions) DetachSessionContext()`
- `func TestResumeSessionAttachesSessionContext(t *testing.T)` — TestResumeSessionAttachesSessionContext 验证恢复会话后 context 模块被
- `func TestResumeSessionFailsWhenContextCorrupt(t *testing.T)` — TestResumeSessionFailsWhenContextCorrupt 验证损坏/不可用的 context 模块
- `func TestBeginNewSessionDetachesSessionContext(t *testing.T)` — TestBeginNewSessionDetachesSessionContext 验证进入 draft/新建会话时解绑
- `func TestSessionArchivePreservesVisibleHistoryPlanAndReadCache(t *testing.T)`
- `func TestResumeSessionDropsMetadataOnlyCheckpointAndUsesDurableConversation(t *testing.T)`
- `func TestResumeLongContextReasksOpeningQuestionFromCheckpoint(t *testing.T)`
- `func historyContainsAssistant(history []EngineMessage, content string) bool`
- `func TestBoundConversationTailKeepsOnlyConfiguredVariableHeightWindow(t *testing.T)`
- `func TestPersistSessionRecordRebuildsConversationFromTranscript(t *testing.T)`
- `func TestSessionRecordStoresLargeContentByReference(t *testing.T)`
- `func TestCompletedTaskClearsTaskScopedSkillsBeforeNextRequest(t *testing.T)`
- `func TestToolResultPaginationMakesProgressAcrossUTF8Boundaries(t *testing.T)`
- `func TestLoadedPlanIsAppendedToSessionPlanStack(t *testing.T)`
- `func TestResumeSessionUsesRecordWhenProviderHistoryIsUnavailable(t *testing.T)`
- `func TestResumeSessionContinuationKeepsTranscriptHistory(t *testing.T)`
- `func TestResumeSessionContinuationKeepsToolStepsWithoutEmptyAssistantEvents(t *testing.T)` — TestResumeSessionContinuationKeepsToolStepsWithoutEmptyAssistantEvents 是
- `func TestResumeSessionContinuationKeepsTrailingUnansweredUserInput(t *testing.T)`
- `func TestProviderRepairNoteNeverBecomesVisibleAssistantText(t *testing.T)`

### session_binding_regression_test.go

- `func TestBindWorkspaceSetsProjectScope(t *testing.T)` — TestBindWorkspaceSetsProjectScope 回归：seelebridge 的 tool/worktree 项目

### session_catalog.go

- `func (service *Service) WaitCatalogRefresh(ctx context.Context) error` — WaitCatalogRefresh 等待会话目录 worker 完成一轮"覆盖了本次请求"的刷新，使

### session_catalog_grid_test.go

- `func TestCatalogRefreshScopedByProjectKeepsOtherProjectGrids(t *testing.T)` — TestCatalogRefreshScopedByProjectKeepsOtherProjectGrids 钉住 G6 目录按
- `func TestCatalogFullRefreshReplacesProjectGrid(t *testing.T)` — TestCatalogFullRefreshReplacesProjectGrid 钉住全量刷新按项目整格替换：
- `func sessionIDsOf(sessions []SessionInfo) []string`
- `func containsSessionID(ids []string, target string) bool`

### session_catalog_test.go

- `func addCatalogSession(sessions *scopedSessions, projectID, sessionID string, updatedAt time.Time)` — addCatalogSession 在会话端口的项目索引里追加一个会话（目录刷新会读到它）。
- `func newCatalogTestService(t *testing.T, sessions *scopedSessions) *Service`
- `func TestWaitCatalogRefreshSettlesFreshCatalog(t *testing.T)` — TestWaitCatalogRefreshSettlesFreshCatalog 是 C3 回执的核心契约：一轮
- `func TestWaitCatalogRefreshServesCoalescedRequests(t *testing.T)` — TestWaitCatalogRefreshServesCoalescedRequests 覆盖唤醒槽位被丢弃时的回执
- `func TestWaitCatalogRefreshConvergesAfterShutdown(t *testing.T)` — TestWaitCatalogRefreshConvergesAfterShutdown 钉住关闭路径：worker 退出时
- `func TestCatalogCacheMirrorsWorkerRound(t *testing.T)` — TestCatalogCacheMirrorsWorkerRound G5 CatalogMu：目录 worker 的枚举结果先落
- `func TestCatalogCacheObservesProjectDiscoveredBindings(t *testing.T)` — TestCatalogCacheObservesProjectDiscoveredBindings 钉住缓存里的 discovered
- `func TestCatalogRefreshConcurrentWithTitleWritesAndSnapshotReads(t *testing.T)` — TestCatalogRefreshConcurrentWithTitleWritesAndSnapshotReads 钉住 G5 锁拆分

### session_cold_read.go

- `func (service *Service) snapshotOfCold(sessionID string) (SessionSnapshot, error)` — snapshotOfCold 组装未驻留会话的只读会话快照（record 事实源）。目录已归档
- `func (service *Service) GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]TranscriptEvent, error)` — GetSessionTranscript 读取指定会话的事件库区间（fromSeq..toSeq 含端点；

### session_cold_read_test.go

- `func newColdReadSessions() *coldReadSessions`
- `func (sessions *coldReadSessions) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error)`
- `func (sessions *coldReadSessions) setEvents(projectID, sessionID string, events []sessionstore.Event)`
- `func newColdReadService(t *testing.T, sessions *coldReadSessions) *Service`
- `func TestSnapshotOfColdSessionAssemblesReadOnlyBaseline(t *testing.T)` — TestSnapshotOfColdSessionAssemblesReadOnlyBaseline C1：未驻留（无 unit）
- `func TestSnapshotOfColdUnknownSessionStillUnavailable(t *testing.T)` — TestSnapshotOfColdUnknownSessionStillUnavailable 钉住未知会话的失败语义
- `func TestGetSessionTranscriptRange(t *testing.T)` — TestGetSessionTranscriptRange 钉住冷读区间语义：含端点、倒置显式报错、
- `func TestListSessionsReturnsAuthoritativeDirectory(t *testing.T)` — TestListSessionsReturnsAuthoritativeDirectory C1：目录枚举不要求视图快照

### session_concurrent_content_isolation_test.go

- `func TestConcurrentSessionsKeepOwnContent(t *testing.T)`
- `func concurrentTranscriptText(events []TranscriptEvent) string` — concurrentTranscriptText 把 transcript 拼成可搜索文本（角色 + 正文 + 调用 ID）。
- `func concurrentViewText(service *Service, sessionID string) string` — concurrentViewText 返回指定会话可见视图的全部消息文本（用户 + assistant +

### session_ctx.go

- `func withSessionID(ctx context.Context, sessionID string) context.Context` — withSessionID 把会话 ID 注入 ctx（runChat 执行路径）。Seele ReActLoop 会
- `func sessionIDFromContext(ctx context.Context) string` — sessionIDFromContext 返回 ctx 中的会话 ID；未注入时返回 ""。

### session_decoupling_test.go

- `func TestCoreHoldsNoSessionContainerFields(t *testing.T)` — TestCoreHoldsNoSessionContainerFields（T2.4 编译断言）：core 门面不再
- `func assertNoSessionContainerField(t *testing.T, typ reflect.Type)`
- `func isSessionContainerType(typ reflect.Type) bool`
- `func TestEventFingerprintStable(t *testing.T)` — TestEventFingerprintStable（P5 事件指纹回归）：相同输入序列驱动两次
- `func runFingerprintScenario(t *testing.T) []string` — runFingerprintScenario 装配一次性服务并驱动单次对话，返回归一化事件
- `func messageIDFromPayload(raw json.RawMessage) string`
- `func TestBackgroundDeltaDoesNotBlockOnGlobalLock(t *testing.T)`
- `func containsText(value, sub string) bool`

### session_draft.go

- `func (service *Service) newDraftSessionIDLocked() string` — newDraftSessionIDLocked 生成早分配的草稿会话 ID（调用方持有 Core.ViewMu）。
- `func (service *Service) BeginNewSession() error` — BeginNewSession 进入幂等的草稿状态：早分配真实会话 ID 并建 SessionUnit
- `func (service *Service) materializeDraftSession(firstQuestion string) error` — materializeDraftSession 为首条请求创建引擎会话与项目绑定：复用早分配

### session_effort_test.go

- `func TestEffortOwnershipPerSession(t *testing.T)` — TestEffortOwnershipPerSession（G4）：effort 选择归属进 SessionUnit——
- `func TestPlanPolicySlotSyncPerSession(t *testing.T)` — TestPlanPolicySlotSyncPerSession（G1-C）：chat 起点按会话 effort 把 plan

### session_fork.go

- `func (service *Service) ForkSession(parentID string, request model.ForkRequest) (string, error)` — ForkSession 从父会话的指定切断点创建独立子会话，并切换到子会话继续。
- `func (service *Service) ForkSessionLatest(parentID string) (string, error)` — ForkSessionLatest 从父会话最新完整段落边界创建独立子会话并切换到子会话
- `func (service *Service) forkSessionLocked(parentID string, request model.ForkRequest) (string, error)` — forkSessionLocked 在持有会话切换锁时执行 fork 落盘：解析切断点 → 构建
- `func maxTranscriptEventSeq(events []model.TranscriptEvent) uint64` — maxTranscriptEventSeq 返回事件流中的最大 Seq（空流返回 0）。
- `func deepCopyForkRecord(record model.SessionRecord) model.SessionRecord` — deepCopyForkRecord 深拷贝 fork 子会话 record：Conversation 消息（含

### session_fork_store_regression_test.go

- `func newRouterForkSessions(t *testing.T) *routerForkSessions`
- `func (s *routerForkSessions) saveParentFixture(t *testing.T, record model.SessionRecord, events []sessionstore.Event, contextPayload []byte)`
- `func (s *routerForkSessions) SaveCurrent(string) error`
- `func (s *routerForkSessions) SetWorkspace(projectID string)`
- `func (s *routerForkSessions) Workspace() string`
- `func (s *routerForkSessions) List() []model.SessionInfo`
- `func (s *routerForkSessions) LoadHistory(string) ([]EngineMessage, error)`
- `func (s *routerForkSessions) LoadHistoryRange(string, int, int) ([]EngineMessage, int, error)`
- `func (s *routerForkSessions) Delete(sessionID string) error`
- `func (s *routerForkSessions) SessionsOf(projectID string) []model.SessionInfo`
- `func (s *routerForkSessions) SaveSessionRecord(sessionID string, record model.SessionRecord) error`
- `func (s *routerForkSessions) SaveSessionRecordWorkspace(projectID, sessionID string, record model.SessionRecord) error`
- `func (s *routerForkSessions) LoadSessionRecord(sessionID string) (model.SessionRecord, error)`
- `func (s *routerForkSessions) LoadSessionRecordWorkspace(projectID, sessionID string) (model.SessionRecord, error)`
- `func (s *routerForkSessions) SaveSessionSnapshot( sessionID string, history []contract.EngineMessage, record model.SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult, ) error`
- `func (s *routerForkSessions) SaveSessionSnapshotWorkspace( projectID, sessionID string, _ []contract.EngineMessage, record model.SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult, ) error`
- `func (s *routerForkSessions) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error)`
- `func (s *routerForkSessions) LoadToolResultsWorkspace(projectID, sessionID string) ([]sessionstore.ToolResult, error)`
- `func (s *routerForkSessions) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error)`
- `func (s *routerForkSessions) SaveContextStateWorkspace(projectID, sessionID string, payload []byte) error`
- `func (s *routerForkSessions) CurrentGenerationWorkspace(projectID, sessionID string) (string, error)`
- `func (s *routerForkSessions) LoadTranscriptTailWorkspace(projectID, sessionID string, _, _ int) ([]model.TranscriptEvent, error)`
- `func (s *routerForkSessions) LoadToolResultWorkspace(projectID, sessionID, ref string) (model.StoredToolResult, error)`
- `func transcriptEventsToStore(events []model.TranscriptEvent) []sessionstore.Event`
- `func storeEventsToTranscript(events []sessionstore.Event) []model.TranscriptEvent`
- `func storedToolResultsToStore(results []model.StoredToolResult) []sessionstore.ToolResult`
- `func TestForkChildVisibleWithRealStore(t *testing.T)`

### session_fork_test.go

- `func (s *forkServiceSessions) LoadSessionRecord(string) (SessionRecord, error)`
- `func (s *forkServiceSessions) LoadSessionRecordWorkspace(_, sessionID string) (SessionRecord, error)`
- `func (s *forkServiceSessions) SaveSessionRecord(string, SessionRecord) error`
- `func (s *forkServiceSessions) SaveSessionRecordWorkspace(string, string, SessionRecord) error`
- `func (s *forkServiceSessions) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error)`
- `func (s *forkServiceSessions) LoadToolResultsWorkspace(projectID, sessionID string) ([]sessionstore.ToolResult, error)`
- `func (s *forkServiceSessions) SaveSessionSnapshotWorkspace(projectID, sessionID string, history []EngineMessage, record SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult) error`
- `func (s *forkServiceSessions) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error)`
- `func (s *forkServiceSessions) SaveContextStateWorkspace(projectID, sessionID string, payload []byte) error`
- `func (s *forkServiceSessions) CurrentGenerationWorkspace(projectID, sessionID string) (string, error)`
- `func TestForkSessionCreatesAndSwitchesToChild(t *testing.T)`
- `func TestForkSessionChildContentVisibleAfterResume(t *testing.T)` — TestForkSessionChildContentVisibleAfterResume 钉住 fork 回归：ForkSession
- `func lastMessageOf(messages []Message) *Message`
- `func TestForkSessionLatestResolvesNewestParagraph(t *testing.T)`
- `func TestForkCommandForksCurrentSession(t *testing.T)`
- `func TestForkSessionDeepCopyIsolation(t *testing.T)` — TestForkSessionDeepCopyIsolation（T2.7）：fork 子会话 record 与父数据面
- `func TestForkSessionRejectsRunningParent(t *testing.T)` — TestForkSessionRejectsRunningParent（UC5）：父会话运行中拒绝 fork。

### session_fullaccess_test.go

- `func TestFullAccessOwnershipPerSession(t *testing.T)` — TestFullAccessOwnershipPerSession（G4）：fullAccess 选择归属进 SessionUnit
- `func TestFullAccessProjectionPerSession(t *testing.T)` — TestFullAccessProjectionPerSession（G4）：运行时投影按会话读取生效的

### session_g5_lock_test.go

- `func (e *streamEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 覆盖内嵌引擎：先发流式块，再委托底层阻塞执行。
- `func TestConcurrentStreamingViewSwitchNoPollution(t *testing.T)` — TestConcurrentStreamingViewSwitchNoPollution 钉住两个会话在流式输出与热

### session_history.go

- `func (service *Service) resumeSession(sessionID string) error` — resumeSession 是会话切换的应用边界：目标已驻留（含运行中）热加载；目标
- `func (service *Service) rollbackSyncResumeFailure(sessionID, previousID string)` — rollbackSyncResumeFailure 在同步冷加载失败后恢复视图一致性（调用方持视图
- `func (service *Service) bumpViewEpoch()` — bumpViewEpoch 推进视图切换序号（任何新的视图激活都推进；后台冷加载完成
- `func (service *Service) beginAsyncRestore(sessionID string) (uint64, error)` — beginAsyncRestore 激活目标会话的 restoring 空壳：视图指针立即切到目标，
- `func (service *Service) resumeSessionColdInBackground(sessionID, previousID string, epoch uint64)` — resumeSessionColdInBackground 后台执行冷加载：完成/失败后按 epoch 判定
- `func (service *Service) handleColdRestoreFailure(sessionID, previousID string, epoch uint64, cause error)` — handleColdRestoreFailure 后台冷加载失败的降级：用户若仍停留在失败的恢复
- `func (service *Service) resetViewToDraftAfterRestoreFailure()` — resetViewToDraftAfterRestoreFailure 在“无前一会话可回退”时把视图重置到
- `func (service *Service) resumeSessionCold(sessionID string, activateEpoch uint64) error` — resumeSessionCold 是 resumeSession 的冷加载主体：目标未驻留时重建引擎、
- `func (service *Service) ResumeSession(sessionID string) error` — ResumeSession 是 GUI/TUI 会话选择的直接应用边界。它刻意绕过命令文本解析，
- `func (service *Service) LoadMoreHistory(limit int) error` — LoadMoreHistory 把更早的一页历史前置到可见会话（GUI 顶部 sentinel 与
- `func (service *Service) LoadLatestHistory() error` — LoadLatestHistory 把可见会话拉回最新一页（历史浏览后的「回到最新」）。
- `func currentWorkspaceIDLocked(service *Service) string` — currentWorkspaceIDLocked 返回当前视图会话的 workspace ID（调用方持有
- `func (service *Service) loadConversationPage(workspaceID, sessionID string, offset, limit int) ([]Message, int, error)` — loadConversationPage 读回一段可见历史：record conversation 模块优先
- `func (service *Service) installVisibleHistory(sessionID string, page []Message, total, offset, window int, mode historyPageInstall) error` — installVisibleHistory 安装一页可见历史：写会话可见投影（事实源）→ 收敛
- `func adaptEngineMessage(msg EngineMessage) Message`
- `func isVisibleHistoryMessage(message EngineMessage) bool`

### session_history_pagination_test.go

- `func withHistoryWindow(window int) func()` — withHistoryWindow 临时把进程级 history_window 调小，让分页行为在少量消息
- `func newPagedSessionStore(sessionID string, count int) *pagedSessionStore`
- `func (store *pagedSessionStore) countLocked() int`
- `func (store *pagedSessionStore) SessionsOf(projectID string) []SessionInfo`
- `func (store *pagedSessionStore) LoadHistory(sessionID string) ([]EngineMessage, error)`
- `func (store *pagedSessionStore) LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error)`
- `func (store *pagedSessionStore) LoadSessionRecordWorkspace(workspaceID, sessionID string) (SessionRecord, error)`
- `func (store *pagedSessionStore) SaveSessionRecord(sessionID string, record SessionRecord) error` — 下面三个方法补齐 SessionRecordPort（生产 internal/adapters.SessionPort 同时
- `func (store *pagedSessionStore) SaveSessionRecordWorkspace(workspaceID, sessionID string, record SessionRecord) error`
- `func (store *pagedSessionStore) LoadSessionRecord(sessionID string) (SessionRecord, error)`
- `func (store *pagedSessionStore) LoadConversationRangeWorkspace(workspaceID, sessionID string, offset, limit int) ([]Message, int, error)` — LoadConversationRangeWorkspace 是生产分页读回面（SessionConversationRangePort）：
- `func (store *pagedSessionStore) appendDurable(role, content string)`
- `func (store *pagedSessionStore) rangeReads() int`
- `func sliceWindow[T any](items *[]T, offset, limit int) []T`
- `func visibleContents(snapshot Snapshot) []string` — visibleContents 取快照对话里的非 system 消息内容（system 是「已恢复会话」
- `func wantRange(start, count int) []string`
- `func assertRange(t *testing.T, label string, snapshot Snapshot, start, count int)`
- `func TestLegacySessionWithoutRecordExposesEarlierHistory(t *testing.T)` — TestLegacySessionWithoutRecordExposesEarlierHistory 旧格式会话（端口只提供
- `func openLongSession(t *testing.T, window, total int) (*Service, *pagedSessionStore)` — openLongSession 打开一个长会话（window 条尾窗 + offset），返回服务与存储。
- `func mirrorOnce(t *testing.T, service *Service, store *pagedSessionStore)` — mirrorOnce 触发一次会话可见投影镜像（真实链路上任何新消息/工具事件都会
- `func TestLoadMoreHistoryPrependsPageAndSurvivesMirror(t *testing.T)` — TestLoadMoreHistoryPrependsPageAndSurvivesMirror 红灯 1：
- `func TestLoadMoreHistoryPagesToBeginning(t *testing.T)` — TestLoadMoreHistoryPagesToBeginning 红灯 2：连续翻页必须单调向更早推进，
- `func TestLoadLatestHistoryReturnsToTail(t *testing.T)` — TestLoadLatestHistoryReturnsToTail 红灯 3：翻到更早以后必须能一键回到最新
- `func TestAppendWhileBrowsingHistoryKeepsWindow(t *testing.T)` — TestAppendWhileBrowsingHistoryKeepsWindow 红灯 4：正在翻更早历史时新到达的

### session_lifecycle.go

- `func (service *Service) hotAttachSession(sessionID string) error` — hotAttachSession 热加载：目标会话已驻留（引擎实例在内存，含运行中），
- `func (service *Service) UnloadSession(sessionID string) error` — UnloadSession 卸载会话：先持久化（非活跃时），再释放内存态（引擎实例、

### session_lifecycle_test.go

- `func TestResumeRunningSessionAllowsHotAttach(t *testing.T)` — TestResumeRunningSessionAllowsHotAttach（TC-A3-01 新语义）：运行中会话
- `func TestHotAttachDoesNotTouchRunningSession(t *testing.T)` — TestHotAttachDoesNotTouchRunningSession（TC-A3-02）：B 活跃、A 后台运行中
- `func TestHotAttachNoReplay(t *testing.T)` — TestHotAttachNoReplay（TC-LC-02）：空闲驻留会话 resume 只换指针，不重建
- `func TestUnloadReleasesScope(t *testing.T)` — TestUnloadReleasesScope（TC-LC-03）：unload 释放会话内存态（chat 运行态、
- `func TestUnloadRejectsRunningSession(t *testing.T)` — TestUnloadRejectsRunningSession（阶段 D · 卸载/提交互斥）：unload 与 submit
- `func containsMessageContent(messages []Message, needle string) bool`
- `func conversationTextsForTest(conversation []Message) []string`

### session_live_content_probe_test.go

- `func newStagedChatEngine() *stagedChatEngine`
- `func (engine *stagedChatEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func TestSwitchBackShowsLatestBackgroundContent(t *testing.T)` — TestSwitchBackShowsLatestBackgroundContent 复现“切回会话时前端拿到的内容

### session_meta.go

- `func (service *Service) metaPort() (session.SessionMetaPort, bool)` — metaPort 返回会话端口的可选展示元数据扩展。
- `func (service *Service) SetSessionMeta(sessionID string, meta SessionMeta) error` — SetSessionMeta 写单个会话的展示元数据，随后唤醒目录刷新让所有客户端重拉。
- `func (service *Service) SessionMeta(sessionID string) (SessionMeta, error)` — SessionMeta 读取单个会话的展示元数据（未装配元数据存储时返回

### session_parallel_test.go

- `func debugLog(format string, args ...any)` — debugLog 是 SEELEX_TEST_DEBUG=1 门控的临时诊断日志（复跑噪音点时用，
- `func dumpServiceState(service *Service, ids ...string) string` — dumpServiceState 在断言失败时输出 service 侧会话状态（断点现场）。
- `func dumpParallelState(service *Service, engine *multiSessionEngine, ids ...string) string` — dumpParallelState 汇总 service + engine 双侧状态，供失败断点输出。
- `func newMultiSessionEngine() *multiSessionEngine`
- `func (e *multiSessionEngine) register(sessionID string)` — register 注册一个已加载会话（模拟 fork/resume 后的子会话）。
- `func (e *multiSessionEngine) UnloadSession(sessionID string) error` — UnloadSession 释放指定会话的引擎状态（阶段 2 生命周期）。
- `func (e *multiSessionEngine) debugSnapshot(sessionIDs ...string) string` — debugSnapshot 返回引擎侧诊断快照（断点现场；自动加锁）。
- `func (e *multiSessionEngine) HasSession(sessionID string) bool`
- `func (e *multiSessionEngine) StartSession() string`
- `func (e *multiSessionEngine) ActivateSession(sessionID string) error` — ActivateSession 以显式会话 ID 创建（如缺）并激活引擎实例。
- `func (e *multiSessionEngine) SessionID() string`
- `func (e *multiSessionEngine) History() []EngineMessage`
- `func (e *multiSessionEngine) HistoryFor(sessionID string) []EngineMessage`
- `func (e *multiSessionEngine) ReplaceHistory(sessionID string, history []EngineMessage) error`
- `func (e *multiSessionEngine) ReplaceHistoryFor(sessionID string, history []EngineMessage) error` — ReplaceHistoryFor 是 SessionChatEngine 接口要求的会话内历史替换：替换
- `func (e *multiSessionEngine) ResumeSession(sessionID string, history []EngineMessage) error` — ResumeSession 模拟 production EnginePort.ResumeSession 语义：目标会话
- `func (e *multiSessionEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (e *multiSessionEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (e *multiSessionEngine) AppendHistoryFor(sessionID string, msg types.Message)`
- `func (e *multiSessionEngine) ClearHistoryFor(sessionID string)`
- `func (e *multiSessionEngine) SetSystemPromptFor(sessionID, prompt string)`
- `func (e *multiSessionEngine) ClearHistory()`
- `func (e *multiSessionEngine) TokenCount() string`
- `func (e *multiSessionEngine) TraceText() string`
- `func (e *multiSessionEngine) SubAgentTree() []dto.SubAgentTreeNode`
- `func (e *multiSessionEngine) SetSystemPrompt(string)`
- `func (e *multiSessionEngine) SetMaxLoops(int)`
- `func TestParallelSessionsExecuteConcurrently(t *testing.T)` — TestParallelSessionsExecuteConcurrently 验证 M2 核心语义：活跃会话运行中，
- `func TestParallelSessionsQueuedPerSession(t *testing.T)` — TestParallelSessionsQueuedPerSession 验证每个会话维护自己的输入队列：A 运行

### session_plan_switch_regression_test.go

- `func TestHotAttachKeepsBackgroundPlanProgress(t *testing.T)` — TestHotAttachKeepsBackgroundPlanProgress 回归：后台会话运行期间 plan 节点

### session_pollution_s0_test.go

- `func TestS0BackgroundSessionTaskWriteMustNotPolluteActiveRegistry(t *testing.T)`

### session_race_test.go

- `func TestSnapshotBumpConcurrentWithRunChatTail(t *testing.T)` — TestSnapshotBumpConcurrentWithRunChatTail（TC-R-02）：并发 Submit（触发
- `func TestReleaseWorkingHistoryConcurrentWithChatStream(t *testing.T)` — TestReleaseWorkingHistoryConcurrentWithChatStream（TC-R-03）：收尾清工作

### session_resource_isolation_test.go

- `func TestSessionDomainsDisjoint(t *testing.T)` — TestSessionDomainsDisjoint（TC-INV-01）：A 与 B 的 M/X/R 状态域无共享，
- `func TestViewSwitchDoesNotMutateExecution(t *testing.T)` — TestViewSwitchDoesNotMutateExecution（TC-INV-02）：切到 B 只换视图指针，
- `func TestPersistReadsOnlyOwnDomain(t *testing.T)` — TestPersistReadsOnlyOwnDomain（TC-INV-03）：快照/活跃槽全是 B 时，

### session_running_not_rerooted_test.go

- `func TestRunningSessionNotRerootedByAttach(t *testing.T)` — TestRunningSessionNotRerootedByAttach 回归（复现报告中“切到其它会话后工具

### session_runtime_slot_integration_test.go

- `func (engine *sessionTokenEngine) TokenCountFor(sessionID string) string`
- `func TestBackgroundRuntimeProjectionLandsInOwnSlot(t *testing.T)` — TestBackgroundRuntimeProjectionLandsInOwnSlot（G1-A/B 验收）：后台会话 B

### session_s0_events_test.go

- `func (engine *s0ChunkEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func viewContains(unit *session.SessionUnit, text string) bool` — viewContains 报告指定会话可见对话中是否出现目标文本。
- `func TestS0BackgroundEventsDoNotPolluteActiveSnapshot(t *testing.T)` — TestS0BackgroundEventsDoNotPolluteActiveSnapshot（波 1 验收锚）：
- `func TestS0SwitchResyncsBaseline(t *testing.T)` — TestS0SwitchResyncsBaseline（波 1 验收锚）：视图切到会话 B 后按显式 sid
- `func conversationTexts(messages []Message) []string`

### session_scope.go

- `func (service *Service) transitionView() sync.Locker` — transitionView 返回视图过渡锁（G5）：影响视图指针/当前视图会话生命周期
- `func (service *Service) perSessionExecution() bool` — perSessionExecution 报告宿主是否具备逐会话执行能力（生产 seelebridge
- `func (service *Service) transitionForSession(sessionID string) sync.Locker` — transitionForSession 返回目标会话生命周期的过渡锁：逐会话宿主按会话 key
- `func (service *Service) newGeneratedSessionID(prefix string) string` — newGeneratedSessionID 生成显式会话 ID（逐会话宿主 fork/切项目新建用；
- `func (service *Service) setWorkspaceWriteScope(workspaceID string)` — setWorkspaceWriteScope 设置 legacy Router 写作用域。逐会话工具根能力
- `func (service *Service) bindGlobalProjectRoot(rootPath string) error` — bindGlobalProjectRoot 设置进程级项目根。同上：per-session root 未实现前
- `func (service *Service) unbindGlobalProjectRoot()` — unbindGlobalProjectRoot 清空进程级项目根。
- `func (service *Service) transitionForKey(key string) sync.Locker` — transitionForKey 返回指定 key 的会话过渡锁（G5 per-session keyed）：会
- `func (service *Service) sessionUnitLocked(sessionID string) *session.SessionUnit` — sessionUnitLocked 返回指定会话的会话单元（聊天运行态已收进 SessionUnit，
- `func (service *Service) currentViewSessionID() string` — currentViewSessionID 返回当前视图会话 ID（读锁内快照；供解锁后发布
- `func (service *Service) effortForSession(sessionID string) string` — effortForSession 返回指定会话生效的 effort 级别（G4：Unit 内选择优先；
- `func (service *Service) syncPlanPolicyFor(sessionID string)` — syncPlanPolicyFor 按会话 effort 向引擎写入该会话的 plan 策略槽（G1-C：
- `func (service *Service) fullAccessForSession(sessionID string) bool` — fullAccessForSession 返回指定会话生效的全权模式（G4：Unit 内选择优先；
- `func (service *Service) syncFullAccessFor(sessionID string)` — syncFullAccessFor 按会话全权模式同步引擎门（G4：chat 起点调用，保证每个
- `func (service *Service) anyChatRunningLocked() bool` — anyChatRunningLocked 报告是否存在任意会话的运行中聊天。M1 单飞执行
- `func (service *Service) mirrorActiveChatLocked()` — mirrorActiveChatLocked 把当前活跃会话的聊天运行态写入会话 view（阶段 1：
- `func queuedChatRequests(requests []session.QueuedRequest) []chatRequest` — queuedChatRequests 把会话域排队输入（不透明载荷）还原为执行内核的
- `func (service *Service) activeQueuedChatRequestsLocked() []chatRequest` — activeQueuedChatRequestsLocked 返回当前会话域的排队输入（还原为执行内核
- `func (service *Service) publishSessionEvent(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) event.Event` — publishSessionEvent 发布事件；装配的 EventHub 支持会话路由时携带
- `func (service *Service) publishViewSessionChanged()` — publishViewSessionChanged 通告权威视图会话已被应用内部切换（进程级事件、
- `func (service *Service) publishChatStateFor(sessionID string)` — publishChatStateFor 下发指定会话的权威聊天运行态（chat.changed）。运行/排队
- `func (service *Service) bindProjectRootIfSafe(_ string, rootPath string) bool` — bindProjectRootIfSafe 在安全条件下重绑全局项目根（P3/G5 收口）：
- `func (service *Service) rebindViewWorkspaceWhenIdle()` — rebindViewWorkspaceWhenIdle 在进程变为完全空闲后，把全局项目根/Router 写
- `func (service *Service) SubmitToSession(ctx context.Context, sessionID, text string) error` — SubmitToSession 是会话级提交 API（M2：多会话并行执行）。目标会话即活跃
- `func (service *Service) sessionLoaded(sessionID string) bool` — sessionLoaded 报告目标会话引擎是否已实例化（后台提交前置检查）。
- `func (service *Service) ActivateSession(sessionID string) error` — ActivateSession 切换当前展示/执行会话。M1 没有每会话驻留快照，切换即
- `func (service *Service) SnapshotOf(sessionID string) (SessionSnapshot, error)` — SnapshotOf 返回指定会话的权威**会话快照**（G3 分型：SessionSnapshot，
- `func (service *Service) sessionResidentLocked(unit *session.SessionUnit, sessionID string) bool` — sessionResidentLocked 判定目标会话引擎是否驻留（SnapshotOf 热/冷分界；
- `func (service *Service) snapshotOfResident(sessionID string) (SessionSnapshot, error)` — snapshotOfResident 组装驻留会话（引擎 bundle 在内存）的会话快照：走单元
- `func sessionRuntimeOf(runtime RuntimeState) SessionRuntime` — sessionRuntimeOf 从全量 RuntimeState 投影提取会话专属运行原件（G3 字段
- `func cloneGoalGovernanceView(view *dto.GoalGovernanceView) *dto.GoalGovernanceView`
- `func (service *Service) sessionEventFilter(sessionID string) func(event.Event) bool` — sessionEventFilter 构造会话级订阅谓词（口径见 SubscribeSession）。
- `func (service *Service) SubscribeSessionWithReplay(sessionID string, buffer, replayWindow int) (Subscription, error)` — SubscribeSessionWithReplay 与 SubscribeSession 同一归属口径，但订阅附带
- `func (service *Service) SubscribeSession(sessionID string, buffer int) (Subscription, error)` — SubscribeSession 返回按会话过滤的事件订阅（只投递该会话或全局事件）。

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

- `func TestSessionSnapshotTransportShape(t *testing.T)` — TestSessionSnapshotTransportShape（G3）：SessionSnapshot 是传输完备的会话
- `func TestSessionSnapshotCarriesResidentFlag(t *testing.T)` — TestSessionSnapshotCarriesResidentFlag C1：SessionSnapshot 顶层携带
- `func TestProcessSnapshotTransportShape(t *testing.T)` — TestProcessSnapshotTransportShape（G3）：ProcessSnapshot 承载进程级目录与

### session_status_test.go

- `func (sessions *liveCatalogSessions) SessionsOf(string) []SessionInfo`
- `func (sessions *liveCatalogSessions) setInfos(infos []SessionInfo)`
- `func catalogStatusOf(t *testing.T, service *Service, sessionID string) SessionStatus` — catalogStatusOf 返回会话在 Snapshot 目录中的可见状态（等待目录刷新）。
- `func TestSessionStatusReflectsRunningWhileChatActive(t *testing.T)` — TestSessionStatusReflectsRunningWhileChatActive（状态机回归）：chat 启动
- `func TestSwitchToRunningSessionPreservesState(t *testing.T)` — TestSwitchToRunningSessionPreservesState（T4.5/运行中切换）：A 运行中切到

### session_storage.go

- `func (service *Service) SessionStorageConfig() (sessionstore.Config, error)`
- `func (service *Service) TestSessionStorage(ctx context.Context, config sessionstore.Config) error`
- `func (service *Service) ConfigureSessionStorage(ctx context.Context, config sessionstore.Config) error`

### session_stress_test.go

- `func TestStressConcurrentSessionsDoNotPollute(t *testing.T)`

### session_switch_ab_test.go

- `func newLockRegistry(interval time.Duration) *lockRegistry`
- `func (registry *lockRegistry) busyWork(stop <-chan struct{})`
- `func (registry *lockRegistry) switchTo(sessionID string) time.Duration`
- `func (registry *lockRegistry) applied() int`
- `func (registry *lockRegistry) validate() bool`
- `func newActorRegistry(interval time.Duration) *actorRegistry`
- `func (registry *actorRegistry) loop()`
- `func (registry *actorRegistry) busyWork(stop <-chan struct{})`
- `func (registry *actorRegistry) switchTo(sessionID string) time.Duration`
- `func (registry *actorRegistry) applied() int`
- `func (registry *actorRegistry) validate() bool`
- `func (registry *actorRegistry) close()`
- `func appendMirrorWork(log *[]int, value int)` — appendMirrorWork 追加一条“镜像工作”日志并裁剪（模拟忙写/切换的固定临界
- `func mirrorMonotonic(log []int) bool`
- `func runABRound(t *testing.T, registry abSwitchRegistry, name string) (time.Duration, time.Duration, int)` — runABRound 在 rate-limited 忙负载下并发发起 switches，返回一次轮次的
- `func median(values []time.Duration) time.Duration`
- `func runABCompare(t *testing.T, registry abSwitchRegistry, name string)` — runABCompare 跑多次轮次并汇总两个维度：
- `func TestSessionSwitchABLockVsActor(t *testing.T)` — TestSessionSwitchABLockVsActor 对比锁串行（A）与 actor 模型（B）在忙会话

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
- `func viewRoles(service *Service, sessionID string) []string` — viewRoles 返回指定会话可见对话的角色序列（含文本内容），用于断言顺序。
- `func indexOf(roles []string, prefix string) int` — indexOf 返回序列中首个以 prefix 开头的下标；不存在返回 -1。
- `func TestToolOrderingStableAfterSwitchToRunningSession(t *testing.T)` — TestToolOrderingStableAfterSwitchToRunningSession 复现「切换运行中会话后
- `func TestBackgroundToolEventsCarryOwnRequestID(t *testing.T)` — TestBackgroundToolEventsCarryOwnRequestID 复现后台会话工具事件携带活跃
- `func TestSubmitPromptWhileOtherSessionRuns(t *testing.T)` — TestSubmitPromptWhileOtherSessionRuns 复现输入阻塞：A 运行中（引擎阻塞），
- `func TestDeltasKeepFlowingAfterSwitchToRunningSession(t *testing.T)` — TestDeltasKeepFlowingAfterSwitchToRunningSession 回归守卫：切到运行中

### session_switch_failed_resume_test.go

- `func (s *attachFailingSessions) AttachSessionContext(_ string, sessionID string) error`
- `func (s *attachFailingSessions) DetachSessionContext()`
- `func TestFailedResumeKeepsViewOnPreviousSessionAndSubmitContinuesIt(t *testing.T)` — TestFailedResumeKeepsViewOnPreviousSessionAndSubmitContinuesIt 复现“切换失败

### session_switch_probe_test.go

- `func switchDuringInFlightChat(t *testing.T, label string)` — switchDuringInFlightChat 在引擎回合仍在执行（未 release）时切换视图到另
- `func TestSwitchDuringToolInvocation(t *testing.T)` — TestSwitchDuringToolInvocation 复现：工具调用（引擎忙）期间切换会话。
- `func TestSwitchDuringLLMStreaming(t *testing.T)` — TestSwitchDuringLLMStreaming 复现：LLM 输出（引擎忙）期间切换会话。
- `func (sessions *gatedHistorySessions) releaseNow()`
- `func (sessions *gatedHistorySessions) LoadHistory(sessionID string) ([]EngineMessage, error)`
- `func (sessions *gatedHistorySessions) LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error)`
- `func (sessions *gatedHistorySessions) gateEnabled(sessionID string) bool`
- `func TestSwitchDuringColdLoadSerializes(t *testing.T)` — TestSwitchDuringColdLoadSerializes 复现：会话 A 处于冷加载（历史装载被
- `func TestBeginNewSessionSingleDraftOwner(t *testing.T)` — TestBeginNewSessionSingleDraftOwner 复现/钉住“新建会话”的幂等与责任链：

### session_switch_running_cold_test.go

- `func TestColdResumeWhileRunningIsAsync(t *testing.T)` — TestColdResumeWhileRunningIsAsync 复现“会话运行中切换到冷加载目标长期处于

### shutdown_concurrent_test.go

- `func (engine *cancelAwareEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *cancelAwareEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *cancelAwareEngine) count() int`
- `func TestShutdownConcurrentRunningSessions(t *testing.T)` — TestShutdownConcurrentRunningSessions（R5）：运行中多会话 + 并发 Shutdown

### subscribe_session_test.go

- `func TestSubscribeSessionFollowsDraftMaterialization(t *testing.T)` — TestSubscribeSessionFollowsDraftMaterialization 验证早分配 SID 后，草稿
- `func TestSubscribeSessionFollowsViewPointer(t *testing.T)` — TestSubscribeSessionFollowsViewPointer 验证空 sid 订阅（过渡口径）仍按

### view_singleton_test.go

- `func TestViewSingletonMirrorConsistent(t *testing.T)` — TestViewSingletonMirrorConsistent（M4/INV-M4）：V 的唯一镜像
- `func TestDraftMaterializeStreamsMirrorToSnapshot(t *testing.T)` — TestDraftMaterializeStreamsMirrorToSnapshot：草稿物化后流式增量走活跃路径，

### view_switch_isolation_test.go

- `func TestLongRunningSessionSurvivesViewSwitch(t *testing.T)` — TestLongRunningSessionSurvivesViewSwitch（视图切换隔离用例）：先开启一个
- `func readSessionView(t *testing.T, service *Service, sessionID string) []Message`
- `func viewText(messages []Message) string`
- `func joinHistoryText(history []EngineMessage) string`
- `func joinTranscript(t *testing.T, service *Service, sessionID string) string`
