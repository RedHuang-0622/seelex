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
- `func (service *Service) runChat(ctx context.Context, sessionID, requestID string, request chatRequest)` — runChat 在独立 goroutine 中执行一次会话提交：委托 Seele loop（9.2 边界，
- `func (service *Service) recordUnhandledTaskErrorLocked(sessionID, requestID string, err error)`
- `func (service *Service) finalizeReActBudget(ctx context.Context, requestID string) error` — finalizeReActBudget 在工具预算耗尽后保留一次纯文本交付回合。常规循环在
- `func queuedInputRefs(queue []chatRequest) []string` — queuedInputRefs 取排队输入的最小引用（displayInput），供任务终态恢复记录
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
- `func (service *Service) newBatchedDeltaSink(requestID string) (*chat.StreamBatcher, func(string))`
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

### chat_queue.go

- `func newConversationQueue(deps conversationQueueDeps) *conversationQueue`
- `func (queue *conversationQueue) ReorderQueuedInput(sessionID string, from, to int) error` — ReorderQueuedInput moves one queued input between projection indexes.
- `func (queue *conversationQueue) RecallQueuedInput(sessionID string, index int) (string, error)` — RecallQueuedInput removes one queued input and returns its display text.
- `func (queue *conversationQueue) guardLocked() error`
- `func (queue *conversationQueue) applyEditLocked(sessionID string, unit conversationQueueUnit)` — applyEditLocked reprojects queue state and releases ViewMu before publishing.
- `func newServiceConversationQueue(service *Service) *conversationQueue`
- `func (service *Service) queueComponent() *conversationQueue` — queueComponent returns nil for a nil facade so the existing queue error semantics remain intact without adding state-machine code to Service.

### chat_turn_request.go

- `func (state *conversationTurnState) NewRequest(input string, layers []prompt.PromptLayer) chatRequest` — NewRequest freezes one accepted input and its current Skill context.
- `func (state *conversationTurnState) QueuedRequests(requests []sessioncase.QueuedRequest) []chatRequest` — QueuedRequests restores only valid chat payloads from opaque SessionUnit queue rows, preserving queue order.
- `func (state *conversationTurnState) Combine(requests []chatRequest) chatRequest` — Combine merges a promoted queue batch using the existing separator and frozen Skill merge semantics.
- `func chatRequestDisplays(requests []chatRequest) []string`
- `func selectedSkillLayers(layers []prompt.PromptLayer) []prompt.PromptLayer`
- `func mergeSkillLayers(current, incoming []prompt.PromptLayer) []prompt.PromptLayer`
- `func queuedInputRefs(queue []chatRequest) []string`

### chat_turn_process.go

- `func newConversationTurnProcess(deps conversationTurnProcessDeps) *conversationTurnProcess`
- `func (process *conversationTurnProcess) BeginLocked(parent context.Context, sessionID string, unit conversationTurnUnit, request chatRequest, options chatTurnStartOptions) (context.Context, string, model.TranscriptEvent)`
- `func (process *conversationTurnProcess) PromoteLocked(sessionID string, unit conversationTurnPromotionUnit) chatTurnPromotion` — PromoteLocked 在有排队输入时启动合并批次，否则结束当前回合并按全局忙闲标记 idle。
- `func newServiceConversationTurnProcess(service *Service) *conversationTurnProcess`

### chat_turn_process_test.go

- `func TestServiceComponentsAssembleConversationTurnProcess(t *testing.T)`
- `func TestChatTurnStartRulesAreCentralized(t *testing.T)`
- `func TestChatTurnPersistenceRulesAreCentralized(t *testing.T)` — 结构门禁要求 ChatTurn 持久化与 durable queue 结算只在完成组件中实现。
- `func TestChatTurnQueuePromotionRulesAreCentralized(t *testing.T)` — 结构门禁要求 ChatTurn 队列提升判断只在 turn process 中实现。
- `func (unit *queuePromotionTestUnit) ChatState() model.ChatState`
- `func (unit *queuePromotionTestUnit) SetChatState(chat model.ChatState, _ *sessioncase.View)`
- `func (unit *queuePromotionTestUnit) UpdateChat(update func(*model.ChatState), _ *sessioncase.View)`
- `func (unit *queuePromotionTestUnit) QueueProjection() ([]string, int)`
- `func (unit *queuePromotionTestUnit) SetCancel(cancel context.CancelFunc)`
- `func (unit *queuePromotionTestUnit) SetStream(sessioncase.VisibleOutputSink)`
- `func (unit *queuePromotionTestUnit) PendingRequests() []sessioncase.QueuedRequest`
- `func (unit *queuePromotionTestUnit) SetRequests(requests []sessioncase.QueuedRequest)`
- `func TestConversationTurnProcessFinishesWhenQueueIsEmpty(t *testing.T)` — 空队列时结束当前回合并按全局忙闲标记 idle。
- `func TestConversationTurnProcessKeepsProcessBusyWhenOtherChatRuns(t *testing.T)` — 其它会话仍运行时不得把进程标记为 idle。

