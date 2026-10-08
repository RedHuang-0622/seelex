// composer 运行参数的装配测试：思考强度（贴发送键）与循环次数（运行状态弹窗，含
// 「无限循环」勾选）各自只改一项，锁是一把（后端运行守卫对两者是同一条）。
import test from "node:test";
import assert from "node:assert/strict";
import {
  createComposerKnobs,
  isInfiniteLoop,
  LOOP_LIMIT_INFINITE,
  LOOP_LIMIT_MAX,
  REASONING_EFFORT_LEVELS,
  loopLimitScale
} from "./composer-knobs.js";

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
    this.checked = false;
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
  const infinite = { checkbox: new FakeElement() };
  const knobs = createComposerKnobs({ reasoning, loop, infinite, setReasoningEffort, setLoopLimit, onError });
  return { knobs, reasoning, loop, infinite };
}

test("循环次数的停位表：有限区间 1..768（max 档默认 loops）", () => {
  assert.deepEqual([...REASONING_EFFORT_LEVELS], ["low", "medium", "high", "max"]);
  assert.equal(LOOP_LIMIT_MAX, 768);
  assert.equal(LOOP_LIMIT_INFINITE, 9999);
  const scale = loopLimitScale();
  assert.equal(scale.valueAt(0), 1);
  assert.equal(scale.valueAt(scale.length - 1), LOOP_LIMIT_MAX);
  assert.equal(scale.length, LOOP_LIMIT_MAX);
  assert.equal(scale.step, 1);
});

test("isInfiniteLoop 只认哨兵（>= 9999），有限区间内一律不是", () => {
  assert.equal(isInfiniteLoop(9999), true);
  assert.equal(isInfiniteLoop(10000), true);
  assert.equal(isInfiniteLoop(768), false);
  assert.equal(isInfiniteLoop(undefined), false);
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

test("快照读数同时铺到两处（档位切换 = 两条一起被预设覆盖）", () => {
  const { knobs, reasoning, loop, infinite } = setup({});
  knobs.setReasoningEffort("high");
  knobs.setLoopLimit(768);
  assert.equal(knobs.reasoningEffort(), "high");
  assert.equal(knobs.loopLimit(), 768);
  assert.equal(reasoning.root.dataset.effort, "high");
  assert.equal(loop.root.dataset.effort, "768");
  assert.equal(reasoning.output.textContent, "High");
  assert.equal(loop.output.textContent, "768");
  assert.equal(infinite.checkbox.checked, false);
});

test("勾选「无限循环」下发哨兵 9999：滑杆停用、读数显示 ∞", async () => {
  const calls = [];
  const { knobs, loop, infinite } = setup({
    setLoopLimit: async value => { calls.push(["loop", value]); return value; }
  });
  knobs.setLoopLimit(384);
  infinite.checkbox.checked = true;
  await infinite.checkbox.dispatch("change");
  assert.deepEqual(calls, [["loop", LOOP_LIMIT_INFINITE]]);
  assert.equal(knobs.isInfinite(), true);
  assert.equal(knobs.loopLimit(), LOOP_LIMIT_INFINITE);
  assert.equal(loop.input.disabled, true);
  assert.equal(loop.output.textContent, "∞");
  assert.equal(infinite.checkbox.checked, true);
  assert.equal(infinite.checkbox.disabled, false);
});

test("取消「无限循环」回到最后一份有限值", async () => {
  const calls = [];
  const { knobs, loop, infinite } = setup({
    setLoopLimit: async value => { calls.push(["loop", value]); return value; }
  });
  knobs.setLoopLimit(384);
  infinite.checkbox.checked = true;
  await infinite.checkbox.dispatch("change");
  infinite.checkbox.checked = false;
  await infinite.checkbox.dispatch("change");
  assert.deepEqual(calls, [["loop", LOOP_LIMIT_INFINITE], ["loop", 384]]);
  assert.equal(knobs.isInfinite(), false);
  assert.equal(knobs.loopLimit(), 384);
  assert.equal(loop.input.disabled, false);
  assert.equal(loop.output.textContent, "384");
});

test("快照给出哨兵读数时勾选态与 ∞ 一起成立，且不打回 0", () => {
  const { knobs, loop, infinite } = setup({});
  knobs.setLoopLimit(LOOP_LIMIT_INFINITE);
  assert.equal(knobs.isInfinite(), true);
  assert.equal(infinite.checkbox.checked, true);
  assert.equal(loop.output.textContent, "∞");
  // 有限读数回来时取消勾选，且拉条按新值落位。
  knobs.setLoopLimit(448);
  assert.equal(knobs.isInfinite(), false);
  assert.equal(infinite.checkbox.checked, false);
  assert.equal(knobs.loopLimit(), 448);
});

test("运行中把两处一起上锁（后端对两者是同一条守卫）", async () => {
  const calls = [];
  const { knobs, reasoning, loop, infinite } = setup({
    setReasoningEffort: async value => { calls.push(["effort", value]); return value; },
    setLoopLimit: async value => { calls.push(["loop", value]); return value; }
  });
  knobs.setReasoningEffort("medium");
  knobs.setLoopLimit(48);
  knobs.setEnabled(false);
  assert.equal(reasoning.input.disabled, true);
  assert.equal(loop.input.disabled, true);
  assert.equal(infinite.checkbox.disabled, true);
  reasoning.input.value = "3";
  await reasoning.input.dispatch("change");
  loop.input.value = "599";
  await loop.input.dispatch("change");
  infinite.checkbox.checked = true;
  await infinite.checkbox.dispatch("change");
  assert.deepEqual(calls, []);
  assert.equal(knobs.reasoningEffort(), "medium");
  assert.equal(knobs.loopLimit(), 48);
  assert.equal(knobs.isInfinite(), false);
  assert.equal(infinite.checkbox.checked, false, "锁定时勾选不许留在界面上");

  knobs.setEnabled(true);
  assert.equal(knobs.isEnabled(), true);
  assert.equal(infinite.checkbox.disabled, false);
});

test("后端把值归一化时以返回值渲染（请求 600 返回 384 → 显示 384）", async () => {
  const { knobs, loop } = setup({ setLoopLimit: async () => 384 });
  loop.input.value = "599";
  loop.input.dispatch("input");
  await loop.input.dispatch("change");
  assert.equal(knobs.loopLimit(), 384);
  assert.equal(loop.input.value, "383");
});

test("「无限循环」提交失败时勾选回滚到后端真值", async () => {
  const errors = [];
  const { knobs, infinite } = setup({
    setLoopLimit: async () => { throw new Error("bridge failed"); },
    onError: error => errors.push(error)
  });
  infinite.checkbox.checked = true;
  await infinite.checkbox.dispatch("change");
  assert.equal(errors.length, 1);
  assert.equal(knobs.isInfinite(), false);
  assert.equal(infinite.checkbox.checked, false);
});
