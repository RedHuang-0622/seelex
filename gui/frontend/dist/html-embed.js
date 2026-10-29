// 会话内 HTML 渲染块（让回答能画出图表/示意图，而不是只给一段文字或源码）。
//
// 事实边界：这是**模型产出的 HTML**，属于不可信内容。渲染规则因此是死的：
//
//   1. 绝不把 HTML 注入应用 DOM —— 只放进 <iframe srcdoc>；
//   2. iframe 只给 `sandbox="allow-scripts"`，**不给 `allow-same-origin`**：
//      没有 same-origin 就是 opaque origin，脚本拿不到宿主 DOM、localStorage、
//      cookie，也够不到 Wails bridge（`allow-scripts` + `allow-same-origin`
//      一起给等于没沙箱，这是必须避免的组合）；
//   3. srcdoc 内嵌 CSP：`default-src 'none'` + 只允许内联样式/脚本与 data: 图片，
//      断掉 connect/img/script 的网络出口（不联网、不埋点、不外传）；
//   4. 不给 allow-forms / allow-popups / allow-top-navigation，弹窗、表单提交、
//      跳转宿主页面全部无效；
//   5. 源码同时以转义文本附在块内，用户能自己核对渲染了什么。
//
// 反方向**开了一条窄路**：块内可以 `postMessage` 到宿主，让画布上的动作变成宿主动作
// （填输入框、向本会话发一条请求、复制文本、展开源码）。**协议**定义在本文件（谁是
// 发送端谁定协议）；**宿主怎么判**在 embed-bridge.js。按围栏参数 `interactive=1` 标出的
// 块才被允许做"有后果"的动作 `ask-agent`，其余块即使发出消息也会被拒。
//
// 颜色同理**不写死**：块内只有一组 `--sx-*` token，缺省是一份深色基座，宿主在帧 load
// 后与每次换肤时用 postMessage 把真实 token 推进来（发送端 embed-theme.js，接收端在本
// 文件的桥脚本里）——所以换肤不重建 srcdoc、不重载帧、块内交互状态不丢。**警告 / 危险 /
// 成功 / 信息**四族语义色只随明暗取值、不随皮肤（"警告"换皮肤也还是那个警告色）。
// 元素层面有一层**内置组件**兜底：`<article>` 就是卡片、`<button>` 就是按钮、
// `<table>` / `<progress>` / `<details>` / `<code>` 都按应用口径出形，另有 `.sx-*` 别名，
// 全部装在 `:where()` 里（特异性 0），作者自己的样式永远优先。
//
// 触发方式：markdown 围栏代码块，语言标记为 `seelex-html`（别名
// `html-preview`）；普通 ```html 仍然是源码块，不受影响。
//
// 另有一条读者：**文件预览**（`file-preview.js` 的只读 HTML 预览）。它复用本文件的
// 沙箱口径与文档组装，但**不带跨帧桥**（`buildHtmlPreviewDocument` / `renderSandboxFrame`）
// ——预览一份磁盘上的 HTML 不该能驱动会话。渲染块的桥仍是这里的 `buildEmbedDocument`。
//
// 本文件**零 import**（测试按 data: URL 内联它，见 markdown.test.mjs / 各 *.test.mjs）；
// 纯函数（isHtmlEmbedLanguage/parseEmbedInfo/buildEmbedDocument/renderHtmlEmbed）
// 可离线单测，见 html-embed.test.mjs。

const EMBED_LANGUAGES = new Set(["seelex-html", "html-preview"]);
const DEFAULT_TITLE = "图形视图";
const DEFAULT_HEIGHT = 260;
const MIN_HEIGHT = 120;
const MAX_HEIGHT = 640;

// ── 跨帧动作协议（iframe → 宿主唯一的出口）────────────────────────────────
// 块内的一切交互默认留在帧内：hover、点击改类、SMIL/CSS 动画、foreignObject 控件、
// 指针拖拽都不需要任何协议。要让"画布里的动作"变成宿主动作，只有 postMessage 一条路。

export const EMBED_BRIDGE_TAG = "seelex-embed";

