# session_runtime

## 生态位

会话域协调器：会话持久化（SessionRecord v3 / transcript / tool-result 原子
提交）、目录与标题恢复、项目 binding、storage 设置与会话三读
（record/history/transcript）。根包 Service 负责跨域事务编排
（BeginNewSession/ResumeSession/BindWorkspace），本包只提供原子域操作。

## 职责与非职责

- 做：`PersistCurrentSession`、`SessionRecordLocked`、`LoadSessionRecord`/
  `LoadSessionTranscript`/`LoadHistoryTailWindow`、目录发现与标题缓存、
  `LocateSession`、storage 设置、`RecordReadFileLocked`、catalog worker
  生命周期（`StartCatalogRefresh`/`StopCatalogRefresh`）。
- 不做：会话切换编排、chat 流式、任务执行（task 域）。

## 关键文件

| 文件 | 职责 |
|---|---|
| `ports.go` | `TaskPersistencePort` 与可选能力断言端口、`Deps`。 |
| `coordinator.go` | `Coordinator` + `sessionRuntimeState` + catalog worker。 |
| `archive.go` | record 构建/合并、transcript 事件关联、恢复历史回退。 |
| `scope.go` | 目录发现、标题恢复、会话定位、history 读。 |
| `history.go` | 尾部窗口读与 user 内容判定。 |
| `storage.go` | 会话存储设置。 |

## 核心实现与数据流

`PersistCurrentSession` 在锁外收集 task 快照 → 锁内构建 record（task 权威
状态经 `TaskPersistencePort` 读取）→ 锁外合并/写入 → 锁内清理已提交
tool-result。目录 worker 锁外做 SessionPort/WorkspacePort I/O，锁内只发布
内存态拷贝（bump → 锁外 Publish）。

## 依赖方向

依赖 `state.Core` + `TaskPersistencePort`（消费方接口）与装配注入的跨域纯
函数端口；禁止反向依赖 core 根包。

## 并发/安全语义

`sessionNameMu`/`sessionTransitionMu` 自持；`TransitionLock()` 供根包跨域
事务共用。Locked 方法要求调用方持有 `Core.Mu`。持锁禁止调用外部端口。

## 扩展与 Review

新增存储后端实现 `contract.SessionPort` 并可选实现本包端口。Review 重点：
会话持久化必须看到 task/plan 权威状态（端口而非拷贝）、catalog 刷新不得
阻塞 GUI 关闭、切项目不得串写历史。

## 测试

```text
go test ./application/core/session_runtime -count=1
```

根包 `service_test.go` session 组与 `session_archive_test.go` 覆盖跨域集成。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### archive.go

- `func (c *Coordinator) PersistCurrentSession(sessionID string) error` — PersistCurrentSession 把当前会话原子落盘：task 快照锁外收集（外部端口），
- `func enrichTranscriptMessageIDs(events []model.TranscriptEvent, record model.SessionRecord)` — enrichTranscriptMessageIDs 建立 event-to-message 关联（模块化方案 §3.2）：
- `func (c *Coordinator) mergeConversationMessages(existing, projected []model.Message) []model.Message`
- `func (c *Coordinator) sessionRecordLocked(sessionID string, tasks []dto.TaskRecord) model.SessionRecord`
- `func (c *Coordinator) SessionRecordLocked(sessionID string, tasks []dto.TaskRecord) model.SessionRecord` — SessionRecordLocked 构建当前会话的归档 record（调用方持有 Core.Mu；
- `func (c *Coordinator) archivedConversationMessageLocked(message model.Message) model.Message`
- `func (c *Coordinator) userInputResultRefLocked(content string) string`
- `func (c *Coordinator) LoadSessionRecord(location Location, sessionID string) (model.SessionRecord, bool, error)` — LoadSessionRecord 读取会话归档 record（可选能力：无 record 端口或版本/
- `func (c *Coordinator) LoadSessionTranscript(location Location, sessionID string) ([]model.TranscriptEvent, error)` — LoadSessionTranscript 读取会话 transcript 尾部窗口（预算 + 单元上限由
- `func recordResumeHistory(record model.SessionRecord) []contract.EngineMessage`
- `func RecordResumeHistory(record model.SessionRecord) []contract.EngineMessage` — RecordResumeHistory 是 durable-record 冷加载兜底（仅当 transcript 与可见
- `func (c *Coordinator) RecordConversation(record model.SessionRecord) []model.Message` — RecordConversation 返回去除内部消息后的可见会话消息（深拷贝 tool 引用）。
- `func isInternalConversationMessage(message model.Message, isInternalContent func(string) bool) bool`
- `func (c *Coordinator) RecordConversationResumeHistory(record model.SessionRecord, tokenBudget, maxUnits int) []contract.EngineMessage` — RecordConversationResumeHistory 是 durable-record 冷加载回退历史（transcript
- `func (c *Coordinator) RecordConversationTranscript(record model.SessionRecord) []model.TranscriptEvent`
- `func (c *Coordinator) RecordConversationTail(record model.SessionRecord, window int) []model.Message` — RecordConversationTail 返回可见会话的尾部窗口消息（含 window 上限）。
- `func CloneSessionPlanStack(stack []model.SessionPlanFrame) []model.SessionPlanFrame` — CloneSessionPlanStack 深拷贝会话 plan 栈（frame 内 Plan 单独克隆）。
- `func (c *Coordinator) RecordReadFileLocked(arguments string)` — RecordReadFileLocked 记录一次 read 工具的文件引用（会话归档 ReadFiles）。

