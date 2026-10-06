# teamwork 里程碑甘特图 —— 真·甘特图静态稿（设计说明）

- 设计稿：`docs/design/teamwork-gantt/index.html`（**单文件自包含**：1× `<style>` + 1× 普通 `<script>`，
  0 外部 css/js/字体/图片；双击 `file://` 直接能看，不用起服务）
- 自证工具：`docs/design/teamwork-gantt/tools/measure_gantt.py`；原始读数与图：`docs/design/teamwork-gantt/evidence/`
- 旧稿保留做前后对比：`docs/design/teamwork-dag/`（上一版的「满宽卡片列表 + 左侧连线」）

调试 query（只为自证，不改默认视图）：
`?view=wide|narrow`（一页一块板，出证图用）、`?demo=story|sketch|mixed`、`?advance=N`、
`?theme=dark`、`?scroll=N`（滚动后再写探针，读数与截图同态）、`?hit=1`（额外把「这个像素归属哪个元素」写进探针）。

---

## 1. 本轮为什么重做：上一版哪里不像甘特图

上一版 `docs/design/teamwork-dag/index.html` 的实际形状是**「按依赖排序的满宽卡片列表 + 左侧 gutter 连线」**：
每行是一张 72px 高的卡片（id / name / 状态 chip / 依赖 chip / session / worktree / 目标全铺在行里），
连线只在行左侧 22–34px 的 gutter 里折。它**没有 x 轴、没有条形、没有错峰的起点**，所以读起来不是甘特图。

本轮把它画成甘特图：**左侧任务名列 + 右侧绘图区（刻度尺 + 竖向网格线 + 每行一根横条 + 正交折线箭头 + 里程碑汇总条）**。

## 2. 数据 → 几何映射（leader 冻结，逐条照抄实现）

| 量 | 公式 | 实现位置 |
|---|---|---|
| `dur(i)` | 恒 `1`（计划里没有工时事实，**条长不编**） | `--d` 恒 1；`width: calc(var(--d) * slotW - 2px)` |
| `start(i)` | `0`（无依赖）；否则 `max(start(d) + dur(d))` | `startOf()` 递归 + `seen` 集合兜底成环 |
| `end(i)` | `start(i) + dur(i)` | `data-end` |
| 条的 x | `plotLeft + start(i) * slotW` | `.wi-bar{left:calc(var(--s) * var(--team-dag-slot-w))}` |
| 条的宽 | `dur(i) * slotW`（相邻槽留 2px 视觉缝） | `width:calc(var(--d) * var(--team-dag-slot-w) - 2px)` |
| 里程碑汇总条 | `start = min(start of items)`、`end = max(end of items)` | `.sum-bar{--s/--e}` |
| 空里程碑 | **零宽菱形**（不画空条），落在它的屏障前驱汇总条右端 | `.sum-diamond`，`data-empty` 控制显隐 |
| 里程碑间箭头 | 被依赖汇总条**右端** → 依赖方汇总条**左端** | `drawEdges()` 的 `sumAnchor()` |
| 工作项边 | 前驱条**右端** → 后继条**左端**（标准 finish→start） | `drawEdges()` 的 `barAnchor()` |

几何常量（宽栏基座）：`--team-dag-slot-w:64px`、`--team-dag-label-w:208px`、`--team-dag-bar-h:18px`、
`--team-dag-row-h:38px`（**38 远小于上一版的 72**）、`--team-dag-ruler-h:30px`、`--team-dag-scroll-max-h:380px`。
窄栏（宿主 ≤520px，容器查询）压到 `slot-w:40px / label-w:118px / bar-h:16px / row-h:30px`，**纯 CSS，不另做一套裁剪数据的口径**。

## 3. 甘特图标准语法：六项逐条落实

