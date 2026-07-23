# Application 协议与会话模块详细设计

## 1. 职责与边界

本模块把 Engine、Session、Plugin、Skill 和审批状态转换为前端可消费的单一状态模型。它负责业务状态、会话恢复、输入队列、流式消息和工具生命周期；不依赖 GUI/Wails。

上游依赖通过 `application.Dependencies` 中的 ports 注入。下游只看到：

- `Snapshot()`：权威全量状态；
- `Subscribe()`：有序事件流；
- `Submit/Cancel/Resolve/Switch/LoadMoreHistory`：业务动作；
- DTO：Snapshot、Event、Message、ToolCall、Interaction、RuntimeState。

## 2. 数据契约

### Snapshot

实现位置：`application/state.go:6-23`。

| 字段 | 语义 | 不变量 |
|------|------|-------|
| `protocol_version` | DTO 契约版本 | 当前固定为 1 |
| `revision` | 业务状态修订号 | 每次可观察状态修改后递增 |
| `session/sessions` | 当前会话与持久化列表 | 当前 ID 与 Engine 保持一致 |
| `conversation` | 当前窗口内的消息 | 每个 UI 实体具有稳定 ID |
| `chat` | running、request、queue、error | 同一时刻至多一个运行请求 |
| `runtime` | model/provider/plugin/effort/tools/plan | 以 Core 运行时为准 |
| `interaction` | 当前审批/选择交互 | nil 表示无阻塞交互 |
| `history_*` | 历史分页游标 | `has_more_history == history_offset > 0` |

### Event

实现位置：`application/event.go:10-36`、`application/event.go:81-101`。

```text
protocol_version + seq + revision + request_id + kind + payload
```

- `seq` 描述交付顺序，用于发现订阅丢失；
- `revision` 描述业务状态版本，用于 Snapshot/Event 竞争判断；
- 多个 Event 可以共享 revision，例如一次工具完成同时发布 tool、message 和 runtime；
- 订阅缓冲溢出时清空旧事件并发送 `resync.required`。

## 3. Chat 状态机

实现位置：`application/chat.go:14-82`。

```text
Idle
  │ Submit(text)
  ▼
Running(request_id)
  ├─ chunk ───────────────→ message.delta
  ├─ tool start/complete ─→ tool/runtime/message events
  ├─ Submit(text) ─────────→ input_queue
  ├─ Cancel ───────────────→ context cancellation
  └─ complete/error ───────→ Snapshot refresh, then optional queued batch
```

关键规则：

1. `startChat` 在启动 goroutine 前原子设置 Running，并添加 user/assistant 消息。
2. `appendDelta` 必须同时验证 Running 和 request ID，再按稳定 assistant message ID发布 delta（`application/chat.go:85-102`）。
3. 完成时 Core 从 Engine 历史重建权威 Conversation，避免 UI 增量成为最终事实。
4. 运行中输入进入 `Chat.InputQueue`；当前请求结束后合并为下一次真实输入。

### Skill 输入双通道

实现位置：`application/app.go` 的 `submitConversation`、`application/input.go` 的 `activateSkillAndSubmit`、`application/skill_context.go`。

Chat 请求在 Application 内部拆成两份：

| 字段 | 消费方 | 内容 |
|------|--------|------|
| `displayInput` | Snapshot/Event/GUI | 用户提交的完整原始文本 |
| `modelInput` | Engine `ChatStream` | 活动 Skill 条目 + 完整原始文本；无活动 Skill 时保持原文 |

`#review 检查问题` 会激活 Skill 并立即发起或排队 Chat；`#review` 只激活，后续普通输入自动携带活动 Skill。队列在 Submit 时固化两份输入，因此排队后执行 `#end` 不会改变已排队请求的模型上下文。Engine 历史中的版本化 envelope 会在进入 UI 前解包，GUI 不显示 Skill 指令或内部 marker。

## 4. 工具生命周期

实现位置：

- 工具开始：`application/chat.go:118-138`；
- 工具完成：`application/chat.go:140-181`；
- Hook 配对和唯一 ID：`application/chat.go:353-417`。

ToolHookBridge 为每个 start 分配 `tool-N`，用 `turn + name + arguments` 队列配对 complete。不能继续使用 `name-turn` 作为 ID，因为不同 ChatStream 轮次可能重复，导致前端 keyed DOM 串卡。

完成事件顺序：

1. 更新原始 tool message 状态；
2. 添加 tool_result message；
3. 必要时添加新的空 assistant 接收后续 delta；
4. 刷新 Runtime/Plan；
5. 发布 `tool.completed`、可选 `message.added`、`runtime.changed`。

## 5. 会话恢复与分页

实现位置：

- 恢复：`application/app.go:398-439`；
- 分页：`application/app.go:441-486`；
- Snapshot：`application/app.go:105-111`。

恢复流程先从 SessionPort 读取历史，再调用 Engine `ReplaceHistory`，只有二者成功才替换 Snapshot。这样“能看旧消息”和“下一轮真正使用旧上下文”保持一致。

分页只把更早区间 prepend 到 UI Conversation，不重复替换 Engine 历史。加载的历史消息在持锁区分配新的稳定 message ID，保证后续 keyed render 不依赖数组位置。

## 6. 深拷贝与并发规则

实现位置：`application/state.go:155-197`。

- Snapshot 在 Service 读锁内复制，返回后调用者不能观察内部 slice/pointer 的并发修改。
- ToolCall、Runtime slices、Interaction options 和 Plan 树被深拷贝。
- Plan nodes 递归复制，避免只复制一层 Children。
- Event payload 在释放 Service 锁前准备独立 Runtime 副本，避免 JSON marshal 与状态更新竞争。

锁顺序不允许 EventHub 回调重新持有 Service 锁；Service 先更新/复制/解锁，再 Publish。

## 7. 错误和恢复策略

| 场景 | 行为 |
|------|------|
| 并发启动 Chat | 返回 `ErrChatRunning` |
| request ID 已过期 | 忽略 delta/cancel |
| Session load/replace 失败 | 保持当前会话并返回带上下文错误 |
| Event subscriber 背压 | 发送 `resync.required` |
| 工具失败 | tool_result 使用 error 状态和错误文本 |
| 保存最终会话失败 | Chat 以错误完成并写入 Error message |

## 8. 自动化证据

- `application/application_test.go:195-242`：EventHub resync、protocol version、delta ID。
- `application/application_test.go:299-351`：tool snapshot、唯一 tool ID、分页消息 ID。
- `application/command_test.go:463-478`：默认 Snapshot 协议状态。
- `application/race_test.go`：Chat、tool、Snapshot 和 EventHub 并发路径。

## 9. 审查清单

- Snapshot/Event schema 变更是否更新 `ProtocolVersion` 或保持向后兼容？
- 一个业务修改是否在 bump 后发布了足以更新 GUI 的事件？
- Event payload 是否脱离 Service 内部可变引用？
- 所有 message/tool 是否具备稳定且不冲突的 ID？
- 会话恢复失败时是否保持旧 Engine/Snapshot 一致性？
