# 2026-09-15 前端渲染与内存减负（事件委托 / 共享提示气泡 / 拓扑单传 / tree-fork）

> 日期: 2026-09-15 | 范围: `gui/frontend/dist/*`（`app.js`、`sidebar.js`、
> `work-table.js`、`git-log-view.js`、`worktree-view.js`、`plan-dsl.js`、
> 新增 `tree-fork.js`）、`workspace/gitlog.go`、`application/contract/dto/tree.go`、
> `application/contract/workspace_tree.go` | commit: `5dec51a`

## 一、问题

三处“常驻成本”，都是前端自己制造的：

1. **列表重绘重建监听器**：会话列表、账户栏、插件列表、命令面板内联建议、
   提交记录、工作树的行级动作各挂一整套闭包 + 监听器；每次重绘重建，旧的若
   没拆干净就是孤儿监听。
2. **每行都在“提示”上花 DOM**：行内 `title`/子元素提示随行数增长，长列表里
   是纯粹的常驻负担。
3. **同一份数据传两遍**：`git log` 同时下发 `Commits` 与字符画 `lines`，前端
   两套几何各自维护。

## 二、改法

### 1. 事件委托

行级动作都挂在容器上**一条**监听（`data-*` 标记 + 一次 `closest()` 分派）：
列表重绘不再重建 N 个闭包与监听器，也不留孤儿监听。

### 2. 一条共享提示气泡

单一 DOM `#ui-tooltip`（`[data-tip]` 委托触发、`\n` 分行）替代每行
`title`/子元素提示。会话条目随之改「标题段 + ⋯ 段」：标题段吃满剩余宽度、
CSS 省略号按栏宽截断（数据层不再砍字，同前缀会话仍可区分），时间/token/完整
标题在鼠标常驻时由气泡给出；⋯ 段点开才是置顶/分支/删除。

### 3. 拓扑只传一遍

`git log` 不再同时下发 `Commits` 与 `lines`：后端 `WorkspaceGitLog` 改
`--topo-order` + `%P` 取每个提交的 parents，`dto.GitCommitNode.Parents` 承载
拓扑，`GitLogLine`/`Graph` 字符画字段删除（`workspace/gitlog.go`、
`application/contract/dto/tree.go`、`application/contract/workspace_tree.go`）。

### 4. 统一的叉/分叉渲染件 `tree-fork.js`（纯函数，零额外 DOM）

- `treeRowAttrs`：把「深度 + 是否末子 + 祖先是否续行」折算成树轨 class / 行内
  style（祖先续行轨 = 每层一道 1px `linear-gradient` 背景，自身连接轨 =
  `::before`）；
- `layoutCommitGraph`：由 parents 拓扑算泳道 + 每行线段（`rows[].lane` /
  `dropped`），`commitGraphRowHTML` 逐行 SVG 画直连/合并贝塞尔 + 提交点。

Plan 树、子代理树、工作树、提交记录共用同一套轨道，字符画连线
（`├─`/`└─`/`│`、`| \ /`）全部删除；像素几何只有一份
（`railOffset` / `laneCenter`），换肤只换 token。

### 5. 版面与交互

- 账户栏从左侧栏底部搬到右栏「状态」子页的键值两列表格（`status-table`）之下；
  左栏只留会话树。
- 左右栏可收起（`Ctrl+B` / `Ctrl+J`，`data-*-collapsed` 驱动 CSS，不收
  `--left-w/--right-w`，展开回原宽度）。
- 焦点只作用于**外框**（1px + 0 偏移发散光晕），按压是“陷进去再弹回来”
  （内阴影 + 1~2px 位移），开关态用内阴影表示；`prefers-reduced-motion` 关闭
  全部 transition/animation。

## 三、验证

```text
node --test gui/frontend/dist/*.test.mjs     # 289 passed
go test ./gui/... ./workspace/... ./application/core/... -count=1
```

关键用例：`gui/frontend/dist/tree-fork.test.mjs`（树轨几何 + 泳道算法，含畸形
载荷不 NaN）、`git-log-view.test.mjs`（parents 泳道布局/合并分叉/截断与泳道上限）、
`worktree-view.test.mjs`、`sidebar.test.mjs`（重名消歧 + 两段式）、
`plan-dsl.test.mjs`（树轨事实）、`event-chain.test.mjs`（tree-fork 依赖注入）、
`gui/bridge_test.go`（内嵌前端契约：tree-fork 复用、无字符画、无 `line.graph`、
共享气泡宿主与 ⋯ 段存在）、`workspace/gitlog_test.go`（parents 解析）、
`application/core/workspace_tree_usecase_test.go`。

## 四、边界

- `tree-fork.js` 只做**投影**：拓扑/深度的权威事实仍来自后端（parents、树结构），
  前端不推导父子关系。
- 泳道数量有上限（截断标记），避免超宽历史把 SVG 画到不可用。