1. **左侧任务名列**：`id · name`（窄栏收窄到 118px，次级列隐藏）。
2. **绘图区顶部刻度尺 + 竖向网格线**：刻度是**依赖槽位**，不是时间；`ruler-basis` 明写「横轴 = 依赖槽位（非时间）· 1 槽 = 1 层依赖 · 条长恒 1 槽」。
3. **每行一根横条**：有左端 x 与长度。
4. **依赖 = 正交折线 + 箭头**：finish→start；多条边按 `k` 分层避让；同槽并行（gap < 16px）时向下绕一个 hook（避免退化的零长线）。
5. **汇总条**：横跨名下全部工作项条的总跨度，两端**向下短折**（`--ms-tone` 粗条 + `.sum-cap-l/-r`）。
6. **里程碑之间的依赖**：同一套语法，画在**汇总条之间**。
7. **本轮明确不做**：当日线 / 关键路径高亮 / 条内百分比进度 —— 计划里没有时间与工时事实，**不假装有**。

## 4. 信息不许丢（verify 可逐项到 DOM 里找）

- 每行 DOM 里都有：`name / role / status / depends_on / session_id / worktree / goal / description / note`
  + 三个标记 `blocked / interrupted / live`。
  - `label 列`：`id`、`name`（可点，`data-team-item-open`）、三个 flag chip（`data-flag` + `data-on`）；
  - `条内`：`name`（截断）；`条左端上方`：`depends_on` 小字（含「→ 槽 n」）；`条后`：role chip（色点 + `data-team-role-open`）+ 状态 chip + session chip（`data-team-session`）；
  - 其余全文（session_id / worktree / goal / description / note 与三个 flag 的布尔值）**既进 `title` 也进 `.wi-full` 文本**（视觉隐藏但**在 DOM 里**，`clip-path:inset(50%)`）。
- 里程碑框头仍读得到：`id · name · L<n> · status · 屏障 deps · 🔒/🔓 · n items · content`。
- 闸门带保留，文案三支（按上一轮已修口径）：`无屏障 → <下一块> 未声明 depends_on` / `闸门已放行 → <deps> 全部 done` / `闸门 → 上一层全部 done 才放行（等 <deps>）`。
- 未解锁里程碑：框虚线 + `opacity:.55` + 🔒。

## 5. 颜色：口径变更（**这条是 leader 拍的，用户可否决**）

### 5.1 变更内容

| 通道 | 上一版（DAG 卡片） | 本轮（甘特条） |
|---|---|---|
| **状态** | 卡片**描边** = 状态色 | **条描边 = 状态色**（硬口径不变，仍只用既有令牌） |
| **归属（teammate）** | 行左侧 **5px 色带** + role chip 色点 | **条填充 = teammate 淡色（14%）+ 条左端 4px 实色 cap** + role chip 色点 |

| 状态 | 描边令牌 | 浅色 / 深色 | 条内呈现 |
|---|---|---|---|
| `running` 正在做 | `--status-running` | `#b26a00` / `#e0a45c` | 实线描边 |
| `done` 完成 | `--status-done` | `#1f9c63` / `#4cc38a` | 实线描边 |
| `review` 待评审 | `--status-info` | `#2e6be6` / `#6ba6ff` | 实线描边 |
| `failed` / `killed` / `interrupted===true` | `--status-failed` | `#d64545` / `#f0716a` | 实线描边 |
| `pending` 未开始 | `--status-idle` | `#9aa0a8` / `#8c8c93` | **虚线描边** |

teammate 色板沿用 `--team-dag-role-0..5`（slot0 artist `#6f5bd6` / slot1 frontend `#a4538f` / slot2 verify `#0f8f9e`…），
按 role **首次出现次序** append-only 登记，重渲染/推进/切主题**不变色**（本稿自证：`roleIndex={artist:0,frontend:1,verify:2}`）。

### 5.2 两通道正交（面=归属，线=状态）

