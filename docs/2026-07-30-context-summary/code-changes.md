# 代码变更摘要

## 修改文件

| 文件 | 类型 | 说明 | 设计方式 |
|---|---|---|---|
| `application/model/state.go` | 修改 | 增加脱敏 `ContextCompaction` DTO。 | 明确 DTO 契约 |
| `application/core/context_controller.go` | 修改 | 成功压缩后写入公开元数据并发布快照。 | 请求私有状态 + 事件 |
| `application/core/task_execution.go` | 修改 | 任务状态更新保留压缩记录。 | 组合状态 |
| `gui/frontend/dist/context-summary.js` | 新增 | 纯渲染器。 | 组合 |
| GUI 侧栏与测试 | 修改/新增 | 在概要中显示条目并验证转义。 | 边界渲染 |

## API 变更

`Snapshot.Task` 新增可选字段 `context_compactions`；字段只包含公开压缩元数据，向后兼容旧客户端。

`Snapshot` 新增可选字段 `read_files`；会话存储新增与 history 同后端的 opaque state sidecar。`SessionArchive` 记录可见会话、Plan、已读文件和有界续接摘要，避免把 provider recovery envelope 当成 UI 历史。

## 验证

- `go test ./application/core ./application/model -count=1 -timeout=180s`
- `node --test gui/frontend/dist/*.test.mjs`

## 本轮上下文加固

- `seelexctx/compactor` 现在保证成功结果的估算 token 不超过调用方预算；预算不足以保留最小安全快照时返回 `ErrBudgetTooSmall`。截断按 UTF-8 rune 边界进行，并深拷贝返回的 snapshot。
- `application/core/context_controller.go` 的工具输出预览保留 tool-call/result 对齐，并以 UTF-8 安全的 head/tail 方式控制 provider history；完整结果仍保留在可见会话记录中以供持久化。
- 新增 `context_hook.go`：上游 LoopHooks 不能返回 error 时，将上下文压缩或历史修复失败传回 `runChat`，停止下一轮请求并向调用方报告失败。

本轮验证：`go test ./application/core ./seelexctx/compactor -count=1`、`go build ./...`、`go vet ./application/core ./seelexctx/compactor`、`git diff --check`。
