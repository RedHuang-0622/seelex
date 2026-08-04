# Application Model

## 定位

`model` 是 Application 与所有前端共享的版本化 DTO 层。这里的字段是进程内 GUI/TUI 协议的一部分，不是随意的内部结构。

## 权威结构

- `Snapshot`：session、conversation、chat、task、runtime、interaction、history window、workspace 和 binding 的完整视图。
- `TaskState`：最近一次请求的可观察结果，只能是 `progressing`、`completed`、`needs_user_decision`、`blocked`、`interrupted` 或 `failed`；不承载模型推理、系统提示词或原始工具日志。
- `SessionState`/`SessionInfo`：`ID` 是唯一操作键，`Name` 是允许重复的显示标题；`SessionState.Draft` 表示尚未生成 ID、不得持久化的待发送会话。
- `WorkspaceInfo`：`ID` 是唯一键，`Name` 默认来自 root basename。
- `Message`/`ToolCall`：前端渲染的消息与工具卡片。
- `RuntimeState`/`PlanState`：模型、Provider、Plugin、Effort、工具和 Plan DAG 的投影；嵌套 `PlanNode` 包含有界生命周期 `events` 和子代理 `tool_events`。
- `SubagentEvent`/`SubagentToolEvent`：前端增量协议；前者携带完整节点及 Plan 进度，后者携带单次子代理工具 started/completed 状态。
- `Interaction`：审批、session/account picker 等等待用户决策的状态。

`ProtocolVersion` 标识不兼容协议版本。`CloneSnapshot` 和 `CloneRuntimeState` 对 slice、map 和嵌套指针做防御性复制。

## Session persistence

`SessionRecord` version 3 is the backend session aggregate: `id`, stable `title`, `plan_stack`, visible `conversation`, `TaskContextProjection`, checkpoint revisions, and `ToolResultRef` metadata are independent records. Provider history is a replaceable execution cache and must never overwrite the stored title, active Plan, task status, or transcript.

`TaskContextProjection` is the restart source for one task. It stores content-addressed active Skills, the canonical Plan reference and node projection, the latest structured `TaskCheckpoint`, and `TokenAudit`. `TranscriptEvent` preserves original user/assistant/tool roles and protocol IDs; oversized content is represented by `result_ref`, while `StoredToolResult.Content` is excluded from JSON and persisted separately.

## 边界

DTO 不执行 IO、不调用 Engine，也不持有锁。它可以引用稳定的桥接值类型，但不应暴露数据库连接、Wails runtime 或可变 backend 对象。
装配后的 system prompt、其文本分层和内部摘要均不属于 DTO；它们只在服务端传给 Engine，不能经 Snapshot/Event 进入前端。

## 兼容性规则

- 新增 optional 字段通常向后兼容；删除、改义或修改 ID/revision 语义需要升级协议并同步前端。
- 修改嵌套 slice/map 时必须更新 clone helper。
- JSON tag 是客户端契约，重命名必须更新 GUI tests 和 docs schemas。

## Review 指南

- 名称是否被误当作索引；恢复/删除/绑定必须继续使用 ID。
- draft 是否保持空 ID，且只在首个真实对话请求到达时物化并用首问生成 Name。
- Snapshot clone 是否仍真正隔离可变数据。
- Plan 节点状态是否覆盖 queued/running/worktree_creating/rebasing/merging/completed/failed/skipped/aborted 生命周期。
- 零值是否对旧客户端安全。

## 测试

```text
go test ./application/core ./gui -count=1
node --test gui/frontend/dist/protocol.test.mjs gui/frontend/dist/client-state.test.mjs
```
