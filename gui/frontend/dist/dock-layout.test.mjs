import test from "node:test";
import assert from "node:assert/strict";
import {
  ALL_VIEWS,
  DEFAULT_DOCK_STATE,
  normalizeDockState,
  regionOf,
  swapViews,
  isViewActive
} from "./dock-layout.js";

function defaultState() {
  return normalizeDockState(null);
}

test("dock: default layout partitions all five views and activates defaults", () => {
  const dock = defaultState();
  assert.deepEqual(dock.layout.main, ["conversation", "trajectory"]);
  assert.deepEqual(dock.layout.right, ["status", "workbench", "code"]);
  assert.equal(dock.active.main, "conversation");
  assert.equal(dock.active.right, "status");
  const all = [...dock.layout.main, ...dock.layout.right];
  assert.equal(all.length, ALL_VIEWS.length);
  assert.deepEqual([...new Set(all)].sort(), [...ALL_VIEWS].sort());
});

test("dock: cross-region swap places dragged view into target slot and swaps actives", () => {
  const next = swapViews(defaultState(), "right", "workbench", "main", "trajectory");
  assert.deepEqual(next.layout.main, ["conversation", "workbench"]);
  assert.deepEqual(next.layout.right, ["status", "trajectory", "code"]);
  assert.equal(next.active.main, "workbench");
  assert.equal(next.active.right, "trajectory");
  assert.equal(regionOf(next.layout, "workbench"), "main");
  assert.equal(regionOf(next.layout, "trajectory"), "right");
});

test("dock: same-region drag reorders tabs and keeps active view id", () => {
  const base = defaultState();
  base.active.main = "trajectory";
  const next = swapViews(base, "main", "trajectory", "main", "conversation");
  assert.deepEqual(next.layout.main, ["trajectory", "conversation"]);
  assert.equal(next.active.main, "trajectory");
  assert.equal(isViewActive(next, "trajectory"), true);
});

test("dock: reverse swap from main back to right restores original membership", () => {
  const moved = swapViews(defaultState(), "right", "workbench", "main", "trajectory");
  const restored = swapViews(moved, "main", "workbench", "right", "trajectory");
  assert.deepEqual(restored.layout.main, ["conversation", "trajectory"]);
  assert.deepEqual(restored.layout.right, ["status", "workbench", "code"]);
  assert.equal(restored.active.main, "trajectory");
  assert.equal(restored.active.right, "workbench");
});

test("dock: swap on self or with missing views returns unchanged state", () => {
  const base = defaultState();
  assert.equal(swapViews(base, "right", "workbench", "right", "workbench"), base);
  assert.equal(swapViews(base, "right", "workbench", "main", "nope"), base);
  assert.equal(swapViews(base, "right", "nope", "main", "conversation"), base);
  assert.equal(swapViews(base, "nowhere", "workbench", "main", "conversation"), base);
  assert.deepEqual(base.layout.main, ["conversation", "trajectory"]);
  assert.deepEqual(base.layout.right, ["status", "workbench", "code"]);
});

test("dock: normalize repairs duplicate, unknown and missing views", () => {
  const dock = normalizeDockState({
    layout: {
      main: ["conversation", "conversation", "bogus"],
      right: ["status"]
    },
    active: { main: "bogus", right: "status" }
  });
  const all = [...dock.layout.main, ...dock.layout.right];
  assert.equal(all.length, ALL_VIEWS.length);
  assert.equal(new Set(all).size, ALL_VIEWS.length);
  assert.equal(ALL_VIEWS.every(view => all.includes(view)), true);
  assert.equal(dock.active.main, "conversation");
  assert.equal(dock.active.right, "status");
});

test("dock: normalize keeps valid persisted arrangement and clamps bad active", () => {
  const dock = normalizeDockState({
    layout: { main: ["code", "conversation"], right: ["status", "workbench", "trajectory"] },
    active: { main: "code", right: "status" }
  });
  assert.deepEqual(dock.layout.main, ["code", "conversation"]);
  assert.deepEqual(dock.layout.right, ["status", "workbench", "trajectory"]);
  assert.equal(dock.active.main, "code");
  const clamped = normalizeDockState({
    layout: { main: ["code", "conversation"], right: ["status", "workbench", "trajectory"] },
    active: { main: "trajectory", right: "code" }
  });
  assert.equal(clamped.active.main, "conversation");
  assert.equal(clamped.active.right, "status");
});

test("dock: storage round-trip survives JSON and normalize", () => {
  const dock = swapViews(defaultState(), "right", "code", "main", "conversation");
  const restored = normalizeDockState(JSON.parse(JSON.stringify(dock)));
  assert.deepEqual(restored, dock);
  assert.equal(restored.active.main, "code");
  assert.equal(restored.active.right, "conversation");
});

test("dock: default constants are frozen and never mutated by swap", () => {
  assert.equal(Object.isFrozen(DEFAULT_DOCK_STATE.layout.main), true);
  const before = JSON.stringify(DEFAULT_DOCK_STATE);
  swapViews(defaultState(), "right", "workbench", "main", "trajectory");
  assert.equal(JSON.stringify(DEFAULT_DOCK_STATE), before);
});
