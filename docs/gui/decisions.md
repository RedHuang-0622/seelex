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
- 决策：实现项目所需的 block/inline 子集，先转义原始 HTML，只允许 http/https/mailto 与受控相对链接；`<think>` 转成压缩 details，未闭合流式块保持展开。
- 替代方案：引入第三方 Markdown + sanitizer。
- 理由：当前无 npm runtime/build pipeline，安全边界可测试且功能范围明确。
- 后果：不追求完整 CommonMark；新增语法必须先补安全测试。
- 实现：`gui/frontend/dist/markdown.js:6-201`。

## ADR-GUI-009：命令模式沿用 Core Suggestions

- 状态：已采用
- 决策：`/`、`#`、`$`、`@` 使用同一 Bridge.Suggestions 数据源，面板和输入框内联建议共享渲染函数。
- 理由：GUI 与 TUI 的命令/Skill/Plugin/团队生态一致，避免前端硬编码清单。
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

- 状态：已采用 → **2026-10-08 由 ADR-GUI-023 取代**（界面上的档位滑杆已下线；"预览 / 显式提交 / 回滚 / 快照覆盖"的分工由两条各自独立的运行参数拉条继承）
- 决策：Effort 从 Runtime modal 移到 topbar，以原生四档 range 常驻；`input` 只预览，`change` 才通过 Bridge 提交；Max 紫色动效完全由 `data-effort` 派生。
- 替代方案：保留弹窗 segmented buttons；每次 range input 都调用 Core；把 Effort 业务状态复制到前端 store。
- 理由：Effort 是高频、连续强度语义的运行参数，应无需打开弹窗即可观察和切换；独立 Controller 可用 Node 测试，且不会增加 app.js 的业务状态。
- 后果：前端档位顺序必须和 Core 同步；视觉预览可能短暂领先于 Core，因此失败必须回滚，Snapshot 必须覆盖 committed 状态；动画必须支持 reduced-motion。
- 实现：`gui/frontend/dist/index.html:14-22`、`gui/frontend/dist/effort-control.js:1-77`、`gui/frontend/dist/styles.css:72-125`。（档位滑杆与 `effort-control.js` 已于 2026-10-08 下线——见 ADR-GUI-023；`.effort-control / .syringe-*` 形态与命中区几何由两条运行参数拉条继续使用。）

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

## ADR-GUI-020：工作表格使用轻量 worktable.changed + 资源级 Actor/CSP 并发

- 状态：已接受（当前实现）。
- 决策：右侧工作台统一为工作表格（plan 节点 / todolist / fork 子代理 →
  WorkItem 行，含任务打点），数据由后端权威投影；新增 `worktable.changed`
  增量只携带有界表格，不整份 runtime。资源级数据竞争保护用 Actor + Mailbox
  （todoState 单消费者串行 + 请求/应答 channel），跨资源高并发事件流转用
  CSP 汇聚发布器（channel cap=1，drain latest-wins，生产者阻塞背压）。
- 替代方案：继续复用整份 `runtime.changed`（改动小但每个工具事件深拷贝
  plan + 全量 JSON）；todo 继续用 mutex（无法表达顺序语义与关闭生命周期）。
- 理由：整份 runtime 每次事件深拷贝与 JSON 是长任务内存/CPU 主要热点；
  actor 让资源并发安全且顺序明确，CSP 发布器把子代理工具事件洪峰合并为
  latest-wins，前端 keyed reconcile 只重建变化行。
- 后果：手动状态更新 v1 仅开放 todo 三态；plan/subagent 状态由执行器管理；
  `worktable.changed` 缺失时由既有 seq gap → Snapshot resync 兜底。
  子代理生命周期是被动数据源：`SetSubagentTreeObserver` 在 fork 注册/完成
  时自动刷新工作表格（不依赖模型主观意愿打点）；done 节点有界保留
  （`subagentTreeRetainDone`），`ClearSubagentTree` 显式清空。
  task 体系落地：task 即 worktable 条目，单一注册表 actor（保护粒度=task）；
  todolist 融合为 kind=todo 的 task（打点表）；主动 `taskadd` + 被动
  plan/subagent 生命周期经 CSP 同步；幂等去重 = 归一化 goal 精确键 +
  提示词约束 + 可注入审判钩子；B6 装配件只把 task_id 绑进子代理 NodeScope
  （不注入内容，保护子代理 prompt 格式）；`task.changed` 逐任务增量 +
  `worktable.changed` 结构增量（责任链）；task 快照复用 SessionRecord stack。
