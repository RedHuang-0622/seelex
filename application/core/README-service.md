# core/service

## 生态位

Service 门面、装配根与跨域用例编排（输入/交互/调度/快照/测试夹具）

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### service.go

- `func New(deps Dependencies) (*Service, error)`
- `func (service *Service) ActiveSkillIDs() []string` — ActiveSkillIDs 返回当前任务的激活 skill ID 列表（goal skill 激活判定用，
- `func (service *Service) PromptLayers() []PromptLayer` — PromptLayers 返回当前会话注入的 prompt 前缀层（system/base/effort/
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
- `func TestRuntimeMailboxDrainsIntoHistoryAndVisibleEvidence(t *testing.T)`
- `func TestSessionBackedIterationInterruptsOnQueuedInput(t *testing.T)`
- `func TestChatPublishesSnapshotWithoutUI(t *testing.T)`
- `func TestGracefulShutdownWaitsForQueuedChat(t *testing.T)`
- `func TestSessionBackedQueueIsConsumedAtRunChatEnd(t *testing.T)`
- `func TestCancelChatInterruptsContextAwareEngine(t *testing.T)`
- `func TestCancelChatWithStaleRequestID(t *testing.T)` — TestCancelChatWithStaleRequestID 验证取消语义归属：request_id 只是参考，动作

### service_components_test.go

- `func TestNewAssemblesFocusedServiceComponents(t *testing.T)`
- `func TestServiceFacadeContainsOnlyAssembly(t *testing.T)`
- `func TestFocusedComponentsDoNotHoldServiceFacade(t *testing.T)`

### service_fakes_test.go

