// 沙箱渲染缓冲的离线判据（node --test，无浏览器、无第三方依赖）。
//
// 三层：
//   1) 纯决策：shouldDeferMount / planEmbedMounts —— 六类输入逐个钉住（领养、挂载、
//      延迟、卸载、新增、内容变化），不需要 DOM。
//   2) DOM 适配（手写最小 DOM 假体）：内容不变的帧**不产生新 iframe 元素**；内容变化
//      的帧在新帧 load 之前**旧元素仍在树上**；流式期间**挂载次数 = 0**；token 过期
//      的帧被丢弃；卸载会清掉 pending 帧。
//   3) 并发上限与排队。
//
// 假体的结构（figure > .html-embed-stage[data-embed-stage] > iframe.html-embed-frame）
// 与 html-embed.js 的 renderHtmlEmbed 对齐；属性名由 html-embed.test.mjs 守住，这里不
// 重复断言标记，只测缓冲行为。

import test from "node:test";
import assert from "node:assert/strict";

import {
  EMBED_SETTLE_MS,
  createEmbedFrameBuffer,
  planEmbedMounts,
  shouldDeferMount
} from "./embed-buffering.js";

// ── 最小 DOM 假体 ─────────────────────────────────────────
class FakeElement {
  constructor(tagName = "div") {
    this.tagName = String(tagName).toUpperCase();
    this.attributes = new Map();
    this.children = [];
    this.parentNode = null;
    this.listeners = new Map();
    this.removed = false;
  }
  setAttribute(name, value) {
    this.attributes.set(String(name), String(value));
  }
  getAttribute(name) {
    const key = String(name);
    return this.attributes.has(key) ? this.attributes.get(key) : null;
  }
  hasAttribute(name) {
    return this.attributes.has(String(name));
  }
  removeAttribute(name) {
    this.attributes.delete(String(name));
  }
  appendChild(child) {
    if (child.parentNode) child.parentNode.removeChild(child);
    child.parentNode = this;
    this.children.push(child);
    return child;
  }
  removeChild(child) {
    const index = this.children.indexOf(child);
    if (index >= 0) this.children.splice(index, 1);
    if (child.parentNode === this) child.parentNode = null;
    return child;
  }
  remove() {
    this.removed = true;
    if (this.parentNode) this.parentNode.removeChild(this);
  }
  replaceChildren(...nodes) {
    for (const child of this.children.slice()) child.parentNode = null;
    this.children = [];
    for (const node of nodes) this.appendChild(node);
  }
  cloneNode() {
    const copy = new FakeElement(this.tagName);
    for (const [name, value] of this.attributes) copy.attributes.set(name, value);
    return copy;
  }
  addEventListener(type, handler) {
    const key = String(type);
    const list = this.listeners.get(key) || [];
    list.push(handler);
    this.listeners.set(key, list);
  }
  removeEventListener(type, handler) {
    const key = String(type);
    const list = this.listeners.get(key) || [];
    this.listeners.set(key, list.filter(item => item !== handler));
  }
  fire(type) {
    for (const handler of [...(this.listeners.get(String(type)) || [])]) handler({ type });
  }
  matches(selector) {
    const text = String(selector).trim();
    if (text.startsWith("[") && text.endsWith("]")) return this.hasAttribute(text.slice(1, -1));
    return this.tagName === text.toUpperCase();
  }
  querySelectorAll(selector) {
    const found = [];
    const walk = node => {
      for (const child of node.children) {
        if (child.matches(selector)) found.push(child);
        walk(child);
      }
    };
    walk(this);
    return found;
  }
  querySelector(selector) {
    const found = this.querySelectorAll(selector);
    return found.length ? found[0] : null;
  }
}

class FakeDocument {
  constructor() {
    this.documentElement = new FakeElement("html");
    this.body = new FakeElement("body");
    this.documentElement.appendChild(this.body);
  }
  createElement(tagName) {
    return new FakeElement(tagName);
  }
}

