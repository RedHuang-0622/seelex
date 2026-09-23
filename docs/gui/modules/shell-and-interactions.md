# Shell、命令和交互模块详细设计

> 状态：已实现（单 active session）
> 总体架构：[`../architecture.md`](../architecture.md)

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
| 左栏 | Sessions（条目 = 标题段 + ⋯ 段） | Snapshot |
| 中区 | 历史分页、Conversation、Composer | Snapshot/Event |
| 右栏 | Project 状态（键值表）、概要、账户栏、Agent Team、资料来源 | Info + Snapshot |
| Runtime modal | Runtime、Plugins、Plan、Skills | Runtime |
| Command modal | `/`、`#`、`$`、`@` 搜索与选择 | Suggestions |
| Interaction modal | 审批/选择问题与选项 | Interaction |

Plugin/Skill 不常驻右栏：它们在输入框同生态位的 runtime button 中打开；右栏保留项目事实。账号是进程级事实，落在右栏「状态」子页的状态一栏之下（模块文档见
[右栏](right-sidebar.md)）；左栏只承载会话树。Effort 是高频运行参数，单独常驻 topbar，详细设计见 [Effort 常驻控件](effort-control.md)。

### 2.1 会话条目（两段式）

会话条目固定两段，别再把信息塞回单行：

- **标题段**（`.session-title`）：状态点 + 完整标题（CSS 省略号按栏宽截断）+ 状态徽标。
  条目里**不出现**时间与 token——数据层也不再砍标题（`sidebar.js` 的截断函数已删）。
- **⋯ 段**（`.session-more`）：省略号按钮，点开显示原来的三个操作（置顶 ★ / 分支 ⑂ /
  删除 ✕）；同一时刻只开一条，点空白、Esc 或执行动作即收起（`openSessionMenu`）。

完整标题、时间与 token 由共享提示气泡给出：`#ui-tooltip` 一条 DOM，`[data-tip]`
（`\n` 分行，首行标题、次行「时间 · tokens」）委托触发，320ms 延迟出现，滚动/
失焦/Esc 收起。鼠标常驻与键盘聚焦都能拿到同样信息。

### 2.2 焦点与按压（拟物但克制）

- 焦点只作用于**外框**：`1px solid var(--focus-ring)` + `box-shadow: var(--focus-glow)`
  （`0 0 12px 1px`，零偏移、纯发散模糊），不描内框；输入区由容器
  `.composer:focus-within` 聚光，内部 `textarea` 的 ring 显式清掉。
- 按压是「陷进去再弹回来」：`:active` 用 0 偏移内阴影 `--press-shadow` +
  `translateY(1px) scale(.985)`（图标键 .94）；开关态（全权 / 已装配 / 已置顶）也用
  内阴影表示已经按进去。位移 1~2px、时长走 `--dur-*`，`prefers-reduced-motion`
  全关。
- 列表行级动作一律**容器委托**（会话列表、账户栏、插件列表、建议列表、提交记录、
  工作树），列表重绘不重建行级监听——这是内存口径的一部分。

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

选择旧会话统一提交 `/resume <id>`，由 Core 完成 Engine history replacement。加载更多历史调用专用 Bridge 方法并使用 anchor scroll。提交完成后清空输入（只移除**已发送的那段**——`Submit` 是异步 RPC，往返期间用户可能已继续输入；整框清空会把这段新输入一起吞掉）；运行中提交由 Core 加入队列。

输入框的编辑规则集中在 `gui/frontend/dist/composer-input.js`（纯函数 + `composer-input.test.mjs`），`app.js` 只把 DOM 事件接上去，避免"吞输入"的三个坑各写一遍：

- `clearSubmittedText(current, sent)`：提交成功后只切掉已发送前缀，保留往返期间追加的新输入；
- `shouldRestoreDraft({draft, snapshotComposer, current, focused, dirty})`：整份快照回填草稿正文时，输入框聚焦或**有未落盘的本地输入**（`composerDirty`）一律不回填——后端草稿副本可能更旧，回填等于吞掉刚敲的字；
- `isComposingEnter(event, composing)`：输入法合成中的 Enter（`compositionstart/end` 跟踪 + `event.isComposing` + `keyCode === 229`）不是发送，只有普通 Enter 才 `requestSubmit`；
- `composerViewSwitch({fromSessionID, toSessionID, current, dirty, stash})`：**输入框正文按会话归属**。整份渲染发现视图会话 ID 变了时才动手（同一会话的渲染/事件密集期是 no-op）；离开会话的未发送正文按 ID 留存（LRU 上限 24，空串不占位），进入会话有自己的留存就用它并置脏，没有则清空正文、**清脏位**（留出草稿回填位）；`switched=false` 表示 ID 未变、不碰输入框。

为什么必须有归属这一条：`composerSubmitPlan` 只认「当前视图会话」。正文要是没跟着视图切，在运行中的 A 里写下的插话（或撤回的 A 的排队消息）就会被当成空闲 B 的内容提交出去——「运行中会话污染了空闲会话的输入框内容提交」；而跨会话延续的脏位又会把 B 自己的草稿回填挡在门外（B 的框里显示的是 A 的字）。顺序也有讲究：先归属、后回填（`render` 里 `syncComposerSession` 在 `restoreComposerDraft` 之前）。

草稿的**生命周期与页面可见性**集中在 `gui/frontend/dist/draft-lifecycle.js`（纯函数 + `draft-lifecycle.test.mjs`，不碰 DOM / Bridge）：

