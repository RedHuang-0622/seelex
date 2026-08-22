# core/service

## 生态位

Service 门面、装配根与跨域用例编排（输入/交互/调度/快照/测试夹具）

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### service.go

- `func New(deps Dependencies) (*Service, error)`
- `func (service *Service) ActiveSkillIDs() []string` — ActiveSkillIDs 返回当前任务的激活 skill ID 列表（goal skill 激活判定用，
- `func (service *Service) GoalSkillActive() bool` — GoalSkillActive 返回最新的本地投影（诊断与测试用）。Runtime 经
- `func (service *Service) PublishRuntimeProjections()` — PublishRuntimeProjections 刷新 Runtime 的不可变状态副本。供在
- `func (service *Service) SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error)` — SubscribeSubagentLive 订阅 node 第一视角实时流（历史回放 + 只读事件通道
- `func (service *Service) HandleSubagentToolEvent(event seelsession.SubagentToolEvent)` — HandleSubagentToolEvent 把 Runtime 工具分发投影进权威 Plan 节点快照并
- `func (service *Service) SubagentSessionDetail(nodeID string) (*model.SubagentDetail, error)` — SubagentSessionDetail 返回节点子代理的详情数据（截断会话 + 上下文快照 +
- `func (service *Service) ClearSubagentTree() error` — ClearSubagentTree 清空子代理树（GUI「清空」按钮入口：失败节点显式清走；

### service_assembler.go

- `func (p taskPromptPort) CurrentEffort() string`
- `func (p taskPromptPort) ClearSkillLayers()`
- `func (p taskPromptPort) PushSkillLayer(kind, name, text string)`
- `func (assembler serviceAssembler) assemble() (*Service, error)`
- `func validateDependencies(deps Dependencies) error`
- `func (assembler *serviceAssembler) applyInfrastructureDefaults()`
- `func (service *Service) restoreInitialWorkspace()`

### service_chat_test.go

- `func TestReActBudgetStopsOnlyAfterItsToolBudget(t *testing.T)`
- `func TestReActBudgetUsesReservedFinalDeliveryTurn(t *testing.T)`
- `func TestRuntimeMailboxDrainsIntoHistoryOutsideServiceLock(t *testing.T)`
- `func TestSessionBackedIterationInterruptsOnQueuedInput(t *testing.T)`
- `func TestChatPublishesSnapshotWithoutUI(t *testing.T)`
- `func TestGracefulShutdownWaitsForQueuedChat(t *testing.T)`
- `func TestSessionBackedQueueIsConsumedAtRunChatEnd(t *testing.T)`
- `func TestCancelChatInterruptsContextAwareEngine(t *testing.T)`

### service_components_test.go

- `func TestNewAssemblesFocusedServiceComponents(t *testing.T)`
- `func TestServiceFacadeContainsOnlyAssembly(t *testing.T)`
- `func TestFocusedComponentsDoNotHoldServiceFacade(t *testing.T)`

### service_fakes_test.go

