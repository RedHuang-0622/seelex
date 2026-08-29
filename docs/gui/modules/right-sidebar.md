# Right sidebar（右侧栏）

## 模块定位

右侧栏是工作台的工程状态侧栏，按内容划分为三个子页：**状态 / 工作台 / 资源管理器**。
子页切换是纯 UI 状态（localStorage 记忆），业务事实全部来自 Application
Snapshot/Event 权威投影；子页 3 内「工作树 / 提交记录」两块面板支持拖拽调换
顺序（localStorage 记忆）。历史检索保留在子页之下的「更多」折叠区。

主要调用方：`app.js` 右侧栏渲染；数据源：`snapshot.runtime`（权威投影）与
`Bridge.WorkspaceTree` / `Bridge.WorkspaceFileCount` / `Bridge.WorkspaceGitLog`
（只读元数据桥）。

## 子页划分

| 子页 | 内容 | 数据源 |
|---|---|---|
| **状态** | 项目状态 grid（状态/会话/消息/任务/文件数）+ 概要 + 上下文压缩时间线 | `snapshot.chat/task/conversation`、`runtime` |
| **工作台** | 「目标」面板 + 工作表格入口 + 定时任务面板 | `runtime.goal_skill_active`、`runtime.active_skills`、`task`、`work_table`、`scheduled_tasks` |
| **代码** | 工作树（上）+ 提交记录树（下），可拖拽调换 | `Bridge.WorkspaceTree/FileCount`、`Bridge.WorkspaceGitLog` |

项目标题（`project-heading`）与「历史检索」折叠区跨子页常驻，不属于任何子页。

## 目标面板（工作台子页）

「目标」面板展示当前任务的工程目标证据面：

- **目标文本**：会话最近一条非空用户消息（本地派生，截断 120 字展示，title 全文）。
- **任务状态**：`snapshot.task.status` + `summary`（权威 TaskState）。
- **激活 skill**：`runtime.active_skills`（任务级 skill 激活权威投影）+ GOAL badge
  （`runtime.goal_skill_active`）。

无目标文本、无任务且无激活 skill 时整个 section 隐藏。数据在
`application/core/view_state` 收集投影（锁内快照），`runtime.changed` 增量携带，
不需要额外 Bridge 调用。

## 资源管理器子页：工作树 + 提交记录树

子页 3 内两块面板上下排列，顶部有拖拽手柄（grip icon）：

- **工作树**：`Bridge.WorkspaceTree(relPath, depth)` / `Bridge.WorkspaceFileCount()`
  （后端权威元数据：名称/路径/类型/大小/直接文件计数，不含文件内容）；目录行
  惰性展开。实现见 `worktree-view.js`。
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
app.js（右侧栏渲染）
  ├── snapshot.runtime.*（状态/工作台子页，权威投影）
  ├── worktree-view.js ──► Bridge.WorkspaceTree / WorkspaceFileCount
  ├── git-log-view.js ──► Bridge.WorkspaceGitLog
  └── history-search.js ──► Bridge.SearchHistory
```

子页切换/面板顺序是本地 UI 状态（localStorage），不进入 Snapshot；数据面
全部来自后端权威源。

## Review 指南

- 子页切换与拖拽调换不得触发后端调用之外的副作用；`code-panes` 顺序变化
  只写 localStorage。
- git log 查询是只读元数据：不得暴露 diff/补丁/文件内容；参数必须固定 argv。
- 面板渲染文本全部 escape（graph 字符、author/subject、错误文案）。
- 工作区切换 / chat 结束时工作树与提交记录都应按需刷新，避免陈旧数据。
- 子页未激活时数据面缓存、激活时按需拉取，避免无谓请求。

## 测试

```text
node --test gui/frontend/dist/git-log-view.test.mjs
go test ./workspace ./gui ./application/core -count=1
```

关键测试：`git-log-view.test.mjs`（归一化/escape/截断/复制回调）、
`workspace/gitlog_test.go`（解析、非 git 仓库、limit 钳制、集成）、
`gui/bridge_test.go`（Bridge 转发）、`application/core/workspace_tree_usecase_test.go`
（用例转发）。