- `draftLifecycleFromSnapshot({sessionID, draft, composer})`：由后端权威快照派生本地草稿状态 `{sessionID, attached, text, phase}`。`attached` = 这份草稿挂在"尚未物化的草稿会话"上（只有这种会话的正文允许落盘，见 `application/core/composer_draft.go` `SaveComposerDraft`）；已物化会话不构成草稿归属。
- `draftLifecycle(state, event)`：状态迁移的唯一入口。`append`（框内正文变化）→ `submit`（一轮提交，正文被带走，`remaining` 由 `clearSubmittedText` 算好）→ **`materialize`**（一轮完成/物化提交：已发送的正文归消息、未发送的剩余部分仍是草稿）/ **`cancel`**（一轮被取消：正文按未发送保留，不吞字）→ `clear`（权威清空）。在途（`sending`）期间继续敲字不打断轮次，收尾才回到 `unsent`/`idle`。
- `composerDraftRows({conversation, state})`：会话页要渲染的行 = **既定 message + 本会话当前未发送的草稿行**（末尾一条 `kind: "draft"` / `draft: true` / `unsent: true`）。空草稿不出行；没有草稿归属时不会把别的会话的字留在这里。渲染层用 `kind`/`draft` 区分，不靠文本猜。
- `draftRoundEvent({wasRunning, isRunning, cancelled})`：一轮结束翻译成收敛事件（完成 → `materialize`，被取消 → `cancel`；`cancelled` 由壳层给出——用户点了停止/取消，壳层知道这次结束不是正常收尾）。

后端侧对应：`clearComposerDraft`（`application/core/composer_draft.go`）在草稿物化成功后清空内存 + 落盘，并按**残留状态**收敛掉所有仍以 `Status=draft` 标记该会话的项目键（草稿态改绑工作区会让旧项目键留下幽灵草稿，重启后 `DraftCandidates` 会把已物化会话当草稿槽恢复回页面）。首份未发送正文落盘时登记草稿槽位，会话树里那一行才是"草稿"（目录行的草稿身份只由槽位判定，否则会被叠成 `idle`）。

## 6. 指令模式

实现位置：`gui/frontend/dist/app.js:268-354`、`gui/frontend/dist/app.js:425-473`。

输入前缀（sigil）契约 = 一条前缀一条含义，前端只消费、后端是唯一事实源
（`application/core/completion.go` 的 `SigilCommand/SigilPlugin/SigilSkill/SigilTeam`
与路由表 `application/core/input_router/router.go`）。四个 trigger：

- `/`：**只列可执行入口**——命令与 Skill（Skill 也可用 `$<name>`）。工具不在这里出现：
  工具由模型调用、经权限门，输入框既不执行它、面板也不列它。一个能力要让用户打
  `/名字` 显式调用，前提是它已注册成命令（例如压缩：命令入口 `/compact`，工具入口
  `compact_context` 归模型）。打 `/<工具名>` 时未知命令提示会指出它是模型侧工具，并
  给出同名能力的命令入口（`compact_context` → `/compact`；`/help` 也写明这条口径）；
- `#`：切换 Plugin（含 `#off` = 停用全部）；
- `$`：召回 Skill（激活到当前会话）；
- `@`：手动召唤团队（内置形态 + 团队库条目，装配到当前会话）。写法 `@<团队> [附言]`：
  `@goal-a2a` 只装配（待命）；`@goal-a2a 看看这个 bug` 是**召唤即干活**——装配后把附言
  落成一个 goal（附言 = 目标陈述）并作为一条输入下发，teammate 随本轮开工，目标收口后
  团队离场（删角色注册表 + 复位顺序）。团队里非内置的席位（`tl`/ADVISOR、员工座位）只由
  goal 治理驱动，所以"只装配不落 goal"会停在"在编但没人开工"。
  团队名可以含空格（库条目由用户起名），所以路由**不**按空格切分：整段余量原样
  交给召唤面，按「最长可命中前缀 = 名字、余下 = 附言」解析，最长优先让"更具体的
  名字"赢；全部候选没命中时只报最可能的名字（首个 token），不把用户整句话当名字
  回显，也不下发附言（没装配成功就不该产生输入）。

Command modal 和 inline suggestions 都调用 Bridge.Suggestions，并共享 `renderSuggestionList/acceptSuggestion`。输入内容不在前端执行，选中项只写回 composer，最终仍走 Submit。

前缀改名后旧肌肉记忆（`#review` = 曾经召回 Skill）由后端在"未命中"时补一句迁移
提示（`sigilMigrationHint`），不静默兜底：`#review` 报"未知 Plugin"并指出 `$review`。

键盘规则：ArrowUp/Down 移动，Tab 接受内联建议，Enter 提交或接受面板项，Escape 关闭，Ctrl/Cmd+K 打开命令面板。

异步 inline suggestions 使用 `inlineRequest` 序号拒绝旧请求结果，避免快速输入时结果倒序覆盖。

团队面板的更新由后端的 `team.changed` 事件驱动（会话级、载荷为空、revision=0）：面板数据
（成员表/顺序/调度）不在会话快照里，两个前端都按需 RPC 拉取并按会话键缓存，所以后端在
装配/顺序/入职/编辑成员后发一条通告，前端作废缓存并在面板可见时重取（`invalidateAgentTeam`），
不可见时只置脏位、等下次展开。此前 GUI 靠 composer 文本以 `@` 开头来猜，漏掉 goal 自动装配、
面板收起时的召唤等来路；TUI 只能靠用户再按一次 Alt+T。

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
