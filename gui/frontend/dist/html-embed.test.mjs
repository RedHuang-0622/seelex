import test from "node:test";
import assert from "node:assert/strict";
import {
  buildEmbedBridgeScript,
  buildEmbedDocument,
  embedContentHash,
  isHtmlEmbedLanguage,
  parseEmbedInfo,
  renderHtmlEmbed
} from "./html-embed.js";

test("html embed: only explicitly marked fences are rendered", () => {
  assert.equal(isHtmlEmbedLanguage("seelex-html"), true);
  assert.equal(isHtmlEmbedLanguage("SEELEX-HTML"), true);
  assert.equal(isHtmlEmbedLanguage("html-preview"), true);
  // 普通 html 围栏仍是源码块：既有会话里的代码片段不能突然被执行。
  assert.equal(isHtmlEmbedLanguage("html"), false);
  assert.equal(isHtmlEmbedLanguage(""), false);
  assert.equal(isHtmlEmbedLanguage(undefined), false);
});

test("html embed: info string carries title and clamped height", () => {
  assert.deepEqual(parseEmbedInfo(""), { title: "图形视图", height: 260, interactive: false });
  assert.deepEqual(parseEmbedInfo('title="吞吐趋势" height=360'), { title: "吞吐趋势", height: 360, interactive: false });
  assert.deepEqual(parseEmbedInfo("height='420'"), { title: "图形视图", height: 420, interactive: false });
  // 高度钳制：不给一条消息把页面撑爆或压成一条缝的机会。
  assert.equal(parseEmbedInfo("height=10").height, 120);
  assert.equal(parseEmbedInfo("height=99999").height, 640);
  assert.equal(parseEmbedInfo("height=abc").height, 260);
});

test("html embed: driving the conversation is opt-in per block", () => {
  assert.equal(parseEmbedInfo("").interactive, false);
  assert.equal(parseEmbedInfo('title="可点架构图" interactive=1').interactive, true);
  // 裸标记也算给了值：围栏信息串是人手写的，不该因为少打一个 `=1` 静默失效。
  assert.equal(parseEmbedInfo("interactive").interactive, true);
  assert.equal(parseEmbedInfo("interactive=true").interactive, true);
  assert.equal(parseEmbedInfo("interactive=0").interactive, false);
  assert.equal(parseEmbedInfo("height=300").interactive, false);

  const plain = renderHtmlEmbed("<svg></svg>");
  assert.match(plain, /data-embed-interactive="0"/);
  assert.match(plain, /沙箱渲染 · 不联网</);
  assert.doesNotMatch(plain, /可驱动会话/);

  const live = renderHtmlEmbed("<svg></svg>", "interactive=1");
  assert.match(live, /data-embed-interactive="1"/);
  assert.match(live, /可驱动会话/);
});

test("html embed: svg interaction defaults ride along in the base css", () => {
  const document = buildEmbedDocument("<svg><text>x</text></svg>");
  // 标签默认不当命中面（压在图形上的 <text> 会吃掉 click）。
  assert.match(document, /:where\(svg text\)\{pointer-events:none\}/);
  // 就地旋转要先有确定的参考框。
  assert.match(document, /transform-box:fill-box/);
});

test("html embed: the action bridge is injected inside the sandbox document", () => {
  const bridge = buildEmbedBridgeScript();
  const document = buildEmbedDocument("<svg></svg>");
  assert.match(document, /seelex-embed/);
  assert.equal((bridge.match(/<\/script>/g) || []).length, 1);
  // 桥排在正文之前：块内脚本一执行就该拿得到 seelex。
  assert.ok(document.indexOf(bridge) < document.indexOf("<svg></svg>"));
  // 块本身的桥只以转义文本存在（渲染出来的 HTML 里没有可执行的 script 标签）。
  assert.doesNotMatch(renderHtmlEmbed("<svg></svg>"), /<script>/);
});

test("html embed: document is locked down by csp", () => {
  const document = buildEmbedDocument("<svg></svg>");
  assert.match(document, /default-src 'none'/);
  assert.match(document, /connect-src 'none'/);
  assert.match(document, /img-src data: blob:/);
  assert.match(document, /form-action 'none'/);
  assert.match(document, /<svg><\/svg>/);
});

