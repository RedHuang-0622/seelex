# Context Lifecycle

## 模块定位

`lifecycle` 是与具体消息类型无关的上下文生命周期基建。它为冷加载、滑动窗口和流式批处理提供泛型 actor/管道，供 Application 等上层组合；它不理解 Session、Plan、Provider 或前端 DTO。

## 职责与边界

- `ContextActor[T]` 串行处理 append、window load 和 snapshot，请求只能通过有界 mailbox 进入。
- `BatchPipeline[T]` 聚合写入，按 `FlushSize` 或 `Interval` 调用 `Storage.Append`。
- `Storage[T]` 是调用方拥有的冷存储接口；本包只提供测试用内存/丢弃实现。
- 本包不选择数据库、不持久化 Application canonical conversation，也不逐 chunk 保存业务记录。

## 文件结构

| 文件 | 职责 |
|---|---|
| `actor.go` | 生命周期策略、Context actor、窗口读写和关闭协议。 |
| `pipeline.go` | 有界批处理、背压、flush/close 和统计。 |
| `storage.go` | `Storage[T]` 的测试实现。 |
| `mock_bench_test.go` | 并发、背压、边界、内存策略和 race 验证。 |

## 数据流与并发语义

`Append/Push` 成功表示所有权已转移：关闭必须排空已接受数据。Actor 和 Pipeline 都使用生命周期门协调投递与 channel close，避免 send-on-closed 和“已入队但无人回复”。Pipeline 的 `Flush` 等待自身 `committed` 数达到调用时的 accepted target，不依赖底层 Storage 构造前已有多少数据。

背压是显式错误：mailbox 满返回 `ErrMailboxFull`，pipeline 满返回 `ErrPipelineFull`。流式调用方应退避重试；关闭后返回 closed error。Storage append 失败会保留 pipeline buffer 并把错误返回给 Flush/Close。

## 扩展与 Review

新增策略应只修改 actor 私有状态机，不把锁或业务类型暴露到调用面。新增 Storage 必须并发安全，并保持 `Append`、`ReadRange`、`Count` 的一致计数语义。

Review 重点：Push/Close、Enqueue/Close、Flush/Close 是否可并发终止；成功接收的数据是否恰好提交一次；失败 flush 是否会静默丢数据；窗口策略是否保持常驻内存上限。

## 测试

```text
go test ./seelexctx/lifecycle -count=1
go test -race ./seelexctx/lifecycle -count=1
```

## Cancellation and drain

`ContextActor.CloseContext` and `BatchPipeline.CloseContext` cancel active
storage work before closing admission and draining accepted messages. Every
`Storage.Append` and `Storage.ReadRange` call receives a bounded context.
`SnapshotContext` also observes caller cancellation, so a full mailbox cannot
cause an unbounded spin.
