// 文件详情面板**控制器级**契约测试：用最小假 DOM 驱动 createFilePreviewController
// 的真实分支（不开窗、不碰真实文件系统），断言模块对外的可观察行为：
//   1. 编辑只改缓冲区，**只有 Ctrl+S（或"保存"按钮）才调 writer 落盘**；
//   2. 落盘字符串按原文件的编码事实（EOL / BOM）序列化，不是把编辑器正文原样丢出去；
//   3. 保存成功后**读回实际文件**并把基线换成读回的那一份（磁盘为准，含漂移提示）；
//   4. 关 chip / 收起抽屉前若有未保存内容，先问（保存 / 不保存 / 取消），
//      取消与保存失败都必须原样保留。
//
// 这与 terminal-panel-controller.test.mjs 同一套路：假 DOM 只实现模块真正用到的那点
// 接口，其余一律不装。

import assert from "node:assert/strict";
import test from "node:test";

import { createFilePreviewController } from "./file-preview.js";

// ── 最小假 DOM ──────────────────────────────────────────────────────────

class FakeClassList {
  constructor() {
    this.names = new Set();
  }
  add(...names) {
    for (const name of names) this.names.add(name);
  }
  remove(...names) {
    for (const name of names) this.names.delete(name);
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
    this.attributes = new Map();
    this.dataset = {};
    this.children = [];
    this.parent = null;
    this.listeners = new Map();
    this.hidden = false;
    this.textContent = "";
    this.value = "";
    this.focused = false;
    this._innerHTML = "";
  }
  get className() {
    return this.classList.value;
  }
  set className(value) {
    this.classList = new FakeClassList();
    for (const name of String(value).split(/\s+/).filter(Boolean)) this.classList.add(name);
  }
  get innerHTML() {
    return this._innerHTML;
  }
  set innerHTML(value) {
    // 与浏览器一致：写 innerHTML 会丢掉既有子节点（渲染函数依赖这个语义）。
    this._innerHTML = String(value);
    this.children = [];
  }
  appendChild(child) {
    child.parent = this;
    this.children.push(child);
    return child;
  }
  remove() {
    if (!this.parent) return;
    this.parent.children = this.parent.children.filter(item => item !== this);
    this.parent = null;
  }
  setAttribute(name, value) {
    this.attributes.set(name, String(value));
  }
  getAttribute(name) {
    return this.attributes.has(name) ? this.attributes.get(name) : null;
  }
  addEventListener(type, handler) {
    if (!this.listeners.has(type)) this.listeners.set(type, []);
    this.listeners.get(type).push(handler);
  }
  dispatch(type, event = {}) {
    for (const handler of this.listeners.get(type) || []) handler({ target: this, preventDefault() {}, ...event });
  }
  focus() {
    this.focused = true;
  }
}

function installFakeGlobals() {
  globalThis.document = { createElement: tag => new FakeElement(tag) };
  // 渲染分支会探 window.hljs / window.marked / window.DOMPurify：这里一律缺席，
  // 于是走纯文本/降级路径（测试关心的是编辑与保存，不是高亮）。
  globalThis.window = {};
}

function walk(node, out = []) {
  for (const child of node.children || []) {
    out.push(child);
    walk(child, out);
  }
  return out;
}

function firstTextarea(view) {
  return walk(view).find(node => node.tagName === "TEXTAREA");
}

function settle() {
  return new Promise(resolve => setTimeout(resolve, 0));
}

// ── 测试夹具 ────────────────────────────────────────────────────────────

