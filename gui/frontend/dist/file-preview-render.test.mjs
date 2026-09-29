// 只读内容渲染入口（file-preview.renderReadOnlyContent）的契约测试。
//
// 它被两处共用：文件详情抽屉（renderLoaded）与「提交记录 → 文件内容」视图。共用是
// 这条测试存在的理由——同一个二进制判定、同一条截断横幅、同一套"没内容可看"的文案
// 必须只写一份。这里用最小 DOM 替身驱动真实渲染分支（不装 hljs/marked/DOMPurify：
// 那几样缺席时渲染降级为纯文本，正是文本类要钉的路径）。

import assert from "node:assert/strict";
import test from "node:test";

import { renderReadOnlyContent } from "./file-preview.js";

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
