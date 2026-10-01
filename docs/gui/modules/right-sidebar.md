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
`Bridge.WorkspaceChanges` / `Bridge.WorkspaceGitCommitDetail` /
`Bridge.WorkspaceFileContent` / `Bridge.WorkspaceGitCommitFileContent`
（只读元数据/受控读取桥）。

## 子页划分

| 子页 | 内容 | 数据源 |
|---|---|---|
| **状态** | 项目状态表（键值两列：状态/会话/消息/任务/待审批/文件数）+ 概要 + **上下文压缩**（都在同一个折叠区里，压缩块紧跟概要）+ **账户栏**（状态一栏之下）+ **Agent Team**（员工库 / 团队库 / 员工栏 / 发言调度四块表格） | `snapshot.chat/task/conversation`、`runtime`（含 `runtime.accounts` / `runtime.account`） |
| **工作台** | 「目标」面板 + 工作表格入口 + 定时任务面板 | `runtime.goal_skill_active`、`runtime.active_skills`、`task`、`work_table`、`scheduled_tasks` |
| **资源管理器** | 左：文件预览抽屉（点工作树或工作区更改的文件行打开）；右：三个平级子页 工作树 / 提交记录 / 工作区更改（页签切换、激活态持久化，各自滚动、各自刷新按钮）；提交记录可再下钻两层（提交详情 → 该提交内的文件内容） | `Bridge.WorkspaceFileContent`、`Bridge.WorkspaceTree/FileCount`、`Bridge.WorkspaceGitLog`、`Bridge.WorkspaceChanges`、`Bridge.WorkspaceGitCommitDetail`、`Bridge.WorkspaceGitCommitFileContent` |

项目标题（`project-heading`）与「历史检索」折叠区跨子页常驻，不属于任何子页。
状态子页自上而下：`状态`（折叠区里依次是状态表 / 概要 / **上下文压缩**）→ `账户` → `Agent Team`。

### 上下文压缩（状态子页）

**位置**：`#context-compactions` 挂在 `#status-panel` 折叠区**里面**、紧随概要
（`#project-overview`）之后——用户口径「上下文压缩内容需要放到状态的概要下面」
（2026-09-24 反转了 2026-09-24 早先"挪到折叠区外面"的处置，见
`docs/devlog/2026-09-24-compaction-frontier-singleton-and-frame-popup.md` §2.4）。
折叠区默认收起，所以"按下回车到底动没动"这条可见性改由视图侧兜住：
`app.js: repaintCompactions` 见到本轮门禁进度（`compactionProgress`）就调用
`revealStatusPanel()` 把折叠区打开（只开不收）；**历次记录不触发打开**——那是用户
展开 状态 才读的静态事实，自动弹开会跟用户手动收起打架。两条一起才是完整口径：
只挪进去 = 折叠态下进度条与记录又整块消失，只自动展开不挪 = 又回到点名的"没放在概要下面"。

渲染集中在 `gui/frontend/dist/context-summary.js`，一块里叠了三种来源不同的东西：

- **门禁进度条（瞬态，不进快照）**：后端每收一关发一条 `compaction.progress`
  （会话级、`revision=0`），一轮的形状是起手帧 → 六关收口 → 恰好一个终局。起手帧
  （`phase=begin`）**只有显式压缩有**（`/compact`、`compact_context`）：自动路径"要不要
  折叠"正是判据估算的结果，提前宣告会在不折叠的那几轮里说谎。六关顺序的唯一事实在后端
  `context_runtime.CompactionGates`（判据估算→装配→替换 provider 历史→渲染帧→存帧→写记录），
  前端文案表 `compaction-format.js` 的 `compactionGateLabels` 键序钉住它（有测试盯着）。
  轨道复用 Plan 面板同款 `.plan-board-progress` / `.plan-board-bar`，不自造第二套进度组件；
  `role=progressbar` 与 `aria-valuemin/max/now`（= 总关数 / 已收口关数）挂在外层
  `.context-compaction-progress` 上。轨道下挂**逐关耗时清单**：整轮只有几十毫秒，读者看不见"慢慢走"的
  条，能回答"它到底干了什么、慢在哪一关"的只有每关自己的墙钟（`elapsed_ms` 是逐段值，
  各帧相加才是总数）。生命周期一轮即结束：视图侧 `app.js: applyCompactionProgress` 在终局后
  保留 2.5s（失败 6s）让人读完结论，再自动撤条；逐帧累计是纯函数
  `compaction-format.mergeCompactionProgress`。**没折叠就没有进度**，但"零记录 + 有进度"
  仍要出条——折叠发生在写记录之前。
