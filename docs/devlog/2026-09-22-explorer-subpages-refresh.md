# 资源管理器子页：三面板 → 三个平级子页 + 内置滚轮 + 原子刷新（2026-09-22）

> 日期: 2026-09-22 | 范围: `gui/frontend/dist/index.html`、`gui/frontend/dist/styles.css`、
> `gui/frontend/dist/app.js`（资源管理器/code 子页区域）、
> `gui/frontend/dist/explorer-pages.js`（新）、`gui/frontend/dist/explorer-refresh.js`（新）、
> `gui/frontend/dist/explorer-pages.test.mjs`（新）、`gui/frontend/dist/explorer-refresh.test.mjs`（新）、
> `docs/gui/modules/right-sidebar.md`（资源管理器子页小节）
> 回归: `node --test gui/frontend/dist/*.test.mjs`（416 例全绿）、
> `go test ./gui/... ./application/core/... -count=1`（全绿，含 `gui/bridge_test.go` 前端契约断言）

## 1. 现象（用户口径）

1. 「资源管理器」子页右侧是**上下堆叠**的工作树 / 提交记录 / 工作区更改三块
   `code-pane`（顶部 grip 手柄 + HTML5 拖拽换序 + `localStorage["seelex.right.codePanes"]`）；
   三块面板同时挤在一列里，谁都得靠整栏滚动才能看全。
2. 滚轮滚的是**整栏**（`.code-panes` 自己 `overflow-y:auto`），滚到工作树末尾会顺势把
   提交记录也顶走；页签条（右栏子页）与面板头部跟着一起滚出视口。
3. 没有逐面板刷新入口：只有"切到资源管理器子页时按需刷新（stale 才拉）"，工作区内容
   变了但数据面已缓存时，用户没有手动重取的手段。
4. 「重新点开资源管理器子页」不刷新：`setDockTab` 对"已激活页签"直接 return，
   `refreshGitLogIfStale/refreshChangesIfStale` 又只在数据面失效时才拉——首次激活之后就
   再也不会重拉。
5. 三块面板各自持有 `gitLogRoot/gitLogLoaded`、`changesRoot/changesLoaded` 这类"根 + 是否
   已加载"的散装状态，没有代次概念：响应回来时只比对根，**无法判断是否已被更新的请求
   覆盖**，也没有"一批一起提交"的约束——极端时序下会出现一块面板是新工作区、另一块是
   旧工作区的数据。

## 2. 根因

- 布局：三块面板共用一个滚动容器（`.code-panes{display:grid;overflow-y:auto}`），
  面板身份只有"上下顺序"，没有"一次只显示一个"的分页概念。
- 刷新：触发时机写在三个 `refreshXxx(snapshot, force)` 里，判据是"根相同 && 已加载"，
  没有单飞（并发触发会叠加请求）、没有代次（过期响应无法识别）、没有整批提交语义。
- 持久化：面板顺序键（`seelex.right.codePanes`）与停靠布局键（`seelex.dock.v1`）之外，
  没有"子页身份"这一层，新增页签后旧值无处可落。

## 3. 改动

### 3.1 纯函数：子页身份与脏存储收敛（`explorer-pages.js`）

- 子页 id 单一事实：`EXPLORER_PAGES = ["worktree","gitlog","changes"]`，
  `EXPLORER_PAGE_META[page] = {label, panelId, viewId, badgeId, failedHint}`（DOM id 也只有
  一份，app.js 不再散写字面量）。
- `normalizeExplorerState(raw)`：order 收敛成三个子页的一次排列（未知项丢弃、缺失项按默认
  顺序补齐），active 必须落在 order 里，否则取可见的第一个页签；脏值整体回退默认。
- `resolveExplorerState(storedRaw, legacyRaw) → {state, migrated}`：新键
  `seelex.right.explorer.v1` 优先；缺新键时消费旧键 `seelex.right.codePanes`——
  **旧三项排列**（旧的首块面板 = 激活页）沿用顺序，**更早的两项存储 / 重复项 / 未知 id /
  坏 JSON** 一律安全回退默认顺序；`migrated=true` 表示旧键已消费（含回退），app.js 随即
  写回新键并 `removeItem` 旧键，存储里不留第二种事实。
- `serializeExplorerState` / `withExplorerPage` / `legacyPaneOrder` / `isExplorerPage` 配套。
- `seelex.dock.v1` 不需要迁移：五个视图 id 与分区模型没变，旧值照旧由
  `dock-layout.js:normalizeDockState` 收敛（既有测试已覆盖）。

### 3.2 原子刷新能力（`explorer-refresh.js`）

`createExplorerRefresh({load, commit, onError}) → {setRoot, refresh, currentRoot, generation, inFlight}`：

- **single-flight**：`refresh(pages)` 在飞行期间返回**同一个 promise**；飞行期间到来的新页
  请求并入当前批（下一轮只拉本批还没拿到的页），已在本次批里加载过的页请求直接算满足
  ——不叠加请求。
- **根 + 代次提交**：每次开刷新 `generation + 1`，`setRoot` 也 `+1` 并把在飞请求从单飞槽位
  摘下（否则紧接着的刷新会被当成"复用"而丢掉）；响应回来时 `ticket.generation !== generation
  || ticket.root !== root` 即判 `stale`，丢弃不提交。
