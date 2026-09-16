// permission-tier.test.mjs — 权限档位（会话粒度）前端口径回归。
//
// 钉住三件事（此前只有 snapshot-shape 的"键归属"断言，档位列表/芯片的渲染口径没人管）：
//   1. 目录唯一来源是后端 runtime.permission_tiers（前端不复制档位名，脏条目被丢弃）；
//   2. 本会话生效档位以 permission_tier 为准，缺失时按旧二元 full_access 回退；
//   3. 列表选项的选中态与顺序、芯片短名（full 档保留中文短名 + isFull 标记）。

import test from "node:test";
import assert from "node:assert/strict";
import {
  currentPermissionTier,
  nextTierIndex,
  permissionTierCatalog,
  permissionTierChip,
  permissionTierChipForRuntime,
  permissionTierMenuItems,
  permissionTierOptions,
} from "./permission-tier.js";

// 与后端 dto.PermissionTiers 同形的目录（测试里手写，正是不想在前端复制一份）。
const CATALOG = [
  { id: "manual", label: "手动", short: "手动", description: "都问人（默认）" },
  { id: "edit", label: "自动改文件", short: "改文件", description: "写文件不再打断" },
  { id: "auto", label: "自动执行", short: "自动", description: "命令直跑" },
  { id: "full", label: "全权", short: "全权", description: "本会话全部放行" },
];

test("catalog comes from the backend projection and drops unusable entries", () => {
  assert.deepEqual(permissionTierCatalog({ permission_tiers: CATALOG }).map(item => item.id), ["manual", "edit", "auto", "full"]);
  // 非数组 / 缺 id 的条目会渲染成点不动的按钮，必须在解析层丢掉。
  assert.deepEqual(permissionTierCatalog({}), []);
  assert.deepEqual(permissionTierCatalog({ permission_tiers: "nope" }), []);
  assert.deepEqual(permissionTierCatalog({ permission_tiers: [null, {}, { label: "缺 id" }] }), []);
});

test("current tier prefers permission_tier and falls back to legacy full_access", () => {
  assert.equal(currentPermissionTier({ permission_tiers: CATALOG, permission_tier: "auto" }).id, "auto");
  // 旧快照（没有 permission_tier）：按 full_access 二元口径回退。
  assert.equal(currentPermissionTier({ permission_tiers: CATALOG, full_access: true }).id, "full");
  assert.equal(currentPermissionTier({ permission_tiers: CATALOG, full_access: false }).id, "manual");
  assert.equal(currentPermissionTier({ permission_tiers: CATALOG }).id, "manual");
  // 目录缺失时合成一条（芯片仍要显示当前档，而不是空串）；短名走芯片的兜底规则。
  assert.equal(currentPermissionTier({ permission_tier: "edit" }).id, "edit");
  assert.equal(permissionTierChipForRuntime({ permission_tier: "edit" }).short, "edit");
});

test("chip model carries the short name and the full marker", () => {
  const full = permissionTierChipForRuntime({ permission_tiers: CATALOG, permission_tier: "full" });
  assert.deepEqual(full, { id: "full", short: "全权", isFull: true });
  const manual = permissionTierChipForRuntime({ permission_tiers: CATALOG, permission_tier: "manual" });
  assert.deepEqual(manual, { id: "manual", short: "手动", isFull: false });
  // 后端回执（乐观回执路径）：目录里没有该档也给出可渲染的短名，full 用中文短名。
  assert.deepEqual(permissionTierChip(CATALOG, "full"), { id: "full", short: "全权", isFull: true });
  assert.deepEqual(permissionTierChip([], "full"), { id: "full", short: "全权", isFull: true });
  assert.deepEqual(permissionTierChip([], "manual"), { id: "manual", short: "manual", isFull: false });
});

test("option list keeps backend order, fills labels, and marks the active tier", () => {
  const options = permissionTierOptions({ permission_tiers: CATALOG, permission_tier: "edit" });
  assert.deepEqual(options.map(item => item.id), ["manual", "edit", "auto", "full"]);
  assert.deepEqual(options.map(item => item.active), [false, true, false, false]);
  assert.equal(options[1].label, "自动改文件");
  assert.equal(options[1].short, "改文件");
  assert.equal(options[1].description, "写文件不再打断");
  // 缺 label/description 的目录项也要有可渲染文本（回退 id / 空串）。
  const sparse = permissionTierOptions({ permission_tiers: [{ id: "manual", label: "手动" }], permission_tier: "manual" });
  assert.deepEqual(sparse, [{ id: "manual", label: "手动", short: "手动", description: "", active: true }]);
});

// 芯片下拉（就地切档）与运行状态弹窗里的档位列表必须**同一份模型**：
// 否则会出现"下拉一份、弹窗一份"的漂移，用户看到的选中态取决于从哪个口进去。
test("menu items stay identical to the runtime list, only adding the full marker", () => {
  const runtime = { permission_tiers: CATALOG, permission_tier: "auto" };
  const menu = permissionTierMenuItems(runtime);
  const list = permissionTierOptions(runtime);
  assert.deepEqual(menu.map(item => item.id), list.map(item => item.id));
  assert.deepEqual(menu.map(item => item.active), list.map(item => item.active));
  assert.deepEqual(menu.map(item => item.isFull), [false, false, false, true]);
  assert.equal(menu[2].label, "自动执行");
  assert.equal(menu[2].description, "命令直跑");
});

test("nextTierIndex wraps around for the dropdown keyboard, -1 when empty", () => {
  assert.equal(nextTierIndex(0, 0, 1), -1, "没有可选条目时不聚焦");
  assert.equal(nextTierIndex(4, -1, 1), 0, "还没聚焦时 ↓ 落到第一条");
  assert.equal(nextTierIndex(4, -1, -1), 3, "还没聚焦时 ↑ 落到最后一条");
  assert.equal(nextTierIndex(4, 3, 1), 0, "末条 ↓ 回到第一条");
  assert.equal(nextTierIndex(4, 0, -1), 3, "首条 ↑ 回到末条");
  assert.equal(nextTierIndex(4, 1, 1), 2);
});
