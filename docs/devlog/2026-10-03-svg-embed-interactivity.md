# 会话内 SVG/HTML 渲染块：交互能力实测与跨帧动作通道（步骤 1–3）

- 日期：2026-10-03
- 范围：`gui/frontend/dist/{html-embed.js,html-embed.test.mjs,embed-bridge.js,embed-bridge.test.mjs,app.js}`、
  `internal/promptassets/{assets/system/instructions.md,planact_harness.go}`、`gui/frontend/README.md`、
  `CHANGELOG.md`
- 上游：用户口径（"svg 支不支持交互动作，如果支持看看后续怎么做"；"b 做 1、2、3，同时需要
  在 3 中驱动 agent"）、`docs/devlog/2026-09-11-computer-use-input-and-html-embed.md`（沙箱契约）

## 1. 调研结论（Playwright 实测，先做后写）

做法：搭探针页**复刻 `html-embed.js` 的真实条件**（`sandbox="allow-scripts"` 无
`allow-same-origin` + srcdoc 内嵌 CSP `default-src 'none'`），逐项驱动后读回结果。
探针页留在 `tmp/svg-interactive-probe{,2,3}.html`。

**支持，而且能力相当完整**（块内交互不需要任何应用改动）：

| 能力 | 实测 |
|---|---|
| CSS `:hover`/`:focus`/`transition`/`@keyframes` | ✅ 颜色/矩阵随时间变化 |
| 内联 JS 改 SVG DOM | ✅ 点击改属性、改文本 |
| SMIL `<animate>`/`<animateTransform>` | ✅ CTM 在动 |
| `<title>` 原生 tooltip、`tabindex` + 键盘 | ✅ 焦点与 keydown 都到 |
| viewBox 缩放下的指针命中 | ✅ `getScreenCTM().inverse()` 换算正确 |
| `<foreignObject>` 里的真 HTML 控件 | ✅ 按钮可点 |
| 指针拖拽（`setPointerCapture` + 坐标换算） | ✅ 节点被拖动 |
| `<use>`/`<symbol>` 复用 | ✅ `fill` 继承进 shadow，hover/click 正常 |
| **沙箱边界** | ✅ `parent.document`/`localStorage`/`top.location` 全 `SecurityError`，`origin=null`，无 bridge |
| **断网** | ✅ `fetch`/XHR/`Image` 全被 CSP 挡 |
| 链接 / 弹窗 | ✅ `window.open` 返回 `null`，外链被拦 |
| **反向出口** | ❌ 过去**完全没有**：块内无法让宿主做任何事 → 这正是步骤 3 要补的 |

两个实测到的坑，后来落成 `EMBED_BASE_CSS` 的兜底（见 §3）：

1. `<text>` 标签压在图形上会**吃掉指针事件**——点击落在 text 上，图形的处理器根本不触发
   （Playwright 的 click 会直接报 "text intercepts pointer events"）。
2. CSS `transform` 作用在 SVG 元素上时默认参考框是 viewBox，`rotate()` 会绕画布中心转。

## 2. 步骤 1：提示词层给出配方与红线

`internal/promptassets/assets/system/instructions.md` 的
「Rendered HTML for Visual Answers」新增 **Interaction inside the block** 段，写清：

- 点击目标放在**图形**上，不放在标签上（标签默认 `pointer-events:none`；要它自己可点就写
  `style="pointer-events:auto"`）；
- 帧内交互自由发挥（hover / 类切换 / 内联 JS / SMIL / CSS 动画 / `foreignObject` / 指针拖拽），
  动画写 `transform-box:fill-box` 并以 `prefers-reduced-motion` 兜住动效；
- 跨帧只能用**白名单动作**，声明式 `<rect data-seelex-action="ask-agent"
  data-seelex-payload='{"text":"…"}'>` 或 `seelex.emit(action, payload)`；
- 要驱动会话必须给围栏加 `interactive=1`，且只认块内刚发生的真实手势；
- 红线：**不许把 driving 动作接在 `onload`/timer/自动重放上**。

`internal/promptassets/planact_harness.go` 增一条 harness 用例
`visual-answer-can-drive-the-conversation`，把这些子句钉成回归项（与其他用例同一机制：
`Required` 是渲染后资产里必须出现的子串）。

## 3. 步骤 2：`EMBED_BASE_CSS` 两条兜底

- `:where(svg text){pointer-events:none}` —— 标签默认不当命中面。
- `:where(svg rect,svg circle,…,svg g,svg use){transform-box:fill-box}` —— 就地旋转有确定的参考框。

两条都用 `:where()`（特异性 0）：作者写**任意**一条自己的规则就能覆盖，不跟模型产出的样式
抢权重。"标签要自己可点"的逃生口是 `style="pointer-events:auto"`（行内样式必胜）。

## 4. 步骤 3：跨帧动作通道（含"驱动 agent"）

### 4.1 协议与判据分居两处

- **协议**住 `html-embed.js`（谁是发送端谁定协议）：标签 `seelex-embed`、两个 data 属性、
  动作表 `EMBED_ACTION_SPECS`、手势窗口、注入脚本 `buildEmbedBridgeScript()`。
  该文件保持**零 import**——既有 11 个测试文件按 data: URL 内联它，多一条 import 就要同时
  改所有内联链（本次特意避开）。
