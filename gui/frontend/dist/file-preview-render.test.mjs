// 只读内容渲染入口（file-preview.renderReadOnlyContent）的契约测试。
//
// 它被两处共用：文件详情抽屉（renderLoaded）与「提交记录 → 文件内容」视图。共用是
// 这条测试存在的理由——同一个二进制判定、同一条截断横幅、同一套"没内容可看"的文案
// 必须只写一份。这里用最小 DOM 替身驱动真实渲染分支（不装 hljs/marked/DOMPurify：
// 那几样缺席时渲染降级为纯文本，正是文本类要钉的路径）。

import assert from "node:assert/strict";
import test from "node:test";

import { renderReadOnlyContent } from "./file-preview.js";
import { buildHtmlPreviewDocument, renderSandboxFrame } from "./html-embed.js";

class FakeClassList {
  constructor() {
    this.names = new Set();
  }
  add(...list) {
    for (const name of list) this.names.add(name);
  }
  remove(...list) {
    for (const name of list) this.names.delete(name);
  }
  toggle(name, force) {
    const on = force === undefined ? !this.names.has(name) : Boolean(force);
    if (on) this.names.add(name);
    else this.names.delete(name);
    return on;
  }
  contains(name) {
    return this.names.has(name);
  }
  get value() {
    return [...this.names].join(" ");
  }
}

class FakeElement {
  constructor(tag = "div") {
    this.tagName = String(tag).toUpperCase();
    this.classList = new FakeClassList();
    this.children = [];
    this.textContent = "";
    this._innerHTML = "";
  }
  set className(value) {
    this.classList = new FakeClassList();
    for (const name of String(value).split(/\s+/).filter(Boolean)) this.classList.add(name);
  }
  get className() {
    return this.classList.value;
  }
  get innerHTML() {
    return this._innerHTML;
  }
  set innerHTML(value) {
    // 与浏览器一致：写 innerHTML 丢掉既有子节点（渲染函数依赖这个语义）。
    this._innerHTML = String(value);
    this.children = [];
  }
  appendChild(child) {
    this.children.push(child);
    return child;
  }
  prepend(child) {
    this.children.unshift(child);
    return child;
  }
  querySelectorAll() {
    return [];
  }
  // 树里所有文本（断言"渲染出来的到底是什么字"时用）。
  text() {
    const own = this.textContent || "";
    return own + this.children.map(child => child.text()).join("");
  }
  classes() {
    return [this.classList.value, ...this.children.map(child => child.classes())].join(" ");
  }
}

function installFakeGlobals() {
  globalThis.document = { createElement: tag => new FakeElement(tag) };
  // hljs / marked / DOMPurify 一律缺席：走纯文本降级路径。
  globalThis.window = {};
}

const payload = (text, extra = {}) => ({
  size: text.length,
  truncated: false,
  text_like: true,
  base64: Buffer.from(text, "utf8").toString("base64"),
  ...extra
});

test("renderReadOnlyContent 按路径分派并把正文写进容器", async () => {
  installFakeGlobals();
  const panel = new FakeElement("div");
  const kind = await renderReadOnlyContent(panel, payload("package main\n"), { path: "src/main.go" });
  assert.equal(kind, "code");
  assert.ok(panel.classes().includes("file-preview-content"), "外壳由渲染器建");
  assert.ok(panel.classes().includes("file-preview-code"), "代码走高亮分支的容器");
  assert.ok(panel.text().includes("package main"), "正文进了 <pre>");
});

test("renderReadOnlyContent 空字节与二进制都不算失败，渲染成提示", async () => {
  installFakeGlobals();
  const empty = new FakeElement("div");
  await renderReadOnlyContent(empty, { base64: "", size: 0, truncated: false, text_like: true }, { path: "empty.txt" });
  assert.ok(empty.innerHTML.includes("文件内容为空或不可读"));
  assert.ok(empty.classList.contains("muted"));

  const binary = new FakeElement("div");
  await renderReadOnlyContent(binary, payload("\u0000\u0001", { text_like: false }), { path: "bin.dat" });
  assert.ok(binary.innerHTML.includes("二进制文件"));
});

test("renderReadOnlyContent 截断文本带截断提示，超上限的分页类型直接放弃", async () => {
  installFakeGlobals();
  const text = new FakeElement("div");
  await renderReadOnlyContent(text, payload("line\n", { truncated: true, size: 1 << 26 }), { path: "notes.txt" });
  assert.ok(text.classes().includes("file-preview-banner"), "文本截断仍可看，但要明说");
  assert.ok(text.text().includes("内容已截断"));

  // PDF 这类要整份才能排版的类型：截断即放弃（与文件详情抽屉同一条口径）。
  const pdf = new FakeElement("div");
  await renderReadOnlyContent(pdf, {
    base64: Buffer.from("%PDF-1.7", "utf8").toString("base64"),
    size: 1 << 26,
    truncated: true,
    text_like: false
  }, { path: "manual.pdf" });
  assert.ok(pdf.innerHTML.includes("暂不支持预览完整内容"));
});

test("renderReadOnlyContent 认调用方给好的 kind（不重复分派）", async () => {
  installFakeGlobals();
  const panel = new FakeElement("div");
  // 路径看着像二进制扩展名，但调用方已经判定为文本：以调用方为准。
  const kind = await renderReadOnlyContent(panel, payload("hello\n"), { path: "blob.bin", kind: "text" });
  assert.equal(kind, "text");
  assert.ok(panel.text().includes("hello"));
});

