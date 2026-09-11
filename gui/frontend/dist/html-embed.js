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
// 触发方式：markdown 围栏代码块，语言标记为 `seelex-html`（别名
// `html-preview`）；普通 ```html 仍然是源码块，不受影响。
//
// 纯函数（isHtmlEmbedLanguage/parseEmbedInfo/buildEmbedDocument/renderHtmlEmbed）
// 可离线单测，见 html-embed.test.mjs。

const EMBED_LANGUAGES = new Set(["seelex-html", "html-preview"]);
const DEFAULT_TITLE = "图形视图";
const DEFAULT_HEIGHT = 260;
const MIN_HEIGHT = 120;
const MAX_HEIGHT = 640;

// EMBED_BASE_CSS 只做最基本的可读性兜底：用户自己的样式优先，避免"预置主题"
// 把图表的配色盖掉。
const EMBED_BASE_CSS = [
  ":root{color-scheme:dark}",
  "html,body{margin:0;padding:10px;background:transparent;color:#e9e4d8;",
  "font:13px/1.5 'Segoe UI Variable','Microsoft YaHei',system-ui,sans-serif}",
  "body>*:first-child{margin-top:0}body>*:last-child{margin-bottom:0}",
  "svg{max-width:100%;height:auto}",
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

// parseEmbedInfo 解析围栏信息串上的可选参数：`title="..."` 与 `height=360`。
// 高度钳制在 [MIN_HEIGHT, MAX_HEIGHT]，避免一条消息把页面撑爆或压成一条缝。
export function parseEmbedInfo(info = "") {
  const text = String(info ?? "");
  const title = attributeValue(text, "title") || DEFAULT_TITLE;
  const height = clampHeight(attributeValue(text, "height"));
  return { title, height };
}

// buildEmbedDocument 组装 srcdoc 文档：CSP 必须随文档一起进去（iframe 的
// sandbox 只隔离能力，不限制联网，断网这一层靠它）。
export function buildEmbedDocument(body) {
  return [
    "<!DOCTYPE html><html><head><meta charset=\"utf-8\">",
    `<meta http-equiv="Content-Security-Policy" content="${EMBED_CSP}">`,
    `<style>${EMBED_BASE_CSS}</style></head><body>`,
    String(body ?? ""),
    "</body></html>"
  ].join("");
}

export function renderHtmlEmbed(body, info = "") {
  const { title, height } = parseEmbedInfo(info);
  const document = buildEmbedDocument(body);
  return `<figure class="html-embed" style="--embed-height:${height}px">
  <figcaption class="html-embed-head"><span class="html-embed-mark" aria-hidden="true"></span><span class="html-embed-title">${escapeHtml(title)}</span><span class="html-embed-note">沙箱渲染 · 不联网</span></figcaption>
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
