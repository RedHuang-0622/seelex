import test from "node:test";
import assert from "node:assert/strict";
import { EFFORT_LEVELS, createEffortControl, effortPresentation } from "./effort-control.js";

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

function setup(selectEffort = async () => {}, onError = () => {}) {
  const root = new FakeElement();
  const input = new FakeElement();
  const output = new FakeElement("");
  const control = createEffortControl({ root, input, output, selectEffort, onError });
  return { control, root, input, output };
}

test("maps four Effort levels onto discrete progress values", () => {
  assert.deepEqual(EFFORT_LEVELS, ["lite", "medium", "high", "max"]);
  assert.deepEqual(EFFORT_LEVELS.map(level => effortPresentation(level).progress), [10, 38, 66, 100]);
  assert.equal(effortPresentation("max").isMax, true);
  assert.equal(effortPresentation("unknown").level, "lite");
});

test("renders authoritative runtime state including Max aura selector", () => {
  const { control, root, input, output } = setup();
  control.setLevel("max");
  assert.equal(input.value, "3");
  assert.equal(input.attributes.get("aria-valuetext"), "Max");
  assert.equal(output.textContent, "Max");
  assert.equal(root.dataset.effort, "max");
  assert.equal(root.style.values.get("--effort-progress"), "100%");
});

test("previews while dragging and commits only on change", async () => {
  const selected = [];
  const { root, input } = setup(async level => selected.push(level));
  input.value = "2";
  input.dispatch("input");
  assert.equal(root.dataset.effort, "high");
  assert.deepEqual(selected, []);
  await input.dispatch("change");
  assert.deepEqual(selected, ["high"]);
  assert.equal(input.disabled, false);
  assert.equal(root.classList.contains("is-pending"), false);
});

test("rolls back to committed level when Bridge selection fails", async () => {
  const failure = new Error("bridge failed");
  const errors = [];
  const { control, root, input } = setup(async () => { throw failure; }, error => errors.push(error));
  control.setLevel("medium");
  input.value = "3";
  input.dispatch("input");
  await input.dispatch("change");
  assert.equal(root.dataset.effort, "medium");
  assert.equal(input.value, "1");
  assert.deepEqual(errors, [failure]);
});

test("ignores unknown snapshot levels instead of snapping back to Lite", () => {
  const { control, root, input } = setup();
  control.setLevel("high");
  assert.equal(root.dataset.effort, "high");
  // 快照缺 effort / 跨会话为空值：必须保持当前档位（否则用户会看到滑块自己跳回 Lite）。
  control.setLevel("");
  control.setLevel(undefined);
  control.setLevel("bogus");
  assert.equal(root.dataset.effort, "high");
  assert.equal(input.value, "2");
});

test("locks the slider while a chat turn is running", async () => {
  const selected = [];
  const { control, root, input, output } = setup(async level => selected.push(level));
  control.setLevel("high");
  control.setEnabled(false);
  assert.equal(input.disabled, true);
  assert.equal(root.classList.contains("is-locked"), true);
  assert.equal(output.title.length > 0, true);

  // 锁定期内的拖动不得预览、不得提交（后端一定会拒，前端不该先答应）。
  input.value = "3";
  input.dispatch("input");
  assert.equal(root.dataset.effort, "high");
  await input.dispatch("change");
  assert.deepEqual(selected, []);
  assert.equal(root.dataset.effort, "high");

  control.setEnabled(true);
  assert.equal(input.disabled, false);
  await input.dispatch("change");
  assert.deepEqual(selected, ["max"]);
});

test("adopts the authoritative level returned by the bridge", async () => {
  // 请求 max，后端归一化/降级成 medium → UI 必须显示真正生效的值。
  const { root, input } = setup(async () => "medium");
  input.value = "3";
  input.dispatch("input");
  await input.dispatch("change");
  assert.equal(root.dataset.effort, "medium");
  assert.equal(input.value, "1");
});
