# 行高自适应：字撑行、框随行长、边按实测几何（wi-gantt-fit）

用户口径（原话）：「样式是对了，但是高度呢？字体需要做的是**撑高这个 workitem**，而不是**里程碑的
大小强压着不放**」。

这一份说的是**几何的事实来源**从此分成两条：

| 轴 | 事实来源 | 为什么 |
| --- | --- | --- |
| **x**（槽位轴） | CSS `calc(var(--team-dag-label-w) + 槽位 × var(--team-dag-slot-w))`，渲染件只写算式 | 槽位是依赖算出来的整数，窄栏容器查询把几何压小时，条 / 汇总条 / 边一起缩，不用重量 |
| **y**（行高轴） | **实测**：插进 DOM 之后量 `.team-dag-row` / `.team-dag-ms-sum` 相对 `.team-dag-content` 的中心 | 行高由**内容**撑（名字折几行就多高），根本不是常量 —— 「y = 前面那些块的高度之和」这条算式不再成立 |

## 1. 改了什么

`gui/frontend/dist/team-board-view.js`：

* **行**：`.team-dag-row { height: auto; min-height: var(--team-dag-row-h) }`、`.team-dag-card`
  同样 `height: auto` 且**不裁**（`overflow: visible`）。`--team-dag-row-h` 从"钉死的行高"
  降为**保底值**（38px；窄栏 30px）。
* **名列的可读性责任在左侧**：`.team-dag-label-top` 改成竖排 —— **id 一行**（mono），
  **name 另起一行**并 `white-space: normal; overflow-wrap: anywhere`，**不裁、不省略号、不设
  max-height**。名字折几行，行就多高。
* **框**：`.team-dag-frame` 没有 `height`（本来也没有）—— 但真正的病根在渲染件：原先
  `ganttLayout()` 用 `--team-dag-row-h: frame.rows.length` 把"框高 = 行数 × 常量"写进了边的 y
  算式。那套（`ganttLayout` / `yCSS` / `Y_VARS` / 每行的 `i + 0.5`）**整条删除**。
* **条内那串字**（视觉回声）：窄栏（`@container (max-width:520px)`）直接 `display: none`
  —— 名字在左侧名列里已经读得完，条里再塞就是"半截字"（改前实测 25px 盒子装 214px 文字）。
* **边**：渲染时只写 x（槽位算式）和**纵向锚点**，`top/height` 是 **0px 占位**，整层
  `.team-dag-edges` 在量出来之前 `visibility: hidden`（宁可先不画，也不画一条停在 0px 的假线）。
  `layoutTeamGanttEdges(root)` 量一次：`row:<id>` = 那一行的实测中心，`sum:<ms>` = 汇总条那一行
  的实测中心，然后按每段线自己的锚点 + 微调（线宽居中 −0.75、箭头上沿 −3.5、绕行钩搁板 +12）
  把 `top`（竖段连 `height`）写成 px。
* **什么时候量**：`renderTeamBoard` 渲染完**排一次**（微任务 + 一帧各一次，同一帧连渲只排一次），
  `ResizeObserver` 盯 `.team-dag-content` 的高度（换字号 / 折行数变了 / 字体加载完再量一次）。
  **没有 DOM 时（node:test）是空操作**；`renderTeamBoard` 签名没动、`app.js` 一行没改。
* **几何数字只剩一份**：CSS 里的 `--team-dag-*` 是唯一来源，渲染件不再复写
  （`GANTT` 里只剩视觉微调：箭头长 7、绕行钩 7/12、半格 0.75/3.5）。

`gui/frontend/dist/team-board-preview.html`：探针页在读数前显式跑一次
`layoutTeamGanttEdges(host)`（同一个函数，不是第二套口径），并把逐行高 / `data-laid-out` /
段线 `top` 一起读出来。

## 2. 自证读数（自己写的工具，真浏览器）

工具：`tools/fit-probe.mjs`（无窗口：起静态源 → headless Chrome `--dump-dom` / `--screenshot` →
把 `#probe` 的 JSON 掰回来）。"改前"那一版是**从 git 引用原样取出来**的
（`--before-ref`，含它的 import 链），同一份夹具、同一档宽度各量一遍。
看图：`tools/fit-analyze.py`（PIL，数像素）。

```
node docs/design/teamwork-gantt/tools/fit-probe.mjs \
  --out docs/design/teamwork-gantt/evidence --before-ref HEAD
python docs/design/teamwork-gantt/tools/fit-analyze.py \
  docs/design/teamwork-gantt/evidence/fit-narrow-370.png \
  docs/design/teamwork-gantt/evidence/fit-pixels.json
```

