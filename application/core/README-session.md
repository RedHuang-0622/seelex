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
- `func TestPersistSessionRecordMergesBoundedProjectionWithFullHistory(t *testing.T)`
- `func TestSessionRecordStoresLargeContentByReference(t *testing.T)`
- `func TestCompletedTaskClearsTaskScopedSkillsBeforeNextRequest(t *testing.T)`
- `func TestToolResultPaginationMakesProgressAcrossUTF8Boundaries(t *testing.T)`
- `func TestLoadedPlanIsAppendedToSessionPlanStack(t *testing.T)`
- `func TestResumeSessionUsesRecordWhenProviderHistoryIsUnavailable(t *testing.T)`
- `func TestResumeSessionContinuationKeepsTranscriptHistory(t *testing.T)`
- `func TestResumeSessionContinuationKeepsTrailingUnansweredUserInput(t *testing.T)`
- `func TestProviderRepairNoteNeverBecomesVisibleAssistantText(t *testing.T)`

### session_draft.go

- `func (service *Service) BeginNewSession() error` — BeginNewSession 进入幂等、未持久化的草稿状态。引擎会话只在第一条真实
- `func (service *Service) materializeDraftSession(firstQuestion string) error` — materializeDraftSession 为首条请求创建引擎会话与项目绑定。调用方必须持有

### session_fork.go

- `func (service *Service) ForkSession(parentID string, request model.ForkRequest) (string, error)` — ForkSession 从父会话的指定切断点创建独立子会话，并切换到子会话继续。
- `func (service *Service) ForkSessionLatest(parentID string) (string, error)` — ForkSessionLatest 从父会话最新完整段落边界创建独立子会话并切换到子会话
- `func (service *Service) forkSessionLocked(parentID string, request model.ForkRequest) (string, error)` — forkSessionLocked 在持有会话切换锁时执行 fork 落盘：解析切断点 → 构建

### session_fork_test.go

- `func (s *forkServiceSessions) LoadSessionRecord(string) (SessionRecord, error)`
- `func (s *forkServiceSessions) LoadSessionRecordWorkspace(_, sessionID string) (SessionRecord, error)`
- `func (s *forkServiceSessions) SaveSessionRecord(string, SessionRecord) error`
- `func (s *forkServiceSessions) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error)`
- `func (s *forkServiceSessions) LoadToolResultsWorkspace(projectID, sessionID string) ([]sessionstore.ToolResult, error)`
- `func (s *forkServiceSessions) SaveSessionSnapshotWorkspace(projectID, sessionID string, history []EngineMessage, record SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult) error`
- `func (s *forkServiceSessions) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error)`
- `func (s *forkServiceSessions) SaveContextStateWorkspace(projectID, sessionID string, payload []byte) error`
- `func (s *forkServiceSessions) CurrentGenerationWorkspace(projectID, sessionID string) (string, error)`
- `func TestForkSessionCreatesAndSwitchesToChild(t *testing.T)`
- `func TestForkSessionLatestResolvesNewestParagraph(t *testing.T)`
- `func TestForkCommandForksCurrentSession(t *testing.T)`

### session_history.go

- `func (service *Service) resumeSession(sessionID string) error` — resumeSession 替换活跃引擎历史并恢复会话的 workspace 绑定，然后发布一份
- `func (service *Service) ResumeSession(sessionID string) error` — ResumeSession 是 GUI/TUI 会话选择的直接应用边界。它刻意绕过命令文本解析，
- `func (service *Service) LoadMoreHistory(limit int) error` — LoadMoreHistory 把更早的历史页前置到可见会话。
- `func adaptEngineMessage(msg EngineMessage) Message`
- `func isVisibleHistoryMessage(message EngineMessage) bool`

### session_scope.go

- `func (service *Service) sessionChatLocked(sessionID string) *sessionChatRuntime` — sessionChatLocked 返回指定会话的聊天运行态（按需创建）。调用方必须
- `func (service *Service) anyChatRunningLocked() bool` — anyChatRunningLocked 报告是否存在任意会话的运行中聊天。M1 单飞执行
- `func (service *Service) mirrorActiveChatLocked()` — mirrorActiveChatLocked 把当前活跃会话的聊天运行态镜像到权威 Snapshot
- `func (service *Service) publishSessionEvent(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) event.Event` — publishSessionEvent 发布事件；装配的 EventHub 支持会话路由时携带
- `func (service *Service) SubmitToSession(ctx context.Context, sessionID, text string) error` — SubmitToSession 是会话级提交 API（M1）：目标会话即活跃会话时等价
- `func (service *Service) ActivateSession(sessionID string) error` — ActivateSession 切换当前展示/执行会话。M1 没有每会话驻留快照，切换即
- `func (service *Service) SnapshotOf(sessionID string) (Snapshot, error)` — SnapshotOf 返回指定会话的权威快照。M1 只有活跃会话有驻留快照，其它
- `func (service *Service) SubscribeSession(sessionID string, buffer int) (Subscription, error)` — SubscribeSession 返回按会话过滤的事件订阅（只投递该会话或全局事件）。

### session_scope_test.go

- `func TestCrossSessionSubmitWhileRunningReturnsSessionBusy(t *testing.T)` — TestCrossSessionSubmitWhileRunningReturnsSessionBusy 验证 M1 单飞执行
- `func TestSubmitToSessionDelegatesForActiveSession(t *testing.T)` — TestSubmitToSessionDelegatesForActiveSession 验证同会话提交走既有
- `func TestSubmitToSessionRejectsEmptyID(t *testing.T)` — TestSubmitToSessionRejectsEmptyID 验证会话级 API 拒绝空会话 ID。
- `func TestActivateSessionRejectedWhileRunning(t *testing.T)` — TestActivateSessionRejectedWhileRunning 验证运行中切换会话被拒绝
- `func TestSnapshotOfOnlyActiveSessionAvailable(t *testing.T)` — TestSnapshotOfOnlyActiveSessionAvailable 验证 M1 快照粒度：仅活跃会话
- `func TestChatEventsCarrySessionID(t *testing.T)` — TestChatEventsCarrySessionID 验证 chat 生命周期事件携带会话路由键。
- `func TestSubscribeSessionFiltersBySessionID(t *testing.T)` — TestSubscribeSessionFiltersBySessionID 验证会话级订阅只投递目标会话

### session_storage.go

- `func (service *Service) SessionStorageConfig() (sessionstore.Config, error)`
- `func (service *Service) TestSessionStorage(ctx context.Context, config sessionstore.Config) error`
- `func (service *Service) ConfigureSessionStorage(ctx context.Context, config sessionstore.Config) error`
