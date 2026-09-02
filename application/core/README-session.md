# core/session

## 生态位

会话草稿/恢复/存储用例与集成测试

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

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
- `func TestResumeSessionContinuationKeepsTrailingUnansweredUserInput(t *testing.T)`
- `func TestProviderRepairNoteNeverBecomesVisibleAssistantText(t *testing.T)`

### session_catalog.go

- `func (service *Service) WaitCatalogRefresh(ctx context.Context) error` — WaitCatalogRefresh 等待会话目录 worker 完成一轮"覆盖了本次请求"的刷新，使

### session_catalog_test.go

- `func addCatalogSession(sessions *scopedSessions, projectID, sessionID string, updatedAt time.Time)` — addCatalogSession 在会话端口的项目索引里追加一个会话（目录刷新会读到它）。
- `func newCatalogTestService(t *testing.T, sessions *scopedSessions) *Service`
- `func TestWaitCatalogRefreshSettlesFreshCatalog(t *testing.T)` — TestWaitCatalogRefreshSettlesFreshCatalog 是 C3 回执的核心契约：一轮
- `func TestWaitCatalogRefreshServesCoalescedRequests(t *testing.T)` — TestWaitCatalogRefreshServesCoalescedRequests 覆盖唤醒槽位被丢弃时的回执
- `func TestWaitCatalogRefreshConvergesAfterShutdown(t *testing.T)` — TestWaitCatalogRefreshConvergesAfterShutdown 钉住关闭路径：worker 退出时

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

- `func (service *Service) BeginNewSession() error` — BeginNewSession 进入幂等、未持久化的草稿状态。引擎会话只在第一条真实
- `func (service *Service) materializeDraftSession(firstQuestion string) error` — materializeDraftSession 为首条请求创建引擎会话与项目绑定。调用方必须持有

### session_fork.go

- `func (service *Service) ForkSession(parentID string, request model.ForkRequest) (string, error)` — ForkSession 从父会话的指定切断点创建独立子会话，并切换到子会话继续。
- `func (service *Service) ForkSessionLatest(parentID string) (string, error)` — ForkSessionLatest 从父会话最新完整段落边界创建独立子会话并切换到子会话
- `func (service *Service) forkSessionLocked(parentID string, request model.ForkRequest) (string, error)` — forkSessionLocked 在持有会话切换锁时执行 fork 落盘：解析切断点 → 构建
- `func deepCopyForkRecord(record model.SessionRecord) model.SessionRecord` — deepCopyForkRecord 深拷贝 fork 子会话 record：Conversation 消息（含

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
- `func TestForkSessionLatestResolvesNewestParagraph(t *testing.T)`
- `func TestForkCommandForksCurrentSession(t *testing.T)`
- `func TestForkSessionDeepCopyIsolation(t *testing.T)` — TestForkSessionDeepCopyIsolation（T2.7）：fork 子会话 record 与父数据面
- `func TestForkSessionRejectsRunningParent(t *testing.T)` — TestForkSessionRejectsRunningParent（UC5）：父会话运行中拒绝 fork。

### session_history.go

- `func (service *Service) resumeSession(sessionID string) error` — resumeSession 替换活跃引擎历史并恢复会话的 workspace 绑定，然后发布一份
- `func (service *Service) ResumeSession(sessionID string) error` — ResumeSession 是 GUI/TUI 会话选择的直接应用边界。它刻意绕过命令文本解析，
- `func (service *Service) LoadMoreHistory(limit int) error` — LoadMoreHistory 把更早的历史页前置到可见会话。
- `func adaptEngineMessage(msg EngineMessage) Message`
- `func isVisibleHistoryMessage(message EngineMessage) bool`

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
- `func (e *multiSessionEngine) SessionID() string`
- `func (e *multiSessionEngine) History() []EngineMessage`
- `func (e *multiSessionEngine) HistoryFor(sessionID string) []EngineMessage`
- `func (e *multiSessionEngine) ReplaceHistory(sessionID string, history []EngineMessage) error`
- `func (e *multiSessionEngine) ReplaceHistoryFor(sessionID string, history []EngineMessage) error` — ReplaceHistoryFor 是 SessionChatEngine 接口要求的会话内历史替换：替换
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

### session_pollution_s0_test.go

- `func TestS0BackgroundSessionTaskWriteMustNotPolluteActiveRegistry(t *testing.T)`

### session_race_test.go

- `func TestSnapshotBumpConcurrentWithRunChatTail(t *testing.T)` — TestSnapshotBumpConcurrentWithRunChatTail（TC-R-02）：并发 Submit（触发
- `func TestReleaseWorkingHistoryConcurrentWithChatStream(t *testing.T)` — TestReleaseWorkingHistoryConcurrentWithChatStream（TC-R-03）：收尾清工作

