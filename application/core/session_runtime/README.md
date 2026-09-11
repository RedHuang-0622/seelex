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
  生命周期（`StartCatalogRefresh`/`StopCatalogRefresh`）与刷新完成回执
  （`RequestCatalogRefresh` 返回的 channel）。
- 不做：会话切换编排、chat 流式、任务执行（task 域）。

## 关键文件

| 文件 | 职责 |
|---|---|
| `ports.go` | `TaskPersistencePort` 与可选能力断言端口、`Deps`。 |
| `coordinator.go` | `Coordinator` + `sessionRuntimeState` + catalog worker。 |
| `transition_manager.go` | per-session keyed 过渡锁注册表（G5：同会话串行、跨会话并行）。 |
| `archive.go` | record 构建/合并、transcript 事件关联、恢复历史回退。 |
| `scope.go` | 目录发现、标题恢复、会话定位、history 读。 |
| `history.go` | 尾部窗口读与 user 内容判定。 |
| `storage.go` | 会话存储设置。 |

## 核心实现与数据流

`PersistCurrentSession` 在锁外收集 task 快照 → 锁内构建 record（task 权威
状态经 `TaskPersistencePort` 读取）→ 锁外合并/写入 → 锁内清理已提交
tool-result。目录 worker 锁外做 SessionPort/WorkspacePort I/O，锁内只发布
内存态拷贝（bump → 锁外 Publish）。

`RequestCatalogRefresh()` 除唤醒外还返回**完成回执**（`<-chan struct{}`）：调用方
可选地等到"某一轮刷新在该请求登记之后开始并收尾"，回执即关闭。回执先登记再
唤醒，因此容量 1 的 wake channel 丢唤醒不会丢请求——正在排空批次的那一轮收尾时
会看到新登记的回执并再刷一轮。等待者不必自己轮询重试（根包
`Service.WaitCatalogRefresh(ctx)` 是它的带 ctx 包装）。`StopCatalogRefresh` 的退出
路径会释放全部在等回执并置停止标记，之后的请求立即收敛，关闭不会被目录 I/O 挂住。

目录三态（最近一轮枚举缓存、会话标题表、刷新回执队列）由 `catalogMu` 保护
（G5）：worker 在锁外做 SessionPort/WorkspacePort I/O，锁内只换内存态，
发布 Snapshot 镜像时另取 ViewMu 短临界区；锁序 ViewMu → catalogMu，不反向。
标题读写（`SessionTitleFor`/`SetSessionTitleLocked`）不再要求调用方持有
ViewMu。

## 依赖方向

依赖 `state.Core` + `TaskPersistencePort`（消费方接口）与装配注入的跨域纯
函数端口；禁止反向依赖 core 根包。

## 并发/安全语义

- 过渡互斥：`SessionTransitionManager`（`TransitionLock(key)`）——每个 key
  一把显式 actor（channel 命令 + 单 goroutine），同 key 串行、跨 key 并行；
  空 key 归一为视图保留 key（视图命令共用）。关闭后 Lock/Unlock 为空操作，
  退出路径不卡死。
- 目录缓存/标题表：`catalogMu`（见上）。持久化/目录/上下文装配的端口 I/O
  一律在锁外完成；持锁段只做内存态组装与拷贝。Locked 方法仍要求调用方持有
  ViewMu（视图锁），但不再为标题/目录持有。

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

