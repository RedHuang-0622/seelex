# application/conversation

## 生态位

`application/conversation` 承载 Conversation Queue、ChatState 和统一会话访问面的应用编排。
`SessionUnit` 仍是队列内容和 ChatState 的唯一状态所有者；`conversation/context`
和 `conversation/search` 为子域包。

## 职责

- 排队输入重排与召回；
- 已受理输入入队、revision 投影、durable queue 镜像；
- durable “已发送未确认”输入回填与非运行会话的队列合并；
- 同一 ViewMu 内的队列投影、ChatState 镜像和 revision 收口；
- 关闭/排空门禁。
- assistant 推理步骤与可见消息的配对回填。
- 单个 ChatTurn 从输入准备到 commit/finalize 的应用编排。
- 单个持久回合的 artifact → session → compaction 跨聚合写序。
- 统一 main/subagent/teammate 会话的 View、Chat、Recover 路由。

## 非职责

- 不拥有 SessionUnit、Engine 或持久队列；
- Engine ReAct 循环本身仍由 Seele 拥有；`TurnRunner` 只编排 application 组件链。
- 不实现 context compaction 或历史检索。

## 文件结构

- `queue.go`：`QueueUnit`、`QueueDeps`、`QueueApplication`；拥有 queue edit、
  enqueue/durable mirror、recovery 回填和 recovered batch 合并。
- `submission.go`：`SubmissionApplication`；拥有一次输入到 Request/Budget、
  当前 draft materialization 与显式后台 session 提交的边界。
- `turn_start.go`：`TurnStartApplication`；唯一拥有 restoring/compacting 延后、
  running 入队、恢复队列排空、turn 起点投影与启动发布顺序。
- `turn_budget.go`：`TurnBudgetFinalizerApplication`；唯一定义预算耗尽后的
  text-only `+1` 收尾输入并编排 prepare/stream/cleanup。
- `turn_error.go`：`TurnErrorApplication`；按权威 Task 状态分类取消、provider
  中断和未验证失败。
- `turn_state.go`：ChatState 转换规则。
- `request.go`：不可变 ChatRequest 与批次合成。
- `turn_input.go`：prompt/context 准备与输入归一。
- `turn_response.go`：模型回复观察与 reasoning 回填。
- `turn_recovery.go`：context recovery、retry 与 Task/budget finalize。
- `turn_cleanup.go`：stream flush、route/mailbox、checkpoint/recovery 清理。
- `turn_process.go`：ChatTurn 起点事务与队列提升。
- `turn_outcome.go`：终态结果、错误和活动 Task 镜像。
- `turn_completion.go`：持久化完成与 consumed queue 结算。
- `turn_store_commit.go`：`TurnStoreCommitPort` 与 `TurnStoreCommitApplication`；
  唯一负责三个聚合提交的跨聚合写序，store 只提供单聚合写口。
- `turn_commit.go`：persist/outcome/promote/mirror 的锁内串联。
- `turn_finalize.go`：commit result 到 publication。
- `turn_publication.go`：`chat.changed` / snapshot / error / message 事件发布。
- `turn_tail.go`：解锁后的 consumed 标记、下一轮、release 与 tool-record flush。
- `turn_execution.go`：会话执行策略同步。
- `reasoning.go`：历史推理步骤提取、可见步骤配对和 legacy fallback。
- `stream.go`：`StreamOutputPort`、可见输出与 batcher 的消费方契约。
- `stream_runtime.go`：可见 chunk/batcher、后台会话增量、reasoning 回填与
  served route 标记的 Application runtime。
- `session_access.go`：统一会话读面与 `SessionRecordsPort`；分页、UTF-8
  裁剪仍由 Middle 的唯一分页实现拥有。
- `unified.go`：`UnifiedApplication` 负责 main/subagent/teammate 路由、
  主会话记录投影、恢复分类和 Submit 委托；不持有 Session 或 Chat 状态。
- `turn_runner.go`：`runChat` 对应的输入→stream→recovery→cleanup→commit→finalize 编排。
- `query/`：ConversationHistoryQuery。
- `context/`：Context Compaction。
- `search/`：history search 工具适配。

## 依赖方向

Conversation 定义 `StreamOutputPort` 与 `SessionRecordsPort`；Wiring 分别注入
`kernel/react` 的可见输出/batcher 实现和
`middle/view/sessionaccess.PaginateRecords`。`application/assembly` 不再 import
engine/react 或 sessionaccess。

