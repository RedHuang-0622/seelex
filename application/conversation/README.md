# application/conversation

## 生态位

`application/conversation` 承载 Conversation Queue 与 ChatState 的应用编排。
`SessionUnit` 仍是队列内容和 ChatState 的唯一状态所有者；`conversation/context`
和 `conversation/search` 为子域包。

## 职责

- 排队输入重排与召回；
- 同一 ViewMu 内的队列投影、ChatState 镜像和 revision 收口；
- 关闭/排空门禁。
- assistant 推理步骤与可见消息的配对回填。
- 单个 ChatTurn 从输入准备到 commit/finalize 的应用编排。

## 非职责

- 不拥有 SessionUnit、Engine 或持久队列；
- Engine ReAct 循环本身仍由 Seele 拥有；`TurnRunner` 只编排 application 组件链。
- 不实现 context compaction 或历史检索。

## 文件结构

- `queue.go`：`QueueUnit`、`QueueDeps`、`QueueApplication`。
- `turn_state.go`：ChatState 转换规则。
- `request.go`：不可变 ChatRequest 与批次合成。
- `turn_input.go`：prompt/context 准备与输入归一。
- `turn_response.go`：模型回复观察与 reasoning 回填。
- `turn_recovery.go`：context recovery、retry 与 Task/budget finalize。
- `turn_cleanup.go`：stream flush、route/mailbox、checkpoint/recovery 清理。
- `turn_process.go`：ChatTurn 起点事务与队列提升。
- `turn_outcome.go`：终态结果、错误和活动 Task 镜像。
- `turn_completion.go`：持久化完成与 consumed queue 结算。
- `turn_commit.go`：persist/outcome/promote/mirror 的锁内串联。
- `turn_finalize.go`：commit result 到 publication。
- `turn_publication.go`：`chat.changed` / snapshot / error / message 事件发布。
- `turn_tail.go`：解锁后的 consumed 标记、下一轮、release 与 tool-record flush。
- `turn_execution.go`：会话执行策略同步。
- `reasoning.go`：历史推理步骤提取、可见步骤配对和 legacy fallback。
- `stream.go`：`StreamOutputPort`、可见输出与 batcher 的消费方契约。
- `session_access.go`：统一会话读面与 `SessionRecordsPort`；分页、UTF-8
  裁剪仍由 Middle 的唯一分页实现拥有。
- `turn_runner.go`：`runChat` 对应的输入→stream→recovery→cleanup→commit→finalize 编排。
- `query/`：ConversationHistoryQuery。
- `context/`：Context Compaction。
- `search/`：history search 工具适配。

## 依赖方向

Conversation 定义 `StreamOutputPort` 与 `SessionRecordsPort`；Wiring 分别注入
`backend/engine/react` 的可见输出/batcher 实现和
`middle/view/sessionaccess.PaginateRecords`。`application/core` 不再 import
engine/react 或 sessionaccess。

## 并发与错误

队列编辑在 `ViewMu` 内修改 SessionUnit，投影后先镜像活跃 ChatState、bump
revision，再解锁发布。关闭/排空错误保持原语义。

## 测试与验证

```text
go test ./application/conversation -count=1
go test ./application/core -run 'Queue|QueueComponent|ReorderQueuedInput|RecallQueuedInput' -count=1
go test ./application/... -count=1
```