- `func (engine *sessionBackedBlockingEngine) SessionBacked() bool`
- `func (engine *sessionBackedBlockingEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *sessionBackedBlockingEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 显式转发到自身 ChatStream（覆盖内嵌 fakeEngine 的提升方法，
- `func (sessions *blockingSaveSessions) SaveCurrent(string) error`
- `func (engine *fakeEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *fakeEngine) History() []EngineMessage`
- `func (engine *fakeEngine) ClearHistory()`
- `func (engine *fakeEngine) SessionID() string`
- `func (engine *fakeEngine) StartSession() string`
- `func (engine *fakeEngine) ActivateSession(sessionID string) error` — ActivateSession 以显式会话 ID 创建并激活引擎实例（G4 早分配 SID：
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
- `func (engine *fakeEngine) ReleaseWorkingHistoryFor(sessionID string)`
- `func (engine *fakeEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *fakeEngine) HistoryFor(sessionID string) []EngineMessage`
- `func (engine *fakeEngine) AppendHistoryFor(sessionID string, msg types.Message)`
- `func (engine *fakeEngine) ClearHistoryFor(sessionID string)`
- `func (engine *fakeEngine) SetSystemPromptFor(sessionID, prompt string)`
- `func (engine *fakeEngine) ReplaceHistoryFor(sessionID string, history []EngineMessage) error`
- `func (engine *fakeEngine) HasSession(sessionID string) bool`
- `func (*fakeRuntime) Model() string`
- `func (*fakeRuntime) Provider() string`
- `func (*fakeRuntime) Accounts() []AccountInfo`
- `func (runtime *fakeRuntime) SelectAccount(name string) bool`
- `func (*fakeRuntime) VisibleTools(context.Context) []Tool`
- `func (*fakeRuntime) ActivePlugin() string`
- `func (runtime *fakeRuntime) FullAccess() bool`
- `func (runtime *fakeRuntime) SetFullAccess(on bool)`
- `func (runtime *fakeRuntime) SetRuntimeVisibilityProjection(projection seelebridge.RuntimeVisibilityProjection)` — SetRuntimeVisibilityProjection / SetParentEvidenceProjection 会被并行会话的
- `func (runtime *fakeRuntime) SetParentEvidenceProjection(projection seelebridge.ParentEvidenceProjection)`
- `func (runtime *fakeRuntime) DrainSubagentContexts() []string` — DrainSubagentContexts 排空 merge-back 邮箱。M2 多会话并行下多个
- `func (runtime *fakeRuntime) SetPlanPolicy(policy dto.PlanPolicy)`
- `func (runtime *fakeRuntime) SetPlanPolicyFor(sessionID string, policy dto.PlanPolicy)`
- `func (runtime *fakeRuntime) planPolicyFor(sessionID string) (dto.PlanPolicy, bool)`
- `func (runtime *fakeRuntime) PrepareReplan(_ context.Context, request dto.ReplanRequest) (dto.PlanPreflight, error)`
- `func (runtime *fakeRuntime) ReplanMetrics() dto.ReplanMetrics`
- `func (runtime *fakeRuntime) ReplanMetricsFor(sessionID string) dto.ReplanMetrics`
- `func (runtime *fakeRuntime) SetPlanBranchBinding(binding dto.PlanBranchBinding)`
- `func (runtime *fakeRuntime) TodoSnapshot() []dto.TodoItem`
- `func (runtime *fakeRuntime) SetTodoStatus(index int, status dto.TodoItemStatus) error`
- `func (runtime *fakeRuntime) TaskSnapshot() []dto.TaskRecord`
- `func (runtime *fakeRuntime) TaskSnapshotFor(sessionID string) []dto.TaskRecord`
- `func (runtime *fakeRuntime) snapshotLocked() []dto.TaskRecord`
- `func (runtime *fakeRuntime) TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error)`
- `func (runtime *fakeRuntime) addTaskLocked(spec dto.TaskSpec) (dto.TaskRecord, bool, error)`
- `func (runtime *fakeRuntime) TaskAddFor(sessionID string, spec dto.TaskSpec) (dto.TaskRecord, bool, error)`
- `func (runtime *fakeRuntime) ResolveTaskByKey(key string) (dto.TaskRecord, bool, error)`
- `func (runtime *fakeRuntime) ResolveTaskByKeyFor(sessionID, key string) (dto.TaskRecord, bool, error)`
- `func (runtime *fakeRuntime) TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error)`
- `func (runtime *fakeRuntime) TaskSetStatusFor(sessionID, id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error)`
- `func (runtime *fakeRuntime) TaskAttachParticipant(id, participant string) (dto.TaskRecord, error)`
- `func (*fakeRuntime) TaskChangedChannel() <-chan dto.TaskRecord`
- `func (*fakeRuntime) SubagentTreeEvents() <-chan struct`
- `func (*fakeRuntime) PlanNodeEventChannel() <-chan dto.PlanNodeEvent`
- `func (runtime *fakeRuntime) SwitchSessionTasks(sessionID string, records []dto.TaskRecord)`
- `func (runtime *fakeRuntime) SetSessionWorkspace(sessionID, workspaceID string)`
- `func (runtime *fakeRuntime) ScheduledCommands() []seelebridge.ScheduledCommandInfo`
- `func (runtime *fakeRuntime) ScheduledTasksSnapshot() []seelebridge.ScheduledTaskStatus`
- `func (runtime *fakeRuntime) ScheduleTask(_ context.Context, spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error)`
- `func (runtime *fakeRuntime) CancelScheduledTask(id string) error`
- `func (runtime *fakeRuntime) ClearSubagentTree() error`
- `func (runtime *fakeRuntime) RestoreSubagentAnchors(string) error`
- `func (runtime *fakeRuntime) SearchHistory(_ context.Context, _ string, _ int) (seelexctxsearch.Result, error)`
- `func (runtime *fakeRuntime) BindProjectRoot(rootPath string) error`
- `func (runtime *fakeRuntime) UnbindProjectRoot()`
- `func (runtime *fakeRuntime) SetCurrentTaskBatch(sessionID, batchID string)` — SetCurrentTaskBatch 会被并行会话的多个 runChat 并发调用（M2：每个会话
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
- `func (sessions *scopedSessions) SessionsOf(projectID string) []SessionInfo` — SessionsOf 实现 session_runtime.SessionGranularPort：按项目索引枚举会话。
- `func (sessions *scopedSessions) LoadedWorkspace() string`
- `func (sessions *scopedSessions) resolveWorkspaceFor(sessionID string) (string, []EngineMessage, bool)` — resolveWorkspaceFor 返回会话历史所在 workspace（扫描 histories；未找到
- `func (sessions *scopedSessions) LoadHistory(sessionID string) ([]EngineMessage, error)` — LoadHistory 实现 SessionGranularPort：读取会话历史（会话粒度键；workspace
- `func (sessions *scopedSessions) LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error)` — LoadHistoryRange 实现 SessionGranularPort：按窗口读取会话历史。
- `func (sessions *scopedSessions) Delete(sessionID string) error` — Delete 实现 SessionGranularPort：从全部项目索引删除会话。
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
- `func (engine *gracefulShutdownEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 显式转发到自身 ChatStream（覆盖内嵌 fakeEngine 的提升方法，
- `func waitForChatCompletion(t *testing.T, service *Service)` — waitForChatCompletion 轮询直到当前 chat 结束。

### service_input.go

- `func (service *Service) injectPendingSubagentContexts()` — injectPendingSubagentContexts 排空 Runtime 持有的有界邮箱（活跃会话兼容
- `func (service *Service) injectPendingSubagentContextsFor(sessionID string)` — injectPendingSubagentContextsFor 排空 Runtime 持有的有界邮箱（单一来源 =
- `func (service *Service) recordSubagentEvidence(sessionID, content string)` — recordSubagentEvidence 把子代理合并回父的一条证据记录写入目标会话：
- `func (service *Service) chatStream(ctx context.Context, sessionID, input string, onChunk func(string)) (string, error)` — chatStream 向指定会话引擎提交流式对话（会话路由引擎用 ChatStreamFor，
- `func (service *Service) appendEngineMessage(sessionID string, msg types.Message)` — appendEngineMessage 追加消息到指定会话引擎历史。
- `func (service *Service) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error` — replaceEngineHistory 会话内替换指定会话引擎历史（会话路由引擎用
- `func (service *Service) engineHistoryFor(sessionID string) []contract.EngineMessage` — engineHistoryFor 返回指定会话引擎历史（只读拷贝）。
- `func (service *Service) Submit(ctx context.Context, text string) error`
- `func (service *Service) submitConversation(ctx context.Context, input string) error`
- `func (service *Service) submitConversationFor(ctx context.Context, sessionID, input string) error` — submitConversationFor 在指定（后台）会话提交对话：目标会话运行中则投递
- `func (service *Service) BeginGracefulShutdown()` — BeginGracefulShutdown 停止接收新输入，同时允许活跃 chat 及其已排队输入
- `func (service *Service) WaitForIdle(ctx context.Context) error` — WaitForIdle 等待全部已接受的 chat 工作完成。它从不取消活跃 chat；调用方
- `func (service *Service) AnyChatRunning() bool` — AnyChatRunning 报告是否存在任一会话的运行中回合（G0c 关闭语义：视图空闲
- `func (service *Service) CancelAllChats()` — CancelAllChats 取消全部会话的运行中回合（G0c 关闭超时路径：后台会话同样
- `func (service *Service) CancelChat(requestID string) bool` — CancelChat 取消当前视图会话正在运行的回合。
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
- `func (service *Service) pendingApprovalSession(id string) (string, bool)` — pendingApprovalSession 在 broker 待批集合中按审批 ID 反查归属会话
- `func (service *Service) appendPlanRetryNotice(message string)`
- `func (service *Service) abortPlanInteraction()`
- `func (service *Service) SelectAccount(_ context.Context, name string) error`
- `func (service *Service) SwitchEffort(_ context.Context, level string) error` — SwitchEffort 切换 Effort 等级（用户级动作，作用于视图会话）。
- `func (service *Service) SwitchPlugin(ctx context.Context, name string) error` — SwitchPlugin 切换/停用插件（进程级动作，G0b/M6）。
- `func (service *Service) SetFullAccess(on bool)`
- `func (service *Service) observeInteraction(sessionID, requestID string, interaction *Interaction)` — observeInteraction 是 ApprovalBroker 的开/结观察回调（波 4 approval 会话
- `func (service *Service) mirrorPendingApprovalsLocked(sessionID string)` — mirrorPendingApprovalsLocked 把指定会话当前首笔待批审批镜像到
- `func (service *Service) openInteraction(interaction *Interaction)`
- `func (service *Service) closeInteraction(id string)`
- `func (service *Service) sessionInteraction() *Interaction`
- `func (service *Service) accountInteraction() *Interaction`

### service_interaction_guard_test.go

- `func newRecordingPromptEngine() *recordingPromptEngine`
- `func (engine *recordingPromptEngine) SetSystemPromptFor(sessionID, prompt string)`
- `func (engine *recordingPromptEngine) SetSystemPrompt(prompt string)`
- `func (engine *recordingPromptEngine) SetMaxLoops(loops int)`
- `func (engine *recordingPromptEngine) ClearHistory()`
- `func (engine *recordingPromptEngine) promptSnapshot() (promptFor map[string]string, globalCount, loops, clears int)` — promptSnapshot 返回测试断言的引擎侧快照（加锁拷贝）。
- `func waitChatStarted(t *testing.T, started <-chan struct{})` — waitChatStarted 等待指定会话的 ChatStream 进入引擎（阻塞点已建立）。
- `func waitUnitIdle(t *testing.T, service *Service, sessionID string)` — waitUnitIdle 轮询指定会话单元直到其聊天停止运行（后台另一会话可能仍在跑，
- `func TestEffortAndPluginGuardsAroundRunningSessions(t *testing.T)` — TestEffortAndPluginGuardsAroundRunningSessions 覆盖 G0b 守卫：
- `func TestEffortCommandUsesGuardedServicePath(t *testing.T)` — TestEffortCommandUsesGuardedServicePath /effort 命令必须走 SwitchEffort：

### service_notice_test.go

- `func TestServiceAddNoticeAppendsSystemMessage(t *testing.T)`

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
- `func (service *Service) ListSessions() []SessionInfo` — ListSessions 返回当前权威会话目录（C1 冷读面/headless 宿主）：与会话树
- `func (service *Service) enrichDirectoryRowsLocked(rows []SessionInfo) []SessionInfo` — enrichDirectoryRowsLocked 给目录行补会话级可见状态（调用方持有
- `func (service *Service) sessionStatusLocked(sessionID string) SessionStatus` — sessionStatusLocked 返回指定会话的可见状态（调用方持有 Core.ViewMu）。
- `func (service *Service) Subscribe(buffer int) Subscription`
- `func (service *Service) collectRuntimeProjection(ctx context.Context) view_state.RuntimeStateProjection`
- `func (service *Service) collectRuntimeProjectionFor(ctx context.Context, sessionID string) view_state.RuntimeStateProjection` — collectRuntimeProjectionFor 按显式会话收集运行时投影（G1：后台会话的
- `func (service *Service) applyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection)`
- `func (service *Service) applyRuntimeProjectionForLocked(sessionID string, projection view_state.RuntimeStateProjection)` — applyRuntimeProjectionForLocked 应用运行时投影到指定会话槽（活跃会话由
- `func (service *Service) appendMessageLocked(role, content string, tool *ToolCall) *Message`
- `func (service *Service) appendSessionMessageLocked(sessionID, role, content string, tool *ToolCall) *Message` — appendSessionMessageLocked 追加一条可见消息到指定会话（阶段 1：后台会话
- `func (service *Service) setSessionChatLockedFor(sessionID string, chat ChatState)` — setSessionChatLockedFor 写指定会话的聊天运行态投影（活跃会话镜像
- `func (service *Service) mirrorActiveViewLocked()` — mirrorActiveViewLocked 把当前活跃会话 scope 镜像到 Snapshot。
- `func (service *Service) sessionViewLocked(sessionID string) *session.View` — sessionViewLocked 返回指定会话的可见投影（core 域工具/恢复路径用；
- `func (service *Service) recordReadFileForSessionLocked(sessionID, arguments string)` — recordReadFileForSessionLocked 记录指定会话的 read 文件引用（阶段 1：
- `func (service *Service) bumpLocked() uint64`
- `func (service *Service) addNotice(notice string)`
- `func (service *Service) AddNotice(notice string)` — AddNotice 追加一条系统通知（以 system 消息进入可见会话并发布
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
- `func TestNewTaskSessionIsTrulyUnbound(t *testing.T)` — TestNewTaskSessionIsTrulyUnbound 未关联工作区的会话必须真正未关联：
- `func TestWorkspaceSessionBindsDraftBeforeMaterialization(t *testing.T)` — TestWorkspaceSessionBindsDraftBeforeMaterialization 覆盖 GUI「工作区会话」
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
