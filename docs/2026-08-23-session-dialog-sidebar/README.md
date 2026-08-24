# 左侧栏重构 + 右栏 Excel 化（打点表 v2）

> 性质：一次性工作包（plan + 打点表，v2 按用户 2026-08-23 补充需求修订）。
> 范围：GUI 前端 `gui/frontend/dist/`（index.html / app.js / work-table.js /
> styles.css / 相关 test.mjs）。**纯前端改动，无后端契约变化**。

## 1. 需求清单（v2，按用户补充修订）

| 编号 | 需求 | 实现拆解 |
|---|---|---|
| R1 | 左侧栏删除「工作区」列表区块 | 移除 `.workspace-section`（workspace-info/workspace-list/new-workspace）；新建工作区入口并入新建会话弹窗 |
| R2 | 左栏保留：新建会话（加号）+ 已有工作区分组头加号 + 会话列表 + 可折叠账户 | 会话列表按工作区分组；分组头「+」= 在该工作区新建会话；账户 `details` 保持 |
| R3 | 新建会话弹窗二选一：新建工作区会话 / 新建任务会话 | 第一步两个选项；第二步（工作区会话）列出已有工作区 + 选择文件夹新建 |
| R4 | 新建工作区会话支持选择文件夹 | `PickDirectory → CreateWorkspace → refresh → 按 root_path 反查 id → BindWorkspace → BeginNewSession` |
| R5 | 会话支持置顶与删除 | 行内置顶按钮（localStorage `seelex.pinned-sessions`），置顶会话列表置顶显示；删除已有 |
| R6 | 会话标题显示前 5 个字省略 | `truncateTitle(name, 5)`（Unicode 码点截断 + 「…」） |
| R7 | 右栏结构：项目 → 可折叠状态（默认折叠）→ 工作表格 → 定时任务 → 历史检索/工作树 | 状态区为 `<details>`（默认关闭），内含 status-grid + 概要；历史检索/工作树保持不动 |
| R8 | 工作表格弹窗改为真正多维 Excel 表格 | `<table>` 网格（固定表头/列），批次切换 = 底部 sheet 页签（类 Excel），类型筛选，替代 div 堆砌 |
| R9 | 定时任务同样采用 Excel 表格表达 | 右栏「定时任务」区改为入口按钮 → 弹窗内 `<table>` 展示（新建入口复用现有表单弹窗） |
| R10 | 弹窗支持拉伸 | `.modal-card` 右下角 resize 手柄（pointer events 调宽高），至少覆盖工作表格/定时任务弹窗 |
| R11 | 统一低圆角扁平化 UI | 圆角 token 整体调低（2/3/4/6/8px 档），pills→低圆角，shadow 收敛；组件统一走 token |

## 2. 现状要点（代码证据）

- 左栏 `#new-session` 直接 `invoke("BeginNewSession")`（app.js ~L892）；
  工作区区块独立于会话区块。
- 右栏：项目 → 状态 → 工作表格 → 定时任务 → `#side-more`（历史检索 →
  概要 → 工作树）；「概要」藏在折叠面板。
- 工作表格：`work-table.js`（572 行）用 div 网格 + 批次段落堆叠渲染；
  `work-table.test.mjs` 断言旧 HTML 结构（需同步更新）。
- 定时任务：`renderScheduledTaskPanel` 内联列表渲染（app.js），无独立模块。
- `gui/bridge_test.go` L670-731 约束：右栏必须含 `project-status` /
  `worktree-view` / `file-count` / `work-table-open`；工作表格弹窗挂
  `work-table-modal-view`；脚本须含 `session.name || shortSessionID(session.id)`
  子串（截断函数包裹后子串仍在）与 `invoke("BeginNewSession")`。
- CSS 圆角 token：`--r-sm:4 / md:6 / lg:8 / xl:12 / 2xl:14px`，另有大量
  `999px` pills 与 `50%` 圆点。

## 3. 改动范围（文件 + 改动点）

