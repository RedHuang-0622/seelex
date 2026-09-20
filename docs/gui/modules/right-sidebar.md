# Right sidebar（右侧栏）

## 模块定位

右侧栏与中间主视图共享一套子页停靠布局：对话区子页（**对话 / 轨迹**）与
右栏子页（**状态 / 工作台 / 资源管理器**）共五个视图，可点击切换、拖拽换序、
跨栏置换（拖到另一栏某页签上松开即与该页签互换位置）。布局与激活是纯 UI
状态（`localStorage["seelex.dock.v1"]` 记忆），业务事实全部来自 Application
Snapshot/Event 权威投影；「资源管理器」子页内另有左「文件预览」抽屉 + 右
「工作树 / 提交记录 / 工作区更改」三面板（面板可拖拽调换顺序，预览宽度可拖，均
localStorage 记忆）。历史检索保留在右栏子页之下的「更多」折叠区。

主要调用方：`app.js` 右侧栏渲染；数据源：`snapshot.runtime`（权威投影）与
`Bridge.WorkspaceTree` / `Bridge.WorkspaceFileCount` / `Bridge.WorkspaceGitLog` /
`Bridge.WorkspaceChanges` / `Bridge.WorkspaceFileContent`（只读元数据/受控读取桥）。

## 子页划分

| 子页 | 内容 | 数据源 |
|---|---|---|
| **状态** | 项目状态表（键值两列：状态/会话/消息/任务/待审批/文件数）+ 概要 + 上下文压缩时间线 + **账户栏**（状态一栏之下）+ **Agent Team**（员工栏 / Team 栏两块表格） | `snapshot.chat/task/conversation`、`runtime`（含 `runtime.accounts` / `runtime.account`） |
| **工作台** | 「目标」面板 + 工作表格入口 + 定时任务面板 | `runtime.goal_skill_active`、`runtime.active_skills`、`task`、`work_table`、`scheduled_tasks` |
| **代码** | 左：文件预览抽屉（点工作树或工作区更改的文件行打开）；右：工作树 + 提交记录 + 工作区更改（可拖拽调换） | `Bridge.WorkspaceFileContent`、`Bridge.WorkspaceTree/FileCount`、`Bridge.WorkspaceGitLog`、`Bridge.WorkspaceChanges` |

项目标题（`project-heading`）与「历史检索」折叠区跨子页常驻，不属于任何子页。
状态子页自上而下：`状态` → `账户` → `Agent Team`。

### 账户栏（状态子页）

账户从左侧栏底部搬到「状态」子页的状态一栏之下：状态区显示的就是当前账户的
provider/model，账户列表贴在它下面，一眼对得上"现在是谁在跑"。切换经
`Bridge.SelectAccount(name)`；条目化单列表格行（○/● 当前标记 + 名字 +
provider · model + 不可用标记），点击走 `#account-list` 容器委托（列表刷新不
重建行级监听）。渲染见 `app.js: renderAccounts`。

### Agent Team（状态子页）

面板拆成两个不同的东西，各自条目化（`role=table` 的网格表格
`.team-table/.team-table-row`）：

- **员工栏** —— 员工管理：员工名单表（员工身份 / 逻辑角色名 / 类型 / 独立会话 /
  顺序位置 / 编辑·删除）+ 一步实例化表单（同 `role_name` 幂等覆盖）；
- **Team 栏** —— 装配与编排：装配 preset 工具栏 + 团队参数表（形态 / 顺序策略 /
  发言权）+ 发言调度表 + 工作顺序表（含未排入顺序的"恢复"）+ 定时 agent 表。

顺序的唯一事实是会话 `lifecycle.order_policy/order_roles`：前端只提交用户改动后
的整张顺序表，不缓存、不乐观重排，每次动作后重拉视图（`Bridge.AgentTeam*`）。

### 页签停靠与置换

- 布局模型：主视图固定两个页签、右栏固定三个页签，五个视图 id 恰好各出现一次；
  纯函数见 `gui/frontend/dist/dock-layout.js`（`normalizeDockState`/`swapViews`），
  DOM 归属、页签渲染与 localStorage 由 `app.js` 的 `applyDockState` 收敛。
- 同栏拖到另一页签 = 换序；跨栏拖到另一页签 = 置换（被拖入页成为目标栏激活页，
  原栏激活页由移入页接管）。跨栏拖到页签条空白处 = 与该栏当前激活页置换，
  避免拖进目标栏却落不到页签上成为空操作。