test("html embed: iframe is sandboxed without same-origin", () => {
  const html = renderHtmlEmbed("<svg><circle r='4'/></svg>", 'title="趋势图"');
  assert.match(html, /<iframe[^>]*sandbox="allow-scripts"/);
  // allow-scripts 与 allow-same-origin 同时出现等于没有沙箱：必须永不出现。
  assert.doesNotMatch(html, /allow-same-origin/);
  assert.doesNotMatch(html, /allow-forms|allow-popups|allow-top-navigation/);
  assert.match(html, /referrerpolicy="no-referrer"/);
  assert.match(html, /title="趋势图"/);
  assert.match(html, /--embed-height:260px/);
});

test("html embed: srcdoc round-trips and cannot break out of the attribute", () => {
  const body = `<div onclick="x()">a "quoted" & <b>bold</b></div>`;
  const html = renderHtmlEmbed(body);
  const match = html.match(/ srcdoc="([^"]*)"/);
  assert.ok(match, "srcdoc 属性必须存在");
  const decoded = match[1]
    .replaceAll("&quot;", '"')
    .replaceAll("&#039;", "'")
    .replaceAll("&lt;", "<")
    .replaceAll("&gt;", ">")
    .replaceAll("&amp;", "&");
  assert.equal(decoded, buildEmbedDocument(body));
  // 属性之外不能出现未转义的正文标签。
  assert.doesNotMatch(html.split("srcdoc=")[0], /<div onclick/);
});

test("html embed: hostile body stays inside the sandbox", () => {
  const hostile = `</iframe><img src=x onerror="alert(1)"><script>parent.postMessage('x','*')<\/script>`;
  const html = renderHtmlEmbed(hostile);
  // 正文里的标签全部转义；渲染出来的 iframe 只有我们自己那一个结束标签。
  assert.match(html, /&lt;\/iframe&gt;/);
  assert.match(html, /&lt;script&gt;/);
  assert.doesNotMatch(html, /<script>/);
  assert.equal(html.match(/<\/iframe>/g).length, 1);
  // 源码折叠块里同样是转义文本（用户可自行核对渲染内容）。
  assert.match(html, /class="html-embed-source"/);
  assert.match(html, /查看源码/);
});

// ── 帧身份与落位台（渲染缓冲的抓手，见 embed-buffering.js）──────────────
// 闪屏的根因是 reconcile 整行重建时 iframe 跟着重建。要让帧在重渲染之间被认回来，
// 标记里必须有两个稳定抓手：**帧身份**（data-embed-key）与**落位台**
// （.html-embed-stage：固定 --embed-height，换帧期间不塌陷）。
test("html embed: every frame carries a stable identity and a fixed-height stage", () => {
  const keyed = renderHtmlEmbed("<svg></svg>", "height=300", { key: "message:m1#0" });
  assert.match(keyed, /data-embed-key="message:m1#0"/);
  // 图与落位台、帧三处同 key：领养时按 key 找落位台、按 key 搬帧。
  assert.equal(keyed.match(/data-embed-key="message:m1#0"/g).length, 3);
  assert.match(keyed, /<div class="html-embed-stage" data-embed-stage="1" data-embed-key="message:m1#0">/);
  // 高度写在图（--embed-height）上、落位台被它撑住：帧撤走也不塌陷。
  assert.match(keyed, /style="--embed-height:300px"/);
  // 安全口径一字未变：仍然只有 allow-scripts，仍然没有 same-origin / forms / popups。
  assert.match(keyed, /sandbox="allow-scripts"/);
  assert.doesNotMatch(keyed, /allow-same-origin|allow-forms|allow-popups|allow-top-navigation/);
  assert.match(keyed, /referrerpolicy="no-referrer"/);
  // CSP 随 srcdoc 一起进去（属性里是转义态）：断网这一层还在。
  assert.match(keyed, /Content-Security-Policy/);
  assert.match(keyed, /default-src /);
});

test("html embed: without a row key the identity falls back to the content hash", () => {
  const first = renderHtmlEmbed("<svg><circle r=\"1\"/></svg>");
  const same = renderHtmlEmbed("<svg><circle r=\"1\"/></svg>");
  const changed = renderHtmlEmbed("<svg><circle r=\"2\"/></svg>");
  const keyOf = html => html.match(/data-embed-key="([^"]+)"/)[1];
  assert.match(keyOf(first), /^content#[a-z0-9]+$/);
  // 同内容 = 同身份（重渲染能认回来），内容变 = 身份变（必须重新挂）。
  assert.equal(keyOf(first), keyOf(same));
  assert.notEqual(keyOf(first), keyOf(changed));
  assert.equal(embedContentHash("<svg/>"), embedContentHash("<svg/>"));
  // 身份只用于比较，不进 srcdoc：文档内容与没有身份时逐字相同。
  assert.equal(buildEmbedDocument("<svg/>"), buildEmbedDocument("<svg/>"));
});

