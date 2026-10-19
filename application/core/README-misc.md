# core/misc（根包分卷）

## 生态位

基础与杂项（vocabulary/assembly_helpers/limits/completion/compressed/diagnostics/fault-guard/perf/snapshot-budget/runtime/workspace/race）

覆盖：显式名单 27 个文件（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### assembly_helpers.go

- `func NewCommandRegistry() *input_router.CommandRegistry`
- `func newInputRouter(handlers inputRouteHandlers) *inputRouter`
- `func NewDefaultWindowPolicy(config cc.WindowConfig) cc.DefaultWindowPolicy`
- `func DefaultWindowConfig() cc.WindowConfig`
- `func LoadWindowConfig(path string) (cc.WindowConfig, error)`
- `func NewEventHub() *event.EventHub`
- `func NewApprovalBroker(events event.Hub) *limitapproval.ApprovalBroker`
- `func NewPromptStack() *prompt.PromptStack`
- `func NewEffortManager(stack *prompt.PromptStack, engine interface { SetMaxLoops(int) SetSystemPrompt(string) }) *prompt.EffortManager`
- `func cloneSnapshot(snapshot model.Snapshot) model.Snapshot`
- `func cloneRuntimeState(runtime model.RuntimeState) model.RuntimeState`
- `func maxLoopsFor(level string) int`
- `func reasoningEffortFor(level string) string`
- `func reactBudgetFor(level string) prompt.ReActBudget`

### completion.go

- `func (service *Service) Suggestions(input string) []Suggestion` — Suggestions 按输入前缀给出候选：前缀不认识、或已经进入参数区（含空格）时
- `func splitSigil(input string) (trigger, prefix string, ok bool)` — splitSigil 拆出输入前缀与它后面的文本（前缀不认识时 ok=false）。
- `func HasSigilPrefix(input string) bool` — HasSigilPrefix 报告输入是否以前缀开头（TUI 的 suggMode 与前端内联建议共用同一条
- `func SigilOf(input string) string` — SigilOf 返回输入的首字符前缀；不可解析时返回空串（调用方自行给默认值）。
- `func (service *Service) sigilMigrationHint(used, name string) string` — sigilMigrationHint 在前缀没命中时给一句跨域迁移提示：命中**别的**域才给，
- `func (service *Service) hasPlugin(name string) bool` — hasPlugin 报告名字是否命中已加载插件（PluginPort 没有按名查询，只能扫列表）。
- `func (service *Service) unknownCommandNotice(name string) string` — unknownCommandNotice 报告未知命令，并尽量给一条能走下去的提示。
- `func (service *Service) visibleToolNamed(name string) bool` — visibleToolNamed 报告名字是否命中当前可见工具（大小写不敏感）。
- `func (service *Service) commandCounterpart(name string) string` — commandCounterpart 找与用户输入最像的命令名：前缀/包含关系优先，其次编辑距离
- `func editDistanceAtMost(a, b string, limit int) bool` — editDistanceAtMost 判定两串的 Levenshtein 距离是否 ≤ limit（有界实现：只保留
- `func (service *Service) commandSuggestions() []Suggestion`
- `func (service *Service) skillSuggestions() []Suggestion`
- `func (service *Service) pluginSuggestions() []Suggestion` — pluginSuggestions 列出可切换插件；off 是"停用全部"的内置入口（与

### completion_test.go

- `func TestSuggestionsExcludeModelSideTools(t *testing.T)` — TestSuggestionsExcludeModelSideTools：`/` 只列"能从输入框直接执行"的入口——

### compressed_turn.go

- `func (a *CompressedTurnArchiver) resolveProjectID(sessionID string) string` — resolveProjectID 解析落盘的项目作用域：先按会话自己的绑定（数据实际所在），
- `func (a *CompressedTurnArchiver) StoreTurn(ctx context.Context, segmentID string, messages []types.Message) (string, error)` — StoreTurn 实现 seelexctx.TurnArchiver。会话归属优先取 ctx（runChat 注入
- `func (service *Service) ReadCompressedTurnHandler(_ context.Context, argsJSON string) (string, error)` — ReadCompressedTurnHandler 读回一次压缩的轮次原文（分页 + 过滤）。
- `func renderCompressedTurns(messages []types.Message) string` — renderCompressedTurns 把轮次原文渲染为可读文本（按角色标记，工具链
- `func pageCompressedTurns(rendered string, offset, limit int, contains string) (string, error)` — pageCompressedTurns 按 offset/limit（字符）/contains 过滤渲染文本分页。

