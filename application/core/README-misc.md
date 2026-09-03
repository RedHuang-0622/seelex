# core/misc

## 生态位

基础与杂项（aliases/limits/completion/compressed/diagnostics/runtime/workspace/race）

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### aliases.go

- `func NewCommandRegistry() *CommandRegistry`
- `func newInputRouter(handlers inputRouteHandlers) *inputRouter`
- `func NewDefaultWindowPolicy(config WindowConfig) DefaultWindowPolicy`
- `func DefaultWindowConfig() WindowConfig`
- `func LoadWindowConfig(path string) (WindowConfig, error)`
- `func NewEventHub() *EventHub`
- `func NewApprovalBroker(events event.Hub) *ApprovalBroker`
- `func NewPromptStack() *PromptStack`
- `func NewEffortManager(stack *PromptStack, engine interface { SetMaxLoops(int) SetSystemPrompt(string) }) *EffortManager`
- `func cloneSnapshot(snapshot Snapshot) Snapshot`
- `func cloneRuntimeState(runtime RuntimeState) RuntimeState`
- `func maxLoopsFor(level string) int`
- `func reactBudgetFor(level string) prompt.ReActBudget`

### completion.go

- `func (service *Service) Suggestions(input string) []Suggestion`

### compressed_turn.go

- `func (a *CompressedTurnArchiver) StoreTurn(ctx context.Context, segmentID string, messages []types.Message) (string, error)` — StoreTurn 实现 seelexctx.TurnArchiver。会话归属优先取 ctx（runChat 注入
- `func (service *Service) ReadCompressedTurnHandler(_ context.Context, argsJSON string) (string, error)` — ReadCompressedTurnHandler 读回一次压缩的轮次原文（分页 + 过滤）。
- `func renderCompressedTurns(messages []types.Message) string` — renderCompressedTurns 把轮次原文渲染为可读文本（按角色标记，工具链
- `func pageCompressedTurns(rendered string, offset, limit int, contains string) (string, error)` — pageCompressedTurns 按 offset/limit（字符）/contains 过滤渲染文本分页。

### compressed_turn_test.go

- `func testStringPtr(value string) *string` — testStringPtr 返回字符串指针（测试消息正文）。
- `func (f *fakeCommitSession) SaveCommit(sessionID string, commit sessionstore.Commit) error`
- `func (f *fakeTranscriptSession) LoadTranscriptTailWorkspace(_, _ string, _, _ int) ([]TranscriptEvent, error)`
- `func (f *fakeTranscriptSession) LoadToolResultWorkspace(_, _, ref string) (StoredToolResult, error)`
- `func TestCompressedTurnArchiverPersistsOriginal(t *testing.T)` — TestCompressedTurnArchiverPersistsOriginal 写侧：溢出轮次原文序列化后
- `func TestCompressedTurnArchiverRoutesByContextSessionID(t *testing.T)` — TestCompressedTurnArchiverRoutesByContextSessionID 写侧归属（G0a 回归）：
- `func TestCompressedTurnArchiverFallsBackToProviderWithoutContext(t *testing.T)` — TestCompressedTurnArchiverFallsBackToProviderWithoutContext 无 ctx 注入时
- `func TestCompressedTurnArchiverRejectsWithoutSessionID(t *testing.T)` — TestCompressedTurnArchiverRejectsWithoutSessionID ctx 与 provider 都拿不到
- `func TestReadCompressedTurnHandlerReadsOriginal(t *testing.T)` — TestReadCompressedTurnHandlerReadsOriginal 读侧：read_compressed_turn

### diagnostics.go

- `func RenderDiag(snap Snapshot) string` — RenderDiag 构建诊断文本。由 /diag 命令调用。

### limits.go

- `func ApplyLimits(l seelexctx.Limits)` — ApplyLimits 应用 seele.yaml limits 段（零值字段自动补默认）；
- `func Limits() seelexctx.Limits` — Limits 返回当前生效的运行时上限（只读拷贝语义）。

### race_test.go

