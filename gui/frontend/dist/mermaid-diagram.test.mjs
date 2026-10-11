// mermaid 图表水合的契约测试（F-5 D2/D3/D4）。
//
// 钉三件事：
//   ① 库缺席 / 渲染抛错 / 净化器缺席 ⇒ 容器标 `data-mermaid-state="failed"`，**源码
//      代码块保持可见**（不白屏），且 hydrateMermaid 自己不抛错（调用方可以是流式路径）；
//   ② 库 + 净化器都在位 ⇒ 画布写入净化后的 SVG、源码块收起、标 ready；
//   ③ SVG 必须经 DOMPurify 才放行——净化器缺席时宁可降级，也不放未净化内容。
import assert from "node:assert/strict";
import test from "node:test";

import { hydrateMermaid, MERMAID_VENDOR_SRC } from "./mermaid-diagram.js";

function fakeDiagram(source, { state = "" } = {}) {
  const code = { textContent: source };
  const sourcePre = { hidden: false };
  const canvas = { hidden: true, innerHTML: "" };
  const block = {
    dataset: state ? { mermaidState: state } : {},
    querySelector(selector) {
      if (selector === ".mermaid-source code") return code;
      if (selector === "pre.mermaid-source") return sourcePre;
      if (selector === "[data-mermaid-canvas]") return canvas;
      return null;
    }
  };
  return { block, canvas, sourcePre };
}

const fakeRoot = blocks => ({ querySelectorAll: () => blocks });

function resetGlobals() {
  delete globalThis.mermaid;
  delete globalThis.DOMPurify;
  delete globalThis.document;
  delete globalThis.window;
}

test("mermaid 库缺席：容器降级为源码代码块（failed 状态，不白屏、不抛错）", async () => {
  resetGlobals();
  const { block, canvas, sourcePre } = fakeDiagram("graph TD;\nA-->B;");

  const rendered = await hydrateMermaid(fakeRoot([block]));

  assert.equal(rendered, 0);
  assert.equal(block.dataset.mermaidState, "failed");
  assert.equal(canvas.innerHTML, "", "没渲染就不许往画布写东西");
  assert.equal(canvas.hidden, true, "画布保持 hidden");
  assert.equal(sourcePre.hidden, false, "源码代码块必须仍然可见（降级面）");
});

test("mermaid 在位且 SVG 过净化：写入画布、收起源码块、标 ready", async () => {
  resetGlobals();
  const svg = '<svg xmlns="http://www.w3.org/2000/svg"><text>ok</text></svg>';
  let initializedWith = null;
  globalThis.mermaid = {
    initialize(options) { initializedWith = options; },
    async render(id, source) {
      assert.match(String(id), /^seelex-mermaid-\d+$/);
      assert.equal(source, "graph TD;\nA-->B;");
      return { svg };
    }
  };
  let sanitized = null;
  globalThis.DOMPurify = {
    sanitize(value, options) { sanitized = { value, options }; return value; }
  };
  const { block, canvas, sourcePre } = fakeDiagram("graph TD;\nA-->B;");

  const rendered = await hydrateMermaid(fakeRoot([block]));

  assert.equal(rendered, 1);
  assert.equal(block.dataset.mermaidState, "ready");
  assert.equal(canvas.innerHTML, svg);
  assert.equal(canvas.hidden, false);
  assert.equal(sourcePre.hidden, true, "渲染成功才收起源码块");
  // D4：SVG 走 DOMPurify 的白名单 profile（svg + svgFilters + html——html 是为
  // mermaid htmlLabels 的 <foreignObject> 标签），不是关掉净化。
  assert.equal(sanitized.value, svg);
  assert.deepEqual(sanitized.options.USE_PROFILES, { svg: true, svgFilters: true, html: true });
  assert.ok(sanitized.options.FORBID_TAGS.includes("script"));
  // 初始化：不开 startOnLoad（我们手动 render），securityLevel strict。
  assert.equal(initializedWith.startOnLoad, false);
  assert.equal(initializedWith.securityLevel, "strict");
  assert.equal(MERMAID_VENDOR_SRC.endsWith("/vendor/mermaid/mermaid.min.js"), true);
});

test("渲染抛错：降级为源码代码块，画布留空", async () => {
  resetGlobals();
  globalThis.mermaid = {
    initialize() {},
    async render() { throw new Error("Parse error on line 1"); }
  };
  globalThis.DOMPurify = { sanitize: value => value };
  const { block, canvas, sourcePre } = fakeDiagram("not a diagram");

  const rendered = await hydrateMermaid(fakeRoot([block]));

  assert.equal(rendered, 0);
  assert.equal(block.dataset.mermaidState, "failed");
  assert.equal(canvas.innerHTML, "");
  assert.equal(canvas.hidden, true);
  assert.equal(sourcePre.hidden, false);
});

test("净化器缺席：即使渲染成功也不放行 SVG（宁可降级）", async () => {
  resetGlobals();
  globalThis.mermaid = {
    initialize() {},
    async render() { return { svg: '<svg><script>alert(1)</script></svg>' }; }
  };
  const { block, canvas, sourcePre } = fakeDiagram("graph TD;\nA-->B;");

  const rendered = await hydrateMermaid(fakeRoot([block]));

  assert.equal(rendered, 0);
  assert.equal(block.dataset.mermaidState, "failed");
  assert.equal(canvas.innerHTML, "", "未净化的 SVG 一律不写进画布");
  assert.equal(canvas.hidden, true);
  assert.equal(sourcePre.hidden, false);
});

test("空源码 / 已处理过的容器：不注入 vendor、不重复渲染", async () => {
  resetGlobals();
  let renderCalls = 0;
  globalThis.mermaid = {
    initialize() {},
    async render() { renderCalls += 1; return { svg: "<svg/>" }; }
  };
  globalThis.DOMPurify = { sanitize: value => value };

  const blank = fakeDiagram("   \n");
  const done = fakeDiagram("graph TD;", { state: "ready" });
  const rendered = await hydrateMermaid(fakeRoot([blank.block, done.block]));

  assert.equal(rendered, 0);
  assert.equal(blank.block.dataset.mermaidState, "failed");
  assert.equal(renderCalls, 0, "空源码与已处理容器都不该触发渲染");
});

test("没有可水合的 root 时安全返回（调用方不必判空）", async () => {
  resetGlobals();
  assert.equal(await hydrateMermaid(null), 0);
  assert.equal(await hydrateMermaid({}), 0);
});
