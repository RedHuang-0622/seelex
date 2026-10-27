# assembly/misc（根包分卷）

## 生态位

基础与杂项（vocabulary/assembly_helpers/limits/completion/compressed/diagnostics/fault-guard/perf/snapshot-budget/runtime/workspace/race）

覆盖：显式名单 30 个文件（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### completion_test.go

- `func TestCompletionApplicationIsAssembled(t *testing.T)`
- `func TestCompletionFacadeOnlyDelegatesToApplication(t *testing.T)`
- `func TestSuggestionsExcludeModelSideTools(t *testing.T)` — TestSuggestionsExcludeModelSideTools：`/` 只列"能从输入框直接执行"的入口——

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

### fault_guard_test.go

- `func TestFaultGuardApplicationIsAssembled(t *testing.T)`
- `func TestFaultGuardFacadeOnlyDelegatesToApplication(t *testing.T)`
- `func TestDegradeAndRequestExitNotifiesHost(t *testing.T)` — TestDegradeAndRequestExitNotifiesHost：降级退出必须幂等、标记 closed/
- `func TestLifecycleFaultPanicsAfterDegrade(t *testing.T)` — TestLifecycleFaultPanicsAfterDegrade：生命周期消费者中的故障不再被静默

### limits.go

- `func workTableBounds() worktable.Limits` — workTableBounds 把进程级上限里**工作表格投影**用到的那三格按值取出，交给中间层的

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

### resident_lru_test.go

- `func withResidentLimit(limit int) func()` — withResidentLimit 把进程级 resident_limit 临时改成 limit，返回还原函数
- `func residentOf(t *testing.T, service *Service, sessionID string) bool`
- `func TestResidentLimitEvictsLeastRecentlyUsedIdle(t *testing.T)` — TestResidentLimitEvictsLeastRecentlyUsedIdle 波 4 G6 INV-G8：驻留上限 2，
- `func TestResidentLimitKeepsBusySessions(t *testing.T)` — TestResidentLimitKeepsBusySessions 波 4 G6 INV-G8：运行中会话不可驱逐
- `func TestSessionReleaseDefaults(t *testing.T)` — TestSessionReleaseDefaults 释放面默认口径（2026-10-14）：统一窗口默认 1

### review_files_test.go

- `func TestReviewApplicationIsAssembled(t *testing.T)`
- `func TestReviewFacadeOnlyDelegatesToApplication(t *testing.T)`
- `func reviewFixture(t *testing.T) (*Service, string)` — reviewFixture 在一个真实临时工作区里摆出各类文件（正常 / 敏感 / 二进制 /
- `func TestRequestFileReviewClassifiesByWorkspaceBoundary(t *testing.T)` — TestRequestFileReviewClassifiesByWorkspaceBoundary：一次批处理召唤的裁决口径——
- `func TestRequestFileReviewProjectsIntoSnapshotAndClears(t *testing.T)` — TestRequestFileReviewProjectsIntoSnapshotAndClears：落点是**会话作用域的可重放
- `func TestRequestFileReviewClearedOnWorkspaceSwitch(t *testing.T)` — TestRequestFileReviewClearedOnWorkspaceSwitch：失效语义对齐既有口径——工作区
- `func TestRequestFileReviewRejectsOverCapAndEmpty(t *testing.T)` — TestRequestFileReviewRejectsOverCapAndEmpty：超限显式报错（不静默截断），空清单
- `func TestRequestFileReviewWithoutWorkspaceFailsExplicitly(t *testing.T)` — TestRequestFileReviewWithoutWorkspaceFailsExplicitly：没有绑定工作区时明确失败
- `func TestRequestFileReviewAllSkippedKeepsSnapshotUntouched(t *testing.T)` — TestRequestFileReviewAllSkippedKeepsSnapshotUntouched：全部被跳过时不写快照——

### runtime_projection.go