- **压缩记录条目（快照事实）**：`snapshot.task.context_compactions` 的公开元数据
  （版本 / 原因 / 来源 / 被压区间 / 估算 / 时间）。展示口径与轨迹「压缩」轨共用同一份纯函数
  （`compaction-format.js`）：两处各写一份就会出现同一条记录两种读法。
- **折叠帧正文（按 ref 分页读回）**：条目上的「查看帧正文」经
  `Bridge.ToolResultContent(frame_ref, offset, 12000)` 读回那一页，复用轨迹详情的同一容器与
  分页交互（`.axis-detail` / `data-compact-frame-load`），不另开面板。正文不进快照，前端只按
  ref 取；没有 `frame_ref` 的条目直说「本次没有可回读正文」，不画一个点了没反应的按钮。

### 账户栏（状态子页）

账户从左侧栏底部搬到「状态」子页的状态一栏之下：状态区显示的就是当前账户的
provider/model，账户列表贴在它下面，一眼对得上"现在是谁在跑"。切换经
`Bridge.SelectAccount(name)`；条目化单列表格行（○/● 当前标记 + 名字 +
provider · model + 不可用标记），点击走 `#account-list` 容器委托（列表刷新不
重建行级监听）。渲染见 `app.js: renderAccounts`。

### Agent Team（状态子页）

面板拆成四块，**一块只管一份事实**（用户口径：语意弄清楚、别功能耦合），各自条目化
（`role=table` 的网格表格 `.team-table/.team-table-row`）：

| 块 | 唯一职责 | 事实域（谁写、写哪） | 行内动作 |
|---|---|---|---|
| **员工库** | 员工是谁 + **档案（提示词 / 权限 / 类型）的唯一编辑入口** | 全局母本 `<root>/team/employees.json`（`AgentTeamSaveEmployee` / `AgentTeamDeleteEmployee`） | 库里的行：入职（装进本会话 / `AgentTeamInstantiateRole`）/ 修改 / ✕；只在本会话在编的行：入库（写母本、不装配）。**没有拖拽**（2026-10-01：「入职」取代了"拖进顺序"） |
| **团队库** | 用户自己的团队（有谁、装配后谁在编） | 全局母本 `<root>/team/library.json`（`AgentTeamMaterializeTeam` / `AgentTeamDeleteTeam`） | 点团队名开团队面板 / 装配（当前那支显示"已装配"）/ ✕。**没有内置形态**：形态目录已删（2026-10-01），面板上既没有形态 chip 行，也没有任何"按形态装配"的按钮 |
| **员工栏** | 本会话**在编名单**（次序 = 登记先后） | 会话副本 `session/team/roles.json` + `lifecycle.order_roles`（`AgentTeamSetOrder` / `AgentTeamDeleteRole`） | ✕ 摘除（出顺序、留角色）/ 删除（连会话注册表一起删）/ 点名字看独立会话 / `+ 入职`。**面板不提供任何调序通道**（拖拽调序、位置列、↑/↓ 已删，2026-10-01：顺序归 leader 的 team plan） |
| **发言调度（Team 栏）** | 运行态：轮次 / 下一个 / 收束 | `TeamView.schedule`（权威投影，前端不推演） | 无（只读串珠条） |

**耦合点已按口径删掉（2026-09-24）**：员工栏行内原来的「编辑」打开的是**会话作用域**的
入职/修改面板——同一个人在两处（员工库 / 员工栏）各有一套"改提示词 / 权限 / 类型"的按钮，
写两份事实、必然要漂移（视图侧因此还得有 `teamGlobalDrift` + 「入库」+ 漂移提示兜着）。
现在档案的唯一编辑入口是员工库；员工栏只**回显**本副本现状（权限 / 提示词 chip 的 title
写明"本会话在编副本"），要改就回员工库改、再「入职」一次（同 `role_name` 幂等覆盖）。
`app.js` 里接 `data-team-edit` 的分支随之删除（没有读者的契约不留）。
三块栏头的 `title` 各自写明事实域（员工库=全局、团队库=全局、员工栏=本会话）；上一轮已钉住
"栏头不摆注解文字"，所以域只在 hover 里说，不占版面（测试见 `agent-team-view.test.mjs`
的 "head carries no annotation text" 与 "员工栏只写本会话"）。