// frameOf / stageOf / figureOf：与 renderHtmlEmbed 的标记同构
function figureFor(key, srcdoc) {
  const figure = new FakeElement("figure");
  figure.setAttribute("class", "html-embed");
  figure.setAttribute("data-embed-key", key);
  const stage = new FakeElement("div");
  stage.setAttribute("class", "html-embed-stage");
  stage.setAttribute("data-embed-stage", "1");
  stage.setAttribute("data-embed-key", key);
  const frame = new FakeElement("iframe");
  frame.setAttribute("class", "html-embed-frame");
  frame.setAttribute("data-embed-key", key);
  frame.setAttribute("sandbox", "allow-scripts");
  frame.setAttribute("referrerpolicy", "no-referrer");
  frame.setAttribute("loading", "lazy");
  frame.setAttribute("srcdoc", srcdoc);
  stage.appendChild(frame);
  figure.appendChild(stage);
  return figure;
}

function rowFor(rowKey, embeds = []) {
  const row = new FakeElement("article");
  row.setAttribute("data-conversation-key", rowKey);
  for (const embed of embeds) row.appendChild(figureFor(embed.key, embed.srcdoc));
  return row;
}

function stageIn(row, key) {
  for (const stage of row.querySelectorAll("[data-embed-stage]")) {
    if (stage.getAttribute("data-embed-key") === key) return stage;
  }
  return null;
}

function frameIn(row, key) {
  const stage = stageIn(row, key);
  return stage ? stage.querySelector("iframe") : null;
}

function figureIn(row, key) {
  const stage = stageIn(row, key);
  return stage ? stage.parentNode : null;
}

function makeClock() {
  let time = 0;
  let sequence = 0;
  const timers = new Map();
  return {
    now: () => time,
    schedule(fn, ms) {
      sequence += 1;
      timers.set(sequence, { at: time + Math.max(0, Number(ms) || 0), fn });
      return sequence;
    },
    cancel(handle) {
      timers.delete(handle);
    },
    advance(ms) {
      time += Math.max(0, Number(ms) || 0);
      let guard = 0;
      for (;;) {
        const due = [...timers.entries()]
          .filter(([, timer]) => timer.at <= time)
          .sort((a, b) => a[1].at - b[1].at);
        if (!due.length) break;
        guard += 1;
        if (guard > 200) throw new Error("timer loop");
        for (const [handle, timer] of due) {
          timers.delete(handle);
          timer.fn();
        }
      }
    },
    pending: () => timers.size
  };
}

function makeBuffer(extra = {}) {
  const doc = new FakeDocument();
  const mountRoot = doc.createElement("div");
  mountRoot.setAttribute("class", "html-embed-mount");
  doc.body.appendChild(mountRoot);
  const clock = extra.clock || makeClock();
  const buffer = createEmbedFrameBuffer({
    document: doc,
    mountRoot,
    now: clock.now,
    schedule: clock.schedule,
    cancel: clock.cancel,
    nextFrame: fn => fn(),
    ...extra,
    clock: undefined
  });
  return { buffer, doc, mountRoot, clock };
}

// ── 1. 纯决策：shouldDeferMount ───────────────────────────
test("shouldDeferMount：流式 / settle 窗口内一律延迟，其余照常挂", () => {
  // 流式：无论内容多稳定都不挂（每帧重建正是闪屏的根因）
  assert.equal(shouldDeferMount({ streaming: true, lastChangeAt: 0, now: 1000, settleMs: 150 }), true);
  assert.equal(shouldDeferMount({ streaming: true, lastChangeAt: 1, now: 99999, settleMs: 150 }), true);
  // 刚变过：窗口内延迟
  assert.equal(shouldDeferMount({ streaming: false, lastChangeAt: 1000, now: 1050, settleMs: 150 }), true);
  // 窗口过了：挂
  assert.equal(shouldDeferMount({ streaming: false, lastChangeAt: 1000, now: 1150, settleMs: 150 }), false);
  assert.equal(shouldDeferMount({ streaming: false, lastChangeAt: 1000, now: 5000 }), false);
  // 没有变更时刻（历史行 / 已被领养的稳定行）：不延迟；null 才是"没有"，0 是合法时刻
  assert.equal(shouldDeferMount({ lastChangeAt: null, now: 0 }), false);
  assert.equal(shouldDeferMount({ now: 0 }), false);
  assert.equal(shouldDeferMount(), false);
  // 缺省窗口就是 150ms 这一档
  assert.equal(EMBED_SETTLE_MS, 150);
  assert.equal(shouldDeferMount({ lastChangeAt: 0, now: 149 }), true);
  assert.equal(shouldDeferMount({ lastChangeAt: 0, now: 150 }), false);
  assert.equal(shouldDeferMount({ lastChangeAt: 1, now: 150 }), true);
});