### 3.1 `gui/frontend/dist/index.html`

1. 左栏：删除 `.workspace-section`；会话区保留；账户 details 保留。
2. 右栏：`#project-status` 区块改为 `<details id="status-panel">`（默认不
   open），内含状态网格 + 概要（`#project-overview` + `#context-compactions`）；
   工作表格按钮区不变；定时任务区改为「打开表格」按钮 +
   `#scheduled-table-modal`（表格容器 + 新建按钮）；`#side-more` 只保留
   历史检索 + 工作树。
3. 新增 `#new-session-modal`（两步：任务会话/工作区会话 → 工作区列表 +
   选择文件夹）。
4. 弹窗 `.modal-card` 追加 `data-resizable` 与右下角手柄元素
   （工作表格、定时任务、新建会话三个弹窗统一处理）。

### 3.2 `gui/frontend/dist/app.js`

1. elements 声明：删除 `workspace-info`/`workspace-list`/`new-workspace`，
   追加 new-session-modal 各元素、scheduled-table-modal 各元素、status-panel。
2. `renderWorkspace` 删除（不再渲染左栏工作区列表；绑定/解绑逻辑移到
   新建会话弹窗与项目头部）。
3. 新建会话流程 `startNewSession()` + 三个入口（任务会话 / 已有工作区 /
   选择文件夹）。
4. 会话置顶：`togglePin(sessionID)` + localStorage；`sessionRow` 加置顶
   按钮与状态；渲染时置顶会话置顶显示（组内排前）。
5. `sessionRow` 标题套 `truncateTitle(..., 5)`（子串兼容 bridge_test）。
6. 分组头改造 + `[data-workspace-new-session]` 绑定。
7. 定时任务：右栏按钮 → 打开 `#scheduled-table-modal`，内部以 `<table>`
   渲染 `renderScheduledTasks`（改为表格行）；新建按钮复用现有表单弹窗。
8. 弹窗拉伸：`initModalResize()` 通用处理器（pointerdown/move/up）。
9. 关闭注册循环追加新弹窗。

### 3.3 `gui/frontend/dist/work-table.js` + `work-table.test.mjs`

- 渲染层重写为 Excel 风格：
  - 工具栏：展开开关 + 类型筛选 chips（全部/Plan/Task/Todo/Subagent）。
  - `<table class="excel-grid">`：固定表头（类型/任务/描述/状态/Assignee/
    依赖/附件/打点/操作），行 keyed reconciliation 沿用。
  - 底部 `#work-sheet-tabs`：每个批次一个 sheet 页签（含计数），点击切换
    当前批次（「全部」页签居首）；批内分页保留。
- 保留导出 API：`workTableView` / `countUnread` / `workTableSignatures` /
  `pageCount` / `pagedRows` / `createWorkTableView`；`renderShellHTML` 改输出
  Excel 结构（测试同步更新）。

### 3.4 `gui/frontend/dist/styles.css`

- 圆角 token 调低：`--r-sm:2 / md:3 / lg:4 / xl:6 / 2xl:8px`；交互 pills
  （history-button、icon 圆钮等）改为低圆角；shadow 弱化（扁平）。
- 新增：`.new-session-*`、`.session-pin`、`.session-group-add`、
  `.excel-grid`（表格边框/表头/斑马纹/列宽）、`.excel-sheets`（页签）、
  `.scheduled-table`、`.modal-resize-handle`。

## 4. 需查看文件（实施前）

| 文件 | 查看重点 |
|---|---|
| `gui/frontend/dist/index.html` | 左右栏结构、modal 模板 |
| `gui/frontend/dist/app.js` | elements、sessionRow/renderSessionGroups、renderWorkspace、renderScheduledTaskPanel、setModal、关闭循环 |
| `gui/frontend/dist/work-table.js` + `work-table.test.mjs` | 现有渲染/导出 API 与断言 |
| `gui/frontend/dist/styles.css` | 圆角 token、modal、session-group、sched-view 样式 |
| `gui/bridge_test.go` L650-740 | DOM 与脚本子串断言 |
| `gui/frontend/dist/protocol.test.mjs` | reducer 回归（不应受影响） |