| 语义 | 落在哪个几何面 | 色源 |
|---|---|---|
| 谁在做 | 条的**填充**（14% 淡色） | `--team-dag-role-*` |
| 谁在做 | 条**左端 4px 实色 cap** | `--team-dag-role-*` |
| 谁在做 | 条后 role chip 的 **8px 色点** | `--team-dag-role-*` |
| 什么状态 | 条的**描边**（1px，pending 虚线） | `--status-*` |
| 什么状态 | 条后 status chip 的文字/边框 | `--status-*` / `--border-*` |
| 里程碑身份 | 汇总条 + 框描边 | `--team-dag-ms-0..4`（低饱和，不抢前两通道信号） |
| 依赖关系 | 折线 + 箭头 | `--team-dag-edge`（**不是**状态色，见下） |

理由：条形几何下上一版的「左侧 5px 色带」被压到 18px 高的条里读不出来；而「谁在做」在甘特图里天然落在**条本身**。

### 5.3 依赖连线的颜色：为什么不借用状态色

上一轮用户说「线框颜色 = 当前状态」——本轮这条语义**由条描边承担**（用户原话里的硬口径「条描边 = 状态色」）。
连线如果再借一次状态色，同一条信息出现两遍，而且会和「里程碑汇总/状态」抢信号；所以连线固定用 `--team-dag-edge`
（浅色 `#3f93d0` / 深色 `#6fb6e0`）。**这条也可以被用户否决**：若要求「线也按源条状态上色」，改一处 `drawEdges()` 即可。

---

## 6. 自证：把「看起来像甘特」变成「量出来是甘特」

**方法**（`tools/measure_gantt.py`，全部走无窗口路径：`--dump-dom` + `--screenshot` + PIL；**不碰 `computer_*`**）：

1. 页内自证块 `#gantt-probe` 写下每个条 / 汇总条 / 边的几何（`getBoundingClientRect` 读数）；
2. PNG 里**自己量出**绘图区原点 `plot_l` 与槽宽 `slotW`：在刻度尺带里找 `--border-strong` 实色竖线（刻度），
   只保留能连成等差链的那几条 → `plot_l=235 / slotW=64`（宽栏）、`plot_l=145 / slotW=40`（窄栏），
   与页面读数**完全一致**（8 张图全部一致）；
3. **逐行扫条的实色 x 区间**：在条的左右竖边所在 y 带里按列统计命中「该行状态色原型（实色 + 未解锁 ×.55）」的像素数，
   取命中 ≥3 的列 → `x0..x1`；只保留落在 `plot_l + k*slotW` 网格上的候选（借此排除 chip 文字、网格线、连线、
   以及压在条顶边上的其他元素）；换算槽位 `round((x0-plot_l)/slotW)`；
4. 与**本脚本独立重算**的 `start()` 公式（只读 DOM 里的 `depends_on`）逐条对照；汇总条另量「跨度 = min..max」。

### 6.1 逐行条形 x 区间 → 槽位 核对表