### chat_turn_completion.go

- `func newConversationTurnCompletion(deps conversationTurnCompletionDeps) *conversationTurnCompletion`
- `func (completion *conversationTurnCompletion) Persist(sessionID, requestID string, turnErr error) (session_runtime.Location, error)` — Persist 结算本轮消费项的持久化结果：失败回草稿，成功后确认消费并释放工作历史。
- `func newServiceConversationTurnCompletion(service *Service) *conversationTurnCompletion`

### chat_turn_completion_test.go

- `func TestConversationTurnCompletionConfirmsPublishedTurn(t *testing.T)` — 成功发布后必须依次确认 queue 并释放工作历史。
- `func TestConversationTurnCompletionReturnsConsumedInputOnPersistenceFailure(t *testing.T)` — 持久化失败时把消费项退回草稿、不确认也不释放，并保留错误分类。

### chat_turn_outcome.go

- `func newConversationTurnOutcome(deps conversationTurnOutcomeDeps) *conversationTurnOutcome`
- `func (outcome *conversationTurnOutcome) ApplyLocked(sessionID, requestID string, unit conversationChatStateUnit, projection view_state.RuntimeStateProjection, turnErr error) (visibleError string, stale bool)` — ApplyLocked 拒绝旧请求并应用错误状态、error 行与 runtime projection。
- `func (outcome *conversationTurnOutcome) MirrorLocked(sessionID string, unit conversationChatStateUnit, active bool) uint64` — MirrorLocked 写入 ChatState，并仅为活跃会话镜像 Task 与推进 revision。
- `func newServiceConversationTurnOutcome(service *Service) *conversationTurnOutcome`

### chat_turn_outcome_test.go

- `func TestChatTurnOutcomeRulesAreCentralized(t *testing.T)` — 结构门禁要求 ChatTurn 终态结果投影集中实现。
- `func TestConversationTurnOutcomeAppliesCurrentResult(t *testing.T)` — 当前成功回合清理旧错误并应用 runtime projection。
- `func TestConversationTurnOutcomeRecordsErrorAndProjection(t *testing.T)` — 当前失败回合保留原始错误正文并追加 error 行。
- `func TestConversationTurnOutcomeRejectsSupersededRequest(t *testing.T)` — 旧 request 不得修改当前会话的任何终态投影。
- `func TestConversationTurnOutcomeMirrorsActiveSessionAndRevision(t *testing.T)` — 活跃会话依次写 ChatState、镜像 Task 并 bump revision。
- `func TestConversationTurnOutcomeDoesNotMirrorBackgroundTask(t *testing.T)` — 后台会话不得覆盖共享 Task 或进程 revision。

### chat_turn_publication.go

- `func newConversationTurnPublisher(deps conversationTurnPublisherDeps) *conversationTurnPublisher`
- `func (publisher *conversationTurnPublisher) Publish(publication chatTurnPublication)` — Publish 按固定顺序发布 ChatState、终态事件和提升批次投影。
- `func newServiceConversationTurnPublisher(service *Service) *conversationTurnPublisher`

### chat_turn_publication_test.go

- `func TestChatTurnPublicationRulesAreCentralized(t *testing.T)` — 结构门禁要求 `runChat` 不直接发布终态事件。
- `func TestConversationTurnPublisherPublishesSnapshotWithoutPromotion(t *testing.T)` — 无队列提升时只发布 ChatState 与 snapshot。
- `func TestConversationTurnPublisherReportsErrorBeforePromotedBatch(t *testing.T)` — error、提升消息、task batch 和 runtime projections 保持既有顺序。

### chat_turn_tail.go

- `func newConversationTurnTail(deps conversationTurnTailDeps) *conversationTurnTail`
- `func (tail *conversationTurnTail) Complete(location session_runtime.Location, publication chatTurnPublication, nextContext context.Context)` — Complete 在 ViewMu 外按固定顺序完成异步收尾。
- `func newServiceConversationTurnTail(service *Service) *conversationTurnTail`

### chat_turn_tail_test.go

