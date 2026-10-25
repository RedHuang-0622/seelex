# 消息编辑与自动 ForkSession 实现方案

## 设计目标

1. 用户消息提供一键复制；只有后端判定可编辑的消息才提供编辑入口。
2. 编辑语义固定为“在该用户消息所属段落的上一段边界 fork”，丢弃原消息及其后续回复。
3. 同一应用调用内完成 child 创建、切换与修改后输入提交，不让前端拼接两个非原子请求。
4. 每次成功的编辑提交创建新 child；同一 `operation_id` 的并发/重放只产生一个 child。
5. Session 血缘只来自 `record.ForkedFrom` / `SessionForkRef`，不按列表顺序或 git graph 猜。
6. 严格保留既有 teammate、WorkItem、teamwork、worktree 与团队动作范围。

## 设计模式选择

| 模式 | 语言实现 | 应用位置 | 理由 |
|---|---|---|---|
| Strategy | Go 纯函数 + 注入的会话端口 | 段落边界、压缩窗口、running/CAS 校验 | 判据可单测，IO 不混入规则 |
| Facade / Use Case | `Service.EditUserMessageAndFork` | `application/core` | 对 GUI 暴露一个原子意图，内部复用 fork 深拷贝 |
| Adapter | Bridge 窄转发 | `gui/bridge.go` | 前端只依赖最小契约 |
| Projection | Snapshot 消息注解 | `application/core` 快照组装 | 前端消费事实，不自行猜可编辑性 |
| State Machine | operation 状态 + loading | 后端 operation registry、前端编辑器 | 处理双击、重放和失败保留 |

## 方案对比

| 维度 | 方案 A：Application 原子用例复用现有 ForkSession | 方案 B：新增存储级 child+message 事务 |
|---|---|---|
| 耦合度 | 低：复用已有 fork 深拷贝与提交链路 | 高：存储层需理解用户编辑与首轮对话语义 |
| 内聚性 | 高：编辑 fork 规则集中在 Application | 中：协议、存储、执行状态跨层 |
| 可测试性 | 高：fake sessions/engine 可覆盖全边界 | 中：需要扩展多后端事务与执行器 |
| 实现成本 | 中 | 高 |
| 改动面 | application、protocol、少量 store 派生字段、GUI | store 所有后端、commit 协议、执行器、恢复 |
| 可回滚性 | 高：仅新增 API/字段与一个用例文件 | 低：持久协议改动大 |
| 半成品风险 | 由应用用例内回滚 child 控制 | 理论最低，但实现复杂且容易引入第二种 fork 语义 |

## 推荐：方案 A

**理由**

- 现有 `PrepareFork` 已实现深拷贝、段落边界、上下文栈、tool-results 与独立快照，新增存储事务会形成第二套 fork 实现。
- 编辑语义可在 Application 层用一次调用编排：先校验，再 fork，再切换，再使用现有 `SubmitToSession` 启动 child 首轮。
- 只要把首次启动失败视作事务失败并回收本次新建 child，就能避免前端可见空分支；正常成功后由 child 自己的持久化链路落新输入和回复。

**最大风险**

现有 `SubmitToSession` 成功代表“回合已被接受并启动”，不代表模型回复完成。因此事务边界定义为“child 与首条用户输入均已被接受”；模型后续失败仍按正常聊天失败留在 child，不回收已经承载新输入的会话。

## 数据契约

```go
type EditUserMessageForkRequest struct {
    TargetEventSeq   uint64 `json:"target_event_seq"`
    NewText          string `json:"new_text"`
    ExpectedRevision uint64 `json:"expected_revision"` // Snapshot.ConversationRevision
    OperationID      string `json:"operation_id,omitempty"`
}

type EditUserMessageForkResult struct {
    ChildSessionID string `json:"child_session_id"`
    ForkPoint      uint64 `json:"fork_point"`
}
```

`ForkRequest` 增加只用于来源展示的字段，不改变切断语义：

```go
SourceEventSeq uint64 `json:"source_event_seq,omitempty"`
SourceMessageID string `json:"source_message_id,omitempty"`
```

`Message` 增加：

```go
EventSeq uint64 `json:"event_seq,omitempty"`
Editable bool `json:"editable,omitempty"`
EditDisabledReason string `json:"edit_disabled_reason,omitempty"`
```

`SessionState` 增加 `ForkedFrom *SessionForkRef`，用于子会话顶部血缘与返回入口。

## 权威判定

### 可编辑

对当前可编辑消息，后端按以下顺序判定：

1. 消息是真实 user 输入，不是 system、内部注入、恢复说明或工具消息。
2. 消息有权威 `EventSeq`，且该事件属于 `store.EventParagraphs` 的完整段落。
3. 父会话不在运行中。
4. `EventSeq >= retainedWindowFirstEventSeq`；该起点由 `ContextRetainedFrom` 下标映射到 `transcript[ContextRetainedFrom].Seq`。
5. 非法情况返回固定、可展示原因，前端不据可见条数推断。

### Fork 点

