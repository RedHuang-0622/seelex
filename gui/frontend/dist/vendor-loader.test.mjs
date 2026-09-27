// vendor 按需装载器：同一 src 只注入一次、并发共用 in-flight、失败可重试、
// 已就绪时不注入（与早期同步加载写法兼容）。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { ensureVendorScript, vendorScriptsInFlight } from "./vendor-loader.js";

function fakeScript() {
  const listeners = new Map();
  return {
    src: "",
    async: false,
    listeners,
    addEventListener(type, fn) { listeners.set(type, fn); },
    fire(type) { const fn = listeners.get(type); if (fn) fn(); }
  };
}

function fakeDocument() {
  const appended = [];
  const head = { appendChild: (node) => { appended.push(node); return node; } };
  return { appended, head, createElement: () => fakeScript() };
}

test("并发只注入一次，已就绪后不再注入，失败后可重试", async () => {
  const doc = fakeDocument();
  let ready = false;
  const isReady = () => ready;

  const first = ensureVendorScript("/vendor/pdfjs/pdf.min.js", isReady, { document: doc, timeoutMs: 0 });
  const second = ensureVendorScript("/vendor/pdfjs/pdf.min.js", isReady, { document: doc, timeoutMs: 0 });
  assert.equal(doc.appended.length, 1, "并发调用只应注入一个 <script>");
  assert.equal(first, second, "并发调用应复用同一个 Promise");
  assert.equal(vendorScriptsInFlight(), 1);

  ready = true;
  doc.appended[0].fire("load");
  assert.equal(await first, true);
  assert.equal(vendorScriptsInFlight(), 0, "落定后不留在途记录");

  await ensureVendorScript("/vendor/pdfjs/pdf.min.js", isReady, { document: doc, timeoutMs: 0 });
  assert.equal(doc.appended.length, 1, "已就绪不应再注入");

  // 失败不缓存：下一次调用可以重新注入并成功。
  let docxReady = false;
  const isDocxReady = () => docxReady;
  const failed = ensureVendorScript("/vendor/docx-preview/docx-preview.min.js", isDocxReady, { document: doc, timeoutMs: 0 });
  doc.appended[1].fire("error");
  await assert.rejects(failed, /加载失败/);

  const retry = ensureVendorScript("/vendor/docx-preview/docx-preview.min.js", isDocxReady, { document: doc, timeoutMs: 0 });
  assert.equal(doc.appended.length, 3, "失败后重试应重新注入");
  docxReady = true;
  doc.appended[2].fire("load");
  assert.equal(await retry, true);
});

test("没有 document 时明确拒绝，不静默吞掉", async () => {
  await assert.rejects(
    ensureVendorScript("/vendor/x.js", () => false, { document: null, timeoutMs: 0 }),
    /没有 document/
  );
});

test("index.html 不再同步加载按需 vendor（docx-preview / pdf.js）", async () => {
  const html = await readFile(new URL("./index.html", import.meta.url), "utf8");
  assert.doesNotMatch(html, /src="\.\/vendor\/docx-preview\/docx-preview\.min\.js"/);
  assert.doesNotMatch(html, /src="\.\/vendor\/pdfjs\/pdf\.min\.js"/);
  assert.match(html, /src="\.\/vendor\/marked\/marked\.min\.js"/, "消息渲染依赖的 marked 仍要同步加载");
});
