# Right sidebar（右侧栏）

## 模块定位

右侧栏与中间主视图共享一套子页停靠布局：对话区子页（**对话 / 轨迹**）与
右栏子页（**状态 / 工作台 / 资源管理器**）共五个视图，可点击切换、拖拽换序、
跨栏置换（拖到另一栏某页签上松开即与该页签互换位置）。布局与激活是纯 UI
状态（`localStorage["seelex.dock.v1"]` 记忆），业务事实全部来自 Application
Snapshot/Event 权威投影；「资源管理器」子页内另有左「文件预览」抽屉 + 右
「工作树 / 提交记录」两面板（面板可拖拽调换顺序，预览宽度可拖，均
localStorage 记忆）。历史检索保留在右栏子页之下的「更多」折叠区。

主要调用方：`app.js` 右侧栏渲染；数据源：`snapshot.runtime`（权威投影）与
`Bridge.WorkspaceTree` / `Bridge.WorkspaceFileCount` / `Bridge.WorkspaceGitLog` /
`Bridge.WorkspaceFileContent`（只读元数据/受控读取桥）。

## 子页划分

| 子页 | 内容 | 数据源 |
|---|---|---|
| **状态** | 项目状态 grid（状态/会话/消息/任务/文件数）+ 概要 + 上下文压缩时间线 | `snapshot.chat/task/conversation`、`runtime` |
| **工作台** | 「目标」面板 + 工作表格入口 + 定时任务面板 | `runtime.goal_skill_active`、`runtime.active_skills`、`task`、`work_table`、`scheduled_tasks` |
| **代码** | 左：文件预览抽屉（点工作树文件打开）；右：工作树 + 提交记录树（可拖拽调换） | `Bridge.WorkspaceFileContent`、`Bridge.WorkspaceTree/FileCount`、`Bridge.WorkspaceGitLog` |

项目标题（`project-heading`）与「历史检索」折叠区跨子页常驻，不属于任何子页。

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

## 资源管理器子页：文件预览 + 工作树 + 提交记录树

子页 3 内部左右分栏（`.code-split`）：左「文件预览」抽屉（`.file-preview-pane`，
默认收起、点击文件自动展开），右栏上下排列工作树与提交记录两块面板（顶部有
拖拽手柄 grip icon）。左抽屉与右栏之间是宽度拖拽分隔条（
`--preview-w`，`localStorage["seelex.preview-pane-width"]`，默认 380px；
展开/收起态存 `seelex.preview-pane-open`，Esc 或关闭按钮收起）。

- **文件预览**：点击工作树文件行 → `Bridge.WorkspaceFileContent(relPath, limit)`
  拉取受控字节（后端 workspace 域 containment/敏感过滤/上限/二进制探测；默认
  文本 4 MiB、文档/图片 24 MiB、后端硬钳 64 MiB，分页类超限放弃渲染并提示）→
  按扩展名分派渲染（markdown=marked→DOMPurify→highlight.js；代码/文本=
  highlight.js 高亮或纯文本；PDF=PDF.js canvas 分页；Word=docx-preview；
  图片=blob img；`.doc` 提示转换）。组件全部本地 vendor（`dist/vendor/`，
  随 embed 离线打包）。实现见 `file-preview.js`；预览内容不进入 Snapshot/业务
  状态，工作区切换时抽屉随树清空。
- **工作树**：`Bridge.WorkspaceTree(relPath, depth)` / `Bridge.WorkspaceFileCount()`
  （后端权威元数据：名称/路径/类型/大小/直接文件计数，不含文件内容）；目录行
  惰性展开，文件行是可点击按钮（打开预览）。实现见 `worktree-view.js`。
- **提交记录树**：`Bridge.WorkspaceGitLog(limit)`（最近 20 条，`git log --all
  --graph` 拓扑行 + hash/作者/时间/标题；只读，不含 diff/文件内容）；graph
  前缀等宽渲染保留分支拓扑，短 hash 可点击复制完整 hash。实现见
  `git-log-view.js`。

### 拖拽调换

两面板支持 HTML5 drag & drop 调换顺序，顺序持久化到
`localStorage["seelex.right.codePanes"]`（默认 `["worktree","gitlog"]`）。子页
激活时按需刷新数据面（工作区切换或 chat 结束产生新提交时重新拉取）。

### 提交记录数据面（后端）

`workspace.GitLog(root, limit)` 在 workspace root 内执行**固定 argv** 的
`git log --all --graph`（不经过 shell、带 5s 超时），解析为结构化
`dto.GitLogResult`（`Lines[]`：graph 前缀 + 可选 `GitCommitNode`；延续线保留
graph、无提交）。非 git 仓库 / git 不可用时以 `Result.Error` 返回展示文案
（不是 Go error），避免 GUI toast 噪音。limit 默认 20、钳制上限 200。
接线：`workspace.Repo.GitLog` → `WorkspaceTreePort.GitLog` →
`application.Service.WorkspaceGitLog` → `Bridge.WorkspaceGitLog`。

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
- 面板渲染文本全部 escape（graph 字符、author/subject、错误文案）。
- 工作区切换 / chat 结束时工作树与提交记录都应按需刷新，避免陈旧数据。
- 子页未激活时数据面缓存、激活时按需拉取，避免无谓请求。

## 测试

```text
node --test gui/frontend/dist/dock-layout.test.mjs
node --test gui/frontend/dist/git-log-view.test.mjs
go test ./workspace ./gui ./application/core -count=1
```

关键测试：`dock-layout.test.mjs`（默认分区/同栏换序/跨栏置换/脏存储收敛/
持久化 round-trip）、`git-log-view.test.mjs`（归一化/escape/截断/复制回调）、
`workspace/gitlog_test.go`（解析、非 git 仓库、limit 钳制、集成）、
`gui/bridge_test.go`（Bridge 转发）、`application/core/workspace_tree_usecase_test.go`
（用例转发）。
