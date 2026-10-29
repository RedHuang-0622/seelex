## 生态位

`draft` 是上下文 draft 的会话级 actor owner。主要调用方是上下文装配与回合收尾：调用方在 actor 外分配 transcript 同源 `Seq`，actor 是唯一写者。

## 职责

- 保存 append-only 的已发出字节片段。
- 按 `(session, seq)` 合并重复投递，拒绝同序号不同字节。
- 轮尾调用 `Commit`；只有 durable ack 后才推进水位并清空已提交工作区。
- 队列满时背压，绝不丢弃。

## 非职责

- 不分配 `Seq`。
- 不判断回合边界，不选择 memory/tools。
- 不持久化事实；持久化通过 `Durable` 窄端口注入。

## 核心类型

- `Item`：`SessionID/Seq/Bytes/Final`。
- `State`：pending items、已 ack watermark、uncommitted 标记。
- `Durable`：`Load/Save/Commit` 三面持久化端口。
- `Actor`：单会话 FIFO 单写者。

## 数据流

```text
caller alloc(seq) -> Append(seq, sent bytes) -> durable Save -> ack
turn end -> Commit(through seq) -> durable Commit -> ack -> clear work set/advance watermark
restart -> Load(state) -> replay pending items by Seq
```

## 并发与错误语义

- 所有命令经有界 channel 进入单 goroutine。
- `Commit` 失败时工作区保留并标 `Uncommitted`；后续可重试同一水位。
- actor 关闭不清持久状态；新 actor 从 watermark 之后重放。
- `Append/Commit` 的 ctx 取消只取消等待，不丢弃已经提交给持久端口的状态。

## 测试

```text
go test -race ./kernel/context/draft -count=1
```
