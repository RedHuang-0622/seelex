# assembly/service（根包分卷）

## 生态位

Service 门面、装配根与跨域用例编排（输入/交互/调度/快照/测试夹具）

覆盖：`service*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

根 `application` 只保留稳定别名、常量与无状态 helper；旧四组兼容接口与
AP-002 能力接口定义均已退场，assembly 不再保存任何入站宽接口。

## Policy Application 首批（AP-054 首个切片）

`Policy switch application` 集中技能激活、reasoning/loop 两条独立拉条、
Session release window 和 FullAccess 兼容壳。底层 `limit.Knobs`、
`skill.Activator`、`middle/switch` 和权限档位状态机仍是唯一判据来源。

AP-055 进一步把 PermissionTier 本体的持久化、SessionUnit、审批门、Runtime 门、
生效档位和 runtime 事件收拢到该组件。

AP-056 把 Effort 切换的视图空闲守卫、cycle/apply、subagent/reasoning 下发、
prompt 同步和 runtime 收口收拢到同一组件。

AP-057 把 Plugin 切换的全进程空闲守卫、activate/deactivate、prompt/history
重建、Effort 恢复、会话 reset 和 runtime 发布收拢到同一组件。

AP-058 新增 `service_admin_application.go`，集中 Admin 账号添加、详情、更新、
删除和余额查询的调用时端口解析、输入裁剪与缺失能力错误。
`Account administration application` 的五个入口只保留一行转发；账号档写入与余额探测仍是宿主
端口中的唯一实现。

AP-059 把 Admin 账号选择收拢到同一组件：Runtime 准入、runtime projection、
锁内账户/投影/revision、解锁后事件发布和通知保持原顺序；
GUI 的 `Accounts` 消费方端口直接调用同一组件。

AP-061 把 Admin 账号窗口收拢到同一组件：nil 守卫、可选 Runtime 端口解析与
`accountwindow.Setter` 适配；`Service.SetAccountWindow` 只保留一行转发。

## Components 与消费方依赖

统一前端适配器已退场。本包只通过 `ComponentsFor(service)`
提供一份**分组**只读快照（`Components` 只有字段，持 concrete owner 指针），
不再保存账号、角色、Workspace 与输入查询的公开转发方法。
GUI、TUI、headless 与 console 各自声明消费方窄接口，由 `wiring/bootstrap`
把组件快照中的 owner 指针逐字段注入。能力有无由装配表决定，缺 owner 即装配错误；
不存在运行时能力探测或统一转发层。

EP-046A 已删除无参、单返回值的 `*ApplicationComponent` getter，包内调用直接
读取 `service.components`。EP-046B 已把全部自由 `newService*` 构造器收拢进
`service_assembler_*.go` 的组合阶段，结构门禁要求该前缀为 0；核心私有状态
仍只在组合阶段读取，没有复制进领域包。EP-046C 已把接口清单与编译期钉子移到
根 Application。

Session 事件订阅判据已归 `application/session/viewstate.Coordinator`：
`SubscribeSession` 与 `SubscribeSessionWithReplay` 直接消费 `session.Domain`
的视图指针和 `EventHub` 的可选过滤/重放能力，assembly 不再定义过滤谓词。

ToolRecord 的开关、flush、看板和命令退出码入口也由 compat 直接调用
`application/execution/toolrecord.Application`；assembly 只保留草稿可写判据、
主代理 outcome 与全量 shutdown flush 三个 host adapter。

AP-068 新增 `schedulerApplication`，集中定时任务创建、编辑、取消的默认参数、
workspace/plugin/执行参数校验顺序与成功后的 runtime 投影刷新。
三条旧入口只保留一行转发。

AP-069 把 `RefreshRuntimeSnapshot` 的 projection 锁边界与发布顺序移入
`runtimeApplication`。AP-070 把提交前的定时运行装配移入
`scheduledRunApplication`；AP-071 把定时新会话顶层编排移入
`scheduledSessionApplication`。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### component_getter_boundary_test.go

- `func TestCoreDoesNotExposeComponentGetters(t *testing.T)` — TestCoreDoesNotExposeComponentGetters pins the composition boundary: once a
- `func TestCoreDoesNotOwnInboundAPIContracts(t *testing.T)` — TestCoreDoesNotOwnInboundAPIContracts keeps consumer-facing interfaces at
- `func TestCoreHasNoComponentMethodForwarders(t *testing.T)` — TestCoreHasNoComponentMethodForwarders removes the second forwarding layer:
- `func isComponentsForwardCall(body *ast.BlockStmt) bool`
- `func startsWithServiceComponents(expression ast.Expr) bool`
- `func hasServiceReceiver(function *ast.FuncDecl) bool`

### components.go

- `func ComponentsFor(service *Service) Components` — ComponentsFor returns a read-only snapshot of assembled component owners.

### fixture_concurrency_test.go

- `func TestTeamRecordingSessionsAccessorsAreRaceFree(t *testing.T)` — TestTeamRecordingSessionsAccessorsAreRaceFree 并发读写夹具：-race 下证明读写两侧
- `func TestFixturesDoNotBypassLockedAccessors(t *testing.T)` — TestFixturesDoNotBypassLockedAccessors 是防复发的机械防线：AST 扫描本包测试源码，

### role_tool_activity_test.go

- `func waitRoleToolEvent(t *testing.T, subscription event.Subscription, wantKind event.EventKind) event.Event` — waitRoleToolEvent 等一条 teammate.tool.* 事件（超时即失败）。
- `func TestHandleRoleToolActivityPublishesSessionEvent(t *testing.T)`
- `func TestHandleRoleToolActivityTruncatesAndDropsUnroutable(t *testing.T)`

### service.go

- `func (service *Service) projectBoardSink() board.ProjectBoardSink` — projectBoardSink 在锁内只复制接口值；外部同步器不得在装配锁下执行。
- `func New(deps contract.Dependencies, ports ExternalPorts) (*Service, error)`

### service_account_test.go

- `func withTestAccountAdmin(admin contract.AccountAdminPort) testServiceOption` — withTestAccountAdmin 注入「手动添加账号」的宿主能力面（不注入 = 该宿主不支持
- `func (admin *fakeAccountAdmin) AddAccount(draft dto.AccountDraft) (dto.AccountAddResult, error)`
- `func (admin *fakeAccountAdmin) AccountDetails(name string) (dto.AccountDetails, error)`
- `func (admin *fakeAccountAdmin) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error)`
- `func (admin *fakeAccountAdmin) DeleteAccount(name string) (dto.AccountDetails, error)`
- `func TestAddAccountNeedsHostPort(t *testing.T)` — TestAddAccountNeedsHostPort：宿主没装 AccountAdmin 时要给一句明确错误，而不是
- `func TestAddAccountForwardsDraftVerbatim(t *testing.T)` — TestAddAccountForwardsDraftVerbatim：草稿（含 api_key）必须原样递到宿主——本层
- `func TestAddAccountPropagatesHostError(t *testing.T)` — TestAddAccountPropagatesHostError：宿主的错误要原样冒上来（界面靠这句话判断
- `func TestUpdateAndDeleteAccountForwardIdentities(t *testing.T)`
- `func (runtime *balanceFakeRuntime) AccountBalance(_ context.Context, name string) (dto.AccountBalance, error)`
- `func TestAccountBalanceNeedsHostPort(t *testing.T)` — TestAccountBalanceNeedsHostPort：宿主没装余额能力时明确报错（demo/headless 宿主）。
- `func TestAccountBalanceForwardsName(t *testing.T)` — TestAccountBalanceForwardsName：按名字查（不是"当前选中账号"），读数原样返回。
- `func TestAccountBalancePropagatesHostError(t *testing.T)` — TestAccountBalancePropagatesHostError：探测失败（含"该供应商没有余额接口"）

### service_account_window_forward_test.go

- `func TestSetAccountWindowNeedsRuntimePort(t *testing.T)`
- `func (store *recordingWindowStoreForForward) SaveAccountWindow(string, int) error`

### service_admin_application.go

- `func (port serviceAdminProjectionPort) Collect(ctx context.Context) view_state.RuntimeStateProjection`
- `func (port serviceAdminProjectionPort) LockView()`
- `func (port serviceAdminProjectionPort) SetRuntimeAccount(name string)`
- `func (port serviceAdminProjectionPort) ApplyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection)`
- `func (port serviceAdminProjectionPort) BumpLocked() uint64`
- `func (port serviceAdminProjectionPort) UnlockView()`
- `func (port serviceAdminProjectionPort) CurrentViewSessionID() string`
- `func (port serviceAdminProjectionPort) RuntimeSnapshot() model.RuntimeState`
- `func (port serviceAdminProjectionPort) PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState)`
- `func (port serviceAdminProjectionPort) AddNotice(message string)`

### service_admin_application_test.go

- `func TestServiceAdminComponentUsesDomainApplication(t *testing.T)`

### service_assembler.go

- `func (p taskPromptPort) CurrentEffort() string`
- `func (p taskPromptPort) ClearSkillLayers()`
- `func (p taskPromptPort) PushSkillLayer(kind, name, text string)`
- `func (assembler serviceAssembler) assemble() (*Service, error)`
- `func validateDependencies(deps contract.Dependencies) error`
- `func (assembler *serviceAssembler) applyInfrastructureDefaults()`
- `func (service *Service) restoreInitialWorkspace()`

### service_assembler_applications.go

- `func (service *Service) assembleExecutionApplication(reader applicationexecution.BoardReader)`
- `func (service *Service) assembleRoleActivityApplication()`
- `func (service *Service) assembleRoleApplication()`
- `func (service *Service) assembleReviewApplication(newProcessor applicationreview.ProcessorFactory)`
- `func (service *Service) assembleHistorySearchApplication()`
- `func (service *Service) assembleProjectBoardQuery()`
- `func (service *Service) assemblePerfStatsApplication()`
- `func (service *Service) assembleConversationQueueApplication()`
- `func (service *Service) assembleConversationSessionApplication()`
- `func (service *Service) assembleSubmissionApplication()`
- `func (service *Service) assembleInteractionApplication()`
- `func (service *Service) assembleForkApplication()`
- `func (service *Service) assembleSubmitApplication()`
- `func (service *Service) assembleTerminalHandlerApplication()`
- `func (service *Service) assembleFaultGuardApplication()`
- `func (service *Service) assembleGoalApplication(hostSessionForToolCall func(context.Context) string)`
- `func (service *Service) assemblePolicySwitchApplication()`
- `func (service *Service) assembleFrontendSnapshotApplication()`
- `func (service *Service) assembleSubagentApplication()`

### service_assembler_capabilities.go

- `func (service *Service) assembleRuntimeApplication()`
- `func (service *Service) assembleAdminApplication()`
- `func (service *Service) assembleWorkspaceApplication()`
- `func (service *Service) assembleAsyncCompletionApplication()`
- `func (service *Service) assembleCompletionApplication()`
- `func (service *Service) assembleContextCompactionApplication()`
- `func (service *Service) assembleReferenceApplication()`

### service_assembler_conversation_turn.go

- `func (service *Service) assembleConversationTurnCoreComponents()` — assembleConversationTurnCoreComponents wires the turn stages that may run as
- `func (service *Service) assembleConversationTurnRuntimeComponents()` — assembleConversationTurnRuntimeComponents wires stages that depend on the

### service_assembler_edit_fork.go

- `func (service *Service) assembleEditForkComponents()`

### service_assembler_scheduler_lifecycle.go

- `func (service *Service) assembleSchedulerApplications()`
- `func (service *Service) assembleKnobs()`
- `func (service *Service) assembleLifecycleApplication()`

### service_assembler_session.go

- `func (service *Service) assembleSessionSSHApplication()`
- `func (service *Service) assembleSessionMetaApplication()`
- `func (service *Service) metaDeps() switching.MetaDeps`
- `func (service *Service) metaPort() sessionstate.SessionMetaPort`
- `func (service *Service) wakeCatalogRefresh()`
- `func (service *Service) assembleSessionReleaseApplication()`
- `func (service *Service) assembleSessionUseCaseApplications()`

### service_assembler_session_cold.go

- `func (service *Service) assembleSessionColdComponents()` — assembleSessionColdComponents owns the cold-restore dependency graph. The

### service_assembler_session_draft.go

- `func (service *Service) assembleSessionDraftComponents()`

### service_assembler_session_query.go

- `func (service *Service) assembleSessionReadApplications(historyWindowAssembler applicationhistory.WindowAssembler)`

### service_chat_test.go

- `func TestReActBudgetStopsOnlyAfterItsToolBudget(t *testing.T)`
- `func TestReActBudgetUsesReservedFinalDeliveryTurn(t *testing.T)`
- `func TestReActBudgetFinalizationPromptSummarizesBudgetWork(t *testing.T)` — TestReActBudgetFinalizationPromptSummarizesBudgetWork 钉住用户裁决的 P2 语义：
- `func TestRuntimeMailboxDrainedAndDiscarded(t *testing.T)`
- `func TestSessionBackedIterationInterruptsOnQueuedInput(t *testing.T)`
- `func TestChatPublishesSnapshotWithoutUI(t *testing.T)`
- `func TestGracefulShutdownWaitsForQueuedChat(t *testing.T)`
- `func TestSessionBackedQueueIsConsumedAtRunChatEnd(t *testing.T)`
- `func TestCancelChatInterruptsContextAwareEngine(t *testing.T)`
- `func TestCancelChatWithStaleRequestID(t *testing.T)` — TestCancelChatWithStaleRequestID 验证取消语义归属：request_id 只是参考，动作

### service_components.go

- `func (port *ProjectBoardSinkPort) Store(sink board.ProjectBoardSink)`
- `func (port *ProjectBoardSinkPort) load() board.ProjectBoardSink`

### service_components_test.go

- `func TestNewAssemblesFocusedServiceComponents(t *testing.T)`
- `func TestServiceFacadeContainsOnlyAssembly(t *testing.T)`
- `func TestFocusedComponentsDoNotHoldServiceFacade(t *testing.T)`

### service_constructor_boundary_test.go

- `func TestConversationTurnConstructorsStayInCompositionRoot(t *testing.T)` — TestConversationTurnConstructorsStayInCompositionRoot prevents each
- `func TestSessionColdConstructorsStayInCompositionRoot(t *testing.T)`
- `func TestEditForkConstructorsStayInCompositionRoot(t *testing.T)`
- `func TestSessionConstructorsStayInCompositionRoot(t *testing.T)`
- `func TestConversationHistoryQueryConstructorStaysInCompositionRoot(t *testing.T)`
- `func TestExtractedApplicationConstructorsStayInCompositionRoot(t *testing.T)`
- `func TestNoFreeServiceConstructors(t *testing.T)`
- `func requireNoFreeConstructors(t *testing.T, prefix string)`

### service_effort_wire_test.go

- `func (runtime *effortWireRuntime) SetSessionReasoningEffort(effort string) int`
- `func (runtime *effortWireRuntime) pushedEfforts() []string`
- `func newEffortWireService(t *testing.T) (*Service, *effortWireRuntime)`
- `func TestSwitchEffortPushesWireReasoningEffort(t *testing.T)` — TestSwitchEffortPushesWireReasoningEffort：档位变化必须把该档对应的 wire 思考
- `func TestSwitchEffortCyclePushesNextLevel(t *testing.T)` — TestSwitchEffortCyclePushesNextLevel：cycle 入口（GUI 控件/TUI 快捷键的循环切档）
- `func TestSwitchEffortUnknownLevelDoesNotPush(t *testing.T)` — TestSwitchEffortUnknownLevelDoesNotPush：不认识的档位在 Apply 阶段就被拒，账号池

### service_fakes_test.go

- `func (engine *sessionBackedBlockingEngine) SessionBacked() bool`
- `func (engine *sessionBackedBlockingEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *sessionBackedBlockingEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 显式转发到自身 ChatStream（覆盖内嵌 fakeEngine 的提升方法，
- `func (sessions *blockingSaveSessions) SaveCurrent(string) error`
- `func (engine *fakeEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *fakeEngine) History() []contract.EngineMessage`
- `func (engine *fakeEngine) ClearHistory()`
- `func (engine *fakeEngine) SessionID() string`
- `func (engine *fakeEngine) StartSession() string`
- `func (engine *fakeEngine) ActivateSession(sessionID string) error` — ActivateSession 以显式会话 ID 创建并激活引擎实例（G4 早分配 SID：
- `func (engine *fakeEngine) ReplaceHistory(sessionID string, history []contract.EngineMessage) error`
- `func (engine *fakeEngine) SetSystemPrompt(prompt string)`
- `func (engine *fakeEngine) SetMaxLoops(maxLoops int)`
- `func (engine *fakeEngine) SetSessionMaxLoopsFor(sessionID string, loops int)` — SetSessionMaxLoopsFor 实现 contract.SessionLoopLimitPort：定时任务声明的循环上限
- `func (engine *fakeEngine) sessionLoopsFor(sessionID string) int` — sessionLoopsFor 读指定会话被钉住的循环上限（0 = 没钉，按进程默认值跑）。
- `func (engine *fakeEngine) AppendHistory(msg contract.EngineMessage)`
- `func (*fakeEngine) TraceText() string`
- `func (*fakeEngine) TokenCount() string`
- `func (*fakeEngine) NodeSessionConversation(string) ([]contract.EngineMessage, bool)`
- `func (engine *fakeEngine) NodeContextSnapshot(string) (*snapshot.ContextSnapshot, bool)`
- `func (engine *fakeEngine) NodeToolResult(nodeID, ref string) (string, bool)`
- `func (engine *fakeEngine) NodeWorktreeInfoFor(nodeID string) (assemble.NodeWorktreeInfo, bool)`
- `func (engine *fakeEngine) SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error)`
- `func (engine *fakeEngine) SubagentLiveHistoryPage(nodeID string, offset, limit int) dto.SubagentLiveHistoryPage`
- `func (engine *fakeEngine) SubAgentTree() []dto.SubAgentTreeNode`
- `func (engine *fakeEngine) ReleaseWorkingHistory()`
- `func (engine *fakeEngine) ReleaseWorkingHistoryFor(sessionID string)`
- `func (engine *fakeEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *fakeEngine) HistoryFor(sessionID string) []contract.EngineMessage`
- `func (engine *fakeEngine) AppendHistoryFor(sessionID string, msg contract.EngineMessage)`
- `func (engine *fakeEngine) ClearHistoryFor(sessionID string)`
- `func (engine *fakeEngine) SetSystemPromptFor(sessionID, prompt string)`
- `func (engine *fakeEngine) ReplaceHistoryFor(sessionID string, history []contract.EngineMessage) error`
- `func (engine *fakeEngine) HasSession(sessionID string) bool`
- `func (*fakeRuntime) Model() string`
- `func (*fakeRuntime) Provider() string`
- `func (*fakeRuntime) Accounts() []model.AccountInfo`
- `func (runtime *fakeRuntime) SelectAccount(name string) bool`
- `func (runtime *fakeRuntime) VisibleTools(context.Context) []model.Tool`
- `func (*fakeRuntime) ActivePlugin() string`
- `func (runtime *fakeRuntime) FullAccess() bool`
- `func (runtime *fakeRuntime) SetFullAccess(on bool)`
- `func (runtime *fakeRuntime) SetFullAccessFor(sessionID string, on bool)` — SetFullAccessFor 镜像生产权限门的会话级解析（空会话 ID = 进程级默认）：
- `func (runtime *fakeRuntime) FullAccessFor(sessionID string) bool` — FullAccessFor 返回指定会话生效的全权模式（会话级选择优先，未选择回退
- `func (runtime *fakeRuntime) PermissionTier() string` — PermissionTier 返回进程级默认权限档位（fake 只有二元口径：fullAccess → full）。
- `func (runtime *fakeRuntime) SetPermissionTierFor(sessionID, tier string) error` — SetPermissionTierFor 把档位落到 fake 的二元全权口径（full → true，其余 → false），
- `func (runtime *fakeRuntime) SetRuntimeVisibilityProjection(projection assemble.RuntimeVisibilityProjection)` — SetRuntimeVisibilityProjection / SetParentEvidenceProjection 会被并行会话的
- `func (runtime *fakeRuntime) SetParentEvidenceProjection(projection assemble.ParentEvidenceProjection)`
- `func (runtime *fakeRuntime) DrainSubagentContexts() []string` — DrainSubagentContexts 排空 merge-back 邮箱。M2 多会话并行下多个
- `func (runtime *fakeRuntime) SetSubagentPolicy(policy dto.SubagentPolicy)`
- `func (runtime *fakeRuntime) SetSubagentPolicyFor(sessionID string, policy dto.SubagentPolicy)`
- `func (runtime *fakeRuntime) subagentPolicyFor(sessionID string) (dto.SubagentPolicy, bool)`
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
- `func (runtime *fakeRuntime) TakeServedRoute(sessionID string) (dto.ServedRoute, bool)` — TakeServedRoute 按 fake 预置回答路由事实（读即清：与生产 Runtime 同语义，
- `func (runtime *fakeRuntime) SwitchSessionTasks(sessionID string, records []dto.TaskRecord)`
- `func (runtime *fakeRuntime) SetSessionWorkspace(sessionID, workspaceID string)`
- `func (runtime *fakeRuntime) WithPluginAssembly(ctx context.Context, plugins []string) context.Context` — WithPluginAssembly 是生产 Runtime 同名方法的镜像：把插件装配带进这一轮执行
- `func (runtime *fakeRuntime) PluginAssembly() []string` — PluginAssembly 读最近一次装配（同包用例直读，加锁）。
- `func (runtime *fakeRuntime) ScheduledCommands() []assemble.ScheduledCommandInfo`
- `func (runtime *fakeRuntime) ScheduledTasksSnapshot() []assemble.ScheduledTaskStatus`
- `func (runtime *fakeRuntime) ScheduleTask(_ context.Context, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error)`
- `func (runtime *fakeRuntime) CancelScheduledTask(id string) error`
- `func (runtime *fakeRuntime) UpdateScheduledTask(_ context.Context, id string, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error)` — UpdateScheduledTask 是编辑入口的桩：记录"改哪一条、改成什么"，并把快照里
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
- `func (runtime *goalVisibilityRuntime) VisibleTools(context.Context) []model.Tool`
- `func (*fakePlugins) All() []model.PluginInfo`
- `func (plugins *fakePlugins) Activate(_ context.Context, name string) error`
- `func (plugins *fakePlugins) Deactivate(context.Context) error`
- `func (plugins *fakePlugins) Current() (model.PluginInfo, bool)`
- `func (fakeSkills) All() []model.SkillInfo`
- `func (fakeSkills) Get(name string) (model.SkillInfo, bool)`
- `func (fakeSessions) SaveCurrent(string) error`
- `func (fakeSessions) Resume(string) error`
- `func (fakeSessions) List() []model.SessionInfo`
- `func (fakeSessions) LoadHistory(string) ([]contract.EngineMessage, error)`
- `func (fakeSessions) LoadHistoryRange(string, int, int) ([]contract.EngineMessage, int, error)`
- `func (fakeSessions) Delete(string) error`
- `func (fakeSessions) MessageCount(string) (int, error)`
- `func (fakeSessions) SetWorkspace(string)`
- `func (fakeSessions) Workspace() string`
- `func (sessions *blockingCatalogSessions) List() []model.SessionInfo`
- `func (persistenceFailingSessions) SaveCurrent(string) error`
- `func (sessions *trackingSessions) SaveCurrent(sessionID string) error`
- `func (sessions *scopedSessions) SetWorkspace(workspaceID string)`
- `func (sessions *scopedSessions) Workspace() string`
- `func (sessions *scopedSessions) SaveCurrent(sessionID string) error`
- `func (sessions *scopedSessions) SavedIDs() []string`
- `func (sessions *scopedSessions) SessionsOf(projectID string) []model.SessionInfo` — SessionsOf 实现 session_runtime.SessionGranularPort：按项目索引枚举会话。
- `func (sessions *scopedSessions) LoadedWorkspace() string`
- `func (sessions *scopedSessions) resolveWorkspaceFor(sessionID string) (string, []contract.EngineMessage, bool)` — resolveWorkspaceFor 返回会话历史所在 workspace（扫描 histories；未找到
- `func (sessions *scopedSessions) LoadHistory(sessionID string) ([]contract.EngineMessage, error)` — LoadHistory 实现 SessionGranularPort：读取会话历史（会话粒度键；workspace
- `func (sessions *scopedSessions) LoadHistoryRange(sessionID string, offset, limit int) ([]contract.EngineMessage, int, error)` — LoadHistoryRange 实现 SessionGranularPort：按窗口读取会话历史。
- `func (sessions *scopedSessions) Delete(sessionID string) error` — Delete 实现 SessionGranularPort：从全部项目索引删除会话。
- `func newFakeWorkspace() *fakeWorkspace`
- `func (repo *fakeWorkspace) Create(name, rootPath, gitRemote string) (model.WorkspaceInfo, error)`
- `func (repo *fakeWorkspace) Get(id string) (model.WorkspaceInfo, error)`
- `func (repo *fakeWorkspace) List() []model.WorkspaceInfo`
- `func (repo *fakeWorkspace) Delete(id string) error`
- `func (repo *fakeWorkspace) BindSession(sessionID, workspaceID string)`
- `func (repo *fakeWorkspace) UnbindSession(sessionID string)`
- `func (repo *fakeWorkspace) SessionWorkspace(sessionID string) (model.WorkspaceInfo, bool)`
- `func (repo *fakeWorkspace) AllBindings() map[string]string`
- `func (*fakeWorkspace) DetectGitRemote(string) string`

### service_helpers_test.go

- `func withSessionID(ctx context.Context, sessionID string) context.Context`
- `func sessionIDFromContext(ctx context.Context) string`
- `func ApplyLimits(limits applicationpolicy.Limits)`
- `func Limits() applicationpolicy.Limits`
- `func withTestSessions(sessions contract.SessionPort) testServiceOption`
- `func withTestRuntime(runtime contract.RuntimePort) testServiceOption`
- `func testResidencyPort() applicationsession.ResidencyPort`
- `func withTestReleaseWindowStore(store contract.ReleaseWindowStore) testServiceOption` — withTestReleaseWindowStore 注入「会话驻留窗口」的落盘口（不注入 = 界面上改的值
- `func withTestAccountWindowStore(store contract.AccountWindowStore) testServiceOption` — withTestAccountWindowStore 注入「每账号上下文窗口」的落盘口（不注入 = 界面上改的值
- `func withTestPlugins(plugins contract.PluginPort) testServiceOption` — withTestPlugins 注入自定义插件端口（默认桩只给一个不含来源的 default）。来源面
- `func newTestService(t testing.TB, engine contract.ChatEngine, options ...testServiceOption) *Service` — newTestService 构造一个带默认 fakes 的 Service（测试夹具），并在测试
- `func mustNew(t testing.TB, deps contract.Dependencies) *Service`
- `func waitForSnapshot(t *testing.T, service *Service, ready func(model.Snapshot) bool) model.Snapshot` — waitForSnapshot 轮询 Snapshot 直到 ready 条件满足，返回满足条件的快照。
- `func (*sessionBackedEngine) SessionBacked() bool`
- `func newGracefulShutdownEngine() *gracefulShutdownEngine`
- `func (engine *gracefulShutdownEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *gracefulShutdownEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 显式转发到自身 ChatStream（覆盖内嵌 fakeEngine 的提升方法，
- `func waitForChatCompletion(t *testing.T, service *Service)` — waitForChatCompletion 轮询直到当前 chat 结束。

### service_input.go

- `func (service *Service) discardPendingSubagentContexts()` — discardPendingSubagentContexts 排空 Runtime 持有的有界邮箱（活跃会话兼容
- `func (service *Service) discardPendingSubagentContextsFor(_ string)` — discardPendingSubagentContextsFor 排空 Runtime 持有的有界邮箱（单一来源 =
- `func (service *Service) chatStream(ctx context.Context, sessionID, input string, onChunk func(string)) (string, error)` — chatStream 向指定会话引擎提交流式对话（会话路由引擎用 ChatStreamFor，
- `func (service *Service) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error` — replaceEngineHistory 会话内替换指定会话引擎历史（会话路由引擎用
- `func (service *Service) engineHistoryFor(sessionID string) []contract.EngineMessage` — engineHistoryFor 返回指定会话引擎历史（只读拷贝）。
- `func (service *Service) clearEngineHistoryFor(sessionID string)` — clearEngineHistoryFor 清空指定会话引擎历史（会话路由引擎用 ClearHistoryFor，
- `func (service *Service) enqueueChatLocked(sessionID string, runtime *session.SessionUnit, request chatRequest, active bool) uint64` — enqueueChatLocked 把一次**已受理**的输入放进会话队列（调用方持有 Core.ViewMu），
- `func (service *Service) afterChatEnqueued(sessionID string, revision uint64, request chatRequest)` — afterChatEnqueued 是入队后的固定两步（调用方已释放 Core.ViewMu）：发布投影 +
- `func (service *Service) submitConversation(ctx context.Context, input string) error`
- `func (service *Service) submitConversationFor(ctx context.Context, sessionID, input string) error` — submitConversationFor 在指定（后台）会话提交对话：目标会话运行中则投递

### service_input_fork_queue_test.go

- `func (runtime *forkQueueRuntime) ForkInFlight(string) bool`
- `func TestSubmitQueuedWhileForkRunning(t *testing.T)` — TestSubmitQueuedWhileForkRunning（2026-10-05 退役 fork 门控）钉住新链路：fork/subagent

### service_input_test.go

- `func TestLifecycleApplicationIsAssembled(t *testing.T)`
- `func TestLifecycleApplicationWaitForIdle(t *testing.T)`
- `func TestLifecycleApplicationShutdownRunsCleanupOnce(t *testing.T)`
- `func trustedSkillInHistory(t *testing.T, history []contract.EngineMessage, name, text string) bool` — trustedSkillInHistory 断言引擎历史中存在激活技能 internal 事件
- `func TestSystemPromptStableAcrossPlanNodeChanges(t *testing.T)`
- `func TestWorkTableTraceBlock(t *testing.T)`
- `func TestPrepareExecutionContextCarriesWorkTableTraceBlock(t *testing.T)`
- `func TestNewRejectsMissingDependencies(t *testing.T)`
- `func TestSuggestionsAndSkillRouting(t *testing.T)`
- `func TestApprovalBrokerResolve(t *testing.T)`

### service_interaction.go

- `func (service *Service) pendingApprovalSession(id string) (string, bool)` — pendingApprovalSession 在 broker 待批集合中按审批 ID 反查归属会话
- `func (service *Service) reapplyEffortAfterPluginSwitch()` — SwitchEffort 切换 Effort 等级（用户级动作，作用于视图会话）。
- `func (service *Service) observeInteraction(sessionID, requestID string, interaction *model.Interaction)` — observeInteraction 是 ApprovalBroker 的开/结观察回调（波 4 approval 会话
- `func (service *Service) mirrorPendingApprovalsLocked(sessionID string)` — mirrorPendingApprovalsLocked 把指定会话当前首笔待批审批镜像到
- `func (service *Service) openInteraction(interaction *model.Interaction)`
- `func (service *Service) closeInteraction(id string)`
- `func (service *Service) sessionInteraction() *model.Interaction`
- `func (service *Service) accountInteraction() *model.Interaction`

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

### service_knobs.go

- `func (host serviceKnobsHost) ViewSession() (string, bool)`
- `func (host serviceKnobsHost) MutateRuntime(mutate func(*model.RuntimeState))`
- `func (host serviceKnobsHost) PublishRuntime(mutate func(*model.RuntimeState)) error`
- `func (host serviceKnobsHost) SetEngineMaxLoops(limit int)`
- `func (host serviceKnobsHost) CurrentEffort() string`
- `func (host serviceKnobsHost) MaxLoopsForEffort(level string) int`
- `func (host serviceKnobsHost) ReasoningEffortSink() contract.ReasoningEffortPort`

### service_knobs_smoke_test.go

- `func (engine *knobSmokeEngine) SetMaxLoops(loops int)`
- `func (engine *knobSmokeEngine) loops() int`
- `func projectRuntime(t *testing.T, service *Service)` — projectRuntime 模拟一次 runtime 投影重建（回合尾 / 工具边界会做的事）。
- `func TestKnobChainSmoke(t *testing.T)`

### service_knobs_test.go

- `func knobService(t *testing.T) (*Service, *fakeEngine)`
- `func runtimeKnobs(service *Service) (string, int)`
- `func TestSetLoopLimitOnlyTouchesLoopLimit(t *testing.T)` — TestSetLoopLimitOnlyTouchesLoopLimit：改循环次数不改思考强度，且引擎与快照一致。
- `func TestSetLoopLimitRejectsOutOfRange(t *testing.T)` — TestSetLoopLimitRejectsOutOfRange：上界 = limits.input_loop_limit（9999 是 goal
- `func TestSetReasoningEffortOnlyTouchesReasoningEffort(t *testing.T)` — TestSetReasoningEffortOnlyTouchesReasoningEffort：改思考强度只推一次 wire 值，
- `func TestSetReasoningEffortRejectsUnknownVocabulary(t *testing.T)` — TestSetReasoningEffortRejectsUnknownVocabulary：非法值（含 "session"——那是"没有
- `func TestSwitchEffortSeedsBothKnobReadings(t *testing.T)` — TestSwitchEffortSeedsBothKnobReadings：档位是两条拉条的**预设**——切一次档位，
- `func TestKnobsAreGuardedWhileViewSessionRuns(t *testing.T)` — TestKnobsAreGuardedWhileViewSessionRuns：视图会话 running 时两条拉条都拒绝，且
- `func serviceRuntimePushes(t *testing.T, service *Service) []string` — serviceRuntimePushes 读记录桩里的下发序列（等价于 effortWireRuntime.pushedEfforts，

### service_notice_test.go

- `func TestServiceAddNoticeAppendsSystemMessage(t *testing.T)`

### service_policy_application_test.go

- `func TestPolicyApplicationIsAssembled(t *testing.T)`
- `func TestPolicyApplicationDelegatesPolicyOperations(t *testing.T)`
- `func TestPolicyApplicationFullAccessError(t *testing.T)`
- `func TestPolicyApplicationSetPermissionTierFullPath(t *testing.T)`
- `func TestPolicyApplicationSetPermissionTierStopsOnPersistAndRuntimeErrors(t *testing.T)`
- `func TestPolicyApplicationSetPermissionTierDoesNotResolveNonFull(t *testing.T)`
- `func TestPolicyApplicationSwitchEffortRunsFullPath(t *testing.T)`
- `func TestPolicyApplicationSwitchEffortStopsOnIdleCycleAndApplyErrors(t *testing.T)`
- `func TestPolicyApplicationSwitchPluginActivatesAndRebuilds(t *testing.T)`
- `func TestPolicyApplicationSwitchPluginOffAndErrors(t *testing.T)`
- `func boolWord(value bool) string`

### service_queue_component_test.go

- `func TestServiceComponentsAssembleConversationQueue(t *testing.T)`
- `func TestConversationQueuePreservesPresentationEventOrder(t *testing.T)`
- `func TestConversationQueueDoesNotOwnServiceOrDuplicateQueueState(t *testing.T)`

### service_queue_test.go

- `func enqueueRawQueuedInput(t *testing.T, service *Service, text string)`
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

### service_release_window.go

- `func (service *Service) releaseWindowDeps() switching.ReleaseWindowDeps` — releaseWindowDeps 装配驻留窗口可调面的宿主面。

### service_release_window_test.go

- `func (store *recordingReleaseWindowStore) SaveSessionReleaseWindow(window int) error`
- `func (store *recordingReleaseWindowStore) calls() []int`
- `func snapshotWindowLimits(t *testing.T)` — snapshotWindowLimits 保存并恢复**进程级** limits 快照：窗口住在包级 atomic 上，
- `func TestSetSessionReleaseWindowAppliesImmediately(t *testing.T)` — TestSetSessionReleaseWindowAppliesImmediately：改一个数，释放面的现读路径立刻看见。
- `func TestSetSessionReleaseWindowAcceptsBoundaries(t *testing.T)` — TestSetSessionReleaseWindowAcceptsBoundaries：区间两端（1 与 16）都被接受。
- `func TestSetSessionReleaseWindowRejectsOutOfRange(t *testing.T)` — TestSetSessionReleaseWindowRejectsOutOfRange：越界一个字节都不改——不落盘、不动内存。
- `func TestSetSessionReleaseWindowLeavesMemoryUntouchedWhenPersistFails(t *testing.T)` — TestSetSessionReleaseWindowLeavesMemoryUntouchedWhenPersistFails：落盘失败 ⇒ 整步
- `func TestSetSessionReleaseWindowWithoutStoreAppliesToMemoryOnly(t *testing.T)` — TestSetSessionReleaseWindowWithoutStoreAppliesToMemoryOnly：宿主没装配落盘口 ⇒

### service_scheduler.go

- `func scheduledRunParams(effort string, loopLimit int) (string, int)`
- `func (service *Service) applyScheduledRunParams(sessionID string, task dto.ScheduledTaskSpec) error`
- `func (service *Service) validateScheduledWorkspace(workspaceID string) error`
- `func (service *Service) resolveScheduledWorkspace(workspaceID string) (*model.WorkspaceInfo, error)`
- `func (service *Service) validateScheduledPlugins(plugins []string) error`

### service_scheduler_test.go

- `func TestSchedulerApplicationIsAssembled(t *testing.T)`
- `func TestRuntimeApplicationIsAssembled(t *testing.T)`
- `func TestScheduledRunApplicationIsAssembled(t *testing.T)`
- `func TestScheduledSessionApplicationIsAssembled(t *testing.T)`
- `func TestSchedulerApplicationCreateStopsOnValidationError(t *testing.T)`
- `func TestStartScheduledSessionCreatesNewSessionInWorkspace(t *testing.T)` — TestStartScheduledSessionCreatesNewSessionInWorkspace 钉住定时触发的会话落点：
- `func TestStartScheduledSessionWithoutWorkspaceKeepsSessionUnbound(t *testing.T)` — TestStartScheduledSessionWithoutWorkspaceKeepsSessionUnbound 钉住"工作区是
- `func (runtime *fakeRuntime) sessionProjectRoot(sessionID string) string` — sessionProjectRoot 读 fakeRuntime 的按会话工具根（同包测试直读，加锁）。
- `func conversationContents(messages []model.Message) string` — conversationContents 把可见会话拼成一段文本（判据只看"出现过没有"）。
- `func TestUpdateScheduledTaskForwardsAndValidatesWorkspace(t *testing.T)` — TestUpdateScheduledTaskForwardsAndValidatesWorkspace 钉住编辑这条链的应用层口径：
- `func TestAssembleScheduledRunAppliesTierAndPlugins(t *testing.T)` — TestAssembleScheduledRunAppliesTierAndPlugins 钉住任务的**装配**落到哪儿：
- `func TestScheduledRunParamsDefaultsAndValidation(t *testing.T)` — TestScheduledRunParamsDefaultsAndValidation 钉住定时任务那两条执行参数（思考强度 /
- `func TestScheduledPluginsValidatedAgainstCatalog(t *testing.T)` — TestScheduledPluginsValidatedAgainstCatalog 钉住应用层的插件装配校验：未知插件名

### service_snapshot_test.go

- `func TestPerfStatsApplicationIsAssembled(t *testing.T)`
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

### service_test.go

- `func TestNewTestServiceCleansUpCatalogWorker(t *testing.T)`
- `func TestSessionCatalogAllowsDuplicateNamesWithDistinctIDs(t *testing.T)`
- `func TestSessionTitleUsesFirstUserQuestion(t *testing.T)`
- `func TestCurrentSessionNameUsesFirstQuestion(t *testing.T)`
- `func TestWorkingHistoryReleasesOnlyAfterSuccessfulPersistence(t *testing.T)`
- `func TestBeginNewSessionIsLazyAndFirstQuestionMaterializesIt(t *testing.T)`
- `func TestLazySessionInheritsProjectOnlyWhenMaterialized(t *testing.T)`
- `func TestCreateWorkspacesPublishesCatalogWithoutBindingCurrentSession(t *testing.T)`
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

- `func teamworkCompletionHarness(t *testing.T, trigger bool, engine contract.ChatEngine) (*Service, *fakeRuntime)` — teamworkCompletionHarness 造一个装配好 teammate 触发路径的 Service。
- `func completedTeammateRecord(handle string, state dto.AsyncState) dto.TeamworkJobCompletionRecord` — completedTeammateRecord 造一条已落到终态的 teammate 作业记录。
- `func TestTeamworkCompletionTriggersIdleSessionTurn(t *testing.T)`
- `func TestTeamworkCompletionTriggersOnFailureToo(t *testing.T)`
- `func TestTeamworkCompletionIgnoresRunningAndKilled(t *testing.T)`
- `func TestTeamworkCompletionDoesNotWakeBusySession(t *testing.T)` — 铁律 §6.1「绝不唤醒忙会话」：忙的时候不起回合，跳过的条目**不记账**——会话回到空闲
- `func TestTeamworkCompletionTriggersOncePerHandle(t *testing.T)` — 幂等：句柄在册期间只触发一次（句柄单调不复用，进程内一个集合就够）。
- `func TestTeamworkCompletionDoesNotCollideWithToolHandles(t *testing.T)` — 两张表的终态链**各触发一次**：幂等账按表（前缀）分开，不靠句柄字面量。
- `func TestTeamworkCompletionStaysOffWhenDisabled(t *testing.T)` — 开关关闭时连扫描都不做（与 subagent 那条同一个开关）。
- `func TestTeamworkTraceLinesCarryCompletionReceipt(t *testing.T)` — 忙会话的读法：**回合边界打点块**里的 teammate 完成行（这是"回执被看见"的另一半——

### teamwork_service.go

- `func (port teamworkBoardViewPort) TeamworkBoardViewFor(sessionID string) *dto.TeamworkBoardView`