- `func (c *Coordinator) PersistCurrentSession(location Location, sessionID string) error` — PersistCurrentSession 把指定会话原子落盘（阶段 0：显式 location 键 +
- `func (c *Coordinator) allTranscriptEventsForSession(location Location, sessionID string, memory []model.TranscriptEvent) []model.TranscriptEvent` — allTranscriptEventsForSession 返回会话全量事件：磁盘持久化事件（全量读）
- `func mergeTranscriptEventsBySeq(persisted, incoming []model.TranscriptEvent) []model.TranscriptEvent`
- `func enrichTranscriptMessageIDs(events []model.TranscriptEvent, record model.SessionRecord)` — enrichTranscriptMessageIDs 建立 event-to-message 关联（模块化方案 §3.2）：
- `func (c *Coordinator) sessionRecordLocked(sessionID string, tasks []dto.TaskRecord) model.SessionRecord`
- `func (c *Coordinator) archivedConversationMessageLocked(sessionID string, message model.Message) model.Message` — archivedConversationMessageLocked 归档单条可见消息：超限工具结果/用户
- `func (c *Coordinator) userInputResultRefLocked(sessionID, content string) string`
- `func (c *Coordinator) conversationFromTranscriptLocked(events []model.TranscriptEvent) []model.Message` — conversationFromTranscriptLocked 从指定会话 transcript 事件重建可见对话
- `func messageKindForEvent(event model.TranscriptEvent) string` — messageKindForEvent 把事件类别映射为可见消息的多线谱类别；旧数据 Kind 为空时
- `func (c *Coordinator) engineHistoryFor(sessionID string) []contract.EngineMessage` — engineHistoryFor 返回指定会话引擎历史（会话路由引擎用 HistoryFor；无会话
- `func (c *Coordinator) SessionRecordLocked(sessionID string, tasks []dto.TaskRecord) model.SessionRecord` — SessionRecordLocked 构建当前会话的归档 record（调用方持有 Core.ViewMu；
- `func (c *Coordinator) LoadSessionRecord(location Location, sessionID string) (model.SessionRecord, bool, error)` — LoadSessionRecord 读取会话归档 record（可选能力：无 record 端口或版本/
- `func (c *Coordinator) MarkSessionArchived(location Location, sessionID string) error` — MarkSessionArchived 把会话 record 的可见状态置为 archived（C2）。只改
- `func (c *Coordinator) LoadSessionTranscript(location Location, sessionID string) ([]model.TranscriptEvent, error)` — LoadSessionTranscript 读取会话 transcript 尾部窗口（预算 + 单元上限由
- `func recordResumeHistory(record model.SessionRecord) []contract.EngineMessage`
- `func RecordResumeHistory(record model.SessionRecord) []contract.EngineMessage` — RecordResumeHistory 是 durable-record 冷加载兜底（仅当 transcript 与可见
- `func (c *Coordinator) RecordConversation(record model.SessionRecord) []model.Message` — RecordConversation 返回去除内部消息后的可见会话消息（深拷贝 tool 引用）。
- `func isInternalConversationMessage(message model.Message, isInternalContent func(string) bool) bool`
- `func (c *Coordinator) RecordConversationResumeHistory(record model.SessionRecord, tokenBudget, maxUnits int) []contract.EngineMessage` — RecordConversationResumeHistory 是 durable-record 冷加载回退历史（transcript
- `func (c *Coordinator) RecordConversationTranscript(record model.SessionRecord) []model.TranscriptEvent` — RecordConversationTranscript 把可见会话消息重建为 provider transcript：role=tool 的调用消息还原为 assistant 工具链轮、tool_result 还原为工具输出，只有推理没有正文的助手步骤不产生空 assistant 事件。
- `func (c *Coordinator) RecordConversationTail(record model.SessionRecord, window int) []model.Message` — RecordConversationTail 返回可见会话的尾部窗口消息（含 window 上限）。
- `func CloneSessionPlanStack(stack []model.SessionPlanFrame) []model.SessionPlanFrame` — CloneSessionPlanStack 深拷贝会话 plan 栈（frame 内 Plan 单独克隆）。
- `func (c *Coordinator) RecordReadFileLocked(arguments string)` — RecordReadFileLocked 记录一次 read 工具的文件引用（会话归档 ReadFiles）。

### archive_test.go