### compressed_turn_test.go

- `func testStringPtr(value string) *string` — testStringPtr 返回字符串指针（测试消息正文）。
- `func (f *fakeCommitSession) SaveCommitWorkspace(projectID, sessionID string, commit store.Commit) error`
- `func TestCompressedTurnArchiverScopesCommitToSessionProject(t *testing.T)` — TestCompressedTurnArchiverScopesCommitToSessionProject 写侧项目作用域：先按
- `func (f *fakeTranscriptSession) LoadTranscriptTailWorkspace(_, _ string, _, _ int) ([]model.TranscriptEvent, error)`
- `func (f *fakeTranscriptSession) LoadToolResultWorkspace(_, _, ref string) (model.StoredToolResult, error)`
- `func TestCompressedTurnArchiverPersistsOriginal(t *testing.T)` — TestCompressedTurnArchiverPersistsOriginal 写侧：溢出轮次原文序列化后
- `func TestCompressedTurnArchiverRoutesByContextSessionID(t *testing.T)` — TestCompressedTurnArchiverRoutesByContextSessionID 写侧归属（G0a 回归）：
- `func TestCompressedTurnArchiverFallsBackToProviderWithoutContext(t *testing.T)` — TestCompressedTurnArchiverFallsBackToProviderWithoutContext 无 ctx 注入时
- `func TestCompressedTurnArchiverRejectsWithoutSessionID(t *testing.T)` — TestCompressedTurnArchiverRejectsWithoutSessionID ctx 与 provider 都拿不到
- `func TestReadCompressedTurnHandlerReadsOriginal(t *testing.T)` — TestReadCompressedTurnHandlerReadsOriginal 读侧：read_compressed_turn

### diagnostics.go

- `func RenderDiag(snap model.Snapshot) string` — RenderDiag 构建诊断文本。由 /diag 命令调用。

### fault_guard.go

- `func (service *Service) degradeAndRequestExit(reason string)` — degradeAndRequestExit 进入降级退出路径（幂等）。调用方不得持有任何锁。
- `func (service *Service) handleLifecycleFault(recovered any)` — handleLifecycleFault 处理生命周期消费者中的并发/逻辑故障：先降级退出，
- `func logPanicReason(recovered any) string`

### fault_guard_test.go

- `func TestDegradeAndRequestExitNotifiesHost(t *testing.T)` — TestDegradeAndRequestExitNotifiesHost：降级退出必须幂等、标记 closed/
- `func TestLifecycleFaultPanicsAfterDegrade(t *testing.T)` — TestLifecycleFaultPanicsAfterDegrade：生命周期消费者中的故障不再被静默

### limits.go

- `func ApplyLimits(l limit.Limits)` — ApplyLimits 应用 seele.yaml limits 段（零值字段自动补默认）；
- `func Limits() limit.Limits` — Limits 返回当前生效的运行时上限（只读拷贝语义）。
- `func workTableBounds() worktable.Limits` — workTableBounds 把进程级上限里**工作表格投影**用到的那三格按值取出，交给中间层的
- `func ApplySessionReleaseWindow(window int) (int, error)` — ApplySessionReleaseWindow 校验并**立刻生效**一个「会话驻留窗口」，返回真正生效的

### perf_stats.go

- `func (service *Service) PerfStats() model.PerfStats` — PerfStats 返回 GUI 性能追踪钩子的后端数据面：只上报数量/体积等无内容

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

### resident_lru.go

- `func (service *Service) touchResident(sessionID string)` — touchResident 记录一次会话引擎使用（冷加载/热切换/物化），并触发超限
- `func (service *Service) markResidentRecentLocked(sessionID string, resident bool)` — markResidentRecentLocked 把会话移到 LRU 使用序最前并标记驻留（调用方持
- `func (service *Service) reconcileResidentLimit()` — reconcileResidentLimit 按释放面口径收敛驻留引擎（引擎层）：受保护集（当前视图 /
- `func (service *Service) residentEvictionSnapshotLocked() residency.EvictionSnapshot` — residentEvictionSnapshotLocked 在同一临界区内取齐一次收敛读数（调用方持有
- `func (service *Service) sessionBusy(unit *session.SessionUnit) bool` — sessionBusy 报告会话是否正忙（释放面的守卫判据之一：忙的会话绝不释放）：
- `func sessionBusyState(unit *session.SessionUnit) switching.BusyState` — sessionBusyState 把会话单元的四个忙态读数装配成中间层的值（装配缝；判据在

