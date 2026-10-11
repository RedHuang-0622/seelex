# 前端一致性与缺陷修复开发文档（2026-10-12）

> **状态：待审核（Review Gate）。** 审核通过后按 §6 拆成 teamwork（里程碑 + Work Item）执行。
>
> **来源**：用户 2026-10-12 多轮口述清单与答复：
> 1. 原始清单（原 7 条）+ 「视觉基线」口径——**组件风格一律参考 Seelex 的左边栏**；
> 2. 「LLM 内容框底部 / 工具过程顶部」按**有无相邻**决定顶/底 0 圆角或顶/底有圆角（F-6）；
> 3. **口径更正**：「召唤 GUI 在侧边的资源管理器打开需要用户审阅的文件」**也是前后端联调**（原第 7 条不是纯前端）= J-2；
> 4. **上一轮六个阻塞项的答复**（F-1/F-2/F-4/F-6/J-1/J-2）→ 全部收进 §7.1 决议表并落进各条目；
> 5. **F-7（新）**：内容是 HTML 时，只读模式要 HTML 预览，点编辑才是代码；
> 6. **J-1 文件引用（新）**：`@文件` + composer 加号；**同日二次口径**：文件内容由 **Seelex 自己 `read_file`/`grep`**，前端只传**文件地址**并负责渲染（`@` 也要渲染）⇒ 不新增 `reader` 角色、不做多模态内容搬运（判定记录见 J-1b）。
>
> **落点**：前端 `gui/frontend/dist/`（dist 即源、无 bundler）；联调项另有 Go 侧落点（`main.go` / `gui/` / `seelebridge/` / `application/core/`）。
>
> **本文档的证据分级**：`已确认` = 本轮实际 grep/read 到 file:line；`待钉` = 方向明确但落点数值未量测，实现前必须先钉住。

---

## 0. 清单与分类（唯一事实）

| ID | 条目（用户口径） | 层 | 关键落点 | 状态 |
|---|---|---|---|---|
| B0 | 视觉基线：组件风格参考**左边栏** | 前端（贯穿） | `gui/frontend/dist/styles.css`（`.session-row` 族）、`sidebar.js` | 新增（口径） |
| F-1 | 对话区右侧**对话轴**不悬停发现不了；语义应读作对话分支导航触发器 / 历史节点浮窗热区 | 纯前端 | `conversation-wheel.js`、`conversation-view.js`、`styles.css:3388-3469` | 待修 |
| F-2 | 工作台**表格弹窗底部灰色挡板**（关于"分片"），横向滚动后长度与表格不一致 | 纯前端 | `work-table.js:482-518`、`styles.css:2848-2868` | 待修 |
| F-3 | 资源管理器**子页分页风格**、子页条目**不向上紧凑**、**内容详情高亮** | 纯前端 | `index.html:219-241`、`styles.css:1468-1532`、`explorer-pages.js`、`worktree-view.js` / `git-log-view.js` / `workspace-changes.js` | 待修 |
| F-4 | **上下文压缩帧条目臃肿**；侧边只显示摘要；要标明**栈顶**；**门禁过程风格不统一** | 纯前端 | `context-summary.js`、`compaction-format.js`、`index.html:158/536-540` | 待修 |
| F-5 | 内容详情**没有 IDE 风格代码高亮**；**md 的 mermaid 不渲染** | 纯前端（含 vendor 资产） | `file-preview.js:950-963`、`styles.css:1651`、`markdown.js`、`dist/vendor/` | 待修 |
| F-6 | LLM 块底 / 工具过程顶按**邻接**决定圆角（融合 vs 分开） | 纯前端 | `components.js:355/435/488`、`styles.css:558-572` | 新增（本轮） |
| F-7 | 内容是 **HTML** 时，只读模式要 **HTML 预览**，点编辑才是代码（本轮新增） | 纯前端 | `file-preview.js:38/63/839-890/932+`、`html-embed.js:1-20` | 新增（本轮） |
| J-1 | **Agent Team 退场** + **`@文件`** + **composer 加号**（**只传文件地址**，内容由 Seelex 自己 `read_file`/`grep`） | **前后端联调** | `index.html:164-180, 83-96`、`agent-team-view.js`、`app.js:2835-2882`、`gui/bridge.go:167-192, 669, 881-882`、`headless_team.go`、`application/core/input_team.go`、`application/core/agentteam/`、`gui/frontend/dist/file-drop.js` | 待修（见 J-1 + J-1b） |
| J-2 | **Agent 驱动 GUI** 在侧边资源管理器打开待审阅文件（多文件 chip，详情只显示一个） | **前后端联调**（本轮更正） | `index.html:205-213`、`file-preview.js`、`app.js:497-529`、`main.go` 注册族、`seelebridge/`、`application/contract/dto` | 待修 |

**分类结论（更正后）**：除 **J-1（Agent Team 退场 + `@文件` / 加号 = 只传文件地址）** 与 **J-2（Agent 驱动打开文件，一次批处理上限 10 份）** 两条是前后端联调外，**B0 + F-1～F-7 全部是纯前端改动**。

> **§7 的口径已全部拍板（见各条目内「已拍板」标注与 §7 的决议记录）；§7 现只保留本轮新产生的未决项。**

---

## 1. B0 视觉基线：以左边栏为唯一组件风格基准（所有条目共用）

**唯一基准 = 左侧栏会话列表**（`#sidebar` 的会话行族）。已确认的锚点：

| 事实 | 落点 |
|---|---|
| 行 = grid（内容列 + 右侧动作列，`gap: 2px`），按钮自身 `min-width: 0` | `styles.css:2723-2724` |
| 行外壳：`min-height: var(--row-min-h)`、`padding: 6px 8px`、`border-radius: var(--r-md)` | `styles.css:3961` |
| 悬停：换边色 + 换底 | `styles.css:3621` |
| 选中：`.is-active` / `[aria-current="true"]` | `styles.css:3623-3624` |
| 行内控件密度（button 22px、字号 12px、状态徽标 9px） | `styles.css:4688-4691` |
| 头部/动作列对齐、菜单触发器的显隐 | `styles.css:4532-4536` |

**基线规则（后续所有条目必须遵守）**：

1. **半径只走 token**：`--r-sm` / `--r-md` / `--r-lg`，不得硬编码 `3px` / `8px` / `999px`（现有例外见 §4）。
2. **悬停与选中只用既有信号**：hover = 边色 + `--surface`；选中 = `.is-active` / `[aria-current]` 同族写法。不得引入第二套"选中色"。
3. **行密度走 `--row-min-h`**：子页/列表行与左栏同一口径，一行一条，不留双行高。
4. **横向贴边条必须与它的滚动容器同宽**（F-2 正是违反这条）。
5. **不新增独立配色语汇**：颜色一律取主题 token（`--surface*` / `--border*` / `--text*` / `--status-*` / `--accent`），皮肤矩阵（`dist/themes/glass|graphite|paper|qoder|silver|verdigris`）不需为新组件再补一套。

> B0 不是一个独立交付物，而是 F-1～F-7 的验收前提：每条修复都要能说清"为什么这样改 = 与左栏同族"。

**既有的"风格闸"（本轮已确认，扩展它、不要新造）**：

| 闸 | 机制 | 与本文档的关系 |
|---|---|---|
| `dist/text-button-chrome.test.mjs:1-30` | 自己解析 `styles.css`（`cssRules` 把 `选择器 → 声明体` 展开），把"裸类名按钮必须有背景"立成通用护栏——注释里记的现场就是"提交标题整列变黑条" | **B0 规则 1/2 可以直接用同一手法钉住**（例如"不得出现硬编码 `border-radius: 8px`"，只许 `--r-*`） |
| `dist/theme.test.mjs:1-28` | 两轴契约：`styles.css` 的 `:root` / `:root[data-theme="dark"]` 供中性基座 token；`themes/<skin>.css` 只供品牌 token，且必须同时给浅/深两个变体 | F-5 补 hljs 配色、F-1/F-3/F-6 改样式都必须遵守这两轴，不能新增第三种供色方式 |
| `dist/consistency.test.mjs:1-45` | 跨视图一致性：对话 chip ↔ 轨迹行同 key / 同 status / 同 duration / 同 size（用 `renderConversationModel` 渲染） | 是 F-6（邻接类进 `components.js` 输出）与 F-1（刻度）的天然回归面 |

---

## 2. 纯前端条目

### F-1 对话轴：从"隐形的线"变成可发现的导航件

**现象（用户原话）**：对话区右侧的对话轴"不悬停鼠标到上面都发现不了"；前端语义上是**对话时间线 / 分支导航条**，准确叫法是「**对话分支导航触发器**」「**历史节点浮窗热区**」。

**证据（已确认）**：