- 详设：`modules/work-table.md`、`schemas/work-table.schema.json`。

## ADR-GUI-021：输入前缀一字符一含义（`#` 换 Plugin、`$` 接管 Skill、`@` 召唤团队）

- 状态：**部分退场（2026-10-12 J-1a）**——`@` 手动召唤团队已整条删除（`@` 让给文件指代），
  团队装配只剩 goal/teamwork 一条通道；本文其余前缀口径（`/`、`#`、`$`）仍现行。以下是
  当时（2026-09-17）的决策原样保留。
- 决策：输入前缀与用例一一对应，不再跨域共用：
  `/` 命令与工具、`#` 切换 Plugin、`$` 召回 Skill、`@` 手动召唤团队。
  权威表在 `application/core/completion.go`（常量）+ `application/core/input_router/router.go`
  （路由），前端只消费。
- 理由：
  1. 旧口径里 `#` 是 Skill、`@` 是 Plugin，两个高频动作挤在"看起来像标签/提醒"的字符上，
     用户没有可推断的语义；改成 `#`=插件（进程级配置）、`$`=Skill（可召回的能力）、
     `@`=团队（像 mention 一个人）后，符号与动作有一致的直觉。
  2. `@` 从 Plugin 让位，是因为"手动召唤团队"此前没有入口：团队**只能**由 goal 上线时
     自动装配（`goal_service.ensureGoalAgentTeam`），人没有对称的显式动作。
- 后果：
  - `@` 与面板「装配」共用同一条通道（`AgentTeamMaterializeTeam` → `MaterializeAgentTeam`
    + `teamJoinSeqFor`），不新增第二份团队事实。**召唤面不再列候选**（2026-10-01）：候选
    只在团队库里，而 `Suggestions` 跑在 TUI 的 `View()` 渲染路径与 GUI 每次输入事件上，
    不能为此读盘——所以 `@` 的建议面为空，可用团队名由 `@` 空参的回执列出；库里条目按
    team_id 或名字都可召唤。goal **上线不再自动装配团队**（那会整份替换掉会话已有的团队，
    见 `docs/devlog/2026-10-01-no-builtin-team-shapes.md`）。
  - `@` 的载荷是 `@<团队> [附言]`：路由不按空格切分（团队名可含空格），切分在
    `application/core/input_team.go` 按"最长可命中前缀 = 名字、余下 = 附言"完成；
    附言按 `$<skill> <args>` 的同一条口径作为一条输入下发。这是对"整段余量当名字"
    的修正：那句附言此前既让名字查不到（回执"未知团队: …"），又被静默丢弃。
  - **召唤即干活、干完就走人**（2026-09-17 追加）：附言不只是"一条输入"——它还落成
    一个 goal，因为团队里非内置的席位（`tl`/ADVISOR、员工回合座位）**只由 goal 治理
    驱动**，只装配不落 goal 会停在"在编但没人开工"（实测：tl 角色会话 `total_rows=0`）。
    目标收口（栈里没有 active goal）后团队离场（删角色注册表 + 复位顺序），角色会话
    子树保留（装配幂等键不变，再次召唤复用同一棵）。不带附言的 `@<团队>` 仍是只装配、
    待命。
  - 旧前缀不静默兜底：未命中时给一句迁移提示（`#review` → "召回 Skill 用 $review"）。
  - 前端 `SIGILS` 与 TUI `suggMode` 不再各写一份字符表，改由 `application.HasSigilPrefix`
    / `application.SigilOf` 判定。
- 实现：`application/core/completion.go`、`application/core/input_team.go`、
  `application/core/input_router/router.go`、`gui/frontend/dist/app.js`、`tui/tui.go`。
- 验证：`go test ./application/core/... -run 'Sigil|SubmitTeam|Suggestions'`、
  `go test ./tui/...`、`node --test gui/frontend/dist/*.test.mjs`。
- 详设：`modules/shell-and-interactions.md` 第 6 节。

## ADR-GUI-022：会话团队事实用 `team.changed` 通告，面板按需读面靠它作废缓存

- 状态：**已退场（2026-10-12 J-1a）**——Agent Team 面板与 `AgentTeamView` 读面已整条删除，
  `team.changed` **不再有发布者**（`application/event.EventTeamChanged` 常量、白名单与
  console 日志分支仍在，属可清理残边）。现行团队面是 teamwork 看板（`snapshot.runtime`
  的 `TeamworkBoardView`，见 `modules/right-sidebar.md` 与
  `../arch/teamwork-leader-worker-architecture.md`）。以下是当时的决策原样保留。
