# GUI / Agent Workbench 设计变更记录

本文件记录会改变模块边界、跨模块契约、兼容性、持久化或运行流程的重要设计。纯文字修正不记录。

## 2026-09-27

### Fixed

- **顶栏两个诊断徽标此前从不渲染：`elements` 注册表缺 `perf-badge-host` /
  `live-diag-host`，两处 `if (elements["..."])` 永远为假。** 数据一直在采
  （`window.__seelexPerf` 的样本环、`__seelexLiveDiag` 的缺口计数都在），但渲染进程
  自己看不见——"谁在吃内存 / 事件有没有缺口"的口径被一个注册表漏项静默掐掉。
  补注册表，并加 `element-registry.test.mjs` 把这类静默失效钉住：`elements["x"]` 用到的
  key 必须都注册、注册的 id 必须都在 `index.html` 里、两个徽标宿主必须真的被 append。

### Changed

- **重型 vendor 从"启动同步加载"改为"首次真用到时注入"。** PDF.js（368KB）、
  docx-preview（69KB）、xterm.js（277KB）+ addon-fit 过去都在 `index.html` 尾部同步
  加载，任何一次启动都为它们付 parse/compile 与内部缓存的内存，哪怕用户从不打开对应
  预览/终端。现在统一走 `vendor-loader.js`：首次走到该类型预览（或第一次真的开终端）
  才注入；同一 src 并发共用 in-flight、失败/超时不缓存可重试、已就绪直接返回（与
  "仍同步加载"的写法兼容）。终端刻意保持**同步启动语义**：组件已就绪（第二次起，以及
  注入了 `createTerminal`/`createFit` 的调用方）走同步路径，只有首次开终端才 `then` 到
  `openTerminal`——`terminal-panel-controller.test.mjs` 那条「Open 未返回时先到的输出
  要按 id 暂存」就是这条语义的护栏（第一版把它改成 `async newTerminal()` 时确实红了）。
  仍同步：highlight 核心+语言包、marked、DOMPurify、xterm.css。
- **诊断钩子自身不再制造开销。** `perf-hooks.domNodes` 由 `querySelectorAll` 全量快照
  改为 `getElementsByTagName` 的 live `length`（它被 10s 轮询与每次 `markRender` 调用）；
  `start`/`stop` 幂等，`stop()` 清掉轮询定时器，视图重建不再叠表。
- **终端回滚缓冲 5000 → 2000 行**（xterm 每个实例一份，按标签、按后端会话各留一份）。

### 暂不改（记下判据，免得下一个人重做同一份分析）

- **不为会话行加 `content-visibility: auto`。** `conversation-wheel.rowsFromDOM()` 逐个测
  真实 `getBoundingClientRect()`（注释写明"不靠模型里的估算值"），会话锚点恢复又用
  `container.scrollHeight` 增量；被跳过渲染的行只报 `contain-intrinsic-size` 占位高度 →
  轮轴刻度比例偏、行首次渲染时容器高度突变可能顶动滚动。要做得和窗口化同一批。
- **不给 `htmlByKey` 加条数上限。** 它已按 desired key 剪枝到"窗口内的行"；上限若小于
  窗口，未命中的行每帧都会被判成"html 变了"→ 走 `reconcile` 的 `replaceWith` 整行重建，
  抖动反而放大。真正的杠杆是**物化行数**的上限（窗口化），不是缓存条数。
- **GPU 常驻不是模糊/图层提升造成的。** 现皮肤已把 `.topbar`/`.left-panel`/`.right-panel`
  的 `backdrop-filter` 覆盖成 `none`，全仓无 `will-change`、无 `contain:`，只有 `.modal`
  打开时才有全窗 `blur(8px)`。要压它得压"失效面积与层数"（窗口化 + 就地更新）。

### 验证与生效

- 前端口径：`cd gui/frontend/dist && node --test` → 485 passed / 0 failed。
- 前端是 `embed.FS` 打进二进制的：以上改动**重建 GUI 后**才生效；实机前后对比需重建后
  由顶栏徽标（DOM 节点 / JS 堆 / 快照体积，10s 一条）给出基线。

