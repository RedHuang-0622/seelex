# 沙箱渲染块 v2：内置组件 + 主题 token + 可见的加载态（2026-10-29）

日期：2026-10-29
范围：`gui/frontend/dist/{html-embed,embed-buffering,conversation-view,agent-conversation-view,app,styles}.{js,css}` + 新增 `gui/frontend/dist/embed-theme.js` 与 `embed-theme.test.mjs` + 提示词资产 `kernel/prompt/assets`
上游约束：[`../gui/FRONTEND-CONSTITUTION.md`](../gui/FRONTEND-CONSTITUTION.md)（加载态 §2、动效 token §4、属性白名单 §4.5、迁移纪律 §9）

## 改了什么（三件事，一个能力）

### 1. 块内颜色不再写死：`--sx-*` 跟随皮肤 × 明暗

旧实现把**深色**烤进了 srcdoc：`:root{color-scheme:dark}` + `color:#e9e4d8` + 表框 `#3c4a56`。
浅色用户看到的是一块深色面板，换皮肤更是完全不动。

现在块内只有一组 `--sx-*`：

- **base 那套**（`--sx-bg/panel/surface/surface-2/border/border-strong/text/text-strong/text-soft/muted/faint/accent/on-accent/code-bg`、字体、圆角、时长）跟随**两条轴**：皮肤（`themes/<skin>.css` 改 `--accent*`）与明暗（`:root` / `:root[data-theme="dark"]`）。
- **语义色四族**（`--sx-warn/danger/ok/info` 及其 `--sx-tint-*` / `--sx-border-*`）只随明暗、**不随皮肤**：它们的来源在宿主里就是 skin-independent 的 `--status-*` / `--tint-*` / `--border-*`。"警告"换个皮肤也还是那个警告色，不会被品牌主信号染掉（`embed-theme.test.mjs` 钉住两张表不相交）。

**怎么流进块内**：帧是 `sandbox="allow-scripts"` 且**没有** `allow-same-origin`，是 opaque
origin——宿主读不到它的 DOM，它也读不到宿主的 CSS 变量。所以只有"宿主推"这一条路：

```
theme.js 的 themeController.onApplied
        └─► embed-theme.js  broadcast()          ← 换肤 / 切深浅
            embed-theme.js  load 捕获监听         ← 每枚新帧加载完补发一次
                 └─► postMessage {source:"seelex-host", type:"theme", mode, tokens}
                      └─► html-embed.js 桥脚本 applyTheme()
                            └─► documentElement.style.setProperty("--sx-*", …)
```

于是**换肤不重建 srcdoc、不重载帧**：块里的滚动位置、选中态、正在播的动画都留着。
载荷只允许写 `--sx-*`（名字白名单 + 值里不许 `;{}<>`、`url(`、`expression(`、`@import`），
接收端还会再校验一遍并只认 `event.source === parent`——这条通道**不授予任何能力**。

没有桥脚本的读者（文件预览 `renderSandboxFrame`）拿不到这条通道，靠沙箱 CSS 里的
`@media (prefers-color-scheme: light)` 兜底；预览一份磁盘 HTML 不该有会话级通道，这是刻意的。

### 2. 内置组件层：不必从头画一套皮

`<article>`（卡片，带 `<header>/<footer>`）、`<button>`/`role="button"`、`<table>`、
`<progress>`、`<details>`、`<code>/<pre>/<kbd>/<mark>` 按应用口径出形；另有
`.sx-card` / `.sx-btn`(+`-primary` / `-ghost`) / `.sx-chip`(+`.is-warn/.is-ok/.is-danger/.is-info`) /
`.sx-row` / `.sx-stack` / `.sx-meter` 别名。

**特异性是刻意的**：每条组件规则都包在 `:where()` 里（特异性 0），作者写任何一条自己的
规则都赢。预置只兜底、不夺权——"块里的图被预置样式压住"是最难解释的一类 bug。

提示词资产（`kernel/prompt/assets/assets/system/instructions.md` 的
*Built-in components and theme tokens*）同步写进了用法，并由
`planact_harness.go` 新增用例锁住：组件名、`--sx-accent`、语义色不跟品牌色、不许写死底色。

### 3. 换帧：加载态可见 + 交叉淡入淡出

旧的 `doubleBuffer` 开关把**加载过程本身**缓冲没了：离屏加载期间用户看到的是一块不动的
旧画面，帧换上去的瞬间才"啪"地出现——既没有进度，也没有变化感。开关已删（宪法 §9：不留
第二套兼容路径），改成恒定两件事同时成立：

**加载态（宪法 §2.2 状态机，挂在 `.html-embed-body`）**