**人工编排已整体撤掉（2026-10-01）**：面板不再给任何"编排手势"——「团队形态」不是设定
（内置形态目录已随 Go 侧 `agentteam/presets.go` 删除，`team_kind` 只是团队名的别名），
`order_policy` 面板连只读展示都不给（它是只回读、不驱动轮次的历史字段），发言顺序的唯一
来源是**登记（入职 / 装配）的先后**。目标态里顺序归 leader 的 team plan
（`stages[].depends_on`），人在面板上要做的只是"谁在编"。

顺序的唯一事实是会话 `lifecycle.order_roles`：前端只在**摘除**（`AgentTeamSetOrder`
带上既有 `order_policy`）时提交整张顺序表，不缓存、不乐观重排，每次动作后重拉视图
（`Bridge.AgentTeam*`）。

角色会话详情里，**未同步草稿独立成区**（`renderRoleDraftBlock`，见
`docs/devlog/2026-09-22-team-role-draft.md`）：草稿不混进"已发布"车道，而是排在既有
行之后、按 `round/unit` 成列，每行带 `is-draft` 类 + 「未同步」chip +
`data-draft-round/-unit/-kind` 凭据；记录表里对应的列头与格子同样标成草稿
（`.role-record-draft-head` / `.role-record-cell.is-draft`，样式在 `styles.css`）。

判据只来自后端投影，前端不推演：会话工作区把 `draft_rows` 与 `main_rows/role_rows`
分列返回（`sessionstore/role_session.go` 的 `syncRoleDraft` / `readRoleSnapshot`），
**一轮结束时**的 `SyncRoleDraft` 才把草稿发布成 message（草稿行不带 seq，发布后
draft 文件删除）。所以「本轮完成」= 草稿变成已发布行、「本轮取消」= 草稿按未同步
原样留着——前端只渲染这份权威投影，不做回写。

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

## 资源管理器子页：三个平级子页（工作树 / 提交记录 / 工作区更改）+ 文件预览

子页 3 内部左右分栏（`.code-split`）：左「内容详情」抽屉（`.file-preview-pane`，
默认收起、点击文件自动展开），右栏是**三个平级子页**——上方一条内嵌页签（点击切换、
激活态高亮并持久化），一次只显示一个子页的内容区；每个子页头部一枚刷新按钮
（`icon-button` + `data-icon`），内容区自己就是滚动容器（`overflow-y: auto`，滚轮
只滚本子页，不滚整栏；内容不溢出时不出滚动条）。左抽屉与右栏之间是宽度拖拽分隔条
（`--preview-w`，`localStorage["seelex.preview-pane-width"]`，默认 380px）。宽度
**无固定上限**：只按子页可视宽封顶（`calc(100% - 6px)`，拖动时 JS 与容器宽对齐），
内容详情与工作树谁宽谁窄完全由拖动决定；左/右主栏同理（`--left-w`/`--right-w` 上界
按视口宽动态计算）。形态四态由 `.code-split` 的类切换：收起 `.is-closed`、展开
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
  提交记录里的文件内容**不走这个抽屉**：抽屉按路径认身份、且带编辑面（Ctrl+S 直接写
  工作区文件），而历史版本与工作区同名文件会是同一枚 chip——一次保存就能把某个历史
  版本写回工作区。那一层在提交记录面板内只读渲染，渲染分派仍共用
  `file-preview.renderReadOnlyContent`。
- **工作树**：`Bridge.WorkspaceTree(relPath, depth)` / `Bridge.WorkspaceFileCount()`
  （后端权威元数据：名称/路径/类型/大小/直接文件计数，不含文件内容）；目录行
  惰性展开，文件行是可点击按钮（打开预览）；层级连线用 `tree-fork` 的树轨
  （祖先续行轨 + 末子弯头，零额外 DOM），行点击走容器委托。实现见
  `worktree-view.js`。
