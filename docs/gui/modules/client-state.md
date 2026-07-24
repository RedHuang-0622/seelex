# 客户端状态与事件归并模块详细设计

> 状态：已实现（protocol v1）
> 总体架构：[`../architecture.md`](../architecture.md)

## 1. 职责与边界

客户端状态层位于 Wails Bridge 与视图之间：

- `protocol.js` 是无 DOM 的纯协议 reducer；
- `client-state.js` 管理 Snapshot 刷新、事件序列和并发竞争；
- 视图只接收已校验的 Snapshot 与 changed kind。

该层不直接访问 `window.go` 或 DOM，因此可以在 Node test runner 中执行。

## 2. 协议校验

实现位置：`gui/frontend/dist/protocol.js:1-13`。

`validateSnapshot` 检查对象、协议版本和 conversation 数组。协议版本不匹配不会静默刷新，因为同一客户端无法证明能解释新 schema；它返回可见错误。

Event kinds 白名单只包含 reducer 能安全归并的类型。`snapshot.changed`、`resync.required`、`error` 和未来未知类型统一要求 Snapshot refresh。

## 3. Event reducer

实现位置：`gui/frontend/dist/protocol.js:15-97`。

处理顺序：

1. 校验 event 对象和 protocol version；
2. 验证 seq 非零、检测向前缺口；
3. 丢弃重复/乱序旧 seq；
4. 未知 kind 或缺少 Snapshot → refresh；
5. revision 不高于权威 Snapshot floor → 只推进 seq；
6. decode payload；
7. clone 必需的 Snapshot 分支并应用增量；
8. 无法应用 payload → refresh。

### 增量规则

| Event | 规则 |
|-------|------|
| `message.added` | 按 message ID upsert |
| `message.delta` | 按 `message_id` 定位并 append delta |
| `tool.started/completed` | 按 message ID upsert，渲染层再配对 tool ID |
| `runtime.changed` | 替换 Runtime DTO |
| `interaction.opened` | 设置 Interaction |
| `interaction.closed` | 清空 Interaction |

## 4. Snapshot 管理

实现位置：`gui/frontend/dist/client-state.js:3-55`。

### 刷新合并

`refreshQueued + refreshPromise` 保证同一时刻只有一个 Bridge Snapshot 调用链。刷新期间的新请求只合并 scroll mode，并在循环下一轮再拉一次，避免丢失显式操作后的刷新。

滚动模式优先级：

```text
auto < preserve < anchor < bottom
```

### 过期 Snapshot

候选 revision 小于当前客户端 revision 时拒绝，防止慢请求覆盖已消费的新事件。

### revision floor

只有接受权威 Snapshot 时更新 `snapshotRevisionFloor`。同一业务修改可能发布多个相同 revision 的合法事件，因此普通 event reducer 不能把当前 snapshot revision 直接当 floor。

## 5. 典型竞态

### Snapshot 先包含 delta，event 后到

Snapshot revision=4、内容=AB；随后 revision=4 的 delta B 到达。floor=4，因此只推进 seq，内容保持 AB。

### 同 revision 兄弟事件

floor=1；revision=2 先后发送 user message 和 assistant placeholder。两个事件都高于 floor，均被应用，即使第一个应用后客户端 snapshot revision 已是2。

### refresh 期间继续收到 event

event 先提高客户端 revision；慢 Snapshot 返回更低 revision 时被拒绝。若 Snapshot 包含更高状态则接受并更新 floor。

## 6. 错误策略

| 错误 | 行为 |
|------|------|
| Snapshot schema/version 无效 | `onError`，保留当前状态 |
| Event version 无效 | `onError`，不尝试按未知协议刷新 |
| seq 缺口 | 拉 Snapshot |
| payload 缺字段/目标不存在 | 拉 Snapshot |
| 未知 kind | 拉 Snapshot |
| Snapshot 请求失败 | `onError`，刷新循环可接收后续请求重试 |

## 7. 自动化证据

- `protocol.test.mjs:17-92`：版本、add/delta、seq gap、未知事件、旧事件、同 revision、runtime/interaction。
- `client-state.test.mjs:17-88`：无刷新 delta、gap refresh、旧 Snapshot、delta replay。
- 测试不需要 WebView、Wails 或 DOM。

## 8. 审查清单

- 新增 Event kind 是否同时加入白名单、reducer、视图路由和测试？
- payload 无法安全归并时是否回退 Snapshot，而非猜测状态？
- seq 和 revision 是否被用于不同目的？
- Snapshot refresh 是否可能并发覆盖新状态？
- reducer 是否保持纯函数边界，不访问 DOM/Bridge？