// 宿主 → 块内**唯一一条**通道：换肤。块是无 same-origin 的 opaque origin，宿主读不到
// 它的 DOM，它也读不到宿主的 token，所以主题只能由宿主**推**进去（embed-theme.js 发、
// buildEmbedBridgeScript 收）。载荷只允许写 `--sx-*` 自定义属性，不承载任何能力。
export const EMBED_HOST_TAG = "seelex-host";
export const EMBED_THEME_MESSAGE = "theme";

// 声明式动作的两个属性：`data-seelex-action` + `data-seelex-payload`。提示词层
// （internal/promptassets）与 GUI README 都按这两个名字写配方，改名要一起改。
export const EMBED_ACTION_ATTR = "data-seelex-action";
export const EMBED_PAYLOAD_ATTR = "data-seelex-payload";

// EMBED_GESTURE_WINDOW_MS 是「块内刚发生过真实手势」的有效窗口。定得比一次点击的
// 处理链长、比一个人反复点同一处短：够用，但拦住 onload/timer 的自动触发。
export const EMBED_GESTURE_WINDOW_MS = 1500;

// EMBED_ACTION_SPECS 是动作表：**唯一可跨出沙箱的动词集合**。
//   - `driving: true` 的动作会改变会话状态（替用户发一条消息），需要块级自愿（围栏
//     `interactive=1`）+ 块内真实手势 + 宿主侧限流；`false` 的动作只碰宿主本地
//     （剪贴板/输入框/源码压缩）——但同样只走这一张白名单，没有"顺手支持一下"。
//   - `maxText` 是载荷里 `text` 的字符上限（0 = 该动作不收文本）。
export const EMBED_ACTION_SPECS = Object.freeze({
  "ask-agent": { driving: true, maxText: 4000 },
  "fill-composer": { driving: false, maxText: 4000 },
  "copy-text": { driving: false, maxText: 4000 },
  "open-source": { driving: false, maxText: 0 }
});

export const EMBED_ACTION_NAMES = Object.freeze(Object.keys(EMBED_ACTION_SPECS));

