# application/execution/taskcontext

## 生态位

`application/execution/taskcontext` 是 Engine Task Context 的 Application-facing
identity facade 与终态工具编排。它服务 core 的 Task execution、transcript tail、plan/tool observation
与 session context 装配；真实状态、ledger 和裁剪判据仍在
[`kernel/task`](../../../kernel/task/README.md)。

## 职责与非职责

- 做：re-export Task Coordinator/Deps/Prompt 与状态类型，逐项转发纯 helper 和构造；
  统一 Task terminal handler 的 Plan 基线、verify/apply 和成功镜像顺序。
- 不做：不拥有 Task 状态、tool observation、transcript、checkpoint 或 ledger 判据。

## 文件结构

- `facade.go`：类型/常量 alias、NewCoordinator 与 helper forwarders。
- `terminal_handler.go`：`TerminalHandlerApplication`，只编排宿主回调和
  `VerifyAndApply`，不复制 Task 状态机。
- `facade_test.go`：Coordinator / ContextBudget 类型身份。

## 核心实现

Core-facing types are identity aliases. `TranscriptTailHistory`、`LedgerIdentity`、
`ActivePlanFromStack` 等只是转发，裁剪、去重与 plan 判据不进入 Application。

## 数据流

core use case → Application taskcontext facade → Engine Task Coordinator/helper →
Task facts/transcript projections.

Runtime terminal tool → `TerminalHandlerApplication.Handler` → core Plan/Task
镜像回调 → Engine Task `VerifyAndApply`。

## 依赖方向

允许依赖 `kernel/task`、`contract`、`contract/dto`、`contract/protocol` 与标准库。
禁止依赖 `application/assembly`、GUI/TUI 或 store 实现。

## 并发与错误语义

本包无状态；锁、requestID、transcript seq、checkpoint 与 tool observation 语义完全由
Engine Task 持有。

## 扩展方式

只有真实 core 调用且 Engine Task 已有唯一实现时才新增 alias/forwarder。不得在这里
写第二套 Task 状态机。

## Review 指南

- 类型/constant 是否仍为 identity alias？
- transcript tail、ledger identity、plan stack 是否有第二份实现？
- 是否把 Task Coordinator 状态搬入 Application？

## 测试与验证

```text
go test ./application/execution/taskcontext -count=1
go test ./kernel/task -count=1
go test ./application/assembly -run 'Task|Plan|Tool|History|Session' -count=1
```

## 文件与函数索引

### `facade.go`

- `type Coordinator/Deps/ChatEndSummary/... = taskcontext.*` — Task Context 的同身份 alias。
- `func ActivePlanFromStack/TranscriptTailHistory/LedgerIdentity/...(...)` — 逐项转发 Engine helper。
