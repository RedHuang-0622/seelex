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

### components.go

- `func ComponentsFor(service *Service) Components`

### service.go

- `func (service *Service) projectBoardSink() board.ProjectBoardSink` — projectBoardSink 在锁内只复制接口值；外部同步器不得在装配锁下执行。
- `func New(deps contract.Dependencies, ports ExternalPorts) (*Service, error)`

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
- `func (service *Service) assembleConversationSessionApplication(sessionRecords applicationconversation.SessionRecordsPort)`
- `func (service *Service) assembleSubmissionApplication()`
- `func (service *Service) assembleInteractionApplication()`
- `func (service *Service) assembleForkApplication()`
- `func (service *Service) assembleSubmitApplication()`
- `func (service *Service) assembleTerminalHandlerApplication()`
- `func (service *Service) assembleFaultGuardApplication()`
- `func (service *Service) assembleGoalApplication(hostSessionForToolCall func(context.Context) string)`
- `func (service *Service) assemblePolicySwitchApplication(skillActivation applicationpolicy.SkillActivationPort)`
- `func (service *Service) assembleFrontendSnapshotApplication()`
- `func (service *Service) assembleSubagentApplication()`

### service_assembler_capabilities.go

- `func (service *Service) assembleRuntimeApplication()`
- `func (service *Service) assembleAdminApplication(accountWindowPolicy applicationadmin.AccountWindowPolicy)`
- `func (service *Service) assembleWorkspaceApplication()`
- `func (service *Service) assembleAsyncCompletionApplication()`
- `func (service *Service) assembleCompletionApplication()`
- `func (service *Service) assembleContextCompactionApplication()`
- `func (service *Service) assembleReferenceApplication()`

### service_assembler_conversation_turn.go

- `func (service *Service) assembleConversationTurnComponents()`
- `func (host turnStartViewHost) SessionTitleFor(sessionID string) model.SessionTitle`
- `func (host turnStartViewHost) SetSessionTitleLocked(sessionID string, title model.SessionTitle)`
- `func (host turnStartViewHost) AppendMessageWithOriginLocked( role, content string, tool *model.ToolCall, origin model.MessageOrigin, ) *model.Message`
- `func (host turnStartViewHost) AppendMessageWithOriginLockedFor( sessionID, role, content string, tool *model.ToolCall, origin model.MessageOrigin, ) *model.Message`
- `func (host turnStartViewHost) BumpLocked() uint64`

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
- `func (service *Service) assembleSessionUseCaseApplications(restore sessionRestoreComponents)`
- `func (service *Service) assembleSessionRestoreComponents() sessionRestoreComponents`

### service_assembler_session_cold.go

- `func (service *Service) assembleSessionColdComponents() sessionColdComponents` — assembleSessionColdComponents owns the cold-restore dependency graph. The

### service_assembler_session_draft.go

- `func (service *Service) assembleSessionDraftComponents()`

### service_assembler_session_query.go

- `func (service *Service) assembleSessionReadApplications(historyWindowAssembler applicationhistory.WindowAssembler)`

### service_components.go

- `func (port *ProjectBoardSinkPort) Store(sink board.ProjectBoardSink)`
- `func (port *ProjectBoardSinkPort) load() board.ProjectBoardSink`

### service_input.go

- `func (service *Service) discardPendingSubagentContexts()` — discardPendingSubagentContexts 排空 Runtime 持有的有界邮箱（活跃会话兼容
- `func (service *Service) discardPendingSubagentContextsFor(_ string)` — discardPendingSubagentContextsFor 排空 Runtime 持有的有界邮箱（单一来源 =
- `func (service *Service) chatStream(ctx context.Context, sessionID, input string, onChunk func(string)) (string, error)` — chatStream 向指定会话引擎提交流式对话（会话路由引擎用 ChatStreamFor，
- `func (service *Service) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error` — replaceEngineHistory 会话内替换指定会话引擎历史（会话路由引擎用
- `func (service *Service) engineHistoryFor(sessionID string) []contract.EngineMessage` — engineHistoryFor 返回指定会话引擎历史（只读拷贝）。
- `func (service *Service) clearEngineHistoryFor(sessionID string)` — clearEngineHistoryFor 清空指定会话引擎历史（会话路由引擎用 ClearHistoryFor，

### service_interaction.go

- `func (service *Service) pendingApprovalSession(id string) (string, bool)` — pendingApprovalSession 在 broker 待批集合中按审批 ID 反查归属会话
- `func (service *Service) reapplyEffortAfterPluginSwitch()` — SwitchEffort 切换 Effort 等级（用户级动作，作用于视图会话）。
- `func (service *Service) openInteraction(interaction *model.Interaction)` — observeInteraction 是 ApprovalBroker 的开/结观察回调（波 4 approval 会话
- `func (service *Service) closeInteraction(id string)`
- `func (service *Service) sessionInteraction() *model.Interaction`
- `func (service *Service) accountInteraction() *model.Interaction`

### service_knobs.go

- `func (host serviceKnobsHost) ViewSession() (string, bool)`
- `func (host serviceKnobsHost) MutateRuntime(mutate func(*model.RuntimeState))`
- `func (host serviceKnobsHost) PublishRuntime(mutate func(*model.RuntimeState)) error`
- `func (host serviceKnobsHost) SetEngineMaxLoops(limit int)`
- `func (host serviceKnobsHost) CurrentEffort() string`
- `func (host serviceKnobsHost) MaxLoopsForEffort(level string) int`
- `func (host serviceKnobsHost) ReasoningEffortSink() contract.ReasoningEffortPort`

### service_release_window.go

- `func (service *Service) releaseWindowDeps() switching.ReleaseWindowDeps` — releaseWindowDeps 装配驻留窗口可调面的宿主面。

### service_scheduler.go

- `func (service *Service) resolveScheduledWorkspace(workspaceID string) (*model.WorkspaceInfo, error)`

### teamwork_service.go

- `func (port teamworkBoardViewPort) TeamworkBoardViewFor(sessionID string) *dto.TeamworkBoardView`