// ── 2. 纯决策：planEmbedMounts 的六类输入 ────────────────
const prevFrames = [
  { key: "message:m1#0", srcdoc: "A" },
  { key: "message:m1#1", srcdoc: "B" },
  { key: "message:gone#0", srcdoc: "Z" }
];

test("planEmbedMounts：同 key 同 srcdoc → adopt（不重新加载）", () => {
  const plan = planEmbedMounts(prevFrames, [{ key: "message:m1#1", srcdoc: "B" }], { now: 0 });
  assert.deepEqual(plan.adopt, [{ key: "message:m1#1", srcdoc: "B" }]);
  assert.deepEqual(plan.mount, []);
  assert.deepEqual(plan.defer, []);
  assert.deepEqual(plan.unmount, ["message:m1#0", "message:gone#0"]);
});

test("planEmbedMounts：同 key 内容变了 → mount；新出现的 key → mount", () => {
  const plan = planEmbedMounts(prevFrames, [
    { key: "message:m1#0", srcdoc: "A2" },
    { key: "message:m2#0", srcdoc: "N" }
  ], { now: 1000, lastChangeAt: 0 });
  assert.deepEqual(plan.adopt, []);
  assert.deepEqual(plan.mount, [
    { key: "message:m1#0", srcdoc: "A2" },
    { key: "message:m2#0", srcdoc: "N" }
  ]);
  assert.deepEqual(plan.unmount, ["message:m1#1", "message:gone#0"]);
});

test("planEmbedMounts：内容变了但还在流式 → defer（占位），不是 mount", () => {
  const plan = planEmbedMounts(prevFrames, [
    { key: "message:m1#0", srcdoc: "A2" },
    { key: "message:m1#1", srcdoc: "B" }
  ], { streaming: true, now: 10_000, lastChangeAt: 0 });
  assert.deepEqual(plan.adopt, [{ key: "message:m1#1", srcdoc: "B" }]);
  assert.deepEqual(plan.defer, [{ key: "message:m1#0", srcdoc: "A2" }]);
  assert.deepEqual(plan.mount, []);
});

test("planEmbedMounts：内容变了但还在 settle 窗口内 → defer；窗口过了 → mount", () => {
  const next = [{ key: "message:m1#0", srcdoc: "A2" }];
  const early = planEmbedMounts(prevFrames, next, { now: 1040, lastChangeAt: 1000, settleMs: 150 });
  assert.deepEqual(early.defer, [{ key: "message:m1#0", srcdoc: "A2" }]);
  assert.deepEqual(early.mount, []);
  const late = planEmbedMounts(prevFrames, next, { now: 1200, lastChangeAt: 1000, settleMs: 150 });
  assert.deepEqual(late.mount, [{ key: "message:m1#0", srcdoc: "A2" }]);
  assert.deepEqual(late.defer, []);
});

test("planEmbedMounts：旧行有、新行没有 → unmount（含旧行整条消失）", () => {
  assert.deepEqual(planEmbedMounts(prevFrames, [], { now: 0 }).unmount, [
    "message:m1#0", "message:m1#1", "message:gone#0"
  ]);
  // 空输入 / 缺字段不炸
  assert.deepEqual(planEmbedMounts(undefined, undefined), { adopt: [], mount: [], defer: [], unmount: [] });
  assert.deepEqual(planEmbedMounts([], [{ srcdoc: "x" }]).mount, []);
});