- `func TestEnrichTranscriptMessageIDsPairsEventsToMessages(t *testing.T)` — TestEnrichTranscriptMessageIDsPairsEventsToMessages 验证 event-to-message
- `func TestEnrichTranscriptMessageIDsSkipsAmbiguousMultiCallEvents(t *testing.T)` — TestEnrichTranscriptMessageIDsSkipsAmbiguousMultiCallEvents 验证多工具调用

### coordinator.go

- `func NewCoordinator(deps Deps) *Coordinator` — NewCoordinator 构造会话域协调器；Tasks 由装配根注入
- `func (c *Coordinator) SessionTitleFor(sessionID string) model.SessionTitle` — SessionTitleFor 返回指定会话标题（G5：标题表由 catalogMu 保护，调用方
- `func (c *Coordinator) SetSessionTitleLocked(sessionID string, title model.SessionTitle)` — SetSessionTitleLocked 设置指定会话标题（标题表由 catalogMu 保护；调用方
- `func (c *Coordinator) UnloadSessionTitle(sessionID string)` — UnloadSessionTitle 释放指定会话的标题（阶段 2 生命周期：unload 后重开走
- `func (c *Coordinator) catalogTitleOf(sessionID string) model.SessionTitle` — catalogTitleOf 返回标题表原始值（不回退活跃会话名；存档 record 用——
- `func (c *Coordinator) CatalogCache() ([]model.SessionInfo, map[string]string)` — CatalogCache 返回目录 worker 最近一轮枚举结果的拷贝（catalogMu 保护；
- `func mergedCatalogLocked(grid map[string][]model.SessionInfo) []model.SessionInfo` — mergedCatalogLocked 从分格网格组合联合目录视图（调用方持 catalogMu）：
- `func catalogProjectOrder(grid map[string][]model.SessionInfo) []string` — catalogProjectOrder 返回网格的全部项目键（稳定顺序，避免测试/镜像抖动）。
- `func (c *Coordinator) TransitionLock(key string) sync.Locker` — TransitionLock 返回指定 key 的会话过渡互斥（key=会话 ID：该会话生命
- `func (c *Coordinator) BindView(view ViewPort)` — BindView 注入 Snapshot revision bump 端口（装配根在 view 构造完成后调用；
- `func (c *Coordinator) StartCatalogRefresh()` — StartCatalogRefresh 启动会话目录刷新 worker：目录发现与标题恢复离开
- `func (c *Coordinator) RequestCatalogRefresh() <-chan struct` — RequestCatalogRefresh 非阻塞唤醒目录刷新 worker，并返回完成回执：某一轮
- `func (c *Coordinator) RequestCatalogRefreshProject(projectID string) <-chan struct` — RequestCatalogRefreshProject 请求只刷新指定项目（projectID="" = 默认/
- `func (c *Coordinator) registerCatalogRefresh(projects map[string]struct{}) <-chan struct`
- `func (c *Coordinator) runCatalogPasses()` — runCatalogPasses 逐批排空回执：每批跑一轮刷新并在发布后关闭回执，批次为空才
- `func unionCatalogScope(batch []catalogWaiter) map[string]struct` — unionCatalogScope 合并一批回执的项目范围：任一请求覆盖全部项目（nil）
- `func (c *Coordinator) stopCatalogWaiters()` — stopCatalogWaiters 在 worker 退出路径上释放仍等待的回执：目录不再刷新，
- `func (c *Coordinator) StopCatalogRefresh()` — StopCatalogRefresh 关闭目录刷新 worker（有限等待，避免慢端口拖垮退出）。
- `func (c *Coordinator) CatalogRefreshDone() <-chan struct` — CatalogRefreshDone 返回目录 worker 退出信号（测试/生命周期钩子：worker
- `func (c *Coordinator) refreshCatalogProjects(scope map[string]struct{})` — refreshCatalogProjects 按项目范围刷新目录：scope == nil 表示全部项目
- `func (c *Coordinator) scopedProjectIDs(scope map[string]struct{}) []string` — scopedProjectIDs 返回本次刷新的项目集合：scope == nil → 全部已知项目；
- `func (c *Coordinator) publishCatalogMirror()` — publishCatalogMirror 把分格目录组合成联合镜像发布进内核（锁内 bump →

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
- `func (s *forkTestSessions) SaveSessionRecordWorkspace(string, string, model.SessionRecord) error`
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

