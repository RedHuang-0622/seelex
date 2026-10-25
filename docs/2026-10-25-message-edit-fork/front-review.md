# 消息编辑与自动 ForkSession 前置审查

## 需求摘要

为当前会话中的可保留用户消息提供复制与编辑入口；编辑后从该消息之前最后一个完整段落边界创建新会话，自动切换到子会话并提交修改后的输入，原会话保持不变。

## 已核实的现状

### ForkSession 与 EventSeq

- `application/core/session_fork.go` 已提供 `ForkSession(parentID, model.ForkRequest)` 和 `ForkSessionLatest(parentID)`。
- `middle/protocol/model/fork.go` 的 `ForkRequest.EventSeq` 表示“继承事件流的最后一个 EventSeq”，含端点。
- `backend/engine/session/fork.go` 的 `resolveForkCut` 要求显式 `EventSeq` 落在 `store.EventParagraphs` 的段落末端；`PrepareFork` 只继承 `Seq <= cut` 的事件。
- 因此“编辑某条用户消息、丢弃该消息及后续回复”可以由“该消息所属段落的上一段 `EventTo`（没有上一段则 0）”作为 fork 点实现，不需要另造截断算法。

### 当前入口粒度

- GUI 当前只有会话列表上的 `ForkSessionLatest`，没有消息级编辑 fork。
- `ForkSessionLatest` 只能 fork 到最新完整段落，不能表达“在某条用户消息之前切断”。
- `application.Service` 已有 `ForkSession`，但没有“fork 后原子提交首条新用户输入”的能力。

### 消息身份与复制

- `gui/frontend/dist/conversation-view.js` 已有 `data-copy` 动作和 `options.copyText` 通道；轨迹、工具等其它视图已有独立复制能力。
- 会话消息当前没有权威 `event_seq`，也没有后端生成的 `editable`/不可编辑原因。
- 实时用户消息创建于 `startChatFor`，持久消息由 `store.derivedConversationMessages` 从 message 事件行派生。两条路径都能关联到事件行，但目前都把该关联在工作区层丢掉了。

### 压缩保留窗口

- `backend/engine/task.TaskExecutionState.ContextRetainedFrom` 是保留窗口在 transcript 事件序列中的**下标**，不是 EventSeq 本身。
- 当 `ContextRetainedFrom > 0` 时，`transcript[ContextRetainedFrom]` 的 `Seq` 才是保留窗口中第一个事件的 EventSeq。
- GUI 不能按已加载消息数或 `limits.history_window` 推断可编辑性。

### Fork 血缘

- `model.SessionForkRef` 已含父会话、父 generation、fork point 等字段。
- 子会话创建时会把 `ForkedFrom` 交给存储层的 `ForkStacks`，但当前 v8 派生 record 没有把完整血缘带回 UI 快照。
- 本次需要补充：编辑源事件的 EventSeq/MessageID、父会话快照可见状态，以及返回父会话入口所需信息。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `middle/protocol/model/state.go` | 修改 | `Message`、`SessionState`、`Snapshot` | 下发权威 EventSeq、editable、禁用原因与当前会话血缘 |
| `middle/protocol/model/fork.go` | 修改 | `ForkRequest`、`SessionForkRef` | 携带编辑源消息定位；新增原子编辑 fork 请求/结果 DTO |
| `store/conversation.go` | 修改 | `ConversationMessage` | 从事件行派生消息时保留 EventSeq |
| `store/json_layout.go` | 修改 | `derivedConversationMessages` | 只把属于完整段落的真实用户事件标为可定位 |
| `backend/engine/session/archive.go` | 修改 | `RecordConversation`、`conversationMessagesFromEvents` | 深拷贝并保留消息级编辑元数据 |
| `backend/engine/session/fork.go` | 修改 | `PrepareFork` | 写入来源消息 EventSeq/MessageID，补充可持久化血缘 |
| `backend/engine/session/session_context.go`（如类型合适） | 修改 | `SessionContextRecord` | 持久化 `forked_from`，供冷恢复与 UI 展示 |
| `backend/engine/task/task_context_state.go` | 修改 | 新增保留窗口读面 | 供应用层按 `ContextRetainedFrom` 生成权威可编辑性 |
| `application/core/session_fork.go` | 修改 | fork 内部复用点 | 支持原子编辑 fork，不在前端拼接两次调用 |
| `application/core/session_message_edit.go` | 新增 | 原子用例 | 校验 EventSeq、段落、压缩窗口、运行态、CAS 与 operation id 幂等 |
| `application/core/session_message_edit_test.go` | 新增 | 用例 | 覆盖复制/编辑元数据、拒因、双击幂等、同点多分支、父子隔离 |
| `application/core/snapshot` 相关组装文件 | 修改 | 当前/会话快照 | 为消息补 `editable`/原因，为当前会话补 `forked_from` |
| `application/faces.go` | 修改 | `ViewFace`/`CommandFace` | 增加原子编辑 fork API |
| `gui/bridge.go` | 修改 | Application 窄接口与 Bridge 方法 | 暴露 `EditUserMessageAndFork` |
| `gui/bridge_test.go` | 修改 | 桥转发/错误/幂等用例 | 钉住 GUI 只发一次原子调用 |
| `gui/frontend/dist/components.js` | 修改 | `renderMessage` | 用户消息增加复制、编辑动作与禁用原因 |
| `gui/frontend/dist/conversation-view.js` | 修改 | 动作委托与编辑器 | 预填原文、复制、取消、loading、失败保留文本 |
| `gui/frontend/dist/chat-view.js` | 修改 | 渲染参数 | 将当前 revision 传给编辑动作用于 CAS |
| `gui/frontend/dist/app.js` | 修改 | invoke 接线、血缘条 | 成功后刷新并切换子会话；提供返回父会话入口 |
| `gui/frontend/dist/index.html` | 修改 | `conversation-shell` | 增加顶部血缘提示宿主 |
| `gui/frontend/dist/styles.css` | 修改 | 消息动作与编辑面板 | hover/focus 显示、移动端常显、键盘焦点可见 |
| `gui/frontend/dist/tree-fork.js` | 修改 | 新增纯函数 | 从显式 `forked_from` 生成血缘文案，不借用 git commit graph |
| `gui/frontend/dist/components.test.mjs` | 修改 | 用户消息动作 | 复制始终有、编辑仅可编辑消息有 |
| `gui/frontend/dist/tree-fork.test.mjs` | 修改 | 血缘纯函数 | 父会话来源只来自 `forked_from` |
| `gui/frontend/dist/branch-origin.test.mjs` | 新增 | 血缘 UI 模型 | 子会话显示父会话并可返回 |
| `application/core/README-session.md` | 修改 | 文件索引/生命周期/验证 | 同步新应用用例 |
| `backend/engine/session/README.md` | 修改 | fork 与消息元数据 | 同步实现事实 |
| `gui/README.md` | 修改 | Bridge/交互说明 | 同步新入口与交互边界 |
| `docs/gui/schemas/session-snapshot.schema.json` | 修改 | Session Snapshot | 同步 `conversation_revision` 与消息编辑字段 |
| `docs/gui/modules/application-protocol.md` | 修改 | Snapshot 数据契约 | 说明 CAS 与可编辑性权威来源 |