- **提交记录**：`Bridge.WorkspaceGitLog(limit)`（最近 20 条：按 `--topo-order`
  的提交行 + 每个提交的 `parents` 父 hash + hash/作者/时间/标题；只读，不含
  diff/文件内容）；分叉由前端算泳道并画 SVG（直线/合并曲线 + 提交点，
  `tree-fork.layoutCommitGraph`），短 hash 可点击复制完整 hash。**这份面板是三层
  的**（2026-09-29 起）：点开某一条提交 → 这次提交改了哪些文件
  （`Bridge.WorkspaceGitCommitDetail(hash, limit)`：状态字母/路径/重命名原路径/
  ±行数，删除的行不可下钻）；再点开清单里的一行 → 该文件**在那个提交时**的内容
  （`Bridge.WorkspaceGitCommitFileContent(hash, path, limit)`，字节来自 git 对象库，
  只读）。返回键逐层退回（文件清单 → 提交列表），刷新（chat 结束 / 手动刷新）只换
  列表数据、不把用户从已打开的提交里踢出去，工作区切换才整块清空（`reset`）。
  实现见 `git-log-view.js`。
  两层下钻的数据源就是 `workspace.GitCommitDetail` / `workspace.GitCommitFileContent`
  （见下「提交详情数据面」）——面板不解释 git 语义，状态字母与中文标签全部来自后端
  的 `Kind`/`Letter`，只有"怎么渲染一份字节"复用文件详情抽屉的
  `file-preview.renderReadOnlyContent`。
- **工作区更改**：`Bridge.WorkspaceChanges(limit)`（最近 200 条未提交改动：
  行内是状态字母/路径/重命名原路径/暂存标记，头上是「分支 · 暂存 x · 未暂存 y ·
  未跟踪 z · 冲突 w」统计；只读元数据，**不含 diff 与文件内容**）。点文件行打开
  预览（与工作树同一个抽屉）；已删除的文件没有字节可读，行不可点（title 说明）。
  实现见 `workspace-changes.js`。

### 子页页签、顺序与刷新

三个子页是同一层级的页签，不再上下堆叠：顺序与激活页是纯 UI 状态，落盘在
`localStorage["seelex.right.explorer.v1"]`（`{"order":[...],"active":"worktree"}`；
收敛函数在 `explorer-pages.js`：`normalizeExplorerState` / `resolveExplorerState` /
`legacyPaneOrder` / `serializeExplorerState`）。旧版面板堆叠的拖拽顺序记忆
`localStorage["seelex.right.codePanes"]` 一次性迁移：恰好是三个子页的一次排列就沿用
其顺序（旧的首块面板成为激活页），旧的两项版本与任何脏值一律安全回退默认顺序；迁移
（含回退）后旧键即删除，存储里不留第二种事实。页签切换只切显隐，不触发 Bridge 调用。

刷新是**原子能力**（`gui/frontend/dist/explorer-refresh.js`）：

- **single-flight**：同一时刻只有一次在飞的刷新；重复触发（重复点页签 / 连点刷新）
  复用同一 promise，不叠加请求。飞行期间到来的新页请求在同一批内补齐，已在本次批里
  加载过的页不重拉；
- **根 + 代次提交**：结果按「工作区根 + generation」判定，根已变（切换工作区）或已有
  更新代次时过期响应直接丢弃；一次刷新是一批页，整批一起提交——三个面板绝不新旧混搭；
- **失败整批保留**：任一页失败则整批不落地（已拿到的页也不提交），只提示一次，旧数据
  与旧计数留在面板上，不清空；
- **触发时机**：切到「资源管理器」子页（由非激活变激活）、**再次点击已激活的
  「资源管理器」页签**、各子页头部的刷新按钮（只刷该子页）、绑定/切换工作区、
  一轮 chat 结束。切换子页页签本身不拉数据。

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

### 提交详情数据面（后端）

`workspace.GitCommitDetail(root, hash, limit)` 回答"这次提交改了哪些文件"，
`workspace.GitCommitFileContent(root, hash, relPath, limit)` 回答"这个文件在这个
提交时是什么内容"。两者共用同一组收敛（路径基准、hash 形状校验、超时与失败文案、
可见性边界），所以放在同一个文件（`workspace/gitcommit.go`）里：