- **宿主判据**住新增的 `embed-bridge.js`：`parseEmbedAction` → `normalizeEmbedPayload` →
  `resolveEmbedAction`（顺序即闸门顺序）+ `createEmbedActionGate`（限流）。纯函数，5 条用例。

### 4.2 动作白名单（封闭集合）

| 动作 | 有后果 | 载荷 | 宿主落点 |
|---|---|---|---|
| `ask-agent` | ✅ driving | `{text}` ≤4000 | `composerSubmitPlan` + `invoke`，**发一条会话消息**（不动输入框、不覆盖用户草稿） |
| `fill-composer` | — | `{text}` ≤4000 | 写进输入框（**追加**，不覆盖），不发送 |
| `copy-text` | — | `{text}` ≤4000 | `navigator.clipboard` |
| `open-source` | — | 无 | 开合该块的「查看源码」 |

超限**拒绝而不是截断**：把要发给 agent 的话从中间砍掉比不支持更危险。新增动作 = 新增一次
能力授予，必须同时写出载荷上限与 `driving`，不许"顺手支持一下"。

### 4.3 三道闸门（driving 动作）

1. **块级自愿**：围栏写 `interactive=1` → 渲染成 `data-embed-interactive="1"`，说明牌同时显示
   「可驱动会话」。非自愿的块发来 `ask-agent` → `block-not-interactive`。
2. **块内真实手势**：帧内脚本用 `event.isTrusted` 记时，只有 1.5s 内的真实点击/按键才放行；
   `onload`/`setTimeout` 里 `seelex.emit("ask-agent", …)` 返回 `false`（宿主拿不到块内输入
   事件——只有帧内脚本分得清"人点的"和"脚本发的"）。宿主侧另有一道 fail-closed：driving 动作
   没接限流器时 `gate-missing`，不放行。
3. **宿主限流**：滑动窗口 10s 内最多 3 次 + 同正文 1.5s 内去重。防的是块内脚本把动作循环
   发出去（重放/双击），不是"用户点得快"。

### 4.4 身份判据（为什么不用 origin）

无 `allow-same-origin` 的 iframe 是 opaque origin，`event.origin` 恒为 `"null"`——按 origin 判
等于不判。`app.js` 拿 `event.source` 与本页 `iframe.html-embed-frame` 的 `contentWindow` 逐个
比对，不匹配的消息**静默忽略**（不回执、不报错：块内本来就没有回执通道，刷屏会把"没收到"
误读成故障）。沙箱属性一字未改（仍只有 `allow-scripts`）。

### 4.5 一个刻意的取舍

`ask-agent` 发出的消息**不加来源前缀**：它由模型在同一会话里写出的指令构成，会话记录里就是一条
普通用户轮。可见性靠 toast（「图形视图已向本会话发送一条请求」）与说明牌上的「可驱动会话」，
而不是往 agent 上下文里塞标号。若日后需要审计口径，改一处 `applyEmbedAction` 即可。

## 5. 回归证据

- `node --test gui/frontend/dist/*.test.mjs` → **576/576**（新增 `embed-bridge.test.mjs` 5 条 +
  `html-embed.test.mjs` 3 条）。
- **Playwright 端到端**（`tmp/svg-bridge-probe.html`，用真实的 `renderHtmlEmbed` 输出搭宿主页，
  复刻 app.js 的身份判据）：
  - 帧内无手势的 `ask-agent`：`window.__auto === false`（帧内就被丢）；
  - 别的 iframe 发来的同协议消息：`matched=false`（身份判据挡住）；
  - 点击**标签压住的图形中心**（Playwright 不带 force 的可点性判定通过）：宿主收到
    `matched=true action=ask-agent payload={"text":"解释节点 A"}` —— 同时验证兜底 CSS 与桥；
  - 只读动作 `copy-text` 在非自愿块上也被收到（`matched=true`）。
- `go build ./...` / `go vet ./internal/... ./gui/... ./application/... ./seelebridge/... ./tui/...`：通过。
- `go test ./internal/promptassets/ -count=1`：通过（新 harness 用例在内）。

## 6. 未决 / 边界

- **只做了"块 → 宿主"方向**：宿主**不回写**块内（没有"宿主 → iframe"的数据通道），因此
  没有"选中同步/服务端推送改图"这类能力。需要时再单开协议（回写会把沙箱块变成持久组件，
  口径要重新定）。
- **重交互（平移缩放编辑器、可写画布）仍不建议塞进 embed**：嵌入块是"一条消息的产物"，
  不是持久画布；那类需求应做成独立面板。
- **真机 GUI 未做 computer-use 逐操作冒烟**：本次端到端用的是沙箱语义等价页（真实渲染件
  输出 + 复刻的宿主接线）。要跑真机需重启 `dist/seelex-gui-dev/seelex-gui.exe`（当前实例是
  旧二进制）。
- `prefers-reduced-motion` 只写进提示词配方，**没有**做成 `EMBED_BASE_CSS` 的强制规则——
  它会把图表动画一并停掉，属于产品口径而不是兜底。
