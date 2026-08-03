# GUI Markdown 渲染前置审查

## 需求摘要

在 GUI 会话消息中安全渲染常用 Markdown 和 `<think>` 思考块，并用真实 ChatState 驱动运行加载动效及可见消息队列，同时保持 Codex 风格、旧会话兼容性和前端组件边界。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---------|---------|---------|---------|
| `gui/frontend/dist/markdown.js` | 新增 | Markdown 渲染组件 | 集中处理解析、转义和链接安全策略 |
| `gui/frontend/dist/components.js` | 修改 | 消息渲染与 ChatState 尾部组件 | 使用独立 Markdown 组件，并组件化运行指示和输入队列 |
| `gui/frontend/dist/app.js` | 修改 | `render()`、提交表单 | 运行时允许继续提交到后端队列，并传入真实 ChatState |
| `gui/frontend/dist/styles.css` | 修改 | 消息与运行状态样式 | 补充 Markdown、思考折叠、加载动效和队列卡片样式 |
| `gui/frontend/dist/markdown.test.mjs` | 新增 | Markdown 单元测试 | 覆盖常用语法、思考块、XSS 转义和危险链接拒绝 |
| `gui/frontend/dist/components.test.mjs` | 新增 | 展示组件单元测试 | 覆盖运行/空闲状态和队列内容安全渲染 |
| `application/chat.go` | 修改 | 队列批量接续 | 避免批量输入开始前重复插入仅供显示的用户消息 |
| `code-changes.md` | 修改 | GUI 变更摘要 | 记录本次实现 |
| `test-report.md` | 修改 | GUI 测试结果 | 记录本次验证 |

## 依赖分析

- 上游依赖：`components.js` 接收 Engine 消息文本和 `Snapshot.Chat` 的 `running/input_queue`。
- 下游影响：当前会话、旧会话恢复、流式刷新、运行指示和排队输入都会使用相同展示组件。
- 外部依赖：无；保持 Wails 内嵌静态资源的离线构建方式。

## 循环依赖检查

- [x] `markdown.js` 仅导出纯函数，不引用业务组件。
- [x] `components.js` 单向引用 `markdown.js`，无新增循环依赖。
- [x] `app.js` 只组合 Snapshot 与展示组件，不复制后端队列状态。

## 风险预估

- HTML/XSS 注入：中概率、高影响；先转义原始 HTML，仅对受控 Markdown token 生成标签。
- 危险链接协议：低概率、高影响；仅允许 `http`、`https`、`mailto` 和安全相对地址。
- 解析歧义：中概率、低影响；优先覆盖会话常用的 CommonMark 子集，未知语法保留为文本。
- 思考标签误判：低概率、中影响；只在块级识别，围栏代码中的标签保持原样。
- 队列重复展示：中概率、中影响；移除后端接续前的临时重复消息，仅以 `InputQueue` 展示等待项。
- 动画可访问性：低概率、低影响；使用 `prefers-reduced-motion` 自动关闭持续动画。

## 建议方案

新增无依赖的纯函数 Markdown 组件，按块级语法和行内语法分层解析，块级 `<think>` 输出原生折叠组件。消息组件组合真实 ChatState 生成运行指示和队列卡片；CSS 提供紧凑的 Codex 风格及减弱动画模式；通过 Node 内置测试验证渲染、安全和状态边界。