- 组件身份：`conversation-wheel.js` 头部注释（`:1-27`）写明这是「对话区右侧**会话内全量用户输入索引**」，2026-09-11 重做，**取代**原「对话导航轮轴」；一条刻度 = 会话里的一条用户输入，助手步骤/思考/工具/系统行**不产生刻度**（`:5-8`）；未加载轮次按确定性比例插值（`:9-13`）。
- 挂载：`conversation-view.js:32` → `createConversationWheel(container, { locateInput })`；容器 `#conversation-shell`（`index.html:61`、`app.js:127`）。
- **不可发现的根因（已确认）**：`styles.css:3388-3398` `.conversation-shell .conversation-wheel { position:absolute; width:34px; opacity:.45; … }`，且只有 `.conversation-shell:hover … / :focus-within { opacity: 1 }`（`styles.css:3398-3399`）→ 默认 45% 透明度、且提升不透明度的唯一途径是悬停整个对话外壳。
- 位置：`right: max(10px, calc((100% - 860px)/2 - 74px))`（`styles.css:3392`）——贴在内容列右侧留白里，不与任何可见控件成组。
- 视觉要素极细：刻度 3px 短线 + 最小间隔 11px + 当前刻度加长系数 0.4（`conversation-wheel.js:25-27`：`DASH = 3` / `MIN_GAP = 11` / `ACTIVE_LINE = 0.4`）；轨道 = 1px 基线（`styles.css:3402/3411`），刻度条 `styles.css:3421-3437`，提示浮窗 `styles.css:3438-3462`，定位高亮 `styles.css:3463-3469`。
- 空态：`.is-empty { opacity: 0; pointer-events: none }`（`styles.css:3400`），且注释明写**不能改成 `display:none`**（`:3401`，会让轨道高度量成 0）。
- 测试：`conversation-wheel.test.mjs`（`normalizeInputIndex` / `inWindowInput` / `planInputLocate` 等纯函数）。

**修法方向（多方向，可组合）**：

- **D1 常显**：默认态提到可读档（建议 `.8–1`），把"未加载刻度压暗"的既有语义（`:16-18` 第 4 条）留给刻度本身，而不是整条轨道。
- **D2 形态**：轨道从"裸 1px 线 + 3px 短线"升级为可点/可悬停的刻度件（胶囊或短条，`--r-md`、`--surface`、`--border`），命中区现状 34px 宽可保留，但**可见要素**必须同步放大——这是"发现不了"的真正原因。
- **D3 语义（用户口径）**：按「对话分支导航触发器 + 历史节点浮窗热区」重写交互文案与可发现性：常态即可见刻度、悬停即出摘要浮窗、点击即定位（现有 `planInputLocate` 路径不变）。
- **D4 反馈一致**：当前刻度/定位高亮改用与左栏 `.is-active` 同族的信号色 token（参照 `styles.css:3623-3624`），不要第三条色路。
- **D5 解释性 + 语义（已拍板，2026-10-12）**：**保留"只有用户输入成刻度"** —— 用户口径："用户输入是节点驱动的开始"。因此 D3 不再动语义，只做可发现性与"悬停即出摘要浮窗"；摘要浮窗里带轮次号，使"为什么只有这几条刻度"可读。

**验收**：`node --test gui/frontend/dist/conversation-wheel.test.mjs gui/frontend/dist/conversation-view.test.mjs`（既有纯函数与视图口径必须保持全绿）+ 新增 DOM/样式断言（默认不悬停即可见）+ 手工：不悬停对话区也能看到轨道形态。

**风险**：轮轴定位依赖真实几何（`:5-18`）；改视觉不得改几何算法，也不得动 `.is-empty` 的视觉隐藏策略。

**已拍板（2026-10-12）**：保留"仅用户输入成刻度"的语义 —— 用户口径"用户输入是节点驱动的开始"；本条只解决**可发现性**与形态，不改刻度来源。

---

### F-2 工作台表格弹窗：取消底部灰色挡板

**现象（用户原话）**：工作台下表格弹窗底部有一条灰色挡板，"你滚轮横向操作一下就会发现……在长度上和表格是有差距的"。

**证据（已确认）**：

- "分片" = **批次 sheet 页签**：`work-table.js:482-495` `renderSheetTabsHTML` → `<div class="excel-sheets scroll-edges-x" data-work-sheets>`；`:497-503` `sheetTabHTML`（「全部」居首 + 各批次 + 计数）。文件头注释 `:15` 自述「**底部** sheet 页签：批次 = 维度」。
- 插法：`work-table.js:506-518` `ensureSheetsBar`，`:515` 用 `anchor.insertAdjacentHTML("afterend", …)` 把它插到表格下沿。
- 灰色来源：`styles.css:2848-2862` `.excel-sheets { display:flex; flex-wrap:nowrap; gap:3px; padding:7px 10px 9px; border-top:1px solid var(--border); background: var(--surface-2); overflow-x:auto; overflow-y:hidden }` → `--surface-2` 灰底 + 顶部分隔线，宽度跟着卡片内宽。
- 滚动容器不是一个：表格正文 `.work-table-scroll`（`styles.css:2240`，仅纵向 `overflow-y`；弹窗内被 `:2324` 改成 `max-height:none` 的单纵向滚动），而弹窗卡片的 flex/overflow 责任链在 `:2309-2363` 反复重述（注释 `:2327` 明确提过"正文区与表格区两层 overflow"的历史问题）。

**根因（已确认 + 推论）**：底部 sheet 栏的宽度由**卡片内宽**决定，而表格的横向滚动由**表格自身容器**决定；横向滚到底时表格内容已经越过卡片可视宽，而这条灰条固定在内宽里 → 长度不一致。灰条本身是"贴边装饰条"却没有与滚动源同宽，违反 §1 规则 4。

**修法方向**：

- **D1 挡板消失（副产物）**：`取消底部灰色挡板` 由 D2 自动达成 —— 不再有独立的底条，也就没有"贴边条比表格短"的问题。
- **D2 并入表头并保留筛选（已拍板，2026-10-12）**：把批次"分片"从底部条**并入 `.work-table-head`**（`styles.css:2222`，本来就是 `--surface-2` 底 + 下边线的一行），作为表头内的筛选控件（下拉/芯片行 + 计数），与表格同宽同源。用户口径："并入表头，然后支持筛选，因为要做的就是工作表格"。
  - 具体口径：批次 = 筛选维度之一，与既有的会话筛选（`sessionFilter`）、实发开关（`sentOnly`）并列（`work-table.js:303` `scopedRows` 已是三合一的过滤链），**筛选状态与计数必须继续走 `batchesInScope`**（`:334-340`），不得把裁剪逻辑复制一份到头部渲染里。
- **D3 同宽口径**：确认弹窗内**只有一个横向滚动源**；任何贴边的条/影（`scroll-edges-x` 的边缘阴影）都必须挂在该滚动容器上。

**验收**：`node --test gui/frontend/dist/work-table.test.mjs`；手工：打开工作表格弹窗 → 横向滚到底，底部无灰色条带、无长度差。

**风险**：批次页签是筛选状态入口（`state.activeBatch`：`work-table.js:130/260-261/334-340`）。**已拍板保留筛选能力**（并入表头，§7.1 第 2 条），因此实现时不得把筛选功能一起删掉。

---

### F-3 资源管理器：子页页签风格 / 条目紧凑 / 内容详情高亮

**现象（用户原话）**：①"资源管理器的子页分页风格"；②"子页下条目不是向上紧凑的问题"；③"当然还有内容详情高亮的问题"。

**证据（已确认）**：

- 子页页签结构：`index.html:219-241` —— `#code-pane-tabs.code-pane-tabs.scroll-edges-x`（role=tablist）+ 三个平级 `section.code-pane`（`#code-pane-worktree` / `#code-pane-gitlog` / `#code-pane-changes`），每页头部 `.code-pane-head` + `.code-pane-title` + `.badge` + `.code-pane-refresh`，内容区 `.code-pane-body`。
- 页签样式：`styles.css:1468` `.code-pane-tabs`、`:1483` `.code-pane-tab`、`:1494` hover、`:1495-1499` `.is-active`、`:1500` focus 环；容器 `:1461` `.code-panes`（注释 `:1460` 明写"容器自己不滚动——滚动交给 `.code-pane-body`"）。
- 子页面板：`styles.css:1501` `.code-pane`、`:1514` `.is-hidden`、`:1515-1517` head、`:1519-1526` body（自身滚动容器）、`:1527-1532` 刷新按钮。
- 顺序/激活是纯 UI 状态并落盘（`localStorage["seelex.right.explorer.v1"]`），收敛函数在 `explorer-pages.js`：`normalizeExplorerState` / `resolveExplorerState` / `legacyPaneOrder` / `serializeExplorerState`（见 `docs/gui/modules/right-sidebar.md`「子页页签、顺序与刷新」节）。
- 左栏对照值（B0 基线）：`styles.css:2723-2724`、`:3621`、`:3623-3624`、`:3961`、`:4688-4691`。

**待钉（Hypothesis，实现前必须量测）**：

- 条目"不向上紧凑"的具体数值未量测：三页的行类分别落在 `worktree-view.js` / `git-log-view.js` / `workspace-changes.js`（模块文档见 `docs/gui/modules/right-sidebar.md`「工作树 / 提交记录 / 工作区更改」三条）。实现前先读对应的 `*.test.mjs` DOM 断言与 `styles.css` 中行类规则，把"紧凑"落成可验收的 `gap` / `line-height` / `padding` / `min-height` 数值。
- "内容详情高亮"具体指哪一层（树行选中 / chip 当前项 / 详情内选区）需在实现时用截图对齐；本轮确认的两处高亮面是：内容详情 chip 当前项（`index.html:208` `#file-preview-tabs`，chip 自身即头部）与页面 `.code-pane-tab.is-active`（`styles.css:1495`）。

**修法方向**：

- **D1 页签统一到左栏同族**：`.code-pane-tab` 的圆角/内边距/选中态改用 `--r-md` 与既有信号色，去掉与左栏不一致的独立写法（`styles.css:1483-1500` 是唯一改动点）。
- **D2 紧凑**：三页行密度收到 `--row-min-h` 口径（§1 规则 3），一行一条；`.code-pane-body` 保持**单一滚动源**（`:1519`），不要在行容器上再开一层 overflow。
- **D3 高亮的唯一语义**：把"当前选中项"统一为 `.is-active` / `[aria-current]` 一套（B0 规则 2）；树行选中与 chip 当前项必须能区分「树里选中的文件」与「详情里正在看的文件」——若二者本就同义，则合并为同一视觉。
- **D4 冗余**：`legacyPaneOrder` 的旧键迁移（`seelex.right.codePanes`）已声明"迁移后删旧键"，本轮确认实现是否真的只剩一份事实（并入 §4）。