### session_resource_isolation_test.go

- `func TestSessionDomainsDisjoint(t *testing.T)` — TestSessionDomainsDisjoint（TC-INV-01）：A 与 B 的 M/X/R 状态域无共享，
- `func TestViewSwitchDoesNotMutateExecution(t *testing.T)` — TestViewSwitchDoesNotMutateExecution（TC-INV-02）：切到 B 只换视图指针，
- `func TestPersistReadsOnlyOwnDomain(t *testing.T)` — TestPersistReadsOnlyOwnDomain（TC-INV-03）：快照/活跃槽全是 B 时，

### session_scope.go

- `func (service *Service) sessionUnitLocked(sessionID string) *session.SessionUnit` — sessionUnitLocked 返回指定会话的会话单元（聊天运行态已收进 SessionUnit，
- `func (service *Service) anyChatRunningLocked() bool` — anyChatRunningLocked 报告是否存在任意会话的运行中聊天。M1 单飞执行
- `func (service *Service) mirrorActiveChatLocked()` — mirrorActiveChatLocked 把当前活跃会话的聊天运行态写入会话 view（阶段 1：
- `func queuedChatRequests(requests []session.QueuedRequest) []chatRequest` — queuedChatRequests 把会话域排队输入（不透明载荷）还原为执行内核的
- `func (service *Service) activeQueuedChatRequestsLocked() []chatRequest` — activeQueuedChatRequestsLocked 返回当前会话域的排队输入（还原为执行内核
- `func (service *Service) publishSessionEvent(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) event.Event` — publishSessionEvent 发布事件；装配的 EventHub 支持会话路由时携带
- `func (service *Service) publishChatStateFor(sessionID string)` — publishChatStateFor 下发指定会话的权威聊天运行态（chat.changed）。运行/排队
- `func (service *Service) bindProjectRootIfSafe(sessionID, rootPath string) bool` — bindProjectRootIfSafe 在安全条件下重绑全局项目根（P3/G5 收口）：
- `func (service *Service) SubmitToSession(ctx context.Context, sessionID, text string) error` — SubmitToSession 是会话级提交 API（M2：多会话并行执行）。目标会话即活跃
- `func (service *Service) sessionLoaded(sessionID string) bool` — sessionLoaded 报告目标会话引擎是否已实例化（后台提交前置检查）。
- `func (service *Service) ActivateSession(sessionID string) error` — ActivateSession 切换当前展示/执行会话。M1 没有每会话驻留快照，切换即
- `func (service *Service) SnapshotOf(sessionID string) (Snapshot, error)` — SnapshotOf 返回指定会话的权威快照：活跃会话直接返回 Snapshot()；其它
- `func (service *Service) sessionEventFilter(sessionID string) func(event.Event) bool` — sessionEventFilter 构造会话级订阅谓词（口径见 SubscribeSession）。
- `func (service *Service) SubscribeSessionWithReplay(sessionID string, buffer, replayWindow int) (Subscription, error)` — SubscribeSessionWithReplay 与 SubscribeSession 同一归属口径，但订阅附带
- `func (service *Service) SubscribeSession(sessionID string, buffer int) (Subscription, error)` — SubscribeSession 返回按会话过滤的事件订阅（只投递该会话或全局事件）。

### session_scope_test.go

- `func TestCrossSessionSubmitWhileRunningNoLongerBusy(t *testing.T)` — TestCrossSessionSubmitWhileRunningNoLongerBusy 验证 M2 多会话并行语义：
- `func TestSubmitToSessionDelegatesForActiveSession(t *testing.T)` — TestSubmitToSessionDelegatesForActiveSession 验证同会话提交走既有
- `func TestSubmitToSessionRejectsEmptyID(t *testing.T)` — TestSubmitToSessionRejectsEmptyID 验证会话级 API 拒绝空会话 ID。
- `func TestActivateSessionAllowedForIdleTargetWhileOtherRunning(t *testing.T)` — TestActivateSessionAllowedForIdleTargetWhileOtherRunning 验证 M2 会话级
- `func TestSnapshotOfOnlyActiveSessionAvailable(t *testing.T)` — TestSnapshotOfOnlyActiveSessionAvailable 验证 M1 快照粒度：仅活跃会话
- `func TestChatEventsCarrySessionID(t *testing.T)` — TestChatEventsCarrySessionID 验证 chat 生命周期事件携带会话路由键。
- `func TestSubscribeSessionFiltersBySessionID(t *testing.T)` — TestSubscribeSessionFiltersBySessionID 验证会话级订阅只投递目标会话

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