## 2026-09-23

### Added

- **一轮上下文压缩在右栏边走边报进度，生命周期只有一轮。** 前端消费新事件
  `compaction.progress`（会话级、`revision=0`、载荷不进快照，与 `team.changed` 同口径）：
  起手帧（`phase=begin`，仅显式压缩）→ 六关收口（判据估算→装配→替换 provider 历史→渲染帧→
  存帧→写记录）→ 恰好一个终局。渲染复用 Plan 面板同款轨道 `.plan-board-progress` /
  `.plan-board-bar`，**不自造第二套进度组件**；轨道下挂逐关耗时清单——整轮只有几十毫秒，
  进度条不可能被肉眼看出"在走"，能回答"它干了什么、慢在哪一关"的只有每关自己的墙钟。
  逐帧累计是纯函数 `compaction-format.mergeCompactionProgress`；撤条由视图侧定时
  （终局后 2.5s、失败 6s）。这条 kind 刻意**不**进 `BUFFERED_INCREMENTAL_KINDS`
  的 120ms 尾随合并：latest-wins 会把中间关吃掉，只剩首尾两帧，而"走到哪一关了"正是它
  唯一的信息。契约见 `modules/right-sidebar.md` §上下文压缩，
  动因与 PROBE 见 `docs/devlog/2026-09-23-compaction-progress-visible-from-first-instant.md`。
- **折叠帧正文可以在面板里直接读，不再是一句占位话。** 压缩记录条目上的「查看帧正文」经
  `Bridge.ToolResultContent(frame_ref, offset, 12000)` 按 ref 分页读回正文（正文不进快照，
  与工具大输出同一条读回路径），复用轨迹详情的同一容器与分页交互
  （`.axis-detail` / `data-compact-frame-load`），不另开面板；没有 `frame_ref` 的条目直说
  「本次没有可回读正文」。同一条记录的展示口径（区间/原因/来源）此前在右栏与轨迹「压缩」轨
  各写一份、已经漂成两种读法，现收敛到 `compaction-format.js` 一处。见
  `docs/devlog/2026-09-23-compaction-frame-visibility-and-panel-drift.md`。

## 2026-09-20

### Fixed

- **运行中会话不再吸走本该发给另一个会话的输入。** composer 提交此前走 ambient
  `Submit`：它由后端按「当前视图会话」路由，而 `ResumeSession` 的切换在途存在
  TOCTOU 窗口（后端视图指针已动、渲染层还没拿到目标快照）。「一个会话在跑、我想
  发给另一个空闲会话」的输入因此可能落进运行中会话的队列
  （`application/core/session_scope.go` 的 `SubmitToSession` 注释记录的压力场景：
  `TestStressConcurrentSessionsDoNotPollute` 抓到 queued-2 进 sess-4 视图）。
  现在普通对话输入经 `SubmitToSession(<当前视图会话 ID>, text)` 显式路由——会话 ID
  在 RPC **之前**取，绝不重读后端 current；sigil 输入（`/` `#` `$` `@`）仍交回后端
  输入路由器（前缀→用例的映射是路由器的职责，输入区锁覆盖切换在途窗口）。规则住在
  `composer-input.js` 的 `composerSubmitPlan`（纯函数，`node --test` 覆盖），口径见
  `modules/multi-session-pages.md` §6「新前端一律使用显式 session ID」。
- **输入框正文按会话归属：运行中会话里未发送的字不再被当成空闲会话的提交内容。**
  路由钉对了会话还不够——输入框正文此前是全进程共享的一份：在运行中的 A 里写了插话
  （或撤回了 A 的排队消息），切到空闲的 B 后字还留在框里，下一次 Enter 就把它当成 B
  的内容发出去。同一处脏位（`composerDirty`）也跨会话延续，把 B 自己的草稿正文挡在
  `shouldRestoreDraft` 门外（用户看到 B 的框里是 A 的字）。现在正文按会话归属：
  `composer-input.js` 新增 `composerViewSwitch`（纯函数，LRU 上限 24），app.js 的整份
  渲染先对齐正文归属与脏位、再谈草稿回填；切回原会话时属于自己的未发送正文还在。