**验收**：`node --test gui/frontend/dist/explorer-pages.test.mjs gui/frontend/dist/explorer-refresh.test.mjs gui/frontend/dist/worktree-view.test.mjs gui/frontend/dist/git-log-view.test.mjs gui/frontend/dist/workspace-changes.test.mjs` + 视觉对照（左栏 vs 子页）。

---

### F-4 上下文压缩帧：条目瘦身 + 栈顶 + 门禁风格

**现象（用户原话）**：条目仍然臃肿；侧边条目显示摘要内容即可；需要表示谁是栈顶；门禁过程和整体前端风格不统一。

**证据（已确认）**：

- 容器：右栏「状态」子页 `#context-compactions`（`index.html:158`）。
- 渲染（`context-summary.js`）：表头 `:38`（`compaction-stack-row is-head`）；正文 `:52`（`div.compaction-stack` role=table，`aria-label` 明写"**栈顶是当前前沿**；压缩失败的行排在栈下方"）；行 `:90`（`is-frontier` / `is-stale` / `is-open`）；三枚单元格 `:91` `is-version`（`renderSeqBadge({seq, unit:"seq", tone:"compaction"})` + **已有** `<em class="compaction-stack-flag">栈顶</em>`）、`:92-94` `is-body`（**原因 + 来源 + 被压区间**）、`:95` `is-actions`（两枚按钮：`帧正文`（`:86`）与 `弹框`（`:87`））；展开时在行下插 `compaction-stack-detail`（`:98`）。
- 失败行另排一条：`:116-129`（失败色 + 「失败」标记 + 原因跨整行 + 动作列写"上下文原样"）。
- 帧正文弹框：`index.html:536-540`（`#compaction-frame-modal` / `-meta` / `-view`）；分页读取与右栏共用同一套标记 `data-compact-frame-load`（`:196`/`:211`）。
- 门禁过程：文案与累计是纯函数 `mergeCompactionProgress` / `compactionGateDurationText`（`compaction-format.js`，登记见 `gui/frontend/README.md:57`）；**条目上方的门禁进度条复用 Plan 面板的轨道**，并配"逐关耗时清单"（`gui/frontend/README.md:58` 明写）。→ **风格不统一的来源就是"借 Plan 的轨道"**。
- 重绘入口：`app.js: repaintCompactions`（`gui/frontend/README.md:446`）。
- 展示口径共用：`compaction-format.js` 是"右栏条目 + 轨迹压缩轨/详情"共用的一份口径（`gui/frontend/README.md:57`）。

**根因（已确认）**：

1. **臃肿** = 一行里塞 seq 徽标 + 原因 + 来源 + 区间 + 估算 + 时间 + 两枚动作按钮（`:90-95`），右栏宽度下必然堆叠换行。
2. **栈顶标识其实已有**（`:91` 的 `.compaction-stack-flag`）；用户的诉求应读作"**瘦身后仍要一眼看出谁是栈顶，且默认视图就要能分辨当前前沿**"。
3. **门禁风格** = 同一段 UI 有第二套语言（借 Plan 轨道）。

**修法方向**：

- **D1 侧边条目 = 一行元数据摘要（已拍板）**：`seq + 栈顶标记 + 原因`（时间/来源/区间/估算进 hover `title`）；两枚动作按钮收敛为一枚"查看帧正文"。用户口径："侧边条目显示摘要内容即可"。
- **D2 栈顶 + 不做展开（已拍板，2026-10-12）**：**不做行内展开、不做置顶**；保留既有的「栈顶」flag（`:91`）作为唯一栈顶标识，非栈顶行压暗（`is-stale` 语义写明），失败行仍排栈下方。
- **D2b 详细内容走弹窗**：点条目 → 打开**已有的帧正文弹框** `#compaction-frame-modal`（`index.html:536-540`，支持 `data-resizable`），在里面展示完整元数据 + 帧正文（沿用 `renderFrameDetail` 的分页读取标记 `data-compact-frame-load`）。**不再在行下插 `compaction-stack-detail`**（`:98` 那条展开路径下线）。
- **D3 门禁**：换成 §1 基线的同族进度语言；或按 `gui/frontend/README.md:58` 的既有判断（整轮只有几十毫秒，进度条不可能"慢慢走"）**以"逐关耗时清单"为唯一展现**，删掉借来的轨道。
- **D4 不新增第二套读法**：所有文案/区间/耗时仍只走 `compaction-format.js`（`README:57` 的既有约束）。

**验收**：`node --test gui/frontend/dist/context-summary.test.mjs gui/frontend/dist/compaction-format.test.mjs gui/frontend/dist/compaction-frame-session-scope.test.mjs` + 同步更新静态原型 `gui/frontend/dist/compaction-preview.html`（`README:58` 称其为"真渲染件 + 真样式"）。

**风险**：`context-summary.js:58-61` 明写"展开哪一行按 **(会话, frame_ref)** 判定，不按数组下标"；排序/瘦身不得破坏该判据（`compaction-frame-session-scope.test.mjs` 就在钉这个）。

---

### F-5 内容详情：IDE 风格高亮 + mermaid 渲染

**现象（用户原话）**：内容详情没有 IDE 风格的代码关键词高亮；md 的 mermaid 在容器下不渲染。

**证据（已确认）**：

- **hljs 已经接线且已加载**：`index.html:553-554` 同步加载 `./vendor/highlightjs/highlight.min.js` 与 `languages.all.min.js`（资产在 `gui/frontend/dist/vendor/highlightjs/`，两文件共约 254KB）；`file-preview.js:19` import `ensureVendorScript`（但 hljs **不走**懒加载，是同步面）；渲染分支 `file-preview.js:950-963`：`window.hljs` → 按语言 `highlight()`，未知语言 `highlightAuto()`，结果类 `hljs`；语言映射 `codeLanguageForPath`（`:53`/`:101` 注释，测试 `file-preview.test.mjs:48`）。
- **缺配色的根因（已确认）**：全仓**只有一条** hljs 规则 —— `styles.css:1651` `.file-preview-code code.hljs { display:block; padding:0; background:transparent; font:inherit }`；**没有任何 `.hljs-keyword` / `.hljs-string` 等 token 配色**。已核查：`gui/frontend/dist/themes/*.css`（glass/graphite/paper/qoder/silver/verdigris）**零命中** `hljs`，`vendor/pico.min.css` 同样零命中。⇒ 高亮 span 生成了，颜色全部继承父色，观感就是"没有高亮"。
- markdown 路径：`markdown.js`（marked + DOMPurify，vendor 见 `dist/vendor/marked|purify`）；markdown 内的 `<pre><code>` 样式在 `styles.css:1668-1680`（`pre` 走 `--surface-2` 底 + `--border` 边 + `--r-sm`）。
- **mermaid：整个 `gui/frontend/dist` 下没有任何 mermaid 资产**（本轮 grep `mermaid` 只命中 README 的代码围栏）⇒ 现在 mermaid 只能渲染成一段文本代码块。**已确认**。
- 渲染分派契约：`file-preview.js:11` 注释（高亮只改写代码块内部）；只读面共用入口 `file-preview.renderReadOnlyContent`（模块文档 `docs/gui/modules/right-sidebar.md:195/215`）。

**修法方向**：

- **D1 补 hljs 主题**：按皮肤 token 化 token 配色（六套皮肤都要交代），或在 `styles.css` 里用一组 `--code-*` 变量统一映射；**必须与 markdown 代码块共用一份**（`file-preview.js:11` 的既有约束）。
- **D2 mermaid 本地 vendor**：新增 `dist/vendor/mermaid/`，**必须走既有懒加载通道** `vendor-loader.js: ensureVendorScript`（用法先例：`file-preview.js:996` PDF.js、`:1084` docx-preview）——`vendor-loader.js` 头部注释写明"重型 vendor 不应在 index.html 尾部同步加载"。
- **D3 渲染分支**：markdown 里语言标记为 `mermaid` 的代码块走 mermaid 渲染，失败/未就绪**降级为现在的代码块**（不得白屏）；图表容器要放进 `.file-preview-content` 的既可横向滚也可纵向滚的容器（`styles.css:1642-1650` 已有 overflow 口径）。
- **D4 安全**：走 DOMPurify 白名单放行 mermaid 产出的 SVG 结构（markdown 走 purify，不能为了 mermaid 破例关掉净化）。
- **D5 体量**：mermaid 数百 KB，懒加载 + 超时/失败可重试由 `ensureVendorScript` 负责（其实现已处理 in-flight 去重、失败不缓存）。

**验收**：`node --test gui/frontend/dist/file-preview.test.mjs gui/frontend/dist/file-preview-render.test.mjs gui/frontend/dist/file-preview-controller.test.mjs gui/frontend/dist/markdown.test.mjs` + 新增断言：① 至少存在一条 `.hljs-keyword`（或等价 token）配色规则；② mermaid 分支存在且"无 mermaid 时降级为代码块"。

**风险**：六套皮肤矩阵；离线 embed（无网络）要求所有资产本地；`file-preview-render.test.mjs:5/83` 的测试替身默认**不装** hljs/marked/DOMPurify（走纯文本降级路径），新增分支不得破坏该降级路径。

