# application/session/runtime

## 生态位

`application/session/runtime` 是 Engine Session 的 Application-facing identity facade。
它服务 core 的 session catalog/record/binding/cold/fork/history 路径；真实 durable
session 状态与恢复策略仍在 [`kernel/session`](../../../kernel/session/README.md)。

## 职责与非职责

- 做：expose Coordinator/Deps/Location、SessionContext/Fork/Transcript/Range ports，
  并转发 title/workspace/resume/fork helper。
- 不做：不复制 session record、catalog、project binding、恢复或存储策略。

## 文件结构

- `facade.go`：类型/var alias、NewCoordinator 与 helper forwarders。
- `facade_test.go`：Coordinator / Location 类型身份。

## 核心实现

所有类型均为 identity alias；`SessionArchiveResumePrefix` 指向 Backend 同一变量。
Application 只改变 import 路径，不改变 durable layout 或恢复语义。

## 数据流

core use case → Application session runtime facade → Engine Session Coordinator →
session records/binding/transcript facts。

## 依赖方向

允许依赖 `kernel/session`、`contract` 与 `contract/protocol`。禁止依赖
`application/core`、GUI/TUI 或具体 store。

## 并发与错误语义

锁、catalog refresh、project binding、fork/release 与恢复顺序全部由 Engine Session
持有；本包无状态。

## 扩展方式

只有 core 真实调用且 Engine Session 已有唯一实现时才新增 alias/forwarder；禁止在本包
复制 session state。

## Review 指南

- 类型/变量是否仍为 identity alias？
- 是否复制 session catalog、binding 或 cold recovery 判据？
- 是否改变 durable record、seq 或 fork 语义？

## 测试与验证

```text
go test ./application/session/runtime -count=1
go test ./kernel/session -count=1
go test ./application/core -run 'Session|Cold|Fork|History|Reference|Chat' -count=1
```

## 文件与函数索引

### `facade.go`

- `type Coordinator/Deps/Location/Session*Port = sessionruntime.*` — Engine Session 的同身份 alias。
- `func NewCoordinator/SessionTitle/RecordResumeHistory/CloneSessionPlanStack(...)` — 逐项转发 Engine helper。