### Added

- **工作区更改面板（资源管理器子页第三块）**：未提交改动此前落在两块面板的夹缝里——
  工作树是当前磁盘快照（无状态），提交记录是已入库历史，而"我改了什么"两块都不说。
  新增 `Bridge.WorkspaceChanges(limit)` → `application.Service.WorkspaceChanges` →
  `WorkspaceTreePort.GitChanges` → `workspace.Repo.GitChanges(root, limit)`，前端新模块
  `gui/frontend/dist/workspace-changes.js` + `code-pane[data-pane="changes"]`
  （接进 `CODE_PANES`，与另两块面板同族的拖拽换序/按需刷新）。

  只读元数据边界与另两块一致：状态字符、路径、重命名原路径、计数，**不含 diff、补丁
  或文件内容**；命令固定 argv、不经过 shell、带 8s 超时、`--no-optional-locks` 不刷新
  索引；非 git 仓库以 `Result.Error` 返回展示文案。三处容易做歪的地方在后端一次收敛：
  （1）`-z` 而非默认输出——默认 porcelain 会把中文/空格路径写成 `\344\270...` 转义序列，
  `-z` 原样返回且重命名是「新路径 NUL 旧路径」两条记录；（2）路径基准——git status 以
  仓库根为基准，面板以工作区根为基准（绑定目录可能是仓库子目录），按
  `rev-parse --show-toplevel` 剥前缀，工作区之外的兄弟路径丢弃并计入 `Filtered`；
  （3）`-- .` 把范围钉在工作区子树内，否则 `-C 子目录` 仍会带出工作区之外的改动。
  可见性边界与 `ListTree`/`ReadFile` 一致（敏感文件名过滤 + 面板显式提示「另有 N 条未
  展示」，不静默）；目录噪音刻意交给 git 自己的 `.gitignore`——套用 `ignoreDirNames`
  会连被跟踪的 `dist/` 改动一起藏掉。`Total` 与四个计数覆盖过滤后的全部条目（含被
  `limit` 截断的部分），头部说的是工作区状态而不是本屏行数。

  见 `docs/devlog/2026-09-20-workspace-changes-pane.md`；契约写在
  `docs/gui/modules/right-sidebar.md` §资源管理器与 `workspace/README.md`。

- **细节动效（拟物）**：新增 `gui/frontend/dist/motion.js`（纯规则 + 只读 DOM 应用器），
  把三类"细节"接进真实界面：
  - **数字变更**：计数（工作表格 `N 项` / `N 打点` / 类型·会话·实发计数 / 未读角标）
    从旧值滚到新值（里程表；`is-rolling` 期间提一档色温并用 tabular-nums 防抖）；
  - **增量入场**：**首次插入**的表格行 / trace 展开行闪一道黄铜底、会话与轨迹条目
    自下"抽出"；keyed reconcile 的内容替换不重播（类由 JS 一次性挂上，避免流式更新里
    反复闪）；
  - **滚动边缘与横向滚轴**：限高滚动块滚到中间时上/下浮出内阴影、到边即隐；sheet 栏与
    页签栏的纵向滚轮翻译成横向滚动、按住拖动即"拨滚轴"（拖动过阈值才算，随后那次 click
    被吞掉，不会误切页签）。
  全部遵守 `prefers-reduced-motion`（减少动效时不接管滚轮/拖动、数字直接落终值）。

### Changed

- `.excel-sheets`（工作表格批次 sheet 栏）由 `flex-wrap: wrap` 改为横向滚动条
  （`nowrap` + `overflow-x: auto`）：批次多了不再把弹窗撑高，滚轮/拖动可翻。
- `prefersReducedMotion` 收敛为一处实现（`motion.js`）：app.js 不再自带一份，
  减少动效判据只有唯一事实。