---

### F-6 LLM 块 / 工具过程块：按邻接决定顶底圆角（本轮新增）

**期望（用户原话＋图示）**：

```
llm                    llm
-----            ×     \___/
工具过程               /‾‾‾\
                       工具过程
（相邻 → 交界 0 圆角，融合成一块）   （不要：两块各自全圆角 + 中间空隙）
```

**证据（已确认）**：

- 块是**各自独立、固定间距、各自全圆角**：`styles.css:558` `.message, .tool-run { max-width: 860px; margin: 0 auto 22px; }` —— 每个块都吃 22px 下边距，**没有任何相邻判定**。
- 左侧信号条：`styles.css:559-572` `.message::before` / `.plan-card::before`（`.message.user::before` 换色）——绝对定位竖条，融合时其连续性同样要处理。
- 块生成：`components.js:435` `<article class="message ${role}${messageRoleClass(message)}" data-conversation-key=… data-wheel-kind=…>`；角色分区类 `components.js:462-466` `messageRoleClass`（群聊归属加一层 class 后缀）；工具块 `components.js:488` `renderToolCall`；工具轴 `components.js:355` `<details class="conversation-axis is-tools" …>`（`:341` 注释：tool-calling 与思考各自收进可展开/收起的"滚动轴"）。
- 本轮实测（2026-10-12 01:49 运行中的 GUI 截图）：助手块是独立圆角卡片，紧邻其下的"执行中"工具行是另一块，两者间有空隙、四角全圆 —— 与用户要的效果不符。
- `components.js` 内 grep `joined` **零命中** ⇒ 渲染层目前没有"邻接"概念。

**修法方向**：

- **D1 渲染期算邻接**：在组件序列化/对话列表装配处（`components.js` 的消息块与工具块渲染路径）按上一条/下一条的 `kind` 产出中介类：`is-joined-above` / `is-joined-below` / `is-joined-both`；类名同时写 `data-*`（既有测试就在断言 class 与 `data-*`，见 `components.test.mjs`）。
- **D2 CSS**：交界处圆角置 0、块间距归零（或只留 1px 分隔线），**融合组的外侧角保留圆角**；半径仍只取 `--r-*` token（B0 规则 1）。
- **D3 邻接规则（已拍板，2026-10-12）**：用户口径 —— "**没有 llm→llm**；相同元素如果需要分割，那么就是**圆角相对**；如果不分割，就是**两边都没有圆角**"。落成两条判据：
  1. **相同元素默认不分割** ⇒ 交界两侧圆角都置 0（融合成一块，外侧角保留圆角）；
  2. **需要分割时** ⇒ 两枚块各自圆角**相对**（上一块下圆角 + 下一块上圆角，中间保留间距），即用户画的第二种形态。
  3. **分割判据必须显式**：一轮内同一归属的连续块默认不分割；**换轮次/换角色归属**（`round_id`、`data-role-name`，`components.js:435` 都已在元素上）＝ 分割。不同类（`llm ↔ 工具过程`）始终融合，不需要 llm→llm 这一种情形。
- **D4 信号条同源**：融合时左侧竖条要贯通（`styles.css:559-572`），不能在新交界处断成一节一节——若做不到，就明确"融合只作用于底/顶圆角与间距，竖条按块各自保留"并写进注释。

**验收**：`node --test gui/frontend/dist/components.test.mjs gui/frontend/dist/consistency.test.mjs`（新增邻接类断言；后者保证 chip/轨迹行口径不被邻接类改动打破）+ 视觉：llm 紧邻工具过程时交界为直角、融合组外侧为圆角；非相邻时保持现状。

**风险**：`.message` / `.tool-run` 的 22px 边距是多处视觉节奏的来源（滚动锚定、轮轴几何用 `offsetTop` 比例，见 `conversation-wheel.js:5-18`），改间距后需回归 F-1 的定位行为。

---

### F-7 HTML 只读预览（本轮新增）

**用户口径（2026-10-12）**："如果内容是 HTML，那么在只读模式下内容详情需要支持 HTML 预览，点击编辑才是代码。"

**证据（已确认）**：

- **`.html` 现在被当代码**：扩展名在代码表里（`file-preview.js:38`，与 `.xml/.css/.scss/.less/.vue/.svelte` 同族），语言映射 `.html → xml`（`:63`）；只读分派 `renderReadOnlyContent`（`:839-890`）的 case 只有 `markdown / code / text / image / pdf / word / word-legacy / default` → `.html` 落 `code` 分支 → `renderHighlighted`（`:932+`，`textContent` 注入 + hljs 高亮）。
- 编辑面是**独立的另一套**（`:167-231`：`canEditPreview` / `decodeEditableText` / `serializeEditableText` / `editDirty`；`EDITABLE_PREVIEW_KINDS` 明确排除图片/PDF/Word/二进制），且"只有 Ctrl+S 才落盘"（`:317-319`）。⇒ "点编辑才是代码"在架构上是**天然的**：预览面与编辑面本来就是两条路。
- **已有可复用的安全渲染口径**：`html-embed.js`（会话内 HTML 渲染块，就是为"模型产出 HTML"设计的）—— 只进 `<iframe srcdoc>`；`sandbox="allow-scripts"` 且**不给** `allow-same-origin`；srcdoc 内嵌 CSP `default-src 'none'`（只放行内联样式/脚本与 data: 图片）；不给 `allow-forms/popups/top-navigation`；源码同时以转义文本附在块内（`html-embed.js:1-20`）。
- 只读分派是**两处共用**的（`file-preview.js:825-838` 注释：文件详情面板与"提交内文件内容"共用），因此新增分支必须同时覆盖提交视图（历史版本不允许有任何写入口）。

**修法方向**：

- **D1 新增预览类型 `html`**：`previewKindForPath` 把 `.html/.htm` 从 `code` 移出；**只读态**渲染为页面，**编辑态**仍是代码（用户口径）。
- **D2 复用 `html-embed.js` 的沙箱口径**，**不要**走 `renderMarkdown` 那条 `purify.sanitize` + `innerHTML` 的路：它的 `FORBID_TAGS` 里就有 `style`/`iframe`（`file-preview.js:935-938`），会把页面的样式与结构直接抹掉——那不是预览。
- **D3 边界**：不联网（CSP `default-src 'none'`，外链 CSS/JS 不加载）、不执行表单/弹窗/跳转；超大文件与截断按既有口径（`needsWholeFile` / `payload.truncated` → `renderNotice`）。
- **D4 编辑面**：是否把 `html` 纳入 `EDITABLE_PREVIEW_KINDS`（`:175-177`）由"编辑 = 源码"决定 —— 建议纳入（源码可编辑），预览默认只读。
- **D5 相对资源**：`srcdoc` 沙箱里取不到同目录的 css/js，预览是"结构 + 内联样式"级；这条要写进 UI 提示或注释，不追求完整还原。

**验收**：`node --test gui/frontend/dist/file-preview.test.mjs gui/frontend/dist/file-preview-render.test.mjs gui/frontend/dist/file-preview-controller.test.mjs gui/frontend/dist/html-embed.test.mjs` + 新增断言：`.html` 只读态 kind = `html` 且渲染出 iframe；编辑态仍是源码；离线渲染不发外部请求。

**风险**：WebView 内嵌 iframe 的 CSP/`srcdoc` 支持度需真机确认（`html-embed.js` 已验证过同一口径，因此风险主要在文件预览容器的尺寸与滚动）；`file-preview-render.test.mjs` 的测试替身默认不装 marked/purify/hljs（走纯文本降级），新增分支不得破坏降级路径。

---

## 3. 前后端联调条目

> 两条共同的"契约先行"要求：**先定唯一事实，再改代码**。联调改动必须同时落三层证据：Go 侧单测/探针、前端 `node --test`、以及 `docs/gui/schemas` 与模块 README 的同步。

### J-1 Agent Team 退场 + `@` 改为文件选择

**用户口径**：前端的 agent team 与当前 teamwork 差距过大、应淘汰（每阶段按里程碑组临时团队）；agent team 完全没有用了；`@` 在使用语义上更常用于**选择指定文件**而不是召唤团队，"这边整体都要改"。

**现状证据（已确认）**：

- **前端两处团队面并存**：①「状态」页的 **Agent Team** 块 —— `index.html:164-168`（`#team-section` / `#team-view` / `#team-count`，折叠标签写着 `Agent Team`）；②「工作台」页的**团队看板** —— `index.html:176-180`（`#team-board-section` / `#team-board-view` / `#team-board-badge`），数据源 `runtime.teamwork_board`，渲染 `team-board-view.js`。`index.html:176-177` 的注释明确：`team-section/team-view/team-count` 已被「状态」页的 Agent Team 块占用、不得复用。⇒ **淘汰对象是 ①，保留 ②**（teamwork）。
- 前端资产：`agent-team-view.js`（约 96KB）+ `agent-team-view.test.mjs` / `agent-team-refresh.test.mjs`；`app.js:2835-2882` 起的 `agentTeamLibrary` / `agentTeamGlobal` / `agentTeamView` / `agentTeamSessionID` / `agentTeamError` / `agentTeamLoading` / `agentTeamDirty` 与 `AgentTeam*` 调用族。
- 后端资产：`gui/bridge.go:167-192` 是 `AgentTeam*` 接口声明（`AgentTeamView/PutRole/DeleteRole/SetOrder/InstantiateRole`、`Library/SaveTeam/SaveCurrentTeam/DeleteTeam/MaterializeTeam`、`GlobalConfig/SaveEmployee/DeleteEmployee/SetDefaultOrder/PublishToGlobal`、`RolePrompt/OptimizeRolePrompt/RoleSnapshot`），实现同文件 `:990-1152+`；`gui/headless_team.go`（`team.*` RPC）；`application/core/agentteam/`（`factory.go` / `spec.go` / `service` / `runtime` / `role_index` 等 + 大量测试）。
- **`@` 的落点在 Go**：`application/core/input_team.go`（文件头注释：`@` 是"**人**的显式召唤入口"，把团队库里的一支团队装配到当前会话；`@<团队>` 只装配、`@<团队> <附言>` 装配后下发附言；`resolveTeamSummon` 用最长前缀切分；`@` 的建议面在 `completion.go`，"读一次、留进程内快照"）；文档登记 `application/core/README-input.md:46-59`（`submitTeam` / `beginGoalForSummon` / `teamSummonHelp`）。
- 建议面出口：`gui/bridge.go:51` `Suggestions(string) []application.Suggestion`，实现 `:881-882`。
- 文档已把它标为旧面：`docs/gui/modules/right-sidebar.md:93`（域只在 hover 里说）、`:97`（内置形态目录已随 `agentteam/presets.go` 删除，`team_kind` 只是团队名别名）。

