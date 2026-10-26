# application/session/switchpolicy

## 生态位

`application/session/switchpolicy`（包名 `switching`）是 Middle Switch 纯策略的
Application-facing identity facade。它服务 `application/core` 的 queue、draft、
interaction、release、fork、unload、meta 与 pending-tail 调用；真实判据仍在
[`middle/switch`](../../../middle/switch/README.md)。

## 职责与非职责

- 做：re-export 纯类型、常量、错误值，逐项转发 Middle 纯函数。
- 不做：不复制队列、草稿、交互、释放窗口、fork gate 或恢复判据。

## 文件结构

- `facade.go`：类型/常量/error alias 与纯函数转发。
- `facade_test.go`：关键类型与错误身份。

## 核心实现

所有类型均为 identity alias；错误变量指向 Middle 同一 error 值。函数只转发参数和
返回值，不改变 trim、顺序、选项 ID 或恢复文案。

## 数据流

core use case → Application switchpolicy facade → Middle switch 纯函数。
没有新的状态、缓存或转换层。

## 依赖方向

允许依赖 `middle/switch`、`contract/dto`、`contract/protocol` 与标准库。禁止依赖
`application/core`、GUI/TUI、backend 或 store 实现。

## 并发与错误语义

本包无状态；并发和错误语义完全由 Middle 纯函数决定。identity alias 保证错误可用
`errors.Is` 与旧路径互认。

## 扩展方式

只有 core 真实调用且 Middle 已有唯一实现时才新增 alias/forwarder。禁止在本包复制
session usecase 或状态机。

## Review 指南

- 类型/错误是否仍为 identity alias？
- 是否改变 trim、排序、选项 ID 或错误文案？
- 是否将 SessionUnit/View 等完整状态引入本包？

## 测试与验证

```text
go test ./application/session/switchpolicy -count=1
go test ./middle/switch -count=1
go test ./application/core -run 'Queue|Draft|Interaction|Release|Fork|Unload|PendingTail'
```

## 文件与函数索引

### `facade.go`

- `type BusyState/DraftTarget/ForkGate/... = middleswitch.*` — Switch 策略类型同身份 alias。
- `var ErrQueue... = middleswitch.ErrQueue...` — 队列错误值同身份兼容面。
- `func QueueTargetSession/.../BuildAccountInteraction(...)` — 逐项转发 Middle 纯判据。