// ── 主题 token（皮肤 × 明暗）────────────────────────────────────────────
// 2026-10-29：块内颜色不再写死。颜色只有一处供源（styles.css / themes/*.css），
// 沙箱里是一组 `--sx-*` 缺省 + 宿主推进来的真值（见 embed-theme.js）。这里钉住三件事：
// 明暗两套缺省都在、语义色不由品牌色派生、旧的硬编码色值已清除。
test("html embed: sandbox colors come from --sx-* tokens, not hardcoded values", () => {
  const document = buildEmbedDocument("<svg></svg>");
  // 明暗两套：缺省深色 + prefers-color-scheme 浅色兜底（文件预览只有一个读者）
  assert.match(document, /:root\{color-scheme:dark;--sx-bg:/);
  assert.match(document, /@media \(prefers-color-scheme: light\)\{:root\{color-scheme:light;--sx-bg:/);
  // 正文颜色走 token：以前的 #e9e4d8 / #3c4a56 不能留在这里
  assert.doesNotMatch(document, /#e9e4d8/);
  assert.doesNotMatch(document, /#3c4a56/);
  assert.match(document, /color:var\(--sx-text\)/);
});

test("html embed: warning/info colors never follow the skin accent", () => {
  const document = buildEmbedDocument("");
  // 语义色是字面色值（按明暗各一套），不是 var(--sx-accent) 派生
  for (const token of ["--sx-warn", "--sx-danger", "--sx-ok", "--sx-info"]) {
    const match = document.match(new RegExp(`${token}:([^;}]+)`));
    assert.ok(match, `${token} 必须在沙箱缺省里`);
    assert.doesNotMatch(match[1], /var\(--sx-accent|var\(--sx-surface/, `${token} 不得由主题色派生`);
  }
  assert.match(document, /--sx-accent:#e9e9ec/, "深色缺省的品牌色仍在（可被宿主覆写）");
});

// ── 内置组件层 ─────────────────────────────────────────────────────────
// 块里不必从头画一套皮：语义元素按应用口径出形，且全部装在 :where() 里（特异性 0），
// 作者自己的样式永远赢。这里钉住"既有组件在"与"不夺权"两条。
test("html embed: built-in components are available to the block", () => {
  const document = buildEmbedDocument("<article><button>a</button></article>");
  for (const selector of [":where(article", ":where(button", ":where(table", ":where(progress", ":where(details", ":where(code", ":where(mark"]) {
    assert.ok(document.includes(selector), `内置组件 ${selector} 缺失`);
  }
  // 命名别名 + 语义芯片
  for (const name of [".sx-card", ".sx-btn", ".sx-chip", ".sx-row", ".sx-stack", ".sx-meter"]) {
    assert.ok(document.includes(name), `内置组件别名 ${name} 缺失`);
  }
  assert.match(document, /:where\(article,\.sx-card\)\{/);
  assert.match(document, /\.sx-chip\.is-warn\)/);
  // 预置组件只兜底：裸元素选择器（不带 :where）不得出现在组件层里
  assert.doesNotMatch(document, /(^|[};\s])(article|button|table|progress|details)\{/);
});

// ── 加载层与淡入淡出的抓手 ──────────────────────────────────────────────
// 帧的加载态与换帧动画由 embed-buffering.js 驱动（见 embed-buffering.test.mjs），
// 这里只钉住标记齐备：加载层挂在 body 上（最小变化区域）、有 role=status、帧落位台不变。
test("html embed: the block carries a status region for the loader", () => {
  const html = renderHtmlEmbed("<svg></svg>", 'title="趋势图"');
  assert.match(html, /<div class="html-embed-body" data-embed-body="1">/);
  assert.match(html, /<p class="html-embed-status" role="status" aria-live="polite">/);
  assert.match(html, /data-embed-loading-text/);
  assert.match(html, /class="html-embed-activity"/);
  // 加载层与帧都在 body 里；帧仍只住 stage（换帧动画只发生在 stage 内）
  assert.match(html, /<div class="html-embed-stage" data-embed-stage="1"/);
  // 初始不带加载态：静止的块不该挂 aria-busy
  assert.doesNotMatch(html, /aria-busy="true"/);
  // 安全口径不变
  assert.match(html, /sandbox="allow-scripts"/);
  assert.doesNotMatch(html, /allow-same-origin/);
});