| 图 | item | role | 状态(eff) | 量到 x0..x1(px) | 换算槽位 | 期望槽位 | 条宽 px | 判 |
|---|---|---|---|---|---|---|---|---|
| wide | `wi-frame` | frontend | done | 235..296 | **0** | 0 | 62 | OK |
| wide | `wi-design` | artist | running | 235..296 | **0** | 0 | 62 | OK |
| wide | `wi-impl` | frontend | pending | 299..360 | **1** | 1 | 62 | OK |
| wide | `wi-tests` | frontend | pending | 363..424 | **2** | 2 | 62 | OK |
| narrow | `wi-frame` | frontend | done | 145..182 | **0** | 0 | 38 | OK |
| narrow | `wi-design` | artist | running | 145..182 | **0** | 0 | 38 | OK |
| narrow | `wi-impl` | frontend | pending | 185..222 | **1** | 1 | 38 | OK |
| narrow | `wi-tests` | frontend | pending | 225..262 | **2** | 2 | 38 | OK |
| advance1 | `wi-design` | artist | done | 235..296 | **0** | 0 | 62 | OK |
| advance1 | `wi-impl` | frontend | running | 299..360 | **1** | 1 | 62 | OK |
| advance1 | `wi-tests` | frontend | pending | 363..424 | **2** | 2 | 62 | OK |
| wide-bottom | `wi-render` | verify | pending | 363..424 | **2** | 2 | 62 | OK |
| wide-bottom | `wi-accept` | verify | pending | 427..488 | **3** | 3 | 62 | OK |
| sketch | `c` | artist | done | 235..296 | **0** | 0 | 62 | OK |
| sketch | `b` | artist | done | 235..296 | **0** | 0 | 62 | OK |
| sketch | `d` | artist | pending | 299..360 | **1** | 1 | 62 | OK |
| sketch | `a` | artist | pending | 363..424 | **2** | 2 | 62 | OK |
| mixed | `wi-mx-1` | artist | done | 235..296 | **0** | 0 | 62 | OK |
| mixed | `wi-mx-2` | frontend | review | 299..360 | **1** | 1 | 62 | OK |
| mixed | `wi-mx-3` | verify | failed(interrupted) | 299..360 | **1** | 1 | 62 | OK |
| mixed | `wi-mx-4` | frontend | running | 363..424 | **2** | 2 | 62 | OK |
| dark | `wi-frame` | frontend | done | 235..296 | **0** | 0 | 62 | OK |
| dark | `wi-design` | artist | running | 235..296 | **0** | 0 | 62 | OK |
| dark | `wi-impl` | frontend | pending | 299..360 | **1** | 1 | 62 | OK |
| dark | `wi-tests` | frontend | pending | 363..424 | **2** | 2 | 62 | OK |

> `wide`（scrollTop=0）只能看到前 4 行（面板 380px 视口 + m-verify 被裁），`wi-render / wi-accept` 是在
> `?scroll=94` 那张滚到底的图里量的 —— 每行**至少被某一图量到**，覆盖性见 §6.4。
> 窄栏那张里被 sticky 刻度尺/框头遮住的行一律标 `skipped`，不当作通过，也不算失败。

### 6.2 汇总条（从 PNG 量跨度）

| 图 | 里程碑 | 量到 x | 期望 x | 左端差 | 右端差 | 公式 min..max |
|---|---|---|---|---|---|---|
| wide | m-design | 235..297 | 235..298 | 0 | −1 | 0..1 |
| wide | m-impl | 299..426 | 299..426 | 0 | 0 | 1..3 |
| narrow | m-impl | 185..264 | 185..264 | 0 | 0 | 1..3 |
| wide-bottom | m-verify | 363..490 | 363..490 | 0 | 0 | 2..4 |
| sketch | m-sketch | 235..425 | 235..426 | 0 | −1 | 0..3 |
| mixed | m-mixed | 235..425 | 235..426 | 0 | −1 | 0..3 |

（−1 是「右端最后一个像素 vs 右边界坐标」的差 1，常驻；`m-impl` 的左端 0 差是因为箭头恰好落在汇总条左端，量到的是箭头右侧第一列。）

### 6.3 其他读数（每张图都跑）

