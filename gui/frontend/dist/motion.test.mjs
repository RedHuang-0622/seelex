import assert from "node:assert/strict";
import test from "node:test";

import {
  bindHorizontalDragDelegate,
  bindHorizontalWheelDelegate,
  countParts,
  markEntering,
  prefersReducedMotion,
  rollNumber,
  scrollEdges,
  syncScrollEdges,
  tweenValue,
  wheelScrollDelta
} from "./motion.js";

function makeElement({ text = "", classes = [] } = {}) {
  const set = new Set(classes);
  return {
    textContent: text,
    classList: {
      add: (...names) => names.forEach(name => set.add(name)),
      remove: (...names) => names.forEach(name => set.delete(name)),
      contains: name => set.has(name),
      toggle: (name, force) => {
        const on = force === undefined ? !set.has(name) : Boolean(force);
        if (on) set.add(name); else set.delete(name);
        return on;
      }
    },
    _classes: set
  };
}

test("countParts：只有一个整数段才可滚动，前缀/后缀原样保留", () => {
  assert.deepEqual(countParts("12 项"), { prefix: "", value: 12, suffix: " 项" });
  assert.deepEqual(countParts("3 未读"), { prefix: "", value: 3, suffix: " 未读" });
  assert.deepEqual(countParts("7"), { prefix: "", value: 7, suffix: "" });
  assert.deepEqual(countParts("RETRY 2"), { prefix: "RETRY ", value: 2, suffix: "" });
  // 多段整数 / 无整数 → 不滚动（退回直接赋值）。
  assert.equal(countParts("1 / 2 页 · 12 项"), null);
  assert.equal(countParts("—"), null);
  assert.equal(countParts(""), null);
  assert.equal(countParts(null), null);
});

test("tweenValue：端点精确、单调、非法输入退化到终值", () => {
  assert.equal(tweenValue(0, 10, 0), 0);
  assert.equal(tweenValue(0, 10, 1), 10);
  assert.equal(tweenValue(10, 0, 1), 0);
  let previous = -1;
  for (let step = 0; step <= 10; step += 1) {
    const value = tweenValue(4, 40, step / 10);
    assert.ok(value >= previous, `第 ${step} 步应单调不减`);
    previous = value;
  }
  assert.equal(tweenValue(undefined, 5, 0.5), 5);
  assert.equal(tweenValue(3, undefined, 0.5), null);
  assert.equal(tweenValue(0, 10, 2), 10, "progress 超界按 1 处理");
});

test("rollNumber：无 rAF（node）时直接落终值，不抛错也不留半途值", () => {
  const element = makeElement({ text: "2 项" });
  assert.equal(rollNumber(element, "5 项"), false);
  assert.equal(element.textContent, "5 项");
});

test("rollNumber：非同一个计数（前后缀不同）直接落终值", () => {
  const element = makeElement({ text: "2 项" });
  assert.equal(rollNumber(element, "3 打点"), false);
  assert.equal(element.textContent, "3 打点");
});

test("rollNumber：文本不含整数时按原文赋值", () => {
  const element = makeElement({ text: "就绪" });
  assert.equal(rollNumber(element, "执行中"), false);
  assert.equal(element.textContent, "执行中");
});

test("rollNumber：有 rAF 且数值变化时逐帧滚动到终值", () => {
  const frames = [];
  const element = makeElement({ text: "0 项" });
  let time = 0;
  assert.equal(
    rollNumber(element, "10 项", { duration: 100, raf: callback => { frames.push(callback); }, now: () => time }),
    true
  );
  // 第一帧：进度 0 → 起点值。
  frames.shift()();
  assert.equal(element.textContent, "0 项");
  assert.equal(element.classList.contains("is-rolling"), true, "滚动期间带 is-rolling（样式细节）");
  time = 50;
  frames.shift()();
  const mid = Number(countParts(element.textContent).value);
  assert.ok(mid > 0 && mid < 10, `中途应是 0..10 之间的取整值，实际 ${mid}`);
  time = 100;
  frames.shift()();
  assert.equal(element.textContent, "10 项");
  assert.equal(frames.length, 0, "到终值后不再排新帧");
  assert.equal(element.classList.contains("is-rolling"), false, "落终值后摘掉滚动类");
});

test("rollNumber：后一次滚动取消前一次（数字不回跳）", () => {
  const first = [];
  const element = makeElement({ text: "0 项" });
  let time = 0;
  const raf = callback => { first.push(callback); };
  rollNumber(element, "10 项", { duration: 100, raf, now: () => time });
  // 第二次滚动取代第一次：旧帧即使被驱动也不得再写 textContent。
  const second = [];
  rollNumber(element, "20 项", { duration: 100, raf: callback => { second.push(callback); }, now: () => time });
  const stale = first.shift();
  time = 20;
  stale();
  assert.notEqual(element.textContent, "2 项", "旧循环不得把数字写回 10 项的中途值");
});

