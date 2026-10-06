# core/service（根包分卷）

## 生态位

Service 门面、装配根与跨域用例编排（输入/交互/调度/快照/测试夹具）

覆盖：`service*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### employee_permission_assembly_test.go

- `func (recorder *recordingEmployeePermissions) AssignEmployeePermissions(roles []dto.RoleSpec) error`
- `func (recorder *recordingEmployeePermissions) snapshot() [][]dto.RoleSpec`
- `func withEmployeePermissions(port contract.EmployeePermissionPort) testServiceOption`
- `func employeeRolesOf(calls [][]dto.RoleSpec) []string` — employeeRolesOf 取记录里出现过的员工角色（按出现顺序；员工 = agent/timer 角色）。
- `func assignedRoleNames(calls [][]dto.RoleSpec) []string` — assignedRoleNames 取记录里出现过的**全部**角色名（含主代理/评审者等内置角色）：
- `func TestAssemblyAssignsEmployeePermissions(t *testing.T)` — TestAssemblyAssignsEmployeePermissions：装配（写注册表）之后，员工权限分配被调用，
- `func TestAssemblySurfacesEmployeePermissionFailure(t *testing.T)` — TestAssemblySurfacesEmployeePermissionFailure：分配失败必须显式上抛。
- `func TestAssemblyWithoutEmployeePermissionsStillWorks(t *testing.T)` — TestAssemblyWithoutEmployeePermissionsStillWorks：未装配分配面时装配照常成功
- `func TestMaterializeAssignsEmployeePermissions(t *testing.T)` — TestMaterializeAssignsEmployeePermissions：装配团队（面板「一键装配」/ `@` 召唤 /

### fixture_concurrency_test.go

- `func TestTeamRecordingSessionsAccessorsAreRaceFree(t *testing.T)` — TestTeamRecordingSessionsAccessorsAreRaceFree 并发读写夹具：-race 下证明读写两侧
- `func TestFixturesDoNotBypassLockedAccessors(t *testing.T)` — TestFixturesDoNotBypassLockedAccessors 是防复发的机械防线：AST 扫描本包测试源码，

### role_tool_activity_test.go

- `func waitRoleToolEvent(t *testing.T, subscription Subscription, wantKind EventKind) Event` — waitRoleToolEvent 等一条 teammate.tool.* 事件（超时即失败）。
- `func TestHandleRoleToolActivityPublishesSessionEvent(t *testing.T)`
- `func TestHandleRoleToolActivityTruncatesAndDropsUnroutable(t *testing.T)`

### service.go

- `func New(deps Dependencies) (*Service, error)`
- `func (service *Service) ActiveSkillIDs() []string` — ActiveSkillIDs 返回当前任务的激活 skill ID 列表（goal skill 激活判定用，
- `func (service *Service) PromptLayers() []PromptLayer` — PromptLayers 返回当前会话注入的 prompt 前缀层（system/base/effort/
- `func (service *Service) GoalSkillActive() bool` — GoalSkillActive 返回最新的本地投影（诊断与测试用）。Runtime 经
- `func (service *Service) PublishRuntimeProjections()` — PublishRuntimeProjections 刷新 Runtime 的不可变状态副本。供在
- `func (service *Service) HandleSubagentToolEvent(event seelsession.SubagentToolEvent)` — HandleSubagentToolEvent 把 Runtime 工具分发投影进权威 Plan 节点快照并
- `func (service *Service) HandleRoleToolActivity(event dto.RoleToolActivity)` — HandleRoleToolActivity 把 Runtime 的**员工回合**工具活动投影成会话级实时事件
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
- `func TestRuntimeMailboxDrainedAndDiscarded(t *testing.T)`
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
- `func (runtime *fakeRuntime) VisibleTools(context.Context) []Tool`
- `func (*fakeRuntime) ActivePlugin() string`
- `func (runtime *fakeRuntime) FullAccess() bool`
- `func (runtime *fakeRuntime) SetFullAccess(on bool)`
- `func (runtime *fakeRuntime) SetFullAccessFor(sessionID string, on bool)` — SetFullAccessFor 镜像生产权限门的会话级解析（空会话 ID = 进程级默认）：
- `func (runtime *fakeRuntime) FullAccessFor(sessionID string) bool` — FullAccessFor 返回指定会话生效的全权模式（会话级选择优先，未选择回退
- `func (runtime *fakeRuntime) PermissionTier() string` — PermissionTier 返回进程级默认权限档位（fake 只有二元口径：fullAccess → full）。
- `func (runtime *fakeRuntime) SetPermissionTierFor(sessionID, tier string) error` — SetPermissionTierFor 把档位落到 fake 的二元全权口径（full → true，其余 → false），
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
- `func (runtime *fakeRuntime) TaskSnapshot() []dto.TaskRecord` — TaskSnapshot 返回**项目/全局** task 表（镜像生产 Runtime：实时注册表 +
- `func (runtime *fakeRuntime) ReleaseSessionAsync(sessionID string) int` — ReleaseSessionAsync 记下被释放的会话，供"会话销毁即杀后台命令"的用例断言。
- `func (runtime *fakeRuntime) AsyncPendingFor(string) int` — AsyncPendingFor 回答测试显式设置的在途后台命令数。
- `func (runtime *fakeRuntime) AsyncRunsSnapshot() []dto.AsyncRunRecord` — AsyncRunsSnapshot 回答测试显式设置的后台执行投影（后台行投影用例的输入源）。
- `func (runtime *fakeRuntime) AsyncRunEvents() <-chan struct` — AsyncRunEvents 返回测试自己持有的信号口（默认 nil = 消费者不启动）。
- `func (runtime *fakeRuntime) TeamworkJobCompletions() []dto.TeamworkJobCompletionRecord` — TeamworkJobCompletions / TeamworkJobEvents 回答 teammate 作业表（jobs.Manager）的
- `func (runtime *fakeRuntime) TeamworkJobEvents() <-chan struct`
- `func (runtime *fakeRuntime) TaskSnapshotFor(sessionID string) []dto.TaskRecord` — TaskSnapshotFor 保持会话粒度（持久化落盘/请求尾部打点块用）。
- `func (runtime *fakeRuntime) globalSnapshotLocked() []dto.TaskRecord` — globalSnapshotLocked 合并实时注册表与所有会话分区（跨会话身份去重：
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
- `func (runtime *fakeRuntime) ListSubagentRecovery(string) ([]dto.SubagentRecoveryView, error)`
- `func (runtime *fakeRuntime) ResumeInterruptedSubagents(context.Context, string) (dto.SubagentResumeReport, error)`
- `func (runtime *fakeRuntime) ResumeSubagent(context.Context, string, string) (dto.SubagentResumeResult, error)`
- `func (runtime *fakeRuntime) ForkSubagents(context.Context, string, []dto.SubagentForkSpec) (string, error)`
- `func (runtime *fakeRuntime) SetSubagentParentRepairer(func(string) error)`
- `func (runtime *fakeRuntime) SearchHistory(_ context.Context, _ string, _ int) (seelexctxsearch.Result, error)`
- `func (runtime *fakeRuntime) BindProjectRoot(rootPath string) error`
- `func (runtime *fakeRuntime) UnbindProjectRoot()`
- `func (runtime *fakeRuntime) ProjectRoot() string` — ProjectRoot 读当前绑定的项目根（加锁）：写侧可能来自后台 chat goroutine，
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

- `func (service *Service) discardPendingSubagentContexts()` — discardPendingSubagentContexts 排空 Runtime 持有的有界邮箱（活跃会话兼容
- `func (service *Service) discardPendingSubagentContextsFor(_ string)` — discardPendingSubagentContextsFor 排空 Runtime 持有的有界邮箱（单一来源 =
- `func (service *Service) chatStream(ctx context.Context, sessionID, input string, onChunk func(string)) (string, error)` — chatStream 向指定会话引擎提交流式对话（会话路由引擎用 ChatStreamFor，
- `func (service *Service) appendEngineMessage(sessionID string, msg types.Message)` — appendEngineMessage 追加消息到指定会话引擎历史。
- `func (service *Service) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error` — replaceEngineHistory 会话内替换指定会话引擎历史（会话路由引擎用
- `func (service *Service) engineHistoryFor(sessionID string) []contract.EngineMessage` — engineHistoryFor 返回指定会话引擎历史（只读拷贝）。
- `func (service *Service) clearEngineHistoryFor(sessionID string)` — clearEngineHistoryFor 清空指定会话引擎历史（会话路由引擎用 ClearHistoryFor，
- `func (service *Service) Submit(ctx context.Context, text string) error`
- `func (service *Service) submitConversation(ctx context.Context, input string) error`
- `func (service *Service) submitConversationFor(ctx context.Context, sessionID, input string) error` — submitConversationFor 在指定（后台）会话提交对话：目标会话运行中则投递
- `func (service *Service) BeginGracefulShutdown()` — BeginGracefulShutdown 停止接收新输入，同时允许活跃 chat 及其已排队输入
- `func (service *Service) WaitForIdle(ctx context.Context) error` — WaitForIdle 等待全部已接受的 chat 工作完成。它从不取消活跃 chat；调用方
- `func (service *Service) AnyChatRunning() bool` — AnyChatRunning 报告是否存在任一会话的运行中回合（G0c 关闭语义：视图空闲
- `func (service *Service) CancelAllChats()` — CancelAllChats 取消全部会话的运行中回合（G0c 关闭超时路径：后台会话同样
- `func (service *Service) CancelChat(requestID string) bool` — CancelChat 是"停止按钮"的终止原语，语义按以下顺序成立（顺序即语义）：
- `func (service *Service) Shutdown()`

### service_input_test.go

- `func trustedSkillInHistory(t *testing.T, history []EngineMessage, name, text string) bool` — trustedSkillInHistory 断言引擎历史中存在激活技能 internal 事件
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
- `func (service *Service) reapplyEffortAfterPluginSwitch()` — reapplyEffortAfterPluginSwitch 在插件切换后重新应用**用户当前的 effort
- `func (service *Service) SetPermissionTier(tier string) (string, error)` — SetPermissionTier 切换**视图会话的权限档位**，并返回真正生效的档位。
- `func (service *Service) SetFullAccess(on bool) bool` — SetFullAccess 是权限档位的**兼容壳**：true → full 档、false → manual 档；返回
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
- `func TestPluginSwitchPreservesEffort(t *testing.T)` — TestPluginSwitchPreservesEffort：插件切换只换 prompt 前缀，不得顺手把用户的

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

### service_queue.go

- `func (service *Service) ReorderQueuedInput(sessionID string, from, to int) error` — ReorderQueuedInput 把目标会话排队输入中 from 位置的条目移动到 to 位置
- `func (service *Service) RecallQueuedInput(sessionID string, index int) (string, error)` — RecallQueuedInput 把目标会话排队输入中 index 位置的条目撤回（出队）并返回
- `func (service *Service) queueEditGuardLocked() error` — queueEditGuardLocked 是队列编辑的关闭/排空门禁（与 Submit 同口径）。
- `func (service *Service) queuedInputUnitLocked(sessionID string) (*session.SessionUnit, error)` — queuedInputUnitLocked 解析队列编辑的目标会话单元（调用方持有 Core.ViewMu）。
- `func (service *Service) applyQueueEditLocked(unit *session.SessionUnit)` — applyQueueEditLocked 在队列编辑成功后重投影该会话的 ChatState.InputQueue /

### service_queue_test.go

- `func enqueueRawQueuedInput(t *testing.T, service *Service, text string)` — enqueueRawQueuedInput 用会话域 API 直接排队一条非 chatRequest 载荷的输入：
- `func TestQueueEditIndexSpaceMatchesProjection(t *testing.T)` — TestQueueEditIndexSpaceMatchesProjection（节点 156 红线）：ChatState.InputQueue
- `func TestReorderQueuedInputFollowsProjectionIndex(t *testing.T)` — TestReorderQueuedInputFollowsProjectionIndex：调换的下标是投影下标——旧实现
- `func startBlockingChat(t *testing.T) (*Service, *blockingEngine)` — startBlockingChat 启动一个阻塞中的回合（返回可在测试里显式放开的引擎）。
- `func enqueueQueuedInputs(t *testing.T, service *Service, inputs ...string)` — enqueueQueuedInputs 在运行中的会话里排队若干输入（按给定顺序）。
- `func TestReorderQueuedInputReordersVisibleQueue(t *testing.T)` — TestReorderQueuedInputReordersVisibleQueue 调换排队顺序：可见投影
- `func TestReorderQueuedInputDrivesPromotedBatchOrder(t *testing.T)` — TestReorderQueuedInputDrivesPromotedBatchOrder 调换后的顺序决定下一轮
- `func TestRecallQueuedInputPopsEntryAndReturnsText(t *testing.T)` — TestRecallQueuedInputPopsEntryAndReturnsText 撤回 = 出队 + 交还展示原文：
- `func TestQueueEditErrorSemantics(t *testing.T)` — TestQueueEditErrorSemantics 越界 / 非运行态 / 会话不存在都有明确错误。
- `func TestQueueEditErrorsBeforeAnySession(t *testing.T)` — TestQueueEditErrorsBeforeAnySession 进程还没有任何会话单元时，视图会话按
- `func TestConcurrentRecallQueuedInput(t *testing.T)` — TestConcurrentRecallQueuedInput（-race）并发撤回同一会话：撤回与 Enqueue /

### service_scheduler.go

- `func (service *Service) ScheduleTask(ctx context.Context, spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error)` — ScheduleTask 创建并启动一个定时/周期任务（校验在 Runtime 调度器内完成）。
- `func (service *Service) CancelScheduledTask(id string) error` — CancelScheduledTask 取消并移除定时/周期任务。
- `func (service *Service) RefreshRuntimeSnapshot()` — RefreshRuntimeSnapshot 重新收集运行时投影（含定时/周期任务快照）并发布

### service_snapshot.go

- `func (service *Service) Snapshot() Snapshot`
- `func (service *Service) ListSessions() []SessionInfo` — ListSessions 返回当前权威会话目录（C1 冷读面/headless 宿主）：与会话树
- `func (service *Service) enrichDirectoryRowsLocked(rows []SessionInfo) []SessionInfo` — enrichDirectoryRowsLocked 给目录行补会话级可见状态（调用方持有
- `func (service *Service) sessionStatusLocked(sessionID string) SessionStatus` — sessionStatusLocked 返回指定会话的可见状态（调用方持有 Core.ViewMu）。
- `func (service *Service) isRestoringLocked(sessionID string) bool` — isRestoringLocked 报告目标会话是否处于后台冷加载（调用方持有
- `func (service *Service) setRestoringLocked(sessionID string)` — setRestoringLocked 标记目标会话进入后台冷加载（调用方持有 Core.ViewMu）。
- `func (service *Service) clearRestoringLocked(sessionID string)` — clearRestoringLocked 移除目标会话的后台冷加载标记（调用方持有
- `func (service *Service) signalRestoreLocked()` — signalRestoreLocked 广播一次“restoring 集合已变化”（调用方持有
- `func (service *Service) restoreSignalLocked() <-chan struct` — restoreSignalLocked 返回当前 restoring 变化信号（调用方持有 Core.ViewMu）。
- `func (service *Service) nextViewEpochLocked() uint64` — nextViewEpoch 推进视图切换序号并返回新值（调用方持有 Core.ViewMu）。
- `func (service *Service) Subscribe(buffer int) Subscription`
- `func (service *Service) refreshRuntimeProjectionForSession(sessionID string)` — refreshRuntimeProjectionForSession 是会话（重）激活时的**重建**一步：按目标会话从
- `func (service *Service) collectRuntimeProjection(ctx context.Context) view_state.RuntimeStateProjection`
- `func (service *Service) collectRuntimeProjectionFor(ctx context.Context, sessionID string) view_state.RuntimeStateProjection` — collectRuntimeProjectionFor 按显式会话收集运行时投影（G1：后台会话的
- `func (service *Service) applyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection)`
- `func (service *Service) applyRuntimeProjectionForLocked(sessionID string, projection view_state.RuntimeStateProjection)` — applyRuntimeProjectionForLocked 应用运行时投影到指定会话槽（活跃会话由
- `func (service *Service) appendMessageLocked(role, content string, tool *ToolCall) *Message`
- `func (service *Service) appendMessageWithOriginLocked(role, content string, tool *ToolCall, origin MessageOrigin) *Message` — appendMessageWithOriginLocked 追加一条带群聊归属的可见消息到活跃会话
- `func (service *Service) appendSessionMessageLocked(sessionID, role, content string, tool *ToolCall) *Message` — appendSessionMessageLocked 追加一条可见消息到指定会话（阶段 1：后台会话
- `func (service *Service) appendSessionMessageWithOriginLocked(sessionID, role, content string, tool *ToolCall, origin MessageOrigin) *Message` — appendSessionMessageWithOriginLocked 追加一条带群聊归属的可见消息到指定
- `func (service *Service) appendAssistantPlaceholderAfterToolLocked(sessionID string) *Message` — appendAssistantPlaceholderAfterToolLocked 在指定会话的 tool_result 之后补一条
- `func (service *Service) setSessionChatLockedFor(sessionID string, chat ChatState)` — setSessionChatLockedFor 写指定会话的聊天运行态投影（活跃会话镜像
- `func (service *Service) mirrorActiveViewLocked()` — mirrorActiveViewLocked 把当前活跃会话 scope 镜像到 Snapshot。
- `func (service *Service) sessionViewLocked(sessionID string) *session.View` — sessionViewLocked 返回指定会话的可见投影（core 域工具/恢复路径用；
- `func (service *Service) sessionViewEmptyLocked(sessionID string) bool` — sessionViewEmptyLocked 报告指定会话的可见会话是否为空（调用方持有
- `func (service *Service) recordReadFileForSessionLocked(sessionID, arguments string)` — recordReadFileForSessionLocked 记录指定会话的 read 文件引用（阶段 1：
- `func (service *Service) bumpLocked() uint64`
- `func (service *Service) addNotice(notice string)`
- `func (service *Service) AddNotice(notice string)` — AddNotice 追加一条系统通知（以 system 消息进入可见会话并发布
- `func (service *Service) resetConversation(notice string)`
- `func (service *Service) advanceMessageSeqLocked(sessionID string, messages []Message)` — advanceMessageSeqLocked 按既有消息 ID 推进**该会话**的消息派号（恢复路径委托）。

### service_snapshot_test.go

- `func TestSnapshotNeverSerializesSystemPrompt(t *testing.T)`
- `func TestEventHubOrdersAndResyncs(t *testing.T)`
- `func TestMessageDeltaIncludesStableMessageID(t *testing.T)`
- `func TestToolEventsUpdateSnapshot(t *testing.T)`
- `func TestToolCompletionDoesNotReenterServiceLockForGoalSkillVisibility(t *testing.T)`

### service_stop_flush_test.go

- `func (engine *stopQueueEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *stopQueueEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 显式转发到自身 ChatStream（覆盖内嵌 fakeEngine 的提升方法，
- `func (engine *stopQueueEngine) recordedInputs() []string`
- `func TestStopFlushesQueuedInputsIntoTheNextTurn(t *testing.T)` — TestStopFlushesQueuedInputsIntoTheNextTurn 钉住停止按钮的第三条语义：点停止后清空

### service_subagent_resume.go

- `func (service *Service) ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error)` — ListSubagentRecovery 列出目标会话下残留子代理单元的恢复态（只读）。
- `func (service *Service) ResumeInterruptedSubagents(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error)` — ResumeInterruptedSubagents 冷恢复续跑目标会话下所有未完成子代理。
- `func (service *Service) ResumeSubagent(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error)` — ResumeSubagent 定点续跑单个子代理（失败可重试）。
- `func (service *Service) PrepareProviderHistory(sessionID string) error` — PrepareProviderHistory 补齐目标会话 provider 历史的残缺工具链：中断的
- `func (service *Service) ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error)` — ForkSubagents 直接派发一批子代理（自动化/冒烟入口；与模型调用

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
- `func TestBeginNewSessionKeepsGlobalWorkTable(t *testing.T)` — TestBeginNewSessionKeepsGlobalWorkTable 工作表格是项目/全局台账：/new
- `func TestResumedChatPersistsToSelectedSession(t *testing.T)`
- `func TestLoadMoreHistoryAssignsStableMessageIDs(t *testing.T)`
- `func TestResumeCommandOpensSessionInteraction(t *testing.T)`

### teamwork_board_projection_test.go

- `func (r teamBoardRuntime) TeamworkBoardSnapshot(string) *dto.TeamworkBoardView`
- `func teamBoardFixture() *dto.TeamworkBoardView`
- `func TestCollectRuntimeProjectionCarriesTeamworkBoard(t *testing.T)`
- `func TestCollectRuntimeProjectionOmitsTeamworkBoardWithoutPort(t *testing.T)`
- `func TestTeamworkBoardViewForWithoutPort(t *testing.T)`

### teamwork_board_session_switch_test.go

- `func (r *sessionSwitchBoardRuntime) TeamworkBoardSnapshot(sessionID string) *dto.TeamworkBoardView`
- `func TestSessionReactivationRebuildsTeamBoardProjection(t *testing.T)`

### teamwork_completion_trigger_test.go

- `func teamworkCompletionHarness(t *testing.T, trigger bool, engine ChatEngine) (*Service, *fakeRuntime)` — teamworkCompletionHarness 造一个装配好 teammate 触发路径的 Service。
- `func completedTeammateRecord(handle string, state dto.AsyncState) dto.TeamworkJobCompletionRecord` — completedTeammateRecord 造一条已落到终态的 teammate 作业记录。
- `func TestTeamworkCompletionTriggersIdleSessionTurn(t *testing.T)`
- `func TestTeamworkCompletionTriggersOnFailureToo(t *testing.T)`
- `func TestTeamworkCompletionIgnoresRunningAndKilled(t *testing.T)`
- `func TestTeamworkCompletionDoesNotWakeBusySession(t *testing.T)` — 铁律 §6.1「绝不唤醒忙会话」：忙的时候不起回合，跳过的条目**不记账**——会话回到空闲
- `func TestTeamworkCompletionTriggersOncePerHandle(t *testing.T)` — 幂等：句柄在册期间只触发一次（句柄单调不复用，进程内一个集合就够）。
- `func TestTeamworkCompletionDoesNotCollideWithToolHandles(t *testing.T)` — 两张表的句柄空间**各自独立**（都是从 a<seq> 起步）：同一个字面量句柄在两张表里同时
- `func TestTeamworkCompletionStaysOffWhenDisabled(t *testing.T)` — 开关关闭时连扫描都不做（与 subagent 那条同一个开关）。
- `func TestTeamworkTraceLinesCarryCompletionReceipt(t *testing.T)` — 忙会话的读法：**回合边界打点块**里的 teammate 完成行（这是"回执被看见"的另一半——

### teamwork_service.go

- `func (service *Service) TeamworkBoardViewFor(sessionID string) *dto.TeamworkBoardView` — TeamworkBoardViewFor 返回指定会话的团队看板只读投影（无计划 / 未装配 → nil，
- `func (service *Service) TeammateSessionLiveFor(sessionID string) dto.TeammateSessionLiveView` — TeammateSessionLiveFor 返回**当前 teammate 会话**的实时只读投影（"这件事的会话此刻在
- `func (service *Service) TeammateSessionLivePageFor(sessionID string, offset, limit int) dto.TeammateSessionLiveView` — TeammateSessionLivePageFor 返回**当前 teammate 会话**实时读数的分页一页（有界窗口 +
