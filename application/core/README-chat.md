# core/chat

## 生态位

聊天主循环与可见输出集成

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### chat.go

- `func runChatDebug(format string, args ...any)` — runChatDebug 是 SEELEX_TEST_DEBUG=1 门控的临时诊断日志（复跑噪音点时
- `func (service *Service) isActiveSessionLocked(sessionID string) bool` — isActiveSessionLocked 判定指定会话是否为共享快照归属会话（锁内调用；
- `func (service *Service) nextChatRequestIDLocked() string` — nextChatRequestIDLocked 生成跨会话唯一的聊天请求 ID（调用方持有
- `func (service *Service) startChat(parent context.Context, request chatRequest) error`
- `func (service *Service) startChatFor(sessionID string, parent context.Context, request chatRequest) error` — startChatFor 在指定会话启动 ReAct 对话（多会话并行：后台会话不写活跃
- `func (service *Service) runChat(ctx context.Context, sessionID, requestID string, request chatRequest)` — runChat 在独立 goroutine 中执行一次会话提交：委托 Seele loop（9.2 边界，
- `func (service *Service) recordUnhandledTaskErrorLocked(requestID string, err error)`
- `func (service *Service) finalizeReActBudget(ctx context.Context, requestID string) error` — finalizeReActBudget 在工具预算耗尽后保留一次纯文本交付回合。常规循环在
- `func queuedInputRefs(queue []chatRequest) []string` — queuedInputRefs 取排队输入的最小引用（displayInput），供任务终态恢复记录
- `func (service *Service) TaskTerminalHandler(kind string) func(context.Context, string) (string, error)` — TaskTerminalHandler 返回面向 Runtime 的终态工具 handler，同时把请求状态
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
- `func (service *Service) attachLatestReasoning(sessionID, requestID string)` — attachLatestReasoning 在聊天回合结束后，把引擎历史中最后一次 assistant
- `func (service *Service) appendHistoryLocked(history []EngineMessage)`
- `func (service *Service) appendHistoryLockedFor(sessionID string, history []EngineMessage)` — appendHistoryLockedFor 把引擎历史追加为指定会话的可见消息（冷加载无

### chat_delegation_test.go

- `func (engine *singleLoopEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)`
- `func (engine *singleLoopEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)` — ChatStreamFor 显式转发到自身 ChatStream（会话路由面下仍单次提交）。
- `func TestLoopDelegatedToSeele(t *testing.T)` — TestLoopDelegatedToSeele（UC6）：core 无自编 ReAct 循环——一次提交只

### chat_state_event_test.go

- `func TestChatStateIsPublishedNotInferred(t *testing.T)` — TestChatStateIsPublishedNotInferred 验证运行态由后端下发：提交后收到