- 会话类页面在主视图激活时显示底部输入框；其它主视图（工作台/状态/资源管理器）
  全宽展示时隐藏输入框，避免遮挡内容。
- 布局变化只写 `seelex.dock.v1`，不触发后端调用；会话局部状态（滚动、轨迹过滤、
  展开）随对应 DOM 一起迁移。

## 目标面板（工作台子页）

「目标」面板展示当前任务的工程目标证据面：

- **目标文本**：会话最近一条非空用户消息（本地派生，截断 120 字展示，title 全文）。
- **任务状态**：`snapshot.task.status` + `summary`（权威 TaskState）。
- **激活 skill**：`runtime.active_skills`（任务级 skill 激活权威投影）+ GOAL badge
  （`runtime.goal_skill_active`）。

无目标文本、无任务且无激活 skill 时整个 section 隐藏。数据在
`application/core/view_state` 收集投影（锁内快照），`runtime.changed` 增量携带，
不需要额外 Bridge 调用。

## 资源管理器子页：文件预览 + 工作树 + 提交记录树 + 工作区更改

子页 3 内部左右分栏（`.code-split`）：左「内容详情」抽屉（`.file-preview-pane`，
默认收起、点击文件自动展开），右栏上下排列工作树、提交记录与工作区更改三块面板
（顶部有拖拽手柄 grip icon）。左抽屉与右栏之间是宽度拖拽分隔条（`--preview-w`，
`localStorage["seelex.preview-pane-width"]`，默认 380px）。宽度**无固定上限**：
只按子页可视宽封顶（`calc(100% - 6px)`，拖动时 JS 与容器宽对齐），内容详情与
工作树谁宽谁窄完全由拖动决定；左/右主栏同理（`--left-w`/`--right-w` 上界按视口
宽动态计算）。形态四态由 `.code-split` 的类切换：收起 `.is-closed`、展开
`.is-preview-open`、详情收成竖轨 `.is-preview-open.is-preview-collapsed`、
隐蔽 `.is-preview-open.is-panes-hidden`。

- **文件预览（多文件详情容器）**：点击工作树文件行 → `Bridge.WorkspaceFileContent(relPath, limit)`
  拉取受控字节（后端 workspace 域 containment/敏感过滤/上限/二进制探测；默认
  文本 4 MiB、文档/图片 24 MiB、后端硬钳 64 MiB，分页类超限放弃渲染并提示）→
  按扩展名分派渲染（markdown=marked→DOMPurify→highlight.js；代码/文本=
  highlight.js 高亮或纯文本；PDF=PDF.js canvas 分页；Word=docx-preview；
  图片=blob img；`.doc` 提示转换）。组件全部本地 vendor（`dist/vendor/`，
  随 embed 离线打包）。实现见 `file-preview.js`；预览内容不进入 Snapshot/业务
  状态，工作区切换时抽屉随树清空。
  抽屉是**多文件详情容器**：每个打开的文件一枚 chip（`renderPreviewTabsHTML`）+
  一个独立面板（切换只切显隐，不重读、不丢滚动位置）。抽屉**头部就是 chip 标签
  条本身**——没有独立标题 / 元信息行（2026-09-17 删掉了「文件详情 · path · size」
  那一行，文件身份直接由 chip 承担，动作按钮贴右）。点 chip 切换当前详情；点 chip
  尾部 ✕ 关闭单个详情；再次点开同一文件只是激活，不重复读盘。**生命周期以「有文件
  详情」为前提**：最后一个 chip 关闭（容器为空）时抽屉自动收起（`onEmpty`）、子页
  恢复原来大小（工作树/提交记录重新占满）；因此不再记忆展开态（只记忆宽度与
  `seelex.preview-panes-hidden`）。标签生命周期是纯函数：
  `previewTabLabel`/`normalizePreviewTab`/`openPreviewTab`/`closePreviewTab`。
  头部右侧「隐蔽工作树/提交记录」按钮（`data-icon="expand"`）让内容详情独占整个
  子页，再次点击或关闭全部详情即恢复。
  反向的「**收起详情，让出内容页**」（2026-09-17，`data-icon="chevron-left"`）把
  抽屉折成一条 28px 竖轨（`.is-preview-collapsed`：`preview panes` 两列，分隔条不再
  占位），工作树 + 提交记录独占子页；点竖轨（或按钮）还原。它与「隐蔽工作树/提交
  记录」互斥（收起时复位隐蔽态），因此不会出现两侧都隐蔽的空子页。收起态是**会话内
  状态、不落盘**：打开文件会解除它（点文件树必须能看见详情），关闭全部详情也归零。
