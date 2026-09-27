# core/session（根包分卷）

## 生态位

会话草稿/恢复/存储用例与集成测试；运行中切到未驻留会话走异步冷加载（restoring 空壳 + 后台装载 + epoch 判定发布基线）

覆盖：`session*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 历史分页契约（2026-09-11 / 2026-09-25）

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
   重新读尾部窗口贴尾，并带回回看期间的新消息——但只带得回**已发布**的那部分，
   见第 6~9 条。**用户行例外（2026-09-28）**：回看期间到达的 `user` 行开启新一轮
   对话，追加前先把窗口原地重置为「以这条新消息为尾」（`endBrowsingForNewTurn`）
   ——用户发言意味着「从现在起看最新」，否则这一轮自己的输入落在窗口之外，前端
   reducer 在 `historyWindowed` 时也不落 `message.added`，表现为「输入被吞、整个
   回合界面一动不动」。
5. 无 record 的旧格式会话冷加载同样写入 `TotalMessages/HistoryOffset`（历史
   总数来自 provider 历史），否则 `HasMoreHistory` 恒为 false，早期历史读不到。
6. **冷读面的右界是发布点**：回合内的可见行先进内存窗口，`PersistCurrentSession`
   要到 runChat 收尾才落盘，而存储侧读者闸门是 message 通道的 `head.LastSeq`
   （`decodePublishedRows`）。所以「内存已可见总数 > 磁盘已发布数」在运行中的
   会话里是常态，差额就是本轮尚未落盘的行。
7. `installVisibleHistory`（三条加载方向入口的唯一安装点）据此守三条：已可见
   总数不因冷读倒退（取磁盘与内存的较大值，否则前端「下方还有 N 条」被抹平，
   且一个并不贴尾的窗口会被 append 路径判成贴尾，下一条消息插进列表中间形成
   断层）；窗口起点由**实际装进去的行**推出，不接受调用方估计值；磁盘页与内存
   窗口之间有空洞时不假装连续（宁可窗口短一页，冷读一页为空时保持原窗口）。
8. **热读短路**：内存窗口已经贴着有效尾时 `LoadLatestHistory` 直接返回——它
   已经在最新处，再冷读只会换上更旧的已发布一页。这条短路就是「加载着加载着
   只剩冷加载内容、尾部没了，一出工具结果又像是恢复了正常」的修复点。
9. 尾部窗口读先探已发布总数、再按总数定位起点（`loadConversationTailPage`，与
   `session_runtime.LoadHistoryTailWindow` 同一形状）；尚未落盘又已滑出窗口的
   行要等这次提交后才能回读（有界滑动窗口的固有语义）。
10. 复现与回归：`session_history_pagination_test.go`（分页态随会话走）+
    `session_history_hot_tail_test.go`（在飞尾部不被冷读抹掉、总数不倒退、
    offset 与内容对齐）+ `session_history_browsing_submit_repro_test.go`
    （回看期间提交的用户行必须回到窗口，「输入被吞」的复现）。

## 提交归属：草稿与运行中会话（2026-09-20）

渲染层把每一条普通输入都显式钉到「当前视图会话 ID」（`SubmitToSession`），包括
草稿的早分配 SID。本卷因此必须自己守住两条边界：

1. **显式提交目标是未物化草稿时，先物化再判定是否加载**（`materializeDraftForSubmit`
   → `materializeDraftSession`）。草稿没有可冷回读的历史：走 `ActivateSession` 会把
   那条 `Status=draft` 的 record 当冷会话装载，新会话以「已恢复会话: draft_…」开头、
   草稿槽位不消费、composer 不清理（重启后已发送的正文会回到输入框）。
2. **归属不符要显式失败**：草稿槽是进程单例，物化会把共享视图镜像切到该 SID，因此
   只有视图仍停在这份草稿上时才允许物化，否则返回 `ErrDraftNotInView`——渲染层拿着
   过期快照提交时，明确失败比把输入投给别的会话安全。多页签各自持有 composer 后这条
   判据要随草稿槽一起改成分布式（见 `docs/2026-09-02-session-subsystem-remediation/target-design.md`）。
3. **任何生命周期命令都不得经进程级活跃别名读写引擎**（`Engine.History()` /
   `ClearHistory()` / `SetSystemPrompt()`），一律用 `engineHistoryFor` /
   `clearEngineHistoryFor` / `SetSystemPromptFor` 按会话路由。别名指向哪个会话不可
   预期，可能是另一个正在运行的会话——它的 framework `Session` 锁被 `ChatStream` 从
   进函数持到出函数，任何别名调用都排在整轮之后。
4. **别名阻塞的放大效应来自过渡锁**：生产宿主 `PerSessionExecution() == false`，
   `transitionForSession` 一律回退视图 key，所以「一次点击排在运行中会话之后」会连带
   冻住所有会话的 resume/unload 与 ambient 提交。审查生命周期方法时先问：锁内有没有
   可能等另一会话引擎的调用？
5. 复现与回归：`session_running_idle_submit_test.go`（草稿显式提交物化、运行中会话
   不挡新建会话）+ `session_submit_restoring_test.go`（restoring 期延后提交）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### approval_session_ownership_test.go

- `func waitApprovalCount(t *testing.T, service *Service, sessionID string, want int)` — waitApprovalCount 轮询指定会话单元的待批数直到到达目标（approval 观察
- `func TestApprovalSessionOwnershipStatusAndSnapshot(t *testing.T)` — TestApprovalSessionOwnershipStatusAndSnapshot 波 4 approval 会话级归属：
- `func TestBackgroundApprovalDoesNotClobberViewSlotAndResolvesById(t *testing.T)` — TestBackgroundApprovalDoesNotClobberViewSlotAndResolvesById 波 4 归属：
- `func TestApprovalConcurrentSessionsStayAttributed(t *testing.T)` — TestApprovalConcurrentSessionsStayAttributed -race 靶场：两会话并发开
- `func startsWithApprovalID(id, prefix string) bool`

### content_lru.go

- `func loadedContentLimit(configured int) int` — loadedContentLimit 把配置值收敛为生效上限（<=0 = 未配置 → 默认值；与
- `func (service *Service) touchContent(sessionID string)` — touchContent 记录一次会话可见正文使用（冷加载完成/热挂载/分页读回/回读），
- `func (service *Service) markContentRecentLocked(sessionID string)` — markContentRecentLocked 把会话移到内容 LRU 使用序最前（索引 0 = 最近使用；
- `func (service *Service) forgetContentLocked(sessionID string)` — forgetContentLocked 把会话移出内容 LRU 使用序（正文已卸载；调用方持有
- `func (service *Service) pruneContentOrderLocked() int` — pruneContentOrderLocked 收敛使用序并返回当前持有正文的会话数（调用方持有
- `func (service *Service) reconcileLoadedContentLimit(protect string)` — reconcileLoadedContentLimit 超限时按 LRU 卸载空闲会话的已加载正文（无候选
- `func (service *Service) pickContentEvictableCandidateLocked(protect string) string` — pickContentEvictableCandidateLocked 从使用序最旧端挑一个可卸载正文的会话
- `func (service *Service) evictLoadedContent(sessionID string) error` — evictLoadedContent 卸载一个空闲会话的已加载正文：先 flush（非活跃会话
- `func (service *Service) sessionContentLoadedLocked(sessionID string) bool` — sessionContentLoadedLocked 报告目标会话当前是否持有已加载的可见正文
- `func (service *Service) sessionContentUnloaded(sessionID string) bool` — sessionContentUnloaded 报告目标会话的可见正文是否已被内容 LRU 卸载（需要
- `func (service *Service) ensureSessionContent(sessionID string) error` — ensureSessionContent 在目标会话正文已被卸载时从磁盘回读（热挂载不重建
- `func (service *Service) reloadSessionContent(sessionID string) error` — reloadSessionContent 冷回读被卸载的可见正文窗口：经与「回到最新」同一条尾部

### content_lru_test.go

- `func withContentLimit(limit int) func()` — withContentLimit 临时把进程级 loaded_content_limit 改成 limit，返回还原
- `func newMultiPagedStore(counts map[string]int) *multiPagedStore`
- `func (store *multiPagedStore) SessionsOf(projectID string) []SessionInfo` — SessionInfo 行只带标题（零正文读）：目录刷新不会把正文拉回内存。
- `func (store *multiPagedStore) LoadHistory(sessionID string) ([]EngineMessage, error)`
- `func (store *multiPagedStore) LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error)`
- `func (store *multiPagedStore) LoadSessionRecordWorkspace(workspaceID, sessionID string) (SessionRecord, error)`
- `func (store *multiPagedStore) SaveSessionRecord(sessionID string, record SessionRecord) error` — SaveSessionRecord* 是 no-op：本夹具的 durable 消息由 messages 槽给出，卸载
- `func (store *multiPagedStore) SaveSessionRecordWorkspace(workspaceID, sessionID string, record SessionRecord) error`
- `func (store *multiPagedStore) LoadSessionRecord(sessionID string) (SessionRecord, error)`
- `func (store *multiPagedStore) LoadConversationRangeWorkspace(workspaceID, sessionID string, offset, limit int) ([]Message, int, error)`
- `func (store *multiPagedStore) messageCount(sessionID string) int`
- `func readContentState(t *testing.T, service *Service, sessionID string) contentState`
- `func assertContentLoaded(t *testing.T, service *Service, sessionID string, want bool) contentState`
- `func assertContentWindow(t *testing.T, service *Service, sessionID string, offset, count int)` — assertContentWindow 断言会话当前持有「尾部窗口 offset 起 count 条」的正文。
- `func waitForContentLoaded(t *testing.T, service *Service, sessionID string) contentState` — waitForContentLoaded 轮询等待会话正文装载完成：运行中会话存在时冷加载走
- `func waitForContentUnloaded(t *testing.T, service *Service, sessionID string) contentState` — waitForContentUnloaded 轮询等待内容 LRU 真正把正文卸载（区别于「本来就
- `func snapshotSessions(t *testing.T, service *Service) map[string]SessionInfo` — snapshotSessions 等一轮目录收敛后返回左侧列表行（按 ID 索引）。
- `func TestLoadedContentLimitDefaults(t *testing.T)` — TestLoadedContentLimitDefaults 内容上限默认 12（seelexctx 单一事实源），
- `func TestContentLimitEvictsLeastRecentlyUsedIdleContent(t *testing.T)` — TestContentLimitEvictsLeastRecentlyUsedIdleContent 上限 1：冷加载第二个会话
- `func TestContentLimitKeepsBusyAndActiveSessions(t *testing.T)` — TestContentLimitKeepsBusyAndActiveSessions 上限 1：运行中（busy）会话与当前
- `func TestContentEvictionColdReloadsSameWindow(t *testing.T)` — TestContentEvictionColdReloadsSameWindow 会话正文被卸载后再激活（冷回读）
- `func TestEnsureSessionContentRebuildsEvictedWindow(t *testing.T)` — TestEnsureSessionContentRebuildsEvictedWindow 热挂载回读面

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

### session_cold_start_draft_test.go

- `func TestColdStartDraftSlotIsRetainedAcrossSwitch(t *testing.T)`

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
- `func (service *Service) isUnmaterializedDraftTarget(sessionID string) bool` — isUnmaterializedDraftTarget 预判显式提交的目标是否就是那份尚未物化的草稿
- `func (service *Service) materializeDraftForSubmit(sessionID, firstInput string) error` — materializeDraftForSubmit 把「显式提交的目标恰好是未物化的草稿」接回物化路径。

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

### session_fullaccess_isolation_test.go

- `func waitPendingApprovalsFor(t *testing.T, service *Service, sessionID string, want int)` — waitPendingApprovalsFor 等待指定会话的待批审批数达到 want。
- `func TestProbeFullAccessToggleStaysWithinViewSession(t *testing.T)` — TestProbeFullAccessToggleStaysWithinViewSession：视图会话点全权只影响自己。
- `func TestProbeFullAccessSurvivesBackgroundSessionStartSync(t *testing.T)` — TestProbeFullAccessSurvivesBackgroundSessionStartSync：别的会话起跑

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
- `func (service *Service) visibleTailServedFromMemory(sessionID string) bool` — visibleTailServedFromMemory 报告内存窗口是否已经把有效尾部整窗带到（非空且
- `func (service *Service) loadConversationTailPage(workspaceID, sessionID string, window int) (conversationPage, error)` — loadConversationTailPage 读磁盘已发布的尾部窗口：先探总数，再按总数定位窗口
- `func currentWorkspaceIDLocked(service *Service) string` — currentWorkspaceIDLocked 返回当前视图会话的 workspace ID（调用方持有
- `func (service *Service) loadConversationPage(workspaceID, sessionID string, offset, limit int) ([]Message, int, error)` — loadConversationPage 读回一段可见历史：record conversation 模块优先
- `func (service *Service) installVisibleHistory(sessionID string, page conversationPage, window int, mode historyPageInstall) error` — installVisibleHistory 安装一页可见历史：写会话可见投影（事实源）→ 收敛
- `func durableConversationRows(messages []Message) []Message` — durableConversationRows 取出可见窗口里参与历史游标的行（system 引导行不占
- `func adaptEngineMessage(msg EngineMessage) Message`
- `func isVisibleHistoryMessage(message EngineMessage) bool`

### session_history_browsing_submit_repro_test.go

- `func TestReproSubmitWhileBrowsingKeepsUserRowInWindow(t *testing.T)` — TestReproSubmitWhileBrowsingKeepsUserRowInWindow 回看历史时提交一轮对话，
- `func TestReproSubmitWhileBrowsingEndsBrowsingState(t *testing.T)` — TestReproSubmitWhileBrowsingEndsBrowsingState 提交之后会话不得再被判成
- `func containsContent(contents []string, want string) bool`

### session_history_hot_tail_test.go

- `func beginInFlightTurn(t *testing.T, service *Service, store *pagedSessionStore, count int) []string` — beginInFlightTurn 造「本轮已进内存窗口、尚未落盘」的在飞尾部：store 只物理
- `func assertVisibleWindow(t *testing.T, label string, snapshot Snapshot, wantOffset int, wantContents []string, wantTotal int)` — assertVisibleWindow 断言快照里的「窗口起点 + 窗口内容 + 已可见总数」。
- `func TestLoadLatestHistoryKeepsInFlightTail(t *testing.T)` — TestLoadLatestHistoryKeepsInFlightTail 红灯 1（缺陷 1）：回合进行中，内存窗口
- `func TestColdPageReadDoesNotRegressVisibleTotal(t *testing.T)` — TestColdPageReadDoesNotRegressVisibleTotal 红灯 2（缺陷 2）：「加载更早」滑走
- `func TestToolResultAfterPagingDoesNotPunchHole(t *testing.T)` — TestToolResultAfterPagingDoesNotPunchHole 红灯 3（缺陷 1+2 的后果）：内存窗口
- `func TestLoadLatestHistorySplicesStraddlingHotTail(t *testing.T)` — TestLoadLatestHistorySplicesStraddlingHotTail 红灯 4（缺陷 1+3）：内存窗口正好
- `func TestInFlightTailReturnsAfterCommit(t *testing.T)` — TestInFlightTailReturnsAfterCommit 收口断言：在飞行落盘（发布点推进）之后，

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
- `func (store *pagedSessionStore) appendDurable(role, content string)` — appendDurable 追加一条**已提交**行（物理落盘 + 推进发布点）。
- `func (store *pagedSessionStore) appendDraft(role, content string)` — appendDraft 只将行物理落盘，不推进发布点：对冷读面不可见（等价 message
- `func (store *pagedSessionStore) publishDraftTail()` — publishDraftTail 推进发布点（回合收尾 PersistCurrentSession 同一效果）。
- `func (store *pagedSessionStore) appendRowLocked(role, content string)`
- `func (store *pagedSessionStore) setPublished(messages ...Message)` — setPublished 直接铺一整份**已发布**的磁盘消息：不关心草稿尾的用例用它，
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

### session_input_index.go

- `func (service *Service) SessionInputIndex(sessionID string) (model.SessionInputIndex, error)` — SessionInputIndex 返回目标会话的**全量**用户输入索引（含未加载的早期轮次）。
- `func (service *Service) sessionInputIndexConversation(location session_runtime.Location, sessionID string) ([]model.Message, int, error)` — sessionInputIndexConversation 读取会话的完整可见会话（用户输入的事实源）。
- `func (service *Service) sessionInputWindowLoaded(sessionID string, total int) (model.SessionInputWindow, []string)` — sessionInputWindowLoaded 返回目标会话的已加载窗口元数据与窗口内已加载的
- `func buildSessionInputIndex(conversation []model.Message, loaded []string, limit int) []model.SessionInputIndexRow` — buildSessionInputIndex 从完整可见会话构建全量用户输入索引（纯函数）。
- `func inputIndexUserText(message model.Message) string` — inputIndexUserText 返回一条可见消息作为「用户输入」的展示正文；非用户输入
- `func summarizeInputIndexText(text string, limit int) (string, int)` — summarizeInputIndexText 把正文压成有界摘要：空白折叠 + rune 截断（末尾省略号）。
- `func alignLoadedInputTexts(texts, loaded []string) (int, int)` — alignLoadedInputTexts 把「窗口内已加载的用户输入」对齐到全量输入序列：

### session_input_index_test.go

- `func inputIndexService(t *testing.T, store *pagedSessionStore) *Service` — inputIndexService 用「durable 消息 + record + conversation 窗口读」的仿真
- `func setLoadedWindow(t *testing.T, service *Service, sessionID string, start, end int, messages []Message)` — setLoadedWindow 模拟"前端只加载了尾部一窗"：把可见窗口锚定在 [start,end)。
- `func TestSessionInputIndexCoversUnloadedEarlyRounds(t *testing.T)` — TestSessionInputIndexCoversUnloadedEarlyRounds 全量索引必须包含尚未加载到
- `func TestSessionInputIndexEmptySession(t *testing.T)` — TestSessionInputIndexEmptySession 空会话（无消息、无窗口）返回空索引且不报错。
- `func TestSessionInputIndexRequiresSessionID(t *testing.T)` — TestSessionInputIndexRequiresSessionID 空 sessionID 显式失败（不静默成空索引）。
- `func TestSessionInputIndexTruncatesSummary(t *testing.T)` — TestSessionInputIndexTruncatesSummary 摘要长度有界（rune 截断 + 原文长度留痕）。
- `func TestSessionInputIndexSkipsNonUserAndInternalRows(t *testing.T)` — TestSessionInputIndexSkipsNonUserAndInternalRows 只索引真正的用户输入：
- `func TestSessionInputIndexFallsBackToRecord(t *testing.T)` — TestSessionInputIndexFallsBackToRecord 会话存储没有 message 行（旧布局）时
- `func TestAlignLoadedInputTexts(t *testing.T)` — TestAlignLoadedInputTexts 对齐算法：尾部窗口整段命中；最新一条输入尚未进入
- `func TestBuildSessionInputIndexMarksLoadedSection(t *testing.T)` — TestBuildSessionInputIndexMarksLoadedSection 纯函数口径：只有命中已加载段

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

### session_pending_tail.go

- `func (service *Service) recoverPendingMessageTailAtLoad(sessionID string) string` — 草稿尾部（seq_draft）恢复的应用侧装配（A4）。
- `func (service *Service) appendPendingTailNoticeLocked(sessionID, notice string)` — appendPendingTailNoticeLocked 把草稿尾恢复的结论作为一条 system 行补进目标会话

### session_pending_tail_test.go

- `func (s *pendingTailSessions) record(call string)`
- `func (s *pendingTailSessions) callOrder() []string`
- `func (s *pendingTailSessions) PendingMessageTailWorkspace(_, _ string) (dto.PendingMessageTailReport, bool, error)`
- `func (s *pendingTailSessions) RecoverPendingMessageTailWorkspace(_, _ string) (dto.PendingMessageTailReport, bool, error)`
- `func (s *pendingTailSessions) DiscardPendingMessageTailWorkspace(_, _ string) (dto.PendingMessageTailReport, bool, error)`
- `func pendingTailVisibleTail(t *testing.T, service *Service) string` — pendingTailVisibleTail 返回可见会话里最后一条 system 行的正文（没有则 ""）。
- `func TestPendingTailRecoverableIsRecoveredAndVisible(t *testing.T)` — TestPendingTailRecoverableIsRecoveredAndVisible：基座一致的草稿尾在冷加载时被
- `func TestPendingTailGapIsReportedOnly(t *testing.T)` — TestPendingTailGapIsReportedOnly：基座断裂只报告——不得调用恢复，也不得丢弃，
- `func TestPendingTailCleanIsSilent(t *testing.T)` — TestPendingTailCleanIsSilent：没有草稿尾时不做任何写、不打扰用户。
- `func TestPendingTailRunningSessionNotTouched(t *testing.T)` — TestPendingTailRunningSessionNotTouched：回合正在跑的会话，加载路径不得抢发布点
- `func TestPendingTailCapabilityAbsentIsNoop(t *testing.T)` — TestPendingTailCapabilityAbsentIsNoop：存储未装配该能力时静默退回（纯增强语义）。

### session_permission_tier.go

- `func (service *Service) settingPort() (session.SessionSettingPort, bool)` — settingPort 返回会话端口的可选"会话级用户设置"扩展（未装配 = 档位退回内存态，
- `func (service *Service) persistPermissionTier(sessionID, tier string) error` — persistPermissionTier 把档位写进会话级设置（"" = 清除该会话的选择）。
- `func (service *Service) readStoredPermissionTier(sessionID string) string` — readStoredPermissionTier 读取该会话持久化的权限档位（"" = 从未选择 / 未装配
- `func (service *Service) applyStoredPermissionTier(sessionID, tier string)` — applyStoredPermissionTier 把持久化档位落到该会话的内存槽 + 执行门 + 审批自动
- `func (service *Service) restorePermissionTierFor(sessionID string)` — restorePermissionTierFor 读回并落地该会话的档位（非锁内衔接场合的便捷入口）。

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
- `func TestPermissionTierSwitchMirrorsViewSnapshot(t *testing.T)` — TestPermissionTierSwitchMirrorsViewSnapshot：切到某会话时，**目标会话**的生效

### session_permission_tier_test.go

- `func TestPermissionTierOwnershipPerSession(t *testing.T)` — TestPermissionTierOwnershipPerSession：档位选择归属进 SessionUnit——每个会话保存
- `func TestPermissionTierProjectionPerSession(t *testing.T)` — TestPermissionTierProjectionPerSession：运行时投影按会话读取生效档位（view 协调器
- `func TestSetPermissionTierRejectsUnknown(t *testing.T)` — TestSetPermissionTierRejectsUnknown：非法档位 id 报错且**不改变**当前档位。
- `func TestSetFullAccessCompatMapsToTier(t *testing.T)` — TestSetFullAccessCompatMapsToTier：旧全权开关是档位的兼容壳（true ⇔ full，

### session_plan_switch_regression_test.go

- `func TestHotAttachKeepsBackgroundPlanProgress(t *testing.T)` — TestHotAttachKeepsBackgroundPlanProgress 回归：后台会话运行期间 plan 节点

### session_pollution_s0_test.go

- `func TestS0BackgroundSessionTaskWriteMustNotPolluteActiveRegistry(t *testing.T)`

### session_project_root_test.go

- `func (runtime *fakeRuntime) BindProjectRootFor(sessionID, rootPath string) error` — BindProjectRootFor / UnbindProjectRootFor 是 fakeRuntime 对生产
- `func (runtime *fakeRuntime) UnbindProjectRootFor(sessionID string)`
- `func (runtime *fakeRuntime) toolRootForSession(sessionID string) string` — toolRootForSession 模拟工具面的路径根解析：会话根优先，未绑定时回退进程默认
- `func newMultiProjectWorkspace() *multiProjectWorkspace`
- `func (repo *multiProjectWorkspace) Create(name, rootPath, gitRemote string) (WorkspaceInfo, error)`
- `func TestBackgroundSessionKeepsOwnProjectRoot(t *testing.T)` — TestBackgroundSessionKeepsOwnProjectRoot 复现工作区污染：会话 A 绑定项目 A，
- `func waitSessionIdle(t *testing.T, service *Service)` — waitSessionIdle 等待全部会话回合结束（多会话并行时视图 Chat 状态不代表进程空闲）。

### session_queue_recover.go

- `func (service *Service) reEnqueueRecoveredInputs(sessionID string, resent []sessionstore.QueueItem)` — durable queue 的重启回填（写入侧接线的恢复半边）。
- `func (service *Service) drainRecoveredQueueLocked(unit *session.SessionUnit, request chatRequest) (chatRequest, bool)` — drainRecoveredQueueLocked 把本会话队列里已有的待发项与本次提交合并为同一轮

### session_race_test.go

- `func TestSnapshotBumpConcurrentWithRunChatTail(t *testing.T)` — TestSnapshotBumpConcurrentWithRunChatTail（TC-R-02）：并发 Submit（触发
- `func TestReleaseWorkingHistoryConcurrentWithChatStream(t *testing.T)` — TestReleaseWorkingHistoryConcurrentWithChatStream（TC-R-03）：收尾清工作

### session_release_async_test.go

- `func TestDeleteSessionReleasesBackgroundRuns(t *testing.T)`

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
- `func (service *Service) bindSessionProjectRoot(sessionID string)` — bindSessionProjectRoot 把指定会话自己的项目根绑到工具面（按会话分格）。
- `func (service *Service) transitionForKey(key string) sync.Locker` — transitionForKey 返回指定 key 的会话过渡锁（G5 per-session keyed）：会
- `func (service *Service) sessionUnitLocked(sessionID string) *session.SessionUnit` — sessionUnitLocked 返回指定会话的会话单元（聊天运行态已收进 SessionUnit，
- `func (service *Service) currentViewSessionID() string` — currentViewSessionID 返回当前视图会话 ID（读锁内快照；供解锁后发布
- `func (service *Service) effortForSession(sessionID string) string` — effortForSession 返回指定会话生效的 effort 级别（G4：Unit 内选择优先；
- `func (service *Service) syncPlanPolicyFor(sessionID string)` — syncPlanPolicyFor 按会话 effort 向引擎写入该会话的 plan 策略槽（G1-C：
- `func (service *Service) permissionTierForSession(sessionID string) string` — permissionTierForSession 返回指定会话生效的权限档位（G4：Unit 内选择优先；
- `func (service *Service) fullAccessForSession(sessionID string) bool` — fullAccessForSession 是档位的**兼容派生读面**：full 档 ⇔ 旧的全权开启。
- `func (service *Service) syncFullAccessFor(sessionID string)` — syncFullAccessFor 按会话档位同步执行门（G4：chat 起点调用，保证每个
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
- `func (service *Service) awaitRestore(ctx context.Context, sessionID string) error` — awaitRestore 等目标会话的后台冷加载结束（restoring 清除）后返回。多会话
- `func (service *Service) deferSubmitUntilRestored(ctx context.Context, sessionID, text string)` — deferSubmitUntilRestored 把一次对话提交挂到后台冷加载完成点：restoring 期间
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

### session_submit_restoring_test.go

- `func (engine *multiSessionEngine) streamCallsFor(sessionID string) int` — streamCallsFor 读引擎在某会话上的 ChatStreamFor 调用次数（加锁；ChatStreamFor
- `func (engine *multiSessionEngine) releaseSession(sessionID string)` — releaseSession 放行某会话的引擎回合（加锁取通道；幂等关闭）。
- `func coldRestoreFixture(t *testing.T) (*multiSessionEngine, *gatedHistorySessions, *Service)` — coldRestoreFixture 复现「A 运行中、目标冷会话 B 的存储读被门闩限速」的
- `func TestSubmitDuringColdRestoreDefersAndKeepsHistoryOrder(t *testing.T)` — TestSubmitDuringColdRestoreDefersAndKeepsHistoryOrder 锁定 2026-09-17 根因
- `func TestSubmitToSessionDefersUntilRestoreCompletes(t *testing.T)` — TestSubmitToSessionDefersUntilRestoreCompletes 锁定「延后」语义的两条边界：

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
