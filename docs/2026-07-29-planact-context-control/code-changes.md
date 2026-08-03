# 代码变更摘要

## 已实现

| 位置 | 变更 | 边界 |
|---|---|---|
| `application/core/task_execution.go` | 请求私有终态、NodeCheckpoint 和终态 payload 校验 | Application 持有状态，不写入 Runtime 全局状态 |
| `application/core/context_controller.go` | token/大工具结果触发的历史压缩与 marker 清理 | 不持久化内部控制消息，不记录思维链 |
| `application/core/react_budget.go` | 连续无进展作为最终熔断条件 | 不使用 wall-clock timeout |
| `main.go` | 注册无副作用的 `task_complete` / `task_failed` | Runtime 仅提供工具入口，状态仍在 Application |
| `internal/promptassets` | 完成/失败协议与 deterministic harness | 正例、反例、兜底和自检均在提示词资产中 |

## 验证目标

- 终态工具拒绝缺失 `summary` 或 `failure_type` 的 payload。
- checkpoint 会裁剪长工具输出，并在保存前移除内部 marker。
- 连续无进展工具轮会请求收敛；正常有进展的长任务不受 wall-clock 限制。
- 终态/authority/context 内部消息不进入 frontend snapshot 或持久化 history。
# Service refactor note (2026-07-29)

`application/core/service.go` now owns only shared Service state and
construction. Input, interaction, snapshot, prompt, workspace, and session
history use cases were moved into focused files without public API changes.
