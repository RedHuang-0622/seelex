# GUI 代码审查与功能追溯

## 1. 审查范围

审查基线为 `gui` 分支的 Wails GUI、Application 前端契约和 `.github/workflows/ci.yml`。功能打点必须同时指向详细设计、当前实现段落和自动化证据；仅有界面或文档不算通过。

状态定义：

| 状态 | 含义 |
|------|------|
| 通过 | 主路径、失败/恢复路径和自动化证据齐全 |
| 有条件通过 | 主体实现可靠，但仍有明确的 E2E/平台验证缺口 |
| 不通过 | 实现或关键自动化证据缺失 |

## 2. 详细设计功能打点

| ID | 功能点 | 详细设计 | 实现代码段落位置 | 自动化证据 | 审查结论 |
|----|--------|---------|------------------|------------|----------|
| GUI-PRO-001 | Snapshot/Event 协议版本 | [Application §2](modules/application-protocol.md#2-数据契约) | `application/state.go:6-23`；`application/event.go:24-30`；`protocol.js:8-12` | `command_test.go:461`；`protocol.test.mjs:18` | 通过：版本缺失/不兼容有明确错误 |
| GUI-PRO-002 | Event seq 缺口重同步 | [Client §3](modules/client-state.md#3-event-reducer) | `application/event.go:81-101`；`protocol.js:15-37` | `application_test.go:207`；`protocol.test.mjs:41` | 通过：背压与前端 gap 均覆盖 |
| GUI-PRO-003 | Snapshot/Event 竞争保护 | [Client §4-5](modules/client-state.md#4-snapshot-管理) | `client-state.js:3-55`；`protocol.js:15-31` | `client-state.test.mjs:58,72`；`protocol.test.mjs:57,70` | 通过：旧 Snapshot 和 delta replay 均拒绝 |
| GUI-CHAT-001 | 流式 delta 精确定位 | [Application §3](modules/application-protocol.md#3-chat-状态机) | `application/chat.go:85-102`；`protocol.js:65-74` | `application_test.go:222`；`protocol.test.mjs:24` | 通过：稳定 message_id 契约完整 |
| GUI-CHAT-002 | 运行中输入队列 | [Application §3](modules/application-protocol.md#3-chat-状态机)；[Render §6](modules/conversation-rendering.md#6-chat-控件与活动尾部) | `application/app.go:114-151`；`components.js:68-84`；`chat-view.js:3-31` | `command_test.go:649`；`components.test.mjs:11-38` | 通过：Core 真队列进入 UI model |
| GUI-CHAT-003 | Chat cancel | [Shell §5](modules/shell-and-interactions.md#5-会话和历史操作) | `application/app.go:153-161`；`app.js:407-410` | `command_test.go:556`；Chat cancellation tests | 通过：request ID 校验防止取消旧请求 |
| GUI-TOOL-001 | 工具 start/complete 配对 | [Application §4](modules/application-protocol.md#4-工具生命周期) | `application/chat.go:118-181`；`application/chat.go:353-417` | `application_test.go:300,317` | 通过：跨 ChatStream 使用唯一 ID |
| GUI-TOOL-002 | 工具 IN/OUT 单卡片 | [Render §3](modules/conversation-rendering.md#3-工具卡片) | `components.js:86-128`；`components.js:152-182` | `components.test.mjs:41` | 通过：按 tool ID 合并 input/result |
| GUI-TOOL-003 | 工具输出限制与按需展开 | [Render §3,8](modules/conversation-rendering.md#3-工具卡片) | `components.js:176-190`；`conversation-view.js:14-38,79-89` | presentation tests + 手工 WebView | 有条件通过：限制/转义已测，真实 DOM 展开待 E2E |
| GUI-STATE-001 | Snapshot 深拷贝 | [Application §6](modules/application-protocol.md#6-深拷贝与并发规则) | `application/state.go:155-197` | `application/race_test.go`；全仓 race CI | 通过：Runtime/Plan 递归复制 |
| GUI-SES-001 | 会话列表 | [Shell §2,5](modules/shell-and-interactions.md#2-页面信息架构) | `application/app.go:105-111`；`app.js:128-159` | `application_test.go:166`；Bridge contract | 通过：当前会话缺少 metadata 时仍补显示 |
| GUI-SES-002 | 恢复旧会话上下文 | [Application §5](modules/application-protocol.md#5-会话恢复与分页) | `application/app.go:398-439`；`app.js:139-156` | `command_test.go:237-267`；`application_test.go:180` | 通过：Engine 与 Snapshot 同步替换 |
| GUI-SES-003 | 历史分页与滚动锚点 | [Application §5](modules/application-protocol.md#5-会话恢复与分页)；[Render §5](modules/conversation-rendering.md#5-滚动策略) | `application/app.go:441-486`；`conversation-view.js:97-113`；`app.js:413-415` | `application_test.go:341` + 手工 scroll | 有条件通过：数据契约已测，滚动像素待 E2E |
| GUI-BRG-001 | 窄 Application Bridge | [Bridge §2](modules/desktop-bridge.md#2-调用方接口) | `gui/bridge.go:19-31`；`gui/bridge.go:173-207` | `bridge_test.go:104` | 通过：GUI 不依赖 Core 具体实现 |
| GUI-BRG-002 | 事件泵生命周期 | [Bridge §3](modules/desktop-bridge.md#3-生命周期) | `gui/bridge.go:109-162` | `bridge_test.go:152`；race CI | 通过：start/stop 幂等，goroutine 可等待退出 |
| GUI-BRG-003 | Wails tags 与 embedded assets | [Bridge §4](modules/desktop-bridge.md#4-wails-组装) | `gui/run_wails.go:1-55`；`gui/run_stub.go:1-12`；`gui/assets.go:5-8` | `bridge_test.go:187`；Windows GUI build | 通过：无 tag stub 与 production build 分离 |
| GUI-CLI-001 | Snapshot 刷新合并 | [Client §4](modules/client-state.md#4-snapshot-管理) | `client-state.js:3-52` | `client-state.test.mjs:21-88` | 通过：同一时刻单 refresh 链且不丢后续请求 |
| GUI-DOM-001 | keyed DOM 局部更新 | [Render §4](modules/conversation-rendering.md#4-keyed-dom-协调) | `components.js:50-66`；`conversation-view.js:41-70` | `components.test.mjs:41` + 手工 WebView | 有条件通过：key model 已测，DOM reconciliation 待 E2E |
| GUI-DOM-002 | 自动跟随与阅读位置保护 | [Render §5](modules/conversation-rendering.md#5-滚动策略) | `conversation-view.js:97-117` | 手工 WebView | 有条件通过：算法明确，缺 DOM layout 自动测试 |
| GUI-MD-001 | 安全 Markdown 子集 | [Render §7](modules/conversation-rendering.md#7-markdown-与-think) | `markdown.js:6-53,86-201,264-275` | `markdown.test.mjs:8-55` | 通过：原始 HTML、危险 URL 和 code inert 均覆盖 |
| GUI-MD-002 | think 折叠与流式展开 | [Render §7](modules/conversation-rendering.md#7-markdown-与-think) | `markdown.js:55-84` | `markdown.test.mjs:57-82` | 通过：闭合、未闭合、fence 三种边界覆盖 |
| GUI-SHL-001 | 三栏项目工作区 | [Shell §2](modules/shell-and-interactions.md#2-页面信息架构) | `index.html:10-82`；`app.js:108-125`；`styles.css` layout sections | embedded asset test + 手工 WebView | 有条件通过：结构稳定，响应式待多平台截图/E2E |
| GUI-SHL-002 | Runtime 弹层 | [Shell §7](modules/shell-and-interactions.md#7-runtime-effort-与审批交互) | `index.html:84-105`；`app.js:166-235,256-265` | Bridge switch tests + 手工 modal | 有条件通过：业务动作有 test，弹层交互待 E2E |
| GUI-EFF-001 | Effort 常驻且独立于弹窗 | [Effort §1,6](modules/effort-control.md#1-目标与边界) | `index.html:14-22`；`bridge_test.go:228-241` | `bridge_test.go:187-244` | 通过：嵌入资源契约固定 topbar/modal 边界 |
| GUI-EFF-002 | 四档拖动、单次提交与失败回滚 | [Effort §2-4](modules/effort-control.md#2-四档数据模型) | `effort-control.js:1-77`；`app.js:41-50,166-177`；`application/app.go:213-229` | `effort-control.test.mjs:36-77`；`command_test.go:564-574` | 通过：预览/提交/回滚和 Core Max 均覆盖 |
| GUI-EFF-003 | Max 动态紫色光效与动效降级 | [Effort §5-6](modules/effort-control.md#5-max-紫色光效) | `styles.css:72-125,358-361`；`effort-control.js:31-40` | `effort-control.test.mjs:43-52` + Edge 静态截图 | 有条件通过：Max/Lite 视觉状态已复核，真实动画时序仍列入 WebView 验收 |
| GUI-CMD-001 | `/ # @` 指令模式 | [Shell §6](modules/shell-and-interactions.md#6-指令模式) | `gui/bridge.go:205-207`；`app.js:268-354,425-473` | `command_test.go:502-554` | 通过：数据源复用 Core Suggestions |
| GUI-INT-001 | Approval/Interaction modal | [Shell §7](modules/shell-and-interactions.md#7-runtime-effort-与审批交互) | `application/app.go:163-197,280-303`；`app.js:237-254` | `application_test.go:357`；`command_test.go:620` | 通过：Core 生命周期已测；WebView 点击为手工验收 |
| GUI-CI-001 | gui 分支 CI 触发 | [CI §2](ci-and-testing.md#2-触发策略) | `.github/workflows/ci.yml:5-10` | workflow 静态审查 | 通过：push/PR 均包含 gui |
| GUI-CI-002 | GUI Node/contract job | [CI §4](ci-and-testing.md#4-gui-tests-job) | `.github/workflows/ci.yml:83-110` | 本地等价 Node/Go 命令 | 通过：独立稳定 job name |
| GUI-CI-003 | Windows production tags build | [CI §5](ci-and-testing.md#5-production-build) | `.github/workflows/ci.yml:50-52` | 本地 production build + Windows runner | 通过：满足 Wails manual build tags |

## 3. 五轴审查

| 维度 | 状态 | 评分 | 结论 |
|------|:---:|:---:|------|
| 正确性 | 通过 | A- | 协议、会话、工具、队列、Effort、Markdown 和 Bridge 有契约测试；WebView layout 行为仍需 E2E |
| 可读性 | 通过 | A- | reducer/view/bridge 职责清晰；Effort 已拆为独立 Controller，`app.js` 仍处于 500 行上限 |
| 架构 | 通过 | A | Core 单一状态源、调用方接口、纯 reducer、keyed view 和 Effort Controller 依赖单向 |
| 安全性 | 通过 | A | Markdown 默认转义、URL allowlist、工具 preview 限制、发布配置白名单 |
| 性能 | 通过 | A- | 流式事件不拉全量 Snapshot，Conversation 不整体重绘；未建立超长会话 benchmark |
| 测试/平台 | 有条件通过 | B+ | GUI CI 已接线；等待远端 CI 首次通过和真实 WebView E2E |

## 4. 发现的问题

### 严重问题

无。

### 警告

1. 真实 WebView 的 DOM replacement、scroll anchor、modal/keyboard、Effort 动画像素和多平台布局没有自动化 E2E。
2. `app.js` 当前为 500 行，达到工程约束上限；Effort 已拆分，后续业务区域仍应拆为 session/project/runtime/command views。
3. 代码行号是当前 revision 的导航信息，后续修改必须通过 symbol 名和本矩阵同步刷新。

### 建议

1. 下一阶段优先实现 fake Bridge + Playwright 的浏览器级测试。
2. 将 CSS 视觉回归截图按 Windows/macOS/Linux WebView 分平台保存。
3. 为 1k/10k 消息 conversation model 和 reconcile 建立性能基线。

## 5. 最终判断

有条件通过，可继续作为 Windows alpha 使用并进入远端 CI。正式预发行门槛还包括：远端 `GUI tests`/race/Windows build 全绿，以及一轮真实 WebView Effort 四档/Max 动效、长输出、历史滚动、工具展开、think、队列、命令和审批验收。