// buildEmbedBridgeScript 生成注入每个渲染块文档的桥脚本（内联，CSP 的
// `script-src 'unsafe-inline'` 允许）。它做两件事：
//   1) 给出唯一出口 `seelex.emit(action, payload)`；
//   2) 让**声明式**动作可用：`[data-seelex-action]` 上的 click / Enter / Space 自动
//      emit，模型不必为了"点一下"写脚本。
// 手势判定也在这一侧（`event.isTrusted`）：宿主拿不到块内的输入事件，只有帧内脚本分得
// 清"人点的"和"脚本发的"。脚本排在正文之前，块内脚本一执行就拿到 seelex。
export function buildEmbedBridgeScript() {
  const driving = EMBED_ACTION_NAMES.filter(name => EMBED_ACTION_SPECS[name].driving);
  return [
    "<script>",
    "(function () {",
    `var TAG = ${JSON.stringify(EMBED_BRIDGE_TAG)};`,
    `var ACTION_ATTR = ${JSON.stringify(EMBED_ACTION_ATTR)};`,
    `var PAYLOAD_ATTR = ${JSON.stringify(EMBED_PAYLOAD_ATTR)};`,
    `var HOST_TAG = ${JSON.stringify(EMBED_HOST_TAG)};`,
    `var THEME_MESSAGE = ${JSON.stringify(EMBED_THEME_MESSAGE)};`,
    `var DRIVING = ${JSON.stringify(driving)};`,
    `var ACTIONS = ${JSON.stringify(EMBED_ACTION_NAMES)};`,
    `var GESTURE_WINDOW_MS = ${EMBED_GESTURE_WINDOW_MS};`,
    "var lastGesture = 0;",
    "function noteGesture(event) { if (event && event.isTrusted) lastGesture = Date.now(); }",
    "document.addEventListener(\"pointerdown\", noteGesture, true);",
    "document.addEventListener(\"keydown\", noteGesture, true);",
    "function isDriving(action) { return DRIVING.indexOf(action) >= 0; }",
    "function emit(action, payload) {",
    "  var name = action == null ? \"\" : String(action);",
    "  if (!name) return false;",
    "  // 有后果的动作只认刚发生的真实手势：onload / timer 里发的消息一律丢掉。",
    "  if (isDriving(name) && Date.now() - lastGesture > GESTURE_WINDOW_MS) return false;",
    "  try {",
    "    parent.postMessage({ source: TAG, action: name, payload: payload === undefined ? null : payload }, \"*\");",
    "    return true;",
    "  } catch (error) { return false; }",
    "}",
    "function payloadOf(element) {",
    "  var raw = element.getAttribute(PAYLOAD_ATTR);",
    "  if (raw === null) return null;",
    "  try { return JSON.parse(raw); } catch (error) { return String(raw); }",
    "}",
    "function activate(event) {",
    "  var target = event.target;",
    "  var element = target && target.closest ? target.closest(\"[\" + ACTION_ATTR + \"]\") : null;",
    "  if (!element) return;",
    "  if (event.type === \"keydown\" && event.key !== \"Enter\" && event.key !== \" \" && event.key !== \"Spacebar\") return;",
    "  emit(element.getAttribute(ACTION_ATTR), payloadOf(element));",
    "}",
    "document.addEventListener(\"click\", activate);",
    "document.addEventListener(\"keydown\", activate);",
    // ── 换肤接收端（宿主 → 块内唯一一条通道）────────────────────────────
    // 只认父窗口发来的换肤消息；载荷只能写 `--sx-*` 自定义属性，写不进别的声明，
    // 因此这条通道**不授予任何能力**（它改不了 DOM、拿不到数据、发不出网络请求）。
    "var TOKEN_NAME = /^--sx-[a-z0-9-]{1,32}$/;",
    "var TOKEN_VALUE_BAD = /[;{}<>\\\\]|url\\s*\\(|expression\\s*\\(|@import/i;",
    "function applyTheme(data) {",
    "  var root = document.documentElement;",
    "  var tokens = data.tokens;",
    "  if (tokens && typeof tokens === \"object\") {",
    "    for (var name in tokens) {",
    "      if (!Object.prototype.hasOwnProperty.call(tokens, name)) continue;",
    "      if (!TOKEN_NAME.test(name)) continue;",
    "      var value = tokens[name];",
    "      if (typeof value !== \"string\" || !value || value.length > 256) continue;",
    "      if (TOKEN_VALUE_BAD.test(value)) continue;",
    "      root.style.setProperty(name, value);",
    "    }",
    "  }",
    "  if (data.mode === \"dark\" || data.mode === \"light\") {",
    "    root.setAttribute(\"data-theme\", data.mode);",
    "    root.style.colorScheme = data.mode;",
    "  }",
    "}",
    "window.addEventListener(\"message\", function (event) {",
    "  if (event.source !== parent) return;",
    "  var data = event.data;",
    "  if (!data || typeof data !== \"object\") return;",
    "  if (data.source !== HOST_TAG || data.type !== THEME_MESSAGE) return;",
    "  applyTheme(data);",
    "});",
    "window.seelex = { emit: emit, actions: ACTIONS };",
    "})();",
    "</script>"
  ].join("\n");
}