// ── 3. DOM 适配：领养不产生新元素 ────────────────────────
test("embed buffer：内容不变的帧被原样领养（没有新 iframe 元素）", () => {
  const { buffer, mountRoot } = makeBuffer();
  const before = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "A" }]);
  const originalFrame = frameIn(before, "message:m1#0");
  const next = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "A" }]);
  const parsedFrame = frameIn(next, "message:m1#0");
  const plan = buffer.adopt(before, next, { streaming: false });
  buffer.attach(next, { streaming: false });
  assert.deepEqual(plan.adopt, [{ key: "message:m1#0", srcdoc: "A" }]);
  assert.equal(frameIn(next, "message:m1#0"), originalFrame);
  assert.notEqual(frameIn(next, "message:m1#0"), parsedFrame);
  assert.equal(parsedFrame.parentNode, null);
  assert.equal(mountRoot.children.length, 0);
  assert.equal(buffer.stats().mounted, 0);
});

// ── 4. DOM 适配：内容变化的帧，load 之前旧元素仍在树上 ──
test("embed buffer：内容变了 → 旧帧留在原位，新帧 load 完成后才交换", () => {
  const { buffer, mountRoot, clock } = makeBuffer();
  const before = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "A" }]);
  const oldFrame = frameIn(before, "message:m1#0");
  const next = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "B" }]);
  buffer.adopt(before, next, { streaming: false });
  clock.advance(EMBED_SETTLE_MS);
  buffer.attach(next, { streaming: false });
  // 新帧在离屏容器里等 load；可视位仍是旧帧（不露白、不塌陷）
  assert.equal(frameIn(next, "message:m1#0"), oldFrame);
  assert.equal(mountRoot.children.length, 1);
  const pending = mountRoot.children[0];
  assert.equal(pending.getAttribute("srcdoc"), "B");
  // 沙箱口径随帧一起搬：克隆不带 srcdoc 以外的改写
  assert.equal(pending.getAttribute("sandbox"), "allow-scripts");
  assert.equal(pending.hasAttribute("allow-same-origin"), false);
  // load 回来 → 交换；旧帧离场
  pending.fire("load");
  assert.equal(frameIn(next, "message:m1#0"), pending);
  assert.equal(oldFrame.parentNode, null);
  assert.equal(figureIn(next, "message:m1#0").hasAttribute("data-embed-pending"), false);
  assert.equal(buffer.stats().mounted, 1);
});

// ── 5. DOM 适配：流式期间挂载次数 = 0 ────────────────────
test("embed buffer：流式期间一帧都不挂，只放同高占位；定稿后一次挂上", () => {
  const { buffer, mountRoot, clock } = makeBuffer();
  const before = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "A" }]);
  const oldFrame = frameIn(before, "message:m1#0");
  let next = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "A" }]);
  buffer.adopt(before, next, { streaming: true });
  buffer.attach(next, { streaming: true });
  // 流式里连变三次：行被换三次、内容每次都变
  let current = next;
  for (const srcdoc of ["A1", "A2", "A3"]) {
    const replaced = rowFor("message:m1", [{ key: "message:m1#0", srcdoc }]);
    buffer.adopt(current, replaced, { streaming: true });
    clock.advance(40);
    buffer.attach(replaced, { streaming: true });
    current = replaced;
  }
  clock.advance(2000);
  assert.equal(buffer.stats().mounted, 0, "流式期间挂载次数必须是 0");
  assert.equal(mountRoot.children.length, 0);
  // 旧帧一直占着可视位（流式期间不重建任何帧）
  assert.equal(frameIn(current, "message:m1#0"), oldFrame);
  assert.equal(figureIn(current, "message:m1#0").getAttribute("data-embed-pending"), "1");
  // 消息定稿：流式关掉 → 一次挂载（内容早已稳定，不再等 settle）
  buffer.setStreaming(false);
  assert.equal(mountRoot.children.length, 1, "定稿后立刻挂一枚，而且只挂一枚");
  assert.equal(mountRoot.children[0].getAttribute("srcdoc"), "A3", "挂的是最后一版内容");
  const fresh = mountRoot.children[0];
  fresh.fire("load");
  assert.equal(frameIn(current, "message:m1#0"), fresh);
  assert.equal(buffer.stats().mounted, 1);
  assert.equal(figureIn(current, "message:m1#0").hasAttribute("data-embed-pending"), false);
});