- `func TestChatTurnTailRulesAreCentralized(t *testing.T)` — 结构门禁要求 `runChat` 不直接执行异步尾段。
- `func TestConversationTurnTailCompletesWithoutPromotion(t *testing.T)` — 无队列提升时仍发布、释放并落盘工具记录，但不启动下一轮。
- `func TestConversationTurnTailSettlesAndStartsPromotedBatch(t *testing.T)` — consumed 标记失败不阻止发布、下一轮启动、释放和落盘。

### chat_turn_input.go

- `func newConversationTurnInput(deps conversationTurnInputDeps) *conversationTurnInput`
- `func (input *conversationTurnInput) Prepare(sessionID, requestID, modelInput string) (string, error)` — Prepare 执行 prompt、context 和非空输入准备。
- `func newServiceConversationTurnInput(service *Service) *conversationTurnInput`

### chat_turn_input_test.go

- `func TestChatTurnInputRulesAreCentralized(t *testing.T)` — 结构门禁要求 runChat 不直接执行输入准备。
- `func TestConversationTurnInputPreparesPromptAndContext(t *testing.T)` — prompt/context/normalize 顺序正确。
- `func TestConversationTurnInputStopsOnContextError(t *testing.T)` — context 失败后停止归一化。

### chat_turn_response.go

- `func newConversationTurnResponse(deps conversationTurnResponseDeps) *conversationTurnResponse`
- `func (response *conversationTurnResponse) Finalize(ctx context.Context, sessionID, requestID, reply string, turnErr error)` — Finalize 处理 reasoning、输出观测和 transcript 回填。
- `func newServiceConversationTurnResponse(service *Service) *conversationTurnResponse`

### chat_turn_response_test.go

- `func TestChatTurnResponseRulesAreCentralized(t *testing.T)` — 结构门禁要求 runChat 不直接执行返回事务。
- `func TestConversationTurnResponseFinalizesSuccessfulReply(t *testing.T)` — 成功回复的 reasoning/observe/final/backfill 顺序正确。
- `func TestConversationTurnResponseSkipsReasoningOnErrorAndEmptyFinal(t *testing.T)` — 失败回合跳过 reasoning，空回复跳过 final，但仍观测与回填。

### chat_turn_recovery.go

- `func newConversationTurnRecovery(deps conversationTurnRecoveryDeps) *conversationTurnRecovery`
- `func (recovery *conversationTurnRecovery) Finalize(ctx context.Context, sessionID, requestID, originalInput, reply string, turnErr error, onChunk func(string)) error` — Finalize 执行 context recovery、bounded retry、budget 和 Task 终态。
- `func newServiceConversationTurnRecovery(service *Service) *conversationTurnRecovery`

### chat_turn_recovery_test.go

- `func TestChatTurnRecoveryRulesAreCentralized(t *testing.T)` — 结构门禁要求 runChat 不直接执行恢复链。
- `func TestConversationTurnRecoveryRetriesAfterRecovery(t *testing.T)` — recovery 后通过有界 retry，再进入 budget 与 Task finalize。
- `func TestConversationTurnRecoveryPropagatesRecoveryAndRetryErrors(t *testing.T)` — recovery/retry 错误保持组合文案并阻断 budget。
- `func TestConversationTurnRecoveryStopsBeforeTaskOnBudgetError(t *testing.T)` — budget 失败后不进入 Task finalize。

### chat_turn_cleanup.go

- `func newConversationTurnCleanup(deps conversationTurnCleanupDeps) *conversationTurnCleanup`
- `func (cleanup *conversationTurnCleanup) Cleanup(sessionID, requestID string, turnErr error, flush func() error, stopDraft func()) (view_state.RuntimeStateProjection, error)` — Cleanup 执行 flush、route、mailbox、projection、cleanup 和错误记录。
- `func newServiceConversationTurnCleanup(service *Service) *conversationTurnCleanup`

### chat_turn_cleanup_test.go

- `func TestChatTurnCleanupRulesAreCentralized(t *testing.T)` — 结构门禁要求 runChat 不直接执行清理链。
- `func TestConversationTurnCleanupRunsFullSuccessfulPath(t *testing.T)` — 成功清理顺序与 projection 返回正确。
- `func TestConversationTurnCleanupCombinesErrorsAndKeepsRunning(t *testing.T)` — 原回合错误优先、checkpoint cleanup 继续、错误记录与 draft stop 保持。
- `func TestConversationTurnCleanupWrapsFlushAndRecoveryErrors(t *testing.T)` — flush/recovery 错误语义保持。

### chat_turn_commit.go

