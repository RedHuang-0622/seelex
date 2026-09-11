import test from "node:test";
import assert from "node:assert/strict";
import {
  buildEmbedDocument,
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
  assert.deepEqual(parseEmbedInfo(""), { title: "图形视图", height: 260 });
  assert.deepEqual(parseEmbedInfo('title="吞吐趋势" height=360'), { title: "吞吐趋势", height: 360 });
  assert.deepEqual(parseEmbedInfo("height='420'"), { title: "图形视图", height: 420 });
  // 高度钳制：不给一条消息把页面撑爆或压成一条缝的机会。
  assert.equal(parseEmbedInfo("height=10").height, 120);
  assert.equal(parseEmbedInfo("height=99999").height, 640);
  assert.equal(parseEmbedInfo("height=abc").height, 260);
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
