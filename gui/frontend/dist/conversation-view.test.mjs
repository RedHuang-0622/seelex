import test from "node:test";
import assert from "node:assert/strict";
import { shouldAutoLoadOlder } from "./conversation-view.js";

// 这一份钉的是「顶部 sentinel 自动翻更早页」的准入纪律：**用户停在尾部时一律不翻**。
//
// 背景（2026-09-27 定位）：可见窗口是"从 history_offset 起的连续一段"，翻更早页会把
// 窗口整体后退，尾部那一段随之离开 DOM。sentinel 的 IntersectionObserver 只看几何
// （rootMargin 160px），于是"容器从 display:none 重新显示 / 侧栏折叠 / 布局抖动"都会
// 在用户根本没滚到顶时触发它——最新消息就这样被静默移出窗口。这条纪律是唯一判据，
// 因此单独抽成纯函数在这里钉住；显式的「加载更早」按钮走宿主命令，不受此限制。

const tailState = {
  hasLoader: true,
  canLoadMore: true,
  loadingOlder: false,
  sentinelArmed: true,
  followsTail: true
};

test("停在尾部时不自动翻更早页（最新消息不得被移出窗口）", () => {
  assert.equal(shouldAutoLoadOlder(tailState), false);
  // 关键回归：sentinel 被几何变化"叫醒"（armed）也不能翻——尾部才是判据。
  assert.equal(shouldAutoLoadOlder({ ...tailState, sentinelArmed: true, followsTail: true }), false);
});

test("离开尾部且有更早历史时才自动翻页", () => {
  assert.equal(shouldAutoLoadOlder({ ...tailState, followsTail: false }), true);
});

test("没有更早历史 / 正在翻页 / 未武装 / 无加载器时都不翻", () => {
  assert.equal(shouldAutoLoadOlder({ ...tailState, followsTail: false, canLoadMore: false }), false);
  assert.equal(shouldAutoLoadOlder({ ...tailState, followsTail: false, loadingOlder: true }), false);
  assert.equal(shouldAutoLoadOlder({ ...tailState, followsTail: false, sentinelArmed: false }), false);
  assert.equal(shouldAutoLoadOlder({ ...tailState, followsTail: false, hasLoader: false }), false);
});

test("缺省/畸形状态按不翻处理（绝不因为状态缺失就去翻页）", () => {
  assert.equal(shouldAutoLoadOlder(undefined), false);
  assert.equal(shouldAutoLoadOlder({}), false);
});