- **清单**由三条固定 argv 查询拼出：头部事实（`show --no-patch
  --pretty=format:` + 提交列表同一条字段口径）+ `show --format= --name-status -z
  --find-renames --diff-merges=first-parent --end-of-options <hash> -- .` +
  同参数换 `--numstat` 取 ±行数（按新路径与名状态对齐）。`--format=` 抑制头部是必需的：
  `--name-status`/`--numstat` 与 `--no-patch` 不能同时出现（git 直接报错），而
  `--diff-merges=first-parent` 让合并提交按首父给出差异（默认合并提交什么都不给）。
- **内容**读 git 对象库：`cat-file -t` 先确认是 blob（目录 tree 与子模块 gitlink
  不是文件，明确失败而不是把目录清单当正文），`cat-file -s` 取真实字节数（截断提示
  要说总数），`show` 读前 limit 字节并在读够后关掉读端。返回形状与工作树预览**完全
  相同**（`dto.FileContent`），因此前端两条通道共用一套渲染分派。
- **参数形状**：hash 必须是 4~64 位十六进制（修订表达式不受理；`--output=` 之类
  会被 git 当选项解析），进程侧再叠加 `--end-of-options` 与路径前的 `--`。
- **路径与可见性**：清单路径剥掉仓库根前缀（工作区根可能是仓库子目录），命中敏感
  文件名计入 `Filtered`（面板显式提示）；内容读取走 `sanitizeWorkspaceRelPath`
  ——与 `ReadFile` 同一条边界（相对路径、containment、忽略目录、敏感文件名）。

接线：`workspace.Repo.GitCommitDetail` → `WorkspaceTreePort.GitCommitDetail` →
`application.Service.WorkspaceGitCommitDetail` → `Bridge.WorkspaceGitCommitDetail`；
`workspace.Repo.GitCommitFileContent` → `WorkspaceFilePort.GitCommitFileContent` →
`application.Service.WorkspaceGitCommitFileContent` → `Bridge.WorkspaceGitCommitFileContent`。
（内容读取与预览读取同属文件端口：Application 侧 `workspaceFilePort()` 一处解析 root，
两条通道不可能解析到不同的工作区。）

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
  ├── git-log-view.js ──► Bridge.WorkspaceGitLog / WorkspaceGitCommitDetail /
  │                        WorkspaceGitCommitFileContent ──► file-preview.renderReadOnlyContent
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
- 提交详情（点开某条提交）同样是只读元数据：不得暴露补丁；hash 必须按形状校验
  （修订表达式与选项形状一律拒绝，进程侧叠加 `--end-of-options`）；提交内文件内容
  只走 `WorkspaceGitCommitFileContent`，且必须与工作树预览共用同一条可见性边界与
  同一套渲染分派——**不得**把历史版本塞进带编辑面的文件详情抽屉（同名文件会串台，
  一次 Ctrl+S 就能把历史版本写回工作区）。
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
node --test gui/frontend/dist/file-preview-render.test.mjs
node --test gui/frontend/dist/workspace-changes.test.mjs
node --test gui/frontend/dist/tree-fork.test.mjs
go test ./workspace ./gui ./application/core -count=1
```

关键测试：`dock-layout.test.mjs`（默认分区/同栏换序/跨栏置换/脏存储收敛/
持久化 round-trip）、`git-log-view.test.mjs`（归一化/parents 泳道/SVG 渲染/
escape/截断/复制回调 + 提交详情归一化与渲染、三层下钻与返回、迟到结果丢弃、
没有加载器时不产生死链接）、`file-preview-render.test.mjs`（共用只读渲染入口：
分派、空/二进制/截断提示、认调用方给的 kind）、`tree-fork.test.mjs`（树轨几何 +
泳道算法）、`workspace/gitlog_test.go`（解析、%P 父提交、非 git 仓库、limit 钳制、
集成）、`workspace/gitcommit_test.go`（名状态/numstat 解析、hash 形状、提交详情集成、
对象库读取与截断、目录与敏感路径拒绝）、`gui/bridge_test.go`（Bridge 转发 +
前端契约断言，含提交记录下钻接线）、
`application/core/workspace_tree_usecase_test.go` 与 `workspace_file_usecase_test.go`
（用例转发与后端能力缺失时的显式报错）。