| 检查 | 结果 |
|---|---|
| 幂等（同输入重渲染 DOM 串逐字相同 hashA=hashB） | PASS ×8 |
| 滚动保持（重渲染后 scrollTop 不跳） | PASS ×8 |
| 刻度尺 sticky（滚动时吸附滚动容器顶边，delta=0） | PASS ×8 |
| 框头 sticky（不越过刻度尺下沿；内容够长时正好吸住） | PASS ×8 |
| 面板 `max-height = 380`（内容超出 → 内部滚动；单屏夹具不适用） | PASS ×8 |
| PNG 刻度竖线逐条落在 `plot_l + i*slotW` 上 | PASS ×8 |
| 空里程碑画零宽菱形 / 非空不画（`data-empty` 与 `data-sumEmpty` 一致 + 对应 CSS 在） | PASS ×8 |
| 行序 = 拓扑序（夹具**倒序声明**，`topo_violations=[]`） | PASS ×8 |
| 边锚点 = 前驱条右端中点 → 后继条左端中点 | 5/5 PASS ×8 |
| DOM 信息不丢（`goal/description/note/session_id/worktree/depends_on` + 三个 flag + 框头 7 字段 + 三个入口钩子） | PASS ×8 |
| 里程碑间箭头数 = Σ `milestones[].depends_on`（本夹具 2 条） | PASS |
| 闸门文案三支（未放行 / 已放行；`deps` 为空那支在本夹具没有样本，见 §7） | 2/3 有样本 |

### 6.4 覆盖性（每个工作项至少被量到一次）

```
wi-frame/wi-design/wi-impl/wi-tests/wi-render/wi-accept  ← 真实故事 6 个，全部量到并命中
b/c/d/a                                                   ← 用户 sketch 4 个，全部量到并命中（b0 / c0 / d1 / a2）
wi-mx-1..4                                                ← 推进中/异常变体 4 个，全部量到并命中
```

### 6.5 夹具

- **(a) 真实故事（3 teammate）**：`m-design`(无依赖) → `m-impl` depends `m-design` → `m-verify` depends `m-impl`；
  6 个工作项，**数组倒序声明**（证明行序/槽位只由 `depends_on` 决定）。
  期望：`wi-design` 0 · `wi-frame` 0（同槽并行）· `wi-impl` 1 · `wi-tests` 2 · `wi-render` 2 · `wi-accept` 3 —— 与量测一致。
- **(b) 用户 sketch**（`?demo=sketch`）：`a/b/c/d`，`c→d`、`b→a`、`d→a` → 量到 **b 0 / c 0 / d 1 / a 2**，与用户手绘形状一致。
- **(c) 推进中/异常（`?demo=mixed`）**：绿 done / 蓝 review / 红 failed+interrupted / 黄 running **同屏**，
  外加一个**空里程碑**（`m-empty`，画零宽菱形）。

### 6.6 三张（+2）PNG 与命令

```
gantt-wide.png          ?view=wide                 宽栏 702px 宿主 · 真实故事
gantt-narrow.png        ?view=narrow               窄栏 342px 宿主（容器查询生效）
gantt-advance1.png      ?view=wide&advance=1       推进一轮（wn-design done / wi-impl running / 闸门放行）
gantt-wide-bottom.png   ?view=wide&scroll=94       滚到底（看 m-verify 的两行）
gantt-sketch.png        ?view=wide&demo=sketch     用户 sketch
gantt-mixed.png         ?view=wide&demo=mixed      done/review/failed/running + 空里程碑
gantt-dark.png          ?view=wide&theme=dark      深色基座
```

一条命令重出全部证据：`python docs/design/teamwork-gantt/tools/measure_gantt.py`（`problems: 0` 才算过）。

### 6.7 量测口径里必须交代的一件事（不是设计缺陷，是 Chrome 的）

`--dump-dom` 与 `--screenshot` 两种模式给页面的**可用宽度/高度不同**（dump 侧页面多一条 18px 滚动条，
legend 因此多折一行），页面 chrome 高度会整体差 16–40px。脚本把该差值量出来（`calib.dy_px`，8 张图分别是
−24 / 0 / −40 / −24 / 0 / −24 / −24 / −24）并在取窗口时用掉；**x 方向的量测完全不依赖它**（`plot_l/slotW` 从 PNG 自己量）。

---

## 7. 我做不到的 / 已知偏差（如实列出）