// ── 沙箱主题 token（`--sx-*`）────────────────────────────────────────────
//
// 宿主有两条正交的外观轴（皮肤 × 深浅，见 theme.js），沙箱里把它们收敛成一组
// `--sx-*`：块内写 `var(--sx-accent)` 就跟着当前皮肤与明暗走，不需要知道宿主把这些
// 变量叫什么。**语义色例外**：警告 / 危险 / 成功 / 信息只按明暗取值——它们在宿主里
// 本来就是 skin-independent 的 `--status-*` 一族，不能被品牌色染掉（"警告"得一直是
// 那个警告色，换个皮肤不能变成品牌色）。
//
// 这些值是**缺省**：宿主真实 token 由 embed-theme.js 在帧 load 后、以及每次换肤时用
// postMessage 推进来覆写。所以换肤不会重建 srcdoc、不会重载帧、块内交互状态不丢。
export const EMBED_THEME_TOKENS_DARK = Object.freeze({
  "--sx-bg": "#181818",
  "--sx-panel": "#1f1f1f",
  "--sx-surface": "#262626",
  "--sx-surface-2": "#2e2e2e",
  "--sx-border": "#333333",
  "--sx-border-strong": "#474747",
  "--sx-text": "#e4e4e4",
  "--sx-text-strong": "#ffffff",
  "--sx-text-soft": "#c8c8c8",
  "--sx-muted": "#a3a3a3",
  "--sx-faint": "#7e7e7e",
  "--sx-accent": "#e9e9ec",
  "--sx-on-accent": "#16161a",
  "--sx-code-bg": "#131313",
  "--sx-font-ui": "'Segoe UI Variable','Microsoft YaHei',system-ui,sans-serif",
  "--sx-font-mono": "ui-monospace,'Cascadia Mono',Consolas,monospace",
  "--sx-radius-sm": "6px",
  "--sx-radius": "8px",
  "--sx-radius-lg": "10px",
  "--sx-duration-fast": "150ms",
  "--sx-duration-normal": "250ms",
  "--sx-ease-out": "cubic-bezier(.16,1,.3,1)",
  "--sx-ease-in": "cubic-bezier(.55,.06,.68,.19)",
  // 语义色：只随明暗，不随皮肤（宿主 --status-* 同一条口径）。
  "--sx-warn": "#e0a45c",
  "--sx-ok": "#4cc38a",
  "--sx-danger": "#f0716a",
  "--sx-info": "#6ba6ff",
  "--sx-tint-warn": "rgba(224,164,92,.12)",
  "--sx-tint-ok": "rgba(76,195,138,.12)",
  "--sx-tint-danger": "rgba(240,113,106,.12)",
  "--sx-tint-info": "rgba(107,166,255,.12)",
  "--sx-border-warn": "rgba(224,164,92,.42)",
  "--sx-border-ok": "rgba(76,195,138,.42)",
  "--sx-border-danger": "rgba(240,113,106,.42)",
  "--sx-border-info": "rgba(107,166,255,.42)"
});

export const EMBED_THEME_TOKENS_LIGHT = Object.freeze({
  "--sx-bg": "#f7f6f2",
  "--sx-panel": "#fbfaf6",
  "--sx-surface": "#ffffff",
  "--sx-surface-2": "#f1f0ea",
  "--sx-border": "#e7e7e4",
  "--sx-border-strong": "#d6d2c6",
  "--sx-text": "#1f2328",
  "--sx-text-strong": "#0f1215",
  "--sx-text-soft": "#3c4148",
  "--sx-muted": "#6b6f76",
  "--sx-faint": "#9096a0",
  "--sx-accent": "#1f2328",
  "--sx-on-accent": "#ffffff",
  "--sx-code-bg": "#f5f4f0",
  "--sx-font-ui": "'Segoe UI Variable','Microsoft YaHei',system-ui,sans-serif",
  "--sx-font-mono": "ui-monospace,'Cascadia Mono',Consolas,monospace",
  "--sx-radius-sm": "6px",
  "--sx-radius": "8px",
  "--sx-radius-lg": "10px",
  "--sx-duration-fast": "150ms",
  "--sx-duration-normal": "250ms",
  "--sx-ease-out": "cubic-bezier(.16,1,.3,1)",
  "--sx-ease-in": "cubic-bezier(.55,.06,.68,.19)",
  "--sx-warn": "#b26a00",
  "--sx-ok": "#1f9c63",
  "--sx-danger": "#d64545",
  "--sx-info": "#2e6be6",
  "--sx-tint-warn": "rgba(178,106,0,.10)",
  "--sx-tint-ok": "rgba(31,156,99,.10)",
  "--sx-tint-danger": "rgba(214,69,69,.10)",
  "--sx-tint-info": "rgba(46,107,230,.10)",
  "--sx-border-warn": "rgba(178,106,0,.38)",
  "--sx-border-ok": "rgba(31,156,99,.38)",
  "--sx-border-danger": "rgba(214,69,69,.38)",
  "--sx-border-info": "rgba(46,107,230,.38)"
});

// EMBED_DEFAULT_TOKENS 是"还没有收到宿主消息"时的缺省（深色）。宿主侧 embed-theme.js
// 读不到 token 时也回退到这一份——两边同一处供源，别各写一套兜底色。
export const EMBED_DEFAULT_TOKENS = EMBED_THEME_TOKENS_DARK;

function tokenDeclarations(tokens) {
  return Object.keys(tokens).map(name => `${name}:${tokens[name]}`).join(";");
}

