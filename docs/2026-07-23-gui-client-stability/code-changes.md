# GUI 客户端稳定性变更记录

## 结果

GUI 的会话主视图已从“每个事件重新拉取完整 Snapshot 并重建 DOM”改为“事件增量归并 + keyed DOM 局部更新 + Snapshot 异常兜底”。流式输出、工具卡片、思考块和历史滚动不再因为普通事件整体重建。

## 后端协议

| 文件 | 变更 |
|------|------|
| `application/state.go` | Snapshot 增加 `protocol_version`；抽取 Runtime 深拷贝，递归复制 Plan 节点，避免事件序列化读取可变状态 |
| `application/event.go` | Event 增加 `protocol_version`；`message.delta` 使用带 `message_id` 的结构化 payload |
| `application/chat.go` | delta 定位稳定消息 ID；工具开始/完成同步 Runtime；工具桥为每次调用分配唯一 ID，避免跨轮 key 冲突 |
| `application/app.go` | 新建 Snapshot 初始化协议版本；分页加载的历史消息补稳定 ID |

## 前端客户端

| 文件 | 变更 |
|------|------|
| `gui/frontend/dist/protocol.js` | 纯 reducer 校验协议、检测 seq 缺口、归并增量事件、拒绝已被权威 Snapshot 包含的旧事件 |
| `gui/frontend/dist/client-state.js` | 串行合并 Snapshot 刷新；拒绝低 revision Snapshot；维护权威 Snapshot revision floor |
| `gui/frontend/dist/conversation-view.js` | 使用 `data-conversation-key` 对消息、工具和运行态做局部 DOM 协调；保留滚动与展开状态 |
| `gui/frontend/dist/chat-view.js` | 封装会话内容和输入区运行状态 |
| `gui/frontend/dist/components.js` | 输出稳定 render model：`message:<id>`、`tool:<id>`、`chat:activity` |
| `gui/frontend/dist/app.js` | 直接消费 `seelex:event` payload；Snapshot 只用于初始化、显式操作和异常重同步 |

## 交互稳定性

- 仅用户原本接近底部时自动跟随流式输出。
- 加载旧历史时使用高度差恢复滚动锚点。
- `<think>` 展开状态和工具 OUT 完整展开状态在对应节点更新后恢复。
- 工具 IN/OUT 继续限制默认展示长度，完整内容只在用户主动展开时进入 DOM。
- 事件丢失、未知事件或 reducer 无法应用时自动回退到 Snapshot。
- Snapshot 与事件并发到达时，用 revision floor 防止 delta 重放。

## 测试变更

- Go：协议版本、事件信封、delta 消息 ID、唯一工具 ID、分页历史稳定 ID。
- Node：版本不兼容、seq 缺口、旧事件重放、同 revision 多事件、客户端刷新竞争、稳定 render key。

## 未改变

- Application Core 仍是唯一业务状态源。
- TUI 路径和会话存储格式未改变。
- `.seelex/sessions` 不参与构建或替换。