**关键判断（已确认）**：**这条不是纯前端**。前端删面板只完成一半；`@` 的语义与装配通道落在 Go 侧（`input_team.go` / `bridge.go` / `application/core/agentteam`）。这与用户"除了 agent team 的变动，其他全部是纯前端改动"的划分一致。

**修法方向**：

- **D1 前端**：`#team-section` 整块与 `agent-team-view.js` 全套下线（含 `app.js` 的 state/刷新钩子与 `AgentTeam*` 调用），相关 `*.test.mjs` 删除或改造；`@` 的补全面改指 **`@文件`** —— 候选来自工作区树/文件索引（而不是团队库），命中后送**附件内容**（详见 **J-1b**）。
- **D2 后端**：`AgentTeam*` Bridge 面与 headless `team.*` 的退场连带处理 `gui/headless_team.go`、`gui/team_live_probe_test.go`、`gui/team_board_wiring_test.go`、`gui/goal_team_wiring_test.go`、`application/core/agentteam/**` 及其测试、`service_assembler.go` 的装配点；`@` 的输入路由（`input_team.go` / `completion.go` 的建议面）改为文件选择语义。
- **D3 契约先行**：先定"`@` 之后的唯一事实"——团队装配是否只剩 goal/teamwork 一条通道（`§7.2` 第 1 条）；随之改 `application.Suggestion` 的候选来源与 notice 文案，并同步 `README-input.md`、`docs/gui/modules/right-sidebar.md`、`CHANGELOG.md`。
- **D4 证据**：`go test ./gui/... ./application/core/...`；退场是破坏性变更，需要在 CHANGELOG 里写清"被删的能力面"。

**风险**：Agent Team 与 goal/teamwork 有历史耦合（`goal_team_wiring_test.go`、`agentteam_floor_wiring_test.go`、`employee_permission_assembly_test.go`、`goal_team_recorder.go` 等都在场）。**必须一次成批退场**，不能留半条通道；且**先拍 `§7.2` 第 1 条的边界再动手**。

---

#### J-1b 文件引用：`@文件` / `+` 加号 —— **只传文件地址，内容由 Seelex 自己读**（本轮新增 + 同日二次口径）

**用户口径**：

- **一轮**：`@` 的语义改成**支持 `@文件`**；输入框**增加一枚加号**（"添加文件内容给到 AI 做识别"）；provider 支持多模态内容的查询；`role` 下需要有专门的 `reader` 的 api 配置（要求按 DeepSeek 的多模态接入文档判定"不变还是需要增加 role"）。
- **二轮（同日，定案）**："文件的阅读功能，**就让 seelex 自己 grep 内容吧**，我们负责**传个文件地址**就行了，只不过**前端需要渲染下**，**`@` 的也要渲染**。"

**定案设计（取代一轮的"多模态搬运 + reader 角色"）**

| 环节 | 定案 |
|---|---|
| 前端 `+` / `@文件` | **只产出工作区相对路径引用**（不是内容、不是字节）；渲染见下一小节 |
| 内容读取 | **Seelex（agent）自己读**：按路径用既有 `read_file` / `grep` 工具取内容（权限走既有只读组规则）——**不需要**把内容搬进请求 |
| 账号 / provider | **不动**：不新增 `reader` 角色、不改 `roles`、不做多模态内容搬运、不加能力声明 |
| 三个入口的关系（**已决**） | **`@` / `+` / 拖拽 = 同一条路径引用语义**：三个入口都只产出**工作区相对路径**，共用**同一套归一化**（`file-drop.js` 的 `normalizeDropPath`）与**同一套渲染**（一枚 chip 组件）。差别只在入口，不在载体 |
| chip 的去向（**已决**） | 路径 chip **也进 agent 可见的正文**（不只做输入回显）：提交后正文里保留**完整路径**（同一枚 chip 两处渲染），于是 agent 能直接看见"用户附了哪个文件"并据此去读它 |
| 多模态既有通道 | `seelebridge/imageattach`（截图随图下发）与 `multimodal`（带图客户端）**保持原样**，不在本条改造范围 |

**前端渲染（用户明确要求：两处都要做）**

1. **composer 内**：`@` 命中文件后渲染成 **token / 芯片**（可删、悬停看完整路径），与普通正文区分开；
2. **消息里**：提交后把路径渲染成**可点开的文件芯片**（复用已有的 `#file-preview-tabs` 多文件详情容器 + `Bridge.WorkspaceFileContent`，`gui/bridge.go:101-108`），点击即打开右栏文件详情（只读预览口径见 **F-7**）。

> **同一枚芯片，两处复用（已决）**：两处是**同一个 chip 组件的两种宿主**（composer 草稿区 / 对话正文），不是两套实现；而且第 2 处是**进 agent 可见正文**的那一处 —— 芯片进正文 = 正文里就是路径文本，chip 只是渲染层的识别，agent 看到的是路径。

**为什么不走"多模态搬运"（记录，避免回退）**

- 本轮实抓 DeepSeek 官方文档（`https://api-docs.deepseek.com/api/create-chat-completion`）：**同一 endpoint/模型确实支持图片**（`content` 可为 content parts：`text` / `image_url`（JPEG/PNG/GIF/WebP，`detail` = low\|high\|original\|auto）/ `file`）；但 **`file` 通道只收图片**（`file_data` 原文写 "of the image"），与 `seelebridge/attachment/README.md` 的真机表（PDF/text 一律 400）**互相印证** ⇒ 这个端点上**不存在文档通道**。
- 而"让 agent 自己 `read_file`/`grep`"这条路**已经存在、零新依赖、且可审计**（工具调用进对话与轨迹，人能复核），因此定为默认路径。
- **⇒ 一轮问的"不变还是需要增加 role" 的最终答案是：不变（不新增 `reader`）。** 将来若要"直接把图 / PDF 交给模型看"，再启用既有 `imageattach`/`multimodal` 通道并另开条目；那时才需要讨论 `reader` 与能力声明。

**A. DeepSeek 官方文档结论（本轮实抓，2026-10-12）**

来源：`https://api-docs.deepseek.com/api/create-chat-completion`（Chat Completions API 页的 OpenAPI 渲染件；导航中另有 `/guides/vision` Vision 指南与 `/guides/files_api`、`/api/create-file` 等 Files API）。

| 事实 | 文档原文要点 | 对本文档的含义 |
|---|---|---|
| 模型 | `deepseek-flash` / `deepseek-v4-pro` | 与 seelex 现有账号配置里的模型名同族 |
| user / tool 消息 `content` | "Either a string, or an array of content parts (**for image input**). See the Vision guide for details." | **同一个 endpoint、同一个模型**就收图 —— 不需要为"发图"另开 provider |
| part：`text` | `{type:"text", text}` | 与 `seelebridge/multimodal.BuildParts` 完全一致 |
| part：`image_url` | `url` = http(s) 地址（≤8192 字符）或 base64 data URL（JPEG/PNG/GIF/WebP）；`detail` = `low`(降到 512×512)\|`high`\|`original`\|`auto` | 与 `types.FilePart`（`DataURL()` / `Detail`）一致；**注意 DeepSeek 多一个 `original`** |
| part：`file` | `file_id`（Files API 上传，形如 `file-api-...`，与 `file_data` 互斥）或 `file_data`（"a base64-encoded data URL **of the image**"）；`filename` 仅在 `file_data` 时有效 | 与 `seelebridge/attachment/README.md` 的**真机结论互相印证**：DeepSeek 的 `file` 通道实际是**图片通道**，不是文档通道 |
| Files API | Upload / List / Retrieve / Delete | 大图与复用图走 `file_id`（`types.FilePart.FileID` 已建模，本端上限 64 MiB） |
| 其他 | `thinking`/`reasoning_effort`（none\|low\|high\|max，minimal→low、medium/xhigh→high） | 与 `dto/reasoning_effort.go` 的既有映射一致，本条不涉及 |

**A 表口径校正（原判定已作废，保留以便溯源）**：一轮曾据此推出"需要新增 `reader` 角色"（理由是"文档通道不存在、只能先读成文本或图"）。二轮定案把"读"从**模型侧**挪到了**工具侧** —— 由 agent 自己用 `read_file`/`grep` 读，因此**不新增 `reader`、不加能力声明、不搬内容**。A 表仍然有用：它记录了这个端点到底收什么；将来若真要走"把图 / PDF 直接交给模型"，不必重新查一遍。

