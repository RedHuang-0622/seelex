# application/event

## 生态位

`application/event` 是共享事件协议的 Application 入口。所有类型与 kind 常量都是
[`middle/protocol/event`](../../middle/protocol/event/README.md) 的 identity alias；
投递、过滤、revision/seq 与重放实现仍只有 Middle 一份。主要调用方是
`application/core`。

## 职责与非职责

- 做：暴露 Event、EventKind、Hub、SessionAwareHub、Subscription 和 EventHub；
  转发 `NewEventHub`。
- 不做：不定义第二套事件序号、过滤、丢弃或重放规则。

## 文件结构

- `facade.go`：类型/常量 alias 与 `NewEventHub` 一行转发。
- `facade_test.go`：Event 与 EventHub 的类型身份测试。

## 核心实现

`Event`、`Subscription` 与 `Hub` 保持同身份 alias，因此既有 JSON 字段、方法集与
接口满足关系不变。事件 kind 常量的字符串值逐字来自 Middle。

## 数据流

Application publisher → `application/event.Hub` → Middle EventHub →
GUI/TUI Subscription。没有额外包装层或第二份事件结构。

## 依赖方向

允许依赖 `middle/protocol/event`。禁止依赖 `application/core`、backend 具体实现或
前端包。

## 并发与错误语义

本包无状态；并发、缓冲、filter、delivery_seq、重放与 resync 语义完全由 Middle
EventHub 持有。

## 扩展方式

新增事件 kind 时先在 Middle 协议实现与前端 reducer 契约中定义，再在本 facade 加
identity alias；不得在 Application 写第二份字符串表。

## Review 指南

- 是否仍是 identity alias，而不是复制 struct/interface？
- kind 字符串是否逐字一致？
- revision/seq、过滤和重放是否仍只在 Middle 实现？

## 测试与验证

```text
go test ./application/event -count=1
go test ./middle/protocol/event -count=1
go test ./application/core -run 'Event|Subscribe|Snapshot' -count=1
```

## 文件与函数索引

### `facade.go`

- `type EventKind/Event/Hub/Subscription/... = middleevent.*` — 共享事件协议的同身份 alias。
- `func NewEventHub() *EventHub` — 构造 Middle 的唯一事件投递实现。