## 依赖分析

- 上游：`store` 的 message 事件行是 EventSeq 的事实源；`backend/engine/task` 的 `ContextRetainedFrom` 是压缩保留窗口事实源。
- 中游：`application/core` 负责把消息、窗口、运行态、CAS、fork 与提交编排成一个用例；GUI 只消费能力。
- 下游：Bridge 新增一个原子方法；renderer 只发送 `target_event_seq`、`new_text`、`expected_revision`、`operation_id`。
- 不允许的依赖：frontend 不能读取或推断后端压缩栈；不能先调用 `ForkSession` 再调用 `Submit`。

## 循环依赖检查

- [x] 新增应用用例依赖现有 `application/core`、`session_runtime`、`task` 与 `store`，不反向引入 GUI。
- [x] Bridge 仍只依赖 `application.Service` 的窄接口。
- [x] 前端只依赖 Snapshot DTO 与单一 Bridge 方法。
- [x] 不修改 `backend/multiagent/teamwork`、teammate 工具/详情、WorkItem、上限、worktree、`team_accept/team_close`。

## 风险预估

- **父会话并发推进**：中高，严重。编辑期间父会话若开始新回合，必须依靠运行态门和 expected revision/CAS 拒绝，不得生成基于漂移快照的分支。
- **已有 fork 落盘后 Submit 失败**：中，严重。需要在应用用例内把“创建子会话 + 切换 + 首条输入提交”视为一个事务；启动失败时回收本次新建 child 并回到 parent。
- **压缩窗口与 EventSeq 坐标系混淆**：中，高。`ContextRetainedFrom` 是下标，需要先映射到对应事件的 EventSeq，不能直接比较下标和 Seq。
- **双击重复 child**：中，高。后端按 `operation_id` 做幂等；前端 loading 只作为第二道防线。
- **旧会话缺 EventSeq**：高，中。消息不可编辑并给出明确原因，不按位置猜。
- **血缘持久化缺口**：中，高。仅存在内存的 `record.ForkedFrom` 不足以重启后展示，需要随会话 context 元数据持久化。
- **DOM 重绘丢失编辑文本**：中，中。conversation view 需要自己保存编辑草稿并在 reconcile 后重新挂载。

## 建议方案

1. 在 message 事件行派生与会话快照之间补齐权威 `event_seq`、`editable`、`edit_disabled_reason`。
2. 由后端按 `target_event_seq` 找到所在段落，取上一段 `EventTo` 作为 fork cut。
3. 新增 `EditUserMessageAndFork` 原子应用/Bridge 方法，内部复用现有 fork 深拷贝规则，父会话运行中或 revision 失配时直接拒绝。
4. 子会话创建后自动切换并提交修改文本；仅当启动失败时回滚本次 child。
5. 前端复用现有 `copyText`，新增消息内编辑器、loading 与失败保留；血缘 UI 只消费 `forked_from`。

## 验收前置结论

- [x] 现有 `ForkSession` 能按 EventSeq 切断，并要求完整段落边界。
- [x] 当前 GUI 只有列表级 `ForkSessionLatest` 入口。
- [x] 可以复用现有 copy 通道，不新增剪贴板实现。
- [x] 可以复用现有会话深拷贝和血缘结构，不需要引用式历史。