### archive_test.go

- `func TestEnrichTranscriptMessageIDsPairsEventsToMessages(t *testing.T)` — TestEnrichTranscriptMessageIDsPairsEventsToMessages 验证 event-to-message
- `func TestEnrichTranscriptMessageIDsSkipsAmbiguousMultiCallEvents(t *testing.T)` — TestEnrichTranscriptMessageIDsSkipsAmbiguousMultiCallEvents 验证多工具调用

### coordinator.go

- `func NewCoordinator(deps Deps) *Coordinator` — NewCoordinator 构造会话域协调器；Tasks 由装配根注入
- `func (c *Coordinator) SessionTitle() model.SessionTitle` — SessionTitle 返回会话标题（调用方持有 Core.Mu）。
- `func (c *Coordinator) SetSessionTitleLocked(title model.SessionTitle)` — SetSessionTitleLocked 设置会话标题（调用方持有 Core.Mu）。
- `func (c *Coordinator) TransitionLock() sync.Locker` — TransitionLock 返回会话切换互斥锁（BeginNewSession/ResumeSession/
- `func (c *Coordinator) BindView(view ViewPort)` — BindView 注入 Snapshot revision bump 端口（装配根在 view 构造完成后调用；
- `func (c *Coordinator) StartCatalogRefresh()` — StartCatalogRefresh 启动会话目录刷新 worker：目录发现与标题恢复离开
- `func (c *Coordinator) RequestCatalogRefresh()` — RequestCatalogRefresh 非阻塞唤醒目录刷新 worker。
- `func (c *Coordinator) StopCatalogRefresh()` — StopCatalogRefresh 关闭目录刷新 worker（有限等待，避免慢端口拖垮退出）。
- `func (c *Coordinator) CatalogRefreshDone() <-chan struct` — CatalogRefreshDone 返回目录 worker 退出信号（测试/生命周期钩子：worker
- `func (c *Coordinator) refreshCatalogCache()` — refreshCatalogCache 把目录快照发布进内核（锁内 bump → 锁外 Publish）。

### fork.go

