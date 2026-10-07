// 共享滑杆组件的行为测试：三条拉条（Effort 档位 / 推理强度 / 循环次数）共用这一份
// 实现，所以这里钉住的正是三条拉条共同的行为契约。
import test from "node:test";
import assert from "node:assert/strict";
import { createKnobSlider, discreteScale, linearScale } from "./knob-slider.js";

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

const LOOP_SCALE = linearScale({ min: 1, max: 9999, step: 1 });
const EFFORT_SCALE = discreteScale(["low", "medium", "high", "max"], {
  labels: { low: "Low", medium: "Medium", high: "High", max: "Max" },
  progress: [15, 45, 75, 100]
});

function setup(scale, commit, onError = () => {}) {
  const root = new FakeElement();
  const input = new FakeElement();
  const output = new FakeElement("");
  const slider = createKnobSlider({ root, input, output, scale, commit, onError });
  return { slider, root, input, output };
}

test("linearScale：index ↔ value 是增长量恒定 1 的线性映射", () => {
  assert.equal(LOOP_SCALE.length, 9999);
  assert.equal(LOOP_SCALE.valueAt(0), 1);
  assert.equal(LOOP_SCALE.valueAt(1), 2);
  assert.equal(LOOP_SCALE.valueAt(383), 384); // high 档默认值
  assert.equal(LOOP_SCALE.indexOf(384), 383);
  // 不可信读数一律 -1（调用方据此忽略，而不是把拉条打回 0）。
  assert.equal(LOOP_SCALE.indexOf(0), -1);
  assert.equal(LOOP_SCALE.indexOf(10000), -1);
  assert.equal(LOOP_SCALE.indexOf(1.5), -1);
  assert.equal(LOOP_SCALE.indexOf("bogus"), -1);
  assert.equal(LOOP_SCALE.labelAt(0), "1");
});

test("discreteScale：停位、标签与进度按表给", () => {
  assert.equal(EFFORT_SCALE.length, 4);
  assert.equal(EFFORT_SCALE.valueAt(3), "max");
  assert.equal(EFFORT_SCALE.labelAt(3), "Max");
  assert.equal(EFFORT_SCALE.progressAt(0), 15);
  assert.equal(EFFORT_SCALE.indexOf("bogus"), -1);
});

test("拖动只预览、松手才提交一次", async () => {
  const selected = [];
  const { root, input, output } = setup(EFFORT_SCALE, async value => { selected.push(value); });
  input.value = "2";
  input.dispatch("input");
  assert.equal(root.dataset.effort, "high");
  assert.equal(output.textContent, "High");
  assert.deepEqual(selected, []);
  await input.dispatch("change");
  assert.deepEqual(selected, ["high"]);
  assert.equal(input.disabled, false);
  assert.equal(root.classList.contains("is-pending"), false);
});

test("采纳后端真正生效的值（不是请求值）", async () => {
  const { root, input } = setup(EFFORT_SCALE, async () => "medium");
  input.value = "3";
  input.dispatch("input");
  await input.dispatch("change");
  assert.equal(root.dataset.effort, "medium");
  assert.equal(input.value, "1");
});

test("后端返回无法识别的值时沿用请求值（而不是打回 0）", async () => {
  const { root } = setup(EFFORT_SCALE, async () => "bogus");
  const { root: root2, input: input2 } = setup(EFFORT_SCALE, async () => "bogus");
  input2.value = "3";
  input2.dispatch("input");
  await input2.dispatch("change");
  assert.equal(root2.dataset.effort, "max");
  assert.equal(root.dataset.effort, "low");
});

test("提交失败回滚到权威值并把错误交出去", async () => {
  const failure = new Error("bridge failed");
  const errors = [];
  const { slider, root, input } = setup(EFFORT_SCALE, async () => { throw failure; }, error => errors.push(error));
  slider.set("medium");
  input.value = "3";
  input.dispatch("input");
  await input.dispatch("change");
  assert.equal(root.dataset.effort, "medium");
  assert.equal(input.value, "1");
  assert.deepEqual(errors, [failure]);
});

test("运行中上锁：既不预览也不提交", async () => {
  const selected = [];
  const { slider, root, input, output } = setup(EFFORT_SCALE, async value => { selected.push(value); });
  slider.set("high");
  slider.setEnabled(false);
  assert.equal(input.disabled, true);
  assert.equal(root.classList.contains("is-locked"), true);
  assert.equal(output.title.length > 0, true);

  input.value = "3";
  input.dispatch("input");
  assert.equal(root.dataset.effort, "high");
  await input.dispatch("change");
  assert.deepEqual(selected, []);

  slider.setEnabled(true);
  assert.equal(input.disabled, false);
  await input.dispatch("change");
  assert.deepEqual(selected, ["max"]);
});

test("快照覆盖只认本表认识的值（缺字段时不打回 0）", () => {
  const { slider, root, input } = setup(LOOP_SCALE, async () => {});
  slider.set(384);
  assert.equal(root.dataset.effort, "384");
  assert.equal(LOOP_SCALE.indexOf(384), Number(input.value));
  slider.set(undefined);
  slider.set("");
  slider.set(-5);
  assert.equal(root.dataset.effort, "384");
});

test("构造时把 range 的量纲钉成 index 0..N", () => {
  const { input } = setup(LOOP_SCALE, async () => {});
  assert.equal(input.min, "0");
  assert.equal(input.max, "9998");
  assert.equal(input.step, "1");
});

test("缺少必需入参就直接拒绝（不静默退化成不能拖的控件）", () => {
  assert.throws(() => createKnobSlider({ root: new FakeElement(), input: new FakeElement() }), TypeError);
});