- **整批提交 / 整批失败**：一次刷新的全部页在同一个 `commit(entries, ticket)` 里落地；任一页
  失败则整批不落地（已拿到的页也丢弃），只调用一次 `onError(error, {pages, root, generation})`
  ——旧数据保留、只提示、不清空。
- 状态：`REFRESH_STATUS = {COMMITTED, STALE, ERROR, NOOP}`。

### 3.3 DOM/接线（app.js：只做接线，不推演 git 语义）

- 页签渲染/切换/持久化：`renderExplorerTabs` / `applyExplorerPageState` / `setExplorerPage`；
  点击走 `#code-panes` 上的一条委托（页签是真 `<button>`，Enter/Space 由浏览器转 click）；
  页签切换**只切显隐**，不触发 Bridge 调用。
- 数据面：`refreshExplorerPages`（唯一刷新入口）/`loadExplorerPage`（3 个只读 Bridge 方法）/
  `commitExplorerPages`（整批渲染 + 计数）/`reportExplorerRefreshFailure`（整批失败提示）；
  `syncExplorerData` 收敛触发时机（根变化 / chat 结束），`resetExplorerData` 三面板一起清空。
- 触发点：`runViewActivation("code")`（由非激活变激活）、`setDockTab` 里"再次点击已激活页签"、
  三个子页头部的刷新按钮（`[data-page-refresh]`，只刷该子页）。
- 删掉：`RIGHT_PANE_ORDER_KEY`/`storedPaneOrder`/`persistPaneOrder`/`applyPaneOrder`/`initCodePanes`/
  `panes()`、`gitLogRoot/gitLogLoaded/changesRoot/changesLoaded`、`refreshGitLog/refreshChanges/
  refreshWorkTree/resetGitLog/resetChanges/refreshGitLogIfStale/refreshChangesIfStale`
  （能力全部上移到两个新模块与 `refreshExplorerPages`）。
- 前端仍不解释 git 语义：状态字母、分类、统计全部来自 `Bridge.WorkspaceChanges`；
  文本 escape 仍在 view 模块（`git-log-view.js` / `workspace-changes.js` / `worktree-view.js`）。

### 3.4 结构与滚动（index.html + styles.css）

- `#code-panes` 变成「页签条 + 当前子页面板」两行网格（`grid-template-rows: auto minmax(0,1fr)`，
  `overflow: hidden`）；`.code-pane` 自身 `grid-template-rows: auto minmax(0,1fr)` + `overflow:hidden`，
  未激活面板 `.is-hidden{display:none}`——一次只有一个子页参与布局。
- `.code-pane-body`（三个内容区共用的类）就是滚轮容器：`overflow-y:auto; overscroll-behavior:contain;
  scrollbar-width:thin`，内容不溢出即无滚动条；原 `.git-log-view` / `.changes-view` 的
  `max-height:320px` 去掉（改由面板高度决定），滚动条不再出现在整栏上。
- 每个子页头部新增刷新按钮（`.icon-button.subtle.code-pane-refresh` + `data-icon`）。
  图标集（`components.js`）不在本轮白名单内，故复用现有 `recall`（回读/回转箭头）承载"刷新"，
  文案由 `title`/`aria-label` 明确（这是本轮唯一将就：图标名不是 `refresh`）。

## 4. 验证（真实输出）

```text
$ node --test gui/frontend/dist/explorer-pages.test.mjs
▶ explorer: 默认状态是三个平级子页的一次排列，激活首个子页
...
# tests 6 / pass 6 / fail 0

$ node --test gui/frontend/dist/explorer-refresh.test.mjs
▶ refresh: 重复触发复用同一 promise，不叠加请求
▶ refresh: 飞行期间更宽的请求在同一批内补齐，整批一次提交
▶ refresh: 根已变 → 旧响应丢弃；新根的结果按新代次提交
▶ refresh: 任一页失败 → 整批作废（旧数据保留）只提示
...
# tests 7 / pass 7 / fail 0

$ node --test gui/frontend/dist/*.test.mjs
# tests 416 / pass 416 / fail 0

$ go test ./gui/... ./application/core/... -count=1
ok  github.com/RedHuang-0622/seelex/gui  7.787s        # 含 bridge_test.go 前端契约断言
ok  github.com/RedHuang-0622/seelex/gui/terminal  15.539s
ok  github.com/RedHuang-0622/seelex/application/core  27.213s
（其余 application/core/* 子包全部 ok）
```

## 5. 遗留与边界（不在本轮白名单内，留给后续）

- `docs/gui/modules/right-sidebar.md` 的「模块定位」「依赖方向」「Review 指南」「测试」小节仍写着
  "三面板 / 可拖拽调换 / `code-panes` 顺序"。本轮只被授权改「资源管理器子页」「拖拽调换」
  「子页划分」表格行，未动这些小节。
- `gui/frontend/README.md:225` 仍描述「代码」子页的"两块面板可拖拽调换（grip 手柄，
  `seelex.right.codePanes`）"，同样不在白名单内。
- 子页页签顺序只**迁移**旧记忆，不提供新的拖拽改序入口（用户口径只要页签切换 + 持久化）；
  若要恢复"可调序"，应在新页签条上加拖拽，而不是把三面板堆叠改回来。
- `components.js` 缺 `refresh` 图标（本轮不得改该文件），已用 `recall` 顶替。
