# Shell、命令和交互模块详细设计

## 1. 职责与边界

Shell 模块组装页面布局、调用 Bridge、路由客户端状态到各视图，并处理用户输入。它是 composition root，不承载协议 reducer 或会话 DOM 算法。

主要文件：

- `index.html`：语义结构和固定挂载点；
- `app.js`：Bridge 调用、组件组装、事件绑定和非会话区域渲染；
- `effort-control.js`：常驻 Effort 滑杆的独立交互 Controller；
- `styles.css`：三栏布局、弹层、状态、响应式和动效。

## 2. 页面信息架构

实现位置：`gui/frontend/dist/index.html:10-126`。

| 区域 | 内容 | 数据源 |
|------|------|-------|
| Topbar | 应用版本、常驻 Effort、连接、provider/model、token | Info + Runtime |
| 左栏 | Sessions、Accounts | Snapshot |
| 中区 | 历史分页、Conversation、Composer | Snapshot/Event |
| 右栏 | Project 状态、概要、资料来源 | Info + Snapshot |
| Runtime modal | Runtime、Plugins、Plan、Skills | Runtime |
| Command modal | `/`、`#`、`@` 搜索与选择 | Suggestions |
| Interaction modal | 审批/选择问题与选项 | Interaction |

Plugin/Skill 不常驻右栏：它们在输入框同生态位的 runtime button 中打开；右栏保留项目事实。Effort 是高频运行参数，单独常驻 topbar，详细设计见 [Effort 常驻控件](effort-control.md)。

## 3. Composition root

实现位置：`gui/frontend/dist/app.js:1-50`。

app.js 在启动时构造：

- ConversationView：接收 container、clipboard adapter 和 toast adapter；
- ChatView：接收 DOM elements 与 ConversationView；
- GUIClient：接收 Snapshot loader、全量/增量 render callback 和 error callback。
- EffortControl：接收 DOM ports、`SwitchEffort` adapter 和统一错误回调。

这些依赖通过构造参数组合，协议模块不反向 import app.js。

## 4. 全量与增量渲染

实现位置：`gui/frontend/dist/app.js:75-105`。

全量 render 更新 sessions、project、runtime、plugins、accounts、chat、plan、skills 和 interaction。

增量路由：

| kind | 更新范围 |
|------|---------|
| message/tool | Conversation、composer controls；非 delta 时更新 project count |
| runtime.changed | Runtime modal、plugins/accounts/plan/skills、project |
| interaction open/close | Interaction modal |

未知或不能归并的事件不会到此处，由 client-state 先做 Snapshot refresh。

## 5. 会话和历史操作

实现位置：

- 会话列表：`gui/frontend/dist/app.js:128-159`；
- 提交：`gui/frontend/dist/app.js:363-374`；
- cancel/history/new：`gui/frontend/dist/app.js:407-421`。

选择旧会话统一提交 `/resume <id>`，由 Core 完成 Engine history replacement。加载更多历史调用专用 Bridge 方法并使用 anchor scroll。提交完成后清空输入；运行中提交由 Core 加入队列。

## 6. 指令模式

实现位置：`gui/frontend/dist/app.js:268-354`、`gui/frontend/dist/app.js:425-473`。

三种 trigger：

- `/`：命令与工具；
- `#`：Skills；
- `@`：Plugins。

Command modal 和 inline suggestions 都调用 Bridge.Suggestions，并共享 `renderSuggestionList/acceptSuggestion`。输入内容不在前端执行，选中项只写回 composer，最终仍走 Submit。

键盘规则：ArrowUp/Down 移动，Tab 接受内联建议，Enter 提交或接受面板项，Escape 关闭，Ctrl/Cmd+K 打开命令面板。

异步 inline suggestions 使用 `inlineRequest` 序号拒绝旧请求结果，避免快速输入时结果倒序覆盖。

## 7. Runtime Effort 与审批交互

实现位置：

- Runtime/account/plugin/plan：`gui/frontend/dist/app.js:166-235`；
- Effort：`gui/frontend/dist/effort-control.js:22-76`；
- Interaction：`gui/frontend/dist/app.js:237-254`。

Runtime controls 调用 Bridge 后显式 refresh，确保非增量 Core 动作也得到完整状态。Effort 不在 modal 内，拖动只预览、松开才提交且失败回滚。审批选项提交 interaction ID + option ID；UI 不自行判断审批结果。

## 8. 启动与重试

实现位置：`gui/frontend/dist/app.js:480-500`。

启动顺序：hydrate icons → Bridge.Info → initial Snapshot → 注册 Wails events。Bridge 尚未注入或调用失败时 toast 显示错误，并在 600ms 后重试初始化。

`seelex:ready` 与 initial Snapshot 可能交错，由 GUIClient 的 revision 检查处理；`seelex:event` 直接传入 handleEvent，不触发无条件全量刷新。

## 9. 安全和可访问性

- 动态文本优先 `textContent`；使用 innerHTML 的模板字段必须 `escapeHtml` 或安全 renderer。
- 图标按钮具有 title 和 aria-label，不使用 emoji 作为功能图标。
- Modal 使用 `role=dialog` 和 `aria-modal=true`；Conversation 使用 `aria-live=polite`。
- Effort 使用原生 range、`aria-valuetext` 和 output；Max 动效支持 reduced-motion。
- Interaction preview 用 `<pre>.textContent`，不解释 HTML。
- Clipboard 仅由用户点击触发。

## 10. 自动化证据与限制

- Bridge tests 验证绑定方法和嵌入资源。
- Node tests 验证可抽离的协议、Markdown、presentation model 和 Effort Controller。
- app.js 的真实键盘、modal、scroll 和 WebView 行为当前依赖手工验收；后续应引入 Wails/WebView E2E 或 Playwright 静态壳测试。

## 11. 审查清单

- app.js 是否保持 composition/orchestration，而非新增业务状态机？
- 新动态 HTML 是否全部有明确转义路径？
- 新按钮是否具备 icon、title、aria-label 和键盘路径？
- 新命令入口是否复用 Suggestions/Submit，而非硬编码执行？
- 新事件 kind 是否只更新必要区域，并有 Snapshot fallback？
- Effort 是否保持常驻、只在 change 提交，并在失败时恢复权威档位？