function createHarness() {
  installFakeGlobals();
  const view = new FakeElement("div");
  const tabsHost = new FakeElement("div");
  const files = new Map(); // path → { bytes, truncated, text_like }
  const externalAfterWrite = new Map(); // path → 写入落地后又被人改成的正文
  const writes = [];
  const reads = [];
  const notices = [];
  const errors = [];
  const saved = [];
  const confirmCalls = [];
  let choice = "cancel";
  let writeFailure = false;
  let empties = 0;

  const controller = createFilePreviewController({
    view,
    tabsHost,
    loader: async (entry, kind, limit) => {
      reads.push({ path: entry.path, kind, limit });
      const file = files.get(entry.path);
      if (!file) throw new Error(`no such file: ${entry.path}`);
      return {
        name: entry.name || entry.path,
        path: entry.path,
        size: file.bytes.length,
        base64: Buffer.from(file.bytes).toString("base64"),
        limit,
        truncated: Boolean(file.truncated),
        text_like: file.text_like !== false
      };
    },
    writer: async (entry, text) => {
      writes.push({ path: entry.path, text });
      if (writeFailure) throw new Error("写入被拒绝（桩）");
      const file = files.get(entry.path);
      if (file) {
        // externalAfterWrite 模拟"我们的写入落地之后，文件又被外部改动"：
        // 读回得到的就不是刚写进去的那一份（基线必须以磁盘为准）。
        const external = externalAfterWrite.get(entry.path);
        file.bytes = new TextEncoder().encode(external === undefined ? text : external);
      }
      return { path: entry.path, size: Buffer.byteLength(text) };
    },
    confirmSave: async ({ paths }) => {
      confirmCalls.push([...paths]);
      return choice;
    },
    onSaved: path => saved.push(path),
    onNotice: message => notices.push(message),
    onError: error => errors.push(error),
    onEmpty: () => { empties += 1; }
  });

  return {
    controller, view, tabsHost, files, writes, reads, notices, errors, saved, confirmCalls,
    setChoice: value => { choice = value; },
    setWriteFailure: value => { writeFailure = value === true; },
    setExternalAfterWrite: (path, text) => { externalAfterWrite.set(path, text); },
    emptyCount: () => empties
  };
}

function putFile(harness, path, text, options = {}) {
  harness.files.set(path, {
    bytes: new TextEncoder().encode(text),
    truncated: options.truncated === true,
    text_like: options.text_like
  });
}

async function openFile(harness, path) {
  await harness.controller.open({ path, name: path });
  await settle();
}

function ctrlS(harness) {
  harness.view.dispatch("keydown", { ctrlKey: true, key: "s" });
}

// ── 用例 ────────────────────────────────────────────────────────────────

test("编辑只改缓冲区：不改字就没有写入，Ctrl+S 才落盘", async () => {
  const harness = createHarness();
  putFile(harness, "notes.txt", "line1\nline2\n");
  await openFile(harness, "notes.txt");

  assert.deepEqual(harness.writes, [], "打开文件不得触发写入");
  assert.deepEqual(harness.controller.dirtyPaths(), []);

  assert.equal(harness.controller.edit("notes.txt"), true);
  const textarea = firstTextarea(harness.view);
  assert.ok(textarea, "编辑态必须出现编辑器");
  assert.equal(textarea.value, "line1\nline2\n", "编辑器初值 = 磁盘基线");

  textarea.value = "line1 changed\nline2\n";
  textarea.dispatch("input");
  assert.deepEqual(harness.controller.dirtyPaths(), ["notes.txt"], "改动必须被标脏");
  assert.deepEqual(harness.writes, [], "改字本身不得落盘（VSCode 口径：只有 Ctrl+S 才保存）");

  ctrlS(harness);
  await settle();
  assert.deepEqual(harness.writes, [{ path: "notes.txt", text: "line1 changed\nline2\n" }]);
  assert.deepEqual(harness.controller.dirtyPaths(), [], "保存成功后不再脏");
  assert.deepEqual(harness.saved, ["notes.txt"], "保存成功要通知宿主刷新改动面");
  assert.equal(new TextDecoder().decode(harness.files.get("notes.txt").bytes), "line1 changed\nline2\n");
});