## 2026-09-17

### Added

- 新增会话级事件 `team.changed`（载荷为空、revision=0）：装配/工作顺序/入职/编辑成员后
  由后端发布，Agent Team 面板据此作废缓存并重取。此前面板只能靠调用方自觉刷新（GUI 靠
  composer 文本以 `@` 开头猜、TUI 靠用户再按一次 Alt+T），漏掉 goal 自动装配与面板收起时
  发起的召唤。见 `docs/gui/decisions.md` ADR-GUI-022。

### Fixed

- `@` 召唤团队不再把名字后面的文字吞进团队名：`@goal-a2a 这次启动团队…` 此前
  整句去查团队库，回执是「未知团队: goal-a2a 这次启动团队…」且那句话被静默丢弃。
  现在路由仍原样下发整段余量（团队名可以含空格），切分改按「最长可命中前缀 = 名字、
  余下 = 附言」在 `application/core/input_team.go` 完成；命中后附言按
  `$<skill> <args>` 的同一条口径作为一条输入下发（用户原文进入会话）。未命中时
  只报最可能的名字（首个 token）。用户可见行为：`@<团队>` 只装配（不变）、
  `@<团队> <附言>` 装配 + 把附言作为输入。口径见 `modules/shell-and-interactions.md`
  第 6 节、ADR-GUI-021。

### Changed

- 输入前缀（sigil）改为**一字符一含义**：`/` 命令与工具、`#` 切换 Plugin、
  `$` 召回 Skill、`@` 手动召唤团队。此前 `#` 是 Skill、`@` 是 Plugin；`@` 让位给
  「手动召唤团队」是因为团队此前只能由 goal 上线时自动装配，人没有显式入口。
  前端 `SIGILS`/`SIGIL_PATTERN`、面板 `data-trigger`、composer 占位提示同步更新；
  旧前缀不静默兜底，未命中时后端补一句迁移提示（`#review` → "召回 Skill 用 $review"）。
  决策与后果见 `docs/gui/decisions.md` ADR-GUI-021，模块口径见
  `modules/shell-and-interactions.md` 第 6 节。

- `@` 召唤从"只装配"变成"召唤即干活、干完就走人"：带附言的召唤（`@<团队> <附言>`）
  在装配后把附言**落成一个 goal**——团队里非内置的席位（`tl`/ADVISOR、员工座位）
  只由 goal 治理驱动，只装配不落 goal 会停在"在编但没人开工"（用户实测：`@goal-a2a …`
  后 tl 的角色会话 `total_rows=0`）。目标收口（栈里没有 active goal）后团队**离场**
  （删角色注册表 + 复位顺序，角色会话子树保留）。不带附言仍是只装配（待命）。
  见 `docs/devlog/2026-09-17-summon-starts-goal.md`、ADR-GUI-021。

## 2026-09-13

### Fixed

- 修复"同一批次还有子代理在跑时详情打不开"：详情入口此前只从 Plan DSL /
  子代理树投影解析节点，而树只由整份快照与 `runtime.changed` 携带，表格行却
  先经 `worktable.changed`/`task.changed` 到达，`resolveNodeForDetail` 取不到
  节点便静默返回（点「详情」无任何反应），回合结束整份刷新后才恢复。前端入口
  改为 Plan DSL → 子代理树 → 工作表格行（`workItemToDetailNode`）→ 仅身份
  节点逐级兜底；后端让 `worktable.changed` 在树内容变化时附带 `subagent_tree`
  （发送侧按内容签名去重，空数组表示已清空，缺省/null 表示保留既有树），
  前端 reducer 同步 `snapshot.runtime.subagent_tree`。payload 契约见
  `schemas/work-table.schema.json`；A/B 阈值与基线见 `docs/test/worktable-ab.md`。

## 2026-09-07

### Changed

