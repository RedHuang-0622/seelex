# 代码变更摘要

## 新增/修改/删除文件

| 文件 | 类型 | 说明 | 设计模式 |
|---|---|---|---|
| `application/core/session_message_edit.go` | 新增 | 原子编辑 fork 用例、目标段落解析、压缩窗口/CAS 校验、operation 幂等、失败回滚、消息编辑注解、血缘恢复 | Use Case + State Machine |
| `application/core/session_message_edit_test.go` | 新增 | 最近/较早消息编辑、压缩拒绝、running/stale revision 拒绝、双击幂等、同点多分支、父子隔离、回滚 | 回归测试 |
| `middle/protocol/model/fork.go` | 修改 | 增加来源 EventSeq/MessageID、CAS EventSeq、原子编辑请求/结果 DTO | DTO |
| `middle/protocol/model/state.go` | 修改 | 增加消息 EventSeq/editable/原因、会话血缘、ConversationRevision 与深拷贝 | Projection |
| `backend/engine/session/fork.go` | 修改 | fork 时校验事件 revision、持久化血缘、按 MessageID 严格截断旧 record | Strategy |
| `backend/engine/task/task_context_state.go` | 修改 | 提供保留窗口首事件 Seq 与会话最新事件 Seq 读面 | Projection |
| `store/conversation.go` / `store/json_layout.go` | 修改 | 完整段落内的 message 事件行向 conversation 投影 EventSeq | Projection |
| `store/session_context.go` | 修改 | 以不透明 RawMessage 持久化 `forked_from` | Adapter |
| `application/core` snapshot/history/scope 文件 | 修改 | 在快照中生成可编辑判据、恢复血缘、下发 ConversationRevision | Projection |
| `application/faces.go` / `gui/bridge.go` | 修改 | 暴露单一 `EditUserMessageAndFork` 原子能力 | Facade |
| `gui/frontend/dist/components.js` | 修改 | 用户消息复制/编辑动作与原文本 copy payload | Renderer |
| `gui/frontend/dist/conversation-view.js` | 修改 | 内联编辑器、loading、失败保留、稳定 operationId | State Machine |
| `gui/frontend/dist/app.js` / `chat-view.js` | 修改 | 单一原子 invoke、CAS revision、血缘条与返回父会话 | Wiring |
| `gui/frontend/dist/branch-origin.js` | 新增 | 仅从 `forked_from` 生成血缘 UI 模型 | Projection |
| `gui/frontend/dist/tree-fork.js` | 修改 | 增加 session 血缘纯函数，不复用 git commit graph 语义 | Strategy |
| `gui/frontend/dist/index.html` / `styles.css` | 修改 | 顶部血缘宿主、hover/focus/移动端动作与编辑器样式 | View |
| 前端/后端测试文件 | 修改/新增 | 复制、编辑、快照、Bridge、CAS、幂等、隔离、血缘 | 回归测试 |
| 模块 README | 修改 | 同步 API、数据流、持久化与验证命令 | 文档 |

## API 变更

| API | 变更 | 兼容性 |
|---|---|---|
| `Service.EditUserMessageAndFork` | 新增原子编辑分叉调用 | 新增 |
| `Bridge.EditUserMessageAndFork` | 新增 Wails RPC | 新增 |
| `model.ForkRequest` | 增加 source 与 expected EventSeq | JSON 字段可选，兼容 |
| `model.Message` | 增加 `event_seq`、`editable`、`edit_disabled_reason` | JSON 字段可选，兼容 |
| `model.Snapshot` / `SessionSnapshot` | 增加 `conversation_revision` | JSON 字段新增，兼容 |
| `model.SessionState` | 增加 `forked_from` | JSON 字段可选，兼容 |
| `store.SessionContextRecord` | 增加不透明 `forked_from` | schema 版本不变，旧记录可读 |

## 复用与单一实现证据

- 段落解析复用 `store.EventParagraphs` / `store.ParagraphEnd`，未新增第二套段落算法。
- fork 深拷贝继续走 `forkSessionLocked` → `PrepareFork`，编辑入口只负责选择 cut 与编排。
- 首条新输入复用 `submitConversationFor`，没有第二套聊天启动链路。
- 文本复制复用 conversation view 既有 `copyText`、`data-copy` 与 payload Map。
- 可编辑性唯一入口为 `annotateConversationForEditLocked`，前端只读字段。
- 血缘展示唯一输入为 `forked_from`；没有从列表顺序、标题或 git graph 推导。

## 删除的重复实现

本任务未删除既有重复实现。新增逻辑没有复制 fork、段落、聊天启动或剪贴板实现；通过上述现有入口复用，编译期接口断言钉住 Application/Bridge 能力面。

## Commit

未创建 commit（用户要求不自动 commit/push）。
