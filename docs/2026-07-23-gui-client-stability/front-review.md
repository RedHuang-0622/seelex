# GUI 客户端稳定性前置审查

## 需求摘要

将 GUI 从事件触发的全量 Snapshot/DOM 重绘，升级为版本化协议、事件增量归并和可保持本地交互状态的稳定客户端。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---------|---------|---------|---------|
| `application/state.go` | 修改 | `Snapshot` | 增加协议版本，供客户端拒绝不兼容 DTO |
| `application/event.go` | 修改 | `Event`、`Publish` | 增加协议版本，明确事件信封契约 |
| `application/chat.go` | 修改 | `appendDelta` | delta 携带稳定消息 ID，支持精确更新 |
| `gui/frontend/dist/protocol.js` | 新增 | 纯函数 reducer | 校验版本、序列并归并事件 |
| `gui/frontend/dist/conversation-view.js` | 新增 | keyed DOM renderer | 仅替换变化节点，保持滚动和展开状态 |
| `gui/frontend/dist/components.js` | 修改 | 会话 render model | 输出稳定 key、HTML 和工具 payload |
| `gui/frontend/dist/app.js` | 修改 | refresh/event/render 链路 | 消费事件 payload，Snapshot 仅作初始化与重同步 |
| `application/*_test.go`、`gui/frontend/dist/*.test.mjs` | 修改/新增 | 协议与渲染测试 | 覆盖版本、序列缺口、delta、key 和状态归并 |

## 依赖分析

- 上游依赖：Application Core 产生 Snapshot 和 Event，Wails Bridge 原样转发。
- 下游影响：流式消息、工具调用、Runtime、审批、队列、会话切换和历史分页。
- 本地 UI 状态：滚动锚点、`details` 展开状态、工具 OUT 展开状态不进入后端 DTO。

## 循环依赖检查

- [x] `application` 不依赖 GUI。
- [x] `protocol.js` 为纯函数，不依赖 DOM。
- [x] `conversation-view.js` 依赖展示 model，不反向依赖 `app.js`。

## 风险预估

- 事件缺失或乱序：中概率、高影响；使用 `seq` 检测并回退 Snapshot。
- 新旧协议不兼容：低概率、高影响；Snapshot/Event 显式携带版本。
- DOM 局部替换丢失交互状态：中概率、中影响；替换前捕获并恢复 details/tool 状态。
- 用户阅读历史时被拉回底部：高概率、中影响；仅在原本接近底部时自动跟随。

## 建议方案

采用 Hybrid Event Reducer：正常事件进入纯函数 reducer，再由 keyed renderer 局部更新；`snapshot.changed`、`resync.required`、序列缺口或协议异常统一调用 Snapshot 兜底。该方案保留 Application Core 为唯一业务状态源，同时让展开、滚动等 UI 状态留在客户端。