- `func TestEventHub_RacePublishSubscribe(t *testing.T)` — TestEventHub_RacePublishSubscribe 验证并发 Publish 与 Subscribe 不产生 data race。
- `func TestEventHub_RaceSubscribeClosePublish(t *testing.T)` — TestEventHub_RaceSubscribeClosePublish 验证 Subscribe/Close 与 Publish 并发安全。
- `func TestEventHub_RaceMultipleSubscribers(t *testing.T)` — TestEventHub_RaceMultipleSubscribers 验证多订阅者并发订阅/关闭。
- `func TestApprovalBroker_RaceConcurrentRequests(t *testing.T)` — TestApprovalBroker_RaceConcurrentRequests 验证并发 Request 安全。
- `func TestApprovalBroker_RaceRequestShutdown(t *testing.T)` — TestApprovalBroker_RaceRequestShutdown 验证 Shutdown 与 Request 并发安全。
- `func TestApprovalBroker_RaceTimeoutResolve(t *testing.T)` — TestApprovalBroker_RaceTimeoutResolve 验证 Timeout 与 Resolve 竞态。
- `func TestApprovalBroker_RaceDuplicateResolve(t *testing.T)` — TestApprovalBroker_RaceDuplicateResolve 验证重复 Resolve 返回错误（并发安全）。
- `func TestChat_RaceConcurrentSubmitChatRunning(t *testing.T)` — TestChat_RaceConcurrentSubmitChatRunning 验证并发 Submit 时 ErrChatRunning 和 InputQueue 竞态安全。
- `func (e *blockingEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (e *blockingEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 显式转发到自身 ChatStream（覆盖内嵌 fakeEngine 的提升方法，
- `func TestChat_RaceSnapshotDuringChat(t *testing.T)` — TestChat_RaceSnapshotDuringChat 验证 Chat 运行期间并发读取 Snapshot 无 data race。
- `func TestChat_RaceToolHandling(t *testing.T)` — TestChat_RaceToolHandling 验证 handleToolStart/Complete 与 Snapshot 并发安全。
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
- `func TestObserveInteraction_Race(t *testing.T)` — TestObserveInteraction_Race 验证 observeInteraction 并发安全。

### runtime_projection.go

- `func (service *Service) publishRuntimeProjections()` — publishRuntimeProjections 在 service.ViewMu 下拷贝应用自有状态，释放锁后发布
- `func latestVisibleUserGoal(messages []Message) string`
- `func truncateRuntimeProjectionGoal(content string) string`

### workspace_usecase.go

- `func (service *Service) DeleteSession(sessionID string) error`
- `func (service *Service) CreateWorkspace(name, rootPath, gitRemote string) error`
- `func (service *Service) BindWorkspace(workspaceID string) error`
- `func (service *Service) bindWorkspaceInfo(workspace WorkspaceInfo) error`
- `func (service *Service) UnbindWorkspace()`
- `func (service *Service) WorkspaceTree(relPath string, depth int) (dto.TreeListing, error)` — WorkspaceTree 列出当前工作区某目录的子条目（GUI 工作树数据源；root 只
- `func (service *Service) WorkspaceFileCount() (dto.TreeCount, error)` — WorkspaceFileCount 统计当前工作区文件/目录数（工作树文件数 badge 数据源）。
- `func (service *Service) WorkspaceGitLog(limit int) (dto.GitLogResult, error)` — WorkspaceGitLog 返回当前工作区最近 limit 条提交的拓扑树（GUI 提交记录树
- `func (service *Service) workspaceTreePort() (contract.WorkspaceTreePort, string, error)` — workspaceTreePort 读取当前工作区 root（锁内快照拷贝，锁外做文件 I/O）并
- `func (service *Service) collectWorkspaceProjection() workspaceStateProjection` — collectWorkspaceProjection 在获取 service.ViewMu 之前执行 WorkspacePort I/O。
- `func (service *Service) applyWorkspaceProjectionLocked(projection workspaceStateProjection)`
