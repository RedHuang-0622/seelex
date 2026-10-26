# application/session/viewstate

## 生态位

`application/session/viewstate` 是共享 View State 的 Application identity facade。
`Coordinator`、`Deps`、`RuntimeStateProjection` 均直接 alias
[`middle/view/state`](../../../middle/view/state/README.md)，构造与 durable conversation
计数只做一行转发。主要调用方是 `application/core`。

## 职责与非职责

- 做：暴露 Middle Coordinator/Projection 类型，转发构造和纯 helper。
- 不做：不复制 Snapshot、message seq、revision、事件发布或 projection 状态机。

## 文件结构

- `facade.go`：类型 alias、`NewCoordinator`、`DurableConversationCount`。
- `facade_test.go`：Coordinator / RuntimeStateProjection 类型身份。

## 核心实现

类型 alias 保持方法集、字段与 revision/seq 语义完全一致。Application 层只改变 import
路径，不增加第二套状态或转换。

## 数据流

core use case → Application viewstate facade → Middle Coordinator → Snapshot/Event；
返回的 `RuntimeStateProjection` 也保持 Middle 同身份。

## 依赖方向

允许依赖 `middle/view/state` 与 `contract/protocol`。禁止依赖 `application/core`、
GUI/TUI 或 backend 实现。

## 并发与错误语义

本包无状态。锁、Core.ViewMu、revision bump、event 发布和投影应用顺序全部由 Middle
Coordinator 持有。

## 扩展方式

新增入口前先确认 Middle Coordinator 已有唯一实现；本包只加 alias/一行转发，不复制判据。

## Review 指南

- 是否仍是 identity alias？
- 是否复制 Snapshot 镜像或 revision 逻辑？
- 是否绕开 Middle Coordinator 直接写字段？

## 测试与验证

```text
go test ./application/session/viewstate -count=1
go test ./middle/view/state -count=1
go test ./application/core -run 'Snapshot|Message|Chat|Runtime|Session' -count=1
```

## 文件与函数索引

### `facade.go`

- `type Coordinator/Deps/RuntimeStateProjection = middlestate.*` — View State 的同身份 alias。
- `func NewCoordinator(deps Deps) *Coordinator` — 转发 Middle 唯一构造。
- `func DurableConversationCount(messages []model.Message) int` — 转发 Middle durable 行计数。
