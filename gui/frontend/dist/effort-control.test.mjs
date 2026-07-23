import test from "node:test";
import assert from "node:assert/strict";
import { EFFORT_LEVELS, createEffortControl, effortPresentation } from "./effort-control.js";

class FakeClassList {
  constructor() { this.values = new Set(); }
  add(value) { this.values.add(value); }
  remove(value) { this.values.delete(value); }
  contains(value) { return this.values.has(value); }
}

class FakeElement {
  constructor(value = "0") {
    this.value = value;
    this.textContent = "";
    this.disabled = false;
    this.dataset = {};
    this.attributes = new Map();
    this.listeners = new Map();
    this.classList = new FakeClassList();
    this.style = { values: new Map(), setProperty: (key, next) => this.style.values.set(key, next) };
  }
  setAttribute(key, value) { this.attributes.set(key, value); }
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
  assert.deepEqual(EFFORT_LEVELS.map(level => effortPresentation(level).progress), [0, 33, 67, 100]);
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
