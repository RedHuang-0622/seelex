# GUI 实现决策记录

## ADR-GUI-001：采用 Go + Wails + 原生 Web 前端

- 状态：已采用
- 决策：桌面壳与 Core 适配使用 Go/Wails，视图使用无构建依赖的 HTML/CSS/ES Modules。
- 替代方案：Rust/Tauri、Electron、Go 原生控件。
- 理由：复用 Go Application Core；Wails 提供跨平台 WebView 和绑定；避免 Electron runtime 体积；当前 UI 对复杂原生控件依赖低。
- 后果：运行时依赖系统 WebView；Linux/macOS 必须验证系统依赖；前端需要自行维护组件和状态边界。
- 实现：`gui/run_wails.go:17`、`gui/assets.go:5`、`gui/frontend/dist/index.html:1`。

## ADR-GUI-002：Application Core 是唯一业务状态源

- 状态：已采用
- 决策：GUI 只能通过 `gui.Application` 调用 Core，并消费 Snapshot/Event；不在 JavaScript 重写会话、Plugin 或审批业务规则。
- 理由：TUI/GUI 行为一致，业务测试无需启动 WebView，后续 sidecar 也可复用同一 Core。
- 后果：Bridge 契约必须稳定；前端出现缺口时优先扩展 Core/DTO，而不是创建 UI 私有事实。
- 实现：`gui/bridge.go:19-30`、`application/app.go:105-151`。

## ADR-GUI-003：显式版本化 Snapshot 与 Event

- 状态：已采用
- 决策：Snapshot 和每个 Event 都携带 `protocol_version`；不兼容版本由客户端明确拒绝。
- 理由：Wails 内嵌资源通常同版本，但调试缓存、未来 sidecar 和远程客户端需要可诊断的不兼容行为。
- 后果：破坏性 schema 变化必须升级版本并提供迁移或明确错误。
- 实现：`application/state.go:6-23`、`application/event.go:24-30`、`gui/frontend/dist/protocol.js:8-12`。

## ADR-GUI-004：Event reducer + Snapshot 兜底

- 状态：已采用
- 决策：正常事件通过纯 reducer 增量归并；seq 缺口、未知类型、无效 payload 或客户端无基线时重新拉 Snapshot。
- 替代方案：每个事件都拉 Snapshot；完全 event sourcing。
- 理由：兼顾流式性能和恢复正确性，不要求所有 Core 状态都永久事件化。
- 后果：Snapshot 仍是公开且必须测试的权威接口；EventHub 必须保持单调 seq。
- 实现：`gui/frontend/dist/protocol.js:15-112`、`gui/frontend/dist/client-state.js:3-55`。

## ADR-GUI-005：用 revision floor 解决 Snapshot/Event 竞争

- 状态：已采用
- 决策：接受权威 Snapshot 时记录 revision floor；其已包含的旧事件只推进 seq，不再次归并。
- 理由：Snapshot 返回和 Wails 事件到达可能交错，单纯按当前 revision 拒绝会误伤同一 revision 的多个合法兄弟事件。
- 后果：floor 只在接受 Snapshot 时更新，不能随每个事件更新。
- 实现：`gui/frontend/dist/client-state.js:5-40`、`gui/frontend/dist/protocol.js:15-31`。

## ADR-GUI-006：会话使用 keyed DOM 局部协调

- 状态：已采用
- 决策：消息、工具和活动尾部分别使用 `message:<id>`、`tool:<id>`、`chat:activity`；只替换 HTML 变化的节点。
- 替代方案：每次设置 `conversation.innerHTML`；引入 React/Vue。
- 理由：保留滚动和展开状态，减少长会话重排，同时维持零前端构建依赖。
- 后果：后端必须提供稳定 ID；renderer 需要显式捕获/恢复节点局部状态。
- 实现：`gui/frontend/dist/components.js:50-66`、`gui/frontend/dist/conversation-view.js:41-89`。

## ADR-GUI-007：工具调用显示为单卡片 IN/OUT 双面板

- 状态：已采用
- 决策：按 tool call ID 合并 start/result，IN 与 OUT 分框；预览按字符和行数截断，完整值保存在内存并按需展开。
- 理由：工具调用可扫描，避免大输出压垮布局，又不丢失复制/检查能力。
- 后果：完整输出仍占客户端内存，但不会默认进入 DOM；极大结果仍应在后端增加持久化/引用策略。
- 实现：`gui/frontend/dist/components.js:86-190`、`gui/frontend/dist/conversation-view.js:14-38`。

