# core/chat

## 生态位

`application/core/chat` 是聊天流式输出侧的叶子域：流式批次管道
（`StreamBatcher`）与模型推理块剥离（`VisibleOutputStream`）。它是
`core` 根包聊天编排（`chat.go`）的底层工具，不持有任何应用状态。

## 职责与非职责

- 职责：chunk 批次聚合/背压/落库（`lifecycle.BatchPipeline` 适配）；
  `<think>` 推理块在进入前端边界前剥离。
- 非职责：聊天状态机、队列、工具事件投影、Plan 打点——这些仍在 `core`
  根包的 `chat.go`/`tool_hooks.go`/`plan_tools.go`。

## 依赖方向

只依赖 `seelexctx/lifecycle` 与标准库；禁止反向依赖 `core` 根包。

## 测试

```text
go test ./application/core/chat -count=1
```

`stream_batcher_test.go` 覆盖节流、并发、背压与幂等关闭；`visible_output_test.go`
覆盖 `<think>` 剥离；`core` 根包的 `visible_output_test.go` 保留 appendDelta
Service 集成用例。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### stream_batcher.go

- `func NewStreamBatcher(render func([]string), options StreamBatcherOptions) *StreamBatcher`
- `func (b *StreamBatcher) OnChunk(chunk string)`
- `func (b *StreamBatcher) Flush() error`
- `func (b *StreamBatcher) FlushPending() error`
- `func (b *StreamBatcher) Stats() StreamBatcherStats`
- `func (s *streamBatchStorage) Append(ctx context.Context, items []string) error`
- `func (s *streamBatchStorage) ReadRange(context.Context, int, int) ([]string, int, error)`
- `func (s *streamBatchStorage) Count() int`

### stream_batcher_test.go

- `func TestStreamBatcherThrottlesRenders(t *testing.T)` — TestStreamBatcherThrottlesRenders 10k chunks → 渲染调用次数被节流
- `func TestStreamBatcherConcurrentOnChunk(t *testing.T)` — TestStreamBatcherConcurrentOnChunk 并发流式：多 goroutine Push，
- `func TestStreamBatcherBackpressureNoLoss(t *testing.T)` — TestStreamBatcherBackpressureNoLoss 背压：管道缓冲极小（FlushSize=2）+
- `func TestStreamBatcherFlushEmptyIdempotent(t *testing.T)` — TestStreamBatcherFlushEmptyIdempotent 空流收尾：无 chunk 时 Flush 幂等。
- `func newDiscardStorage() *lifecycleMemoryStorage`
- `func (s *lifecycleMemoryStorage) Append(_ context.Context, items []string) error`
- `func (s *lifecycleMemoryStorage) ReadRange(_ context.Context, offset, limit int) ([]string, int, error)`
- `func (s *lifecycleMemoryStorage) Count() int`

### visible_output.go

- `func NewVisibleOutputStream(requestID string) *VisibleOutputStream`
- `func (stream *VisibleOutputStream) RequestID() string` — RequestID 返回该输出流绑定的请求 ID（跨包只读访问）。
- `func (stream *VisibleOutputStream) Consume(chunk string) string`
- `func StripThoughtBlocks(value string) string`
- `func trailingTagPrefix(value, tag string) string`

### visible_output_test.go

- `func TestVisibleOutputStreamSuppressesSplitThoughtBlock(t *testing.T)`