## 5. 实施决策（默认，用户可纠正）

- D1 置顶粒度：置顶会话在各自工作区组内排前；无独立「置顶」分组。
- D2 「概要」并入可折叠「状态」区（默认折叠，展开可见状态 + 概要）。
- D3 定时任务：右栏只留入口按钮，表格放弹窗（与工作表格一致）。
- D4 新建任务会话不解除当前工作区绑定（保持现状语义）；工作区会话显式绑定。
- D5 历史检索/工作树区块结构不动（仅随右栏整体位置自然下移）。
- D6 弹窗拉伸只做尺寸调整（右下角手柄），不做拖动定位。

## 6. 验证清单

```text
node --test gui/frontend/dist/*.test.mjs
go test ./gui -count=1
go test ./e2e -run TestGUIDocumentContracts -count=1
git diff --check
```

## 7. 任务拆分与 ASCII 预览（v3，按用户要求分左右栏两个任务）

### 7.1 左栏任务 L（子代理 L）

```text
┌─ 左侧栏 ─────────────────────────────────────┐
│ 会话                                    [+]  │   [+] = 新建会话弹窗
│ ┌ 工作区A (2)                       [+] ┐    │   组头 [+] = 该工作区新建会话
│ │ 📌 修复首页样式崩溃…              ✕   │    │   📌 = 置顶（localStorage）
│ │ 📌 实现登录流程…                  ✕   │    │   ✕ = 删除（已有）
│ └─────────────────────────────────────┘    │
│ ┌ 工作区B (1)                       [+] ┐    │
│ │ 重构数据库迁移脚本…                ✕   │    │
│ └─────────────────────────────────────┘    │
│ ┌ 未关联会话 (1) ───────────────────────┐   │
│ │ 日常答疑记录…                      ✕   │   │
│ └──────────────────────────────────────┘   │
│ ▸ 账户 (2)                                 │
│    · deepseek-chat · openai · …            │   可折叠，保持不变
└────────────────────────────────────────────┘

[+] 新建会话弹窗（两步）：
┌ 新建会话 ─────────────────────┐
│ ┌ 新建任务会话 ─────────────┐ │
│ │ 普通对话会话，不绑定工作区  │ │
│ └───────────────────────────┘ │
│ ┌ 新建工作区会话 ───────────┐ │
│ │ 绑定项目文件夹，进入第二步  │ │
│ └───────────────────────────┘ │
└───────────────────────────────┘
┌ 新建工作区会话 ───────────────┐
│ ← 返回                          │
│ 已有工作区                      │
│   · 工作区A  (root/…/A)        │
│   · 工作区B  (root/…/B)        │
│ [ 选择文件夹新建工作区… ]       │
└────────────────────────────────┘
```

### 7.2 右栏任务 R（子代理 R）