### migration_test.go

- `func TestWorkspaceScopedDataReadableThroughSessionGranularStore(t *testing.T)` — TestWorkspaceScopedDataReadableThroughSessionGranularStore（迁移测试）：
- `func (s *granularPortTestSessions) SessionsOf(string) []model.SessionInfo`
- `func (s *granularPortTestSessions) LoadHistory(string) ([]contract.EngineMessage, error)`
- `func (s *granularPortTestSessions) LoadHistoryRange(sessionID string, offset, limit int) ([]contract.EngineMessage, int, error)`
- `func (s *granularPortTestSessions) Delete(string) error`
- `func TestLoadSessionHistoryPrefersSessionGranularPort(t *testing.T)` — TestLoadSessionHistoryPrefersSessionGranularPort 验证 9.3.2 迁移：

### scope.go

- `func (c *Coordinator) sessionCatalogProject(granular SessionGranularPort, projectID string) ([]model.SessionInfo, map[string]string)` — sessionCatalogProject 枚举单个项目的会话集合（G6：目录按 projectID 分格，
- `func (c *Coordinator) sessionNameFromTail(granular SessionGranularPort, sessionID string) string` — sessionNameFromTail 从会话历史尾部窗口提取标题（会话粒度端口；
- `func SessionTitleFromHistory(history []contract.EngineMessage, displayUserInput func(string) string) string` — SessionTitleFromHistory 从历史窗口内的首条可见 user 消息提取标题。
- `func SessionTitle(input string) string` — SessionTitle 从输入首行提取会话标题（>48 rune 截断）。
- `func (c *Coordinator) ShortSessionID(id string) string` — ShortSessionID 按 limits.session_name_runes 截断会话 ID 显示。
- `func (c *Coordinator) allProjectIDs() []string` — allProjectIDs 返回目录枚举的项目集合（空项目 = 当前 active scope + 全部
- `func (c *Coordinator) AllProjectIDs() []string` — AllProjectIDs 返回目录枚举的项目集合（公开观察面：冷启动草稿恢复需要跨
- `func (c *Coordinator) DraftCandidates() []model.SessionInfo` — DraftCandidates 返回目录里 status=draft 的会话候选（G：跨项目枚举，用于
- `func (c *Coordinator) LocateSession(sessionID string) Location` — LocateSession 定位会话（workspace 绑定优先；支持 scoped 读取时遍历全部
- `func preferSessionLocation(candidate, current Location, boundWorkspaceID string) bool`
- `func preferSessionInfo(candidate, current model.SessionInfo) bool`
- `func WorkspaceID(workspace *model.WorkspaceInfo) string` — WorkspaceID 返回工作区指针的 ID（nil → ""）。
- `func (c *Coordinator) LoadSessionHistory(location Location, sessionID string) ([]contract.EngineMessage, error)` — LoadSessionHistory 加载会话 provider 历史（scoped 端口优先；回退切换写
- `func (c *Coordinator) LoadSessionHistoryRange(workspaceID, sessionID string, offset, limit int) ([]contract.EngineMessage, int, error)` — LoadSessionHistoryRange 按偏移量窗口加载历史（scoped 端口优先）。
- `func (c *Coordinator) loadGranularRecord(sessionID string) (model.SessionRecord, bool, error)` — loadGranularRecord 读取会话 record（会话粒度；不存在返回 false）。

### storage.go