test("保存按原文件编码事实序列化（CRLF 与 BOM 原样回去）", async () => {
  const harness = createHarness();
  putFile(harness, "win.txt", "\ufeffa\r\nb\r\n");
  await openFile(harness, "win.txt");
  harness.controller.edit("win.txt");
  const textarea = firstTextarea(harness.view);
  assert.equal(textarea.value, "a\nb\n", "编辑器正文归一成 LF（BOM 已剥离）");

  textarea.value = "A\nb\n";
  ctrlS(harness);
  await settle();
  assert.equal(harness.writes[0].text, "\ufeffA\r\nb\r\n", "落盘必须还原 CRLF 与 BOM，而不是刷掉整个文件的重行风格");
  assert.deepEqual(harness.notices, []);
});

test("保存后读回：磁盘内容与写入不一致时按磁盘刷新基线并提示", async () => {
  const harness = createHarness();
  putFile(harness, "race.txt", "original\n");
  await openFile(harness, "race.txt");
  harness.controller.edit("race.txt");
  const textarea = firstTextarea(harness.view);
  textarea.value = "mine\n";

  // 我们的写入落地之后，文件又被外部改动（保存后读回得到的不是刚写的那一份）。
  harness.setExternalAfterWrite("race.txt", "someone else\n");

  ctrlS(harness);
  await settle();
  assert.equal(harness.writes.length, 1);
  assert.match(harness.notices.join(" "), /已按磁盘内容刷新基线/, "读回漂移必须如实提示");
  assert.equal(textarea.value, "someone else\n", "基线以磁盘为准（编辑器换成读回的内容）");
  assert.deepEqual(harness.controller.dirtyPaths(), [], "漂移按磁盘对齐后不算脏（用户看到的就是磁盘）");
});

test("脏 chip 关闭前弹窗：取消保留、不保存丢弃、保存先落盘再关", async () => {
  const harness = createHarness();
  putFile(harness, "a.txt", "one\n");
  putFile(harness, "b.txt", "two\n");
  await openFile(harness, "a.txt");
  await openFile(harness, "b.txt");

  // 取消：什么都不变。
  harness.setChoice("cancel");
  harness.controller.edit("a.txt");
  harness.controller.activateTab("a.txt");
  firstTextarea(harness.view).value = "one changed\n";
  await harness.controller.requestClose("a.txt");
  assert.deepEqual(harness.confirmCalls, [["a.txt"]], "脏关闭必须问一次");
  assert.ok(harness.controller.tabs().some(tab => tab.path === "a.txt"), "取消后文件仍在");
  assert.deepEqual(harness.writes, []);

  // 不保存：关掉、丢弃缓冲区，且**没有**写入。
  harness.setChoice("discard");
  await harness.controller.requestClose("a.txt");
  assert.ok(!harness.controller.tabs().some(tab => tab.path === "a.txt"), "不保存应当关闭");
  assert.deepEqual(harness.writes, []);
  assert.equal(harness.controller.isDirty("a.txt"), false, "面板已销毁，脏状态一起消失");

  // 保存：先落盘再关闭。
  harness.setChoice("save");
  await openFile(harness, "a.txt");
  harness.controller.edit("a.txt");
  firstTextarea(harness.view).value = "one saved\n";
  await harness.controller.requestClose("a.txt");
  assert.deepEqual(harness.writes, [{ path: "a.txt", text: "one saved\n" }]);
  assert.ok(!harness.controller.tabs().some(tab => tab.path === "a.txt"), "保存成功后关闭");
});

