# 前置审查报告

## 需求摘要

在右侧项目摘要中显示上下文压缩发生的公开条目，同时不得暴露私有 checkpoint、系统提示词或原始工具记录。

## 影响文件清单

| 文件 | 修改类型 | 原因 |
|---|---|---|
| `application/model/state.go` | 修改 | 为前端快照增加脱敏压缩元数据。 |
| `application/core/context_controller.go` | 修改 | 成功压缩后记录并发布公开条目。 |
| `application/core/task_execution.go` | 修改 | 任务状态切换保留压缩条目。 |
| `gui/frontend/dist/*` | 修改/新增 | 在右侧概要显示条目并测试。 |

## 依赖与风险

- `ContextController` 产生元数据，`Snapshot.Task` 传给 GUI；不新增反向依赖。
- 固定 DTO 不包含 checkpoint、prompt、工具参数、工具结果或对话文本，测试阻止泄露。
- 任务状态更新从请求私有状态复制元数据，避免覆盖。

## 建议方案

公开版本、触发原因、压缩前消息数、估算 token 数和发生时间；私有 checkpoint 保持在 Engine history。