```text
┌─ 右侧栏 ────────────────────────────────────┐
│ 项目                                        │
│  工作区A（名称 / root 路径）                 │
│ ▸ 状态（默认折叠）                          │   <details> 默认关闭
│    状态 · 会话 · 消息 · 文件数              │
│    概要：当前任务 / 上下文压缩               │
│ 工作表格                            0 未读  │
│ ┌─ 打开工作表格 ─────────────────┐          │
│ └────────────────────────────────┘          │
│ 定时任务                               [+]  │
│ ┌─ 打开定时任务表格 ──────────────┐          │
│ └────────────────────────────────┘          │
│ ▾ 更多面板（历史检索 → 工作树，不变）         │
└──────────────────────────────────────────────┘

工作表格弹窗（Excel 化，右下角可拉伸）：
┌ 工作表格 ─────────────────────────────────────────┐
│ 类型 │ 任务    │ 描述 │ 状态   │ Assignee │ 依赖 │… │ ← 固定表头
│ todo │ 修复首页…│ …   │ DOING  │ main:…   │ —    │  │
│ task │ 补测试…  │ …   │ DONE   │ main:…   │ t…   │  │
│ plan │ 调研接口…│ …   │ RUNNING│ main:…   │ —    │  │
│─────────────────────────────────────────────────── │
│  全部(5) │ 批次A(3) │ 批次B(2)       ← sheet 页签  │ ← 切批次=切维度
└────────────────────────────────────────────────────┘

定时任务表格弹窗（同 Excel 风格）：
┌ 定时任务 ─────────────────────────────────────────┐
│ 名称      │ 类型 │ 周期      │ 下次运行 │ 状态 │ 操作 │
│ 自动投简历 │ 命令 │ 每 1 小时 │ …      │ 已启用│ 取消 │
│ 每日巡检   │ 提示词│ 每天 08:00│ …      │ 上次成功│ 取消 │
└────────────────────────────────────────────────────┘
```

### 7.3 文件与函数归属矩阵（避免并行冲突）

| 资源 | 归属 | 说明 |
|---|---|---|
| `gui/frontend/dist/index.html` 左栏 + `#new-session-modal` | L | 会话区/账户区/新建弹窗骨架 |
| `gui/frontend/dist/index.html` 右栏 + `#scheduled-table-modal` + resize 手柄 | R | 状态折叠、定时任务按钮、Excel 弹窗 |
| `gui/frontend/dist/app.js` elements 数组 | **root 预置** | 一次性补齐 new-session-*/scheduled-table-* 并删除 workspace-* |
| `gui/frontend/dist/app.js` 左栏函数区 | L | renderSessions / renderSessionGroups / sessionRow / togglePin / startNewSession / new-session handler / 删除 renderWorkspace 与 new-workspace handler / 关闭循环追加 |
| `gui/frontend/dist/app.js` 右栏函数区 | R | renderWorkTable / renderScheduledTaskPanel / openCloseScheduledTable / initModalResize / 关闭循环追加 |
| `gui/frontend/dist/work-table.js` + `work-table.test.mjs` | R | Excel 表格 + sheet 页签重写 |
| `gui/frontend/dist/scheduled-tasks-view.js` | R | 表格行渲染（`<tr>`） |
| `gui/frontend/dist/sidebar.js`（新）+ `sidebar.test.mjs`（新） | L | truncateTitle / pin 状态 helpers（可测） |
| `gui/frontend/dist/styles.css` 会话/弹窗样式块 | L | `.session-*` / `.new-session-*` |
| `gui/frontend/dist/styles.css` Excel/定时/resize 样式块 + token | R | `.excel-*` / `.modal-resize-*` / `.scheduled-table` / 圆角 token |
| `gui/bridge_test.go` | R | 保持 L670-731 断言通过（含 `session.name || shortSessionID(session.id)` 子串） |

约定：root 先落骨架（elements 数组 + index.html 弹窗空骨架 + CSS token），
L/R 只改自己区域；同一文件内不同函数区互不重叠；禁止整文件重写；
各自完成后跑 `node --test gui/frontend/dist/*.test.mjs` 全量与 `go test ./gui`。

## 8. 任务 M：会话保护粒度从全局单例 → 单会话粒度（v4 新增）

### 8.1 现状（证据）

- `application/core/internal/state/state.go`：唯一 `Core.Mu` + 唯一 `Snapshot`。
- `application/core/chat.go` `startChat`：`if Core.Snapshot.Chat.Running { return ErrChatRunning }`
  ——全局唯一 ChatState，任何第二个会话提交都被拒绝。
- `internal/adapters/engine_port.go`：单 `engine` 指针 + 全局 `activeCalls`；
  `BeginNewSession/ResumeSession` 是破坏式切换（清空/替换同一引擎）。
