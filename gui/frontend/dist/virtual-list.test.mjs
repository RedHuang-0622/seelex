import test from "node:test";
import assert from "node:assert/strict";
import { createVirtualList } from "./virtual-list.js";

// ── 基建-B：虚拟列表核心（非 happy path）──────────────────────────────

test("空列表返回空区间且哨兵不触发", () => {
  const list = createVirtualList({ rowHeight: 24, viewportHeight: 600 });
  let topFired = 0;
  list.onReachTop(() => topFired++);
  const range = list.onScroll(100);
  assert.equal(range.start, 0);
  assert.equal(range.end, 0);
  assert.equal(range.totalHeight, 0);
  assert.equal(topFired, 0, "空列表不得触发哨兵");
});

test("可见区计算：滚动位置 → 首末行（含 overscan）", () => {
  const list = createVirtualList({ rowHeight: 24, viewportHeight: 600, overscan: 4 });
  list.setCount(1000);
  // 顶部：行 0 可见 + overscan 4 → start 0。
  const top = list.onScroll(0);
  assert.equal(top.start, 0);
  assert.equal(top.end, Math.ceil(600 / 24) + 4);
  // 中部：scrollTop 6000 → 行 250 起（6000/24）+ overscan。
  const mid = list.onScroll(6000);
  assert.equal(mid.start, 250 - 4);
  assert.ok(mid.end > 250 + 25, "end 覆盖视口 + overscan");
  assert.equal(mid.offsetY, (250 - 4) * 24);
  // 底部越界钳制：scrollTop 超过最大 → 不越过最后一行。
  const maxScroll = 1000 * 24 - 600;
  const bottom = list.onScroll(maxScroll + 100000);
  assert.ok(bottom.end <= 1000, "end 不得越过总数");
  assert.equal(bottom.totalHeight, 1000 * 24);
});

test("overscan=0 且 viewport 极小：边界不越界", () => {
  const list = createVirtualList({ rowHeight: 1, viewportHeight: 1, overscan: 0 });
  list.setCount(3);
  const range = list.onScroll(0);
  assert.ok(range.start >= 0 && range.end <= 3);
  const tail = list.onScroll(10);
  assert.ok(tail.end <= 3);
});

test("上哨兵：接近顶部触发一次，越过阈值后可再次触发", () => {
  const list = createVirtualList({ rowHeight: 24, viewportHeight: 600 });
  list.setCount(1000);
  let fired = 0;
  list.onReachTop(() => fired++);
  list.onScroll(0);
  assert.equal(fired, 1, "顶部触发");
  list.onScroll(50); // 仍在阈值内 → 不重复
  assert.equal(fired, 1);
  list.onScroll(700); // 离开顶部阈值
  list.onScroll(0); // 回到顶部 → 再触发
  assert.equal(fired, 2, "返回顶部后重新触发");
});

test("下哨兵：接近底部触发一次", () => {
  const list = createVirtualList({ rowHeight: 24, viewportHeight: 600 });
  list.setCount(100);
  let fired = 0;
  list.onReachBottom(() => fired++);
  list.onScroll(100 * 24 - 600); // 底部
  assert.equal(fired, 1);
  list.onScroll(100 * 24 - 700); // 稍离底部 → 不重复
  assert.equal(fired, 1);
});

test("setCount 变化后可见区自适应（消息追加/历史加载）", () => {
  const list = createVirtualList({ rowHeight: 24, viewportHeight: 600 });
  list.setCount(50);
  const before = list.onScroll(0);
  assert.equal(before.totalHeight, 50 * 24);
  list.setCount(2000); // 历史加载（向上增长）
  const after = list.setCount(2000);
  assert.equal(after.totalHeight, 2000 * 24);
  assert.equal(after.start, 0);
});

test("scrollTo 恢复位置（锚点）", () => {
  const list = createVirtualList({ rowHeight: 24, viewportHeight: 600 });
  list.setCount(500);
  const restored = list.scrollTo(2400);
  assert.equal(restored.start, 100 - 8); // 2400/24 = 100，含默认 overscan 8
});