| 阶段 | 触发 | 视觉 |
|---|---|---|
| `idle` | 静止的块 | 什么都没有，也没有 `aria-busy` |
| `pending` | 挂载已发起 | 无（前 100ms 不显示指示器，短加载因此只有最终内容） |
| `active` | 100ms 后仍未落帧 | 状态文字 + 一条不确定活动条，容器 `aria-busy="true"` |
| `defer` | 会话仍在流式输出 | 静态文字"等待内容稳定"，**不放活动条**（流式有它自己的 cadence） |
| `settled` | 帧落位 | 加载层淡出，`aria-busy` 摘掉，文字清空 |

活动条只动 `transform`（60fps 友好的白名单属性），周期沿用 `--duration-shimmer`；
它只在 `active` 时可见，不存在静止循环。

**交叉淡入淡出（唯一一条换帧路径）**

- 新帧以 `opacity:0`（`data-embed-fade="in"`）叠进落位台，下一帧切 `settled` →
  `opacity:1`，`--duration-normal` / `--ease-out`；
- 旧帧同时切 `out` → `opacity:0`，`--duration-fast` / `--ease-in`（退出短于进入，宪法 §4.2），
  过渡结束由脚本撤离；
- 两层都在固定高度的 `.html-embed-stage` 里绝对定位，**落位台几何一秒都没动** → 零 CLS；
- `prefers-reduced-motion: reduce`：过渡全部 `none`（直接到终态），活动条变成静态填充条，
  状态文字照常可读——不隐藏内容、不跳过状态（宪法 §2.5 / §5.4）。

`embed-buffering.js` 里对应新增 `EMBED_LOADING_FIRST_DELAY_MS = 100`、
`EMBED_FADE_OUT_MS = 150`，以及 `syncLoading()` 这一个"这一槽当下该是什么加载态"的判据
（宪法 §7.3：同一状态不许有两套判据）。

## 怎么看是否生效

1. **跟着主题走**：切皮肤（主题选择器）与切深浅（浅色/深色）各来一次，块内卡片、表格、
   按钮、芯片的底色与文字应同时改变，而 `iframe` 节点**不重建**（devtools 元素面板看节点
   是否闪烁/换 id）。警告芯片在任何皮肤下都还是琥珀色。
2. **语义色不被染**：切到品牌色最艳的皮肤（铜金/铜褐），`.sx-chip.is-warn` 仍是警告色，
   只有普通卡片跟着品牌色走。
3. **加载态**：让模型输出一个 `seelex-html` 块并在流式结束后改动一行别处的文字触发重挂；
   盯住那块区域：先是旧画面（或空），约 100ms 后出现"正在渲染图形"+ 活动条，随后新旧
   两帧交叉淡入淡出，卡片高度全程不变。短暂加载（<100ms）不该出现任何指示器。
4. **流式期间**：会话还在跑时不出现活动条，只有静态的"等待内容稳定"；定稿后一次挂上。
5. **reduced-motion**：系统开启"减少动态效果"后重做第 3 步：没有淡入淡出、活动条是静止
   的一条，但状态文字与最终图形都在。

## 证据

- `node --test gui/frontend/dist/*.test.mjs` —— **997 项全绿**，含新增
  `embed-theme.test.mjs`（11 项：映射表不相交、语义色只取 status 族、读取回落、值清洗、
  继电器广播/补发/幂等、接收端只写 `--sx-*`）、`embed-buffering.test.mjs` 的加载态与
  淡入淡出用例、`html-embed.test.mjs` 的主题/组件/加载层标记用例。
- `go test ./kernel/prompt/...` —— 提示词 harness 通过（新增
  `visual-answer-uses-builtin-components-and-theme-tokens` 用例）。
- `node --check` 覆盖 `html-embed.js` / `embed-theme.js` / `embed-buffering.js` / `app.js`。
- **未做浏览器实拍**（本地无 GUI 自动化在跑）：观感类结论（淡入淡出是否顺、加载层是否
  闪）需人眼按上面五条确认；状态机与时序由 `embed-buffering.test.mjs` 的最小 DOM 假体
  + 注入时钟覆盖。

## 已知边界

- 主题消息在帧 `load` 时发出，跨文档投递是异步的。落帧淡入发生在 `load` 之后的下一帧，
  实践上主题先到；最坏情况是首帧按缺省深色画一次再被校正——因为帧在 `settled` 之前是
  `opacity:0`，肉眼通常看不到这一帧。
- 文件预览（`renderSandboxFrame`）收不到换肤通道，只有 `prefers-color-scheme` 兜底；
  要让它跟应用深浅一致，需要给它一条独立通道——那会改变预览"不带跨帧桥"的既有口径，
  留给单独一条需求。
- 组件层的字体栈取宿主的 `--font-ui` / `--font-mono`，块内若引用了宿主没有的字体族
  仍会回落到系统字体（沙箱离线，不能拉网络字体）。
