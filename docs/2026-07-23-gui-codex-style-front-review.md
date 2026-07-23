# 前置审查报告

## 需求摘要

将 Wails GUI 重构为克制的 Codex 风格，组件化消息渲染，以一个工具组件呈现配对的 `IN`/`OUT`，限制默认输出长度，并以原生 SVG 图标替代冗长按钮文字。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---------|---------|---------|---------|
| `gui/frontend/dist/components.js` | 新增 | 全文件 | 承载图标、消息、工具调用、资料源等纯渲染组件和输出截断策略 |
| `gui/frontend/dist/app.js` | 修改 | import、会话渲染、交互绑定 | 从单体渲染函数迁移到组件，并绑定工具输出展开/复制行为 |
| `gui/frontend/dist/index.html` | 修改 | 顶栏、输入区操作按钮、弹窗按钮 | 使用原生 SVG 图标和可访问名称，压缩文字控件 |
| `gui/frontend/dist/styles.css` | 修改 | 主题、消息、工具、按钮、布局 | 建立 Codex 风格的中性色、细边框、紧凑排版和 IN/OUT 双框 |
| `application/chat.go` | 修改 | `appendHistoryLocked` | 为历史 tool result 保留 `ToolCallID`，确保恢复会话后 IN/OUT 能稳定配对 |
| `gui/bridge_test.go` | 修改 | 嵌入资源契约 | 验证组件模块、IN/OUT 和图标控件随 GUI 嵌入 |
| `code-changes.md`、`test-report.md` | 修改 | GUI 变更与验证摘要 | 同步记录设计和质量门禁 |

## 依赖分析

- 上游依赖：`application.Snapshot.Conversation` 提供 `Message.Tool` 的 ID、参数、结果、错误和状态。
- 下游影响：Wails 通过 `embed.FS` 自动包含新增的 `frontend/dist/components.js`，无需打包器或 Node 运行时。
- 模块方向：`app.js -> components.js`；组件模块不访问 Wails Bridge，不反向依赖 `app.js`。

## 循环依赖检查

- [x] `components.js` 仅包含纯函数和无状态组件，不导入 `app.js`
- [x] Go Application Core 不依赖 GUI 组件
- [x] 不新增外部前端依赖

## 风险预估

- 历史工具调用缺 ID：中概率、中影响；后端补齐 `ToolCallID`，前端再按 ID/顺序降级配对。
- 超长输出造成 WebView 卡顿：中概率、高影响；默认按字符和行数双重截断，仅在用户主动展开时插入完整内容。
- 图标化降低可发现性：中概率、中影响；所有图标按钮保留 `title`、`aria-label` 和 focus 样式。
- ES module 路径错误：低概率、高影响；Node 语法检查、嵌入资源契约和 Wails 生产构建共同验证。

## 建议方案

新增零依赖组件模块，以纯函数生成可测试 HTML。工具消息先归并为 display item，再渲染成一个 Tool Call 卡片；输入参数进入 `IN`，结果或错误进入 `OUT`。OUT 默认最多显示 40 行或 2400 字符，超出部分显示剩余量并提供展开按钮。整体视觉改为低饱和中性色、细边框和紧凑间距，交互控件使用统一 16px SVG 图标。
