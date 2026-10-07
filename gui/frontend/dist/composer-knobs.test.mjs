// composer 底部两条独立拉条的装配测试：它们各自只改一项，锁是一把（后端运行守卫
// 对两条是同一条）。
import test from "node:test";
import assert from "node:assert/strict";
import { createComposerKnobs, LOOP_LIMIT_MAX, REASONING_EFFORT_LEVELS, loopLimitScale } from "./composer-knobs.js";

class FakeClassList {
  constructor() { this.values = new Set(); }
  add(value) { this.values.add(value); }
  remove(value) { this.values.delete(value); }
  contains(value) { return this.values.has(value); }
  toggle(value, force) {
    const next = force === undefined ? !this.values.has(value) : Boolean(force);
    if (next) this.values.add(value);
    else this.values.delete(value);
    return next;
  }
}

class FakeElement {
  constructor(value = "0") {
    this.value = value;
    this.textContent = "";
    this.title = "";
    this.disabled = false;
    this.dataset = {};
    this.attributes = new Map();
    this.listeners = new Map();
    this.classList = new FakeClassList();
    this.style = { values: new Map(), setProperty: (key, next) => this.style.values.set(key, next) };
  }
  setAttribute(key, value) { this.attributes.set(key, value); }
  removeAttribute(key) { this.attributes.delete(key); }
  addEventListener(type, listener) { this.listeners.set(type, listener); }
  dispatch(type) { return this.listeners.get(type)?.(); }
}

function setup({ setReasoningEffort, setLoopLimit, onError } = {}) {
  const reasoning = { root: new FakeElement(), input: new FakeElement(), output: new FakeElement("") };
  const loop = { root: new FakeElement(), input: new FakeElement(), output: new FakeElement("") };
  const knobs = createComposerKnobs({ reasoning, loop, setReasoningEffort, setLoopLimit, onError });
  return { knobs, reasoning, loop };
}

test("两条拉条的停位表：推理强度是四档词表，循环次数是 1..9999", () => {
  assert.deepEqual([...REASONING_EFFORT_LEVELS], ["low", "medium", "high", "max"]);
  const scale = loopLimitScale();
  assert.equal(scale.valueAt(0), 1);
  assert.equal(scale.valueAt(scale.length - 1), LOOP_LIMIT_MAX);
  assert.equal(scale.step, 1);
});

test("拖推理强度只调 SetReasoningEffort，循环次数一个字都不动", async () => {
  const calls = [];
  const { knobs, reasoning } = setup({
    setReasoningEffort: async value => { calls.push(["effort", value]); return value; },
    setLoopLimit: async value => { calls.push(["loop", value]); return value; }
  });
  knobs.setLoopLimit(600);
  reasoning.input.value = "3";
  reasoning.input.dispatch("input");
  await reasoning.input.dispatch("change");
  assert.deepEqual(calls, [["effort", "max"]]);
  assert.equal(knobs.loopLimit(), 600);
  assert.equal(reasoning.root.dataset.effort, "max");
});

test("拖循环次数只调 SetLoopLimit（值就是拉条位置上的整数）", async () => {
  const calls = [];
  const { knobs, loop } = setup({
    setReasoningEffort: async value => { calls.push(["effort", value]); return value; },
    setLoopLimit: async value => { calls.push(["loop", value]); return value; }
  });
  loop.input.value = "599"; // index 599 → 值 600
  loop.input.dispatch("input");
  await loop.input.dispatch("change");
  assert.deepEqual(calls, [["loop", 600]]);
  assert.equal(knobs.loopLimit(), 600);
  assert.equal(loop.root.dataset.effort, "600");
});

test("快照读数同时铺到两条拉条上（档位切换 = 两条一起被预设覆盖）", () => {
  const { knobs, reasoning, loop } = setup({});
  knobs.setReasoningEffort("high");
  knobs.setLoopLimit(768);
  assert.equal(knobs.reasoningEffort(), "high");
  assert.equal(knobs.loopLimit(), 768);
  assert.equal(reasoning.root.dataset.effort, "high");
  assert.equal(loop.root.dataset.effort, "768");
  assert.equal(reasoning.output.textContent, "High");
  assert.equal(loop.output.textContent, "768");
});

test("运行中把两条一起上锁（后端对两者是同一条守卫）", async () => {
  const calls = [];
  const { knobs, reasoning, loop } = setup({
    setReasoningEffort: async value => { calls.push(["effort", value]); return value; },
    setLoopLimit: async value => { calls.push(["loop", value]); return value; }
  });
  knobs.setReasoningEffort("medium");
  knobs.setLoopLimit(48);
  knobs.setEnabled(false);
  assert.equal(reasoning.input.disabled, true);
  assert.equal(loop.input.disabled, true);
  reasoning.input.value = "3";
  await reasoning.input.dispatch("change");
  loop.input.value = "599";
  await loop.input.dispatch("change");
  assert.deepEqual(calls, []);
  assert.equal(knobs.reasoningEffort(), "medium");
  assert.equal(knobs.loopLimit(), 48);

  knobs.setEnabled(true);
  assert.equal(knobs.isEnabled(), true);
});

test("后端把值归一化时以返回值渲染（请求 600 返回 384 → 显示 384）", async () => {
  const { knobs, loop } = setup({ setLoopLimit: async () => 384 });
  loop.input.value = "599";
  loop.input.dispatch("input");
  await loop.input.dispatch("change");
  assert.equal(knobs.loopLimit(), 384);
  assert.equal(loop.input.value, "383");
});