- `Snapshot/Event` 无 `session_id` 路由键；Effort/Plugin/Skill/Plan/
  Interaction/input queue 都是"当前会话"全局态（
  `docs/gui/modules/multi-session-pages.md` §2 已记录该基线）。

### 8.2 目标（per-session 保护）

```text
application.Service
  ├─ activeSessionID string              // 前端当前展示的会话
  ├─ sessions map[sessionID]*sessionRuntime
  │    ├─ engine  ReactorEngine          // 每会话独立 frameworkSession
  │    ├─ mu      sync.Mutex             // 会话级锁（保护该会话状态）
  │    ├─ chat    ChatState{Running, RequestID, cancel}
  │    ├─ snapshot model.Snapshot        // 会话级权威状态
  │    │        （Conversation/Task/Plan/WorkTable/Interaction/ReadFiles…）
  │    └─ promptStack / inputQueue / taskContext / viewState   // 会话级实例
  └─ 全局共享：EventHub（事件带 session_id 路由）、Runtime（账号/插件/
       MCP 共享；TaskRegistry/SubAgentTree/WorkTable 按会话隔离）

语义变化：
  - ErrChatRunning 只对同一 sessionID 生效；跨会话可并行执行；
  - 切页不再清空/替换 Engine 与业务状态，只切换 activeSessionID；
  - 前端 API 显式携带 sessionID：Submit(sessionID, text) / Snapshot(sessionID)
    / Subscribe(sessionID)；旧 API 迁移期委托 active session。
```

### 8.3 实施阶段（M1 本轮核心，M2/M3 后续）

| 阶段 | 内容 | 风险 |
|---|---|---|
| M1 | EnginePort 多实例 map + 每会话锁；Service 增加 `sessionRuntime` 容器（每会话 Snapshot/ChatState/cancel/promptStack）；`ErrChatRunning` 按 sessionID 判定；RuntimePort 增加 `SubmitToSession/ActivateSession` 等显式 API；GUI Bridge 显式 sessionID 绑定；事件与快照带 `session_id` | 高：核心状态机从全局迁到会话级 |
| M2 | 持久化/审批/worktable/子代理树按会话隔离；会话并行 race + E2E | 高 |
| M3 | 删除全局 ChatState/单 Engine 语义；前端多页签并行渲染与通知 | 中 |

### 8.4 归属与冲突（M 与 L/R 的关系）

| 资源 | 归属 |
|---|---|
| `application/core/internal/state/state.go`、`application/core/chat.go`、`service_state.go`、`service_assembler.go`、`session_runtime/*`、`view_state/*`、`task_context/*` | M |
| `internal/adapters/engine_port.go`、`session_workspace_ports.go`、`runtime_port.go` | M |
| `application/contract/ports.go`、`application/model/state.go`（session_id/快照结构） | M（与 L/R 不重叠：L/R 不碰 model/ports） |
| `seelebridge/runtime.go`、`seelebridge/task/*`（注册表会话维度）、`seelebridge/ports.go` | M |
| `main.go`（装配 per-session factory、bridge 绑定） | M |
| `gui/bridge.go` + `gui/bridge_test.go`（显式 sessionID API） | M（R 不碰 bridge.go） |
| `gui/frontend/dist/`（页签路由、session 事件过滤） | M2/M3（本轮不进入 L/R 文件） |
| L/R 的文件域 | 与 M 互斥（L/R 只动 dist 前端与 work-table/scheduled 视图；M 不动 dist 前端） |

### 8.5 M1 验证

```text
go build ./...
go vet ./application/... ./seelebridge ./internal/adapters ./gui
go test ./application/core ./seelebridge/... ./gui ./e2e -count=1 -timeout=300s
go test -race ./application/core -run "Chat|Session|Race" -count=1   # Linux/支持环境
```

> 说明：M 是三个任务中风险最高、改动面最大的后端重构（即
> `multi-session-pages.md` 方案 B 的 SessionActor 落地的核心阶段）。
> 建议与 L/R 并行推进，但 M1 完成后需要独立的联调与回归窗口；
> 若本轮时间不允许 M1 完整落地，可先交付 L/R，M 单独一个迭代。