- `func (c *Coordinator) PrepareFork(location Location, childID, parentID string, request model.ForkRequest) (ForkContext, error)` — PrepareFork 基于父会话的已发布快照构建子会话深拷贝（一期决策契约）：
- `func (c *Coordinator) LatestForkCut(location Location, parentID string) (uint64, error)` — LatestForkCut 返回父会话最新完整段落边界（最后一个完整轮次的 EventSeq，
- `func forkTranscriptEvents(events []sessionstore.Event) []model.TranscriptEvent`
- `func forkStoredToolResults(results []sessionstore.ToolResult) []model.StoredToolResult`
- `func resolveForkCut(events []sessionstore.Event, request model.ForkRequest) (uint64, *sessionstore.Event, error)` — resolveForkCut 解析 fork 切断点（EventSeq 含端点，段落边界语义）。
- `func eventAt(events []sessionstore.Event, seq uint64) *sessionstore.Event`
- `func forkInheritedEvents(events []sessionstore.Event, cut uint64) []sessionstore.Event`
- `func truncateForkRecord(record model.SessionRecord, events []sessionstore.Event, frames []sessionstore.CompactFrame, cutTime time.Time, displayUserInput func(string) string) model.SessionRecord` — truncateForkRecord 把父 SessionRecord 截断到 fork 时刻：Conversation/
- `func forkCutSeq(events []sessionstore.Event) uint64`
- `func inheritedForkTitle(title model.SessionTitle, messages []model.Message, now time.Time, displayUserInput func(string) string) model.SessionTitle`
- `func forkConversationMessages(messages []model.Message, events []sessionstore.Event, cutTime time.Time) []model.Message`
- `func forkPlanFramesByTime(frames []model.SessionPlanFrame, cutTime time.Time) []model.SessionPlanFrame`
- `func hasPlanFrame(frames []model.SessionPlanFrame, planID string) bool`
- `func forkTaskRecordsByTime(tasks []dto.TaskRecord, cutTime time.Time) []dto.TaskRecord` — forkTaskRecordsByTime 按切断时间截断父 task 注册表，并丢弃 kind=todo
- `func forkCheckpoints(checkpoints []model.TaskCheckpoint, cut uint64) []model.TaskCheckpoint`
- `func forkProjection(projection *model.TaskContextProjection, cut uint64, sessionID string) *model.TaskContextProjection`
- `func forkReadFiles(files []model.ReadFileRef, cutTime time.Time) []model.ReadFileRef`
- `func reachableToolResultRefs(events []sessionstore.Event, record model.SessionRecord, frames []sessionstore.CompactFrame) map[string]struct` — reachableToolResultRefs 汇总子会话可达的 tool-result ref：继承事件流的
- `func forkToolResultRegistry(refs []model.ToolResultRef, reachable map[string]struct{}) []model.ToolResultRef`
- `func (c *Coordinator) forkContextRecord(location Location, parentID string, cut uint64, cutTime time.Time, cutMessageID string) ([]byte, []sessionstore.CompactFrame, error)` — forkContextRecord 重写父 context 四栈为子会话独立栈起点：Plan/Task/Skill
- `func forkContextPlanFrames(frames []sessionstore.PlanFrame, cutTime time.Time) []sessionstore.PlanFrame`
- `func forkContextTaskFrames(frames []sessionstore.TaskFrame, cutTime time.Time) []sessionstore.TaskFrame`
- `func forkContextSkillFrames(frames []sessionstore.SkillFrame, cutTime time.Time) []sessionstore.SkillFrame`
- `func rewriteForkCompactStack(frames []sessionstore.CompactFrame, cut uint64, cutMessageID string) []sessionstore.CompactFrame` — rewriteForkCompactStack 处理压缩帧内的 fork 切断：整帧继承 + 范围重写

### fork_test.go

- `func (s *forkTestSessions) SaveCurrent(string) error`
- `func (s *forkTestSessions) Delete(string) error`
- `func (s *forkTestSessions) List() []model.SessionInfo`
- `func (s *forkTestSessions) LoadHistory(string) ([]contract.EngineMessage, error)`
- `func (s *forkTestSessions) LoadHistoryRange(string, int, int) ([]contract.EngineMessage, int, error)`
- `func (s *forkTestSessions) SetWorkspace(string)`
- `func (s *forkTestSessions) Workspace() string`
- `func (s *forkTestSessions) SaveSessionRecord(string, model.SessionRecord) error`
- `func (s *forkTestSessions) LoadSessionRecord(string) (model.SessionRecord, error)`
- `func (s *forkTestSessions) LoadSessionRecordWorkspace(projectID, sessionID string) (model.SessionRecord, error)`
- `func (s *forkTestSessions) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error)`
- `func (s *forkTestSessions) LoadToolResultsWorkspace(projectID, sessionID string) ([]sessionstore.ToolResult, error)`
- `func (s *forkTestSessions) SaveSessionSnapshotWorkspace(projectID, sessionID string, history []contract.EngineMessage, record model.SessionRecord, events []model.TranscriptEvent, results []model.StoredToolResult) error`
- `func (s *forkTestSessions) LoadContextStateWorkspace(projectID, sessionID string) ([]byte, error)`
- `func (s *forkTestSessions) SaveContextStateWorkspace(projectID, sessionID string, payload []byte) error`
- `func (s *forkTestSessions) CurrentGenerationWorkspace(projectID, sessionID string) (string, error)`
- `func newForkTestCoordinator(t *testing.T, sessions contract.SessionPort) *Coordinator`
- `func forkTestFixture() (*forkTestSessions, time.Time)`
- `func TestPrepareForkTruncatesToRequestBoundary(t *testing.T)`
- `func TestPrepareForkRejectsInvalidCutPoints(t *testing.T)`
- `func TestPrepareForkAtStartProducesEmptyChild(t *testing.T)`
- `func TestForkTaskRecordsFiltersTodolist(t *testing.T)`