### resident_lru_test.go

- `func withResidentLimit(limit int) func()` — withResidentLimit 把进程级 resident_limit 临时改成 limit，返回还原函数
- `func residentOf(t *testing.T, service *Service, sessionID string) bool`
- `func TestResidentLimitEvictsLeastRecentlyUsedIdle(t *testing.T)` — TestResidentLimitEvictsLeastRecentlyUsedIdle 波 4 G6 INV-G8：驻留上限 2，
- `func TestResidentLimitKeepsBusySessions(t *testing.T)` — TestResidentLimitKeepsBusySessions 波 4 G6 INV-G8：运行中会话不可驱逐
- `func TestSessionReleaseDefaults(t *testing.T)` — TestSessionReleaseDefaults 释放面默认口径（2026-10-14）：统一窗口默认 1

### review_files.go

- `func (service *Service) reviewRequests() *review.Requests` — reviewRequests 返回落点（每次调用构造一次值语义的宿主适配；无状态）。
- `func (service *Service) RequestFileReview(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error)` — RequestFileReview 实现 assemble.ReviewFilesPort（判据在 `backend/tool/review`）。
- `func (service *Service) ClearReviewRequest(sessionID string)` — ClearReviewRequest 清空指定会话的待审阅请求（工作区切换/解绑的失效面；
- `func (host serviceReviewHost) WorkspaceFile() (contract.WorkspaceFilePort, string, error)`
- `func (host serviceReviewHost) NextRequestID() string` — NextRequestID 派一个跨调用唯一的召唤 ID（前端据此区分"新的一次请求"与"同一份快照的补放"）。
- `func (host serviceReviewHost) SessionIDFromContext(ctx context.Context) string`
- `func (host serviceReviewHost) ViewSessionID() string`
- `func (host serviceReviewHost) SetReviewRequest(sessionID string, request *dto.ReviewRequest) uint64`
- `func (host serviceReviewHost) ClearReviewRequest(sessionID string)`
- `func (host serviceReviewHost) PublishSnapshotChanged(sessionID string, revision uint64)`

### review_files_test.go

- `func reviewFixture(t *testing.T) (*Service, string)` — reviewFixture 在一个真实临时工作区里摆出各类文件（正常 / 敏感 / 二进制 /
- `func TestRequestFileReviewClassifiesByWorkspaceBoundary(t *testing.T)` — TestRequestFileReviewClassifiesByWorkspaceBoundary：一次批处理召唤的裁决口径——
- `func TestRequestFileReviewProjectsIntoSnapshotAndClears(t *testing.T)` — TestRequestFileReviewProjectsIntoSnapshotAndClears：落点是**会话作用域的可重放
- `func TestRequestFileReviewClearedOnWorkspaceSwitch(t *testing.T)` — TestRequestFileReviewClearedOnWorkspaceSwitch：失效语义对齐既有口径——工作区
- `func TestRequestFileReviewRejectsOverCapAndEmpty(t *testing.T)` — TestRequestFileReviewRejectsOverCapAndEmpty：超限显式报错（不静默截断），空清单
- `func TestRequestFileReviewWithoutWorkspaceFailsExplicitly(t *testing.T)` — TestRequestFileReviewWithoutWorkspaceFailsExplicitly：没有绑定工作区时明确失败
- `func TestRequestFileReviewAllSkippedKeepsSnapshotUntouched(t *testing.T)` — TestRequestFileReviewAllSkippedKeepsSnapshotUntouched：全部被跳过时不写快照——

### runtime_projection.go

- `func (service *Service) publishRuntimeProjections()` — publishRuntimeProjections 在 service.ViewMu 下拷贝应用自有状态，释放锁后发布

### served_route_test.go

- `func TestStampServedRouteFillsAssistantMessageAndPublishes(t *testing.T)`
- `func TestStampServedRouteWithoutFactIsNoop(t *testing.T)`
- `func TestStampServedRouteSkipsAlreadyStampedTail(t *testing.T)`
- `func TestStampAssistantRouteForScopeAndIdempotence(t *testing.T)` — StampAssistantRouteFor 的行级口径：只写本轮（TaskID 匹配）的 assistant 行，

### snapshot_budget.go