1. **跨越里程碑的连线会被 sticky 框头遮断**：连线层在框之下（`z-index:1` < 框的 2），滚动时框头（`z-index:5`）
   会把跨框的那一段盖住。与上一版行为一致，**未修**（修法：把连线层抬到框头之上，但那样线会压住框里的字）。
2. **`--d` 恒 1 时看不出「错峰并行」以外的东西**：`dur` 恒 1 是 leader 冻结的（计划里没有工时事实），
   所以本轮没有「一根条跨多槽」的样本；如果将来给工作项加了工时字段，`start/end` 公式要跟着扩。
3. **闸门文案第三支（`deps` 为空）没有样本**：本夹具三个里程碑都有 `depends_on`，所以「无屏障 → …未声明 depends_on」
   这条只在代码里、没有被量到；要证它得再补一个「无依赖的中间里程碑」夹具。
4. **`--team-dag-ms-*` / `--team-dag-role-*` 是本稿自造的 hex**（既有令牌里没有这两组），
   但**只作用在 `.team-board` / `.gantt` 作用域内**，状态色仍逐字引用既有令牌 —— 这条要 leader 确认（见 §8）。
5. **布局稳定化用了 2 秒逐帧重画连线**（`requestAnimationFrame`，150 帧内每帧 `drawEdges()`，几个检查点重写探针）：
   因为**字体替换会让整页位移**，而在 `load/fonts.ready/resize` 之外没有更好的时机事件（`ResizeObserver` 只看尺寸、
   看不到「位置整体位移」）。产品实现里应换成组件生命周期里的 `ResizeObserver`/`document.fonts.ready` 一次性重算。
   这条是本轮**踩到的真问题**（不重画会让边锚点与条错位 16px）。
6. **窄栏里框头会折两行**（宿主 342px），框头被 sticky 顶到框尾时会有 14px 压到刻度尺下面（被刻度尺盖住）。
   可读性没坏，但不算漂亮；若要修，窄栏要把 `ms-deps/ms-count` 也收起来。

## 8. 最需要 leader 拍板的 3 点

1. **颜色口径变更**（§5.1：状态从「行描边」搬到「条描边」，teammate 从「左侧色带」搬到「条填充 + 左端 cap」）
   是否照此进实现？用户可否决。
2. **连线颜色不借状态色**（§5.3）：保持中性 `--team-dag-edge`，还是要「线按源条状态上色」？
3. **`--team-dag-ms-*` / `--team-dag-role-*` 这两组自造 hex 的归属**：本轮继续自造（限定作用域），
   还是趁这次把 teammate 色板与里程碑淡色**登记进 `gui/frontend/dist/styles.css` 令牌表**（那样实现侧就不用再抄一份）。

## 9. 落点建议（给实现侧 / 下一轮的 frontend）

- 落点沿用上一轮的裁决：**替换 `gui/frontend/dist/team-board-view.js` 里「里程碑 · 工作项」一节的渲染**，
  head / teammate 队列 / 审计三节保留；`TEAM_BOARD_CSS` 仍是**唯一 CSS 来源**（不许往 `styles.css` 抄第二份）。
- 可直接复用的名字（与本稿逐字一致）：`--team-dag-slot-w / -label-w / -bar-h / -row-h / -ruler-h / -scroll-max-h`、
  `--team-dag-role-0..5`、`--team-dag-ms-0..4`、`--team-dag-edge`；DOM 钩子 `data-team-item-open / -role-open / -session`。
- 几何函数照抄即可：`dur/start/end`、`sumBar()`、`ganttLayout()`（本稿内联在 `index.html` 的 `<script>` 里，可整段搬）。
- **别再把框头写成 2 列 grid**：本轮自证时抓到——`.ms-head` 若共用「label 列 | 绘图区」的 grid，
  8 个字段会被自动排成 4 行、撑出 28px 框头，压到汇总条与第一行上（量测里表现为「条顶边被一条全宽灰线盖住」）。
  本稿改成 `display:flex;flex-wrap:wrap`。