- **工作树**：`Bridge.WorkspaceTree(relPath, depth)` / `Bridge.WorkspaceFileCount()`
  （后端权威元数据：名称/路径/类型/大小/直接文件计数，不含文件内容）；目录行
  惰性展开，文件行是可点击按钮（打开预览）；层级连线用 `tree-fork` 的树轨
  （祖先续行轨 + 末子弯头，零额外 DOM），行点击走容器委托。实现见
  `worktree-view.js`。
- **提交记录**：`Bridge.WorkspaceGitLog(limit)`（最近 20 条：按 `--topo-order`
  的提交行 + 每个提交的 `parents` 父 hash + hash/作者/时间/标题；只读，不含
  diff/文件内容）；分叉由前端算泳道并画 SVG（直线/合并曲线 + 提交点，
  `tree-fork.layoutCommitGraph`），短 hash 可点击复制完整 hash。实现见
  `git-log-view.js`。
- **工作区更改**：`Bridge.WorkspaceChanges(limit)`（最近 200 条未提交改动：
  行内是状态字母/路径/重命名原路径/暂存标记，头上是「分支 · 暂存 x · 未暂存 y ·
  未跟踪 z · 冲突 w」统计；只读元数据，**不含 diff 与文件内容**）。点文件行打开
  预览（与工作树同一个抽屉）；已删除的文件没有字节可读，行不可点（title 说明）。
  实现见 `workspace-changes.js`。

### 拖拽调换

三面板支持 HTML5 drag & drop 调换顺序，顺序持久化到
`localStorage["seelex.right.codePanes"]`（默认 `["worktree","gitlog","changes"]`；
旧的两项存储因长度不匹配自动落回默认顺序）。子页激活时按需刷新数据面（工作区
切换或 chat 结束产生新提交/新改动时重新拉取）。

### 提交记录数据面（后端）

`workspace.GitLog(root, limit)` 在 workspace root 内执行**固定 argv** 的
`git log --all --topo-order`（不经过 shell、带 5s 超时），取
`%H %h %an %ad %P %s`（`\x01` 分隔，主题放最后故可含任意字符），解析为结构化
`dto.GitLogResult`（`Commits[]`：hash/短 hash/作者/时间/`Parents[]`/标题，按拓扑序
新→旧）。不再取 `--graph` 的字符画前缀——泳道由前端按 `Parents` 算，拓扑事实
只有一份。非 git 仓库 / git 不可用时以 `Result.Error` 返回展示文案（不是 Go
error），避免 GUI toast 噪音。limit 默认 20、钳制上限 200。
接线：`workspace.Repo.GitLog` → `WorkspaceTreePort.GitLog` →
`application.Service.WorkspaceGitLog` → `Bridge.WorkspaceGitLog`。

### 工作区更改数据面（后端）

`workspace.GitChanges(root, limit)` 在 workspace root 内执行**固定 argv** 的
`git --no-optional-locks -C <root> status --porcelain=v1 -b -z -uall -- .`
（不经过 shell、带 8s 超时；`--no-optional-locks` 以只读方式取状态，不刷新索引、
不抢 index 锁）。用 `-z` 而不是默认输出：NUL 分隔且**不做引号转义**，含空格/中文
的路径原样返回，重命名是「新路径 NUL 旧路径」两条记录（默认输出会把非 ASCII
路径写成 `\344\270...` 转义序列，前端还得反解一套 C 风格转义）。`-- .` 把范围钉在
工作区子树内——只给 `-C 子目录` 时 git 仍会带出仓库里工作区之外的改动。

两处收敛都在 workspace 层做掉，不让展示层各自实现：

1. **路径基准**：git status 的路径以**仓库根**为基准，而面板以**工作区根**为基准
   （绑定的目录可能是仓库子目录）。这里按 `rev-parse --show-toplevel` 剥掉前缀，
   工作区之外的兄弟路径直接丢弃并计入 `Result.Filtered`。
2. **可见性边界**：与 ListTree/ReadFile 一致——路径任一环节命中敏感文件名
   （`accounts.yaml`、`*.local.yaml`）不展示并计入 `Filtered`（面板显式提示「另有 N
   条未展示」，不静默）。目录噪音边界交给 git 自己的 `.gitignore`：把
   `ignoreDirNames`（node_modules/dist/tmp…）也套上去，会连「被仓库跟踪的 dist/
   改动」一起藏掉，那是误报而不是降噪。

