import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { canPatchMessageBody, shouldAutoLoadOlder, shouldFollowTail } from "./conversation-view.js";

// 这一份钉的是「顶部 sentinel 自动翻更早页」的准入纪律：**用户停在尾部时一律不翻**。
//
// 背景（2026-09-27 定位）：可见窗口是"从 history_offset 起的连续一段"，翻更早页会把
// 窗口整体后退，尾部那一段随之离开 DOM。sentinel 的 IntersectionObserver 只看几何
// （rootMargin 160px），于是"容器从 display:none 重新显示 / 侧栏压缩 / 布局抖动"都会
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

test("流式正文只允许 patch 稳定消息的 body；外壳变化仍走 keyed 替换", () => {
  const before = `<article class="message assistant" data-conversation-key="message:a"><div class="message-head"><strong>AGENT</strong></div><div class="message-body" data-message-body="true">旧</div><!--/seelex-message-body--><div class="message-actions"></div></article>`;
  const next = `<article class="message assistant" data-conversation-key="message:a"><div class="message-head"><strong>AGENT</strong></div><div class="message-body" data-message-body="true">新正文</div><!--/seelex-message-body--><div class="message-actions"></div></article>`;
  const shellChanged = next.replace("<strong>AGENT</strong>", "<strong>EXEC</strong>");
  assert.equal(canPatchMessageBody(before, next), true);
  assert.equal(canPatchMessageBody(before, shellChanged), false);
  assert.equal(canPatchMessageBody("", next), false);
});

test("用户接管滚动后 auto/preserve 都不得抢回尾部", () => {
  assert.equal(shouldFollowTail("auto", true), true);
  assert.equal(shouldFollowTail("auto", false), false);
  assert.equal(shouldFollowTail("bottom", false), true);
  assert.equal(shouldFollowTail("preserve", true), false);
  assert.equal(shouldFollowTail("anchor", true), false);
});

test("会话容器暴露 aria-busy / stream phase，局部 patch 不替换整条 article", async () => {
  const source = await readFile(new URL("./conversation-view.js", import.meta.url), "utf8");
  assert.match(source, /setAttribute\?\.\("aria-busy",\s*options\.streaming \? "true" : "false"\)/);
  assert.match(source, /dataset\.streamPhase = streamPhase/);
  assert.match(source, /patchMessageBody\(node, replacement/);
  const patched = source.slice(source.indexOf("const patched"), source.indexOf("const uiState = captureUIState(node);"));
  assert.doesNotMatch(patched, /node\.replaceWith/, "可 patch 的稳定消息不得走整条 replaceWith");
});

test("消息编辑器复用 copyText 通道，并用稳定 operation_id 调一次原子 editFork", async () => {
  const source = await readFile(new URL("./conversation-view.js", import.meta.url), "utf8");
  assert.match(source, /data-edit-message/);
  assert.match(source, /operationID:\s*newEditOperationID\(\)/);
  assert.match(source, /options\.editFork\(\{/);
  assert.match(source, /expectedRevision:\s*currentEditor\.expectedRevision/);
  assert.match(source, /options\.copyText\(editor\.original\)/);
  assert.doesNotMatch(source, /navigator\.clipboard/);
  assert.match(source, /编辑内容已保留/);
});

test("app 只调用原子 EditUserMessageAndFork，不在前端拼 Fork + Submit", async () => {
  const source = await readFile(new URL("./app.js", import.meta.url), "utf8");
  assert.match(source, /invoke\("EditUserMessageAndFork", parentID, request\)/);
  assert.doesNotMatch(source, /invoke\("ForkSession",\s*parentID[\s\S]{0,200}invoke\("SubmitToSession"/);
});