// 主题层排在最前：组件层的每条规则都只引用 `--sx-*`，自己不写颜色。
const EMBED_THEME_CSS = [
  `:root{color-scheme:dark;${tokenDeclarations(EMBED_THEME_TOKENS_DARK)}}`,
  // 浅色兜底的读者只有文件预览（它没有换肤通道）：跟随系统偏好，别在浅色页面上画深色块。
  `@media (prefers-color-scheme: light){:root{color-scheme:light;${tokenDeclarations(EMBED_THEME_TOKENS_LIGHT)}}}`
].join("");

// EMBED_BASE_CSS 只做最基本的可读性兜底：用户自己的样式优先，避免"预置主题"
// 把图表的配色盖掉。两条 SVG 兜底来自实测（Playwright 探针，见 devlog
// 2026-10-03-svg-embed-interactivity），都是"点了没反应/转得不对"的常见因：
//
//   - `:where(svg text){pointer-events:none}`：`<text>` 标签压在图形上时会**吃掉**
//     指针事件，点在字上的 click 到不了图形（图形自己的处理器根本不触发）。
//     标签默认不当命中面；要让某个标签自己可点，给它写 `style="pointer-events:auto"`
//     或任意一条自己的 `text` 规则即可——`:where()` 特异性为 0，不为难作者。
//   - `:where(svg rect,…){transform-box:fill-box}`：CSS `transform` 作用在 SVG 元素上
//     时，默认参考框是 viewBox，`transform:rotate()` 会绕画布中心转；`fill-box` 才是
//     作者通常要的"就地转"。同样用 `:where()` 兜底，作者写了自己的规则就听作者的。
const EMBED_BASE_CSS = [
  "html,body{margin:0;padding:10px;background:transparent;color:var(--sx-text);",
  "font:13px/1.5 var(--sx-font-ui)}",
  "body>*:first-child{margin-top:0}body>*:last-child{margin-bottom:0}",
  "svg{max-width:100%;height:auto}",
  ":where(svg text){pointer-events:none}",
  ":where(svg rect,svg circle,svg ellipse,svg path,svg polygon,svg polyline,svg line,svg image,svg g,svg use){transform-box:fill-box}"
].join("");