`TurnStoreCommitPort` 由消费方定义，wiring adapter 把 App DTO 映射到 `store`
的三个单聚合写口；本包不 import store，也不复制内容寻址或 head 发布规则。

`UnifiedApplication` 只接收 Session Snapshot 查询、Record 分页、
`AgentConversationPort` 与 `SubmitToSession` 回调。GUI/headless 经
消费方自己的 `ConversationSessions` 端口直接调用该组件，core 不保存统一会话适配器。

## 并发与错误

队列编辑在 `ViewMu` 内修改 SessionUnit，投影后先镜像活跃 ChatState、bump
revision，再解锁发布。关闭/排空错误保持原语义。

## 测试与验证

```text
go test ./application/conversation -count=1
go test ./application/conversation -run 'Test(TurnStartApplication|ConversationSubmission|TurnBudget|TurnErrorApplication)' -count=1
go test ./application/assembly -run 'Queue|QueueComponent|ReorderQueuedInput|RecallQueuedInput' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### queue.go

- `func NewQueue(deps QueueDeps) *QueueApplication` — NewQueue constructs the queue application.
- `func (application *QueueApplication) ReorderQueuedInput(sessionID string, from, to int) error` — ReorderQueuedInput moves one queued input between projection indexes.
- `func (application *QueueApplication) RecallQueuedInput(sessionID string, index int) (string, error)` — RecallQueuedInput removes one queued input and returns its text.
- `func (application *QueueApplication) EnqueueLocked(sessionID string, unit QueueUnit, request Request, active bool) uint64` — EnqueueLocked projects one accepted request into the session queue. The
- `func (application *QueueApplication) AfterEnqueue(sessionID string, revision uint64, request Request)` — AfterEnqueue publishes the in-memory queue projection and mirrors it to the
- `func (application *QueueApplication) DrainLocked(unit QueueUnit, request Request) (Request, bool)` — DrainLocked merges recovered pending rows with the current request. The
- `func (application *QueueApplication) RecoverInputs( sessionID string, resent []dto.QueueItem, layers []prompt.PromptLayer, )` — RecoverInputs projects durable "sent but unconfirmed" rows back into the
- `func (application *QueueApplication) guardLocked() error`
- `func (application *QueueApplication) applyEditLocked(sessionID string, unit QueueUnit)`

### queue_enqueue_external_test.go

- `func TestQueueEnqueueProjectsAndPublishesRevision(t *testing.T)`
- `func TestQueueAfterEnqueuePublishesThenMirrorsDurably(t *testing.T)`
- `func TestQueueDrainLockedCombinesPendingBeforeCurrentRequest(t *testing.T)`
- `func TestQueueRecoverInputsProjectsOnlyNonEmptyRows(t *testing.T)`

### reasoning.go

- `func ReasoningStepsFromHistory(history []contract.EngineMessage) []ReasoningStep` — ReasoningStepsFromHistory extracts assistant reasoning in history order.
- `func LastReasoningFromHistory(history []contract.EngineMessage) string` — LastReasoningFromHistory returns the last assistant reasoning text.
- `func AttachStepReasoningLocked(view *session.View, steps []ReasoningStep) []event.MessageDelta` — AttachStepReasoningLocked attaches each visible assistant step to its own
- `func visibleStepCallIDs(conversation []model.Message, index int) map[string]bool`
- `func takeReasoningStep(steps []ReasoningStep, content string, ids map[string]bool) int`
- `func AttachLastReasoningLocked(view *session.View, reasoning string) (event.MessageDelta, bool)` — AttachLastReasoningLocked is the legacy fallback when no step matches.

### reasoning_test.go

- `func TestReasoningProjectionPairsVisibleSteps(t *testing.T)`
- `func TestAttachLastReasoningFallback(t *testing.T)`

### records_test.go

- `func TestMainRecordsPreserveOrderAndToolRows(t *testing.T)`
- `func TestMainRecoveryClassifiesLiveColdAndInterrupted(t *testing.T)`

### request.go

- `func NewRequestApplication(deps RequestDeps) *RequestApplication` — NewRequestApplication constructs the request factory.
- `func (*RequestApplication) NewRequest(input string, layers []prompt.PromptLayer) Request` — NewRequest freezes one accepted input and its Skill layers.
- `func (*RequestApplication) QueuedRequests(requests []sessioncase.QueuedRequest) []Request` — QueuedRequests restores valid request payloads from opaque queue rows.
- `func (application *RequestApplication) Combine(requests []Request) Request` — Combine merges a promoted queue batch with the existing separator and Skill
- `func ChatRequestDisplays(requests []Request) []string` — ChatRequestDisplays extracts display inputs.
- `func QueuedInputRefs(queue []Request) []string` — QueuedInputRefs extracts non-empty display inputs.
- `func SelectedSkillLayers(layers []prompt.PromptLayer) []prompt.PromptLayer` — SelectedSkillLayers filters frozen Skill layers.
- `func mergeSkillLayers(current, incoming []prompt.PromptLayer) []prompt.PromptLayer`

### request_test.go

- `func envelopeDeps() conversation.RequestDeps` — envelopeDeps 用最小信封实现替代装配注入的真实编码，只验证 Combine 自己的
- `func TestSelectedSkillLayersKeepsOnlySkillKind(t *testing.T)`
- `func TestRequestCombinePreservesDisplayAndFrozenSkillLayers(t *testing.T)`
- `func TestRequestCombineUnwrapsAndRewrapsEnvelopes(t *testing.T)` — TestRequestCombineUnwrapsAndRewrapsEnvelopes 钉住已带信封的队列批次：
- `func TestRequestCombineMergesSkillsByName(t *testing.T)`

### session_access_boundary_test.go

- `func TestCoreDoesNotImportSessionAccessView(t *testing.T)`

### stream_boundary_test.go

- `func TestCoreDoesNotImportEngineReactStreamSurface(t *testing.T)`
- `func conversationRepositoryRoot(t *testing.T) string`

### stream_runtime.go

- `func NewStreamRuntime(deps StreamRuntimeDeps) *StreamRuntime` — NewStreamRuntime constructs the streaming runtime.
- `func (runtime *StreamRuntime) AppendDelta(requestID, chunk string)` — AppendDelta consumes one provider chunk and appends its visible portion.
- `func (runtime *StreamRuntime) NewBatchedDeltaSink(requestID string) (StreamBatcher, func(string))` — NewBatchedDeltaSink creates a bounded visible-output batcher.
- `func (runtime *StreamRuntime) FlushStreamBatcherFor(sessionID string)` — FlushStreamBatcherFor flushes one session's buffered visible text.
- `func (runtime *StreamRuntime) ConsumeVisibleChunk(requestID, chunk string) string` — ConsumeVisibleChunk applies one provider chunk to the owning session.
- `func (runtime *StreamRuntime) consumeVisibleChunkBackground(sessionID, requestID, chunk string) string`
- `func (runtime *StreamRuntime) AppendVisibleDelta(requestID, chunk string)` — AppendVisibleDelta appends one visible chunk to the active or background
- `func (runtime *StreamRuntime) appendVisibleDeltaBackground(sessionID, requestID, chunk string)`
- `func (runtime *StreamRuntime) AttachLatestReasoning(sessionID, requestID string)` — AttachLatestReasoning attaches engine reasoning steps to visible assistant
- `func (runtime *StreamRuntime) StampServedRoute(sessionID, requestID string)` — StampServedRoute fills the served route onto the current turn's assistant
- `func (runtime *StreamRuntime) StreamedAssistantTextLocked(sessionID string) string` — StreamedAssistantTextLocked returns the current request's accumulated
- `func (runtime *StreamRuntime) streamedRequestTextLocked(sessionID string) string`
- `func (runtime *StreamRuntime) sessionUnitLocked(sessionID string) *sessionstate.SessionUnit`

### submission.go

- `func NewSubmission(deps SubmissionDeps) *SubmissionApplication` — NewSubmission constructs the submission application.
- `func (application *SubmissionApplication) Submit(ctx context.Context, input string) error` — Submit materializes the viewed draft and submits to that session.
- `func (application *SubmissionApplication) SubmitFor(ctx context.Context, sessionID, input string) error` — SubmitFor submits to an explicit background session without consuming the
- `func (application *SubmissionApplication) requestFor(sessionID, input string) Request`

### test_unit_test.go

- `func (unit *queuePromotionTestUnit) ChatState() model.ChatState`
- `func (unit *queuePromotionTestUnit) SetChatState(chat model.ChatState, _ *sessioncase.View)`
- `func (unit *queuePromotionTestUnit) UpdateChat(update func(*model.ChatState), _ *sessioncase.View)`
- `func (unit *queuePromotionTestUnit) QueueProjection() ([]string, int)`
- `func (unit *queuePromotionTestUnit) SetCancel(cancel context.CancelFunc)`
- `func (unit *queuePromotionTestUnit) SetStream(sessioncase.VisibleOutputSink)`
- `func (unit *queuePromotionTestUnit) PendingRequests() []sessioncase.QueuedRequest`
- `func (unit *queuePromotionTestUnit) SetRequests(requests []sessioncase.QueuedRequest)`

### turn_budget.go

- `func NewTurnBudgetFinalizer(deps TurnBudgetFinalizerDeps) *TurnBudgetFinalizerApplication` — NewTurnBudgetFinalizer constructs the budget finalizer.
- `func (application *TurnBudgetFinalizerApplication) Finalize( ctx context.Context, requestID string, onChunk func(string), ) error` — Finalize performs at most one text-only model call after budget exhaustion.

### turn_budget_test.go

- `func TestReActBudgetFinalizationInputHasSingleContractMarker(t *testing.T)`
- `func TestTurnBudgetFinalizerPreservesPrepareStreamCleanupOrder(t *testing.T)`
- `func TestTurnBudgetFinalizerReturnsCleanupFailureAfterDelivery(t *testing.T)`

### turn_cleanup.go

- `func NewTurnCleanup(deps TurnCleanupDeps) *TurnCleanupApplication` — NewTurnCleanup constructs the turn-cleanup application.
- `func (application *TurnCleanupApplication) Cleanup( sessionID, requestID string, turnErr error, flush func() error, stopDraft func(), ) (view_state.RuntimeStateProjection, error)` — Cleanup flushes, stamps route, discards mailbox, collects projection and

### turn_cleanup_test.go

- `func TestConversationTurnCleanupRunsFullSuccessfulPath(t *testing.T)`
- `func TestConversationTurnCleanupCombinesErrorsAndKeepsRunning(t *testing.T)`
- `func TestConversationTurnCleanupWrapsFlushAndRecoveryErrors(t *testing.T)`

### turn_commit.go

- `func NewTurnCommit(deps TurnCommitDeps) *TurnCommitApplication` — NewTurnCommit constructs the turn-commit application.
- `func (application *TurnCommitApplication) Commit( sessionID, requestID string, turnErr error, projection view_state.RuntimeStateProjection, ) CommitResult` — Commit persists, applies outcome, promotes queue and mirrors under the lock.

### turn_commit_test.go

- `func (locker *recordingLocker) Lock()`
- `func (locker *recordingLocker) Unlock()`
- `func TestConversationTurnCommitRunsPersistenceOutcomePromotion(t *testing.T)`
- `func TestConversationTurnCommitStopsOnStaleRequest(t *testing.T)`

### turn_completion.go

- `func NewTurnCompletion(deps TurnCompletionDeps) *TurnCompletionApplication` — NewTurnCompletion constructs the turn-completion application.
- `func (application *TurnCompletionApplication) Persist(sessionID, requestID string, turnErr error) (session_runtime.Location, error)` — Persist settles consumed input according to persistence success.

### turn_completion_test.go

- `func TestConversationTurnCompletionConfirmsPublishedTurn(t *testing.T)`
- `func TestConversationTurnCompletionReturnsConsumedInputOnPersistenceFailure(t *testing.T)`

### turn_error.go

- `func NewTurnErrorApplication(deps TurnErrorDeps) *TurnErrorApplication` — NewTurnErrorApplication constructs the turn-error application.
- `func (application *TurnErrorApplication) Record(sessionID, requestID string, err error)` — Record moves the active Task to the matching terminal state.

### turn_error_test.go

- `func TestTurnErrorApplicationClassifiesUnhandledTaskFailures(t *testing.T)`

### turn_execution.go

- `func NewTurnExecution(deps TurnExecutionDeps) *TurnExecutionApplication` — NewTurnExecution constructs the turn execution application.
- `func (application *TurnExecutionApplication) Prepare(sessionID string)` — Prepare applies the execution policy in stable order.

### turn_execution_test.go

- `func TestConversationTurnExecutionPreparesInOrder(t *testing.T)`

### turn_finalize.go

- `func NewTurnFinalize(deps TurnFinalizeDeps) *TurnFinalizeApplication` — NewTurnFinalize constructs the finalize application.
- `func (application *TurnFinalizeApplication) Finalize(committed CommitResult)` — Finalize ignores stale results and starts the tail for current results.

### turn_finalize_test.go

- `func TestConversationTurnFinalizeMapsCommitToPublication(t *testing.T)`
- `func TestConversationTurnFinalizeIgnoresStaleCommit(t *testing.T)`

### turn_input.go

- `func NewTurnInput(deps TurnInputDeps) *TurnInputApplication` — NewTurnInput constructs the turn-input application.
- `func (application *TurnInputApplication) Prepare(sessionID, requestID, modelInput string) (string, error)` — Prepare applies the prompt, prepares provider context and normalizes input.

### turn_input_test.go

- `func TestConversationTurnInputPreparesPromptAndContext(t *testing.T)`
- `func TestConversationTurnInputStopsOnContextError(t *testing.T)`

### turn_outcome.go

- `func NewTurnOutcome(deps TurnOutcomeDeps) *TurnOutcomeApplication` — NewTurnOutcome constructs the turn-outcome application.
- `func (application *TurnOutcomeApplication) ApplyLocked( sessionID, requestID string, unit ChatStateUnit, projection view_state.RuntimeStateProjection, turnErr error, ) (string, bool)` — ApplyLocked applies the current outcome or rejects a superseded request.
- `func (application *TurnOutcomeApplication) MirrorLocked(sessionID string, unit ChatStateUnit, active bool) uint64` — MirrorLocked mirrors ChatState and active task/revision.

### turn_outcome_test.go

- `func TestConversationTurnOutcomeAppliesCurrentResult(t *testing.T)`
- `func TestConversationTurnOutcomeRecordsErrorAndProjection(t *testing.T)`
- `func TestConversationTurnOutcomeRejectsSupersededRequest(t *testing.T)`
- `func TestConversationTurnOutcomeMirrorsActiveSessionAndRevision(t *testing.T)`
- `func TestConversationTurnOutcomeDoesNotMirrorBackgroundTask(t *testing.T)`

### turn_process.go

- `func NewTurnProcess(deps TurnProcessDeps) *TurnProcessApplication` — NewTurnProcess constructs the turn-process application.
- `func (application *TurnProcessApplication) BeginLocked( parent context.Context, sessionID string, unit TurnUnit, request Request, options StartOptions, ) (context.Context, string, model.TranscriptEvent)` — BeginLocked starts one turn under the caller's view lock.
- `func (application *TurnProcessApplication) PromoteLocked(sessionID string, unit PromotionUnit) Promotion` — PromoteLocked is the single queue decision point after persistence.

### turn_process_test.go

- `func (*turnStateTestAdapter) QueuedRequests([]sessioncase.QueuedRequest) []applicationconversation.Request`
- `func (*turnStateTestAdapter) Combine([]applicationconversation.Request) applicationconversation.Request`
- `func TestConversationTurnProcessFinishesWhenQueueIsEmpty(t *testing.T)`
- `func TestConversationTurnProcessKeepsProcessBusyWhenOtherChatRuns(t *testing.T)`

### turn_publication.go

- `func NewTurnPublisher(deps TurnPublisherDeps) *TurnPublisherApplication` — NewTurnPublisher constructs the turn publisher.
- `func (application *TurnPublisherApplication) Publish(publication Publication)` — Publish emits chat/snapshot/error and promoted-message events.

### turn_publication_test.go

- `func TestConversationTurnPublisherPublishesSnapshotWithoutPromotion(t *testing.T)`
- `func TestConversationTurnPublisherReportsErrorBeforePromotedBatch(t *testing.T)`

### turn_recovery.go

- `func NewTurnRecovery(deps TurnRecoveryDeps) *TurnRecoveryApplication` — NewTurnRecovery constructs the turn-recovery application.
- `func (application *TurnRecoveryApplication) Finalize( ctx context.Context, sessionID, requestID, originalInput, reply string, turnErr error, onChunk func(string), ) error` — Finalize applies context failure, bounded retry, budget and task finalization.

### turn_recovery_test.go

- `func TestConversationTurnRecoveryRetriesAfterRecovery(t *testing.T)`
- `func TestConversationTurnRecoveryPropagatesRecoveryAndRetryErrors(t *testing.T)`
- `func TestConversationTurnRecoveryStopsBeforeTaskOnBudgetError(t *testing.T)`

### turn_response.go

- `func NewTurnResponse(deps TurnResponseDeps) *TurnResponseApplication` — NewTurnResponse constructs the turn-response application.
- `func (application *TurnResponseApplication) Finalize( ctx context.Context, sessionID, requestID, reply string, turnErr error, )` — Finalize observes model output and backfills assistant reasoning.

### turn_response_test.go

- `func TestConversationTurnResponseFinalizesSuccessfulReply(t *testing.T)`
- `func TestConversationTurnResponseSkipsReasoningOnErrorAndEmptyFinal(t *testing.T)`

### turn_runner.go

- `func NewTurnRunner(deps TurnRunnerDeps) *TurnRunnerApplication` — NewTurnRunner constructs the turn runner.
- `func (application *TurnRunnerApplication) Run( ctx context.Context, sessionID, requestID string, request Request, )` — Run executes one accepted ChatTurn through the existing component chain.

### turn_runner_test.go

- `func (batcher *testBatcher) Flush() error`
- `func TestTurnRunnerExecutesComponentOrder(t *testing.T)`

### turn_start.go

- `func NewTurnStart(deps TurnStartDeps) *TurnStartApplication` — NewTurnStart constructs the turn-start application.
- `func (application *TurnStartApplication) StartCurrent(ctx context.Context, request Request) error` — StartCurrent starts or queues one request in the currently viewed session.
- `func (application *TurnStartApplication) StartFor(ctx context.Context, sessionID string, request Request) error` — StartFor starts or queues one request for an explicit session.
- `func (application *TurnStartApplication) startLocked( ctx context.Context, sessionID string, request Request, ) error`
- `func (application *TurnStartApplication) ensureTitleLocked(sessionID string, active bool, input string)`
- `func (application *TurnStartApplication) appendTurnMessagesLocked( sessionID string, active bool, input string, userEvent model.TranscriptEvent, ) (model.Message, model.Message)`
- `func (application *TurnStartApplication) publishStart( sessionID, requestID string, revision uint64, user, assistant model.Message, userEvent model.TranscriptEvent, )`

### turn_start_test.go

- `func (view *turnStartTestView) SessionTitleFor(string) model.SessionTitle`
- `func (view *turnStartTestView) SetSessionTitleLocked(_ string, title model.SessionTitle)`
- `func (view *turnStartTestView) AppendMessageWithOriginLocked(role, content string, _ *model.ToolCall, _ model.MessageOrigin) *model.Message`
- `func (view *turnStartTestView) AppendMessageWithOriginLockedFor(sessionID, role, content string, _ *model.ToolCall, _ model.MessageOrigin) *model.Message`
- `func (view *turnStartTestView) SetSessionChatLockedFor(string, model.ChatState)`
- `func (view *turnStartTestView) BumpLocked() uint64`
- `func (process *turnStartTestProcess) BeginLocked( parent context.Context, _ string, _ applicationconversation.TurnUnit, request applicationconversation.Request, _ applicationconversation.StartOptions, ) (context.Context, string, model.TranscriptEvent)`
- `func newTurnStartTestApplication(t *testing.T, view *turnStartTestView, unit *sessioncase.SessionUnit) (*applicationconversation.TurnStartApplication, *[]string)`
- `func TestTurnStartApplicationDefersBeforeOpeningRestoredShell(t *testing.T)`
- `func TestTurnStartApplicationDefersBeforeCompactionWritesHistory(t *testing.T)`
- `func TestTurnStartApplicationQueuesRunningSessionBeforeDraining(t *testing.T)`
- `func TestTurnStartApplicationDrainsRecoveredInputBeforeStarting(t *testing.T)`
- `func TestConversationSubmissionMaterializesOnlyCurrentDraft(t *testing.T)`
- `func (start turnSubmissionTestStarter) StartFor(ctx context.Context, sessionID string, request applicationconversation.Request) error`

### turn_state.go

- `func NewTurnState() *TurnStateApplication` — NewTurnState constructs the turn-state application.
- `func (*TurnStateApplication) BeginLocked(unit ChatStateUnit, requestID string)` — BeginLocked starts one active turn.
- `func (*TurnStateApplication) ProjectQueueLocked(unit QueueProjectionUnit) ([]string, int)` — ProjectQueueLocked projects queued inputs into ChatState.
- `func (*TurnStateApplication) ClearErrorLocked(unit ChatStateUnit)` — ClearErrorLocked removes the previous visible error.
- `func (*TurnStateApplication) SetErrorLocked(unit ChatStateUnit, visibleError string)` — SetErrorLocked records the current visible error.
- `func (*TurnStateApplication) FinishLocked(unit ChatStateUnit)` — FinishLocked ends the active turn.
- `func (*TurnStateApplication) ResetLocked(unit ChatStateUnit)` — ResetLocked returns ChatState to empty.

### turn_store_commit.go

- `func NewTurnStoreCommit(port TurnStoreCommitPort) *TurnStoreCommitApplication` — NewTurnStoreCommit constructs the application turn unit-of-work.
- `func (application *TurnStoreCommitApplication) Commit(ctx context.Context, batch TurnStoreCommitBatch) error` — Commit preserves the M0 compatible artifact -> message -> compaction order.

### turn_store_commit_test.go

- `func (fake *turnStoreCommitFake) WriteArtifacts(context.Context, string, string, []protocol.StoredToolResult) error`
- `func (fake *turnStoreCommitFake) WriteMessages(context.Context, string, string, []protocol.TranscriptEvent) error`
- `func (fake *turnStoreCommitFake) WriteCompactions(context.Context, string, string, []json.RawMessage) error`
- `func TestTurnStoreCommitPreservesM0OrderAndStopsAtFailure(t *testing.T)`

### turn_tail.go

- `func NewTurnTail(deps TurnTailDeps) *TurnTailApplication` — NewTurnTail constructs the tail application.
- `func (application *TurnTailApplication) Complete(location session_runtime.Location, publication Publication, nextContext context.Context)` — Complete settles promoted input, publishes, starts the next turn and flushes.

### turn_tail_test.go

- `func TestConversationTurnTailCompletesWithoutPromotion(t *testing.T)`
- `func TestConversationTurnTailSettlesAndStartsPromotedBatch(t *testing.T)`

### unified.go

- `func NewUnified(deps UnifiedDeps) *UnifiedApplication`
- `func (application *UnifiedApplication) View(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error)`
- `func (application *UnifiedApplication) Chat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error)`
- `func (application *UnifiedApplication) Recover(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error)`
- `func (application *UnifiedApplication) session(ref dto.ConversationSessionRef) (ConversationSession, error)`
- `func (adapter mainConversation) View(_ context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error)`
- `func (adapter mainConversation) Chat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error)`
- `func (adapter mainConversation) Recover(_ context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error)`
- `func MainRecovery(snapshot protocol.SessionSnapshot) dto.SessionRecoveryState` — MainRecovery classifies the durable/live state without taking an
- `func MainRecords(messages []protocol.Message) []dto.ConversationRecord` — MainRecords projects transcript messages into the shared conversation record
- `func (adapter agentConversation) View(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error)`
- `func (adapter agentConversation) Chat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error)`
- `func (adapter agentConversation) Recover(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error)`

### unified_test.go

- `func (stub unifiedQueryStub) SnapshotOf(string) (protocol.SessionSnapshot, error)`
- `func (unifiedRecordsStub) PaginateRecords(sessionID string, records []dto.ConversationRecord, _ dto.ConversationViewRequest) dto.SessionRecordPage`
- `func (stub *unifiedAgentStub) ViewAgentConversation(_ context.Context, ref dto.ConversationSessionRef, _ dto.ConversationViewRequest) (dto.SessionRecordPage, error)`
- `func (*unifiedAgentStub) ChatAgentConversation(context.Context, dto.ConversationSessionRef, dto.ConversationChatRequest) (dto.ChatReceipt, error)`
- `func (*unifiedAgentStub) RecoverAgentConversation(context.Context, dto.ConversationSessionRef) (dto.SessionRecovery, error)`
- `func TestUnifiedApplicationRoutesMainAndAgentConversations(t *testing.T)`
