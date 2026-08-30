# core/chat

## 生态位

聊天主循环与可见输出集成

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### chat.go

- `func runChatDebug(format string, args ...any)` — runChatDebug 是 SEELEX_TEST_DEBUG=1 门控的临时诊断日志（复跑噪音点时
- `func (service *Service) isActiveSessionLocked(sessionID string) bool` — isActiveSessionLocked 判定指定会话是否为共享快照归属会话（锁内调用；
- `func (service *Service) buildBackgroundMessage(role, content string, tool *ToolCall) Message` — buildBackgroundMessage 为后台会话（非活跃、并行执行）构造可见消息，但不
- `func (service *Service) nextChatRequestIDLocked() string` — nextChatRequestIDLocked 生成跨会话唯一的聊天请求 ID（调用方持有
- `func (service *Service) startChat(parent context.Context, request chatRequest) error`
- `func (service *Service) startChatFor(sessionID string, parent context.Context, request chatRequest) error` — startChatFor 在指定会话启动 ReAct 对话（多会话并行：后台会话不写活跃
- `func (service *Service) runChat(ctx context.Context, sessionID, requestID string, request chatRequest)`
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
- `func (service *Service) flushStreamBatcher(requestID string)`
- `func (service *Service) consumeVisibleChunk(requestID, chunk string) string`
- `func (service *Service) appendVisibleDelta(requestID, chunk string)`
- `func (service *Service) appendHistoryLocked(history []EngineMessage)`