**B. Seelex 侧现状（本轮实读；多处已经存在，不要重复造）**

| 能力 | 现状 | 证据 |
|---|---|---|
| 引擎侧多模态 | **已支持**：`types.Message` 带 `Files []FilePart`；纯文本消息的 wire JSON **逐字节不变**，只有带附件时才展开 content parts（`image_url` / `file`），且能反解 Anthropic 的 `source` | Seele v0.3.4 `types/content.go`（`FilePart`/`FileKind`/`MarshalJSON`/`parseWirePart`），模块缓存 `!seele@v0.3.4` |
| 随请求送图 | **已有链路**：会话级待随图队列（截图 → `Add`；下一次请求 `Take` 即清空，**至多送一次**），挂在引擎与 provider 之间的 `Completer` 缝上（不改引擎历史语义） | `seelebridge/imageattach/README.md`（含架构图/时序图）、`seelebridge/runtime.go:351`（`images: imageattach.NewRegistry()`）、`runtime_image.go` |
| 独立带图请求 | **已有**：最小带图同步客户端（`Config{BaseURL,APIKey,Model,MaxTokens,Timeout,MaxImageBytes}` + `Answer.FinalText()` 兜住思考模式把内容放 `reasoning_content` 的情形）；wire 上限 `DefaultMaxImageBytes = 5 MiB` | `seelebridge/multimodal/README.md`、`client.go:20-70` |
| 附件分流/降级 | **已有**：`PlanFor` —— 图片原样走附件通道；文档能当文本读则内联（单文档上限 256 KiB），不能读则产诊断 `Notes`（**绝不静默丢弃**） | `seelebridge/attachment/README.md`、`inline.go` |
| 账号角色 | **只有三类**：`roles.agent` / `roles.subagent` / `roles.goalplan`；解析在 `seelebridge/internal/config`（`simplifiedConfig.Roles`），角色常量在 `model.Role*` 与 `dto.AccountRole` | `seelebridge/internal/config/config.go:66-96`、`application/contract/dto/plan.go:15-21` |
| 契约先例 | 工具注册在 `main.go` 的 `register*` 族；"工具 → 人过目"的既有先例是 `ask_approve` | `main.go:1114-1121`、`application/approval/broker.go` |
| 前端"给 AI 文件" | **只有路径文本**：从右栏工作树拖行 → 落成一条**相对路径**的排队消息（不是附件内容） | `gui/frontend/dist/file-drop.js:1-20`（`FILE_DRAG_MIME`/`normalizeDropPath`/`dropPaths`）、`app.js:2/894/3672` |
| 输入面 | **文本签名**：`Bridge.Submit(text)` / `SubmitToSession(sessionID, text)` —— 没有附件参数 | `gui/bridge.go:669` / `:722` |
| 输入框工具条 | 已有两枚图标按钮位（命令面板、运行状态）→ 加号落这里 | `index.html:83-96`（`form#composer` / `.composer-tools` / `#command-button` / `#runtime-button`） |

> **顺带发现的文档漂移（既有问题，本次一并更正）**：`seelebridge/multimodal/README.md` 写着"Seele 的 `types.Message.Content` 是 `*string`（v0.3.1 实测），带图请求无法经引擎下发……等 Seele 支持 content parts 后由引擎接管，本包只保留编码与冒烟用途"。v0.3.4 的 `types/content.go` **已经支持 content parts**，这条口径已过时；不更正会继续误导"多模态只能绕开引擎"。

**C. 改动清单（收敛后；**Go 侧无改动**）**

1. **前端输入面**（`gui/frontend/dist`）：
   - **`+` 按钮**：落在 `.composer-tools`（`index.html:87`，与 `#command-button`/`#runtime-button` 同排），打开工作区文件选择器；选中后成为一枚 chip（可删、悬停看完整路径）。
   - **`@文件`**：候选源从"团队库"改成**工作区文件**（唯一出口是 `Bridge.Suggestions(input)`，`gui/bridge.go:881-882`）；命中后 **token 化**（可删），不再是普通文本。
   - **三入口共用同一套归一化（已决）**：`@` / `+` / 拖拽都走 `file-drop.js` 的 `normalizeDropPath`（必须工作区内相对路径；拒绝对路径、`..`、超长）。**不要另写一套**，否则会出现两套"什么算合法路径"的口径。
   - **两处渲染**：composer 内 token + 消息内的文件芯片（复用 `#file-preview-tabs` 与 `Bridge.WorkspaceFileContent`；点击展开右栏详情，只读预览按 **F-7**）。
2. **提交面**：`Bridge.Submit(text)`（`gui/bridge.go:669`）**签名不变** —— 引用就是正文的一部分（与拖拽现状一致，改动面最小）；本条要的是**解析与渲染**，不是新增传参通道。（**"芯片进正文"这条已决正是靠它成立**：正文里就是路径文本，chip 只是渲染层识别。）
3. **契约与文档**：`application.Suggestion` 的候选来源（团队库 → 工作区文件）与 notice 文案；同步 `application/core/README-input.md`（`@` 语义）、`docs/gui/modules/shell-and-interactions.md`（composer 的 `+`）、`seelebridge/multimodal/README.md`（更正上文那条漂移）。
4. **明确不做**：不改 `config/accounts.yaml`、不加 `reader`、不碰 provider 多模态、不搬内容字节、不动 `types.FilePart` 链路。
5. **提示词口径（别漏）**：既然只传地址，就必须让 agent **知道"附了文件要先读"** —— 这条要落进输入装配的提示词（`internal/promptassets`）与 `application/core/README-input.md`，否则会出现"给了路径却没去读"的空转。

**D. 验收**

- 前端：`node --test gui/frontend/dist/file-drop.test.mjs gui/frontend/dist/composer-*.test.mjs`（新增：`@` 命中 → token 化；提交后路径 → 芯片；非法路径丢弃；`+` 选择器与 chip 增删）。
- 手工：`@` 选一个文件 → 发送 → 消息里出现可点开的文件芯片 → 点击打开右栏详情（只读 HTML 预览按 F-7）。
- Go 侧：**无新增测试**（本条无 Go 改动）；既有 `go test ./gui/... ./seelebridge/...` 保持全绿。

**E. 拍板记录（本轮已决）**：① `@` / `+` / 拖拽**统一成同一条路径引用语义**（同一归一化 + 同一 chip 组件）；② 路径 chip **也进 agent 可见的正文**（不是只做输入回显）。⇒ 提示词口径按"正文里有路径 = 用户已附加该文件、应当去读"写（见 C 第 5 条）。

**E2. 落点与证据（2026-10-12 实施，Go 侧仍无改动）**

| 环节 | 落点 | 说明 |
|---|---|---|
| 引用载体（三入口共用） | `gui/frontend/dist/file-reference.js` | `normalizeFilePath` **就是** `file-drop.js` 的 `normalizeDropPath`（同一函数引用，测试断言 identity）；`fileReferenceTokens` 识别 `@路径` 与拖拽落的裸路径两形态（保守判定：`@someone`、「and/or」不认）；`applyFileReference` / `removeFileReference` 是草稿上的唯一插入/删除实现 |
| `@文件` 候选 | `app.js` 的 `updateInlineSuggestions` 的 `@` 分支 + `renderFileReferenceSuggestions` | 候选来自**已加载的工作区树索引**（`ensureFileRefIndex` 按工作区 root 缓存，首次用一次 `WorkspaceTree("", 4)`，之后按键只筛内存，**不逐键读盘**）；**不调 `Bridge.Suggestions`**（后端表里没有 `@`） |
| `+` 加号 | `index.html` 的 `.composer-tools` → `#file-button` + `#composer-file-picker`；`app.js` 的 `openFilePicker`/`renderFilePickerResults` | 与 `@` **同一段插入代码**（同一个 `fileReferenceCandidates` + `acceptFileReference`） |
| 拖拽 | 未改（`file-drop.js` 的 `fileDropPayload`/`dropPaths`/`dropPlan` + `app.js` 的 `initFileDrop`） | 本来就只产出工作区相对路径：J-1b 把它接进同一枚 chip 的识别范围（`fileQueueText` 那种「读取工作树文件：- src/a.js」正文里也长出 chip） |
| chip 两处渲染 | `fileReferenceChipHTML`（`host: composer\|message`）；草稿条 `#composer-file-refs`；正文走 `markdown.js` 的 `fileRefs` 通道（stash 机制，只有 user 消息开启） | 正文 chip 点击开右栏文件详情（只读预览按 F-7）；目录引用只提示，不打开面板；**不强制切换当前详情**（§7.2 第 2 条的保守默认） |
| 前缀口径（二选一：**前端输入增强**） | `application/core/completion.go` 前缀契约注释（`@` 仍不是前缀，三条理由写在注释里） | `composer-input.js` 的 `SIGIL_PREFIXES` 同步去掉 `@`：引用是正文文本，提交走 `SubmitToSession` + 显式视图会话 ID（留在表里会走 ambient `Submit`，切会话的窗口里会投错会话） |
| 提示词口径 | `internal/promptassets/assets/system/instructions.md`（新增 "User-Attached Files Arrive As Workspace Paths"）+ `application/core/README-input.md` | 「附了文件就先自己 `read_file`/`grep` 读，并引用读到的路径」；不读=缺一步 |
| 文档同步 | `docs/gui/modules/shell-and-interactions.md`（前缀表 `@` 行 + `+`/chip 两条）、`gui/frontend/README.md`（`file-reference.js` 一节 + 三入口口径） | |

