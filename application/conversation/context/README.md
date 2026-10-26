# application/conversation/context

## 生态位

`application/conversation/context` 承载显式上下文压缩（`/compact` 与
`compact_context`）的应用编排和回执。压缩算法、持久化与门禁判据仍在
`backend/engine/context`。

## 职责与非职责

职责：

- 从 ctx/视图解析当前 Session；
- acquire/release 同会话压缩 gate；
- 调用 Runtime 唯一压缩入口；
- 映射 Done/Unrecorded/Failed/Scheduled/NotNeeded 回执；
- 组装用户可读 note、门禁耗时、区间和失败原因。

非职责：

- 不实现摘要、预算、替换历史或持久化；
- 不拥有 Task、Engine、View 或 EventHub；
- 不导入 `application/core`。

## 文件结构

- `application.go`：`Deps`、`Application`、`Result`、`GateTiming` 与用例。
- `engine_facade.go`：Engine Context Coordinator/History/Deps alias 与 predicate/warning
  一行转发，供 core 装配。

## 依赖方向

允许依赖 backend engine context 的 identity alias/转发面、`middle/protocol/model` 与
标准库。禁止依赖 `application/core`、GUI/TUI 或存储实现。

## 并发与错误

压缩 gate 覆盖压缩、落盘和回执构造；模型调用在本地回合内也不会持有 Session
写锁。错误与五种 outcome 的 note 文案保持迁移前语义。

## 测试与验证

```text
go test ./application/conversation/context -count=1
go test ./application/core -run 'CompactContext' -count=1
go test ./application/... -count=1
```