// EMBED_COMPONENT_CSS 是**内置组件层**：块里不必从头画一套皮，语义元素按应用的口径
// 出形（`<article>` = 卡片、`<button>` = 按钮、`<table>` = 表格、`<progress>` = 进度），
// 另有一组 `.sx-*` 别名给不想用语义标签的场合。全部走 `--sx-*`，因此天然跟随皮肤与
// 明暗，换肤不用改块内 HTML。
//
// 特异性是刻意的：一律 `:where()` 包一层（特异性 0），作者写任何一条自己的规则都赢，
// 不会出现"预置组件压住我的图"——预置只兜底，不夺权。
const EMBED_COMPONENT_CSS = [
  ":where(h1,h2,h3,h4){margin:0 0 6px;font-size:14px;font-weight:700;color:var(--sx-text-strong)}",
  ":where(p,ul,ol){margin:0 0 8px}",
  ":where(ul,ol){padding-left:18px}",
  ":where(small,.sx-muted){color:var(--sx-muted)}",
  ":where(a){color:var(--sx-accent);text-decoration:none;border-bottom:1px solid var(--sx-border-strong)}",
  // 卡片：<article> 与其 .sx-card 别名
  ":where(article,.sx-card){margin:0 0 10px;padding:10px 12px;border:1px solid var(--sx-border);",
  "border-radius:var(--sx-radius-lg);background:var(--sx-surface)}",
  ":where(article>header,.sx-card>header){margin:0 0 8px;padding:0 0 6px;",
  "border-bottom:1px solid var(--sx-border);font-weight:600;color:var(--sx-text-strong)}",
  ":where(article>footer,.sx-card>footer){margin:8px 0 0;padding:6px 0 0;",
  "border-top:1px solid var(--sx-border);color:var(--sx-muted)}",
  // 按钮：语义 <button>/role=button，外加 .sx-btn 的三个变体
  ":where(button,[role='button'],.sx-btn){display:inline-flex;align-items:center;gap:6px;",
  "padding:5px 10px;border:1px solid var(--sx-border-strong);border-radius:var(--sx-radius);",
  "background:var(--sx-surface-2);color:var(--sx-text);font:inherit;cursor:pointer;",
  "transition:background-color var(--sx-duration-fast) var(--sx-ease-out),color var(--sx-duration-fast) var(--sx-ease-out)}",
  ":where(button,[role='button'],.sx-btn):hover{background:var(--sx-surface);color:var(--sx-text-strong)}",
  ":where(.sx-btn-primary){background:var(--sx-accent);border-color:var(--sx-accent);color:var(--sx-on-accent)}",
  ":where(.sx-btn-ghost){background:transparent;border-color:transparent;color:var(--sx-muted)}",
  // 表格：数字列等宽，表头弱色、行分隔发丝线
  ":where(table){width:100%;border-collapse:collapse;font-variant-numeric:tabular-nums}",
  ":where(th,td){padding:4px 8px;border-bottom:1px solid var(--sx-border);text-align:left}",
  ":where(thead th){color:var(--sx-muted);font-weight:600;border-bottom-color:var(--sx-border-strong)}",
  // 进度：<progress> 与 .sx-meter>.sx-meter-fill
  ":where(progress){width:100%;height:6px;appearance:none;border:0;border-radius:999px;background:var(--sx-surface-2)}",
  ":where(progress)::-webkit-progress-bar{background:var(--sx-surface-2);border-radius:999px}",
  ":where(progress)::-webkit-progress-value{background:var(--sx-accent);border-radius:999px}",
  ":where(.sx-meter){height:6px;border-radius:999px;background:var(--sx-surface-2);overflow:hidden}",
  ":where(.sx-meter-fill){display:block;height:100%;border-radius:inherit;background:var(--sx-accent)}",
  // 代码 / 折叠面板 / 强调
  ":where(code,kbd){padding:1px 4px;border-radius:var(--sx-radius-sm);background:var(--sx-code-bg);",
  "color:var(--sx-text-soft);font:11px var(--sx-font-mono)}",
  ":where(pre){margin:0 0 8px;padding:8px;border-radius:var(--sx-radius);background:var(--sx-code-bg);",
  "overflow:auto;font:11px/1.5 var(--sx-font-mono)}",
  ":where(pre code){padding:0;background:transparent}",
  ":where(mark){background:var(--sx-tint-warn);color:var(--sx-text)}",
  ":where(details){margin:0 0 8px;padding:6px 8px;border:1px solid var(--sx-border);border-radius:var(--sx-radius)}",
  ":where(summary){cursor:pointer;color:var(--sx-text-soft)}",
  // 状态芯片：语义色只在这里出场（警告/危险/成功/信息），且不跟品牌色走
  ":where(.sx-chip){display:inline-flex;align-items:center;gap:4px;padding:1px 7px;",
  "border:1px solid var(--sx-border-strong);border-radius:999px;background:var(--sx-surface-2);",
  "color:var(--sx-text-soft);font:11px var(--sx-font-mono)}",
  ":where(.sx-chip.is-warn){background:var(--sx-tint-warn);border-color:var(--sx-border-warn);color:var(--sx-warn)}",
  ":where(.sx-chip.is-ok){background:var(--sx-tint-ok);border-color:var(--sx-border-ok);color:var(--sx-ok)}",
  ":where(.sx-chip.is-danger){background:var(--sx-tint-danger);border-color:var(--sx-border-danger);color:var(--sx-danger)}",
  ":where(.sx-chip.is-info){background:var(--sx-tint-info);border-color:var(--sx-border-info);color:var(--sx-info)}",
  // 版式小工具：只给间距，不改作者的字号与颜色
  ":where(.sx-row){display:flex;align-items:center;gap:8px;flex-wrap:wrap}",
  ":where(.sx-stack){display:flex;flex-direction:column;gap:6px}",
  // 动效降级：预置组件的过渡在 reduced-motion 下直接到终态（作者自己的动画由提示词要求）
  "@media (prefers-reduced-motion: reduce){:where(button,[role='button'],.sx-btn){transition:none}}"
].join("");

const EMBED_SANDBOX_CSS = EMBED_THEME_CSS + EMBED_BASE_CSS + EMBED_COMPONENT_CSS;


const EMBED_CSP = [
  "default-src 'none'",
  "img-src data: blob:",
  "style-src 'unsafe-inline'",
  "script-src 'unsafe-inline'",
  "font-src data:",
  "connect-src 'none'",
  "form-action 'none'",
  "base-uri 'none'"
].join("; ");