**验收（本轮实测）**：`node --test "gui/frontend/dist/*.test.mjs"` **755 pass / 0 fail**（新增
`file-reference.test.mjs` 11 例、`file-reference-wiring.test.mjs` 6 例结构性断言、`file-drop.test.mjs`
三入口同源 1 例、`composer-input.test.mjs` 引用不是 sigil 1 例）；Go 侧无改动（`go build ./...` 与
`go test ./gui/... ./application/core/... ./internal/promptassets/...` 全绿）。悬停/点击等手工观感需在
GUI 里目视确认（桌面纪律：本轮不操作窗口）。

---

### J-2 Agent 驱动 GUI 打开待审阅文件（本轮更正：前后端联调；一次批处理，上限 10 份）

**用户口径（本轮补充）**：Agent 应能控制 AgentGUI 操作 —— 在最后列举用户想看的文件或需要给用户看的文件时，**在侧边资源管理器弹出文件内容**，并支持多个文件内容的弹出（详情只用显示一个，其他的像文件详情多开一样在内容详情导航栏显示）；**这条需要前后端联调**（更正原"纯前端"归类）。

**现状证据（已确认）**：

- 前端**已有**"多文件详情容器"：`index.html:208` `#file-preview-tabs`（`role=tablist`，`aria-label="已打开的文件详情"`；注释 `:205-207` 写明"一枚 chip = 一个已打开的文件详情，动作按钮贴右"）+ `index.html:212` `#file-preview-view` + `:209-211` 三枚动作按钮（收起详情 / 隐藏工作树与提交记录 / 关闭全部）。
- 标签生命周期是纯函数：`previewTabLabel` / `normalizePreviewTab` / `openPreviewTab` / `closePreviewTab`，渲染 `renderPreviewTabsHTML`；抽屉是"多文件详情容器"，**切换只切显隐、不重读、不丢滚动位置**（`docs/gui/modules/right-sidebar.md`「文件预览（多文件详情容器）」节）。
- 宿主接线：`app.js:497-529`（`view` / `tabsHost: elements["file-preview-tabs"]`、close/hide/collapse 监听、`--preview-w` 宽度拖拽）。
- 读文件数据面：`Bridge.WorkspaceFileContent(relPath, limit) (dto.FileContent, error)`（`gui/bridge.go:101-104` 声明；实现见同文件 Workspace 转发族 `:1319-1345+`）；后端 workspace 域统一做 containment / 敏感过滤 / 上限 / 二进制探测（模块文档同一节）。提交记录里的只读面走 `WorkspaceGitCommitFileContent`（`:108`）。
- **触发面现状 = 只有人**：点工作树行 / 提交行才打开（同一节）；**Go 侧没有任何 agent 入口** —— 本轮实跑的 grep 证据：`gui/` 下 `reveal` 只命中前端 README 的文案（无 Go 命中）、`summon` 在 `gui/` 下零命中；拟用工具名 `show_files`（`seelebridge/` 下零命中）、`review_files`（`application/` 下零命中）**均未被占用**。
- **可复用的联调骨架**：工具注册在 `main.go`（`registerProductTools` `:1114-1121`、`registerTaskTerminalTools` `:960-963`、`registerGoalTools` `:597`、`registerContextReadTools` `:664`）；工具实现落在 `seelebridge/`（`runtime_tools.go`、`runtime_computer.go`、`task_terminal.go` 等）；事件面 `seelebridge/events.go` / `events_unified.go`；**"工具 → GUI 弹面"的既有先例 = `ask_approve`**（`main.go:1121` `registerAskApprove`，broker 在 `application/approval/broker.go`）；GUI 事件出口 `gui/run_wails.go:58-61`（`bridge.Start(ctx, emit)` → Wails `runtime.EventsEmit`）；前端增量入口 `protocol.js:38` `applyEvent`（按 `INCREMENTAL_KINDS` 决定是否需要刷新）+ `app.js:674` `render(snapshot, options)`。

**修法方向（契约先行）**：

- **D1 后端**：
  - 新工具（命名待定，建议 `show_files` / `review_files`）在 `main.go` 的 `register*` 族注册，实现落 `seelebridge/`（新文件或 `runtime_tools.go`）。参数建议 `files: [{path, note?, start_line?, end_line?}]`；语义 = "请 GUI 打开这些文件供人审阅"。**一次调用 = 一次批处理召唤**（不是一份文件一次调用），**单次上限 10 份**（用户口径，2026-10-12）；超限要显式报错并回契约，不静默截断。**返回值必须回契约**（已请求 N 个 + 哪些因沙盒/敏感/二进制被跳过），因为工具结果会进入对话并被后续回合读到。
  - **落点必须是 session 作用域的可重放请求**，而不是一次性事件：GUI 可能未开/被重连，快照是真相源。建议做成 `application/core/view_state/coordinator.go` 的投影或 Snapshot 的只读段，前端起来后能补放；dto 侧新增字段（`application/contract/dto`）并与 `docs/gui/schemas` 同步。
  - **失效语义**对齐既有口径：工作区切换时抽屉随树清空、会话切换清帧态（`docs/gui/modules/right-sidebar.md`）。
  - **权限**：属"agent 控制 agentGUI"，按 `docs/arch/agent-permission-subjects.md` 的权责档定级（只读浏览建议不高于会话读写档；`§7.2` 第 5 条）。
- **D2 前端**：
  - 新增"apply review request"路径：收到请求 → 展开内容详情（`.is-preview-open`，必要时解除 `.is-preview-collapsed`）→ 逐个 `openPreviewTab`（**只有第一个成为当前显示的详情**，其余只出现 chip）→（可选）滚动/高亮树中对应行。
  - 复用既有语义：同路径文件只激活、不重复读盘；抽屉为空自动收起（`onEmpty`）。
  - **不许撑爆 chip 条**：批处理上限 10 份（后端硬门），前端 chip 条仍要有"还有 M 个"的可展开入口（建议可见 5 个 + 溢出计数），把 chip 条宽度当作有限资源。
  - **不许抢视线**：请求到达时若用户正在别的子页/别的文件上，是否强制切换需口径（`§7.2` 第 5 条）。
- **D3 证据**：Go 侧 `go test ./gui/...`（新增 bridge/headless 断言：工具调用 → 快照字段/事件）；前端 `node --test` 新增（apply 请求 → chip 数量与激活项）；契约更新 `docs/gui/schemas`；端到端用 `SEELEX_HEADLESS_PORT` 的 headless 控制面驱动（`gui/run_wails.go:24-27`）。

**风险**：这是"agent 主动改 GUI 视线"的能力，必须限定在**人可撤销、可忽略**的范围（弹的是详情面板，不是模态框、不是置顶窗口）；沙盒边界仍由后端 workspace 域统一裁决，**不得因新增入口绕过 containment / 敏感过滤 / 上限**。

---

## 4. 冗余设计清理（用户第 1 轮："还有一些冗余的设计"）

| 冗余 | 证据 | 处置方向 |
|---|---|---|
| `.excel-sheet` 同一组件被定义/覆盖 4 处 | `styles.css:2864-2867`（基础）、`:5213-5217`（动效段覆盖）、`:5253`（reduced-motion 复位）、`:5499-5501`（末段再覆盖）；`.excel-sheets` 亦然（`:2848-2862`） | 合并为一段（基础 + 动效 + reduced-motion 三段各自职责，不靠"更靠后的覆盖"） |
| 前端两套团队面板 | `index.html:164-168`（Agent Team）vs `:176-180`（团队看板） | 随 J-1 退场 Agent Team 块 |
| 资源管理器旧 localStorage 键 | `legacyPaneOrder` 迁移 `seelex.right.codePanes` → `seelex.right.explorer.v1`（`explorer-pages.js`；`right-sidebar.md` 声明迁移后删旧键） | 确认迁移后只留一份事实，删旧键读写 |
| 圆角/半径硬编码 | 例：`styles.css:2864` 一带历史硬编码已被收进 token（注释自述），但仍有多处 `border-radius: 8px`（如 `:4089`）/`999px` | 按 B0 规则 1 收敛到 token，**只删不减语义** |

**验收**：`node --test gui/frontend/dist/*.test.mjs` 全绿；样式体积不因清理而增大；清理必须是"删重复源"，不是"再写一条覆盖"。

---

## 5. 验收与测试口径（全条目共用）

- **前端**：`node --test gui/frontend/dist/*.test.mjs`（dist 即源；新测试与既有测试同目录、同命名习惯 `<module>.test.mjs`）。其中三道**既有风格闸**必须保持全绿并优先扩展（见 §1）：`text-button-chrome.test.mjs`（解析 CSS 的组件皮护栏）、`theme.test.mjs`（深浅 × 皮肤两轴 token 契约）、`consistency.test.mjs`（对话 chip ↔ 轨迹行跨视图一致）。
- **后端/联调**：`go test ./gui/... ./application/core/...`；联调改动必须带 bridge 单测或 headless 探针。
- **实机**：`make rebuild-gui`（**该目标包含 `clean-gui`**：按 `AGENTS.md` 第 0 节，执行前必须先读根目录 `MEMORY.md` 并用中文向用户预警将删除什么、影响哪些用户数据、是否可恢复）。
- **文档同步**（不写文档不算完成）：`gui/frontend/README.md`（每个 `dist/*.js` 一行职责说明的既有格式）、`docs/gui/modules/right-sidebar.md`（资源管理器 / 文件详情）、`docs/gui/modules/shell-and-interactions.md`（消息块与工具轴的邻接圆角）、`docs/gui/schemas/` 与 `docs/gui/module_dotting.json`（联调项）、`CHANGELOG.md`。
- **每个 Work Item 的完成条件**：既有测试全绿 + 新增断言 + 文档同步 + 一张前后对照截图（证据先于结论）。