- `func (engine *sessionBackedBlockingEngine) SessionBacked() bool`
- `func (engine *sessionBackedBlockingEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (sessions *blockingSaveSessions) SaveCurrent(string) error`
- `func (engine *fakeEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *fakeEngine) History() []EngineMessage`
- `func (engine *fakeEngine) ClearHistory()`
- `func (engine *fakeEngine) SessionID() string`
- `func (engine *fakeEngine) StartSession() string`
- `func (engine *fakeEngine) ReplaceHistory(sessionID string, history []EngineMessage) error`
- `func (engine *fakeEngine) SetSystemPrompt(prompt string)`
- `func (engine *fakeEngine) SetMaxLoops(maxLoops int)`
- `func (engine *fakeEngine) AppendHistory(msg types.Message)`
- `func (*fakeEngine) TraceText() string`
- `func (*fakeEngine) TokenCount() string`
- `func (*fakeEngine) NodeSessionConversation(string) ([]types.Message, bool)`
- `func (engine *fakeEngine) NodeContextSnapshot(string) (*snapshot.ContextSnapshot, bool)`
- `func (engine *fakeEngine) NodeToolResult(nodeID, ref string) (string, bool)`
- `func (engine *fakeEngine) NodeWorktreeInfoFor(nodeID string) (seelebridge.NodeWorktreeInfo, bool)`
- `func (engine *fakeEngine) SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error)`
- `func (engine *fakeEngine) SubAgentTree() []dto.SubAgentTreeNode`
- `func (engine *fakeEngine) ReleaseWorkingHistory()`
- `func (*fakeRuntime) Model() string`
- `func (*fakeRuntime) Provider() string`
- `func (*fakeRuntime) Accounts() []AccountInfo`
- `func (runtime *fakeRuntime) SelectAccount(name string) bool`
- `func (*fakeRuntime) VisibleTools(context.Context) []Tool`
- `func (*fakeRuntime) ActivePlugin() string`
- `func (runtime *fakeRuntime) FullAccess() bool`
- `func (runtime *fakeRuntime) SetFullAccess(on bool)`
- `func (runtime *fakeRuntime) SetRuntimeVisibilityProjection(projection seelebridge.RuntimeVisibilityProjection)`
- `func (runtime *fakeRuntime) SetParentEvidenceProjection(projection seelebridge.ParentEvidenceProjection)`
- `func (runtime *fakeRuntime) DrainSubagentContexts() []string`
- `func (runtime *fakeRuntime) SetPlanPolicy(policy dto.PlanPolicy)`
- `func (runtime *fakeRuntime) PrepareReplan(_ context.Context, request dto.ReplanRequest) (dto.PlanPreflight, error)`
- `func (runtime *fakeRuntime) ReplanMetrics() dto.ReplanMetrics`
- `func (runtime *fakeRuntime) SetPlanBranchBinding(binding dto.PlanBranchBinding)`
- `func (runtime *fakeRuntime) TodoSnapshot() []dto.TodoItem`
- `func (runtime *fakeRuntime) SetTodoStatus(index int, status dto.TodoItemStatus) error`
- `func (runtime *fakeRuntime) TaskSnapshot() []dto.TaskRecord`
- `func (runtime *fakeRuntime) TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error)`
- `func (runtime *fakeRuntime) ResolveTaskByKey(key string) (dto.TaskRecord, bool, error)`
- `func (runtime *fakeRuntime) TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error)`
- `func (runtime *fakeRuntime) TaskAttachParticipant(id, participant string) (dto.TaskRecord, error)`
- `func (*fakeRuntime) TaskChangedChannel() <-chan dto.TaskRecord`
- `func (*fakeRuntime) SubagentTreeEvents() <-chan struct`
- `func (*fakeRuntime) PlanNodeEventChannel() <-chan dto.PlanNodeEvent`
- `func (runtime *fakeRuntime) SwitchSessionTasks(records []dto.TaskRecord)`
- `func (runtime *fakeRuntime) ScheduledCommands() []seelebridge.ScheduledCommandInfo`
- `func (runtime *fakeRuntime) ScheduledTasksSnapshot() []seelebridge.ScheduledTaskStatus`
- `func (runtime *fakeRuntime) ScheduleTask(_ context.Context, spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error)`
- `func (runtime *fakeRuntime) CancelScheduledTask(id string) error`
- `func (runtime *fakeRuntime) ClearSubagentTree() error`
- `func (runtime *fakeRuntime) SearchHistory(_ context.Context, _ string, _ int) (seelexctxsearch.Result, error)`
- `func (runtime *fakeRuntime) BindProjectRoot(rootPath string) error`
- `func (runtime *fakeRuntime) UnbindProjectRoot()`
- `func (runtime *goalVisibilityRuntime) VisibleTools(context.Context) []Tool`
- `func (*fakePlugins) All() []PluginInfo`
- `func (plugins *fakePlugins) Activate(_ context.Context, name string) error`
- `func (plugins *fakePlugins) Deactivate(context.Context) error`
- `func (plugins *fakePlugins) Current() (PluginInfo, bool)`
- `func (fakeSkills) All() []SkillInfo`
- `func (fakeSkills) Get(name string) (SkillInfo, bool)`
- `func (fakeSessions) SaveCurrent(string) error`
- `func (fakeSessions) Resume(string) error`
- `func (fakeSessions) List() []SessionInfo`
- `func (fakeSessions) LoadHistory(string) ([]EngineMessage, error)`
- `func (fakeSessions) LoadHistoryRange(string, int, int) ([]EngineMessage, int, error)`
- `func (fakeSessions) Delete(string) error`
- `func (fakeSessions) MessageCount(string) (int, error)`
- `func (fakeSessions) SetWorkspace(string)`
- `func (fakeSessions) Workspace() string`
- `func (sessions *blockingCatalogSessions) List() []SessionInfo`
- `func (persistenceFailingSessions) SaveCurrent(string) error`
- `func (sessions *trackingSessions) SaveCurrent(sessionID string) error`
- `func (sessions *scopedSessions) SetWorkspace(workspaceID string)`
- `func (sessions *scopedSessions) Workspace() string`
- `func (sessions *scopedSessions) SaveCurrent(sessionID string) error`
- `func (sessions *scopedSessions) SavedIDs() []string`
- `func (sessions *scopedSessions) ListWorkspace(workspaceID string) []SessionInfo`
- `func (sessions *scopedSessions) LoadedWorkspace() string`
- `func (sessions *scopedSessions) LoadHistoryWorkspace(workspaceID, sessionID string) ([]EngineMessage, error)`
- `func (sessions *scopedSessions) LoadHistoryRangeWorkspace(workspaceID, sessionID string, offset, limit int) ([]EngineMessage, int, error)`
- `func (sessions *scopedSessions) DeleteWorkspace(workspaceID, sessionID string) error`
- `func newFakeWorkspace() *fakeWorkspace`
- `func (repo *fakeWorkspace) Create(name, rootPath, gitRemote string) (WorkspaceInfo, error)`
- `func (repo *fakeWorkspace) Get(id string) (WorkspaceInfo, error)`
- `func (repo *fakeWorkspace) List() []WorkspaceInfo`
- `func (repo *fakeWorkspace) Delete(id string) error`
- `func (repo *fakeWorkspace) BindSession(sessionID, workspaceID string)`
- `func (repo *fakeWorkspace) UnbindSession(sessionID string)`
- `func (repo *fakeWorkspace) SessionWorkspace(sessionID string) (WorkspaceInfo, bool)`
- `func (repo *fakeWorkspace) AllBindings() map[string]string`
- `func (*fakeWorkspace) DetectGitRemote(string) string`