test("scrollEdges：到边不再显示那侧阴影", () => {
  assert.deepEqual(scrollEdges(0, 100, 300), { start: false, end: true });
  assert.deepEqual(scrollEdges(100, 100, 300), { start: true, end: true });
  assert.deepEqual(scrollEdges(200, 100, 300), { start: true, end: false });
  assert.deepEqual(scrollEdges(0, 300, 300), { start: false, end: false }, "内容不足一屏两侧都不显示");
});

test("syncScrollEdges：纵向挂 up/down，横向挂 start/end", () => {
  const vertical = { classList: makeElement().classList, scrollTop: 40, clientHeight: 100, scrollHeight: 300 };
  syncScrollEdges(vertical, "y");
  assert.equal(vertical.classList.contains("is-scroll-up"), true);
  assert.equal(vertical.classList.contains("is-scroll-down"), true);
  const horizontal = { classList: makeElement().classList, scrollLeft: 0, clientWidth: 100, scrollWidth: 300 };
  syncScrollEdges(horizontal, "x");
  assert.equal(horizontal.classList.contains("is-scroll-start"), false);
  assert.equal(horizontal.classList.contains("is-scroll-end"), true);
});

test("wheelScrollDelta：到边/不足一屏时放行（返回 null），否则原样翻译", () => {
  const strip = { scrollWidth: 400, clientWidth: 100, scrollLeft: 0 };
  assert.equal(wheelScrollDelta(strip, -120), null, "已在最左：向上滚轮放行给外层");
  assert.equal(wheelScrollDelta(strip, 120), 120);
  const narrow = { scrollWidth: 100, clientWidth: 100, scrollLeft: 0 };
  assert.equal(wheelScrollDelta(narrow, 120), null);
  assert.equal(wheelScrollDelta(strip, 0), null);
  assert.equal(wheelScrollDelta(null, 120), null);
});

test("bindHorizontalWheelDelegate：只接管横条上的纵向滚轮", () => {
  const listeners = {};
  const root = { addEventListener: (name, handler) => { listeners[name] = handler; } };
  bindHorizontalWheelDelegate(root);
  const strip = { scrollWidth: 400, clientWidth: 100, scrollLeft: 0, classList: makeElement().classList };
  let prevented = 0;
  const handler = listeners.wheel;
  handler({ target: { closest: () => strip }, deltaY: 120, preventDefault: () => { prevented += 1; } });
  assert.equal(prevented, 1);
  assert.equal(strip.scrollLeft, 120);
  // 非横条目标：放行，不 preventDefault。
  handler({ target: { closest: () => null }, deltaY: 120, preventDefault: () => { prevented += 1; } });
  assert.equal(prevented, 1);
});

test("bindHorizontalDragDelegate：拖动超过阈值才拨动，并吞掉随后的 click", () => {
  const listeners = {};
  const root = {
    addEventListener: (name, handler) => { listeners[name] = handler; },
    removeEventListener: () => {}
  };
  bindHorizontalDragDelegate(root);
  const strip = { scrollWidth: 400, clientWidth: 100, scrollLeft: 10, classList: makeElement().classList, addEventListener: () => {}, removeEventListener: () => {} };
  listeners.pointerdown({ button: 0, pointerId: 1, clientX: 100, target: { closest: selector => (selector === ".scroll-edges-x" ? strip : null) } }, undefined);
  // 未过阈值：不拨动。
  listeners.pointermove({ pointerId: 1, clientX: 102 });
  assert.equal(strip.scrollLeft, 10);
  // 过阈值：横向拨动（向左拖 → 内容右移）。
  listeners.pointermove({ pointerId: 1, clientX: 60 });
  assert.equal(strip.scrollLeft, 50);
  assert.equal(strip.classList.contains("is-drag-scrolling"), true);
  // 松手：按住了才算拖动，类被摘掉。
  listeners.pointerup();
  assert.equal(strip.classList.contains("is-drag-scrolling"), false);
});

test("bindHorizontalDragDelegate：draggable 页签上的手势交给 HTML5 拖拽换序", () => {
  const listeners = {};
  const root = { addEventListener: (name, handler) => { listeners[name] = handler; }, removeEventListener: () => {} };
  bindHorizontalDragDelegate(root);
  const strip = { scrollWidth: 400, clientWidth: 100, scrollLeft: 0, classList: makeElement().classList, addEventListener: () => {}, removeEventListener: () => {} };
  const tab = { closest: selector => (selector === '[draggable="true"]' ? tab : strip) };
  listeners.pointerdown({ button: 0, pointerId: 7, clientX: 100, target: tab });
  listeners.pointermove({ pointerId: 7, clientX: 20 });
  assert.equal(strip.scrollLeft, 0, "draggable 页签上的拖动不得被滚轴接管");
});

test("markEntering：无 classList 直接返回 false；有则挂一次性入场类", () => {
  assert.equal(markEntering(null), false);
  assert.equal(markEntering({}), false);
  const element = makeElement();
  assert.equal(markEntering(element), true);
  assert.equal(element.classList.contains("is-entering"), true);
});

test("prefersReducedMotion：无 window 环境下不抛错", () => {
  assert.equal(typeof prefersReducedMotion(), "boolean");
});
