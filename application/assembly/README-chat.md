# assembly/chat（根包分卷）

## 生态位

聊天主循环与可见输出集成（含可见会话的群聊角色归属载荷）

覆盖：`chat*.go`、`conversation_turn_*.go` + 显式名单（见生成器 `ROOT_GROUPS`）；未归属文件由覆盖自检拦下。

## Queue application ownership（AP-007 首个切片）

`Conversation queue application` 是排队输入编辑的应用组件。它消费 `SessionUnit` 的窄结构面
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
assembly 只保留一行 wrapper，Engine ReAct loop 仍归 Seele。

## ChatState 单一写入点（AP-008 首个切片）

`chat_turn_state.go` 集中 `ChatState` 的开始、队列投影、错误、收尾和重置。
状态仍存在 `SessionUnit.Chat`；`conversationTurnState` 只拥有转换判据。
`chat_turn_state_test.go` 的 AST 门禁禁止其它生产文件直接调用
`SetChatState/UpdateChat`。

## ChatTurn 请求工厂（AP-009 首个切片）

`chat_turn_request.go` 集中构造 `chatRequest`、恢复队列 payload、合并提升批次、
选择 Skill 层和提取排队引用。`chat.go` 与 `session_scope.go` 不再各自持有这些规则。

## 提交与 turn 起点归属（A5.1）

`application/conversation.SubmissionApplication` 统一 Request/Budget 构造和当前
draft materialization；`TurnStartApplication` 唯一拥有 restoring/compacting
延后、running 入队、恢复队列排空、标题/消息投影、revision 与启动发布顺序。
assembly 的 `submitConversation*` 与 `startChat*` 只保留 host wrapper。

## ChatTurnProcess 起点事务（AP-014 首个切片）

`ChatTurn process application` 的 `conversationTurnProcess.BeginLocked` 统一正常提交与队列
批次提升的起点规则：requestID、Effort 预算回退、cancel/stream、Task 起点与
checkpoint、Skill 激活和 user transcript。Engine 流与收尾持久化仍在主循环。

## ChatTurn 持久化完成事务（AP-018 首个切片）

`ChatTurn completion application` 的 `conversationTurnCompletion.Persist` 承接回合收尾的
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

`ChatTurn input application` 固定 Engine 调用前顺序：激活当前 task system prompt、
调用 provider context 装配、对成功结果做非空输入归一化。context 失败直接返回，
不再归一化或进入 Engine 流。

## ChatTurn Engine 返回事务（AP-048 首个切片）

`ChatTurn response application` 在 `ChatStream` 返回后按固定顺序执行 reasoning attach、
模型输出观测、最终 assistant transcript 补齐和 reasoning 回填。观测错误保持
原有忽略语义；context recovery 与 budget/task finalization 仍在主循环。

## ChatTurn 恢复与终态（AP-049 首个切片）

`ChatTurn recovery application` 集中 context control failure 读取、provider failure
恢复、有界补充回合重试、ReAct budget finalization 和自然停止后的 Task finalize。
恢复/重试错误保持原组合文案；stream flush、route 和持久化仍在 `runChat`。

## ChatTurn 清理与持久化前准备（AP-050 首个切片）

`ChatTurn cleanup application` 固定解锁持久化前的清理顺序：batcher flush、route stamp、
subagent mailbox drain、runtime projection 采集、checkpoint 清理、provider
recovery 清理、未处理任务错误记录和 draft snapshot 停止。成功/失败清理顺序与
错误优先级保持原实现。

## ChatTurn Commit（AP-051 首个切片）

`ChatTurn commit application` 把持久化完成、stale/outcome、队列提升、下一批 user/assistant
消息和 ChatState/Task/revision 镜像串成一次提交。stale 请求在 outcome 后短路；
非 stale 结果直接供 publication/tail 使用。

## ChatTurn Finalize（AP-052 首个切片）

`ChatTurn finalize application` 将 commit result 映射为 `chatTurnPublication` 并调用
异步 tail；stale commit 不发布、不启动下一轮。`runChat` 只保留 Engine 循环与
各组件串联。

## ChatTurn 执行策略同步（AP-053 首个切片）

`ChatTurn execution application` 固定每个回合起点的执行面同步：绑定本 Session project
root、下发 subagent policy、reasoning effort 和 full access。该同步不读取其它
Session 的遗留执行开关。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### chat.go

- `func runChatDebug(format string, args ...any)` — runChatDebug 是 SEELEX_TEST_DEBUG=1 门控的临时诊断日志（复跑噪音点时
- `func (service *Service) isActiveSessionLocked(sessionID string) bool` — isActiveSessionLocked 判定指定会话是否为共享快照归属会话（锁内调用；
- `func (service *Service) nextChatRequestIDLocked() string` — nextChatRequestIDLocked 生成跨会话唯一的聊天请求 ID（调用方持有
- `func (service *Service) ensureViewPlanProjection(ctx context.Context)` — startChat 在**当前视图会话**上启动 ReAct 对话。
- `func (service *Service) mirrorActivePlanAfterTerminal(ctx context.Context)` — mirrorActivePlanAfterTerminal 在 check/complete 终态把协调器投影的最新
- `func (service *Service) mirrorActiveTaskAfterTerminal(ctx context.Context)` — mirrorActiveTaskAfterTerminal 在终态工具落地后把活跃会话的权威任务状态
- `func (service *Service) finalizeTaskExecution(requestID string) error` — finalizeTaskExecution 把自然停止转换为可审计的完成/交接
- `func (service *Service) removeReActBudgetFinalizationInput(sessionID string) error`
- `func closedSignal() chan struct`
- `func (service *Service) markBusyLocked()`
- `func (service *Service) markIdleLocked()`
- `func (service *Service) appendHistoryLocked(history []contract.EngineMessage)`
- `func (service *Service) appendHistoryLockedFor(sessionID string, history []contract.EngineMessage)` — appendHistoryLockedFor 把引擎历史追加为指定会话的可见消息（冷加载无

### chat_turn_request.go

- `func chatRequestDisplays(requests []chatRequest) []string`
- `func queuedInputRefs(queue []chatRequest) []string`
- `func selectedSkillLayers(layers []prompt.PromptLayer) []prompt.PromptLayer`

### chat_turn_state.go

- `func newConversationTurnState() *conversationTurnState`