## 9. R 实施记录（2026-08-23，子代理 R）

### 已实现（对应 §1 R7–R11）

- R7 右栏结构：`#status-panel` 为 `<details>` 默认折叠，内含状态网格 +
  `#project-overview` + `#context-compactions`；顺序 = 项目 → 状态 →
  工作表格 → 定时任务 → 更多面板（历史检索 → 工作树，未动）。
- R8 工作表格 Excel 化：`work-table.js` 重写渲染层——工具栏（展开/折叠 +
  kind 筛选 chips）+ `<table class="excel-grid">` 固定表头（类型/任务/描述/
  状态/Assignee/依赖/附件/打点/操作）+ 底部 `#work-sheet-tabs`
  （`data-work-sheets`，「全部」页签居首，批次页签带标签与各类计数，点击
  切换 `activeBatch` 维度）+ 批内分页保留；导出 API 兼容
  （workTableView/countUnread/workTableSignatures/pageCount/pagedRows/
  createWorkTableView/renderShellHTML/renderWorkItemRow/renderWorkTraceHTML）。
- R9 定时任务 Excel 表格：`scheduled-tasks-view.js` 新增
  `renderScheduledTasksTable`（名称/类型/周期/下次运行/状态/操作），右栏
  `#scheduled-table-open` 打开 `#scheduled-table-modal`；新建复用
  `#scheduled-task-modal` 表单弹窗；原 `#scheduled-task-view` 面板保留
  （hidden，向后兼容）。
- R10 弹窗拉伸：`initModalResize()` 通用处理器（pointerdown/move/up +
  pointer capture），覆盖 `data-resizable` 弹窗（工作表格/定时任务表格/
  新建会话/定时任务表单）；仅尺寸调整，不做拖动定位。
- R11 扁平低圆角：样式在 `/* __R_STYLES__ */` 区新增 `.excel-grid` /
  `.excel-sheets` / `.scheduled-table` / `.modal-resize-handle`；筛选 chips
  圆角收敛到 `--r-lg`（token 已由骨架调低为 2/3/4/6/8px）。

### 改动文件

- `gui/frontend/dist/work-table.js`（渲染层重写）+ `work-table.test.mjs`
- `gui/frontend/dist/scheduled-tasks-view.js` + `scheduled-tasks-view.test.mjs`
- `gui/frontend/dist/app.js`（右栏函数区：renderScheduledTaskPanel /
  openScheduledTable / closeScheduledTable / initModalResize / 表格取消委托）
- `gui/frontend/dist/styles.css`（R 样式块）
- `docs/gui/modules/work-table.md`（模块文档同步）

### 验证

```text
node --test gui/frontend/dist/*.test.mjs   # 123/123 通过
go test ./gui -count=1                     # ok
go test ./e2e -run TestGUIDocumentContracts -count=1  # ok
node --check app.js / work-table.js / scheduled-tasks-view.js  # 语法通过
git diff --check                           # 通过
```

### 取舍与遗留

- 批次页签计数展示权威批次头计数摘要（如 `Task 1 · Todo 1`），「全部」页签
  展示当前行总数；行数滞后于批次头时以页签计数为参考（展示层偏差，v1 接受）。
- `renderScheduledTasks`（旧面板列表）保留导出与测试，右栏面板隐藏但代码
  兼容；待文件预览/清理轮次可整体移除。
- `initModalResize` 只做尺寸拉伸；`data-resizable` 弹窗 min-width/height
  由 CSS 约束（420×260），视口内 clamp。

## 10. L 实施记录（左栏重构，子代理 L）

### 完成内容

- R1 左栏工作区列表区块：骨架已删除（上个被打断回合），本回合移除
  `renderWorkspace` 函数与 `new-workspace` 监听（修复启动崩溃：两者引用已
  删除的 `workspace-info`/`workspace-list` 元素）。
