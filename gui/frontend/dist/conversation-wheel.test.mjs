import test from "node:test";
import assert from "node:assert/strict";
import {
  buildWheelLines,
  lineAtOffset,
  normalizeWheelKind,
  scrollTopForFraction,
  scrollTopForThumbTop,
  wheelSignature,
  wheelThumb
} from "./conversation-wheel.js";

test("wheel: kinds normalize to the canonical set", () => {
  assert.equal(normalizeWheelKind("user"), "user");
  assert.equal(normalizeWheelKind("assistant"), "agent");
  assert.equal(normalizeWheelKind("llm"), "agent");
  assert.equal(normalizeWheelKind("thinking"), "think");
  assert.equal(normalizeWheelKind("tool_result"), "tools");
  assert.equal(normalizeWheelKind("axis"), "tools");
  assert.equal(normalizeWheelKind("error"), "system");
  assert.equal(normalizeWheelKind(""), "other");
  assert.equal(normalizeWheelKind(undefined), "other");
});

test("wheel: lines are placed by real geometry, not by uniform weight", () => {
  const rows = [
    { key: "message:1", kind: "user", label: "你 · 目标", offsetTop: 0, height: 100 },
    { key: "roll:tool:a", kind: "tools", label: "工具过程 · 3 次", offsetTop: 100, height: 600 },
    { key: "message:2", kind: "agent", label: "Seelex · 结论", offsetTop: 700, height: 300 }
  ];
  const result = buildWheelLines(rows, { scrollHeight: 1000, trackHeight: 500, minLine: 2, maxLine: 400 });
  assert.equal(result.empty, false);
  assert.equal(result.lines.length, 3);
  // 大块（600px 工具过程）必须明显长于小块（100px 用户输入）——均分权重做不到。
  assert.ok(result.lines[1].height > result.lines[0].height * 3);
  // 首条线顶部在轨道顶部；末条线落在轨道底部而非均分位置。
  assert.equal(result.lines[0].top, 0);
  assert.ok(result.lines[2].top > 300);
  assert.equal(result.lines[1].kind, "tools");
  assert.equal(result.lines[1].label, "工具过程 · 3 次");
});

test("wheel: line height is clamped so tiny and huge items stay visible", () => {
  const lines = buildWheelLines([
    { key: "a", kind: "user", offsetTop: 0, height: 1 },
    { key: "b", kind: "agent", offsetTop: 1, height: 99999 }
  ], { scrollHeight: 100000, trackHeight: 400, minLine: 2, maxLine: 28 }).lines;
  assert.equal(lines[0].height, 2);
  assert.equal(lines[1].height, 28);
  // 线不会溢出轨道。
  for (const line of lines) assert.ok(line.top + line.height <= 400);
});

test("wheel: empty geometry renders no lines and hides the rail", () => {
  assert.equal(buildWheelLines([], { scrollHeight: 100, trackHeight: 100 }).empty, true);
  assert.equal(buildWheelLines([{ key: "a", offsetTop: 0, height: 10 }], { scrollHeight: 0, trackHeight: 100 }).empty, true);
  assert.equal(buildWheelLines([{ key: "a", offsetTop: 0, height: 10 }], { scrollHeight: 100, trackHeight: 0 }).empty, true);
});

test("wheel: thumb reflects viewport size and scroll position", () => {
  const box = { scrollHeight: 1000, clientHeight: 250, trackHeight: 400, scrollTop: 0 };
  const top = wheelThumb(box);
  assert.equal(top.top, 0);
  assert.equal(top.height, 100); // 250/1000 * 400
  assert.equal(top.progress, 0);

  const middle = wheelThumb({ ...box, scrollTop: 375 });
  assert.equal(middle.progress, 0.5);
  assert.equal(middle.top, 150); // travel = 300, 50% -> 150

  const bottom = wheelThumb({ ...box, scrollTop: 750 });
  assert.equal(bottom.top, 300);
  assert.equal(bottom.progress, 1);

  // 内容不足一屏：滑柄铺满轨道、不抖动。
  const short = wheelThumb({ scrollHeight: 100, clientHeight: 400, trackHeight: 400, scrollTop: 0 });
  assert.equal(short.height, 400);
  assert.equal(short.top, 0);
  assert.equal(short.maxScroll, 0);
});

