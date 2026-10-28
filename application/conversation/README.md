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