- 会话区子页与右栏子页改为统一停靠布局：对话/轨迹（会话区）与状态/工作台/
  资源管理器（右栏）共五个视图，默认主视图两页、右栏三页；页签点击切换，
  同栏拖拽换序，跨栏拖拽与目标页签置换（拖入页成为目标栏激活页，原栏激活页
  由移入页接管）。布局与激活持久化到 `seelex.dock.v1`（旧 `seelex.right.tab`
  只作一次迁移读取），纯演算下沉到 `gui/frontend/dist/dock-layout.js`
  （`normalizeDockState`/`swapViews`），由 `app.js` 统一渲染 DOM 归属、页签条、
  激活钩子与会话专属悬浮件显隐；不新增后端/Bridge 契约。
- `index.html` 会话区重构为 `#main-host`（内含 `#conversation-shell` 与
  `#trajectory`），右栏三面板收进 `#right-host`；面板随布局在两个宿主间移动，
  会话局部状态（滚动/过滤/展开）随 DOM 迁移保留。聊天区 chip 跳轨迹与文件
  预览打开都改为先 `revealView` 让目标子页在所在栏激活。
- 拖拽可靠性修复：跨栏置换的 `drop` 改为下一帧再重建页签条，并增加全局
  `dragend` 兜底清理——避免 drop 同步删除拖动源导致 WebView2 收不到
  `dragend`、残留拖拽鼠标状态（表现为拖拽后文件预览/目录展开等点击失灵）。

### Added

- `gui/frontend/dist/dock-layout.js` + `dock-layout.test.mjs`：默认分区、
  同栏换序、跨栏置换、脏存储收敛与持久化 round-trip 单测。

### Design decisions

- 主视图固定两个页签、右栏固定三个页签（置换不改变两栏容量），避免空白栏与
  输入框归属歧义；输入框只在主视图处于会话类子页时显示，防止遮挡全宽工作台。

## 2026-08-16

### Changed

- 会话树折叠箭头方向修正：展开组显示下箭头（∨）、折叠组显示右箭头（›），
  与账户/更多面板的 `<details>` 箭头语义一致（`styles.css`）；此前旋转角度
  与状态接反，导致展开状态误显示 `>`，点击反而折叠隐藏整组会话。
- 左侧栏工作区/会话条目尺寸与对齐统一：条目内边距收敛为 6px 10px；工作区
  列表条目补齐 folder 图标并与会话条目共用 `.entry-name` 布局，详情行缩进
  统一到图标文本起点（30px），工作区列表间距与会话列表一致（gap 3px）。
- 新建会话流程防御：`BeginNewSession` 后若首轮快照因异步目录刷新竞态返回
  空 `sessions`，保留上一次可见会话列表并安排一次权威重拉，避免左侧栏会话
  「全部消失」的假象（`app.js`）。
- 定时任务面板从「周期任务」更名为「定时任务」，新建弹窗增加「执行方式」：
  - **周期重复**：原「每 n 小时/天/周/月」路径不变；
  - **定时执行（一次性）**：`datetime-local` 选择执行时间，提交 `runAt`
    （RFC3339），后端校验晚于当前时间、创建即启用、执行后自动停用并保留
    记录（`index.html` + `app.js` + `scheduled-tasks-view.js`）。

### Added

- 调度器一次性定时任务：`dto.ScheduledTaskSpec` 新增 `RunAt`，
  `ScheduledTaskStatus` 新增 `run_at`/`one_shot`；`nextScheduledAt` 优先
  返回 `RunAt`，执行完成后自动停用并清除下次排期
  （`seelebridge/scheduler/scheduler.go` + `scheduler_test.go`）。
- 定时任务面板一次性任务展示：任务行显示「定时 MM-dd HH:mm」与「一次性」
  chip，空态文案统一为「暂无定时任务」（`scheduled-tasks-view.js` +
  `scheduled-tasks-view.test.mjs`）。

### Design decisions

- 一次性任务不支持「创建后停用」（后端创建即启用），避免出现无法再次启用的
  死胡同；执行后保留记录供面板查看，用户可手动取消移除。

## 2026-08-15

### Changed