- `func snapshotToolOutputLimit() int` — snapshotToolOutputLimit 返回可见会话快照单条工具输出的字符预算
- `func toolOutputPreview(content string, limit int) (string, bool)` — toolOutputPreview 是快照预览截断判据的**宿主转发**：判据本体在中间层
- `func (service *Service) boundToolResultForSnapshot(name, content string) (visible string, ref string, truncated bool, totalChars int)` — boundToolResultForSnapshot 生成进入可见会话快照的工具输出（调用方持有

### snapshot_budget_test.go

- `func TestBoundToolResultForSnapshotArchives(t *testing.T)` — TestBoundToolResultForSnapshotArchives 验证截断链入口：超限输出归档为
- `func TestToolResultContentPagination(t *testing.T)` — TestToolResultContentPagination 验证分页读回：offset/limit/UTF-8 边界
- `func TestSnapshotBudgetUsesConfiguredLimit(t *testing.T)` — TestSnapshotBudgetUsesConfiguredLimit 验证 limits 注入生效：设置较小

### turn_status_single_word_test.go

- `func TestTurnStatusSpeaksOneWordOnSnapshotAndArchive(t *testing.T)` — turn_status_single_word_test.go — 「一次回合（请求）现在处在哪一步」这件事，
- `func TestTurnStatusOfRecordKeepsLegacyRunningAndNeverFoldsToTerminal(t *testing.T)` — 落盘面读回：合并前的存档里"进行中"写的是 "running"，合并后写 "progressing"。

### window.go

- `func ApplyWindowConfig(config cc.WindowConfig)` — ApplyWindowConfig 应用 seele.yaml window 段（零值字段自动补默认）；
- `func CurrentWindowConfig() cc.WindowConfig` — CurrentWindowConfig 返回当前生效的 window 段（含默认值）。
- `func RetainedReadTailBudget(runtime any) int` — RetainedReadTailBudget 返回**读尾**（冷恢复装载 transcript/history 尾部）的
- `func RetainFloorTokens(budget task_context.ContextBudget) int` — RetainFloorTokens 返回当前配置下保护区下限的**比例部分**（token 数）：
- `func ValidateRetainWindow(config cc.WindowConfig, limits limit.Limits, windowTokens int) error` — ValidateRetainWindow 校验 window 段与 limits 段的组合是否自洽（启动期调用）：
- `func (r retainWindowLimits) ContextWindow() int`
- `func (r retainWindowLimits) MaxOutputTokens() int`

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

### workspace_host.go

- `func (host workspaceHost) LockTransition() func()`
- `func (host workspaceHost) State() switchworkspace.ViewState`
- `func (host workspaceHost) ApplyDraftBinding(info model.WorkspaceInfo, projection switchworkspace.Projection) uint64`
- `func (host workspaceHost) ApplyBinding(info model.WorkspaceInfo, projection switchworkspace.Projection, freshSessionID, notice string) uint64`
- `func (host workspaceHost) ApplyUnbind(sessionID string, projection switchworkspace.Projection) uint64`
- `func (host workspaceHost) PublishSnapshotChanged(revision uint64)`
- `func (host workspaceHost) RequestCatalogRefresh()`
- `func (host workspaceHost) ClearReviewRequest()`

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

### workspace_usecase.go