---

## 6. teamwork 派活建议（审核通过后执行）

**里程碑 M1 —— 前端基线与纯前端七条（可并行，注意单一写入面）**

- `WI-B0`（先行，其余条目的依赖）：落 §1 基线（token 与组件写法）并在 `styles.css` 顶部写清规则注释。
- `WI-F1` 对话轴、`WI-F2` 工作表格底部挡板（并入表头 + 筛选）、`WI-F3` 资源管理器子页、`WI-F4` 压缩帧条目（瘦身 + 栈顶 + 弹窗 + 门禁风格）、`WI-F5` 高亮 + mermaid、`WI-F6` 邻接圆角、`WI-F7` HTML 只读预览。
- **冲突面警示**：七条都写 `gui/frontend/dist/styles.css`（28 万字节单文件）。建议按**段落硬切**（各自组件所在段落）+ 归属同一个 worker，或串行合并；`app.js`（27 万字节）同样是热点，F-1/F-4/F-6/F-7 会碰到它。

**里程碑 M2 —— 前后端联调三条（依赖 M1）**

- `WI-J2`（Agent 驱动打开文件，一次批处理 ≤10 份，契约先行）。
- `WI-J1a`（**Agent Team 退场** + `@` 候选源改造：删面、影响测试最多）。
- `WI-J1b`（**文件引用**：`+` / `@文件` / 拖拽**三入口统一为路径引用** + 同一 chip 两处渲染（含**进正文**）；**不新增角色、不搬内容**）—— 与 J-1a 共享 `@` 与 `Suggestions` 的改动面，建议**紧接 J-1a 之后**，并由同一个 worker 承接，避免两套 `@` 语义并存。
- **冲突面**：三者都在 `gui/bridge.go`（62KB）与 `main.go`（92KB）上有写入面 → **串行**或按文件硬切。
- 建议顺序：先 J-2（新增能力，纯增面），再 J-1a（删面），最后 J-1b（扩面，含账号配置与契约）。J-1a 之前先定 `@<团队>` 去留（`§7.2`）。

---

## 7. 口径决议记录（2026-10-12 已拍板）+ 剩余未决

### 7.1 已拍板（上一轮六个阻塞项，全部已落进各自条目）

| # | 问题 | 决议（用户口径原文要点） | 落点 |
|---|---|---|---|
| 1 | F-1 对话轴语义 | **保留"只有用户输入成刻度"** —— "用户输入是节点驱动的开始" | F-1「已拍板」段 + D5 |
| 2 | F-2 批次"分片" | **并入表头，然后支持筛选** —— "因为要做的就是工作表格"；底部灰挡板随之消失 | F-2 D1/D2 |
| 3 | F-4 压缩帧条目 | **不做展开**；所有帧**只在侧边留元数据摘要条目**，点开用**弹窗**展示详细内容 | F-4 D1/D2/D2b |
| 4 | F-6 邻接 | **没有 llm→llm**；**相同元素需要分割 → 圆角相对；不分割 → 两边都没有圆角** | F-6 D3 |
| 5 | J-1 `@` 与文件引用 | `@` 改成 **`@文件`**；输入框**增加加号**添加文件；**同日二次口径**：文件的阅读"**就让 seelex 自己 grep 内容，我们负责传个文件地址就行**"，前端（含 `@`）负责渲染 ⇒ **不新增 `reader` 角色、不做多模态内容搬运**；**三次口径（本轮拍板）**：`@` / `+` / 拖拽**统一成同一条路径引用语义**，且路径 chip **也进 agent 可见正文** | J-1b |
| 6 | J-2 批量 | **一次使用 = 一次批处理召唤**，**最多一次召唤 10 个文档** | J-2 D1/D2 |
| — | F-7（本轮新增） | HTML 内容**只读模式要 HTML 预览**，**点编辑才是代码** | F-7 |

### 7.2 剩余未决（本轮新产生；**都不阻塞 M1 的前端七条先动工**）

1. **J-1a（2026-10-12 已拍板：删除）**：`@<团队>` 手动召唤**直接删除**（不保留"隐藏入口"），`@` 这个字符让给**文件指代**；团队装配只剩 goal/teamwork 一条通道。据此 J-1a 的删除面 = 前端 Agent Team 面板与 `agent-team-view.js` + `@` 召唤路由与建议面 + `Bridge.AgentTeam*`（装配 / 角色 CRUD / 团队库 / 员工库 / 母本 / 员工提示词）+ headless `team.*` + `agentteam` 的工厂与注册表 + TUI 的 Alt+T 面板段；**保留**被 goal 治理与 teamwork 共用的角色会话基座（`agentteam.RoleSessionID`、role draft / sync / floor、`dto.RoleSnapshot`、`sessionstore/team_registry.go`）与团队看板（teamwork）。`AgentTeamRoleSnapshot` 保留原名（看板「员工会话」入口在用）。验收与证据见 CHANGELOG `[Unreleased] / Removed`。
2. **J-2**：chip 上限已定 10，但"到达时是否允许**自动切换当前详情**到第一个文件"仍需定（涉及抢视线）；以及请求来源的**权限档**如何映射（只读浏览是否等于免问）。
3. **F-7**：只读 HTML 预览是否允许**内联脚本**（`html-embed.js` 允许 `allow-scripts` 但不给 same-origin），还是更严一档（纯静态无脚本）？

---

## 附：本文档的证据索引（全部为本轮实际读到的锚点）

- 视觉基线：`gui/frontend/dist/styles.css:2723-2724, 3621, 3623-3624, 3961, 4532-4536, 4688-4691`
- F-1：`gui/frontend/dist/conversation-wheel.js:1-31`、`conversation-view.js:32`、`index.html:61`、`app.js:127`、`styles.css:3388-3469`、`conversation-wheel.test.mjs`
- F-2：`gui/frontend/dist/work-table.js:15, 482-518`、`styles.css:2220-2222, 2240, 2309-2363, 2848-2868`
- F-3：`gui/frontend/dist/index.html:219-241`、`styles.css:1460-1532`、`explorer-pages.js`、`docs/gui/modules/right-sidebar.md`（子页与树/提交/更改三条）
- F-4：`gui/frontend/dist/index.html:158, 536-540`、`context-summary.js:38-129, 196, 211`、`compaction-format.js`、`gui/frontend/README.md:57-58, 446`
- F-5：`gui/frontend/dist/index.html:553-554`、`file-preview.js:11, 19, 53, 101, 950-963, 996, 1084`、`markdown.js`、`styles.css:1651-1685`、`vendor/highlightjs/`、`vendor-loader.js`
- F-6：`gui/frontend/dist/components.js:341, 355, 435, 462-466, 488`、`styles.css:558-572`、`components.test.mjs`
- J-1：`gui/frontend/dist/index.html:164-180`、`app.js:2835-2882`、`agent-team-view.js`、`gui/bridge.go:51, 167-192, 990-1152, 881-882`、`gui/headless_team.go`、`application/core/input_team.go`、`application/core/README-input.md:46-59`、`docs/gui/modules/right-sidebar.md:93, 97`
- J-2：`gui/frontend/dist/index.html:205-213`、`app.js:497-529, 674`、`protocol.js:38`、`gui/bridge.go:101-108, 1319-1345+`、`main.go:597, 664, 960-963, 1114-1121`、`gui/run_wails.go:19-23, 60`、`seelebridge/{runtime_tools.go, events.go, events_unified.go}`、`application/approval/broker.go`、`application/core/view_state/coordinator.go`、`docs/gui/modules/right-sidebar.md`（文件预览节）
- F-7：`gui/frontend/dist/file-preview.js:38, 63, 167-231, 317-319, 825-890, 932-963`、`html-embed.js:1-20`、`file-preview-render.test.mjs:5, 83`、`html-embed.test.mjs`
- J-1b（DeepSeek 侧）：`https://api-docs.deepseek.com/api/create-chat-completion`（本轮实抓：模型 `deepseek-flash`/`deepseek-v4-pro`；user/tool `content` 支持 content parts "for image input"；part 类型 `text`/`image_url`（JPEG/PNG/GIF/WebP，`detail`=low\|high\|original\|auto）/`file`（`file_id` 或 `file_data`，文档原文写 "of the image"）；`/guides/vision`、`/guides/files_api`、`/api/create-file`）
- J-1b（动手面）：`gui/frontend/dist/file-drop.js:1-20`（`normalizeDropPath`/`dropPaths`）、`index.html:83-96`（composer 工具条）、`gui/bridge.go:669`（`Submit(text)`）、`gui/bridge.go:881-882`（`Suggestions`）、`gui/bridge.go:101-108`（`WorkspaceFileContent`）、`app.js:2/894/3672`（拖拽接线）
- J-1b（既有通道，本条**不**改）：`seelebridge/imageattach/README.md` + `seelebridge/runtime.go:351`；`seelebridge/multimodal/README.md` + `client.go:20-70`；`seelebridge/attachment/README.md` + `inline.go`；`seelebridge/internal/config/config.go:66-96`（`roles` 只有 agent/subagent/goalplan）；`application/contract/dto/plan.go:15-21`（`AccountRole`）；Seele v0.3.4 `types/content.go`（模块缓存 `!seele@v0.3.4`，content parts 已支持）