test("wheel: dragging maps thumb position back to scrollTop 1:1", () => {
  const box = { scrollHeight: 1000, clientHeight: 250, trackHeight: 400, scrollTop: 0 };
  assert.equal(scrollTopForThumbTop(0, box), 0);
  assert.equal(scrollTopForThumbTop(150, box), 375);
  assert.equal(scrollTopForThumbTop(300, box), 750);
  // 越界钳制：拖出轨道不会把内容甩飞。
  assert.equal(scrollTopForThumbTop(-40, box), 0);
  assert.equal(scrollTopForThumbTop(9999, box), 750);
  // 往返一致（拖 37% 再反解回同一位置）。
  const back = scrollTopForThumbTop(wheelThumb({ ...box, scrollTop: 200 }).top, box);
  assert.ok(Math.abs(back - 200) < 0.01);
});

test("wheel: track click maps fraction to scroll position", () => {
  const box = { scrollHeight: 1200, clientHeight: 200 };
  assert.equal(scrollTopForFraction(0, box), 0);
  assert.equal(scrollTopForFraction(0.5, box), 500);
  assert.equal(scrollTopForFraction(1, box), 1000);
  assert.equal(scrollTopForFraction(2, box), 1000);
  assert.equal(scrollTopForFraction(-1, box), 0);
});

test("wheel: hover hit-test prefers exact hit then nearest line", () => {
  const lines = [
    { key: "a", kind: "user", top: 0, height: 10, offsetTop: 0, offsetBottom: 100 },
    { key: "b", kind: "agent", top: 100, height: 20, offsetTop: 100, offsetBottom: 300 }
  ];
  assert.equal(lineAtOffset(lines, 5).key, "a");
  assert.equal(lineAtOffset(lines, 110).key, "b");
  // 线之间的空隙：取最近的一条（细线也要能点中）。
  assert.equal(lineAtOffset(lines, 60).key, "b");
  assert.equal(lineAtOffset(lines, 30).key, "a");
  assert.equal(lineAtOffset(lines, 130).key, "b");
  assert.equal(lineAtOffset([], 10), null);
});

test("wheel: signature changes only when geometry or kind changes", () => {
  const first = buildWheelLines([{ key: "a", kind: "user", offsetTop: 0, height: 10 }], { scrollHeight: 10, trackHeight: 100 }).lines;
  const same = buildWheelLines([{ key: "a", kind: "user", offsetTop: 0, height: 10 }], { scrollHeight: 10, trackHeight: 100 }).lines;
  const moved = buildWheelLines([
    { key: "x", kind: "user", offsetTop: 0, height: 1 },
    { key: "a", kind: "user", offsetTop: 900, height: 100 }
  ], { scrollHeight: 1000, trackHeight: 200, minLine: 2, maxLine: 30 }).lines;
  const movedAgain = buildWheelLines([
    { key: "x", kind: "user", offsetTop: 0, height: 1 },
    { key: "a", kind: "user", offsetTop: 800, height: 200 }
  ], { scrollHeight: 1000, trackHeight: 200, minLine: 2, maxLine: 30 }).lines;
  assert.equal(wheelSignature(first), wheelSignature(same));
  assert.notEqual(wheelSignature(first), wheelSignature(moved));
  assert.notEqual(wheelSignature(moved), wheelSignature(movedAgain));
});

// ── 最小 DOM 替身（只覆盖轮轴用到的接口） ──────────────────────────────
function stubClassList() {
  const set = new Set();
  return {
    add: name => set.add(name),
    remove: name => set.delete(name),
    contains: name => set.has(name),
    toggle(name, force) {
      const next = force === undefined ? !set.has(name) : Boolean(force);
      if (next) set.add(name);
      else set.delete(name);
      return next;
    },
    get value() { return [...set].join(" "); }
  };
}