分类与统计只在后端解释一次：porcelain 的 XY → `dto.Change*`（modified/added/
deleted/renamed/copied/type_changed/conflicted/untracked，暂存侧优先），`Index`/
`Worktree` 两个单字符与 `Staged` 如实下发。`Total` 与四个计数统计的是**过滤后的
全部条目**（含被 limit 截断、未出现在 `Entries` 里的部分）——头部说的是工作区状态，
不是"本屏列了多少行"，列表自身的截断另有 `Truncated` 提示。limit 默认 200、钳制
上限 1000；解析预算 20000 条（`-uall` 在未收敛的仓库上能一次吐出上百 MB）。非 git
仓库 / git 不可用时以 `Result.Error` 返回展示文案（不是 Go error），避免 GUI toast
噪音；空仓库的分支头（`## No commits yet on main`）与分离头指针（`## HEAD (no
branch)`）都归一化成可展示的分支名。

接线：`workspace.Repo.GitChanges` → `WorkspaceTreePort.GitChanges` →
`application.Service.WorkspaceChanges` → `Bridge.WorkspaceChanges`。

## 历史检索

历史检索保留在 `#side-more` 折叠区（跨子页常驻），不并入任何子页：它是低频
检索操作，不应占据子页主空间。数据源 `Bridge.SearchHistory`（压缩栈索引 →
真实记录）。

## 依赖方向

```text
app.js（停靠布局渲染）
  ├── dock-layout.js（分区/排序/置换纯函数）
  ├── snapshot.runtime.*（状态/工作台子页，权威投影）
  ├── worktree-view.js ──► Bridge.WorkspaceTree / WorkspaceFileCount
  ├── git-log-view.js ──► Bridge.WorkspaceGitLog
  ├── workspace-changes.js ──► Bridge.WorkspaceChanges
  └── history-search.js ──► Bridge.SearchHistory
```

子页切换/停靠布局/面板顺序都是本地 UI 状态（localStorage），不进入 Snapshot；
数据面全部来自后端权威源。

## Review 指南

- 子页切换与拖拽置换不得触发后端调用之外的副作用；`seelex.dock.v1` 布局与
  `code-panes` 顺序变化只写 localStorage。
- 停靠布局必须保持“每个视图恰好属于一栏、激活页属于所在栏”不变量；脏存储回退
  默认布局（`normalizeDockState` 契约测试覆盖）。
- 会话类页面在主视图之外时，空态/历史按钮/输入框的显隐由
  `syncSessionChrome` 统一收敛，避免隐藏容器渲染把会话专属悬浮件带出来。
- git log 查询是只读元数据：不得暴露 diff/补丁/文件内容；参数必须固定 argv。
- 工作区更改查询同样是只读元数据：不得暴露 diff/补丁/文件内容/blob；参数必须
  固定 argv，路径一律以**工作区根**为基准下发（仓库子目录即工作区根时要剥前缀），
  敏感文件名与工作区之外的路径必须过滤并计入 `Filtered`（不许静默丢弃）。
- 面板渲染文本全部 escape（commits 的 author/subject、改动路径/原路径、错误
  文案）；分叉只走 `tree-fork`（不许再引入字符画连线）。
- Agent Team 两栏别混：人员档案进「员工栏」，装配/顺序/调度进「Team 栏」。
- 工作区切换 / chat 结束时工作树、提交记录与工作区更改都应按需刷新，避免陈旧数据。
- 子页未激活时数据面缓存、激活时按需拉取，避免无谓请求。

## 测试

```text
node --test gui/frontend/dist/dock-layout.test.mjs
node --test gui/frontend/dist/git-log-view.test.mjs
node --test gui/frontend/dist/workspace-changes.test.mjs
node --test gui/frontend/dist/tree-fork.test.mjs
go test ./workspace ./gui ./application/core -count=1
```

关键测试：`dock-layout.test.mjs`（默认分区/同栏换序/跨栏置换/脏存储收敛/
持久化 round-trip）、`git-log-view.test.mjs`（归一化/parents 泳道/SVG 渲染/
escape/截断/复制回调）、`tree-fork.test.mjs`（树轨几何 + 泳道算法）、
`workspace/gitlog_test.go`（解析、%P 父提交、非 git 仓库、limit 钳制、集成）、
`gui/bridge_test.go`（Bridge 转发 + 前端契约断言）、
`application/core/workspace_tree_usecase_test.go`（用例转发）。