### service_helpers_test.go

- `func withTestSessions(sessions SessionPort) testServiceOption`
- `func withTestRuntime(runtime RuntimePort) testServiceOption`
- `func newTestService(t testing.TB, engine ChatEngine, options ...testServiceOption) *Service` — newTestService 构造一个带默认 fakes 的 Service（测试夹具），并在测试
- `func mustNew(t testing.TB, deps Dependencies) *Service`
- `func waitForSnapshot(t *testing.T, service *Service, ready func(Snapshot) bool) Snapshot` — waitForSnapshot 轮询 Snapshot 直到 ready 条件满足，返回满足条件的快照。
- `func (*sessionBackedEngine) SessionBacked() bool`
- `func newGracefulShutdownEngine() *gracefulShutdownEngine`
- `func (engine *gracefulShutdownEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func waitForChatCompletion(t *testing.T, service *Service)` — waitForChatCompletion 轮询直到当前 chat 结束。

### service_input.go

- `func (service *Service) injectPendingSubagentContexts()` — injectPendingSubagentContexts 排空 Runtime 持有的有界邮箱（单一来源 =
- `func (service *Service) Submit(ctx context.Context, text string) error`
- `func (service *Service) submitConversation(ctx context.Context, input string) error`
- `func (service *Service) BeginGracefulShutdown()` — BeginGracefulShutdown 停止接收新输入，同时允许活跃 chat 及其已排队输入
- `func (service *Service) WaitForIdle(ctx context.Context) error` — WaitForIdle 等待全部已接受的 chat 工作完成。它从不取消活跃 chat；调用方
- `func (service *Service) CancelChat(requestID string) bool`
- `func (service *Service) Shutdown()`

### service_input_test.go

- `func TestSystemPromptStableAcrossPlanNodeChanges(t *testing.T)`
- `func TestWorkTableTraceBlock(t *testing.T)`
- `func TestPrepareExecutionContextCarriesWorkTableTraceBlock(t *testing.T)`
- `func TestNewRejectsMissingDependencies(t *testing.T)`
- `func TestSuggestionsAndSkillRouting(t *testing.T)`
- `func TestApprovalBrokerResolve(t *testing.T)`

### service_interaction.go

- `func (service *Service) ResolveInteraction(ctx context.Context, id, optionID string) error`
- `func (service *Service) appendPlanRetryNotice(message string)`
- `func (service *Service) abortPlanInteraction()`
- `func (service *Service) SelectAccount(_ context.Context, name string) error`
- `func (service *Service) SwitchEffort(_ context.Context, level string) error`
- `func (service *Service) SwitchPlugin(ctx context.Context, name string) error`
- `func (service *Service) SetFullAccess(on bool)`
- `func (service *Service) observeInteraction(interaction *Interaction)`
- `func (service *Service) openInteraction(interaction *Interaction)`
- `func (service *Service) closeInteraction(id string)`
- `func (service *Service) sessionInteraction() *Interaction`
- `func (service *Service) accountInteraction() *Interaction`

### service_plan_test.go

