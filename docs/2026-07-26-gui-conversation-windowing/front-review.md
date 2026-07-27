# GUI 对话区滑动窗口虚拟滚动前置审查

## 需求摘要

为 Seelex GUI 会话对话区引入滑动窗口（Windowing / Virtual Scrolling）机制，限制同时渲染的 DOM 节点数量，避免长会话因过多 Markdown 内容、代码块、工具调用卡片导致渲染性能下降和界面卡顿。

## 性能瓶颈分析

当前 `conversation.js` + `components.js` 渲染链路存在以下问题：

| # | 问题 | 影响 |
|---|------|------|
| 1 | 全部消息始终渲染为 DOM 节点 | 200+ 条消息 → 数千 DOM 节点，布局/重绘/GC 压力持续增大 |
| 2 | 每次 `renderConversationModel` 遍历所有消息并调用 `renderMessage`/`renderToolCall` | 即使 1 条新消息到来，也遍历 200+ 条历史消息 |
| 3 | Markdown 每次重新解析 | `renderMessage` 内的 `markdown(content)` 对每条消息全文解析，大代码块影响显著 |
| 4 | `htmlByKey` 缓存仅单次 reconcile 生命周期有效 | 节点从 DOM 移除后缓存即丢失，下次窗口平移需重新 Markdown |
| 5 | `prettyValue` 中 JSON 格式化大字符串 | 大工具参数反复 `JSON.parse/stringify` |

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---------|---------|---------|---------|
| `gui/frontend/dist/conversation-view.js` | 重构 | 全文件 | 将现有 reconciliation 扩展为滑动窗口虚拟滚动管理器 |
| `gui/frontend/dist/components.js` | 修改 | `renderConversationModel` 等 | 输出增加 estimatedHeight 元数据；加入完整 HTML 缓存层 |
| `gui/frontend/dist/chat-view.js` | 修改 | `renderConversation` | 调整渲染接口适配窗口参数 |
| `gui/frontend/dist/app.js` | 修改 | `conversationView.render` 调用 | 传入窗口配置参数 |
| `gui/frontend/dist/styles.css` | 修改 | `.conversation` 及相关 | 确保 spacer 布局与滚动行为一致 |
| `gui/frontend/dist/components.test.mjs` | 新增/修改 | 渲染测试 | 窗口行为、HTML 缓存、窗口平移测试 |
| `gui/frontend/dist/conversation-view.test.mjs` | 新增 | — | 滑动窗口与滚动锚点测试 |

不涉及后端 `application/`、`gui/bridge.go` 或 `gui/run_wails.go` — 本方案完全在前端实现。

## 依赖分析

- **上游依赖**：`components.js` 的输出格式（`items: [{ key, html }]`）须扩展为包含 `{ kind, estimatedHeight }` 但不破坏现有接口。
- **下游影响**：
  - `chat-view.js` 的 `renderConversation` 接口签名不变，内部调用替换。
  - `app.js` 中 `conversationView.render(model, { scrollMode })` 接口签名不变。
  - 所有增量更新路径（`message.delta`, `tool.started` 等）通过窗口管理器处理，不影响外部调用方。
- **局部 UI 状态**：滚动位置、details 展开状态、tool OUT 展开状态、copy toast 等保持在 windowing 层恢复。

## 循环依赖检查

- [x] `conversation-view.js` 依赖 `components.js` 的输出，不反向依赖 `app.js`。
- [x] `components.js` 保持纯函数，不依赖 DOM 或 `conversation-view.js`。
- [x] 窗口管理器是 `conversation-view.js` 的内部重构，不引入新模块依赖。

## 风险评估

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| 窗口切换时滚动位置跳变 | 中 | 高 | 精确计算 spacer 高度；滚动位置补偿公式验证 |
| 窗口内项高度估算不准 | 中 | 中 | 首次渲染后缓存实际 offsetHeight；工具调用使用固定估算值 |
| 流式增量与窗口平移竞争 | 低 | 中 | 尾部追加时锁定窗口位置在尾部；不允许并发平移 |
| 智能感知/命令弹窗与滚动冲突 | 低 | 低 | 弹窗使用 fixed 定位，不受滚动影响 |
| 窗口 size 选择不当 | 低 | 中 | 设计为配置参数，后续根据性能反馈调整默认值 |
| 回到顶部加载历史后窗口定位错误 | 低 | 高 | 历史锚点基于 scrollHeight 增量 + 窗口起始偏移双重修正 |

## 建议方案

采用 **非等高的 Fixed-Window Virtual Scroller**（固定窗口 + 可变项目高度），核心设计：

1. 限制同时渲染 ~60 项（50 窗口 + 10 预渲染缓冲）。
2. 上下各用 `<div>` spacer 保持滚动容器正确高度。
3. 所有项目的 HTML 字符串在内存中持久缓存（`htmlCache: Map<key, {html, height}>`）。
4. 首次渲染后测量实际高度并缓存。
5. 跟随尾部（`isAtBottom`）时新消息自动推入窗口底部，不移除旧消息直到窗口满。
6. 用户向上滚动时窗口向上平移，`scrollTop` 补偿保持视觉连续。

详见 [`plan.md`](plan.md) 的完整设计。