### history.go

- `func (c *Coordinator) LoadHistoryTailWindow(location Location) ([]contract.EngineMessage, int, error)` — LoadHistoryTailWindow 尾部窗口读：先探总数（limit=0 只读 manifest），
- `func (c *Coordinator) LatestUserContent(messages []model.Message) string` — LatestUserContent 返回可见会话中最后一条 user 消息内容（内部标记跳过）。
- `func (c *Coordinator) HistoryContainsUser(history []contract.EngineMessage, content string) bool` — HistoryContainsUser 判定 provider 历史中是否存在指定 user 内容。
- `func (c *Coordinator) TranscriptContainsUser(events []model.TranscriptEvent, content string) bool` — TranscriptContainsUser 判定 transcript 事件中是否存在指定 user 内容。

### scope.go

- `func (c *Coordinator) sessionCatalog() ([]model.SessionInfo, map[string]string)` — sessionCatalog 返回可见会话列表与工作区绑定发现结果。
- `func (c *Coordinator) sessionName(location Location, scoped ScopedSessionPort) string`
- `func (c *Coordinator) sessionNameFromTail(location Location, scoped ScopedSessionPort) string`
- `func (c *Coordinator) InvalidateSessionName(sessionID string)` — InvalidateSessionName 删除会话标题缓存（删除/重命名后立即失效）。
- `func (c *Coordinator) clearSessionNames()`
- `func SessionTitleFromHistory(history []contract.EngineMessage, displayUserInput func(string) string) string` — SessionTitleFromHistory 从历史窗口内的首条可见 user 消息提取标题。
- `func SessionTitle(input string) string` — SessionTitle 从输入首行提取会话标题（>48 rune 截断）。
- `func (c *Coordinator) ShortSessionID(id string) string` — ShortSessionID 按 limits.session_name_runes 截断会话 ID 显示。
- `func (c *Coordinator) allSessionLocations(scoped ScopedSessionPort) []Location`
- `func (c *Coordinator) LocateSession(sessionID string) Location` — LocateSession 定位会话（workspace 绑定优先；支持 scoped 读取时遍历全部
- `func preferSessionLocation(candidate, current Location, boundWorkspaceID string) bool`
- `func WorkspaceID(workspace *model.WorkspaceInfo) string` — WorkspaceID 返回工作区指针的 ID（nil → ""）。
- `func (c *Coordinator) LoadSessionHistory(location Location, sessionID string) ([]contract.EngineMessage, error)` — LoadSessionHistory 加载会话 provider 历史（scoped 端口优先；回退切换写
- `func (c *Coordinator) LoadSessionHistoryRange(workspaceID, sessionID string, offset, limit int) ([]contract.EngineMessage, int, error)` — LoadSessionHistoryRange 按偏移量窗口加载历史（scoped 端口优先）。

### storage.go

- `func (c *Coordinator) SessionStorageConfig() (sessionstore.Config, error)` — SessionStorageConfig 返回会话存储设置（可选能力：无 storage 端口报错）。
- `func (c *Coordinator) TestSessionStorage(ctx context.Context, config sessionstore.Config) error` — TestSessionStorage 验证会话存储配置可用性（可选能力）。
- `func (c *Coordinator) ConfigureSessionStorage(ctx context.Context, config sessionstore.Config) error` — ConfigureSessionStorage 应用会话存储配置并清空标题缓存（可选能力）。

