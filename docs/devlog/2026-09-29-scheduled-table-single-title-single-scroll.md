# 定时任务表格弹窗：标题只留一条、纵向只留一个滚动容器

- 日期：2026-09-29
- 起因（用户报告，直接在运行中的 GUI 上指出）：「你自己打开看看前端」→「新添定时任务的前端」
  →「然后你看看定时任务表格的内容」→「依旧嵌套」→「依旧多重标题」
- 结论：两条都在**运行中的 dev GUI 上复现并修掉**，并且用 headless 场景对 HEAD 与修复后各量了一遍
  （先红后绿，见 §4）。同一处的第三个毛病（新建弹窗的开关行）一并修掉（§5）。
- 未修（另立，见 §7）：命令白名单在这一版 dev 包里为空、定时任务只活在进程内。

---

## 1. 现场：先把前端打开看

`computer_*` 真机取证（不是读代码猜）：

| 步骤 | 观察 |
|---|---|
| 右栏「工作台」→ 定时任务 → `+` | 弹出**新建定时任务**；`命令` 下拉只有一项「（无可用白名单命令）」；`创建后立即启用` 的勾选框在第 1 行、文案掉到第 2 行 |
| 右栏「工作台」→ 定时任务 → 「0 项任务 点击查看完整表格」 | 弹出**定时任务表格**；内容 `暂无定时任务`（本机 0 条任务）；弹窗头两行标题：`定时任务` + `定时任务表格` |

两条抱怨对得上这两处，而且都不是新毛病——**同一个家族的修法早就有了，只是没落到这个弹窗上**：

- 「多重标题」：`e2dd0f0 fix(gui): 工作表格弹窗只留一个标题（弹窗头不再重复）`。当时的用户口径是
  「工作表格里面三个工作表格的标题是不是有点多了」「保留内部黄色的部分即可」——弹窗头删掉
  eyebrow + h2，名字交给表格自己的头带（`.work-table-head` 那条 `--surface-2` 浅色带）。
- 「嵌套」：同一族的多重滚轮。`TestEmbeddedWorkTableSingleScrollContainer` 的注释写得很直白：
  「根因是三层嵌套——`.modal-card[data-resizable]` 自带 `overflow:auto`、正文容器
  `.work-table-modal-view` 又写了一份 `overflow:auto`、表格区再自限高自滚」，修法是纵向只留一个
  滚动容器。

定时任务表格弹窗（`422d4be` 引入）两条都没跟上。

---

## 2. 改法

| 处 | 修前 | 修后 |
|---|---|---|
| `index.html` | `aria-labelledby="scheduled-table-modal-title"`；弹窗头 `span.eyebrow`「定时任务」+ `h2`「定时任务表格」 | `aria-label="定时任务表格"`；弹窗头只剩 `.scheduled-table-modal-head` 里那枚关闭按钮 |
| `styles.css` | `.scheduled-table-card` / `.scheduled-table-view { overflow: auto }`（与 `.modal-card[data-resizable]{overflow:auto}` 叠成两层滚动容器） | `.modal-card.scheduled-table-card[data-resizable] { display:flex; flex-direction:column; overflow:hidden }`，`#scheduled-table-view` 只让高度、不滚，唯一滚动容器是 `.sched-table-scroll` |
| `scheduled-tasks-view.js` | 表体直接返回 `<table>`（空态返回一句 `暂无定时任务`，整块没有名字） | 返回 `.sched-table`（头带 `.sched-table-band`：`▾ 定时任务 N 项`，坐在 `--surface-2` 上、在滚动容器之外）+ `.sched-table-scroll`（表体或空态） |
| `app.js` | `SCROLL_SHADOW_TARGETS` 里只有 `.work-table-scroll` | 补 `.sched-table-scroll`——单滚动容器才有边缘阴影手感 |

要点：**名字从「弹窗头的两行」搬到「表内的一条」**，不是简单删掉。空态也随之有了名字（修前 0 条任务
时弹窗里连一个名字都没有，这是「多重标题」修一半会掉进去的坑）。

---

## 3. 钉子

- `gui/bridge_test.go`：
  - `TestEmbeddedScheduledTableTitleOnce`——切出弹窗头那一段，反向禁止 `class="eyebrow"` /
    `scheduled-table-modal-title` / `<h2`，要求 `aria-label="定时任务表格"`，并要求
    `scheduled-tasks-view.js` 里 `<strong>定时任务</strong>` 必须留着（弹窗头删掉后就靠它命名）。
  - `TestEmbeddedScheduledTableSingleScrollContainer`——钉住「卡片与正文容器不滚、只有
    `.sched-table-scroll` 滚」这条链，并禁止 `.scheduled-table-view { overflow: auto;` 回来。
  - `TestEmbeddedScheduledToggleInline`——钉住 `.settings-field.sched-toggle`（(0,2,0)），并禁止
    裸 `.sched-toggle { display: flex;` 回来。