产物：`evidence/fit-readings.json` / `fit-readings.txt`（两档 × 改前改后）、
`evidence/fit-narrow-370.png`（改后 370px 实图）、`fit-narrow-370-before.png`（改前同框）、
`fit-pixels*.json|txt`。

夹具：三个里程碑屏障串行（m-design → m-impl → m-verify），5 件工作项，名字长短都有
（最长的一句是「出图：teamwork 里程碑/工作项 DAG 静态 UI 稿」）。

### 2.1 窄栏 370px（产品右栏真实宽度）

| 项 | 改前（HEAD） | 改后 |
| --- | --- | --- |
| 逐行行高 | 一律 **30**（= `--team-dag-row-h`，被钉死） | **40 / 74 / 57 / 57 / 40** —— 正好跟着名字折的 1/3/2/2/1 行走（每多一行 +17） |
| 名字（名列） | `51 / 96…257` **被截** | `100 / 100` **读得完**（折 1~3 行） |
| 条内回声 | `25 / 80…214`（半截） | `display: none`（不画） |
| 里程碑框高 | `112 = 头26 + 汇总22 + 2×30 + 4`（**行数 × 常量**） | `166 = 头26 + 汇总22 + Σ行114 + 4`（Δ0）；m-verify `92 = 26+22+40+4` |
| 依赖边 | 每段 `top` 是 `calc(8.25px + var(--team-dag-ruler-h) + … + 0.5 × var(--team-dag-row-h))`（算式，常量） | 每段 `top` 是**实测 px**；与「从 DOM 反读的锚点中心」最大偏差 **0.01px**，超 0.5px 的段 **0** |
| 产品自己那次实测 | `data-laid-out=null`（没有这一步） | `data-laid-out=true`、`visibility=visible`、首段 `top: 104.05px`（**在我显式再跑之前**读到的） |
| 幂等 | — | 连跑两次段线 style 逐字相同 `true` |
| 滚动 | 380/412 | 370/530（上限仍是定值 380px） |

### 2.2 宽栏 1180px

* 行高：改前一律 **38**；改后 **58 / 75 / 58 / 58 / 58**（长名折 2 行 → 75）。
* 名字：改前最长那句 `141 / 257` **被截**；改后 `190 / 190` 读得完（折 2 行）。
* 条内回声：宽栏保留（`49 / 84…225`，**占它自己的可用宽 96%**——不是被挤成 0~几 px 的伪文字）。
* 框高：`184 vs 185`、`167 vs 168`（**Δ−1**：`clientHeight` 是取整后的数，Σ行高会差 1px）、
  m-verify `110 = 110`（Δ0）。
* 边：最大偏差 **0.01px**、超 0.5px **0**、幂等 `true`。

### 2.3 看图像素（改后 370px PNG）

* 状态色真的落在画面上：done(绿) 698 px（y 126–333）、running(黄) 111 px（y 325–552）。
  review / failed 是 0 —— **这份夹具里没有这两个状态**（不是没画）。
* 依赖边（`--team-dag-edge`，严格容差）：144 px，**x ∈ [162, 245]**（≥ 页面边距 11 + labelW 118
  = 129 才是绘图区，线**没有**画进任务名列），**y 落在 6 档**（240–244 / 285–298 / 304–314 /
  350–354 / 390–394 / 447–450）—— 边是真的落在各行自己量出来的高度上，不是一套常量推出来的。

## 3. 已知偏差 / 做不到的

1. **量之前不画边**（`visibility: hidden`）：渲染件是纯字符串函数，量不到 rect；与其先画一套
   假几何再改，不如等实测（同一帧内完成，正常渲染看不到这一瞬）。代价：如果有人把这段 HTML
   静态存下来、且不带 JS 跑，边不会出现（`data-laid-out="false"`）。
2. **宽栏条内回声仍会省略号截断**（`49/225`）。判据按"占它自己的可用宽 ≥60%"取的（实测 96%）；
   名字的完整可读性由左侧名列负责。窄栏直接不画。
3. **框高对账 ±1px**：`clientHeight` 是取整值，Σ(行高) 与框高在宽栏会差 1px（窄栏 Δ0）。
4. **行高保底值仍在**（38px / 窄栏 30px）：名字只有一行时行高就是保底值，不再"更矮"。
5. **`.team-dag-depnote`（条左上方那行 `depends_on: …`）仍钉在行的 `top: 0`**，宽栏它可能被行末
   截断（它是次级信息，窄栏本来就不画）。
6. **`roleSlotOf` 的登记次序仍是"首次进入渲染的次序"**（模块级 append-only），本件没动。
7. **本件没做**：真实产品右栏里的端到端点击（工作项入口子页面）——那一层在 `app.js`，本轮口径
   是"app.js 一行不改"。