- 依据 `design-taste-frontend`（taste-skill）完成第二轮前端精修
  （`gui/frontend/dist/styles.css` + `index.html` + `components.js` + `plan-dsl.js`）：
  - 修复顶部连接状态点无样式问题：新增 `.status-dot` / `.status-dot.online`
    （`chat-view.js` 始终追加 `online`，此前整条规则缺失导致指示点隐形）；
  - 移除界面 emoji：并行分支箭头 `⚡→` 改为 `⇉`，子代理工具消息的 `🛠`
    改为内联 SVG（`node-msg-tool-icon`）；终端状态字形（`✓ ✕ ○ ●` 等）保留；
  - 字体栈移除 `Inter` 首选项，改用 Windows 原生 `Segoe UI Variable` +
    CJK 回退（桌面应用不再默认加载通用 web 字体）；
  - 硬编码色值收敛：新增 `--text-bright/strong/soft/mid/dim`、
    `--code-bg`、`--tint-running/done/failed/info`、
    `--border-running/done/failed` 派生 token，组件内 160+ 处硬编码色
    替换为 token（保留 effort 四档渐变、风险金、链接蓝等语义色）；
  - 交互触觉反馈：按钮 `:active` 统一 `translateY(1px)` 按压位移；
  - 动效参数 token 化：`--ease-out`（cubic-bezier(.16,1,.3,1)）与
    `--dur-fast/med/slow`，过渡统一走 token，`prefers-reduced-motion`
    覆盖新增过渡项；
  - 工作表格入口的 `▣` 字形改为 `data-icon="table"` SVG（ICONS 注册表
    新增 `table`）；补全 `new-scheduled-task`、`new-workspace`、
    `perm-toggle` 的 `aria-label`。
- 使用习惯与文案一致性修正（`app.js` + `index.html` + `styles.css`）：
  - 中文输入法（IME）合成期间按 Enter 确认候选词不再误发送
    （`event.isComposing` 守卫）；
  - 发送（含失败路径）后焦点回归输入框，连续输入无需重新点击；
  - 「解除项目绑定」增加确认弹窗，与删除会话/取消定时任务保持一致；
  - 账户列表空态补「暂无账户」占位（与会话空态对齐）；
  - 界面标签统一中文：Sessions→会话、Accounts→账户、Project→项目、
    Unbind project→解除项目绑定、初始状态 Ready→READY；
  - 圆角收敛：新增 `--r-2xl`（14px）token，tool-run 由 10px 归一到
    `--r-xl`，composer/modal-card/空状态 orb 统一 `--r-2xl`。

### Added

- 工具 in/out 面板支持展开/收回切换（`conversation-view.js` 把原「展开后移除
  按钮」改为可折叠 toggle，`prefers-reduced-motion` 不受影响）。
- 会话树：左侧栏会话按工作区（`session_workspaces`）分组渲染，未绑定/已消失
  工作区的会话收进「未关联会话」组并置底（`app.js renderSessionGroups`）。
- 账户区改为可折叠 `<details>`（`collapse-section`，无需 JS 状态）。
- 左右栏手动调宽：`app-shell` 改为
  `var(--left-w) 6px 1fr 6px var(--right-w)` 五列网格，拖拽/键盘调整并
  localStorage 记忆（`--left-w` 200–420px、`--right-w` 220–480px）。
- 工作区绑定/新建从右栏 `#side-more` 移到左侧栏（工作区 section），左侧栏
  现支持新建会话与新建工作区；右栏保留项目/状态/工作表格/周期任务常驻。
- 周期任务配置：`周期（分钟）` 改为「每 n 小时/天/周/月」组件
  （`sched-period-value` + `sched-period-unit`）。
- 后端 `ScheduledTaskSpec/Status` 增加 `period_unit`/`period_value`；
  调度器支持日历月周期（`addCalendarMonths` 月末钳制），小时/天/周为
  固定时长；旧任务以 `interval_seconds` 回退展示
  （`seelebridge/scheduler/scheduler.go` + `scheduler_test.go`）。