## ADR-GUI-008：Markdown 自有安全子集，think 使用 details

- 状态：已采用
- 决策：实现项目所需的 block/inline 子集，先转义原始 HTML，只允许 http/https/mailto 与受控相对链接；`<think>` 转成折叠 details，未闭合流式块保持展开。
- 替代方案：引入第三方 Markdown + sanitizer。
- 理由：当前无 npm runtime/build pipeline，安全边界可测试且功能范围明确。
- 后果：不追求完整 CommonMark；新增语法必须先补安全测试。
- 实现：`gui/frontend/dist/markdown.js:6-201`。

## ADR-GUI-009：命令模式沿用 Core Suggestions

- 状态：已采用
- 决策：`/`、`#`、`@` 使用同一 Bridge.Suggestions 数据源，面板和输入框内联建议共享渲染函数。
- 理由：GUI 与 TUI 的命令/Skill/Plugin 生态一致，避免前端硬编码清单。
- 后果：Suggestion DTO 是多前端契约的一部分；输入执行仍统一走 Submit。
- 实现：`gui/bridge.go:205-207`、`gui/frontend/dist/app.js:267-353`。

## ADR-GUI-010：GUI CI 使用独立逻辑 job

- 状态：已采用
- 决策：Ubuntu `gui-tests` 单独执行 Node/JS/Core/Bridge 契约，Windows build matrix 保留 production tags 编译；workflow 监听 `main` 和 `gui`。
- 替代方案：在三平台 build matrix 中重复执行 Node tests。
- 理由：检查名稳定、故障定位直接、执行成本更低，并保留平台职责边界。
- 后果：真实 WebView E2E 仍需后续独立 job/runner。
- 实现：`.github/workflows/ci.yml:5-10`、`.github/workflows/ci.yml:83-110`。

## ADR-GUI-011：Effort 使用独立常驻滑杆与派生 Max 动效

- 状态：已采用
- 决策：Effort 从 Runtime modal 移到 topbar，以原生四档 range 常驻；`input` 只预览，`change` 才通过 Bridge 提交；Max 紫色动效完全由 `data-effort` 派生。
- 替代方案：保留弹窗 segmented buttons；每次 range input 都调用 Core；把 Effort 业务状态复制到前端 store。
- 理由：Effort 是高频、连续强度语义的运行参数，应无需打开弹窗即可观察和切换；独立 Controller 可用 Node 测试，且不会增加 app.js 的业务状态。
- 后果：前端档位顺序必须和 Core 同步；视觉预览可能短暂领先于 Core，因此失败必须回滚，Snapshot 必须覆盖 committed 状态；动画必须支持 reduced-motion。
- 实现：`gui/frontend/dist/index.html:14-22`、`gui/frontend/dist/effort-control.js:1-77`、`gui/frontend/dist/styles.css:72-125`。

## ADR-GUI-012：DSL 卡片是 Conversation item，使用 Core-owned 协议

- 状态：拟议。
- 决策：Agent 只能通过 `render_card` 工具提交 JSON DSL；Core 校验、持久化并生成 `ConversationItem(kind=card)`。卡片在中间对话区渲染，不进入右栏 Workspace。
- 替代方案：前端解析 Markdown fenced JSON；引入 React/A2UI runtime；把 Card 当 Workspace widget。
- 理由：显式结构化入口便于权限、持久化、恢复和 E2E；保留现有 Wails/ES Modules；Conversation 是 Agent 回复的正确语义位置。
- 后果：需要 protocol v2 与 transcript/presentation store；通用 surface 抽象在 v1 仍只允许 conversation target。
- 详设：`modules/dsl-card-runtime.md`。

## ADR-GUI-013：Agent E2E 使用确定性分层，不以真实模型作为 PR 主门禁

- 状态：拟议。
- 决策：Go scenario 验证真实 Application，Playwright + fake Wails Bridge 验证真实前端，Windows Wails smoke 验证容器；live Agent 只在 opt-in nightly。
- 替代方案：只跑真实模型/Wails；只增加更多 Node 单元测试。
- 理由：同时获得状态机真实度、DOM 覆盖、可复现性和可控成本。
- 后果：需要共享 scenario schema、fixture、trace 与稳定实体 selector。
- 详设：`modules/agent-e2e-interaction.md`。