export function isHtmlEmbedLanguage(raw) {
  return EMBED_LANGUAGES.has(String(raw ?? "").trim().toLowerCase());
}

// parseEmbedInfo 解析围栏信息串上的可选参数：`title="..."`、`height=360` 与
// `interactive`（=1 / 裸写均可）。高度钳制在 [MIN_HEIGHT, MAX_HEIGHT]，避免一条消息
// 把页面撑爆或压成一条缝；`interactive` 是**块级自愿**——只有它为真，宿主才接受这块
// 的"有后果"动作（`ask-agent`，见 embed-bridge.js）。
export function parseEmbedInfo(info = "") {
  const text = String(info ?? "");
  const title = attributeValue(text, "title") || DEFAULT_TITLE;
  const height = clampHeight(attributeValue(text, "height"));
  const interactive = /^(?:1|true|yes|on)$/i.test(flagValue(text, "interactive"));
  return { title, height, interactive };
}

// buildEmbedDocument 组装 srcdoc 文档：CSP 必须随文档一起进去（iframe 的
// sandbox 只隔离能力，不限制联网，断网这一层靠它）；桥脚本紧跟 `<body>` 开口。
export function buildEmbedDocument(body) {
  return [
    "<!DOCTYPE html><html><head><meta charset=\"utf-8\">",
    `<meta http-equiv="Content-Security-Policy" content="${EMBED_CSP}">`,
    `<style>${EMBED_SANDBOX_CSS}</style></head><body>`,
    buildEmbedBridgeScript(),
    String(body ?? ""),
    "</body></html>"
  ].join("");
}

// buildHtmlPreviewDocument 是**文件预览**（file-preview.js）用的同一套沙箱文档，
// 与 buildEmbedDocument 只差一处：**不注入跨帧桥脚本**。理由——预览一份磁盘上的
// HTML 不该有"驱动会话"的出口（桥只服务于会话内的渲染块；宿主侧也只认
// `.html-embed-frame` 的 contentWindow，见 embed-bridge.js）。CSP、基座样式与转义
// 口径与嵌入块**共用同一份常量**，改一处两处一起变。
export function buildHtmlPreviewDocument(body) {
  return [
    "<!DOCTYPE html><html><head><meta charset=\"utf-8\">",
    `<meta http-equiv="Content-Security-Policy" content="${EMBED_CSP}">`,
    `<style>${EMBED_SANDBOX_CSS}</style></head><body>`,
    String(body ?? ""),
    "</body></html>"
  ].join("");
}

// renderSandboxFrame 把一份**不可信 HTML** 渲染成沙箱 iframe（无跨帧桥）。这是文件
// 预览只读页面的唯一入口，口径与嵌入块逐条一致：正文只进 `srcdoc`、
// `sandbox="allow-scripts"` 且**不给** `allow-same-origin`（opaque origin，脚本够不到
// 宿主）、CSP 随文档一起进去（`default-src 'none'` → 外链 CSS/JS 与 connect/img 网络
// 出口全断，只放行内联样式/脚本与 data: 图片）、不给 `allow-forms/popups/
// top-navigation`（表单提交、弹窗、跳转宿主页全部无效）。返回 HTML 字符串，调用方
// `innerHTML` 进容器（无 DOM API 依赖，可离线单测）。
export function renderSandboxFrame(body, { title = "", className = "" } = {}) {
  const document = buildHtmlPreviewDocument(body);
  const classAttr = className ? ` class="${escapeAttribute(className)}"` : "";
  return `<iframe${classAttr} sandbox="allow-scripts" referrerpolicy="no-referrer" loading="lazy" title="${escapeAttribute(title)}" srcdoc="${escapeAttribute(document)}"></iframe>`;
}