- `func (service *Service) releaseSessionAsync(sessionID string)` — releaseSessionAsync 把"这个会话没有了"转告执行域：杀掉它名下所有在途后台命令。
- `func (service *Service) DeleteSession(sessionID string) error` — DeleteSession 删除会话记录并走完整释放面（会话域用例，留在 application/core）。
- `func (service *Service) CreateWorkspace(name, rootPath, gitRemote string) error` — CreateWorkspace 建工作区并绑定到当前视图（实现：middle/switch/workspace）。
- `func (service *Service) BindWorkspace(workspaceID string) error` — BindWorkspace 把工作区绑定到当前视图会话/草稿（实现：middle/switch/workspace）。
- `func (service *Service) UnbindWorkspace()` — UnbindWorkspace 解绑当前视图会话/草稿的工作区（实现：middle/switch/workspace）。
- `func (service *Service) WorkspaceTree(relPath string, depth int) (dto.TreeListing, error)` — WorkspaceTree 列出当前工作区某目录的子条目（root 只来自后端当前 workspace）。
- `func (service *Service) WorkspaceFileCount() (dto.TreeCount, error)` — WorkspaceFileCount 统计当前工作区文件/目录数。
- `func (service *Service) WorkspaceGitLog(repo string, limit int) (dto.GitLogResult, error)` — WorkspaceGitLog 返回**指定仓库**（repo 相对工作区根，"." = 工作区根）最近 limit 条提交。
- `func (service *Service) WorkspaceChanges(repo string, limit int) (dto.WorkspaceChangesResult, error)` — WorkspaceChanges 返回**指定仓库**未提交的改动。
- `func (service *Service) WorkspaceGitCommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error)` — WorkspaceGitCommitDetail 返回**指定仓库**某提交改了哪些文件。
- `func (service *Service) WorkspaceFileContent(relPath string, limit int64) (dto.FileContent, error)` — WorkspaceFileContent 读取当前工作区某文件的前 limit 字节。
- `func (service *Service) WorkspaceGitCommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error)` — WorkspaceGitCommitFileContent 读取某文件在**指定仓库**某个提交时的内容（git 对象库）。
- `func (service *Service) WorkspaceWriteFile(relPath, content string) (dto.FileWriteResult, error)` — WorkspaceWriteFile 覆盖当前工作区某文件的全部内容（面板编辑保存）。
- `func (service *Service) applyWorkspaceProjectionLocked(projection switchworkspace.Projection)` — applyWorkspaceProjectionLocked 把 middle 产出的窄投影写进 Snapshot。

### workspace_gitrepos.go（多仓库 git 的应用用例面）

多仓库 git 只在这一处编排两件事：仓库**发现**与仓库**路径解析**。

- `func (service *Service) WorkspaceGitRepos() (dto.GitReposResult, error)` —
  发现当前工作区内的 git 仓库。端口是 optional 的
  `contract.WorkspaceGitReposPort`（`workspace.Repo` 实现，`wiring/adapters`
  薄转发），在这里用**类型断言**启用；未绑定工作区、后端没有这个端口都只回一份
  带 `Error` 文案的**空结果**（不报错）：面板显示"不可用"，不是整块渲染失败。
  `limit` 传 0，默认值（64）与钳制（1..256）都归 `store/workspace` 一份口径。
- `func (service *Service) workspaceRepoPort(repo string) (contract.WorkspacePort, string, error)` —
  四条 git 查询共用的入口：取当前工作区根 → 解析 repo → 连同后端端口交出。
- `func resolveWorkspaceRepo(root, repo string) (string, error)` —
  repo（前端给的**相对**路径，`""`/`"."` = 工作区根）→ 绝对仓库根。绝对路径、
  `..` 逃逸、软链逃逸（`EvalSymlinks` 两侧再判一次 containment）、目录不存在
  都是**调用方传错参数 → Go error**（不是 `Result.Error`）。端口签名与 git 查询
  语义一律不变。
- `func repoWithinRoot(root, candidate string) bool` — 词法 containment 判据
  （与 `store/workspace` 目录树同口径）。

四条查询因此都多了 `repo` 参数：`WorkspaceGitLog(repo, limit)`、
`WorkspaceChanges(repo, limit)`、`WorkspaceGitCommitDetail(repo, hash, limit)`、
`WorkspaceGitCommitFileContent(repo, hash, relPath, limit)`。

### workspace_gitrepos_usecase_test.go

- `func (fake *gitReposFakeWorkspace) GitRepos(root string, limit int) (dto.GitReposResult, error)`
- `func TestResolveWorkspaceRepoContainment(t *testing.T)` — 四种入参口径：空/"." → 工作区根；相对 → 根内绝对路径；绝对路径与 `..` 逃逸 → Go error。
- `func TestResolveWorkspaceRepoRejectsSymlinkEscape(t *testing.T)` — 软链逃逸必须报错，指向根内的链接放行（Windows 无权限造链接时跳过）。
- `func TestWorkspaceGitQueriesForwardResolvedRepoRoot(t *testing.T)` — 三条查询把解析后的绝对仓库根（及 hash/limit）原样转发；越界入参一次端口调用都不发生。
- `func TestWorkspaceGitCommitFileContentForwardsResolvedRepoRoot(t *testing.T)` — 提交内文件内容走同一个仓库根与文件端口。
- `func TestWorkspaceGitReposForwardsWorkspaceRoot(t *testing.T)` — 仓库发现的 root 只来自后端当前 workspace。
- `func TestWorkspaceGitReposDegradesWithoutPort(t *testing.T)` — 缺端口 / 未绑定工作区都只给文案，不是 Go error。