- 会话树工作区组可折叠：点击组头收起/展开其下会话（`app.js`
  `toggleWorkspaceGroup`，折叠状态 localStorage 记忆，重渲染保持）。
- 全局去线框：badge/chip/icon-button 默认去边框（hover 再出现），
  status-item/project-overview/ws-current/sched-item/history-search-hit/
  node-detail-meta/用户消息气泡等卡片去边框，改用底色或留白分组。

### Design decisions

- 语义色（含 Effort `max` 紫、subagent 淡紫）保留：四档/多阶段状态是
  功能性编码而非装饰性 AI 色调，继续遵守 `docs/gui/modules/effort-control.md`
  的「克制、无光效」契约；反 emoji 只针对装饰性 emoji，终端字形保留。
- 颜色 token 化以「常用中性色 + 状态底色/描边」为界，避免对一次性
  hover/警示微调色过度抽象导致视觉回退。
- 「每 n 月」必须走日历月语义（月末钳制），前端只负责把 `period_unit/value`
  透传给后端，不把月份近似成 30 天。
- 去线框的边界：主面板与内容面去框、以间距和字阶分层；work table/Plan/
  节点详情等数据密集视图保留细边框作为数据分隔（密度需要）。

## 2026-08-14

### Changed

- 前端视觉系统重做（`gui/frontend/dist/styles.css` + `index.html`）：
  - 建立 `:root` token 层（表面色/文本色/品牌色/语义色/字阶/圆角/间距），
    同义状态收敛到 `--status-running/done/failed/info`，组件不再硬编码色值；
  - 右栏信息层级主/次拆分：Project/状态/工作表格/定时任务常驻，
    历史检索/概要/工作区/Agent 已读文件收进 `#side-more` 折叠区；
  - Effort 控件改为紧凑分段滑轨，移除针筒装饰、紫色辉光与常驻动画；
  - 动效收敛：只保留 `runtime-spinner` 加载指示，移除扫光/连点/呼吸动画；
  - 最小可见字号提升到 9.5px、正文不低于 12px，`--faint` 对比度达标；
  - 空状态改为终端开场（`>_` + 命令提示），品牌标记统一为品牌色。
- 同步更新 `docs/gui/modules/effort-control.md`（视觉契约改为「克制、无光效」），
  `gui/frontend/README.md` 增加视觉设计系统章节。

### Design decisions

- 纯视觉改版不动 Bridge API 与 Snapshot/Event Schema；`gui/bridge_test.go:641-652`
  的 Effort 常驻 composer 契约保持不变。
- 语义色以 `:root` token 为唯一事实来源；新增组件必须复用 token，禁止新增同义色。
- 未来如需浅色模式，只需在 token 层扩展 `data-theme`，不改变组件结构。

## 2026-08-09

### Added

- 新增 `worktable.changed` 轻量增量事件与 `schemas/work-table.schema.json`
  （工作表格行 + 任务打点，只带表格不整份 runtime）。
- 新增 Work Table 模块（`modules/work-table.md`）：右侧工作台统一表格视图，
  替换 Plan 树 / 待办 / 子代理三个分区；节点详情弹窗保留。
- `application/core/work_table.go`：plan/todo/subagent → 扁平 WorkItem 读模型
  （有界：`work_table_rows` / `plan_node_events` / `evidence_chars`）。
- `application/core/worktable_publisher.go`：CSP 汇聚发布器（latest-wins，
  生产者阻塞背压，关闭排空尾态）。
- `seelebridge/todo_tool.go`：todoState 改为 Actor + Mailbox；TodoItem 增加
  三态 `status`（pending/doing/done），`Done` 为派生字段；新增
  `Runtime.SetTodoStatus`（不新增工具族）。
- Bridge 新增 `UpdateWorkItemStatus`（v1 仅 todo 三态）。
- 前端 `work-table.js`：条目展开多维表格、筛选、行内打点、todo 三态按钮、
  plan/subagent 详情入口；keyed reconciliation + html 缓存（只重建变化行）。
