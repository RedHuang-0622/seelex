# task_context

## 生态位

任务执行域协调器：`TaskExecutionState`（功能打点快照与终态判定）、
`TaskService`（Plan 打卡/终态工具）、append-only transcript、checkpoint、
result-ref、token 审计（`CalibratedTokenCounter`）、plan 帧状态与 ReAct
预算。

## 职责与非职责

- 做：`BeginTask`/`ActivateTaskSkillsLocked`/`AppendTranscriptEventLocked`/
  `BuildTaskCheckpointLocked`/`TaskProjectionLocked`、终态工具
  `VerifyAndApply`、`ObserveTool/PlanEvent/ModelOutput`、上下文压缩记录、
  plan 栈/重规划/预算状态、token 计数与上下文预算。
- 不做：会话持久化跨域事务、chat 流式编排、context 装配。

## 关键文件

| 文件 | 职责 |
|---|---|
| `ports.go` | `Deps` + `PromptPort` + `TaskPersistencePort` 编译期断言。 |
| `coordinator.go` | `Coordinator` + task/plan 子状态 + ReAct 预算 + 恢复装载。 |
| `task_execution.go` | `TaskExecutionState`/`NodeCheckpoint` 与证据/摘要。 |
| `task_service.go` | `TaskService` 终态判定/打点工具 + Plan 投影读取。 |
| `task_context_state.go` | transcript/checkpoint/result-ref/token 审计协调器。 |
| `token_counter.go` | 校准 token 计数器与上下文预算。 |
| `plan_transcript.go` | Plan 投影 helper 与 transcript 协议单元收敛。 |

## 依赖方向

依赖 `state.Core` + 注入端口（prompt 栈、limits、错误呈现、队列引用）；
满足 `session_runtime.TaskPersistencePort` 与 context 域 `TaskPort`（编译期
断言见 `ports.go` 与根包 compat）。

## 并发/安全语义

`Coordinator` 嵌入内核锁；Locked 方法要求持有 `Core.Mu`。`TaskService` 只
持有内核（锁 + 只读 Snapshot）与任务状态，不接触其它域；
`CalibratedTokenCounter` 自持 `mu`。

## 扩展与 Review

新增终态工具注册进 `TaskService.terminals`；替换 token 估算实现
`RequestTokenCounter`。Review 重点：锁内不得调用外部端口、transcript 追加
seq 单调、checkpoint 有界、终态判定依赖收敛的 Plan 投影。

## 测试