- `func (c *Coordinator) SessionStorageConfig() (sessionstore.Config, error)` — SessionStorageConfig 返回会话存储设置（可选能力：无 storage 端口报错）。
- `func (c *Coordinator) TestSessionStorage(ctx context.Context, config sessionstore.Config) error` — TestSessionStorage 验证会话存储配置可用性（可选能力）。
- `func (c *Coordinator) ConfigureSessionStorage(ctx context.Context, config sessionstore.Config) error` — ConfigureSessionStorage 应用会话存储配置并清空标题缓存（可选能力）。

### transcript_range.go

- `func (c *Coordinator) LoadTranscriptRange(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error)` — LoadTranscriptRange 按 Seq 区间（含端点）读回会话事件日志并转换为
- `func modelTranscriptEventsFromStore(events []sessionstore.Event) []model.TranscriptEvent` — modelTranscriptEventsFromStore 把存储层事件转换为应用层 transcript 事件

### transition_actor.go

- `func NewSessionTransitionActor() *SessionTransitionActor` — NewSessionTransitionActor 启动切换互斥 actor（单 goroutine）。
- `func (actor *SessionTransitionActor) loop()` — loop 是 actor 的唯一状态持有者：inFlight 与等待队列只在本 goroutine 内
- `func (actor *SessionTransitionActor) Acquire()` — Acquire 阻塞直到获得切换互斥（FIFO；Close 后退化为无操作）。
- `func (actor *SessionTransitionActor) Release()` — Release 释放切换互斥（未持有也可调用：空操作；Close 后同样安全）。
- `func (actor *SessionTransitionActor) Close()` — Close 停止 actor goroutine（幂等）。契约：调用方须保证无活跃持有者；
- `func (locker transitionLocker) Lock()`
- `func (locker transitionLocker) Unlock()`

### transition_actor_test.go

- `func TestTransitionActorSerializesConcurrentAcquire(t *testing.T)` — TestTransitionActorSerializesConcurrentAcquire（无锁化语义）：并发
- `func TestTransitionActorFIFOOrder(t *testing.T)` — TestTransitionActorFIFOOrder：等待者按请求顺序被授予（队列即等待队列）。
- `func TestTransitionActorCloseReleasesGoroutine(t *testing.T)` — TestTransitionActorCloseReleasesGoroutine：Close 停止 actor goroutine，
- `func TestTransitionLockerAdapter(t *testing.T)` — TestTransitionLockerAdapter：sync.Locker 适配层与现有调用方契约一致。

### transition_manager.go

- `func NewSessionTransitionManager() *SessionTransitionManager` — NewSessionTransitionManager 构造空的 per-session 过渡锁注册表。
- `func (manager *SessionTransitionManager) Lock(key string) sync.Locker` — Lock 返回 keyed locker：同 key 串行、跨 key 并行；关闭后为空操作。
- `func (manager *SessionTransitionManager) lock(key string)` — lock 阻塞直到获得指定 key 的过渡锁（actor 不存在时按需创建）。
- `func (manager *SessionTransitionManager) unlock(key string)` — unlock 释放指定 key 的过渡锁（未持有也可调用：空操作）。
- `func (manager *SessionTransitionManager) Close()` — Close 停止全部 per-key actor（幂等）。契约：调用方保证无活跃持有者；
- `func (locker keyedTransitionLocker) Lock()`
- `func (locker keyedTransitionLocker) Unlock()`

### transition_manager_test.go

- `func TestSessionTransitionManagerSerializesSameKey(t *testing.T)` — TestSessionTransitionManagerSerializesSameKey G5：同一 key 的命令串行
- `func TestSessionTransitionManagerParallelAcrossKeys(t *testing.T)` — TestSessionTransitionManagerParallelAcrossKeys G5：不同 key 的命令并行
- `func TestSessionTransitionManagerViewKeyAliasesEmpty(t *testing.T)` — TestSessionTransitionManagerViewKeyAliasesEmpty G5：空 key 与保留视图 key
- `func TestSessionTransitionManagerCloseReleasesWaiters(t *testing.T)` — TestSessionTransitionManagerCloseReleasesWaiters G5：关闭后释放全部等待者，
