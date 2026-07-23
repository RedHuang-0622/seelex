# GUI 客户端稳定性实现方案

## 设计目标

- Snapshot 与 Event 使用显式协议版本。
- 流式 delta 只更新对应消息，不重建全部历史。
- 事件丢失、乱序或未知类型时自动全量重同步。
- 保持用户滚动位置、思考块展开状态和工具 OUT 展开状态。
- 纯 reducer 与 render model 可在无 WebView 环境测试。

## 设计模式选择

| 模式 | 语言实现 | 应用位置 | 理由 |
|------|---------|---------|------|
| Reducer | JS 纯函数 | `protocol.js` | 事件归并可重复测试，不耦合 DOM |
| Adapter | Go DTO/Event | `application` → Wails | 明确客户端契约并保持核心独立 |
| Keyed Reconciliation | JS Map + DOM key | `conversation-view.js` | 复用未变化节点，避免全量 innerHTML |
| Fallback/Resync | seq + Snapshot | `app.js` | 正常路径高效，异常路径保持正确性 |

## 方案对比

| 维度 | 方案 A：Snapshot + keyed DOM | 方案 B：事件 reducer + Snapshot 兜底 |
|------|---------------------------|------------------------------------|
| 网络/Bridge 调用 | 每个事件拉全量 Snapshot | 正常 delta 不调用 Bridge |
| DOM 更新 | 局部 | 局部 |
| 状态一致性 | 简单但传输成本高 | seq 检测，异常自动 resync |
| 可测试性 | 中 | 高，reducer 可纯函数测试 |
| 实现成本 | 低 | 中 |
| 长会话性能 | 中 | 高 |

## 推荐：方案 B

理由：现有 EventHub 已提供 `seq/revision/kind/payload`，补齐消息 ID 和版本即可成为稳定的客户端事件源。最大风险是事件 schema 不完整，因此保留 Snapshot 作为权威恢复路径。

## 循环依赖检查

- `application → Event/Snapshot DTO`，不引用 GUI。
- `app.js → protocol.js + conversation-view.js → components.js`，无反向引用。

## 核心接口

```js
applyEvent(snapshot, event, lastSeq)
// => { snapshot, lastSeq, needsRefresh, error }

createConversationView(container).render(model, { scrollMode })
// model.items => [{ key, html }]
```

## 实现步骤

| # | 步骤 | 文件 | 模式 |
|---|------|------|------|
| 1 | 增加协议版本和 delta message_id | `application/state.go`、`event.go`、`chat.go` | DTO |
| 2 | 增加事件 reducer 与协议校验 | `protocol.js` | Reducer |
| 3 | 输出稳定会话 render model | `components.js` | Presentation Model |
| 4 | 实现 keyed DOM 与滚动/展开保持 | `conversation-view.js` | Reconciliation |
| 5 | app 消费事件，Snapshot 仅兜底 | `app.js` | Fallback |
| 6 | 更新 Go/Node 测试和报告 | tests/docs | Contract Test |

## 测试策略

- Go：协议版本、事件信封、delta 消息 ID、全量 application/gui 测试。
- Node：版本不兼容、seq 缺口、消息新增/delta/tool/runtime/interaction reducer。
- Node：render model 稳定 key、工具配对与 activity key。
- 构建：`go vet ./...` 和 Wails production tags。

## 回滚方案

删除新 JS 模块并恢复 `EventsOn(..., () => refresh())` 即可退回全量 Snapshot；Go 新增字段为向后兼容 JSON 字段，不影响 TUI。