```text
go test ./application/core/task_context -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### coordinator.go

- `func NewCoordinator(deps Deps) *Coordinator` — NewCoordinator 构造任务域协调器。
- `func (c *Coordinator) semanticProgressLocked(requestID string) (uint64, bool)` — semanticProgressLocked 返回当前任务的语义进展计数（TaskService epoch）；
- `func (c *Coordinator) ActiveSkillIDs() []string` — ActiveSkillIDs 返回当前任务的激活 skill ID 列表（锁内快照）。
- `func (c *Coordinator) GoalSkillActive() bool` — GoalSkillActive 返回 goal skill 可见性投影（lock-free 原子值）。
- `func (c *Coordinator) CurrentTaskExecution() *TaskExecutionState` — CurrentTaskExecution 返回当前任务执行状态（调用方持有 Core.Mu 时读取）。
- `func (c *Coordinator) ActivePlanID() string` — ActivePlanID 返回当前激活 plan 帧 ID。
- `func (c *Coordinator) PlanSequence() uint64` — PlanSequence 返回 plan 帧序列号。
- `func (c *Coordinator) PlanStack() []model.SessionPlanFrame` — PlanStack 返回会话 plan 帧栈。
- `func (c *Coordinator) ClearActivePlanLocked()` — ClearActivePlanLocked 清空激活 plan 帧 ID（plan_clear 路径）。
- `func (c *Coordinator) ResetPlanStateLocked()` — ResetPlanStateLocked 清空 plan 帧状态（新会话/恢复路径；调用方持有
- `func (c *Coordinator) SetPlanStateLocked(stack []model.SessionPlanFrame, activeID string)` — SetPlanStateLocked 装载 plan 帧栈与激活帧（测试/恢复路径；调用方持有
- `func (c *Coordinator) SetResultRefByCallIDLocked(callID, ref string)` — SetResultRefByCallIDLocked 登记 callID → resultRef（测试/恢复路径；调用方
- `func (c *Coordinator) ReplanInFlight(interactionID string) bool` — ReplanInFlight 判定重规划交互是否在途。
- `func (c *Coordinator) MarkReplanInFlight(interactionID string)` — MarkReplanInFlight 登记重规划交互。
- `func (c *Coordinator) DeleteReplanInFlight(interactionID string)` — DeleteReplanInFlight 移除重规划交互标记。
- `func (c *Coordinator) StartReActBudgetLocked(requestID string, budget prompt.ReActBudget)` — StartReActBudgetLocked 启动请求级执行预算（调用方持有 Core.Mu）。
- `func (c *Coordinator) SetReActBudgetExhaustedLocked(requestID, reason string)` — SetReActBudgetExhaustedLocked 直接置位预算终止原因（测试模拟预算耗尽；
- `func (c *Coordinator) ClearReActBudget(requestID string)` — ClearReActBudget 清除请求级预算（自行加锁）。
- `func (c *Coordinator) RecordReActToolCall()` — RecordReActToolCall 累计一次工具调用（自行加锁）。
- `func (c *Coordinator) AllowNextReActIteration(turn int) bool` — AllowNextReActIteration 判定是否允许下一轮模型迭代（自行加锁）。
- `func (c *Coordinator) ReActBudgetError(requestID string) error` — ReActBudgetError 返回预算终止错误（自行加锁；无终止原因 → nil）。
- `func (c *Coordinator) RecordContextControlFailure(requestID string, err error)` — RecordContextControlFailure 把 hook 失败转移给 runChat（自行加锁）。
- `func (c *Coordinator) TakeContextControlFailure(requestID string) error` — TakeContextControlFailure 取走当前请求的 context 控制失败（自行加锁）。
- `func (c *Coordinator) currentTaskService() *TaskService` — currentTaskService 返回当前任务的 TaskService（自行加 RLock，供无锁调用点）。
- `func (c *Coordinator) currentTaskServiceLocked() *TaskService` — currentTaskServiceLocked 返回当前任务的 TaskService；要求调用方持有
- `func (c *Coordinator) RestoreSessionTaskLocked(restored RestoredTaskState)` — RestoreSessionTaskLocked 装载会话恢复的任务/plan 状态（调用方持有
- `func (c *Coordinator) ResetForNewSessionLocked()` — ResetForNewSessionLocked 清空任务/plan 状态（BeginNewSession / 无 record

### plan_transcript.go

- `func PlanNodeStatus(s string) model.NodeStatus` — PlanNodeStatus 将字符串转换为 NodeStatus（queued/running/... 全量映射）。
- `func ActivePlanProjection(plan *model.PlanState, activePlanID string, planSequence uint64) *model.ActivePlanProjection` — ActivePlanProjection 返回 Plan 的只读投影（Completed/Failed/Pending 节点
- `func ActivePlanFrame(stack []model.SessionPlanFrame, activeID string) *model.SessionPlanFrame` — ActivePlanFrame 返回 plan 栈中的激活帧（未找到 → nil）。
- `func ActivePlanFromStack(stack []model.SessionPlanFrame, activeID string) *model.PlanState` — ActivePlanFromStack 返回激活帧的 Plan 深拷贝（未找到 → nil）。
- `func TranscriptTailHistory(events []model.TranscriptEvent, tokenBudget, maxUnits int) []contract.EngineMessage` — TranscriptTailHistory 把 transcript 尾部事件按协议单元收敛为 provider
- `func transcriptEventMessage(event model.TranscriptEvent) contract.EngineMessage`
- `func transcriptProtocolUnits(events []model.TranscriptEvent) [][]model.TranscriptEvent`
- `func transcriptUserUnit(events []model.TranscriptEvent, start int) ([]model.TranscriptEvent, int, bool)`
- `func nextTranscriptUserIndex(events []model.TranscriptEvent, start int) int`
- `func transcriptToolUnit(events []model.TranscriptEvent, start int) ([]model.TranscriptEvent, int, bool)`

### task_context_state.go

- `func (c *Coordinator) ActivateTaskSkillsLocked(state *TaskExecutionState, layers []prompt.PromptLayer)` — ActivateTaskSkillsLocked 把请求级 skill 层投影进任务状态（调用方持有
- `func (c *Coordinator) SyncGoalSkillActiveLocked()` — SyncGoalSkillActiveLocked 把任务级 skill 集投影到 lock-free 可见性值
- `func (c *Coordinator) syncGoalSkillActiveLocked()`
- `func (c *Coordinator) AppendTranscriptEventLocked(event model.TranscriptEvent) model.TranscriptEvent` — AppendTranscriptEventLocked 追加一条 append-only transcript 事件（seq 自增；
- `func (c *Coordinator) ImportEngineHistoryAsTranscriptLocked(history []contract.EngineMessage)` — ImportEngineHistoryAsTranscriptLocked 把引擎既有历史导入 transcript
- `func (c *Coordinator) CountTranscriptEvent(event model.TranscriptEvent) int` — CountTranscriptEvent 估算一条 transcript 事件的 token 数。
- `func (c *Coordinator) RecordLLMComplete(info session.LLMInfo)` — RecordLLMComplete 记录一次 LLM 完成（真实 usage 校准 + assistant 事件；
- `func (c *Coordinator) EnsureToolCallTranscriptLocked(name, fallbackID, arguments string)` — EnsureToolCallTranscriptLocked 保证工具调用宣告已入 transcript（缺失时
- `func (c *Coordinator) RecordToolTranscriptLocked(name, fallbackID, arguments, result string, toolErr error) (string, string)` — RecordToolTranscriptLocked 记录工具结果事件（错误呈现/超限引用；返回
- `func defaultToolResultLimit() int` — defaultToolResultLimit 返回工具结果字符预算（seelex.yaml limits 段
- `func DefaultToolResultLimit() int` — DefaultToolResultLimit 返回工具结果字符预算（导出面；根包兼容包装用）。
- `func (c *Coordinator) StoreToolResultLocked(name, content string) model.StoredToolResult` — StoreToolResultLocked 把超限工具结果以引用形式存储（内容 + 摘要元数据）。
- `func (c *Coordinator) EnsureFinalAssistantTranscript(requestID, content string)` — EnsureFinalAssistantTranscript 在请求结束时补一条可见 assistant 终态事件
- `func (c *Coordinator) TaskProjectionLocked(sessionID string) *model.TaskContextProjection` — TaskProjectionLocked 构建任务的权威投影（会话归档用；调用方持有
- `func (c *Coordinator) BuildTaskCheckpointLocked(state *TaskExecutionState) model.TaskCheckpoint` — BuildTaskCheckpointLocked 构建任务 checkpoint（调用方持有 Core.Mu）。
- `func extendEventRange(existing, current model.EventRange) model.EventRange`
- `func boundedFailure(value string) string`
- `func eventRangeForTask(events []model.TranscriptEvent, taskID string) model.EventRange`
- `func AppendUniqueStrings(values []string, incoming ...string) []string` — AppendUniqueStrings 追加去重后的非空字符串。
- `func (c *Coordinator) ActivePlanProjectionLocked() *model.ActivePlanProjection` — ActivePlanProjectionLocked 返回当前激活 Plan 的只读投影（调用方持有
- `func (c *Coordinator) restoreTaskProjectionLocked(projection *model.TaskContextProjection, fallbackObjective string)`
- `func (c *Coordinator) resolveObjectiveRefLocked(objectiveRef string) string`
- `func (c *Coordinator) RecordContextCompactionLocked(requestID string, compaction model.ContextCompaction) bool` — RecordContextCompactionLocked 记录一次上下文压缩（仅运行中任务；调用方
- `func (c *Coordinator) SetTaskStateLocked(requestID string, status model.TaskStatus, summary string)` — SetTaskStateLocked 把任务可见状态写入快照（调用方持有 Core.Mu）。
- `func (c *Coordinator) InterruptTaskLocked(requestID, summary string)` — InterruptTaskLocked 把任务置为中断（快照 + 内部状态；调用方持有 Core.Mu）。
- `func (c *Coordinator) FailTaskLocked(requestID, summary string)` — FailTaskLocked 把任务置为失败（快照 + 内部状态；调用方持有 Core.Mu）。
- `func (c *Coordinator) ResumeTaskLocked(requestID, summary string)` — ResumeTaskLocked 恢复被压缩/中断的任务（快照 + 内部状态 + epoch 推进；
- `func (c *Coordinator) RememberCheckpointLocked(checkpoint model.TaskCheckpoint)` — RememberCheckpointLocked 按 version 替换或追加 checkpoint（调用方持有
- `func (c *Coordinator) BeginTask(requestID, objective, effort string, previous *TaskExecutionState, checkpoint model.TaskCheckpoint) *TaskExecutionState` — BeginTask 为当前请求创建任务执行状态与 TaskService（调用方持有
- `func (c *Coordinator) ContinuationSummary(requestID string) string` — ContinuationSummary 返回当前任务的恢复摘要（requestID 不匹配 → ""）。
- `func (c *Coordinator) Transcript() []model.TranscriptEvent` — Transcript 返回 append-only 会话事件。
- `func (c *Coordinator) PendingToolResults() []model.StoredToolResult` — PendingToolResults 返回尚未随会话原子提交的工具结果。
- `func (c *Coordinator) TaskCheckpoints() []model.TaskCheckpoint` — TaskCheckpoints 返回任务 checkpoint 序列。
- `func (c *Coordinator) ToolResultRefs() []model.ToolResultRef` — ToolResultRefs 返回工具结果引用表。
- `func (c *Coordinator) ToolResultRefByCallID(callID string) string` — ToolResultRefByCallID 按工具调用 ID 查结果引用（未找到 → ""）。
- `func (c *Coordinator) ResultRefsByCallID() map[string]string` — ResultRefsByCallID 返回 callID → resultRef 全量拷贝（上下文拒绝路径）。
- `func (c *Coordinator) SyncActivePlanFrameLocked(now time.Time)` — SyncActivePlanFrameLocked 把当前快照 Plan 收敛进激活帧（调用方持有
- `func (c *Coordinator) PushLoadedPlanLocked(arguments string, now time.Time)` — PushLoadedPlanLocked 把 plan_load 参数追加为新的激活帧（调用方持有
- `func (c *Coordinator) RemoveCommittedToolResultsLocked(committed []model.StoredToolResult)` — RemoveCommittedToolResultsLocked 清理已随会话快照提交的待定工具结果。
- `func (c *Coordinator) TokenCounterName() string` — TokenCounterName 返回当前 token 计数器标识。
- `func (c *Coordinator) CountRequestTokens(systemPrompt string, history []contract.EngineMessage, currentInput string, tools []model.Tool) int` — CountRequestTokens 估算一次完整请求 token 数。
- `func (c *Coordinator) CountTextTokens(value string) int` — CountTextTokens 估算文本 token 数。
- `func (c *Coordinator) VerifyAndApply(ctx context.Context, kind, argsJSON string) (string, error)` — VerifyAndApply 是终态/打点工具的入口（Registry handler 面）。
- `func (c *Coordinator) FinalizeTask(ctx context.Context, summary ChatEndSummary) error` — FinalizeTask 把自然停止转换为可审计完成/交接（OnChatEnd 入口）。
- `func (c *Coordinator) OnChatEnd(ctx context.Context, summary ChatEndSummary) (model.TaskState, error)` — OnChatEnd 把自然停止转换为可审计完成/交接，返回可见任务状态。
- `func (c *Coordinator) CurrentTaskResumeRecord() TaskResumeRecord` — CurrentTaskResumeRecord 返回当前任务的终态恢复记录。
- `func (c *Coordinator) SetTaskProjectionFlushLocked(flush func(context.Context) error)` — SetTaskProjectionFlushLocked 注入 TaskService 的 Plan 投影 flush 钩子
- `func (c *Coordinator) ObserveTool(observation ToolObservation)` — ObserveTool 记录工具执行观测（调用方持有 Core.Mu）。
- `func (c *Coordinator) ObservePlanEvent(event PlanEvent)` — ObservePlanEvent 记录 plan 事件投影观测（调用方持有 Core.Mu）。
- `func (c *Coordinator) ObserveModelOutput(ctx context.Context, output ModelOutput) error` — ObserveModelOutput 记录模型回复观测（自行加锁）。

### task_execution.go

- `func NewTaskExecutionState(requestID, objective, effort string) *TaskExecutionState` — NewTaskExecutionState 构造一个运行中任务状态。
- `func continuationTaskExecutionState(requestID, objective, effort string, previous *TaskExecutionState, checkpoint model.TaskCheckpoint) *TaskExecutionState`
- `func IsContinuableStatus(status string) bool` — IsContinuableStatus 判定任务状态是否可被续接（排队输入/恢复路径）。
- `func cloneNodeCheckpoints(source map[string]*NodeCheckpoint) map[string]*NodeCheckpoint`
- `func (state *TaskExecutionState) RecordTool(name, result string, toolErr error)` — RecordTool 记录一次工具执行观测（tool 签名去重 + progressEpoch 推进）。
- `func taskToolOutcome(name, result string, toolErr error) string`
- `func (state *TaskExecutionState) Checkpoint(nodeKey, objective, status, output, failure string)` — Checkpoint 记录一个节点打点（节点终态/证据；推进 progressEpoch）。
- `func (state *TaskExecutionState) NodeCheckpoint(nodeKey string) *NodeCheckpoint` — NodeCheckpoint 返回指定节点的打点记录（未找到 → nil）。
- `func (state *TaskExecutionState) EvidenceText() string` — EvidenceText 返回节点打点的证据文本（replan 请求/恢复摘要用）。
- `func (state *TaskExecutionState) ContextSummary() string` — ContextSummary 返回任务的恢复摘要（objective/plan/checkpoint/evidence/
- `func checkpointSummary(checkpoint model.TaskCheckpoint) string`
- `func HasSubstantiveCheckpoint(checkpoint model.TaskCheckpoint) bool` — HasSubstantiveCheckpoint 区分可恢复任务事实与上下文过渡期的仅元数据标记。
- `func appendContextSummary(out *strings.Builder, limit int, value string) bool` — appendContextSummary 保持 checkpoint 在 provider 上下文预算内（只接收
- `func BoundedEvidence(value string) string` — BoundedEvidence 把证据文本截断到 limits.evidence_chars（默认 800）。
- `func ContainsString(values []string, want string) bool` — ContainsString 判断 values 是否包含 want。
- `func CloneTaskCheckpoint(checkpoint model.TaskCheckpoint) model.TaskCheckpoint` — CloneTaskCheckpoint 深拷贝 checkpoint 的 slice 字段。

### task_service.go

- `func (r *planProjectionReader) AllNodes() []string`
- `func (r *planProjectionReader) NodeStatus(nodeID string) model.NodeStatus`
- `func (r *planProjectionReader) PlanStatus() model.PlanStatus`
- `func (r *planProjectionReader) Converged() bool`
- `func (r *planProjectionReader) Flush(ctx context.Context) error`
- `func newTaskService(core *state.Core, taskState *TaskExecutionState, queueRefs func() []string) *TaskService` — newTaskService 构造当前任务的 TaskService。state 为 nil 时表示无活跃任务。
- `func (s *TaskService) SemanticProgress(requestID string) (uint64, bool)` — SemanticProgress 返回任务的语义进展计数（epoch）。
- `func (s *TaskService) ResumeRecord() TaskResumeRecord` — ResumeRecord 返回任务终态时保留的最小恢复记录。
- `func (s *TaskService) ObserveTool(observation ToolObservation)` — ObserveTool 记录一次工具执行观测（tool 签名去重 + progressEpoch 推进）。
- `func (s *TaskService) ObservePlanEvent(event PlanEvent)` — ObservePlanEvent 记录一次 plan 事件投影观测（节点终态 → checkpoint 打点）。
- `func (s *TaskService) ObserveModelOutput(ctx context.Context, output ModelOutput) error` — ObserveModelOutput 记录一次模型回复观测（自然终态判定输入）。
- `func (s *TaskService) OnChatEnd(ctx context.Context, summary ChatEndSummary) (model.TaskState, error)` — OnChatEnd 把自然停止转换为可审计的完成/交接。只消费事件投影与终态标记；
- `func (s *TaskService) VerifyAndApply(ctx context.Context, kind, argsJSON string) (string, error)` — VerifyAndApply 是终态/打点工具的 Registry handler 入口：解析/校验入参 →
- `func (s *TaskService) applyCheckNodeLocked(ctx context.Context, input taskTerminal) (string, error)` — applyCheckNodeLocked 接受 task_check_node：把已加载任务结构中的单个节点
- `func (s *TaskService) applyCompleteLocked(ctx context.Context, input taskTerminal) (string, error)`
- `func (s *TaskService) applyFailedLocked(ctx context.Context, input taskTerminal) (string, error)`
- `func (s *TaskService) applyDecisionLocked(ctx context.Context, input taskTerminal) (string, error)`
- `func (s *TaskService) verifyCompletionLocked(terminal taskTerminal) error`
- `func (s *TaskService) completeAuthoritativePlanLocked() error`
- `func (s *TaskService) rememberResumeLocked(summary ChatEndSummary)` — rememberResumeLocked 在任务终态时保留最小恢复记录。
- `func (s *TaskService) setTaskStateLocked(requestID string, status model.TaskStatus, summary string)` — setTaskStateLocked 把任务可见状态写入快照。
- `func nodesNotCovered(projected, completed []string, projection PlanProjectionReader) []string`
- `func AppendPlanNodeEvent(node *model.PlanNode, event dto.PlanNodeEvent)` — AppendPlanNodeEvent 把一次节点事件追加到节点时间线（详情页数据源；上限
- `func RecalculatePlanProgress(plan *model.PlanState)` — RecalculatePlanProgress 重算计划整体进度（completed/skipped / total）。

### token_counter.go

- `func NewCalibratedTokenCounter() *CalibratedTokenCounter` — NewCalibratedTokenCounter 构造生产默认计数器。
- `func (c *CalibratedTokenCounter) Name() string` — Name 返回计数器标识。
- `func (c *CalibratedTokenCounter) CountText(value string) int` — CountText 估算文本 token 数。
- `func (c *CalibratedTokenCounter) CountMessage(message contract.EngineMessage) int` — CountMessage 估算单条消息 token 数。
- `func (c *CalibratedTokenCounter) CountRequest(systemPrompt string, history []contract.EngineMessage, currentInput string, tools []model.Tool) int` — CountRequest 估算一次完整请求的 token 数（system + history + input +
- `func (c *CalibratedTokenCounter) apply(base int) int` — apply 对基础估算施加修正因子（向上取整；0 保持 0）。
- `func (c *CalibratedTokenCounter) Observe(estimated, actual int)` — Observe 用一次 LLM 调用的真实 usage 校准因子（EMA；clamp 保守区间）。
- `func DefaultContextBudget() ContextBudget` — DefaultContextBudget 返回基于 seelexctx 默认窗口的预算。
- `func ContextBudgetFor(runtime any) ContextBudget` — ContextBudgetFor 返回给定 Runtime 的上下文预算（未实现 contextLimitProvider
- `func newContextBudget(window, outputReserve int) ContextBudget`

### token_counter_test.go

- `func (runtime runtimeWithContextLimits) ContextWindow() int`
- `func (runtime runtimeWithContextLimits) MaxOutputTokens() int`
- `func TestContextBudgetUsesRuntimeLimits(t *testing.T)`
- `func TestContextBudgetFallsBackForLegacyRuntime(t *testing.T)`
- `func TestCalibratedTokenCounter_InitialFactor(t *testing.T)`
- `func TestCalibratedTokenCounter_ObserveAdjustsFactor(t *testing.T)`
- `func TestCalibratedTokenCounter_ObserveClamps(t *testing.T)`
- `func TestCalibratedTokenCounter_ObserveIgnoresNonPositive(t *testing.T)`

