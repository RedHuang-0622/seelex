# GUI / Agent Workbench 设计变更记录

本文件记录会改变模块边界、跨模块契约、兼容性、持久化或运行流程的重要设计。纯文字修正不记录。

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