function stubElement(tag = "div", className = "") {
  const node = {
    tagName: tag.toUpperCase(),
    children: [],
    dataset: {},
    style: {},
    attributes: {},
    parentElement: null,
    textContent: "",
    rect: { top: 0, left: 0, width: 0, height: 0 },
    focused: false,
    appendChild(child) {
      if (child.parentElement) {
        const index = child.parentElement.children.indexOf(child);
        if (index >= 0) child.parentElement.children.splice(index, 1);
      }
      child.parentElement = node;
      node.children.push(child);
      return child;
    },
    append(...items) { for (const item of items) node.appendChild(item); },
    setAttribute(name, value) { node.attributes[name] = String(value); },
    addEventListener() {},
    remove() {
      if (!node.parentElement) return;
      const index = node.parentElement.children.indexOf(node);
      if (index >= 0) node.parentElement.children.splice(index, 1);
      node.parentElement = null;
    },
    querySelector() { return null; },
    closest() { return null; },
    focus() { node.focused = true; },
    getBoundingClientRect() {
      if (node.classList.contains("wheel-track")) return { top: 0, left: 0, width: 26, height: 400, right: 26, bottom: 400 };
      return node.rect;
    }
  };
  node.classList = stubClassList();
  Object.defineProperty(node, "className", {
    get: () => node.classList.value,
    set: value => {
      node.classList = stubClassList();
      for (const name of String(value).split(/\s+/).filter(Boolean)) node.classList.add(name);
    }
  });
  if (className) node.className = className;
  return node;
}

function installDom() {
  const previous = { document: globalThis.document, CSS: globalThis.CSS, Element: globalThis.Element };
  globalThis.document = { createElement: tag => stubElement(tag) };
  globalThis.CSS = { escape: value => String(value) };
  globalThis.Element = Object;
  return () => Object.assign(globalThis, previous);
}

test("wheel: component mounts on the shell and renders lines from measured geometry", async () => {
  const restore = installDom();
  try {
    const { createConversationWheel } = await import("./conversation-wheel.js");
    const shell = stubElement("div", "conversation-shell");
    const container = stubElement("div", "conversation");
    shell.appendChild(container);
    container.scrollHeight = 1000;
    container.clientHeight = 250;
    container.scrollTop = 0;
    const rows = [
      ["message:1", "user", "你 · 目标", 0, 100],
      ["roll:tool:a", "tools", "工具过程 · 3 次", 100, 600],
      ["message:2", "agent", "Seelex · 结论", 700, 300]
    ];
    for (const [key, kind, label, top, height] of rows) {
      const node = stubElement("article");
      node.dataset.conversationKey = key;
      node.dataset.wheelKind = kind;
      node.dataset.wheelLabel = label;
      node.rect = { top, left: 0, width: 800, height };
      container.appendChild(node);
    }

    const wheel = createConversationWheel(container);
    const rail = shell.children.find(child => child.classList.contains("conversation-wheel"));
    assert.ok(rail, "轮轴没有挂到外壳上");
    const track = rail.children[0];
    assert.ok(track.classList.contains("wheel-track"));

    const result = wheel.refresh();
    assert.equal(result.empty, false);
    assert.equal(rail.classList.contains("is-empty"), false);
    const lines = track.children.filter(child => child.classList.contains("wheel-line"));
    assert.equal(lines.length, 3);
    // 线高与位置来自几何（轨道 400px：比例 0.4，上限 48px）。
    assert.equal(lines[0].style.top, "0px");
    assert.equal(lines[0].style.height, "40px");
    assert.equal(lines[1].style.height, "48px");
    assert.equal(lines[2].style.top, "280px");
    assert.equal(lines[2].style.height, "48px");
    assert.equal(lines[0].dataset.wheelKind, "user");
    assert.equal(lines[1].attributes["aria-label"], "工具过程 · 3 次");

    const viewport = track.children.find(child => child.classList.contains("wheel-viewport"));
    assert.equal(viewport.style.height, "100px");
    assert.equal(viewport.style.top, "0px");
    assert.equal(track.attributes["aria-valuenow"], "0");

    // 滚动后滑柄跟随（250/1000 视口 -> 100px 高，底部时 top = 300）。
    container.scrollTop = 750;
    wheel.updateViewport();
    assert.equal(viewport.style.top, "300px");
    assert.equal(track.attributes["aria-valuenow"], "100");
  } finally {
    restore();
  }
});

test("wheel: component hides itself when there is nothing to scroll", async () => {
  const restore = installDom();
  try {
    const { createConversationWheel } = await import("./conversation-wheel.js");
    const shell = stubElement("div", "conversation-shell");
    const container = stubElement("div", "conversation");
    shell.appendChild(container);
    container.scrollHeight = 300;
    container.clientHeight = 300;
    const wheel = createConversationWheel(container);
    const result = wheel.refresh();
    const rail = shell.children.find(child => child.classList.contains("conversation-wheel"));
    assert.equal(result.empty, true);
    assert.equal(rail.classList.contains("is-empty"), true);
  } finally {
    restore();
  }
});
