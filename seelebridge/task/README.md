# Task

`seelebridge/task` 承载 worktable/task 注册表域：

- `task.go`：`TaskRegistry` actor（mailbox 单消费者串行，按 task 键隔离状态）、
  `TaskRecord`/`TaskSpec`/`TaskStatus`/`TaskTracePoint`/`TaskPhase*` 共享 DTO、
  `TodoItem` 兼容契约（`TaskToTodoItem`/`TodoToTaskStatus`）、
  `TaskKeyForGoal` 幂等键（归一化 goal 哈希）。
- `terminal.go`：`TaskTerminalHandler` 与 `TaskTerminalProvider`
  （`task_complete`/`task_check_node`/`task_failed`/`task_needs_user_decision`
  工具定义，handler 由 application 侧注入）。

依赖方向为根 facade → task；task 不反向依赖 `seelebridge` 根包。
根包 `task_aliases.go` 重导出全部公开类型/常量/辅助函数保持 API 兼容，
`task_facade.go` 保留 *Runtime 门面方法（委托本包实现）。

## 与其它域的关系

```text
node（完成/失败打点）──► task ──► application（worktable 投影）
fork（幂等登记）────────►  │
                            └──► tools（终态工具 provider 注册）
```

task 是 worktable 状态面：node 完成/失败、fork 幂等登记均写 task；todolist
融合为 kind=todo 的 task；终态工具 provider 经 tools 注册表暴露给模型。

## 验证

```text
go test ./seelebridge/task -count=1
```