- `func (port serviceRuntimeProjectionPort) CollectProjection(ctx context.Context) view_state.RuntimeStateProjection`
- `func (port serviceRuntimeProjectionPort) LockView()`
- `func (port serviceRuntimeProjectionPort) ApplyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection)`
- `func (port serviceRuntimeProjectionPort) BumpLocked() uint64`
- `func (port serviceRuntimeProjectionPort) UnlockView()`
- `func (port serviceRuntimeProjectionPort) CurrentViewSessionID() string`
- `func (port serviceRuntimeProjectionPort) RuntimeSnapshot() model.RuntimeState`
- `func (port serviceRuntimeProjectionPort) PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState)`
- `func (port serviceRuntimeProjectionPort) PublishTaskDeltas()`
- `func (service *Service) publishRuntimeProjections()` — publishRuntimeProjections copies application-owned state under the view lock

### served_route_test.go

- `func TestStampServedRouteFillsAssistantMessageAndPublishes(t *testing.T)`
- `func TestStampServedRouteWithoutFactIsNoop(t *testing.T)`
- `func TestStampServedRouteSkipsAlreadyStampedTail(t *testing.T)`
- `func TestStampAssistantRouteForScopeAndIdempotence(t *testing.T)` — StampAssistantRouteFor 的行级口径：只写本轮（TaskID 匹配）的 assistant 行，

### snapshot_budget.go

- `func snapshotToolOutputLimit() int` — snapshotToolOutputLimit 返回可见会话快照单条工具输出的字符预算
- `func (service *Service) toolOutputPreview(content string, limit int) (string, bool)` — toolOutputPreview 是快照预览截断判据的**宿主转发**：判据本体在中间层
- `func (service *Service) boundToolResultForSnapshot(name, content string) (visible string, ref string, truncated bool, totalChars int)` — boundToolResultForSnapshot 生成进入可见会话快照的工具输出（调用方持有

### snapshot_budget_test.go

- `func TestBoundToolResultForSnapshotArchives(t *testing.T)` — TestBoundToolResultForSnapshotArchives 验证截断链入口：超限输出归档为
- `func TestToolResultContentPagination(t *testing.T)` — TestToolResultContentPagination 验证分页读回：offset/limit/UTF-8 边界
- `func TestSnapshotBudgetUsesConfiguredLimit(t *testing.T)` — TestSnapshotBudgetUsesConfiguredLimit 验证 limits 注入生效：设置较小

### turn_status_single_word_test.go

- `func TestTurnStatusSpeaksOneWordOnSnapshotAndArchive(t *testing.T)` — turn_status_single_word_test.go — 「一次回合（请求）现在处在哪一步」这件事，
- `func TestTurnStatusOfRecordKeepsLegacyRunningAndNeverFoldsToTerminal(t *testing.T)` — 落盘面读回：合并前的存档里"进行中"写的是 "running"，合并后写 "progressing"。

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
- `func TestWorkspaceGitApplicationIsAssembled(t *testing.T)`
- `func TestWorkspaceGitFacadeOnlyDelegatesToApplication(t *testing.T)`

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

- `func TestWorkspaceApplicationIsAssembled(t *testing.T)`
- `func TestCreateWorkspacesFacadeOnlyDelegatesToApplication(t *testing.T)`
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

- `func (service *Service) releaseSessionAsync(sessionID string)` — releaseSessionAsync tells the execution domain that a session's background
- `func (service *Service) applyWorkspaceProjectionLocked(projection switchworkspace.Projection)` — applyWorkspaceProjectionLocked copies a middle-layer projection into the
- `func (port serviceWorkspaceProjectionPort) Projection() switchworkspace.Projection`
- `func (port serviceWorkspaceProjectionPort) LockView()`
- `func (port serviceWorkspaceProjectionPort) ApplyWorkspaceProjectionLocked(projection switchworkspace.Projection)`
- `func (port serviceWorkspaceProjectionPort) BumpLocked() uint64`
- `func (port serviceWorkspaceProjectionPort) UnlockView()`
- `func (port serviceWorkspaceProjectionPort) CurrentViewSessionID() string`
- `func (port serviceWorkspaceProjectionPort) PublishSnapshotChanged(revision uint64, sessionID string)`