- 前端 reducer 内存优化：`runtime.changed` 不再预克隆 plan；
  `subagent.*` 改为路径级结构共享（`protocol.js`）。
- 子代理树生命周期被动触发：`SetSubagentTreeObserver` → 
  `Service.RefreshWorkTableSnapshot`，fork 注册/完成自动发布
  `worktable.changed`（无需模型调用工具）；done 节点有界保留
  （`subagentTreeRetainDone`=50，超限清最旧，`ClearSubagentTree` 显式清空）。
- worktable task 状态体系：task 就是 worktable 条目，单一 task 注册表
  （Actor + Mailbox，保护粒度=task）；todolist 融合为 kind=todo 的 task
  （打点表，无独立 todo 实体）；主动 `taskadd` 工具 + 被动 plan/subagent
  生命周期同步（CSP channel 通知）；retry 状态 + retry_count；幂等去重
  （归一化 goal 精确键 + 提示词约束 + 可注入审判钩子，B6 装配件只绑
  task_id）；`task.changed` 逐任务增量 + `worktable.changed` 结构增量；
  task 快照随 SessionRecord 复用 stack 存储（T4）。

### Changed

- 右侧工作台 `index.html`：`plan-section` / `subagent-section` / `todo-section`
  合并为 `work-section`（`work-table-view`）。
- 运行上限新增 `limits.work_table_rows`（默认 200）。
- `event.schema.json` 的 `kind` 允许 `worktable.changed`（pattern 兼容）。

### Design decisions

- worktable.changed 只携带表格投影，不整份 runtime；revision 与 items 在同一
  临界区生成。
- plan/subagent 状态由执行器权威管理，手动状态更新仅开放 todo 三态。
- todolist 保持 harness 默认工具族，三态只经 `SetTodoStatus` 落地。
- 工作表格是子代理的被动观察者：数据面只依赖后端生命周期投影，不依赖
  模型主观意愿打点。
- 事件语义按责任链拆分：task 内部变更 → task.changed；worktable 增删 →
  worktable.changed；todo 状态在 task 注册表内流转（todo:done 映射）。

## 2026-07-24

### Added

- 建立 `architecture.md` 作为 GUI/Agent Workbench 权威总体架构入口。
- 建立机器可读 `module_dotting.json`，登记模块状态、职责、接口、实现路径、输入输出和依赖。
- 冻结 protocol v2 的 Snapshot、Event、Page、Error、Card 和 Generation Manifest JSON Schema。
- 增加与 Schema 对应的可执行示例和 Go 契约测试。
- 定义规划中的 HTTP API、安全、分页、错误、幂等、条件请求和 Snapshot 语义。
- 定义 generation 提交、回滚、重建和故障恢复 recipes。
- 增加 Generation Repository 与 HTTP API Adapter 模块详细设计。
- 增加证据门禁驱动的需求到 Dev 自迭代模块、运行 recipe、Evidence Assessment 与 Dev Iteration Schema/示例。

### Changed

- `docs/arch/agent-workbench-architecture.md` 降为方案推演材料；字段、依赖和发布语义以 `docs/gui/` 为准。
- 规划模块的总体架构链接统一指向 `docs/gui/architecture.md`。
- 明确当前实现仍为 protocol v1/单 Engine；v2/多 SessionActor/HTTP/generation repository 均为规划状态。

### Design decisions

- Event sequence 改为 per-scope，不采用全局高频序列。
- 大型 Workspace/历史数据使用 cursor Query Page，不进入 Workbench Snapshot。
- generation 采用不可变资源目录、manifest hash 和原子 current 指针，不允许原地覆盖。
- HTTP 与 Wails 是并列 adapter，共享 Application ports，但不共享 transport DTO。
- 模块依赖必须为 DAG，并纳入自动化验证。
- RAG 从辅助上下文升级为工程证据获取机制；在线使用 evidence readiness，低证据条目不删除，E2E 反馈按需求/架构/详设/Dev/Test 层精确重开。