- `func TestPlanRunJSONFailureOpensRecoveryInteraction(t *testing.T)`
- `func TestPlanRunToolErrorDoesNotDeadlock(t *testing.T)`
- `func TestResolvePlanFailureReplansWithoutRunningReplacement(t *testing.T)`
- `func TestResolvePlanFailureKeepsInteractionWhenReplanFails(t *testing.T)`
- `func TestResolvePlanFailureStopsAfterPlanChainReplanLimit(t *testing.T)`
- `func TestRuntimeSnapshotIncludesReplanMonitor(t *testing.T)`
- `func TestNormalizePlanToolCallInfoUsesCanonicalAdapterJSON(t *testing.T)`
- `func TestHandlePlanBranchEventUpdatesLifecycleAndRuntime(t *testing.T)`
- `func TestHandleSubagentToolEventProjectsBoundedIncrementals(t *testing.T)`
- `func TestToolHookBridgeAssignsUniqueStableIDs(t *testing.T)`

### service_scheduler.go

- `func (service *Service) ScheduleTask(ctx context.Context, spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error)` — ScheduleTask 创建并启动一个定时/周期任务（校验在 Runtime 调度器内完成）。
- `func (service *Service) CancelScheduledTask(id string) error` — CancelScheduledTask 取消并移除定时/周期任务。
- `func (service *Service) RefreshRuntimeSnapshot()` — RefreshRuntimeSnapshot 重新收集运行时投影（含定时/周期任务快照）并发布

### service_snapshot.go

- `func (service *Service) Snapshot() Snapshot`
- `func (service *Service) Subscribe(buffer int) Subscription`
- `func (service *Service) collectRuntimeProjection(ctx context.Context) view_state.RuntimeStateProjection`
- `func (service *Service) applyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection)`
- `func (service *Service) appendMessageLocked(role, content string, tool *ToolCall) *Message`
- `func (service *Service) bumpLocked() uint64`
- `func (service *Service) addNotice(notice string)`
- `func (service *Service) resetConversation(notice string)`
- `func (service *Service) advanceMessageSeqLocked(messages []Message)` — advanceMessageSeqLocked 按既有消息 ID 推进消息序列（恢复路径委托）。

### service_snapshot_test.go

- `func TestSnapshotNeverSerializesSystemPrompt(t *testing.T)`
- `func TestEventHubOrdersAndResyncs(t *testing.T)`
- `func TestMessageDeltaIncludesStableMessageID(t *testing.T)`
- `func TestToolEventsUpdateSnapshot(t *testing.T)`
- `func TestToolCompletionDoesNotReenterServiceLockForGoalSkillVisibility(t *testing.T)`

### service_test.go

- `func TestNewTestServiceCleansUpCatalogWorker(t *testing.T)`
- `func TestSessionCatalogAllowsDuplicateNamesWithDistinctIDs(t *testing.T)`
- `func TestSessionTitleUsesFirstUserQuestion(t *testing.T)`
- `func TestCurrentSessionNameUsesFirstQuestion(t *testing.T)`
- `func TestWorkingHistoryReleasesOnlyAfterSuccessfulPersistence(t *testing.T)`
- `func TestBeginNewSessionIsLazyAndFirstQuestionMaterializesIt(t *testing.T)`
- `func TestLazySessionInheritsProjectOnlyWhenMaterialized(t *testing.T)`
- `func TestInitialLazySessionIsDraftAndFirstSubmitMaterializes(t *testing.T)`
- `func TestResumeSessionLeavesLazyDraft(t *testing.T)`
- `func TestProjectBindingCreatesScopesAndNewSessionInheritsProject(t *testing.T)`
- `func TestResumeRestoresProjectScope(t *testing.T)`
- `func TestNewHydratesPersistedWorkspaceSessions(t *testing.T)`
- `func TestResumeReadsSessionFromItsPersistedWorkspace(t *testing.T)`
- `func TestResumeRepairsBindingWhenHistoryLivesInAnotherWorkspace(t *testing.T)`
- `func TestLoadMoreHistoryUsesResumedSessionWorkspace(t *testing.T)`
- `func TestSwitchProjectStartsIndependentSessionWhenHistoryExists(t *testing.T)`
- `func TestSnapshotIncludesPersistedSessions(t *testing.T)`
- `func TestSnapshotDoesNotReadBlockedSessionCatalog(t *testing.T)`
- `func TestShutdownDoesNotWaitForBlockedSessionCatalog(t *testing.T)`
- `func TestBeginNewSessionClearsWorkTable(t *testing.T)`
- `func TestResumedChatPersistsToSelectedSession(t *testing.T)`
- `func TestLoadMoreHistoryAssignsStableMessageIDs(t *testing.T)`
- `func TestResumeCommandOpensSessionInteraction(t *testing.T)`