test("embed buffer：刚变过就结束流式 → 仍等 settle 窗口再一次挂上", () => {
  const { buffer, mountRoot, clock } = makeBuffer();
  const before = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "A" }]);
  const next = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "B" }]);
  buffer.adopt(before, next, { streaming: true });
  clock.advance(30);
  buffer.attach(next, { streaming: true });
  buffer.setStreaming(false);
  assert.equal(mountRoot.children.length, 0, "变更后 30ms：还在 settle 窗口里");
  clock.advance(EMBED_SETTLE_MS);
  assert.equal(mountRoot.children.length, 1);
  assert.equal(mountRoot.children[0].getAttribute("srcdoc"), "B");
});

// ── 6. DOM 适配：token 过期的帧被丢弃 ────────────────────
test("embed buffer：token 过期（期间又变了一次）的帧被丢弃，绝不上屏", () => {
  const { buffer, mountRoot, clock } = makeBuffer();
  const before = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "A" }]);
  let current = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "B" }]);
  buffer.adopt(before, current, { streaming: false });
  clock.advance(EMBED_SETTLE_MS);
  buffer.attach(current, { streaming: false });
  assert.equal(mountRoot.children.length, 1);
  const stale = mountRoot.children[0];
  // 还没 load 回来，内容又变了：换行 → 新 token
  const replaced = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "C" }]);
  buffer.adopt(current, replaced, { streaming: false });
  clock.advance(EMBED_SETTLE_MS);
  buffer.attach(replaced, { streaming: false });
  current = replaced;
  // 老帧此时才 load：token 已过期 → 丢弃，界面上不能出现 B
  stale.fire("load");
  assert.equal(stale.parentNode, null);
  assert.notEqual(frameIn(current, "message:m1#0"), stale);
  assert.equal(buffer.stats().discarded, 1);
  // 新帧（C）到了才换
  const fresh = mountRoot.children.find(child => child.getAttribute("srcdoc") === "C");
  assert.ok(fresh, "新内容的帧应该已经发出去");
  fresh.fire("load");
  assert.equal(frameIn(current, "message:m1#0"), fresh);
  assert.equal(buffer.stats().mounted >= 1, true);
});

// ── 7. DOM 适配：卸载会清 pending ────────────────────────
test("embed buffer：行被移出时清 pending 帧、撤在途挂载与监听", () => {
  const { buffer, mountRoot, clock } = makeBuffer();
  const row = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "A" }]);
  buffer.adopt(null, row, { streaming: false });
  clock.advance(EMBED_SETTLE_MS);
  buffer.attach(row, { streaming: false });
  assert.equal(mountRoot.children.length, 1);
  const pending = mountRoot.children[0];
  assert.equal(pending.listeners.get("load")?.length, 1);
  buffer.release(row);
  assert.equal(pending.parentNode, null, "pending 帧必须从离屏容器里撤掉");
  assert.equal(buffer.stats().inFlight, 0);
  // load 迟到也不会把已卸载的行唤醒
  pending.fire("load");
  assert.equal(buffer.stats().mounted, 0);
  // 老的定时器不再把行挂回去
  clock.advance(5000);
  assert.equal(mountRoot.children.length, 0);
});

test("embed buffer：key 从新行消失时撤销该 key 的在途挂载", () => {
  const { buffer, mountRoot, clock } = makeBuffer();
  const before = rowFor("message:m1", [
    { key: "message:m1#0", srcdoc: "A" },
    { key: "message:m1#1", srcdoc: "B" }
  ]);
  const next = rowFor("message:m1", [{ key: "message:m1#1", srcdoc: "B2" }]);
  const plan = buffer.adopt(before, next, { streaming: false });
  assert.deepEqual(plan.unmount, ["message:m1#0"]);
  clock.advance(EMBED_SETTLE_MS);
  buffer.attach(next, { streaming: false });
  assert.equal(buffer.stats().unmounted, 1);
  const keys = buffer.pendingKeys();
  assert.deepEqual(keys, ["message:m1::message:m1#1"]);
  const pending = mountRoot.children[0];
  assert.equal(pending.getAttribute("srcdoc"), "B2");
});