- R2 会话分组：`renderSessionGroups` 按 `session_workspaces` 投影分组，
  未关联会话收进「未关联会话」组置底；分组头新增组内「+」（
  `data-workspace-new-session`，仅限已关联工作区组）。
- R3 新建会话弹窗两步：`#new-session` 从直接 `BeginNewSession` 改为打开
  `#new-session-modal`；第一步任务会话/工作区会话，第二步列出已有工作区 +
  「选择文件夹新建工作区…」。
- R4 工作区会话：选择文件夹 → `PickDirectory` → `CreateWorkspace` →
  refresh 后按 `root_path` 反查新工作区 id → `BindWorkspace` →
  `BeginNewSession`；已有工作区点击 = 绑定 + 新建会话。
- R5 置顶与删除：置顶走 `localStorage`（`seelex.pinned-sessions`，纯函数放
  `sidebar.js`），置顶会话组内排前；删除沿用既有 `DeleteSession`。
- R6 标题截断：`truncateTitle(name, 5)`（Unicode 码点安全），完整标题放
  `title` 属性。
- 解绑：原左栏「解除项目绑定」迁到新建会话弹窗第二步（
  `data-new-session-unbind` → `UnbindWorkspace`）。
- 弹窗接线：`new-session-modal` 背景点击关闭与 Escape 关闭；关闭按钮绑定。

### 改动文件

- `gui/frontend/dist/sidebar.js`（新增）：`truncateTitle` / `readPinnedSessions` /
  `writePinnedSessions` / `isPinned` / `togglePinned`，纯函数可注入存储。
- `gui/frontend/dist/sidebar.test.mjs`（新增）：8 用例（截断边界、代理对、
  置顶持久化/容错）。
- `gui/frontend/dist/app.js`：左栏函数区（renderSessions 委托、renderSessionGroups
  排序与组头、sessionRow 置顶/截断、new-session-modal 两步流程、移除
  renderWorkspace/new-workspace、关闭循环与 Escape 追加）。
- `gui/frontend/dist/styles.css`：`/* __L_STYLES__ */` 区替换为新建会话弹窗 /
  分组头行 / 置顶按钮样式（低圆角扁平）。

### 验证

```text
node --test gui/frontend/dist/*.test.mjs   # 131/131 通过（含 R 的 123 + L 新增 8）
go test ./gui -count=1                     # ok（bridge_test 子串断言保持）
go test ./e2e -run TestGUIDocumentContracts -count=1  # ok
node --check app.js / sidebar.js           # 语法通过
git diff --check                           # 通过
```

### 取舍与遗留

- `styles.css` 仍残留 `#workspace-list` / `.workspace-info` 两条死规则（DOM
  已无对应元素），不影响行为，待整体样式清理轮次移除。
- 弹窗「新建任务会话」不解除当前工作区绑定（保持 `BeginNewSession` 现状
  语义）；「工作区会话」显式绑定。
- 置顶只影响展示排序（组内前置），不改变后端会话目录顺序；删除会话后
  localStorage 中残留的置顶 id 会被无害忽略。

## 11. M 实施记录（会话保护粒度：全局单例 → 单会话粒度，子代理 M1）

> 状态：M1 已完成并全仓验证；M2/M3 为后续迭代（见 §8.3 与
> `docs/gui/modules/multi-session-pages.md` §2.1）。

### 改动文件