## ADR-GUI-014：Workspace 采用后端 Port + PathGuard，右栏只做专用视图

- 状态：拟议。
- 决策：目录、预览、diff 和产物由 `application.WorkspacePort` 编排，`workspace` adapter 强制 root、symlink、size 和 policy；右栏固定为 Overview/Files/Changes/Artifacts。
- 替代方案：Bridge 直接读文件；用任意 DSL surface 生成 Workspace UI。
- 理由：安全边界可复用、可测试且不依赖 DOM；领域视图比 Agent 自由生成 Workspace 更可预测。
- 后果：Snapshot 只放摘要，内容改为分页 query；Card 文件跳转必须经过 opaque resource ID 和 Core action resolution。
- 详设：`modules/workspace-sandbox.md`。

## ADR-GUI-015：多会话采用 SessionActor + Coordinator，不复用单 Engine 热切换

- 状态：拟议。
- 决策：中心区域用页签多开会话；`WorkbenchCoordinator` 管 open/active 页面和公平调度，每个 `SessionActor` 通过 factory 获得独立 Engine、状态、审批与 Event scope。active 页面唯一，但 running 会话可以有多个。
- 替代方案：继续用一个 Service 通过 `ReplaceHistory` 切换；每个会话启动独立子进程。
- 理由：单 Engine 切换不能让后台任务继续且易串 History；子进程隔离更强但显著增加 IPC、监督和桌面打包成本。进程内 actor 能复用当前状态机，并以并发上限 1 渐进迁移。
- 后果：所有动作和事件必须显式携带 session ID；Effort/Skill/Plan/input queue 变为会话级；共享 Workspace 写需要 revision precondition；页签切换不能调用 `/resume`。
- 详设：`modules/multi-session-pages.md`。

## ADR-GUI-016：Generation 使用不可变目录与原子 current 指针

- 状态：已接受（目标架构）。
- 决策：checkpoint 先在同文件系统 staging 写完资源和 manifest，校验 hash 后原子发布目录，再用 CAS/atomic replace 更新 `current`；已提交 generation 永不原地修改。
- 理由：把崩溃结果限制为完整旧版本或完整新版本，支持可验证回滚和故障调查。
- 后果：需要 manifest Schema、每步崩溃测试、保留/pin/reader lease 与独立 staging 清理；rollback 只切指针并产生新内存 revision。

## ADR-GUI-017：Schema 是 JSON 契约事实源

- 状态：已接受。
- 决策：对外与跨模块 JSON payload 必须有 Draft 2020-12 Schema、稳定 `$id` 和可校验示例；Markdown、Go struct 或 JS object 不单独定义契约。
- 理由：避免当前实现、规划模块和多个 adapter 复制出不同字段语义。
- 后果：Schema、示例和 module DAG 纳入 Go 测试；不兼容变更升级 protocol/schema version 并更新 Changelog。

## ADR-GUI-018：HTTP 与 Wails 是并列 Adapter

- 状态：已接受（HTTP 为规划）。
- 决策：二者共享 Application ports，但各自拥有 transport DTO、生命周期和错误映射；HTTP 不直接暴露 Wails binding 或内部 channel。
- 理由：网络边界需要认证、幂等、条件请求、cursor、限流与错误脱敏，桌面进程内调用不应被这些细节污染。
- 后果：HTTP 默认 loopback，远程模式 fail-closed；API 固定 `/api/v2`，mutation 必须有幂等与 revision precondition。

## ADR-GUI-019：RAG 证据门禁控制需求到 Dev 的自动生成资格

- 状态：已接受（目标架构）。
- 决策：LLM 只生成候选需求/架构/详设；混合 RAG 为 atomic claim 获取可定位证据，独立 assessor 判定 supports/contradicts/related/insufficient，版本化 policy 依据 evidence readiness 决定自动生成资格。
- 理由：向量相关性不等于工程支持关系；在线无法知道完整证据全集，不能把相似度或伪 Recall 当门禁。
- 后果：低证据/冲突条目保留并进入人工队列；项目新增可由人工确认进入 capability gap；需求、架构、详设、代码和测试继承 evidence ID 与审计链；E2E 反馈按责任层重开后继 generation。
