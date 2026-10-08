import test from "node:test";
import assert from "node:assert/strict";
import { WINDOW_TREATMENT_OPAQUE, resolveWindowTreatment } from "./window-treatment.js";

// 责任链：只有登记在链上的材质皮肤走非默认，其余一律 opaque——"加一套皮肤不改窗口"
// 这条底线，就靠这些断言守住。
test("window-treatment: 只有毛玻璃皮肤要亚克力窗口", () => {
  assert.equal(resolveWindowTreatment("glass"), "acrylic");
});

test("window-treatment: 其余皮肤（含未知/空值）一律不透明", () => {
  for (const skin of ["qoder", "graphite", "verdigris", "paper", "silver"]) {
    assert.equal(resolveWindowTreatment(skin), WINDOW_TREATMENT_OPAQUE, skin);
  }
  assert.equal(resolveWindowTreatment("does-not-exist"), WINDOW_TREATMENT_OPAQUE);
  assert.equal(resolveWindowTreatment(""), WINDOW_TREATMENT_OPAQUE);
  assert.equal(resolveWindowTreatment(null), WINDOW_TREATMENT_OPAQUE);
  assert.equal(resolveWindowTreatment(undefined), WINDOW_TREATMENT_OPAQUE);
});

test("window-treatment: 归一化输入（大小写/空白）", () => {
  assert.equal(resolveWindowTreatment("  Glass  "), "acrylic");
  assert.equal(resolveWindowTreatment("GLASS"), "acrylic");
});

test("window-treatment: 责任链第一个认领的胜出", () => {
  const chain = [
    { id: "second", skins: ["glass"], treatment: "mica" },
    { id: "first", skins: ["glass"], treatment: "acrylic" }
  ];
  assert.equal(resolveWindowTreatment("glass", chain), "mica");
  // 链上没人认领 → 默认
  assert.equal(resolveWindowTreatment("glass", [{ skins: ["other"], treatment: "mica" }]), WINDOW_TREATMENT_OPAQUE);
  // 畸形节点（没有 skins）不能把链路带崩
  assert.equal(resolveWindowTreatment("glass", [null, { id: "x" }, { skins: ["glass"], treatment: "acrylic" }]), "acrylic");
});