// ── 8. 并发上限与排队 ────────────────────────────────────
test("embed buffer：并发 pending 上限 4，超出排队，空位后自动接上", () => {
  const { buffer, mountRoot, clock } = makeBuffer({ maxPending: 4 });
  const embeds = Array.from({ length: 6 }, (_, index) => ({ key: `message:m1#${index}`, srcdoc: `S${index}` }));
  const row = rowFor("message:m1", embeds);
  buffer.adopt(null, row, { streaming: false });
  clock.advance(EMBED_SETTLE_MS);
  buffer.attach(row, { streaming: false });
  assert.equal(buffer.stats().inFlight, 4);
  assert.equal(buffer.stats().queued, 2);
  assert.equal(mountRoot.children.length, 4);
  // 完成一枚 → 队列接一枚
  mountRoot.children[0].fire("load");
  assert.equal(buffer.stats().inFlight, 4);
  assert.equal(buffer.stats().queued, 1);
  assert.equal(mountRoot.children.length, 4);
  assert.equal(buffer.stats().mounted, 1);
});

// ── 10. 接线顺序（源码级守卫）────────────────────────────
// reconcile 里最容易出错的是**顺序**：领养必须在换行之前（换行之后旧帧已经不在了），
// 挂载必须在行进文档之后（离屏容器里的帧与可视位无关，但占位与旧帧要先就位），
// 释放必须在移除之前（行没了就找不到它的状态）。这里钉住顺序，不重复测缓冲算法。
test("接线顺序：conversation-view 先领养再换行、行进文档后再挂载、移除前先释放", async () => {
  const { readFile } = await import("node:fs/promises");
  const source = await readFile(new URL("./conversation-view.js", import.meta.url), "utf8");
  assert.match(source, /import \{ createEmbedFrameBuffer \} from "\.\/embed-buffering\.js"/);
  const at = needle => {
    const index = source.indexOf(needle);
    assert.ok(index > 0, `接线里缺 ${needle}`);
    return index;
  };
  const adopt = at("embedBuffer?.adopt(node, replacement");
  const swap = at("node.replaceWith(replacement)");
  const insert = at("container.insertBefore(node, cursor)");
  const attach = at("embedBuffer?.attach(node, { streaming })");
  const release = at("embedBuffer?.release(cursor)");
  const remove = at("cursor.remove()");
  assert.ok(adopt < swap, "领养必须在换行之前");
  assert.ok(swap < insert && insert < attach, "挂载必须在行进文档之后");
  assert.ok(release < remove, "释放必须在移除之前");
  // 流式标记来自渲染选项，不是缓冲层自己猜的
  assert.match(source, /embedBuffer\.setStreaming\(Boolean\(options\.streaming\)\)/);
});
// ── 9. 双缓冲开关 ────────────────────────────────────────
test("embed buffer：doubleBuffer 时新帧叠在旧帧上，过渡结束才撤旧帧（默认关）", () => {
  const single = makeBuffer();
  assert.equal(single.buffer.doubleBuffer(), false);
  const { buffer, mountRoot, clock } = makeBuffer({ doubleBuffer: true });
  assert.equal(buffer.doubleBuffer(), true);
  const before = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "A" }]);
  const oldFrame = frameIn(before, "message:m1#0");
  const next = rowFor("message:m1", [{ key: "message:m1#0", srcdoc: "B" }]);
  buffer.adopt(before, next, { streaming: false });
  clock.advance(EMBED_SETTLE_MS);
  buffer.attach(next, { streaming: false });
  const pending = mountRoot.children[0];
  pending.fire("load");
  const stage = stageIn(next, "message:m1#0");
  // 两层同时在树上：新帧带 data-embed-layer="new" 等着淡入
  assert.equal(stage.children.length, 2);
  assert.equal(pending.getAttribute("data-embed-layer"), "new");
  assert.equal(pending.getAttribute("data-embed-live"), "1");
  assert.equal(oldFrame.parentNode, stage);
  clock.advance(200);
  assert.equal(stage.children.length, 1);
  assert.equal(oldFrame.parentNode, null);
  assert.equal(stage.querySelector("iframe"), pending);
});