- 背景：Agent Team 面板的数据（成员表/工作顺序/发言调度）**不在**会话快照里——
  GUI 的 status 子页与 TUI 的 Alt+T 都按 RPC `AgentTeamView` 拉取，并把结果按会话键缓存。
  "事实变了"因此没有下行通道：GUI 曾靠 composer 文本以 `@` 开头来猜（面板收起、
  goal 自动装配、非本 composer 的来路全漏），TUI 只能靠用户再按一次 Alt+T。
- 决策：新增会话级事件 `team.changed`（`application/event`，载荷为空、**revision=0**），
  由装配/顺序/入职/编辑成员的写路径发布；前端收到后**作废面板缓存**，面板可见时立即
  重取，不可见时只置脏位等下次展开/渲染。
  - 发布点钉在写路径，不钉在某个调用方：`MaterializeAgentTeam`（`@` 召唤 / goal 自动装配 /
    面板 RPC 的共同收口）、`AgentTeamSetOrder`、`AgentTeamInstantiateRole`、
    `AgentTeamPutRole`、`AgentTeamDeleteRole`。读路径（`AgentTeamView`）**不发**。
- 理由：
  1. 面板是"按需读面 + 客户端缓存"，缺一个权威的变更通告就只能靠猜或靠人按刷新。
  2. 事件是既有下行口径（`worktable.changed`/`chat.changed` 同类）：不新增第二份团队事实，
     也不让前端复刻后端判定。
  3. revision=0 沿用 `chat.changed` 的口径：载荷不在快照里，带 revision 会被协议层的
     "已由权威快照表示"陈旧判据丢掉，而面板缓存并不随快照翻转（召唤后调用方紧接一次
     快照刷新是常态，那条事件会被吞）。
- 后果：
  - 不变量：**读路径不发 `team.changed`**（否则"取成员表→通告→再取"自激循环）。
  - 团队库/员工库/默认顺序是**全局母本**，不在本事件范围内：母本 CRUD 的调用方按返回值刷新。
  - 前端不再按 composer 文本猜面板刷新（GUI），TUI 面板打开时自动重取。
- 实现：`application/event/hub.go`、`application/core/agentteam_service.go`、
  `application/core/aliases.go`、`application/application.go`、
  `application/console/backend_console.go`（探针可见性）、
  `gui/frontend/dist/protocol.js`、`gui/frontend/dist/app.js`、`tui/tui.go`。
- 验证：`go test ./application/event/ ./application/core/ -run TeamChanged`、
  `go test ./application/console/ -run EventLogger`、`go test ./tui/ -run TeamPanel`、
  `node --test gui/frontend/dist/protocol.test.mjs
  gui/frontend/dist/agent-team-refresh.test.mjs`；真机探针
  `go run . -frontend backend -backend-prompt "@goal-a2a"` → `stage=team.changed` + exit=0。
- 详设：`modules/shell-and-interactions.md` 第 6 节。

## ADR-GUI-023：档位滑杆从 composer 下线，界面只留两条各自独立的运行参数拉条

- 状态：已采用（2026-10-08）
- 背景：composer 底部一度有三条滑杆——Effort 档位（一次同时设置"思考强度"与"循环
  次数"）、推理强度、循环次数。档位那条是**多件事的预设**（同一动作改写 prompt 层 /
  plan 策略 / ReAct 预算），放在两条读数拉条旁边就成了"一个滑杆同时控制两项"的错觉。
  现场（用户报告）："前端有且只有一个同时控制思考强度和循环次数的 effort 滑杆"、
  "根本没有一条能拖的滑杆"。后半句还有一层真实缺陷：两条读数拉条的容器
  `.composer-knobs .knob-slider` 只声明了 3 列，而拉条根元素有 5 个子元素
  （标签 / 起点 / 滑轨 / 终点 / 数值），多出来的两个被自动排到第二行、滑轨落进 `auto`
  列后宽度塌成 0 —— 界面上只剩两行小字，既看不到滑轨也没有可拖的命中区。
- 决策：
  1. 档位（Effort 等级 `lite/medium/high/max`）**从 composer 下线，不再有界面入口**；
     只经 `/effort` 命令切换（CLI/TUI → `Service.SwitchEffort`，同一道 G0b/INV-G7 运行
     守卫）。`Bridge.SwitchEffort` 保留给命令面板 / 远端控制。
  2. composer 只留**两条各自独立**的拉条（推理强度 / 循环次数），每条只改自己那一项。
  3. 拉条的网格列数必须等于根元素的直接子元素个数（宽 5 / 窄 3），由契约测试钉住。