- 找到包含 `TargetEventSeq` 的段落。
- `cut = 上一段.EventTo`；若目标在首段，则 `cut = 0`。
- 调用现有 `forkSessionLocked(parent, ForkRequest{EventSeq: cut, SourceEventSeq: target.Seq, SourceMessageID: target.MessageID})`。
- 子会话只继承 `Seq <= cut` 的事件，因此不含目标用户消息及其后续回复。

### CAS 与幂等

- 请求携带打开编辑器时的 `Snapshot.ConversationRevision`（父事件流最大 Seq）；
  `ForkRequest.ExpectedEventSeq` 在真正读盘构建 child 时再次比较，避免预览校验与
  快照落盘之间的父会话推进。
- 后端在 fork 完成后、提交 child 前再检查父会话未运行。
- `OperationID` 由前端在打开编辑器时生成，提交重试复用。
- 后端维护短期结果缓存：
  - 同 ID 在飞：等待并返回同一结果；
  - 同 ID 已完成：返回同一 child；
  - 不同 ID：即使同一 parent/EventSeq/newText 也创建新 child。

## 事务与失败处理

1. 解析 parent location，读取事件并定位目标段落。
2. 在父会话 transition lock 下校验 running、revision、压缩窗口和目标消息。
3. 创建 child 快照。
4. 恢复并切换到 child。
5. 使用 `SubmitToSession` 提交修改文本。
6. 成功：登记 operation 结果，返回 child。
7. 首次启动失败：删除本次刚创建的 child，恢复 parent 视图，保留 operation 为可重试（不缓存失败为成功）。

模型在 child 内后续失败不回滚 child；这是正常聊天失败，不是编辑事务失败。

## 实现步骤

| # | 步骤 | 文件 | 设计模式 |
|---|---|---|---|
| 1 | 先写失败测试：复制/编辑动作、不可编辑、原子调用、revision/running 拒绝、双击幂等、同点多分支 | `components.test.mjs`、`session_message_edit_test.go`、`bridge_test.go` | 红灯先行 |
| 2 | 补消息 EventSeq 与派生逻辑 | `model/state.go`、`store/conversation.go`、`store/json_layout.go` | Projection |
| 3 | 补运行态保留窗口读面 | `backend/engine/task` | Strategy |
| 4 | 在快照中生成 editable/原因与 session lineage | `application/core` snapshot files | Projection |
| 5 | 持久化 `forked_from` 并写入来源 EventSeq/MessageID | `session/fork.go`、`SessionContextRecord` | Adapter |
| 6 | 实现原子 `EditUserMessageAndFork` 与 operation 幂等 | `application/core/session_message_edit.go` | Use Case + State Machine |
| 7 | 暴露 Application/Bridge 窄接口 | `application/faces.go`、`gui/bridge.go` | Facade |
| 8 | 用户消息复制/编辑按钮与内联编辑器 | `components.js`、`conversation-view.js` | Progressive UI |
| 9 | App 接线、成功刷新、失败保留与血缘条 | `app.js`、`chat-view.js`、`index.html`、`styles.css` | Projection |
| 10 | 更新模块 README 与工作包实现记录 | 各模块 README、本目录 | 文档同步 |

## 测试策略

### 后端

- 目标用户消息有 EventSeq。
- 压缩前缀消息不可编辑；保留窗口首条可编辑。
- 目标不是 user、缺 EventSeq、父会话运行中、revision 不符均拒绝。
- fork cut 等于目标段落上一段末端。
- child 继承 fork 点之前历史、首条新 user 为新文本、父会话不变。
- child 恢复后只见继承前缀 + 新输入/回复。
- 同一 operation 双击只创建一个 child。
- 同一点先后两个 operation 得到不同 child。
- parent 后续继续聊天不进入 child。
- child 血缘含来源 EventSeq/MessageID。
- 首次提交失败不留下可见 child。

### 前端

- 用户消息始终有复制动作，复制内容来自原始 `content`。
- `editable=false` 时没有可点击编辑动作，并显示原因。
- 编辑器预填原文；取消、重新复制、loading、失败保留文本。
- 成功只调用一次原子 Bridge 方法并刷新。
- 血缘文案只从 `forked_from` 生成；返回父会话入口指向 `parent_session_id`。
- 文本全部 escape；编辑失败不清空 textarea。

### 验证命令

```text
go test ./application/core/... ./backend/engine/session/... -count=1
go test ./gui/... -count=1
go build ./...
node --test gui/frontend/dist/conversation-view.test.mjs
node --test gui/frontend/dist/tree-fork.test.mjs
node --test gui/frontend/dist/*.test.mjs
git diff --check
```

## 回滚方案

- 特性入口集中在新增 API、消息字段与前端动作；若需要回退，移除 Bridge 动作和按钮即可，不改变既有 `ForkSessionLatest`。
- 已有数据新增字段均为可选 JSON，旧快照/旧会话缺字段时按不可编辑或无血缘处理。
- 不自动 commit/push，所有改动保留在工作区供审查。