test("收起抽屉前的守卫：取消 / 保存失败都不得收起，也不得丢内容", async () => {
  const harness = createHarness();
  putFile(harness, "a.txt", "one\n");
  await openFile(harness, "a.txt");

  // 未编辑时直接放行（不动弹窗）。
  assert.equal(await harness.controller.requestCloseAll(), true);
  assert.deepEqual(harness.confirmCalls, []);

  harness.controller.edit("a.txt");
  firstTextarea(harness.view).value = "dirty\n";

  // 取消：拦住收起，内容仍在。
  harness.setChoice("cancel");
  assert.equal(await harness.controller.requestCloseAll(), false, "取消必须拦住收起");
  assert.deepEqual(harness.controller.dirtyPaths(), ["a.txt"]);

  // 保存失败：同样拦住收起（用户得看见失败），编辑器内容不得被清掉。
  harness.setChoice("save");
  harness.setWriteFailure(true);
  assert.equal(await harness.controller.requestCloseAll(), false, "保存失败必须拦住收起");
  assert.equal(harness.controller.isDirty("a.txt"), true, "失败后内容仍在编辑器里");
  assert.ok(harness.errors.length > 0, "失败要有失败面");

  // 修好写入面后保存成功 → 放行收起。
  harness.setWriteFailure(false);
  assert.equal(await harness.controller.requestCloseAll(), true);
  assert.deepEqual(harness.controller.dirtyPaths(), []);

  // 不保存同样放行。
  harness.controller.edit("a.txt");
  firstTextarea(harness.view).value = "dirty again\n";
  harness.setChoice("discard");
  assert.equal(await harness.controller.requestCloseAll(), true);
});

test("不可编辑的读取结果不给编辑入口（二进制 / 截断 / 非 UTF-8）", async () => {
  const harness = createHarness();
  putFile(harness, "blob.bin", "AB\u0000CD", { text_like: false });
  putFile(harness, "huge.txt", "0123456789", { truncated: true });
  harness.files.set("gbk.txt", { bytes: new Uint8Array([0xB1, 0xE0, 0xC2, 0xEB]), text_like: true });
  putFile(harness, "ok.txt", "fine\n");

  await openFile(harness, "blob.bin");
  assert.equal(harness.controller.edit("blob.bin"), false, "二进制文件不得进编辑态");
  await openFile(harness, "huge.txt");
  assert.equal(harness.controller.edit("huge.txt"), false, "截断的读取不得进编辑态");
  await openFile(harness, "gbk.txt");
  assert.equal(harness.controller.edit("gbk.txt"), false, "非 UTF-8 不得进编辑态（保存会静默改编码）");
  await openFile(harness, "ok.txt");
  assert.equal(harness.controller.edit("ok.txt"), true, "普通文本文件应当可编辑");
});

test("非编辑态的 Ctrl+S 不写盘；一个 chip 的编辑不影响另一个", async () => {
  const harness = createHarness();
  putFile(harness, "a.txt", "one\n");
  putFile(harness, "b.txt", "two\n");
  await openFile(harness, "a.txt");
  await openFile(harness, "b.txt");

  ctrlS(harness);
  await settle();
  assert.deepEqual(harness.writes, [], "没有编辑器时 Ctrl+S 不得写盘");

  harness.controller.edit("a.txt");
  harness.controller.activateTab("a.txt");
  firstTextarea(harness.view).value = "one changed\n";
  ctrlS(harness);
  await settle();
  assert.deepEqual(harness.writes, [{ path: "a.txt", text: "one changed\n" }]);
  assert.equal(harness.controller.isDirty("b.txt"), false, "另一个文件不受影响");
  assert.deepEqual(harness.controller.dirtyPaths(), []);
});

test("「完成」退出编辑：有未保存内容先问，不保存则回到只读视图", async () => {
  const harness = createHarness();
  putFile(harness, "a.txt", "one\n");
  await openFile(harness, "a.txt");
  harness.controller.edit("a.txt");
  const textarea = firstTextarea(harness.view);
  textarea.value = "dirty\n";

  harness.setChoice("cancel");
  await harness.controller.exitEdit("a.txt");
  assert.equal(firstTextarea(harness.view), textarea, "取消后仍在编辑态");

  harness.setChoice("discard");
  await harness.controller.exitEdit("a.txt");
  assert.equal(firstTextarea(harness.view), undefined, "不保存后回到只读视图");
  assert.equal(harness.controller.isDirty("a.txt"), false);
  assert.deepEqual(harness.writes, []);
});