- 替代方案：保留档位滑杆并加"预设"文案；让档位滑杆改为只改一项；把两条拉条合并成一条
  （都违反"一个控件一件事"）。
- 理由：运行参数是"各改一项"的微调语义；把多件事的预设混进同一片空间，会同时误导用户
  （以为只能整档切换）与代理（以为界面已实现）。界面能改的运行参数必须看得见、拖得动，
  否则"已实现"只存在于代码里。
- 后果：
  - 前端：`effort-control.js` 与其 Node 用例删除；档位专属的视图映射只留在 Core；
    `.effort-control / .syringe-*` 形态与 12.9 的命中区归位由两条拉条继续使用；
    `.composer-knobs .knob-slider` 的列数变更必须与拉条子元素个数同改。
  - 后端：`SwitchEffort` / `SetReasoningEffort` / `SetLoopLimit` 三个入口一个都不删，
    共用同一条运行守卫；切档位仍会把两条读数一起带进该档（拉条跟着动是"命令改了
    读数"，不是界面又长出一条能同时改两项的滑杆）。
- 实现：`gui/frontend/dist/index.html`、`gui/frontend/dist/app.js`、
  `gui/frontend/dist/knob-slider.js`、`gui/frontend/dist/composer-knobs.js`、
  `gui/frontend/dist/styles.css`、`application/core/command.go`。
- 验证：`node --test "gui/frontend/dist/*.test.mjs"`、
  `go test ./gui/ -run 'TestEmbedded'`、
  `go test ./application/core/ -run 'Effort|Knob'`。
- 详设：`modules/composer-knobs.md`。

## ADR-GUI-024：运行参数重排——思考强度贴发送键，循环次数进运行状态弹窗（含「无限循环」）

- 状态：已采用（2026-10-08）
- 背景：ADR-GUI-023 之后，composer 底部仍并排挤着两条拉条（思考强度 / 循环次数）：
  5 段微字一行、轨道细，循环那条 1..9999 的填充分辨率极低（3% 填充看起来像没生效）。
  用户裁决："循环 loop 最高变成 700 多的一个数字配置，支持勾选无限循环，这个循环次数可以
  在运行状态的面板里改；思考强度就放在发送按钮旁边即可。"
- 决策：
  1. **思考强度**（"这一问想多深"）挪进 composer 动作行、发送键之左（`.effort-inline`）。
  2. **循环次数**（"这一趟跑多少轮"，运行期预算）挪进 `#runtime-modal` 的 `.runtime-knobs`
     段；**有限上限 768**（= max 档默认 loops），**「无限循环」勾选**复用既有哨兵
     `limits.input_loop_limit`（9999）——不新增后端字段，取消勾选回到最后一份有限值。
  3. 输入框底部 `.composer-knobs` 行整块退场。
- 替代方案：保留并排两条拉条只做美化；把「无限循环」做成新的后端布尔字段；把思考强度做成
  chip + 下拉菜单（隐藏选项，与"看得见"的诉求相悖）。
- 理由：两者本是不同语义——思考强度属于"这一次输入"，循环次数属于"这一趟运行"；放在同一
  行只是空间巧合，逻辑上并不相邻。768 覆盖四个档位的默认 loops（15/48/384/768），超出它
  的需求本质是"我不想设上限"，用勾选表达比把拉条推到 9999 更诚实。
- 后果：
  - 前端：`index.html`（`.composer-knobs` → `.effort-inline` + `.runtime-knobs`）、
    `styles.css`（轨道底色 `--surface-3`、可见滑块 `.syringe-handle`、`.runtime-knobs`）、
    `composer-knobs.js`（`LOOP_LIMIT_MAX = 768` / `LOOP_LIMIT_INFINITE = 9999` / `infinite`）、
    `app.js`（登记 `loop-limit-infinite`、装配 `infinite`）。
  - 后端：无改动（哨兵与 `validateLoopLimit` 上界都不变）。
  - 契约：列数用例 `TestEmbeddedComposerKnobGridMatchesChildren` 退役，换成
    `TestEmbeddedLoopLimitBounds` + 嵌入布局用例（两处边界）。
- 实现：`gui/frontend/dist/index.html`、`styles.css`、`app.js`、`composer-knobs.js`、
  `knob-slider.js`。
- 验证：`node --test "gui/frontend/dist/*.test.mjs"`（719 通过）、
  `go test ./gui/ -count=1`、`go build ./...`。
- 详设：`modules/composer-knobs.md`。
