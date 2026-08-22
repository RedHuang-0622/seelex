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

### session_history.go

- `func (service *Service) resumeSession(sessionID string) error` — resumeSession 替换活跃引擎历史并恢复会话的 workspace 绑定，然后发布一份
- `func (service *Service) ResumeSession(sessionID string) error` — ResumeSession 是 GUI/TUI 会话选择的直接应用边界。它刻意绕过命令文本解析，
- `func (service *Service) LoadMoreHistory(limit int) error` — LoadMoreHistory 把更早的历史页前置到可见会话。
- `func adaptEngineMessage(msg EngineMessage) Message`
- `func isVisibleHistoryMessage(message EngineMessage) bool`

### session_storage.go

- `func (service *Service) SessionStorageConfig() (sessionstore.Config, error)`
- `func (service *Service) TestSessionStorage(ctx context.Context, config sessionstore.Config) error`
- `func (service *Service) ConfigureSessionStorage(ctx context.Context, config sessionstore.Config) error`