- `gui/frontend/dist/scheduled-tasks-view.test.mjs`：新增「头带只有一条 + 只有一个滚动容器 + 头带在
  滚动容器之外」；空态断言从「有 `暂无定时任务`」加严到「头带仍在（`定时任务` / `0 项`）」。

---

## 4. 先红后绿：用真实渲染量尺寸

场景：`_tmp/sched-shape/`（`serve.mjs` + `scheduled.html`/`report.html`），红场景由
`make-red.mjs` 用 `git show HEAD:<file>` 取出**修前的** `styles.css`、`scheduled-tasks-view.js`、
`index.html` 的弹窗标记拼出来；绿场景直接用工作区当前文件。两者跑同一份 `report.js`，量完把 JSON
写进 `<pre id="report">`，再用 `msedge --headless=new --dump-dom` 读回来（`1400x900` 与 `760x420`
两档窗口）。

| 判据（3 条任务时） | 修前（HEAD） | 修后 |
|---|---|---|
| 弹窗内自带 `overflow:auto/scroll` 的层 | `modal-card scheduled-table-card` + `scheduled-table-view`（**2 层**） | 只有 `sched-table-scroll`（**1 层**） |
| 小窗 `760x420` 下真正溢出中的层 | 上面**两层都在溢出**（滚轮接力） | 只有 `sched-table-scroll` 溢 |
| 可见标题候选 | `span.eyebrow=定时任务`、`h2=定时任务表格`（**2 条**） | `strong=定时任务`（**1 条**） |
| 空态标题 | （无） | `定时任务`（1 条） |
| 头带是否在滚动容器之外 | 无头带 | `true` |
| 新建弹窗开关行 | `display: grid`；勾选框 left 460、文案 left 460（同一行左缘 = 另起一行） | `display: flex; flex-direction: row`；文案 left 542 > 勾选框 right 528（同一行） |

视觉留档：`_tmp/sched-shape/shot-both.png`（上=3 条任务、下=空态；headless 截图），弹窗头只有一枚
✕，名字只有头带那条「定时任务 3 项」。

---

## 5. 顺带修掉的第三处：开关与文案分两行

现场（真机截图）：新建弹窗里勾选框一行、`创建后立即启用` 另一行。根因是权重 + 顺序双双失手：
标记里两者同属 `.settings-field`（`display: grid`，子项各占一行），而 `.settings-field` 在
`styles.css` 里更靠后、权重又不低于 `.sched-toggle`，于是 `.sched-toggle` 的 `display: flex` 被盖掉。
修法把自己抬到 (0,2,0)：`.settings-field.sched-toggle`。钉子见 §3。

---

## 6. 门禁

- `go test ./gui/ -count=1` → `ok`（含三条新钉子）。
- `node --test gui/frontend/dist/scheduled-tasks-view.test.mjs` → 11 全绿。
- headless 量尺寸：红/绿两份报告（§4）。视觉截图一张。
- 只动 `gui/frontend/dist` 与 `gui/bridge_test.go`，不触 Go 运行时；`go build ./...` 未受影响。

---

## 7. 未修：两条另立的现场

这两个都不在本批口径内（一个要产品决定，一个是本机 dev 包的装配事实），如实记在这里：

1. **命令白名单在这一版 dev 包里为空** → 新建弹窗的 `命令` 下拉只有「（无可用白名单命令）」，
   命令类任务根本无法发布。根因链：`registerScheduledTaskCapability`（`main.go`）先
   `resolveAutoGetJobsDir()`（按 **CWD** 与 **exe 目录**找 `local/tools/auto_get_jobs/main.py`），
   再 `resolvePythonCommand()`（`exec.LookPath("python"/"py")`）；dev 包在 `dist/seelex-gui-dev/`，
   该目录下没有 `local/`，两处候选都落空 → 白名单没登记 → `runtime.scheduled_commands` 为空。
   仓库根目录下脚本与 `.venv` 都在（`local/tools/auto_get_jobs/{main.py,.venv/Scripts/python.exe}`），
   所以这是**dev 包的装配缺口**，不是脚本缺失。
2. **定时任务是纯进程内状态**：`seelebridge/scheduler/scheduler.go` 的 `State` 只有
   `tasks map[string]*task` / `commands map[string]ScheduledCommand`，没有任何落盘——进程重启即清空。
   这与既有注释「清单/定时任务属于进程」一致，先记为事实，不擅自改成持久化。