| 文件 | 改动 |
|---|---|
| `internal/adapters/engine_port.go` | 会话级引擎多实例：`engines` 注册表 + 每会话 `engineCalls`；`ActivateSession`/`ResumeSession`/`ResumeRawSession` 按会话切换/恢复，不销毁其它会话引擎 |
| `application/core/session_scope.go`（新） | 会话级聊天运行态 `sessionChatRuntime`（ChatState/cancel/inputQueue/流输出）；`SubmitToSession`/`ActivateSession`/`SnapshotOf`/`SubscribeSession` 显式 API；`publishSessionEvent` |
| `application/core/service_state.go` | `serviceState` 增加 `sessionChat` 注册表 |
| `application/core/chat.go` | `startChat`/`runChat` 改读会话级运行态（ErrChatRunning 只对同会话生效；跨会话提交单飞边界）；chat 事件携带 sessionID |
| `application/core/service_input.go` | `submitConversation` 队列与 `CancelChat` 走会话级运行态 |
| `application/core/session_draft.go` | `BeginNewSession` 会话级运行中判定 + draft 运行态重置 + 镜像 |
| `application/core/session_history.go` | `resumeSession` 经 `EnginePort.ResumeSession` 恢复目标会话引擎 + 快照镜像 + 会话路由事件 |
| `application/core/service.go` | 新增 `ErrSessionBusy`/`ErrSessionSnapshotUnavailable` |
| `application/event/hub.go` | `Event.SessionID` 字段、`PublishSession`、`SubscribeSession` 会话过滤订阅 |
| `gui/bridge.go` | 可选 `sessionAwareApplication` 接口：`SubmitToSession`/`ActivateSession`/`SnapshotOf`/`SubscribeSession`（旧 API 不受影响） |
| 测试 | `internal/adapters/adapters_test.go`（会话引擎隔离/按需激活/恢复复用）、`application/core/session_scope_test.go`（跨会话提交边界/快照粒度/事件路由/订阅过滤）、`gui/bridge_session_test.go` |
| 文档 | `application/core/README.md`、`docs/gui/modules/multi-session-pages.md` |

### 新 API 签名（M1）

```go
// application/core（Service）
func (service *Service) SubmitToSession(ctx context.Context, sessionID, text string) error
func (service *Service) ActivateSession(sessionID string) error // M1 = resumeSession
func (service *Service) SnapshotOf(sessionID string) (Snapshot, error)
func (service *Service) SubscribeSession(sessionID string, buffer int) (Subscription, error)

// internal/adapters（EnginePort，可选能力经接口断言接入）
func (port *EnginePort) ActivateSession(sessionID string) error
func (port *EnginePort) ResumeSession(sessionID string, history []contract.EngineMessage) error
func (port *EnginePort) ResumeRawSession(sessionID string, history []types.Message) error

// application/event
func (hub *EventHub) PublishSession(kind EventKind, revision uint64, requestID, sessionID string, payload any) Event
func (hub *EventHub) SubscribeSession(sessionID string, buffer int) Subscription
```

### 语义变化

- `ErrChatRunning` 只对**同一会话**生效（同会话运行中二次提交 → 排队）；
- 跨会话提交在任意会话运行中返回 `ErrSessionBusy`（M1 单飞执行边界，
  真并行 = M2 会话级组件栈隔离）；
- `EnginePort` 切会话不再销毁其它会话引擎（按需创建 + 注册表缓存）；
- Event 携带 `session_id`，`SubscribeSession` 按会话过滤（含全局事件）；
- `BeginNewSession`/`ResumeSession` 运行中仍被拒绝（共享 Snapshot 不串写）。

### 验证

```text
go build ./...                                        # 通过
go vet ./...                                          # 通过
gofmt -l .                                            # 空
go test ./... -count=1 -timeout=420s                  # 全仓通过
node --test gui/frontend/dist/*.test.mjs              # 131/131（L/R 合并态）
git diff --check                                      # 通过
```

### 遗留（M2/M3 规划）

- 真并行执行：共享组件栈（task_context/prompt_layer/view_state/
  session_runtime）需迁入每会话 runtime（SessionActor），才能放开
  "多个 running"；
- 每会话驻留 Snapshot（`SnapshotOf` 目前仅活跃会话可用）；
- Effort/Plugin/Skill/Plan/Interaction/审批 会话级隔离；
- 前端页签 + 会话级事件消费（前端当前仍用全局订阅）；
- 删除全局 ChatState/单 Engine 语义的最后痕迹。