- `func newConversationTurnCommit(deps conversationTurnCommitDeps) *conversationTurnCommit`
- `func (commit *conversationTurnCommit) Commit(sessionID, requestID string, turnErr error, projection view_state.RuntimeStateProjection) chatTurnCommitResult` — Commit 串联持久化、outcome、提升、消息和镜像。
- `func newServiceConversationTurnCommit(service *Service) *conversationTurnCommit`

### chat_turn_commit_test.go

- `func TestChatTurnCommitRulesAreCentralized(t *testing.T)` — 结构门禁要求 runChat 不直接执行持久化/outcome/提升/镜像。
- `func TestConversationTurnCommitRunsPersistenceOutcomePromotion(t *testing.T)` — 完整成功提交顺序与结果字段正确。
- `func TestConversationTurnCommitStopsOnStaleRequest(t *testing.T)` — stale 请求不提升、不追加消息、不镜像并正常释放锁。

### chat_turn_finalize.go

- `func newConversationTurnFinalize(deps conversationTurnFinalizeDeps) *conversationTurnFinalize`
- `func (finalize *conversationTurnFinalize) Finalize(committed chatTurnCommitResult)` — Finalize 映射 publication 并交给 tail。
- `func newServiceConversationTurnFinalize(service *Service) *conversationTurnFinalize`

### chat_turn_finalize_test.go

- `func TestChatTurnFinalizeRulesAreCentralized(t *testing.T)` — 结构门禁要求 runChat 不直接构造 publication/tail。
- `func TestConversationTurnFinalizeMapsCommitToPublication(t *testing.T)` — commit 字段完整映射到 publication/tail。
- `func TestConversationTurnFinalizeIgnoresStaleCommit(t *testing.T)` — stale commit 不发布。

### chat_turn_execution.go

- `func newConversationTurnExecution(deps conversationTurnExecutionDeps) *conversationTurnExecution`
- `func (execution *conversationTurnExecution) Prepare(sessionID string)` — Prepare 按固定顺序同步 Session 执行策略。
- `func newServiceConversationTurnExecution(service *Service) *conversationTurnExecution`

### chat_turn_execution_test.go

- `func TestChatTurnExecutionRulesAreCentralized(t *testing.T)` — 结构门禁要求 runChat 不直接执行策略同步。
- `func TestConversationTurnExecutionPreparesInOrder(t *testing.T)` — root/subagent/reasoning/full-access 顺序正确。

### chat_turn_state.go

- `func newConversationTurnState() *conversationTurnState`
- `func (state *conversationTurnState) BeginLocked(unit conversationChatStateUnit, requestID string)` — BeginLocked starts one active turn with a fresh request identity.
- `func (state *conversationTurnState) ProjectQueueLocked(unit conversationQueueProjectionUnit) ([]string, int)` — ProjectQueueLocked makes the queue projection the ChatState queue view.
- `func (state *conversationTurnState) ClearErrorLocked(unit conversationChatStateUnit)` — ClearErrorLocked removes the previous turn's visible error.
- `func (state *conversationTurnState) SetErrorLocked(unit conversationChatStateUnit, visibleError string)` — SetErrorLocked records the current turn's visible error.
- `func (state *conversationTurnState) FinishLocked(unit conversationChatStateUnit)` — FinishLocked ends the active turn while retaining request metadata for the final display/event pair, matching existing behavior.
- `func (state *conversationTurnState) ResetLocked(unit conversationChatStateUnit)` — ResetLocked returns the unit to an empty, non-running state.

### chat_turn_state_test.go

- `func TestServiceComponentsAssembleConversationTurnState(t *testing.T)`
- `func TestChatStateWritesAreCentralized(t *testing.T)`

### chat_state_event_test.go

- `func TestChatStateIsPublishedNotInferred(t *testing.T)` — TestChatStateIsPublishedNotInferred 验证运行态由后端下发：提交后收到

### reasoning_visible_test.go

- `func TestAttachLatestReasoningExposesThinkingToVisibleMessage(t *testing.T)` — TestAttachLatestReasoningExposesThinkingToVisibleMessage 验证回合结束后，

### visible_output_test.go

- `func TestAppendDeltaDoesNotExposeThoughtContent(t *testing.T)`

### visible_role_attribution_test.go

- `func TestVisibleConversationCarriesRoleAttribution(t *testing.T)` — TestVisibleConversationCarriesRoleAttribution 复现实时聊天载荷缺归属：
- `func visibleMessage(t *testing.T, messages []model.Message, match func(model.Message) bool) model.Message` — visibleMessage 取第一条满足条件的可见消息（缺失即用例失败）。
- `func visibleConversationFor(service *Service, sessionID string) []model.Message` — visibleConversationFor 读取指定会话的可见投影（非活跃会话也能读）。