// renderHtmlEmbed 渲染一块沙箱视图。第三个参数只有一项：`key`——帧的**稳定身份**
// （`<行 key>#<围栏序号>`，由 markdown.js 按每条消息内的 seelex-html 围栏序号传入）。
// 有了它，同一帧在重渲染之间能被认回来（见 embed-buffering.js 的领养）；没有稳定 key
// 时退回**内容哈希**：同内容同身份、内容变了身份就变——身份只在"这是不是同一帧"这个
// 意义上使用，与安全判断无关（沙箱口径仍逐条不变：只进 srcdoc、只给 allow-scripts）。
//
// 三层各自的职责（换帧动画就发生在这三层之间，见 styles.css 的会话内 HTML 渲染块）：
//   · `.html-embed-body`  帧 + 加载层的**最小变化区域**：加载态与 aria-busy 挂在这里，
//     换帧只影响这一块，不外溢到卡片铭牌、源码折叠与相邻行；
//   · `.html-embed-stage` 帧的**固定高度落位台**（高度取 `--embed-height`）：交叉淡入
//     淡出时新旧两帧都在这一层里绝对定位叠放，因此换帧期间不塌陷、不抖；
//   · `.html-embed-status` 加载态（改由 embed-buffering.js 驱动）：状态文字 + 一条
//     不确定活动条；`role="status"` 让读屏能听到"正在渲染图形"，先 100ms 不显示
//     （宪法 §2.2 的首延迟），避免短任务闪一下又消失。
export function renderHtmlEmbed(body, info = "", options = {}) {
  const { title, height, interactive } = parseEmbedInfo(info);
  const document = buildEmbedDocument(body);
  const key = String(options?.key ?? "").trim() || `content#${embedContentHash(body)}`;
  const keyAttr = escapeAttribute(key);
  // 说明牌如实写出这一块有没有跨帧能力：能驱动会话的块，用户在点之前就该看得见。
  const note = interactive ? "沙箱渲染 · 不联网 · 可驱动会话" : "沙箱渲染 · 不联网";
  return `<figure class="html-embed" data-embed-key="${keyAttr}" data-embed-interactive="${interactive ? "1" : "0"}" style="--embed-height:${height}px">
  <figcaption class="html-embed-head"><span class="html-embed-mark" aria-hidden="true"></span><span class="html-embed-title">${escapeHtml(title)}</span><span class="html-embed-note">${note}</span></figcaption>
  <div class="html-embed-body" data-embed-body="1">
  <div class="html-embed-stage" data-embed-stage="1" data-embed-key="${keyAttr}">
  <iframe class="html-embed-frame" data-embed-key="${keyAttr}" sandbox="allow-scripts" referrerpolicy="no-referrer" loading="lazy" title="${escapeAttribute(title)}" srcdoc="${escapeAttribute(document)}"></iframe>
  </div>
  <p class="html-embed-status" role="status" aria-live="polite"><span class="html-embed-status-text" data-embed-loading-text></span><span class="html-embed-activity" aria-hidden="true"></span></p>
  </div>
  <details class="html-embed-source"><summary>查看源码</summary><pre><code>${escapeHtml(body)}</code></pre></details>
</figure>`;
}

// embedContentHash 给"没有稳定行 key"的场合一个**内容身份**（FNV-1a/32 → base36）。
// 只用于帧身份比较，不做安全判断，也不进任何用户可见文案。
export function embedContentHash(value) {
  const text = String(value ?? "");
  let hash = 0x811c9dc5;
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(36);
}

function attributeValue(text, name) {
  const pattern = new RegExp(`(?:^|\\s)${name}\\s*=\\s*(?:"([^"]*)"|'([^']*)'|([^\\s"']+))`);
  const match = text.match(pattern);
  if (!match) return "";
  return (match[1] ?? match[2] ?? match[3] ?? "").trim();
}

// flagValue 读布尔型围栏参数：`interactive=1` 与裸写 `interactive` 都算给了值
// （围栏信息串是人手写的，多一个 `=1` 没有意义，不该因此静默失效）。
function flagValue(text, name) {
  const explicit = attributeValue(text, name);
  if (explicit) return explicit;
  return new RegExp(`(?:^|\\s)${name}(?=\\s|$)`).test(text) ? "1" : "";
}

function clampHeight(raw) {
  const value = Number.parseInt(raw, 10);
  if (!Number.isFinite(value)) return DEFAULT_HEIGHT;
  return Math.min(Math.max(value, MIN_HEIGHT), MAX_HEIGHT);
}

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

// escapeAttribute 额外处理反引号：srcdoc 会被塞进属性里，反引号在部分解析
// 路径下仍可能提前闭合属性。
function escapeAttribute(value) {
  return escapeHtml(value).replaceAll("`", "&#096;");
}