// F-5 D3：markdown 里的 ```mermaid 围栏换图表容器；这里模拟"marked/purify 在位、
// 但 mermaid 库缺席"的中间态——容器必须保留源码代码块（不白屏），且不抛错。
test("renderReadOnlyContent 把 mermaid 围栏换成图表容器，mermaid 缺席时降级为代码块", async () => {
  globalThis.document = { createElement: tag => new FakeElement(tag) };
  globalThis.window = {
    marked: { parse: text => `<pre><code class="language-mermaid">${text.trim()}</code></pre>` },
    DOMPurify: { sanitize: html => html }
  };
  const panel = new FakeElement("div");
  const kind = await renderReadOnlyContent(panel, payload("```mermaid\ngraph TD;\nA-->B;\n```\n"), { path: "flow.md" });
  assert.equal(kind, "markdown");

  // 外壳 + 容器（渲染面在 panel 的第一个子节点 .file-preview-content 里）。
  const content = panel.children[0];
  assert.ok(content.className.includes("file-preview-content"), "外壳由渲染器建");
  assert.ok(content.className.includes("is-markdown"), "走 markdown 分支");
  const html = content.innerHTML;
  assert.ok(html.includes("data-mermaid-diagram"), "mermaid 围栏要换成图表容器（有渲染分支）");
  assert.ok(html.includes('class="language-mermaid"'), "mermaid 缺席时降级为源码代码块，不白屏");
  assert.ok(html.includes("graph TD;"), "源码仍可读");
  // 画布初始为空且 hidden：没有被净化过的 SVG 也不会被塞进去。
  assert.ok(html.includes("data-mermaid-canvas hidden"), "画布初始必须 hidden");
});

// ── F-7 HTML 只读预览 ────────────────────────────────────────────────
// 内容详情在**只读态**把 .html/.htm 渲染成页面（沙箱 iframe），点「编辑」才切到源码。
// 这一层是「文件详情抽屉」与「提交内文件内容」两处共用的同一分派，因此本文件就是它的
// 契约测试。

test("F-7：.html 只读态 kind=html 且渲染出沙箱 iframe（不是代码块）", async () => {
  installFakeGlobals();
  const panel = new FakeElement("div");
  const source = "<!DOCTYPE html>\n<html><body><h1>你好</h1></body></html>\n";
  const kind = await renderReadOnlyContent(panel, payload(source), { path: "site/index.html" });
  assert.equal(kind, "html");
  const content = panel.children[0];
  assert.ok(content.className.includes("file-preview-content"), "外壳由渲染器建");
  assert.ok(content.className.includes("is-html"), "走 html 分支");
  assert.ok(content.innerHTML.includes("<iframe"), "页面在 iframe 里");
  assert.ok(content.innerHTML.includes('sandbox="allow-scripts"'), "沙箱口径");
  assert.ok(!content.innerHTML.includes("allow-same-origin"), "绝不与 allow-scripts 同给");
  assert.ok(!content.innerHTML.includes("file-preview-code"), "不再是代码块");
  assert.ok(content.innerHTML.includes("file-preview-html-note"), "写明「结构 + 内联样式」边界");
});

test("F-7：.htm 与 .html 同分支（共用一条只读分派）", async () => {
  installFakeGlobals();
  const panel = new FakeElement("div");
  const kind = await renderReadOnlyContent(panel, payload("<p>hi</p>"), { path: "old/legacy.htm" });
  assert.equal(kind, "html");
  assert.ok(panel.children[0].innerHTML.includes("<iframe"));
});

// 离线口径：CSP 随文档一起进 srcdoc，断掉外链 CSS/JS 与 connect/img 的网络出口；
// iframe 只有 srcdoc、没有 src，且**不带跨帧桥**（磁盘上的 HTML 不该能驱动会话）。
test("F-7：离线渲染不发外部请求（CSP 断网 + 无 src + 无跨帧桥）", () => {
  const hostile = [
    '<link rel="stylesheet" href="https://evil.example/x.css">',
    '<img src="http://evil.example/x.png">',
    '<form action="https://evil.example/"><input name="a"></form>',
    '<script src="https://evil.example/x.js"><\/script>'
  ].join("");
  const document = buildHtmlPreviewDocument(hostile);
  assert.match(document, /default-src 'none'/);
  assert.match(document, /connect-src 'none'/);
  assert.match(document, /img-src data: blob:/);
  assert.match(document, /style-src 'unsafe-inline'/);
  assert.match(document, /form-action 'none'/);
  assert.doesNotMatch(document, /seelex-embed/, "预览不带跨帧桥（无驱动会话的出口）");

  const frame = renderSandboxFrame(hostile, { title: "site/index.html 预览", className: "file-preview-html-frame" });
  assert.equal(frame.match(/<iframe/g).length, 1);
  // 只有 srcdoc，没有 src：iframe 自己不发起任何外部请求。
  assert.doesNotMatch(frame.split("srcdoc=")[0], /(^|\s)src=/);
  assert.match(frame, /sandbox="allow-scripts"/);
  assert.doesNotMatch(frame, /allow-same-origin|allow-forms|allow-popups|allow-top-navigation/);
  // 正文（含外链）只作为 srcdoc 属性值存在，不进宿主 DOM；属性解码后必须还原沙箱文档。
  const srcdoc = frame.match(/ srcdoc="([^"]*)"/)[1];
  const decoded = srcdoc
    .replaceAll("&quot;", '"')
    .replaceAll("&#039;", "'")
    .replaceAll("&#096;", "`")
    .replaceAll("&lt;", "<")
    .replaceAll("&gt;", ">")
    .replaceAll("&amp;", "&");
  assert.equal(decoded, document);
  assert.doesNotMatch(frame.split("srcdoc=")[0], /<link|<img|<form|<script/);
});
