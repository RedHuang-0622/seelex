# Task

`seelebridge/task` 承载 worktable/task 注册表域：

- `task.go`：`TaskRegistry` actor（mailbox 单消费者串行，按 task 键隔离状态）、
  `TaskRecord`/`TaskSpec`/`TaskStatus`/`TaskTracePoint`/`TaskPhase*` 共享 DTO、
  `TodoItem` 兼容契约（`TaskToTodoItem`/`TodoToTaskStatus`）、
  `TaskKeyForGoal` 幂等键（归一化 goal 哈希）、`SetDefaultBatch` 默认批次
  （startChat 注入 requestID，新建条目自动盖章 `BatchID`）。
- `terminal.go`：`TaskTerminalHandler` 与 `TaskTerminalProvider`
  （`task_complete`/`task_check_node`/`task_failed`/`task_needs_user_decision`
  工具定义，handler 由 application 侧注入）。
- `tools.go`：todo 清单族（`todo_init/todo_add/todo_done/todo_status` 规范名
  + `todolist_*` deprecated 兼容别名）与主动任务 `task_add`（旧 `taskadd`
  别名）的注册与 handler；todo 与 task 共用注册表，条目类型由 `Kind` 权威
  区分（todo 三态状态机，task/plan/subagent 通用迁移）。

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
融合为 kind=todo 的 task；主动 `task_add` 直接登记 kind=task 条目；终态
工具 provider（`task_complete` 等，会话级协议）经 tools 注册表暴露给模型。
新建条目按 `defaultBatchID`（chat 请求批次）盖章，空批次（早期/恢复数据）
归入「早期任务」展示分组。

## 命名与状态机

- 工具命名体现条目类型粒度：`todo_*`（清单项）、`task_add`（主动任务）、
  `task_complete/failed/needs_user_decision`（会话级终态协议）。兼容期
  内 `todolist_*`/`taskadd` 保留为 deprecated 别名（描述标注），
  `main.go defaultManualRules` 新旧名同列。
- `validateTaskTransition(kind, current, next)`：todo 仅允许
  pending/doing/completed 三态；task/plan/subagent 维持终态→retry 重开、
  running/doing 不回退、retry 仅前向的通用迁移。

## 验证

```text
go test ./seelebridge/task -count=1
```
