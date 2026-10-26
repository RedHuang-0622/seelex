# core/chat（根包分卷）

## 生态位

聊天主循环与可见输出集成（含可见会话的群聊角色归属载荷）

覆盖：`chat*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## Queue application ownership（AP-007 首个切片）

`chat_queue.go` 是排队输入编辑的应用组件。它消费 `SessionUnit` 的窄结构面
执行调序/撤回，并复用同一把 `Core.ViewMu` 与既有 `chat.changed` /
`snapshot.changed` 发布顺序；队列内容与 `ChatState` 仍只由 `SessionUnit`
持有。`service_queue.go` 的公开方法只做一行组件转发。

该切片不进入 `startChat/runChat` 主循环，不改变 durable queue、revision、
恢复路径或前端协议。AP-007 的设计门禁见
`docs/2026-10-25-application-modularization/ap-007-conversation-design.md`。

AP-113D1 起，assistant reasoning 步骤提取、可见消息配对和 legacy fallback 由
[`application/conversation/reasoning.go`](../conversation/reasoning.go) 唯一拥有；
AP-113D2 起，`runChat` application 组件链由
[`application/conversation/turn_runner.go`](../conversation/turn_runner.go) 编排；
core 只保留一行 wrapper，Engine ReAct loop 仍归 Seele。

## ChatState 单一写入点（AP-008 首个切片）

`chat_turn_state.go` 集中 `ChatState` 的开始、队列投影、错误、收尾和重置。
状态仍存在 `SessionUnit.Chat`；`conversationTurnState` 只拥有转换判据。
`chat_turn_state_test.go` 的 AST 门禁禁止其它生产文件直接调用
`SetChatState/UpdateChat`。

## ChatTurn 请求工厂（AP-009 首个切片）

`chat_turn_request.go` 集中构造 `chatRequest`、恢复队列 payload、合并提升批次、
选择 Skill 层和提取排队引用。`chat.go` 与 `session_scope.go` 不再各自持有这些规则。

## ChatTurnProcess 起点事务（AP-014 首个切片）

`chat_turn_process.go` 的 `conversationTurnProcess.BeginLocked` 统一正常提交与队列
批次提升的起点规则：requestID、Effort 预算回退、cancel/stream、Task 起点与
checkpoint、Skill 激活和 user transcript。Engine 流与收尾持久化仍在主循环。

## ChatTurn 持久化完成事务（AP-018 首个切片）

`chat_turn_completion.go` 的 `conversationTurnCompletion.Persist` 承接回合收尾的
durable 事务：定位会话、原子落盘、持久化失败时 queue 回草稿、成功后 queue 确认
与工作历史释放。错误包装和 `session.persistence_failed` 结构化错误码不变；
Engine 流、消息投影、事件发布和会话释放仍由 `runChat` 编排。

## ChatTurn 队列提升决策（AP-019 首个切片）

`conversationTurnProcess.PromoteLocked` 是回合收尾后的唯一队列决策点。空队列时
结束 ChatState、清空 cancel，并在没有其它聊天运行时标记进程 idle；非空队列时
合并批次、清空 SessionUnit 队列、刷新队列投影并复用 `BeginLocked`。下一回合的
可见消息插入、`message.added` 发布、durable queue consumed 标记和 goroutine
启动仍由 `runChat` 编排。

## ChatTurn 终态结果投影（AP-020 首个切片）

`conversationTurnOutcome.ApplyLocked` 在 `Core.ViewMu` 内应用回合终态：先按
`requestID` 拒绝已被取代的请求，再清理旧错误、写入当前错误 ChatState/error 行，
最后应用该会话的 runtime projection。队列提升、任务镜像、revision 和事件发布
仍在 `runChat`。

## ChatTurn 终态事件发布（AP-021 首个切片）

`conversationTurnPublisher.Publish` 在释放 `Core.ViewMu` 后集中发布终态事件：
先发布 `chat.changed`，再按成功/失败发布 `snapshot.changed` 或 `EventError`；
存在提升批次时继续发布 user/assistant 两条 `message.added`、设置下一 task batch
并发布 runtime projections。下一轮 `runChat` goroutine 仍由 `runChat` 启动。

## ChatTurn 终态镜像（AP-022 首个切片）

`conversationTurnOutcome.MirrorLocked` 在 `Core.ViewMu` 内完成解锁前镜像：写入
会话 ChatState；仅活跃会话读取权威 Task 镜像到共享 Snapshot 并推进进程 revision。
后台会话不得覆盖共享 Task 或进程 revision。

## ChatTurn 异步尾段（AP-023 首个切片）

`conversationTurnTail.Complete` 在 `Core.ViewMu` 之外按固定顺序执行：工作区重绑、
提升批次 durable queue consumed 标记、终态发布、下一轮启动、会话释放和工具记录
落盘。consumed 标记失败仍只记诊断日志，不中断发布、释放或落盘流程。

## ChatTurn Engine 前输入准备（AP-047 首个切片）

`chat_turn_input.go` 固定 Engine 调用前顺序：激活当前 task system prompt、
调用 provider context 装配、对成功结果做非空输入归一化。context 失败直接返回，
不再归一化或进入 Engine 流。

## ChatTurn Engine 返回事务（AP-048 首个切片）

`chat_turn_response.go` 在 `ChatStream` 返回后按固定顺序执行 reasoning attach、
模型输出观测、最终 assistant transcript 补齐和 reasoning 回填。观测错误保持
原有忽略语义；context recovery 与 budget/task finalization 仍在主循环。

## ChatTurn 恢复与终态（AP-049 首个切片）

`chat_turn_recovery.go` 集中 context control failure 读取、provider failure
恢复、有界补充回合重试、ReAct budget finalization 和自然停止后的 Task finalize。
恢复/重试错误保持原组合文案；stream flush、route 和持久化仍在 `runChat`。

## ChatTurn 清理与持久化前准备（AP-050 首个切片）

`chat_turn_cleanup.go` 固定解锁持久化前的清理顺序：batcher flush、route stamp、
subagent mailbox drain、runtime projection 采集、checkpoint 清理、provider
recovery 清理、未处理任务错误记录和 draft snapshot 停止。成功/失败清理顺序与
错误优先级保持原实现。

## ChatTurn Commit（AP-051 首个切片）

`chat_turn_commit.go` 把持久化完成、stale/outcome、队列提升、下一批 user/assistant
消息和 ChatState/Task/revision 镜像串成一次提交。stale 请求在 outcome 后短路；
非 stale 结果直接供 publication/tail 使用。

## ChatTurn Finalize（AP-052 首个切片）

`chat_turn_finalize.go` 将 commit result 映射为 `chatTurnPublication` 并调用
异步 tail；stale commit 不发布、不启动下一轮。`runChat` 只保留 Engine 循环与
各组件串联。

## ChatTurn 执行策略同步（AP-053 首个切片）

`chat_turn_execution.go` 固定每个回合起点的执行面同步：绑定本 Session project
root、下发 subagent policy、reasoning effort 和 full access。该同步不读取其它
Session 的遗留执行开关。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### chat.go

- `func runChatDebug(format string, args ...any)` — runChatDebug 是 SEELEX_TEST_DEBUG=1 门控的临时诊断日志（复跑噪音点时
- `func (service *Service) isActiveSessionLocked(sessionID string) bool` — isActiveSessionLocked 判定指定会话是否为共享快照归属会话（锁内调用；
- `func (service *Service) nextChatRequestIDLocked() string` — nextChatRequestIDLocked 生成跨会话唯一的聊天请求 ID（调用方持有
- `func (service *Service) startChat(parent context.Context, request chatRequest) error` — startChat 在**当前视图会话**上启动 ReAct 对话。
- `func (service *Service) startChatFor(sessionID string, parent context.Context, request chatRequest) error` — startChatFor 在指定会话启动 ReAct 对话（多会话并行：后台会话不写活跃
- `func (service *Service) recordUnhandledTaskErrorLocked(sessionID, requestID string, err error)` — runChat 在独立 goroutine 中执行一次会话提交：委托 Seele loop（9.2 边界，
- `func (service *Service) finalizeReActBudget(ctx context.Context, requestID string) error` — finalizeReActBudget 在工具预算耗尽后保留一次纯文本交付回合。常规循环在
- `func (service *Service) TaskTerminalHandler(kind string) func(context.Context, string) (string, error)` — TaskTerminalHandler 返回面向 Runtime 的终态工具 handler，同时把请求状态
- `func (service *Service) ensureViewPlanProjection(ctx context.Context)` — ensureViewPlanProjection 在进入终态工具前确保当前视图会话的协调器投影有
- `func (service *Service) mirrorActivePlanAfterTerminal(ctx context.Context)` — mirrorActivePlanAfterTerminal 在 check/complete 终态把协调器投影的最新
- `func (service *Service) mirrorActiveTaskAfterTerminal(ctx context.Context)` — mirrorActiveTaskAfterTerminal 在终态工具落地后把活跃会话的权威任务状态
- `func (service *Service) finalizeTaskExecution(requestID string) error` — finalizeTaskExecution 把自然停止转换为可审计的完成/交接
- `func (service *Service) finalizeReActBudgetWithSink(ctx context.Context, requestID string, onChunk func(string)) error`
- `func (service *Service) removeReActBudgetFinalizationInput(sessionID string) error`
- `func closedSignal() chan struct`
- `func (service *Service) markBusyLocked()`
- `func (service *Service) markIdleLocked()`
- `func (service *Service) appendDelta(requestID, chunk string)`
- `func (service *Service) newBatchedDeltaSink(requestID string) (applicationconversation.StreamBatcher, func(string))`
- `func (service *Service) flushStreamBatcherFor(sessionID string)` — flushStreamBatcherFor 把指定会话的流式缓冲同步落地（工具钩子边界调用：
- `func (service *Service) consumeVisibleChunk(requestID, chunk string) string`
- `func (service *Service) consumeVisibleChunkBackground(sessionID, requestID, chunk string) string` — consumeVisibleChunkBackground 后台会话的流式消费：仅触碰该会话单元自身的
- `func (service *Service) appendVisibleDelta(requestID, chunk string)`
- `func (service *Service) appendVisibleDeltaBackground(sessionID, requestID, chunk string)` — appendVisibleDeltaBackground 后台会话的流式增量：仅 View.mu + 会话本地
- `func (service *Service) attachLatestReasoning(sessionID, requestID string)` — attachLatestReasoning 在聊天回合结束后，把引擎历史里**每个 assistant 步骤**
- `func (service *Service) stampServedRoute(sessionID, requestID string)` — stampServedRoute 在回合收尾把本轮请求的实际承接路由（config role / config
- `func (service *Service) streamedAssistantTextLocked(sessionID string) string` — streamedAssistantTextLocked 返回会话本轮的可见 assistant 正文累积
- `func (service *Service) streamedRequestTextLocked(sessionID string) string` — streamedRequestTextLocked 返回会话当前请求已累积的可见流式正文（会话单元无
- `func (service *Service) appendHistoryLocked(history []contract.EngineMessage)`
- `func (service *Service) appendHistoryLockedFor(sessionID string, history []contract.EngineMessage)` — appendHistoryLockedFor 把引擎历史追加为指定会话的可见消息（冷加载无

### chat_cold_load_narration_repro_test.go

- `func (s *coldNarrationStore) SaveSessionRecordWorkspace(_, _ string, record model.SessionRecord) error`
- `func (s *coldNarrationStore) LoadSessionRecordWorkspace(workspaceID, sessionID string) (model.SessionRecord, error)`
- `func TestColdLoadKeepsToolWheelNarration(t *testing.T)`

### chat_delegation_test.go

- `func (engine *singleLoopEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *singleLoopEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 显式转发到自身 ChatStream（会话路由面下仍单次提交）。
- `func TestLoopDelegatedToSeele(t *testing.T)` — TestLoopDelegatedToSeele（UC6）：core 无自编 ReAct 循环——一次提交只

### chat_error_surface_test.go

- `func (failingSnapshotSessions) SaveSessionSnapshot(string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult) error`
- `func (failingSnapshotSessions) SaveSessionSnapshotWorkspace(string, string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult) error`
- `func TestRunChatUsesRawErrorWhileToolProjectionUsesPresentedError(t *testing.T)`
- `func TestRunChatLogsRawUnclassifiedError(t *testing.T)`
- `func TestPersistenceFailureDoesNotClaimProgressWasSaved(t *testing.T)`
- `func conversationContains(conversation []model.Message, role, fragment string) bool`

### chat_hot_attach_reasoning_repro_test.go

- `func TestHotMountKeepsStepReasoningInVisibleWindow(t *testing.T)`

### chat_state_event_test.go

- `func TestChatStateIsPublishedNotInferred(t *testing.T)` — TestChatStateIsPublishedNotInferred 验证运行态由后端下发：提交后收到

### chat_turn_cleanup_test.go

- `func TestChatTurnCleanupRulesAreCentralized(t *testing.T)`
- `func TestConversationTurnCleanupRunsFullSuccessfulPath(t *testing.T)`
- `func TestConversationTurnCleanupCombinesErrorsAndKeepsRunning(t *testing.T)`
- `func TestConversationTurnCleanupWrapsFlushAndRecoveryErrors(t *testing.T)`

### chat_turn_commit_test.go

- `func TestChatTurnCommitRulesAreCentralized(t *testing.T)`
- `func TestConversationTurnCommitRunsPersistenceOutcomePromotion(t *testing.T)`
- `func TestConversationTurnCommitStopsOnStaleRequest(t *testing.T)`

### chat_turn_completion_test.go

- `func TestConversationTurnCompletionConfirmsPublishedTurn(t *testing.T)`
- `func TestConversationTurnCompletionReturnsConsumedInputOnPersistenceFailure(t *testing.T)`

### chat_turn_execution_test.go

- `func TestChatTurnExecutionRulesAreCentralized(t *testing.T)`
- `func TestConversationTurnExecutionPreparesInOrder(t *testing.T)`

### chat_turn_finalize_test.go

- `func TestChatTurnFinalizeRulesAreCentralized(t *testing.T)`
- `func TestConversationTurnFinalizeMapsCommitToPublication(t *testing.T)`
- `func TestConversationTurnFinalizeIgnoresStaleCommit(t *testing.T)`

### chat_turn_input_test.go

- `func TestChatTurnInputRulesAreCentralized(t *testing.T)`
- `func TestConversationTurnInputPreparesPromptAndContext(t *testing.T)`
- `func TestConversationTurnInputStopsOnContextError(t *testing.T)`

### chat_turn_outcome_test.go

- `func TestChatTurnOutcomeRulesAreCentralized(t *testing.T)`
- `func TestConversationTurnOutcomeAppliesCurrentResult(t *testing.T)`
- `func TestConversationTurnOutcomeRecordsErrorAndProjection(t *testing.T)`
- `func TestConversationTurnOutcomeRejectsSupersededRequest(t *testing.T)`
- `func TestConversationTurnOutcomeMirrorsActiveSessionAndRevision(t *testing.T)`
- `func TestConversationTurnOutcomeDoesNotMirrorBackgroundTask(t *testing.T)`

### chat_turn_pipeline_test_aliases2_test.go

- `func newConversationTurnProcess(deps conversationTurnProcessDeps) *conversationTurnProcess`
- `func newConversationTurnCompletion(deps conversationTurnCompletionDeps) *conversationTurnCompletion`
- `func newConversationTurnOutcome(deps conversationTurnOutcomeDeps) *conversationTurnOutcome`
- `func newConversationTurnPublisher(deps conversationTurnPublisherDeps) *conversationTurnPublisher`
- `func newConversationTurnTail(deps conversationTurnTailDeps) *conversationTurnTail`
- `func newConversationTurnCommit(deps conversationTurnCommitDeps) *conversationTurnCommit`
- `func newConversationTurnFinalize(deps conversationTurnFinalizeDeps) *conversationTurnFinalize`
- `func newConversationTurnExecution(deps conversationTurnExecutionDeps) *conversationTurnExecution`

### chat_turn_pipeline_test_aliases_test.go

- `func newConversationTurnInput(deps conversationTurnInputDeps) *conversationTurnInput`
- `func newConversationTurnResponse(deps conversationTurnResponseDeps) *conversationTurnResponse`
- `func newConversationTurnRecovery(deps conversationTurnRecoveryDeps) *conversationTurnRecovery`
- `func newConversationTurnCleanup(deps conversationTurnCleanupDeps) *conversationTurnCleanup`

### chat_turn_process_test.go

- `func TestServiceComponentsAssembleConversationTurnProcess(t *testing.T)`
- `func TestChatTurnStartRulesAreCentralized(t *testing.T)`
- `func TestChatTurnPersistenceRulesAreCentralized(t *testing.T)`
- `func TestChatTurnQueuePromotionRulesAreCentralized(t *testing.T)`
- `func (unit *queuePromotionTestUnit) ChatState() model.ChatState`
- `func (unit *queuePromotionTestUnit) SetChatState(chat model.ChatState, _ *sessioncase.View)`
- `func (unit *queuePromotionTestUnit) UpdateChat(update func(*model.ChatState), _ *sessioncase.View)`
- `func (unit *queuePromotionTestUnit) QueueProjection() ([]string, int)`
- `func (unit *queuePromotionTestUnit) SetCancel(cancel context.CancelFunc)`
- `func (unit *queuePromotionTestUnit) SetStream(sessioncase.VisibleOutputSink)`
- `func (unit *queuePromotionTestUnit) PendingRequests() []sessioncase.QueuedRequest`
- `func (unit *queuePromotionTestUnit) SetRequests(requests []sessioncase.QueuedRequest)`
- `func TestConversationTurnProcessFinishesWhenQueueIsEmpty(t *testing.T)`
- `func TestConversationTurnProcessKeepsProcessBusyWhenOtherChatRuns(t *testing.T)`

### chat_turn_publication_test.go

- `func TestChatTurnPublicationRulesAreCentralized(t *testing.T)`
- `func TestConversationTurnPublisherPublishesSnapshotWithoutPromotion(t *testing.T)`
- `func TestConversationTurnPublisherReportsErrorBeforePromotedBatch(t *testing.T)`

### chat_turn_recovery_test.go

- `func TestChatTurnRecoveryRulesAreCentralized(t *testing.T)`
- `func TestConversationTurnRecoveryRetriesAfterRecovery(t *testing.T)`
- `func TestConversationTurnRecoveryPropagatesRecoveryAndRetryErrors(t *testing.T)`
- `func TestConversationTurnRecoveryStopsBeforeTaskOnBudgetError(t *testing.T)`

### chat_turn_request.go

- `func chatRequestDisplays(requests []chatRequest) []string`
- `func queuedInputRefs(queue []chatRequest) []string`
- `func selectedSkillLayers(layers []prompt.PromptLayer) []prompt.PromptLayer`

### chat_turn_response_test.go

- `func TestChatTurnResponseRulesAreCentralized(t *testing.T)`
- `func TestConversationTurnResponseFinalizesSuccessfulReply(t *testing.T)`
- `func TestConversationTurnResponseSkipsReasoningOnErrorAndEmptyFinal(t *testing.T)`

### chat_turn_state.go

- `func newConversationTurnState() *conversationTurnState`

### chat_turn_state_test.go

- `func TestServiceComponentsAssembleConversationTurnState(t *testing.T)`
- `func TestChatStateWritesAreCentralized(t *testing.T)`
- `func TestChatTurnRequestConstructionIsCentralized(t *testing.T)`

### chat_turn_tail_test.go

- `func TestChatTurnTailRulesAreCentralized(t *testing.T)`
- `func TestConversationTurnTailCompletesWithoutPromotion(t *testing.T)`
- `func TestConversationTurnTailSettlesAndStartsPromotedBatch(t *testing.T)`

### reasoning_visible_test.go

- `func TestAttachLatestReasoningExposesThinkingToVisibleMessage(t *testing.T)` — TestAttachLatestReasoningExposesThinkingToVisibleMessage 验证回合结束后，

### visible_output_test.go

- `func TestAppendDeltaDoesNotExposeThoughtContent(t *testing.T)`

### visible_role_attribution_test.go

- `func TestVisibleConversationCarriesRoleAttribution(t *testing.T)` — TestVisibleConversationCarriesRoleAttribution 复现实时聊天载荷缺归属：
- `func visibleMessage(t *testing.T, messages []model.Message, match func(model.Message) bool) model.Message` — visibleMessage 取第一条满足条件的可见消息（缺失即用例失败）。
- `func visibleConversationFor(service *Service, sessionID string) []model.Message` — visibleConversationFor 读取指定会话的可见投影（非活跃会话也能读）。
