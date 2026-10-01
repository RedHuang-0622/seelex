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
// 触发方式：markdown 围栏代码块，语言标记为 `seelex-html`（别名
// `html-preview`）；普通 ```html 仍然是源码块，不受影响。
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
//     （剪贴板/输入框/源码折叠）——但同样只走这一张白名单，没有"顺手支持一下"。
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
    "window.seelex = { emit: emit, actions: ACTIONS };",
    "})();",
    "</script>"
  ].join("\n");
}

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
  ":root{color-scheme:dark}",
  "html,body{margin:0;padding:10px;background:transparent;color:#e9e4d8;",
  "font:13px/1.5 'Segoe UI Variable','Microsoft YaHei',system-ui,sans-serif}",
  "body>*:first-child{margin-top:0}body>*:last-child{margin-bottom:0}",
  "svg{max-width:100%;height:auto}",
  ":where(svg text){pointer-events:none}",
  ":where(svg rect,svg circle,svg ellipse,svg path,svg polygon,svg polyline,svg line,svg image,svg g,svg use){transform-box:fill-box}",
  "table{border-collapse:collapse}th,td{padding:4px 8px;border:1px solid #3c4a56}"
].join("");

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
    `<style>${EMBED_BASE_CSS}</style></head><body>`,
    buildEmbedBridgeScript(),
    String(body ?? ""),
    "</body></html>"
  ].join("");
}

export function renderHtmlEmbed(body, info = "") {
  const { title, height, interactive } = parseEmbedInfo(info);
  const document = buildEmbedDocument(body);
  // 说明牌如实写出这一块有没有跨帧能力：能驱动会话的块，用户在点之前就该看得见。
  const note = interactive ? "沙箱渲染 · 不联网 · 可驱动会话" : "沙箱渲染 · 不联网";
  return `<figure class="html-embed" data-embed-interactive="${interactive ? "1" : "0"}" style="--embed-height:${height}px">
  <figcaption class="html-embed-head"><span class="html-embed-mark" aria-hidden="true"></span><span class="html-embed-title">${escapeHtml(title)}</span><span class="html-embed-note">${note}</span></figcaption>
  <iframe class="html-embed-frame" sandbox="allow-scripts" referrerpolicy="no-referrer" loading="lazy" title="${escapeAttribute(title)}" srcdoc="${escapeAttribute(document)}"></iframe>
  <details class="html-embed-source"><summary>查看源码</summary><pre><code>${escapeHtml(body)}</code></pre></details>
</figure>`;
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
